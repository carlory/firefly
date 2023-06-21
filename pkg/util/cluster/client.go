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

package cluster

import (
	"context"
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clusterv1alpha1 "github.com/carlory/firefly/pkg/apis/cluster/v1alpha1"
	"github.com/carlory/firefly/pkg/clientbuilder"
)

// RESTConfigForCluster creates a rest config for the given cluster.
func RESTConfigForCluster(cluster *clusterv1alpha1.Cluster) (*rest.Config, error) {
	if len(cluster.Spec.Connection.KubeConfig) == 0 {
		return nil, fmt.Errorf("kubeconfig is empty")
	}
	return clientcmd.RESTConfigFromKubeConfig(cluster.Spec.Connection.KubeConfig)
}

// NewKubeClientForCluster creates a new kubernetes client for the given cluster.
func NewKubeClientForCluster(ctx context.Context, cluster *clusterv1alpha1.Cluster, clientUserAgent string) (kubernetes.Interface, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(cluster.Spec.Connection.KubeConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to create rest config for cluster", "cluster", cluster.Name)
		return nil, err
	}

	clientBuilder := clientbuilder.SimpleControllerClientBuilder{ClientConfig: restConfig}
	kubeClient, err := clientBuilder.Client(clientUserAgent)
	if err != nil {
		klog.ErrorS(err, "Failed to create kube client for cluster", "cluster", cluster.Name)
		return nil, err
	}
	return kubeClient, nil
}

// NewDiscoveryClientForCluster creates a new discovery client for the given cluster.
func NewDiscoveryClientForCluster(ctx context.Context, cluster *clusterv1alpha1.Cluster, clientUserAgent string) (discovery.DiscoveryInterface, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(cluster.Spec.Connection.KubeConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to create rest config for cluster", "cluster", cluster.Name)
		return nil, err
	}

	clientBuilder := clientbuilder.SimpleControllerClientBuilder{ClientConfig: restConfig}
	discoveryClient, err := clientBuilder.DiscoveryClient(clientUserAgent)
	if err != nil {
		klog.ErrorS(err, "Failed to create discovery client for cluster", "cluster", cluster.Name)
		return nil, err
	}
	return discoveryClient, nil
}
