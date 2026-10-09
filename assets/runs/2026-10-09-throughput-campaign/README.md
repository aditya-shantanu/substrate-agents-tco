# Activation throughput with 650 awake nano agents — optimization campaign (2026-10-09)

Goal: raise steady-state suspend+resume throughput (swaps/s) with 650 nano-personal-agent actors awake and 5,000 registered, one c3-standard-192-metal node per runtime, Substrate main 66f8a888 plus the experimental patches listed per iteration. Full method, assumptions and the original numbers: `nano-personal-agent-cold-start-and-turnover-5000-gvisor-vs-microvm-main-2026-10-08.md`.

Each level runs 2 minutes (4 in the original test); the gates are: resume or suspend P90 > 2.5× the first level, errors or refusals > 0.5 % of wakes, more than 5 s' worth of swaps still in flight at the level's end (backlog), host memory/PSI limits, crashed actors (5, later 20). A level marked `pass` with a higher achieved rate than the last clean one but a `wake-p90-vs-baseline` verdict means the system delivered the rate with zero errors and only the relative-latency rule stopped the ramp.

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

_see the agent's report for the change set_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 536 / 660 / 938 | 593 / 713 / 992 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 610 / 733 / 957 | 616 / 761 / 995 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 840 / 1,056 / 1,211 | 717 / 922 / 1,109 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.87** | 1,305 / 1,821 / 2,706 | 989 / 1,288 / 1,616 | 0 + 0 | 8 | 0 | wake-p90-vs-baseline |
| 5 per tick | 5.00 | **4.98** | 842 / 1,062 / 1,226 | 740 / 936 / 1,152 | 0 + 0 | 0 | 0 | pass |

### gVisor agent iteration 2

_see the agent's report for the change set_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 557 / 762 / 1,008 | 588 / 763 / 940 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 614 / 757 / 973 | 623 / 745 / 962 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 813 / 929 / 1,083 | 713 / 866 / 1,074 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.87** | 1,150 / 1,374 / 1,649 | 861 / 1,037 / 1,257 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.62** | 2,563 / 3,178 / 4,106 | 1,246 / 1,695 / 1,958 | 0 + 0 | 36 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 3

_see the agent's report for the change set_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 539 / 669 / 785 | 580 / 714 / 847 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 594 / 675 / 879 | 591 / 696 / 807 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 800 / 968 / 1,208 | 683 / 878 / 1,159 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.88** | 1,080 / 1,315 / 1,859 | 838 / 1,035 / 1,267 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.72** | 1,618 / 2,363 / 3,005 | 1,104 / 1,572 / 1,962 | 0 + 0 | 24 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 4

_see the agent's report for the change set_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 530 / 656 / 782 | 552 / 650 / 796 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 597 / 687 / 811 | 566 / 670 / 789 | 0 + 0 | 3 | 0 | pass |
| 5 per tick | 5.00 | **4.96** | 784 / 935 / 1,071 | 650 / 728 / 870 | 0 + 0 | 0 | 0 | pass |
| 8 per tick | 8.00 | **7.91** | 1,076 / 1,254 / 1,425 | 783 / 958 / 1,215 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.77** | 1,584 / 2,365 / 2,877 | 1,024 / 1,367 / 1,658 | 0 + 0 | 24 | 0 | wake-p90-vs-baseline |

### gVisor agent iteration 5

_see the agent's report for the change set_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 691 / 797 / 890 | 651 / 823 / 921 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 761 / 862 / 971 | 677 / 842 / 956 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.92** | 991 / 1,174 / 1,355 | 850 / 1,069 / 1,274 | 0 + 0 | 5 | 0 | pass |
| 8 per tick | 8.00 | **7.77** | 2,328 / 2,786 / 3,166 | 1,860 / 2,265 / 2,520 | 0 + 0 | 24 | 0 | wake-p90-vs-baseline+park-p90-vs-baseline |

### gVisor agent iteration 6

_see the agent's report for the change set_

| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|
| 2 per tick | 2.00 | **1.98** | 652 / 791 / 935 | 648 / 777 / 908 | 0 + 0 | 0 | 0 | pass |
| 3 per tick | 3.00 | **2.97** | 731 / 842 / 1,022 | 669 / 792 / 918 | 0 + 0 | 0 | 0 | pass |
| 5 per tick | 5.00 | **4.92** | 885 / 1,025 / 1,190 | 750 / 893 / 1,046 | 0 + 0 | 5 | 0 | pass |
| 8 per tick | 8.00 | **7.87** | 1,165 / 1,420 / 1,640 | 943 / 1,129 / 1,312 | 0 + 0 | 8 | 0 | pass |
| 12 per tick | 12.00 | **11.80** | 1,824 / 2,565 / 3,151 | 1,293 / 1,654 / 2,063 | 0 + 0 | 12 | 0 | wake-p90-vs-baseline |
