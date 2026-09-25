# Phase 2 design: measuring real density on Substrate

Goal: stop asserting oversubscription and measure it — drive a
personal-agent-shaped workload at a fixed worker pool with light
auto-suspend/resume, and read off achieved density, activation latency, and
the effective per-agent worker occupancy that Phase 1's model only estimates.

## What Substrate gives us / what's missing

Substrate already resumes on demand: atenet's ext_proc sees a request for a
SUSPENDED actor, calls `ResumeActor`, parks the request (≤5s by default) and
forwards when the actor is up. **Nothing suspends**: `SuspendActor` is a
client-driven RPC and idle-GC is only a roadmap idea. A sandboxed actor also
*cannot* suspend itself — Substrate projects identity facts into the sandbox
but no control-plane credential (always-on-agent's one upstream ask).

So the minimum viable "auto suspend/resume" is a credentialed sidecar service
that decides when an actor is idle and calls `SuspendActor`. That is
`service/cmd/autosuspender`.

## Idle detection: signals over inference

The OpenClaw integration solved this inside its gateway (touch/begin/end per
turn, never suspend while a turn is in flight, idle window then suspend). We
generalize the same contract into a standalone HTTP API, because the general
case has the same shape: *whatever fronts the actors* (gateway, ingress,
message-bus consumer) already sees every turn and can emit signals.

Fallbacks, because signals get lost:
- baseline: the actor record's `update_time` (moves when a resume commits
  RUNNING) — traffic through the router does **not** move it, which is
  precisely why signals are needed;
- `--max-running` TTL: any RUNNING actor older than this is suspended
  regardless (the roadmap's "GC of idle actors").

Error handling copies idle-suspender.ts: a suspending set as reentrancy
guard, races (`FailedPrecondition`/`Aborted`/`NotFound`) treated as fine, and
failed suspends retried on the next sweep rather than hot-looped.

## The workload: Glutton dressed as OpenClaw

`agentsim` gives each simulated agent one Glutton actor
(template `benchmark-workloads/glutton`, HTTP mode) and generates a Poisson
process of activations from the Phase 1 workload profile (3 sessions × 8 min +
40 wakes × ~1 turn / day ⇒ ~2.4% duty cycle).

Per activation, mirroring the boomer GluttonUser cycle so it exercises what a
real agent does to a sandbox:

1. **first HTTP request** through the router → implicit resume; its latency is
   the user-visible activation cost (503/504 retried with backoff, counted as
   `refusals`);
2. **ReadRAM walk** of the working set (1 byte per 4KiB page) → measures the
   demand-paging cost of reaching the previous snapshot's memory;
3. per turn: **WriteRAM rotate** (dirty a moving window, so every suspend
   snapshots changed memory) + **WriteDisk** (rootfs delta), and a
   **ReadDisk (digest)** at the end of the activation;
4. activity signals to the autosuspender around all of it;
5. go quiet; the autosuspender suspends after the idle window.

Setup (create + `ResumeActor{boot:true}` + RAM fill to `--mem-target`) doubles
as node pre-warm; snapshots therefore carry a realistic working set from the
first suspend.

### Time compression

`--compress k` divides workload intervals by k (a day in 86400/k s).
Suspend/resume and the idle window run in real time, so the switching tax is
k× overweighted relative to reality: **compare measurements to the model run
at the compressed duty cycle**, then use the calculator to extrapolate to real
time. Running k=60 and k=20 and checking both against the model is the
validation that the model's `T_s`/`T_r`/`U` terms are right.

## What we measure and how it maps to the model

| Measurement | Source | Model term |
|---|---|---|
| first-request latency p50/p99 | agentsim CSV | user-visible activation cost (T_r + park) |
| RAM-walk latency | agentsim CSV | demand-paging tail of resume |
| suspend latency avg | autosuspender /metrics | `T_s` |
| workers assigned over time | autosuspender occupancy CSV | utilization `U`, effective occupancy `d_eff` |
| achieved density mean/p99/peak | report.py | `N` (size on p99/peak, not mean) |
| refusals | agentsim | park-budget/pool-size headroom check |

Peak-vs-average discipline comes from always-on-agent's `density.py`: the
average density is just 1/duty-cycle (workload idleness); the bankable number
is agents ÷ p99-peak busy workers. Their reference point: measured 8.3s
occupancy per wake (0.72% duty) ⇒ modeled 1,000 agents ⇒ P99 peak 21 workers ⇒
**47:1 bankable** vs 144:1 naive.

## Experiment matrix (suggested)

| Run | Pool | Agents | Compress | Purpose |
|---|---|---|---|---|
| A | 10 workers | 20 | 60 | smoke: everything suspends/resumes, no refusals |
| B | 10 | 50 | 60 | 5:1 nominal — measure latency under contention |
| C | 10 | 100→150 | 60 | push to first refusals: max density at this duty cycle |
| D | 10 | 50 | 20 | compression sensitivity: model validation |
| E | repeat B with `--mem-target 512Mi` | | | snapshot-size sensitivity (T_s/T_r growth) |

## Known limitations

- One actor per worker (`actorsPerAteom = 1`): density is time-multiplexing
  only. Multi-actor workers (epic #1266) would multiply it.
- Occupancy is sampled (default 5s), not integrated from control-plane logs;
  for finer intervals use always-on-agent's `density.py measure` against
  `ate-api-server` logs (stream them during the run — the ring buffer is
  dominated by ListActors polling).
- gVisor only out of the box; for the microVM column, install
  `hack/install-microvm-deps.sh` on a nested-virt node pool (Intel N2/N4/C3/C4,
  `--enable-nested-virtualization`) and point the WorkerPool's sandboxClass at
  `microvm` — the harness is identical.
