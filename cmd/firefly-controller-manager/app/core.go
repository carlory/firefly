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

package app

import (
	"context"
	"fmt"
	"time"

	"k8s.io/controller-manager/controller"

	"github.com/carlory/firefly/pkg/controller/cluster"
	"github.com/carlory/firefly/pkg/controller/cluster/node"
)

func startClusterController(ctx context.Context, controllerContext ControllerContext) (controller.Interface, bool, error) {
	ctrl, err := cluster.NewClusterController(
		controllerContext.ClientBuilder.ClientOrDie("cluster-controller"),
		controllerContext.ClientBuilder.FireflyClientOrDie("cluster-controller"),
		controllerContext.FireflyInformerFactory.Cluster().V1alpha1().Clusters(),
		30*time.Second,
	)
	if err != nil {
		return nil, false, fmt.Errorf("error creating cluster controller: %v", err)
	}
	go ctrl.Run(ctx, 1)
	return nil, true, nil
}

func startNodeController(ctx context.Context, controllerContext ControllerContext) (controller.Interface, bool, error) {
	ctrl, err := node.NewNodeController(
		controllerContext.ClientBuilder.ClientOrDie("node-controller"),
		controllerContext.ClientBuilder.FireflyClientOrDie("node-controller"),
		controllerContext.FireflyInformerFactory.Cluster().V1alpha1().Clusters(),
		30*time.Second,
	)
	if err != nil {
		return nil, false, fmt.Errorf("error creating node controller: %v", err)
	}
	go ctrl.Run(ctx, 1)
	return nil, true, nil
}
