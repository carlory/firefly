/*
Copyright 2022 The Firefly Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package node

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	fireflyclient "github.com/carlory/firefly/pkg/generated/clientset/versioned"
	clusterinformers "github.com/carlory/firefly/pkg/generated/informers/externalversions/cluster/v1alpha1"
	clusterlisters "github.com/carlory/firefly/pkg/generated/listers/cluster/v1alpha1"
	"github.com/carlory/firefly/pkg/scheme"
)

/**

In feature, we will re-implement the node controller to manage the node resources in the cluster
based on mulit-cluster informers .

**/

const (
	// maxRetries is the number of times a cluster will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a cluster.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	// name of the cluster controller finalizer
	NodeControllerFinalizerName = "cluster-node.firefly.io/finalizer"
)

// NewNodeController returns a new *Controller.
func NewNodeController(
	client kubernetes.Interface,
	fireflyClient fireflyclient.Interface,
	clusterInformer clusterinformers.ClusterInformer,
	nodesInformer coreinformers.NodeInformer,
	workerNodesInformer coreinformers.NodeInformer,
	resyncPeriod time.Duration) (*NodeController, error) {
	broadcaster := record.NewBroadcaster()
	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "node-controller"})

	ctrl := &NodeController{
		client:            client,
		fireflyClient:     fireflyClient,
		clustersLister:    clusterInformer.Lister(),
		clustersSynced:    clusterInformer.Informer().HasSynced,
		nodesLister:       nodesInformer.Lister(),
		nodesSynced:       nodesInformer.Informer().HasSynced,
		workerNodesLister: workerNodesInformer.Lister(),
		workerNodesSynced: workerNodesInformer.Informer().HasSynced,
		queue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "node"),
		workerLoopPeriod:  time.Second,
		eventBroadcaster:  broadcaster,
		eventRecorder:     recorder,
	}

	// clusterInformer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
	// 	AddFunc:    ctrl.addCluster,
	// 	UpdateFunc: ctrl.updateCluster,
	// 	DeleteFunc: ctrl.deleteCluster,
	// }, resyncPeriod)

	nodesInformer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		UpdateFunc: ctrl.updateNode,
		DeleteFunc: ctrl.deleteNode,
	}, resyncPeriod)

	workerNodesInformer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addWorkerNode,
		UpdateFunc: ctrl.updateWorkerNode,
		DeleteFunc: ctrl.deleteWorkerNode,
	}, resyncPeriod)

	return ctrl, nil
}

type NodeController struct {
	client           kubernetes.Interface
	fireflyClient    fireflyclient.Interface
	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	clustersLister clusterlisters.ClusterLister
	clustersSynced cache.InformerSynced

	nodesLister corelisters.NodeLister
	nodesSynced cache.InformerSynced

	workerNodesLister corelisters.NodeLister
	workerNodesSynced cache.InformerSynced

	// Cluster that need to be updated. A channel is inappropriate here,
	// because it allows services with lots of pods to be serviced much
	// more often than services with few pods; it also would cause a
	// service that's inserted multiple times to be processed more than
	// necessary.
	queue workqueue.RateLimitingInterface

	// workerLoopPeriod is the time between worker runs. The workers process the queue of service and pod changes.
	workerLoopPeriod time.Duration
}

// Run will not return until stopCh is closed. workers determines how many
// cluster will be handled in parallel.
func (ctrl *NodeController) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()

	// Start events processing pipelinctrl.
	ctrl.eventBroadcaster.StartStructuredLogging(0)
	ctrl.eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: ctrl.client.CoreV1().Events("")})
	defer ctrl.eventBroadcaster.Shutdown()

	defer ctrl.queue.ShutDown()

	klog.Infof("Starting node controller")
	defer klog.Infof("Shutting down node controller")

	if !cache.WaitForNamedCacheSync("node", ctx.Done(), ctrl.clustersSynced, ctrl.nodesSynced, ctrl.workerNodesSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, ctrl.worker, ctrl.workerLoopPeriod)
	}
	<-ctx.Done()
}

// worker runs a worker thread that just dequeues items, processes them, and
// marks them done. You may run as many of these in parallel as you wish; the
// workqueue guarantees that they will not end up processing the same service
// at the same time.
func (ctrl *NodeController) worker(ctx context.Context) {
	for ctrl.processNextWorkItem(ctx) {
	}
}

func (ctrl *NodeController) processNextWorkItem(ctx context.Context) bool {
	key, quit := ctrl.queue.Get()
	if quit {
		return false
	}
	defer ctrl.queue.Done(key)

	err := ctrl.sync(ctx, key.(string))
	ctrl.handleErr(err, key)

	return true
}

func (ctrl *NodeController) updateNode(old, cur interface{}) {
	oldNode := old.(*v1.Node)
	curNode := cur.(*v1.Node)
	klog.V(4).InfoS("Updating node", "node", klog.KObj(oldNode))

	node := curNode.DeepCopy()
	key, _ := cache.MetaNamespaceKeyFunc(node)
	cluster, _, _, _ := cache.SplitClusterMetaNamespaceKey(key)
	if cluster == "" {
		return
	}

	_, err := ctrl.clustersLister.Get(cluster)
	if err != nil {
		klog.V(2).InfoS("Cluster has been deleted", "cluster", cluster)
		return
	}

	node.Name = strings.TrimSuffix(node.Name, "."+cluster)
	ctrl.enqueue(node)
}

