#!/usr/bin/env bash
# roll_pool.sh <ateom-gvisor image ref>: park all RUNNING actors, roll the worker pool to REF, delete CRASHED leftovers
set -u; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig; REF=${1:?}
echo "== park pass $(date)"; /tmp/tco-runs/fill/gv-base/park_all.sh
echo "== patch pool $(date)"
kubectl -n benchmark-workloads patch workerpool benchmark-ateom --type merge -p "{\"spec\":{\"workerImage\":\"$REF\"}}"
sleep 5; perl -e 'alarm 900; exec @ARGV' -- kubectl -n benchmark-workloads rollout status deployment/benchmark-ateom
kubectl -n benchmark-workloads get deploy benchmark-ateom -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
echo "== wait for WorkerPool status $(date)"
for i in $(seq 1 60); do r=$(kubectl -n benchmark-workloads get workerpool benchmark-ateom -o jsonpath='{.status.readyReplicas}'); [ "$r" = "100" ] && break; sleep 5; done; echo "workerpool readyReplicas=$r"; sleep 20
echo "== crashed cleanup $(date)"
L=/tmp/tco-runs/fill/gv-base/post-roll-list-$(date +%s).txt
perl -e 'alarm 300; exec @ARGV' -- ~/go/bin/kubectl-ate get actors -a agents-sim > $L; awk '{print $4}' $L | sort | uniq -c
awk '$4!="ACTOR_STATE_SUSPENDED" && $2 ~ /^turn-/ {print $2}' $L | xargs -P 12 -I{} sh -c 'perl -e "alarm 120; exec @ARGV" -- ~/go/bin/kubectl-ate delete actor {} -a agents-sim --any-state >/dev/null 2>&1 || echo "delete failed: {}"'
echo "== done $(date)"
