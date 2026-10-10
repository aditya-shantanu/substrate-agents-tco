# Resident fill with the nano-personal-agent — awake agents per bare-metal node, gVisor vs microVM (2026-10-10)

How many nano-personal-agent actors one c3-standard-192-metal node (192 vCPU, 755 GiB) keeps awake at once, with no parking at all. Same method as the October 7 resident-fill test with the personal-assistant actor: each node has its fleet registered (cold boot from the golden snapshot, first ping, one park); the harness wakes a wave of agents, lets them stay resident playing the assistant day (61 events per 24 h at think ×0.02, mocked model/API/file ops, no synthetic heap), measures a window, and if the window passes every gate wakes the next wave. The last clean wave is the node's number. Gates: node MemAvailable < 10 %, memory PSI full above the gate, CPU PSI some > 50 %, CPU-probe P99 > 1 s, turn P99 above the gate, wake P99 above the gate, wake or turn P90 > 2× the first wave, errors or refusals above the gate, crashed actors above the gate. Substrate main 66f8a888 plus each node's end-of-campaign experimental patches (gVisor: network-namespace pool, shared gofer namespace, lean teardown, no app cgroup; microVM: pooled namespaces, raised socket waits, reseed retry, no tar fsync; both: pooled snapshot plugin). 100 unsized worker pods per node, catch-up off, the two nodes ran in parallel.

## Result in one table

| | gVisor | microVM |
|---|---|---|
| **Awake nano agents per node, last clean wave** | **5,950** | **4,250** |
| First failing wave and why | 6,150: turn P90 > 2× first wave | 4,500: turn P90 > 2× first wave, mem-available |
| 10-min hold at the last clean count | probe P90 > 2× first wave, turn P90 > 2× first wave (probe P90 40.7 ms, turn P90 31 ms, 51.1 % memory free) | probe P90 > 2× first wave, turn P90 > 2× first wave (probe P90 39.0 ms, turn P90 24 ms, 12.0 % memory free) |
| Node memory available at the last clean wave | 51.6 % | 12.8 % |
| Sandboxes resident on the node at the last clean wave | 5,901 | 4,876 |
| Host memory per resident sandbox (slope over the clean waves) | 59 MiB | 127 MiB |
| CPU probe P99 / turn P99 at the last clean wave | 75 / 90 ms | 60 / 55 ms |
| Wake P50 / P90 at the last clean wave (whole wave woken together) | 1,537 / 2,734 ms | 1,286 / 4,469 ms |
| Extension with the 2×-first-wave rules switched off (absolute gates only: probe P99 1 s, turn P99 1 s, memory 10 %, PSI, errors / refusals 5 %) | **7,550** passed, 7,750 failed: errors > gate; probe P99 253 ms, turn P99 189 ms, 39.3 % memory free, CPU PSI some 31.91 %; **10-min hold at 7,550: turn P99 > gate, CPU probe P99 > 1 s** (probe P90 / P99 230.2 / 2018.6 ms, turn P90 / P99 306 / 1955 ms) | not run — the microVM node is out of memory, there is nothing to switch off |
| Wave size / interval of the final run | 200 / 2 min | 250 / 3 min |
| October 7 reference: personal-assistant actor (~1 GiB resident), 1 vCPU | 650 | 650 |
| October 8 reference: ping actor (256 MiB, idle) | ≥ 4,000 (fleet exhausted) | 3,550 |

## What limited each node

**microVM: guest memory, and nothing else.** Every microVM nano agent costs the host the slope below in resident memory (the 256 MiB guest is memfd-backed and the agent touches most of it after a few events, plus the VMM, virtiofsd and the tap). At 4,250 awake agents (4,876 VMs resident) the node had 12.8 % of 755 GiB left and the kernel was already reclaiming: the CPU probe P99 went from 26–35 ms to 60 ms and the turn P99 from 12–14 ms to 55 ms, and in the 10-minute hold both P90s were more than 2× the first wave's (still only tens of milliseconds in absolute terms). The next wave, 4,500, dropped MemAvailable to 8.6 % and failed the 10 % rule. CPU was idle (PSI cpu some ≤ 8 %), disk idle, no refusals, 0.0–0.1 % errors throughout; the 21 crashed actors are the leftovers of earlier runs (virtiofsd / reseed start-up failures). So the microVM ceiling is a straight memory budget: ≈ 755 GiB × 0.9 ÷ (memory per VM) minus the worker pods and the page cache the node needs; a node with 1.5 TB would roughly double it, as would a smaller guest (the agent itself needs well under 100 MiB).

