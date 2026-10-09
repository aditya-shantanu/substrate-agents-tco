#!/usr/bin/env bash
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
./park_all_metal2.sh 2>&1 | tail -2; log "old fleet parked"
./tmpfs_revert_metal2.sh 2>&1 | tail -4
kubectl -n kube-system exec nodeshell -- nsenter -t 1 -m -- sh -c 'df -h /var/lib/ate/actors | tail -1; ls /var/lib/ate/actors | wc -l'
kubectl --request-timeout=30s -n kube-system delete pod plugsample --ignore-not-found --wait=true >/dev/null 2>&1; AT=$(kubectl --request-timeout=30s -n ate-system get pods --no-headers -o custom-columns=:metadata.name | grep ^atelet); CID=$(kubectl --request-timeout=30s -n ate-system get pod $AT -o jsonpath='{.status.initContainerStatuses[?(@.name=="snapshot-plugin")].containerID}' | sed 's#containerd://##'); sed -i '' "s/[0-9a-f]\{64\}/$CID/" plugsample.sh; kubectl --request-timeout=30s -n kube-system delete configmap plugsample --ignore-not-found >/dev/null 2>&1; kubectl -n kube-system create configmap plugsample --from-file=run.sh=plugsample.sh >/dev/null; kubectl apply -f plugsample.json >/dev/null; log "sampler restarted"
TAG=metal2 ITER=1 FLEET_PREFIX="pause-$(date +%s)-microvm" CLS=microvm CLUSTER=agents-tco-euw4 ZONE=europe-west4-c KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east1 ACTOR_MEMORY_OVERRIDE=256Mi DASH_PORT=18180 PURGE_PORT=18198 SWAP_START_OVERRIDE=8 SWAP_MULT_OVERRIDE=1.35 ./nanopause.sh > /tmp/tco-runs/fill/nanopause-metal2-1.log 2>&1
log "pause iteration 1 finished"
