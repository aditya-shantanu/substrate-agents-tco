#!/usr/bin/env bash
# extension beyond the rule ceiling: relative-P90 gates off, absolute gates only (probe P99 1 s, turn P99 1 s, mem 10 %, PSI, errors/refusals 5 %) — where is the hard wall?
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }
TAG=east-ext CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a KUBECONFIG=/tmp/tco-runs/east.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098 AGENTS=9000 ACTOR_MEMORY=2Gi ACTOR_PREFIX_OVERRIDE=res-1791598802-gvisor-nano FAIL_REL_P90=100 FAIL_PSI_MEM_FULL=30 FAIL_CRASHED=200 FAIL_REFUSAL_PCT=5 FAIL_ERROR_PCT=5 FAIL_TURN_P99_MS=1000 FAIL_WAKE_P99_MS=60000 WAVE_START=5950 WAVE_STEP=200 WAVE_INTERVAL=2m ./nanofill.sh > nanofill-east-ext.log 2>&1
log "east extension finished"
