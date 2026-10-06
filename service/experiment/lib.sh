# Shared prelude for the experiment scripts. Not executable on its own.
#
# Configuration model (mirrors substrate-gke): there is no env file. The tco
# TUI pre-fills every value on its config screen and passes the result via
# process environment; script users override the same way
# (`AGENTS=100 ./run.sh`). Everything below is a fallback default.
set -euo pipefail

EXP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_DIR="$(dirname "${EXP_DIR}")"

export SUBSTRATE_REPO="${SUBSTRATE_REPO:-$HOME/repos/substrate}"

export PROJECT_ID="${PROJECT_ID:-$(gcloud config get-value project 2>/dev/null)}"
export PROJECT_NUMBER="${PROJECT_NUMBER:-$(gcloud projects describe ${PROJECT_ID} --format='value(projectNumber)')}"
export CLUSTER_LOCATION="${CLUSTER_LOCATION:-us-central1-c}"
export GCE_REGION="${GCE_REGION:-${CLUSTER_LOCATION%-*}}"
export NETWORK="${NETWORK:-default}"
export SUBNETWORK="${SUBNETWORK:-default}"

export CLUSTER_NAME="${CLUSTER_NAME:-agents-tco}"
# Substrate needs GKE >= 1.36 (beta APIs at creation — setup-gcp handles it)
# or >= 1.37. List valid versions:
#   gcloud container get-server-config --zone $CLUSTER_LOCATION --format=json
export CLUSTER_VERSION="${CLUSTER_VERSION:-1.36.4-gke.1495000}"
export NODE_POOL_NAME="${NODE_POOL_NAME:-gvisor-pool}"
export NODE_POOL_VERSION="${NODE_POOL_VERSION:-${CLUSTER_VERSION}}"
# Machine type for the substrate node pool. microVM needs a nested-virt
# capable type: n1/n2/n4/c2/c3/c4 (Intel) or n4d.
export GVISOR_NODE_MACHINE_TYPE="${GVISOR_NODE_MACHINE_TYPE:-c3-standard-4}"
# Resize the pool to this many nodes after bootstrap (empty = leave as-is).
export NODE_COUNT="${NODE_COUNT:-}"

export KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-}"
export BUCKET_NAME="${BUCKET_NAME:-snapshot-${CLUSTER_NAME}-${PROJECT_ID}}"
export KO_DOCKER_REPO="${KO_DOCKER_REPO:-gcr.io/${PROJECT_ID}/ate-images}"
export KO_DEFAULTPLATFORMS="${KO_DEFAULTPLATFORMS:-linux/amd64}"
export PATH="$HOME/go/bin:$PATH"   # ko lives here if installed via `go install`

