# $ / agent-month: how we get there — personal-assistant workload on bare metal, measured 2026-10-06

Workload: the **personal-assistant** agent-session script from substrate's benchmarking suite (substrate#2230): one lap is one day of an always-on assistant of the OpenClaw / Hermes kind — 61 steps named by time of day: 30 user messages in 7 clusters, 26 heartbeats, hourly crons folded into them, a morning briefing, four catalog refreshes, a context compaction, a nightly memory sweep. Every model turn reads its files, burns CPU, ships its context out through the router, **dwells** for the model round trip (worker held, no request) and appends to its WAL. Per lap: ~65 CPU-seconds, 440 s of resident dwell, 150 MiB written, resident set 640 → 960 MiB (1.5 GiB actors). Think gaps are the real gaps between events (2 min to 3 h, 24 h per lap); the driver parks the actor in every gap (**PauseActor** = node-local checkpoint, **SuspendActor** = durable checkpoint in the bucket) and the next step's first request wakes it. Played back-to-back at think scale 0.02, so one lap takes ~30 min plus the work.

Host: `agents-tco-east` (us-east4-a, one c3-standard-192-metal bare-metal node, native KVM, 3 TB Hyperdisk), 50 unsized workers, 30 agents with starts staggered over 20 minutes (so parks do not align), 55-minute windows (≥ 1 full lap per agent). Actor memory limit 1.5 GiB for the microVM legs and **3 GiB for the gVisor legs** (see the OOM note below; the resident set is the same). Every latency as P50 / P90 / P99 with n. Substrate: perf-resume-latency @ b98e7189 (main + docs).

| Step | gVisor bare metal · pause | gVisor bare metal · suspend | microVM bare metal · pause | microVM bare metal · suspend |
|---|---|---|---|---|
| Host, 3-yr CUD | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) |
| Workers per host → $ per worker-month | 50 → $63.58 | 50 → $63.58 | 50 → $63.58 | 50 → $63.58 |
| Agent profile | personal-assistant: 1 tasks × 61 steps/day, think ×0.02 (1728 s/task), driver suspend, 1536Mi actors | personal-assistant: 1 tasks × 61 steps/day, think ×0.02 (1728 s/task), driver suspend, 1536Mi actors | personal-assistant: 1 tasks × 61 steps/day, think ×0.02 (1728 s/task), driver suspend, 1536Mi actors | personal-assistant: 1 tasks × 61 steps/day, think ×0.02 (1728 s/task), driver suspend, 1536Mi actors |
| Run | 30 agents, time ×1, 54 min; 1897 activations, 0 errors, 0 refusals | 30 agents, time ×1, 54 min; 1826 activations, 0 errors, 0 refusals | 30 agents, time ×1, 53 min; 1362 activations, 9 errors, 35 refusals | 30 agents, time ×1, 54 min; 1737 activations, 0 errors, 1 refusals |
| Resume P50 / P90 / P99 | 423 / 537 / 890 ms (n=1897) ← T_r | 439 / 554 / 786 ms (n=1826) ← T_r | 593 / 724 / 3,094 ms (n=1362) ← T_r | 679 / 834 / 1,366 ms (n=1737) ← T_r |
| Park P50 / P90 / P99 (pause = node-local, suspend = bucket) | **pause** 1,131 / 3,619 / 5,157 ms (n=1897) ← T_s; avg 1,810 ms (driver, n=1897) | **suspend** 3,456 / 6,032 / 9,206 ms (n=1826) ← T_s; avg 4,092 ms (driver, n=1826) | **pause** 5,850 / 53,906 / 82,301 ms (n=1354) ← T_s; avg 18,912 ms (driver, n=1354) | **suspend** 3,286 / 15,261 / 55,333 ms (n=1737) ← T_s; avg 6,831 ms (driver, n=1737) |
| Step work P50 / P90 / P99 | 4,449 / 15,837 / 47,192 ms (n=1897) | 4,450 / 15,845 / 47,196 ms (n=1826) | 4,858 / 17,198 / 47,448 ms (n=1355) | 4,566 / 16,065 / 47,336 ms (n=1737) |
| Snapshot per agent (measured) and its parts | 1,050 MiB mean per agent (p50 1,050 MiB, max 1,056 MiB), node-local checkpoint, n=30: pages.img 1,048 MiB + checkpoint.img 2 MiB + pages_meta.img 0 MiB + manifest.json 0 MiB | 1,021 MiB mean per agent (p50 1,020 MiB, max 1,029 MiB), bucket snapshot, n=30: pages.img.zstd 1,020 MiB + checkpoint.img.zstd 0 MiB + pages_meta.img.zstd 0 MiB + manifest.json 0 MiB | 1,435 MiB mean per agent (p50 1,437 MiB, max 1,440 MiB), node-local checkpoint, n=30: memory-ranges 1,408 MiB + rootfs-upper.tar 27 MiB + state.json 0 MiB + config.json 0 MiB | 1,083 MiB mean per agent (p50 1,083 MiB, max 1,091 MiB), bucket snapshot, n=30: memory-ranges.zstd 1,054 MiB + rootfs-upper.tar.zstd 29 MiB + state.json.zstd 0 MiB + manifest.json 0 MiB |
| Worker time per wake-up | 2.2 s (wait + T_s + T_r) | 4.5 s (wait + T_s + T_r) | 19.5 s (wait + T_s + T_r) | 7.5 s (wait + T_s + T_r) |
| Occupancy (worker time per agent) | 0.77 %  (1 tasks × 61 steps = 61 steps, 8.6s work each (measured)) | 0.93 %  (1 tasks × 61 steps = 61 steps, 8.6s work each (measured)) | 2.01 %  (1 tasks × 61 steps = 61 steps, 9.0s work each (measured)) | 1.15 %  (1 tasks × 61 steps = 61 steps, 8.8s work each (measured)) |
| Measured density | 4.7:1 mean · 2.0:1 at P99 (busy workers mean 6.4, P99 15, peak 16 of 50) | 4.1:1 mean · 1.8:1 at P99 (busy workers mean 7.3, P99 17, peak 19 of 50) | 2.7:1 mean · 1.2:1 at P99 (busy workers mean 11.1, P99 25, peak 27 of 50) | 3.6:1 mean · 1.2:1 at P99 (busy workers mean 8.4, P99 25, peak 26 of 50) |
| Agents per worker (overcommit) | 45.6 = 0.70 ÷ (0.77 % × 2 peak) | 37.7 = 0.70 ÷ (0.93 % × 2 peak) | 17.4 = 0.70 ÷ (2.01 % × 2 peak) | 30.4 = 0.70 ÷ (1.15 % × 2 peak) |
| Agents per host | 2280 | 1883 | 868 | 1520 |
| $ / agent-month | $63.58 ÷ 45.6 = $1.39 compute + $0.00 GCS ops + $0.000 snapshots = **$1.39** | $63.58 ÷ 37.7 = $1.69 compute + $0.07 GCS ops + $0.020 snapshots = **$1.78** | $63.58 ÷ 17.4 = $3.66 compute + $0.00 GCS ops + $0.000 snapshots = **$3.66** | $63.58 ÷ 30.4 = $2.09 compute + $0.07 GCS ops + $0.021 snapshots = **$2.18** |
| Multi-actor projection (roadmap) | ≈25136 agents/host → ≈$0.13 | ≈20629 agents/host → ≈$0.24 | ≈9346 agents/host → ≈$0.34 | ≈15933 agents/host → ≈$0.29 |

