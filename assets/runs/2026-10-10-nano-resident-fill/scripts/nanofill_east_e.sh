#!/usr/bin/env bash
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }
TAG=east CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a KUBECONFIG=/tmp/tco-runs/east.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098 AGENTS=9000 ACTOR_MEMORY=2Gi ACTOR_PREFIX_OVERRIDE=res-1791598802-gvisor-nano FAIL_PSI_MEM_FULL=30 FAIL_CRASHED=200 FAIL_REFUSAL_PCT=5 FAIL_ERROR_PCT=5 FAIL_TURN_P99_MS=30000 FAIL_WAKE_P99_MS=60000 WAVE_START=1150 WAVE_STEP=50 WAVE_INTERVAL=90s ./nanofill.sh > nanofill-east.log 2>&1
log "east fine fill finished"