**gVisor: CPU contention from the agents' own work, with half the memory still free.** A resident gVisor nano agent costs the host only 59 MiB (the sentry backs just the pages the application touches), so at the rule ceiling of 5,950 awake agents the node still had 51.6 % of 755 GiB available; the 9,000-actor fleet would fit in memory twice over. What moved was CPU: the node went from 2 % busy at 1,150 agents to 33 % busy (peak 48 %, half of it kernel time) at 5,950, load average 43 on 192 vCPUs, CPU PSI some 7–13 %. The agents' assistant day runs at think ×0.02, so each agent issues an event every ~28 s, and every event is a tool turn through runsc (netstack, gofer, syscall interception) — about 0.01 vCPU per agent, 60 vCPUs for 6,000 agents. With that much steady background load the latency floor rises: the CPU probe P99 went 17 → 75 ms and the turn P99 7 → 90 ms between 1,150 and 5,950, and at 6,150 the turn P90 crossed 2× the first wave's (the rule that stopped the run; the absolute numbers were still below 100 ms). The 10-minute hold at 5,950 confirmed it: probe P90 41 ms, turn P90 31 ms, 0 refusals, 0.2 % errors, 51 % memory free. The extension run (same fleet, 2×-first-wave rules off, absolute gates only) shows where the hard wall is: waves kept passing up to 7,550 awake agents, but by then the node was 54–76 % busy with kernel time larger than user time (sys 17–40 %: runsc's syscall interception, netstack and the gofer, plus the scheduler shuffling ~7,500 sandboxes), CPU PSI some reached 32–45 %, wakes took 7–10 s at P90 and 1–2 % of them came back as router 502s because the sentry was not ready inside the router's budget; at 7,750 the 502s reached 9.2 % and the wave failed on errors; and the 10-minute hold at 7,550 failed outright with probe and turn P99 around 2 s. So for this agent on this build a gVisor node sustains about 6,000 awake nano agents within the rules and tops out between 6,000 and 7,500 on CPU, with roughly half the memory unused; a faster per-event path in runsc (or a lighter workload per event) would move that number, memory would not. Two walls had to be removed on the way and were not actor costs: (1) the wake burst — the whole wave is woken in the same second and the router parks requests to not-yet-ready actors for at most 5 s, so bursts of 250+ gVisor wakes produce 503s / client timeouts in the window, which is why the waves were cut to 200 and the error / turn gates relaxed; (2) the kernel ARP neighbour table — every gVisor actor adds two network namespaces and veths on the host, and at ~1,100 live sandboxes the host's neighbour cache hits the default gc_thresh3 = 1,024 and new sandboxes' traffic times out ('neighbor table overflow' in dmesg). This wall had been raised on both nodes on October 8, but the east node rebooted at 19:30 (cause unknown, the kernel log of the previous boot is gone) and lost the sysctl, which produced two spurious failures at 1,200–1,350 before it was found and raised again (gc_thresh3 = 16,384, inotify watches 1,048,576). Both are node-prep items for Substrate, not actor costs.

## Changes made for this test

- **Gates relaxed, both nodes:** errors and refusals 0.5 % → 5 %, turn P99 1 s → 30 s, wake P99 10 s → 60 s. Reason: the fill wakes a whole wave in one burst; with 200–500 wakes in the same second the router's 5-s parked-request budget is exceeded for the tail of the burst and the first turns of the just-woken agents dominate the window's turn P99. These gates only mask the burst, not resident degradation — the probe P99 (1 s), memory (10 %), PSI and the 2×-baseline rules stayed in force.
- **Memory PSI gate, microVM node:** 10 % → 30 % after dropping the page cache. The node still held 591 GB of pause checkpoints from the previous test on the Hyperdisk Extreme volume and the first fills failed on memory PSI from page-cache writeback, not from the agents.
- **Wave size:** gVisor 500 → 250 → 200 agents (burst limit above); microVM 500 → 250 for the top of the ladder.
- **East node after its reboot:** neighbour-table and inotify sysctls re-applied (`neightune`, `inotifytune` node-critical pods), worker pool / router / egress restarted, 907 crashed actors deleted.
- **Harness:** fill mode gives up on actors whose wake error is permanent (CRASHED / DELETING / not found) instead of retrying them 12 times (commit 1b118c8); `make_md_nanofill.py` writes this file.

