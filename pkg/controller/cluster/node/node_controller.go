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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1alpha1 "github.com/carlory/firefly/pkg/apis/cluster/v1alpha1"
	fireflyclient "github.com/carlory/firefly/pkg/generated/clientset/versioned"
	clusterinformers "github.com/carlory/firefly/pkg/generated/informers/externalversions/cluster/v1alpha1"
	clusterlisters "github.com/carlory/firefly/pkg/generated/listers/cluster/v1alpha1"
	"github.com/carlory/firefly/pkg/scheme"
)

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
	resyncPeriod time.Duration) (*NodeController, error) {
	broadcaster := record.NewBroadcaster()
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "node-controller"})

	ctrl := &NodeController{
		client:           client,
		fireflyClient:    fireflyClient,
		clustersLister:   clusterInformer.Lister(),
		clustersSynced:   clusterInformer.Informer().HasSynced,
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cluster"),
		workerLoopPeriod: time.Second,
		eventBroadcaster: broadcaster,
		eventRecorder:    recorder,
	}

	clusterInformer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addCluster,
		UpdateFunc: ctrl.updateCluster,
		DeleteFunc: ctrl.deleteCluster,
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

	klog.Infof("Starting cluster controller")
	defer klog.Infof("Shutting down cluster controller")

	if !cache.WaitForNamedCacheSync("cluster", ctx.Done(), ctrl.clustersSynced) {
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

func (ctrl *NodeController) addCluster(obj interface{}) {
	cluster := obj.(*clusterv1alpha1.Cluster)
	klog.V(4).InfoS("Adding cluster", "cluster", klog.KObj(cluster))
	ctrl.enqueue(cluster)
}

func (ctrl *NodeController) updateCluster(old, cur interface{}) {
	oldCluster := old.(*clusterv1alpha1.Cluster)
	curCluster := cur.(*clusterv1alpha1.Cluster)
	klog.V(4).InfoS("Updating cluster", "cluster", klog.KObj(oldCluster))
	ctrl.enqueue(curCluster)
}

func (ctrl *NodeController) deleteCluster(obj interface{}) {
	cluster, ok := obj.(*clusterv1alpha1.Cluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		cluster, ok = tombstone.Obj.(*clusterv1alpha1.Cluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Cluster %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Deleting cluster", "cluster", klog.KObj(cluster))
	ctrl.enqueue(cluster)
}

func (ctrl *NodeController) enqueue(cluster *clusterv1alpha1.Cluster) {
	key, err := cache.MetaNamespaceKeyFunc(cluster)
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
	_, clusterName, keyErr := cache.SplitMetaNamespaceKey(key.(string))
	if keyErr != nil {
		klog.ErrorS(err, "Failed to split meta cache key", "cacheKey", key)
	}

	if ctrl.queue.NumRequeues(key) < maxRetries {
		klog.V(2).InfoS("Error syncing cluster, retrying", "cluster", clusterName, "err", err)
		ctrl.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	klog.V(2).InfoS("Dropping cluster out of the queue", "cluster", clusterName, "err", err)
	ctrl.queue.Forget(key)
}

func (ctrl *NodeController) sync(ctx context.Context, key string) error {
	_, clusterName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.ErrorS(err, "Failed to split meta cache key", "cacheKey", key)
		return err
	}

	startTime := time.Now()
	klog.V(4).InfoS("Started syncing cluster", "cluster", clusterName, "startTime", startTime)
	defer func() {
		klog.V(4).InfoS("Finished syncing cluster", "cluster", clusterName, "duration", time.Since(startTime))
	}()

	cluster, err := ctrl.clustersLister.Get(clusterName)
	if errors.IsNotFound(err) {
		klog.V(2).InfoS("Cluster has been deleted", "cluster", clusterName)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	cluster = cluster.DeepCopy()

	// examine DeletionTimestamp to determine if object is under deletion
	if cluster.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !controllerutil.ContainsFinalizer(cluster, NodeControllerFinalizerName) {
			controllerutil.AddFinalizer(cluster, NodeControllerFinalizerName)
			cluster, err = ctrl.fireflyClient.ClusterV1alpha1().Clusters().Update(ctx, cluster, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(cluster, NodeControllerFinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := ctrl.deleteUnableGCResources(cluster); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return err
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(cluster, NodeControllerFinalizerName)
			_, err := ctrl.fireflyClient.ClusterV1alpha1().Clusters().Update(ctx, cluster, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			// Stop reconciliation as the item is being deleted
			return nil
		}
	}

	klog.InfoS("Syncing cluster", "cluster", klog.KObj(cluster))

	return nil
}

// deleteUnableGCResources deletes the resources that are unable to garbage collect.
func (ctrl *NodeController) deleteUnableGCResources(cluster *clusterv1alpha1.Cluster) error {
	return nil
}
