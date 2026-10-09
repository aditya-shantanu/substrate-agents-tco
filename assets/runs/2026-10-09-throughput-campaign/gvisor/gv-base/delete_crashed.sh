#!/usr/bin/env bash
# delete CRASHED actors (12 parallel) so the next sim setup re-registers them
export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
cat ${1:?} | xargs -P 12 -I{} sh -c 'perl -e "alarm 120; exec @ARGV" -- ~/go/bin/kubectl-ate delete actor {} -a agents-sim --any-state >/dev/null 2>&1 || echo "delete failed: {}"'
echo "delete pass done $(date)"
