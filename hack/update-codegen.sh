#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
export GOPATH=$(go env GOPATH | awk -F ':' '{print $1}')
export PATH=$PATH:$GOPATH/bin

GO111MODULE=on go install k8s.io/code-generator/cmd/deepcopy-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/defaulter-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/register-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/client-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/lister-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/informer-gen
GO111MODULE=on go install k8s.io/code-generator/cmd/openapi-gen

echo "Generating external apis"

deepcopy-gen \
  --go-header-file hack/boilerplate/boilerplate.go.txt \
  --input-dirs=github.com/carlory/firefly/pkg/adm/apis/kubeadm/v1beta3 \
  --output-package=github.com/carlory/firefly/pkg/adm/apis/kubeadm/v1beta3 \
  --output-file-base=zz_generated.deepcopy 

conversion-gen \
  --go-header-file hack/boilerplate/boilerplate.go.txt \
  --input-dirs=github.com/carlory/firefly/pkg/adm/apis/kubeadm/v1beta3 \
  --output-package=github.com/carlory/firefly/pkg/adm/apis/kubeadm/v1beta3 \
  --output-file-base=zz_generated.conversion 

defaulter-gen \
  --go-header-file hack/boilerplate/boilerplate.go.txt \
  --input-dirs=github.com/carlory/firefly/pkg/adm/apis/kubeadm/v1beta3 \
  --output-package=github.com/carlory/firefly/pkg/adm/apis/kubeadm/v1beta3 \
  --output-file-base=zz_generated.defaults 


echo "Generating internal apis"

deepcopy-gen \
  --go-header-file hack/boilerplate/boilerplate.go.txt \
  --input-dirs=github.com/carlory/firefly/pkg/adm/apis/kubeadm \
  --output-package=github.com/carlory/firefly/pkg/adm/apis/kubeadm \
  --output-file-base=zz_generated.deepcopy 