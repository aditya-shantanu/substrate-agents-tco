#!/usr/bin/env bash
# Pre-flight cleaner: removes every trace of a previous run so each test
# starts from zero — leftover sim actors (any state, including wedged ones
# still pinning workers), the old agentsim Job, the run's GCS snapshot
# objects (stale objects would inflate the measured snapshot size), and the
# autosuspender's in-memory counters/samples (restart). Safe on a fresh
# cluster: every step tolerates absence. Called by 40-deploy-experiment.sh;
# also usable standalone.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ATESPACE="${ATESPACE:-agents-sim}"

echo "cleaning previous run artifacts (atespace ${ATESPACE})..."

# 1. Stop the old load generator first so nothing recreates actors mid-purge.
kubectl -n agent-sim delete job agentsim --ignore-not-found --wait=true 2>/dev/null || true

# 2. Purge actors. kubectl-ate is built once from the substrate checkout.
KATE=/tmp/kubectl-ate-clean
if [[ ! -x "${KATE}" ]]; then
  (cd "${SUBSTRATE_REPO}" && go build -o "${KATE}" ./cmd/kubectl-ate)
fi
list_actors() { "${KATE}" get actors -a "${ATESPACE}" 2>/dev/null | awk 'NR>1{print $2}'; }

deadline=$((SECONDS + 150))
while :; do
  actors=$(list_actors)
  [[ -z "${actors}" ]] && break
  for a in ${actors}; do
    "${KATE}" delete actor "${a}" -a "${ATESPACE}" --any-state >/dev/null 2>&1 || true
  done
  if (( SECONDS > deadline )); then
    echo "WARNING: some actors still draining after 150s:" >&2
    list_actors >&2
    break
  fi
  sleep 5
done
echo "  actors purged"

# 3. Old snapshots: actors are gone, so their objects are orphans. Removing
#    them keeps the per-run snapshot-size measurement honest.
gcloud storage rm -r \
  "gs://${BUCKET_NAME}/benchmark-workloads/glutton/atespaces/${ATESPACE}/**" \
  --project "${PROJECT_ID}" >/dev/null 2>&1 || true
echo "  stale snapshots removed"

# 4. Fresh counters and occupancy history for the new run.
if kubectl -n agent-sim get deploy autosuspender >/dev/null 2>&1; then
  kubectl -n agent-sim rollout restart deploy/autosuspender >/dev/null
  kubectl -n agent-sim rollout status deploy/autosuspender --timeout=120s >/dev/null
  echo "  autosuspender counters reset"
fi

echo "clean."
