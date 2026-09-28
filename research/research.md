# How to increase hardware oversubscription (and why it's the cost lever)

Research date: 2026-09-27. Sources: this repo's model and measured runs,
a source-level sweep of the substrate tree (@ e3a041bc), and industry/
academic practice for sandbox-fleet density. Definition used throughout
(MODEL.md §3a): **oversubscription = resources promised to scheduled agents
÷ physical machine resources**, CPU and memory reported separately.

## 1. Is oversubscription the only way to cut cost per agent?

Almost. The bill is:

```
$/agent/mo = machine_$ / agents_per_machine  +  snapshot  +  GCS ops  +  fixed/fleet
             └─────────── ~90% ───────────┘     ~0.2%        ~5%         ~6%
```

So there are exactly two levers on the dominant term: **pay less per unit
of hardware** (spot ≈40–76% off, 3y CUD 55% off, cheaper families — all
already in the calculator; bounded, one-time wins) and **raise
agents-per-machine** — which is oversubscription, unbounded until physics
(duty cycle) stops you. Everything below is about the second lever. One
non-obvious corollary: **reliability is capacity** — a wedged worker
(substrate#1914) is negative oversubscription, and our early runs lost 70%
of a pool to it.

Today's position (defaults, measured switch times): 301 agents on an
n2-standard-16 → **CPU 9.4× / memory 4.7×**, $0.97/agent. Measured on the
live cluster: 14.8:1 mean density, $1.21. Theoretical ceiling at this
workload's duty cycle (2.36% + overhead → d′≈3.05%) with U=0.7:
**N = U/d′ ≈ 23 per worker ⇒ ~620 agents/machine ⇒ CPU ~19×** — everything
below is about closing the gap to (and then raising) that ceiling.

## 2. Knobs we already expose, quantified (our model, gVisor, n2-standard-16)

| Lever | Agents/machine | Δ | CPU / mem oversub |
|---|---|---|---|
| Baseline (defaults) | 301 | — | 9.4× / 4.7× |
| Small-actor switch times (0.4s/0.25s) | 327 | +9% | 10.2× / 5.1× |
| Idle wait 10s → 2s | 356 | +18% | 11.1× / 5.6× |
| Utilization 0.70 → 0.85 | 366 | +21% | 11.4× / 5.7× |
| Batch heartbeats 40 → 8/day | 477 | +58% | 14.9× / 7.5× |
| Autoscale pool with the day (peak ×1) | 602 | +100% | 18.8× / 9.4× |
| **Worker right-sizing 0.5 → 0.25 vCPU** | **602** | **+100%** | **9.4× / 9.4×** |
| Safe stack of the above | ~2 000 | +560% | ~31× / ~31× |

Notes:
- **Worker right-sizing is the free lunch nobody sees.** Our machines are
  CPU-bound at 27 slots (0.5 vCPU workers) while memory sits half idle
  (4.7× vs 9.4×). Matching worker shape to machine shape (0.25 vCPU/1 GiB
  on a 1:4 machine) doubles slots. Caveat: active agents burst above
  0.25 vCPU (restore ≈1.22 vCPU per the benchmark sheet) — cgroup limits
  make this throttling, not failure, but it needs a Phase 2 run to verify
  wake latency holds.
- **De-correlating wake-ups** is the same +100% seen through the herd lens:
  aligned crons force provisioning for the burst. Splay schedules
  (Jenkins-style deterministic hash offsets; Google SRE's "?" crontab) and
  the autoscaled WorkerPool HPA (`demos/autoscaled-workerpool/`, headroom =
  AverageValue 0.7) both attack peak-vs-average.
- **Batching heartbeats** is a workload-side fix (OpenClaw's
  `isolatedSession`/active-hours reduce wake count) — fewer wakes means
  less switch overhead *and* less churn.

## 3. What Substrate already does for density (on by default @ e3a041bc)

The platform already ships most of the "make suspend/resume cheap"
machinery — worth knowing before inventing more:

- **Node-shared OCI layer cache + overlayfs** (`internal/imagecache/`):
  unpack once per node; oci_unpack ~15–20s → milliseconds warm; co-located
  actors share page cache for the image.
- **Sparse zstd snapshots** (`ategcs/sparsezstd.go`): T_s/T_r scale with
  resident set, not guest size (2 GiB guest → ~150 MiB moved).
- **Parallel part upload/download** (32 MiB × 4, compose; download conc 8).
- **microVM userfaultfd OnDemand restore** (`ateom-microvm/restore.go`):
  resume touches only faulted pages; the **next checkpoint is implicitly a
  delta** (only faulted/dirtied pages) merged over the base
  (`internal/ch/merge.go`).
- **gVisor lazy restore** (`runsc restore -background`): demand paging from
  the staged image. ⚠ This branch still passes `-direct`, which serializes
  concurrent restores (~130 MB/s/node); main dropped it for **88 concurrent
  restores 190s → 15s p50** — check the deploy branch.
- **Sandbox asset prewarm** per node (removes the ~20s cold gVisor extract).
- **PauseActor node-local checkpoints**: no GCS round trip (at the price of
  node pinning; no eviction/GC yet, TODO #664).

## 4. Biggest platform gaps (in-tree or upstream, ranked by expected effect)

1. **Multi-actor workers (#1266) — the headline lever.** Control plane is
   ready (assignments table, capacity counters, 28k claims/s storage);
   the blocker is one constant (`actorsPerAteom = 1`) plus ateom/atunnel
   single-actor assumptions. Removes slot quantization entirely: packing
   becomes CPU/memory-governed. At scale docs note only ~3k of 100k
   one-actor workers are ever busy at 50k cycles/min.
2. **Node-local golden/snapshot cache (#690)** — `filecache/` LRU is merged
   but **unwired**: today the golden is re-downloaded from GCS per actor
   per resume. Wiring it makes DATA-scope + golden-resume the cheap path
   (tiny durable-data snapshots; shared, cached base) — the single biggest
   T_s/T_r structural win for fleets of same-template agents.
3. **Fix the wedge (#1914) + add a per-node restore semaphore** (readiness
   doc recommends ≈ worker count): reliability and tail control convert
   directly into usable utilization U.
4. **Request parking headroom**: `--parked-request-budget` 5s → 10–15s and
   a bigger lot turns cold-resume 503s into short waits — cheap U points.
5. **Snapshot/actor-dir GC leaks** (#664, #1757, orphaned snapshots): slow
   density rot on long-lived nodes.

## 5. Industry techniques worth importing (with evidence)

Ranked for this platform (gVisor + CHV on GKE, snapshots in GCS):

1. **Lazy, provenance-shared restore** (Lambda SnapStart model): restore
   O(working set) from 512 KiB chunks with working-set prefetch, and clones
   from one base snapshot **share clean pages** — Firecracker gets ~90%
   data-movement reduction without KSM's CPU cost. Substrate already has
   uffd OnDemand (µVM) and lazy gVisor restore; the missing pieces are the
   node-local chunk cache (#690) and MAP_PRIVATE-style page sharing across
   actors from the same golden. (nsdi20 Firecracker; brooker.co.za 2025.)
2. **Skip/delta checkpoints** (Crab, arXiv 2604.28138: >75% of agent turns
   produce no state worth checkpointing, −87% checkpoint traffic; DeltaBox,
   arXiv 2605.22781: 14 ms delta checkpoints). CHV already does implicit
   deltas post-restore; gVisor deltas are roadmap (`docs/roadmap.md:51-53`).
   CRIU soft-dirty/pre-dump is the production primitive.
3. **Node swap (K8s GA since 1.34) + zswap**: cold agent pages compress
   ~3:1; Burstable pods get proportional swap — the cheapest *memory*
   oversubscription raise with zero sandbox-stack changes.
4. **requests = idle footprint, limits = peak** (Burstable everywhere):
   Modal defaults to 0.125 core/128 MiB requests with 16-core bursts — a
   deliberate >100× request-level CPU oversell for idle-heavy fleets. Our
   worker right-sizing row is the same idea in slot form.
5. **Free-page reporting + DAMON reclaim** for resident microVMs (32–46%
   RSS reduction at ~2% overhead); gVisor already returns madvised memory
   when idle, which is why it overcommits well.
6. **Hash-splayed schedules + admission pacing** for wake de-correlation
   (Google SRE "?"-cron; PayPal jitter) — herd% is a *choice*, not a fact.

Anti-pattern to avoid: Fly.io-style suspend that keeps reserving host
memory (≤2 GB machines only, not durable) — suspended capacity must leave
the host, which Substrate's GCS snapshots already do. KSM is dominated by
provenance sharing for identical runtimes.

## 6. What would move our own numbers next (validation plan, Phase 2)

| Experiment | Lever validated | Expect |
|---|---|---|
| `WORKER_COUNT=54` with 0.25-vCPU workers, same 2 nodes | right-sizing | 2× agents, wake p99 unchanged? |
| `IDLE_TIMEOUT=2s` vs 10s A/B | idle wait | +15–18% density, watch mid-session suspends |
| load-test mode to failure at both worker shapes | true ceiling | knee vs model's N=U/d′ |
| DATA-scope glutton template + golden resume | snapshot scope | T_s ↓ (durable-only upload); T_r after #690 |
| PauseActor lifecycle run | local checkpoints | T_s/T_r without GCS; node-pinning cost |
| cron-aligned vs splayed wake schedules | herd | peak busy workers ratio |

## 7. Bottom line

- Yes: with hardware prices fixed, oversubscription **is** the cost lever,
  and it's governed by one fraction: `U / peak-adjusted d′`, times how many
  right-shaped slots (or, post-#1266, how much fungible CPU/RAM) a machine
  yields.
- Without touching the platform: right-size workers + autoscale/splay +
  shorter idle wait + batched heartbeats ≈ **6.7× more agents on the same
  machines (~31× CPU and memory oversubscription, ~$0.24/agent)** — all
  testable with the Phase 2 lab today.
- The two platform investments with step-change upside: **multi-actor
  workers** (removes slot quantization) and **node-cached, lazily-paged,
  provenance-shared golden restores** (makes wake-ups nearly free), with
  **skip/delta checkpoints** close behind. Everything else is tail-trimming.

## Addendum (2026-09-28): swap on GKE — it's first-party now

GKE ships native node swap (KEP-2400 semantics) for Standard clusters ≥
**1.34.1-gke.1341000**: `linuxConfig.swapConfig` in the node-system-config
file (`gcloud beta container node-pools update --system-config-from-file`),
boot-disk-backed on any machine (≤50% of boot disk) or Local-SSD-backed on
`-lssd` machine variants (plain c3 cannot attach Local SSD; n2 can via
`--ephemeral-storage-local-ssd`). Constraints that matter: **changing it
recreates the pool's nodes**; **only Burstable pods swap** (our benchmark
workers were BestEffort — the harness now patches requests<limits when swap
is on); Autopilot: no; zram/zswap DaemonSet hacks: skip (kubelet
failSwapOn crash-loops on restart, and swapped pages blind the eviction
signal — Bottlerocket #4903).

The harness now has the knob (`SWAP_GIB=16 ./run.sh`, or the TUI's "Node
swap" field) and, critically, the referee: per-wave SLO gates (wake p99,
turn p99), a fixed-work CPU probe, RAM-walk paging latency, and node PSI
(cpu/mem/io) in the occupancy CSV and dashboards. **Swap safely raises
oversubscription exactly when density climbs while those stay flat** — the
A/B to run: baseline vs SWAP_GIB=16 with rising AGENTS, compare the
load-test knee.

### Measured A/B (2026-09-28, 120 agents · ×6 · 30 min window · 10 workers on 2× c3-standard-4)

| | A: no swap | control: Burstable, no swap | B: swap 16 GiB (Burstable) |
|---|---|---|---|
| $/agent/mo | **$1.22** | $1.23 | **$1.24** |
| wake p50 / p99 | 1.8 s / **31.9 s** | 1.9 s / 31.9 s | 1.7 s / **6.5 s** |
| router refusals / errors | 339 / 59 | 391 / 154 | **15 / 4** |
| suspend avg | 3.0 s | 3.4 s | 4.3 s |
| wedge medic interventions | 37 | 34 | **4** |
| CPU probe p50/p99 (fixed work) | 17 / 46 ms | 18 / 47 ms | 18 / 38 ms |
| turn p99 · RAM-walk p99 | 26 ms · 43 ms | 28 ms · 49 ms | 29 ms · 36 ms |
| PSI mem-full / io-some (10s avg mean) | 1.1% / 19% | 0.8% / 19% | 3.3% / 35% |

Readings, honestly stated:
- **Swap is safe at this load.** The starvation referee stayed flat or
  improved: fixed-work CPU probe, turn p99 and post-resume RAM walk are
  all within noise of no-swap. Memory/IO pressure is visibly higher
  (mem-full 1.1→3.3%, io-some 19→35%) — the kernel *is* paging — but
  none of it reached the workload's latency.
- **Cost is unchanged at fixed density** ($1.22 → $1.24; the +2¢ is the
  slower suspend, 3.0→4.3 s, feeding the overhead term). That's expected:
  at the same agent count swap can't cut the bill — its payoff is
  *headroom*, so the experiment that monetizes it is the load-test knee
  (rising waves, swap vs not), still to run.
- **The tail collapse (wake p99 32 s → 6.5 s, refusals 339 → 15,
  wedges 37 → 4) is real but partly confounded**: the swap arm ran on
  freshly recreated nodes with a clean reinstall, while A and the control
  reused the aged pool. Attribute it to "fresh pool + swap", not swap
  alone, until a repeat on an aged pool says otherwise.
- Ops note for reproducers: `swapSizeGib` must be an int (a quoted
  string is a 400), `enabled: true` is required alongside the profile,
  and the node roll needs zone capacity — in a stocked-out zone, set the
  pool to delete-first upgrades (`--max-surge-upgrade=0
  --max-unavailable-upgrade=1`) so the roll recycles its own machines.

### Does swap raise the ceiling? (load-test knees, 2026-09-28)

The fixed-density A/B above cannot show an oversubscription gain by
construction; the knee test (waves of agents until an SLO gate trips) can.

**Knee 1 — swap 16 GiB, 10 workers, 128 MiB agents (the default profile):
sustainable 120 active agents, failure at 140** (15% router refusals,
wake p99 15 s). Host swap in use during the run: 23 MB of 16 GB; node PSI
≈ 0. This knee is *slot-bound* (10 workers, 1 actor each) — memory never
entered the picture, so swap could not have moved it. Lesson: with the
default 128 MiB agent on a 4 GiB-per-vCPU machine, the ceiling is worker
slots and checkpoint CPU, and swap is irrelevant. Swap can only add
agents where memory binds first — i.e. memory-heavy agents (OpenClaw's
gateway requests 512 MiB and is limited at 1.5 GiB) with enough worker
slots that slots are not the limit.

**Attempt at the memory-bound regime — 14 workers, 1 GiB agents: not
runnable.** Every suspend wedged (0 completed, 0 snapshots in GCS, node
idle). Cause, from the node's kernel log and runsc: `Memory cgroup out of
memory: Killed process (gvisor_sentry)` followed by `checkpoint failed:
containerManager.Checkpoint: EOF`. **gVisor's checkpoint transiently needs
roughly 2× the sandbox's resident memory**, so a 1 GiB agent overruns a
2 GiB worker limit mid-save; the sentry dies and Substrate leaves the actor
in SUSPENDING (a second route into the #1914 absorbing state — a failed
checkpoint is never propagated to a terminal state). Two consequences for
the cost model: (1) worker memory limits must be sized at ≥2–3× the
agent's resident set or suspend silently stops working; (2) T_s and the
checkpoint's memory burst both scale with resident set, which is the
strongest argument for *not* snapshotting warm-but-idle agents at all and
letting swap park them instead (see §5.3).

**Knee 2 (running) — swap on, 14 workers, 512 MiB agents**, then the same
on a no-swap node (a second pool in us-central1-a; us-central1-c is
stocked out). 14 busy agents ≈ 12 GiB resident + checkpoint transients on
a node with ~10 GiB free, so memory is on the critical path while a
single checkpoint still fits the 2 GiB limit.
