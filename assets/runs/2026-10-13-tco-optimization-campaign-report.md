# Substrate TCO campaign, October 2 to 10, 2026: what we changed, what we measured, where the code is

This is the shareable account of the Substrate total-cost-of-ownership (TCO) work: the measurement series on bare metal, the optimization campaigns that followed, every software change made along the way with its commit or patch, and the numbers before and after. Everything here is about the Agent Substrate actor lifecycle (suspend, pause, resume, resident capacity). Nothing from the RL sandbox experiments is included.

Two repositories hold the work:

| Repository | What is in it | Link |
|---|---|---|
| `aditya-shantanu/substrate` (fork of `agent-substrate/substrate`) | Substrate source changes: the `perf/resume-latency` branch (committed) and the October 8 to 9 campaign patches (archived as patch files, see section 3.2) | https://github.com/aditya-shantanu/substrate |
| `aditya-shantanu/substrate-agents-tco` | The benchmark harness (`agentsim`, `experiment/run.sh`, workloads), every run's data under `assets/runs/`, and every report | https://github.com/aditya-shantanu/substrate-agents-tco |

All Substrate changes are experimental. None of them is on `agent-substrate/substrate` main. Section 8 lists what each one needs before it could be proposed upstream.

---

## 1. Headline: before and after

| What | Before | After | Where it came from |
|---|---|---|---|
| gVisor suspend/resume wake, 1 worker node, 100 agents (WakeFirstTouch P50 / P99) | 1,200 / 3,400 ms | 190 / 340 ms | retained node-local snapshot copy (commit 0ccab235) |
| gVisor suspend/resume wake, 3 worker nodes | 730 / 2,100 ms | 180 / 240 ms | retained copy + schedule resume onto the producing node (0ccab235 + 2f4d9d46) |
| microVM suspend/resume wake on metal | 1,400 / >10,000 ms, 110 failures | 470 / 5,500 ms, 0 failures | same two commits (control run with retention off confirms it) |
| microVM pause wake on metal, actor state on tmpfs instead of the boot disk | 460 / 4,500 ms | 310 / 580 ms | node experiment E1 (not a code change) |
| Activation throughput, microVM, 650 awake of 5,000 nano agents, within the latency rule | 2 swaps/s | 17 swaps/s (resume P50 1.30 s, 0 errors) | 1-s ticks, catch-up fix, snapshot-plugin pooling, microVM worker fixes |
| Activation throughput, gVisor, same test | 2 swaps/s | 9 swaps/s within the rule, 12 error-free | 1-s ticks, snapshot plugin, netns pool, shared gofer root, lean teardown |
| Activation throughput, microVM, pause instead of suspend | not measured before | 12 swaps/s on the boot disk, 15 on Hyperdisk Extreme (resume P90 0.9 s) | scheduler bfq to none, raised worker waits, faster disk |
| Awake nano agents per metal node, no parking | not measured before | gVisor 5,950 within the rules (7,550 with relative rules off), microVM 4,250 | resident fill, October 10 |
| Awake personal-assistant agents (1 GiB resident) per metal node | not measured before | 650 on both runtimes | resident fill, October 7 and 8 |

The two biggest levers were not clever: measure steadily instead of in bursts, and stop downloading bytes the node already has. After those, the limits are inside the sandbox runtimes and the kernel, and section 7 lists what to do about them.

---

## 2. Timeline

