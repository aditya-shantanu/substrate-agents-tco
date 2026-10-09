#!/usr/bin/env bash
# delete node actor dirs that belong to no live actor (list computed on the node from the live-UID listing)
set -u; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
kubectl -n kube-system exec nodeshell -- sh -c 'cd /host/var/lib/ate/actors && n=$(wc -l < /host/tmp/orphans.txt); echo "deleting $n orphan dirs"; xargs -a /host/tmp/orphans.txt -n 100 rm -rf; echo "left: $(ls | wc -l) dirs, $(du -sh . | cut -f1)"'