# --- experiment knobs ---
export SANDBOX_CLASS="${SANDBOX_CLASS:-gvisor}"   # gvisor | microvm
export WORKER_COUNT="${WORKER_COUNT:-10}"
export AGENTS="${AGENTS:-120}"
export COMPRESS="${COMPRESS:-6}"                  # time compression; switch overhead does NOT compress
export DURATION="${DURATION:-30m}"
export IDLE_TIMEOUT="${IDLE_TIMEOUT:-2s}"
# Medic threshold: an actor in SUSPENDING longer than this is treated as
# wedged (#1914) and recreated. A delete that lands mid-checkpoint hangs in
# DELETING with its worker pinned, so keep this well above the slowest real
# checkpoint (≈3 s at 128Mi agents; ≈90 s+ at 512Mi on a boot-disk-swap node).
export UNWEDGE_AFTER="${UNWEDGE_AFTER:-3m}"
export MAX_RUNNING="${MAX_RUNNING:-10m}"         # autosuspender: force-suspend actors running longer than this
# Resident-parking design: SETUP_SUSPEND=false keeps every agent on its
# worker (WORKER_COUNT >= AGENTS, IDLE_TIMEOUT/MAX_RUNNING longer than the
# window). Density then comes from RAM + swap, and a wake is a page-in.
export SETUP_SUSPEND="${SETUP_SUSPEND:-true}"
export PRICE_MODEL="${PRICE_MODEL:-cud3}"         # od | cud1 | cud3
export PEAK_MODEL="${PEAK_MODEL:-mult}"          # mult | herd — peak lens for the report
export PEAK_VALUE="${PEAK_VALUE:-2}"             # multiplier (mult) or fraction 0-1 (herd)
# Per-agent memory footprint (glutton WriteRAM). 128Mi is a small tool
# agent; an OpenClaw gateway sits at 512Mi requested / 1.5-2Gi limit, so
# use MEM_TARGET=1Gi to put memory (not worker slots) on the critical path.
export MEM_TARGET="${MEM_TARGET:-128Mi}"         # resident working set filled at boot
export MEM_CHURN="${MEM_CHURN:-16Mi}"            # dirtied every turn (snapshots change like a live app)
# --- workload ---
# WORKLOAD selects what a simulated agent does when it is awake:
#   personal        agentsim's personal-agent turns: SESSIONS_PER_DAY chats
#                   of 8 min + WAKES_PER_DAY 15 s check-ins, RAM walk/churn
#                   and a file write per turn (MEM_TARGET/MEM_CHURN apply).
#   coding-session  the agent-session script from substrate's benchmarking
#                   suite (agent-substrate/substrate#1934): every session is
#                   one 20-step coding task (clone, build, fix a test,
#                   refactor, package) with an LLM think gap before each
#                   step. SESSIONS_PER_DAY is then tasks per agent-day.
#   <path>.yaml     a script of your own in the same format (validated
#                   locally, shipped to the Job as a ConfigMap).
# THINK_SCALE multiplies every think gap (the script's 2–8 s are optimistic
# for a real model; ×4 ≈ 8–32 s). Think gaps are not time-compressed.
# SCRIPT_SUSPEND: driver = agentsim suspends the actor right after each
# step (the upstream benchmark's behavior; think gaps are free); idle = the
# autosuspender's IDLE_TIMEOUT decides (production-like; gaps shorter than
# the wait keep the worker).
export WORKLOAD="${WORKLOAD:-coding-session}"   # the default workload since 2026-10-05
# remember what the caller set before defaults apply (workload branches below pick their own)
USER_THINK_SCALE="${THINK_SCALE-}"
USER_SCRIPT_LOOP="${SCRIPT_LOOP-}"
USER_RAMP_SECONDS="${RAMP_SECONDS-}"
export RAMP_SECONDS="${RAMP_SECONDS:-60}"   # agent start offsets are spread over this many seconds
export THINK_SCALE="${THINK_SCALE:-4}"
export SCRIPT_SUSPEND="${SCRIPT_SUSPEND:-driver}"
export SCRIPT_LOOP="${SCRIPT_LOOP:-false}"
# WORKLOAD=ping: the one-ping GluttonUser loop of substrate's benchmarking
# suite (the Prow 200K run): PING_ACTORS_PER_USER actors per virtual user,
# served one at a time — wake by ping, PING_LIVE awake, suspend, PING_WAIT —
# no memory fill, no other work. Overcommit is N:1 by construction.
# LIFECYCLE_MODE: how agentsim parks an actor it has finished with (script
# driver mode, one-ping, the first park after setup): suspend = durable
# checkpoint in the bucket; pause = node-local checkpoint (PauseActor), the
# actor resumes on the same node. Pause skips the bucket round trip.
export LIFECYCLE_MODE="${LIFECYCLE_MODE:-suspend}"
export PING_ACTORS_PER_USER="${PING_ACTORS_PER_USER:-0}"
export PING_WAIT="${PING_WAIT:-10s}"
export PING_LIVE="${PING_LIVE:-0s}"
# PING_INDEPENDENT=true drops the user loop: every agent wakes on its own
# Poisson schedule (mean gap PING_WAIT), so the pool can saturate; with
# LOAD_TEST=true the waves find the host's real ceiling. SETUP_CONCURRENCY
# bounds concurrent boots during setup (raise for fleets of thousands).
export PING_INDEPENDENT="${PING_INDEPENDENT:-false}"
export SETUP_CONCURRENCY="${SETUP_CONCURRENCY:-8}"
# COLD_START_ONLY=true: agentsim creates the fleet, times every actor's first
# life (CreateActor → golden-snapshot restore → first answered ping), prints
# P50/P90/P99 + per-actor CSV, and exits without running a workload.
export COLD_START_ONLY="${COLD_START_ONLY:-false}"
# ACTOR_PREFIX names this run's actors (<prefix>-NNNN). Give each run a fresh
# one (e.g. cold-$(date +%s)) when a previous fleet may still be deleting:
# CreateActor on an existing name returns AlreadyExists and the boot — and
# any cold-start measurement — is silently skipped for that actor.
export ACTOR_PREFIX="${ACTOR_PREFIX:-sim}"
if [[ "${WORKLOAD}" == "personal" ]]; then
  export SESSIONS_PER_DAY="${SESSIONS_PER_DAY:-3}"
  export WAKES_PER_DAY="${WAKES_PER_DAY:-40}"
  export SCRIPT_ARG=""
