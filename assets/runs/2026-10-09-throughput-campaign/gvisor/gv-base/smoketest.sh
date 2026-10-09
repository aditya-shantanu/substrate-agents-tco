#!/usr/bin/env bash
# smoketest.sh <K>: resume K suspended actors, suspend them, resume K others; then count gofer failures in worker logs.
# Exercises "second create on a worker after a teardown" without the sim.
set -u; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig; G=/tmp/tco-runs/fill/gv-base; K=${1:-60}
T0=$(date +%s)
L=$G/smoke-list-$T0.txt
perl -e 'alarm 300; exec @ARGV' -- ~/go/bin/kubectl-ate get actors -a agents-sim | awk '$4=="ACTOR_STATE_SUSPENDED"{print $2}' > $L
n=$(wc -l < $L); echo "suspended available: $n"
A=$(head -$K $L); B=$(sed -n "$((K+1)),$((2*K))p" $L)
echo "== wave 1 resume $(date +%T)"; echo "$A" | xargs -P 8 -I{} sh -c 'perl -e "alarm 120; exec @ARGV" -- ~/go/bin/kubectl-ate resume actor {} -a agents-sim >/dev/null 2>&1 || echo "resume failed: {}"' | sort | uniq -c | head -3
sleep 5
echo "== wave 1 suspend $(date +%T)"; echo "$A" | xargs -P 8 -I{} sh -c 'perl -e "alarm 120; exec @ARGV" -- ~/go/bin/kubectl-ate suspend actor {} -a agents-sim >/dev/null 2>&1 || echo "suspend failed: {}"' | sort | uniq -c | head -3
sleep 5
echo "== wave 2 resume $(date +%T)"; echo "$B" | xargs -P 8 -I{} sh -c 'perl -e "alarm 120; exec @ARGV" -- ~/go/bin/kubectl-ate resume actor {} -a agents-sim >/dev/null 2>&1 || echo "resume failed: {}"' | sort | uniq -c | head -3
sleep 5
echo "== wave 2 suspend $(date +%T)"; echo "$B" | xargs -P 8 -I{} sh -c 'perl -e "alarm 120; exec @ARGV" -- ~/go/bin/kubectl-ate suspend actor {} -a agents-sim >/dev/null 2>&1 || echo "suspend failed: {}"' | sort | uniq -c | head -3
S=$(( $(date +%s) - T0 + 30 ))
echo "== worker log scan (last ${S}s)"
creates=0; fails=0; for W in $(kubectl -n benchmark-workloads get pods -o name | grep benchmark-ateom); do out=$(kubectl -n benchmark-workloads logs $W --since=${S}s 2>/dev/null | grep -c -E 'About to run runsc create"|cannot create gofer process' ); c=$(kubectl -n benchmark-workloads logs $W --since=${S}s 2>/dev/null | grep -c 'About to run runsc create'); f=$(kubectl -n benchmark-workloads logs $W --since=${S}s 2>/dev/null | grep -c 'cannot create gofer process'); creates=$((creates+c)); fails=$((fails+f)); done
echo "runsc creates=$creates gofer_failures=$fails"
