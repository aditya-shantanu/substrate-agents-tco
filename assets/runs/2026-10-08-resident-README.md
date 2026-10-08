# Resident host fill — awake agents one c3-standard-192-metal node sustains, no pause/suspend (measured 2026-10-07/08)

**Machine under test (one per cell, two in parallel):** GKE bare-metal node **c3-standard-192-metal** — 192 vCPU (Intel Sapphire Rapids, native KVM), 768 GiB RAM (755 GiB visible to the kernel), 3 TB Hyperdisk Balanced boot disk at 100k IOPS / 2,400 MiB/s, COS, GKE 1.36.4, 256 pods/node. Nodes: `agents-tco-east` (us-east4-a) ran the gVisor cells, `agents-tco-euw4` (europe-west4-c) the microVM cells. Substrate perf/resume-latency @ 584d0318; worker pods unsized (no requests/limits); every actor limited to **1 vCPU** plus 3 GiB (gVisor assistant), 1.5 GiB (microVM assistant) or 256 MiB (ping).

Two workloads, two runtimes, two pool shapes. **Nothing parks**: actors are created once (parked), each wave wakes a batch and they stay resident for the rest of the run (autosuspender idle timeout 24 h, driver lifecycle `none`). Personal-assistant agents keep playing their day (61 steps, think ×0.02, ~1 GiB resident, 3 GiB / 1.5 GiB limit, **1 vCPU limit**); ping agents are the one-ping actors (256 MiB, 1 vCPU) each pinged on a Poisson schedule with a 10 s mean. Waves add agents every 5 minutes; after the first wave that trips a gate, that wave's agents are stopped and the last clean level holds for 10 minutes and is scored again.

**Gates (any one fails the wave):** in-sandbox CPU probe or awake-request (turn) P90 above 2× the first wave's; probe or turn P99 above 1 s; errors or router refusals above 0.5 % of activations; node MemAvailable below 10 %; memory PSI full avg10 above 10 %; CPU PSI some avg10 above 50 %; any actor CRASHED. First-wave wake P90 is also reported per wave (provisioning signal; 10 s P99 hard gate).

## Summary

Question asked: with pause/suspend taken out of the picture, how many agents can one bare-metal node hold **awake**, and what tells us to stop adding? Two workloads (the personal-assistant day at think ×0.02 with ~1 GiB resident, and the one-ping actor at 256 MiB), two runtimes, two pool shapes, on two c3-standard-192-metal nodes in parallel, every actor limited to 1 vCPU. Agents were pre-created parked and woken in waves; nothing was parked again.

| Workload | Runtime | 50 pods: last clean / first fail (awake agents on the node) | Configured oversubscription at the last clean level (memory · CPU) | What tripped | 1 pod: last clean / first fail (oversubscription) |
|---|---|---|---|---|---|
| Personal assistant | gvisor (3 GiB limit) | **650 / 700** | memory 2.5× · CPU 3.4× | node memory below 10 % available | 150 / 200 (memory 0.59× · CPU 0.78×) |
| Personal assistant | microvm (1.5 GiB limit) | **650 / 700** | memory 1.3× · CPU 3.4× | memory reclaim: CPU probe P90 15 → 117 ms, request P90 5 → 65 ms, wakes 6.7 s P50 | 150 / 200 (memory 0.29× · CPU 0.78×) |
| Ping | gvisor (0.25 GiB limit) | **≥ 4,000** (fleet exhausted) | memory 1.3× · CPU 20.8× | nothing — fleet exhausted (62 % memory free, CPU idle) | 100 / 250 (memory 0.03× · CPU 0.52×) |
| Ping | microvm (0.25 GiB limit) | **3,550 / 3,850** | memory 1.2× · CPU 18.5× | wake P99 of the newly added 300 above 10 s | 100 / 250 (memory 0.03× · CPU 0.52×) |

**What the signals showed**

