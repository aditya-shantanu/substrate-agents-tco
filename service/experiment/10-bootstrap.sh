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

for pool in $(gcloud container node-pools list --cluster "${CLUSTER_NAME}" \
    --location "${CLUSTER_LOCATION}" --project "${PROJECT_ID}" --format="value(name)"); do
  gcloud container node-pools update "${pool}" \
    --cluster "${CLUSTER_NAME}" --location "${CLUSTER_LOCATION}" \
    --project "${PROJECT_ID}" --no-enable-autoupgrade --quiet
done

kubectl get nodes
