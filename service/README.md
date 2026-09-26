# Phase 2: auto-suspend service + density experiment

Three Go binaries that answer, empirically, "how much oversubscription do I
actually get, and what does an agent cost per month?" on a Substrate cluster
(`cmd/tco` is the front door; the other two run in-cluster):

- **`cmd/autosuspender`** — the missing piece of the suspend/resume loop.
  Substrate resumes actors on demand (atenet router) but nothing suspends
  them; this service does, from activity signals (`POST /touch|/begin|/end`)
  plus a TTL fallback. It also samples worker occupancy and serves a **live
  dashboard at `GET /`** (plus `/state.json`, `/occupancy.csv`,
  Prometheus-style `/metrics`). It is the workload-agnostic generalization of
  the gateway-embedded idle-suspender in the `always-on-agent` OpenClaw
  integration.
- **`cmd/agentsim`** — simulates N personal agents (OpenClaw/Hermes-shaped:
  ~3 sessions + ~40 short wakes per day, idle otherwise) against **Glutton**
  actors through the router. Every activation resumes implicitly via HTTP,
  then behaves like a real workload: walks its RAM working set (demand-paging
  cost), dirties a rotating window of memory each turn, and writes/reads
  files. Time compression (`--compress`) plays a day in minutes.

Analysis lives in `analysis/`: `final_report.py` renders the results card
ending in **measured cost per agent per month**; `report.py` is the density
deep-dive (mean/p99/peak busy workers — size on the peak).

## Prerequisites (once)

- `gcloud` authenticated, **with ADC**: `gcloud auth application-default login`
  (on corp-managed machines the CLI's own tokens are CBA-bound and GKE rejects
  them; the bootstrap detects this and switches kubectl to ADC automatically)
- `kubectl`, `envsubst`, Go, and `ko`: `go install github.com/google/ko@latest`
- the substrate repo checked out next door (default `~/repos/substrate`)
- a GCP project you can create clusters in

There is **no config file** — the TUI pre-fills everything (project detected
from gcloud) and you edit on screen; script users override via environment
variables (defaults in `experiment/lib.sh`).

## The TUI — one command, whole show

```bash
cd service && go run ./cmd/tco
```

A full-screen terminal app (same visual language as substrate-gke's
installer): a config screen with prefilled choices — machine type dropdown
with live prices, node count, **gVisor vs microVM** (list filtered to
nested-virt machines), fleet size, time compression, test length, idle wait,
pricing model, and **baseline vs load-test** mode — plus a live feasibility
check (predicted busy workers vs pool) and a model cost preview before you
commit. Then a stage rail (skipping whatever is already set up), the live
test view (awake/asleep agents, worker occupancy bar + sparkline, suspend
avg, wake p50/p99, throughput, wedge warnings, wave table in load-test
mode), and the finale: the measured **cost per agent per month** card.

Load-test mode activates agents in waves until refusals/errors cross
thresholds and reports the last sustainable level — the pool's real maximum
density for this workload.

On startup the TUI **probes GCP for the named cluster** and shows what's
really there — exists/not-found, GKE version, node count, and every node
pool with its machine type, size and nested-virt capability — pre-filling
the machine/node knobs from the discovered pool. Editing project/zone/
cluster re-probes automatically; `r` re-checks on demand. Machine types
found on the cluster but missing from the built-in catalog are added to the
dropdown (priced when the family is known).

Keys: `↑/↓` field, `←/→` change, type into text fields, `r` re-check
cluster, `enter` run, `q`/`ctrl+c` quit. Artifacts land in
`experiment/results/<timestamp>/` (run.log, occupancy.csv, metrics.txt,
report.txt, ui.log).

## Headless: `experiment/run.sh`

The same show without the full-screen UI — staged banners, a live ticker,
and the cost card at the end. Setup stages self-detect and skip what's
already in place.

```bash
cd service/experiment
./run.sh                                  # everything, with defaults
./run.sh --duration 10m --agents 30       # quicker test on existing infra
./run.sh --compress 12 --workers 20       # other knobs: see lib.sh
./run.sh --load-test                      # waves until failure -> max density
./run.sh --force                          # redo setup stages even if present
AGENTS=100 IDLE_TIMEOUT=5s ./run.sh       # env overrides work too
```

## Step-by-step: the numbered scripts

For debugging or partial reruns, each stage stands alone:

```bash
cd service/experiment
./10-bootstrap.sh               # GKE cluster + GCS bucket + IAM (~10-15 min)
./20-install-substrate.sh       # Substrate control plane (ko build + install)
./30-deploy-workloads.sh        # glutton ActorTemplates + WorkerPool ($WORKER_COUNT)
./40-deploy-experiment.sh       # our images (ko) + autosuspender + agentsim Job

# watch it live
kubectl -n agent-sim port-forward svc/autosuspender 8080 &
open http://localhost:8080/
kubectl -n agent-sim logs -f job/agentsim

./50-collect.sh                 # run.log + occupancy.csv + report -> results/<ts>/
./90-teardown.sh                # delete the cluster (add --bucket for the bucket)
```

Experiment knobs (environment overrides, defaults in `lib.sh`):
`SANDBOX_CLASS` (gvisor|microvm), `GVISOR_NODE_MACHINE_TYPE`, `NODE_COUNT`,
`WORKER_COUNT`, `AGENTS`, `COMPRESS`, `DURATION`, `IDLE_TIMEOUT`,
`PRICE_MODEL`, and for load-test `LOAD_TEST`/`WAVE_START`/`WAVE_STEP`/
`WAVE_INTERVAL`. Re-run `40-deploy-experiment.sh` to launch a new Job with
changed knobs (it replaces the old one); `50-collect.sh` snapshots results
(density deep-dive + cost card) into a timestamped folder.

