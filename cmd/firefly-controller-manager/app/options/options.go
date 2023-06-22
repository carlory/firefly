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

package options

import (
	"fmt"
	"net"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	apiserveroptions "k8s.io/apiserver/pkg/server/options"
	clientset "k8s.io/client-go/kubernetes"
	clientgokubescheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/metrics"
	"k8s.io/controller-manager/config"
	cmoptions "k8s.io/controller-manager/options"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"

	fireflycontrollerconfig "github.com/carlory/firefly/cmd/firefly-controller-manager/app/config"
	fireflyctrlmgrconfig "github.com/carlory/firefly/pkg/controller/apis/config"
)

const (
	// FireflyControllerManagerUserAgent is the userAgent name when starting firefly-controller managers.
	FireflyControllerManagerUserAgent = "firefly-controller-manager"
)

// FireflyControllerManagerOptions is the main context object for the firefly-controller-manager.
type ControllerManagerOptions struct {
	Generic *cmoptions.GenericControllerManagerConfigurationOptions

	SecureServing  *apiserveroptions.SecureServingOptionsWithLoopback
	Authentication *apiserveroptions.DelegatingAuthenticationOptions
	Authorization  *apiserveroptions.DelegatingAuthorizationOptions
	Metrics        *metrics.Options
	Logs           *logs.Options

	Master                 string
	Kubeconfig             string
	ClusterpediaKubeconfig string
}

// NewControllerManagerOptions creates a new ControllerManagerOptions with a default config.
func NewControllerManagerOptions() (*ControllerManagerOptions, error) {
	componentConfig, err := NewDefaultComponentConfig()
	if err != nil {
		return nil, err
	}

	s := ControllerManagerOptions{
		Generic: cmoptions.NewGenericControllerManagerConfigurationOptions(&componentConfig.Generic),

		SecureServing:  apiserveroptions.NewSecureServingOptions().WithLoopback(),
		Authentication: apiserveroptions.NewDelegatingAuthenticationOptions(),
		Authorization:  apiserveroptions.NewDelegatingAuthorizationOptions(),
		Metrics:        metrics.NewOptions(),
		Logs:           logs.NewOptions(),
	}

	// Set the PairName but leave certificate directory blank to generate in-memory by default
	s.SecureServing.ServerCert.CertDirectory = ""
	s.SecureServing.ServerCert.PairName = "firefly-controller-manager"
	// s.SecureServing.BindPort = 10257
	s.SecureServing.BindPort = 10357

	s.Generic.LeaderElection.ResourceName = "firefly-controller-manager"
	s.Generic.LeaderElection.ResourceNamespace = "kube-system"
	return &s, nil
}

func NewDefaultComponentConfig() (fireflyctrlmgrconfig.ControllerManagerConfiguration, error) {
	internal := fireflyctrlmgrconfig.ControllerManagerConfiguration{
		Generic: config.GenericControllerManagerConfiguration{
			Address:                 "0.0.0.0",
			Controllers:             []string{"*"},
			MinResyncPeriod:         metav1.Duration{Duration: 12 * time.Hour},
			ControllerStartInterval: metav1.Duration{Duration: 0 * time.Second},
		},
	}
	return internal, nil
}

// Flags returns flags for a specific APIServer by section name
func (s *ControllerManagerOptions) Flags(allControllers []string, disabledByDefaultControllers []string) cliflag.NamedFlagSets {
	fss := cliflag.NamedFlagSets{}
	s.Generic.AddFlags(&fss, allControllers, disabledByDefaultControllers)

	s.SecureServing.AddFlags(fss.FlagSet("secure serving"))
	s.Authentication.AddFlags(fss.FlagSet("authentication"))
	s.Authorization.AddFlags(fss.FlagSet("authorization"))

	s.Metrics.AddFlags(fss.FlagSet("metrics"))
	logsapi.AddFlags(s.Logs, fss.FlagSet("logs"))

	fs := fss.FlagSet("misc")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig).")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	fs.StringVar(&s.ClusterpediaKubeconfig, "clusterpedia-kubeconfig", s.ClusterpediaKubeconfig, "Path to kubeconfig file with authorization and master location information for clusterpedia.")

	return fss
}

// ApplyTo fills up controller manager config with options.
func (s *ControllerManagerOptions) ApplyTo(c *fireflycontrollerconfig.Config) error {
	if err := s.Generic.ApplyTo(&c.ComponentConfig.Generic); err != nil {
		return err
	}
	if err := s.SecureServing.ApplyTo(&c.SecureServing, &c.LoopbackClientConfig); err != nil {
		return err
	}
	if s.SecureServing.BindPort != 0 || s.SecureServing.Listener != nil {
		if err := s.Authentication.ApplyTo(&c.Authentication, c.SecureServing, nil); err != nil {
			return err
		}
		if err := s.Authorization.ApplyTo(&c.Authorization); err != nil {
			return err
		}
	}
	return nil
}

// Validate is used to validate the options and config before launching the controller manager
func (s *ControllerManagerOptions) Validate(allControllers []string, disabledByDefaultControllers []string) error {
	var errs []error
	return utilerrors.NewAggregate(errs)
}

// Config return a controller manager config objective
func (s ControllerManagerOptions) Config(allControllers []string, disabledByDefaultControllers []string) (*fireflycontrollerconfig.Config, error) {
	if err := s.Validate(allControllers, disabledByDefaultControllers); err != nil {
		return nil, err
	}

	if err := s.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	kubeconfig, err := clientcmd.BuildConfigFromFlags(s.Master, s.Kubeconfig)
	if err != nil {
		return nil, err
	}
	kubeconfig.DisableCompression = true
	kubeconfig.ContentConfig.AcceptContentTypes = s.Generic.ClientConnection.AcceptContentTypes
	kubeconfig.ContentConfig.ContentType = s.Generic.ClientConnection.ContentType
	kubeconfig.QPS = s.Generic.ClientConnection.QPS
	kubeconfig.Burst = int(s.Generic.ClientConnection.Burst)

	client, err := clientset.NewForConfig(restclient.AddUserAgent(kubeconfig, FireflyControllerManagerUserAgent))
	if err != nil {
		return nil, err
	}

	var pediaKubeconfig *restclient.Config
	if s.ClusterpediaKubeconfig != "" {
		pediaKubeconfig, err = clientcmd.BuildConfigFromFlags(s.Master, s.ClusterpediaKubeconfig)
		if err != nil {
			return nil, err
		}
	} else {
		pediaKubeconfig = restclient.CopyConfig(kubeconfig)
		pediaKubeconfig.Host = kubeconfig.Host + "/apis/clusterpedia.io/v1beta1/resources"
	}

	pediaClient, err := clientset.NewForConfig(restclient.AddUserAgent(pediaKubeconfig, FireflyControllerManagerUserAgent))
	if err != nil {
		return nil, err
	}

	info, err := client.ServerVersion()
	if err != nil {
		return nil, err
	}
	klog.Infof("firefly-controller-manager version: %s", info.String())

	eventBroadcaster := record.NewBroadcaster()
	eventRecorder := eventBroadcaster.NewRecorder(clientgokubescheme.Scheme, v1.EventSource{Component: FireflyControllerManagerUserAgent})

	c := &fireflycontrollerconfig.Config{
		Client:           client,
		PediaClient:      pediaClient,
		Kubeconfig:       kubeconfig,
		PediaKubeconfig:  pediaKubeconfig,
		EventBroadcaster: eventBroadcaster,
		EventRecorder:    eventRecorder,
	}
	if err := s.ApplyTo(c); err != nil {
		return nil, err
	}
	s.Metrics.Apply()

	return c, nil
}