## Run log

| Node | Run | Ladder | Outcome |
|---|---|---|---|
| gVisor | `nanofill-east-waves250` | 250-agent waves every 4 min from 250, original gates | 1,000 failed: turn P99 19.7 s and 0.5 % errors — the 250-agent wake burst parks requests in the router past its 5-s budget; gates relaxed (see below) |
| gVisor | `nanofill-east-waves200` | 200-agent waves every 2 min from 750, relaxed gates | 1,150 passed, 1,350 failed: 7.2 % client timeouts, wake P90 10.5 s, turn P99 36 s with 87 % memory free |
| gVisor | `nanofill-east-fine-neighwall` | 50-agent waves every 90 s from 1,150 | 1,200 failed at once: 12.3 % timeouts — cause found: the node reboot had reset the ARP neighbour table to gc_thresh3 = 1,024 (2,692 'neighbor table overflow' lines in dmesg); limits raised again, inotify limits too |
| gVisor | `nanofill-east` | 200-agent waves every 2 min from 1,150, neighbour table fixed | **5,950 passed**, 6,150 failed: turn P90 > 2× first wave; hold at 5,950: probe P90 > 2× first wave, turn P90 > 2× first wave |
| gVisor | `nanofill-east-ext` | extension: 200-agent waves every 2 min from 5,950, 2×-first-wave rules off, turn P99 gate 1 s | **7,550 passed**, 7,750 failed: errors > gate; hold at 7,550: turn P99 > gate, CPU probe P99 > 1 s |
| microVM | `nanofill-metal2-psi10` | 500-agent waves every 4 min from 500, original gates | 500 failed on memory PSI full 10 % — not the agents: 591 GB of pause checkpoints from the previous test were still in the page cache; caches dropped, PSI gate raised to 30 % |
| microVM | `nanofill-metal2-psi10` | rerun | 2,000 passed, 2,500 failed on memory PSI full 15.9 % (page cache again) |
| microVM | `nanofill-metal2-psi30` | 500-agent waves every 4 min from 2,000 | 3,500 passed, 4,000 failed: 2.6 % refusals, wake P90 35 s — the 500-wake burst, not the host (17.7 % memory free) |
| microVM | `nanofill-metal2` | 250-agent waves every 3 min from 3,500 | **4,250 passed**, 4,500 failed: turn P90 > 2× first wave, mem-available; hold at 4,250: probe P90 > 2× first wave, turn P90 > 2× first wave |

## gVisor — agents-tco-east, us-east4-a, actor 2 vCPU + 2 GiB limit (runsc, two network namespaces per actor): final run, wave by wave

Registered fleet 9,000 actors (0 failed to register). 'Sandboxes running on the node' counts every resident sandbox of the fleet, including the ones left awake by the earlier runs on the same fleet, so the memory slope uses that column.

| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1,150 | 1,015 | 0 / 0 / 0 | 4 / 7 | 13 / 15 / 21 | 0.0 | 0.0 | 87.7 % | 0.05 / 0 / 0 | 1,159 | 0 | pass |
| 1,350 | 315 | 511 / 602 / 813 | 5 / 9 | 13 / 15 / 20 | 0.0 | 0.0 | 86.6 % | 1.66 / 0 / 0 | 1,326 | 0 | pass |
| 1,550 | 1,168 | 621 / 757 / 890 | 4 / 7 | 14 / 15 / 20 | 0.0 | 0.0 | 85.1 % | 1.59 / 0 / 0 | 1,506 | 0 | pass |
| 1,750 | 1,101 | 635 / 793 / 1,069 | 5 / 8 | 14 / 15 / 20 | 0.0 | 0.0 | 83.6 % | 1.12 / 0 / 0 | 1,724 | 0 | pass |
| 1,950 | 1,230 | 658 / 838 / 1,284 | 5 / 8 | 14 / 15 / 22 | 0.0 | 0.0 | 82.4 % | 0.81 / 0 / 0.01 | 1,909 | 0 | pass |
| 2,150 | 4,498 | 681 / 862 / 1,355 | 4 / 8 | 14 / 16 / 21 | 0.0 | 0.0 | 80.7 % | 1.51 / 0 / 0 | 2,115 | 0 | pass |
| 2,350 | 4,887 | 700 / 888 / 1,680 | 4 / 8 | 14 / 16 / 21 | 0.0 | 0.0 | 79.1 % | 1.38 / 0 / 0 | 2,314 | 0 | pass |
| 2,550 | 8,771 | 732 / 955 / 1,597 | 4 / 9 | 14 / 16 / 24 | 0.0 | 0.0 | 77.6 % | 1.63 / 0 / 0 | 2,517 | 0 | pass |
| 2,750 | 8,212 | 724 / 926 / 1,314 | 4 / 9 | 14 / 16 / 25 | 0.0 | 0.0 | 76.2 % | 1.85 / 0 / 0 | 2,709 | 0 | pass |
| 2,950 | 10,919 | 769 / 1,006 / 1,761 | 4 / 10 | 14 / 16 / 25 | 0.0 | 0.0 | 74.6 % | 2.36 / 0 / 0 | 2,917 | 0 | pass |
| 3,150 | 9,149 | 776 / 1,044 / 1,442 | 4 / 10 | 14 / 16 / 24 | 0.0 | 0.0 | 73.1 % | 5.84 / 0 / 0.09 | 3,121 | 0 | pass |
| 3,350 | 12,070 | 814 / 1,238 / 2,371 | 4 / 10 | 14 / 17 / 25 | 0.0 | 0.0 | 71.6 % | 2.6 / 0 / 0 | 3,314 | 0 | pass |
| 3,550 | 11,313 | 846 / 1,464 / 2,320 | 4 / 10 | 14 / 16 / 25 | 0.0 | 0.0 | 70.1 % | 2.69 / 0 / 0.01 | 3,517 | 0 | pass |
| 3,750 | 14,760 | 900 / 1,210 / 2,236 | 6 / 58 | 15 / 19 / 71 | 0.0 | 0.0 | 68.6 % | 3.16 / 0 / 0 | 3,709 | 0 | pass |
| 3,950 | 14,980 | 920 / 1,521 / 3,078 | 6 / 30 | 15 / 18 / 41 | 0.0 | 0.0 | 67.0 % | 3.67 / 0 / 0 | 3,913 | 0 | pass |
| 4,150 | 14,701 | 995 / 1,646 / 3,268 | 5 / 11 | 14 / 17 / 27 | 0.0 | 0.0 | 65.5 % | 2.33 / 0 / 0 | 4,109 | 0 | pass |
| 4,350 | 14,698 | 1,058 / 1,670 / 2,799 | 5 / 12 | 14 / 17 / 28 | 0.0 | 0.0 | 64.0 % | 2.46 / 0 / 0 | 4,309 | 0 | pass |
| 4,550 | 13,556 | 1,182 / 2,104 / 3,111 | 5 / 12 | 14 / 17 / 27 | 0.1 | 0.0 | 62.3 % | 7.88 / 0 / 0 | 4,508 | 0 | pass |
| 4,750 | 12,224 | 1,325 / 2,254 / 4,063 | 5 / 12 | 14 / 17 / 31 | 0.0 | 0.0 | 60.9 % | 4.38 / 0 / 0 | 4,718 | 0 | pass |
| 4,950 | 12,717 | 1,491 / 3,153 / 5,298 | 5 / 14 | 14 / 17 / 31 | 0.1 | 0.0 | 59.3 % | 9.29 / 0 / 0.01 | 4,914 | 0 | pass |
| 5,150 | 12,855 | 1,248 / 2,504 / 4,159 | 5 / 12 | 14 / 17 / 28 | 0.2 | 0.0 | 58.0 % | 5.11 / 0 / 0 | 5,106 | 0 | pass |
| 5,350 | 13,321 | 1,549 / 2,775 / 4,297 | 5 / 14 | 15 / 17 / 32 | 0.2 | 0.0 | 56.4 % | 6.29 / 0 / 0.04 | 5,314 | 0 | pass |
| 5,550 | 15,781 | 2,143 / 5,566 / 8,160 | 6 / 24 | 15 / 18 / 37 | 0.2 | 0.0 | 54.8 % | 10.75 / 0 / 0 | 5,500 | 0 | pass |
| 5,750 | 17,144 | 1,578 / 4,501 / 7,197 | 6 / 24 | 15 / 19 / 83 | 0.2 | 0.0 | 53.1 % | 12.43 / 0 / 0 | 5,704 | 0 | pass |
| 5,950 | 19,888 | 1,537 / 2,734 / 4,712 | 8 / 90 | 15 / 21 / 75 | 0.2 | 0.0 | 51.6 % | 7.42 / 0 / 0 | 5,901 | 0 | pass |
| 6,150 | 20,333 | 1,962 / 4,689 / 6,713 | 10 / 63 | 16 / 24 / 87 | 0.1 | 0.0 | 50.1 % | 13.15 / 0 / 0 | 6,092 | 0 | turn P90 > 2× first wave |
| 5,950 | 116,985 | 0 / 0 / 0 | 31 / 173 | 16 / 40 / 190 | 0.2 | 0.0 | 51.1 % | 3.86 / 0 / 0 | 5,954 | 0 | probe P90 > 2× first wave, turn P90 > 2× first wave |
| hold at 5,950 (10 min) | 116,985 | 0 / 0 / 0 | 31 / 173 | 16.6 / 40.7 / 190.1 | — | — | 51.1 % | 3.86 / 0.0 / 0.0 | 5,954 | 0 | probe P90 > 2× first wave, turn P90 > 2× first wave |

