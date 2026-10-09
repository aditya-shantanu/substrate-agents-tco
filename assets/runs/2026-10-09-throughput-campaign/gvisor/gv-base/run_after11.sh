#!/usr/bin/env bash
set -u; G=/tmp/tco-runs/fill/gv-base; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
$G/archive_iter.sh 11 D-ramp125 2026-10-09T10:50:00
kubectl -n kube-system logs gv-forkprobe > $G/forkprobe.log 2>/dev/null; kubectl -n kube-system logs gv-cgprobe > $G/cgprobe-full.log 2>/dev/null; kubectl -n kube-system logs gv-mountprobe > $G/mountprobe.log 2>/dev/null
python3 $G/probes.py $G/turn-iter11-D-ramp125.txt | tee $G/probes-iter11.md
$G/assemble_report.sh; echo "== after11 done $(date)"
