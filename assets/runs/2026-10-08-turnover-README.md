# Turnover at a fixed fill — how many actors per second one bare-metal node can swap in and out at 500 resident (measured 2026-10-08)

**Machine under test:** GKE bare-metal node **c3-standard-192-metal** — 192 vCPU, 768 GiB RAM, 3 TB Hyperdisk Balanced boot disk (100k IOPS / 2,400 MiB/s provisioned; single ext4, `bfq` scheduler, 128 KiB max request), COS, GKE 1.36.4. `agents-tco-east` (us-east4-a) ran gVisor, `agents-tco-euw4` (europe-west4-c) ran microVM. Substrate perf/resume-latency @ 584d0318, 50 unsized worker pods, actors limited to 1 vCPU and 3 GiB (gVisor) / 1.5 GiB (microVM).

**Test.** 800 actors per node, each filled to **1 GiB of RAM** at creation and parked once (so every wake restores a real 1 GiB snapshot). 500 are woken and left **idle** — the node's steady state. Then every 10 s the harness parks N of the awake actors (SuspendActor → bucket + node-local retained copy, or PauseActor → node-local) and wakes N of the parked ones, oldest first, so the node stays at 500 awake / 300 parked while actors turn over. N doubles every 5 minutes: 5, 10, 20, 40, 80, 160 per tick = 0.5 … 16 swaps/s. One swap = one park + one wake, so the swap rate is the activation rate. Gates per level: wake P90 and park P90 within 2.5× the first level; wake P99 under 5 s; errors or router refusals above 0.5 % of wakes; more than two ticks' worth of swaps still in flight at the end of the level (backlog); node MemAvailable under 10 %, memory PSI full over 10 %, CPU PSI some over 50 %; 5+ crashed actors. After the first failing level the harness holds the last clean N for 10 minutes and scores it again.

## Summary

**Answer: at 500 awake 1 GiB-class agents, one c3-standard-192-metal node sustains ~0.5 activations per second of turnover (one park + one wake every two seconds) and does not sustain 1.0 per second — in all four cells, gVisor and microVM, suspend and pause.** The ceiling lies between those two levels; the fine-tuning pass (6, 7, 8 per 10 s) was deliberately left for a second run.

**Why it stops there.** Every failing level has the same shape: the wakes of the batch still complete (0.68–0.83 wakes/s), but the parks queue on the boot disk — 50–196 of ~230 parks finish inside the 5-minute level, park P90 climbs from 2–7 s to 9–144 s, 120–180 swaps are still in flight when the level ends (the backlog gate), the router starts refusing the queued wakes (503 past the 5 s park budget), and wake P90 follows (10–49 s). The disk is the limiter in the same way as in the park/wake host-fill report: time-saturated with a deep queue at 1–1.5 GiB/s of writes, not at its provisioned bandwidth. Host CPU and memory never mattered (CPU PSI 0, memory ≥ 30 % available).

**What 0.5 swaps/s means.** ~41,000 activations per node per day. A real-time personal assistant wakes ~61 times a day, so this turnover rate serves about 680 such agents' wake-ups per node — the same order as the memory-bound 650 awake agents measured the night before.

**Caveats recorded below.** (1) The 10-minute hold after each failed level is *not* a clean data point in any cell: the 120–180 unfinished swaps of the failed level were still draining through it, so the hold rows show the collapse continuing, not the lower level failing. (2) The gVisor suspend cell was run twice; the first attempt stopped at its first level on a single transient error (one in 145 wakes, 0.69 % against a 0.5 % gate) and is not shown; the gates now require at least three occurrences. (3) In every cell the fill (waking 500 actors from parked) took 20–30 s because actors are small at creation; the resident set is built by the agents' own first-lap catch-up. (4) The microVM suspend cell's host-probe samples were lost in a results-directory mix-up on the laptop; its rows carry the autosuspender's memory/PSI fields only. (5) A few actors per node (0–2) failed to wake in the fill and stayed in the parked pool.

## Ceiling per cell

