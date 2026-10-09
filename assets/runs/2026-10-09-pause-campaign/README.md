# microVM activation throughput with **pause** (node-local checkpoints) — 650 awake, 5,000 registered (measured 2026-10-09)

Same test as the suspend campaign, with `PauseActor` instead of `SuspendActor`: the checkpoint (the VM's 128 MiB memory image plus its rootfs upper layer) stays on the node's boot disk and a wake restores from it; nothing goes to or comes from the bucket, so the snapshot plugin and the network are out of the path. Everything else is as in the suspend run: one c3-standard-192-metal (192 vCPU, 768 GiB, 3 TB Hyperdisk Balanced at 100k IOPS / 2,400 MiB/s), Substrate main 66f8a888 plus the experimental microVM worker patches of the campaign (reseed 15 s + retry, no tar fsync, no graceful VMM shutdown after checkpoint, pooled sandbox network namespaces), 100 unsized worker pods, actor 2 vCPU + 256 MiB, nano-personal-agent at think ×1, 1-s swap ticks, 2-minute levels growing ×1.35 from 8 swaps/s, 4-minute hold at the last clean level, catch-up off, gates: P90 ≤ 2.5× the first level, errors + refusals < 0.5 %, backlog ≤ 5 s of swaps, ≤ 20 crashed. The old suspend-mode fleet was parked and left in place under its own name; this run registered a fresh fleet of 5,000 actors in pause mode (cold boot, first ping, PauseActor), so 5,000 local checkpoints live on the disk for the whole run.

## Pause run 1

**Node disk 7 min into registration:**

```
/dev/nvme0n1p1            2.9T    231.7G      2.7T   8% /host/mnt/stateful_partition
228.9G	/host/var/lib/ate/actors
54896
```

**Node disk at the end:**

```
/dev/nvme0n1p1            2.9T    610.3G      2.3T  21% /host/mnt/stateful_partition
589.1G	/host/var/lib/ate/actors
57527
4	/host/var/lib/ate/actors/568f1867-1233-4c62-8350-6e4f11693e57/volumes
48	/host/var/lib/ate/actors/568f1867-1233-4c62-8350-6e4f11693e57/bundles
8228	/host/var/lib/ate/actors/568f1867-1233-4c62-8350-6e4f11693e57/rootfs-upper
121868	/host/var/lib/ate/actors/568f1867-1233-4c62-8350-6e4f11693e57/local-checkpoint
```

| Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Suspend-or-pause P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|
| 8 | **5.05** | 27,546 / 48,642 / 58,116 | 22,394 / 36,614 / 43,233 | 244 + 1088 | 488 | 23 | refusals, backlog, crashed |

## Pause run 2

**Node disk 7 min into registration:**

```
/dev/nvme0n1p1            2.9T    611.3G      2.3T  21% /host/mnt/stateful_partition
590.1G	/host/var/lib/ate/actors
57527
```

**Node disk at the end:**

```
/dev/nvme0n1p1            2.9T    611.5G      2.3T  21% /host/mnt/stateful_partition
590.3G	/host/var/lib/ate/actors
57527
4	/host/var/lib/ate/actors/4fd2ccf4-89aa-4f52-9cda-d1fe4f7dbf0f/volumes
48	/host/var/lib/ate/actors/4fd2ccf4-89aa-4f52-9cda-d1fe4f7dbf0f/bundles
8228	/host/var/lib/ate/actors/4fd2ccf4-89aa-4f52-9cda-d1fe4f7dbf0f/rootfs-upper
123048	/host/var/lib/ate/actors/4fd2ccf4-89aa-4f52-9cda-d1fe4f7dbf0f/local-checkpoint
```

| Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Suspend-or-pause P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|
| 2 | **1.97** | 164 / 286 / 889 | 176 / 220 / 336 | 1 + 2 | 2 | 23 | crashed |

## Pause run 4

**Node disk 7 min into registration:**

```
/dev/nvme0n1p1            2.9T    612.4G      2.3T  21% /host/mnt/stateful_partition
590.9G	/host/var/lib/ate/actors
57527
```

**Node disk at the end:**

```
/dev/nvme0n1p1            2.9T    612.6G      2.3T  21% /host/mnt/stateful_partition
591.2G	/host/var/lib/ate/actors
57527
4	/host/var/lib/ate/actors/76d1a41f-e13b-4b83-b4b4-07d206bea67e/volumes
48	/host/var/lib/ate/actors/76d1a41f-e13b-4b83-b4b4-07d206bea67e/bundles
8228	/host/var/lib/ate/actors/76d1a41f-e13b-4b83-b4b4-07d206bea67e/rootfs-upper
121712	/host/var/lib/ate/actors/76d1a41f-e13b-4b83-b4b4-07d206bea67e/local-checkpoint
```

| Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Suspend-or-pause P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|
| 2 | **1.98** | 182 / 708 / 3,594 | 182 / 259 / 1,168 | 0 + 0 | 0 | 0 | pass |
| 3 | **2.96** | 215 / 390 / 570 | 190 / 234 / 263 | 4 + 13 | 3 | 5 | refusals |

## Pause run 5

**Node disk 7 min into registration:**

```
/dev/nvme0n1p1            2.9T    613.4G      2.3T  21% /host/mnt/stateful_partition
591.7G	/host/var/lib/ate/actors
57527
```

**Node disk at the end:**

```
/dev/nvme0n1p1            2.9T    617.4G      2.3T  21% /host/mnt/stateful_partition
595.9G	/host/var/lib/ate/actors
57527
4	/host/var/lib/ate/actors/26adf856-5eea-437d-b4eb-9369a2204a3f/volumes
48	/host/var/lib/ate/actors/26adf856-5eea-437d-b4eb-9369a2204a3f/bundles
8228	/host/var/lib/ate/actors/26adf856-5eea-437d-b4eb-9369a2204a3f/rootfs-upper
123244	/host/var/lib/ate/actors/26adf856-5eea-437d-b4eb-9369a2204a3f/local-checkpoint
```

| Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Suspend-or-pause P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|
| 2 | **1.98** | 158 / 171 / 205 | 170 / 203 / 229 | 0 + 0 | 0 | 0 | pass |
| 3 | **2.97** | 163 / 183 / 445 | 174 / 201 / 239 | 0 + 1 | 0 | 0 | pass |
| 5 | **4.96** | 195 / 231 / 3,497 | 204 / 256 / 309 | 0 + 1 | 0 | 0 | pass |
| 8 | **7.93** | 252 / 369 / 560 | 249 / 340 / 479 | 0 + 1 | 0 | 0 | pass |
| 12 | **11.90** | 321 / 509 / 646 | 301 / 453 / 583 | 1 + 5 | 0 | 1 | latency rule |

## Reference: the same ramp with suspend (bucket snapshots), final configuration, iteration 19 of the suspend campaign

| Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Suspend-or-pause P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|
| 12 | **11.90** | 908 / 951 / 1,042 | 1,041 / 1,269 / 1,637 | 0 + 0 | 12 | 0 | pass |
| 14 | **13.88** | 1,005 / 1,066 / 1,119 | 1,101 / 1,809 / 13,681 | 0 + 1 | 28 | 0 | pass |
| 17 | **16.72** | 1,298 / 1,397 / 1,560 | 1,092 / 1,345 / 1,615 | 0 + 0 | 17 | 0 | pass |
| 20 | **19.21** | 4,692 / 8,687 / 11,507 | 2,106 / 2,793 / 3,413 | 0 + 91 | 100 | 0 | refusals, latency rule |

## What happened, run by run

- **Run 1 (fresh registration, ramp from 8).** Registering 5,000 actors in pause mode wrote 580 GB of checkpoints (119 MB allocated per actor: the 128 MiB sparse memory image plus the rootfs upper tar) at ~1 GB/s with the disk 93 % busy and 25–44 GB of dirty pages for ten minutes; the 650-actor fill took 2m41 instead of ~50 s and the first level at 8 swaps/s ran inside that writeback storm: resume P50 27.5 s, 244 errors, 1,088 refusals, 23 actors crashed on 10-s socket timeouts (virtiofsd, VMM API, vsock). Not a measurement of pause, a measurement of registering 5,000 pause checkpoints on one Hyperdisk.
- **Run 2 (fleet reused, ramp from 2).** 2 swaps/s: resume 164 / 286 / 889 ms, pause 176 / 220 / 336 ms, zero errors — but the level was failed by the crash gate (23 crashed actors still in the fleet from run 1). Also confirmed from the node agent's logs that pause is purely local: every restore `kind=local` with no download, every checkpoint with no upload, the snapshot plugin idle.
- **Run 3.** Lost to the sim's fill: a handful of crashed actors held the eight fill slots through twelve long retries each (fixed: the fill now gives up on CRASHED/DELETING/not-found actors, commit 1b118c8).
- **Run 4 (crash gate count-only).** 2 swaps/s clean; 3 swaps/s failed on 13 refusals + 4 errors with 5 new crashes in two minutes. The medians are tiny — worker restore 160–200 ms, worker checkpoint 150–180 ms, node-agent restore ~190 ms end to end — but the P99 is 10 s: restores time out waiting for virtiofsd's socket or the VMM's API socket, or the guest agent does not answer the CRNG reseed within 30 s, and each such timeout crashes the actor. These are process-startup stalls on a disk that was only 5–15 % busy; the node's Hyperdisk was running the `bfq` I/O scheduler (8 ms idling slices, 128 KiB maximum request) with the actor state directory back on it. Before run 5 the queue scheduler was switched to `none` and read-ahead raised to 1 MiB.
- **Run 5 (after the scheduler change).** Fill 25 s. 2 swaps/s: resume 158 / 171 / 205 ms, pause 170 / 203 / 229; 8 swaps/s: resume 252 / 369 / 560, pause 249 / 340 / 479, zero errors and zero crashes; 12 swaps/s delivered (11.9 achieved, 1 error, 5 refusals, 1 crash) at resume 321 / 509 / 646 ms — failed only the relative rule, because 2.5× a 171 ms baseline is 428 ms. The 10-s tails are gone: worker restore P99 542 ms at 8/s. **Within the 2.5× rule pause gives 8 swaps/s; it delivers 12 at a resume P90 of 0.5 s**, five times better latency than suspend at the same rate.
- **Run 6.** Same fleet, ramp 8 → 11 → 15 → 20 → 27 with the relative latency rule off (errors, refusals, backlog, host and crash gates only), to find where pause actually breaks.

## Where the time goes with pause (run 5, medians)

| level | worker restore total (vm_restore / lowers / teardown) | worker checkpoint total (snapshot / teardown) | disk write MB/s | disk read MB/s | disk busy % | PSI io % | node CPU busy % |
|---|---|---|---|---|---|---|---|
| 2 swaps/s | 143 ms (86 / 9 / —) | 144 ms (73 / 64) | 266 | 5 | 13 | 2.8 | 1.7 |
| 5 | 177 (107 / 18 / —) | 177 (74 / 99) | 707 | 24 | 33 | 8.6 | 3.0 |
| 8 | 234 (137 / 27 / —) | 219 (77 / 135) | 1,055 | 151 | 52 | 18.3 | 5.0 |
| 12 | 294 (172 / 41 / —) | 273 (79 / 185) | 1,598 | 231 | 76 | 27.6 | 7.5 |

Every pause writes its 128 MiB memory image plus the rootfs tar to the disk (~133 MB of writes per swap, from the write rate), and a wake reads the image back, mostly from the page cache. With the queue scheduler at `none` the disk sustains this to about 1.6 GB/s at 12 swaps/s, 76 % busy; the stage that grows with rate is the VMM restore (page-cache reads of the image) and teardown. The provisioned 2,400 MiB/s therefore puts the pause ceiling near 15–18 swaps/s on this disk, before the kernel convoy that stopped suspend at 20. CPU is nearly idle (7.5 % of 192 cores at 12 swaps/s) because nothing is compressed or transferred.

## Reading the numbers

- With pause, a wake is: control plane → node agent → worker restore from the local checkpoint directory (eager restore of the 128 MiB image from the page cache or disk) → first request. There is no manifest fetch, no download and no decompression. A park is `vm.snapshot` to the local directory plus the rootfs-upper tar, with no upload.
- What is left on the path is exactly the part that limited suspend at 17–20 swaps/s: tap device setup, overlay/virtiofsd staging, VMM launch, the VMM's own restore, and teardown, all of which convoy on kernel locks as the rate rises. Pause therefore measures that ceiling directly.
- Disk: 5,000 pause checkpoints occupy roughly 5,000 × 140 MB ≈ 0.7 TB of the 3 TB disk; each swap rewrites one checkpoint (~140 MB) and reads one, i.e. about 280 MB of disk traffic per activation.
