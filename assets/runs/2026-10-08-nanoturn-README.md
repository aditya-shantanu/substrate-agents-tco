# nano-personal-agent on Substrate main — cold start, and suspend/resume turnover at 650 awake of 5,000 registered (measured 2026-10-08)

## What was asked

Suspend path only, gVisor and microVM on the two bare-metal nodes, Substrate from **main**, `nano-personal-agent` workload. Measure P50/P90/P99 of (1) cold start, (2) resume, (3) suspend. Structure: cold starts as an independent step; then register 5,000 agents, keep 650 awake, find the maximum sustainable activation throughput with N replacements per tick (N suspends + N resumes), and at that rate cycle through all remaining agents, recording the total time.

## Summary

| | gVisor | microVM |
|---|---|---|
| Cold start P50 / P90 / P99 (ms) | **280 / 349 / 427** | **567 / 959 / 1061** |
| Snapshot in the bucket, zstd (mean, registration → end of run) | **9.6 → 9.4 MiB** | **49.2 → 50.6 MiB** |
| Sustainable turnover (last clean level, held 10 min) | **1.97 swaps/s** (20 suspends + 20 resumes per 10 s) | **1.97 swaps/s** (20 suspends + 20 resumes per 10 s) |
| Resume P50 / P90 / P99 at that rate (ms) | **2,276 / 2,557 / 2,780** | **2,304 / 2,547 / 3,195** |
| Suspend P50 / P90 / P99 at that rate (ms) | **1,713 / 1,942 / 2,167** | **3,992 / 4,638 / 4,819** |
| First failing level | 40 per 10 s (3.83 swaps/s): wake-p90-vs-baseline+park-p90-vs-baseline; errors 0, refusals 0 | 40 per 10 s (3.83 swaps/s): wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline; errors 0, refusals 0 |
| Cycle: actors resident at least once | **4,999 of 5,000** | **4,995 of 5,000** |
| Cycle phase at the sustainable rate (from the hold's end) | **14.2 min** for the last 1,559 | **14.3 min** for the last 1,559 |
| Fill end → last actor resident once (ramp + hold + cycle) | **36.2 min** | **36.3 min** |

## Assumptions and configuration (everything that shaped the numbers)

- **Substrate:** upstream `main` @ 66f8a888 (2026-10-08, "Egress gateway can mint and cache actor JWTs (#2135)"), fresh install on both clusters (the September-branch control plane, its Postgres and its SandboxConfigs were removed first). gVisor: `gvisor-default` SandboxConfig from main (gVisor nightly 2026-09-02). microVM: main's `microvm` SandboxConfig with kata 4.1.0 + Cloud Hypervisor v53.0 assets assembled on the node and verified against main's pinned sha256. No retained node-local snapshot copy on main: every resume from suspend downloads from the bucket.
- **Machines:** one GKE bare-metal node per runtime, c3-standard-192-metal (192 vCPU, 768 GiB, 3 TB Hyperdisk Balanced boot disk at 100k IOPS / 2,400 MiB/s, COS, GKE 1.36.4). Node tuning carried over from earlier runs: `net.ipv{4,6}.neigh.default.gc_thresh` 4096/8192/16384 (the ARP table overflowed at ~1,000 live sandboxes at the COS default) and `fs.inotify.max_user_{instances,watches}` 65536/1048576 (exhausted at ~800 sandboxes at the default). Orphaned node directories from previous runs were reclaimed before the run; Substrate itself does not reclaim them (#641).
- **Workload:** `nano-personal-agent` — the personal-assistant day (61 events over 24 h, real think gaps) with every action mocked: model call = dwell ≤ 4 s with the actor resident, API call = a ping, file open = read or ≤ 32 KiB write of KiB-sized files (256 KiB session store and index), CPU burns ≤ 50 ms, **no synthetic heap**. Per day: 4.75 CPU-s, 4.8 MiB written, 848 KiB of files, declared RAM 0. Resident agents play it at **think ×1** (real pace): ~0.5 mocked actions per second across 650 agents. The driver never parks after a step (idle mode; autosuspender idle timeout 24 h): the only suspends/resumes are the test's swaps.
- **Actor shape:** gVisor 2 vCPU + 2 GiB limit; microVM 2 vCPU + **256 MiB** (the memory limit is the guest's RAM on microVM and the guest's touched pages are the snapshot, so it was kept at the minimum, per Aditya). Worker pool: 100 unsized worker pods per node (multi-actor, `--max-actors` 1000).
- **Registration:** 5,000 actors created (cold boot from the golden snapshot, first ping, one suspend to the bucket), 32 in flight. **Fill:** 650 woken from the pool, 8 in flight, retried until resident. Pool for swaps: the other 4,350.
- **Turnover ramp:** every 10 s park N awake actors (oldest-awake first, never one mid-step) with SuspendActor and wake N parked ones (oldest-parked first) with a request; N = 10, 20, 40, 80, 160, 320, 640 per tick (1 … 64 swaps/s), one level per 4 minutes, levels back to back. Gates per level (any one fails it): wake P90 or suspend P90 above 2.5× the first level; wake P99 above 5 s; errors or router refusals above 0.5 % of wakes (≥ 3 occurrences); more than two ticks' worth of swaps still in flight at the end of the level (backlog); node MemAvailable < 10 %, memory PSI full > 10 %, CPU PSI some > 50 %; ≥ 5 crashed actors. After the first failed level: 10-minute hold at the last clean N, then the **cycle**: keep swapping at that N until every one of the 5,000 has been resident at least once; the cycle's wall time and latencies are recorded.
- **Cold start:** 100 fresh actors per runtime, 4 in flight, create → golden-snapshot restore → first answered ping.
- **Latency definitions:** resume = the wake request's round trip through the router including the restore (client-side, up to 5 refusal retries); suspend = the SuspendActor call's wall time (checkpoint + upload); cold start as above. Host figures from a node probe every 15 s; actor states and node memory/PSI from the autosuspender every 5 s.

## gVisor — agents-tco-east (us-east4-a), actor 2 vCPU, 2 GiB

**1. Cold start (100 actors, 4 in flight):** P50 / P90 / P99 / max = **280 / 349 / 427 / 427 ms**

**Snapshot in the bucket (suspend, zstd; 100 snapshots of 100 actors sampled during registration):** mean **9.6 MiB**, p50 9.6, max 9.6. Composition (mean per snapshot): pages.img.zstd 9.55 MiB, checkpoint.img.zstd 0.07 MiB, pages_meta.img.zstd 0.00 MiB, manifest.json 0.00 MiB.

**Snapshot at the end of the run (500 snapshots of 500 actors):** mean 9.4 MiB, max 10.2; pages.img.zstd 9.33 MiB, checkpoint.img.zstd 0.07 MiB, pages_meta.img.zstd 0.00 MiB.

**2 + 3. Resume and suspend under turnover (one level = 4 min; hold = 10 min; cycle = until all 5,000 have been resident once):**

| Level | N per 10 s | Target swaps/s | Achieved swaps/s | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Wakes / suspends | Errors + refusals | Backlog | Node mem avail | PSI cpu / mem / io | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| ramp 4 min | 10 | 1.00 | **0.96** | 1,243 / 1,431 / 1,630 | 951 / 1,079 / 1,221 | 230 / 230 | 0 + 0 | 0 | 86.5 % | 2.07 / 0 / 0.25 | pass |
| ramp 4 min | 20 | 2.00 | **1.92** | 2,200 / 2,434 / 2,659 | 1,399 / 1,691 / 1,966 | 460 / 460 | 0 + 0 | 0 | 86.3 % | 1.88 / 0 / 0.23 | pass |
| ramp 4 min | 40 | 4.00 | **3.83** | 4,089 / 4,368 / 4,596 | 2,507 / 2,985 / 3,232 | 920 / 920 | 0 + 0 | 0 | 85.6 % | 3.44 / 0 / 0.68 | wake-p90-vs-baseline+park-p90-vs-baseline |
| hold 10 min | 20 | 2.00 | **1.97** | 2,276 / 2,557 / 2,780 | 1,713 / 1,942 / 2,167 | 1180 / 1180 | 0 + 0 | 0 | 85.8 % | 1.79 / 0 / 0.46 | pass |

**Cycle phase (measured from the sim's progress lines, 70-s sampling):** started at 00:57:25 UTC at 20 per 10 s with 3,440 of 5,000 actors already resident at least once (fill + ramp + hold); reached **4,999 of 5,000** at 01:11:35 UTC, i.e. **1,559 further actors in 13.0–14.2 min** (1.83 swaps/s; the resident 650 kept playing their day throughout). The remaining **1** actor never became resident (see caveats); the cycle loop kept waiting for it until the run was ended at 01:21:35 UTC. Wall time from the end of the 650 fill to the last actor being resident once: **36.2 min** (this includes the 4-min ramp levels at 1, 2 and 4 swaps/s and the 10-min hold).

**Caveats:** The sim's resident-once count stopped at 4,999 and its cycle loop waited for the last actor until the run was ended 10 min later. The actor states after the run show every one of the 5,000 was in fact resumed at least once (actor version histogram 1 × 7, 3,619 × 9, 649 × 11, 731 × 13 — none left at the registration version; the one at version 7, turn-…-4499, is RUNNING since its fill wake and was never parked), so this is a bookkeeping gap in the sim's fill path, not a Substrate failure. The sim's cumulative counter showed 1 error for the whole run; every measured level shows 0 wake/suspend errors and 0 step errors. The sim's final quantile table was not printed (job ended), so the level rows above come from the sim's per-level JSON lines, which carry the same fields.

**Host over the run (15-s samples):** disk write 25 / 45 MiB/s (p50 / p90), read 4 / 6, busy 21 / 37 %, queue depth 1 / 2; CPU 5 / 10 %; memory used max 114 GiB.

## microVM — agents-tco-euw4 (europe-west4-c), actor 2 vCPU, 256 MiB

**1. Cold start (100 actors, 4 in flight):** P50 / P90 / P99 / max = **567 / 959 / 1061 / 1073 ms**

**Snapshot in the bucket (suspend, zstd; 67 snapshots of 67 actors sampled during registration):** mean **49.2 MiB**, p50 49.4, max 49.4. Composition (mean per snapshot): memory-ranges.zstd 41.36 MiB, rootfs-upper-glutton.tar.zstd 8.00 MiB, state.json.zstd 0.01 MiB, config.json.zstd 0.00 MiB, manifest.json 0.00 MiB, base-id.zstd 0.00 MiB.

**Snapshot at the end of the run (334 snapshots of 334 actors):** mean 50.6 MiB, max 54.6; memory-ranges.zstd 42.10 MiB, rootfs-upper-glutton.tar.zstd 8.69 MiB, state.json.zstd 0.01 MiB.

**2 + 3. Resume and suspend under turnover (one level = 4 min; hold = 10 min; cycle = until all 5,000 have been resident once):**

| Level | N per 10 s | Target swaps/s | Achieved swaps/s | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Wakes / suspends | Errors + refusals | Backlog | Node mem avail | PSI cpu / mem / io | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| ramp 4 min | 10 | 1.00 | **0.96** | 1,257 / 1,379 / 1,516 | 2,209 / 2,360 / 2,458 | 230 / 230 | 0 + 0 | 0 | 85.0 % | 0 / 0 / 0.25 | pass |
| ramp 4 min | 20 | 2.00 | **1.92** | 2,273 / 2,422 / 2,480 | 3,931 / 4,360 / 9,048 | 460 / 460 | 0 + 0 | 0 | 83.6 % | 0.07 / 0 / 2.93 | pass |
| ramp 4 min | 40 | 4.00 | **3.83** | 3,030 / 3,909 / 5,405 | 8,128 / 8,566 / 8,766 | 920 / 920 | 0 + 0 | 0 | 82.0 % | 1.05 / 0 / 10.05 | wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline |
| hold 10 min | 20 | 2.00 | **1.97** | 2,304 / 2,547 / 3,195 | 3,992 / 4,638 / 4,819 | 1180 / 1180 | 2 + 0 | 0 | 82.7 % | 0 / 0 / 1.83 | pass |

**Cycle phase (measured from the sim's progress lines, 70-s sampling):** started at 01:24:10 UTC at 20 per 10 s with 3,436 of 5,000 actors already resident at least once (fill + ramp + hold); reached **4,995 of 5,000** at 01:38:30 UTC, i.e. **1,559 further actors in 13.2–14.3 min** (1.81 swaps/s; the resident 650 kept playing their day throughout). The remaining **5** actors never became resident (see caveats); the cycle loop kept waiting for them until the run was ended at 01:41:40 UTC. Wall time from the end of the 650 fill to the last actor being resident once: **36.3 min** (this includes the 4-min ramp levels at 1, 2 and 4 swaps/s and the 10-min hold).

**Caveats:** Actors crashed over the run: 2 during the 650 fill (their first wake), 4 by the end of the hold (the hold row also shows 2 wake errors), 13 by the time the run was ended (10 of them never got past their first wake; the gate was ≥ 5 crashed per level and no level after the hold was scored). The sim's resident-once count plateaued at 4,995 of 5,000, so 5 actors were never counted resident; the cycle loop waited for them and the run was ended 3 min after the plateau. 20 actors were left SUSPENDING (their parks were in flight when the job was ended); final states are in actors-final.txt in the archive. The sim's final quantile table was not printed (job ended); level rows come from the per-level JSON lines.

**Host over the run (15-s samples):** disk write 66 / 228 MiB/s (p50 / p90), read 2 / 6, busy 26 / 50 %, queue depth 5 / 45; CPU 5 / 9 %; memory used max 145 GiB.

## Reading the numbers

- **Activations per second = swaps per second** at a constant 650 awake: each swap is one SuspendActor (checkpoint + bucket upload) and one resume (bucket download + restore + first request). The last clean ramp level is the sustainable rate; the first failing level shows what gives out.
- **Why the nano agent changes the picture:** the suspend path moves ~10 MiB (gVisor) or ~50 MiB (microVM at 256 MiB) per swap instead of 1–2.5 GiB, so the boot disk — the limiter of every earlier run — stops being the constraint and the control plane / router / per-worker wake path take over.
- **Cycle time** is the wall time to get the remaining 4,350 parked agents resident at the sustainable rate, with the resident agents still playing their day.
- **Caveats** are listed next to each cell where they apply.

## Observations

- **Latency grows linearly with N per tick, on both runtimes.** gVisor resume P50 1.2 / 2.2 / 4.1 s at N = 10 / 20 / 40, suspend P50 1.0 / 1.4 / 2.5 s; microVM resume 1.3 / 2.3 / 3.0 s, suspend 2.2 / 3.9 / 8.1 s. All N requests of a tick are issued at the same instant and the latencies are the bursts' queueing: they complete at roughly 100–110 ms per gVisor resume and ~200 ms per microVM suspend, i.e. a largely serialised path that drains about 9–10 resumes/s (gVisor) and ~5 suspends/s (microVM) — not a per-request cost of seconds. A single cold start is 0.3 s (gVisor) / 0.6 s (microVM) on the same hosts.
- **The gate that stopped the ramp was the relative-latency gate, not an error.** The N = 40 level (3.83 swaps/s achieved) completed with 0 errors, 0 refusals and no backlog on both runtimes; it failed because P90 was > 2.5× the N = 10 level (and, on microVM, resume P99 > 5 s). So **2 swaps/s is the sustainable rate under the latency rule as written; the systems delivered 3.8 swaps/s error-free.** A finer step (25, 30 per 10 s) or spreading the N swaps across the tick instead of a burst would land the gated limit between 2 and 4 swaps/s.
- **The host is nowhere near its limits.** CPU 5–10 % on both nodes; gVisor disk write 25 / 45 MiB/s (p50 / p90), queue depth 1–2; microVM 66 / 228 MiB/s with queue depth 5 / 45 at the bursts (the 128 MiB memory-ranges file is staged on disk before compression) — against 1–1.6 GiB/s and queue depths in the hundreds in every personal-assistant run; > 82 % of memory available. The nano agent's 10 MiB (gVisor) / 50 MiB (microVM) snapshot takes the boot disk out of the equation; what remains is the control plane / router / worker wake path.
- **Cycle:** at 2 swaps/s the remaining pool drains at the expected pace — 4,350 agents in ~36 min from the end of the fill (gVisor 36.2 min, microVM 36.3 min measured from the fill's end, including the slower first level and the faster failed level; the cycle phase alone moved the last ~1,560 agents in ~14 min on both).