func (ctrl *NodeController) deleteNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		node, ok = tombstone.Obj.(*v1.Node)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Node %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Deleting node", "node", klog.KObj(node))
	clone := node.DeepCopy()
	key, _ := cache.MetaNamespaceKeyFunc(node)
	cluster, _, _, _ := cache.SplitClusterMetaNamespaceKey(key)
	_, err := ctrl.clustersLister.Get(cluster)
	if err != nil {
		klog.V(2).InfoS("Cluster has been deleted", "cluster", cluster)
		return
	}
	clone.Name = strings.TrimSuffix(clone.Name, "."+cluster)
	ctrl.enqueue(clone)
}

func (ctrl *NodeController) addWorkerNode(obj interface{}) {
	node := obj.(*v1.Node)
	klog.V(4).InfoS("Adding node", "node", klog.KObj(node))
	ctrl.enqueue(node)
}

func (ctrl *NodeController) updateWorkerNode(old, cur interface{}) {
	oldNode := old.(*v1.Node)
	curNode := cur.(*v1.Node)
	klog.V(4).InfoS("Updating node", "node", klog.KObj(oldNode))
	ctrl.enqueue(curNode)
}

func (ctrl *NodeController) deleteWorkerNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		node, ok = tombstone.Obj.(*v1.Node)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Node %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Deleting node", "node", klog.KObj(node))
	ctrl.enqueue(node)
}

func (ctrl *NodeController) enqueue(node *v1.Node) {
	key, err := cache.MetaNamespaceKeyFunc(node)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	ctrl.queue.Add(key)
}

func (ctrl *NodeController) handleErr(err error, key interface{}) {
	if err == nil {
		ctrl.queue.Forget(key)
		return
	}
	cluster, namespace, name, keyErr := cache.SplitClusterMetaNamespaceKey(key.(string))
	if keyErr != nil {
		klog.ErrorS(err, "Failed to split meta cache key", "cacheKey", key)
	}

	if ctrl.queue.NumRequeues(key) < maxRetries {
		klog.V(2).InfoS("Error syncing node, retrying", "cluster", cluster, "node", klog.KRef(namespace, name), "err", err)
		ctrl.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	klog.V(2).InfoS("Dropping node out of the queue", "cluster", cluster, "node", klog.KRef(namespace, name), "err", err)
	ctrl.queue.Forget(key)
}

func (ctrl *NodeController) sync(ctx context.Context, key string) error {
	clusterName, _, name, err := cache.SplitClusterMetaNamespaceKey(key)
	if err != nil {
		klog.ErrorS(err, "Failed to split meta cache key", "cacheKey", key)
		return err
	}

	startTime := time.Now()
	klog.V(4).InfoS("Started syncing node", "cluster", clusterName, "node", name, "startTime", startTime)
	defer func() {
		klog.V(4).InfoS("Finished syncing node", "cluster", clusterName, "node", name, "duration", time.Since(startTime))
	}()

	_, err = ctrl.clustersLister.Get(clusterName)
	if errors.IsNotFound(err) {
		klog.V(2).InfoS("Cluster has been deleted", "cluster", clusterName)
		return nil
	}
	if err != nil {
		return err
	}

	workerNode, err := ctrl.workerNodesLister.Get(key)
	if errors.IsNotFound(err) {
		klog.V(2).InfoS("Node has been deleted", "cluster", clusterName, "node", name)
		return nil
	}

	klog.InfoS("Syncing node", "node", klog.KObj(workerNode))
	return ctrl.createOrUpdateFirelyNode(ctx, clusterName, workerNode.DeepCopy())
}

func (ctrl *NodeController) createOrUpdateFirelyNode(ctx context.Context, clusterName string, clusterNode *v1.Node) error {

	var fireflyNodeName = func(clusterName, nodeName string) string {
		return fmt.Sprintf("%s.%s", nodeName, clusterName)
	}

	node, err := ctrl.client.CoreV1().Nodes().Get(ctx, fireflyNodeName(clusterName, clusterNode.Name), metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// Create node
		clusterNode.Finalizers = []string{}
		clusterNode.OwnerReferences = nil
		clusterNode.ResourceVersion = ""
		clusterNode.UID = ""
		clusterNode.CreationTimestamp = metav1.Time{}
		clusterNode.DeletionTimestamp = nil
		clusterNode.Generation = 0
		clusterNode.GenerateName = ""
		clusterNode.Name = fireflyNodeName(clusterName, clusterNode.Name)
		got, err := ctrl.client.CoreV1().Nodes().Create(ctx, clusterNode, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		clusterNode.ResourceVersion = got.ResourceVersion
		_, err = ctrl.client.CoreV1().Nodes().UpdateStatus(ctx, clusterNode, metav1.UpdateOptions{})
		return err
	}

	// Update node
	clusterNode.ResourceVersion = node.ResourceVersion
	clusterNode.UID = node.UID
	clusterNode.Name = fireflyNodeName(clusterName, clusterNode.Name)
	// got, err := ctrl.client.CoreV1().Nodes().Update(ctx, clusterNode, metav1.UpdateOptions{})
	// if err != nil {
	// 	return err
	// }
	clusterNode.ResourceVersion = node.ResourceVersion
	_, err = ctrl.client.CoreV1().Nodes().UpdateStatus(ctx, clusterNode, metav1.UpdateOptions{})
	return err
}
