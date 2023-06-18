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

package etcd

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pkg/errors"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	utilsnet "k8s.io/utils/net"

	kubeadmapi "github.com/carlory/firefly/pkg/adm/apis/kubeadm"
	"github.com/carlory/firefly/pkg/adm/images"

	"github.com/carlory/firefly/pkg/adm/constants"
	fireflyadmutil "github.com/carlory/firefly/pkg/adm/util"
	"github.com/carlory/firefly/pkg/adm/util/apiclient"
	etcdutil "github.com/carlory/firefly/pkg/adm/util/etcd"
	staticpodutil "github.com/carlory/firefly/pkg/adm/util/staticpod"
)

const (
	etcdVolumeName           = "etcd-data"
	certsVolumeName          = "etcd-certs"
	etcdHealthyCheckInterval = 5 * time.Second
	etcdHealthyCheckRetries  = 8
	certificatesDir          = "/etc/kubernetes/pki"
)

// CreateHostedEtcd will create a hosted etcd instance in the cluster.
// This function is used by init - when the etcd cluster is empty - or by kubeadm
// upgrade - when the etcd cluster is already up and running (and the --initial-cluster flag have no impact)
func CreateHostedEtcd(client clientset.Interface, namespace, manifestDir string, patchesDir string, cfg *kubeadmapi.ClusterConfiguration, isDryRun bool) error {
	if cfg.Etcd.External != nil {
		return errors.New("etcd static pod manifest cannot be generated for cluster using external etcd")
	}

	if err := createVolume(client, namespace); err != nil {
		return err
	}

	if err := createWorkloads(client, namespace, manifestDir, patchesDir, cfg, []etcdutil.Member{}, isDryRun); err != nil {
		return err
	}

	if err := createService(client, namespace); err != nil {
		return err
	}

	klog.V(1).Infof("[etcd] wrote Static Pod manifest for a local etcd member to %q\n", constants.GetStaticPodFilepath(constants.Etcd, manifestDir))
	return nil
}

// CheckLocalEtcdClusterStatus verifies health state of local/stacked etcd cluster before installing a new etcd member
func CheckLocalEtcdClusterStatus(client clientset.Interface, certificatesDir string) error {
	klog.V(1).Info("[etcd] Checking etcd cluster health")

	// creates an etcd client that connects to all the local/stacked etcd members
	klog.V(1).Info("creating etcd client that connects to etcd pods")
	etcdClient, err := etcdutil.NewFromCluster(client, certificatesDir)
	if err != nil {
		return err
	}

	// Checking health state
	err = etcdClient.CheckClusterHealth()
	if err != nil {
		return errors.Wrap(err, "etcd cluster is not healthy")
	}

	return nil
}

// GetEtcdPodSpec returns the etcd static Pod actualized to the context of the current configuration
// NB. GetEtcdPodSpec methods holds the information about how kubeadm creates etcd static pod manifests.
func GetEtcdPodSpec(cfg *kubeadmapi.ClusterConfiguration, endpoint *kubeadmapi.APIEndpoint, nodeName string, initialCluster []etcdutil.Member) v1.Pod {
	etcdMounts := map[string]v1.Volume{
		// etcdVolumeName:  staticpodutil.NewClaimVolume(etcdVolumeName, etcdVolumeName),
		etcdVolumeName:  staticpodutil.NewEmptyDirVolume(etcdVolumeName),
		certsVolumeName: staticpodutil.NewSecretVolume(certsVolumeName, certsVolumeName),
	}
	// probeHostname returns the correct localhost IP address family based on the endpoint AdvertiseAddress
	probeHostname, probePort, probeScheme := staticpodutil.GetEtcdProbeEndpoint(&cfg.Etcd, utilsnet.IsIPv6String(endpoint.AdvertiseAddress))
	return staticpodutil.ComponentPod(
		v1.Container{
			Name:            constants.Etcd,
			Command:         getEtcdCommand(cfg, initialCluster),
			Image:           images.GetEtcdImage(cfg),
			ImagePullPolicy: v1.PullIfNotPresent,
			// Mount the etcd datadir path read-write so etcd can store data in a more persistent manner
			VolumeMounts: []v1.VolumeMount{
				staticpodutil.NewVolumeMount(etcdVolumeName, cfg.Etcd.Local.DataDir, false),
				staticpodutil.NewVolumeMount(certsVolumeName, certificatesDir+"/etcd", false),
			},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("100m"),
					v1.ResourceMemory: resource.MustParse("100Mi"),
				},
			},
			LivenessProbe: staticpodutil.LivenessProbe(probeHostname, "/health?exclude=NOSPACE&serializable=true", probePort, probeScheme),
			StartupProbe:  staticpodutil.StartupProbe(probeHostname, "/health?serializable=false", probePort, probeScheme, cfg.APIServer.TimeoutForControlPlane),
		},
		etcdMounts,
		// etcd will listen on the advertise address of the API server, in a different port (2379)
		map[string]string{constants.EtcdAdvertiseClientUrlsAnnotationKey: etcdutil.GetClientURL(endpoint)},
	)
}

