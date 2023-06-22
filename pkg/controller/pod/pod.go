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

package pod

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
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterinformers "github.com/carlory/firefly/pkg/generated/informers/externalversions/cluster/v1alpha1"
	clusterlisters "github.com/carlory/firefly/pkg/generated/listers/cluster/v1alpha1"
	"github.com/carlory/firefly/pkg/scheme"
	utilcluster "github.com/carlory/firefly/pkg/util/cluster"
)

/**

In feature, we will re-implement the pod controller to manage the pod resources in the cluster
based on mulit-cluster informers .

**/

const (
	// maxRetries is the number of times a cluster will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a cluster.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15

	// name of the pod controller finalizer
	PodControllerFinalizerName = "pod.firefly.io/finalizer"
)

// NewPodController returns a new *Controller.
func NewPodController(
	client kubernetes.Interface,
	podInformer coreinformers.PodInformer,
	clusterInformer clusterinformers.ClusterInformer,
	workerPodsInformer coreinformers.PodInformer,
	resyncPeriod time.Duration) (*PodController, error) {
	broadcaster := record.NewBroadcaster()
	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "pod-controller"})

	ctrl := &PodController{
		client:            client,
		podInformer:       podInformer,
		podsSynced:        podInformer.Informer().HasSynced,
		clustersLister:    clusterInformer.Lister(),
		clustersSynced:    clusterInformer.Informer().HasSynced,
		workerPodInformer: workerPodsInformer,
		workerPodsSynced:  workerPodsInformer.Informer().HasSynced,
		queue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "pod"),
		workerLoopPeriod:  time.Second,
		eventBroadcaster:  broadcaster,
		eventRecorder:     recorder,
	}

	podInformer.Informer().AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addPod,
		UpdateFunc: ctrl.updatePod,
		DeleteFunc: ctrl.deletePod,
	}, resyncPeriod)

	workerPodsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.addWorkerPod,
		UpdateFunc: ctrl.updateWorkerPod,
		DeleteFunc: ctrl.deleteWorkerPod,
	})

	return ctrl, nil
}

type PodController struct {
	client           kubernetes.Interface
	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	podInformer coreinformers.PodInformer
	podsSynced  cache.InformerSynced

	clustersLister clusterlisters.ClusterLister
	clustersSynced cache.InformerSynced

	workerPodInformer coreinformers.PodInformer
	workerPodsSynced  cache.InformerSynced

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
func (ctrl *PodController) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()

	// Start events processing pipelinctrl.
	ctrl.eventBroadcaster.StartStructuredLogging(0)
	ctrl.eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: ctrl.client.CoreV1().Events("")})
	defer ctrl.eventBroadcaster.Shutdown()

	defer ctrl.queue.ShutDown()

	klog.Infof("Starting pod controller")
	defer klog.Infof("Shutting down pod controller")

	if !cache.WaitForNamedCacheSync("pod", ctx.Done(), ctrl.clustersSynced, ctrl.podsSynced, ctrl.workerPodsSynced) {
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
func (ctrl *PodController) worker(ctx context.Context) {
	for ctrl.processNextWorkItem(ctx) {
	}
}

func (ctrl *PodController) processNextWorkItem(ctx context.Context) bool {
	key, quit := ctrl.queue.Get()
	if quit {
		return false
	}
	defer ctrl.queue.Done(key)

	err := ctrl.sync(ctx, key.(string))
	ctrl.handleErr(err, key)

	return true
}

func (ctrl *PodController) addWorkerPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	klog.V(4).InfoS("Adding pod", "pod", klog.KObj(pod))
	key, err := cache.MetaNamespaceKeyFunc(pod)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	ctrl.queue.Add(key)
}

func (ctrl *PodController) updateWorkerPod(old, cur interface{}) {
	oldPod := old.(*v1.Pod)
	curPod := cur.(*v1.Pod)
	klog.V(4).InfoS("Updating pod", "pod", klog.KObj(oldPod))
	key, err := cache.MetaNamespaceKeyFunc(curPod)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	ctrl.queue.Add(key)
}

func (ctrl *PodController) deleteWorkerPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Pod %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Deleting pod", "pod", klog.KObj(pod))
	key, err := cache.MetaNamespaceKeyFunc(pod)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	ctrl.queue.Add(key)
}

func (ctrl *PodController) addPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	klog.V(4).InfoS("Adding pod", "pod", klog.KObj(pod))
	key, err := ClusterMetaNamespaceKeyFunc(pod)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	ctrl.queue.Add(key)
}

func (ctrl *PodController) updatePod(old, cur interface{}) {
	oldPod := old.(*v1.Pod)
	curPod := cur.(*v1.Pod)
	klog.V(4).InfoS("Updating pod", "pod", klog.KObj(oldPod))
	key, err := ClusterMetaNamespaceKeyFunc(curPod)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	ctrl.queue.Add(key)
}

