#!/usr/bin/env bash
# ONE COMMAND, WHOLE SHOW:
#   ./run.sh                # cluster → substrate → workloads → live test → $$
#   ./run.sh --skip-setup   # cluster already prepared: live test + report only
#
# Stages print as banners; the test streams live stats (awake/asleep agents,
# worker occupancy bar, suspend/resume latencies, throughput); the finale is
# the results card with measured COST PER AGENT PER MONTH.
# Knobs in env.sh: WORKER_COUNT, AGENTS, COMPRESS, DURATION, IDLE_TIMEOUT.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

BOLD=$'\033[1m'; DIM=$'\033[2m'; CYAN=$'\033[36m'; GREEN=$'\033[32m'; RESET=$'\033[0m'
RULE="━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
STAGE_T0=0

stage() { # stage <n> <total> <title>
  echo
  echo "${CYAN}${BOLD}${RULE}${RESET}"
  echo "${CYAN}${BOLD} [$1/$2] $3${RESET}"
  echo "${CYAN}${BOLD}${RULE}${RESET}"
  STAGE_T0=$(date +%s)
}
stage_done() {
  echo "${GREEN}${BOLD} ✓ done in $(( $(date +%s) - STAGE_T0 ))s${RESET}"
}
run_quiet() { # indent child output, keep it visible but subordinate
  "$@" 2>&1 | sed "s/^/   ${DIM}/;s/$/${RESET}/"
}

TOTAL=5
if [[ "${1:-}" != "--skip-setup" ]]; then
  stage 1 $TOTAL "Creating GKE cluster '${CLUSTER_NAME}' + snapshot bucket + IAM  ${DIM}(~10-15 min first time)${RESET}"
  run_quiet "${EXP_DIR}/10-bootstrap.sh";        stage_done
  stage 2 $TOTAL "Installing Substrate control plane  ${DIM}(ateapi · scheduler · router · atelet)${RESET}"
  run_quiet "${EXP_DIR}/20-install-substrate.sh"; stage_done
  stage 3 $TOTAL "Deploying sandbox workload (glutton) + ${WORKER_COUNT}-worker pool"
  run_quiet "${EXP_DIR}/30-deploy-workloads.sh"; stage_done
fi

stage 4 $TOTAL "Deploying the experiment  ${DIM}(auto-suspender + ${AGENTS} simulated personal agents)${RESET}"
run_quiet "${EXP_DIR}/40-deploy-experiment.sh"; stage_done

OUT="${EXP_DIR}/results/$(date +%Y%m%d-%H%M%S)"
mkdir -p "${OUT}"

kubectl -n agent-sim port-forward svc/autosuspender 18080:8080 >/dev/null 2>&1 &
PF_PID=$!
trap 'kill ${PF_PID} 2>/dev/null || true' EXIT
sleep 3

stage 5 $TOTAL "TEST RUNNING — ${AGENTS} agents · ${WORKER_COUNT} workers · ${DURATION} · time ×${COMPRESS}"
echo "   ${DIM}live dashboard: http://localhost:18080/   (kept open while this runs)${RESET}"
echo

cluster_tick() {
  curl -sf --max-time 4 localhost:18080/state.json | python3 -c '
import json, sys, datetime
try:
    s = json.load(sys.stdin)
except Exception:
    sys.exit(0)
sm = s.get("samples") or []
if not sm:
    sys.exit(0)
l = sm[-1]
awake = l["running"] + l["resuming"] + l["suspending"]
busy, total = l["assigned"], l["workers_total"]
agents, asleep = l["actors_total"], l["suspended"]
dens = "%.1f:1" % (agents / busy) if busy else "-"
bar = "#" * busy + "." * max(total - busy, 0)
now = datetime.datetime.now().strftime("%H:%M:%S")
print("[%s] agents: %2d awake / %3d asleep | workers [%s] %d/%d | %d suspends, avg %.1fs | density now %s"
      % (now, awake, asleep, bar, busy, total, s.get("suspends", 0),
         s.get("suspend_avg_ms", 0) / 1000, dens))' 2>/dev/null || true
}

sim_tick() {
  kubectl -n agent-sim logs job/agentsim --tail=400 2>/dev/null | python3 -c '
import json, sys
last = None
for line in sys.stdin:
    if "\"progress\"" in line:
        try:
            last = json.loads(line)
        except Exception:
            pass
if last:
    print("           sim: %d activations | wake p50 %.1fs p99 %.1fs | in-session req p50 %dms | refusals %d errors %d"
          % (last.get("activations", 0), last.get("wake_p50_ms", 0) / 1000,
             last.get("wake_p99_ms", 0) / 1000, last.get("session_p50_ms", 0),
             last.get("refusals", 0), last.get("errors", 0)))' 2>/dev/null || true
}

while true; do
  cluster_tick
  sim_tick
  if kubectl -n agent-sim wait --for=condition=complete job/agentsim --timeout=0s >/dev/null 2>&1; then
    echo
    echo "${GREEN}${BOLD} ✓ test complete${RESET}"
    break
  fi
  if kubectl -n agent-sim wait --for=condition=failed job/agentsim --timeout=0s >/dev/null 2>&1; then
    echo " ✗ agentsim FAILED — last logs:" >&2
    kubectl -n agent-sim logs job/agentsim | tail -30 >&2
    exit 1
  fi
  sleep 20
done

# ---- collect + the finale ----
kubectl -n agent-sim logs job/agentsim > "${OUT}/run.log"
curl -sf localhost:18080/occupancy.csv > "${OUT}/occupancy.csv"
curl -sf localhost:18080/metrics > "${OUT}/metrics.txt"

MACHINE_TYPE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.labels.node\.kubernetes\.io/instance-type}')
POOL_NODES=$(kubectl -n benchmark-workloads get pods -l ate.dev/worker-pool \
  -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' 2>/dev/null | sort -u | grep -c . || echo 1)
SNAP_GIB=$(gcloud storage du -s "gs://${BUCKET_NAME}/benchmark-workloads/glutton/atespaces/agents-sim/**" 2>/dev/null \
  | awk -v n="${AGENTS}" '$1>0 {printf "%.3f", $1/n/1073741824}')
SNAP_GIB=${SNAP_GIB:-0.05}

python3 "${SERVICE_DIR}/analysis/final_report.py" \
  --run-log "${OUT}/run.log" --occupancy "${OUT}/occupancy.csv" \
  --metrics "${OUT}/metrics.txt" --compress "${COMPRESS}" \
  --machine-type "${MACHINE_TYPE}" --pool-workers "${WORKER_COUNT}" \
  --pool-nodes "${POOL_NODES}" --snap-gib "${SNAP_GIB}" \
  ${PRICE_MODEL:+--price-model ${PRICE_MODEL}} \
  | tee "${OUT}/report.txt"

echo "${DIM}artifacts: ${OUT}${RESET}"
