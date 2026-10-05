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
# deploy.sh waits for the pool rollout and the golden snapshots; a 60-worker
# rollout needs more than its 300s default (upstream reads WAIT_TIMEOUT_SECS
# from the environment in our pinned worktree).
# WORKER_NODE_SELECTOR / WORKER_TOLERATION pin the worker pods to a node set
# (e.g. a bare-metal pool: WORKER_NODE_SELECTOR=ate.dev/pool=metal
# WORKER_TOLERATION=ate.dev/sandboxClass=microvm:NoSchedule); empty = any node.
WAIT_TIMEOUT_SECS="${DEPLOY_WAIT_SECS:-900}" ./benchmarking/workloads/deploy.sh --deploy \
  --worker-count "${WORKER_COUNT}" --sandbox-class "${SANDBOX_CLASS:-gvisor}" --actor-memory "${ACTOR_MEMORY}" \
  ${WORKER_NODE_SELECTOR:+--worker-node-selector "${WORKER_NODE_SELECTOR}"} \
  ${WORKER_TOLERATION:+--worker-toleration "${WORKER_TOLERATION}"}

# Swap applies only to Burstable pods: give workers requests<limits so the
# kernel may actually swap their cold pages.
if [[ "${WORKER_BURSTABLE:-false}" == "true" ]]; then
  kubectl -n benchmark-workloads patch workerpool benchmark-ateom --type merge -p "$(worker_resources_patch)"
  echo "workerpool patched Burstable (${WORKER_REQ_CPU}/${WORKER_REQ_MEM} req, ${WORKER_LIM_CPU}/${WORKER_LIM_MEM} lim)"
fi

kubectl get workerpools -A
