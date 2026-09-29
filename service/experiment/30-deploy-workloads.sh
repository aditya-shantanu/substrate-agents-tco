#!/usr/bin/env bash
# Deploys the benchmark ActorTemplates (glutton et al, atespace
# benchmark-workloads) and a WorkerPool with $WORKER_COUNT gVisor workers.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
sync_substrate_env

cd "${SUBSTRATE_REPO}"
# Shape the workers BEFORE deploy.sh scales the pool: a previous run's
# (larger) requests persist through `apply`, and deploy.sh waits for the
# rollout — with 60 workers at 512Mi requests that wait times out.
if [[ "${WORKER_BURSTABLE:-false}" == "true" ]]; then
  kubectl -n benchmark-workloads patch workerpool benchmark-ateom --type merge \
    -p "$(worker_resources_patch)" 2>/dev/null || true
fi
./benchmarking/workloads/deploy.sh --deploy --worker-count "${WORKER_COUNT}" \
  --sandbox-class "${SANDBOX_CLASS:-gvisor}" --actor-memory "${ACTOR_MEMORY}"

# Swap applies only to Burstable pods: give workers requests<limits so the
# kernel may actually swap their cold pages.
if [[ "${WORKER_BURSTABLE:-false}" == "true" ]]; then
  kubectl -n benchmark-workloads patch workerpool benchmark-ateom --type merge -p "$(worker_resources_patch)"
  echo "workerpool patched Burstable (${WORKER_REQ_CPU}/${WORKER_REQ_MEM} req, ${WORKER_LIM_CPU}/${WORKER_LIM_MEM} lim)"
fi

kubectl get workerpools -A
