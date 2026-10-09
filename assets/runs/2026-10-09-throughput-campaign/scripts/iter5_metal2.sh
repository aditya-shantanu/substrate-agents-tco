#!/usr/bin/env bash
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }
while ! grep -q 'all done' nanoturn3-metal2-4.log; do sleep 15; done; log "iteration 4 done"
export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
kubectl --request-timeout=30s -n kube-system delete pod plugsample --ignore-not-found --wait=true >/dev/null 2>&1; kubectl apply -f plugsample.json >/dev/null; log "sampler restarted"
TAG=metal2 ITER=5 FLEET_PREFIX=turn-1791517949-microvm CLS=microvm CLUSTER=agents-tco-euw4 ZONE=europe-west4-c KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east1 ACTOR_MEMORY_OVERRIDE=256Mi DASH_PORT=18180 PURGE_PORT=18198 SWAP_START_OVERRIDE=8 SWAP_MULT_OVERRIDE=1.2 ./nanoturn3.sh > /tmp/tco-runs/fill/nanoturn3-metal2-5.log 2>&1
log "iteration 5 finished"
