#!/usr/bin/env bash
# Builds (ko) and installs the Substrate control plane onto the cluster:
# ateapi x2 + Postgres, atecontroller, atelet DaemonSet, atenet router/DNS,
# podcertcontroller, gvisor-default SandboxConfig. Idempotent.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
sync_substrate_env

cd "${SUBSTRATE_REPO}"
./hack/install-ate.sh --deploy-ate-system

# Guard against cross-version residue: if the freshly installed atelet
# DaemonSet has desired 0, the nodes still wear an older
# ate.dev/substrate-version label (install's label step does not always
# overwrite). Relabel to the DS's own selector and let it schedule —
# without atelet there is no credential-broker socket and workers can
# never report capacity ("no free workers available" with a FREE pool).
for ds in $(kubectl -n ate-system get ds -o name | grep atelet); do
  ver=$(kubectl -n ate-system get "$ds" -o jsonpath='{.spec.template.spec.nodeSelector.ate\.dev/substrate-version}')
  unlabeled=$(kubectl get nodes -l "ate.dev/substrate-version!=${ver}" -o name | wc -l | tr -d ' ')
  if [[ "${unlabeled}" != "0" ]]; then
    echo "WARNING: ${unlabeled} node(s) not labeled for $ds — relabeling all nodes to ${ver}"
    kubectl label nodes --all "ate.dev/substrate-version=${ver}" --overwrite
    kubectl -n ate-system rollout status "$ds" --timeout=180s
  fi
done

kubectl -n ate-system get pods
