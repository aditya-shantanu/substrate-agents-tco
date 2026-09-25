# Phase 2: auto-suspend service + density experiment

Two small Go binaries that answer, empirically, "how much oversubscription do
I actually get?" on a Substrate cluster:

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

Analysis: `analysis/report.py` joins the sim log with the occupancy samples
and prints activation latency, achieved density (mean / p99 / peak — size on
the peak), and a measured cost-per-agent line.

## Runbook

Prereqs: a Substrate cluster per `substrate/tools/setup-gcp` +
`hack/install-ate.sh --deploy-ate-system`, and env vars from
`hack/ate-dev-env.sh` (`PROJECT_ID`, `BUCKET_NAME`).

```bash
# 1. Deploy the glutton workload template (from the substrate repo)
cd ~/repos/substrate/benchmarking/workloads && ./deploy.sh

# 2. Give yourself a worker pool to oversubscribe (10 workers, with limits —
#    a worker without limits is never schedulable for a template with limits)
cd ~/repos/substrate/benchmarking && ./deploy_locust.sh --worker-count 10   # or your own WorkerPool

# 3. Build + push this repo's image, deploy the experiment
cd ~/repos/substrate-agents-tco/service
PROJECT_ID=$PROJECT_ID ./build.sh
envsubst < manifests/agent-sim.yaml | kubectl apply -f -

# 4. Watch it live
kubectl -n agent-sim port-forward svc/autosuspender 8080 &
open http://localhost:8080/          # live dashboard (see below)
kubectl -n agent-sim logs -f job/agentsim | tee run.log

# 5. Collect + report when the job finishes
#    (worker-cost-hr = node $/hr ÷ workers per node; see the calculator)
curl -s localhost:8080/occupancy.csv > occupancy.csv
python3 analysis/report.py --sim-log run.log --occupancy occupancy.csv \
    --compress 60 --worker-cost-hr 0.02
```

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
- `awake` climbing while `suspends` stalls and `errors` grows → suspends are
  failing (check bucket IAM — the classic wedge is ate-api-server missing
  `storage.objects.list`).

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

## Gotchas (learned from always-on-agent, they will bite here too)

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