## The bare-metal 2×2 at a glance

| | Pause (node-local) | Suspend (bucket) |
|---|---|---|
| **gVisor** resume | 423 / 537 / 890 (n=1,897) | 439 / 554 / 786 (n=1,826) |
| **gVisor** park | 1,131 / 3,619 / 5,157 (n=1,897) | 3,456 / 6,032 / 9,206 (n=1,826) |
| **microVM** resume | 593 / 724 / 3,094 (n=1,362) | 679 / 834 / 1,366 (n=1,737) |
| **microVM** park | 5,850 / 53,906 / 82,301 (n=1,354) | 3,286 / 15,261 / 55,333 (n=1,737) |
| snapshot per agent (mean, measured) | gVisor 1,050 MiB (pages.img 1,048 MiB + checkpoint.img 2 MiB + pages_meta.img 0 MiB) · microVM 1,435 MiB (memory-ranges 1,408 MiB + rootfs-upper.tar 27 MiB + state.json 0 MiB) | gVisor 1,021 MiB (pages.img.zstd 1,020 MiB + checkpoint.img.zstd 0 MiB + pages_meta.img.zstd 0 MiB) · microVM 1,083 MiB (memory-ranges.zstd 1,054 MiB + rootfs-upper.tar.zstd 29 MiB + state.json.zstd 0 MiB) |
| step work (ops + dwell) P50 / P90 / P99 | gVisor 4,449 / 15,837 / 47,192 (n=1,897) · microVM 4,858 / 17,198 / 47,448 (n=1,355) | gVisor 4,450 / 15,845 / 47,196 (n=1,826) · microVM 4,566 / 16,065 / 47,336 (n=1,737) |
| $ / agent-month, loop as run | gVisor $1.39 · microVM $3.66 | gVisor $1.78 · microVM $2.18 |