Feed the measured suspend/resume averages and achieved utilization back into
`tool/index.html` to reconcile theory with practice.

## What you'll see

**1. The live dashboard** (`http://localhost:8080/` after the port-forward,
refreshes every 2 s). Five tiles — agents, awake right now, busy workers /
pool, density now, suspends (rate + avg latency + errors) — and a chart of
**workers busy vs actors awake** with the pool size as a dashed line. A
healthy run looks like a **sawtooth**: orange/blue spikes as agents wake and
hold workers, falling back as the autosuspender puts them to sleep, with
almost all actors sitting in `suspended`. Two failure signatures to know:

- blue pinned at the dashed pool line → pool saturated; new wake-ups are
  parking (≤5 s) and then getting 503s (they show up as `refusals` in the
  agentsim summary);
- a red "⚠ N actor(s) wedged in SUSPENDING" line → the resume-during-suspend
  race (see `docs/FINDINGS.md`): the suspend never commits and the worker
  stays pinned. The autosuspender's **medic** (`--unwedge-after`, default 3m)
  heals these automatically — delete + recreate from the template — and
  counts interventions in `autosuspend_unwedged_total`. If wedges keep
  climbing faster than the medic clears them, also check bucket IAM
  (ate-api-server missing `storage.objects.list` causes the same signature).

The "Density over the window" table gives mean/p50/p99/peak busy workers and
the corresponding density — **the p99/peak line is the number to bank**.

**2. autosuspender logs** (`kubectl -n agent-sim logs deploy/autosuspender`) —
one JSON line per suspend:

```json
{"msg":"autosuspender up","listen":":8080","atespace":"agents-sim","idle_timeout":"10s",...}
{"msg":"suspended","actor":"sim-0007","elapsed_ms":2214,"forced":false}
```

`elapsed_ms` here is your measured suspend time (`T_s`) for the calculator.

**3. agentsim logs** — setup progress, then a warning only when something is
off, then the final summary + embedded CSV:

```
=== agentsim summary ===
agents=50 activations=1291 pings=3410 errors=2 refusals_503_504=7 compress=60
session  n=94    first-request ms p50=142 p90=4180 p99=5327 max=6180
wake     n=1197  first-request ms p50=3891 p90=4412 p99=5721 max=9822
post-resume RAM walk ms p50=310 p99=1450 (demand-paging cost)
```

Read it like this: `wake` p50 ≈ resume time + routing (the actor was asleep);
`session` p50 is bimodal — small if a heartbeat recently woke the actor,
resume-sized otherwise. The RAM-walk line is the demand-paging tail that a
plain ping never shows. `refusals` should be ~0; if not, the pool is too
small for the duty cycle you configured.

**4. The report** (`analysis/report.py`) turns both files into the density
verdict:

```
== density ==
effective per-agent worker occupancy (compressed): 6.10%
achieved density  (mean):   16.4 : 1   <- workload idleness, not bankable
achieved density  (p99):     7.1 : 1   <- size the pool on this
...
compute cost/agent-month = $1.02 (+ snapshot storage + ops; ...)
```

(Numbers above are illustrative — the point is the shape. At `--compress 60`
the switching tax is 60× overweighted, so real-world density is much higher;
extrapolate with the calculator.)

**5. Cross-checks without the dashboard**: `kubectl ate get actors -a
agents-sim` (states flipping RUNNING↔SUSPENDED), `kubectl ate get workers`,
or always-on-agent's `demo/watch-fleet.py` pointed at the `agents-sim`
atespace.

## Gotchas (learned from always-on-agent and our own runs)

- **Resume-during-suspend wedge** (found here, `docs/FINDINGS.md`): a request
  arriving while an actor is suspending can leave it in SUSPENDING forever,
  worker pinned; a few of these collapse a small pool. The medic works
  around it; the bug itself belongs upstream.
- **Keep-alives wedge checkpoints**: if you point your own client at actors,
  disable HTTP keep-alives — an idle connection held open into the sandbox
  makes the next gVisor checkpoint fragile (agentsim does this already).

- **Pre-warm every worker node**: the first resume on a cold node is far
  slower and can 504 permanently on some builds. `agentsim`'s setup phase
  (boot + RAM fill for every actor) doubles as a pre-warm, but let it finish
  before treating latencies as data.
- **Router budgets**: atenet parks a resume-triggering request for max 5s
  (`--parked-request-budget`) inside a 10s route timeout (`--route-timeout`).
  Real agent resumes (3.4s P50 for OpenClaw) leave little headroom — raise
  both on the atenet deployment for large-snapshot experiments. agentsim
  retries 503/504 with backoff and reports them separately (`refusals`).
- **Pool full = 503, not a queue** (past the park budget). One actor per
  worker is the concurrency unit until multi-actor workers land.
- **Bucket IAM**: both `atelet` *and* `ate-api-server` need
  `roles/storage.objectAdmin` on the snapshot bucket; missing the second
  makes the *second* suspend fail and wedges the actor in SUSPENDING.
- **Suspend delay = idle-timeout + up to one poll interval.** Keep
  `--poll` well under `--idle-timeout`.
- **Compressed time**: `--compress k` divides workload intervals by k but
  suspend/resume/idle-timeout run in real time, so the switching tax is k×
  overweighted vs reality. Compare measurements against the model run at the
  *compressed* duty cycle; extrapolate to real duty with the calculator.

## Local build check

```bash
cd service && go build ./... && go vet ./...
```