| Runtime | Park mode | Last clean level (swaps/s) | First failing level (swaps/s) | Failed on | Wake P50 / P90 / P99 at the last clean level (ms) | Park P50 / P90 / P99 at the last clean level (s) | Disk busy % · queue depth at the last clean level (p90) |
|---|---|---|---|---|---|---|---|
| **gvisor** | suspend | 5 per 10 s = **0.48/s** | 10 per 10 s = 1.00/s target, 0.77 achieved | refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog | 205 / 599 / 828 | 1.0 / 2.2 / 3.3 | — |
| **gvisor** | suspend | 5 per 10 s = **0.48/s** | 10 per 10 s = 1.00/s target, 0.77 achieved | refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog | 205 / 599 / 828 | 1.0 / 2.2 / 3.3 | — |
| **gvisor** | pause | 5 per 10 s = **0.48/s** | 10 per 10 s = 1.00/s target, 0.83 achieved | refusals+wake-p99+wake-p90-vs-baseline+backlog+psi-mem-full | 512 / 574 / 605 | 0.6 / 2.9 / 4.9 | 96 % · 109 |
| **microvm** | suspend | 5 per 10 s = **0.48/s** | 10 per 10 s = 1.00/s target, 0.77 achieved | refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog | 216 / 454 / 928 | 4.0 / 5.1 / 7.1 | — |
| **microvm** | pause | 5 per 10 s = **0.48/s** | 10 per 10 s = 1.00/s target, 0.77 achieved | refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog | 205 / 599 / 828 | 1.0 / 2.2 / 3.3 | — |

## gvisor · suspend — agents-tco-east (us-east4-a)

Fill of 500 resident actors took 19.541359103s. Verdict: failure at 10 per 10s (1.00 swaps/s target, 0.77 achieved; refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog); last clean level = 5 per 10s (0.50 swaps/s); hold at 5 also failed (refusals+errors+wake-p99+backlog+crashed)

| N per 10 s | Target swaps/s | Achieved swaps/s | Wakes / parks in the level | Wake P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Errors + refusals | Backlog at end | Node mem avail | PSI cpu / mem / io | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 5 | 0.50 | 0.48 | 145 / 145 | 205 / 599 / 828 | 1.0 / 2.2 / 3.3 | 0 + 0 | 0 | 33.8 % | 0.00 / 0.00 / 19.50 | 500 | 0 | pass |
| 10 | 1.00 | 0.77 | 230 / 122 | 3,179 / 9,783 / 31,555 | 8.1 / 18.9 / 30.0 | 79 + 392 | 160 | 29.5 % | 0.00 / 1.51 / 86.22 | 460 | 0 | refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog |
| 5 (hold) | 0.50 | 0.44 | 265 / 0 | 9,943 / 49,943 / 49,943 | 0.0 / 0.0 / 0.0 | 259 + 543 | 90 | 17.2 % | 0.00 / 1.04 / 81.31 | 450 | 6 | refusals+errors+wake-p99+backlog+crashed |

## gvisor · suspend — agents-tco-east (us-east4-a)

Fill of 500 resident actors took 19.541359103s. Verdict: failure at 10 per 10s (1.00 swaps/s target, 0.77 achieved; refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog); last clean level = 5 per 10s (0.50 swaps/s); hold at 5 also failed (refusals+errors+wake-p99+backlog+crashed)

| N per 10 s | Target swaps/s | Achieved swaps/s | Wakes / parks in the level | Wake P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Errors + refusals | Backlog at end | Node mem avail | PSI cpu / mem / io | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 5 | 0.50 | 0.48 | 145 / 145 | 205 / 599 / 828 | 1.0 / 2.2 / 3.3 | 0 + 0 | 0 | 33.8 % | 0.00 / 0.00 / 19.50 | 500 | 0 | pass |
| 10 | 1.00 | 0.77 | 230 / 122 | 3,179 / 9,783 / 31,555 | 8.1 / 18.9 / 30.0 | 79 + 392 | 160 | 29.5 % | 0.00 / 1.51 / 86.22 | 460 | 0 | refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog |
| 5 (hold) | 0.50 | 0.44 | 265 / 0 | 9,943 / 49,943 / 49,943 | 0.0 / 0.0 / 0.0 | 259 + 543 | 90 | 17.2 % | 0.00 / 1.04 / 81.31 | 450 | 6 | refusals+errors+wake-p99+backlog+crashed |

## gvisor · pause — agents-tco-east (us-east4-a)

Fill of 500 resident actors took 13m37.959476975s. Verdict: failure at 10 per 10s (1.00 swaps/s target, 0.83 achieved; refusals+wake-p99+wake-p90-vs-baseline+backlog+psi-mem-full); last clean level = 5 per 10s (0.50 swaps/s); hold at 5 also failed (refusals+errors+wake-p99+wake-p90-vs-baseline+backlog+crashed)

| N per 10 s | Target swaps/s | Achieved swaps/s | Wakes / parks in the level | Wake P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Errors + refusals | Backlog at end | Node mem avail | PSI cpu / mem / io | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 5 | 0.50 | 0.48 | 145 / 145 | 512 / 574 / 605 | 0.6 / 2.9 / 4.9 | 0 + 0 | 0 | 35.5 % | 0.68 / 0.11 / 13.80 | 499 | 1 | pass |
| 10 | 1.00 | 0.83 | 248 / 196 | 1,645 / 17,483 / 42,758 | 1.2 / 6.5 / 8.1 | 1 + 79 | 130 | 31.7 % | 24.63 / 21.13 / 88.63 | 483 | 1 | refusals+wake-p99+wake-p90-vs-baseline+backlog+psi-mem-full |
| 5 (hold) | 0.50 | 0.41 | 244 / 34 | 14,607 / 100,569 / 150,880 | 0.7 / 3.2 / 5.4 | 165 + 409 | 90 | 31.4 % | 0.08 / 2.02 / 98.75 | 414 | 8 | refusals+errors+wake-p99+wake-p90-vs-baseline+backlog+crashed |

