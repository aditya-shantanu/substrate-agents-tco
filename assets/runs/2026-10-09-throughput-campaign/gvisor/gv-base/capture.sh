#!/usr/bin/env bash
# re-created capture loop for nanoturn3-east-1 (pipeline wrapper was killed at 22:33)
export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
OUT=${1:?}; name=turn
while [ ! -f $OUT/stop-$name ]; do
  perl -e 'alarm 50; exec @ARGV' -- kubectl --request-timeout=40s -n agent-sim logs job/agentsim --tail=3000 2>/dev/null | grep -E '"msg":"(setup complete|swap fill|swap fill done|swap level|swap level result|swap hold|swap cycle|swap cycle progress|swap cycle done|snapshot point)"|=== |^tag,|^,|^hold,|^cycle,|SWAP VERDICT|ready ms|^unix_ms,agent,create_ms|^[0-9]{13},[0-9]+,' >> $OUT/$name.raw
  sort -u $OUT/$name.raw > $OUT/$name.txt; sleep 45
done
