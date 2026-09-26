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

# 2. Purge actors. Fast path: the autosuspender's in-cluster /purge endpoint
#    (one connection, 8-way parallel deletes). Fallback for a cluster where
#    the autosuspender isn't deployed yet: kubectl-ate, parallelized —
#    each CLI invocation pays ~1.5s of port-forward setup, so 8 at a time.
purged=false
if kubectl -n agent-sim get svc autosuspender >/dev/null 2>&1; then
  PFPORT=18098
  kubectl -n agent-sim port-forward svc/autosuspender ${PFPORT}:8080 >/dev/null 2>&1 &
  PF_PID=$!
  trap 'kill ${PF_PID} 2>/dev/null || true' EXIT
  sleep 3
  resp=$(curl -sf --max-time 200 -X POST "localhost:${PFPORT}/purge?atespace=${ATESPACE}" || true)
  if [[ "${resp}" == *'"remaining":0'* ]]; then
    echo "  actors purged (${resp})"
    purged=true
  elif [[ -n "${resp}" ]]; then
    echo "  purge incomplete (${resp}); falling back" >&2
  fi
fi

if [[ "${purged}" != true ]]; then
  KATE=/tmp/kubectl-ate-clean
  if [[ ! -x "${KATE}" ]]; then
    echo "  building kubectl-ate (first time only)..."
    (cd "${SUBSTRATE_REPO}" && go build -o "${KATE}" ./cmd/kubectl-ate)
  fi
  list_actors() { "${KATE}" get actors -a "${ATESPACE}" 2>/dev/null | awk 'NR>1{print $2}'; }
  deadline=$((SECONDS + 180))
  while :; do
    actors=$(list_actors)
    [[ -z "${actors}" ]] && break
    printf '%s\n' ${actors} | xargs -P 8 -I{} \
      "${KATE}" delete actor {} -a "${ATESPACE}" --any-state >/dev/null 2>&1 || true
    if (( SECONDS > deadline )); then
      echo "WARNING: some actors still draining after 180s:" >&2
      list_actors >&2
      break
    fi
    sleep 5
  done
  echo "  actors purged"
fi

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
