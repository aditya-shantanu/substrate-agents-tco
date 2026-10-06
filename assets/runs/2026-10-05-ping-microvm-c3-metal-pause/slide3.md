# $ / agent-month: how we get there — one-ping workload with PauseActor, measured 2026-10-05/06

Workload: the GluttonUser loop of substrate's benchmarking suite (the Prow 200K run's workload): a virtual user owns 20 actors and serves them one at a time — wake by ping through the router (implicit resume), **PauseActor** (node-local checkpoint) right after the ping, 10 s wait, next actor. No memory fill, no other work. Overcommit is 20:1 by construction; what is measured is the switch cost. Every latency as P50 / P90 / P99 with n.

Hosts: gVisor on `agents-tco` (us-central1, c3-standard-4 pool, 10 workers, 200 agents). microVM on `agents-tco-east` (us-east4-a, one c3-standard-192-metal bare-metal node, native KVM, 1 TB Hyperdisk boot, 50 workers, 1,000 agents). Reference column: the same loop on the same bare-metal host with gVisor and durable **SuspendActor** (bucket), run earlier the same day. Substrate: main @ 01e299f9 (central) / perf-resume-latency @ b98e7189 (east; same code plus docs).

| Step | gVisor c3-standard-4 · pause | microVM bare metal · pause | gVisor bare metal · suspend (reference) |
|---|---|---|---|
| Host, 3-yr CUD | c3-standard-4, $66.2/mo (cud3) · 4 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) | c3-standard-192-metal, $3,179.0/mo (cud3) · 1 node(s) |
| Workers per host → $ per worker-month | 2 → $26.49 | 50 → $63.58 | 50 → $63.58 |
| Agent profile | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill | one-ping GluttonUser loop: 20 actors/user, 10 s wait, 0 s live, no memory fill |
| Run | 200 agents, time ×1, 20 min; 1168 activations, 0 errors, 0 refusals | 1000 agents, time ×1, 20 min; 5659 activations, 0 errors, 0 refusals | 1000 agents, time ×1, 20 min; 5407 activations, 0 errors, 0 refusals |
| Resume P50 / P90 / P99 | 157 / 188 / 215 ms (n=1168) ← T_r | 160 / 231 / 797 ms (n=5659) ← T_r | 371 / 769 / 1,239 ms (n=5407) ← T_r |
| Park P50 / P90 / P99 (pause = node-local, suspend = bucket) | **pause** 120 / 139 / 161 ms (n=1168) ← T_s; avg 122 ms (driver, n=1168) | **pause** 214 / 577 / 3,688 ms (n=5659) ← T_s; avg 396 ms (driver, n=5659) | **suspend** 585 / 923 / 1,331 ms (n=5407) ← T_s; avg 645 ms (driver, n=5407) |
| Step work P50 / P90 / P99 | — | — | — |
| Worker time per wake-up | 0.3 s (wait + T_s + T_r) | 0.6 s (wait + T_s + T_r) | 1.0 s (wait + T_s + T_r) |
| Occupancy (worker time per agent) | 0.14 %  (420 wakes/day of 1 ping; an actor's period = 20 × (0.3 s cycle + 10 s wait) = 206 s) | 0.26 %  (409 wakes/day of 1 ping; an actor's period = 20 × (0.6 s cycle + 10 s wait) = 211 s) | 0.46 %  (392 wakes/day of 1 ping; an actor's period = 20 × (1.0 s cycle + 10 s wait) = 220 s) |
| Measured density | 716.4:1 mean · 100.0:1 at P99 (busy workers mean 0.3, P99 2, peak 3 of 10) | 399.7:1 mean · 45.5:1 at P99 (busy workers mean 2.5, P99 22, peak 37 of 50) | n/a (no occupancy samples in the window) |
| Agents per worker (overcommit) | 257.7 = 0.70 ÷ (0.14 % × 2 peak) | 132.9 = 0.70 ÷ (0.26 % × 2 peak) | 75.9 = 0.70 ÷ (0.46 % × 2 peak) |
| Agents per host | 644 | 6646 | 3793 |
| $ / agent-month | $26.49 ÷ 257.7 = $0.10 compute + $0.00 GCS ops + $0.000 snapshots = **$0.10** | $63.58 ÷ 132.9 = $0.48 compute + $0.00 GCS ops + $0.000 snapshots = **$0.48** | $63.58 ÷ 75.9 = $0.84 compute + $0.44 GCS ops + $0.000 snapshots = **$1.28** |
| Multi-actor projection (roadmap) | ≈1071 agents/host → ≈$0.06 | ≈38418 agents/host → ≈$0.08 | ≈19474 agents/host → ≈$0.60 |

## Reading the numbers

- **Pause is the fast path.** Node-local pause/resume on gVisor c3-standard-4: resume 157 / 188 / 215 ms, pause 120 / 139 / 161 ms. The same loop with durable suspend on bare-metal gVisor: resume 371 / 769 / 1,239 ms, suspend 585 / 923 / 1,331 ms. The bucket round trip is 2–3× on resume and 4–5× on the park.
- **microVM on bare metal with pause: resume 160 / 231 / 797 ms, pause 214 / 577 / 3,688 ms** (n = 5,659, zero errors). Median resume matches gVisor's; the tails are wider, and pause has a long tail (P99 3.7 s) — 20 % of cycles land on a busy host (busy workers peaked at 37 of 50 while mean was 2.5), which is where pause's CPU cost shows.
- **No nested-virt microVM ping run exists yet** (the nested-virt microVM measurement today was the coding-session workload with suspend: resume 1,906 / 3,278 / 9,603 ms). The bare-metal-vs-nested comparison for microVM needs the same ping+pause loop on the c3-standard-4 pool next.
- **$ lines are for the loop as run and are not the point here.** Pause has no GCS operations and no snapshot at rest (node-local), hence the lower totals; the "agents per host" extrapolates the measured per-worker share to the whole machine. The gVisor c3-standard-4 column's workers were spread over 4 nodes (2 per node) after the zone expansion, so its worker cost is $26.49 rather than the $13.25 of the earlier 2-node runs.
- **Pause pins the actor to its node** (resume must happen where the checkpoint lives). On a single-node bare-metal cluster that costs nothing; on a multi-node pool it limits placement, which the suspend path does not.
- Excludes LLM tokens and the amortized cluster fee / control plane. Utilization 0.7 and peak ×2 are assumptions; T_s, T_r and density are measured.

Artifacts: `assets/runs/2026-10-05-ping-{gvisor-c3-standard-4-pause,microvm-c3-metal-pause,gvisor-c3-metal-suspend}/` (report.txt, latency.csv, summary.json, run.log, occupancy.csv, metrics.txt).
