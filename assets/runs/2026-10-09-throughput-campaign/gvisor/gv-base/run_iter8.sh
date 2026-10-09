#!/usr/bin/env bash
# roll variant C, smoke-test it, and only if clean start sim iteration 8
set -u; G=/tmp/tco-runs/fill/gv-base; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
while pgrep -f ab_variants.sh >/dev/null; do sleep 10; done
REF=$(cat $G/ateom-varC-ref.txt); echo "#### variant C $REF $(date +%T)"
$G/roll_pool.sh "$REF" 2>&1 | grep -E "^== |readyReplicas"
$G/smoketest.sh 60 2>&1 | tee $G/smoke-varC.log | tail -4
f=$(grep -o 'gofer_failures=[0-9]*' $G/smoke-varC.log | cut -d= -f2); r=$(grep -c 'resume failed' $G/smoke-varC.log)
echo "smoke C: gofer_failures=$f resume_failed_lines=$r"
if [ "${f:-1}" = "0" ] && [ "$r" = "0" ]; then
  cd /tmp/tco-runs/fill && export TAG=east ITER=8 FLEET_PREFIX=turn-1791517949-gvisor CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098
  nohup bash ./nanoturn3.sh > $G/pipeline-iter8.log 2>&1 & echo "pipeline iter8 pid $!" | tee $G/pipeline-iter8.pid; date
else
  echo "NOT starting the sim: smoke test not clean"
fi
