# Theoretical cost model: agents on GKE via Substrate

This document defines the math behind the interactive calculator in [`calculator.html`](calculator.html).
Every symbol here maps 1:1 to a labeled input in the UI.

## 1. The core idea

Substrate maps a large set of **actors** (agent sandboxes) onto a small pool of
**workers** (K8s pods). An actor only occupies a worker while it is *live* or
while it is being *resumed/suspended*. The rest of the time it exists only as a
snapshot in storage. So the cost of an agent is:

```
cost_per_agent_month = (share of a worker it occupies) * (cost of a worker)
                     + (snapshot storage)
                     + (per-transition overhead, usually negligible)
                     + (amortized control plane / cluster fees)
```

## 2. Workload model (duty cycle)

An agent alternates between **active bursts** and idle gaps.

| Symbol | Meaning | UI input |
|---|---|---|
| `d` | duty cycle — fraction of wall-clock time the agent is active | "Duty cycle %" (or derived from sessions/day x session length) |
| `L` | mean live period per activation (seconds of continuous activity before it goes idle long enough to be suspended) | "Avg active burst length" |
| `T_r` | mean resume time (worker is held, agent not yet serving) | "Avg resume time" |
| `T_s` | mean suspend time (worker is held, agent already done) | "Avg suspend time" |

Duty cycle can be entered directly or derived:

```
d = (activations_per_day * L) / 86400
```

For "personal agents" (OpenClaw / Hermes style) we expect `d` in the 0.1%–5%
range: a handful of interactive sessions plus periodic background jobs per day.

### Effective worker occupancy

Each activation costs `L + T_r + T_s` seconds of worker time to deliver `L`
seconds of useful work. This is exactly the colleague's equation

```
QPS_max = num_workers / (L + T_s + T_r)          (worker turnover rate)
```

read in reverse. Per agent, the fraction of one worker consumed is:

```
d_eff = d * (L + T_r + T_s) / L
      = d * (1 + (T_r + T_s) / L)
```

The `(T_r + T_s)/L` term is the **switching tax**. If suspend+resume take 0.65s
and bursts are 60s, the tax is ~1%. If bursts are 2s, the tax is 32% — this is
why fast suspend/resume matters and why very chatty short-burst workloads
oversubscribe worse than the raw duty cycle suggests.

Suspends are only worth it when the idle gap exceeds a threshold; a **suspend
policy idle-timeout** `T_idle` (how long the system waits before suspending)
adds directly to occupied time per activation:

```
d_eff = d * (L + T_idle + T_r + T_s) / L
```

(`T_idle = 0` models an oracle scheduler; a real policy of e.g. 10–30s protects
against thrashing on multi-message conversations but costs occupancy.)

## 3. Oversubscription (actors per worker)

If workers could be packed to 100% busy:

```
N_ideal = 1 / d_eff
```

Real systems need headroom so that when an actor wakes, a worker is free
quickly (resume-wait SLO), and to absorb diurnal peaks and imperfect
scheduling:

| Symbol | Meaning |
|---|---|
| `U` | target worker utilization (0–1), e.g. 0.5–0.8. Covers queueing headroom + scheduler inefficiency. |
| `P` | peak-to-average activity ratio (diurnal concentration), e.g. 2–4 for consumer agents. Capacity must be provisioned for the peak. |
| `H` | herd fraction (0–1): the share of the fleet that can be *simultaneously* active because wake-ups are schedule-aligned (crons firing at :00). |

The tool offers **two peak models — pick one, they answer the same
provisioning question through different lenses**:

```
multiplier:  N_time = U / (d_eff · P)            rate varies over the day
herd:        N_time = U / (H + (1−H) · d_eff)    a fraction wakes at once
```

