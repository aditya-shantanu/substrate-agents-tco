#!/usr/bin/env bash
# Builds+pushes the autosuspender/agentsim images with ko and deploys the
# experiment: agent-sim namespace, autosuspender Deployment+Service, and the
# agentsim Job (parameterized from env.sh). Re-running replaces the Job.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cd "${SERVICE_DIR}"
export AUTOSUSPENDER_IMAGE=$(ko build --base-import-paths ./cmd/autosuspender)
export AGENTSIM_IMAGE=$(ko build --base-import-paths ./cmd/agentsim)
echo "autosuspender: ${AUTOSUSPENDER_IMAGE}"
echo "agentsim:      ${AGENTSIM_IMAGE}"

# Jobs are immutable; drop a previous run before re-applying.
kubectl -n agent-sim delete job agentsim --ignore-not-found

export AGENTS COMPRESS DURATION IDLE_TIMEOUT LOAD_TEST WAVE_START WAVE_STEP WAVE_INTERVAL
envsubst < manifests/agent-sim.yaml | kubectl apply -f -

kubectl -n agent-sim rollout status deploy/autosuspender --timeout=120s
kubectl -n agent-sim get pods
echo
echo "Live dashboard:  kubectl -n agent-sim port-forward svc/autosuspender 8080 &"
echo "                 open http://localhost:8080/"
echo "Job logs:        kubectl -n agent-sim logs -f job/agentsim | tee run.log"
