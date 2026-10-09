#!/usr/bin/env bash
# park every RUNNING actor in agents-sim (before rolling the worker pool), 12 in parallel
set -u; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
L=/tmp/tco-runs/fill/gv-base/park-list-$(date +%s).txt
perl -e 'alarm 300; exec @ARGV' -- ~/go/bin/kubectl-ate get actors -a agents-sim | awk '$4=="ACTOR_STATE_RUNNING"{print $2}' > $L
echo "$(wc -l < $L) running actors to park"
cat $L | xargs -P 12 -I{} sh -c 'perl -e "alarm 120; exec @ARGV" -- ~/go/bin/kubectl-ate suspend actor {} -a agents-sim >/dev/null 2>&1 || echo "park failed: {}"'
echo "park pass done $(date)"
