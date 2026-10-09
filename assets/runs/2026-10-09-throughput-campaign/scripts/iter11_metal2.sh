#!/usr/bin/env bash
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
while ! grep -q 'all done' nanoturn3-metal2-10.log; do sleep 15; done; log "iteration 10 done"
./park_all_metal2.sh 2>&1 | tail -3; log "parked"
./tmpfs_actors_metal2.sh 2>&1 | tail -6
kubectl --request-timeout=30s -n kube-system delete pod plugsample --ignore-not-found --wait=true >/dev/null 2>&1; AT=$(kubectl --request-timeout=30s -n ate-system get pods --no-headers -o custom-columns=:metadata.name | grep ^atelet); CID=$(kubectl --request-timeout=30s -n ate-system get pod $AT -o jsonpath='{.status.initContainerStatuses[?(@.name=="snapshot-plugin")].containerID}' | sed 's#containerd://##'); sed -i '' "s/[0-9a-f]\{64\}/$CID/" plugsample.sh; kubectl --request-timeout=30s -n kube-system delete configmap plugsample --ignore-not-found >/dev/null 2>&1; kubectl -n kube-system create configmap plugsample --from-file=run.sh=plugsample.sh >/dev/null; kubectl apply -f plugsample.json >/dev/null; log "sampler restarted"
nohup ./profile_plugin_metal2.sh 14 11 > profile-i11.log 2>&1 &
TAG=metal2 ITER=11 FLEET_PREFIX=turn-1791517949-microvm CLS=microvm CLUSTER=agents-tco-euw4 ZONE=europe-west4-c KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east1 ACTOR_MEMORY_OVERRIDE=256Mi DASH_PORT=18180 PURGE_PORT=18198 SWAP_START_OVERRIDE=8 SWAP_MULT_OVERRIDE=1.3 ./nanoturn3.sh > /tmp/tco-runs/fill/nanoturn3-metal2-11.log 2>&1
log "iteration 11 finished"
