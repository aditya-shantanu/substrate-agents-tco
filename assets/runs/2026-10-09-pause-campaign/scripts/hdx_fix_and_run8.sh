#!/usr/bin/env bash
set -u; cd /tmp/tco-runs/fill; log() { echo "$(date +%H:%M:%S) == $*"; }; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
# 1. stop run 8 (started on the old disk): delete the job, kill its run.sh hard (the exit trap only kills a port-forward)
kubectl --request-timeout=30s -n agent-sim delete job agentsim --ignore-not-found --wait=false >/dev/null 2>&1
for p in $(pgrep -f 'bash ./run.sh'); do kill -9 $p 2>/dev/null; done
while pgrep -f 'nanopause.sh' >/dev/null; do sleep 5; done; log "run 8 (old disk) stopped"; mv nanopause-metal2-8 nanopause-metal2-8-aborted 2>/dev/null
# 2. park anything that woke during setup, then migrate onto the Hyperdisk Extreme using a writable mount point
PARK_VERB=pause ./park_all_metal2b.sh 2>&1 | tail -1
kubectl -n kube-system exec nodeshell -- nsenter -t 1 -m -- sh -c '
set -e
dev=$(readlink -f /dev/disk/by-id/google-agents-tco-euw4-actors-hdx); echo "device $dev"
if grep -q " /var/lib/ate/actors " /proc/mounts; then echo "already mounted"; exit 0; fi
mkdir -p /var/lib/ate/actors-hdx && mount -o noatime "$dev" /var/lib/ate/actors-hdx
t=$(date +%s); cp -a /var/lib/ate/actors/. /var/lib/ate/actors-hdx/; echo "copied $(ls /var/lib/ate/actors-hdx | wc -l) dirs in $(( $(date +%s)-t )) s"
umount /var/lib/ate/actors-hdx
mv /var/lib/ate/actors /var/lib/ate/actors.boot && mkdir -m 0700 /var/lib/ate/actors && mount -o noatime "$dev" /var/lib/ate/actors
df -h /var/lib/ate/actors | tail -1; ls /var/lib/ate/actors | wc -l'
DS=atelet-v0-4-0-25-g66f8a888-dirty; kubectl --request-timeout=60s -n ate-system rollout restart daemonset/$DS >/dev/null; kubectl --request-timeout=300s -n ate-system rollout status daemonset/$DS --timeout=300s | tail -1; log "actor state directory on Hyperdisk Extreme"
PREFIX=$(cat pause-fleet-prefix.txt)
kubectl --request-timeout=30s -n kube-system delete pod plugsample --ignore-not-found --wait=true >/dev/null 2>&1; kubectl apply -f plugsample.json >/dev/null; log "sampler restarted"
TAG=metal2 ITER=8 FLEET_PREFIX="$PREFIX" CLS=microvm CLUSTER=agents-tco-euw4 ZONE=europe-west4-c KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east1 ACTOR_MEMORY_OVERRIDE=256Mi DASH_PORT=18180 PURGE_PORT=18198 SWAP_START_OVERRIDE=8 SWAP_MULT_OVERRIDE=1.35 FAIL_CRASHED_OVERRIDE=200 FAIL_REL_P90_OVERRIDE=0 FAIL_REFUSAL_PCT_OVERRIDE=2 FAIL_ERROR_PCT_OVERRIDE=2 ./nanopause.sh > /tmp/tco-runs/fill/nanopause-metal2-8.log 2>&1
log "pause run 8 (Hyperdisk Extreme) finished"