### gVisor extension beyond the rule ceiling (2×-first-wave rules off)

| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 5,950 | 5,259 | 0 / 0 / 0 | 5 / 13 | 14 / 16 / 27 | 1.2 | 0.0 | 51.0 % | 0.35 / 0 / 0 | 5,954 | 0 | pass |
| 6,150 | 853 | 1,567 / 3,812 / 5,844 | 5 / 13 | 14 / 16 / 33 | 1.3 | 0.0 | 50.6 % | 12.04 / 0 / 0.01 | 6,105 | 0 | pass |
| 6,350 | 5,271 | 2,530 / 5,388 / 7,105 | 7 / 29 | 15 / 19 / 43 | 1.6 | 0.0 | 48.0 % | 15.11 / 0 / 0 | 6,306 | 0 | pass |
| 6,550 | 4,068 | 2,911 / 5,816 / 7,241 | 6 / 24 | 15 / 18 / 37 | 2.0 | 0.0 | 47.4 % | 10.27 / 0.32 / 0.02 | 6,516 | 0 | pass |
| 6,750 | 4,374 | 4,059 / 7,198 / 9,132 | 6 / 18 | 14 / 17 / 35 | 2.4 | 0.0 | 46.4 % | 15.21 / 0.1 / 0 | 6,703 | 0 | pass |
| 6,950 | 19,910 | 4,021 / 7,746 / 11,157 | 133 / 467 | 19 / 205 / 555 | 1.2 | 0.0 | 44.0 % | 23.72 / 0.1 / 0.23 | 6,898 | 0 | pass |
| 7,150 | 19,337 | 5,154 / 9,422 / 13,092 | 19 / 107 | 16 / 35 / 148 | 0.6 | 0.1 | 42.1 % | 22.23 / 0.68 / 3.29 | 7,104 | 0 | pass |
| 7,350 | 35,957 | 3,494 / 7,190 / 10,698 | 108 / 256 | 33 / 130 / 286 | 1.1 | 0.0 | 40.8 % | 34.79 / 0 / 0 | 7,288 | 0 | pass |
| 7,550 | 28,234 | 4,144 / 8,439 / 12,467 | 72 / 189 | 20 / 102 / 253 | 1.0 | 0.0 | 39.3 % | 31.91 / 0 / 0 | 7,477 | 0 | pass |
| 7,750 | 38,362 | 303 / 10,036 / 13,424 | 206 / 511 | 67 / 265 / 613 | 9.2 | 0.1 | 37.8 % | 44.6 / 2.55 / 2.16 | 7,679 | 1 | errors > gate |
| 7,550 | 170,411 | 1,016 / 2,102 / 3,432 | 305 / 1955 | 26 / 230 / 2018 | 3.1 | 0.0 | 38.6 % | 5.31 / 0 / 0.1 | 7,562 | 1 | turn P99 > gate, CPU probe P99 > 1 s |
| hold at 7,550 (10 min) | 170,411 | 1,016 / 2,103 / 3,433 | 306 / 1955 | 26.5 / 230.2 / 2018.6 | — | — | 38.6 % | 5.31 / 0.0 / 0.1 | 7,562 | 1 | turn P99 > gate, CPU probe P99 > 1 s |

