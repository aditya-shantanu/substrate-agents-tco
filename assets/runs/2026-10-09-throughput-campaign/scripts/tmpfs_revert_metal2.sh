#!/usr/bin/env bash
# undo the tmpfs experiment: copy dirs that only exist in tmpfs back to disk, unmount, restart atelet (fleet must be parked)
set -u; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig; log() { echo "$(date +%H:%M:%S) == $*"; }
kubectl -n kube-system exec nodeshell -- nsenter -t 1 -m -- sh -c 'cd /var/lib/ate; n=0; for d in actors/*; do b=${d#actors/}; [ -e "actors.disk/$b" ] || { cp -a "$d" actors.disk/ && n=$((n+1)); }; done; echo "copied $n tmpfs-only dirs to disk"; umount /var/lib/ate/actors && echo unmounted; rm -rf /var/lib/ate/actors; mv /var/lib/ate/actors.disk /var/lib/ate/actors; df -h /var/lib/ate/actors | tail -1; ls /var/lib/ate/actors | wc -l'
DS=atelet-v0-4-0-25-g66f8a888-dirty; kubectl --request-timeout=60s -n ate-system rollout restart daemonset/$DS >/dev/null; kubectl --request-timeout=300s -n ate-system rollout status daemonset/$DS --timeout=300s | tail -1; log "tmpfs reverted"
