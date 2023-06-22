/*
Copyright 2022 The Firefly Authors.

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

package pod

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
)

func ClusterMetaNamespaceKeyFunc(pod *v1.Pod) (string, error) {
	parts := strings.Split(pod.Spec.NodeName, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected node name format: %q", pod.Spec.NodeName)
	}
	clusterName := parts[len(parts)-1]
	return fmt.Sprintf("%s~%s/%s", clusterName, pod.Namespace, pod.Name), nil
}
