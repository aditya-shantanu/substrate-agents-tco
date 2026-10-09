#!/usr/bin/env bash
# nano-personal-agent on Substrate main, suspend only: (A) 100 cold starts, 4 in flight; (B) register 5000, 650 awake
# at think x1, turnover ramp N=10..640 per 10 s (4-min levels, back to back), hold at the last clean N, then cycle until
# every actor has been resident once. Actor shape 2 vCPU + 2 GiB. 100 worker pods.
#   CLS= CLUSTER= ZONE= KUBECONFIG= SUBSTRATE_REPO= TAG= DASH_PORT= PURGE_PORT= ./nanoturn.sh
set -u
F=/tmp/tco-runs/fill; TAG=${TAG:?}; export KUBECONFIG=${KUBECONFIG:?} SUBSTRATE_REPO=${SUBSTRATE_REPO:?} DASH_PORT=${DASH_PORT:-18080} PURGE_PORT=${PURGE_PORT:-18098}
log() { echo "$(date +%H:%M:%S) [$TAG] == $*"; }
cd /Users/adityashantanu/repos/substrate-agents-tco/service/experiment
P=gke-ai-eco-dev; BUCKET=snapshot-${CLUSTER:?}-$P
export CLUSTER_NAME=$CLUSTER CLUSTER_LOCATION=${ZONE:?} GCE_REGION=${ZONE%-*} GVISOR_NODE_MACHINE_TYPE=c3-standard-192-metal NODE_COUNT="" ENABLE_NESTED_VIRTUALIZATION=false
export KUBECTL_CONTEXT=gke_${P}_${ZONE}_${CLUSTER} SANDBOX_CLASS=${CLS:?}
export WORKLOAD=nano-personal-agent ACTOR_MEMORY=${ACTOR_MEMORY_OVERRIDE:-2Gi} ACTOR_CPU=2 LIFECYCLE_MODE=suspend WORKER_BURSTABLE=false DEPLOY_WAIT_SECS=900 LOAD_TEST=false COMPRESS=1
export IDLE_TIMEOUT=24h MAX_RUNNING=24h UNWEDGE_AFTER=10m
RES=/Users/adityashantanu/repos/substrate-agents-tco/service/experiment/results
OUT=$F/nanoturn-$TAG; mkdir -p $OUT
capture() { # bg: keep the sim's key lines (cold starts, swap levels, cycle) even if the kubelet rotates the log
  : > $OUT/$1.raw; while [ ! -f $OUT/stop-$1 ]; do perl -e 'alarm 50; exec @ARGV' -- kubectl --request-timeout=40s -n agent-sim logs job/agentsim --tail=3000 2>/dev/null | grep -E '"msg":"(setup complete|swap fill|swap fill done|swap level|swap level result|swap hold|swap cycle|swap cycle progress|swap cycle done|snapshot point)"|=== |^tag,|^,|^hold,|^cycle,|SWAP VERDICT|ready ms|^unix_ms,agent,create_ms|^[0-9]{13},[0-9]+,' >> $OUT/$1.raw; sort -u $OUT/$1.raw > $OUT/$1.txt; sleep 45; done; }
# ---------- (A) cold starts
export COLD_START_ONLY=true AGENTS=100 SETUP_CONCURRENCY=4 WORKER_COUNT=100 ACTOR_PREFIX="cold-$(date +%s)-$CLS" DURATION=10m
log "######## (A) cold starts: $AGENTS fresh nano actors, 4 in flight, $CLS, $ACTOR_MEMORY + ${ACTOR_CPU} vCPU ########"
./clean.sh > $OUT/clean-cold.log 2>&1; ./30-deploy-workloads.sh > $OUT/deploy-cold.log 2>&1; log "deploy exit=$?"
rm -f $OUT/stop-cold; capture cold & CAP=$!
./run.sh > $OUT/run-cold.log 2>&1; log "run.sh exit=$?"; touch $OUT/stop-cold; wait $CAP 2>/dev/null
out=$(ls -td $RES/*/ | head -1); cp "$out/run.log" $OUT/simlog-cold.txt 2>/dev/null
grep -h "ready ms" $OUT/cold.txt $OUT/simlog-cold.txt 2>/dev/null | head -1 | xargs log "cold start:"
# ---------- (B) register 5000, fill 650, ramp, hold, cycle
export COLD_START_ONLY=false AGENTS=5000 SETUP_CONCURRENCY=32 WORKER_COUNT=100 ACTOR_PREFIX="turn-$(date +%s)-$CLS" DURATION=180m
export THINK_SCALE=1 SCRIPT_LOOP=true SCRIPT_SUSPEND=idle RAMP_SECONDS=0 SETUP_SUSPEND=true
export SWAP_FILL=650 SWAP_START=10 SWAP_MULT=2 SWAP_EVERY=10s WAVE_INTERVAL=4m HOLD_AFTER_FAIL=10m SWAP_CYCLE_ALL=true
export FAIL_REL_P90=2.5 FAIL_WAKE_P99_MS=5000 FAIL_REFUSAL_PCT=0.5 FAIL_ERROR_PCT=0.5 FAIL_MEM_AVAIL_PCT=10 FAIL_PSI_MEM_FULL=10 FAIL_PSI_CPU_SOME=50 FAIL_CRASHED=5
log "######## (B) register $AGENTS, $SWAP_FILL awake at think x1, swap N=$SWAP_START x$SWAP_MULT per $SWAP_EVERY, ${WAVE_INTERVAL} levels, hold $HOLD_AFTER_FAIL, then cycle all ########"
./clean.sh > $OUT/clean-turn.log 2>&1; ./30-deploy-workloads.sh > $OUT/deploy-turn.log 2>&1; log "deploy exit=$? (pool $(kubectl -n benchmark-workloads get workerpool benchmark-ateom -o jsonpath='{.status.readyReplicas}')/$WORKER_COUNT)"
kubectl -n default delete pod hostprobe --ignore-not-found --wait=true >/dev/null 2>&1; kubectl apply -f $F/hostprobe.yaml >/dev/null
rm -f $OUT/stop-turn; capture turn & CAP=$!
# snapshot-size sample a few minutes into registration (first suspends land in the bucket)
( sleep 420; gcloud storage ls -l "gs://$BUCKET/benchmark-workloads/glutton/atespaces/agents-sim/actors/**" 2>/dev/null | head -400 > $OUT/bucket-early.txt; log "early bucket sample: $(grep -c 'gs://' $OUT/bucket-early.txt) objects" ) &
./run.sh > $OUT/run-turn.log 2>&1; log "run.sh exit=$?"; touch $OUT/stop-turn; wait $CAP 2>/dev/null
out=$(ls -td $RES/*/ | head -1); cp "$out/run.log" $OUT/simlog-turn.txt 2>/dev/null
kubectl -n default logs hostprobe > $OUT/hostprobe-turn.log 2>/dev/null; kubectl -n default delete pod hostprobe --wait=false >/dev/null 2>&1
grep -h "SWAP VERDICT" $OUT/turn.txt $OUT/simlog-turn.txt 2>/dev/null | head -1 | cut -c1-400 | xargs -0 log
log "all done"
