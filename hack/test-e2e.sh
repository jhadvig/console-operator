#!/bin/bash
source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

os::log::info "Running e2e tests"
KUBERNETES_CONFIG=${KUBECONFIG} go test -timeout 30m -v ./test/e2e/ -run TestCustomBrand