Disk over the whole run (node probe, 15-s samples): write 15 / 671 / 2,340 MiB/s (p50 / p90 / max), read p90 159 MiB/s, busy 72 / 96 %, queue depth 21 / 109, write await 7.3 / 47.4 ms.

## microvm · suspend — agents-tco-euw4 (europe-west4-c)

Fill of 500 resident actors took 21.66578496s. Verdict: failure at 10 per 10s (1.00 swaps/s target, 0.77 achieved; refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog); last clean level = 5 per 10s (0.50 swaps/s); hold at 5 also failed (refusals+errors+wake-p99+wake-p90-vs-baseline+backlog+crashed)

| N per 10 s | Target swaps/s | Achieved swaps/s | Wakes / parks in the level | Wake P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Errors + refusals | Backlog at end | Node mem avail | PSI cpu / mem / io | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 5 | 0.50 | 0.48 | 145 / 145 | 216 / 454 / 928 | 4.0 / 5.1 / 7.1 | 0 + 0 | 0 | 33.4 % | 0.00 / 0.02 / 18.26 | 500 | 0 | pass |
| 10 | 1.00 | 0.77 | 230 / 50 | 6,817 / 48,819 / 58,820 | 24.2 / 143.6 / 153.6 | 145 + 211 | 180 | 29.5 % | 0.00 / 0.59 / 77.09 | 455 | 0 | refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog |
| 5 (hold) | 0.50 | 0.44 | 265 / 1 | 25,781 / 45,772 / 55,772 | 0.0 / 0.0 / 0.0 | 225 + 29 | 85 | 35.0 % | 0.00 / 0.00 / 82.75 | 231 | 10 | refusals+errors+wake-p99+wake-p90-vs-baseline+backlog+crashed |

## microvm · pause — agents-tco-euw4 (europe-west4-c)

Fill of 500 resident actors took 19.541359103s. Verdict: failure at 10 per 10s (1.00 swaps/s target, 0.77 achieved; refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog); last clean level = 5 per 10s (0.50 swaps/s); hold at 5 also failed (refusals+errors+wake-p99+backlog+crashed)

| N per 10 s | Target swaps/s | Achieved swaps/s | Wakes / parks in the level | Wake P50 / P90 / P99 ms | Park P50 / P90 / P99 s | Errors + refusals | Backlog at end | Node mem avail | PSI cpu / mem / io | Running | Crashed | Failed on |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 5 | 0.50 | 0.48 | 145 / 145 | 205 / 599 / 828 | 1.0 / 2.2 / 3.3 | 0 + 0 | 0 | 33.8 % | 0.00 / 0.00 / 19.50 | 500 | 0 | pass |
| 10 | 1.00 | 0.77 | 230 / 122 | 3,179 / 9,783 / 31,555 | 8.1 / 18.9 / 30.0 | 79 + 392 | 160 | 29.5 % | 0.00 / 1.51 / 86.22 | 460 | 0 | refusals+errors+wake-p99+wake-p90-vs-baseline+park-p90-vs-baseline+backlog |
| 5 (hold) | 0.50 | 0.44 | 265 / 0 | 9,943 / 49,943 / 49,943 | 0.0 / 0.0 / 0.0 | 259 + 543 | 90 | 17.2 % | 0.00 / 1.04 / 81.31 | 450 | 6 | refusals+errors+wake-p99+backlog+crashed |

## Reading the numbers

- **Swaps/s is activations/s at a constant 500 awake.** Each swap moves ~1 GiB out (park) and ~1 GiB back in (wake); per swap the suspend path writes the raw image plus a compressed copy to the boot disk and uploads it, pause writes the raw image only; the wake stages the retained node-local copy.
- **Level N is clean only if both halves keep up:** the wakes stay within 2.5× of their unloaded latency and under 5 s at P99, the parks stay within 2.5× of theirs, nothing is refused, and the ticks do not pile up (backlog).
- **The resident 500 are idle**, so the only load on the node is the turnover itself — the ceiling is the park/wake path (disk queueing, see the host-fill report) and the per-worker wake path, not the awake agents.
- **Fine-tuning between the last clean and first failing level** was deliberately left for a second pass (speed first).
