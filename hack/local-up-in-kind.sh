#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

# This script starts a firelfy control plane based on current codebase.

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
BIN="${SCRIPT_ROOT}/_output/bin"
HOST_OS=$(uname -s)
GOOS=linux

kind delete cluster --name firefly > /dev/null 2>&1 || true
kind create cluster --name firefly --config ${SCRIPT_ROOT}/hack/kind-config.yaml

echo
echo "Building fireflyadm and copying to kind cluster"
GOOS=linux go build -o $BIN/$GOOS/fireflyadm $SCRIPT_ROOT/cmd/fireflyadm/main.go
docker cp $BIN/$GOOS/fireflyadm firefly-control-plane:/usr/local/bin/
docker cp $SCRIPT_ROOT/deploy firefly-control-plane:/root/.firefly/

docker exec -it firefly-control-plane fireflyadm init --image-repository m.daocloud.io/registry.k8s.io

docker exec -it firefly-control-plane kubectl -n firefly-system get all

echo
echo "Installing CRDs"
docker exec -it firefly-control-plane kubectl apply -f /root/.firefly/crds --kubeconfig /root/.firefly/control-plane/admin.conf

echo
echo "Installing kwok"
docker cp "${SCRIPT_ROOT}/test" firefly-control-plane:/root/.firefly/test/
docker exec -it firefly-control-plane kubectl apply -f /root/.firefly/test/kwok
docker exec -it firefly-control-plane kubectl apply -f /root/.firefly/test/demo --kubeconfig /root/.firefly/control-plane/admin.conf

echo
echo "Copying admin.conf"
docker cp firefly-control-plane:/root/.firefly/control-plane/admin.conf ${SCRIPT_ROOT}/_output/admin.conf
# chmod 755 ${SCRIPT_ROOT}/_output/admin.conf

if [ "$HOST_OS" == "Linux" ]; then
  sed -i 's/^    server:.*$/    server: https:\/\/127.0.0.1:31000/g' ${SCRIPT_ROOT}/_output/admin.conf
elif [ "$HOST_OS" == "Darwin" ]; then
  sed -i '' 's/^    server:.*$/    server: https:\/\/127.0.0.1:31000/g' ${SCRIPT_ROOT}/_output/admin.conf
else
  echo "Unsupported platform: $OS"
  exit 1
fi

echo
echo "Access the firefly control plane with:"
echo "kubectl --kubeconfig ${SCRIPT_ROOT}/_output/admin.conf"