#!/usr/bin/env bash
# Collects artifacts after the agentsim Job finishes and runs BOTH analyses:
# analysis/report.py (density deep-dive: mean/p99/peak, latency quantiles)
# and analysis/final_report.py (the cost-per-agent-per-month card). run.sh
# and the tco TUI do this inline; this script is the standalone equivalent.
# Usage: 50-collect.sh [output-dir]   (default results/<timestamp>)
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

OUT="${1:-${EXP_DIR}/results/$(date +%Y%m%d-%H%M%S)}"
mkdir -p "${OUT}"

kubectl -n agent-sim wait --for=condition=complete job/agentsim --timeout=10s 2>/dev/null \
  || echo "note: job not (yet) complete — collecting anyway"

kubectl -n agent-sim logs job/agentsim > "${OUT}/run.log"

kubectl -n agent-sim port-forward svc/autosuspender 18080:8080 >/dev/null 2>&1 &
PF_PID=$!
trap 'kill ${PF_PID} 2>/dev/null || true' EXIT
sleep 2
curl -s localhost:18080/occupancy.csv > "${OUT}/occupancy.csv"
curl -s localhost:18080/metrics > "${OUT}/metrics.txt"

WORKER_COST_HR="${WORKER_COST_HR:-}"
python3 "${SERVICE_DIR}/analysis/report.py" \
  --sim-log "${OUT}/run.log" --occupancy "${OUT}/occupancy.csv" \
  --compress "${COMPRESS}" ${WORKER_COST_HR:+--worker-cost-hr ${WORKER_COST_HR}} \
  | tee "${OUT}/density.txt"

MACHINE_TYPE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.labels.node\.kubernetes\.io/instance-type}')
POOL_NODES=$(kubectl -n benchmark-workloads get pods -l ate.dev/worker-pool \
  -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' 2>/dev/null | sort -u | grep -c . || echo 1)
SNAP_GIB=$(gcloud storage du -s "gs://${BUCKET_NAME}/benchmark-workloads/glutton/atespaces/agents-sim/**" 2>/dev/null \
  | awk -v n="${AGENTS}" '$1>0 {printf "%.3f", $1/n/1073741824}')
python3 "${SERVICE_DIR}/analysis/final_report.py" \
  --run-log "${OUT}/run.log" --occupancy "${OUT}/occupancy.csv" \
  --metrics "${OUT}/metrics.txt" --compress "${COMPRESS}" \
  --machine-type "${MACHINE_TYPE}" --pool-workers "${WORKER_COUNT}" \
  --pool-nodes "${POOL_NODES}" --snap-gib "${SNAP_GIB:-0.05}" \
  --price-model "${PRICE_MODEL}" | tee "${OUT}/report.txt"

echo
echo "results in ${OUT}"
