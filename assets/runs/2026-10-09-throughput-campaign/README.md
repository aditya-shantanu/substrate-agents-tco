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

**What moved the numbers, in order of effect**

1. **Measuring correctly** (harness): issuing swaps steadily (1-s ticks) instead of bursts of N every 10 s. Every earlier latency figure was burst queueing. 2 → 5 swaps/s on both runtimes with no Substrate change.
2. **Harness again**: the sim replayed every skipped step's ops on an agent's first wake (its random day-offset "catch-up"), ~46 requests per first wake — a request storm proportional to the swap rate, present in every run until iteration 12. Off since (`SCRIPT_CATCHUP=false`).
3. **Snapshot plugin** (`pkg/objectstorage`, both nodes): one GCS connection per node → a pool used by every transfer; fresh 64 MiB upload buffers, 128 MiB download range buffers, encoders and decoders per transfer → bounded free lists and pooled codecs (page faults 150k/s → 300/s); 4 MiB download ranges, 12 in flight per object (per-stream GCS throughput here is ~45 MB/s). Rejected after measurement: composite uploads in 8 MiB parts (suspend P99 7.8 s), denser zstd level (guest memory compresses 41 → 40 MiB only, 3× the CPU), GC tuning (GOGC/GOMEMLIMIT: no effect on the ceiling).
4. **microVM worker** (`cmd/ateom-microvm`): guest CRNG reseed after restore given 15 s and a retry instead of 5 s and a crash (~1 restore in 500–1,000 was crashing the actor, and a crashed actor's deletion then hangs when its worker is gone); per-checkpoint tar fsync skipped (approved for the experiment); graceful VMM shutdown skipped after a checkpoint (no measurable gain); the gVisor campaign's sandbox network-namespace pool ported (no gain on microVM: its per-activation cost is the tap device, VMM launch/restore and teardown).
5. **Node**: `/var/lib/ate/actors` on tmpfs (restore/checkpoint staging off the boot disk) — removes IO pressure, no change to the ceiling; 37k orphaned actor directories (576 GB) reclaimed.
6. **gVisor worker** (`cmd/ateom-gvisor`, `internal/ateomnet`, by the background agent): pooled sandbox network namespaces (net_setup 170–330 → 1 ms), one shared gofer namespace per worker (`runsc --shared-root`), a lean post-checkpoint teardown that also fixed a ~30 % cgroup leak per suspend (8,157 cgroups for 650 actors before), no cgroup for the app container. Rejected: `--ignore-cgroups` (sentry sized from the host), reusable worker-owned cgroup slots (gofers die in a reused cgroup).

**Caveats.** Every iteration below purged and re-registered the 5,000 actors at its start (the deploy step runs `clean.sh`); the fleet is therefore fresh in every run, and the per-run numbers are comparable but each run's first level is both the gate baseline and a cold start — ramps were started two or more levels below the suspected ceiling. The experimental Substrate changes live uncommitted in two worktrees (`~/repos/substrate-east1` microVM, `~/repos/substrate-east` gVisor); the patches are in the archive. The fsync skip and the reseed/teardown changes are experiment-grade and need proper flags upstream. The harness fixes (1-s ticks, catch-up switch, readiness check, fleet reuse, backlog gate) are committed in `substrate-agents-tco`.


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
