# Activation throughput with 650 awake nano agents — optimization campaign (2026-10-09)

Goal: raise steady-state suspend+resume throughput (swaps/s) with 650 nano-personal-agent actors awake and 5,000 registered, one c3-standard-192-metal node per runtime, Substrate main 66f8a888 plus the experimental patches listed per iteration. Full method, assumptions and the original numbers: `nano-personal-agent-cold-start-and-turnover-5000-gvisor-vs-microvm-main-2026-10-08.md`.

Each level runs 2 minutes (4 in the original test); the gates are: resume or suspend P90 > 2.5× the first level, errors or refusals > 0.5 % of wakes, more than 5 s' worth of swaps still in flight at the level's end (backlog), host memory/PSI limits, crashed actors (5, later 20). A level marked `pass` with a higher achieved rate than the last clean one but a `wake-p90-vs-baseline` verdict means the system delivered the rate with zero errors and only the relative-latency rule stopped the ramp.

## Result in one table

| | microVM (c3-standard-192-metal, 650 awake, 5,000 registered) | gVisor (same) |
|---|---|---|
| **Start of the day** (10-s burst ticks, main 66f8a888) | 2 swaps/s within the 2.5× latency rule; 4 fails | 2 swaps/s; 4 fails |
| Same build, 1-s ticks | 5 swaps/s | 5 swaps/s (8 fails the latency rule by a little) |
| **End of the day, within the bounds** (P90 ≤ 2.5× first level, < 0.5 % errors/refusals, no backlog growth) | **17 swaps/s** (iteration 19: 16.7 achieved, resume P50 1.30 s / P90 1.40 s / P99 1.56 s, suspend P50 1.09 s / P90 1.35 s, 0 errors, 0 refusals); 15 swaps/s held cleanly in six consecutive iterations with resume P50 ≈ 1.1 s | **9 swaps/s** — resume P50 0.96 s / P90 1.19 s, suspend P50 0.59 s, 0 errors |
| Delivered error-free beyond the latency rule | 17 (20 swaps/s collapses: resume P90 8.7 s, refusals) | 12 swaps/s (11.8 achieved, 0 errors, 0 refusals) |
| Where it breaks now | 18–21 swaps/s: every per-VM step on the node convoys at once (tap device setup 2 → 1,300 ms, VMM restore 140 → 1,500, teardown 230 → 1,460, load average 140) with the CPUs only ~35 % busy and nothing blocked on IO — kernel/VMM-level serialisation of sandbox create/teardown, plus ~2 GB/s of snapshot transfer | 12 swaps/s: the kernel's mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, gVisor's own restore (measured on the node) |


### Activations per second with resume and suspend latency, start of the day vs. end

