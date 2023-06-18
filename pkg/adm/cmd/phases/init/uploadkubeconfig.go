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
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/carlory/firefly/pkg/adm/cmd/options"

	"github.com/carlory/firefly/pkg/adm/cmd/phases/workflow"

	cmdutil "github.com/carlory/firefly/pkg/adm/cmd/util"
	"github.com/carlory/firefly/pkg/adm/constants"
	"github.com/carlory/firefly/pkg/adm/util/apiclient"
)

var (
	uploadAdminKubeConfigFileExample = cmdutil.Examples(`
		# upload the configuration of your cluster
		kubeadm init phase upload-config --config=myConfig.yaml
		`)

	uploadControllerManagerKubeConfigFileExample = cmdutil.Examples(`
		# Upload the kubelet configuration from the kubeadm Config file to a ConfigMap in the cluster.
		kubeadm init phase upload-config kubelet --config kubeadm.yaml
		`)

	uploadSchedulerKubeConfigFileExample = cmdutil.Examples(`
		# Upload the kubelet configuration from the kubeadm Config file to a ConfigMap in the cluster.
		kubeadm init phase upload-config kubelet --config kubeadm.yaml
		`)
)

// NewUploadConfigPhase returns the phase to uploadConfig
func NewUploadKubeConfigPhase() workflow.Phase {
	return workflow.Phase{
		Name:    "upload-kubeconfig",
		Aliases: []string{"uploadconfig"},
		Short:   "Upload the kubeadm and kubelet configuration to a ConfigMap",
		Long:    cmdutil.MacroCommandLongDescription,
		Phases: []workflow.Phase{
			{
				Name:           "all",
				Short:          "Upload all configuration to a config map",
				RunAllSiblings: true,
				InheritFlags:   getUploadKubeConfigPhaseFlags(),
			},
			{
				Name:         "admin",
				Short:        "Upload the admin kubeconfig to a ConfigMap",
				Example:      uploadAdminKubeConfigFileExample,
				Run:          runUploadAdminKubeConfig,
				InheritFlags: getUploadKubeConfigPhaseFlags(),
			},
			{
				Name:         "controller-manager",
				Short:        "Upload the controller-manager kubeconfig to a ConfigMap",
				Example:      uploadControllerManagerKubeConfigFileExample,
				Run:          runUploadControllerManagerKubeConfig,
				InheritFlags: getUploadKubeConfigPhaseFlags(),
			},
			{
				Name:         "scheduler",
				Short:        "Upload the scheduler kubeconfig to a ConfigMap",
				Example:      uploadSchedulerKubeConfigFileExample,
				Run:          runUploadSchedulerKubeConfig,
				InheritFlags: getUploadKubeConfigPhaseFlags(),
			},
		},
	}
}

func getUploadKubeConfigPhaseFlags() []string {
	return []string{
		options.CfgPath,
		options.NodeCRISocket,
		options.KubeconfigPath,
		options.DryRun,
	}
}

// runUploadAdminKubeConfig uploads the admin configuration to a ConfigMap
func runUploadAdminKubeConfig(c workflow.RunData) error {
	data, ok := c.(InitData)
	if !ok {
		return errors.New("upload-kubeconfig phase invoked with an invalid data struct")
	}

	client, err := data.Client()
	if err != nil {
		return err
	}
	namespace := data.Cfg().FireflyNamespace

	fmt.Println("[upload-kubeconfig] Uploading the admin kubeconfig to a ConfigMap")
	return uploadKubeConfigFile(client, namespace, data.KubeConfigDir(), constants.AdminKubeConfigFileName)
}

// runUploadControllerManagerKubeConfig uploads the controller-manager kubeconfig to a ConfigMap
func runUploadControllerManagerKubeConfig(c workflow.RunData) error {
	data, ok := c.(InitData)
	if !ok {
		return errors.New("upload-kubeconfig phase invoked with an invalid data struct")
	}
	client, err := data.Client()
	if err != nil {
		return err
	}

	fmt.Println("[upload-kubeconfig] Uploading the controller-manager kubeconfig to a ConfigMap")
	namespace := data.Cfg().FireflyNamespace
	return uploadKubeConfigFile(client, namespace, data.KubeConfigDir(), constants.ControllerManagerKubeConfigFileName)
}

// runUploadSchedulerKubeConfig uploads the scheduler kubeconfig to a ConfigMap
func runUploadSchedulerKubeConfig(c workflow.RunData) error {
	data, ok := c.(InitData)
	if !ok {
		return errors.New("upload-kubeconfig phase invoked with an invalid data struct")
	}
	client, err := data.Client()
	if err != nil {
		return err
	}
	namespace := data.Cfg().FireflyNamespace

	fmt.Println("[upload-kubeconfig] Uploading the scheduler kubeconfig to a ConfigMap")
	return uploadKubeConfigFile(client, namespace, data.KubeConfigDir(), constants.SchedulerKubeConfigFileName)
}

// uploadKubeConfigFile uploads the given kubeconfig to a ConfigMap
func uploadKubeConfigFile(client kubernetes.Interface, namespace, readDir string, kubeConfigFileName string) error {
	klog.V(1).Infof("reading kubeconfig file for %s", kubeConfigFileName)
	kubeConfigFilePath := filepath.Join(readDir, kubeConfigFileName)
	kubeConfigBytes, err := os.ReadFile(kubeConfigFilePath)
	if err != nil {
		return errors.Wrapf(err, "couldn't read kubeconfig file %s", kubeConfigFilePath)
	}
	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubeConfigFileName,
			Namespace: namespace,
		},
		Data: map[string]string{
			kubeConfigFileName: string(kubeConfigBytes),
		},
	}

	return apiclient.CreateOrUpdateConfigMap(client, configMap)
}
