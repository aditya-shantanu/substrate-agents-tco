#!/usr/bin/env bash
# next_iter.sh <prev_iter> <prev_label> <prev_cutoff ISO> <image ref or "-" to keep> <next_iter>
# waits for the running job, archives prev, rolls the pool (if ref given), starts the next iteration
set -u; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig; G=/tmp/tco-runs/fill/gv-base
PREV=${1:?}; LABEL=${2:?}; CUT=${3:?}; REF=${4:?}; NEXT=${5:?}
while true; do s=$(kubectl -n agent-sim get job agentsim -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null); [ "$s" = "True" ] && break; f=$(kubectl -n agent-sim get job agentsim -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null); [ "$f" = "True" ] && break; sleep 15; done
echo "== job done $(date)"; sleep 40; tail -1 $G/pipeline-iter$PREV.log
OUT=/tmp/tco-runs/fill/nanoturn3-east-$PREV
cp $OUT/turn.txt $G/turn-iter$PREV-$LABEL.txt; cp $OUT/hostprobe-turn.log $G/hostprobe-iter$PREV.log 2>/dev/null || kubectl -n default logs hostprobe > $G/hostprobe-iter$PREV.log 2>/dev/null
$G/collect_timing.sh $CUT $G/timing-iter$PREV-$LABEL.txt
if [ "$REF" != "-" ]; then $G/roll_pool.sh "$REF"; fi
if [ "${CGCLEAN:-0}" = "1" ]; then echo "== cgroup cleanup $(date)"; $G/cgclean.sh; fi
cd /tmp/tco-runs/fill && export TAG=east ITER=$NEXT FLEET_PREFIX=turn-1791517949-gvisor CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098
nohup bash ./nanoturn3.sh > $G/pipeline-iter$NEXT.log 2>&1 & echo "pipeline iter$NEXT pid $!" | tee $G/pipeline-iter$NEXT.pid; date
