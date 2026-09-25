#!/usr/bin/env bash
# Deletes the experiment cluster and (optionally) the snapshot bucket.
# The cluster is the cost driver; the bucket is pennies but --bucket removes
# it too.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

gcloud container clusters delete "${CLUSTER_NAME}" \
  --location "${CLUSTER_LOCATION}" --project "${PROJECT_ID}" --quiet

if [[ "${1:-}" == "--bucket" ]]; then
  gcloud storage rm -r "gs://${BUCKET_NAME}" --project "${PROJECT_ID}"
fi
