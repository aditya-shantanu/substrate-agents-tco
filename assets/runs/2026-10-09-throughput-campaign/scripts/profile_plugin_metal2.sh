#!/usr/bin/env bash
# wait until the sim reaches the given level (n per tick), then take a 40-s CPU profile + heap profile of the snapshot plugin
set -u; LEVEL=${1:?n_per_tick}; ITER=${2:?iter}; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig; cd /tmp/tco-runs/fill
log() { echo "$(date +%H:%M:%S) == $*"; }
T=nanoturn3-metal2-$ITER/turn.txt
while ! grep -h '"msg":"swap level"' $T 2>/dev/null | python3 -c "import sys,json; sys.exit(0 if any(json.loads(l)['n_per_tick']>=$LEVEL for l in sys.stdin) else 1)"; do sleep 10; done; log "level $LEVEL started; waiting 30 s into it"; sleep 30
AT=$(kubectl --request-timeout=30s -n ate-system get pods --no-headers -o custom-columns=:metadata.name | grep ^atelet)
kubectl -n ate-system port-forward pod/$AT 16060:6060 >/dev/null 2>&1 & PF=$!; sleep 3
curl -sf -m 60 "localhost:16060/debug/pprof/profile?seconds=40" -o prof-plugin-cpu-l$LEVEL-i$ITER.pb.gz && log "cpu profile saved ($(wc -c < prof-plugin-cpu-l$LEVEL-i$ITER.pb.gz) bytes)"
curl -sf -m 20 "localhost:16060/debug/pprof/heap" -o prof-plugin-heap-l$LEVEL-i$ITER.pb.gz && log "heap profile saved"
curl -sf -m 20 "localhost:16060/debug/pprof/allocs" -o prof-plugin-allocs-l$LEVEL-i$ITER.pb.gz && log "allocs profile saved"
for k in 1 2 3 4 5; do curl -sf -m 20 "localhost:16060/debug/pprof/goroutine?debug=1" -o prof-plugin-goroutines-l$LEVEL-i$ITER-$k.txt && log "goroutines $k saved"; sleep 15; done
kill $PF 2>/dev/null; log "done"
