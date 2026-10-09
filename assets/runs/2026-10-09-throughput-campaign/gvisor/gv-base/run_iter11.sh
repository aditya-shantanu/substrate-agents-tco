#!/usr/bin/env bash
# iteration 11: build D, catch-up off, finer ramp (x1.25) to locate the ceiling between 8 and 12 swaps/s
set -u; G=/tmp/tco-runs/fill/gv-base; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
while pgrep -f "archive_iter.sh 10" >/dev/null; do sleep 10; done
echo "== archive 10 done $(date +%T); pulling probe logs"
kubectl -n kube-system logs gv-forkprobe > $G/forkprobe.log 2>/dev/null; kubectl -n kube-system logs gv-cgprobe > $G/cgprobe-full.log 2>/dev/null; kubectl -n kube-system logs gv-mountprobe > $G/mountprobe.log 2>/dev/null
python3 $G/probes.py $G/turn-iter10-D-nocatchup.txt | tee $G/probes-iter10.md
echo "== settling 90 s $(date +%T)"; sleep 90
cd /tmp/tco-runs/fill && export TAG=east ITER=11 FLEET_PREFIX=turn-1791517949-gvisor CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098 SCRIPT_CATCHUP=false SWAP_MULT_OVERRIDE=1.25
nohup bash ./nanoturn3.sh > $G/pipeline-iter11.log 2>&1 & echo "pipeline iter11 pid $!" | tee $G/pipeline-iter11.pid; date
