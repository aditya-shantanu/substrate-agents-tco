# $ / agent-month: how we get there — one-ping workload with PauseActor, measured 2026-10-05/06

Workload: the GluttonUser loop of substrate's benchmarking suite (the Prow 200K run's workload): a virtual user owns 20 actors and serves them one at a time — wake by ping through the router (implicit resume), **PauseActor** (node-local checkpoint) right after the ping, 10 s wait, next actor. No memory fill, no other work. Overcommit is 20:1 by construction; what is measured is the switch cost. Every latency as P50 / P90 / P99 with n.

Hosts: gVisor on `agents-tco` (us-central1, c3-standard-4 pool, 10 workers over 4 nodes, 200 agents). microVM and gVisor on `agents-tco-east` (us-east4-a, one c3-standard-192-metal bare-metal node, native KVM, 50 unsized workers, 1,000 agents each). Reference column: the same loop on the same bare-metal host with gVisor and durable **SuspendActor** (bucket). Substrate: main @ 01e299f9 (central) / perf-resume-latency @ b98e7189 (east; same code plus docs). Actor template 256 MiB; workers carry no CPU/memory requests or limits in these four runs.

| Step | gVisor c3-standard-4 · pause | microVM bare metal · pause | gVisor bare metal · pause | gVisor bare metal · suspend (reference) |
|---|---|---|---|---|
| Host, 3-yr CUD | c3-standard-4, $66.2/mo (cud3) · 4 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) |
| Workers per host → $ per worker-month | 2 → $26.49 | 50 → $63.58 | 50 → $63.58 | 50 → $63.58 |
| Agent profile | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill |
| Run | 200 agents, time ×1, 20 min; 1168 activations, 0 errors, 0 refusals | 1000 agents, time ×1, 20 min; 5659 activations, 0 errors, 0 refusals | 1000 agents, time ×1, 20 min; 5701 activations, 0 errors, 0 refusals | 1000 agents, time ×1, 20 min; 5407 activations, 0 errors, 0 refusals |
| Resume P50 / P90 / P99 | 157 / 188 / 215 ms (n=1168) ← T_r | 160 / 231 / 797 ms (n=5659) ← T_r | 275 / 318 / 400 ms (n=5701) ← T_r | 371 / 769 / 1,239 ms (n=5407) ← T_r |
| Park P50 / P90 / P99 (pause = node-local, suspend = bucket) | **pause** 120 / 139 / 161 ms (n=1168) ← T_s; avg 122 ms (driver, n=1168) | **pause** 214 / 577 / 3,688 ms (n=5659) ← T_s; avg 396 ms (driver, n=5659) | **pause** 241 / 292 / 388 ms (n=5701) ← T_s; avg 249 ms (driver, n=5701) | **suspend** 585 / 923 / 1,331 ms (n=5407) ← T_s; avg 645 ms (driver, n=5407) |
| Step work P50 / P90 / P99 | — | — | — | — |
| Worker time per wake-up | 0.3 s (wait + T_s + T_r) | 0.6 s (wait + T_s + T_r) | 0.5 s (wait + T_s + T_r) | 1.0 s (wait + T_s + T_r) |
| Occupancy (worker time per agent) | 0.14 %  (420 wakes/day of 1 ping; an actor's period = 20 × (0.3 s cycle + 10 s wait) = 206 s) | 0.26 %  (409 wakes/day of 1 ping; an actor's period = 20 × (0.6 s cycle + 10 s wait) = 211 s) | 0.25 %  (410 wakes/day of 1 ping; an actor's period = 20 × (0.5 s cycle + 10 s wait) = 210 s) | 0.46 %  (392 wakes/day of 1 ping; an actor's period = 20 × (1.0 s cycle + 10 s wait) = 220 s) |
| Measured density | 716.4:1 mean · 100.0:1 at P99 (busy workers mean 0.3, P99 2, peak 3 of 10) | 399.7:1 mean · 45.5:1 at P99 (busy workers mean 2.5, P99 22, peak 37 of 50) | 399.3:1 mean · 166.7:1 at P99 (busy workers mean 2.5, P99 6, peak 7 of 50) | n/a (no occupancy samples in the window) |
| Agents per worker (overcommit) | 257.7 = 0.70 ÷ (0.14 % × 2 peak) | 132.9 = 0.70 ÷ (0.26 % × 2 peak) | 140.4 = 0.70 ÷ (0.25 % × 2 peak) | 75.9 = 0.70 ÷ (0.46 % × 2 peak) |
| Agents per host | 644 | 6646 | 7022 | 3793 |
| $ / agent-month | $26.49 ÷ 257.7 = $0.10 compute + $0.00 GCS ops + $0.000 snapshots = **$0.10** | $63.58 ÷ 132.9 = $0.48 compute + $0.00 GCS ops + $0.000 snapshots = **$0.48** | $63.58 ÷ 140.4 = $0.45 compute + $0.00 GCS ops + $0.000 snapshots = **$0.45** | $63.58 ÷ 75.9 = $0.84 compute + $0.44 GCS ops + $0.000 snapshots = **$1.28** |
| Multi-actor projection (roadmap) | ≈1071 agents/host → ≈$0.06 | ≈38418 agents/host → ≈$0.08 | ≈29285 agents/host → ≈$0.11 | ≈19474 agents/host → ≈$0.60 |

## Reading the numbers

