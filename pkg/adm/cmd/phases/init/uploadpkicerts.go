/*
Copyright 2019 The Kubernetes Authors.

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

	"github.com/carlory/firefly/pkg/adm/cmd/options"

	"github.com/carlory/firefly/pkg/adm/cmd/phases/workflow"

	"github.com/carlory/firefly/pkg/adm/constants"
	"github.com/carlory/firefly/pkg/adm/util/apiclient"
)

// NewUploadPKICertsPhase returns the uploadPKICertsPhase phase
func NewUploadPKICertsPhase() workflow.Phase {
	return workflow.Phase{
		Name:  "upload-pkicerts",
		Short: fmt.Sprintf("Upload certificates to %s", constants.KubeadmCertsSecret),
		Long:  fmt.Sprintf("Upload control plane certificates to the %s Secret", constants.KubeadmCertsSecret),
		Run:   runUploadPKICerts,
		InheritFlags: []string{
			options.CfgPath,
			options.KubeconfigPath,
			options.UploadCerts,
			options.CertificateKey,
			options.SkipCertificateKeyPrint,
			options.DryRun,
		},
	}
}

func runUploadPKICerts(c workflow.RunData) error {
	data, ok := c.(InitData)
	if !ok {
		return errors.New("upload-pkicerts phase invoked with an invalid data struct")
	}

	client, err := data.Client()
	if err != nil {
		return err
	}
	namespace := data.Cfg().FireflyNamespace

	fmt.Println("[upload-pkicerts] Uploading the pki certs to a Secret.")

	fmt.Println("k8s")

	if err := uploadCerts(client, namespace, data.CertificateDir(), "pkicerts"); err != nil {
		return err
	}

	fmt.Println("etcd")
	if err := uploadCerts(client, namespace, filepath.Join(data.CertificateDir(), "etcd"), "etcd-certs"); err != nil {
		return err
	}
	return nil
}

func getCertificateDataFromDisk(certificateDir string) (map[string][]byte, error) {
	secretData := map[string][]byte{}
	err := filepath.Walk(certificateDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || filepath.Dir(path) != certificateDir {
			return nil
		}

		data, err1 := os.ReadFile(path)
		if err1 != nil {
			return err1
		}
		secretData[info.Name()] = data
		return nil
	})
	return secretData, err
}

func uploadCerts(client kubernetes.Interface, namespace, certificateDir string, secretName string) error {
	secretData, err := getCertificateDataFromDisk(certificateDir)
	if err != nil {
		return err
	}

	return apiclient.CreateOrUpdateSecret(client, &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: secretData,
	})
}
