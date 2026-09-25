# Theoretical cost model: agents on GKE via Substrate

This document defines the math behind the interactive calculator in `tool/`.
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

```
N_time = U / (d_eff * P)      actors per worker, time-sharing limit
```

**Churn cap.** Operators may cap suspend/resume cycles per worker (snapshot
I/O, GCS traffic and node pressure all scale with cycle rate). With `A`
activations per agent-day and a cap `C` cycles/worker/hour applied at the
peak hour:

```
N_churn = 24·C / (A · P)
N       = min(N_time, N_churn)
```

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
   families supporting it (see `docs/RESEARCH-gcp-pricing.md`); gVisor (GKE
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
