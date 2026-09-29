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

## Swap on GKE as an oversubscription lever — final state (2026-09-28/29)

**Question.** Can GKE node swap raise agents-per-node (and so cut $/agent)
for mostly-idle personal agents on Substrate?

**Answer.** *Yes, in exactly one configuration; no in the others.* With a
**dedicated Local-SSD swap device plus the reclaim sysctls recommended by the
GKE swap team**, a 16 GB node holds and *serves* **50 resident 512 MiB agents
instead of 15** (3.3×) — $4.25 → **$1.88 per agent-month** (3-yr CUD,
including the $30/mo local SSD) — with wakes of ~20 ms because nothing is
snapshotted. The costs: the first full touch of a paged-out 512 MiB working
set takes **5.7 s p50 / 8.6 s p90**, the node's CPU is measurably busier
(fixed-work probe 145 ms vs 47 ms), and the ceiling is a hard paging
throughput wall (58 active → timeouts). Boot-disk swap, ephemeral-Local-SSD
swap without the sysctls, and the snapshot-suspend path all fail to convert
swap into density, and the sysctls themselves livelock a swapless node.
Per-run artifacts: `assets/runs/2026-09-29-swap-parking/` and
`service/experiment/results/`.

### Results (same harness; within a row only swap differs)

| Design · shape | No swap | Swap variant | Outcome |
|---|---|---|---|
| **A. Snapshot-suspend** (production path), 10 workers, 128 MiB agents, c3-standard-4 | **$1.22/agent/mo**, knee 120 sustained / 140 fail (slot-bound) | boot-disk 16 GiB: $1.24, same knee, 23 MB swap touched | swap irrelevant at this footprint |
| **B. Snapshot-suspend**, 22 workers, 512 MiB agents, c3-standard-4-lssd | wave 1 (30 active) fails: wake p50 5.6 s / p99 12.7 s | Local-SSD 16 GiB: same (5.9 s / 12.2 s), 0 B swap used | 545 MiB FULL snapshot restore ≈ 6 s — SLO-infeasible with or without swap |
| **C. Resident parking** (no suspends; 60 workers = 60 agents), 512 MiB, c3-standard-4-lssd | **20 resident**, 20 active clean (wake 10 ms) | boot-disk 64 GiB: parks 60 (17 GB swapped) but wave 1 fails — `504` timeouts, 19 OOM kills, 9 evictions · Local-SSD 64 GiB (ephemeral profile, default sysctls): parks 60, wave 1 fails, node hung once | parked ≠ servable |
| **D. Resident parking**, 512 MiB, n2-standard-4, tuned sysctls (`swappiness=120`, `watermark_scale_factor=2000`, `min_free_kbytes=614400`) | default sysctls: **15 resident**, waves 10/15 clean · tuned sysctls: **node livelock** (twice; kubelet silent within 2 min, journald "under memory pressure", NPD plugins killed) | **dedicated Local-SSD swap (341 GB NVMe, encryption off): 58 resident, waves 10→50 clean (wake p50 15–22 ms, p99 ≤ 242 ms, turn p99 30 ms, 0 OOM/evictions), fail at 58 (7.7 % errors)** | **sustainable 50/node, 3.3× density, $1.88/agent-mo** |

Working-set page-in on the dedicated-SSD node (`readram` of the full
512 MiB after a wake): p50 5.7 s, p90 8.6 s, max 9.75 s, flat from 10 to 50
active — a per-sandbox fault-throughput limit (~90 MB/s through gVisor),
not a device limit; past ~50 active it crosses the router's 10 s upstream
timeout and the run degrades. Without swap the same read is 44 ms.

### Why the other variants fail (mechanisms, all observed)

1. **Kubelet eviction ignores swap** (`memory.available` is RAM-only). A node
   parking 60 agents at the 100 Mi threshold evicts Burstable workers as
   soon as a few become active ("container ateom using 1.1–2.0 GiB, request
   128 Mi"). The tuned sysctls + dedicated SSD avoided this entirely (0
   evictions): paging keeps `memory.available` above the threshold.
2. **Active agents balloon** to 1.5–2 GiB RSS in gVisor (churn + reads +
   sentry) and hit the 2 GiB actor limit unless swap absorbs it.
3. **Writes at capacity stall in reclaim** on boot-disk and default-sysctl
   nodes (`/writeram 504 UT` after 10 s); the tuned watermarks keep reclaim
   ahead of demand, but only when there is a fast device to reclaim into.
4. **The sysctls are not free-standing.** On a swapless node a 600 MB
   `min_free_kbytes` and a 20 % watermark gap keep the kernel reclaiming
   instead of OOM-killing → whole-node livelock (reproduced twice; the
   default-sysctls twin took 14 OOM kills and stayed up).
5. **LimitedSwap couples swap to requests** (`request × swap/RAM`); a
   341 GB device makes the allowance a non-issue, 16–64 GiB did not.
6. **Dead sandboxes stay RUNNING** (OOM-killed/evicted sentries keep
   `ACTOR_STATE_RUNNING`; requests 503 after the 5 s parking budget). The
   simulator pings placed agents before measuring; the platform should
   surface this (same family as #1914's absorbing states).

### Learnings worth keeping

- **Snapshot-suspend is size-bound:** 128 MiB agents wake in 1.7 s at $1.22;
  512 MiB agents need ~6 s and cannot meet a 10 s wake SLO at any density.
  For memory-heavy agents the choices are smaller/lazier snapshots (DATA
  scope, golden delta, node cache #690) or resident parking on swap.
- **Resident parking on dedicated-SSD swap is a real third tier**: ~20 ms
  wake, 5–9 s first full working-set touch, 3.3× density, node CPU tax.
  It is viable for agents whose wake touches a fraction of their memory;
  our `mem-read=all` is the worst case.
- **Per-sandbox memory limit is a separate knob** (ActorTemplate
  `limits.memory`, benchmark default 256 Mi; harness `ACTOR_MEMORY`).
  Undersized, it OOM-kills the sentry mid-checkpoint; swap *masks* it into
  90 s checkpoints.
- **Failures that never reach a terminal state** (SUSPENDING, RESUMING,
  DELETING, sandbox death under RUNNING) each pin a worker; a medic whose
  threshold exceeds the slowest honest operation is mandatory
  (`UNWEDGE_AFTER`).
- **gVisor snapshots pin the CPU FeatureSet** (`vmx`): every pool serving a
  fleet must expose identical CPU features.
- **GKE mechanics:** `swapSizeGib` int + `enabled: true`; boot-disk swap
  ≤ 50 % of disk and network-attached (avoid); ephemeral-SSD profile needs
  `-lssd` shapes; **dedicated-SSD profile (`diskCount`) needs a family that
  attaches Local SSDs (N2/N2D…) and is the one to use**; swap changes
  recreate the pool; run long experiments on an isolated `KUBECONFIG`.

### Verdict for the calculator/model

Swap stays out of the snapshot-suspend density model (it does not change
that path). It earns a place as a *separate operating mode* — resident
parking on dedicated-SSD swap with the tuned sysctls — for memory-heavy
agents: model it as agents/node ≈ 50 per 16 GB node at ~$1.9/agent-mo, with
a 20 ms wake and a 5–9 s first-touch, and validate zswap as the next arm
once the version allow-listing lands.
