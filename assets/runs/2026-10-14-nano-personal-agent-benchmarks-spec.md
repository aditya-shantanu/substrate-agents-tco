# The nano-personal-agent benchmarks: what we ran and how to reproduce them as standing benchmarks

This describes the five tests we ran with the nano-personal-agent actor on Substrate between October 8 and 10, 2026, in enough detail to implement each one as a benchmark. For each test: the question it answers, the setup, the procedure step by step, what to record, the pass/fail rules, the numbers we got (so a new implementation can be checked against them), and the pitfalls that cost us runs. The reference implementation is the `agentsim` driver and `experiment/run.sh` in https://github.com/aditya-shantanu/substrate-agents-tco; file pointers are given where useful, but nothing below depends on that code.

## 1. The workload

**Idea.** The smallest always-on personal agent that still has a day. It keeps the 24-hour event structure of the personal-assistant script from substrate#2230 (one lap = one agent-day, steps named by time of day, real think gaps between them) but every action is a mock:

- a model call is a dwell of up to 4 s with the actor resident;
- an API call is a ping through the router;
- a tool or file operation is a read, a digest, or a write of a KiB-sized file (32 KiB cap, except the 256 KiB session store and memory index);
- CPU work is a 50 ms burn;
- there is no synthetic heap. The resident set is whatever the sandbox plus a tiny HTTP server need.

**Numbers.** 61 steps per day (the gateway start at 00:00, the nightly consolidation at 03:00, a catalog refresh, 30 user messages in 7 clusters, heartbeats, crons, briefing, compaction). Per agent-day: about 4.75 CPU-seconds, 4.8 MiB written, 848 KiB of files, zero declared RAM. The script is `service/internal/agentscript/scripts/nano-personal-agent.yaml` (`min_actor_memory: 64Mi`).

**Think scale.** The gaps between steps sum to 24 h. A think scale compresses them: ×1 plays a real day; ×0.02 plays a day in about 29 minutes (one event every ~28 s per agent). Use ×1 when the actors should be mostly idle (turnover tests) and ×0.02 when they must generate load during a short window (resident fills).

**Actor shape used.** 2 vCPU everywhere. microVM: 256 MiB guest. gVisor: 2 GiB limit (gVisor only backs touched pages, so this costs nothing; it leaves checkpoint headroom).

**Sizes measured (Substrate main 66f8a888).** Snapshot in the bucket after zstd: gVisor 9.6 MiB, microVM 49 MiB (41 MiB memory image + 8 MiB rootfs upper layer). Raw node-local pause checkpoint: gVisor 15 MiB, microVM 136 MiB (128 MiB memory image, constant: every guest page ever touched). Host memory per resident sandbox: gVisor 59 MiB, microVM 127 MiB.

## 2. Common setup

