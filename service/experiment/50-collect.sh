#!/usr/bin/env bash
# Collects results after the agentsim Job finishes and runs the report.
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
  | tee "${OUT}/report.txt"

echo
echo "results in ${OUT}"