The multiplier lens fits timezone-driven consumer fleets (and is what a
measured peak-to-average ratio anchors). The herd lens fits cron-heavy
fleets: it is directly checkable against the schedule ("could 25% of my
agents fire simultaneously?") and charges the burst at full occupancy.
A measured p99 busy-worker count from a real run (Phase 2) beats both.

**Churn cap.** Operators may cap suspend/resume cycles per worker (snapshot
I/O, GCS traffic and node pressure all scale with cycle rate). With `A`
activations per agent-day and a cap `C` cycles/worker/minute applied at the
peak hour:

```
N_churn = 1440·C / (A · peak_factor)
N       = min(N_time, N_churn)
```

where `peak_factor` is `P` in multiplier mode and `1` in herd mode — the
herd burst is priced through occupancy, not churn, so the cap is compared
to the average cycle rate.

When `N_churn` binds, workers sit partly idle (below `U`) because they may
not churn faster — density is capped by cycles, not by time, and cost per
agent rises accordingly. The tool flags which limit is binding.

(If you provision with autoscaling that tracks the diurnal curve, set `P`
closer to 1 and instead reflect autoscaler slack in `U`.)

**Memory cap.** Density can also be capped by live memory rather than time:
a worker of size `M_worker` GB running actors of working-set `M_actor` GB can
hold at most `M_worker / M_actor` *live* actors — with 1 actor per worker
(current Substrate model) this is not binding, but multi-actor-workers change
that. The tool reports the time-based `N` and flags when snapshot/restore
bandwidth or memory would plausibly bind first.

### 3b. Per-phase CPU and the multi-actor (CPU-packed) projection

Each lifecycle phase has a measured CPU intensity (defaults from the GKE
Agent Runtime Benchmark v2 sheet):

| Phase | vCPU while in phase |
|---|---|
| active (serving a turn) | `c_act` ≈ 0.25 |
| suspending (checkpoint + zstd) | `c_sus` ≈ 0.30 |
| **restoring (decompress + page-in)** | **`c_res` ≈ 1.22 — the peak** |

Restore being the most expensive phase is why churn, not steady load, is
what saturates nodes — it also gives the churn cap a physical basis
(concurrent restores × 1.22 vCPU ≤ node CPU).

Today's Substrate runs one actor per fixed-size worker, so these weights
don't change the slot-based cost. But **multi-actor workers** (the
platform's roadmap lever) turn density into CPU packing:

```
cpu_avg      = (L_day·c_act + A·(T_s·c_sus + T_r·c_res)) / 86400   per agent
cpu_weighted = herd:       H·c_res + (1−H)·cpu_avg
               multiplier: cpu_avg · P
agents/node  = floor( U · min( cpu_alloc / cpu_weighted,
                             mem_alloc / (M_active · active_fraction) ) )
```

where `active_fraction` is `H + (1−H)·d_eff` (herd) or `d_eff·P`
(multiplier) — suspended agents hold no RAM. For microVM, `cpu_alloc`
already carries the nested-virt CPU tax, exactly as in slot packing.
The tool reports this as the **multi-actor upside**, clearly labeled as a
projection, never as today's price.

## 4. Worker cost

Workers are pods on GKE nodes.

| Symbol | Meaning |
|---|---|
| `C_node` | node $/hour (machine type, pricing model: on-demand / spot / 1y / 3y CUD) |
| `W` | workers per node = floor(allocatable_cpu / cpu_per_worker) ∩ floor(allocatable_mem / mem_per_worker) |
| `F_sys` | node overhead reserved for kubelet/system/Substrate node components (reduces allocatable) |

```
C_worker = C_node / W                       $/hour per worker
C_compute_per_agent = C_worker * 730 / N    $/month
```

**gVisor vs microVM.** The two scenarios differ in:

1. **Machine family.** microVMs need nested virtualization → restricted to
   families supporting it (see the price book appendix); gVisor (GKE
   Sandbox) runs on nearly anything. This changes `C_node`.
2. **Per-sandbox overhead.** gVisor: Sentry overhead (~memory + syscall tax).
   uVM: guest kernel + VMM memory overhead per sandbox, affects `W` and
   suspend/resume times (snapshot includes guest RAM).
3. **`T_r` / `T_s`.** Different checkpoint/restore paths and snapshot sizes.

## 5. Storage cost (suspended state)

Every non-live agent holds a snapshot (RAM + filesystem delta):

| Symbol | Meaning |
|---|---|
| `S` | snapshot size GB (RAM working set + FS delta, possibly compressed) |
| `C_store` | $/GB-month for snapshot storage (GCS Standard, or PD if node-local architecture) |
| `ops` | 2 storage writes+reads per activation cycle — GCS ops cost, usually cents |

```
C_storage_per_agent = S * C_store * (1 - d)   ≈ S * C_store
C_ops_per_agent     = activations_per_month * (op_cost_write + op_cost_read)
```

## 6. Fixed / amortized costs

```
C_fixed_per_agent = (cluster_fee + control_plane_nodes) / total_agents
```

GKE cluster fee is per-cluster ($/hr); Substrate control plane pods consume
some nodes too. Amortized over the whole fleet, this is small at scale but
dominates for tiny fleets — the tool exposes fleet size for this reason.

## 7. Total

```
cost_per_agent_month =
    C_worker * 730 / N          # compute share
  + S * C_store                 # snapshot at rest
  + C_ops_per_agent             # transition ops
  + C_fixed_per_agent           # cluster fee + control plane amortized
```

The tool computes this bottom-up as a **fleet bill** so the division is
visible: workers = ⌈fleet/N⌉, nodes = ⌈workers/W⌉, then
`total $/mo = nodes·C_node·730 + fleet·S·C_store + fleet·ops + fixed`, and
`cost per agent = total / fleet` (identical to the formula above up to node
rounding). It also reports churn: fleet resumes/min = fleet·A/1440, cycles
per worker-hour, and the per-worker ceiling `3600/(L_avg + T_idle + T_s + T_r)`
— the QPS identity again.

And the headline comparison: **1 dedicated always-on sandbox** would cost
`C_worker * 730`, so the savings multiple is ≈ `N` (minus storage/ops).

## 8. Relationship to the benchmarking equations

The colleague's ideal-throughput equation

```
QPS = num_workers / (live_period + avg_suspend + avg_resume)
```

is the *turnover* view of the same model: measured cluster QPS at saturation
tells you the real `L + T_s + T_r` including all systemic inefficiencies, so

```
cost_per_actor = (1/QPS) * (1/num_workers)⁻¹ … ≡ (L + T_s + T_r) * C_worker_per_second
```

is the measured cost of *one activation*. Multiply by activations/month and you
recover the compute term of §7. Phase 2 of this repo measures QPS empirically
(Glutton workload + auto-suspend service) to calibrate `T_r`, `T_s`, `U`
against the theoretical model.

## Known limits of the model

- Assumes 1 actor per worker at a time (multi-actor-workers make `W`/`N`
  fuzzier; model that as smaller "virtual workers" for now).
- Assumes suspend/resume latency independent of cluster load; in reality both
  grow with workers-per-node, snapshot size, and storage/network saturation —
  the tool exposes them as direct inputs so you can plug in measured values.
- Erlang/queueing behavior is collapsed into the single utilization knob `U`.

## Appendix A: price book (us-central1, retrieved 2026-09-25)

Per-vCPU / per-GiB on-demand $/hr; resource CUDs 37% (1y) / 55% (3y) off;
spot is dynamic (point-in-time discounts shown). "nested" = supports nested
virtualization (required by microVM); Autopilot does not support nested virt.

| Family | $/vCPU | $/GiB | spot disc. | nested |
|---|---|---|---|---|
| e2 | 0.021811 | 0.002923 | ~40% | no |
| n1 | 0.031611 | 0.004237 | ~40% | yes |
| n2 | 0.031611 | 0.004237 | ~40% | yes |
| n2d | 0.027502 | 0.003686 | ~51% | no |
| n4 | 0.031190 | 0.003540 | ~43% | yes |
| c3 | 0.034650 | 0.003938 | ~62% | yes |
| c3d | 0.029563 | 0.003959 | ~76% | no |
| c4 | 0.034650 | 0.003938 | ~40% | yes |
| c4d | 0.032704 | 0.003753 | ~58% | no |
| t2d | 0.027502 | 0.003686 | ~40% | no |

Also nested-capable: n4d, c2, c4n, a2, g2, h3, m4, z3. GKE cluster fee
$0.10/hr. GCS Standard $0.020/GiB-mo; ops $5.00/M writes, $0.40/M reads;
VM↔GCS same region free. Nested virt: GKE Standard only,
`--enable-nested-virtualization` at node-pool creation, ~10% CPU penalty.

## Appendix B: measured constants (live cluster, 2026-09-25)

Canonical personal-agent profile used across the tools: 3 sessions × 8 min
+ 40 wakes × 15 s per day (A = 43 wake-ups, 2 040 s live, 2.36% duty).

From a 30-min run of 50 personal-agent-profile actors (128Mi working sets,
real paging + file I/O) on 10 gVisor workers / 2× c3-standard-4, GKE 1.36.4,
substrate main @ e3a041bc — artifacts in `assets/runs/2026-09-25-baseline-x6/`:

| Constant | Value |
|---|---|
| SuspendActor avg (`T_s`) | 2.46 s |
| Resume via router p50 (`T_r`) | 1.43 s |
| Snapshot per agent (zstd, GCS) | 0.116 GiB |
| Post-resume RAM walk p50 | 14 ms |
| Density | 14.8:1 mean · 7.1:1 at p99 |
| Cost | $1.21/agent/month (3y CUD) |

Real-OpenClaw reference points (measured elsewhere, 2026-09-14): suspend
2.2 s / resume 3.4 s P50, 55–61 MiB snapshots, 0.72% duty on a 20-min cron.
Comparison-sheet reference (GKE Agent Runtime Benchmark v2): per-host
sandbox densities 82 (CHV) / 84 (GKE microVM) / 119 (gVisor) / 616
(Substrate gVisor + suspend/resume, 2× n2-standard-48); its independent
CPU-packing model prices 10.4%-duty agents at $5.35 — which scales to
exactly our measured $1.21 at 2.36% duty.

Platform bug found during the runs (actors wedge in SUSPENDING, workers
pinned): filed as agent-substrate/substrate#1914; the autosuspender's medic
works around it.
