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
