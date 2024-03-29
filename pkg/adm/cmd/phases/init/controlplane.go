/*
Copyright 2018 The Kubernetes Authors.

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
	"github.com/carlory/firefly/pkg/adm/constants"
	"github.com/carlory/firefly/pkg/adm/phases/controlplane"
)

var (
	controlPlaneExample = cmdutil.Examples(`
		# Generates all static Pod manifest files for control plane components,
		# functionally equivalent to what is generated by kubeadm init.
		kubeadm init phase control-plane all

		# Generates all static Pod manifest files using options read from a configuration file.
		kubeadm init phase control-plane all --config config.yaml
		`)

	controlPlanePhaseProperties = map[string]struct {
		name  string
		short string
	}{
		constants.KubeAPIServer: {
			name:  "apiserver",
			short: getPhaseDescription(constants.KubeAPIServer),
		},
		constants.KubeControllerManager: {
			name:  "controller-manager",
			short: getPhaseDescription(constants.KubeControllerManager),
		},
		constants.KubeScheduler: {
			name:  "scheduler",
			short: getPhaseDescription(constants.KubeScheduler),
		},
	}
)

func getPhaseDescription(component string) string {
	return fmt.Sprintf("Generates the %s static Pod manifest", component)
}

// NewControlPlanePhase creates a kubeadm workflow phase that implements bootstrapping the control plane.
func NewControlPlanePhase() workflow.Phase {
	phase := workflow.Phase{
		Name:  "control-plane",
		Short: "Generate all static Pod manifest files necessary to establish the control plane",
		Long:  cmdutil.MacroCommandLongDescription,
		Phases: []workflow.Phase{
			{
				Name:           "all",
				Short:          "Generate all static Pod manifest files",
				InheritFlags:   getControlPlanePhaseFlags("all"),
				Example:        controlPlaneExample,
				RunAllSiblings: true,
			},
			newControlPlaneSubphase(constants.KubeAPIServer),
			newControlPlaneSubphase(constants.KubeControllerManager),
			newControlPlaneSubphase(constants.KubeScheduler),
		},
		Run: runControlPlanePhase,
	}
	return phase
}

func newControlPlaneSubphase(component string) workflow.Phase {
	phase := workflow.Phase{
		Name:         controlPlanePhaseProperties[component].name,
		Short:        controlPlanePhaseProperties[component].short,
		Run:          runControlPlaneSubphase(component),
		InheritFlags: getControlPlanePhaseFlags(component),
	}
	return phase
}

func getControlPlanePhaseFlags(name string) []string {
	flags := []string{
		options.CfgPath,
		options.CertificatesDir,
		options.KubernetesVersion,
		options.ImageRepository,
		options.Patches,
		options.DryRun,
	}
	if name == "all" || name == constants.KubeAPIServer {
		flags = append(flags,
			options.APIServerAdvertiseAddress,
			options.ControlPlaneEndpoint,
			options.APIServerBindPort,
			options.APIServerExtraArgs,
			options.FeatureGatesString,
			options.NetworkingServiceSubnet,
		)
	}
	if name == "all" || name == constants.KubeControllerManager {
		flags = append(flags,
			options.ControllerManagerExtraArgs,
			options.NetworkingPodSubnet,
		)
	}
	if name == "all" || name == constants.KubeScheduler {
		flags = append(flags,
			options.SchedulerExtraArgs,
		)
	}
	return flags
}

func runControlPlanePhase(c workflow.RunData) error {
	data, ok := c.(InitData)
	if !ok {
		return errors.New("control-plane phase invoked with an invalid data struct")
	}

	fmt.Printf("[control-plane] Using manifest folder %q\n", data.ManifestDir())
	return nil
}

func runControlPlaneSubphase(component string) func(c workflow.RunData) error {
	return func(c workflow.RunData) error {
		data, ok := c.(InitData)
		if !ok {
			return errors.New("control-plane phase invoked with an invalid data struct")
		}
		cfg := data.Cfg()
		namespace := cfg.FireflyNamespace
		client, err := data.Client()
		if err != nil {
			return err
		}
		fmt.Printf("[control-plane] Creating static Pod manifest for %q\n", component)
		return controlplane.CreateStaticPodFiles(client, namespace, data.ManifestDir(), data.PatchesDir(), &cfg.ClusterConfiguration, data.DryRun(), component)
	}
}
