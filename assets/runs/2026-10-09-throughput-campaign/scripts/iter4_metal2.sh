#!/usr/bin/env bash
set -u
cd /tmp/tco-runs/fill; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
log() { echo "$(date +%H:%M:%S) == $*"; }
./roll_workers_metal2.sh "$(cat ateom-reseed-ref.txt)" 2>&1 | tail -12
log "deleting CRASHED actors"
perl -e 'alarm 590; exec @ARGV' -- ~/go/bin/kubectl-ate get actors -a agents-sim -o json 2>/dev/null | python3 -c "
import sys,json,collections
d=json.load(sys.stdin); items=d.get('actors',d) if isinstance(d,dict) else d
print(collections.Counter(a.get('status',{}).get('state') for a in items))
open('/tmp/tco-runs/fill/crashed-now.txt','w').write('\n'.join(a['metadata']['name'] for a in items if a.get('status',{}).get('state')=='ACTOR_STATE_CRASHED'))"
cat crashed-now.txt | xargs -P 8 -I{} sh -c '~/go/bin/kubectl-ate delete actor {} -a agents-sim --any-state >/dev/null 2>&1 || echo "fail {}"' | tail -3; log "deleted $(wc -l < crashed-now.txt) crashed"
kubectl --request-timeout=30s -n kube-system delete pod plugsample --ignore-not-found --wait=true >/dev/null 2>&1; kubectl apply -f plugsample.json >/dev/null; log "sampler restarted"
TAG=metal2 ITER=4 FLEET_PREFIX=turn-1791517949-microvm CLS=microvm CLUSTER=agents-tco-euw4 ZONE=europe-west4-c KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east1 ACTOR_MEMORY_OVERRIDE=256Mi DASH_PORT=18180 PURGE_PORT=18198 ./nanoturn3.sh > /tmp/tco-runs/fill/nanoturn3-metal2-4.log 2>&1
log "iteration 4 finished"