## microVM — agents-tco-euw4, europe-west4-c, actor 2 vCPU + 256 MiB guest (Cloud Hypervisor, memfd guest RAM): final run, wave by wave

Registered fleet 5,000 actors (0 failed to register). 'Sandboxes running on the node' counts every resident sandbox of the fleet, including the ones left awake by the earlier runs on the same fleet, so the memory slope uses that column.

| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 3,500 | 3,500 | 31,504 / 31,504 / 31,504 | 5 / 12 | 14 / 17 / 26 | 0.1 | 0.3 | 25.0 % | 0.4 / 0 / 0 | 4,132 | 15 | pass |
| 3,750 | 3,256 | 688 / 895 / 18,341 | 6 / 13 | 14 / 19 / 30 | 0.1 | 0.5 | 20.9 % | 1.55 / 0.13 / 0 | 4,381 | 16 | pass |
| 4,000 | 4,107 | 700 / 882 / 46,505 | 5 / 14 | 14 / 19 / 35 | 0.1 | 0.7 | 16.9 % | 1.23 / 3.78 / 0 | 4,626 | 21 | pass |
| 4,250 | 13,537 | 1,286 / 4,469 / 5,424 | 7 / 55 | 16 / 23 / 60 | 0.0 | 0.1 | 12.8 % | 3.64 / 0.2 / 0 | 4,876 | 21 | pass |
| 4,500 | 22,048 | 1,957 / 5,335 / 31,489 | 15 / 159 | 17 / 33 / 137 | 0.0 | 0.1 | 8.6 % | 7.83 / 0.99 / 0 | 5,126 | 21 | turn P90 > 2× first wave, mem-available |
| 4,250 | 101,347 | 31,628 / 31,861 / 32,086 | 23 / 120 | 17 / 39 / 135 | 0.0 | 0.1 | 12.0 % | 4.49 / 12.76 / 1.11 | 4,876 | 21 | probe P90 > 2× first wave, turn P90 > 2× first wave |
| hold at 4,250 (10 min) | 101,347 | 31,628 / 31,862 / 32,086 | 24 / 120 | 17.4 / 39.0 / 135.9 | — | — | 12.0 % | 4.49 / 12.76 / 1.11 | 4,876 | 21 | probe P90 > 2× first wave, turn P90 > 2× first wave |

## Earlier runs, wave by wave

### gVisor `nanofill-east-waves250` — 250-agent waves every 4 min from 250, original gates

| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 500 | 500 | 511 / 622 / 749 | 4 / 4 | 14 / 16 / 20 | 0.2 | 1.0 | 92.2 % | 0 / 0 / 0.04 | 499 | 907 | refusals > gate, crashed actors > gate |
| 250 | 250 | 460 / 460 / 460 | 6 / 11 | 13 / 14 / 18 | 0.0 | 0.0 | 91.9 % | 0 / 0 / 0 | 500 | 0 | pass |
| 500 | 468 | 0 / 0 / 0 | 6 / 8 | 13 / 15 / 18 | 0.0 | 0.0 | 92.8 % | 0 / 0 / 0 | 500 | 0 | pass |
| 750 | 723 | 529 / 624 / 793 | 6 / 7 | 14 / 16 / 19 | 0.0 | 0.0 | 91.0 % | 0.02 / 0 / 0 | 750 | 0 | pass |
| 1,000 | 1,485 | 576 / 1,192 / 6,324 | 7 / 19712 | 14 / 15 / 20 | 0.5 | 0.0 | 89.2 % | 0.01 / 0 / 0 | 1,000 | 0 | turn P99 > gate |