## Reading the numbers

- **What is being parked is ~1 GiB of resident memory**, not the 256 MiB bare actor of the one-ping runs: compare with `one-ping-pause-gvisor-vs-microvm-metal-2026-10-05.md` for the same host and lifecycle on an empty actor. The difference between the two files is the cost of the assistant's resident set per park and per wake.
- **Dwell is worker time that no park can reclaim.** The 440 s of model round trips per day are inside steps; the report counts them in "step work" and therefore in occupancy. Only the between-event think gaps (24 h a day) are parked.
- **Heartbeats and crons are wakes something external must provide**: in a suspended or paused sandbox the assistant's own timers cannot fire. The script models them as driver-initiated wakes, which is what a gateway or scheduler would have to do.
- **Snapshot size is the hidden half of the park cost.** A node-local pause of a 1.5 GiB microVM guest writes the whole guest memory (≈ 1.4 GiB per agent, uncompressed) to the node's disk; the bucket snapshot of a suspend is the compressed resident set. The row above gives each cell's measured per-agent size and its parts (memory image, root-filesystem upper layer, metadata).
- **gVisor could not park these actors at a 1.5 GiB limit.** A first gVisor/pause leg at ACTOR_MEMORY=1536Mi lost 7 of 30 agents in 35 minutes: `runsc checkpoint` exits 128 (`containerManager.Checkpoint failed: EOF`) because the node OOM-killer kills `gvisor_sentry` inside the actor's own memory cgroup during the checkpoint — the dirty page cache of the ~1 GiB checkpoint file being written is charged to the actor (dmesg: `Memory cgroup out of memory … gvisor_sentry`, `__GFP_WRITE`). A crashed actor is absorbing (every later step is a 503). microVM writes its memory image from the VMM, outside the guest cgroup, so the same limit was fine there. The gVisor columns above therefore ran with a 3 GiB limit (≈ 2× the resident set); the failed leg is archived as `assets/runs/2026-10-06-pa-gvisor-c3-metal-pause-1536Mi-OOM-ARTIFACT/`. This is a Substrate finding: a gVisor checkpoint needs headroom ≈ the resident set, or must write outside the actor's memcg.
- **Why 30 agents and not 100.** Two 100-agent legs (aligned and staggered starts) collapsed under their own parking: 100 × 1.4 GiB per pause landing on the one node's disk (≈ 5 GB/s demanded vs 2.4 GB/s of Hyperdisk) wedged the workers. The archived artifacts of those attempts are in `assets/runs/2026-10-06-pa-microvm-c3-metal-pause-ALIGNED-starts/` and `…-pause-100agents-staggered-ARTIFACT/` for reference; the numbers in this file come from the 30-agent legs.
- **$ lines price the loop as run** (50 workers on an otherwise empty 192-vCPU host; one lap per day; utilization 0.7 and peak ×2 assumptions) and exclude LLM tokens and the amortized cluster fee / control plane. Pause has no GCS operations or snapshot at rest; suspend does.

Artifacts: `assets/runs/2026-10-06-pa-{gvisor,microvm}-c3-metal-{pause,suspend}/` (report.txt, latency.csv, summary.json, run.log, occupancy.csv, metrics.txt; `=== agentsim steps ===` per-step table in run.log).

## Why microVM pause ($3.66) costs more than microVM suspend ($2.18)

This is counter-intuitive — a node-local pause should be the cheap park — so here is the arithmetic. Both cells use the same identity; the only input that differs materially is the park time, and it enters 61 times per agent-day.

| | microVM · pause | microVM · suspend |
|---|---|---|
| live work per day (61 steps × measured step work) | 551 s | 537 s |
| T_park, mean | 18.9 s | 6.8 s |
| T_park P50 / P90 / P99 | 5,850 / 53,906 / 82,301 ms | 3,286 / 15,261 / 55,333 ms |
| T_resume, P50 | 0.6 s | 0.7 s |
| (T_park + T_resume) × 61 wakes | 1,190 s | 458 s |
| occupancy | 2.01 % | 1.15 % |
| agents per worker | 17.4 | 30.4 |
| compute ($63.58 per worker-month ÷ agents per worker) | $3.66 | $2.09 |
| GCS ops + snapshot at rest | $0 | $0.07 + $0.021 |
| **$ / agent-month** | **$3.66** | **$2.18** |