elif [[ "${WORKLOAD}" == "ping" ]]; then
  export SESSIONS_PER_DAY="${SESSIONS_PER_DAY:-0}"   # unused: the loop is continuous
  export WAKES_PER_DAY="${WAKES_PER_DAY:-0}"
  export SCRIPT_ARG=""
  [[ "${PING_ACTORS_PER_USER}" -gt 0 || "${PING_INDEPENDENT}" == "true" ]] || export PING_ACTORS_PER_USER=20
elif [[ "${WORKLOAD}" == "personal-assistant" ]]; then
  # substrate#2230: one lap = one day of an always-on assistant (61 steps,
  # 24 h of think gaps, 440 s of resident dwell, 640→960 MiB resident).
  # Played back-to-back at THINK_SCALE 0.02 (a day in ~30 min); tasks/day = 1.
  export SESSIONS_PER_DAY="${SESSIONS_PER_DAY:-1}"
  export WAKES_PER_DAY="${WAKES_PER_DAY:-0}"
  export THINK_SCALE="${USER_THINK_SCALE:-0.02}"
  export SCRIPT_LOOP="${USER_SCRIPT_LOOP:-true}"
  # Stagger each agent's start of day over most of a lap (24 h × 0.02 =
  # 1,728 s): assistants' days are not aligned, and aligned ones herd every
  # agent into the same 07:30 message cluster at once.
  export RAMP_SECONDS="${USER_RAMP_SECONDS:-1200}"
  export SCRIPT_ARG="personal-assistant"
