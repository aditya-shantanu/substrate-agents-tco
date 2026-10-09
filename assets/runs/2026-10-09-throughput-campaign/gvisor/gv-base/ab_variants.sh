#!/usr/bin/env bash
set -u; G=/tmp/tco-runs/fill/gv-base
for v in A B; do
  if [ $v = A ]; then REF=$(cat $G/ateom-varA-noslots-ref.txt); else REF=$(cat $G/ateom-varB-nosharedroot-ref.txt); fi
  echo "#### variant $v $REF $(date +%T)"
  $G/roll_pool.sh "$REF" 2>&1 | grep -E "^== |readyReplicas" 
  $G/smoketest.sh 60 2>&1 | tail -3
done
echo "#### done $(date +%T)"
