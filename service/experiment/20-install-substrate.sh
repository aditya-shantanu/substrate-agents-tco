#!/usr/bin/env bash
# Builds (ko) and installs the Substrate control plane onto the cluster:
# ateapi x2 + Postgres, atecontroller, atelet DaemonSet, atenet router/DNS,
# podcertcontroller, gvisor-default SandboxConfig. Idempotent.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
sync_substrate_env

cd "${SUBSTRATE_REPO}"
./hack/install-ate.sh --deploy-ate-system

kubectl -n ate-system get pods