- **Node.** One c3-standard-192-metal per runtime (192 vCPU, 768 GiB, native KVM, 3 TB Hyperdisk Balanced at 100k IOPS / 2,400 MiB/s, COS, GKE 1.36.4, `--max-pods-per-node 256`). gVisor and microVM each get their own node and cluster so they run in parallel.
- **Substrate.** Fresh install of the build under test; record the commit in the report title. Our runs: main 66f8a888, then the same plus experimental patches.
- **Worker pool.** 100 worker pods, no CPU or memory requests or limits on the pods (unsized), multi-actor (`--max-actors 1000`). Keep enough pods that no single pod absorbs more than a few dozen simultaneous wakes: one pod caps out at 50 to 150 concurrent wakes on the worker row lock, which is not a node limit.
- **Node preparation (lost on reboot, re-apply after every reboot):** `net.ipv4/ipv6.neigh.default.gc_thresh1/2/3` = 4096 / 8192 / 16384 (the COS default of 1,024 overflows at about 1,000 live sandboxes and looks like a capacity failure); `fs.inotify.max_user_instances` 65536 and `max_user_watches` 1048576 (exhausted at about 800 sandboxes). Reclaim orphaned `/var/lib/ate/actors/<uid>` directories before a campaign, never right before a measurement (Substrate does not reclaim them, upstream #641).
- **Registration.** Create every actor of the fleet with a fresh name prefix per campaign (reusing a name makes CreateActor return AlreadyExists and the boot is silently skipped), 32 creations in flight: cold boot from the golden snapshot, first ping, one park (suspend or pause, matching the test). 5,000 actors take about 20 minutes. Reuse the registered fleet across iterations (park everything, then wake the number you want) rather than re-registering; exclude actors in DELETING or CRASHED from reuse, they answer 503 to every wake.
- **Router budget.** The router parks a request to a waking actor for at most 5 s, then returns 503. Any test that wakes a burst will see refusals once wake P99 crosses 5 s; this is a property of the router, not of the node. Keep it in mind when choosing wave sizes.
- **Latencies.** Every latency is recorded as P50 / P90 / P99 (resume first), measured at the client: resume = the time from the request that triggers the wake to its answer; suspend or pause = the lifecycle call's duration.
- **Node probe.** A privileged pod sampling `/proc/meminfo`, `/proc/stat`, `/proc/pressure/*`, `/proc/diskstats` every 15 to 30 s during the run; pod and worker logs are collected from `/var/log/pods` at the end because kubelet rotates large logs and `kubectl logs` can return nothing.

## 3. Benchmark A: cold start

**Question.** How long from CreateActor to the first answered request, per runtime and per node type.

**Procedure.** 100 new actors per scenario, 4 in flight, fresh name prefix. For each actor: CreateActor, then the first request through the router (which triggers the golden-snapshot restore), then the answer. Record the three components: CreateActor, first ResumeActor (golden restore), resume to first answered ping.

**Record.** Ready time P50 / P90 / P99 / max and the three components; failures and retries (must be zero).

**Reference.** gVisor metal 280 / 349 / 427 ms; microVM metal 567 / 959 / 1,061 ms (nano agent, main 66f8a888). Nested-virt c3-standard-4: gVisor 274 / 355 / 586, microVM 710 / 1,268 / 1,752 (ping actor). Cold start is the golden restore; the first ping adds about 15 ms.

**Pitfalls.** Node-level first-time costs (image pull, golden snapshot cache) must be excluded: warm the node with a few actors first. The microVM golden snapshot built by a previous worker image can hang 300 s in the wake-up probe after a worker image change; regenerate the golden snapshot with the worker image under test.

## 4. Benchmark B: snapshot size through the day

**Question.** Does the memory image stay under 200 MiB uncompressed and 50 MiB compressed, and what grows over a day.

**Procedure.** Two cohorts of 10 actors, 256 MiB limit, think ×0.01 (a day in about 15 minutes). The actors do not park between steps; the driver parks them only at snapshot points after steps 3, 10, 20, 30, 40, 50 and 61 (`--script-snapshot-steps`). Cohort A parks with PauseActor (raw node-local checkpoint), cohort B with SuspendActor (zstd in the bucket). A floor cohort of 5 actors running the benchmark `sleep` template gives the sandbox-only size. After each snapshot point, list the checkpoint files (node directory or bucket prefix) and record size and composition (pages / memory-ranges, checkpoint state, rootfs upper layer).

**Record.** Per snapshot point: n, mean, P50, max, per-file breakdown; the growth from first to last point.

**Reference.** gVisor raw pause image 13.6 → 15.2 MiB over the day (pages.img 13.3 → 14.9). microVM raw 136.1 → 136.9 MiB (memory-ranges constant at 128 MiB, rootfs tar 8.0 → 8.8). Bucket suspend sizes: gVisor 9.6 → 9.4 MiB, microVM 49.2 → 50.6 MiB.

**Pitfalls.** Capture the step index together with each snapshot while the fleet exists; we lost the step labels of one run with the pod log.

## 5. Benchmark C: suspend/resume turnover at a fixed fill

**Question.** With N agents awake out of a registered fleet, how many activations per second does the node sustain, and at what resume and suspend latency. One activation = one actor resumed from its snapshot and answering a request; the test suspends one actor for every one it resumes, so activations/s = swaps/s = resumes/s = suspends/s.

**Setup.** Fleet 5,000 registered and parked (suspend), 650 awake playing the day at think ×1 (idle most of the time), lifecycle mode idle (autosuspender timeout 24 h so nothing parks on its own), 100 worker pods.

**Procedure.**
1. Fill: wake 650 actors, 8 in flight, retry until resident; give up on actors whose wake error is permanent.
2. Levels: at each level, every second park `r` idle awake actors (SuspendActor) and wake `r` parked ones through a request, where `r` is the level's rate in swaps/s (fractional rates spread over ticks). Only idle actors are parked; an agent whose next step is due waits until it is woken. Levels last 2 minutes; the first level is the baseline for the relative gates. Ramp ×1.2 to ×1.5 per level, and start two or more levels below the suspected ceiling because the first level is also a cold start of the fleet.
3. Gates, evaluated at the end of each level: resume P90 or suspend P90 above 2.5× the first level's; errors or refusals above 0.5 % of wakes (with at least three occurrences); more than 5 seconds' worth of swaps still in flight at level end (backlog); node MemAvailable < 10 %; memory PSI full > 10 %; CPU PSI some > 50 %; crashed actors above a count (20). The first failing level ends the ramp.
4. Hold: 4 to 10 minutes at the last clean rate. The hold is the number to report; a hold right after a failed level is contaminated by its backlog, so let it drain or skip it.
5. Cycle (optional): keep swapping at the last clean rate until every actor of the fleet has been resident at least once; report the wall time (5,000 actors: 36 minutes end to end at 2 swaps/s on main).

**Record per level.** Target and achieved swaps/s; resume and suspend P50 / P90 / P99; wakes and suspends counted; errors and refusals; backlog; node memory available, PSI cpu/mem/io, disk write MB/s and busy %; crashed actors; the gate that fired. Plus per-stage timings from the worker and node-agent logs ("Restore timing breakdown" / "Checkpoint timing breakdown" lines) at each level: manifest fetch, download, network setup, sandbox create, page or VM restore, checkpoint, upload, teardown. The stage table is what tells you where the time went.

**Reference (main 66f8a888 unmodified, 1-s ticks).** 5 swaps/s on both runtimes. 10-s burst ticks gave 2, which was the burst, not the system. After the optimization campaign: microVM 17 swaps/s within the rule (resume 1,298 / 1,397 / 1,560 ms, suspend 1,092 / 1,345 / 1,615, 0 errors), 20 collapses; gVisor 9 within the rule (959 / 1,194 / 1,449; 589 / 742 / 932), 12 error-free.

**Pitfalls, each of which invalidated at least one run.**
- Issuing N swaps at once every 10 s instead of steadily: latency then grows linearly with N and the relative gate measures burst size.
- The driver replaying every skipped step on an agent's first wake (a "catch-up"): about 46 requests per first wake, a storm proportional to the swap rate. The driver's first wake must do the day's state-building ops once, cheaply, or nothing.
- A readiness check that redeploys the workloads rolls the worker pool and crashes every awake actor.
- Crash gate of 1 to 5 trips on leftovers from earlier runs; count crashes per level, or raise the gate.
- Reclaiming hundreds of GB of orphan directories right before a run: the filesystem's background work fails the next level.
- Two actors stuck in DELETING (crashed actor whose worker pod is gone) answer 503 to every wake; exclude them.

## 6. Benchmark D: pause/resume turnover

**Question.** Same as C with PauseActor: checkpoints stay on the node, no bucket and no transfer.

**Differences from C.**
- Register the fleet in pause mode under its own prefix (every actor needs a local checkpoint). 5,000 microVM actors write 580 GB at about 1 GB/s; let that storm finish before measuring (ten minutes with the disk 93 % busy).
- Disk: 5,000 × 119 MB must fit (tmpfs does not). The boot-disk I/O scheduler must not be `bfq` (its per-process idling starves the many short I/O streams of VM start-up; restores timed out on virtiofsd sockets with the disk 10 % busy). Use `none`, read-ahead 1 MiB.
- Park the fleet with PauseActor before any pool roll or disk migration; parking with SuspendActor sends the actors to the bucket and their next wakes become downloads.
- Record the disk: write and read MB/s, busy %, queue depth, IO PSI per level, and the page cache (5,000 local checkpoints = 591 GB of page cache; memory PSI full rises even when idle).

**Reference (microVM, patched worker).** Boot disk: 8 swaps/s within the 2.5× rule, 12 delivered (resume 321 / 509 / 646 ms, pause 301 / 453 / 583), 15 collapses on the disk (95 % busy). Hyperdisk Extreme: 15 delivered at 798 / 897 / 946 (pause 322 / 508 / 671) with 3 % errors and refusals from per-VM start-up failures. gVisor pause turnover was not run with the nano agent.

## 7. Benchmark E: resident fill (how many run at the same time)

**Question.** How many nano agents one node keeps awake and responsive with no parking at all, and what fails first.

**Setup.** Fleet registered and parked: 9,000 gVisor (needed more than 5,000), 5,000 microVM. Lifecycle mode none (autosuspender off, 24 h). Think ×0.02 so the agents generate load: at ×1 an agent makes no request within a 4-minute wave and nothing is measured. Actor shapes as above; set an explicit CPU limit per actor.

**Procedure.**
1. Setup wakes the starting count (we used 500 to 1,150) and measures the first wave as the baseline.
2. Every interval, wake a wave of W more agents (all requests issued together); they stay resident and play the day. Window = the interval. We used W = 500 every 4 minutes to find the region, then 200 to 250 every 2 to 3 minutes near the ceiling. On gVisor, waves above 200 to 250 exceed the router's 5-s budget on the tail of the burst and produce refusals that are not a capacity signal.
3. Gates per wave: node MemAvailable < 10 %; memory PSI full above 10 % (30 % if the page cache is dirty from a previous test); CPU PSI some > 50 %; CPU-probe P99 > 1 s (a small CPU burn against a sample of resident agents, measures scheduling); turn P99 (a normal request to a resident agent) above the gate; wake P99 above the gate; wake P90 or turn P90 above 2× the first wave's; errors or refusals above the gate; crashed actors above a count. We ran the strict gates (turn P99 1 s, wake P99 10 s, errors and refusals 0.5 %) and then relaxed them to 30 s, 60 s and 5 % because the wake burst dominates those three; the probe, memory, PSI and 2× rules did the work.
4. Hold 10 minutes at the last clean count; park the agents of the failed wave first.
5. Extension (optional): continue from the last clean count with the 2×-first-wave rules off and absolute gates only (probe and turn P99 1 s, memory 10 %, PSI, errors 5 %) to find the hard wall behind the soft one.

**Record per wave.** Awake agents; sandboxes actually resident on the node (counts leftovers from earlier runs, use it for the memory slope); wake P50 / P90 / P99 of the wave; turn P90 / P99 and CPU-probe P50 / P90 / P99 over the window; errors and refusals; MemAvailable %; PSI cpu/mem/io; crashed; verdict. From the node probe: CPU busy split user/sys, load average, memory used; derive host memory per sandbox as the slope of memory used against resident sandboxes over the clean waves.

**Reference.** gVisor 5,950 within the rules (stopped by turn P90 > 2× baseline with 52 % memory free and the node 33 % busy); extension 7,550 passed waves, 7,750 failed on 9 % wake 502s, hold at 7,550 failed (P99 about 2 s, node 54 to 76 % busy with kernel time above user time). microVM 4,250 within the rules, 4,500 fails on memory < 10 % (127 MiB per VM). Earlier references with other actors on the same nodes: personal assistant (1 GiB resident) 650 on both runtimes; ping actor gVisor ≥ 4,000, microVM 3,550.

**Pitfalls.** The ARP neighbour-table wall at about 1,000 to 1,200 live gVisor sandboxes looks exactly like a capacity failure (client timeouts with memory free) and comes back after a reboot. 591 GB of local pause checkpoints in the page cache trip the memory-PSI gate before the agents do; drop caches or raise the gate. A node reboot during registration (ours, cause unknown) leaves the pod network to the workers dead until worker pool, router and egress are restarted, and leaves hundreds of actors CRASHED.

## 8. What a report of each benchmark should contain

1. Title with workload, runtimes, node type, Substrate commit and date.
2. A single result table first: the headline number per runtime, the first failing level or wave and the gate that fired, latencies at the headline level as P50 / P90 / P99, errors and refusals, node memory and CPU at the ceiling.
3. The changes made to the method or the system during the campaign and why.
4. Where the bottleneck is now, with the per-stage timings or node counters that show it.
5. The run log (every attempt, including the invalid ones, with what spoiled them).
6. The level-by-level or wave-by-wave tables and the raw data directory.

The October 2026 reports of each benchmark are in `assets/runs/` of the TCO repo: `2026-10-08-nanoturn-README.md` (A, B, C baseline), `2026-10-09-throughput-campaign/README.md` (C after optimization, with the stage tables), `2026-10-09-pause-campaign/README.md` (D), `2026-10-10-nano-resident-fill/README.md` (E).