- **Pause is the fast path.** Node-local pause/resume on gVisor c3-standard-4: resume 157 / 188 / 215 ms, pause 120 / 139 / 161 ms. The same loop with durable suspend on bare-metal gVisor: resume 371 / 769 / 1,239 ms, suspend 585 / 923 / 1,331 ms. The bucket round trip is 2–3× on resume and 4–5× on the park.
- **microVM on bare metal with pause: resume 160 / 231 / 797 ms, pause 214 / 577 / 3,688 ms** (n = 5,659, zero errors). Median resume matches gVisor's on the small nodes; the tails are wider, and pause has a long tail (P99 3.7 s) where cycles land on a busy host (busy workers peaked at 37 of 50 while the mean was 2.5).
- **gVisor on the same bare-metal host with pause: resume 275 / 318 / 400 ms, pause 241 / 292 / 388 ms** (n = 5,701). Against microVM on the identical host and loop: resume P50 275 vs 160 ms, P99 400 vs 797 ms; pause P50 241 vs 214 ms, P99 388 vs 3688 ms. This is the apples-to-apples runtime comparison on bare metal.
- **Bare metal vs nested virtualization for microVM is still open:** no nested-virt microVM run of this ping+pause loop exists yet (the nested microVM measurement so far is the coding-session workload with suspend: resume 1,906 / 3,278 / 9,603 ms).
- **$ lines are for the loop as run and are not the point here.** Pause has no GCS operations and no snapshot at rest (node-local); "agents per host" extrapolates the measured per-worker share to the whole machine under the model's utilization and peak assumptions. The gVisor c3-standard-4 column's workers were spread over 4 nodes (2 per node), so its worker cost is $26.49 rather than the $13.25 of the earlier 2-node runs. The bare-metal columns price 50 workers on a 192-vCPU host that was otherwise empty.
- **Pause pins the actor to its node** and keeps a checkpoint on local disk (see the ladder section: ~230 MB per parked 256 MiB microVM actor; deleted actors' directories are not reclaimed).
- Excludes LLM tokens and the amortized cluster fee / control plane. Utilization 0.7 and peak ×2 are assumptions; T_s, T_r and density are measured.

Artifacts: `assets/runs/2026-10-05-ping-{gvisor-c3-standard-4-pause,microvm-c3-metal-pause,gvisor-c3-metal-suspend}/`, `assets/runs/2026-10-06-ping-gvisor-c3-metal-pause/`, ladder `assets/runs/2026-10-06-ping-ladder-microvm-c3-metal-pause/` (report.txt, latency.csv, summary.json, run.log, occupancy.csv, metrics.txt, waves.csv).

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

**gVisor c3-standard-4 · pause**
- cycle = 0.00 + 0.122 (pause, mean) + 0.157 (resume P50) = 0.28 s; period = 20 × (0.28 + 10) = 206 s; wakes/day = 86,400 ÷ 206 = 420
- occupancy = 420 × 0.28 ÷ 86,400 = 0.14 %; agents/worker = 0.70 ÷ (0.14 % × 2) = 257.7
- node c3-standard-4: $0.0907/h × 730 = $66/mo; 2 workers per node → worker $26.49/mo; compute = $26.49 ÷ 257.7 = $0.10
- pause: GCS ops $0, snapshot $0 → **$ / agent-month = $0.10**; agents per host at that share = 257.7 × 2 = 644

**microVM bare metal · pause**
- cycle = 0.00 + 0.396 (pause, mean) + 0.160 (resume P50) = 0.56 s; period = 20 × (0.56 + 10) = 211 s; wakes/day = 86,400 ÷ 211 = 409
- occupancy = 409 × 0.56 ÷ 86,400 = 0.26 %; agents/worker = 0.70 ÷ (0.26 % × 2) = 132.9
- node c3-standard-192-metal: $4.3547/h × 730 = $3,179/mo; 50 workers per node → worker $63.58/mo; compute = $63.58 ÷ 132.9 = $0.48
- pause: GCS ops $0, snapshot $0 → **$ / agent-month = $0.48**; agents per host at that share = 132.9 × 50 = 6,646

**gVisor bare metal · pause**
- cycle = 0.00 + 0.249 (pause, mean) + 0.275 (resume P50) = 0.52 s; period = 20 × (0.52 + 10) = 210 s; wakes/day = 86,400 ÷ 210 = 410
- occupancy = 410 × 0.52 ÷ 86,400 = 0.25 %; agents/worker = 0.70 ÷ (0.25 % × 2) = 140.4
- node c3-standard-192-metal: $4.3547/h × 730 = $3,179/mo; 50 workers per node → worker $63.58/mo; compute = $63.58 ÷ 140.4 = $0.45
- pause: GCS ops $0, snapshot $0 → **$ / agent-month = $0.45**; agents per host at that share = 140.4 × 50 = 7,022

**gVisor bare metal · suspend (reference)**
- cycle = 0.00 + 0.645 (suspend, mean) + 0.371 (resume P50) = 1.02 s; period = 20 × (1.02 + 10) = 220 s; wakes/day = 86,400 ÷ 220 = 392
- occupancy = 392 × 1.02 ÷ 86,400 = 0.46 %; agents/worker = 0.70 ÷ (0.46 % × 2) = 75.9
- node c3-standard-192-metal: $4.3547/h × 730 = $3,179/mo; 50 workers per node → worker $63.58/mo; compute = $63.58 ÷ 75.9 = $0.84
- GCS ops = 392 × 30.44 × (7 × $5e-6 + 4 × $4e-7) = $0.44; snapshot = 0.02 GiB × $0.02 = $0.000 → **$ / agent-month = $1.28**; agents per host at that share = 75.9 × 50 = 3,793

Two cautions on reading these dollar figures: the gVisor c3-standard-4 column's 10 workers sat on 4 nodes (2 per node) after the zone expansion, so its worker costs $26.49 rather than the $13.25 of a 5-per-node pool, and the bare-metal columns price 50 workers on a 192-vCPU host that was otherwise empty — in production the same host would carry several hundred workers, which is what the "agents per host" line extrapolates. The ladder section above is the measured, non-extrapolated view of that host.
