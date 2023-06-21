/*
Copyright 2023 The Firefly Authors.

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

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster",categories={firefly-io}
// +kubebuilder:printcolumn:name="READY",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Cluster is ready"
// +kubebuilder:printcolumn:name="VERSION",type=string,JSONPath=`.status.clusterInfo.serverVersion`,description="Server Version is the Kubernetes version which is reported by the cluster from the API endpoint '/version'"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Cluster is a worker cluster in Firefly.
// Each cluster will have a unique identifier in the cache (i.e. in etcd).
type Cluster struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the behavior of a cluster.
	// https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec ClusterSpec `json:"spec,omitempty"`

	// Most recently observed status of the cluster.
	// Populated by the system.
	// Read-only.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status ClusterStatus `json:"status,omitempty"`
}

// ClusterSpec describes the attributes that a cluster is created with.
type ClusterSpec struct {
	// Connection is the connection information for the cluster.
	Connection Connection `json:"connection,omitempty"`
}

// Connection is the connection information for the cluster.
type Connection struct {
	// KubeConfig is the kubeconfig for the cluster.
	KubeConfig []byte `json:"kubeconfig,omitempty"`
}

// ClusterStatus is information about the current status of a cluster.
type ClusterStatus struct {
	// Conditions is an array of current observed cluster conditions.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []ClusterCondition `json:"conditions,omitempty"`
	// Set of ids/uuids to uniquely identify the cluster.
	// +optional
	ClusterInfo ClusterSystemInfo `json:"clusterInfo,omitempty"`
}

type ClusterConditionType string

const (
	//ClusterReady means cluster is healthy and ready to accept pods.
	ClusterReadyClusterConditionType = "Ready"
	//ClusterMemoryPressure means the cluster is under pressure due to insufficient available memory.
	ClusterMemoryPressureClusterConditionType = "MemoryPressure"
	//ClusterDiskPressure means the cluster is under pressure due to insufficient available disk.
	ClusterDiskPressureClusterConditionType = "DiskPressure"
	//ClusterPIDPressure means the cluster is under pressure due to insufficient available PID.
	ClusterPIDPressureClusterConditionType = "PIDPressure"
	//ClusterNetworkUnavailable means that network is not correctly configured between the Firefly and Cluster.
	ClusterNetworkUnavailableClusterConditionType = "NetworkUnavailable"
)

// ClusterCondition contains condition information for a cluster.
type ClusterCondition struct {
	// Type of cluster condition.
	Type ClusterConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status v1.ConditionStatus `json:"status"`
	// Last time we got an update on a given condition.
	// +optional
	LastHeartbeatTime metav1.Time `json:"lastHeartbeatTime,omitempty"`
	// Last time the condition transit from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// (brief) reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Human readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// ClusterSystemInfo is a set of ids/uuids to uniquely identify the cluster.
type ClusterSystemInfo struct {
	// ClusterID reported by the cluster. For unique cluster identification
	// in Firefly this field is preferred.
	ClusterID string `json:"clusterID,omitempty"`

	// Server Version is the Kubernetes version which is reported by the cluster from the API endpoint '/version' (e.g. 1.27).
	ServerVersion string `json:"serverVersion,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterList is the whole list of all Clusters which have been registered with Firefly.
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// List of clusters
	Items []Cluster `json:"items"`
}
