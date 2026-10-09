#!/usr/bin/env bash
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
while ! grep -q 'all done' nanopause-metal2-6.log; do sleep 15; done; log "pause run 6 done"
PREFIX=$(cat pause-fleet-prefix.txt)
./roll_workers_metal2.sh "$(cat ateom-timeouts-ref.txt)" 2>&1 | tail -4
perl -e 'alarm 590; exec @ARGV' -- ~/go/bin/kubectl-ate get actors -a agents-sim -o json 2>/dev/null | python3 -c "
import sys,json,collections; d=json.load(sys.stdin); items=[a for a in (d.get('actors',d) if isinstance(d,dict) else d) if a['metadata']['name'].startswith('$PREFIX')]
print('pause fleet:', collections.Counter(a.get('status',{}).get('state') for a in items))
open('/tmp/tco-runs/fill/pause-crashed.txt','w').write('\n'.join(a['metadata']['name'] for a in items if a.get('status',{}).get('state')=='ACTOR_STATE_CRASHED')+'\n')"
n=$(grep -c . pause-crashed.txt); log "deleting $n crashed"; [[ $n -gt 0 ]] && cat pause-crashed.txt | xargs -P 8 -I{} sh -c '~/go/bin/kubectl-ate delete actor {} -a agents-sim --any-state >/dev/null 2>&1 || true'
kubectl --request-timeout=30s -n kube-system delete pod plugsample --ignore-not-found --wait=true >/dev/null 2>&1; kubectl apply -f plugsample.json >/dev/null; log "sampler restarted"
TAG=metal2 ITER=7 FLEET_PREFIX="$PREFIX" CLS=microvm CLUSTER=agents-tco-euw4 ZONE=europe-west4-c KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east1 ACTOR_MEMORY_OVERRIDE=256Mi DASH_PORT=18180 PURGE_PORT=18198 SWAP_START_OVERRIDE=8 SWAP_MULT_OVERRIDE=1.35 FAIL_CRASHED_OVERRIDE=200 FAIL_REL_P90_OVERRIDE=0 FAIL_REFUSAL_PCT_OVERRIDE=2 FAIL_ERROR_PCT_OVERRIDE=2 ./nanopause.sh > /tmp/tco-runs/fill/nanopause-metal2-7.log 2>&1
log "pause run 7 finished"