- **Park time dominates the bill.** The worker is held for the whole checkpoint. With pause, the 61 parks cost 1,154 s of worker time per agent-day — about twice the 551 s the agent spends doing its own work. A second shaved off the park is worth as much as a second of the agent's compute.
- **Pause writes more bytes, uncompressed, to one disk.** A microVM pause writes the whole 1.4 GiB guest memory raw to the node's Hyperdisk (memory-ranges 1,408 MiB). Suspend zstd-compresses it first (1,054 MiB) and streams it to the bucket over the network, which scales with the number of concurrent parks in a way the single disk does not.
- **The mean is what the formula uses, and pause's mean is its tail.** Pause P50 is 5.8 s but P90 is 54 s and P99 is 82 s, so the mean lands at 18.9 s. When several agents park within the same minute they contend for the one disk (2,400 MiB/s provisioned); the 30-agent stagger spreads this out but does not remove it. Suspend's tail is much shorter (P90 15 s, P99 55 s), mean 6.8 s. Host CPU stayed under 8 % throughout — bandwidth, not compute, is the limiter.
- **What would flip it back.** If pause's tail matched its median (T_park ≈ 6 s) the pause cell would land near $1.98, below suspend. That is the engineering target for microVM pause: a compressed or incremental node-local checkpoint, or more local write bandwidth. gVisor pause shows what that buys — it writes only the ~1 GiB resident set, parks in 1.8 s mean, and comes out at $1.39.
- **Caveat on absolutes.** Dividing the node across only 50 workers is the loop as run; with a fuller pool every cell scales down together. The ratios between cells are the durable result.

## How $ / agent-month is calculated

Same identity as the one-ping file, with the script's structure supplying the counts: `S` = 61 steps per lap, one lap per agent-day, `W` = measured mean step work (ops + dwell), `T_s` = measured park (pause or suspend, mean), `T_r` = measured resume (P50 of the wake), `U` = 0.70, `P` = 2.

```
live             = S × W                                 worker time spent in steps (incl. dwell) per agent-day
wakes/day  A     = S × f                                 f = share of steps whose actor was parked (driver mode ≈ 1)
occupancy  d'    = (live + A × (T_s + T_r)) ÷ 86,400
agents/worker N  = U ÷ (d' × P)
$ / agent-month  = worker $/mo ÷ N  (+ GCS ops and snapshot at rest for suspend)
```

Worked, with each run's numbers (from `summary.json`):
**gVisor bare metal · pause**
- 1 tasks × 61 steps = 61 steps, 8.6s work each (measured); T_s 1.81 s (pause, mean), T_r 0.42 s (P50); overhead per wake 2.2 s
- occupancy = (527 s + 61 × 2.2 s) ÷ 86,400 = 0.77 %; agents/worker = 0.70 ÷ (0.77 % × 2) = 45.6
- node c3-standard-192-metal: $3,179/mo ÷ 50 workers = $63.58/worker-mo; compute $63.58 ÷ 45.6 = $1.39; GCS ops $0.00; snapshot $0.000 → **$1.39**; agents per host at that share 2,280

**gVisor bare metal · suspend**
- 1 tasks × 61 steps = 61 steps, 8.6s work each (measured); T_s 4.09 s (suspend, mean), T_r 0.44 s (P50); overhead per wake 4.5 s
- occupancy = (527 s + 61 × 4.5 s) ÷ 86,400 = 0.93 %; agents/worker = 0.70 ÷ (0.93 % × 2) = 37.7
- node c3-standard-192-metal: $3,179/mo ÷ 50 workers = $63.58/worker-mo; compute $63.58 ÷ 37.7 = $1.69; GCS ops $0.07; snapshot $0.020 → **$1.78**; agents per host at that share 1,883

**microVM bare metal · pause**
- 1 tasks × 61 steps = 61 steps, 9.0s work each (measured); T_s 18.91 s (pause, mean), T_r 0.59 s (P50); overhead per wake 19.5 s
- occupancy = (551 s + 61 × 19.5 s) ÷ 86,400 = 2.01 %; agents/worker = 0.70 ÷ (2.01 % × 2) = 17.4
- node c3-standard-192-metal: $3,179/mo ÷ 50 workers = $63.58/worker-mo; compute $63.58 ÷ 17.4 = $3.66; GCS ops $0.00; snapshot $0.000 → **$3.66**; agents per host at that share 868

**microVM bare metal · suspend**
- 1 tasks × 61 steps = 61 steps, 8.8s work each (measured); T_s 6.83 s (suspend, mean), T_r 0.68 s (P50); overhead per wake 7.5 s
- occupancy = (537 s + 61 × 7.5 s) ÷ 86,400 = 1.15 %; agents/worker = 0.70 ÷ (1.15 % × 2) = 30.4
- node c3-standard-192-metal: $3,179/mo ÷ 50 workers = $63.58/worker-mo; compute $63.58 ÷ 30.4 = $2.09; GCS ops $0.07; snapshot $0.021 → **$2.18**; agents per host at that share 1,520
