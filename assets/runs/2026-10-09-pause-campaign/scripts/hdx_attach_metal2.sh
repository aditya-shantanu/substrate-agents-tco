#!/usr/bin/env bash
# Attach a Hyperdisk Extreme to the microVM node and move /var/lib/ate/actors onto it (fleet must be parked).
set -u; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig; log() { echo "$(date +%H:%M:%S) == $*"; }
ZONE=europe-west4-c; VM=gke-agents-tco-euw4-default-pool-7b5efd89-m6np; DISK=agents-tco-euw4-actors-hdx; SIZE=${HDX_SIZE:-2000GB}; IOPS=${HDX_IOPS:-350000}
gcloud compute disks describe $DISK --zone $ZONE >/dev/null 2>&1 || gcloud compute disks create $DISK --zone $ZONE --type hyperdisk-extreme --size $SIZE --provisioned-iops $IOPS --quiet 2>&1 | tail -2
gcloud compute disks describe $DISK --zone $ZONE --format="value(sizeGb,type,provisionedIops,provisionedThroughput,users)" 2>/dev/null
gcloud compute instances describe $VM --zone $ZONE --format="value(disks[].deviceName)" | tr ';' '\n' | grep -q "^$DISK$" || gcloud compute instances attach-disk $VM --zone $ZONE --disk $DISK --device-name $DISK --quiet 2>&1 | tail -1
log "attached; preparing on the node"
kubectl -n kube-system exec nodeshell -- nsenter -t 1 -m -- sh -c '
set -e
dev=$(readlink -f /dev/disk/by-id/google-'"$DISK"')
echo "device $dev"
if ! blkid "$dev" >/dev/null 2>&1; then mkfs.ext4 -F -q -E lazy_itable_init=0,lazy_journal_init=0 "$dev"; fi
b=$(basename "$dev"); echo none > /sys/block/$b/queue/scheduler 2>/dev/null || true; echo 1024 > /sys/block/$b/queue/read_ahead_kb 2>/dev/null || true
cat /sys/block/$b/queue/scheduler /sys/block/$b/queue/max_hw_sectors_kb
if grep -q " /var/lib/ate/actors " /proc/mounts; then echo "something already mounted at /var/lib/ate/actors"; exit 1; fi
mkdir -p /mnt/actors-hdx && mount -o noatime "$dev" /mnt/actors-hdx
t=$(date +%s); cp -a /var/lib/ate/actors/. /mnt/actors-hdx/ ; echo "copied $(ls /mnt/actors-hdx | wc -l) dirs in $(( $(date +%s)-t )) s"
umount /mnt/actors-hdx
mv /var/lib/ate/actors /var/lib/ate/actors.boot && mkdir -m 0700 /var/lib/ate/actors && mount -o noatime "$dev" /var/lib/ate/actors
df -h /var/lib/ate/actors | tail -1'
DS=atelet-v0-4-0-25-g66f8a888-dirty; kubectl --request-timeout=60s -n ate-system rollout restart daemonset/$DS >/dev/null; kubectl --request-timeout=300s -n ate-system rollout status daemonset/$DS --timeout=300s | tail -1
log "actor state directory now on Hyperdisk Extreme"
