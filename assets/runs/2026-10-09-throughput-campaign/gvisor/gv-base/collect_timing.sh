#!/usr/bin/env bash
# collect_timing.sh <cutoff ISO e.g. 2026-10-09T05:30:00> <outfile>
set -u; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
CUT=${1:?}; OUT=${2:?}
kubectl -n kube-system delete pod gv-timingprobe --ignore-not-found --wait=true >/dev/null 2>&1
sed "s/__CUTOFF__/$CUT/" /tmp/tco-runs/fill/gv-base/gv-timingprobe.yaml | kubectl apply -f - >/dev/null
for i in $(seq 1 60); do sleep 5; n=$(kubectl -n kube-system logs gv-timingprobe 2>/dev/null | head -1); [ -n "$n" ] && break; done
echo "probe: $n"
kubectl -n kube-system exec gv-timingprobe -- cat /tmp/out.txt > "$OUT"
kubectl -n kube-system delete pod gv-timingprobe --wait=false >/dev/null 2>&1
wc -l "$OUT"
