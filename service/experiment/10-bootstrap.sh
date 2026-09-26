#!/usr/bin/env bash
# Creates the GKE cluster, GCS snapshot bucket, and IAM bindings via
# substrate's setup-gcp tool (idempotent), then fetches kubectl credentials
# and disables node auto-upgrade on the worker-bearing pool (an auto-upgrade
# mid-run CRASHes every awake actor — see setup-gcp README).
# Takes ~10-15 min on first run.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
sync_substrate_env

cd "${SUBSTRATE_REPO}"
go run ./tools/setup-gcp bootstrap

gcloud container clusters get-credentials "${CLUSTER_NAME}" \
  --location "${CLUSTER_LOCATION}" --project "${PROJECT_ID}"

# On corp-managed gcloud installs the CLI's own tokens are bound to
# Certificate Based Access and the GKE endpoint rejects them ("the server has
# asked for the client to provide credentials"). ADC tokens are not CBA-bound.
# Setting this gcloud property makes every get-credentials (including the ones
# substrate's install scripts run) write an ADC-using kubeconfig entry.
if ! kubectl get ns >/dev/null 2>&1; then
  echo "kubectl auth failed; switching GKE auth to Application Default Credentials..."
  gcloud config set container/use_application_default_credentials true
  gcloud container clusters get-credentials "${CLUSTER_NAME}" \
    --location "${CLUSTER_LOCATION}" --project "${PROJECT_ID}"
  rm -f ~/.kube/gke_gcloud_auth_plugin_cache
fi

for pool in $(gcloud container node-pools list --cluster "${CLUSTER_NAME}" \
    --location "${CLUSTER_LOCATION}" --project "${PROJECT_ID}" --format="value(name)"); do
  gcloud container node-pools update "${pool}" \
    --cluster "${CLUSTER_NAME}" --location "${CLUSTER_LOCATION}" \
    --project "${PROJECT_ID}" --no-enable-autoupgrade --quiet
done

# Optional: size the substrate node pool (empty NODE_COUNT leaves it alone).
if [[ -n "${NODE_COUNT:-}" ]]; then
  gcloud container clusters resize "${CLUSTER_NAME}" \
    --node-pool substrate-node-pool --num-nodes "${NODE_COUNT}" \
    --location "${CLUSTER_LOCATION}" --project "${PROJECT_ID}" --quiet
fi

kubectl get nodes
