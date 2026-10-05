# $ / agent-month: how we get there — coding-session workload, measured 2026-10-05

Cluster `agents-tco` (GKE 1.36, us-central1-c, 2 × c3-standard-4 with nested virt), Substrate main @ 01e299f9, agent-session coding script from substrate#1934 (think ×4, driver suspend, 1 GiB actors). Same 40 agents and identical per-agent schedules in both runs (seeded), 27-min window at ×6 time.
Artifacts: `assets/runs/2026-10-05-coding-session-{gvisor,microvm}/` (report.txt, latency.csv, summary.json, run.log, occupancy.csv, metrics.txt).

| Step | gVisor | microVM |
|---|---|---|
| Host, 3-yr CUD | c3-standard-4, $66.2/mo (cud3) · 2 node(s) | c3-standard-4, $66.2/mo (cud3) · 2 node(s) |
| Workers per host → $ per worker-month | 5 → $13.25 | 5 → $13.25 |
| Agent profile | coding-session: 8 tasks × 20 steps/day, think ×4 (276 s/task), driver suspend, 1Gi actors | coding-session: 8 tasks × 20 steps/day, think ×4 (276 s/task), driver suspend, 1Gi actors |
| Run | 40 agents, time ×6, 27 min; 599 activations, 9 errors, 35 refusals | 40 agents, time ×6, 27 min; 618 activations, 0 errors, 5 refusals |
| Resume P50 / P90 / P99 | 1,251 / 2,152 / 31,512 ms (n=599) ← T_r | 1,906 / 3,278 / 9,603 ms (n=618) ← T_r |
| Suspend | 2,595 ms avg (driver, n=591); P50/P90/P99 2,262 / 3,795 / 8,594 ms (n=591) | 3,849 ms avg (driver, n=618); P50/P90/P99 2,720 / 8,419 / 14,132 ms (n=618) |
| Step work P50 / P90 / P99 | 822 / 3,342 / 4,082 ms (n=591) | 1,030 / 3,663 / 4,627 ms (n=618) |
| Worker time per wake-up | 3.8 s (wait + T_s + T_r) | 5.8 s (wait + T_s + T_r) |
| Occupancy (worker time per agent) | 0.94 %  (8 tasks × 20 steps = 160 steps, 1.2s work each (measured)) | 1.35 %  (8 tasks × 20 steps = 160 steps, 1.5s work each (measured)) |
| Measured density | 22.1:1 mean · 8.0:1 at P99 (busy workers mean 1.8, P99 5, peak 6 of 10) | 15.3:1 mean · 5.7:1 at P99 (busy workers mean 2.6, P99 7, peak 8 of 10) |
| Agents per worker (overcommit) | 37.2 = 0.70 ÷ (0.94 % × 2 peak) | 25.9 = 0.70 ÷ (1.35 % × 2 peak) |
| Agents per host | 186 | 129 |
| $ / agent-month | $13.25 ÷ 37.2 = $0.36 compute + $0.18 GCS ops + $0.003 snapshots = **$0.54** | $13.25 ÷ 25.9 = $0.51 compute + $0.18 GCS ops + $0.005 snapshots = **$0.69** |
| Multi-actor projection (roadmap) | ≈246 agents/host → ≈$0.45 | ≈166 agents/host → ≈$0.58 |

## Per-step resume latency (ms), P50 / P99

| Step | gVisor wake P50 / P99 | gVisor step work P50 | microVM wake P50 / P99 | microVM step work P50 |
|---|---|---|---|---|
| 01_read_task | 506 / 31590 | 117 | 1062 / 3709 | 257 |
| 02_clone_repo | 973 / 31564 | 618 | 1278 / 4481 | 980 |
| 03_explore_tree | 927 / 31512 | 242 | 1556 / 2905 | 335 |
| 04_read_key_files | 812 / 1689 | 162 | 1709 / 4657 | 509 |
| 05_install_deps | 910 / 1616 | 1708 | 1450 / 6082 | 2477 |
| 06_first_build | 1277 / 1877 | 3287 | 1717 / 9923 | 3990 |
| 07_run_unit_tests | 1490 / 3069 | 2559 | 1835 / 3909 | 2692 |
| 08_reason_about_failure | 1483 / 3518 | 24 | 2146 / 7605 | 153 |
| 09_edit_source | 1510 / 8586 | 34 | 2137 / 3409 | 121 |
| 10_incremental_build | 1494 / 2323 | 1249 | 2007 / 3134 | 1325 |
| 11_rerun_failed_test | 1391 / 2571 | 810 | 1937 / 3175 | 811 |
| 12_write_new_tests | 1439 / 5308 | 31 | 2110 / 5404 | 62 |
| 13_run_new_tests | 1290 / 2645 | 1023 | 2102 / 9024 | 1051 |
| 14_full_test_suite | 1281 / 2390 | 4055 | 1985 / 3753 | 4150 |
| 15_lint_format | 1048 / 3823 | 926 | 1971 / 9603 | 961 |
| 16_refactor | 1224 / 1943 | 661 | 1965 / 3820 | 757 |
| 17_rebuild | 1058 / 2735 | 2144 | 1853 / 3793 | 2485 |
| 18_final_test_suite | 1099 / 2480 | 3510 | 1847 / 3854 | 3514 |
| 19_package_artifact | 1171 / 1542 | 1134 | 1916 / 3014 | 1531 |
| 20_commit_and_summarize | 1125 / 2152 | 185 | 2035 / 3731 | 820 |

## Reading the numbers

- **microVM costs 28 % more than gVisor on the same hosts ($0.69 vs $0.54)** and all of it is switch time: suspend P50 2.7 s vs 2.3 s, resume P50 1.9 s vs 1.3 s, snapshot 0.25 GiB vs 0.13 GiB. Step work is within 25 % (1.5 s vs 1.2 s mean).
- **Switching is the cost model for this workload.** Per agent-day: 197–247 s of work versus 608–928 s of suspend+resume; GCS operations ($0.18) are a quarter to a third of the bill. Each second off the per-wake switch is worth ≈12 % (gVisor) / ≈9 % (microVM) of the price.
- **Tails.** gVisor's resume P99 (31.5 s) is one failure: a restore hit "inconsistent private memory files on restore", the actor went to ACTOR_STATE_CRASHED, and Substrate refuses to resume a CRASHED actor (an absorbing state next to #1914's SUSPENDING wedge); its retries carried steps 01–03. Steps 04–20 sit at 1.5–8.6 s P99. microVM had no failures; its P99 of 9.6 s is genuine tail (suspend P99 14.1 s).
- **Load.** 22 of 40 agents drew a task in the window (8 tasks/day at ×6), so the pool ran light (gVisor 1.8, microVM 2.6 busy workers mean); density divides the full fleet by busy workers. The $ figure prices the real-world rate (8 tasks/day) and does not depend on the window's load.
- **Assumptions in the $ line:** utilization 0.7, peak ×2, 3-yr CUD; excludes LLM tokens and the amortized cluster fee / control plane (≈ $573 / mo ÷ fleet). T_s, T_r, step work, snapshot size and density are measured.
