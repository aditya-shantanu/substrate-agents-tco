#!/usr/bin/env bash
# archive_iter.sh <iter> <label> <cutoff>: wait for the job, archive turn/hostprobe/timing, regenerate the report
set -u; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig; G=/tmp/tco-runs/fill/gv-base; IT=${1:?}; LABEL=${2:?}; CUT=${3:?}
while true; do s=$(kubectl -n agent-sim get job agentsim -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null); [ "$s" = "True" ] && break; f=$(kubectl -n agent-sim get job agentsim -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null); [ "$f" = "True" ] && break; sleep 15; done
echo "== job done $(date)"; sleep 45
OUT=/tmp/tco-runs/fill/nanoturn3-east-$IT
cp $OUT/turn.txt $G/turn-iter$IT-$LABEL.txt; cp $OUT/hostprobe-turn.log $G/hostprobe-iter$IT.log 2>/dev/null || kubectl -n default logs hostprobe > $G/hostprobe-iter$IT.log 2>/dev/null
$G/collect_timing.sh $CUT $G/timing-iter$IT-$LABEL.txt
$G/assemble_report.sh; echo "== archived $(date)"
