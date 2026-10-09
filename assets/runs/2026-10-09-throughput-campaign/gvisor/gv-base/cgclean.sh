#!/usr/bin/env bash
# remove empty, >2-min-old per-container sandbox cgroups leaked by failed runsc deletes (run when actors are parked)
export KUBECONFIG=/tmp/tco-runs/east.kubeconfig; G=/tmp/tco-runs/fill/gv-base
kubectl -n kube-system delete pod gv-cgclean --ignore-not-found --wait=true >/dev/null 2>&1
kubectl apply -f $G/gv-cgclean.yaml >/dev/null
for i in $(seq 1 120); do sleep 5; kubectl -n kube-system logs gv-cgclean 2>/dev/null | grep -q DONE && break; done
kubectl -n kube-system logs gv-cgclean 2>/dev/null | tee -a $G/cgclean.log
kubectl -n kube-system delete pod gv-cgclean --wait=false >/dev/null 2>&1
