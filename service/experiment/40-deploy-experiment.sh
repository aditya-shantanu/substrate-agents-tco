#!/usr/bin/env bash
# Builds+pushes the autosuspender/agentsim images with ko and deploys the
# experiment: agent-sim namespace, autosuspender Deployment+Service, and the
# agentsim Job (parameterized from the environment). Re-running replaces the Job.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# Every run starts from zero: purge previous actors/job/snapshots/counters.
# (The TUI runs clean.sh as its own visible stage and sets SKIP_CLEAN=true.)
if [[ "${SKIP_CLEAN:-}" != "true" ]]; then
  "${EXP_DIR}/clean.sh"
fi

# Stage 3 self-skips when the pool is already up, so the Burstable patch
# (required for node swap to apply to workers) must also happen here.
if [[ "${WORKER_BURSTABLE:-false}" == "true" ]]; then
  if kubectl -n benchmark-workloads patch workerpool benchmark-ateom --type merge -p "$(worker_resources_patch)"; then
    echo "workerpool Burstable (${WORKER_REQ_CPU}/${WORKER_REQ_MEM} req, ${WORKER_LIM_CPU}/${WORKER_LIM_MEM} lim); waiting for worker rollout..."
    sleep 5
    kubectl -n benchmark-workloads rollout status deploy/benchmark-ateom --timeout=240s || true
  fi
fi

cd "${SERVICE_DIR}"
export AUTOSUSPENDER_IMAGE=$(ko build --base-import-paths ./cmd/autosuspender)
export AGENTSIM_IMAGE=$(ko build --base-import-paths ./cmd/agentsim)
echo "autosuspender: ${AUTOSUSPENDER_IMAGE}"
echo "agentsim:      ${AGENTSIM_IMAGE}"

# Jobs are immutable; drop a previous run before re-applying.
kubectl -n agent-sim delete job agentsim --ignore-not-found

# A script of your own (WORKLOAD=<path>.yaml) rides to the Job in a
# ConfigMap mounted at /etc/agentscript/script.yaml. Validate it here with
# the same loader agentsim uses, so a bad file fails now, not in pod logs.
kubectl create namespace agent-sim --dry-run=client -o yaml | kubectl apply -f - >/dev/null
if [[ "${SCRIPT_ARG}" == /etc/agentscript/* ]]; then
  go run ./cmd/agentsim --check-script "${WORKLOAD}"
  kubectl -n agent-sim create configmap agent-script --from-file=script.yaml="${WORKLOAD}" \
    --dry-run=client -o yaml | kubectl apply -f -
else
  kubectl -n agent-sim delete configmap agent-script --ignore-not-found >/dev/null
fi
echo "workload: ${WORKLOAD} (script=${SCRIPT_ARG:-none}, think ×${THINK_SCALE}, suspend=${SCRIPT_SUSPEND}, ${SESSIONS_PER_DAY} sessions/tasks + ${WAKES_PER_DAY} wakes per day, actor memory ${ACTOR_MEMORY})"

export AGENTS COMPRESS DURATION IDLE_TIMEOUT UNWEDGE_AFTER MAX_RUNNING SETUP_SUSPEND LOAD_TEST WAVE_START WAVE_STEP WAVE_INTERVAL MEM_TARGET MEM_CHURN
export FAIL_REL_P90 FAIL_PROBE_P99_MS FAIL_MEM_AVAIL_PCT FAIL_PSI_MEM_FULL FAIL_PSI_CPU_SOME FAIL_CRASHED HOLD_AFTER_FAIL FAIL_REFUSAL_PCT FAIL_ERROR_PCT FAIL_WAKE_P99_MS FAIL_TURN_P99_MS SWAP_FILL SWAP_START SWAP_MULT SWAP_EVERY SWAP_CYCLE_ALL SCRIPT_SNAPSHOT_STEPS
export SESSIONS_PER_DAY WAKES_PER_DAY SCRIPT_ARG THINK_SCALE SCRIPT_SUSPEND PING_ACTORS_PER_USER PING_WAIT PING_LIVE LIFECYCLE_MODE PING_INDEPENDENT SETUP_CONCURRENCY COLD_START_ONLY ACTOR_PREFIX SCRIPT_LOOP RAMP_SECONDS
envsubst < manifests/agent-sim.yaml | kubectl apply -f -

kubectl -n agent-sim rollout status deploy/autosuspender --timeout=120s
kubectl -n agent-sim get pods
echo
echo "Live dashboard:  kubectl -n agent-sim port-forward svc/autosuspender 8080 &"
echo "                 open http://localhost:8080/"
echo "Job logs:        kubectl -n agent-sim logs -f job/agentsim | tee run.log"