### gVisor `nanofill-east-waves200` — 200-agent waves every 2 min from 750, relaxed gates

| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 750 | 660 | 0 / 0 / 0 | 5 / 9 | 13 / 15 / 18 | 0.0 | 0.0 | 90.7 % | 0 / 0 / 0.05 | 750 | 0 | pass |
| 950 | 261 | 477 / 563 / 644 | 5 / 8 | 13 / 15 / 19 | 0.0 | 0.0 | 89.5 % | 1.57 / 0 / 0 | 916 | 0 | pass |
| 1,150 | 778 | 618 / 2,983 / 6,914 | 5 / 11227 | 13 / 15 / 19 | 0.0 | 0.0 | 88.0 % | 1.53 / 0 / 0.01 | 1,117 | 0 | pass |
| 1,350 | 622 | 3,242 / 10,503 / 12,425 | 19644 / 36085 | 14 / 15 / 21 | 7.2 | 3.1 | 86.6 % | 1.35 / 0.14 / 0.29 | 1,307 | 0 | errors > gate, wake P99 > gate, turn P99 > gate, turn P90 > 2× first wave |
| 1,150 | 12,214 | 0 / 0 / 0 | 2075 / 52371 | 13 / 15 / 1029 | 10.2 | 0.0 | 87.8 % | 0.18 / 0 / 0 | 1,156 | 0 | errors > gate, turn P99 > gate, CPU probe P99 > 1 s, turn P90 > 2× first wave |
| hold at 1,150 (10 min) | 12,214 | 0 / 0 / 0 | 2075 / 52371 | 13.9 / 15.8 / 1030.0 | — | — | 87.8 % | 0.18 / 0.0 / 0.0 | 1,156 | 0 | errors > gate, turn P99 > gate, CPU probe P99 > 1 s, turn P90 > 2× first wave |

### gVisor `nanofill-east-fine-neighwall` — 50-agent waves every 90 s from 1,150

| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 750 | 660 | 0 / 0 / 0 | 5 / 9 | 13 / 15 / 18 | 0.0 | 0.0 | 90.7 % | 0 / 0 / 0.05 | 750 | 0 | pass |
| 950 | 261 | 477 / 563 / 644 | 5 / 8 | 13 / 15 / 19 | 0.0 | 0.0 | 89.5 % | 1.57 / 0 / 0 | 916 | 0 | pass |
| 1,150 | 778 | 618 / 2,983 / 6,914 | 5 / 11227 | 13 / 15 / 19 | 0.0 | 0.0 | 88.0 % | 1.53 / 0 / 0.01 | 1,117 | 0 | pass |
| 1,350 | 622 | 3,242 / 10,503 / 12,425 | 19644 / 36085 | 14 / 15 / 21 | 7.2 | 3.1 | 86.6 % | 1.35 / 0.14 / 0.29 | 1,307 | 0 | errors > gate, wake P99 > gate, turn P99 > gate, turn P90 > 2× first wave |
| 1,150 | 12,214 | 0 / 0 / 0 | 2075 / 52371 | 13 / 15 / 1029 | 10.2 | 0.0 | 87.8 % | 0.18 / 0 / 0 | 1,156 | 0 | errors > gate, turn P99 > gate, CPU probe P99 > 1 s, turn P90 > 2× first wave |
| 1,150 | 449 | 0 / 0 / 0 | 5 / 12 | 13 / 14 / 17 | 0.0 | 0.0 | 88.0 % | 0.05 / 0 / 0 | 1,156 | 0 | pass |
| 1,200 | 681 | 1,639 / 5,050 / 5,898 | 19706 / 36105 | 13 / 15 / 18 | 12.3 | 0.0 | 87.7 % | 0.1 / 0 / 0 | 1,165 | 0 | errors > gate, turn P99 > gate, turn P90 > 2× first wave |
| hold at 1,150 (10 min) | 12,214 | 0 / 0 / 0 | 2075 / 52371 | 13.9 / 15.8 / 1030.0 | — | — | 87.8 % | 0.18 / 0.0 / 0.0 | 1,156 | 0 | errors > gate, turn P99 > gate, CPU probe P99 > 1 s, turn P90 > 2× first wave |

### microVM `nanofill-metal2-psi10` — 500-agent waves every 4 min from 500, original gates

| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 500 | 500 | 0 / 0 / 0 | 6 / 21 | 17 / 20 / 30 | 0.0 | 0.0 | 76.4 % | 0.01 / 0.87 / 0.1 | 1,134 | 13 | pass |
| 1,000 | 1,271 | 608 / 712 / 781 | 6 / 11 | 15 / 20 / 31 | 0.0 | 0.0 | 68.4 % | 0.2 / 8.22 / 1 | 1,634 | 13 | pass |
| 1,500 | 3,304 | 623 / 727 / 813 | 5 / 9 | 14 / 19 / 31 | 0.0 | 0.0 | 60.3 % | 0.07 / 1.84 / 0.18 | 2,134 | 13 | pass |
| 2,000 | 7,921 | 653 / 776 / 957 | 5 / 10 | 14 / 18 / 32 | 0.0 | 0.0 | 52.1 % | 0.61 / 0.08 / 0.04 | 2,634 | 13 | pass |
| 2,500 | 13,320 | 698 / 832 / 1,008 | 5 / 11 | 14 / 21 / 38 | 0.0 | 0.0 | 43.6 % | 2.19 / 15.89 / 1.57 | 3,134 | 13 | memory PSI full > gate |
| 2,000 | 46,228 | 0 / 0 / 0 | 5 / 10 | 14 / 18 / 28 | 0.0 | 0.0 | 50.9 % | 1.42 / 0.04 / 0 | 2,634 | 13 | pass |
| hold at 2,000 (10 min) | 46,228 | 0 / 0 / 0 | 5 / 11 | 14.1 / 18.2 / 28.7 | — | — | 50.9 % | 1.42 / 0.04 / 0.0 | 2,634 | 13 | pass |

### microVM `nanofill-metal2-psi30` — 500-agent waves every 4 min from 2,000

| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 2,000 | 2,000 | 0 / 0 / 0 | 5 / 11 | 16 / 19 / 29 | 0.0 | 0.0 | 51.0 % | 0.07 / 0.89 / 0.08 | 2,634 | 13 | pass |
| 2,500 | 3,611 | 618 / 768 / 963 | 5 / 11 | 14 / 19 / 31 | 0.1 | 0.2 | 42.9 % | 0.04 / 0.24 / 0.05 | 3,133 | 14 | pass |
| 3,000 | 9,470 | 776 / 944 / 1,111 | 5 / 12 | 14 / 19 / 30 | 0.0 | 0.1 | 34.6 % | 2.27 / 4.54 / 0.21 | 3,633 | 14 | pass |
| 3,500 | 21,810 | 1,014 / 4,468 / 5,496 | 6 / 14 | 15 / 20 / 33 | 0.0 | 0.1 | 26.3 % | 2.33 / 0.14 / 0.01 | 4,132 | 15 | pass |
| 4,000 | 29,262 | 21,732 / 35,013 / 38,322 | 8 / 67 | 16 / 25 / 87 | 0.0 | 2.6 | 17.7 % | 3.01 / 0 / 0 | 4,632 | 15 | refusals > gate, wake P99 > gate |
| 3,500 | 83,289 | 31,581 / 31,723 / 31,723 | 18 / 134 | 16 / 32 / 137 | 0.0 | 0.0 | 25.0 % | 3.81 / 0 / 0 | 4,132 | 15 | wake P99 > gate, turn P90 > 2× first wave |
| hold at 3,500 (10 min) | 83,289 | 31,582 / 31,723 / 31,723 | 18 / 134 | 16.0 / 32.4 / 137.7 | — | — | 25.0 % | 3.81 / 0.0 / 0.0 | 4,132 | 15 | wake P99 > gate, turn P90 > 2× first wave |

## Data

Per run directory under `/tmp/tco-runs/fill/` (archived in `assets/runs/2026-10-10-nano-resident-fill/`): `fill.txt` (wave results, JSON), `simlog-fill.txt` (full agentsim log incl. the hold CSV and the verdict), `hostprobe-fill.log` (node meminfo / cpu / diskstats every 30 s), `run-fill.log`. Pipeline: `nanofill.sh` with the per-run launchers `nanofill_east_{d,e,f}.sh`; sysctl pods `sysctl-neigh.yaml`, `sysctl-inotify.yaml`.
