# Resident host fill — awake agents one c3-standard-192-metal node sustains, no pause/suspend (measured 2026-10-07/08)

Two workloads, two runtimes, two pool shapes. **Nothing parks**: actors are created once (parked), each wave wakes a batch and they stay resident for the rest of the run (autosuspender idle timeout 24 h, driver lifecycle `none`). Personal-assistant agents keep playing their day (61 steps, think ×0.02, ~1 GiB resident, 3 GiB / 1.5 GiB limit, **1 vCPU limit**); ping agents are the one-ping actors (256 MiB, 1 vCPU) each pinged on a Poisson schedule with a 10 s mean. Waves add agents every 5 minutes; after the first wave that trips a gate, that wave's agents are stopped and the last clean level holds for 10 minutes and is scored again.

**Gates (any one fails the wave):** in-sandbox CPU probe or awake-request (turn) P90 above 2× the first wave's; probe or turn P99 above 1 s; errors or router refusals above 0.5 % of activations; node MemAvailable below 10 %; memory PSI full avg10 above 10 %; CPU PSI some avg10 above 50 %; any actor CRASHED. First-wave wake P90 is also reported per wave (provisioning signal; 10 s P99 hard gate).

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
| **microvm · ping** | 50 | from 0 to 4000 | 0 | none up to 4000 | — | — | — | 10 % / 18 % | — |
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
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | None / None / None | step:"1830_heartbeat" + actor:"res-1791437043-microvm-pa-w50-0339" | None % | None / None / None | None | None | pass |
| 400 | 2934 | 167 / 177 / 183 | 4 / 6 | 13.1 / 15.5 / 26.5 | 0 + 0 | 44.6 % | 0.29 / 0.00 / 0.00 | 400 | 0 | pass |
| 450 | 3056 | 169 / 178 / 191 | 4 / 6 | 13.1 / 15.6 / 25.6 | 0 + 0 | 36.9 % | 0.49 / 0.00 / 0.14 | 450 | 0 | pass |
| 500 | 3291 | 170 / 185 / 195 | 4 / 6 | 13.2 / 15.9 / 29.1 | 0 + 0 | 29.2 % | 0.51 / 0.00 / 0.00 | 500 | 0 | pass |
| 550 | 3808 | 171 / 188 / 205 | 4 / 6 | 13.3 / 16.0 / 33.2 | 1 + 0 | 21.7 % | 0.43 / 0.00 / 0.00 | 550 | 0 | pass |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | None / None / None | step:"1836_save_it" + actor:"res-1791437043-microvm-pa-w50-0325" | None % | None / None / None | None | None | pass |
| 600 | 4327 | 185 / 505 / 747 | 4 / 7 | 13.3 / 16.9 / 37.9 | 0 + 0 | 13.9 % | 0.57 / 0.00 / 0.29 | 600 | 0 | pass |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | None / None / None | step:"1518_web_check" + actor:"res-1791437043-microvm-pa-w50-0359" | None % | None / None / None | None | None | pass |

## microvm · pa · 50 pods — agents-tco-euw4 (europe-west4-c)

Verdict: failure at 700 active agents (errors+wake-p99+probe-p99+probe-p90-vs-baseline+turn-p90-vs-baseline); last sustainable level = 650; hold at 650 also failed (errors)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 600 | 604 | 177 / 201 / 270 | 8 / 8 | 18.8 / 21.8 / 52.8 | 0 + 0 | 33.9 % | 0.00 / 0.00 / 0.00 | 600 | 0 | pass |
| 650 | 1063 | 178 / 301 / 775 | 5 / 16 | 13.7 / 15.9 / 26.4 | 0 + 0 | 18.1 % | 0.00 / 0.22 / 3.07 | 650 | 0 | pass |
| 700 | 4143 | 6,745 / 15,615 / 20,411 | 66 / 654 | 16.8 / 117.5 / 1005.0 | 27 + 19 | 10.9 % | 0.64 / 0.04 / 0.00 | 700 | 0 | errors+wake-p99+probe-p99+probe-p90-vs-baseline+turn-p90-vs-baseline |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | None / None / None | step:"1630_heartbeat" + actor:"res-1791450176-microvm-pa-w50-0305" | None % | None / None / None | None | None | pass |
| level:"WARN" | msg:"step op failed" | — / — / — | — / — | None / None / None | step:"1400_heartbeat_and_cron" + actor:"res-1791450176-microvm-pa-w50-0639" | None % | None / None / None | None | None | pass |
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
| level:"INFO" | msg:"wave result" | — / — / — | — / — | wake_p99_ms:0 / turn_p90_ms:4 / turn_p99_ms:7 | active_agents:100 + tag:"" | probe_p50_ms:0 % | probe_p90_ms:0 / probe_p99_ms:0 / mem_avail_pct:"92.5" | psi_cpu_some10:0 | psi_mem_full10:0 | psi_io_some10:0 |

## microvm · ping · 50 pods — agents-tco-euw4 (europe-west4-c)

Verdict: failure at 1000 active agents (refusals+errors+wake-p99+turn-p99+crashed); last sustainable level = 850; hold at 850 also failed (crashed)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|
| 100 | 2755 | 134 / 145 / 153 | 4 / 6 | NaN / NaN / NaN | 0 + 0 | 95.2 % | 0.00 / 0.00 / 0.00 | 100 | 0 | pass |
| 250 | 6940 | 135 / 143 / 156 | 4 / 6 | NaN / NaN / NaN | 0 + 0 | 93.0 % | 0.00 / 0.00 / 0.00 | 250 | 0 | pass |
| 400 | 11440 | 140 / 148 / 160 | 4 / 6 | NaN / NaN / NaN | 0 + 0 | 90.8 % | 0.00 / 0.00 / 0.06 | 400 | 0 | pass |
| 550 | 15959 | 143 / 153 / 162 | 4 / 6 | NaN / NaN / NaN | 0 + 0 | 88.6 % | 0.00 / 0.00 / 0.00 | 550 | 0 | pass |
| 700 | 20675 | 147 / 162 / 174 | 4 / 6 | NaN / NaN / NaN | 0 + 0 | 86.4 % | 0.00 / 0.00 / 0.01 | 700 | 0 | pass |
| 850 | 25148 | 152 / 162 / 192 | 4 / 6 | NaN / NaN / NaN | 0 + 0 | 84.2 % | 0.00 / 0.00 / 0.00 | 850 | 0 | pass |
| 1000 | 26774 | 8,470 / 31,590 / 32,079 | 4 / 3,144 | NaN / NaN / NaN | 309 + 240 | 82.1 % | 0.00 / 0.00 / 0.00 | 993 | 7 | refusals+errors+wake-p99+turn-p99+crashed |
| 850 (hold) | 49701 | nan / nan / nan | 4 / 621 | NaN / NaN / NaN | 49 + 0 | 82.1 % | 0.00 / 0.00 / 0.32 | 993 | 7 | crashed |

## microvm · ping · 50 pods — agents-tco-euw4 (europe-west4-c)

Verdict: no verdict (run did not finish)

| Awake agents | Activations in window | Wake P50 / P90 / P99 ms (newly woken) | Turn P90 / P99 ms (awake request) | CPU probe P50 / P90 / P99 ms | Err + refusals | Node mem avail | PSI cpu some / mem full / io some | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|

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
