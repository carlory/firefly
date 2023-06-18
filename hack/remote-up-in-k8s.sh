#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

# This script starts a firelfy control plane based on current codebase.

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
BIN="${SCRIPT_ROOT}/_output/bin"

echo "Building fireflyadm"
go build -o $BIN/fireflyadm $SCRIPT_ROOT/cmd/fireflyadm/main.go

echo "Installing firefly"
sudo $BIN/fireflyadm init --image-repository m.daocloud.io/registry.k8s.io

kubectl -n firefly-system get all