| Dates | Phase | Question | Report |
|---|---|---|---|
| Oct 2 to 6 | Resume-latency study on a dedicated cluster (`resume-lat`, c3-highmem-88 workers, later a c3-highmem-192-metal node) | Where does a suspend/resume wake spend its time, and what removes it? | `docs/dev/perf/resume-latency-journal.md` on the `perf/resume-latency` branch ([journal](https://github.com/aditya-shantanu/substrate/blob/perf/resume-latency/docs/dev/perf/resume-latency-journal.md)) |
| Oct 5 to 6 | One-ping loop with PauseActor, gVisor vs microVM, nested VM vs bare metal; cold starts; personal-assistant 2×2 (runtime × park mode) | Switch cost per activation and $/agent-month on the bare-metal node | `one-ping-pause-…-2026-10-05.md`, `cold-start-…-2026-10-06.md`, `personal-assistant-pause-vs-suspend-…-2026-10-06.md` |
| Oct 7 to 8 | Host fill (park/wake assistants until the node breaks), resident fill (awake assistants and ping actors, no parking), turnover at 500 resident | How many agents one node sustains in each mode | `host-fill-…-2026-10-07.md`, `resident-fill-personal-assistant-and-ping-…-2026-10-07.md`, `turnover-at-500-…-2026-10-08.md` |
| Oct 8 | Nano-personal-agent on upstream main 66f8a888: cold start, snapshot sizes, suspend turnover at 650 awake of 5,000 | The baseline for the optimization campaign | `nano-personal-agent-cold-start-and-turnover-5000-…-2026-10-08.md` |
| Oct 9 | Throughput optimization campaign, suspend mode, both runtimes in parallel (19 microVM iterations, 11 gVisor builds) | Raise activations per second until the hardware is the limit | `nano-agent-activation-throughput-optimization-gvisor-vs-microvm-2026-10-09.md` |
| Oct 9 | Pause-mode throughput, microVM | Same question with node-local checkpoints | `nano-agent-pause-turnover-microvm-metal-2026-10-09.md` |
| Oct 9 to 10 | Resident fill with the nano agent, both nodes | How many nano agents run at the same time on one node | `resident-fill-nano-personal-agent-gvisor-vs-microvm-metal-2026-10-10.md` |

All reports live in the TCO repo under [`assets/runs/`](https://github.com/aditya-shantanu/substrate-agents-tco/tree/main/assets/runs) next to the raw data of every run (see section 9 for the index).

---

## 3. Substrate changes

### 3.1 Branch `perf/resume-latency` (committed, on the fork)

Branch: https://github.com/aditya-shantanu/substrate/tree/perf/resume-latency. Base: upstream main at `bec46812`. Not on upstream main. The bare-metal measurement series of October 5 to 8 ran on builds of this branch (`b98e7189`, then `584d0318`); the October 8 to 10 nano-agent campaigns ran on upstream main `66f8a888` instead, so they do not include these changes.

| Commit | Title | What it does | Measured effect |
|---|---|---|---|
| [20d10da3](https://github.com/aditya-shantanu/substrate/commit/20d10da3) | perf: instrument the resume path end to end (journal "C0") | Phase timings for every hop of a wake. ateom-gvisor gets the `Restore timing breakdown` / `Checkpoint timing breakdown` log records ateom-microvm already had (prep, net_setup, pause_create, app_restore, …); atelet's `ate.snapshot.phase` values now partition the total (adds `dirs_reset`, `sysinfo_register`, `ateom_dial`, `sandbox_record`, `local_prune`, `volume_unmount`); ateapi logs one `Resume timing breakdown` per resume (`get_actor`, `lease_acquire`, `assign`, `atelet_restore`, …); the router's `ResumeActor result` carries elapsed time and attempt counts. `deploy.sh` gains `--worker-node-selector` and `--worker-toleration`. | None by itself. It produced the baseline breakdown: on a 200 ms gVisor pause wake, ~175 ms is runsc and ~25 ms is Substrate (ateapi 12, router 7, atelet 3, ateom 15). On a suspend wake 3/4 of the time was the GCS download. |
| [590efbb5](https://github.com/aditya-shantanu/substrate/commit/590efbb5) | atelet, ateom-gvisor: take two serial steps off the restore critical path (C1 + C2) | C1: atelet fetches the manifest inside the download leg so the sandbox-prep leg starts immediately. C2: ateom-gvisor mints the egress certificate (ateom → atelet → ateapi → Postgres) in a goroutine concurrent with namespace and runsc setup instead of serially before them; a new `egress_join` phase reports what is left on the critical path. | C2: the 4 ms mint fully overlapped (`egress_join` 0). C1: "correct and harmless but not a measured win" on this path, because the download leg is the longer one anyway. |
| [0ccab235](https://github.com/aditya-shantanu/substrate/commit/0ccab235) | atelet: restore from the node-local copy of the snapshot it just uploaded (C3) | After a suspend uploads the checkpoint, atelet keeps the uncompressed files under the actor's `retained-snapshot/` directory with the exact snapshot URI. On the next resume it checks whether that URI is retained locally; if so it hard-links the files into the restore directory and logs `ate.actor.restore.source=retained`; otherwise it downloads. Only `latest` snapshots are retained; the copy is replaced on the next upload and removed on terminate. Flag `--retain-uploaded-snapshots`, default true (false still uses existing copies). New file `cmd/atelet/retained_snapshot.go` plus tests. | gVisor suspend wake on one node: P50 1,200 → 190 ms, P99 3,400 → 340 ms (B3 → B4b; 987 retained, 16 downloads). microVM on metal, control run with retention off: P50 1.4 s, P90 11.9 s, 110 failures; with retention: 0.47 s, 3.0 s, 0 failures (B8c vs B8b). |
| [2f4d9d46](https://github.com/aditya-shantanu/substrate/commit/2f4d9d46) | ateapi: prefer the node that produced a suspended actor's snapshot (C4) | Suspend records `ExternalSnapshot.produced_on_node` (new proto field 4, a scheduling hint only). Resume of a SUSPENDED actor adds that node as `PreferredNodes` to the scheduler constraints; candidates on the preferred node win if any has room, otherwise scheduling is unchanged. Log attribute `ate.scheduler.node_preference` = hit / miss / none. | With 3 worker nodes only 29 % of resumes found a retained copy (B6a: P50 730 ms). With the preference 551 hits, 0 misses: P50 180 ms, P99 240 ms (B7a). |
| [d6c7d832](https://github.com/aditya-shantanu/substrate/commit/d6c7d832) | Trace the whole wake: phase spans, a flight span, an always-sampled wake root (T1) | Every restore/checkpoint phase becomes an OpenTelemetry span (new `internal/phasespan`), the router's resume flight becomes a span with parked waits as events, the certificate mint joins the trace, Envoy spawns an upstream span. ateapi roots a new always-sampled `ActorWake` trace for every resume that restores an actor, linked both ways to the request trace and to the snapshot producer. Flag `--trace-every-wake` (default true). Benchmark env `ACTOR_TRACES_SAMPLER` (default `parentbased_always_off`). | At 1 % router sampling, 532 of 532 wakes in a 5-minute window were complete traces (production check T1f); cost ~60 spans/s from wakes. Found the serial certificate mint on microVM (gVisor got C2, microVM did not) and that at P50 a microVM wake is 57 % `vm_restore`. |
| [7ad6e240](https://github.com/aditya-shantanu/substrate/commit/7ad6e240) | ateapi: root a wake trace only for paused and suspended actors | Crashed or deleting actors keep the wake nested in the request trace. | On an overloaded run 551 of 1,424 wake traces had been three-span error traces for crashed actors. |
| 86dced34 … [584d0318](https://github.com/aditya-shantanu/substrate/commit/584d0318) | docs: resume-latency journal (10 commits) | The journal: setup, every run B1 to B10b with client and server-side numbers, the tmpfs experiment E1, actor sizing, the candidates list, the tracing study. | See the before/after table below. |

Resume-latency journal, wake P50 / P99 in ms (WakeFirstTouch, 100 agents):

| Case | Before | After | Change |
|---|---|---|---|
| gVisor pause, 1 node | 190 / 290 | 190 / 310 | none, runsc-bound |
| gVisor suspend, 1 node | 1,200 / 3,400 | 190 / 340 | C3 |
| gVisor suspend, 3 nodes | 730 / 2,100 | 180 / 240 | C3 + C4 |
| microVM pause, metal | 460 / 4,500 | 460 / 4,500 | none, disk-bound |
| microVM suspend, metal | 1,400 / >10,000 (110 failures) | 470 / 5,500 | C3 + C4 |
| gVisor pause, actor state on tmpfs (E1), 2 GiB actor | 190 / 310 | 180 / 230 | node experiment |
| microVM pause, tmpfs | 460 / 4,500 | 310 / 580 | node experiment |
| microVM suspend, tmpfs | 470 / 5,500 | 300 / 620 | node experiment |

Two side findings from the journal that shaped the later work: a gVisor checkpoint's dirty page cache is charged to the actor's memory cgroup, so a 1 GiB-resident actor needs about 2× its resident set as limit or the sentry is OOM-killed at checkpoint (upstream #1917); deleting a SUSPENDED actor does not reclaim its node directory (upstream #1688, #641).

### 3.2 October 8 to 9 campaign patches on upstream main `66f8a888`

These changes were made in two git worktrees of the fork, both detached at upstream main `66f8a888` ("Egress gateway can mint and cache actor JWTs (#2135)"), one per runtime. They are uncommitted in the worktrees; the complete diffs are archived in the TCO repo and that is the reference to share:

| Patch | Covers | Link |
|---|---|---|
| `substrate-microvm-worktree.patch` (1,122 lines) | snapshot plugin / `pkg/objectstorage`, `cmd/ateom-microvm`, `internal/ateomnet` (netns pool), `internal/tarutil`, `deploy.sh` | [assets/runs/2026-10-09-throughput-campaign/substrate-microvm-worktree.patch](https://github.com/aditya-shantanu/substrate-agents-tco/blob/main/assets/runs/2026-10-09-throughput-campaign/substrate-microvm-worktree.patch) |
| `substrate-gvisor-worktree.patch` (1,275 lines) | `pkg/objectstorage` (shared core), `cmd/ateom-gvisor`, `internal/ateomnet` (netns pool), `internal/tarutil`, `deploy.sh` | [assets/runs/2026-10-09-throughput-campaign/substrate-gvisor-worktree.patch](https://github.com/aditya-shantanu/substrate-agents-tco/blob/main/assets/runs/2026-10-09-throughput-campaign/substrate-gvisor-worktree.patch) |
| `substrate-microvm-worker-pause-run7plus.patch` (108 lines) | the raised virtiofsd / VMM / guest-agent waits added during the pause campaign | [assets/runs/2026-10-09-pause-campaign/substrate-microvm-worker-pause-run7plus.patch](https://github.com/aditya-shantanu/substrate-agents-tco/blob/main/assets/runs/2026-10-09-pause-campaign/substrate-microvm-worker-pause-run7plus.patch) |

To give them commit links, the two worktrees (`~/repos/substrate-east1` microVM, `~/repos/substrate-east` gVisor) need to be committed to branches of the fork; the compiled binaries `ateom-microvm` and `snapshot-plugin` sitting untracked in the microVM worktree must be left out. Suggested names: `perf/throughput-microvm-66f8a888` and `perf/throughput-gvisor-66f8a888`.

Every change below is marked `EXPERIMENT` in its code comment and most are switchable by an environment variable so the stock behaviour can be restored without a rebuild.

#### 3.2.1 Snapshot plugin and `pkg/objectstorage` (both runtimes)

The snapshot plugin is one process per node; every suspend upload and every resume download of every worker pod goes through it.

| Change | Files | Knob (default) | Why | Measured |
|---|---|---|---|---|
| Pool of GCS clients for every transfer. Previously one `storage.Client` (one HTTP/2 connection) carried every object below 16 MiB (download) / 64 MiB (upload), which is every nano-agent snapshot. `nextClient()` round-robins over the pool for whole-object GETs, the first range read, and `putSingle`. | `gcs.go`, `gcscompose.go`, `gcsranged.go` | `ATE_GCS_CLIENT_POOL` (8; the campaign ran 64) | Quiet probe on main: 20 concurrent microVM restores all finished within 250 ms of each other at 1.9 s, download 1,645 ms, i.e. N × size ÷ one flow. | 20 concurrent restores: download 1,645 → 1,233 ms. The same defect was fixed independently upstream by a colleague in [e2a9e2e](https://github.com/agent-substrate/substrate/commit/e2a9e2eec69ada09fb45bb88469756e06d1bfb44); this patch additionally routes the `GetObject` head range through the pool. |
| Pooled zstd encoders, decoders, chunk and output buffers; parallel-zstd fan-out capped at 8 workers. | `parzstd.go`, `sparsezstd.go`, `objects.go` | none | A 128 MiB snapshot allocated ~500 MiB of fresh memory per upload; the plugin spent 20 to 26 cores and ~1 M minor page faults/s zeroing memory. | At 2 swaps/s suspend P50 1,001 → 889 ms, P90 1,246 → 1,040; page faults 157k → 56k/s; CPU per swap −20 %. |
| Bounded free lists for the 64 MiB upload head buffers, 16 MiB range buffers and 1 MiB copy buffers; writer chunk size = object size (rounded to 256 KiB) instead of a fixed 64 MiB. | `gcs.go`, `rangedget.go`, `sparsezstd.go` | none | Fresh head and media buffers were 39 % of all bytes the plugin allocated; `memclrNoHeapPointers` was 18 % of CPU with RSS swinging 4 to 17 GB. | Page faults 150k/s → 300/s at 10 swaps/s, RSS stable. Ceiling unchanged: a cost, not the limit. |
| Download ranges 16 MiB → 4 MiB, 8 → 12 in flight per object. | `rangedget.go` (microVM worktree) | none | Per-stream GCS throughput measured ~45 MB/s on this node; a 42 MiB object over three 16 MiB ranges was capped near 140 MB/s. | At 15 swaps/s resume P90 1,508 → 1,256 ms. |
| zstd decoder concurrency 1 → 4 for the sparse memory image; `io.CopyBuffer` with 1 MiB chunks instead of 32 KiB `ReadFrom`. | `sparsezstd.go` (microVM worktree) | none | Single-threaded decode of a 128 MiB image was the largest term of the plugin's download stage; 4,000 write syscalls per image cost ~4 cores at 12 restores/s. | Part of the 15 → 17 swaps/s configuration. |
| Selectable zstd level. | `parzstd.go` | `ATE_ZSTD_LEVEL` (fastest; `default`, `better`) | Test whether denser compression pays. | Rejected: image 41 → 39 to 42 MiB (−3 %) for ~3× compression CPU; resume P50 at 15/s 1,097 → 1,391 ms. |
| Instrumented HTTP transport with per-method latency and status counters. | `gcs.go` | `ATE_GCS_HTTP_STATS` (off) | Rule out GCS throttling. | Diagnostic only: all 2xx, request P50 55 ms. |
| pprof endpoint in the plugin. | `cmd/snapshot-plugin/main.go` | `ATE_PPROF_ADDR` (unset) | Profile the plugin under load. | Produced the CPU, heap and allocation profiles behind the pooling changes. |
| Composite uploads in 8 MiB parts (constant `uploadCompositeMin`). | `gcscompose.go` | unchanged at 64 MiB, comment records the test | Try to parallelise uploads. | Rejected: suspend P50 1,119 → 1,603 ms, P99 1,770 → 7,817 at 15/s; 300 goroutines in HTTP/2 flow control at 21/s. |
| Go GC tuning (`GOGC=400`, `GOMEMLIMIT=96GiB`). | deployment env | | | No effect; heap grew to 91 GB under the limit; back to `GOGC=200`. |

#### 3.2.2 Shared: sandbox network-namespace pool, tar fsync, deploy script

| Change | Files | Knob (default) | Why | Measured |
|---|---|---|---|---|
| Sandbox network pool: a worker keeps up to N idle sandbox networks (two namespaces, veth pair, nftables table) and re-adopts one for the next activation instead of creating and destroying them. Pooled namespaces are handed to the runtime as `/proc/<pid>/fd/<n>` so no bind mounts are needed. Only the veth shape is pooled. | new `internal/ateomnet/netpool.go`, `sandbox.go`, `netns/netns.go` | `ATE_NETNS_POOL` (16; 0 disables), `ATE_NETNS_BIND_NAMES` (off) | On a 100-worker metal node the kernel's single `cleanup_net` worker and `rtnl_lock` serialise every namespace create and teardown node-wide: netns setup 11 ms alone → 293 / 495 / 800 ms at 10 / 20 / 40 concurrent wakes. | gVisor: `net_setup` 170 to 330 → 1 ms, ~95 % pool hit rate, at 12 swaps/s wake P50 2,563 → 1,618 ms. microVM: no measurable gain (its cost is the tap device, not the namespace). |
| Skip the `fsync` after writing a checkpoint tar unless asked. | `internal/tarutil/tarutil.go` | `ATE_TAR_FSYNC` (off; `1` restores the fsync) | N concurrent checkpoints' fsyncs convoy on the node filesystem's journal: the rootfs-upper tar stage went 0.5 s alone → 2.4 s at 40 concurrent on an otherwise idle disk. Approved for the experiment on October 9. | With the reseed fix, microVM 5 → 8 swaps/s; the stage no longer grows with concurrency. Needs a proper flag and a durability decision upstream. |
| `--actor-cpu` for `deploy.sh`; emits a CPU limit in the actor template when set, nothing otherwise. | `benchmarking/workloads/deploy.sh`, `glutton-template.yaml.tmpl` | none | Actors needed an explicit CPU limit for the resident tests. | Tooling. |

#### 3.2.3 microVM worker (`cmd/ateom-microvm`)

| Change | File | Why | Measured |
|---|---|---|---|
| Guest CRNG reseed after restore: timeout 5 s → 15 s with one retry. | `restore.go` | Under dense restore load the guest agent occasionally answers after 5 s; a failed reseed marks the actor CRASHED, and deleting a crashed actor whose worker pod is gone hangs in DELETING and answers 503 to every later wake. ~1 crash per 500 to 1,000 restores. | Restore crashes → 0 in later runs; the crash gate stopped ending runs; made 8 swaps/s reachable. |
| Skip the graceful Cloud Hypervisor shutdown in teardown after a checkpoint (context marker `afterCheckpointKey`). | `checkpoint.go` | The guest is paused and its snapshot is on disk; the API shutdown cost P50 178 / P90 370 / P99 810 ms per suspend at 15 swaps/s. | Teardown 258 → 258 to 322 ms: no gain, kept as harmless. |
| Raised waits: CH API `WaitReady` 10 and 15 s → 60 s, virtiofsd socket 10 s → 60 s, guest-agent dial 15 s → 45 s. | `checkpoint.go`, `internal/ch/restorefds.go`, `internal/kata/overlay_linux.go`, `restore.go` | In pause mode at 11 to 15 swaps/s, restores timed out waiting for virtiofsd and VMM sockets with the disk only 5 to 15 % busy; a stall must become latency, not a crashed actor. | Tails shorter; a residual 0.3 to 0.5 % of restores still fail outright at 15 pause swaps/s (virtiofsd never opens its socket within 60 s, or the agent never answers two 15-s reseeds). |

#### 3.2.4 gVisor worker (`cmd/ateom-gvisor`)

Build-time defaults are collected in a new file `experiment_defaults.go` ("build D": netns pool, shared gofer root, lean teardown, no app-container cgroup, no cgroup slots, no runsc debug logging).

| Change | File | Knob (default) | Why | Measured |
|---|---|---|---|---|
| One `runsc --shared-root` directory per worker instead of a root per actor. | `runsc.go` | `ATE_RUNSC_SHARED_ROOT` (`/run/ateom-runsc-shared`; `off` restores) | runsc pins the gofers' empty network namespace under `--shared-root`; per-actor roots meant a namespace unshare and pin per create and a teardown per delete, serialised node-wide. This made `pause_create` grow from 90 ms alone to 300 to 650 ms at 8 to 12 swaps/s. | Teardown 313 → 233 ms at 8/s, 467 → 392 at 12/s; wake P90 at 8/s 1,315 → 1,254 ms. |
| Lean post-checkpoint teardown: after a FULL checkpoint the sandbox is already stopped, so the kill / wait / state / state round trips (four runsc spawns that only ever fail) are skipped and both containers are deleted directly. | `main.go` (`terminateCheckpointedWorkload`) | `ATE_LEAN_TEARDOWN` (on; `0` restores) | Also fixed a cgroup leak: ~30 % of suspends leaked both cgroups, 8,157 cgroups and 118 GiB of unreclaimable slab for 650 actors. | Cgroups 8,157 → exactly 650; teardown −60 to 90 ms; every cgroup operation on the node faster. |
| No cgroup for the application container (its processes run inside the sentry, which the pause container's cgroup already bounds). | `runsc.go` (`shapeSpec`) | `ATE_SUBCONTAINER_CGROUP` (off; `1` restores) | Saves one mkdir, two `cgroup.procs` attaches and one rmdir per activation, each a node-global lock at 10 to 60 ms under load. | With lean teardown: wake P50 at 8/s 1,076 → 912 ms, at 12/s 1,584 → 1,337. |
| `--ignore-cgroups` (diagnostic). | `runsc.go` | `ATE_RUNSC_IGNORE_CGROUPS` (off) | Measure the cgroup cost. | Rejected: without a cgroup runsc sizes the sentry from the host's 192 vCPUs and every stage gets slower (wake P50 at 8/s 1,076 → 2,328 ms). |
| Reusable worker-owned cgroup slots (`ateom-slot-N`). | new `cgroupslots.go` | `ATE_CGROUP_SLOTS` (off) | Avoid the per-activation cgroup create/remove. | Rejected: a gofer forked into a previously used slot dies before its first log line (A/B smoke test 0/58 failures off, 26 to 44 of 58 on); 4,806 actors crashed in the run that tried it. |
| runsc debug logging switch. | `runsc.go` | `ATE_RUNSC_DEBUG` (off) | Diagnostics; costs noticeable time per command. | Tooling. |

#### 3.2.5 Node-level changes (not code)

| Change | Where | Why | Measured |
|---|---|---|---|
| ARP neighbour table: `net.ipv{4,6}.neigh.default.gc_thresh1/2/3` 128/512/1,024 → 4,096/8,192/16,384 (`sysctl-neigh.yaml`, node-critical pod) | both metal nodes | Every gVisor actor adds two network namespaces and veths on the host; at ~1,000 to 1,100 live sandboxes the host's neighbour cache overflows and new sandboxes' traffic times out ("neighbor table overflow" in dmesg). Lost on reboot: the east node rebooted on October 9 and the wall came back at 1,200 agents until the sysctl was re-applied. | Removed the wall in the ping and nano resident fills. A Substrate node-prep item. |
| inotify limits: `max_user_instances` 8,192 → 65,536, `max_user_watches` 12,288 → 1,048,576 (`sysctl-inotify.yaml`) | both nodes | Exhausted at ~800 sandboxes. Also lost on reboot. | Node-prep item. |
| Actor state directory (`/var/lib/ate/actors`) on tmpfs | microVM node (throughput campaign), `resume-lat` cluster (journal E1) | Take checkpoint and restore staging off the boot disk. | Journal: pause P99 improved up to 140× and every tail. Throughput campaign: IO pressure 13 % → 0 but the ceiling did not move (the limit was elsewhere by then). Not usable for pause mode at 5,000 actors (580 GB). |
| Boot-disk I/O scheduler `bfq` → `none`, read-ahead 128 KiB → 1 MiB | microVM node (pause campaign) | bfq's per-process idling starves the many short I/O streams of VM start-up: restores timed out on virtiofsd and VMM sockets with the disk 5 to 15 % busy. | At 8 pause swaps/s resume P99 10 s → 560 ms; fill 54 → 25 s. |
| 2 TB Hyperdisk Extreme (350k IOPS) attached and mounted at `/var/lib/ate/actors` | microVM node (pause campaign) | The boot volume saturated at 15 pause swaps/s (95 % busy, IO pressure 46 %). | IO pressure at 15/s 46 → 22 %; 15 swaps/s delivered at resume P90 0.9 s. |
| Orphaned actor directories reclaimed (37k dirs, 576 GB) | microVM node | Substrate never reclaims node directories of deleted actors (upstream #641, #1757, #1688); the 3 TB disk fills in hours of testing. | Disk 614 → 21 GB used. The run right after failed from the filesystem's background work, so reclaim well before measuring. |
| cgroup v2 `favordynmods` remount | gVisor node | Cheaper task migration. | Attach pair 42 → 2.7 ms, rmdir 8.6 → 3.0 ms; lost at the next worker-pool roll. Needs `cgroup_favordynmods=1` on the kernel command line. |

---

## 4. Harness changes (`substrate-agents-tco`)

Everything below is committed on `main` of https://github.com/aditya-shantanu/substrate-agents-tco. Commit links are `https://github.com/aditya-shantanu/substrate-agents-tco/commit/<sha>`.

### 4.1 Changes that changed the measured numbers

| Commit | Change | Effect |
|---|---|---|
| [16712b7](https://github.com/aditya-shantanu/substrate-agents-tco/commit/16712b7), [665d22f](https://github.com/aditya-shantanu/substrate-agents-tco/commit/665d22f), [f274e25](https://github.com/aditya-shantanu/substrate-agents-tco/commit/f274e25), [3efde32](https://github.com/aditya-shantanu/substrate-agents-tco/commit/3efde32), [cc3c9b6](https://github.com/aditya-shantanu/substrate-agents-tco/commit/cc3c9b6) | agentsim swap mode: hold a fleet with N awake, park N and wake N per tick, N growing per level, gates on wake/park P90 vs the first level, wake P99, errors, refusals, backlog, node memory, PSI and crashes, hold after the first failure; resident agents play the assistant day at its real pace. | The turnover test used for every throughput number. |
| `SWAP_EVERY=1s` (env in `nanoturn3.sh`, swap mode already supported it) | Issue swaps steadily every second instead of N at once every 10 s. | 2 → 5 swaps/s on both runtimes with no Substrate change; at 2/s microVM resume P50 2,304 → 662 ms, gVisor 2,276 → 536. Every earlier latency figure had been burst queueing. |
| [605abef](https://github.com/aditya-shantanu/substrate-agents-tco/commit/605abef) | `--script-catchup` (default on, off for throughput runs): the sim had replayed every skipped step's operations on an agent's first wake, ~46 requests plus one `ResumeActor` per first wake, a request storm proportional to the swap rate (router log: 133 first wakes → 6,170 requests in 30 s). | microVM at 15/s: resume P50 1,651 → 1,305 ms, P90 2,354 → 1,468; suspend P90 1,813 → 1,360. Present in every run before iteration 12 on both nodes. |
| [7f1616c](https://github.com/aditya-shantanu/substrate-agents-tco/commit/7f1616c) | Backlog gate is five seconds' worth of swaps when ticks are shorter than 2.5 s. | With 1-s ticks ~1.5 s of arrivals is always in flight; the old "two ticks" gate failed clean levels. |
| [9a4d730](https://github.com/aditya-shantanu/substrate-agents-tco/commit/9a4d730) | Fleet reuse excludes DELETING and CRASHED actors. | 22 DELETING actors had answered 503 to every wake and invalidated a run. |
| [99189f5](https://github.com/aditya-shantanu/substrate-agents-tco/commit/99189f5) | Error and refusal gates need at least three occurrences. | One error in 145 wakes had ended a run at its first level. |
| [1b118c8](https://github.com/aditya-shantanu/substrate-agents-tco/commit/1b118c8) | Fill gives up on actors whose wake error is permanent (CRASHED, DELETING, not found). | A handful of crashed actors held the 8 fill slots through 12 long retries each; fill 2 min 41 s → 25 to 50 s. |
| [7a7471c](https://github.com/aditya-shantanu/substrate-agents-tco/commit/7a7471c), [b7a7741](https://github.com/aditya-shantanu/substrate-agents-tco/commit/b7a7741) | `run.sh` readiness check accepts main's UUID golden tag, retries the CLI and never redeploys on a transient listing failure. | The old check always failed, so every run redeployed the workloads and rolled the worker pool, which crashes every awake actor (361 crashed in one run). |
| [45d1e79](https://github.com/aditya-shantanu/substrate-agents-tco/commit/45d1e79) | `--setup-park-existing` reuses a registered fleet. | Iterations skip the 20-minute registration. |
| [f7b2343](https://github.com/aditya-shantanu/substrate-agents-tco/commit/f7b2343), [e0effd6](https://github.com/aditya-shantanu/substrate-agents-tco/commit/e0effd6) | Load-test (wave) mode with live gates for the resident fills: relative P90 vs the first wave, probe P99, node MemAvailable, PSI, crashes; hold at the last clean level; park the agents dropped at hold. | The resident-fill numbers. |
| [313ee9e](https://github.com/aditya-shantanu/substrate-agents-tco/commit/313ee9e), [0571981](https://github.com/aditya-shantanu/substrate-agents-tco/commit/0571981) | `--swap-cycle-all`: keep swapping at the last clean rate until every registered actor has been resident once. | The "5,000 actors cycled in 36 minutes" figure. |

### 4.2 Workloads and instrumentation

| Commit | Change |
|---|---|
| [59c122a](https://github.com/aditya-shantanu/substrate-agents-tco/commit/59c122a), [27dd4c9](https://github.com/aditya-shantanu/substrate-agents-tco/commit/27dd4c9) | One-ping (GluttonUser loop) workload, PauseActor lifecycle, bare-metal support, independent Poisson wakes for the ladder. |
| [0b77891](https://github.com/aditya-shantanu/substrate-agents-tco/commit/0b77891) | `--cold-start-only` and `--actor-prefix`; `cold_report.py`. |
| [e516ccc](https://github.com/aditya-shantanu/substrate-agents-tco/commit/e516ccc), [8dc38b5](https://github.com/aditya-shantanu/substrate-agents-tco/commit/8dc38b5) | Personal-assistant workload (substrate#2230 script: 61 steps per agent-day), `--script-loop`, fractional think scale, staggered starts, `snapshot_sizes.py`. |
| [087a94f](https://github.com/aditya-shantanu/substrate-agents-tco/commit/087a94f), [6e1dfdb](https://github.com/aditya-shantanu/substrate-agents-tco/commit/6e1dfdb) | Nano-personal-agent: the mock-everything personal agent (61 steps/day; model call = dwell, API call = ping, file = small write, no synthetic heap); `--script-snapshot-steps` for the snapshot-size study. |
| [5a0a598](https://github.com/aditya-shantanu/substrate-agents-tco/commit/5a0a598) | Coding-agent script workload; P50/P90/P99 for every latency. |
| [b817a8c](https://github.com/aditya-shantanu/substrate-agents-tco/commit/b817a8c) | Per-stage restore/checkpoint timing extraction from worker and node-agent logs; quiet 1-vs-20 wake probes. |
| [d946fe7](https://github.com/aditya-shantanu/substrate-agents-tco/commit/d946fe7) | Think gaps are cancellable (a resident loop sleeping hours kept a run alive after its verdict). |

Scripts used for the campaigns (iteration pipelines, node tooling, report generators) are archived under each campaign's `scripts/` directory.

---

## 5. Results by campaign

### 5.1 Suspend throughput at 650 awake of 5,000 nano agents (October 8 baseline → October 9 final)

One c3-standard-192-metal per runtime, 100 unsized worker pods, Substrate main 66f8a888 plus the patches in section 3.2. One activation = one actor resumed from its suspended snapshot and answering a request; the test suspends one for every one it resumes. Rule: resume and suspend P90 within 2.5× the first level, errors and refusals < 0.5 %, no backlog growth.

| | microVM (2 vCPU + 256 MiB guest) | gVisor (2 vCPU + 2 GiB limit) |
|---|---|---|
| Start: main 66f8a888, 10-s burst ticks | 2 swaps/s (resume 2,273 / 2,422 / 2,480 ms, suspend 3,931 / 4,360 / 9,048); 4 fails the rule | 2 swaps/s (resume 2,200 / 2,434 / 2,659, suspend 1,399 / 1,691 / 1,966); 4 fails |
| Same build, 1-s ticks | 5 swaps/s | 5 swaps/s |
| End, within the rule | **17 swaps/s** (16.7 achieved; resume 1,298 / 1,397 / 1,560 ms, suspend 1,092 / 1,345 / 1,615; 0 errors, 0 refusals) | **9 swaps/s** (resume 959 / 1,194 / 1,449 ms, suspend 589 / 742 / 932; 0 errors) |
| Delivered error-free beyond the rule | 17 (20 collapses: resume P90 8.7 s, refusals) | 12 (11.8 achieved, 0 errors) |
| Where it breaks now | 18 to 21 swaps/s: every per-VM step convoys at once (tap setup 2 → 1,300 ms, VMM restore 140 → 1,500, teardown 230 → 1,460; load average 140 with CPUs ~35 % busy and no IO wait) | 12 swaps/s: the kernel's mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, gVisor's own restore |

Per-improvement impact (from the campaign report; "ceiling" = highest activations/s within the rule):

| # | Change | Where | Ceiling | Resume latency | Suspend latency | Node / plugin cost | Verdict |
|---|---|---|---|---|---|---|---|
| 1 | 1-s ticks instead of 10-s bursts | harness | microVM 2 → 5, gVisor 2 → 5 | at 2/s microVM P50 2,304 → 662 ms, gVisor 2,276 → 536 | microVM 3,992 → 1,001; gVisor 1,713 → 593 | none | kept |
| 2 | Pooled GCS clients for every transfer | snapshot plugin | microVM 5 | 20 concurrent restores: download 1,645 → 1,233 ms | | | kept |
| 3 | Pooled zstd buffers, encoders, decoders; fan-out cap 8 | snapshot plugin | | | at 2/s P50 1,001 → 889, P90 1,246 → 1,040 | page faults 157k → 56k/s; CPU per swap −20 % | kept |
| 4 | Tar fsync skipped after checkpoint | microVM worker | 5 → 8 with #5 | | rootfs-upper stage no longer grows with concurrency | IO pressure lower | kept, needs a flag |
| 5 | Guest CRNG reseed 15 s + retry | microVM worker | 8 reachable | | | crashes ~1 per 500 to 1,000 restores → 0 | kept, needs upstream fix |
| 6 | Crash gate 5 → 20; fleet excludes DELETING/CRASHED | harness | results readable | | | | kept |
| 7 | Bounded buffer free lists; writer chunk = object size | snapshot plugin | 15 unchanged | | | page faults 150k/s → 300/s; RSS stable | kept, a cost not the limit |
| 8 | GOGC 400 + GOMEMLIMIT 96 GiB | plugin env | unchanged | | | heap 91 GB, no effect | reverted |
| 9 | Graceful VMM shutdown skipped after checkpoint | microVM worker | unchanged | | teardown 258 → 258 to 322 ms | | kept, harmless |
| 10 | Orphan actor directories reclaimed (37k, 576 GB) | node | | the next run failed at 14/s from the disk's background work | | disk 614 → 21 GB | do it well before measuring |
| 11 | Actor state on tmpfs | node | unchanged (15) | with #12: 15/s P90 2,354 → 1,468 | | IO pressure 13 → 0 | kept; removes the disk, not the ceiling |
| 12 | Sim first-wake catch-up off | harness | 15 held with margin | 15/s P50 1,651 → 1,305, P90 2,354 → 1,468 | 15/s P90 1,813 → 1,360 | ~46 requests per first wake removed | kept |
| 13 | Instrumented GCS transport | snapshot plugin | diagnostic | no throttling: all 2xx, P50 55 ms | | | off |
| 14 | 4 MiB download ranges, 12 in flight | snapshot plugin | 15, then 17 | 15/s P90 1,508 → 1,256 | | | kept |
| 15 | Composite uploads in 8 MiB parts | snapshot plugin | unchanged | | 15/s P50 1,119 → 1,603, P99 1,770 → 7,817 | 300 upload goroutines in flow control | reverted |
| 16 | Netns pool ported to the microVM worker | `internal/ateomnet` | unchanged | 15/s P90 1,508 → 1,223 | | tap stage unchanged | kept, no measurable gain on microVM |
| 17 | Denser zstd level | snapshot plugin | 21 collapsed harder | 15/s P50 1,097 → 1,391 | | image −3 %, ~3× CPU | reverted |
| 18 | Fine ramp from 12 (final) | harness | **microVM 17** | 1,298 / 1,397 / 1,560 | 1,092 / 1,345 / 1,615 | plugin ~25 cores | final |
| g1 | Plugin patch on gVisor | snapshot plugin | 5 → 8 | 8/s P50 1,305 → 1,150 | | | kept |
| g2 | Sandbox netns pool | gVisor worker | 8 | net_setup 170 to 330 → 1 ms; 12/s P50 2,563 → 1,618 | 12/s 1,246 → 1,104 | ~95 % pool hits | kept |
| g3 | `runsc --shared-root` | gVisor worker | 8 | 8/s P90 1,315 → 1,254 | teardown −80 ms | | kept |
| g4 | `--ignore-cgroups` | gVisor worker | 8 → 5 | 8/s P50 1,076 → 2,328 | | sentry sized from the host | rejected |
| g5 | Reusable cgroup slots | gVisor worker | run failed | | | 4,806 actors crashed | rejected |
| g6 | Lean teardown + no app-container cgroup | gVisor worker | 8 | 8/s P50 1,076 → 912; 12/s 1,584 → 1,337 | | cgroup leak fixed: 8,157 → 650 | kept |
| g7 | Catch-up off + ×1.25 ramp | harness | **gVisor 9** | 959 / 1,194 / 1,449 | 589 / 742 / 932 | | final |

### 5.2 Pause throughput, microVM, 650 awake of 5,000 (October 9)

Same test with `PauseActor`: the 128 MiB memory image plus rootfs upper layer (119 MB per actor) stays on the node; no bucket, no snapshot plugin.

| | Activations/s delivered | Resume P50 / P90 / P99 ms | Pause P50 / P90 / P99 ms | What stopped it |
|---|---|---|---|---|
| Pause, boot disk (3 TB Hyperdisk Balanced, scheduler bfq → none) | 12 (within the 2.5× rule: 8) | 321 / 509 / 646 | 301 / 453 / 583 | the disk at 15/s: 95 % busy, IO pressure 46 % |
| Pause, Hyperdisk Extreme (2 TB, 350k IOPS) | 15 (3.2 % errors + refusals) | 798 / 897 / 946 | 322 / 508 / 671 | per-VM start-up failures crashing 0.3 to 0.5 % of restores; page-cache pressure from 591 GB of local checkpoints |
| Suspend, reference (final build) | 17 (0 errors) | 1,298 / 1,397 / 1,560 | suspend 1,092 / 1,345 / 1,615 | node-wide convoy at 20/s |

Pause is about 5× faster per activation but moves about 3× the bytes through the local disk, uncompressed in and out. Compressing the local checkpoint the way the suspend path does would let the boot disk alone carry roughly 35 activations/s.

### 5.3 Resident fill with the nano agent (October 10): how many run at the same time

| | gVisor | microVM |
|---|---|---|
| Awake nano agents per node, last clean wave | **5,950** | **4,250** |
| First failing wave and why | 6,150: turn P90 > 2× the first wave | 4,500: memory available < 10 % |
| Memory free at the ceiling | 51.6 % | 12.8 % |
| Host memory per resident sandbox | 59 MiB | 127 MiB |
| Probe / turn P99 at the ceiling | 75 / 90 ms | 60 / 55 ms |
| With the relative rules off (absolute gates only) | 7,550 passed, 7,750 failed on 9 % wake 502s; the 10-min hold at 7,550 failed (P99 ≈ 2 s, node 54 to 76 % busy, kernel time above user time) | not applicable, out of memory |

gVisor is CPU-bound with half the memory unused; microVM is a straight memory budget.

### 5.4 Measurement series on the bare-metal node, October 5 to 8 (builds of `perf/resume-latency`)

| Test | Result |
|---|---|
| Cold start (CreateActor → golden restore → first ping), P50 / P90 / P99 ms | gVisor nested c3-standard-4 274 / 355 / 586; microVM nested 710 / 1,268 / 1,752; gVisor metal 340 / 379 / 456; microVM metal 596 / 666 / 800. Cold start is the golden-snapshot restore. |
| One-ping loop, 1,000 agents, 20-min windows, resume P50 / P90 / P99 ms | gVisor pause 275 / 318 / 400, suspend 371 / 769 / 1,239; microVM pause 160 / 231 / 797, suspend 160 / 234 / 434. Park: gVisor pause 241 / 292 / 388, suspend 585 / 923 / 1,331; microVM pause 214 / 577 / 3,688, suspend 1,172 / 2,404 / 4,466. |
| One-ping overcommit ladder, microVM pause, 200 shaped workers, 2,000 parked actors | Last sustainable 300 active agents = 12 wakes/s (resume 382 / 2,729 / 4,778 ms); 14 wakes/s collapses (refusals 37 %). Host CPU < 5 %; the per-node checkpoint/restore path is the limit. |
| Personal assistant (1 GiB resident, 61 steps/day), 30 agents, $/agent-month as run | gVisor pause $1.39 (resume 423 / 537 / 890 ms), gVisor suspend $1.78 (439 / 554 / 786), microVM pause $3.66 (593 / 724 / 3,094; park P90 54 s, disk-bound), microVM suspend $2.18 (679 / 834 / 1,366). gVisor needs a 3 GiB limit to checkpoint a 1 GiB-resident actor. |
| Host fill (park/wake assistants, 50-pod pool) | Ceilings: gVisor suspend and pause 90 pass / 120 collapse; microVM suspend 60 / 90; microVM pause 60 / 90. Limiter: the boot disk is time-saturated at 1.0 to 1.6 GiB/s (busy 95 to 98 %, queue depth in the hundreds), not RAM or CPU. |
| Resident fill, assistants and ping actors, no parking | Assistant: 650 clean / 700 fail on both runtimes (memory). Ping (256 MiB): gVisor ≥ 4,000 (fleet exhausted), microVM 3,550 / 3,850; both had walled at 1,000 on the ARP neighbour table before the sysctl. One worker pod: 150 assistants or 100 ping actors, limited by per-pod wake serialisation (worker row lock in `BindActorToWorker`, router 5-s parked budget). |
| Turnover at 500 resident (1 GiB actors, 10-s burst ticks) | 0.5 swaps/s clean in all four cells, 1.0 fails: parks queue on the boot disk (park P90 2 to 7 s → 9 to 144 s). |
| Nano agent on main 66f8a888, cold start and snapshot size | Cold start gVisor 280 / 349 / 427 ms, microVM 567 / 959 / 1,061. Snapshot in the bucket: gVisor 9.6 MiB, microVM 49 MiB (zstd); raw pause image gVisor 15 MiB, microVM 136 MiB. |

---

## 6. Where the time went, in order of discovery

1. **The test design.** Issuing N suspends and N resumes in the same instant every 10 s made latency grow linearly with N and tripped a relative latency rule on burst size alone. Steady 1-s ticks: 2 → 5 swaps/s on both runtimes with nothing else changed.
2. **The harness's first-wake catch-up.** Each agent's first wake replayed every skipped step, ~46 requests, a storm that scaled with swap rate and was present in every run until it was found in a router log.
3. **The snapshot plugin's single connection.** One HTTP/2 connection per node carried every transfer: 20 concurrent restores finished together at N × size ÷ one flow. Fixed with the client pool (the same defect upstream fixed in e2a9e2e).
4. **The snapshot plugin's allocation churn.** Fresh 64 MiB and 128 MiB buffers and new codecs per transfer: 24 cores at 12 swaps/s, a third of it zeroing memory. Pooled; a cost, not the limit.
5. **Per-stream GCS throughput.** ~45 MB/s per stream; smaller ranges with more in flight helped, composite uploads and denser compression did not.
6. **microVM worker defects.** A 5-s guest reseed timeout crashed one restore in 500 to 1,000 and left undeletable actors; per-checkpoint fsyncs convoyed on the node's journal. Both fixed for the experiment.
7. **microVM now: the node's sandbox create and teardown path.** At 18 to 21 swaps/s every per-VM stage convoys at once with CPUs 35 % busy and no IO wait: `rtnl_lock` for tap and namespace work, the mount-namespace lock for overlay and virtiofs mounts, cgroup and mm locks around 20 process spawns and kills per second, and Cloud Hypervisor's eager restore of 128 MiB per VM.
8. **gVisor now: runsc's per-sandbox chroots, cgroup migrations and its own restore.** `pause_create` 90 ms alone → 600 to 700 ms at 12 swaps/s under the kernel's global mount-namespace lock; each activation's cgroup attach stalls every fork on the node; `app_restore` 100 → 400 to 570 ms. Download is flat at ~160 to 190 ms for the 10 MiB image.
9. **Pause mode: bytes on the local disk**, then per-VM start-up failures and page-cache pressure once the disk was fast enough.
10. **Resident capacity:** memory per VM for microVM (127 MiB); CPU for gVisor (0.01 vCPU per agent at this event rate), with the ARP neighbour table and the router's 5-s parked-request budget as the two walls that had to be removed first.

Not limiting anywhere: the control plane and database, the router (its 5-s budget converts a latency cliff into 503s but does not cause it), host memory on the turnover tests, CPU count.

---

## 7. What would move the numbers next (ranked in the campaign report)

microVM, per-activation path (17 → 20+ swaps/s):

1. **Cloud Hypervisor on-demand restore.** The worker has the mode but `restoreMemMode()` forces eager copy on CH ≥ v53; eager copy is the `vm_restore` 140 ms → 1.5 s under load. Single largest lever.
2. **Pool the network namespace together with its tap per worker.** The pool covers only the veth shape; microVM still does a bind mount and 5 to 6 netlink round trips under `rtnl_lock` per tap. About 1.3 s of the 5.5 s restore at 21/s.
3. **Pre-spawn idle VMMs**, one virtiofsd per worker, stop scanning `/proc` and mountinfo twice per activation, keep the cgroup leaf and bundle overlay, asynchronous batched teardown.

Fewer bytes per snapshot: drop the guest page cache before `vm.snapshot`; a balloon with free-page reporting; deduplicate against the golden image; a zstd dictionary trained on the golden image. Compress pause checkpoints like suspend ones (disk bytes ÷ 3).

Transfer path: resume from the node-local retained copy ([0ccab235](https://github.com/aditya-shantanu/substrate/commit/0ccab235), resume P50 −0.6 s and zero download bandwidth for same-node resumes); let worker pods fetch their own snapshots instead of one plugin per node.

gVisor: fewer mounts and chroots per sandbox in runsc (`--overlay2=none`, a long-lived gofer chroot upstream); `cgroup_favordynmods=1` on the kernel command line; parallel or lazy page loading in gVisor's restore, or keep the sentry alive across suspend/resume.

Node and kernel: a newer kernel with per-netns `rtnl_lock`; isolate the lifecycle path from the running fleet on separate CPU sets; two worker pools and two plugins per node.

Measurement: replace the relative 2.5× rule with an absolute SLO per runtime (microVM delivers 17/s at P90 1.4 s); tie the router's parked-request budget to measured restore P99; keep the harness fixes as the standard way to run the test.

---

## 8. Status of each Substrate change

| Change | Where | Status | What it needs |
|---|---|---|---|
| Retained node-local snapshot + schedule onto the producing node (C3 + C4) | `perf/resume-latency` 0ccab235, 2f4d9d46 | committed on the fork, not upstream | a size/age budget for retained copies, a default decision, alignment with #1551's cache directory and `SnapshotSharing`; C4 changes `ExternalSnapshot` and answers #1761 / #589 |
| End-to-end instrumentation (C0) and wake tracing (T1) | 20d10da3, d6c7d832, 7ad6e240 | committed on the fork | splits into several PRs; metrics registry additions |
| Egress mint overlap (C2), manifest reordering (C1) | 590efbb5 | committed on the fork | C2 is a clean win; C1 only if the cold asset case benefits |
| GCS client pool for every transfer | worktree patch | upstream has the equivalent in e2a9e2e | route the `GetObject` head range through the pool too |
| Pooled codecs and buffers, 4 MiB ranges × 12, decoder concurrency 4 | worktree patch | experimental | upstream review; defaults are safe |
| Sandbox network pool | worktree patch (`internal/ateomnet/netpool.go`) | experimental, default on in the patch | a flag, limits on pooled networks, gVisor measured win; microVM needs the tap pooled too |
| Tar fsync skipped | worktree patch | experimental, default off-fsync (approved for the experiment only) | a real flag and a durability decision; default should stay fsync |
| microVM reseed retry and raised waits | worktree patch + pause patch | experimental | proper retry policy upstream; keep virtiofsd logs on failure |
| Skip graceful VMM shutdown after checkpoint | worktree patch | experimental, no measured gain | optional |
| gVisor `--shared-root`, lean teardown, no app-container cgroup | worktree patch | experimental, default on in the patch | upstream review; lean teardown also fixes a cgroup leak worth its own issue |
| Reusable cgroup slots, `--ignore-cgroups` | worktree patch | rejected, default off | none |
| Node prep: neighbour table, inotify, scheduler, orphan reclaim | sysctl pods and manual steps | not persistent across reboots | Substrate node-preparation issues (#641 / #1757 / #1688 for the directory leak) |

---

## 9. Index of reports and data

Reports (also the `README.md` of the matching archive directory):

| Report | Archive in `assets/runs/` |
|---|---|
| Resume-latency journal | [`docs/dev/perf/resume-latency-journal.md` on `perf/resume-latency`](https://github.com/aditya-shantanu/substrate/blob/perf/resume-latency/docs/dev/perf/resume-latency-journal.md) |
| `nano-agent-activation-throughput-optimization-gvisor-vs-microvm-2026-10-09.md` | [`2026-10-09-throughput-campaign/`](https://github.com/aditya-shantanu/substrate-agents-tco/tree/main/assets/runs/2026-10-09-throughput-campaign) (README, `microvm/`, `gvisor/` incl. `gv-report.md`, `scripts/`, both worktree patches) |
| `nano-agent-pause-turnover-microvm-metal-2026-10-09.md` | [`2026-10-09-pause-campaign/`](https://github.com/aditya-shantanu/substrate-agents-tco/tree/main/assets/runs/2026-10-09-pause-campaign) |
| `resident-fill-nano-personal-agent-gvisor-vs-microvm-metal-2026-10-10.md` | [`2026-10-10-nano-resident-fill/`](https://github.com/aditya-shantanu/substrate-agents-tco/tree/main/assets/runs/2026-10-10-nano-resident-fill) |
| `nano-personal-agent-cold-start-and-turnover-5000-gvisor-vs-microvm-main-2026-10-08.md` | [`2026-10-08-nanoturn-README.md`](https://github.com/aditya-shantanu/substrate-agents-tco/blob/main/assets/runs/2026-10-08-nanoturn-README.md), `2026-10-08-nanoturn-{gvisor,microvm}-c3-metal-main/`, `2026-10-08-nanoturn-bottleneck/` |
| `turnover-at-500-resident-gvisor-vs-microvm-suspend-vs-pause-metal-2026-10-08.md` | `2026-10-08-turnover-README.md`, `2026-10-08-turnover-*-fill500/` |
| `resident-fill-personal-assistant-and-ping-gvisor-vs-microvm-metal-2026-10-07.md` | `2026-10-08-resident-README.md`, `2026-10-08-resident-*/` |
| `host-fill-personal-assistant-gvisor-vs-microvm-metal-2026-10-07.md` | `2026-10-07-fill-README.md`, `2026-10-07-fill-*/` |
| `personal-assistant-pause-vs-suspend-gvisor-vs-microvm-metal-2026-10-06.md` | `2026-10-06-pa-*/` |
| `cold-start-gvisor-vs-microvm-nested-vs-metal-2026-10-06.md` | `2026-10-06-cold-start-*/` |
| `one-ping-pause-gvisor-vs-microvm-metal-2026-10-05.md` | `2026-10-05-ping-*/`, `2026-10-06-ping-*/` |

Hardware used throughout: c3-standard-192-metal nodes (192 vCPU Sapphire Rapids, 768 GiB, native KVM, 3 TB Hyperdisk Balanced at 100k IOPS / 2,400 MiB/s, COS, GKE 1.36.4, 256 pods per node) in clusters `agents-tco-east` (us-east4-a, gVisor) and `agents-tco-euw4` (europe-west4-c, microVM); the resume-latency study used `resume-lat` in us-central1-a (c3-highmem-88 workers, one c3-highmem-192-metal). Runtimes: gVisor nightly 2026-09-02 via `gvisor-default`; Kata 4.1.0 with Cloud Hypervisor v53 via `microvm`.
