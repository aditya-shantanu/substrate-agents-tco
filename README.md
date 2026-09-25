# substrate-agents-tco

Cost of running "personal agents" (OpenClaw / Hermes shaped: mostly idle,
always addressable) on GKE with [Agent Substrate](https://github.com/agent-substrate/substrate),
in two phases:

1. **Theory** — an interactive calculator for $/agent/month under
   suspend/resume multiplexing, for gVisor and microVM sandboxes.
2. **Practice** — a light auto-suspend service + Glutton-based fleet
   simulator that measures the density you actually get.

## Phase 1: the calculator

Open **`tool/index.html`** in a browser (no build, works from `file://`).
Every assumption is an input with its provenance next to it; outputs update
live: cost per agent per month (gVisor vs microVM side by side), achieved
oversubscription, cost breakdown, and sensitivity curves for the two key
variables — duty cycle and suspend+resume time.

The math is specified in [`docs/MODEL.md`](docs/MODEL.md). Core identity
(same equation as the benchmarking QPS formula, inverted):

```
occupancy/agent  d' = duty_cycle × (burst + idle_timeout + suspend + resume) / burst
agents/worker    N  = utilization / (d' × peak_to_average)
$/agent/month       = worker_$/hr × 730 / N  + snapshot GiB × $/GiB-mo + GCS ops + fixed/fleet
```

Defaults are sourced, not invented:
- **Workload** (~2.4% duty cycle): [`docs/RESEARCH-workloads.md`](docs/RESEARCH-workloads.md)
  — OpenClaw/Hermes footprints, heartbeat cadence, assistant usage stats,
  E2B/Fly/Modal price anchors.
- **Prices** (us-central1, retrieved 2026-09-25): [`docs/RESEARCH-gcp-pricing.md`](docs/RESEARCH-gcp-pricing.md)
  — per-family VM rates (OD/spot/CUD), nested-virt support matrix (microVM ⇒
  GKE Standard + Intel N1/N2/N4/C2/C3/C4 or N4D, ~10% CPU tax), GCS/PD rates.
- **Substrate mechanics**: [`docs/RESEARCH-substrate.md`](docs/RESEARCH-substrate.md)
  — snapshot sizes, GCS ops per cycle, per-sandbox overheads, 1 actor/worker.
- **Measured reality** (real OpenClaw actor on Substrate, and the source of
  the calculator's *default* suspend/resume/snapshot numbers):
  [`docs/RESEARCH-always-on-agent.md`](docs/RESEARCH-always-on-agent.md)
  — suspend 2.2s / resume 3.4s P50, 55–61 MiB snapshots, 0.72% measured duty
  on a 20-min cron, P99-peak fleet sizing, 47:1 bankable density. A "small
  test actor" preset (suspend 0.4s / resume 0.24s) shows the optimistic end.

The calculator also shows the plumbing behind the headline: each machine's
$/month, the fleet-wide monthly bill (machines + snapshots + storage ops +
cluster fee), cost per agent = bill ÷ agents, and the suspend/resume churn
rate (fleet resumes/min, cycles per worker per hour vs each worker's physical
ceiling) needed to make the multiplexing work.

Ballpark with defaults (10k agents, measured OpenClaw switch times, 10s idle
wait, 3yr CUD, both scenarios on the same n2-standard-16 so the comparison is
apples-to-apples): **≈$1.0/agent/month on gVisor** and **≈$1.1/agent/month on
microVM** — the residual gap is the nested-virt CPU tax plus slower switch
estimates. Repointing gVisor at non-nested families it alone can use (E2,
spot C3D) drops it to ~$0.7 or below. Versus ~$7–15/month for a dedicated
always-on worker or VPS. LLM tokens are out of scope (and typically dominate
— see the OpenClaw heartbeat-cost issue).

## Phase 2: measure it

[`service/`](service/README.md) — two Go binaries (built against the sibling
`substrate` checkout):

- **autosuspender**: the missing suspend side of the loop (resume-on-request
  already exists in atenet). Activity-signal API + TTL fallback + worker
  occupancy sampling, and a **built-in live dashboard** (`kubectl port-forward
  svc/autosuspender 8080` → http://localhost:8080/) showing workers busy vs
  actors awake as the fleet churns, current density, and suspend rate/latency.
  A healthy run is a sawtooth under the dashed pool line; blue pinned at the
  line means the pool is saturated.
- **agentsim**: N simulated personal agents against Glutton actors through
  the router — implicit resume, RAM working-set walk (demand paging), memory
  churn and file I/O every turn, Poisson sessions/wakes, time compression.

Design and experiment matrix: [`docs/PHASE2-DESIGN.md`](docs/PHASE2-DESIGN.md).
**Runbook with a step-by-step "what you'll see"** (dashboard, log lines,
summary output, how to read each number): [`service/README.md`](service/README.md).
Analysis: `service/analysis/report.py` (reports density at mean/p99/peak —
size on the peak, the mean is just your workload's idleness).

## Repo map

```
tool/index.html            interactive cost calculator (Phase 1)
docs/MODEL.md              the math behind it
docs/RESEARCH-*.md         sourced inputs: pricing, workloads, substrate, measured
docs/PHASE2-DESIGN.md      density experiment design
service/                   Go: autosuspender + agentsim + manifests + report
```
