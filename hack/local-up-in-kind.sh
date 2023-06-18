#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

# This script starts a firelfy control plane based on current codebase.

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
BIN="${SCRIPT_ROOT}/_output/bin"
GOOS=linux

kind delete cluster --name firefly
kind create cluster --name firefly

echo
echo "Building fireflyadm and copying to kind cluster"
GOOS=linux go build -o $BIN/$GOOS/fireflyadm $SCRIPT_ROOT/cmd/fireflyadm/main.go
docker cp $BIN/$GOOS/fireflyadm firefly-control-plane:/usr/local/bin/ 

docker exec -it firefly-control-plane fireflyadm init --image-repository m.daocloud.io/registry.k8s.io

docker exec -it firefly-control-plane kubectl -n firefly-system get all

echo
echo "Installing kwok"
docker cp BIN="${SCRIPT_ROOT}/test" firefly-control-plane:/root/.firefly/test/
docker exec -it firefly-control-plane kubectl apply -f /root/.firefly/test/kwok
docker exec -it firefly-control-plane kubectl apply -f /root/.firefly/test/demo --kubeconfig /root/.firefly/control-plane/admin.conf

echo "Access the firefly with: fk"
echo 'alias fk="docker exec -it firefly-control-plane kubectl --kubeconfig /root/.firefly/control-plane/admin.conf"'