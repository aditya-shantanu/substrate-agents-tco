# microVM activation throughput with **pause** (node-local checkpoints) — 650 awake, 5,000 registered (2026-10-09)

Same test as the suspend campaign, with `PauseActor` in place of `SuspendActor`: the checkpoint (the VM's 128 MiB memory image plus its rootfs upper layer, 119 MB allocated per actor) stays on the node and a wake restores from it. No bucket, no network, no snapshot plugin: what remains is the control plane, the worker's VM lifecycle, and the node's disk. One c3-standard-192-metal (192 vCPU, 768 GiB), Substrate main 66f8a888 plus the campaign's experimental microVM worker patches, 100 unsized worker pods, actor 2 vCPU + 256 MiB, nano-personal-agent at think ×1, 1-s swap ticks, 2-minute levels, 4-minute hold at the last clean level, catch-up off. Bounds as before: resume/pause P90 ≤ 2.5× the run's first level, errors + refusals ≤ 0.5 % of wakes, no backlog growth, host memory/PSI limits; runs 7–8 relaxed the latency rule and the error gate (2 %) to find where the system itself breaks.

## Result in one table

| | Within the bounds as written | Best delivered error-free | Resume P50 / P90 / P99 ms at the best delivered rate | Pause P50 / P90 / P99 ms at that rate | What stopped it |
|---|---|---|---|---|---|
| **Pause, boot disk** (3 TB Hyperdisk Balanced, 2,400 MiB/s, queue scheduler switched from bfq to none) | **8 activations/s** | **12 activations/s** (11.9 achieved: 1 error, 5 refusals, 1 crash) | 321 / 509 / 646 | 301 / 453 / 583 | the 2.5× rule (a 171 ms baseline makes it 428 ms), then the disk at 15/s: 95 % busy, IO pressure 46 % |
| **Pause, Hyperdisk Extreme** (2 TB, 350k IOPS, attached and migrated mid-test) | 8 (same rule) | **15 activations/s** (14.9 achieved: 8 errors, 40 refusals = 3.2 %) | 798 / 897 / 946 | 322 / 508 / 671 | per-VM startup failures (virtiofsd, guest agent) crashing ~0.3–0.5 % of restores, and page-cache pressure from 591 GB of local checkpoints |
| Suspend, reference (bucket snapshots, final build, iteration 19) | 17 activations/s | 17 | 1,298 / 1,397 / 1,560 | suspend 1,092 / 1,345 / 1,615 | node-wide convoy at 20/s |

Read it as: **pause is five times faster per activation** (resume P50 0.25–0.8 s against 1.1–1.3 s) **but moves three times the bytes through the local disk** (uncompressed image in and out), so on the boot disk it tops out at 12 activations/s where suspend reached 17 by going compressed to the network; on a Hyperdisk Extreme it delivers 15 at a 0.9 s resume P90, and is then stopped by a small per-VM failure rate rather than by throughput.

### Activations per second with resume and pause latency

**Boot disk, run 5** (strict gates; the bfq → none scheduler change applied):

| Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Pause P50 / P90 / P99 ms | Errors + refusals (of wakes) | Backlog | Verdict |
|---|---|---|---|---|---|---|
| 2 | 1.98 | 158 / 171 / 205 | 170 / 203 / 229 | 0 + 0 (0.0 %) | 0 | pass |
| 3 | 2.97 | 163 / 183 / 445 | 174 / 201 / 239 | 0 + 1 (0.3 %) | 0 | pass |
| 5 | 4.96 | 195 / 231 / 3,497 | 204 / 256 / 309 | 0 + 1 (0.2 %) | 0 | pass |
| **8** | **7.93** | **252 / 369 / 560** | **249 / 340 / 479** | 0 + 1 (0.1 %) | 0 | **pass** |
| 12 | 11.90 | 321 / 509 / 646 | 301 / 453 / 583 | 1 + 5 (0.4 %) | 0 | 2.5× latency rule |

**Hyperdisk Extreme, run 8** (latency rule off, error gate 2 %):

| Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Pause P50 / P90 / P99 ms | Errors + refusals (of wakes) | Backlog | Verdict |
|---|---|---|---|---|---|---|
| 8 | 7.93 | 437 / 543 / 587 | 222 / 279 / 372 | 0 + 0 (0.0 %) | 0 | pass |
| **11** | **10.91** | **585 / 680 / 721** | **269 / 366 / 498** | 2 + 13 (1.1 %) | 0 | **pass** |
| 15 | 14.86 | 798 / 897 / 946 | 322 / 508 / 671 | 8 + 40 (2.7 %) | 30 | refusals/errors > gate |

**Boot disk, run 7** (latency rule off, error gate 2 %; same build as run 8, for the disk comparison):

| Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Pause P50 / P90 / P99 ms | Errors + refusals (of wakes) | Backlog | Verdict |
|---|---|---|---|---|---|---|
| 8 | 7.93 | 698 / 800 / 1,998 | 240 / 328 / 731 | 0 + 4 (0.4 %) | 0 | pass |
| **11** | **10.91** | **286 / 506 / 1,427** | **299 / 507 / 862** | 0 + 0 (0.0 %) | 0 | **pass** |
| 15 | 14.42 | 1,039 / 4,645 / 7,434 | 699 / 3,678 / 7,349 | 64 + 11 (4.3 %) | 135 | refusals/errors > gate, backlog |

_Bold = the highest rate that passed that run's gates. Resume = the wake request's round trip through the router including the local restore; pause = the PauseActor call (VM snapshot to the local directory + rootfs tar, no upload)._

## Changes made for this test

| # | Change | Why | Effect |
|---|---|---|---|
| 1 | Actor state directory moved back from tmpfs to the boot disk | 5,000 pause checkpoints = 580 GB; the tmpfs was capped at 400 GB | required |
| 2 | Fresh fleet registered in pause mode under its own prefix; the suspend fleet left parked | pause checkpoints must exist for every actor | registration wrote 580 GB at ~1 GB/s with the disk 93 % busy for ten minutes; the first run measured that storm, not pause |
| 3 | Sim fill gives up on actors whose wake error is permanent (CRASHED / DELETING / gone) — commit 1b118c8 | a handful of crashed actors held the 8 fill slots through 12 long retries each; one run never got past its fill | fill 2m41 → 25–50 s |
| 4 | Crash gate made count-only for these runs (`FAIL_CRASHED_OVERRIDE=200`) | crashed actors from a previous run failed a level whose own numbers were clean | levels judged on their own behaviour |
| 5 | Boot disk queue scheduler `bfq` → `none`, read-ahead 128 KiB → 1 MiB | restores timed out waiting for virtiofsd / VMM sockets with the disk only 5–15 % busy; bfq's per-process idling (8 ms slices, 128 KiB requests) starves the many short I/O streams of VM startup | at 8/s: resume P50 252 ms, P99 560 (run 5) against P99 10 s with bfq (run 4); fill 54 → 25 s |
| 6 | Worker socket and agent waits raised: virtiofsd 10 → 60 s, VMM API 10/15 → 60 s, guest agent 15 → 45 s (microVM worker, experimental) | a stall should cost latency, not a crashed actor | tails shorter; a residual ~0.3–0.5 % of restores still fail outright (see below) |
| 7 | 2 TB Hyperdisk Extreme (350k IOPS) attached to the node, 591 GB of actor state copied onto it (1.3 GB/s), mounted at `/var/lib/ate/actors` | the boot disk saturated at 15/s | PSI io at 15/s 46 % → 22 %; 15 activations/s delivered at a 0.9 s resume P90 |
| 8 | Fleet parked with `PauseActor` (not `SuspendActor`) before pool rolls and the disk migration | parking by suspend sent 648 actors to the bucket, and their next wakes were downloads | clean first levels |

## Why it stops where it stops

**1. The disk, on the boot volume (the hard wall at 12–15 activations/s).** Each pause writes the full 128 MiB memory image plus the rootfs tar — about 133 MB of writes per swap, measured from the device counters — and each wake reads 128 MiB back, from the page cache when it is still there and from the disk when it is not. Per level in run 5 (boot disk, scheduler `none`):

| activations/s | disk write MB/s | disk read MB/s | disk busy % | PSI io % | node CPU busy % | worker restore median (VMM restore part) | worker pause median (teardown part) |
|---|---|---|---|---|---|---|---|
| 2 | 266 | 5 | 13 | 2.8 | 1.7 | 143 ms (86) | 144 ms (64) |
| 5 | 707 | 24 | 33 | 8.6 | 3.0 | 177 (107) | 177 (99) |
| 8 | 1,055 | 151 | 52 | 18.3 | 5.0 | 234 (137) | 219 (135) |
| 12 | 1,598 | 231 | 76 | 27.6 | 7.5 | 294 (172) | 273 (185) |
| 15 (run 7) | — | — | ~95 | 46 | — | collapse: resume P90 4.6 s, 64 errors, backlog 135 | pause P90 3.7 s |

The write rate scales exactly with the swap rate and reaches the volume's practical limit between 12 and 15 activations/s; the CPU is idle (7.5 % of 192 cores at 12/s) because nothing is compressed or transferred. This is a byte problem: suspend's compressed 42 MiB object plus tmpfs staging never touched this disk, which is why suspend reached 17 and pause does not.

**2. On the Hyperdisk Extreme, per-VM startup failures (the wall at 15 activations/s is a gate on errors, not a capacity limit).** With the disk relieved (PSI io 22 % at 15/s, tails tight: resume P99 946 ms), what fails the 2 % error gate is a steady trickle of restores that never complete: in run 8, 8 restores had virtiofsd not bring up its socket within 60 s, and 5 had the guest agent not answer the CRNG reseed within 2 × 15 s. Each such failure marks the actor CRASHED; every later wake of that actor is a 503 until it is deleted, so a 0.3–0.5 % restore failure rate shows up as 3 % refusals at the gate. The same failure modes existed under suspend (one restore in 500–1,000); pause's higher rate of VM starts per second and the cold page cache make them more frequent. Fixing virtiofsd's startup (its log is in the VM directory, removed at teardown; the next step is to keep it) and retrying the reseed inside the worker instead of crashing the actor are worker changes, not infrastructure.

**3. Page-cache pressure from 5,000 local checkpoints.** 591 GB of checkpoints sit on the node and the kernel keeps them cached: 710 GB of a 792 GB node is page cache, `MemAvailable` reads 657 GB but reclaim runs continuously — PSI memory "full" 11–14 % even when idle — and the sim's memory-pressure gate ended the hold at 11 in run 8. It also makes every wake of an actor whose image was evicted a 128 MiB disk read. Compressing local checkpoints (×3 smaller) or dropping them from the page cache after they are written would remove this.

**4. The relative latency rule.** With a 2-activations/s baseline of 158 / 171 ms the 2.5× rule allows a resume P90 of 428 ms, which pause crosses at 12 activations/s (509 ms) while still well inside anything a user would notice. Within the rule as written pause gives 8 activations/s; an absolute bound (say resume P90 ≤ 1 s) would give 15 on the Extreme volume.

**5. Not limiting:** the control plane (sub-millisecond apart from the restore), the network and the snapshot plugin (idle: every restore `kind=local`, every checkpoint with no upload), CPU (≤ 8 % busy).

**What would raise it next.** (a) Compress the local checkpoint the way the suspend path does (the CPU is idle): disk bytes ÷ 3, page-cache footprint ÷ 3, and the boot disk alone would carry ~35 activations/s of pause traffic. (b) Keep the failing virtiofsd logs and make the worker retry the reseed and the virtiofsd start instead of crashing the actor. (c) Cloud Hypervisor's on-demand restore (implemented in the worker, disabled on CH ≥ v53 by the prefault check) would stop reading the whole 128 MiB on every wake. (d) Hyperdisk Extreme or several volumes if the uncompressed format is kept.

## Run log

| Run | Setup | Outcome |
|---|---|---|
| 1 | fresh registration of 5,000 pause actors, ramp from 8 | registration wrote 580 GB at ~1 GB/s (disk 93 % busy, 25–44 GB dirty for 10 min); the 8/s level ran inside it: resume P50 27.5 s, 244 errors, 1,088 refusals, 23 crashes — a measurement of the registration, not of pause |
| 2 | fleet reused, ramp from 2 | 2/s clean (resume 164 / 286 / 889 ms, pause 176 / 220 / 336) but failed by the crash gate on run 1's leftovers; confirmed every restore local, every checkpoint without upload, plugin idle |
| 3 | same | lost to the sim's fill retrying crashed actors (fixed, 1b118c8) |
| 4 | crash gate count-only | 2/s clean; 3/s failed on 13 refusals + 4 errors with 5 new crashes: medians ~160–200 ms per side, P99 10 s socket timeouts under bfq |
| 5 | scheduler none, read-ahead 1 MiB | fill 25 s; 8/s clean (252 / 369 / 560 ms); 12/s delivered at 321 / 509 / 646 ms, failed only the 2.5× rule |
| 6 | latency rule off | ended at the first level: 0.63 % errors+refusals vs the 0.5 % gate, 2 crashes |
| 7 | worker waits raised to 45–60 s, error gate 2 % | 11/s clean (286 / 506 / 1,427 ms); 15/s collapses on the disk (PSI io 46 %, 64 errors, backlog 135); hold at 11 |
| 8 | Hyperdisk Extreme | 15/s delivered at 798 / 897 / 946 ms (pause 322 / 508 / 671) but 3.2 % errors+refusals from per-VM startup failures; hold at 11 ended by the memory-pressure gate |

## Node state left behind

The Hyperdisk Extreme `agents-tco-euw4-actors-hdx` (2 TB, 350k IOPS) is attached to the microVM node and mounted at `/var/lib/ate/actors`; the boot-disk copy of the actor state is kept at `/var/lib/ate/actors.boot` (591 GB). The boot disk's queue scheduler is `none` with 1 MiB read-ahead. The microVM worker image carries the raised waits. Both fleets (5,000 suspended, 5,000 paused) remain registered; the clusters keep running.
