# $ / agent-month: how we get there — one-ping workload with PauseActor, measured 2026-10-05/06

Workload: the GluttonUser loop of substrate's benchmarking suite (the Prow 200K run's workload): a virtual user owns 20 actors and serves them one at a time — wake by ping through the router (implicit resume), **PauseActor** (node-local checkpoint) right after the ping, 10 s wait, next actor. No memory fill, no other work. Overcommit is 20:1 by construction; what is measured is the switch cost. Every latency as P50 / P90 / P99 with n.

The 2×2 on bare metal: gVisor and microVM × PauseActor (node-local checkpoint) and SuspendActor (durable checkpoint in the bucket), all on `agents-tco-east` (us-east4-a, one c3-standard-192-metal node, native KVM, 50 unsized workers, 1,000 agents, 20-minute windows). Last column: the same loop with gVisor + pause on `agents-tco` (us-central1, c3-standard-4 pool, 10 workers over 4 nodes, 200 agents) as the small-node reference. Substrate: main @ 01e299f9 (central) / perf-resume-latency @ b98e7189 (east; same code plus docs). Actor template 256 MiB; workers carry no CPU/memory requests or limits in these four runs.

| Step | gVisor bare metal · pause | gVisor bare metal · suspend | microVM bare metal · pause | microVM bare metal · suspend | gVisor c3-standard-4 · pause (small-node reference) |
|---|---|---|---|---|---|
| Host, 3-yr CUD | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-4, $66.2/mo (cud3) · 4 node(s) |
| Workers per host → $ per worker-month | 50 → $63.58 | 50 → $63.58 | 50 → $63.58 | 50 → $63.58 | 2 → $26.49 |
| Agent profile | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill |
| Run | 1000 agents, time ×1, 20 min; 5701 activations, 0 errors, 0 refusals | 1000 agents, time ×1, 20 min; 5407 activations, 0 errors, 0 refusals | 1000 agents, time ×1, 20 min; 5659 activations, 0 errors, 0 refusals | 1000 agents, time ×1, 20 min; 5160 activations, 0 errors, 0 refusals | 200 agents, time ×1, 20 min; 1168 activations, 0 errors, 0 refusals |
| Resume P50 / P90 / P99 | 275 / 318 / 400 ms (n=5701) ← T_r | 371 / 769 / 1,239 ms (n=5407) ← T_r | 160 / 231 / 797 ms (n=5659) ← T_r | 160 / 234 / 434 ms (n=5160) ← T_r | 157 / 188 / 215 ms (n=1168) ← T_r |
| Park P50 / P90 / P99 (pause = node-local, suspend = bucket) | **pause** 241 / 292 / 388 ms (n=5701) ← T_s; avg 249 ms (driver, n=5701) | **suspend** 585 / 923 / 1,331 ms (n=5407) ← T_s; avg 645 ms (driver, n=5407) | **pause** 214 / 577 / 3,688 ms (n=5659) ← T_s; avg 396 ms (driver, n=5659) | **suspend** 1,172 / 2,404 / 4,466 ms (n=5160) ← T_s; avg 1,456 ms (driver, n=5160) | **pause** 120 / 139 / 161 ms (n=1168) ← T_s; avg 122 ms (driver, n=1168) |
| Step work P50 / P90 / P99 | — | — | — | — | — |
| Worker time per wake-up | 0.5 s (wait + T_s + T_r) | 1.0 s (wait + T_s + T_r) | 0.6 s (wait + T_s + T_r) | 1.6 s (wait + T_s + T_r) | 0.3 s (wait + T_s + T_r) |
| Occupancy (worker time per agent) | 0.25 %  (410 wakes/day of 1 ping; an actor's period = 20 × (0.5 s cycle + 10 s wait) = 210 s) | 0.46 %  (392 wakes/day of 1 ping; an actor's period = 20 × (1.0 s cycle + 10 s wait) = 220 s) | 0.26 %  (409 wakes/day of 1 ping; an actor's period = 20 × (0.6 s cycle + 10 s wait) = 211 s) | 0.70 %  (372 wakes/day of 1 ping; an actor's period = 20 × (1.6 s cycle + 10 s wait) = 232 s) | 0.14 %  (420 wakes/day of 1 ping; an actor's period = 20 × (0.3 s cycle + 10 s wait) = 206 s) |
| Measured density | 399.3:1 mean · 166.7:1 at P99 (busy workers mean 2.5, P99 6, peak 7 of 50) | n/a (no occupancy samples in the window) | 399.7:1 mean · 45.5:1 at P99 (busy workers mean 2.5, P99 22, peak 37 of 50) | 158.2:1 mean · 38.5:1 at P99 (busy workers mean 6.3, P99 26, peak 30 of 50) | 716.4:1 mean · 100.0:1 at P99 (busy workers mean 0.3, P99 2, peak 3 of 10) |
| Agents per worker (overcommit) | 140.4 = 0.70 ÷ (0.25 % × 2 peak) | 75.9 = 0.70 ÷ (0.46 % × 2 peak) | 132.9 = 0.70 ÷ (0.26 % × 2 peak) | 50.3 = 0.70 ÷ (0.70 % × 2 peak) | 257.7 = 0.70 ÷ (0.14 % × 2 peak) |
| Agents per host | 7022 | 3793 | 6646 | 2516 | 644 |
| $ / agent-month | $63.58 ÷ 140.4 = $0.45 compute + $0.00 GCS ops + $0.000 snapshots = **$0.45** | $63.58 ÷ 75.9 = $0.84 compute + $0.44 GCS ops + $0.000 snapshots = **$1.28** | $63.58 ÷ 132.9 = $0.48 compute + $0.00 GCS ops + $0.000 snapshots = **$0.48** | $63.58 ÷ 50.3 = $1.26 compute + $0.41 GCS ops + $0.001 snapshots = **$1.68** | $26.49 ÷ 257.7 = $0.10 compute + $0.00 GCS ops + $0.000 snapshots = **$0.10** |
| Multi-actor projection (roadmap) | ≈29285 agents/host → ≈$0.11 | ≈19474 agents/host → ≈$0.60 | ≈38418 agents/host → ≈$0.08 | ≈20986 agents/host → ≈$0.57 | ≈1071 agents/host → ≈$0.06 |

## The bare-metal 2×2 at a glance

Same host (c3-standard-192-metal, native KVM), same loop, 50 unsized workers, 1,000 agents, 20-minute windows, zero errors in all four. Resume = the wake ping through the router; park = PauseActor (node-local) or SuspendActor (bucket). All P50 / P90 / P99 in ms.

| | Pause (node-local) | Suspend (bucket) |
|---|---|---|
| **gVisor** resume | 275 / 318 / 400 (n=5,701) | 371 / 769 / 1,239 (n=5,407) |
| **gVisor** park | 241 / 292 / 388 | 585 / 923 / 1,331 |
| **microVM** resume | 160 / 231 / 797 (n=5,659) | 160 / 234 / 434 (n=5,160) |
| **microVM** park | 214 / 577 / 3,688 | 1,172 / 2,404 / 4,466 |
| $ / agent-month, loop as run | gVisor $0.45 · microVM $0.48 | gVisor $1.28 · microVM $1.68 |

- **Park mode is the bigger lever for gVisor, the runtime for microVM resume.** gVisor pays ~96 ms more on resume and ~344 ms more on park when it goes through the bucket. microVM's resume is the same ~160 ms either way; what suspend costs it is the park, 1.2 s median versus 0.2 s, because the full guest memory goes to the bucket.
- **microVM resumes faster than gVisor on bare metal at the median, in both modes** (160 vs 275 ms with pause, 160 vs 371 ms with suspend). gVisor's advantage is the tail: with pause its P99 resume is 400 ms against microVM's 797 ms, and its P99 park 388 ms against 3,688 ms.
- **The suspend run is the cleaner microVM measurement**: microVM's tails under suspend (P99 resume 434 ms) are tighter than under pause (797 ms), the opposite of gVisor. Pause's long tail on microVM is the host-contention effect the ladder section exposes.
- **The $ lines follow the park mode**: pause has no GCS operations, suspend does; all four price 50 workers on an otherwise empty 192-vCPU host and are not a TCO.

## Reading the numbers

- **Park mode dominates, runtime second.** On the same bare-metal host, pause vs suspend: gVisor resume 275 / 318 / 400 vs 371 / 769 / 1,239 ms and park 241 / 292 / 388 vs 585 / 923 / 1,331 ms; microVM resume 160 / 231 / 797 vs 160 / 234 / 434 ms and park 214 / 577 / 3,688 vs 1,172 / 2,404 / 4,466 ms. The bucket round trip is what the user feels most.
- **gVisor vs microVM with pause (same host, same loop):** microVM has the faster median resume (160 vs 275 ms) and gVisor the tighter tails (resume P99 400 vs 797 ms; pause P99 388 vs 3688 ms).
- **gVisor vs microVM with suspend (same host, same loop):** resume 371 / 769 / 1,239 vs 160 / 234 / 434 ms; suspend 585 / 923 / 1,331 vs 1,172 / 2,404 / 4,466 ms.
- **Small node vs bare metal for gVisor pause:** c3-standard-4 resume 157 / 188 / 215 ms vs bare metal 275 / 318 / 400 ms. The small node had 2 workers per 4-vCPU node and 200 agents; the metal host 50 workers and 1,000 agents.
- **Bare metal vs nested virtualization for microVM is still open:** no nested-virt microVM run of this ping loop exists yet (the nested microVM measurement so far is the coding-session workload with suspend: resume 1,906 / 3,278 / 9,603 ms).
- **$ lines are for the loop as run and are not the point here.** Pause has no GCS operations and no snapshot at rest (node-local); "agents per host" extrapolates the measured per-worker share to the whole machine under the model's utilization and peak assumptions; the bare-metal columns price 50 workers on a 192-vCPU host that was otherwise empty, the c3-standard-4 column 2 workers per node ($26.49 per worker rather than $13.25 at 5 per node). The ladder section is the measured, non-extrapolated view of the host.
- **Pause pins the actor to its node** and keeps a checkpoint on local disk (~230 MB per parked 256 MiB microVM actor; deleted actors' directories are not reclaimed — see the ladder section). Suspend costs the round trip but frees the node entirely.
- Excludes LLM tokens and the amortized cluster fee / control plane. Utilization 0.7 and peak ×2 are assumptions; T_s, T_r and density are measured.

Artifacts: `assets/runs/2026-10-05-ping-{gvisor-c3-standard-4-pause,microvm-c3-metal-pause,gvisor-c3-metal-suspend}/`, `assets/runs/2026-10-06-ping-{gvisor-c3-metal-pause,microvm-c3-metal-suspend}/`, ladder `assets/runs/2026-10-06-ping-ladder-microvm-c3-metal-pause/` (report.txt, latency.csv, summary.json, run.log, occupancy.csv, metrics.txt, waves.csv).


## Cold start: a new actor's first life, 2026-10-06

Substrate has no per-actor boot: an actor is created logically and its first `ResumeActor` restores the **template's golden snapshot** onto a free worker. "Cold start" here is therefore measured the way the upstream spawn benchmark measures `ActorTimeToReady`: `CreateActor` → first successful `ResumeActor` (the golden-snapshot restore) → first ping answered through the router. 100 new actors per scenario, 4 in flight at a time, the one-ping workload's own glutton template (256 MiB actors), unsized workers (50 on the metal host, 10 on the c3-standard-4 pool over 4 nodes). Node-level cold costs (first gVisor/kata asset fetch, first image unpack on a fresh node) are *not* in these numbers: every node had served actors before. Each scenario used a fresh actor-name prefix so no actor could pre-exist.

| Cold start (ms, P50 / P90 / P99) | c3-standard-4 nested virt · gVisor | c3-standard-4 nested virt · microVM | bare metal · gVisor | bare metal · microVM |
|---|---|---|---|---|
| Ready: CreateActor → first answered ping | 274 / 355 / 586 (max 610) | 710 / 1,268 / 1,752 (max 1,914) | 340 / 379 / 456 (max 458) | 596 / 666 / 800 (max 1,360) |
| of which first ResumeActor (golden-snapshot restore) | 254 / 341 / 555 (max 578) | 691 / 1,240 / 1,734 (max 1,883) | 330 / 369 / 445 (max 446) | 586 / 640 / 790 (max 1,350) |
| of which resume → first answered ping | 15 / 24 / 37 (max 46) | 18 / 30 / 40 (max 45) | 9 / 12 / 18 (max 32) | 8 / 10 / 32 (max 33) |
| of which CreateActor | 3 / 4 / 14 (max 14) | 2 / 4 / 7 (max 16) | 1 / 2 / 2 (max 3) | 1 / 2 / 3 (max 10) |
| n (failed) · retries (resume / ping) | 100 (0) · 0 / 0 | 100 (0) · 0 / 0 | 100 (0) · 0 / 0 | 100 (0) · 0 / 0 |

Reading it: the first ping is milliseconds everywhere, so a cold start *is* the golden-snapshot restore plus control-plane bookkeeping, no boot. gVisor cold-starts in 274 ms (nested) / 340 ms (metal) at the median; microVM in 710 ms (nested) / 596 ms (metal). Bare metal helps microVM (−16 % median, far tighter tail: P99 800 vs 1,752 ms) and costs gVisor a little at the median while tightening its tail. Against the warm wakes in the table above, a cold start is roughly one extra restore's worth of time.

Artifacts: `assets/runs/2026-10-06-cold-start-{central,east}-{gvisor,microvm}/` (run.log, cold-starts.csv, cold-summary.json).

## How $ / agent-month is calculated

The card prices the loop the way the calculator and MODEL.md do: the whole bill divided by the agents it serves, built bottom-up from measured switch times. Symbols: `N_u` actors per user (20), `wait` the sleep after each park (10 s), `W` the live window actually held (0 here: the actor is parked right after its ping), `T_s` the measured park time (pause or suspend, mean), `T_r` the measured resume time (P50 of the wake ping), `U` target utilization (0.70), `P` peak-hour multiplier (2).

```
cycle            = W + T_s + T_r                       worker time one wake holds
period           = N_u × (cycle + wait)                 one actor's time between wakes (its user serializes N_u actors)
wakes/day        = 86,400 ÷ period
occupancy  d'    = wakes/day × cycle ÷ 86,400           ≈ cycle ÷ period: share of one worker an agent consumes
agents/worker N  = U ÷ (d' × P)                         headroom for queueing and for the busiest hour
node $/mo        = (vCPU × $/vCPU-h + GiB × $/GiB-h) × 0.45 (3-yr CUD) × 730 h    us-central1 price book, MODEL.md App. A
worker $/mo      = node $/mo ÷ workers per node
compute          = worker $/mo ÷ N
GCS ops          = wakes/day × 30.44 × (7 writes × $5/M + 4 reads × $0.40/M)      suspend only; pause is node-local → 0
snapshot at rest = GiB per agent × $0.02                                          suspend only → 0 for pause
$ / agent-month  = compute + GCS ops + snapshot at rest
```

Excluded on purpose: LLM tokens, and the cluster fee + control plane (≈ $573 / mo, divided by the fleet). The 20:1 of the loop itself does not enter the price: `N` comes from measured occupancy, which is why a faster park/resume buys more agents per worker.

Worked, with each run's numbers (from `summary.json`):

**gVisor bare metal · pause**
- cycle = 0.00 + 0.249 (pause, mean) + 0.275 (resume P50) = 0.52 s; period = 20 × (0.52 + 10) = 210 s; wakes/day = 86,400 ÷ 210 = 410
- occupancy = 410 × 0.52 ÷ 86,400 = 0.25 %; agents/worker = 0.70 ÷ (0.25 % × 2) = 140.4
- node c3-standard-192-metal: $4.3547/h × 730 = $3,179/mo; 50 workers per node → worker $63.58/mo; compute = $63.58 ÷ 140.4 = $0.45
- pause: GCS ops $0, snapshot $0 → **$ / agent-month = $0.45**; agents per host at that share = 140.4 × 50 = 7,022

**gVisor bare metal · suspend**
- cycle = 0.00 + 0.645 (suspend, mean) + 0.371 (resume P50) = 1.02 s; period = 20 × (1.02 + 10) = 220 s; wakes/day = 86,400 ÷ 220 = 392
- occupancy = 392 × 1.02 ÷ 86,400 = 0.46 %; agents/worker = 0.70 ÷ (0.46 % × 2) = 75.9
- node c3-standard-192-metal: $4.3547/h × 730 = $3,179/mo; 50 workers per node → worker $63.58/mo; compute = $63.58 ÷ 75.9 = $0.84
- GCS ops = 392 × 30.44 × (7 × $5e-6 + 4 × $4e-7) = $0.44; snapshot = 0.02 GiB × $0.02 = $0.000 → **$ / agent-month = $1.28**; agents per host at that share = 75.9 × 50 = 3,793

**microVM bare metal · pause**
- cycle = 0.00 + 0.396 (pause, mean) + 0.160 (resume P50) = 0.56 s; period = 20 × (0.56 + 10) = 211 s; wakes/day = 86,400 ÷ 211 = 409
- occupancy = 409 × 0.56 ÷ 86,400 = 0.26 %; agents/worker = 0.70 ÷ (0.26 % × 2) = 132.9
- node c3-standard-192-metal: $4.3547/h × 730 = $3,179/mo; 50 workers per node → worker $63.58/mo; compute = $63.58 ÷ 132.9 = $0.48
- pause: GCS ops $0, snapshot $0 → **$ / agent-month = $0.48**; agents per host at that share = 132.9 × 50 = 6,646

**microVM bare metal · suspend**
- cycle = 0.00 + 1.456 (suspend, mean) + 0.160 (resume P50) = 1.62 s; period = 20 × (1.62 + 10) = 232 s; wakes/day = 86,400 ÷ 232 = 372
- occupancy = 372 × 1.62 ÷ 86,400 = 0.70 %; agents/worker = 0.70 ÷ (0.70 % × 2) = 50.3
- node c3-standard-192-metal: $4.3547/h × 730 = $3,179/mo; 50 workers per node → worker $63.58/mo; compute = $63.58 ÷ 50.3 = $1.26
- GCS ops = 372 × 30.44 × (7 × $5e-6 + 4 × $4e-7) = $0.41; snapshot = 0.03 GiB × $0.02 = $0.001 → **$ / agent-month = $1.68**; agents per host at that share = 50.3 × 50 = 2,516

**gVisor c3-standard-4 · pause (small-node reference)**
- cycle = 0.00 + 0.122 (pause, mean) + 0.157 (resume P50) = 0.28 s; period = 20 × (0.28 + 10) = 206 s; wakes/day = 86,400 ÷ 206 = 420
- occupancy = 420 × 0.28 ÷ 86,400 = 0.14 %; agents/worker = 0.70 ÷ (0.14 % × 2) = 257.7
- node c3-standard-4: $0.0907/h × 730 = $66/mo; 2 workers per node → worker $26.49/mo; compute = $26.49 ÷ 257.7 = $0.10
- pause: GCS ops $0, snapshot $0 → **$ / agent-month = $0.10**; agents per host at that share = 257.7 × 2 = 644

Two cautions on reading these dollar figures: the gVisor c3-standard-4 column's 10 workers sat on 4 nodes (2 per node) after the zone expansion, so its worker costs $26.49 rather than the $13.25 of a 5-per-node pool, and the bare-metal columns price 50 workers on a 192-vCPU host that was otherwise empty — in production the same host would carry several hundred workers, which is what the "agents per host" line extrapolates. The ladder section above is the measured, non-extrapolated view of that host.
