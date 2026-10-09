#!/usr/bin/env bash
# iteration 10: best build (C, already deployed) with the sim's first-lap catch-up disabled (SCRIPT_CATCHUP=false, nanoturn3.sh default)
set -u; G=/tmp/tco-runs/fill/gv-base; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
while pgrep -f "archive_iter.sh 9" >/dev/null; do sleep 10; done
echo "== archive 9 done; settling 90 s $(date +%T)"; sleep 90
kubectl -n benchmark-workloads get deploy benchmark-ateom -o jsonpath='{.spec.template.spec.containers[0].image} ready={.status.readyReplicas}{"\n"}'
cd /tmp/tco-runs/fill && export TAG=east ITER=10 FLEET_PREFIX=turn-1791517949-gvisor CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098 SCRIPT_CATCHUP=false
nohup bash ./nanoturn3.sh > $G/pipeline-iter10.log 2>&1 & echo "pipeline iter10 pid $!" | tee $G/pipeline-iter10.pid; date
