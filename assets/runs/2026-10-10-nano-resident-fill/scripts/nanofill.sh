#!/usr/bin/env bash
# Resident host fill with the nano-personal-agent (no park/resume once awake): register AGENTS (boot + suspend), then wake
# WAVE_STEP more every WAVE_INTERVAL and keep them resident until a gate trips; hold at the last clean wave.
#   TAG= CLS= CLUSTER= ZONE= KUBECONFIG= SUBSTRATE_REPO= DASH_PORT= PURGE_PORT= AGENTS= ACTOR_MEMORY= ./nanofill.sh
set -u
F=/tmp/tco-runs/fill; TAG=${TAG:?}; export KUBECONFIG=${KUBECONFIG:?} SUBSTRATE_REPO=${SUBSTRATE_REPO:?} DASH_PORT=${DASH_PORT:-18080} PURGE_PORT=${PURGE_PORT:-18098}
log() { echo "$(date +%H:%M:%S) [$TAG] == $*"; }
cd /Users/adityashantanu/repos/substrate-agents-tco/service/experiment
P=gke-ai-eco-dev
export CLUSTER_NAME=${CLUSTER:?} CLUSTER_LOCATION=${ZONE:?} GCE_REGION=${ZONE%-*} GVISOR_NODE_MACHINE_TYPE=c3-standard-192-metal NODE_COUNT="" ENABLE_NESTED_VIRTUALIZATION=false
export KUBECTL_CONTEXT=gke_${P}_${ZONE}_${CLUSTER} SANDBOX_CLASS=${CLS:?}
export WORKLOAD=nano-personal-agent THINK_SCALE=${THINK_SCALE:-0.02} SCRIPT_LOOP=true SCRIPT_SUSPEND=idle SCRIPT_CATCHUP=false RAMP_SECONDS=60
export ACTOR_MEMORY=${ACTOR_MEMORY:?} ACTOR_CPU=2 WORKER_COUNT=100 WORKER_BURSTABLE=false DEPLOY_WAIT_SECS=900 COMPRESS=1
export LOAD_TEST=true WAVE_START=${WAVE_START:-500} WAVE_STEP=${WAVE_STEP:-500} WAVE_INTERVAL=${WAVE_INTERVAL:-4m} HOLD_AFTER_FAIL=${HOLD_AFTER_FAIL:-10m}
export FAIL_REL_P90=${FAIL_REL_P90:-2} FAIL_PROBE_P99_MS=1000 FAIL_TURN_P99_MS=${FAIL_TURN_P99_MS:-1000} FAIL_WAKE_P99_MS=${FAIL_WAKE_P99_MS:-10000} FAIL_MEM_AVAIL_PCT=10 FAIL_PSI_MEM_FULL=${FAIL_PSI_MEM_FULL:-10} FAIL_PSI_CPU_SOME=50 FAIL_CRASHED=${FAIL_CRASHED:-50} FAIL_REFUSAL_PCT=${FAIL_REFUSAL_PCT:-0.5} FAIL_ERROR_PCT=${FAIL_ERROR_PCT:-0.5}
export LIFECYCLE_MODE=none SETUP_SUSPEND=true IDLE_TIMEOUT=24h MAX_RUNNING=24h UNWEDGE_AFTER=10m SETUP_CONCURRENCY=32 SKIP_CLEAN=true SETUP_PARK_EXISTING=false
export AGENTS=${AGENTS:?} DURATION=${DURATION:-240m} ACTOR_PREFIX="${ACTOR_PREFIX_OVERRIDE:-res-$(date +%s)-$CLS-nano}"
RES=/Users/adityashantanu/repos/substrate-agents-tco/service/experiment/results
OUT=$F/nanofill-$TAG; mkdir -p $OUT
capture() { : > $OUT/fill.raw; while [ ! -f $OUT/stop-fill ]; do perl -e 'alarm 50; exec @ARGV' -- kubectl --request-timeout=40s -n agent-sim logs job/agentsim --tail=3000 2>/dev/null | grep -E '"msg":"(setup complete|wave|wave result|hold|loadtest)"|=== |^wave,|^hold,|LOADTEST VERDICT' >> $OUT/fill.raw; sort -u $OUT/fill.raw > $OUT/fill.txt; sleep 45; done; }
log "######## resident fill: $CLS nano agents, up to $AGENTS (+$WAVE_STEP per $WAVE_INTERVAL from $WAVE_START), $ACTOR_MEMORY + ${ACTOR_CPU} vCPU, 100 pods, prefix $ACTOR_PREFIX ########"
kubectl -n agent-sim delete job agentsim --ignore-not-found --wait=true >/dev/null 2>&1
kubectl -n default delete pod hostprobe --ignore-not-found --wait=true >/dev/null 2>&1; kubectl apply -f $F/hostprobe.yaml >/dev/null
rm -f $OUT/stop-fill; capture & CAP=$!
./run.sh > $OUT/run-fill.log 2>&1; log "run.sh exit=$?"; touch $OUT/stop-fill; wait $CAP 2>/dev/null
out=$(ls -td $RES/*/ | head -1); for d in $(ls -td $RES/*/ | head -5); do grep -q "$ACTOR_PREFIX" "$d/run.log" 2>/dev/null && { out=$d; break; }; done; cp "$out/run.log" $OUT/simlog-fill.txt 2>/dev/null
kubectl -n default logs hostprobe > $OUT/hostprobe-fill.log 2>/dev/null; kubectl -n default delete pod hostprobe --wait=false >/dev/null 2>&1
grep -h "LOADTEST VERDICT" $OUT/fill.txt $OUT/simlog-fill.txt 2>/dev/null | head -1 | cut -c1-300 | xargs -0 log
log "all done"
