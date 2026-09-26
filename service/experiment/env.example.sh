# Experiment environment. Copy to env.sh, adjust, then run ./run.sh or the
# numbered scripts. Every value is a DEFAULT — anything already set in the
# environment (e.g. by the tco TUI or `FOO=bar ./run.sh`) wins.

export SUBSTRATE_REPO="${SUBSTRATE_REPO:-$HOME/repos/substrate}"

export PROJECT_ID="${PROJECT_ID:-gke-ai-eco-dev}"
export PROJECT_NUMBER="${PROJECT_NUMBER:-$(gcloud projects describe ${PROJECT_ID} --format='value(projectNumber)')}"
export GCE_REGION="${GCE_REGION:-us-central1}"
export CLUSTER_LOCATION="${CLUSTER_LOCATION:-us-central1-c}"
export NETWORK="${NETWORK:-default}"
export SUBNETWORK="${SUBNETWORK:-default}"

export CLUSTER_NAME="${CLUSTER_NAME:-agents-tco}"
# Substrate needs GKE >= 1.36 (beta APIs at creation — setup-gcp handles it)
# or >= 1.37. List valid versions:
#   gcloud container get-server-config --zone $CLUSTER_LOCATION --format=json
export CLUSTER_VERSION="${CLUSTER_VERSION:-1.36.4-gke.1495000}"
export NODE_POOL_NAME="${NODE_POOL_NAME:-gvisor-pool}"
export NODE_POOL_VERSION="${NODE_POOL_VERSION:-${CLUSTER_VERSION}}"
# Machine type for the substrate node pool (created by setup-gcp). microVM
# runs need a nested-virt-capable type: n1/n2/n4/c2/c3/c4 (Intel) or n4d.
export GVISOR_NODE_MACHINE_TYPE="${GVISOR_NODE_MACHINE_TYPE:-c3-standard-4}"
# Resize the pool to this many nodes after bootstrap (empty = leave as-is).
export NODE_COUNT="${NODE_COUNT:-}"

export KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-}"
export BUCKET_NAME="${BUCKET_NAME:-snapshot-${CLUSTER_NAME}-${PROJECT_ID}}"
export KO_DOCKER_REPO="${KO_DOCKER_REPO:-gcr.io/${PROJECT_ID}/ate-images}"
export KO_DEFAULTPLATFORMS="${KO_DEFAULTPLATFORMS:-linux/amd64}"
export PATH="$HOME/go/bin:$PATH"   # ko lives here if installed via `go install`

# --- experiment knobs ---
export SANDBOX_CLASS="${SANDBOX_CLASS:-gvisor}"   # gvisor | microvm (microvm needs nested-virt nodes + hack/install-microvm-deps.sh)
export WORKER_COUNT="${WORKER_COUNT:-10}"         # WorkerPool replicas to oversubscribe
export AGENTS="${AGENTS:-50}"                     # simulated personal agents
export COMPRESS="${COMPRESS:-6}"                  # time compression (6 = a day in 4h; overhead seconds do NOT compress)
export DURATION="${DURATION:-30m}"                # test length (load-test: upper bound)
export IDLE_TIMEOUT="${IDLE_TIMEOUT:-2s}"         # autosuspender idle window (compressed clock)
export PRICE_MODEL="${PRICE_MODEL:-cud3}"         # od | cud1 | cud3 — pricing for the report

# --- load-test mode (./run.sh --load-test): waves until failure ---
export LOAD_TEST="${LOAD_TEST:-false}"
export WAVE_START="${WAVE_START:-10}"             # agents active in wave 1
export WAVE_STEP="${WAVE_STEP:-10}"               # agents added per wave
export WAVE_INTERVAL="${WAVE_INTERVAL:-3m}"       # observation window per wave
