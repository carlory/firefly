package phases

/*
Copyright 2017 The Kubernetes Authors.

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

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/carlory/firefly/pkg/adm/cmd/options"
	"github.com/carlory/firefly/pkg/adm/cmd/phases/workflow"
	cmdutil "github.com/carlory/firefly/pkg/adm/cmd/util"
	"github.com/carlory/firefly/pkg/adm/util/apiclient"
)

var (
	loadBalanceExample = cmdutil.Examples(`
		# Run pre-flight checks for kubeadm init using a config file.
		kubeadm init phase loadbalance --config kubeadm-config.yaml
		`)
)

// NewLoadBalancePhase creates a kubeadm workflow phase that implements preflight checks for a new control-plane node.
func NewLoadBalancePhase() workflow.Phase {
	return workflow.Phase{
		Name:    "loadbalance",
		Example: loadBalanceExample,
		Run:     runLoadBalance,
		InheritFlags: []string{
			options.CfgPath,
			options.IgnorePreflightErrors,
			options.DryRun,
		},
	}
}

// runLoadBalance executes preflight checks logic.
func runLoadBalance(c workflow.RunData) error {
	data, ok := c.(InitData)
	if !ok {
		return errors.New("preflight phase invoked with an invalid data struct")
	}

	fmt.Println("[loadbalance] Creating loadbalance for the controlplane")

	client, err := data.Client()
	if err != nil {
		return err
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-apiserver",
			Namespace: data.Cfg().FireflyNamespace,
			Labels: map[string]string{
				"component": "kube-apiserver",
				"tier":      "control-plane",
			},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeNodePort,
			Selector: map[string]string{
				"component": "kube-apiserver",
				"tier":      "control-plane",
			},
			Ports: []v1.ServicePort{
				{
					Port: 6443,
				},
			},
		},
	}

	err = apiclient.CreateOrUpdateService(client, svc)
	if err != nil {
		return err
	}

	// wait for the service to be ready if its type is LoadBalancer
	got, err := client.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	switch got.Spec.Type {
	case v1.ServiceTypeClusterIP:
		data.Cfg().ControlPlaneEndpoint = net.JoinHostPort(got.Spec.ClusterIP, strconv.Itoa(int(got.Spec.Ports[0].Port)))
	case v1.ServiceTypeNodePort:
		address, err := findNodeAddress(client)
		if err != nil {
			return err
		}
		data.Cfg().ControlPlaneEndpoint = net.JoinHostPort(address, strconv.Itoa(int(got.Spec.Ports[0].NodePort)))
	}
	return nil
}

func findNodeAddress(client kubernetes.Interface) (string, error) {
	nodes, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	if len(nodes.Items) == 0 {
		return "", errors.New("no nodes found")
	}

	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Address != "" {
				return addr.Address, nil
			}
		}
	}
	return "", errors.New("no node address found")
}
