#!/usr/bin/env bash
# move /var/lib/ate/actors onto tmpfs on the node (fleet must be parked), then restart the atelet pod so it sees the mount
set -u; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig; log() { echo "$(date +%H:%M:%S) == $*"; }
kubectl -n kube-system exec nodeshell -- sh -c 'set -e; cd /host/var/lib/ate; if grep -q " /var/lib/ate/actors tmpfs" /host/proc/1/mounts; then echo "already tmpfs"; exit 0; fi; mv actors actors.disk; mkdir -m 0700 actors; nsenter -t 1 -m -- mount -t tmpfs -o size=400G,mode=0700 tmpfs /var/lib/ate/actors; cp -a actors.disk/. actors/; echo "copied $(ls actors | wc -l) dirs: $(du -sh actors | cut -f1)"; grep " /var/lib/ate/actors " /host/proc/1/mounts'
log "tmpfs mounted; restarting atelet pod"
DS=atelet-v0-4-0-25-g66f8a888-dirty; kubectl --request-timeout=60s -n ate-system rollout restart daemonset/$DS >/dev/null; kubectl --request-timeout=300s -n ate-system rollout status daemonset/$DS --timeout=300s | tail -1
AT=$(kubectl --request-timeout=30s -n ate-system get pods --no-headers -o custom-columns=:metadata.name | grep ^atelet); kubectl -n ate-system exec $AT -c atelet -- sh -c 'grep " /var/lib/ate/actors " /proc/mounts || echo "ATELET DOES NOT SEE TMPFS"' 2>/dev/null | cut -c1-120
P=$(kubectl --request-timeout=30s -n benchmark-workloads get pods --no-headers -o custom-columns=:metadata.name | grep benchmark-ateom | head -1); kubectl -n benchmark-workloads exec $P -- sh -c 'grep " /var/lib/ate/actors " /proc/mounts || echo "WORKER DOES NOT SEE TMPFS"' 2>/dev/null | cut -c1-120
log "tmpfs migration done"