// getEtcdCommand builds the right etcd command from the given config object
func getEtcdCommand(cfg *kubeadmapi.ClusterConfiguration, initialCluster []etcdutil.Member) []string {
	defaultArguments := map[string]string{
		"name": "etcd0",
		// TODO: start using --initial-corrupt-check once the graduated flag is available:
		// https://github.com/kubernetes/kubeadm/issues/2676
		"experimental-initial-corrupt-check": "true",
		"listen-client-urls":                 etcdutil.GetClientURLByIP("0.0.0.0"),
		"advertise-client-urls":              etcdutil.GetClientURLByDomain("etcd"),
		"listen-peer-urls":                   etcdutil.GetPeerURLByIP("0.0.0.0"),
		"initial-advertise-peer-urls":        etcdutil.GetPeerURLByDomain("etcd-0.etcd"),
		"data-dir":                           cfg.Etcd.Local.DataDir,
		"cert-file":                          filepath.Join(certificatesDir, constants.EtcdServerCertName),
		"key-file":                           filepath.Join(certificatesDir, constants.EtcdServerKeyName),
		"trusted-ca-file":                    filepath.Join(certificatesDir, constants.EtcdCACertName),
		"client-cert-auth":                   "true",
		"peer-cert-file":                     filepath.Join(certificatesDir, constants.EtcdPeerCertName),
		"peer-key-file":                      filepath.Join(certificatesDir, constants.EtcdPeerKeyName),
		"peer-trusted-ca-file":               filepath.Join(certificatesDir, constants.EtcdCACertName),
		"peer-client-cert-auth":              "true",
		"snapshot-count":                     "10000",
		"listen-metrics-urls":                fmt.Sprintf("http://%s", net.JoinHostPort("0.0.0.0", strconv.Itoa(constants.EtcdMetricsPort))),
		"experimental-watch-progress-notify-interval": "5s",
	}

	defaultArguments["initial-cluster"] = fmt.Sprintf("%s=%s", "etcd0", etcdutil.GetPeerURLByDomain("etcd-0.etcd"))
	defaultArguments["initial-cluster-state"] = "new"
	command := []string{"etcd"}
	command = append(command, fireflyadmutil.BuildArgumentListFromMap(defaultArguments, cfg.Etcd.Local.ExtraArgs)...)
	return command
}

func createVolume(client clientset.Interface, namespace string) error {
	// construct claim object
	claim := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      etcdVolumeName,
			Namespace: namespace,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
	return apiclient.CreateOrUpdatePersistentVolumeClaim(client, claim)
}

// GetPodTemplateSpec returns the etcd's pod template spec actualized to the context of the current configuration.
func GetEtcdPodTemplateSpec(cfg *kubeadmapi.ClusterConfiguration, initialCluster []etcdutil.Member) v1.PodTemplateSpec {
	etcdMounts := map[string]v1.Volume{
		// etcdVolumeName:  staticpodutil.NewClaimVolume(etcdVolumeName, etcdVolumeName),
		etcdVolumeName:  staticpodutil.NewEmptyDirVolume(etcdVolumeName),
		certsVolumeName: staticpodutil.NewSecretVolume(certsVolumeName, certsVolumeName),
	}

	probeHostname, probePort, probeScheme := staticpodutil.GetEtcdProbeEndpoint(&cfg.Etcd, false)
	return staticpodutil.ComponentPodTemplateSpec(
		v1.Container{
			Name:            constants.Etcd,
			Command:         getEtcdCommand(cfg, initialCluster),
			Image:           images.GetEtcdImage(cfg),
			ImagePullPolicy: v1.PullIfNotPresent,
			// Mount the etcd datadir path read-write so etcd can store data in a more persistent manner
			VolumeMounts: []v1.VolumeMount{
				staticpodutil.NewVolumeMount(etcdVolumeName, cfg.Etcd.Local.DataDir, false),
				staticpodutil.NewVolumeMount(certsVolumeName, certificatesDir+"/etcd", false),
			},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("100m"),
					v1.ResourceMemory: resource.MustParse("100Mi"),
				},
			},
			LivenessProbe: staticpodutil.LivenessProbe(probeHostname, "/health?exclude=NOSPACE&serializable=true", probePort, probeScheme),
			StartupProbe:  staticpodutil.StartupProbe(probeHostname, "/health?serializable=false", probePort, probeScheme, cfg.APIServer.TimeoutForControlPlane),
		},
		etcdMounts,
		// etcd will listen on the advertise address of the API server, in a different port (2379)
		map[string]string{},
	)
}

func createWorkloads(client clientset.Interface, namespace, manifestDir string, patchesDir string, cfg *kubeadmapi.ClusterConfiguration, initialCluster []etcdutil.Member, isDryRun bool) error {
	workloads := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etcd",
			Namespace: namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"component": "etcd", "tier": constants.ControlPlaneTier},
			},
			ServiceName: "etcd",
			Template:    GetEtcdPodTemplateSpec(cfg, initialCluster),
		},
	}

	// // if patchesDir is defined, patch the static Pod manifest
	// if patchesDir != "" {
	// 	patchedSpec, err := staticpodutil.PatchStaticPod(&spec, patchesDir, os.Stdout)
	// 	if err != nil {
	// 		return errors.Wrapf(err, "failed to patch static Pod manifest file for %q", constants.Etcd)
	// 	}
	// 	spec = *patchedSpec
	// }

	return apiclient.CreateOrUpdateStatefulSet(client, workloads)
}

func createService(client clientset.Interface, namespace string) error {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etcd",
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Selector:  map[string]string{"component": "etcd", "tier": constants.ControlPlaneTier},
			ClusterIP: "None",
			Type:      v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{
				{
					Name:     "client",
					Protocol: v1.ProtocolTCP,
					Port:     2379,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 2379,
					},
				},
				{
					Name:     "server",
					Protocol: v1.ProtocolTCP,
					Port:     2380,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 2380,
					},
				},
			},
		},
	}
	return apiclient.CreateOrUpdateService(client, svc)
}