else
  export SESSIONS_PER_DAY="${SESSIONS_PER_DAY:-8}"   # tasks per agent-day
  export WAKES_PER_DAY="${WAKES_PER_DAY:-0}"
  if [[ "${WORKLOAD}" == */* || "${WORKLOAD}" == *.yaml || "${WORKLOAD}" == *.yml ]]; then
    [[ -f "${WORKLOAD}" ]] || { echo "WORKLOAD=${WORKLOAD}: no such script file" >&2; exit 1; }
    WORKLOAD="$(cd "$(dirname "${WORKLOAD}")" && pwd)/$(basename "${WORKLOAD}")"  # stages cd elsewhere
    export SCRIPT_ARG="/etc/agentscript/script.yaml"   # ConfigMap mount (40-deploy)
  else
    export SCRIPT_ARG="${WORKLOAD}"                     # built-in name
  fi
fi
# Per-ACTOR memory limit (ActorTemplate spec.resources.limits.memory, a
# cgroup on the sandbox — separate from the worker pod's limit). Must exceed
# MEM_TARGET + churn + ~100Mi sentry overhead, or the sandbox is OOM-killed
# (silently, mid-checkpoint → stuck SUSPENDING). With node swap the sandbox's
# shmem pages can page out and mask an undersized limit — badly. The
# coding-session script declares a 1Gi floor (agentsim refuses a smaller
# template), so script workloads default to 1Gi.
actor_mem_default() { case "${WORKLOAD}" in personal|ping) echo 256Mi ;; personal-assistant) echo 1536Mi ;; *) echo 1Gi ;; esac; }
export ACTOR_MEMORY="${ACTOR_MEMORY:-$(actor_mem_default)}"

# --- worker pod shape (used by the Burstable patch; requests<limits) ---
# Under kubelet LimitedSwap a pod may swap at most request/nodeRAM × swap,
# so the memory REQUEST is also the per-worker swap allowance.
export WORKER_REQ_CPU="${WORKER_REQ_CPU:-200m}"
export WORKER_REQ_MEM="${WORKER_REQ_MEM:-512Mi}"
export WORKER_LIM_CPU="${WORKER_LIM_CPU:-1}"
export WORKER_LIM_MEM="${WORKER_LIM_MEM:-1Gi}"
worker_resources_patch() {
  # The patch replaces the whole resources block: microVM workers must keep
  # their ate.dev/kvm device request or they never get /dev/kvm.
  local kvm=""
  [[ "${SANDBOX_CLASS:-gvisor}" == "microvm" ]] && kvm=',"ate.dev/kvm":"1"'
  printf '{"spec":{"template":{"resources":{"requests":{"cpu":"%s","memory":"%s"%s},"limits":{"cpu":"%s","memory":"%s"%s}}}}}' \
    "${WORKER_REQ_CPU}" "${WORKER_REQ_MEM}" "${kvm}" "${WORKER_LIM_CPU}" "${WORKER_LIM_MEM}" "${kvm}"
}

# --- node swap: RESEARCH-ONLY, not exposed in the TUI and not part of the
# cost model. The 2026-09-28/29 A/B (research/research.md, "Swap on GKE")
# found swap parks more idle agents but yields no sustainable density.
# SWAP_GIB: "" = leave the pool as-is (default); a number = enable
# boot-disk-backed swap of that many GiB on the substrate node pool (NODE
# POOL IS RECREATED). Swap applies to Burstable pods only, so
# WORKER_BURSTABLE=true (the default when swap is on) patches the benchmark
# WorkerPool with requests<limits.
export SWAP_GIB="${SWAP_GIB:-}"
export WORKER_BURSTABLE="${WORKER_BURSTABLE:-$([[ -n "${SWAP_GIB}" ]] && echo true || echo false)}"

# --- load-test mode (./run.sh --load-test) ---
export LOAD_TEST="${LOAD_TEST:-false}"
export WAVE_START="${WAVE_START:-10}"
export WAVE_STEP="${WAVE_STEP:-10}"
export WAVE_INTERVAL="${WAVE_INTERVAL:-3m}"

if [[ ! -d "${SUBSTRATE_REPO}" ]]; then
  echo "SUBSTRATE_REPO=${SUBSTRATE_REPO} does not exist." >&2
  exit 1
fi

# The substrate hack/ scripts read env from .ate-dev-env.sh at their repo
# root; write the RESOLVED values there so they see the same world we do.
sync_substrate_env() {
  {
    echo "# generated by substrate-agents-tco/service/experiment — do not edit"
    for v in PROJECT_ID PROJECT_NUMBER GCE_REGION CLUSTER_LOCATION NETWORK \
             SUBNETWORK CLUSTER_NAME CLUSTER_VERSION NODE_POOL_NAME \
             NODE_POOL_VERSION GVISOR_NODE_MACHINE_TYPE KUBECTL_CONTEXT \
             BUCKET_NAME KO_DOCKER_REPO KO_DEFAULTPLATFORMS; do
      printf 'export %s=%q\n' "$v" "${!v}"
    done
    printf 'export PATH="$HOME/go/bin:$PATH"\n'
  } > "${SUBSTRATE_REPO}/.ate-dev-env.sh"
}
