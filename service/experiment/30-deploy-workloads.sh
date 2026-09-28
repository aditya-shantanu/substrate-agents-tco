#!/usr/bin/env bash
# Deploys the benchmark ActorTemplates (glutton et al, atespace
# benchmark-workloads) and a WorkerPool with $WORKER_COUNT gVisor workers.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
sync_substrate_env

cd "${SUBSTRATE_REPO}"
./benchmarking/workloads/deploy.sh --deploy --worker-count "${WORKER_COUNT}" \
  --sandbox-class "${SANDBOX_CLASS:-gvisor}"

# Swap applies only to Burstable pods: give workers requests<limits so the
# kernel may actually swap their cold pages.
if [[ "${WORKER_BURSTABLE:-false}" == "true" ]]; then
  kubectl -n benchmark-workloads patch workerpool benchmark-ateom --type merge -p \
    '{"spec":{"template":{"resources":{"requests":{"cpu":"200m","memory":"512Mi"},"limits":{"cpu":"1","memory":"1Gi"}}}}}'
  echo "workerpool patched Burstable (200m/512Mi req, 1/1Gi lim)"
fi

kubectl get workerpools -A