func (ctrl *PodController) deletePod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Pod %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Deleting pod", "pod", klog.KObj(pod))
	key, err := ClusterMetaNamespaceKeyFunc(pod)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	ctrl.queue.Add(key)
}

func (ctrl *PodController) handleErr(err error, key interface{}) {
	if err == nil {
		ctrl.queue.Forget(key)
		return
	}
	namespace, name, keyErr := cache.SplitMetaNamespaceKey(key.(string))
	if keyErr != nil {
		klog.ErrorS(err, "Failed to split meta cache key", "cacheKey", key)
	}

	if ctrl.queue.NumRequeues(key) < maxRetries {
		klog.V(2).InfoS("Error syncing pod, retrying", "pod", klog.KRef(namespace, name), "err", err)
		ctrl.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	klog.V(2).InfoS("Dropping cluster out of the queue", "pod", klog.KRef(namespace, name), "err", err)
	ctrl.queue.Forget(key)
}

func (ctrl *PodController) sync(ctx context.Context, key string) error {
	clusterName, namespace, name, err := cache.SplitClusterMetaNamespaceKey(key)
	if err != nil {
		klog.ErrorS(err, "Failed to split meta cache key", "cacheKey", key)
		return err
	}

	startTime := time.Now()
	klog.V(4).InfoS("Started syncing pod", "cluster", clusterName, "pod", klog.KRef(namespace, name), "startTime", startTime)
	defer func() {
		klog.V(4).InfoS("Finished syncing pod", "cluster", clusterName, "pod", klog.KRef(namespace, name), "duration", time.Since(startTime))
	}()

	pod, err := ctrl.podInformer.Lister().Pods(namespace).Get(name)
	if errors.IsNotFound(err) {
		klog.V(2).InfoS("pod has been deleted", "pod", klog.KRef(namespace, name), "cluster", clusterName)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	// TODO: Deep-copy only when needed.
	pod = pod.DeepCopy()

	// examine DeletionTimestamp to determine if object is under deletion
	if pod.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !controllerutil.ContainsFinalizer(pod, PodControllerFinalizerName) {
			controllerutil.AddFinalizer(pod, PodControllerFinalizerName)
			pod, err = ctrl.client.CoreV1().Pods(namespace).Update(ctx, pod, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(pod, PodControllerFinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := ctrl.deleteUnableGCResources(clusterName, pod); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return err
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(pod, PodControllerFinalizerName)
			_, err := ctrl.client.CoreV1().Pods(namespace).Update(ctx, pod, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			// Stop reconciliation as the item is being deleted
			return nil
		}
		return ctrl.deleteUnableGCResources(clusterName, pod)
	}

	klog.InfoS("Syncing pod", "pod", klog.KObj(pod))

	cluster, err := ctrl.clustersLister.Get(clusterName)
	if errors.IsNotFound(err) {
		klog.V(2).InfoS("Cluster has been deleted", "cluster", clusterName)
		return nil
	}
	if err != nil {
		return err
	}
	client, err := utilcluster.NewKubeClientForCluster(ctx, cluster, "pod-controller")
	if err != nil {
		klog.ErrorS(err, "Failed to create client for cluster", "cluster", clusterName)
		return err
	}

	actualPod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// do create
		actualPod = pod.DeepCopy()
		actualPod.Spec.NodeName = strings.TrimSuffix(pod.Spec.NodeName, "."+clusterName)
		actualPod.Finalizers = []string{}
		actualPod.OwnerReferences = nil
		actualPod.ResourceVersion = ""
		actualPod.UID = ""
		actualPod.CreationTimestamp = metav1.Time{}
		actualPod.DeletionTimestamp = nil
		actualPod.Generation = 0
		actualPod.GenerateName = ""
		actualPod, err = client.CoreV1().Pods(namespace).Create(ctx, actualPod, metav1.CreateOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to create pod", "pod", klog.KObj(pod))
			return err
		}
		klog.InfoS("Created pod", "pod", klog.KObj(actualPod))
	}

	// do update
	pod.Status = actualPod.Status
	_, err = ctrl.client.CoreV1().Pods(namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	return err
}

// deleteUnableGCResources deletes the resources that are unable to garbage collect.
func (ctrl *PodController) deleteUnableGCResources(clusterName string, pod *v1.Pod) error {
	cluster, err := ctrl.clustersLister.Get(clusterName)
	if err != nil {
		return err
	}
	client, err := utilcluster.NewKubeClientForCluster(context.Background(), cluster, "pod-controller")
	if err != nil {
		klog.ErrorS(err, "Failed to create client for cluster", "cluster", clusterName)
		return err
	}
	var gracePeriodSeconds int64 = 0
	err = client.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return ctrl.client.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds})
}
