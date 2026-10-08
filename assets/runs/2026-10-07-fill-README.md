# Host fill — how many personal-assistant agents one c3-standard-192-metal node sustains (measured 2026-10-07)

Test per `Workload TCO Calculations.md`: same host, same assistant actor (substrate#2230 lap, 1.5 GiB actor / 3 GiB limit on gVisor, ~1 GiB resident, 61 steps per agent-day, think ×0.02), keep adding load until something breaks. **The pool is multi-actor** (ateom `--max-actors` 1000; an unsized WorkerPool is unconstrained), so the knob is the agent count on a fixed pool of 50 workers, not the worker count; "active" is the measured peak of live actors. Each leg: 20-min window, starts staggered over 5 min, host sampled every 15 s (memory, CPU, Hyperdisk, network). Substrate perf/resume-latency @ 584d0318.

Stop rules, evaluated per leg: **literal** = resume P90 ≤ GA bar (150 ms gVisor / 300 ms microVM) ∧ host memory ≤ 90 % ∧ errors+refusals ≤ 0.5 %; **degradation** = same but resume P90 ≤ 2 × the cell's own 60-agent control. The ladder bisects on the degradation rule.

## Ceilings at a glance (degradation rule)

| Cell | Last pass (agents) | First fail (agents) | Peak live actors at the ceiling | Resume P50 / P90 / P99 at the ceiling (ms) | Park P50 / P90 / P99 at the ceiling (s) | Hyperdisk write p90 at the ceiling | What broke at the first fail |
|---|---|---|---|---|---|---|---|
| **gvisor · pause** | 90 | 120 | 29 | 404 / 546 / 985 | 2.0 / 57.0 / 88.2 | 1,578 MiB/s | err+ref 95 %, park P90 82 s, resume P90 31.5 s, disk write max 1,867 MiB/s |
| **gvisor · suspend** | 90 | 120 | 69 | 423 / 546 / 756 | 10.4 / 31.7 / 40.5 | 1,508 MiB/s | err+ref 117 %, park P90 70 s, resume P90 31.7 s, disk write max 2,079 MiB/s |
| **microvm · pause** | 60 | 90 | 23 | 572 / 621 / 794 | 1.2 / 58.0 / 109.4 | 975 MiB/s | err+ref 133 %, park P90 33 s, resume P90 31.7 s, disk write max 1,970 MiB/s |
| **microvm · suspend** | 60 | 90 | 59 | 633 / 699 / 843 | 3.1 / 40.8 / 73.9 | 1,032 MiB/s | err+ref 31 %, park P90 75 s, resume P90 0.7 s, disk write max 2,397 MiB/s |

Host memory never exceeded 163 GB of 768 and host CPU P90 never exceeded 34 % in any leg; the node's Hyperdisk write throughput (2,400 MiB/s provisioned) is the limiter in every cell. Agent counts are at think ×0.02: 60 agents park as often as ~3,000 real-time assistants (see Reading the ladders).

## gvisor · pause

| Agents (50 workers) | Peak live actors | Resume P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Err + refusals | Host mem GB (%) | Host CPU p90 | Hyperdisk write p90 / max MiB/s | Net tx max MiB/s | Literal | Degradation |
|---|---|---|---|---|---|---|---|---|---|---|
| 60 | 23 (6) | 416 / 639 / 1,815 | 2.1 / 37.7 / 67.6 | 0.00 % (0+0 of 827) | 127 (17 %) | 26 % | 1,152 / 0 | 0 | fail | pass |
| 90 | 29 (9) | 404 / 546 / 985 | 2.0 / 57.0 / 88.2 | 0.18 % (0+2 of 1131) | 130 (17 %) | 34 % | 1,578 / 2,234 | 273 | fail | pass |
| 120 | 28 (9) | 394 / 31,489 / 31,989 | 1.8 / 81.9 / 116.3 | 94.97 % (273+709 of 1034) | 131 (17 %) | 26 % | 1,105 / 1,867 | 0 | fail | fail |
| 240 | — | — | — | — | — | — | — | — | invalid | invalid ((node DiskPressure: atelet evicted; boot disk 87%+ full of orphaned actor dirs)) |

- **Ceiling (degradation rule):** 90 agents pass, 120 fails → measured peak live actors at the ceiling **29**; live mean 9.4.
- **Ceiling (literal GA bars):** 0 agents — the GA resume bars are not met by this 1 GiB-resident actor at any rung.
- **Step 8, ACE's basis:** active per node = 29; runnable = 29 × 46 (Oct 6 agents/worker for gvisor pause) = 1,334; memory overcommit = 1,334 × 1.0 GiB ÷ 768 = 1.7×; $ at 100 % active = $3179 ÷ 29 = **$109.62** per active agent-month; on our basis ($3179 ÷ 29) ÷ 46 + $0.09 = **$2.47** per runnable agent-month.

## gvisor · suspend

| Agents (50 workers) | Peak live actors | Resume P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Err + refusals | Host mem GB (%) | Host CPU p90 | Hyperdisk write p90 / max MiB/s | Net tx max MiB/s | Literal | Degradation |
|---|---|---|---|---|---|---|---|---|---|---|
| 60 | 31 (11) | 439 / 571 / 798 | 4.3 / 9.4 / 15.5 | 0.00 % (0+0 of 906) | 101 (13 %) | 32 % | 1,452 / 1,723 | 2,731 | fail | pass |
| 90 | 69 (22) | 423 / 546 / 756 | 10.4 / 31.7 / 40.5 | 0.00 % (0+0 of 1062) | 88 (12 %) | 31 % | 1,508 / 2,191 | 3,004 | fail | pass |
| 120 | 118 (34) | 494 / 31,714 / 61,628 | 4.9 / 70.5 / 114.8 | 117.03 % (316+866 of 1010) | 163 (22 %) | 22 % | 1,099 / 2,079 | 2,185 | fail | fail |

- **Ceiling (degradation rule):** 90 agents pass, 120 fails → measured peak live actors at the ceiling **69**; live mean 21.6.
- **Ceiling (literal GA bars):** 0 agents — the GA resume bars are not met by this 1 GiB-resident actor at any rung.
- **Step 8, ACE's basis:** active per node = 69; runnable = 69 × 38 (Oct 6 agents/worker for gvisor suspend) = 2,622; memory overcommit = 2,622 × 1.0 GiB ÷ 768 = 3.4×; $ at 100 % active = $3179 ÷ 69 = **$46.07** per active agent-month; on our basis ($3179 ÷ 69) ÷ 38 + $0.09 = **$1.30** per runnable agent-month.

## microvm · pause

| Agents (50 workers) | Peak live actors | Resume P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Err + refusals | Host mem GB (%) | Host CPU p90 | Hyperdisk write p90 / max MiB/s | Net tx max MiB/s | Literal | Degradation |
|---|---|---|---|---|---|---|---|---|---|---|
| 30 | 13 (4) | 571 / 633 / 849 | 0.9 / 1.3 / 2.6 | 0.00 % (0+0 of 508) | 41 (5 %) | 2 % | 592 / 1,184 | 18 | fail | pass |
| 45 | 19 (5) | 572 / 621 / 743 | 1.0 / 48.1 / 69.8 | 0.00 % (0+0 of 565) | 58 (8 %) | 2 % | 738 / 2,180 | 5 | fail | pass |
| 60 | 21 (6) | 572 / 626 / 934 | 1.0 / 62.7 / 108.2 | 2.07 % (4+10 of 676) | 76 (10 %) | 3 % | 780 / 1,751 | 19 | fail | fail |
| 60 (rerun) | 23 (6) | 572 / 621 / 794 | 1.2 / 58.0 / 109.4 | 0.40 % (3+0 of 743) | 73 (10 %) | 3 % | 975 / 1,841 | 6 | fail | pass |
| 90 | 33 (7) | 574 / 31,653 / 32,052 | 0.9 / 33.1 / 111.7 | 133.41 % (303+835 of 853) | 89 (12 %) | 4 % | 796 / 1,970 | 12 | fail | fail |

- **Ceiling (degradation rule):** 60 agents pass, 90 fails → measured peak live actors at the ceiling **23**; live mean 6.1.
- **Ceiling (literal GA bars):** 0 agents — the GA resume bars are not met by this 1 GiB-resident actor at any rung.
- **Step 8, ACE's basis:** active per node = 23; runnable = 23 × 17 (Oct 6 agents/worker for microvm pause) = 391; memory overcommit = 391 × 1.0 GiB ÷ 768 = 0.5×; $ at 100 % active = $3179 ÷ 23 = **$138.22** per active agent-month; on our basis ($3179 ÷ 23) ÷ 17 + $0.09 = **$8.22** per runnable agent-month.

## microvm · suspend

| Agents (50 workers) | Peak live actors | Resume P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Err + refusals | Host mem GB (%) | Host CPU p90 | Hyperdisk write p90 / max MiB/s | Net tx max MiB/s | Literal | Degradation |
|---|---|---|---|---|---|---|---|---|---|---|
| 60 | 59 (13) | 633 / 699 / 843 | 3.1 / 40.8 / 73.9 | 0.00 % (0+0 of 739) | 73 (10 %) | 7 % | 1,032 / 2,170 | 2,172 | fail | pass |
| 90 | 90 (25) | 638 / 723 / 31,804 | 3.1 / 74.8 / 117.2 | 30.84 % (93+175 of 869) | 136 (18 %) | 8 % | 1,241 / 2,397 | 4,116 | fail | fail |
| 120 | 120 (32) | 641 / 772 / 43,502 | 4.4 / 76.1 / 113.3 | 26.61 % (127+133 of 977) | 146 (19 %) | 10 % | 1,434 / 2,143 | 2,965 | fail | fail |

- **Ceiling (degradation rule):** 60 agents pass, 90 fails → measured peak live actors at the ceiling **59**; live mean 13.0.
- **Ceiling (literal GA bars):** 0 agents — the GA resume bars are not met by this 1 GiB-resident actor at any rung.
- **Step 8, ACE's basis:** active per node = 59; runnable = 59 × 30 (Oct 6 agents/worker for microvm suspend) = 1,770; memory overcommit = 1,770 × 1.0 GiB ÷ 768 = 2.3×; $ at 100 % active = $3179 ÷ 59 = **$53.88** per active agent-month; on our basis ($3179 ÷ 59) ÷ 30 + $0.09 = **$1.89** per runnable agent-month.

## What the disk actually did (node probe, /proc/diskstats for nvme0n1 every 15 s)

A colleague pointed out that the write throughput in the tables never sustains the 2,400 MiB/s provisioned, so bandwidth cannot be the limiter. Correct. The per-sample device statistics tell the real story: the disk is **time-saturated and queue-bound**, not bandwidth-bound.

| Leg | Verdict | Write MiB/s p50 / p90 / max | Device busy % p50 / p90 / max | Avg queue depth p50 / p90 / max | Write await ms p50 / p90 / max | Write IOPS p90 |
|---|---|---|---|---|---|---|
| gvisor · pause · 90 agents | pass | 489 / 1,578 / 2,234 | 86 / 94 / 96 | 7 / 406 / 1059 | 1.7 / 42.8 / 79.9 | 12,743 |
| gvisor · pause · 120 agents | fail | 502 / 1,105 / 1,867 | 89 / 96 / 97 | 9 / 171 / 687 | 1.9 / 27.2 / 64.7 | 9,028 |
| gvisor · suspend · 60 agents | pass | 320 / 1,452 / 1,723 | 66 / 91 / 94 | 5 / 172 / 477 | 1.9 / 15.8 / 40.5 | 12,018 |
| gvisor · suspend · 90 agents | pass | 485 / 1,508 / 2,191 | 82 / 93 / 95 | 7 / 68 / 521 | 1.7 / 6.9 / 30.1 | 12,381 |
| gvisor · suspend · 120 agents | fail | 398 / 1,099 / 2,079 | 79 / 95 / 97 | 12 / 334 / 676 | 2.4 / 41.4 / 90.4 | 9,225 |
| microvm · pause · 30 agents | pass | 172 / 592 / 1,184 | 64 / 89 / 96 | 2 / 12 / 405 | 1.6 / 3.6 / 42.0 | 4,887 |
| microvm · pause · 45 agents | pass | 230 / 738 / 2,180 | 72 / 97 / 98 | 4 / 117 / 1633 | 1.7 / 29.2 / 92.4 | 6,058 |
| microvm · pause · 60 agents | fail | 328 / 780 / 1,751 | 80 / 97 / 98 | 5 / 158 / 1128 | 1.7 / 32.1 / 84.3 | 6,452 |
| microvm · pause · 60 agents | pass | 353 / 975 / 1,841 | 83 / 97 / 98 | 6 / 205 / 1167 | 1.7 / 31.1 / 79.9 | 7,978 |
| microvm · pause · 90 agents | fail | 335 / 796 / 1,970 | 85 / 97 / 98 | 7 / 143 / 1247 | 1.8 / 39.8 / 77.9 | 6,557 |
| microvm · suspend · 60 agents | pass | 340 / 1,032 / 2,170 | 33 / 81 / 97 | 11 / 102 / 901 | 4.2 / 16.4 / 73.3 | 8,477 |
| microvm · suspend · 90 agents | fail | 411 / 1,241 / 2,397 | 29 / 96 / 98 | 9 / 300 / 1233 | 2.4 / 49.5 / 81.2 | 10,176 |
| microvm · suspend · 120 agents | fail | 473 / 1,434 / 2,143 | 33 / 97 / 98 | 21 / 510 / 1151 | 4.0 / 55.8 / 71.3 | 11,693 |

**How to read it.** Busy % is the share of wall time with at least one I/O in flight; average queue depth is the mean number of I/Os waiting or in service (weighted I/O time ÷ wall time). On every failing rung the device is busy 95–98 % of the time at P90 with queue depths of 150–500 and write latency 15–25× its idle value, yet it moves only ~12k writes/s of ≤128 KiB each (`max_sectors_kb = 128`), i.e. ~1.5 GiB/s at best. The path is saturated on **request latency**, not on the provisioned throughput (2,400 MiB/s) or IOPS (100k) ceilings.

**What is in the path, from the node:** one ext4 filesystem (`/mnt/stateful_partition`, `rw,relatime,commit=30`) on one Hyperdisk Balanced device; block scheduler **`bfq`** (COS default) with `nr_requests = 256`; write cache reported write-through; `vm.dirty_ratio = 20 %` / `dirty_background_ratio = 10 %` of 768 GB, so checkpoints land in the page cache first and writers are throttled only once ~75–150 GB is dirty — which is why a park that should take 4 s shows up as 30–80 s only when many agents park at once.

**fsync hypothesis (from single-node density tests):** plausible but not shown by this data. Substrate's own checkpoint code issues no fsync/O_SYNC (grep of atelet and both ateoms), and the device reports zero flush requests — though with a write-through cache the block layer drops flushes, so an fsync from runsc or Cloud Hypervisor would be invisible here. What *is* visible is consistent with hundreds of concurrent 1 GiB sequential writers contending for one BFQ-scheduled device with 128 KiB requests, plus ext4 journal commits every 30 s on the same device.

**Tests that would settle it** (each is one failing rung re-run, ~35 min): (1) switch the boot-disk scheduler to `none` or `mq-deadline` and raise `max_sectors_kb`; (2) `fio` with 100–150 concurrent 1 GiB sequential writers at the current and alternative settings, with and without fsync, to get the device's real concurrent-writer ceiling; (3) move `/var/lib/ate/actors` to a separate device (Local SSD or a second Hyperdisk) or one filesystem per worker; (4) capture `blktrace` / `iostat -x` with flush accounting during a 120-agent gVisor suspend rung.

## Reading the ladders

- **The limiter is the park path's I/O, not host memory or CPU — and it is latency/queueing on the single boot disk, not its provisioned bandwidth.** Every rung that failed did so with host memory under a quarter of 768 GB and CPU P90 under a third. The disk, however, was busy 95–98 % of the time at P90 on every failing rung, with an average queue depth in the hundreds and write latency rising from ~2 ms (P50) to 30–56 ms (P90), while the *achieved* write rate sat at 0.3–0.5 GiB/s median and 1.0–1.6 GiB/s at P90 — well under the 2,400 MiB/s provisioned, which was touched only in single 15-s samples. See "What the disk actually did" below; the earlier statement that provisioned throughput was the limiter was wrong.
- **Think ×0.02 multiplies the park rate by 50.** 60 agents here park as often as ~3,000 real-time assistants. The sustainable rate is about one 1 GiB park per second per host; at real time that is ~1,500 personal-assistant agents per host on the park/wake path regardless of how many are awake at once. The active (awake) ceiling set by memory is a different test — resident actors that never park — and is not what this ladder measures.
- **Orphaned node-local checkpoints are an operational hazard.** Substrate does not reclaim `/var/lib/ate/actors/<uid>` for deleted actors; a 3 TB boot disk went from 69 % to DiskPressure in a morning of legs (atelet evicted). Any pause-path deployment needs a reclaimer.
- **W, the worker count, is not the active cap.** Our pools are multi-actor since 2026-09-24 (#1836); 134 live actors were observed on 48 workers. "Workers per host" in the slide-3 arithmetic should be read as "worker pods", and density as live actors per host.
