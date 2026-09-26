# Experiment environment. Copy to env.sh, adjust, then run the numbered
# scripts in order. env.sh is sourced by every script and also written to
# $SUBSTRATE_REPO/.ate-dev-env.sh (the substrate tooling reads it there).

export SUBSTRATE_REPO="${SUBSTRATE_REPO:-$HOME/repos/substrate}"

export PROJECT_ID=gke-ai-eco-dev
export PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format="value(projectNumber)")
export GCE_REGION=us-central1
export CLUSTER_LOCATION=us-central1-c
export NETWORK=default
export SUBNETWORK=default

export CLUSTER_NAME=agents-tco
# Substrate needs GKE >= 1.36 (with beta APIs enabled at creation — setup-gcp
# does this) or >= 1.37. Pick a valid one:
#   gcloud container get-server-config --zone $CLUSTER_LOCATION --format=json \
#     | jq -r '.validMasterVersions[]' | grep -E '1\.3[67]' | head
export CLUSTER_VERSION=1.36.4-gke.1495000
export NODE_POOL_NAME=gvisor-pool
export NODE_POOL_VERSION=${CLUSTER_VERSION}
export GVISOR_NODE_MACHINE_TYPE=c3-standard-4

export KUBECTL_CONTEXT=
export BUCKET_NAME=snapshot-${CLUSTER_NAME}-${PROJECT_ID}
export KO_DOCKER_REPO="gcr.io/${PROJECT_ID}/ate-images"
export KO_DEFAULTPLATFORMS=linux/amd64
export PATH="$HOME/go/bin:$PATH"   # ko lives here if installed via `go install`

# --- experiment knobs (40/50-series scripts) ---
export WORKER_COUNT=10          # WorkerPool replicas to oversubscribe
export AGENTS=50                # simulated personal agents
export COMPRESS=60              # a day of behavior in 24 wall-clock minutes
export DURATION=30m             # agentsim run length after setup
export IDLE_TIMEOUT=10s         # autosuspender idle window (compressed time)
export PRICE_MODEL=cud3            # od | cud1 | cud3 — pricing for the report
