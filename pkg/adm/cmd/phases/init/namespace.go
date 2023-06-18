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

package phases

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/carlory/firefly/pkg/adm/cmd/options"

	"github.com/carlory/firefly/pkg/adm/cmd/phases/workflow"

	cmdutil "github.com/carlory/firefly/pkg/adm/cmd/util"
)

var (
	namespaceExample = cmdutil.Examples(`
		# Run pre-flight checks for kubeadm init using a config file.
		kubeadm init phase preflight --config kubeadm-config.yaml
		`)
)

// NewNamespacePhase creates a kubeadm workflow phase that implements preflight checks for a new control-plane node.
func NewNamespacePhase() workflow.Phase {
	return workflow.Phase{
		Name:    "namespace",
		Example: namespaceExample,
		Run:     runNamespace,
		InheritFlags: []string{
			options.CfgPath,
			options.DryRun,
		},
	}
}

// runNamespace executes namespace creation logic.
func runNamespace(c workflow.RunData) error {
	data, ok := c.(InitData)
	if !ok {
		return errors.New("namespace phase invoked with an invalid data struct")
	}

	fmt.Println("[namespace] Running firefly-system namespace creation flow for init")

	client, err := data.Client()
	if err != nil {
		return err
	}

	ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: data.Cfg().FireflyNamespace}}
	_, err = client.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
