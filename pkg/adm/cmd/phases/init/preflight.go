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
	"fmt"

	"github.com/pkg/errors"

	"github.com/carlory/firefly/pkg/adm/cmd/options"

	"github.com/carlory/firefly/pkg/adm/cmd/phases/workflow"

	cmdutil "github.com/carlory/firefly/pkg/adm/cmd/util"
)

var (
	preflightExample = cmdutil.Examples(`
		# Run pre-flight checks for kubeadm init using a config file.
		kubeadm init phase preflight --config kubeadm-config.yaml
		`)
)

// NewPreflightPhase creates a kubeadm workflow phase that implements preflight checks for a new control-plane node.
func NewPreflightPhase() workflow.Phase {
	return workflow.Phase{
		Name:    "preflight",
		Short:   "Run pre-flight checks",
		Long:    "Run pre-flight checks for kubeadm init.",
		Example: preflightExample,
		Run:     runPreflight,
		InheritFlags: []string{
			options.CfgPath,
			options.NodeCRISocket,
			options.IgnorePreflightErrors,
			options.DryRun,
		},
	}
}

// runPreflight executes preflight checks logic.
func runPreflight(c workflow.RunData) error {
	_, ok := c.(InitData)
	if !ok {
		return errors.New("preflight phase invoked with an invalid data struct")
	}

	fmt.Println("[preflight] Running pre-flight checks")

	return nil
}
