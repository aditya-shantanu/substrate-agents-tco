#!/usr/bin/env bash
# Deploys the benchmark ActorTemplates (glutton et al, atespace
# benchmark-workloads) and a WorkerPool with $WORKER_COUNT gVisor workers.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
sync_substrate_env

cd "${SUBSTRATE_REPO}"
./benchmarking/workloads/deploy.sh --deploy --worker-count "${WORKER_COUNT}" \
  --sandbox-class "${SANDBOX_CLASS:-gvisor}"

kubectl get workerpools -A
