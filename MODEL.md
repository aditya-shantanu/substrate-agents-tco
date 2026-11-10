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


**Share of workspaces used on a given day (`share`, DAU %, default 32 %).**
Every quantity above is per *active* agent-day. All registered agents stay
addressable (a message to a suspended one resumes it), but only a share of
them use their workspace on a given day; the rest keep their snapshot (or
volume) and make no wake-ups, at no compute cost. 32 % with the personal-agent
preset puts ~1 % of the fleet awake at once, the concurrency of the Filestore
team's aligned "long-running personal agent" scenario (1,000 of 100,000);
100 % was the calculator's original assumption. Below 100 % is also what lets
a Filestore workspace ever reach the Cold tier (§5b): at 100 % every volume
returns inside a 24 h window. The calculator carries this as a fleet-average factor:
`occupancy_avg = share × occupancy_active`, `activations_avg = share × A`.
Agents per worker, churn and storage operations use the averages; the
Filestore mapping below needs both the average and the per-active values.

**Workspace in the overhead.** The measured suspend and resume times come from
a 55 MiB memory-only snapshot. When the workspace travels in the snapshot
(durable directory) each wake-up also pays `2 × W / rate` for tar + zstd +
upload and download + untar, `rate` defaulting to 250 MB/s end to end (87–107
MB/s per GCS stream, ~500 MB/s per node with 8 streams, 450 MB restored in
2.2 s, measured). On a Filestore volume that term becomes the CSI attach +
NFS mount and detach, 0.1 s by default. Both are added to `T_idle + T_s +
T_r` in the occupancy above.
(The Filestore team's "long-running personal agent" scenario is 1,000 of
100,000 agents concurrently active for 1 h/day each, i.e. 24 % active per
day.)

**Agents awake at once.** The calculator shows the fleet-level consequence of
the per-agent numbers, `fleet × share × d'` on average and `× P` (or the
herd form) at the peak, as an editable field. Typing a target there scales
the useful-work part of the day (session and check-in lengths) so the fleet
reaches that concurrency, keeping the number of wake-ups fixed, so churn and
GCS traffic do not move; a target below the overhead-only floor
(`A × (T_idle + T_s + T_r)` per day) is refused with a note.

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

### 3a. Hardware oversubscription (the reported ratio)

The design-independent measure of packing — unaffected by whether agents
occupy one-per-worker slots today or CPU-pack under multi-actor later:

```
cpu_oversub = agents_on_machine · cpu_promised_per_agent / machine_vCPU
mem_oversub = agents_on_machine · mem_promised_per_agent / machine_GiB
```

"Promised" = the worker limits an agent gets while live. The two ratios are
reported separately because they differ whenever the worker shape doesn't
match the machine shape (e.g. 0.5 vCPU/1 GiB workers on a 16 vCPU/64 GiB
machine → CPU oversubscription is 2× the memory one). 1× = the hardware is
committed exactly once.

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
The Phase 2 measured-run report prints this as a clearly-labeled
projection line; the calculator intentionally does not (it shows only
current-settings numbers).

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

## 5. Storage cost

What Substrate actually does (upstream main, November 2026, read from
`cmd/atelet`, `cmd/ateworker-*`, `internal/objectstoreplugin`,
`pkg/objectstorage`, `cmd/ateapi/internal/controlapi`):

