#!/usr/bin/env bash
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
kubectl --request-timeout=30s -n agent-sim delete job agentsim --ignore-not-found --wait=false >/dev/null 2>&1
for n in $(pgrep -f 'nanofill.sh'); do if ps -E -o command= -p $n 2>/dev/null | grep -q 'TAG=east'; then for c in $(pgrep -P $n); do ps -o command= -p $c | grep -q run.sh && kill -9 $c; done; fi; done
sleep 15; while pgrep -f 'nanofill.sh' >/dev/null && ! grep -q 'all done' nanofill-east.log; do sleep 5; done; log "east fill (250-waves) stopped"; mv nanofill-east nanofill-east-waves250 2>/dev/null
TAG=east CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a KUBECONFIG=/tmp/tco-runs/east.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098 AGENTS=9000 ACTOR_MEMORY=2Gi ACTOR_PREFIX_OVERRIDE=res-1791598802-gvisor-nano FAIL_PSI_MEM_FULL=30 FAIL_CRASHED=200 FAIL_REFUSAL_PCT=5 FAIL_ERROR_PCT=5 FAIL_TURN_P99_MS=30000 WAVE_START=750 WAVE_STEP=200 WAVE_INTERVAL=2m ./nanofill.sh > nanofill-east.log 2>&1
log "east fill finished"
