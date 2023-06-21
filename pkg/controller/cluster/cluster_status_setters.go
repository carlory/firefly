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

package cluster

import (
	"context"
	"fmt"
	"net/http"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	clusterv1alpha1 "github.com/carlory/firefly/pkg/apis/cluster/v1alpha1"
	utilcluster "github.com/carlory/firefly/pkg/util/cluster"
)

// defaultNodeStatusFuncs is a factory that generates the default set of
// setNodeStatus funcs
func (ctrl *ClusterController) defaultClusterStatusFuncs() []func(context.Context, *clusterv1alpha1.Cluster) error {
	setters := []func(context.Context, *clusterv1alpha1.Cluster) error{
		ctrl.gatherClusterSystemInfo,
		// condition funcs
		ctrl.setReadyCondition,
	}
	return setters
}

// gatherClusterSystemInfo gathers information about the cluster and sets it in the cluster's status
func (ctrl *ClusterController) gatherClusterSystemInfo(ctx context.Context, cluster *clusterv1alpha1.Cluster) error {
	kubeClient, err := utilcluster.NewKubeClientForCluster(ctx, cluster, "cluster-controller")
	if err != nil {
		klog.ErrorS(err, "Failed to create client for cluster")
		return err
	}

	// try to get the cluster's id
	ns, err := kubeClient.CoreV1().Namespaces().Get(ctx, metav1.NamespaceSystem, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to get kube-system namespace")
		return err
	}

	// try to get the cluster's version
	versionInfo, err := kubeClient.Discovery().ServerVersion()
	if err != nil {
		klog.ErrorS(err, "Failed to get cluster version")
		return err
	}

	clusterInfo := clusterv1alpha1.ClusterSystemInfo{
		ClusterID:     string(ns.UID),
		ServerVersion: fmt.Sprintf("%s.%s", versionInfo.Major, versionInfo.Minor),
	}
	cluster.Status.ClusterInfo = clusterInfo
	return nil
}

// setReadyCondition sets the ready condition on the cluster's status
func (ctrl *ClusterController) setReadyCondition(ctx context.Context, cluster *clusterv1alpha1.Cluster) error {
	currentTime := metav1.Now()
	newClusterReadyCondition := clusterv1alpha1.ClusterCondition{
		Type:              clusterv1alpha1.ClusterReadyClusterConditionType,
		Status:            v1.ConditionTrue,
		Reason:            "ReadyzEndpointReady",
		Message:           "the kube-apiserver of the cluster is posting ready status",
		LastHeartbeatTime: currentTime,
	}

	discoveryClient, err := utilcluster.NewDiscoveryClientForCluster(ctx, cluster, "cluster-controller")
	if err != nil {
		newClusterReadyCondition = clusterv1alpha1.ClusterCondition{
			Type:              clusterv1alpha1.ClusterReadyClusterConditionType,
			Status:            v1.ConditionUnknown,
			Reason:            "InvalidClusterConnection",
			Message:           err.Error(),
			LastHeartbeatTime: currentTime,
		}
	}

	readyzStatus, err := ctrl.doEndpointCheck(discoveryClient.RESTClient(), "/readyz")
	if err != nil && readyzStatus == http.StatusNotFound {
		// do health check with healthz endpoint if the readyz endpoint is not installed in member cluster
		readyzStatus, err = ctrl.doEndpointCheck(discoveryClient.RESTClient(), "/healthz")
	}

	if err != nil {
		newClusterReadyCondition = clusterv1alpha1.ClusterCondition{
			Type:              clusterv1alpha1.ClusterReadyClusterConditionType,
			Status:            v1.ConditionUnknown,
			Reason:            "ReadyzEndpointFailed",
			Message:           err.Error(),
			LastHeartbeatTime: currentTime,
		}
	}

	if err == nil && readyzStatus != http.StatusOK {
		newClusterReadyCondition = clusterv1alpha1.ClusterCondition{
			Type:              clusterv1alpha1.ClusterReadyClusterConditionType,
			Status:            v1.ConditionFalse,
			Reason:            "ReadyzEndpointNotReady",
			Message:           "the kube-apiserver of the cluster is running but not ready",
			LastHeartbeatTime: currentTime,
		}
	}

	readyConditionUpdated := false
	needToRecordEvent := false
	for i := range cluster.Status.Conditions {
		if cluster.Status.Conditions[i].Type == clusterv1alpha1.ClusterReadyClusterConditionType {
			if cluster.Status.Conditions[i].Status == newClusterReadyCondition.Status {
				newClusterReadyCondition.LastTransitionTime = cluster.Status.Conditions[i].LastTransitionTime
			} else {
				newClusterReadyCondition.LastTransitionTime = currentTime
				needToRecordEvent = true
			}
			cluster.Status.Conditions[i] = newClusterReadyCondition
			readyConditionUpdated = true
			break
		}
	}
	if !readyConditionUpdated {
		newClusterReadyCondition.LastTransitionTime = currentTime
		cluster.Status.Conditions = append(cluster.Status.Conditions, newClusterReadyCondition)
	}
	if needToRecordEvent {
		if newClusterReadyCondition.Status == v1.ConditionTrue {
			ctrl.recordEvent(cluster, v1.EventTypeNormal, ClusterReady)
		} else {
			ctrl.recordEvent(cluster, v1.EventTypeNormal, ClusterNotReady)
			klog.InfoS("Cluster became not ready", "cluster", klog.KObj(cluster), "condition", newClusterReadyCondition)
		}
	}
	return nil
}