- **GCS holds snapshots and nothing else.** It is written at suspend and read
  at resume. Nothing an agent does while awake touches GCS: its file writes
  go to the node's boot disk (microVM: overlay upper and durable directories
  through virtiofs) or to sandbox memory (gVisor: rootfs overlay and durable
  directories are sentry tmpfs, charged to the actor's memory limit).
- **A snapshot has three layers**, each written in full on every checkpoint:
  the memory image (sparse; the only layer that compresses to its populated
  pages), the root-filesystem writes, and every `durableDir` volume, tarred
  (microVM) or bundled (gVisor). Compression is zstd, applied only on upload.
  External CSI volumes are never part of a snapshot.
- **One snapshot per agent at rest**; the previous one is released when the
  next suspend commits. Golden snapshots are one copy per template.
- **Operations.** Each data file is one PUT (objects over 64 MiB go as
  32 MiB parts plus a compose), the manifest is one PUT, and releasing the
  previous snapshot costs one LIST (deletes are free). A resume downloads the
  manifest and one 16 MiB range per file part, unless the node still holds
  the local copy.
- **Node-local copy.** After a suspend the checkpoint stays on the node for
  `--snapshot-detach-delay` (default 30 min). A resume on the same node inside
  that window makes no GCS request.
- **Durability mode.** `DURABILITY_MODE_SYNCHRONOUS` uploads before
  SuspendActor returns, once per suspend. `ASYNCHRONOUS` keeps the checkpoint
  local and uploads the latest one at most once per `--snapshot-upload-delay`
  (default 5 min) window, so checkpoints superseded inside the window are
  never uploaded.

The calculator's inputs and derivations:

| Symbol | Meaning | Default |
|---|---|---|
| `S_mem` | memory image + rootfs writes, compressed, GiB | 0.06 (OpenClaw measured) |
| `W` | workspace (durable directory) data, GiB | 1 (Filestore scenario; Hermes 3 GB, OpenClaw tens of MB) |
| `z` | workspace compression on upload | 50 % |
| `S` | bytes per snapshot in GCS = `S_mem + W·z` (durable dir) or `S_mem` (Filestore volume) | |
| `F_small` | small files per snapshot: gVisor 2 (checkpoint.img, rootfs bundle), microVM 4 (config, state, base-id, rootfs-upper tar) | |
| `D` | detach delay, min | 30 |
| `U` | upload window for async, min | 5 |

```
parts(x)   = x ≤ 64 MiB ? 1 : ceil(x / 32 MiB) + 1          # PUTs incl. compose
ranges(x)  = max(1, ceil(x / 16 MiB))                       # GETs
writes_per_suspend  = parts(S_mem) + [durdir] parts(W·z) + F_small + 1 (manifest) + 1 (LIST)
reads_per_download  = 1 (manifest) + ranges(S_mem) + [durdir] ranges(W·z) + F_small
uploads_per_day     = sync ? A : (1440/U) · (1 − exp(−A·U/1440))
local_fraction      = 1 − exp(−A·D/1440)                     # resumes that find the node-local copy
downloads_per_day   = A · (1 − local_fraction)

C_at_rest_per_agent = S · C_store                                     ($0.02/GiB-mo Standard)
C_ops_per_agent     = share · 30.44 · (uploads_per_day · writes · $5e-6 + downloads_per_day · reads · $4e-7)
```

The gap model is Poisson (wake-ups spread through the day), which is what the
personal-agent profiles assume. Bytes moved are free in-region but are
reported, because `S` bytes per suspend at ~500 MB/s per node is time inside
the suspend, and fleet-wide upload rates of tens of GB/s are an operational
limit long before they are a cost: with a 1 GiB durable directory at 43
suspends a day, 10,000 agents push ~250 TB/day through the snapshot path.

## 5b. Storage backend: Filestore agent volumes for the workspace

The second backend keeps the memory snapshot in GCS and moves the workspace
out of the snapshot onto one Filestore agent volume per agent. In Substrate
terms the volume is an external CSI volume (`ExternalVolumes` preview gate):
created with the actor, attached by the control plane at every resume,
detached at every suspend, never snapshotted, deleted with the actor. The repo
ships an NFS CSI driver and the hooks for any driver with a `CSIDriverConfig`;
a Filestore driver is operator-supplied. Writing the memory image to the
volume as well is not modelled: it would need new Substrate work.

Pricing and tiering behaviour are a straight port of the Filestore team's
*Filestore Agent Volumes TCO Calculator v2.7* (Nov 2026), sheet "Filestore
Agent Volumes TCO" rows 18–58, verified cell for cell against four of its
reference scenarios including the "Scenario 1" of the *TCO Scenario
Alignment* document.

**What is billed (1.00× region list):**

| SKU | Price | Applies to |
|---|---|---|
| Hot tier | $0.30 / GiB-month | volumes mounted by an awake agent |
| Warm tier | $0.15 / GiB-month | unmounted volumes inside the inactivity window (billed at the Hot rate during the initial GA, selectable) |
| Cold tier | $0.02 / GiB-month | dormant volumes past the window |
| Tiering transfer | $0.01 / GiB | each way between Warm and Cold (Hot↔Warm is free) |
| Provisioned IOPS | $0.027 / IOPS-month | every mounted volume: floor 1,600 IOPS (≤ 2 GiB, Default pool) or 3,000 (> 2 GiB, Large pool), plus any extra, max 7,500 |

**The eight Filestore inputs and how the calculator derives them:**

| Filestore input | Derived as |
|---|---|
| 1. Registered workspaces | `fleet` |
| 2. Active concurrent workspaces `A_vol` | mounted volumes = agents working or waiting: `fleet × share × (L + A·T_idle)/86400`, without the runtime's suspend and resume seconds, so the pool and the Filestore bill are identical for gVisor and microVM; sized on the 24-h average by default (the Filestore sheet's convention); the busiest hour (`× P`, or `H + (1−H)·d` in herd mode) is a switch for a pool whose provisioned IOPS cannot follow the daily curve, the same lens as the machines |
| 3. Continuous 24/7 share `c` | input (0 % in the aligned scenario) |
| 4. Daily active hours per workspace `h` | `(L + A·T_idle)/86400 × 24`: an active agent's useful work plus idle wait |
| 5. Used capacity per volume `g` | `W`, the workspace GiB (uncompressed; the memory snapshot stays in GCS) |
| 6. Extra IOPS | input, 0 default |
| 7. Inactivity window `W_h` | input, 24 h default (service minimum) |
| 8. Cold transition rate `k` | manual, default 10 % (the aligned Scenario 1 value; 21.18 % is the sheet's default), or derived: gaps inside an active day (Poisson, `exp(−A·W_h/24)`) plus the gap to the next active day for the agents dormant tomorrow, `k = ((A−1)·exp(−A·W_h/24) + (1 − share)) / A`. Whatever `k`, Cold stays empty while every agent returns inside the window: the Warm buffer (turnover × window) then covers the whole fleet, and the sheet's guardrail does the same |

**The sheet's formulas (ported verbatim):**

```
r   = A_vol / N
T   = min((N − A_vol)/24,  N·r·(1−c)·(24/h)·(1−r)/24)   # volumes/hour leaving the active pool
hot = A_vol · g
warm_candidate = min(N·g − hot, ceil(T·(24·(1−k) + k·W_h)) · g)
# break-even guardrail: Cold must pay for freeze+thaw ($0.02/GiB) through its
# $0.13/GiB-mo saving, i.e. ≥ 112.3 h of dormancy; otherwise keep inactive volumes Warm
warm = (k ≤ 0 or (N·g − hot − warm_candidate) < T·k·112.3·g) ? N·g − hot : warm_candidate
cold = max(0, N·g − hot − warm)
tiered_GiB_month = (cold > 0 ? min(2·cold, T·24·g·2·k) : 0) · 730/24
iops_per_vol = min(7500, (g ≤ 2 ? 1600 : 3000) + extra)
bill = hot·0.30 + warm·0.15 + cold·0.02 + tiered·0.01 + A_vol·iops_per_vol·0.027     (× (1 − discount))
```

In the fleet bill the GCS lines stay (memory snapshot at rest and its
operations, now without the workspace tar), the volume tiers and tiering join
the storage line, and the IOPS term joins the operations line. The "what is
going on" panel under the bill prints the GCS derivation and the eight
Filestore inputs with their provenance.

**What it shows.** For the default personal-agent workload (10,000 registered
agents, 32 % using their workspace on a given day with 43 wake-ups each, 1 GiB
workspace, ~113 awake at once) the two backends split like this. With the
workspace in the snapshot, GCS costs $0.06/agent (operations, mostly the
composite parts of a 512 MiB tar at every suspend) and adds ~8 s to every
wake-up for the tar, compress, upload, download and untar of the workspace:
$0.45/agent/month on gVisor, $0.48 on microVM, of which machines are $0.33 /
$0.36. With the workspace on Filestore, GCS drops to $0.01/agent and the
wake-up overhead to ~0.1 s (machines $0.28 / $0.33), but the Filestore bill
adds $0.48/agent: 91 mounted volumes on the 24-h average × 1,600 IOPS × $0.027
= $3,952/mo in provisioned IOPS plus $831/mo in tiered capacity and tiering
(Hot 91 / Warm 3,171 / Cold 6,738 GiB, 19,289 GiB moved per month), the same
for both runtimes: $0.83 / $0.88 total. That $0.48 is the Filestore team's
~$0.50 per registered workspace baseline reproduced on this fleet. The aligned
Scenario 1 (24 % of 100,000 agents active a day, one 1-h wake-up each)
reproduces their $50,014/mo exactly: 1,000 mounted volumes, Hot 1,000 / Warm
23,760 / Cold 75,240 GiB, $0.50 per registered agent.

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

## Appendix C: the coding-agent (script) profile

Phase 2 can run the agent-session script from Substrate's benchmarking suite
(agent-substrate/substrate#1934; `WORKLOAD=coding-session`) instead of the
personal-agent turns. A task is `S` steps (20 in the built-in script); step
`i` is preceded by a think gap `θ_i` (script value × `THINK_SCALE`) and
holds a worker for its measured work `W_i`. The same occupancy identity
applies, with the activation count and the idle wait set by the step
structure rather than by sessions and check-ins:

```
steps/day      = tasks_per_day × S
A (wakes/day)  = steps/day × f + check-ins            f = share of steps whose actor was suspended
live           = steps/day × W̄ + check-ins × 15 s      W̄ = measured mean step work
held gaps      = steps/day × (1 − f) × θ̄               gaps spent awake (idle mode only)
T_idle         = 0 (driver suspends when the turn ends) | IDLE_TIMEOUT (idle mode)
d'             = (live + held gaps + A × (T_idle + T_s + T_r)) / 86400
```

`final_report.py` reads `S`, `tasks_per_day`, the scaled think total and
the suspend mode from the profile block agentsim prints, and `W̄`, `f`,
`T_s` (driver mode: agentsim's own SuspendActor timings) and `T_r` from the
run. Two things differ from the personal profile and dominate the result:
the per-agent wake count (160 per day at 8 tasks, vs 43) and the snapshot
size (1 GiB actors, a few hundred MiB resident), so `T_s + T_r` and restore
CPU, not time-sharing, bound density. At the script's native think gaps
(2–8 s) a worker is never free; `THINK_SCALE` is the knob that places the
workload in the deck's "80–95 % idle" coding-agent band.
