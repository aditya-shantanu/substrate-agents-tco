#!/usr/bin/env bash
# Between iterations: park every RUNNING actor (so the roll crashes nothing), point the WorkerPool at the
# rebuilt ateom-microvm image (tar fsync skipped), wait for the 100 workers to roll.
set -u
export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
REF=${1:?image ref}
log() { echo "$(date +%H:%M:%S) == $*"; }
~/go/bin/kubectl-ate get actors -a agents-sim 2>/dev/null > /tmp/tco-runs/fill/actors-before-roll.txt
awk 'NR>1 && ($4=="ACTOR_STATE_RUNNING"){print $2}' /tmp/tco-runs/fill/actors-before-roll.txt > /tmp/tco-runs/fill/to-park.txt
log "parking $(wc -l < /tmp/tco-runs/fill/to-park.txt) running actors (16 in parallel)"
cat /tmp/tco-runs/fill/to-park.txt | xargs -P 16 -I{} sh -c '~/go/bin/kubectl-ate suspend actor {} -a agents-sim >/dev/null 2>&1 || echo "park failed {}"' | tail -5
sleep 20; ~/go/bin/kubectl-ate get actors -a agents-sim 2>/dev/null | awk 'NR>1{print $4}' | sort | uniq -c
log "rolling workers to $REF"
kubectl -n benchmark-workloads patch workerpool benchmark-ateom --type merge -p "{\"spec\":{\"workerImage\":\"$REF\"}}"
sleep 10; kubectl -n benchmark-workloads rollout status deployment/benchmark-ateom --timeout=900s | tail -1
kubectl -n benchmark-workloads get pods --no-headers | awk '{print $3}' | sort | uniq -c
P=$(kubectl -n benchmark-workloads get pods --no-headers -o custom-columns=:metadata.name | grep benchmark-ateom | head -1); kubectl -n benchmark-workloads get pod $P -o jsonpath='{.spec.containers[0].image}{"\n"}'
log "roll done"
