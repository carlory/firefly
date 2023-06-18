/*
Copyright 2016 The Kubernetes Authors.

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

package controlplane

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	kubeadmapi "github.com/carlory/firefly/pkg/adm/apis/kubeadm"
	"github.com/carlory/firefly/pkg/adm/images"

	"github.com/carlory/firefly/pkg/adm/constants"
	certphase "github.com/carlory/firefly/pkg/adm/phases/certs"
	fireflyadmutil "github.com/carlory/firefly/pkg/adm/util"
	staticpodutil "github.com/carlory/firefly/pkg/adm/util/staticpod"
)

const (
	certificatesDir = "/etc/kubernetes/pki"
	kubernetesDir   = "/etc/kubernetes"
)

// GetStaticPodSpecs returns all staticPodSpecs actualized to the context of the current configuration
// NB. this method holds the information about how kubeadm creates static pod manifests.
func GetStaticPodSpecs(cfg *kubeadmapi.ClusterConfiguration) map[string]v1.Pod {
	// Get the required hostpath mounts
	// mounts := getHostPathVolumesForTheControlPlane(cfg)

	// Prepare static pod specs
	staticPodSpecs := map[string]v1.Pod{
		constants.KubeAPIServer: staticpodutil.ComponentPod(v1.Container{
			Name:            constants.KubeAPIServer,
			Image:           images.GetKubernetesImage(constants.KubeAPIServer, cfg),
			ImagePullPolicy: v1.PullIfNotPresent,
			Command:         getAPIServerCommand(cfg),
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "k8s-certs",
					MountPath: "/etc/kubernetes/pki",
					ReadOnly:  true,
				},
				{
					Name:      "etcd-certs",
					MountPath: "/etc/kubernetes/pki/etcd",
					ReadOnly:  true,
				},
			},
			// LivenessProbe:  staticpodutil.LivenessProbe(staticpodutil.GetAPIServerProbeAddress(endpoint), "/livez", int(endpoint.BindPort), v1.URISchemeHTTPS),
			// ReadinessProbe: staticpodutil.ReadinessProbe(staticpodutil.GetAPIServerProbeAddress(endpoint), "/readyz", int(endpoint.BindPort), v1.URISchemeHTTPS),
			// StartupProbe:   staticpodutil.StartupProbe(staticpodutil.GetAPIServerProbeAddress(endpoint), "/livez", int(endpoint.BindPort), v1.URISchemeHTTPS, cfg.APIServer.TimeoutForControlPlane),
			Resources: staticpodutil.ComponentResources("250m"),
			// Env:       fireflyadmutil.GetProxyEnvVars(),
		}, map[string]v1.Volume{
			"k8s-certs": {
				Name: "k8s-certs",
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: "pkicerts",
					},
				},
			},
			"etcd-certs": {
				Name: "etcd-certs",
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: "etcd-certs",
					},
				},
			},
		},
			map[string]string{}),
		constants.KubeControllerManager: staticpodutil.ComponentPod(v1.Container{
			Name:            constants.KubeControllerManager,
			Image:           images.GetKubernetesImage(constants.KubeControllerManager, cfg),
			ImagePullPolicy: v1.PullIfNotPresent,
			Command:         getControllerManagerCommand(cfg),
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "k8s-certs",
					MountPath: "/etc/kubernetes/pki",
					ReadOnly:  true,
				},
				{
					Name:      "kubeconfig",
					MountPath: "/etc/kubernetes",
					ReadOnly:  true,
				},
			},
			// VolumeMounts:    staticpodutil.VolumeMountMapToSlice(mounts.GetVolumeMounts(constants.KubeControllerManager)),
			// LivenessProbe:   staticpodutil.LivenessProbe(staticpodutil.GetControllerManagerProbeAddress(cfg), "/healthz", constants.KubeControllerManagerPort, v1.URISchemeHTTPS),
			// StartupProbe:    staticpodutil.StartupProbe(staticpodutil.GetControllerManagerProbeAddress(cfg), "/healthz", constants.KubeControllerManagerPort, v1.URISchemeHTTPS, cfg.APIServer.TimeoutForControlPlane),
			Resources: staticpodutil.ComponentResources("200m"),
			// Env:       fireflyadmutil.GetProxyEnvVars(),
		}, map[string]v1.Volume{
			"k8s-certs": {
				Name: "k8s-certs",
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: "pkicerts",
					},
				},
			},
			"kubeconfig": {
				Name: "kubeconfig",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: constants.ControllerManagerKubeConfigFileName,
						},
					},
				},
			},
		}, nil),
		constants.KubeScheduler: staticpodutil.ComponentPod(v1.Container{
			Name:            constants.KubeScheduler,
			Image:           images.GetKubernetesImage(constants.KubeScheduler, cfg),
			ImagePullPolicy: v1.PullIfNotPresent,
			Command:         getSchedulerCommand(cfg),
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "k8s-certs",
					MountPath: "/etc/kubernetes/pki",
					ReadOnly:  true,
				},
				{
					Name:      "kubeconfig",
					MountPath: "/etc/kubernetes",
					ReadOnly:  true,
				},
			},
			// VolumeMounts:    staticpodutil.VolumeMountMapToSlice(mounts.GetVolumeMounts(constants.KubeScheduler)),
			// LivenessProbe:   staticpodutil.LivenessProbe(staticpodutil.GetSchedulerProbeAddress(cfg), "/healthz", constants.KubeSchedulerPort, v1.URISchemeHTTPS),
			// StartupProbe:    staticpodutil.StartupProbe(staticpodutil.GetSchedulerProbeAddress(cfg), "/healthz", constants.KubeSchedulerPort, v1.URISchemeHTTPS, cfg.APIServer.TimeoutForControlPlane),
			Resources: staticpodutil.ComponentResources("100m"),
			// Env:       fireflyadmutil.GetProxyEnvVars(),
		}, map[string]v1.Volume{
			"k8s-certs": {
				Name: "k8s-certs",
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: "pkicerts",
					},
				},
			},
			"kubeconfig": {
				Name: "kubeconfig",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: constants.SchedulerKubeConfigFileName,
						},
					},
				},
			},
		}, nil),
	}
	return staticPodSpecs
}

// CreateStaticPodFiles creates all the requested static pod files.
func CreateStaticPodFiles(client kubernetes.Interface, namespace, manifestDir, patchesDir string, cfg *kubeadmapi.ClusterConfiguration, isDryRun bool, componentNames ...string) error {
	// gets the StaticPodSpecs, actualized for the current ClusterConfiguration
	klog.V(1).Infoln("[control-plane] getting StaticPodSpecs")
	specs := GetStaticPodSpecs(cfg)

	// creates required static pod specs
	for _, componentName := range componentNames {
		// retrieves the StaticPodSpec for given component
		spec, exists := specs[componentName]
		if !exists {
			return errors.Errorf("couldn't retrieve StaticPodSpec for %q", componentName)
		}

		// print all volumes that are mounted
		for _, v := range spec.Spec.Volumes {
			klog.V(2).Infof("[control-plane] adding volume %q for component %q", v.Name, componentName)
		}

		// if patchesDir is defined, patch the static Pod manifest
		if patchesDir != "" {
			patchedSpec, err := staticpodutil.PatchStaticPod(&spec, patchesDir, os.Stdout)
			if err != nil {
				return errors.Wrapf(err, "failed to patch static Pod manifest file for %q", componentName)
			}
			spec = *patchedSpec
		}

		wait.PollImmediate(1*time.Second, 10*time.Second, func() (bool, error) {
			_, err := client.CoreV1().ServiceAccounts(namespace).Get(context.Background(), "default", metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			return true, nil
		})

		_, err := client.CoreV1().Pods(namespace).Create(context.TODO(), &spec, metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				client.CoreV1().Pods(namespace).Delete(context.TODO(), spec.Name, metav1.DeleteOptions{})
				time.Sleep(15 * time.Second)
				_, err = client.CoreV1().Pods(namespace).Create(context.TODO(), &spec, metav1.CreateOptions{})
			}
			return errors.Wrapf(err, "failed to create static Pod manifest file for %q", componentName)
		}
		klog.V(1).Infof("[control-plane] wrote static Pod manifest for component %q to %q\n", componentName, constants.GetStaticPodFilepath(componentName, manifestDir))
	}

	return nil
}

// getAPIServerCommand builds the right API server command from the given config object and version
func getAPIServerCommand(cfg *kubeadmapi.ClusterConfiguration) []string {
	defaultArguments := map[string]string{
		// "advertise-address":                localAPIEndpoint.AdvertiseAddress,
		"enable-admission-plugins":         "NodeRestriction",
		"service-cluster-ip-range":         cfg.Networking.ServiceSubnet,
		"service-account-key-file":         filepath.Join(certificatesDir, constants.ServiceAccountPublicKeyName),
		"service-account-signing-key-file": filepath.Join(certificatesDir, constants.ServiceAccountPrivateKeyName),
		"service-account-issuer":           fmt.Sprintf("https://kubernetes.default.svc.%s", cfg.Networking.DNSDomain),
		"client-ca-file":                   filepath.Join(certificatesDir, constants.CACertName),
		"tls-cert-file":                    filepath.Join(certificatesDir, constants.APIServerCertName),
		"tls-private-key-file":             filepath.Join(certificatesDir, constants.APIServerKeyName),
		"kubelet-client-certificate":       filepath.Join(certificatesDir, constants.APIServerKubeletClientCertName),
		"kubelet-client-key":               filepath.Join(certificatesDir, constants.APIServerKubeletClientKeyName),
		"enable-bootstrap-token-auth":      "true",
		"secure-port":                      "6443",
		"allow-privileged":                 "true",
		"kubelet-preferred-address-types":  "InternalIP,ExternalIP,Hostname",
		// add options to configure the front proxy.  Without the generated client cert, this will never be useable
		// so add it unconditionally with recommended values
		"requestheader-username-headers":     "X-Remote-User",
		"requestheader-group-headers":        "X-Remote-Group",
		"requestheader-extra-headers-prefix": "X-Remote-Extra-",
		"requestheader-client-ca-file":       filepath.Join(certificatesDir, constants.FrontProxyCACertName),
		"requestheader-allowed-names":        "front-proxy-client",
		"proxy-client-cert-file":             filepath.Join(certificatesDir, constants.FrontProxyClientCertName),
		"proxy-client-key-file":              filepath.Join(certificatesDir, constants.FrontProxyClientKeyName),
	}

	command := []string{"kube-apiserver"}

	// If the user set endpoints for an external etcd cluster
	if cfg.Etcd.External != nil {
		defaultArguments["etcd-servers"] = strings.Join(cfg.Etcd.External.Endpoints, ",")

		// Use any user supplied etcd certificates
		if cfg.Etcd.External.CAFile != "" {
			defaultArguments["etcd-cafile"] = cfg.Etcd.External.CAFile
		}
		if cfg.Etcd.External.CertFile != "" && cfg.Etcd.External.KeyFile != "" {
			defaultArguments["etcd-certfile"] = cfg.Etcd.External.CertFile
			defaultArguments["etcd-keyfile"] = cfg.Etcd.External.KeyFile
		}
	} else {
		// Default to etcd static pod on localhost
		// localhost IP family should be the same that the AdvertiseAddress
		etcdLocalhostAddress := "etcd"
		defaultArguments["etcd-servers"] = fmt.Sprintf("https://%s", net.JoinHostPort(etcdLocalhostAddress, strconv.Itoa(constants.EtcdListenClientPort)))
		defaultArguments["etcd-cafile"] = filepath.Join(certificatesDir, constants.EtcdCACertName)
		defaultArguments["etcd-certfile"] = filepath.Join(certificatesDir, constants.APIServerEtcdClientCertName)
		defaultArguments["etcd-keyfile"] = filepath.Join(certificatesDir, constants.APIServerEtcdClientKeyName)

		// Apply user configurations for local etcd
		if cfg.Etcd.Local != nil {
			if value, ok := cfg.Etcd.Local.ExtraArgs["advertise-client-urls"]; ok {
				defaultArguments["etcd-servers"] = value
			}
		}
	}

	if cfg.APIServer.ExtraArgs == nil {
		cfg.APIServer.ExtraArgs = map[string]string{}
	}
	cfg.APIServer.ExtraArgs["authorization-mode"] = getAuthzModes(cfg.APIServer.ExtraArgs["authorization-mode"])
	command = append(command, fireflyadmutil.BuildArgumentListFromMap(defaultArguments, cfg.APIServer.ExtraArgs)...)

	return command
}

// getAuthzModes gets the authorization-related parameters to the api server
// Node,RBAC is the default mode if nothing is passed to kubeadm. User provided modes override
// the default.
func getAuthzModes(authzModeExtraArgs string) string {
	defaultMode := []string{
		constants.ModeNode,
		constants.ModeRBAC,
	}

	if len(authzModeExtraArgs) > 0 {
		mode := []string{}
		for _, requested := range strings.Split(authzModeExtraArgs, ",") {
			if isValidAuthzMode(requested) {
				mode = append(mode, requested)
			} else {
				klog.Warningf("ignoring unknown kube-apiserver authorization-mode %q", requested)
			}
		}

		// only return the user provided mode if at least one was valid
		if len(mode) > 0 {
			if !compareAuthzModes(defaultMode, mode) {
				klog.Warningf("the default kube-apiserver authorization-mode is %q; using %q",
					strings.Join(defaultMode, ","),
					strings.Join(mode, ","),
				)
			}
			return strings.Join(mode, ",")
		}
	}
	return strings.Join(defaultMode, ",")
}

// compareAuthzModes compares two given authz modes and returns false if they do not match
func compareAuthzModes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, m := range a {
		if m != b[i] {
			return false
		}
	}
	return true
}

func isValidAuthzMode(authzMode string) bool {
	allModes := []string{
		constants.ModeNode,
		constants.ModeRBAC,
		constants.ModeWebhook,
		constants.ModeABAC,
		constants.ModeAlwaysAllow,
		constants.ModeAlwaysDeny,
	}

	for _, mode := range allModes {
		if authzMode == mode {
			return true
		}
	}
	return false
}

// getControllerManagerCommand builds the right controller manager command from the given config object and version
func getControllerManagerCommand(cfg *kubeadmapi.ClusterConfiguration) []string {

	kubeconfigFile := filepath.Join(kubernetesDir, constants.ControllerManagerKubeConfigFileName)
	caFile := filepath.Join(certificatesDir, constants.CACertName)

	defaultArguments := map[string]string{
		"bind-address":                     "127.0.0.1",
		"leader-elect":                     "true",
		"kubeconfig":                       kubeconfigFile,
		"authentication-kubeconfig":        kubeconfigFile,
		"authorization-kubeconfig":         kubeconfigFile,
		"client-ca-file":                   caFile,
		"requestheader-client-ca-file":     filepath.Join(certificatesDir, constants.FrontProxyCACertName),
		"root-ca-file":                     caFile,
		"service-account-private-key-file": filepath.Join(certificatesDir, constants.ServiceAccountPrivateKeyName),
		"cluster-signing-cert-file":        caFile,
		"cluster-signing-key-file":         filepath.Join(certificatesDir, constants.CAKeyName),
		"use-service-account-credentials":  "true",
		"controllers":                      "*,bootstrapsigner,tokencleaner",
	}

	// If using external CA, pass empty string to controller manager instead of ca.key/ca.crt path,
	// so that the csrsigning controller fails to start
	if res, _ := certphase.UsingExternalCA(cfg); res {
		defaultArguments["cluster-signing-key-file"] = ""
		defaultArguments["cluster-signing-cert-file"] = ""
	}

	// Let the controller-manager allocate Node CIDRs for the Pod network.
	// Each node will get a subspace of the address CIDR provided with --pod-network-cidr.
	if cfg.Networking.PodSubnet != "" {
		defaultArguments["allocate-node-cidrs"] = "true"
		defaultArguments["cluster-cidr"] = cfg.Networking.PodSubnet
		if cfg.Networking.ServiceSubnet != "" {
			defaultArguments["service-cluster-ip-range"] = cfg.Networking.ServiceSubnet
		}
	}

	// Set cluster name
	if cfg.ClusterName != "" {
		defaultArguments["cluster-name"] = cfg.ClusterName
	}

	command := []string{"kube-controller-manager"}
	command = append(command, fireflyadmutil.BuildArgumentListFromMap(defaultArguments, cfg.ControllerManager.ExtraArgs)...)

	return command
}

// getSchedulerCommand builds the right scheduler command from the given config object and version
func getSchedulerCommand(cfg *kubeadmapi.ClusterConfiguration) []string {
	kubeconfigFile := filepath.Join(kubernetesDir, constants.SchedulerKubeConfigFileName)
	defaultArguments := map[string]string{
		"bind-address":              "127.0.0.1",
		"leader-elect":              "true",
		"kubeconfig":                kubeconfigFile,
		"authentication-kubeconfig": kubeconfigFile,
		"authorization-kubeconfig":  kubeconfigFile,
	}

	command := []string{"kube-scheduler"}
	command = append(command, fireflyadmutil.BuildArgumentListFromMap(defaultArguments, cfg.Scheduler.ExtraArgs)...)
	return command
}
