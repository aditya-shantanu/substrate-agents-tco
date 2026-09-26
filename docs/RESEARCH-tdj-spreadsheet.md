# Assessment: "GKE Agent Sandbox cost savings v2" spreadsheet vs this repo

Reviewed 2026-09-25 (sheet: "Updated TdJ Slide Content" + "assumptions 2026
q2 and q3"). Verdict up front: **the two models agree remarkably well where
they overlap, ours is broader and empirically grounded, but the sheet does
three things genuinely better that we should adopt** (per-phase CPU weights
with restore as the peak, a "herd %" treatment of correlated wake-ups, and a
CPU-packed density mode that prices the multi-actor future).

## What the sheet computes

Headline table: cost/agent/month = machine $/mo (3y CUD) ÷ sandboxes per
machine, for seven stacks:

| Stack | Machine | Sandboxes | $/agent/mo |
|---|---|---|---|
| Dedicated VM | n2-highcpu-2 | 1 | $23.55 |
| CHV (self-managed) | n2-standard-64 | 82 | $12.45 |
| GKE microVM | n2-standard-64 | 84 | $12.15 |
| gVisor | n2-standard-64 | 119 | $8.58 |
| Substrate gVisor | n2-standard-48 | 88 | $8.70 |
| gVisor + suspend/resume | 2× n2-standard-48 | 286 | $5.35 |
| **Substrate gVisor + S/R** | 2× n2-standard-48 | **616** | **$2.49** |

Assumptions model behind the S/R rows (per 1800s work cycle): active 180s @
0.25 vCPU, suspend 3–5s @ 0.3 vCPU, **restore 5–6s @ 1.22 vCPU**; non-idle
10.4–10.6%. Density = available vCPU ÷ *weighted vCPU per workload*, where
weighted vCPU = herd% × peak_cpu + (1−herd%) × avg_nonidle_cpu × nonidle%,
with **herd% = 25%** (fraction of the fleet assumed simultaneously in its
most expensive phase). 47 avail vCPU ÷ 0.327 = 143 workloads/host → $5.35.

## Cross-check: the models agree

Their gVisor+S/R model: $5.35/agent at 10.4% duty. Our measured run: $1.21
at 2.36% duty. Scale theirs by duty ratio: **$5.35 × (2.36/10.44) = $1.21.**
Dead match. Two independently built models (theirs CPU-packing, ours
time-slot packing) land on the same $/duty curve — strong validation of both.

## Where the sheet is better (adopt these)

1. **Per-phase CPU weights, restore is the peak.** They charge each phase
   its measured CPU: active 0.25, suspend 0.3, **restore 1.22 vCPU** (zstd
   decompress + page-in is the most CPU-intensive thing an agent does). We
   model suspend/resume only as occupied *time* on a fixed slot. Their
   framing explains our own observations (churn saturating nodes) and gives
   the churn cap a physical basis: concurrent restores × 1.22 vCPU is the
   real node budget.
2. **Herd % instead of a bare peak multiplier.** Their capacity charge
   blends "herd% of the fleet simultaneously at peak CPU" with the rest at
   average — physically interpretable (aligned crons = herd), conservative,
   and it naturally prices correlated wake-ups against the *restore* peak.
   Our single peak-to-average ratio P is blunter. Worth offering herd% as an
   alternative peak model in the calculator.
3. **CPU-packed density (the multi-actor future).** Their density has no
   concept of one-actor-per-worker slots: workloads pack by fungible vCPU.
   That's wrong for Substrate *today* (slots are real) but it's exactly the
   multi-actor-workers upside — and it quantifies it: their 616 sandboxes
   per 96 vCPU vs slot math. Adding a "CPU-packed (multi-actor) density"
   line to the calculator prices the platform's biggest roadmap lever.

Their observed per-host densities (82 CHV / 84 GKE-microVM / 119 gVisor /
88, 286, 616 for the Substrate variants) are also good reference anchors.

## Where our model is better (keep these)

1. **Memory is a dimension.** Their density is CPU-only: 616 sandboxes on
   384 GB = 0.62 GB each — personal agents (0.3–1.5 GB RSS) would blow that
   long before CPU. We cap by both CPU and memory.
2. **Non-compute costs exist.** They price compute ÷ density only; we carry
   snapshot storage, GCS ops per cycle (~$0.05/agent-mo — visible at scale),
   cluster fee and control-plane amortization.
3. **No utilization headroom** — they pack to 100% of available vCPU
   (herd% partially substitutes). We keep explicit utilization U for
   resume-latency SLOs.
4. **Workload parameterization**: their cycle is fixed (one suspend/restore
   per 30 min); ours derives churn from sessions/wakes per day, exposes the
   idle-wait policy, the switching tax, and the churn cap.
5. **Pricing breadth and constraints**: machine catalog with spot/CUD, the
   nested-virt matrix for microVM, gVisor-anywhere.
6. **Ground truth**: our numbers come off a live cluster run with the
   measurement harness; theirs are benchmark-derived point values.

## Adoption plan (pending confirmation)

Calculator additions, all optional with sheet-matching defaults:
- Per-phase CPU inputs: active 0.25 / suspend 0.3 / restore 1.22 vCPU.
- Peak model selector: `peak multiplier` (current) **or** `herd %`
  (default 25%) charging herd × restore-peak + rest × average.
- A "CPU-packed density (multi-actor workers)" output row: available vCPU ÷
  weighted vCPU per agent — shown beside today's slot-based density with a
  clear "roadmap upside" label, and a memory-bound check so it can't
  overclaim past RAM.
