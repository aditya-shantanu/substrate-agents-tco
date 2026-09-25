# Phase 2: auto-suspend service + density experiment

Two small Go binaries that answer, empirically, "how much oversubscription do
I actually get?" on a Substrate cluster:

- **`cmd/autosuspender`** — the missing piece of the suspend/resume loop.
  Substrate resumes actors on demand (atenet router) but nothing suspends
  them; this service does, from activity signals (`POST /touch|/begin|/end`)
  plus a TTL fallback. It also samples worker occupancy (`GET /occupancy.csv`)
  and serves Prometheus-style `GET /metrics`. It is the workload-agnostic
  generalization of the gateway-embedded idle-suspender in the
  `always-on-agent` OpenClaw integration.
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

# 4. Watch it run; collect outputs when the job finishes
kubectl -n agent-sim logs -f job/agentsim | tee run.log
kubectl -n agent-sim port-forward svc/autosuspender 8080 &
curl -s localhost:8080/occupancy.csv > occupancy.csv

# 5. Report (worker-cost-hr = node $/hr ÷ workers per node; see the calculator)
python3 analysis/report.py --sim-log run.log --occupancy occupancy.csv \
    --compress 60 --worker-cost-hr 0.02
```

Feed the measured suspend/resume averages and achieved utilization back into
`tool/index.html` to reconcile theory with practice.

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