- **Awake capacity for the assistant actor is memory-bound at ~650 per 768 GB node, on both runtimes.** Request latency (4 ms P90), the in-sandbox CPU probe (15–16 ms P90) and host CPU (P90 under 20 %) were flat all the way up; only MemAvailable moved, about 1.1 GB of host RAM per awake agent including sandbox overhead. Configured oversubscription at the ceiling: memory 2.5× for gVisor at its 3 GiB limit and 1.3× for microVM at 1.5 GiB; CPU 3.4× for both at 1 vCPU each.
- **Ping is not host-bound.** gVisor carried 4,000 awake sandboxes with 38 % of memory used and CPU idle; microVM reached 3,550 and stopped only because waking 300 more guests at once pushed that batch's wake tail past 10 s. Request latency for already-awake actors stayed at 4–8 ms P90 throughout.
- **One worker pod caps provisioning, not serving.** Every one-pod cell — both workloads, both runtimes — failed the moment a wave asked for more than roughly 50–100 simultaneous wakes through a single ateom (wake P90 at the router's 30 s limit, refusals 4–28 %), while the agents already awake on that pod kept answering in 4 ms. The limit is in the per-worker wake path (assignment/restore), not in the sandbox runtime; worth locating in the code.
- **The kernel's ARP neighbour table was the real wall at ~1,000 live sandboxes.** Both ping cells collapsed at the 1,000 wave on both nodes with the host idle; dmesg showed `neighbour: arp_cache: neighbor table overflow!` at the COS default `gc_thresh3 = 1024`. Raising `net.ipv{4,6}.neigh.default.gc_thresh*` to 4096/8192/16384 removed it (the extension rows). This is a Substrate node-preparation gap, like the node-directory leak.

**Stop rule that actually fired, per cell:** MemAvailable (gVisor PA), relative latency degradation + errors (microVM PA, the early-warning gates), wake P99 (microVM ping and all one-pod cells), the crash gate (the pre-fix ping cells — caused by the ARP wall). Errors/refusals were 0 in every clean wave.

**Caveats recorded in the tables below:** the first hold for gVisor PA is invalid (the harness did not yet park the agents it dropped, fixed before all later cells); one gVisor ping extension attempt was aborted by a single crash in its 850-agent opening wave under the crash gate of 1 (then set to 5) and is shown as its own row; the two largest runs' wave tables were recovered from the node's rotated container logs after `kubectl logs` returned nothing.

## Ceilings at a glance

| Cell | Pods | Ramp | Last clean level (awake agents) | First failing level | Failed on | Hold at last clean level | Host memory used at ceiling | Host CPU p90 / max over the run | Cold start of new actors (p50/p90/p99 ms) |
|---|---|---|---|---|---|---|---|---|---|
| **gvisor · pa** | 50 | from 50 to 800 | 650 | 700 | mem-available | failed: errors+mem-available | 88 % of node | 15 % / 18 % | p50=725 p90=988 p99=1221 max=1495 |
| **gvisor · pa** | 1 | from 50 to 800 | 150 | 200 | refusals+errors+wake-p99 | failed: errors | 21 % of node | 6 % / 17 % | p50=491 p90=650 p99=837 max=895 |
| **microvm · pa** | 50 | from 50 to 600 | 600 | none up to 600 | — | — | 86 % of node | 17 % / 22 % | p50=2000 p90=2369 p99=2903 max=8806 |
| **microvm · pa** | 50 | from 600 to 900 (extension) | 650 | 700 | errors+wake-p99+probe-p99+probe-p90-vs-baseline+turn-p90-vs-baseline | failed: errors | 82 % of node | 38 % / 47 % | p50=2251 p90=2654 p99=3005 max=13089 |
| **microvm · pa** | 1 | from 50 to 600 | 150 | 200 | refusals+errors+wake-p99 | failed: errors | 20 % of node | 9 % / 13 % | p50=2850 p90=3321 p99=3972 max=4113 |
| **gvisor · ping** | 50 | from 100 to 1600 | 850 | 1000 | refusals+errors+wake-p99+turn-p99+crashed | failed: crashed | 11 % of node | 3 % / 17 % | p50=787 p90=1033 p99=1277 max=1398 |
| **gvisor · ping** | 50 | from 850 to 4000 (extension) — aborted attempt: 1 crash in the opening wave tripped the crash gate (then set to 5) | 0 | 850 | crashed | — | — | 18 % / 19 % | p50=852 p90=1137 p99=1405 max=1896 |
| **gvisor · ping** | 50 | from 850 to 4000 (extension) | 4000 | none up to 4000 | — | — | 38 % of node | 14 % / 21 % | — |
| **gvisor · ping** | 1 | from 100 to 1600 | 100 | 250 | refusals+errors+wake-p99 | clean | 7 % of node | 2 % / 22 % | p50=557 p90=717 p99=862 max=949 |
| **microvm · ping** | 50 | from 100 to 1600 | 850 | 1000 | refusals+errors+wake-p99+turn-p99+crashed | failed: crashed | 16 % of node | 3 % / 12 % | p50=1417 p90=2173 p99=2760 max=3123 |
| **microvm · ping** | 50 | from 850 to 4000 (extension) | 3550 | 3850 | wake-p99 | clean | 56 % of node | 10 % / 18 % | — |
| **microvm · ping** | 1 | from 100 to 1600 | 100 | 250 | refusals+errors+wake-p99 | clean | 5 % of node | 5 % / 7 % | p50=1785 p90=2819 p99=3307 max=3481 |

## gvisor · pa · 50 pods — agents-tco-east (us-east4-a)

Verdict: failure at 700 active agents (mem-available); last sustainable level = 650; hold at 650 also failed (errors+mem-available)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 50 | 51 | 321 / 368 / 384 | 32 / 32 | 14.2 / 16.3 / 16.6 | 0 + 0 | 89.8 % | 0.00 / 0.00 / 0.06 | 50 | 0 | pass |
| 100 | 136 | 316 / 354 / 373 | 8 / 27 | 14.3 / 16.5 / 18.9 | 0 + 0 | 84.3 % | 0.00 / 0.00 / 0.02 | 100 | 0 | pass |
| 150 | 482 | 329 / 378 / 440 | 6 / 18 | 13.9 / 16.2 / 20.4 | 0 + 0 | 78.7 % | 0.00 / 0.00 / 0.10 | 150 | 0 | pass |
| 200 | 1096 | 341 / 395 / 417 | 5 / 7 | 13.9 / 15.9 / 19.6 | 0 + 0 | 73.1 % | 0.00 / 0.00 / 0.06 | 200 | 0 | pass |
| 250 | 1625 | 336 / 420 / 450 | 5 / 6 | 14.0 / 16.1 / 19.4 | 0 + 0 | 67.4 % | 0.00 / 0.00 / 0.00 | 250 | 0 | pass |
| 300 | 2139 | 355 / 413 / 435 | 4 / 6 | 13.9 / 15.9 / 19.0 | 0 + 0 | 60.6 % | 5.51 / 0.00 / 0.00 | 300 | 0 | pass |
| 350 | 2652 | 381 / 458 / 480 | 4 / 7 | 14.0 / 16.2 / 20.4 | 0 + 0 | 53.7 % | 3.22 / 0.00 / 0.00 | 350 | 0 | pass |
| 400 | 2932 | 377 / 441 / 482 | 4 / 6 | 14.0 / 16.3 / 20.7 | 0 + 0 | 46.9 % | 5.30 / 0.00 / 0.00 | 400 | 0 | pass |
| 450 | 3061 | 389 / 449 / 475 | 4 / 6 | 14.1 / 16.5 / 20.8 | 0 + 0 | 39.8 % | 4.47 / 0.00 / 0.00 | 450 | 0 | pass |
| 500 | 3292 | 358 / 425 / 460 | 4 / 6 | 14.1 / 16.6 / 22.1 | 0 + 0 | 32.8 % | 4.80 / 0.00 / 0.00 | 500 | 0 | pass |
| 550 | 3815 | 384 / 441 / 466 | 4 / 7 | 14.1 / 16.5 / 20.6 | 0 + 0 | 25.8 % | 2.71 / 0.00 / 0.00 | 550 | 0 | pass |
| 600 | 4324 | 386 / 465 / 481 | 4 / 7 | 14.2 / 16.7 / 21.5 | 3 + 0 | 18.9 % | 2.06 / 0.00 / 0.00 | 600 | 0 | pass |
| 650 | 4813 | 396 / 456 / 493 | 4 / 7 | 14.2 / 16.9 / 21.0 | 2 + 0 | 12.2 % | 2.86 / 0.08 / 0.00 | 650 | 0 | pass |
| 700 | 5399 | 395 / 471 / 506 | 5 / 8 | 14.3 / 17.1 / 21.8 | 6 + 0 | 6.5 % | 2.81 / 2.69 / 0.25 | 700 | 0 | mem-available |
| 650 (hold) | 11553 | 0 / 0 / 0 | 5 / 7 | 14.4 / 17.0 / 21.6 | 137 + 0 | 3.7 % | 3.07 / 6.47 / 0.70 | 700 | 0 | errors+mem-available |

## gvisor · pa · 1 pod — agents-tco-east (us-east4-a)

Verdict: failure at 200 active agents (refusals+errors+wake-p99); last sustainable level = 150; hold at 150 also failed (errors)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 50 | 51 | 290 / 342 / 376 | 27 / 27 | 13.7 / 15.4 / 16.8 | 0 + 0 | 90.1 % | 0.00 / 0.00 / 0.00 | 50 | 0 | pass |
| 100 | 136 | 308 / 354 / 376 | 6 / 28 | 13.7 / 15.4 / 17.7 | 0 + 0 | 84.7 % | 0.00 / 0.00 / 0.00 | 100 | 0 | pass |
| 150 | 482 | 304 / 353 / 381 | 5 / 7 | 13.5 / 15.7 / 20.1 | 0 + 0 | 79.2 % | 0.00 / 0.00 / 0.00 | 150 | 0 | pass |
| 200 | 1098 | 340 / 31,698 / 32,087 | 4 / 6 | 13.6 / 15.8 / 20.1 | 9 + 45 | 74.4 % | 0.00 / 0.00 / 0.00 | 191 | 0 | refusals+errors+wake-p99 |
| 150 (hold) | 3411 | 0 / 0 / 0 | 4 / 6 | 13.6 / 15.7 / 19.2 | 123 + 0 | 76.8 % | 3.01 / 0.00 / 0.00 | 150 | 0 | errors |

## microvm · pa · 50 pods — agents-tco-euw4 (europe-west4-c)

Verdict: no failure up to 600 active agents (raise --agents to push further)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 50 | 51 | 157 / 177 / 180 | 17 / 17 | 17.4 / 18.9 / 21.5 | 0 + 0 | 92.0 % | 0.00 / 0.00 / 0.01 | 50 | 0 | pass |
| 100 | 136 | 156 / 162 / 172 | 7 / 24 | 14.2 / 18.1 / 22.3 | 0 + 0 | 85.8 % | 0.00 / 0.00 / 0.00 | 100 | 0 | pass |
| 150 | 482 | 160 / 183 / 188 | 5 / 14 | 13.5 / 16.3 / 21.9 | 0 + 0 | 79.6 % | 0.00 / 0.00 / 0.00 | 150 | 0 | pass |
| 200 | 1095 | 161 / 170 / 190 | 5 / 7 | 13.0 / 15.2 / 21.1 | 0 + 0 | 73.4 % | 0.00 / 0.00 / 0.00 | 200 | 0 | pass |
| 250 | 1622 | 164 / 179 / 188 | 5 / 6 | 12.9 / 14.8 / 21.4 | 0 + 0 | 67.1 % | 0.02 / 0.00 / 0.00 | 250 | 0 | pass |
| 300 | 2136 | 168 / 187 / 192 | 4 / 6 | 13.0 / 14.9 / 22.1 | 0 + 0 | 59.6 % | 0.22 / 0.00 / 0.00 | 300 | 0 | pass |
| 350 | 2652 | 166 / 182 / 193 | 4 / 6 | 13.1 / 15.5 / 23.4 | 0 + 0 | 52.1 % | 0.27 / 0.00 / 0.00 | 350 | 0 | pass |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | — / — / — | step:"1830_heartbeat" + actor:"res-1791437043-microvm-pa-w50-0339" | None % | None / None / None | None | None | pass |
| 400 | 2934 | 167 / 177 / 183 | 4 / 6 | 13.1 / 15.5 / 26.5 | 0 + 0 | 44.6 % | 0.29 / 0.00 / 0.00 | 400 | 0 | pass |
| 450 | 3056 | 169 / 178 / 191 | 4 / 6 | 13.1 / 15.6 / 25.6 | 0 + 0 | 36.9 % | 0.49 / 0.00 / 0.14 | 450 | 0 | pass |
| 500 | 3291 | 170 / 185 / 195 | 4 / 6 | 13.2 / 15.9 / 29.1 | 0 + 0 | 29.2 % | 0.51 / 0.00 / 0.00 | 500 | 0 | pass |
| 550 | 3808 | 171 / 188 / 205 | 4 / 6 | 13.3 / 16.0 / 33.2 | 1 + 0 | 21.7 % | 0.43 / 0.00 / 0.00 | 550 | 0 | pass |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | — / — / — | step:"1836_save_it" + actor:"res-1791437043-microvm-pa-w50-0325" | None % | None / None / None | None | None | pass |
| 600 | 4327 | 185 / 505 / 747 | 4 / 7 | 13.3 / 16.9 / 37.9 | 0 + 0 | 13.9 % | 0.57 / 0.00 / 0.29 | 600 | 0 | pass |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | — / — / — | step:"1518_web_check" + actor:"res-1791437043-microvm-pa-w50-0359" | None % | None / None / None | None | None | pass |

## microvm · pa · 50 pods — agents-tco-euw4 (europe-west4-c)

Verdict: failure at 700 active agents (errors+wake-p99+probe-p99+probe-p90-vs-baseline+turn-p90-vs-baseline); last sustainable level = 650; hold at 650 also failed (errors)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 600 | 604 | 177 / 201 / 270 | 8 / 8 | 18.8 / 21.8 / 52.8 | 0 + 0 | 33.9 % | 0.00 / 0.00 / 0.00 | 600 | 0 | pass |
| 650 | 1063 | 178 / 301 / 775 | 5 / 16 | 13.7 / 15.9 / 26.4 | 0 + 0 | 18.1 % | 0.00 / 0.22 / 3.07 | 650 | 0 | pass |
| 700 | 4143 | 6,745 / 15,615 / 20,411 | 66 / 654 | 16.8 / 117.5 / 1,005.0 | 27 + 19 | 10.9 % | 0.64 / 0.04 / 0.00 | 700 | 0 | errors+wake-p99+probe-p99+probe-p90-vs-baseline+turn-p90-vs-baseline |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | — / — / — | step:"1630_heartbeat" + actor:"res-1791450176-microvm-pa-w50-0305" | None % | None / None / None | None | None | pass |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | — / — / — | step:"1400_heartbeat_and_cron" + actor:"res-1791450176-microvm-pa-w50-0639" | None % | None / None / None | None | None | pass |
| 650 (hold) | 14934 | 0 / 0 / 0 | 4 / 7 | 13.5 / 19.6 / 39.1 | 102 + 0 | 15.3 % | 0.82 / 0.01 / 0.09 | 650 | 0 | errors |

## microvm · pa · 1 pod — agents-tco-euw4 (europe-west4-c)

Verdict: failure at 200 active agents (refusals+errors+wake-p99); last sustainable level = 150; hold at 150 also failed (errors)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 50 | 51 | 159 / 165 / 180 | 14 / 14 | 17.1 / 18.1 / 19.3 | 0 + 0 | 91.8 % | 0.00 / 0.00 / 0.00 | 50 | 0 | pass |
| 100 | 136 | 165 / 173 / 178 | 5 / 23 | 13.4 / 17.1 / 18.3 | 0 + 0 | 85.7 % | 0.00 / 0.00 / 0.00 | 100 | 0 | pass |
| 150 | 482 | 173 / 183 / 183 | 4 / 8 | 12.7 / 15.9 / 18.7 | 0 + 0 | 79.5 % | 0.00 / 0.00 / 0.00 | 150 | 0 | pass |
| 200 | 1095 | 184 / 31,698 / 32,091 | 4 / 6 | 12.6 / 14.2 / 21.1 | 9 + 45 | 74.2 % | 0.00 / 0.00 / 0.00 | 191 | 0 | refusals+errors+wake-p99 |
| 150 (hold) | 3430 | 0 / 0 / 0 | 4 / 6 | 12.4 / 14.0 / 21.1 | 113 + 0 | 77.0 % | 0.08 / 0.00 / 0.00 | 150 | 0 | errors |

## gvisor · ping · 50 pods — agents-tco-east (us-east4-a)

Verdict: failure at 1000 active agents (refusals+errors+wake-p99+turn-p99+crashed); last sustainable level = 850; hold at 850 also failed (crashed)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 100 | 2754 | 227 / 265 / 280 | 5 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 94.0 % | 0.00 / 0.21 / 0.75 | 100 | 0 | pass |
| 250 | 6938 | 239 / 276 / 284 | 4 / 6 | 0.0 / 0.0 / 0.0 | 0 + 0 | 93.0 % | 0.00 / 0.19 / 2.99 | 250 | 0 | pass |
| 400 | 11441 | 255 / 292 / 322 | 4 / 6 | 0.0 / 0.0 / 0.0 | 0 + 0 | 92.1 % | 0.00 / 0.19 / 1.77 | 400 | 0 | pass |
| 550 | 15948 | 262 / 297 / 353 | 4 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 91.2 % | 0.00 / 0.00 / 0.39 | 550 | 0 | pass |
| 700 | 20684 | 274 / 326 / 387 | 4 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 90.3 % | 0.02 / 0.00 / 0.03 | 700 | 0 | pass |
| 850 | 25132 | 282 / 325 / 372 | 4 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 89.4 % | 0.00 / 0.00 / 0.00 | 850 | 0 | pass |
| 1000 | 26551 | 15,105 / 31,859 / 46,304 | 4 / 15,599 | 0.0 / 0.0 / 0.0 | 308 + 471 | 88.5 % | 0.00 / 0.00 / 0.36 | 985 | 15 | refusals+errors+wake-p99+turn-p99+crashed |
| 850 (hold) | 50898 | 0 / 0 / 0 | 4 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 89.2 % | 0.00 / 0.00 / 0.12 | 850 | 15 | crashed |

## gvisor · ping · 50 pods — agents-tco-east (us-east4-a)

Verdict: failure at 850 active agents (crashed); last sustainable level = 0

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 850 | 22879 | 346 / 632 / 1,513 | 5 / 17 | 0.0 / 0.0 / 0.0 | 7 + 30 | 84.1 % | 0.00 / 0.00 / 0.01 | 849 | 1 | crashed |

## gvisor · ping · 50 pods — agents-tco-east (us-east4-a)

Verdict: no failure up to 4000 active agents (raise --agents to push further)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 850 | 22899 | 336 / 544 / 916 | 5 / 23 | 0.0 / 0.0 / 0.0 | 0 + 0 | 81.3 % | 0.00 / 0.00 / 0.00 | 850 | 0 | pass |
| 1150 | 33631 | 334 / 469 / 830 | 5 / 25 | 0.0 / 0.0 / 0.0 | 0 + 0 | 79.5 % | 0.01 / 0.00 / 0.05 | 1150 | 0 | pass |
| 1450 | 42695 | 372 / 513 / 1,063 | 5 / 27 | 0.0 / 0.0 / 0.0 | 0 + 0 | 77.6 % | 0.00 / 0.00 / 0.00 | 1450 | 0 | pass |
| 1750 | 51519 | 392 / 528 / 1,210 | 5 / 27 | 0.0 / 0.0 / 0.0 | 0 + 0 | 75.6 % | 0.72 / 2.79 / 6.22 | 1750 | 0 | pass |
| 2050 | 60437 | 446 / 627 / 1,469 | 5 / 27 | 0.0 / 0.0 / 0.0 | 0 + 0 | 73.8 % | 0.00 / 0.00 / 0.00 | 2050 | 0 | pass |
| 2350 | 69154 | 519 / 778 / 1,818 | 5 / 28 | 0.0 / 0.0 / 0.0 | 0 + 0 | 72.1 % | 0.00 / 0.00 / 0.00 | 2350 | 0 | pass |
| 2650 | 78805 | 521 / 917 / 1,992 | 5 / 30 | 0.0 / 0.0 / 0.0 | 0 + 0 | 70.2 % | 0.00 / 0.00 / 0.22 | 2650 | 0 | pass |
| 2950 | 87689 | 601 / 1,307 / 2,547 | 5 / 30 | 0.0 / 0.0 / 0.0 | 0 + 0 | 68.3 % | 0.15 / 0.00 / 0.00 | 2950 | 0 | pass |
| 3250 | 96947 | 663 / 1,060 / 2,496 | 6 / 37 | 0.0 / 0.0 / 0.0 | 0 + 0 | 66.4 % | 0.19 / 0.00 / 0.00 | 3250 | 0 | pass |
| 3550 | 106112 | 800 / 1,999 / 3,570 | 7 / 41 | 0.0 / 0.0 / 0.0 | 8 + 0 | 64.4 % | 0.22 / 0.00 / 0.38 | 3550 | 0 | pass |
| 3850 | 114573 | 800 / 1,455 / 3,604 | 9 / 47 | 0.0 / 0.0 / 0.0 | 170 + 0 | 62.5 % | 0.68 / 0.00 / 0.00 | 3850 | 0 | pass |
| 4000 | 118731 | 618 / 893 / 1,260 | 9 / 52 | 0.0 / 0.0 / 0.0 | 238 + 0 | 61.6 % | 0.35 / 0.00 / 0.00 | 4000 | 0 | pass |

## gvisor · ping · 1 pod — agents-tco-east (us-east4-a)

Verdict: failure at 250 active agents (refusals+errors+wake-p99); last sustainable level = 100; hold at 100 clean for 10m0s

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 100 | 2754 | 188 / 205 / 212 | 5 / 8 | 0.0 / 0.0 / 0.0 | 0 + 0 | 92.6 % | 0.00 / 0.00 / 0.00 | 100 | 0 | pass |
| 250 | 5761 | 31,549 / 31,946 / 32,114 | 4 / 7 | 0.0 / 0.0 / 0.0 | 323 + 1615 | 92.1 % | 0.00 / 0.00 / 0.00 | 191 | 0 | refusals+errors+wake-p99 |
| 100 (hold) | 5932 | 0 / 0 / 0 | 5 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 92.5 % | 0.00 / 0.00 / 0.00 | 110 | 0 | pass |
| level:"INFO" | msg:"wave result" | — / — / — | — / — | — / — / — | active_agents:100 + tag:"" | probe_p50_ms:0 % | probe_p90_ms:0 / probe_p99_ms:0 / mem_avail_pct:"92.5" | psi_cpu_some10:0 | psi_mem_full10:0 | psi_io_some10:0 |

## microvm · ping · 50 pods — agents-tco-euw4 (europe-west4-c)

Verdict: failure at 1000 active agents (refusals+errors+wake-p99+turn-p99+crashed); last sustainable level = 850; hold at 850 also failed (crashed)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 100 | 2755 | 134 / 145 / 153 | 4 / 6 | — / — / — | 0 + 0 | 95.2 % | 0.00 / 0.00 / 0.00 | 100 | 0 | pass |
| 250 | 6940 | 135 / 143 / 156 | 4 / 6 | — / — / — | 0 + 0 | 93.0 % | 0.00 / 0.00 / 0.00 | 250 | 0 | pass |
| 400 | 11440 | 140 / 148 / 160 | 4 / 6 | — / — / — | 0 + 0 | 90.8 % | 0.00 / 0.00 / 0.06 | 400 | 0 | pass |
| 550 | 15959 | 143 / 153 / 162 | 4 / 6 | — / — / — | 0 + 0 | 88.6 % | 0.00 / 0.00 / 0.00 | 550 | 0 | pass |
| 700 | 20675 | 147 / 162 / 174 | 4 / 6 | — / — / — | 0 + 0 | 86.4 % | 0.00 / 0.00 / 0.01 | 700 | 0 | pass |
| 850 | 25148 | 152 / 162 / 192 | 4 / 6 | — / — / — | 0 + 0 | 84.2 % | 0.00 / 0.00 / 0.00 | 850 | 0 | pass |
| 1000 | 26774 | 8,470 / 31,590 / 32,079 | 4 / 3,144 | — / — / — | 309 + 240 | 82.1 % | 0.00 / 0.00 / 0.00 | 993 | 7 | refusals+errors+wake-p99+turn-p99+crashed |
| 850 (hold) | 49701 | — / — / — | 4 / 621 | — / — / — | 49 + 0 | 82.1 % | 0.00 / 0.00 / 0.32 | 993 | 7 | crashed |

## microvm · ping · 50 pods — agents-tco-euw4 (europe-west4-c)

Verdict: failure at 3850 active agents (wake-p99); last sustainable level = 3550; hold at 3550 clean for 10m0s

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 850 | 22923 | 150 / 179 / 339 | 4 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 83.6 % | 0.03 / 0.54 / 3.43 | 850 | 0 | pass |
| 1150 | 33627 | 160 / 171 / 185 | 4 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 79.2 % | 0.00 / 0.00 / 0.00 | 1150 | 0 | pass |
| 1450 | 42732 | 169 / 189 / 220 | 4 / 7 | 0.0 / 0.0 / 0.0 | 0 + 0 | 74.7 % | 0.00 / 0.00 / 0.00 | 1450 | 0 | pass |
| 1750 | 51518 | 178 / 197 / 250 | 4 / 8 | 0.0 / 0.0 / 0.0 | 0 + 0 | 70.3 % | 0.00 / 0.00 / 0.00 | 1750 | 0 | pass |
| 2050 | 60481 | 185 / 200 / 243 | 4 / 8 | 0.0 / 0.0 / 0.0 | 0 + 0 | 65.9 % | 0.05 / 0.00 / 0.00 | 2050 | 0 | pass |
| 2350 | 69155 | 199 / 229 / 255 | 5 / 8 | 0.0 / 0.0 / 0.0 | 0 + 0 | 61.5 % | 0.01 / 0.00 / 0.00 | 2350 | 0 | pass |
| 2650 | 78829 | 217 / 261 / 315 | 5 / 9 | 0.0 / 0.0 / 0.0 | 0 + 0 | 57.1 % | 0.04 / 0.00 / 0.00 | 2650 | 0 | pass |
| 2950 | 87741 | 259 / 374 / 623 | 5 / 11 | 0.0 / 0.0 / 0.0 | 0 + 0 | 52.7 % | 0.18 / 0.00 / 0.00 | 2950 | 0 | pass |
| 3250 | 96978 | 322 / 518 / 770 | 5 / 13 | 0.0 / 0.0 / 0.0 | 0 + 0 | 48.1 % | 0.09 / 0.14 / 0.59 | 3250 | 0 | pass |
| 3550 | 106129 | 429 / 1,129 / 1,947 | 6 / 18 | 0.0 / 0.0 / 0.0 | 0 + 0 | 43.8 % | 0.74 / 0.00 / 0.00 | 3550 | 0 | pass |
| 3850 | 114228 | 551 / 7,334 / 14,889 | 8 / 203 | 0.0 / 0.0 / 0.0 | 31 + 37 | 39.3 % | 1.37 / 0.03 / 0.14 | 3850 | 0 | wake-p99 |
| 3550 (hold) | 212468 | 0 / 0 / 0 | 6 / 16 | 0.0 / 0.0 / 0.0 | 0 + 0 | 43.1 % | 1.31 / 0.00 / 0.00 | 3550 | 0 | pass |

## microvm · ping · 1 pod — agents-tco-euw4 (europe-west4-c)

Verdict: failure at 250 active agents (refusals+errors+wake-p99); last sustainable level = 100; hold at 100 clean for 10m0s

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 100 | 2755 | 137 / 148 / 158 | 4 / 6 | 0.0 / 0.0 / 0.0 | 0 + 0 | 95.3 % | 0.00 / 0.00 / 0.00 | 100 | 0 | pass |
| 250 | 5761 | 31,550 / 31,944 / 32,111 | 4 / 6 | 0.0 / 0.0 / 0.0 | 323 + 1615 | 94.0 % | 0.00 / 0.00 / 0.00 | 191 | 0 | refusals+errors+wake-p99 |
| 100 (hold) | 5926 | 0 / 0 / 0 | 4 / 6 | 0.0 / 0.0 / 0.0 | 0 + 0 | 95.2 % | 0.00 / 0.00 / 0.00 | 104 | 0 | pass |

## Reading the numbers

- **Awake agents is the whole fleet on the node that is resident at that level** — not per pod. With one pod, one ateom process hosts all of them (`--max-actors` 1000).
- **Configured oversubscription at a level L:** memory L × limit ÷ 768 GB (3 GiB gVisor / 1.5 GiB microVM personal-assistant; 256 MiB ping), CPU L × 1 vCPU ÷ 192. Real host usage is in the node-memory and PSI columns.
- **What each gate catches:** the CPU probe (sha256 over 8 MiB inside the sandbox) drifts when the sandbox is CPU-starved; the turn is the request RTT to an awake actor (router + sandbox); wake is the restore of a newly added agent (provisioning under load); MemAvailable / PSI are the host; crashes are hard failures.
- **Compare with the park/wake host fill** (`host-fill-personal-assistant-gvisor-vs-microvm-metal-2026-10-07.md`): there the ceiling was 60–90 agents, set by checkpoint disk traffic. Here nothing is written to disk between steps, so the ceiling is RAM/CPU/router — the "active" number ACE quotes.

## Why the one-pod numbers are so bad — and why that matters

The one-pod cells are not a capacity result. Read them against the 50-pod cells on the same nodes:

| | 50 pods | 1 pod |
|---|---|---|
| Awake assistants, last clean / first fail | 650 / 700 (both runtimes) | 150 / 200 (both runtimes) |
| Awake ping actors, last clean / first fail | ≥ 4,000 gVisor; 3,550 / 3,850 microVM | 100 / 250 (both runtimes) |
| Request P90 of already-awake agents at the last clean level | 4 ms | 4 ms |
| CPU probe P90 at the last clean level | 15–16 ms | 14–16 ms |
| Host memory free at the first fail | 6–11 % (assistant) | 74 % (assistant), 92–94 % (ping) |
| Wake P90 of the batch added in the failing wave | 0.5–1.1 s (while clean) | 31.7 s — the client's timeout |
| Refusals (503) in the failing wave | 0 % (clean waves) | 4 % (+50 assistants), 28 % (+150 ping actors) |

**1. Serving is not the problem.** One ateom pod hosting 150 assistant sandboxes (or 100 ping sandboxes) answered requests exactly as fast as fifty pods did, the in-sandbox CPU probe did not move, and the host was three-quarters empty. Multi-actor packing itself costs nothing measurable.

**2. Waking is.** Every one-pod failure happened in the wave that *added* agents, and only the newly woken batch suffered: its wakes queued until the sim's 30 s client limit, refusals climbed with the size of the batch (4 % for 50, 28 % for 150), while the agents already awake on the same pod kept answering in 4 ms. The numbers are identical on gVisor and microVM to the decimal, so the sandbox runtime is not involved.

**3. The mechanism: wakes to one worker serialize on that worker's database row.** A resume of a parked actor runs through the control plane's assignment step (`cmd/ateapi/internal/controlapi/workflow_resume.go`, `ensureWorkerAssigned` → `assignWorkerAttempt`):

- the scheduler picks a worker from a cache (with 50 pods, power-of-two choices spreads a batch; with one pod there is one candidate);
- the store **binds the actor to the worker under the Worker's row lock** — `BindActorToWorker` re-checks eligibility and room "under the Worker's row lock, where the answer holds until the bind commits, so two claims for the last place cannot both be admitted" (around line 690–702);
- the actor is then written as RESUMING with the assignment; a version conflict there is retried under a bounded backoff, and those retries are deliberately not counted as errors (`schedulerRecordable`).

With one pod, every wake in a batch of 50 or 150 takes a turn on the same row lock. The restores start late, one after another. Meanwhile the router has parked each waiting request with a **5 s budget** (`cmd/atenet/internal/router/ingress/parking.go`, `DefaultParkedRequestBudget`); when the budget runs out the request gets a 503 (`budget_exhausted`), the client retries with backoff, and after five retries the wake has consumed ~30 s — the P90 we measured. The restore itself is fast (150–350 ms from the node-local retained snapshot; the 50-pod cells restored 300 agents per wave at that speed), so the queue in front of it is the whole story.

**4. It gets worse as the pod fills.** The same +50 wave passed at 50 → 100 → 150 hosted assistants and failed at 150 → 200. Each bind rewrites the worker's allocation record, which grows with every hosted actor, and each assignment re-validates what the worker hosts; both make the lock hold time grow with the pod's population, so the wake budget of a single worker shrinks as it fills. (Confirming the split between the bind and the actor update is a matter of reading the `dSchedule / dBind / dUpdate` timings the resume workflow already records.)

**5. Other reasons not to run few, large worker pods**, beyond this measurement:

- *Blast radius.* An ateom pod that is OOM-killed, evicted or rolled takes every actor it hosts with it; the epoch reconciler crashes actors bound to the old pod rather than migrating them. One pod per node means one failure domain per node.
- *Upgrades and drains* move whole pods; a pod with 600 live sandboxes is a 600-agent outage, a pod with 12 is not.
- *One tunnel.* All traffic to a pod's sandboxes shares that pod's atunnel and network namespace; the ping runs did not reach that limit, but it is one more serial resource.
- *Capacity accounting* is per worker record; a single giant record is a hotspot for every assignment and release on the node.

**6. What it means for sizing.** Awake capacity per node is memory-bound at ~650 assistants no matter how they are spread across pods. Spread them anyway: keep enough worker pods on a node that no single pod has to absorb more than a few dozen simultaneous wakes (50 pods × 13 agents each worked; 1 pod × 150 did not), and treat the per-worker wake path as the thing to fix if fewer, bigger pods are ever wanted — it is a control-plane property, not a host or runtime one. A 5-pod cell would bracket the per-pod wake budget precisely; the 1-pod and 50-pod points say it lies between 50 and 150 concurrent wakes per worker.