One activation = one actor resumed from its suspended snapshot and answering a request; the test keeps 650 awake by suspending one actor for every one it resumes, so activations/s = swaps/s = suspends/s = resumes/s. Latencies are what the client saw (resume = request round trip through the router including the restore; suspend = SuspendActor call). ms, P50 / P90 / P99. "pass" = within the bounds (P90 ≤ 2.5× the run's first level, errors + refusals < 0.5 %, no backlog growth).

### microVM

| Build | Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Verdict |
|---|---|---|---|---|---|---|
| Start of the day: main 66f8a888, 10-s burst ticks | 1 | 0.96 | 1,257 / 1,379 / 1,516 | 2,209 / 2,360 / 2,458 | 0 + 0 | pass |
|  | **2** | **1.92** | **2,273 / 2,422 / 2,480** | **3,931 / 4,360 / 9,048** | 0 + 0 | **pass** |
|  | 4 | 3.83 | 3,030 / 3,909 / 5,405 | 8,128 / 8,566 / 8,766 | 0 + 0 | latency rule |
| End of the day: patched build, 1-s ticks | 12 | 11.90 | 908 / 951 / 1,042 | 1,041 / 1,269 / 1,637 | 0 + 0 | pass |
|  | 14 | 13.88 | 1,005 / 1,066 / 1,119 | 1,101 / 1,809 / 13,681 | 0 + 1 | pass |
|  | **17** | **16.72** | **1,298 / 1,397 / 1,560** | **1,092 / 1,345 / 1,615** | 0 + 0 | **pass** |
|  | 20 | 19.21 | 4,692 / 8,687 / 11,507 | 2,106 / 2,793 / 3,413 | 0 + 91 | refusals, latency rule |

_Bold = the highest rate within the bounds for that build._

### gVisor

| Build | Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Verdict |
|---|---|---|---|---|---|---|
| Start of the day: main 66f8a888, 10-s burst ticks | 1 | 0.96 | 1,243 / 1,431 / 1,630 | 951 / 1,079 / 1,221 | 0 + 0 | pass |
|  | **2** | **1.92** | **2,200 / 2,434 / 2,659** | **1,399 / 1,691 / 1,966** | 0 + 0 | **pass** |
|  | 4 | 3.83 | 4,089 / 4,368 / 4,596 | 2,507 / 2,985 / 3,232 | 0 + 0 | latency rule |
| End of the day: patched build, 1-s ticks | 2 | 1.98 | 486 / 618 / 696 | 462 / 541 / 645 | 0 + 0 | pass |
|  | 3 | 2.97 | 509 / 637 / 823 | 440 / 511 / 623 | 0 + 0 | pass |
|  | 4 | 3.97 | 576 / 703 / 837 | 467 / 539 / 688 | 0 + 0 | pass |
|  | 5 | 4.96 | 644 / 777 / 984 | 493 / 563 / 671 | 0 + 0 | pass |
|  | 7 | 6.94 | 802 / 966 / 1,203 | 532 / 646 / 727 | 0 + 0 | pass |
|  | **9** | **8.92** | **959 / 1,194 / 1,449** | **589 / 742 / 932** | 0 + 0 | **pass** |
|  | 12 | 11.80 | 1,449 / 1,997 / 2,481 | 893 / 1,114 / 1,473 | 0 + 0 | latency rule |

_Bold = the highest rate within the bounds for that build._



**What moved the numbers, in order of effect**

1. **Measuring correctly** (harness): issuing swaps steadily (1-s ticks) instead of bursts of N every 10 s. Every earlier latency figure was burst queueing. 2 → 5 swaps/s on both runtimes with no Substrate change.
2. **Harness again**: the sim replayed every skipped step's ops on an agent's first wake (its random day-offset "catch-up"), ~46 requests per first wake — a request storm proportional to the swap rate, present in every run until iteration 12. Off since (`SCRIPT_CATCHUP=false`).
3. **Snapshot plugin** (`pkg/objectstorage`, both nodes): one GCS connection per node → a pool used by every transfer; fresh 64 MiB upload buffers, 128 MiB download range buffers, encoders and decoders per transfer → bounded free lists and pooled codecs (page faults 150k/s → 300/s); 4 MiB download ranges, 12 in flight per object (per-stream GCS throughput here is ~45 MB/s). Rejected after measurement: composite uploads in 8 MiB parts (suspend P99 7.8 s), denser zstd level (guest memory compresses 41 → 40 MiB only, 3× the CPU), GC tuning (GOGC/GOMEMLIMIT: no effect on the ceiling).
4. **microVM worker** (`cmd/ateom-microvm`): guest CRNG reseed after restore given 15 s and a retry instead of 5 s and a crash (~1 restore in 500–1,000 was crashing the actor, and a crashed actor's deletion then hangs when its worker is gone); per-checkpoint tar fsync skipped (approved for the experiment); graceful VMM shutdown skipped after a checkpoint (no measurable gain); the gVisor campaign's sandbox network-namespace pool ported (no gain on microVM: its per-activation cost is the tap device, VMM launch/restore and teardown).
5. **Node**: `/var/lib/ate/actors` on tmpfs (restore/checkpoint staging off the boot disk) — removes IO pressure, no change to the ceiling; 37k orphaned actor directories (576 GB) reclaimed.
6. **gVisor worker** (`cmd/ateom-gvisor`, `internal/ateomnet`, by the background agent): pooled sandbox network namespaces (net_setup 170–330 → 1 ms), one shared gofer namespace per worker (`runsc --shared-root`), a lean post-checkpoint teardown that also fixed a ~30 % cgroup leak per suspend (8,157 cgroups for 650 actors before), no cgroup for the app container. Rejected: `--ignore-cgroups` (sentry sized from the host), reusable worker-owned cgroup slots (gofers die in a reused cgroup).

**Caveats.** Every iteration below purged and re-registered the 5,000 actors at its start (the deploy step runs `clean.sh`); the fleet is therefore fresh in every run, and the per-run numbers are comparable but each run's first level is both the gate baseline and a cold start — ramps were started two or more levels below the suspected ceiling. The experimental Substrate changes live uncommitted in two worktrees (`~/repos/substrate-east1` microVM, `~/repos/substrate-east` gVisor); the patches are in the archive. The fsync skip and the reseed/teardown changes are experiment-grade and need proper flags upstream. The harness fixes (1-s ticks, catch-up switch, readiness check, fleet reuse, backlog gate) are committed in `substrate-agents-tco`.


## Where the bottleneck moved, and what is happening now (for whoever picks this up)

The limiter changed five times during the day. Each hand-off below says what was measured, where in the code or kernel it sits, and what state it is in now.

**1. The test itself (fixed).** The original harness issued all N suspends and N resumes of a tick at the same instant every 10 s. Latency then grows linearly with N (microVM resume P50 1.26 / 2.27 / 3.03 s at 1 / 2 / 4 swaps/s) because the burst queues, and a 2.5× relative-latency gate trips on burst size, not on the system. Evidence: the same build at 1-s ticks passes 5 swaps/s on both runtimes. Anyone re-running this must use `SWAP_EVERY=1s` and start the ramp at least two levels below the suspected ceiling, because the first level is also the gate's baseline and a cold start.

**2. The harness's first-wake catch-up (fixed).** In swap mode the sim replayed every skipped step's ingest/write ops on an agent's first lap, i.e. right after its first wake. The router log showed 133 first-time wakes producing 6,170 requests in 30 s (~46 per wake), each also costing a `ResumeActor` call at the api-server. The storm scaled with the swap rate and was present in every run until iteration 12. `--script-catchup=false` (commit 605abef) removes it; all figures from iteration 12 on exclude it.

**3. The snapshot plugin, one process per node (fixed as far as it matters).** Three things inside `cmd/snapshot-plugin` / `pkg/objectstorage` bounded throughput before anything on the node did:
- *One GCS connection.* One `storage.Client` (one HTTP/2 connection) carried every transfer on the node; the pooled clients were only used for parts of objects over 16 MiB (download) / 64 MiB (upload), and the 10 MiB gVisor and 42 MiB microVM snapshots never qualified. Quiet probe, 20 concurrent restores: download 1,645 → 1,233 ms with a 64-client pool. (Upstream commit e2a9e2e by dberkov makes the same fix minus the `GetObject` head range; this build routes that too.)
- *Allocation churn.* Each upload allocated a fresh 64 MiB head buffer plus a 64 MiB media buffer for a 42 MiB object; each download 128 MiB of range buffers; every transfer a new zstd encoder or decoder. At 12 swaps/s that was 24 cores in the plugin, 8 of them page faults and zeroing (`runtime.memclrNoHeapPointers` 18 % of CPU, ~150k minor faults/s, RSS swinging 4–17 GB). Bounded free lists for the big buffers, pooled codecs, writer chunk sized to the object and a 1 MiB copy buffer took faults to ~300/s and CPU per swap down ~20 %. The ceiling did not move for that alone — the churn was a cost, not the limit.
- *Per-stream throughput.* A single GCS stream from this node delivers ~45 MB/s, so a 42 MiB object over three 16 MiB ranges capped at ~140 MB/s per restore. 4 MiB ranges with 12 in flight per object brought resume P90 at 15 swaps/s from 1,508 to 1,256 ms. Composite uploads in 8 MiB parts were tried and rejected (suspend P99 7.8 s: compose round trips and HTTP/2 flow control on the upload side). A denser zstd level was tried and rejected (guest memory compresses 41 → 40 MiB only, 3× the CPU). GC settings (GOGC 200/400, GOMEMLIMIT) changed nothing.
State now: at 17 swaps/s the plugin uses ~25 cores, its download stage is ~0.7–1.0 s per 128 MiB image (GCS first byte 50 ms, write 50 ms, the rest decode and body transfer), and it is no longer what gives out first.

**4. microVM worker defects on the path (fixed for the experiment, need proper flags).** (a) After a restore the worker asks the guest agent to reseed the CRNG with a 5-s timeout and no retry; under load ~1 restore in 500–1,000 timed out and the actor was marked CRASHED — and deleting a crashed actor whose worker pod is gone hangs in DELETING, which then answers 503 to every wake. 15 s + one retry: zero crashes since. (b) Every checkpoint fsynced its rootfs-upper tar on the shared boot disk (`internal/tarutil`), 0.5 → 2.4 s at 40 concurrent; skipped. (c) The graceful VMM shutdown after a checkpoint (p50 178 / p90 370 ms) was skipped; teardown did not get measurably shorter, so the rest of teardown dominates.

**5. Where it is now: the node's sandbox create/teardown path, in the kernel and the VMM.** This is the limiter on both runtimes and it is not Substrate Go code.

*microVM, 17 swaps/s clean, 20 collapses.* Worker-side stage medians from the `Restore/Checkpoint timing breakdown` lines (iteration 17; identical shape in 15, 16 and 19):

| stage (ms, median) | 8/s | 15/s | 21/s |
|---|---|---|---|
| restore: prep (egress prepare, sandbox-state cleanup, dirs) | 11 | 43 | 1,034 |
| restore: lowers (overlay mounts + virtiofsd spawn) | 16 | 60 | 514 |
| restore: tap device setup (netlink in the actor netns) | 2 | 3 | 1,347 |
| restore: vmm_launch (cloud-hypervisor spawn + API socket) | 12 | 15 | 191 |
| restore: vm_restore (CH restore of the 128 MiB image) | 114 | 168 | 1,541 |
| restore total (worker) | 205 | 389 | 5,492 |
| atelet download (plugin) | 548 | 629 | 886 |
| checkpoint: teardown (kill VMM + virtiofsd, unmount, netns/tap release) | 120 | 222 | 1,458 |
| checkpoint total (worker) | 193 | 302 | 1,561 |

At 21/s the node's CPUs are ~35 % busy (user 46 cores, system 20, irq 5 of 192), `procs_blocked` ≈ 0 (no IO wait), PSI io 0 with the state directory on tmpfs, load average ~140. Every unrelated per-VM syscall path slows 10–100× together: that is lock convoying — `rtnl_lock` for tap/veth/netns work, the mount namespace lock for the overlay/virtiofs mounts, cgroup and mm locks around 20 process spawns and kills per second, plus cloud-hypervisor's own restore of 128 MiB per VM. The pooled network namespaces (ported from the gVisor work) removed namespace creation but not the tap, and changed nothing here. Transfer is the second term: 20/s × (42 MiB down + 50 MiB up) ≈ 1.9 GB/s, where uploads start queuing on HTTP/2 flow control.

*gVisor, 9 swaps/s clean, 12 delivered error-free but over the latency rule.* From the gVisor agent's node probes (gv-report.md): sandbox network setup went 170–330 ms → 1 ms with the namespace pool; the per-sandbox gofer namespace went away with `runsc --shared-root`; a ~30 % cgroup leak per suspend (8,157 cgroups for 650 actors) was fixed by a lean teardown. What remains and grows with rate is `pause_create` (90 ms alone → 600–700 ms at 12/s), and the probe pins it: a bind+umount pair on the node goes 2.4 → 19 ms and `unshare -m` 1.5 → 18 ms at 12/s — runsc's gofer and sentry each build a chroot per sandbox under the kernel's global mount-namespace lock. Then cgroup v2 task migration (1 mkdir + 2 `cgroup.procs` writes + 1 rmdir per activation; each write takes `cgroup_threadgroup_rwsem` as a writer and stalls every fork on the node; the `favordynmods` mount option measured 42 → 2.7 ms per attach pair but did not survive a pool roll and needs the kernel command line), and gVisor's own restore inside the sentry (app_restore 100 → 400–570 ms at 12/s). Download is flat at ~170 ms for the 10 MiB image.

*Not limiting on either node:* the control plane (actors table 4 MB, outbox 9 MB, resume RPC handling sub-millisecond apart from the restore), the router (its 5-s parked-request budget converts the latency cliff into 503s but does not cause it), the boot disk (tmpfs made no difference), host memory (> 600 GB free), and CPU count.

### Where the bottlenecks are now, in short

**microVM — 17 activations/s clean, 20 collapses.** At 20/s every per-VM step on the node slows at once while the CPUs are ~35 % busy, nothing is blocked on IO (`procs_blocked` ≈ 0) and the load average goes to ~140: tap device setup 2 → 1,300 ms, VMM launch 12 → 190, VM restore 140 → 1,500, overlay staging 10 → 500, checkpoint teardown 230 → 1,460 (worker `Restore/Checkpoint timing breakdown` medians). That is lock serialisation in the kernel and VMM for sandbox create/teardown (netlink for the tap, mounts for the rootfs overlays and virtiofsd, process spawn and kill for cloud-hypervisor), not a Substrate loop — the snapshot plugin's own download stays at ~1.0 s at that rate and the GCS requests themselves at ~55 ms. Second term: ~2 GB/s of snapshot traffic at 20/s (42 MiB down + 50 MiB up per swap) — uploads start queuing on HTTP/2 flow control there. Levers left: a pool of pre-launched VMMs with their tap devices (takes vmm_launch, tap and most of teardown off the critical path), and fewer bytes per snapshot by dropping the guest page cache before the checkpoint (the 128 MiB memory image is mostly page cache; denser compression bought only 3 %).

**gVisor — 9 activations/s clean, 12 delivered error-free, 12 fails the latency rule.** The node probe during 12/s shows a bind+umount pair going 2.4 → 19 ms and `unshare -m` 1.5 → 18 ms: runsc's gofer and sentry each build a chroot per sandbox under the kernel's global mount-namespace lock, which is the `pause_create` growth (90 ms alone → 600–700 ms at 12/s) that survived the namespace and cgroup fixes. Then cgroup v2 task migration (1 mkdir + 2 `cgroup.procs` writes + 1 rmdir per activation, each a `cgroup_threadgroup_rwsem` writer that stalls all forks; `favordynmods` measured 42 → 2.7 ms per attach pair but needs the kernel command line to stick), and gVisor's own restore (app_restore 100 → 400–570 ms at 12/s inside the sentry). All three sit in gVisor or the kernel; the fixes are fewer mounts or a long-lived gofer chroot in runsc, `CLONE_INTO_CGROUP`, and `cgroup_favordynmods=1`.

**Common to both.** Snapshot transfer and the plugin are no longer limiting below ~20/s. The control plane (api-server, Postgres, router) never was: its tables are a few MB, resume RPC handling is sub-millisecond apart from the restore itself, and the router's only contribution is its 5-s parked-request budget, which converts a latency cliff into 503s.

## Improvements and what each one changed

Measured effects only; "ceiling" = highest activations/s within the bounds. Each row compares the iteration that introduced the change with the one before it on the same node, unless stated.

| # | Change | Where | Ceiling | Resume latency | Suspend latency | Node / plugin cost | Errors, crashes | Verdict |
|---|---|---|---|---|---|---|---|---|
| 1 | 1-s ticks instead of 10-s bursts | harness (`SWAP_EVERY`) | microVM 2 → 5, gVisor 2 → 5 | at 2/s: microVM P50 2,304 → 662 ms, gVisor 2,276 → 536 | microVM 3,992 → 1,001; gVisor 1,713 → 593 | none | none | kept; every earlier figure was burst queueing |
| 2 | Pooled GCS clients for every transfer (`ATE_GCS_CLIENT_POOL=64`) | snapshot plugin | microVM 5 (first 1-s run) | quiet probe, 20 concurrent restores: download 1,645 → 1,233 ms | — | — | — | kept (same fix as upstream e2a9e2e plus the small-object download path) |
| 3 | Pooled zstd buffers/encoders/decoders, fan-out cap 8 | snapshot plugin | — | — | at 2/s P50 1,001 → 889, P90 1,246 → 1,040 | page faults 157k → 56k/s at 2/s; CPU per swap −20 % | — | kept |
| 4 | Tar fsync skipped after checkpoint | microVM worker (`internal/tarutil`) | 5 → 8 together with #5 | — | rootfs-upper stage no longer grows with concurrency (was 0.5 → 2.4 s at 40 concurrent) | PSI io lower | — | kept for the experiment; needs a flag upstream |
| 5 | Guest CRNG reseed 15 s + retry (was 5 s, no retry) | microVM worker (`restore.go`) | 8 reachable (holds stopped failing on the crash gate) | — | — | — | restore crashes ~1 per 500–1,000 → 0 in later runs; no more DELETING-stuck actors | kept; needs upstream fix |
| 6 | Crash gate 5 → 20, fleet excludes DELETING/CRASHED actors | harness | made results readable | — | — | — | spurious level failures gone | kept |
| 7 | Bounded free lists for 64 MiB upload / 16 MiB range / 1 MiB copy buffers, writer chunk = object size | snapshot plugin | 15 unchanged | — | — | page faults 150k/s → 300/s at 10/s; RSS stable | — | kept; a cost, not the limit |
| 8 | GOGC 400 + GOMEMLIMIT 96 GiB; then GOGC 200 | snapshot plugin env | 15 / 14 unchanged | — | — | heap grew to 91 GB under the limit; no effect | — | reverted to GOGC 200 |
| 9 | Graceful VMM shutdown skipped after checkpoint | microVM worker | unchanged | — | teardown 258 → 258–322 ms (no gain) | — | — | kept, harmless |
| 10 | Orphan actor directories reclaimed (37k dirs, 576 GB) | node | — | the run right after it failed at 14/s from the disk's background work | — | disk 614 → 21 GB used | — | do it well before measuring |
| 11 | `/var/lib/ate/actors` on tmpfs | node | unchanged (15) | resume P90 at 15/s 2,354 → 1,468 together with #12 | — | PSI io 13 → 0; `procs_blocked` 0 | — | kept; removes the disk, not the ceiling |
| 12 | Sim first-wake catch-up off (`SCRIPT_CATCHUP=false`) | harness | 15 held with margin | at 15/s: P50 1,651 → 1,305, P90 2,354 → 1,468; at 8/s P90 1,139 → 864 | at 15/s P90 1,813 → 1,360 | ~46 extra requests per first wake and one ResumeActor each removed | — | kept; affected every run before iteration 12 on both nodes |
| 13 | Instrumented GCS transport (`ATE_GCS_HTTP_STATS`) | snapshot plugin | diagnostic | showed no throttling: all 2xx, request p50 55 ms | — | — | — | off |
| 14 | 4 MiB download ranges, 12 in flight per object | snapshot plugin | 15 unchanged, 17 later | at 15/s P90 1,508 → 1,256 ms | — | — | — | kept |
| 15 | Composite uploads in 8 MiB parts | snapshot plugin | unchanged | — | at 15/s P50 1,119 → 1,603, P99 1,770 → 7,817 | 300 upload goroutines in HTTP/2 flow control at 21/s | — | reverted |
| 16 | Sandbox network-namespace pool ported to the microVM worker | `internal/ateomnet` | unchanged (15 → 17 came from the fine ramp, not this) | at 15/s P90 1,508 → 1,223 ms | — | tap stage unchanged (the microVM cost is the tap, not the namespace) | — | kept, no measurable gain on microVM |
| 17 | Denser zstd level (`ATE_ZSTD_LEVEL=default`) | snapshot plugin | 21/s collapsed harder | at 15/s P50 1,097 → 1,391 | — | image 41–42 → 39–42 MiB (−3 %); ~3× compression CPU | — | reverted |
| 18 | Fine ramp from 12 (final configuration) | harness | **microVM 17** (16.7 achieved) | 1,298 / 1,397 / 1,560 ms | 1,092 / 1,345 / 1,615 ms | plugin ~25 cores | 0 errors, 0 refusals, 0 crashes | final |
| g1 | Plugin/atelet patch (#2, #3) on gVisor | snapshot plugin | 5 → 8 | at 8/s P50 1,305 → 1,150 | 989 → 1,246 (noise) | — | — | kept |
| g2 | Sandbox network-namespace pool (`ATE_NETNS_POOL`) | `internal/ateomnet` (gVisor) | 8 | net_setup 170–330 → 1 ms; at 12/s P50 2,563 → 1,618 | 12/s park 1,246 → 1,104 | ~95 % pool hit rate | 0 adoption failures | kept |
| g3 | `runsc --shared-root` (one gofer namespace per worker) | gVisor worker | 8 | at 8/s P90 1,315 → 1,254 | teardown −80 ms at 8/s | — | — | kept |
| g4 | `--ignore-cgroups` | gVisor worker | 8 → 5 | at 8/s P50 1,076 → 2,328 | — | sentry sized from the host (192 vCPUs) | — | rejected |
| g5 | Reusable worker-owned cgroup slots | gVisor worker | run failed | — | — | — | 4,806 actors crashed (gofers die in a reused cgroup) | rejected |
| g6 | Lean post-checkpoint teardown + no app-container cgroup | gVisor worker | 8 | at 8/s P50 1,076 → 912, at 12/s 1,584 → 1,337 | — | cgroup leak fixed: 8,157 → 650 cgroups; every cgroup op faster | — | kept |
| g7 | Catch-up off + ×1.25 ramp | harness | **gVisor 9** | 959 / 1,194 / 1,449 ms | 589 / 742 / 932 ms | — | 0 errors, 0 refusals | final |


## How to break the remaining bottlenecks — ranked proposals (code-grounded; no idea excluded)

Every proposal names the measured cost it attacks (from the stage tables above and the node probes), the code it touches on main 66f8a888, an estimate of what it buys, and effort. Facts about what the code can and cannot do today were checked in the tree, not assumed.

### A. microVM: take work off the per-activation path (the 17 → 20+ swaps/s wall)

1. **Stop reading the whole memory image eagerly on restore — use Cloud Hypervisor's on-demand mode.** The worker already implements both modes (`cmd/ateom-microvm/internal/ch/prefault.go:23-30`: `OnDemand` = userfaultfd demand paging, `Copy` = eager) but `restoreMemMode()` (`restore.go:56-67`) forces eager on CH ≥ v53 because CH started prefaulting unconditionally (PRs #8150/#8556). We run CH v53. Eager restore is why `vm_restore` costs 140 ms alone and 1.5 s at 21/s (it copies 128 MiB per VM into the memfd, 2.7 GB/s of page allocation at 21/s), and why the next snapshot is the full 128 MiB again. Fix: get CH to honour the on-demand/no-prefault request again (newer CH exposes it as a restore option; otherwise pin a CH build that does) and re-enable `MemRestoreOnDemand`. Expected: `vm_restore` → tens of ms, memory churn per swap ÷ 3, images sparser, and the delta-merge path (`mergeOnDemandDelta`, `checkpoint.go:304-311`) gives incremental checkpoints. Effort: small in Substrate, depends on the CH side. This is the single largest lever on microVM.

2. **Pool the network namespace *with its tap* per worker.** The namespace pool ported from the gVisor work only applies to the veth shape (`internal/ateomnet/netpool.go:118`); the microVM path still creates and destroys a namespace (1 bind mount + unmount, `netns.go:55-88`) and does 5–6 netlink round trips under `rtnl_lock` per tap (`net.go:54-99`), and `tap` is the stage that goes 2 → 1,347 ms at 21/s. The tap's MAC and address are constants (`net.go:36-45`) and CH takes the tap by file descriptor (`restorefds.go:143-147`), so a pooled namespace can keep its tap and hand the same FDs to the next VM. Expected: removes the `tap` stage and the namespace mount/unmount from every activation; at 21/s that is ~1.3 s of the 5.5 s restore and part of the 1.5 s teardown. Effort: a day; the pool machinery exists.

3. **Pre-spawn idle VMMs.** `LaunchVMM` (`restorefds.go:62-87`) execs `cloud-hypervisor --api-socket` and pings it every 10 ms; 12–15 ms alone, 190–290 ms at 20/s because fork/exec convoys with everything else. A per-worker pool of N idle VMM processes (no VM yet) that a restore claims, plus `vm.delete` and re-use after teardown, removes `vmm_launch` and half the process churn of teardown (`Process.Kill()+Wait` for CH and virtiofsd, `checkpoint.go:369-377`). CH supports `vm.delete` followed by another `vm.restore` on the same VMM; the code never exercises it. Expected: −0.2–0.3 s per restore at load, fewer forks on the node. Effort: 1–2 days.

4. **One virtiofsd per worker instead of one per VM.** `StartVirtiofsd` (`overlay_linux.go:143-172`) spawns a daemon per actor with `--shared-dir=<SharedDir(id)>` and find-paths pinned to `<baseID>/rootfs`; it is spawned in `lowers` (16 → 514 ms at 21/s) and killed in teardown. A per-worker daemon serving a share root with per-actor subdirectories removes a spawn and a kill per activation and most of the `lowers` growth. Effort: medium (find-paths and the snapshot's recorded paths must stay stable).

5. **Stop scanning the node twice per activation.** `cleanupSandboxState` runs at restore (`restore.go:234`) and again at teardown (`checkpoint.go:385`); each parses `/proc/self/mountinfo` (2,500 lines on this node), MNT_DETACHes under two trees, and scans `/proc/*/cmdline` across ~6,300 processes to find orphans (`cleanup_linux.go:50-130`). That is O(processes) work, 40 times a second at 20/s, taking mount and task-list locks every time. The worker tracks its `runningActor`; skip the scans when the actor is known and reserve the full sweep for recovery. Also `imagecache.UnmountAllUnder` parses mountinfo again. Expected: cuts `prep` (11 → 1,034 ms at 21/s) and part of teardown; nearly free to do.

6. **Reuse the cgroup leaf and the bundle overlay.** `OpenActorLeaf`/`RemoveActorLeaf` mkdir+rmdir a cgroup per activation (`ateomcgroup/actor.go:47-70`); the bundle lower overlay (`imagecache.SetupBundleRootfs`, one `fsmount` per container) is content-addressed and could stay mounted across the actor-dir reset that atelet performs. Each saves a mount or a cgroup operation under a global lock per activation. Effort: small each; the gVisor campaign measured cgroup ops at 7 → 24 ms (mkdir) and 1 → 157 ms (rmdir) under load.

7. **Make teardown asynchronous and batched.** After `vm.snapshot` returns, the suspend's client-visible latency still includes kill/wait of two processes, the unmounts and the namespace release (`teardown` 120 → 1,458 ms). Return the checkpoint once the files are listed (`checkpoint.go:224`) and run teardown on a worker-local queue that holds the per-actor lock, coalescing unmounts and process reaps. Expected: suspend P50 −0.2 s at 15/s, more at 20/s; it also smooths the convoy because teardown no longer competes with the restores of the same tick. Effort: a day, with care for the restore-while-tearing-down ordering (the lock already exists).

### B. Fewer bytes per snapshot (helps every stage: download, decode, write, VMM restore, upload, network)

8. **Drop the guest page cache before `vm.snapshot`.** The 128 MiB image of a 256 MiB guest is mostly page cache from reading the rootfs over virtiofs; it compresses to 42 MiB and a denser zstd level only reached 40. The kata agent exposes `ExecProcess` and `MemAgentMemcgSet`/`MemAgentCompactSet` (`agent.proto:33,64-65`) which the worker does not wrap today (`agentclient.go` wraps 13 RPCs, none of them). `sync; echo 3 > /proc/sys/vm/drop_caches` via `ExecProcess` right before the snapshot (or memcg eviction via the mem-agent) should take the image to tens of MiB. Expected: ×2–4 fewer bytes everywhere: the transfer term at 20/s (≈1.9 GB/s) and the memory churn of restore both shrink proportionally. Effort: small (one RPC + a sandbox exec), needs a check that `/proc/sys` is writable in the exec context.

9. **Add a balloon with free-page reporting to the VM config.** `createvm.go:25-36` has no balloon device; with `balloon {size: 0, free_page_reporting: true}` the guest reports freed pages and CH punches them out of the memfd, so pages freed after the page-cache drop (or by the workload) stop appearing in later snapshots. Pairs with 8 and with on-demand restore (1). Effort: small, but it changes the VM config so existing snapshots must be regenerated (the golden snapshot too).

10. **Deduplicate against the golden image / between actors.** All 5,000 actors boot from one golden snapshot; the kernel, agent and rootfs pages are byte-identical across them. Content-addressed page chunks (4 KiB or CDC) with a per-node cache of the golden pages means an upload carries only the actor's unique pages and a restore fetches only the delta over a node-cached base. Firecracker's diff snapshots and the OnDemand delta merge already in this tree (`ch/merge.go`) are the existing shapes of this. Expected: order-of-magnitude fewer bytes for idle agents. Effort: large; the biggest structural win for cost as well as throughput (GCS egress and storage).

11. **Train a zstd dictionary on the golden image** and compress with it (`zstd.WithEncoderDict`/`WithDecoderDicts`): cheap to try, typically 10–30 % on page-image data that a plain level could not improve. Effort: hours.

### C. The transfer path

12. **Resume from the node-local copy when the actor comes back to the same node.** Branch commit 0ccab235 (`cmd/atelet/retained_snapshot.go`, `--retain-uploaded-snapshots`) kept the uploaded checkpoint on the node and restored from it when the URI matched; it is not on main. In this test (and in any node-affine scheduler) every resume lands on the node that suspended the actor, so the whole download stage (0.6–1.0 s, ~0.9 GB/s of GCS egress at 20/s) disappears; main already has the plumbing for local checkpoints (`CHECKPOINT_TYPE_LOCAL`, `LocalSnapshotDir`, `PreserveRestoreDir`) used by the pause path. Expected: resume P50 −0.6 s, download bandwidth → 0 for same-node resumes; needs a disk/tmpfs budget and an eviction policy. Effort: small to port, medium to make safe (invalidate on a newer snapshot).

13. **Spread transfers across the worker pods instead of one node process.** The plugin is a node singleton (`cmd/atelet/main.go:118`, `internal/objectstoreplugin/node.go`); every byte on the node goes through one process and one pod network interface, and its intrinsic ceiling in a quiet probe is ~0.8–0.9 GB/s of compressed intake. The workers (100 pods) could fetch and push their own snapshots given a new `ateompb` RPC (or the plugin socket mounted into them) and storage credentials; that multiplies connections, veths and processes by 100. Effort: medium; also removes the atelet restart as a disruption point.

14. **Keep one request per object for uploads, more streams for downloads** (done), and consider gRPC/direct-connect or HTTP/3 for GCS later; per-stream GCS throughput measured here is ~45 MB/s, so stream count is what matters.

### D. gVisor: the mount-namespace lock, cgroups, and the sentry

15. **Fewer mounts and chroots per sandbox in runsc.** The worker passes no runsc tuning flags at all (`cmd/ateom-gvisor/runsc.go:85-106`): upstream defaults, i.e. a rootfs overlay set up per container and a gofer with its own chroot per sandbox, each a burst of mounts under the global mount lock that the node probe measured at 2.4 → 19 ms per bind+umount pair at 12/s. Try `--overlay2=none` (or a shared backing directory), and `--shared-root` (already added by the campaign, keep it); longer term runsc needs a long-lived gofer chroot or fewer `mount(2)` calls per create. Expected: this is the `pause_create` growth (90 → 600–700 ms at 12/s). Effort: flags are hours; the runsc change is upstream work.

16. **Cheaper cgroup migrations.** Each activation does 1 mkdir + 2 `cgroup.procs` writes + 1 rmdir; each write is a `cgroup_threadgroup_rwsem` writer that stalls every fork on the node. `favordynmods` measured 42 → 2.7 ms per attach pair but has to be set where it sticks (kernel command line `cgroup_favordynmods=1`, or a privileged DaemonSet that remounts before the pool starts and after every roll); runsc using `CLONE_INTO_CGROUP` avoids the attach write entirely. The reusable-slot approach was tried and does not work (gofers die in a reused cgroup).

17. **gVisor restore itself** (app_restore 100 → 400–570 ms at 12/s) is inside the sentry: page-image loading and the restore of the kernel state. Options are upstream: parallel page loading, lazy restore (gVisor has a lazy-load mode for pages.img in recent releases), or keeping the sentry process alive across suspend/resume (checkpoint without exiting, "pause-to-disk" instead of full restore).

### E. Node and kernel

18. **Newer kernel.** Two of the convoys are known kernel scalability items: per-network-namespace `rtnl_lock` (merged upstream in 6.13-era networking) and finer mount-namespace locking work; GKE's COS node image pins the kernel, so this is a node-image choice, not a Substrate change. Worth testing on a newer COS/Ubuntu image when available.

19. **Isolate the sandbox lifecycle from the running fleet.** Pin the 650 running VMs' vCPU threads and the plugin to distinct CPU sets (or NUMA nodes) so the create/teardown path and the compression threads do not share run queues with the guests; the load average of 140 at 21/s says the scheduler is part of the convoy even at 35 % utilisation.

20. **Two nodes' worth of control on one node.** Run two worker pools and two snapshot plugins per node (sharded by actor), each with its own network interface and process space: it halves every per-process and per-interface ceiling without new hardware.

### F. Measurement and operations

21. The relative 2.5× latency rule should become an absolute SLO per runtime (e.g. resume P90 ≤ 1.5 s) for the production target; the relative rule was right for finding knees, but at the end of the day microVM delivers 17 swaps/s at P90 1.4 s and the rule, not the users, is what stops it.
22. The router's 5-s parked-request budget (`parking.go`) should be tied to the measured restore P99 so that a latency knee degrades instead of returning 503s.
23. Keep the fixes to the harness that this day produced (1-s ticks, catch-up off, readiness check that cannot roll the pool, fleet exclusion of DELETING/CRASHED actors, per-stage timing extraction) as the standard way to run this test.

**If only three things get done:** (1) on-demand restore + page-cache drop before snapshot (A1 + B8: fewer bytes and no eager copy), (2) a tap-carrying namespace pool and the removal of the two per-activation node scans (A2 + A5), (3) same-node resume from the retained local copy (C12). Together they remove most of the per-activation kernel work and most of the bytes; the expected ceiling is then set by VMM restore and transfer again, well above 20 swaps/s on this node.

## microVM (agents-tco-euw4, actor 2 vCPU + 256 MiB)

### Original test: 10-s ticks (burst of N), latency gate 2.5×, unmodified main

_baseline as reported on 2026-10-08_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 10 per tick | 1.00 | **0.96** | 1,257 / 1,379 / 1,516 | 2,209 / 2,360 / 2,458 | 0 + 0 | 0 | 2 | pass |
| 20 per tick | 2.00 | **1.92** | 2,273 / 2,422 / 2,480 | 3,931 / 4,360 / 9,048 | 0 + 0 | 0 | 2 | pass |
| 40 per tick | 4.00 | **3.83** | 3,030 / 3,909 / 5,405 | 8,128 / 8,566 / 8,766 | 0 + 0 | 0 | 2 | wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline |
| 20 per tick | 2.00 | **1.97** | 2,304 / 2,547 / 3,195 | 3,992 / 4,638 / 4,819 | 2 + 0 | 0 | 4 | pass |

### Rerun: 10-s ticks, latency gates OFF

_same build; shows the burst design collapsing at 4 swaps/s_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 20 per tick | 2.00 | **1.92** | 2,371 / 2,612 / 3,227 | 4,095 / 4,756 / 5,092 | 0 + 0 | 0 | 0 | pass |
| 40 per tick | 4.00 | **3.02** | 4,380 / 37,765 / 49,585 | 26,627 / 59,638 / 79,737 | 120 + 715 | 400 | 3 | refusals+errors+backlog |

### Iteration 1: 1-s ticks + pooled GCS clients (ATE_GCS_CLIENT_POOL=64)

_sim: SWAP_EVERY=1s, ×1.5 levels of 2 min, latency gate 2.5× back on; plugin: every transfer on a pooled client_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 662 / 866 / 1,279 | 1,001 / 1,246 / 1,496 | 0 + 0 | 2 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 677 / 824 / 1,026 | 1,041 / 1,240 / 1,474 | 1 + 0 | 3 | 1 | pass |
| 5 per tick | 5.00 | **4.96** | 801 / 946 / 1,287 | 1,275 / 1,466 / 1,748 | 1 + 0 | 5 | 2 | pass |
| 8 per tick | 8.00 | **5.41** | 1,209 / 1,679 / 7,921 | 1,607 / 4,428 / 52,906 | 119 + 54 | 488 | 2 | refusals+errors+park-p90-vs-baseline+backlog |

### Iteration 2: + pooled compression buffers/encoders/decoders, fan-out cap 8, GOGC=200; worker: tar fsync skipped

_INVALID beyond 2 swaps/s: 22 actors stuck DELETING answered 503 to every wake (sim now excludes them)_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 650 / 839 / 6,691 | 889 / 1,040 / 1,253 | 0 + 0 | 2 | 2 | pass |
| 3 per tick | 3.00 | **2.79** | 633 / 751 / 857 | 894 / 1,046 / 1,200 | 0 + 0 | 30 | 2 | backlog |
| 2 per tick | 2.00 | **1.99** | 626 / 730 / 912 | 889 / 1,031 / 1,204 | 1 + 0 | 0 | 3 | pass |

### Iteration 3: same build, fleet exclusion fix

_cut short by the crash gate (5 crashed actors already in the fleet)_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 661 / 869 / 1,250 | 906 / 1,064 / 1,225 | 0 + 0 | 0 | 5 | crashed |

### Iteration 4: + worker reseed timeout 15 s with retry (restore crash fix), crash gate 20

_first clean run of the full patch set: 8 swaps/s passes; 12 swaps/s delivered error-free, failed the 2.5× rule by 23 ms_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 636 / 751 / 980 | 900 / 1,065 / 1,166 | 0 + 0 | 2 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 650 / 725 / 869 | 921 / 1,110 / 1,333 | 0 + 0 | 3 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 719 / 824 / 1,416 | 986 / 1,157 / 1,329 | 0 + 0 | 5 | 0 | pass |
| 8 per tick | 8.00 | **7.92** | 923 / 1,114 / 1,825 | 1,080 / 1,357 / 1,774 | 0 + 0 | 16 | 0 | pass |
| 12 per tick | 12.00 | **11.80** | 1,341 / 1,901 / 2,816 | 1,228 / 1,534 / 2,207 | 0 + 0 | 12 | 0 | wake-p90-vs-baseline |
| 8 per tick | 8.00 | **7.96** | 898 / 1,109 / 1,675 | 1,068 / 1,271 / 1,638 | 0 + 2 | 16 | 0 | pass |

### Iteration 6: same build + pprof endpoint, ramp from 8 in ×1.2 steps

_15 swaps/s passes (14.75 achieved, 0 errors); 18 swaps/s fails (latency + 12 refusals). CPU profile at 12 swaps/s: 24 cores in the plugin, of which ~8 are page faults/zeroing of fresh 64 MiB upload buffers and 128 MiB download range buffers, ~9 compression, ~4 decoded-output write syscalls_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.87** | 914 / 1,097 / 1,395 | 1,072 / 1,268 / 1,530 | 0 + 1 | 8 | 0 | pass |
| 10 per tick | 10.00 | **9.88** | 1,025 / 1,238 / 1,549 | 1,114 / 1,321 / 1,669 | 0 + 2 | 10 | 0 | pass |
| 12 per tick | 12.00 | **11.80** | 1,338 / 1,849 / 2,315 | 1,308 / 1,641 / 1,929 | 0 + 2 | 12 | 0 | pass |
| 15 per tick | 15.00 | **14.75** | 1,651 / 2,354 / 3,255 | 1,408 / 1,813 / 2,319 | 0 + 1 | 30 | 0 | pass |
| 18 per tick | 18.00 | **17.42** | 3,479 / 5,216 / 13,106 | 1,885 / 2,468 / 3,400 | 2 + 12 | 54 | 0 | refusals+wake-p90-vs-baseline |

### Iteration 7: plugin v5 (bounded free lists for 64 MiB upload head / 16 MiB range / 1 MiB copy buffers, writer chunk = object size, decoder concurrency 4) + GOGC=400 GOMEMLIMIT=96GiB; ramp from 10 ×1.2

_page faults gone (300/s vs 150k/s) but the ceiling did not move: 15 passes, 18 fails (resume P50 7.6 s, 410 refusals); plugin heap grew to 91 GB under the memory limit_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 10 per tick | 10.00 | **9.83** | 968 / 1,232 / 1,514 | 1,059 / 1,291 / 1,576 | 0 + 1 | 10 | 0 | pass |
| 12 per tick | 12.00 | **11.82** | 1,077 / 1,477 / 2,285 | 1,111 / 1,427 / 1,747 | 0 + 1 | 12 | 0 | pass |
| 15 per tick | 15.00 | **14.73** | 1,726 / 2,562 / 3,070 | 1,400 / 1,779 / 2,395 | 0 + 0 | 30 | 0 | pass |
| 18 per tick | 18.00 | **17.08** | 7,603 / 11,944 / 16,169 | 1,832 / 2,793 / 3,650 | 0 + 410 | 126 | 0 | refusals+wake-p90-vs-baseline+backlog |
| 15 per tick | 15.00 | **13.29** | 1,612 / 2,530 / 16,656 | 1,388 / 1,893 / 6,674 | 56 + 96 | 510 | 2 | refusals+errors+backlog |

### Iteration 8: same build, GOGC=200 and no GOMEMLIMIT; ramp from 12 ×1.15

_14 passes, 17 fails: the plugin's download stage goes from 1.1 s to 7.2 s median between 14 and 17 swaps/s while uploads stay at 0.7 s — the restore download pipeline saturates at ~15-16 swaps/s_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 12 per tick | 12.00 | **11.80** | 1,381 / 1,813 / 2,329 | 1,198 / 1,490 / 1,845 | 0 + 0 | 12 | 0 | pass |
| 14 per tick | 14.00 | **13.77** | 1,660 / 2,449 / 2,842 | 1,296 / 1,665 / 2,036 | 0 + 0 | 14 | 0 | pass |
| 17 per tick | 17.00 | **16.03** | 8,294 / 9,373 / 10,170 | 1,482 / 1,921 / 2,332 | 0 + 33 | 136 | 0 | refusals+wake-p90-vs-baseline+backlog |

### Iteration 10: same build, ramp from 8 ×1.3

_8 and 11 pass; 15 fails with PSI io 13 % (it had passed in iterations 6-7 at PSI io 7) — the disk is back in the hot path_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.93** | 860 / 1,139 / 3,048 | 1,003 / 1,193 / 1,442 | 0 + 1 | 8 | 0 | pass |
| 11 per tick | 11.00 | **10.82** | 1,128 / 1,602 / 2,388 | 1,200 / 1,569 / 1,884 | 0 + 2 | 11 | 0 | pass |
| 15 per tick | 15.00 | **13.88** | 2,743 / 10,607 / 11,094 | 1,492 / 1,944 / 2,562 | 0 + 303 | 135 | 0 | refusals+wake-p90-vs-baseline+backlog |

### Iteration 11: /var/lib/ate/actors moved onto tmpfs (restore and checkpoint staging no longer touch the boot disk)

_8 passes with a long tail, 11 fails — PSI io 0 but the sim's own first-wake catch-up storm (see iteration 12) was found in this run's router log_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.87** | 1,142 / 2,333 / 4,109 | 964 / 1,141 / 1,348 | 0 + 0 | 8 | 0 | pass |
| 11 per tick | 11.00 | **10.18** | 4,888 / 13,776 / 14,069 | 1,019 / 1,204 / 1,531 | 0 + 520 | 110 | 0 | refusals+wake-p90-vs-baseline+backlog |

### Iteration 13: plugin with an instrumented GCS transport (ATE_GCS_HTTP_STATS=1), ramp started at 11

_collapsed at the first level; the counters showed no GCS throttling (all 2xx, per-request p50 55 ms) — the instrumented transport and the cold first level are both suspects_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 11 per tick | 11.00 | **10.07** | 6,918 / 16,771 / 19,258 | 1,019 / 1,243 / 3,087 | 0 + 568 | 165 | 0 | refusals+backlog |

### Iteration 14: same image, default transport again, ramp from 8

_collapsed at 8 swaps/s (download stage 15 s mean) although a quiet 1/20-wake probe on the same build is normal (0.55 s / 1.2 s) — cause not pinned; the plugin's intrinsic aggregate ceiling in the quiet probe is ~0.75-0.9 GB/s of compressed intake, i.e. ~15-16 swaps/s at 42 MiB per snapshot_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.12** | 16,890 / 21,229 / 24,695 | 949 / 1,119 / 1,384 | 0 + 647 | 144 | 0 | refusals+backlog |

### Iteration 15: plugin v8 — 4 MiB download ranges with 12 in flight per object, composite uploads in 8 MiB parts from 8 MiB (per-stream GCS throughput here is ~45 MB/s, so more streams per object)

_best resume yet at 15 swaps/s (P50 1,087 / P90 1,256 ms) but the composite uploads made suspend worse (P99 7.8 s). At 21 swaps/s everything on the node slows at once: worker tap setup 2 → 1,282 ms, VM restore 136 → 1,537, prep 13 → 1,038, teardown 232 → 1,462, load average 142, 300 upload goroutines waiting on HTTP/2 flow control (outbound network saturated with 8 MiB parts); the plugin's own download stayed ~1.0 s. The microVM wall above ~15-20 swaps/s is the node's sandbox create/teardown path (tap/netlink, mounts, VMM launch) plus ~2 GB/s of combined transfer_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.93** | 786 / 873 / 952 | 1,273 / 1,366 / 1,760 | 0 + 1 | 8 | 0 | pass |
| 11 per tick | 11.00 | **10.91** | 924 / 1,014 / 2,133 | 1,400 / 1,506 / 4,060 | 0 + 0 | 11 | 0 | pass |
| 15 per tick | 15.00 | **14.77** | 1,087 / 1,256 / 1,351 | 1,603 / 2,271 / 7,817 | 0 + 1 | 30 | 0 | pass |
| 21 per tick | 21.00 | **19.79** | 6,384 / 8,928 / 10,977 | 6,860 / 7,588 / 8,386 | 0 + 86 | 210 | 0 | refusals+wake-p90-vs-baseline+park-p90-vs-baseline+backlog |

### Iteration 16: plugin v9b — the 4 MiB/12 downloads kept, uploads back to one request per object

_15 swaps/s clean (resume 1,113 / 1,508 ms, suspend 1,119 / 1,352); 21 collapses the same way as iteration 15 — the ceiling is not in the plugin any more_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.93** | 728 / 807 / 949 | 954 / 1,138 / 1,805 | 0 + 0 | 0 | 0 | pass |
| 11 per tick | 11.00 | **10.91** | 922 / 1,295 / 3,091 | 1,040 / 1,262 / 1,528 | 0 + 0 | 11 | 0 | pass |
| 15 per tick | 15.00 | **14.75** | 1,113 / 1,508 / 3,111 | 1,119 / 1,352 / 1,770 | 0 + 0 | 15 | 0 | pass |
| 21 per tick | 21.00 | **19.62** | 6,749 / 9,883 / 12,320 | 2,504 / 3,368 / 4,062 | 0 + 213 | 231 | 0 | refusals+wake-p90-vs-baseline+park-p90-vs-baseline+backlog |

### Iteration 17: microVM worker with the sandbox network-namespace pool ported from the gVisor campaign (internal/ateomnet/netpool.go; the two workers share the package)

_no change: the microVM worker's per-activation cost is the tap device, VMM launch/restore and teardown, not namespace creation; 15 passes (resume 1,097 / 1,223), 21 collapses identically_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.93** | 714 / 776 / 1,019 | 979 / 1,154 / 1,352 | 0 + 1 | 8 | 0 | pass |
| 11 per tick | 11.00 | **10.91** | 869 / 927 / 1,069 | 1,043 / 1,286 / 1,704 | 0 + 0 | 11 | 0 | pass |
| 15 per tick | 15.00 | **14.75** | 1,097 / 1,223 / 1,389 | 1,119 / 1,393 / 2,794 | 0 + 2 | 15 | 0 | pass |
| 21 per tick | 21.00 | **19.67** | 6,504 / 9,525 / 12,440 | 2,394 / 3,221 / 3,974 | 0 + 159 | 189 | 0 | refusals+wake-p90-vs-baseline+park-p90-vs-baseline+backlog |

### Iteration 18: + denser zstd level for snapshot uploads (ATE_ZSTD_LEVEL=default)

_the guest memory image barely compresses better (39-42 MiB vs 41-42) and the extra CPU makes 21 swaps/s collapse harder; reverted_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.93** | 756 / 855 / 1,074 | 980 / 1,175 / 1,509 | 0 + 0 | 8 | 0 | pass |
| 11 per tick | 11.00 | **10.83** | 896 / 1,022 / 1,299 | 1,039 / 1,260 / 1,904 | 0 + 0 | 11 | 0 | pass |
| 15 per tick | 15.00 | **14.75** | 1,391 / 1,706 / 2,801 | 1,060 / 1,308 / 1,677 | 0 + 0 | 15 | 0 | pass |
| 21 per tick | 21.00 | **17.46** | 8,404 / 21,567 / 27,093 | 2,946 / 4,927 / 6,254 | 0 + 1270 | 567 | 0 | refusals+wake-p90-vs-baseline+park-p90-vs-baseline+backlog |

### Iteration 19: final configuration (plugin v9b, pooled namespaces, tmpfs, catch-up off, zstd fastest), fine ramp 12, 14, 17, 20

_17 swaps/s clean; 20 fails (resume P90 8.7 s, 91 refusals)_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 12 per tick | 12.00 | **11.90** | 908 / 951 / 1,042 | 1,041 / 1,269 / 1,637 | 0 + 0 | 12 | 0 | pass |
| 14 per tick | 14.00 | **13.88** | 1,005 / 1,066 / 1,119 | 1,101 / 1,809 / 13,681 | 0 + 1 | 28 | 0 | pass |
| 17 per tick | 17.00 | **16.72** | 1,298 / 1,397 / 1,560 | 1,092 / 1,345 / 1,615 | 0 + 0 | 17 | 0 | pass |
| 20 per tick | 20.00 | **19.21** | 4,692 / 8,687 / 11,507 | 2,106 / 2,793 / 3,413 | 0 + 91 | 100 | 0 | refusals+wake-p90-vs-baseline |

### Iteration 12: tmpfs + sim first-wake catch-up OFF (SCRIPT_CATCHUP=false)

_the catch-up replayed every skipped step's ops on an agent's first wake: ~46 requests per wake, a storm proportional to the swap rate, present in every earlier iteration on both nodes_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.93** | 790 / 864 / 984 | 958 / 1,151 / 1,441 | 0 + 0 | 8 | 0 | pass |
| 11 per tick | 11.00 | **10.83** | 935 / 1,032 / 1,221 | 1,008 / 1,197 / 1,426 | 0 + 0 | 11 | 0 | pass |
| 15 per tick | 15.00 | **14.77** | 1,305 / 1,468 / 1,974 | 1,133 / 1,360 / 1,653 | 0 + 0 | 15 | 0 | pass |
| 20 per tick | 20.00 | **18.82** | 6,294 / 7,085 / 9,337 | 1,226 / 1,506 / 2,024 | 0 + 15 | 160 | 0 | refusals+wake-p90-vs-baseline+backlog |

### Iteration 5: same build, ramp from 8 in ×1.2 steps

_baseline for the relative gate is the 8 swaps/s level here; the 12 swaps/s resume P90 (1,839 ms) is also within 2.5× of the 2 swaps/s baseline of iteration 4 (751 ms × 2.5 = 1,878)_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 8 per tick | 8.00 | **7.93** | 897 / 1,040 / 1,449 | 1,111 / 1,328 / 1,650 | 0 + 1 | 8 | 0 | pass |
| 10 per tick | 10.00 | **9.84** | 1,049 / 1,433 / 2,047 | 1,176 / 1,467 / 1,834 | 0 + 0 | 10 | 0 | pass |
| 12 per tick | 12.00 | **11.80** | 1,363 / 1,839 / 2,350 | 1,326 / 1,641 / 1,952 | 0 + 0 | 12 | 0 | pass |
| 15 per tick | 15.00 | **13.85** | 2,651 / 10,514 / 11,687 | 1,640 / 2,012 / 2,946 | 0 + 265 | 165 | 0 | refusals+wake-p90-vs-baseline+backlog |
| 12 per tick | 12.00 | **9.76** | 1,842 / 4,639 / 55,258 | 1,384 / 35,205 / 58,084 | 419 + 1403 | 920 | 54 | refusals+errors+wake-p90-vs-baseline+park-p90-vs-baseline+backlog+crashed |

## gVisor (agents-tco-east, actor 2 vCPU + 2 GiB)

### Original test: 10-s ticks, latency gate 2.5×, unmodified main

_baseline as reported on 2026-10-08_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 10 per tick | 1.00 | **0.96** | 1,243 / 1,431 / 1,630 | 951 / 1,079 / 1,221 | 0 + 0 | 0 | 0 | pass |
| 20 per tick | 2.00 | **1.92** | 2,200 / 2,434 / 2,659 | 1,399 / 1,691 / 1,966 | 0 + 0 | 0 | 0 | pass |
| 40 per tick | 4.00 | **3.83** | 4,089 / 4,368 / 4,596 | 2,507 / 2,985 / 3,232 | 0 + 0 | 0 | 0 | wake-p90-vs-baseline+park-p90-vs-baseline |
| 20 per tick | 2.00 | **1.97** | 2,276 / 2,557 / 2,780 | 1,713 / 1,942 / 2,167 | 0 + 0 | 0 | 0 | pass |

### Rerun: 10-s ticks, latency gates OFF

_8 swaps/s fails on errors: resume P50 crosses the router's 5-s parked-request budget_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 20 per tick | 2.00 | **1.92** | 2,296 / 2,516 / 2,789 | 1,956 / 2,234 / 2,367 | 0 + 0 | 0 | 0 | pass |
| 40 per tick | 4.00 | **3.83** | 4,168 / 4,523 / 4,883 | 3,564 / 3,994 / 4,314 | 0 + 0 | 0 | 0 | pass |
| 80 per tick | 8.00 | **7.67** | 5,524 / 6,223 / 6,640 | 3,642 / 4,593 / 5,126 | 452 + 0 | 0 | 0 | errors |

### gVisor agent iteration 1

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 536 / 660 / 938 | 593 / 713 / 992 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 610 / 733 / 957 | 616 / 761 / 995 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 840 / 1,056 / 1,211 | 717 / 922 / 1,109 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.87** | 1,305 / 1,821 / 2,706 | 989 / 1,288 / 1,616 | 0 + 0 | 8 | 0 | wake-p90-vs-baseline |
| 5 per tick | 5.00 | **4.98** | 842 / 1,062 / 1,226 | 740 / 936 / 1,152 | 0 + 0 | 0 | 0 | pass |

### gVisor agent iteration 2

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 557 / 762 / 1,008 | 588 / 763 / 940 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 614 / 757 / 973 | 623 / 745 / 962 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 813 / 929 / 1,083 | 713 / 866 / 1,074 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.87** | 1,150 / 1,374 / 1,649 | 861 / 1,037 / 1,257 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.62** | 2,563 / 3,178 / 4,106 | 1,246 / 1,695 / 1,958 | 0 + 0 | 36 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 3

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 539 / 669 / 785 | 580 / 714 / 847 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 594 / 675 / 879 | 591 / 696 / 807 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 800 / 968 / 1,208 | 683 / 878 / 1,159 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.88** | 1,080 / 1,315 / 1,859 | 838 / 1,035 / 1,267 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.72** | 1,618 / 2,363 / 3,005 | 1,104 / 1,572 / 1,962 | 0 + 0 | 24 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 4

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 530 / 656 / 782 | 552 / 650 / 796 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 597 / 687 / 811 | 566 / 670 / 789 | 0 + 0 | 3 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 784 / 935 / 1,071 | 650 / 728 / 870 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.91** | 1,076 / 1,254 / 1,425 | 783 / 958 / 1,215 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.77** | 1,584 / 2,365 / 2,877 | 1,024 / 1,367 / 1,658 | 0 + 0 | 24 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 5

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 691 / 797 / 890 | 651 / 823 / 921 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 761 / 862 / 971 | 677 / 842 / 956 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.92** | 991 / 1,174 / 1,355 | 850 / 1,069 / 1,274 | 0 + 0 | 5 | 0 | pass |
| 8 per tick | 8.00 | **7.77** | 2,328 / 2,786 / 3,166 | 1,860 / 2,265 / 2,520 | 0 + 0 | 24 | 0 | wake-p90-vs-baseline+park-p90-vs-baseline |

### gVisor agent iteration 6

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 652 / 791 / 935 | 648 / 777 / 908 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 731 / 842 / 1,022 | 669 / 792 / 918 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.92** | 885 / 1,025 / 1,190 | 750 / 893 / 1,046 | 0 + 0 | 5 | 0 | pass |
| 8 per tick | 8.00 | **7.87** | 1,165 / 1,420 / 1,640 | 943 / 1,129 / 1,312 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.80** | 1,824 / 2,565 / 3,151 | 1,293 / 1,654 / 2,063 | 0 + 0 | 12 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 8

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 619 / 703 / 782 | 543 / 635 / 716 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 668 / 779 / 943 | 531 / 642 / 895 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 819 / 939 / 1,085 | 600 / 719 / 951 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.87** | 1,144 / 1,324 / 1,535 | 844 / 1,085 / 1,260 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.77** | 1,937 / 2,738 / 3,295 | 1,328 / 1,802 / 2,199 | 0 + 0 | 24 | 0 | wake-p90-vs-baseline+park-p90-vs-baseline |

### gVisor agent iteration 9

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 493 / 602 / 853 | 471 / 549 / 740 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 520 / 592 / 704 | 458 / 517 / 600 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 659 / 795 / 963 | 511 / 581 / 688 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.87** | 912 / 1,090 / 1,322 | 575 / 726 / 876 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.80** | 1,337 / 1,651 / 2,154 | 895 / 1,112 / 1,308 | 0 + 0 | 12 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 10

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 483 / 588 / 800 | 455 / 543 / 662 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 510 / 617 / 752 | 446 / 512 / 611 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 653 / 806 / 942 | 503 / 576 / 837 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.92** | 877 / 1,061 / 1,386 | 558 / 705 / 815 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.81** | 1,326 / 1,625 / 2,190 | 862 / 1,117 / 1,417 | 0 + 0 | 12 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 11

_builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 486 / 618 / 696 | 462 / 541 / 645 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 509 / 637 / 823 | 440 / 511 / 623 | 0 + 0 | 0 | 0 | pass |
| 4 per tick | 4.00 | **3.97** | 576 / 703 / 837 | 467 / 539 / 688 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 644 / 777 / 984 | 493 / 563 / 671 | 0 + 0 | 0 | 0 | pass |
| 7 per tick | 7.00 | **6.94** | 802 / 966 / 1,203 | 532 / 646 / 727 | 0 + 0 | 0 | 0 | pass |
| 9 per tick | 9.00 | **8.92** | 959 / 1,194 / 1,449 | 589 / 742 / 932 | 0 + 0 | 9 | 0 | pass |
| 12 per tick | 12.00 | **11.80** | 1,449 / 1,997 / 2,481 | 893 / 1,114 / 1,473 | 0 + 0 | 12 | 0 | wake-p90-vs-baseline |
