# Host fill — how many personal-assistant agents one c3-standard-192-metal node sustains (measured 2026-10-07)

Test per `Workload TCO Calculations.md`: same host, same assistant actor (substrate#2230 lap, 1.5 GiB actor / 3 GiB limit on gVisor, ~1 GiB resident, 61 steps per agent-day, think ×0.02), keep adding load until something breaks. **The pool is multi-actor** (ateom `--max-actors` 1000; an unsized WorkerPool is unconstrained), so the knob is the agent count on a fixed pool of 50 workers, not the worker count; "active" is the measured peak of live actors. Each leg: 20-min window, starts staggered over 5 min, host sampled every 15 s (memory, CPU, Hyperdisk, network). Substrate perf/resume-latency @ 584d0318.

Stop rules, evaluated per leg: **literal** = resume P90 ≤ GA bar (150 ms gVisor / 300 ms microVM) ∧ host memory ≤ 90 % ∧ errors+refusals ≤ 0.5 %; **degradation** = same but resume P90 ≤ 2 × the cell's own 60-agent control. The ladder bisects on the degradation rule.

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
| 60 | 21 (6) | 572 / 626 / 934 | 1.0 / 62.7 / 108.2 | 2.07 % (4+10 of 676) | 76 (10 %) | 3 % | 780 / 1,751 | 19 | fail | fail |

- **Ceiling (degradation rule):** 30 agents pass, 60 fails → measured peak live actors at the ceiling **13**; live mean 3.9.
- **Ceiling (literal GA bars):** 0 agents — the GA resume bars are not met by this 1 GiB-resident actor at any rung.
- **Step 8, ACE's basis:** active per node = 13; runnable = 13 × 17 (Oct 6 agents/worker for microvm pause) = 221; memory overcommit = 221 × 1.0 GiB ÷ 768 = 0.3×; $ at 100 % active = $3179 ÷ 13 = **$244.54** per active agent-month; on our basis ($3179 ÷ 13) ÷ 17 + $0.09 = **$14.47** per runnable agent-month.

## microvm · suspend

| Agents (50 workers) | Peak live actors | Resume P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Err + refusals | Host mem GB (%) | Host CPU p90 | Hyperdisk write p90 / max MiB/s | Net tx max MiB/s | Literal | Degradation |
|---|---|---|---|---|---|---|---|---|---|---|
| 60 | 59 (13) | 633 / 699 / 843 | 3.1 / 40.8 / 73.9 | 0.00 % (0+0 of 739) | 73 (10 %) | 7 % | 1,032 / 2,170 | 2,172 | fail | pass |
| 90 | 90 (25) | 638 / 723 / 31,804 | 3.1 / 74.8 / 117.2 | 30.84 % (93+175 of 869) | 136 (18 %) | 8 % | 1,241 / 2,397 | 4,116 | fail | fail |
| 120 | 120 (32) | 641 / 772 / 43,502 | 4.4 / 76.1 / 113.3 | 26.61 % (127+133 of 977) | 146 (19 %) | 10 % | 1,434 / 2,143 | 2,965 | fail | fail |

- **Ceiling (degradation rule):** 60 agents pass, 90 fails → measured peak live actors at the ceiling **59**; live mean 13.0.
- **Ceiling (literal GA bars):** 0 agents — the GA resume bars are not met by this 1 GiB-resident actor at any rung.
- **Step 8, ACE's basis:** active per node = 59; runnable = 59 × 30 (Oct 6 agents/worker for microvm suspend) = 1,770; memory overcommit = 1,770 × 1.0 GiB ÷ 768 = 2.3×; $ at 100 % active = $3179 ÷ 59 = **$53.88** per active agent-month; on our basis ($3179 ÷ 59) ÷ 30 + $0.09 = **$1.89** per runnable agent-month.

## Reading the ladders

- **The limiter is the park path's disk traffic, not host memory or CPU.** Every rung that failed did so with host memory under a quarter of 768 GB and CPU P90 under a third. The suspend path stages each checkpoint on the node's Hyperdisk (raw page image, then the compressed copy) before uploading; pause writes the raw image and keeps it. Hyperdisk Balanced is provisioned at 2,400 MiB/s; the write P90 is already above half of that at the 60-agent control and the failing rungs hit the ceiling, I/O pressure climbs, parks queue behind each other and the backlog feeds itself (over 100 of 120 actors suspending at once).
- **Think ×0.02 multiplies the park rate by 50.** 60 agents here park as often as ~3,000 real-time assistants. The sustainable rate is about one 1 GiB park per second per host; at real time that is ~1,500 personal-assistant agents per host on the park/wake path regardless of how many are awake at once. The active (awake) ceiling set by memory is a different test — resident actors that never park — and is not what this ladder measures.
- **Orphaned node-local checkpoints are an operational hazard.** Substrate does not reclaim `/var/lib/ate/actors/<uid>` for deleted actors; a 3 TB boot disk went from 69 % to DiskPressure in a morning of legs (atelet evicted). Any pause-path deployment needs a reclaimer.
- **W, the worker count, is not the active cap.** Our pools are multi-actor since 2026-09-24 (#1836); 134 live actors were observed on 48 workers. "Workers per host" in the slide-3 arithmetic should be read as "worker pods", and density as live actors per host.
