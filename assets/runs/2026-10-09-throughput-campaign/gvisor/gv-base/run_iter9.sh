#!/usr/bin/env bash
# after the iteration-8 archive: roll build 4 (pool + shared-root, old teardown) and run iteration 9 on the same (fresh) fleet
set -u; G=/tmp/tco-runs/fill/gv-base; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
while pgrep -f archive_iter.sh >/dev/null; do sleep 10; done
REF=$(cat $G/ateom-sharedroot-ref.txt); echo "#### build 4 again $REF $(date +%T)"
$G/roll_pool.sh "$REF" 2>&1 | grep -E "^== |readyReplicas"
cd /tmp/tco-runs/fill && export TAG=east ITER=9 FLEET_PREFIX=turn-1791517949-gvisor CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098
nohup bash ./nanoturn3.sh > $G/pipeline-iter9.log 2>&1 & echo "pipeline iter9 pid $!" | tee $G/pipeline-iter9.pid; date
