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

	"k8s.io/klog/v2"

	"github.com/carlory/firefly/pkg/adm/cmd/options"

	"github.com/carlory/firefly/pkg/adm/cmd/phases/workflow"

	cmdutil "github.com/carlory/firefly/pkg/adm/cmd/util"
	etcdphase "github.com/carlory/firefly/pkg/adm/phases/etcd"
)

var (
	etcdhostedExample = cmdutil.Examples(`
		# Generates manifest files for etcd and apply them into the Kubernetes,
		# functionally equivalent to what is applied by fireflyadm init.
		fireflyadm init phase etcd hosted

		# Generates manifest files for etcd using options
		# read from a configuration file.
		fireflyadm init phase etcd hosted --config config.yaml
		`)
)

// NewEtcdPhase creates a kubeadm workflow phase that implements handling of etcd.
func NewEtcdPhase() workflow.Phase {
	phase := workflow.Phase{
		Name:  "etcd",
		Short: "Generate manifest files for local etcd",
		Long:  cmdutil.MacroCommandLongDescription,
		Phases: []workflow.Phase{
			newEtcdHostedSubPhase(),
		},
	}
	return phase
}

func newEtcdHostedSubPhase() workflow.Phase {
	phase := workflow.Phase{
		Name:         "hosted",
		Short:        "Generate manifest files for a local, single-node hosted etcd instance",
		Example:      etcdhostedExample,
		Run:          runEtcdPhaseHosted,
		InheritFlags: getEtcdPhaseFlags(),
	}
	return phase
}

func getEtcdPhaseFlags() []string {
	flags := []string{
		options.CertificatesDir,
		options.CfgPath,
		options.ImageRepository,
		options.Patches,
		options.DryRun,
	}
	return flags
}

func runEtcdPhaseHosted(c workflow.RunData) error {
	data, ok := c.(InitData)
	if !ok {
		return errors.New("etcd phase invoked with an invalid data struct")
	}
	cfg := data.Cfg()
	client, err := data.Client()
	if err != nil {
		return errors.Wrap(err, "error creating client")
	}
	namespace := data.Cfg().FireflyNamespace

	// Add etcd static pod spec only if external etcd is not configured
	if cfg.Etcd.External == nil {
		fmt.Printf("[etcd] Creating static Pod manifest for local etcd in %q\n", data.ManifestDir())
		if err := etcdphase.CreateHostedEtcd(client, namespace, data.ManifestDir(), data.PatchesDir(), &cfg.ClusterConfiguration, data.DryRun()); err != nil {
			return errors.Wrap(err, "error creating local etcd static pod manifest file")
		}
		// if err := etcdphase.EnsureEtcd(nil, data.DryRun()); err != nil {
		// 	return errors.Wrap(err, "error creating local etcd static pod manifest file")
		// }
	} else {
		klog.V(1).Infoln("[etcd] External etcd mode. Skipping the generation and apply of manifests for hosted etcd")
	}
	return nil
}
