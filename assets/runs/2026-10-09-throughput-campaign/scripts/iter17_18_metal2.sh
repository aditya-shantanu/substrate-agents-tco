#!/usr/bin/env bash
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
run_iter() { # ITER SWAP_START MULT
  nohup ./profile_plugin_metal2.sh 18 $1 > profile-i$1.log 2>&1 &
  TAG=metal2 ITER=$1 FLEET_PREFIX=turn-1791517949-microvm CLS=microvm CLUSTER=agents-tco-euw4 ZONE=europe-west4-c KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east1 ACTOR_MEMORY_OVERRIDE=256Mi DASH_PORT=18180 PURGE_PORT=18198 SWAP_START_OVERRIDE=$2 SWAP_MULT_OVERRIDE=$3 ./nanoturn3.sh > /tmp/tco-runs/fill/nanoturn3-metal2-$1.log 2>&1
  log "iteration $1 finished"
}
restart_sampler() { kubectl --request-timeout=30s -n kube-system delete pod plugsample --ignore-not-found --wait=true >/dev/null 2>&1; AT=$(kubectl --request-timeout=30s -n ate-system get pods --no-headers -o custom-columns=:metadata.name | grep ^atelet); CID=$(kubectl --request-timeout=30s -n ate-system get pod $AT -o jsonpath='{.status.initContainerStatuses[?(@.name=="snapshot-plugin")].containerID}' | sed 's#containerd://##'); sed -i '' "s/[0-9a-f]\{64\}/$CID/" plugsample.sh; kubectl --request-timeout=30s -n kube-system delete configmap plugsample --ignore-not-found >/dev/null 2>&1; kubectl -n kube-system create configmap plugsample --from-file=run.sh=plugsample.sh >/dev/null; kubectl apply -f plugsample.json >/dev/null; log "sampler restarted"; }
while ! grep -q 'all done' nanoturn3-metal2-16.log; do sleep 15; done; log "iteration 16 done"
# 17: worker with the sandbox network-namespace pool (ported from the gVisor campaign), plugin v9b
./roll_workers_metal2.sh "$(cat ateom-netpool-ref.txt)" 2>&1 | tail -5
restart_sampler; run_iter 17 8 1.35
# 18: + denser zstd level for snapshot uploads (ATE_ZSTD_LEVEL=default)
DS=atelet-v0-4-0-25-g66f8a888-dirty; kubectl --request-timeout=60s -n ate-system set env daemonset/$DS -c snapshot-plugin ATE_ZSTD_LEVEL=default >/dev/null; kubectl --request-timeout=300s -n ate-system rollout status daemonset/$DS --timeout=300s | tail -1
restart_sampler; run_iter 18 8 1.35
