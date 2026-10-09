# gVisor activation-throughput campaign — agents-tco-east (c3-standard-192-metal, 1 node, 100 worker pods)

Workload: nano-personal-agent, 5,000 registered actors (`turn-1791517949-gvisor-NNNN`, atespace agents-sim), 650 awake, 2 vCPU + 2 GiB.
Sim: swap mode, 1-s ticks, SWAP_START=2 x1.5 (2,3,5,8,12,18… swaps/s), 2-min levels, hold 4 min at the last clean level.
Gates: wake P90 > 2.5x the first level's P90 (FAIL_REL_P90=2.5), errors/refusals > 0.5 %, backlog (> 5 s of swaps in flight at level end), node memory/PSI/crashed.
Substrate: upstream main 66f8a888 in worktree /Users/adityashantanu/repos/substrate-east (+ the ACTOR_CPU deploy patch), uncommitted.
Raw data: /tmp/tco-runs/fill/gv-base/ (turn-iter*.txt = sim JSON lines, timing-iter*.txt = node "timing breakdown" lines, hostprobe-iter*.log, worker log samples, probe YAMLs, this report's generator assemble_report.sh) and /tmp/tco-runs/fill/nanoturn3-east-<n>/.

_Last updated: 2026-10-09 04:13 PDT._

## Status (incremental)

- Done: iteration 1 (baseline), 2 (plugin/atelet patch), 3 (netns pool), 4 (runsc --shared-root), 5 (runsc --ignore-cgroups, DIAGNOSTIC — rejected, see below). Next: iteration 6 = build 4 + node cgroup2 `favordynmods` remount (cheap task migrations), then worker-managed slot cgroups if mkdir/rmdir still convoy.
- Best so far within the bounds: **8 swaps/s clean** (from 5 at baseline); 12 swaps/s delivers 11.8 with 0 errors / 0 refusals but fails the 2.5x wake-P90 rule (P90 ≈ 2.37 s vs ≈1.65 s allowed).
- Root-cause chain so far (all measured on this node): (1) two sandbox netns per wake/suspend → pooled (net_setup 170-330 ms → 1 ms); (2) runsc's own per-sandbox gofer "null" netns → `--shared-root` (one per worker); (3) what remains in `pause_create` (90 ms alone → 300/530/600 ms at 5/8/12 swaps/s) is runsc's per-container cgroup create/join/remove: a host probe measures cgroup v2 `mkdir` 7→24 ms, a `cgroup.procs` attach pair 20→60 ms (max >200), `rmdir` 1→157 ms as load rises, and runsc does ~2 mkdir + ~4 attaches + 2 rmdir per activation.
- Iteration 5 (`--ignore-cgroups`) made things worse (8/s failed: wake P50 2.3 s): without a cgroup runsc sizes the sentry from the host (192 vCPUs, host memory), so restore/checkpoint/create all slowed (checkpoint 46 → 177 ms unloaded). Not a viable configuration; it only confirmed the cgroup work is a large share of pause_create (170 vs 300 ms at the same concurrency during the setup sweep).
- 01:05 finding: the node had accumulated **8,157 cgroups (3,786 `*-_pause` + 3,784 `*-glutton`) for 650 awake actors** and 118 GiB of unreclaimable slab. Cause: on ~30 % of suspends the app container's `runsc delete` fails (the pre-existing exit-128 race after `runsc checkpoint` stopped the sandbox), `cleanupContainers` returns on that first error so the pause container is never deleted, and both cgroups leak (pre-existing on main; invisible until a node runs thousands of swaps). Every cgroup op slows with the population, which is why builds 5-6 look slower than build 4 at light load. Iteration 7 (a) removes the stale cgroups while the fleet is parked, (b) runs the sandboxes in worker-owned reusable cgroup slots so runsc neither creates nor removes cgroups and nothing can leak, (c) replaces the post-checkpoint kill/wait/state/delete sequence with two `runsc delete -force` calls that do not stop at the first failure.
- 01:15: iteration 6 done (build 4 image). It is NOT a clean `favordynmods` test: the probe shows the cheap attaches (11 ms avg in the 00:45 bucket) lasted only until ~00:50 and the mount later read `rw,relatime` — the option was dropped during the worker-pool roll (a non-bind cgroup2 remount anywhere on the node resets it). With the cgroup population still growing (4,500 leaked `_pause` cgroups by 01:10) iteration 6 came out slightly worse than iteration 4 (12/s wake P50 1,824 / P90 2,565). Iteration 7 (slots + lean teardown + stale-cgroup cleanup) is running next; iteration 8 image (no cgroup for the app container at all) is built.
- 01:25-01:50: **iteration 7 (reusable cgroup slots + lean post-checkpoint teardown) FAILED hard**: on every worker the first activation works, but after the first checkpoint+teardown every subsequent `runsc create _pause` dies within ~30 ms — runsc reports `cannot create gofer process: creating gofer filestore files: failed to open directory "/proc/<gofer-pid>/root/.../bundles/_pause/rootfs": no such file or directory`, i.e. the freshly forked gofer is already gone. The sim's setup sweep hit this on all 100 workers and left **4,806 actors CRASHED** (194 SUSPENDED). The job then failed by itself; my attempt to stop it earlier was blocked by the tool permission policy. Recovery: the CRASHED actors are being deleted (background, ~10 min) and the next sim setup re-registers them (~20 min). Also learned from the node: runsc does not configure limits on a pre-existing cgroup (slots showed `cpu.max=max`, `memory.max=max`), so the slot design must write cpu.max/memory.max itself or runsc sizes the sentry from the host as in iteration 5. Root cause of the gofer death not yet identified (slot attributes are clean: no freeze, no limits, populated 0; the shared null-netns pin is intact; no leftover processes). A debug image (runsc `-debug-log` into the actor dir) is being rolled and will be exercised with a sim-free smoke test (resume/suspend/resume 60 spare actors) so the fleet is not put at risk again.
- 01:50-02:05: reproduced iteration 7's failure without the sim (smoke test: resume 60 spare actors, suspend, resume 60 more → 44 gofer deaths out of 58 creates). runsc debug logs on the node show the failing `runsc create` installs the slot cgroup, joins the pinned gofer "null" netns (`/proc/self/fd/25`), forks the gofer, and 2 ms later `/proc/<gofer>/root` is already gone; the gofer's own log file stays 0 bytes and nothing reaches stderr, so it dies before its first instruction of logging. Healthy creates (the first on each worker) create the null netns themselves instead of joining the pin. Two discriminating builds (A: slots off; B: shared-root off) are being A/B-tested with the smoke test to isolate whether the slot reuse or the pinned-netns join kills the gofer. The 4,806 CRASHED actors have been deleted (01:58); the fleet now has 194 registered actors and the next sim setup will re-register the rest (~20 min).
- 02:10: A/B on the node with the sim-free smoke test: **variant A (cgroup slots OFF, lean teardown ON, shared-root ON): 58 creates, 0 gofer deaths** → the reusable-slot cgroups are what kills the freshly forked gofer (a pre-existing, previously used cgroup; mechanism not pinned down — attributes are clean), and the lean post-checkpoint teardown is innocent. Variant B (slots ON, shared-root OFF) result below. Next sim iteration (8) uses build C = netns pool + shared-root + lean teardown (pause container now always deleted → no cgroup leak) + no cgroup for the app container (its processes live in the sentry, which the pause cgroup already bounds), with runsc-owned cgroups; it is smoke-tested first and the sim is started only if clean.
- 02:15: variant B (slots ON, shared gofer netns OFF): 44 creates, 26 gofer deaths → the reusable-slot cgroups fail on their own; the slot design is abandoned (runsc also refuses to configure limits on a cgroup it did not create, so it would have needed the worker to write cpu.max/memory.max anyway).
- 02:21: build C passed the smoke test (61 creates, 0 failures, 0 resume failures); **sim iteration 8 started 02:21:43** (its setup first re-registers the 4,806 deleted actors, ~25 min, then the usual sweep/fill/ramp; results expected ~03:05).
- 02:47: **iteration 8 (build C) done: 8 swaps/s clean, 12 fails (wake P50 1,937 / P90 2,738; park P90 also over)** — numerically worse than build 4 at 12/s although the kernel-convoy stages improved (pause_create 536 → 444 ms at 8/s, teardown 233 → 162 ms, no cgroup leak: exactly 650 `_pause` cgroups on the node during the ramp). The regression is in the sandbox-content stages (app_restore 149 → 241 ms at 8/s, 407 → 570 at 12/s; `runsc checkpoint` 46 → 115 ms even unloaded), and the fleet changed under iteration 8: 4,806 of the 5,000 actors were re-registered from scratch 20 minutes earlier. Compressed snapshot size is unchanged (pages.img.zstd 10.0 → 10.1 MB) but the restored logical image is larger (see below). To attribute fairly, iteration 9 re-runs build 4 on this same fleet (paired comparison), with a fork-latency probe running on the node to test the hypothesis that each `cgroup.procs` write (two per activation in runsc's create) stalls every fork/clone on the node.
- 02:56: iteration 9 is running **build C again**, not build 4: the harness's `workloads_ready` check failed at pipeline start (all three of its checks pass when run by hand a few minutes later; both re-deploys this night followed one of my pool rolls within a minute, so it looks like a transient `kubectl-ate` collision) and its deploy step rebuilt the worktree (= build C) and re-rolled the pool. Iteration 9 therefore measures build C's repeatability on the same fleet; iteration 10 (queued) switches the worktree's compile-time defaults to build-4 semantics (old teardown, per-container cgroups) so that whichever path deploys it, the paired comparison runs on identical actors. Fork-latency and cgroup-op probes are running on the node throughout.
- 03:13: **iteration 9 (build C repeat, same fleet, catch-up still on): 8/s clean with wake P50 912 / P90 1,090 (best so far); 12/s wake P50 1,337 / P90 1,651 vs a 1,505 gate (2.5 × 602) — fails by 10 %, 0 errors, backlog 12; park P50 895.** Iteration 8's content-stage regression was the fresh actors' first-generation snapshots; one swap later (iteration 9) the unloaded wake is the lowest of the campaign (493 ms).
- 03:15: main-thread finding: in swap mode the sim replayed every skipped step on an actor's first lap (~46 extra requests per first wake, each a ResumeActor at the api-server), loading router/api-server/actors in every iteration so far. The pipeline now defaults `SCRIPT_CATCHUP=false`. Iteration 10 = build C (already deployed) with catch-up off = the new reference; the build-4 paired run is deferred behind it.
- 03:18: the harness re-deployed a third time and rolled the pool to a fresh build of the worktree, which by then carried **build D** = build C + the pooled namespace handed to runsc as `/proc/<worker-pid>/fd/<n>` instead of two bind mounts + two unmounts per activation (`internal/ateomnet/netpool.go` adopt / `NSPath`, `runsc.go` shapeSpec overrides the OCI network namespace path; `ATE_NETNS_BIND_NAMES=1` restores names). It had not been smoke-tested, so the first minutes of iteration 10's setup sweep were watched live: 80 creates / 0 failures on a sampled worker, 0 setup failures. **Iteration 10 therefore = build D with catch-up off.** Lesson recorded: whatever is in the worktree is what the harness may deploy at any pipeline start.
- 03:38: **iteration 10 (build D, catch-up OFF): 8/s clean, wake P50 877 / P90 1,061, park P50 558; 12/s wake P50 1,326 / P90 1,625 vs a 1,470 gate (2.5 × 588) → fails by ~10 %, 0 errors, backlog 12.** Disabling the catch-up storm moved little on this node (iteration 9 → 10: 912 → 877 at 8/s, 1,337 → 1,326 at 12/s): the gVisor path was not router/api-server bound. Iteration 11 (queued) keeps build D and catch-up off but ramps ×1.25 (2, 3, 4, 5, 7, 9, 12, 15 …) to locate the ceiling between 8 and 12.
- 04:09: **iteration 11 (build D, catch-up off, ×1.25 ramp): 9 swaps/s clean — wake P50 959 / P90 1,194 (gate 1,545), park P50 589, 0 errors, 0 refusals, backlog 9 (≤ 5 s of swaps); 12 swaps/s delivers 11.8/s with 0 errors but wake P90 1,997 > 1,545 → fails the 2.5× rule. Final answer on this node: 9 swaps/s sustainable within the bounds (from 5 on main's baseline), 12 is the next level and fails only on latency.** Campaign closed at this point; the limiter is the kernel's mount-namespace lock driven by runsc's per-sandbox chroot setup (plus cgroup migration writers), see "Where it stands".

## Summary of levels per build (wake = resume as seen by the sim, park = suspend; ms)

| build | 5/s wake P50/P90 | 8/s wake P50/P90 | 12/s wake P50/P90 | 12/s park P50 | first failing level (reason) | last clean level |
|---|---|---|---|---|---|---|
| 1 baseline | 842/1062 | 1305/1821 | - | - | 8 (wake-p90-vs-baseline) | 5 |
| 2 +plugin/atelet patch | 813/929 | 1150/1374 | 2563/3178 | 1246 | 12 (wake-p90-vs-baseline) | 8 |
| 3 +netns pool | 800/968 | 1080/1315 | 1618/2363 | 1104 | 12 (wake-p90-vs-baseline) | 8 |
| 4 +shared-root | 784/935 | 1076/1254 | 1584/2365 | 1024 | 12 (wake-p90-vs-baseline) | 8 |
| 5 +ignore-cgroups (diagnostic, rejected) | 991/1174 | 2328/2786 | - | - | 8 (wake-p90-vs-baseline+park-p90-vs-baseline) | 5 |
| 6 build 4 + node cgroup2 favordynmods | 885/1025 | 1165/1420 | 1824/2565 | 1293 | 12 (wake-p90-vs-baseline) | 8 |
| 8 build C: no leak + no app-container cgroup | 819/939 | 1144/1324 | 1937/2738 | 1328 | 12 (wake-p90-vs-baseline+park-p90-vs-baseline) | 8 |
| 9 build C repeat (harness re-deployed the worktree) | 659/795 | 912/1090 | 1337/1651 | 895 | 12 (wake-p90-vs-baseline) | 8 |
| 10 build D (C + netns by descriptor), sim catch-up OFF | 653/806 | 877/1061 | 1326/1625 | 862 | 12 (wake-p90-vs-baseline) | 8 |
| 11 build D, catch-up OFF, x1.25 ramp | 644/777 | - | 1449/1997 | 893 | 12 (wake-p90-vs-baseline) | 9 |

## Where the time goes — stage medians (ms) per build at 5 / 8 / 12 swaps/s

| swaps/s | build | net_setup | pause_create | pause_restore | app_create | app_restore | worker restore total | checkpoint | teardown | worker checkpoint total | atelet download | atelet restore total |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 5 | 1 baseline | 183 | 176 | 26 | 43 | 124 | 612 | 47 | 271 | 325 | 157 | 816 |
| 5 | 2 +plugin | 170 | 178 | 25 | 41 | 117 | 589 | 47 | 268 | 321 | 158 | 797 |
| 5 | 3 +netns pool | 1 | 319 | 25 | 42 | 122 | 583 | 47 | 254 | 303 | 159 | 788 |
| 5 | 4 +shared-root | 1 | 301 | 25 | 41 | 121 | 550 | 47 | 185 | 236 | 169 | 770 |
| 5 | 5 +ignore-cgroups (rejected) | 1 | 330 | 56 | 51 | 238 | 733 | 133 | 285 | 426 | 202 | 1024 |
| 5 | 6 build4 + favordynmods | 1 | 276 | 29 | 71 | 182 | 629 | 121 | 208 | 335 | 170 | 871 |
| 5 | 8 build C: runsc-owned cgroups, lean teardown (no leak), no app-container cgroup | 1 | 261 | 26 | 49 | 186 | 592 | 120 | 40 | 162 | 164 | 806 |
| 5 | 9 build C repeat | 1 | 212 | 25 | 30 | 129 | 456 | 46 | 36 | 84 | 147 | 643 |
| 5 | 10 build D, catch-up OFF | 1 | 213 | 25 | 29 | 124 | 453 | 44 | 35 | 81 | 144 | 640 |
| 5 | 11 build D, x1.25 ramp | 1 | 219 | 24 | 26 | 129 | 459 | 44 | 35 | 80 | 135 | 631 |
| 8 | 1 baseline | 308 | 285 | 37 | 45 | 193 | 974 | 83 | 386 | 487 | - | - |
| 8 | 2 +plugin | 240 | 290 | 32 | 40 | 165 | 916 | 64 | 347 | 420 | 164 | 1133 |
| 8 | 3 +netns pool | 1 | 529 | 33 | 44 | 159 | 848 | 59 | 313 | 387 | 159 | 1063 |
| 8 | 4 +shared-root | 1 | 536 | 34 | 44 | 149 | 825 | 61 | 233 | 303 | 165 | 1057 |
| 8 | 5 +ignore-cgroups (rejected) | 4 | 673 | 130 | 156 | 771 | 1999 | 301 | 867 | 1178 | - | - |
| 8 | 6 build4 + favordynmods | 1 | 395 | 38 | 86 | 245 | 902 | 148 | 330 | 490 | 172 | 1150 |
| 8 | 8 build C: runsc-owned cgroups, lean teardown (no leak), no app-container cgroup | 1 | 444 | 48 | 38 | 241 | 854 | 174 | 162 | 330 | 180 | 1128 |
| 8 | 9 build C repeat | 1 | 414 | 29 | 20 | 170 | 716 | 50 | 41 | 94 | 133 | 900 |
| 8 | 10 build D, catch-up OFF | 1 | 386 | 28 | 20 | 163 | 678 | 47 | 37 | 86 | 142 | 863 |
| 12 | 2 +plugin | 329 | 640 | 183 | 144 | 506 | 2259 | 107 | 533 | 673 | 182 | 2469 |
| 12 | 3 +netns pool | 3 | 601 | 66 | 93 | 387 | 1378 | 105 | 467 | 594 | 160 | 1603 |
| 12 | 4 +shared-root | 2 | 616 | 52 | 96 | 407 | 1346 | 93 | 392 | 503 | 179 | 1589 |
| 12 | 6 build4 + favordynmods | 3 | 596 | 77 | 123 | 513 | 1522 | 223 | 471 | 697 | 195 | 1803 |
| 12 | 8 build C: runsc-owned cgroups, lean teardown (no leak), no app-container cgroup | 4 | 702 | 67 | 33 | 570 | 1625 | 256 | 458 | 730 | 193 | 1914 |
| 12 | 9 build C repeat | 2 | 669 | 35 | 23 | 276 | 1119 | 91 | 284 | 380 | 130 | 1273 |
| 12 | 10 build D, catch-up OFF | 1 | 614 | 35 | 25 | 286 | 1065 | 97 | 228 | 323 | 164 | 1315 |
| 12 | 11 build D, x1.25 ramp | 1 | 652 | 44 | 29 | 342 | 1169 | 115 | 235 | 362 | 183 | 1388 |

## Where it stands and what still limits it

**Sustainable activation throughput on gVisor, 650 awake of 5,000, within the rules: 9 swaps/s (iteration 11: wake P50 959 / P90 1,194 ms, park P50 589 ms, 0 errors, 0 refusals, backlog within 5 s), up from 5 swaps/s on main's baseline. Builds 2-10 all held 8 on the ×1.5 ramp; the ×1.25 ramp showed 9 is clean too. 12 swaps/s is delivered (11.8/s achieved) with 0 errors, 0 refusals and no backlog growth, but its wake P90 (1.6-2.0 s) is over the 2.5 × first-level rule (≈1.5 s), so it does not count.** The wake latency itself moved substantially: at 8 swaps/s wake P50 1,305 → 877 ms and park P50 989 → 558 ms (baseline → build D, catch-up off); at 12 swaps/s wake P50 2,563 → 1,326 ms (build 2 → D).

What the remaining 12-swaps/s latency is made of (build C/D, worker stage medians at 12/s, ms): `pause_create` ≈ 600-700, `app_restore` ≈ 400-570, `teardown` ≈ 400-460, `checkpoint` ≈ 100-250, download ≈ 160-190 flat, manifest ≈ 50 flat. The host is at ≈25 % CPU, 180 MB/s disk, 560 GiB free. The growth is serialisation in the kernel, triggered by what every activation still has to do:
1. **Per-activation cgroup create/join/remove by runsc** (1 mkdir, 2 `cgroup.procs` writes, 1 rmdir): probe-measured ≈60 ms at 12 swaps/s, ≈30 ms unloaded; each `cgroup.procs` write is a `cgroup_threadgroup_rwsem` writer (RCU grace period) that also stalls every fork/clone on the node. The sentry's vCPU count comes from that cgroup's `cpu.max`, so the cgroup cannot simply be dropped (iterations 5 and 7 showed what happens). Fixes are outside this repo: the cgroup2 `favordynmods` mount option (measured 42 → 2.7 ms per attach pair while it was on; must be applied so it survives remounts — kernel cmdline `cgroup_favordynmods=1`), or runsc placing gofer and sentry with `CLONE_INTO_CGROUP` instead of joining.
2. **Mount-namespace traffic per activation — the measured wall at 12 swaps/s.** runsc's gofer and sentry each build a chroot (a burst of mount/bind/pivot operations under the global `namespace_sem`), and the runsc parent waits for the gofer's chroot before it can proceed — the "banner → Gofer started" window grows from 29 ms alone to ≈300 ms at 8-12 swaps/s. The node probe shows the lock saturating exactly there: a bind+umount pair 2.4 ms → 4.1 ms (8/s) → 18.5 ms (12/s), `unshare -m` 1.5 → 13.6 ms. Substrate's own share (two overlay mounts/unmounts; until build D also two netns bind mounts/unmounts) is now minimal; the rest is runsc's chroot setup and teardown, i.e. a gVisor change (fewer mounts per sandbox, or a long-lived gofer/sentry chroot per worker) or a kernel with finer-grained mount locking.
3. **gVisor restore and checkpoint proper** (`app_restore`, `pause_restore`, `checkpoint`): 2-4 × slower at 12 swaps/s than alone with no host resource saturated — inside the sentry/runsc (restore of a ≈20 MB logical image, systrap stub creation, seccomp installs), not reachable from Substrate.
4. **Process spawns**: ≈6-8 `runsc` invocations per swap plus their gofer/sentry children; every fork waits behind the cgroup writers above (fork+exec 0.6 → 0.9 ms median, p90 1.9 ms at 12 swaps/s).

Software changes that moved the number (all in the east worktree, uncommitted): pooled GCS clients + pooled zstd buffers (plugin/atelet), pooled sandbox network namespaces (net_setup 170-330 ms → 1 ms), runsc `--shared-root` per worker (one gofer null-netns instead of one per sandbox), lean post-checkpoint teardown (teardown −60-90 ms and, more importantly, no more leaked cgroups: ~30 % of suspends used to leak two), no cgroup for the app container, pooled netns handed to runsc by descriptor (no bind mounts). Node-level: the stale-cgroup population and the `favordynmods` measurement are documented above; neither is applied persistently.

What a further step would need: (a) node image / kernel command line: `cgroup_favordynmods=1`; (b) gVisor: `CLONE_INTO_CGROUP` for gofer/sentry, and fewer mount operations in the gofer/sentry chroot setup (or a reusable gofer per worker); (c) Substrate: a single `runsc` round trip for create+restore of the sandbox and app container (today four), and keeping the atelet's manifest fetch + download (≈210 ms, flat) off the critical path by overlapping it with the worker's network/pause setup. Everything measured says the next wall after that is gVisor's own restore cost, not the node's hardware.

## Findings — what actually limits gVisor swaps on this node

1. **Not CPU, disk, memory or network.** At 12 swaps/s the host is 23-28 % busy (192 threads), disk 170-180 MB/s, 540+ GiB available, download 160-180 ms flat at every level (10 MiB snapshots). The sim's wake latency tracks the worker's `RestoreWorkload` almost 1:1 (atelet total ≈ sim P50), and inside the worker the growth with load sits in the stages that create and destroy kernel objects.
2. **Convoy 1 — Substrate's own sandbox namespaces (fixed, iteration 3).** Two netns + veth + nftables per wake, torn down per suspend: `net_setup` 170/240/330 ms at 5/8/12 swaps/s on main → 1 ms with the pool. Pool hit rate ~95 %, no adoption failures.
3. **Convoy 2 — runsc's gofer "null" netns (fixed, iteration 4).** One more unshare+pin per `runsc create` and one teardown per delete because `--shared-root` defaulted to the per-actor `--root`. Fixed with `--shared-root` per worker. Teardown (worker checkpoint stage) 313 → 233 ms at 8/s, 467 → 392 at 12/s; `pause_create` did not move, because…
4. **Convoy 3 — cgroup v2 create/attach/remove (iterations 5-7).** runsc timestamps inside `runsc create _pause` (from the worker logs, 4 workers, build 4):

   | swaps/s | exec → runsc banner | banner → "Gofer started" | gofer → "Sandbox started" | "Sandbox started" → exit | whole `runsc create` |
   |---|---|---|---|---|---|
   | 2 | 13 | 29 | 4 | 42 | 111 |
   | 5 | 14 | 176 | 25 | 96 | 335 |
   | 8 | 15 | 304 | 36 | 113 | 532 |
   | 12 | 18 | 266 | 56 | 169 | 601 |

   The "banner → Gofer started" window is where runsc creates the container's cgroup and joins it (`cgroup.procs`), and the tail is the sentry boot (which joins again and sets up its namespaces). A host probe doing mkdir / cpu.max+memory.max / attach-and-return / rmdir in a loop on the node's cgroup2 measured, under 5-8 swaps/s load: mkdir 7-24 ms, attach pair 20-60 ms (max >200 ms), rmdir 1-157 ms. runsc performs ~2 mkdir, ~4 attaches and 2 rmdir per activation (one cgroup per container, `ocispec.GVisorCgroupLeaf`).
   - `--ignore-cgroups` (iteration 5) removed that work but made runsc size the sentry from the host (192 vCPUs): every stage slowed (checkpoint 46 → 177 ms unloaded) and 8/s failed. Rejected.
   - cgroup2 `favordynmods` (node remount, iteration 6): under the same 5/s hold the probe's attach pair went 42.4 → 2.7 ms and rmdir 8.6 → 3.0 ms (mkdir unchanged ~15 ms). This is the kernel option made for exactly this pattern (cheap task migrations at the cost of slightly dearer fork/exit).
   - Reusable worker-owned cgroup slots (iteration 7): runsc neither mkdirs nor rmdirs (it only removes cgroups it created); the worker writes the slot into both containers' `cgroupsPath`, runsc writes the limits into it (sizing intact), the worker kills+reuses the slot after teardown.
5. **Still per activation and not touched:** `runsc` process spawns (create/restore/checkpoint/kill/wait/state/delete ≈ 8-10 execs of a 40 MB static binary per swap, ~15 ms each unloaded), the sentry boot proper, and gVisor's own restore (`app_restore` 100 ms alone → 150/400 ms at 8/12 swaps/s), plus `runsc checkpoint` 46 → 60-105 ms. Those are inside gVisor.
6. **Why the pause container must keep a real cgroup:** runsc sizes the sentry from its cgroup, not from the OCI spec — a sandbox whose cgroup carried no `cpu.max` booted with `loader.go: CPUs: 192` (runsc debug log on the node, 02:11). That is what made `--ignore-cgroups` (iteration 5) and the reusable slots (iteration 7, limits unconfigured) slower across the board. So the per-activation cgroup work that remains in build 8 is exactly one mkdir, the runsc create process's join/leave of that cgroup (two `cgroup.procs` writes, each a `cgroup_threadgroup_rwsem` writer), and one rmdir at delete; removing those needs either runsc changes (e.g. CLONE_INTO_CGROUP for gofer/sentry instead of joining) or the `favordynmods` mount option on the node.
7. **The fleet is not a constant.** When iteration 7 crashed 4,806 actors they were deleted and re-registered from a cold boot; their first snapshots compress to the same ~10 MB `pages.img.zstd` as before but restore to a larger logical image (snapshot-plugin log: pages files median 19.9 MB, p90 22.4 MB, versus 15.7-16.0 MB sampled in iteration 2), and `app_restore` / `runsc checkpoint` scale with that (unloaded app_restore 104 → 150 ms, checkpoint 46 → 115 ms in iteration 8). Builds 1-6 ran on long-lived actors, build 8 on freshly registered ones; iteration 9 therefore re-measures build 4 on the new fleet for a paired comparison with build C.
8. **Node probes during iteration 9 (build C, catch-up on):** a loop of 200 fork+exec of `/bin/true` and a loop of cgroup mkdir / limits / attach-and-return / rmdir, sampled every 2 s:

   | phase | fork+exec µs (median / p90) | cgroup mkdir ms | `cgroup.procs` attach pair ms | rmdir ms |
   |---|---|---|---|---|
   | idle | 599 / 950 | 7 | 19 | 1 |
   | 2 swaps/s | 599 / 750 | 7 | 20 | 1 |
   | 5 swaps/s | 650 / 1,199 | 7 | 21 | 1 |
   | 8 swaps/s | 749 / 1,499 | 8 | 22 | 1 |
   | 12 swaps/s | 899 / 1,900 | 20 | 36 | 2 |

   So at 12 swaps/s a fork costs 1.5× idle and the cgroup work of one activation (one mkdir, one attach pair, one rmdir) is ~60 ms — real, but only a tenth of the 600-700 ms `pause_create` measured there. The rest of that stage is the gofer start: the runsc parent waits on a "chroot sync" pipe until its gofer has built its chroot (a burst of mount-namespace operations, all under the kernel's global `namespace_sem`), and then on the sentry boot. Substrate adds its own mount-namespace traffic per activation: two overlay mounts and unmounts (rootfs of pause and app container) and, with the netns pool, two bind mounts and two unmounts for the namespace names. The mount-latency probe added for iteration 10 measures this directly.
9. **The wall at 12 swaps/s is the mount-namespace lock.** Node probes during iteration 10 (build D, catch-up off), medians per phase: a bind-mount + umount pair costs 2.4 ms at 2-5 swaps/s, 4.1 ms at 8 and **18.5 ms at 12 swaps/s**; `unshare -m` (one mount-namespace copy) 1.5 → 1.9 → **13.6 ms**; in the same phases fork+exec goes 0.6 → 0.65 → 0.98 ms and the cgroup attach pair 20 → 21 → 28 ms. Every `runsc create` starts a gofer that builds its own chroot (tmpfs + bind mounts + pivot) and a sentry that does the same in a new mount namespace ("Sandbox will be started in new mount, IPC and UTS namespaces … minimal chroot"), and the runsc parent waits for the gofer's chroot before it continues — all of it serialised on the kernel's single `namespace_sem`, which is also what every unmount at sandbox exit takes. That is the "banner → Gofer started" and "Sandbox started → exit" growth inside `pause_create`, and it is why the stage keeps growing after the netns and cgroup convoys were removed. Substrate's own mount traffic per activation is now two overlay mounts and two unmounts (the netns bind mounts went away in build D); the rest belongs to runsc.

## Changes (all in /Users/adityashantanu/repos/substrate-east, uncommitted; `git diff` there is the patch)

### Change 1 — snapshot-plugin / atelet: pooled GCS clients and pooled zstd buffers (ported from the microVM worktree)
Files: `pkg/objectstorage/gcs.go` (`clientPoolSize()` :53, `nextClient()` :62 — every whole-object GET/PUT round-robins over a pool of `storage.Client`s, env `ATE_GCS_CLIENT_POOL`, default 8, set to 64 on the DaemonSet), `gcscompose.go` (pool size from `clientPoolSize`), `gcsranged.go` (first range read uses `nextClient`), `objects.go` + `sparsezstd.go` (`zstdDecPool` :197, pooled decoders), `parzstd.go` (pooled chunk/output buffers and encoders, per-file fan-out capped at `parZstdMaxWorkers = 8` :87), `internal/tarutil/tarutil.go` :152-156 (per-checkpoint tar fsync only with `ATE_TAR_FSYNC=1`; gVisor only tars durable-dir volumes, which this workload has none of, so it is inert here).
Rationale: one `storage.Client` is one HTTP/2 connection, so every snapshot transfer on the node shared a single TCP flow; the parallel zstd path allocated ~500 MiB of fresh buffers per 128 MiB file. Deployed as DaemonSet `atelet-v0-4-0-25-g66f8a888-dirty` images `snapshot-plugin@sha256:8c878fda…` and `atelet@sha256:4e96f9ae…`, env `ATE_GCS_CLIENT_POOL=64 GOGC=200` on the plugin.

### Change 2 — gVisor worker: pool of sandbox networks (new file `internal/ateomnet/netpool.go`; `sandbox.go` :88 take-from-pool, :127 `egressPort` recorded, :287-300 `CleanupSandboxNetwork` releases to the pool; `netns/netns.go` :89 `BindNamed`)
What it does: a torn-down sandbox network (actor netns + gateway netns + veth pair + nftables redirect + per-ns sysctl) is no longer destroyed; its names are removed and it is parked on a per-worker free list (`ATE_NETNS_POOL`, default 16 idle). The next activation on that worker bind-mounts the two pooled namespaces under the new actor's names (`BindNamed`: enter the ns, bind `/proc/self/task/<tid>/ns/net` to `/run/netns/<name>`, the same mechanism `vishvananda/netns.NewNamed` uses) and re-applies the actor-side address and default route, which runsc deletes when it takes over the veth. Networks are only reused for the same egress port (a per-worker constant). Adoption failure falls back to a fresh build.
Rationale: every gVisor wake unshared two network namespaces and every suspend destroyed them; unshare/veth-add and above all the kernel's single `cleanup_net` worker (which holds `rtnl_lock` while unregistering the dying namespaces' veths) serialise node-wide across the 100 worker pods. Measured hit rate in steady state: ~95 % (worker logs `Reused pooled sandbox network … hits/misses`), zero adoption failures across 3 iterations.
Deployed as WorkerPool `benchmark-ateom` image `ateom-gvisor@sha256:90094acd…`.

### Change 3 — gVisor worker: `--shared-root` for runsc (`cmd/ateom-gvisor/runsc.go` :92 `runscSharedRoot`, :104 `runscSharedRootFor`, :112 `ensureSharedRoot`; the flag is added next to `-root` in all 12 runsc invocations)
What it does: passes `-shared-root /run/ateom-runsc-shared` (one directory per worker pod; `ATE_RUNSC_SHARED_ROOT` overrides, `off` restores the default) to every runsc command.
Rationale: runsc's gofer runs in an empty "null" network namespace that is "shared by all gofers using the same --shared-root, which defaults to --root" (`runsc flags`, `-gofer-network-namespace`). Substrate's `--root` is per actor (`runscStateDir`), so every `runsc create` unshared and pinned a fresh netns (runsc log: `Pinned null network namespace at …/runsc-state/null-netns`, plus the warning `gVisor startup is slower if --root (or --shared-root) is not reused across multiple sandboxes`) and every delete destroyed one — i.e. one more namespace per activation that the pool in Change 2 could not reach. With the flag each worker pins the null namespace once (`No usable null network namespace … creating a new one` once per worker pod, then reused).
Deployed as `ateom-gvisor@sha256:7c9c8184…`.

### Change 5 — gVisor worker: lean post-checkpoint teardown (`cmd/ateom-gvisor/main.go` `terminateCheckpointedWorkload`, `leanTeardown()`; `ATE_LEAN_TEARDOWN=0` restores the old sequence)
After a FULL `runsc checkpoint` the sandbox is already stopped, so the old `kill` → `wait` → `state` × 2 → `delete` sequence was four runsc process spawns that only ever failed, and — the important part — `cleanupContainers` returned on the first error, so whenever the app container's delete lost the race against the dying sandbox (~30 % of suspends) the pause container was never deleted and **both cgroups leaked** (3,786 `_pause` + 3,784 `glutton` leaked cgroups and 118 GiB of unreclaimable slab found on the node after ~4 hours; every cgroup operation slows with that population, which is the within-run drift visible in builds 2-6). The new path runs `runsc delete -force` on the app container(s) and then always on the pause container, logging rather than propagating a failed app delete. Verified clean by the smoke test (variant A: 58 creates, 0 failures) and the cgroup count no longer grows.

### Change 6 — gVisor worker: no cgroup of its own for the application container (`runsc.go` `shapeSpec`; `ATE_SUBCONTAINER_CGROUP=1` restores it)
`ocispec.ShapeGVisor` gave every container its own cgroup. In gVisor the application's processes run inside the sentry, which already lives in the pause container's cgroup; the sub-container cgroup only ever held that container's gofer, at the price of one cgroup mkdir, two `cgroup.procs` attaches and one rmdir per activation (each a node-global lock: probe-measured 7-24 ms, 20-60 ms, 1-157 ms under load). The app container's spec now carries an empty `cgroupsPath`, so runsc creates no cgroup for it.

### Rejected along the way (measured, not kept)
- `--ignore-cgroups` (iteration 5): runsc then sizes the sentry from the host (192 vCPUs, host memory) and everything slows (checkpoint 46 → 177 ms unloaded; 8 swaps/s failed).
- Reusable worker-owned cgroup "slots" (iteration 7, `cgroupslots.go`, left in the tree but off by default via `cgroupSlotsDefault = false`): a gofer forked into a previously used slot dies before its first log line (A/B with the smoke test: slots off → 0/58 failures; slots on → 26/44 and 44/58 failures with and without the shared gofer netns). runsc also leaves limits unconfigured on a pre-existing cgroup. Not understood further; abandoned.
- Node knob `favordynmods` on the cgroup2 mount: measured 15× cheaper `cgroup.procs` attaches under identical load (42 → 2.7 ms per pair) but it did not survive the next worker-pool roll (a non-bind remount of cgroup2 resets it) and the policy did not let me re-apply it. Recommended as a node-image/boot setting (`cgroup_favordynmods=1` on the kernel command line) for churn-heavy nodes.

### Change 4 — DIAGNOSTIC only: `--ignore-cgroups` for runsc (`runsc.go` `runscIgnoreCgroups`, `ATE_RUNSC_IGNORE_CGROUPS=0` restores the default)
Not a fix: it drops the host-side per-actor memory limit and per-container cgroups (the sentry's vCPU count still comes from the spec's cpu quota through `--cpu-num-from-quota`; the worker only logs `No initial usage sample`). Run to test the hypothesis that the remaining `pause_create` convoy is cgroup create/join/rmdir (Substrate gives every container its own cgroup, `ocispec.GVisorCgroupLeaf`, and runsc creates and joins it between its startup banner and "Gofer started" — the window that grows from 29 ms to ~300 ms under load, see "Where the time goes"). Deployed as `ateom-gvisor@sha256:913f8d79…`.

### Node probes per phase (probes-iter10)

| phase | fork+exec µs med/p90 | cgroup mkdir ms | attach pair ms | rmdir ms | bind+umount pair µs | unshare -m µs |
|---|---|---|---|---|---|---|
| pre | 550 / 899 | 8 | 22 | 1 | 3306 | 1947 |
| sweep/fill | 900 / 1400 | 7 | 25 | 1 | 4606 | 2748 |
| 2 | 599 / 700 | 7 | 20 | 1 | 2460 | 1462 |
| 3 | 599 / 849 | 7 | 19 | 1 | 2425 | 1471 |
| 5 | 599 / 1150 | 7 | 20 | 1 | 2674 | 1732 |
| 8 | 650 / 1500 | 8 | 21 | 1 | 4136 | 1942 |
| 12 | 975 / 1849 | 19 | 28 | 1 | 18509 | 13564 |
| hold | 650 / 1350 | 8 | 21 | 1 | 3412 | 1822 |

### Node probes per phase (probes-iter11)

| phase | fork+exec µs med/p90 | cgroup mkdir ms | attach pair ms | rmdir ms | bind+umount pair µs | unshare -m µs |
|---|---|---|---|---|---|---|
| pre | 550 / 950 | 8 | 21 | 1 | 2488 | 1594 |
| sweep/fill | 950 / 1099 | 8 | 22 | 1 | 4133 | 3220 |
| 2 | 599 / 700 | 7 | 20 | 1 | 2389 | 1448 |
| 3 | 599 / 950 | 7 | 20 | 1 | 2462 | 1469 |
| 4 | 599 / 1050 | 8 | 21 | 1 | 2475 | 1602 |
| 5 | 600 / 1149 | 8 | 21 | 1 | 2724 | 1684 |
| 7 | 650 / 1350 | 8 | 22 | 1 | 3009 | 1937 |
| 9 | 700 / 1549 | 8 | 24 | 1 | 4397 | 1926 |
| 12 | 849 / 1950 | 15 | 31 | 1 | 19424 | 17926 |
| hold | 650 / 1500 | 8 | 21 | 1 | 4624 | 1932 |

## Per-iteration detail

### Iteration 1 baseline (main 66f8a888 + ACTOR_CPU patch)

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 536 | 660 | 938 | 593 | 713 | 992 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 610 | 733 | 957 | 616 | 761 | 995 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.96 | 595 | 595 | 840 | 1056 | 1211 | 717 | 922 | 1109 | 0 | 0 | 0 | 0 | False |  |
| 8 | 8.00 | 7.87 | 944 | 947 | 1305 | 1821 | 2706 | 989 | 1288 | 1616 | 0 | 0 | 8 | 0 | True | wake-p90-vs-baseline |
| 5 | 5.00 | 4.98 | 1195 | 1195 | 842 | 1062 | 1226 | 740 | 936 | 1152 | 0 | 0 | 0 | 0 | False |  |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 228 | 18 | 89 | 23 | 36 | 94 | 322 |
| 3 | 335 | 60 | 114 | 23 | 34 | 103 | 396 |
| 5 | 562 | 183 | 176 | 26 | 43 | 124 | 612 |
| 8 | 908 | 308 | 285 | 37 | 45 | 193 | 974 |
| hold | 1147 | 166 | 176 | 26 | 39 | 120 | 598 |


| level | n | checkpoint | teardown | total |
| 2 | 225 | 46 | 149 | 214 |
| 3 | 338 | 46 | 190 | 241 |
| 5 | 564 | 47 | 271 | 325 |
| 8 | 908 | 83 | 386 | 487 |
| hold | 1150 | 48 | 287 | 341 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 47 | 159 | 323 | 520 |
| 3 | 357 | 46 | 150 | 397 | 597 |
| 5 | 210 | 41 | 157 | 614 | 816 |
| hold | 985 | 44 | 181 | 599 | 826 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 215 | 291 | 521 |
| 3 | 357 | 243 | 295 | 543 |
| 5 | 210 | 314 | 292 | 622 |
| hold | 985 | 342 | 305 | 664 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 8.0 | 72 | 593 |
| 3 | 8 | 9.2 | 57 | 591 |
| 5 | 8 | 12.1 | 87 | 589 |
| 8 | 8 | 17.4 | 133 | 587 |
| hold | 16 | 12.4 | 94 | 584 |

### Iteration 2 + snapshot-plugin/atelet patch

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 557 | 762 | 1008 | 588 | 763 | 940 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 614 | 757 | 973 | 623 | 745 | 962 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.96 | 595 | 595 | 813 | 929 | 1083 | 713 | 866 | 1074 | 0 | 0 | 0 | 0 | False |  |
| 8 | 8.00 | 7.87 | 944 | 947 | 1150 | 1374 | 1649 | 861 | 1037 | 1257 | 0 | 0 | 8 | 0 | False |  |
| 12 | 12.00 | 11.62 | 1395 | 1416 | 2563 | 3178 | 4106 | 1246 | 1695 | 1958 | 0 | 0 | 36 | 0 | True | wake-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 234 | 33 | 93 | 24 | 38 | 106 | 341 |
| 3 | 354 | 74 | 121 | 23 | 36 | 103 | 410 |
| 5 | 595 | 170 | 178 | 25 | 41 | 117 | 589 |
| 8 | 952 | 240 | 290 | 32 | 40 | 165 | 916 |
| 12 | 1428 | 329 | 640 | 183 | 144 | 506 | 2259 |
| hold | 1904 | 225 | 330 | 42 | 55 | 207 | 983 |


| level | n | checkpoint | teardown | total |
| 2 | 232 | 46 | 136 | 193 |
| 3 | 355 | 45 | 194 | 241 |
| 5 | 595 | 47 | 268 | 321 |
| 8 | 952 | 64 | 347 | 420 |
| 12 | 1428 | 107 | 533 | 673 |
| hold | 1912 | 87 | 389 | 486 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 49 | 151 | 341 | 542 |
| 3 | 357 | 44 | 145 | 410 | 599 |
| 5 | 595 | 46 | 158 | 590 | 797 |
| 8 | 952 | 47 | 164 | 916 | 1133 |
| 12 | 936 | 53 | 182 | 2179 | 2469 |
| hold | 729 | 52 | 176 | 902 | 1156 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 194 | 293 | 509 |
| 3 | 357 | 241 | 282 | 541 |
| 5 | 595 | 321 | 287 | 631 |
| 8 | 952 | 421 | 290 | 782 |
| 12 | 954 | 664 | 315 | 1119 |
| hold | 728 | 440 | 286 | 803 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 8.8 | 73 | 559 |
| 3 | 8 | 9.6 | 53 | 557 |
| 5 | 8 | 12.6 | 80 | 557 |
| 8 | 8 | 18.3 | 126 | 553 |
| 12 | 8 | 28.1 | 173 | 541 |
| hold | 16 | 17.2 | 153 | 540 |

### Iteration 3 + sandbox network-namespace pool (ATE_NETNS_POOL=16)

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 539 | 669 | 785 | 580 | 714 | 847 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 594 | 675 | 879 | 591 | 696 | 807 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.96 | 595 | 595 | 800 | 968 | 1208 | 683 | 878 | 1159 | 0 | 0 | 0 | 0 | False |  |
| 8 | 8.00 | 7.88 | 946 | 952 | 1080 | 1315 | 1859 | 838 | 1035 | 1267 | 0 | 0 | 8 | 0 | False |  |
| 12 | 12.00 | 11.72 | 1407 | 1416 | 1618 | 2363 | 3005 | 1104 | 1572 | 1962 | 0 | 0 | 24 | 0 | True | wake-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 238 | 5 | 93 | 23 | 39 | 99 | 313 |
| 3 | 357 | 1 | 152 | 24 | 36 | 103 | 382 |
| 5 | 595 | 1 | 319 | 25 | 42 | 122 | 583 |
| 8 | 952 | 1 | 529 | 33 | 44 | 159 | 848 |
| 12 | 1428 | 3 | 601 | 66 | 93 | 387 | 1378 |
| hold | 1906 | 1 | 531 | 28 | 43 | 156 | 836 |


| level | n | checkpoint | teardown | total |
| 2 | 238 | 46 | 150 | 200 |
| 3 | 357 | 45 | 171 | 221 |
| 5 | 595 | 47 | 254 | 303 |
| 8 | 952 | 59 | 313 | 387 |
| 12 | 1428 | 105 | 467 | 594 |
| hold | 1912 | 68 | 301 | 382 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 44 | 161 | 313 | 523 |
| 3 | 357 | 41 | 158 | 383 | 583 |
| 5 | 595 | 43 | 159 | 584 | 788 |
| 8 | 952 | 42 | 159 | 849 | 1063 |
| 12 | 1428 | 49 | 160 | 1378 | 1603 |
| hold | 306 | 49 | 163 | 877 | 1103 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 201 | 283 | 500 |
| 3 | 357 | 222 | 274 | 512 |
| 5 | 595 | 304 | 281 | 606 |
| 8 | 952 | 388 | 281 | 757 |
| 12 | 1428 | 595 | 294 | 1022 |
| hold | 309 | 398 | 289 | 773 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 6.9 | 90 | 589 |
| 3 | 8 | 7.9 | 52 | 590 |
| 5 | 8 | 10.8 | 82 | 586 |
| 8 | 8 | 15.6 | 117 | 584 |
| 12 | 8 | 23.3 | 178 | 578 |
| hold | 16 | 13.9 | 148 | 576 |

### Iteration 4 + runsc --shared-root per worker

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 530 | 656 | 782 | 552 | 650 | 796 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 356 | 597 | 687 | 811 | 566 | 670 | 789 | 0 | 0 | 3 | 0 | False |  |
| 5 | 5.00 | 4.96 | 595 | 596 | 784 | 935 | 1071 | 650 | 728 | 870 | 0 | 0 | 0 | 0 | False |  |
| 8 | 8.00 | 7.91 | 949 | 952 | 1076 | 1254 | 1425 | 783 | 958 | 1215 | 0 | 0 | 8 | 0 | False |  |
| 12 | 12.00 | 11.77 | 1413 | 1428 | 1584 | 2365 | 2877 | 1024 | 1367 | 1658 | 0 | 0 | 24 | 0 | True | wake-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 238 | 1 | 87 | 23 | 38 | 104 | 301 |
| 3 | 357 | 1 | 140 | 24 | 42 | 103 | 365 |
| 5 | 595 | 1 | 301 | 25 | 41 | 121 | 550 |
| 8 | 952 | 1 | 536 | 34 | 44 | 149 | 825 |
| 12 | 1428 | 2 | 616 | 52 | 96 | 407 | 1346 |
| hold | 1904 | 1 | 495 | 41 | 64 | 189 | 860 |


| level | n | checkpoint | teardown | total |
| 2 | 238 | 46 | 94 | 160 |
| 3 | 357 | 45 | 147 | 198 |
| 5 | 595 | 47 | 185 | 236 |
| 8 | 952 | 61 | 233 | 303 |
| 12 | 1428 | 93 | 392 | 503 |
| hold | 1912 | 83 | 291 | 385 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 49 | 166 | 301 | 517 |
| 3 | 357 | 51 | 163 | 365 | 584 |
| 5 | 595 | 53 | 169 | 550 | 770 |
| 8 | 952 | 52 | 165 | 826 | 1057 |
| 12 | 733 | 49 | 179 | 1362 | 1589 |
| hold | 933 | 59 | 182 | 910 | 1181 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 161 | 286 | 471 |
| 3 | 357 | 199 | 277 | 489 |
| 5 | 595 | 236 | 296 | 567 |
| 8 | 952 | 303 | 298 | 703 |
| 12 | 744 | 525 | 302 | 957 |
| hold | 936 | 436 | 293 | 852 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 6.1 | 71 | 581 |
| 3 | 8 | 7.2 | 51 | 578 |
| 5 | 8 | 10.3 | 82 | 575 |
| 8 | 8 | 15.4 | 130 | 571 |
| 12 | 8 | 23.5 | 183 | 566 |
| hold | 16 | 14.2 | 146 | 565 |

### Iteration 5 + runsc --ignore-cgroups (DIAGNOSTIC)

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 691 | 797 | 890 | 651 | 823 | 921 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 761 | 862 | 971 | 677 | 842 | 956 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.92 | 590 | 595 | 991 | 1174 | 1355 | 850 | 1069 | 1274 | 0 | 0 | 5 | 0 | False |  |
| 8 | 8.00 | 7.77 | 932 | 936 | 2328 | 2786 | 3166 | 1860 | 2265 | 2520 | 0 | 0 | 24 | 0 | True | wake-p90-vs-baseline+park-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 238 | 1 | 165 | 27 | 23 | 170 | 442 |
| 3 | 357 | 1 | 209 | 28 | 25 | 181 | 526 |
| 5 | 595 | 1 | 330 | 56 | 51 | 238 | 733 |
| 8 | 952 | 4 | 673 | 130 | 156 | 771 | 1999 |
| hold | 1194 | 1 | 325 | 56 | 53 | 242 | 750 |


| level | n | checkpoint | teardown | total |
| 2 | 238 | 123 | 136 | 264 |
| 3 | 357 | 122 | 182 | 310 |
| 5 | 595 | 133 | 285 | 426 |
| 8 | 952 | 301 | 867 | 1178 |
| hold | 1195 | 142 | 343 | 498 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 52 | 171 | 442 | 678 |
| 3 | 357 | 51 | 165 | 526 | 748 |
| 5 | 20 | 51 | 202 | 752 | 1024 |
| hold | 874 | 52 | 179 | 750 | 988 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 265 | 284 | 577 |
| 3 | 357 | 311 | 273 | 595 |
| 5 | 20 | 496 | 314 | 877 |
| hold | 875 | 495 | 287 | 830 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 7.4 | 98 | 553 |
| 3 | 8 | 9.4 | 70 | 552 |
| 5 | 8 | 15.3 | 112 | 549 |
| 8 | 8 | 35.2 | 189 | 548 |
| hold | 16 | 16.0 | 131 | 548 |

### Iteration 6 build 4 + node cgroup2 favordynmods remount

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 652 | 791 | 935 | 648 | 777 | 908 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 731 | 842 | 1022 | 669 | 792 | 918 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.92 | 591 | 595 | 885 | 1025 | 1190 | 750 | 893 | 1046 | 0 | 0 | 5 | 0 | False |  |
| 8 | 8.00 | 7.87 | 944 | 945 | 1165 | 1420 | 1640 | 943 | 1129 | 1312 | 0 | 0 | 8 | 0 | False |  |
| 12 | 12.00 | 11.80 | 1416 | 1416 | 1824 | 2565 | 3151 | 1293 | 1654 | 2063 | 0 | 0 | 12 | 0 | True | wake-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 238 | 1 | 100 | 23 | 64 | 171 | 412 |
| 3 | 357 | 1 | 159 | 24 | 62 | 173 | 479 |
| 5 | 595 | 1 | 276 | 29 | 71 | 182 | 629 |
| 8 | 952 | 1 | 395 | 38 | 86 | 245 | 902 |
| 12 | 1428 | 3 | 596 | 77 | 123 | 513 | 1522 |
| hold | 1905 | 1 | 310 | 38 | 97 | 263 | 880 |


| level | n | checkpoint | teardown | total |
| 2 | 238 | 118 | 122 | 243 |
| 3 | 357 | 114 | 153 | 276 |
| 5 | 595 | 121 | 208 | 335 |
| 8 | 952 | 148 | 330 | 490 |
| 12 | 1428 | 223 | 471 | 697 |
| hold | 1912 | 160 | 339 | 509 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 50 | 164 | 413 | 640 |
| 3 | 357 | 53 | 164 | 480 | 719 |
| 5 | 595 | 51 | 170 | 629 | 871 |
| 8 | 952 | 53 | 172 | 902 | 1150 |
| 12 | 1428 | 58 | 195 | 1522 | 1803 |
| hold | 311 | 50 | 180 | 893 | 1140 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 244 | 291 | 569 |
| 3 | 357 | 277 | 287 | 587 |
| 5 | 595 | 336 | 290 | 667 |
| 8 | 952 | 491 | 295 | 869 |
| 12 | 1428 | 697 | 321 | 1208 |
| hold | 319 | 499 | 297 | 876 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|

### Iteration 8 build C = pool + shared-root + lean teardown (pause always deleted, no cgroup leak) + no cgroup for the app container

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 619 | 703 | 782 | 543 | 635 | 716 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 668 | 779 | 943 | 531 | 642 | 895 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.96 | 595 | 595 | 819 | 939 | 1085 | 600 | 719 | 951 | 0 | 0 | 0 | 0 | False |  |
| 8 | 8.00 | 7.87 | 945 | 952 | 1144 | 1324 | 1535 | 844 | 1085 | 1260 | 0 | 0 | 8 | 0 | False |  |
| 12 | 12.00 | 11.77 | 1412 | 1421 | 1937 | 2738 | 3295 | 1328 | 1802 | 2199 | 0 | 0 | 24 | 0 | True | wake-p90-vs-baseline+park-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 10 | 1 | 79 | 23 | 65 | 150 | 369 |
| 3 | 357 | 1 | 141 | 24 | 53 | 172 | 439 |
| 5 | 595 | 1 | 261 | 26 | 49 | 186 | 592 |
| 8 | 952 | 1 | 444 | 48 | 38 | 241 | 854 |
| 12 | 1428 | 4 | 702 | 67 | 33 | 570 | 1625 |
| hold | 1908 | 1 | 437 | 37 | 40 | 224 | 808 |


| level | n | checkpoint | teardown | total |
| 2 | 10 | 115 | 35 | 149 |
| 3 | 357 | 114 | 37 | 154 |
| 5 | 595 | 120 | 40 | 162 |
| 8 | 952 | 174 | 162 | 330 |
| 12 | 1428 | 256 | 458 | 730 |
| hold | 1912 | 138 | 134 | 288 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 10 | 45 | 155 | 369 | 581 |
| 3 | 357 | 50 | 159 | 439 | 654 |
| 5 | 595 | 49 | 164 | 593 | 806 |
| 8 | 952 | 56 | 180 | 855 | 1128 |
| 12 | 1428 | 56 | 193 | 1626 | 1914 |
| hold | 464 | 51 | 168 | 821 | 1053 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 10 | 149 | 283 | 439 |
| 3 | 357 | 154 | 280 | 454 |
| 5 | 595 | 163 | 285 | 521 |
| 8 | 952 | 331 | 309 | 765 |
| 12 | 1428 | 731 | 318 | 1239 |
| hold | 464 | 324 | 303 | 739 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 4.9 | 90 | 559 |
| 3 | 8 | 5.9 | 73 | 558 |
| 5 | 8 | 9.1 | 116 | 554 |
| 8 | 8 | 14.0 | 158 | 549 |
| 12 | 8 | 23.0 | 277 | 546 |
| hold | 16 | 11.8 | 213 | 550 |

### Iteration 9 build C again (the harness re-deployed a fresh build of the worktree = build C; repeatability on the same fleet)

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 493 | 602 | 853 | 471 | 549 | 740 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 520 | 592 | 704 | 458 | 517 | 600 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.96 | 595 | 595 | 659 | 795 | 963 | 511 | 581 | 688 | 0 | 0 | 0 | 0 | False |  |
| 8 | 8.00 | 7.87 | 944 | 952 | 912 | 1090 | 1322 | 575 | 726 | 876 | 0 | 0 | 8 | 0 | False |  |
| 12 | 12.00 | 11.80 | 1416 | 1426 | 1337 | 1651 | 2154 | 895 | 1112 | 1308 | 0 | 0 | 12 | 0 | True | wake-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 238 | 1 | 80 | 23 | 27 | 102 | 271 |
| 3 | 357 | 1 | 95 | 24 | 32 | 105 | 297 |
| 5 | 595 | 1 | 212 | 25 | 30 | 129 | 456 |
| 8 | 952 | 1 | 414 | 29 | 20 | 170 | 716 |
| 12 | 1428 | 2 | 669 | 35 | 23 | 276 | 1119 |
| hold | 1909 | 1 | 416 | 26 | 20 | 163 | 698 |


| level | n | checkpoint | teardown | total |
| 2 | 238 | 45 | 34 | 81 |
| 3 | 357 | 45 | 34 | 79 |
| 5 | 595 | 46 | 36 | 84 |
| 8 | 952 | 50 | 41 | 94 |
| 12 | 1428 | 91 | 284 | 380 |
| hold | 1912 | 51 | 43 | 108 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 48 | 156 | 272 | 476 |
| 3 | 357 | 44 | 150 | 298 | 508 |
| 5 | 595 | 39 | 147 | 456 | 643 |
| 8 | 952 | 40 | 133 | 716 | 900 |
| 12 | 559 | 38 | 130 | 1076 | 1273 |
| hold | 1157 | 42 | 137 | 689 | 875 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 82 | 285 | 385 |
| 3 | 357 | 80 | 281 | 378 |
| 5 | 595 | 84 | 291 | 429 |
| 8 | 952 | 95 | 298 | 494 |
| 12 | 564 | 379 | 311 | 816 |
| hold | 1152 | 98 | 292 | 487 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 4.4 | 75 | 561 |
| 3 | 8 | 5.4 | 49 | 560 |
| 5 | 8 | 8.2 | 78 | 556 |
| 8 | 8 | 12.2 | 126 | 552 |
| 12 | 8 | 18.6 | 181 | 551 |
| hold | 16 | 10.4 | 138 | 550 |

### Iteration 10 build D = build C + pooled netns handed to runsc by descriptor path (no bind mounts), with the sim first-lap catch-up disabled (SCRIPT_CATCHUP=false) — the new reference

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 483 | 588 | 800 | 455 | 543 | 662 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 510 | 617 | 752 | 446 | 512 | 611 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.96 | 595 | 595 | 653 | 806 | 942 | 503 | 576 | 837 | 0 | 0 | 0 | 0 | False |  |
| 8 | 8.00 | 7.92 | 950 | 952 | 877 | 1061 | 1386 | 558 | 705 | 815 | 0 | 0 | 8 | 0 | False |  |
| 12 | 12.00 | 11.81 | 1417 | 1417 | 1326 | 1625 | 2190 | 862 | 1117 | 1417 | 0 | 0 | 12 | 0 | True | wake-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 238 | 1 | 73 | 24 | 27 | 111 | 281 |
| 3 | 357 | 1 | 99 | 26 | 33 | 105 | 304 |
| 5 | 595 | 1 | 213 | 25 | 29 | 124 | 453 |
| 8 | 952 | 1 | 386 | 28 | 20 | 163 | 678 |
| 12 | 1428 | 1 | 614 | 35 | 25 | 286 | 1065 |
| hold | 1912 | 1 | 397 | 34 | 27 | 174 | 702 |


| level | n | checkpoint | teardown | total |
| 2 | 238 | 44 | 34 | 79 |
| 3 | 357 | 43 | 34 | 78 |
| 5 | 595 | 44 | 35 | 81 |
| 8 | 952 | 47 | 37 | 86 |
| 12 | 1428 | 97 | 228 | 323 |
| hold | 1912 | 49 | 40 | 94 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 38 | 138 | 281 | 470 |
| 3 | 357 | 43 | 143 | 305 | 498 |
| 5 | 595 | 42 | 144 | 454 | 640 |
| 8 | 952 | 40 | 142 | 678 | 863 |
| 12 | 1090 | 48 | 164 | 1066 | 1315 |
| hold | 624 | 47 | 146 | 681 | 868 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 79 | 280 | 378 |
| 3 | 357 | 79 | 273 | 368 |
| 5 | 595 | 81 | 281 | 422 |
| 8 | 952 | 87 | 301 | 473 |
| 12 | 1092 | 323 | 305 | 769 |
| hold | 624 | 89 | 288 | 467 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 3.4 | 80 | 555 |
| 3 | 8 | 4.2 | 50 | 553 |
| 5 | 8 | 6.1 | 80 | 549 |
| 8 | 8 | 9.2 | 118 | 545 |
| 12 | 8 | 14.5 | 179 | 542 |
| hold | 16 | 9.1 | 137 | 541 |

### Iteration 11 build D, catch-up off, finer x1.25 ramp (2,3,4,5,7,9,12,15…) to locate the ceiling

| N/tick | target/s | achieved/s | wakes | parks | wake P50 | P90 | P99 | park P50 | P90 | P99 | err | refused | backlog | crashed | failed | failed_on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 2 | 2.00 | 1.98 | 238 | 238 | 486 | 618 | 696 | 462 | 541 | 645 | 0 | 0 | 0 | 0 | False |  |
| 3 | 3.00 | 2.97 | 357 | 357 | 509 | 637 | 823 | 440 | 511 | 623 | 0 | 0 | 0 | 0 | False |  |
| 4 | 4.00 | 3.97 | 476 | 476 | 576 | 703 | 837 | 467 | 539 | 688 | 0 | 0 | 0 | 0 | False |  |
| 5 | 5.00 | 4.96 | 595 | 595 | 644 | 777 | 984 | 493 | 563 | 671 | 0 | 0 | 0 | 0 | False |  |
| 7 | 7.00 | 6.94 | 833 | 833 | 802 | 966 | 1203 | 532 | 646 | 727 | 0 | 0 | 0 | 0 | False |  |
| 9 | 9.00 | 8.92 | 1070 | 1071 | 959 | 1194 | 1449 | 589 | 742 | 932 | 0 | 0 | 9 | 0 | False |  |
| 12 | 12.00 | 11.80 | 1416 | 1428 | 1449 | 1997 | 2481 | 893 | 1114 | 1473 | 0 | 0 | 12 | 0 | True | wake-p90-vs-baseline |

Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):

median ms per stage per level


| level | n | net_setup | pause_create | pause_restore | app_create | app_restore | total |
| 2 | 86 | 1 | 91 | 24 | 29 | 112 | 282 |
| 3 | 150 | 1 | 112 | 24 | 29 | 108 | 314 |
| 4 | 188 | 1 | 161 | 24 | 31 | 119 | 386 |
| 5 | 246 | 1 | 219 | 24 | 26 | 129 | 459 |
| 7 | 329 | 1 | 311 | 26 | 24 | 149 | 590 |
| 9 | 422 | 1 | 411 | 28 | 19 | 174 | 720 |
| 12 | 566 | 1 | 652 | 44 | 29 | 342 | 1169 |
| hold | 1212 | 1 | 442 | 36 | 25 | 192 | 769 |


| level | n | checkpoint | teardown | total |
| 2 | 92 | 44 | 33 | 79 |
| 3 | 146 | 43 | 34 | 78 |
| 4 | 182 | 44 | 34 | 80 |
| 5 | 239 | 44 | 35 | 80 |
| 7 | 331 | 46 | 36 | 84 |
| 9 | 421 | 48 | 39 | 89 |
| 12 | 570 | 115 | 235 | 362 |
| hold | 1228 | 51 | 41 | 110 |


| level | n | manifest_fetch | download | ateom_restore | total |
| 2 | 238 | 44 | 134 | 282 | 473 |
| 3 | 357 | 41 | 133 | 315 | 496 |
| 4 | 476 | 41 | 138 | 387 | 563 |
| 5 | 595 | 40 | 135 | 460 | 631 |
| 7 | 833 | 47 | 161 | 588 | 789 |
| 9 | 1071 | 49 | 173 | 718 | 945 |
| 12 | 416 | 50 | 183 | 1150 | 1388 |
| hold | 1462 | 51 | 163 | 774 | 999 |


| level | n | ateom_checkpoint | persist | total |
| 2 | 238 | 80 | 284 | 383 |
| 3 | 357 | 79 | 270 | 364 |
| 4 | 476 | 80 | 280 | 386 |
| 5 | 595 | 80 | 274 | 417 |
| 7 | 833 | 85 | 294 | 452 |
| 9 | 1071 | 89 | 314 | 504 |
| 12 | 432 | 337 | 302 | 787 |
| hold | 1467 | 105 | 295 | 516 |

Host (hostprobe, 15-s samples):

| level | samples | CPU busy % | disk MB/s | MemAvail GiB |
|---|---|---|---|---|
| 2 | 8 | 3.4 | 69 | 544 |
| 3 | 8 | 4.1 | 51 | 542 |
| 4 | 8 | 5.1 | 68 | 540 |
| 5 | 8 | 6.2 | 79 | 538 |
| 7 | 7 | 8.3 | 109 | 536 |
| 9 | 8 | 10.5 | 137 | 533 |
| 12 | 8 | 14.5 | 180 | 532 |
| hold | 16 | 12.1 | 161 | 532 |

## Caveats and operational notes

- All Substrate changes are uncommitted in /Users/adityashantanu/repos/substrate-east; images were built with ko (`--base-import-paths`) and pointed at directly (DaemonSet image/env edits, WorkerPool `spec.workerImage` patches). The harness's deploy step re-ran once (iteration 5) because the WorkerPool status lagged my roll; it rebuilt the same source as `ateom-gvisor-7158…@sha256:c66d804b…` and re-rolled the pool before the sim started, so that iteration still measured the intended build.
- Node change (agents-tco-east metal node, not persistent across reboot): cgroup2 remounted with `favordynmods` at 00:45:42 PDT (`nsenter -t 1 -m -- mount -o remount,favordynmods none /sys/fs/cgroup`). The remount reset the mount's `nosuid,nodev,noexec` flags; my attempt to put them back was blocked by the tool permission policy, and ~5 minutes later (during the 00:49-00:53 worker-pool roll) something on the node remounted cgroup2 again: by 01:10 the mount read `rw,relatime` — no `favordynmods`, and still without `nosuid,nodev,noexec`. So the option was in effect only ~00:46-00:50 (probe: attach pair 42 → 2.7 ms while it lasted) and no sim level ran under it. If it is to be used it must be applied in a way that survives remounts (kernel cmdline `cgroup_favordynmods=1`, or whatever re-mounts cgroup2 on the node must carry the option). Restoring the flags: `mount -o remount,nosuid,nodev,noexec[,favordynmods] none /sys/fs/cgroup`.
- The sim's 2.5x rule is relative to the first level's P90 of the same run (660-800 ms here), so the pass/fail verdicts across builds are not on an identical absolute threshold; compare the absolute P50/P90 columns.
- `runsc delete` of the app container fails with exit 128 ("connecting to control server … connection refused") on a few percent of suspends with every build: `runsc checkpoint` stops the sandbox and the best-effort kill/wait/delete that follow race it. Pre-existing on main; it costs log noise and skips the pause container's delete (the state dir is wiped by atelet anyway).
- Atelet (node-agent) timing lines rotate out of the kubelet log within the hour; worker lines are complete. Where the atelet table shows fewer samples than the worker table, that is why.
- The baseline run (iteration 1) lost its pipeline wrapper to a stray SIGTERM from the main thread at 22:33; I re-created the capture loop by hand, the sim job itself ran to completion untouched, and the fleet was verified intact (650 RUNNING / 4,350 SUSPENDED) before iteration 2.
- The gv-cgprobe / gv-wchan probe pods (kube-system, privileged, host PID) were removed after use except gv-cgprobe, which may still be running until the end of the campaign; delete with `kubectl -n kube-system delete pod gv-cgprobe`.
