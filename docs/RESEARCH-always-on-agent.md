# Research: always-on-agent (OpenClaw on Substrate) — measured reality check

Compiled 2026-09-25 from `~/repos/always-on-agent` (subagent deep-dive). This
is the closest thing to ground truth for the "personal agent on Substrate"
workload: a real OpenClaw actor, gVisor sandboxClass, GKE, gateway-driven
idle-suspend. It calibrates the calculator's "OpenClaw (measured)" preset and
Phase 2's method.

## Architecture in one line

Split presence from cognition: one always-on multi-tenant **gateway** (holds
WhatsApp/IM sockets, 512Mi req / 1536Mi lim) + one suspendable Substrate
**actor per conversation** (`conv-<sha256[:12]>`), resumed implicitly by
atenet on the next turn, suspended by the gateway after an idle window
(demo: 2s timeout, 250ms poll; plugin defaults 120s / 5s).

## Measured numbers (14 Sep 2026, c2d-standard-8 pool, n=12 warm cycles)

| Quantity | Value |
|---|---|
| End-to-end inbound message → actor serving | 3.3–4.7 s warm |
| `ResumeActor` handler | **3.4 s P50** |
| `SuspendActor` handler | **2.2 s P50** |
| Restore breakdown | manifest 0.08s, GCS download 1.06s, oci_unpack 0.01s, gVisor restore **0.21s**, un-instrumented (Node process thaw) **1.98s** |
| Snapshot size (FULL, zstd) | **55–61 MiB** per actor |
| Occupancy per wake (20-min cron agent, 11.5h window) | **8.3 s** → duty cycle **0.72%** |
| Conversation turn occupancy | ~10.5 s (6.0s reply + 4.5s idle-detect + checkpoint) |
| Modeled fleet (1,000 agents, random offsets) | P99 peak 21 workers → **47:1 bankable**; naive average 144:1 "practically unreachable" |
| Demo fleet | 20 actors on 5 workers (4:1), 10 cycling in the recording |

Key insight: **gVisor restore is the cheapest large stage.** A real
multi-process Node agent adds snapshot bytes and ~2 s of process thaw that
scale with the agent, not with Substrate. Published resume figures from
near-empty actors (Glutton small: suspend ~0.4s / resume ~0.24s) do not
transfer — re-measure per workload.

## Method to steal (demo/measure/density.py)

- Occupancy from ate-api-server logs: pair each successful ResumeActor with
  the next SuspendActor per actor; skip failed RPCs and orphan suspends;
  close open intervals at the window edge. Stream logs during the run (the
  ring buffer is dominated by ListActors polling).
- Report **peak, not average**: average density = 1/duty-cycle = workload
  idleness. Bankable density = agents ÷ P99 peak concurrency (Monte-Carlo
  over uniform schedule offsets for modeled fleets; aligned crons = worst
  case, peak = N).
- Occupancy must come from a workload that parks itself; burst-woken actors
  measure "how long you waited", not real occupancy.

## Operational gotchas (all hit in practice)

1. **Cold-worker trap**: first resume on a fresh node is far slower and can
   504 and never recover; pre-warm before measuring.
2. **Golden-snapshot trap**: if the process isn't alive at golden-checkpoint
   time (20s hardcoded warmup), the checkpoint "succeeds" at tens of KiB and
   every restore fails (`inconsistent private memory files`). Check object
   size in the bucket.
3. **Router budgets**: `--route-timeout` 10s, `--parked-request-budget` 5s —
   too tight for LLM turns + 3.4s restores; raise both.
4. **Bucket IAM**: atelet *and* ate-api-server need objectAdmin; missing the
   second wedges actors in SUSPENDING on the *second* suspend.
5. **Pool full = 503** (after ≤5s parking), not queueing; one actor per ateom.
6. Router addressing differs by version: release-0.1 routes by `Host`, main
   by `ate-target-actor` header — send both.
7. Suspend delay = idle timeout + up to one poll interval.
8. Actors cannot suspend themselves (no control-plane credential in the
   sandbox) — suspend must be driven from outside (their one upstream ask).
9. Image digests: actor image must be @sha256-pinned; changing the image
   invalidates snapshots (FULL, no DurableDir → template repoint drops
   conversation state).
10. The 4 local substrate patches were retracted; stock v0.1.0 works with
    only the two router flags tuned. PR #487 became a per-container readyz
    `timeout_seconds`; issue #465 (in-sandbox ingress/egress proxies) is the
    long-term fix.

## What this means for the cost model

- Always-on footprint per *fleet* (not per agent): gateway + control plane —
  in the calculator this sits in "control plane $/mo".
- Personal-agent duty cycle with a 20-min heartbeat measured **0.72%**, below
  our default 2.4% (which includes interactive sessions) — the model's range
  0.5–5% brackets reality.
- Per-wake occupancy is dominated by idle window + suspend (8.3s for a
  seconds-long task): the idle-timeout knob in the calculator is doing real
  work; the OpenClaw demo chose 2s.
- 47:1 at P99 peak with 3.4s/2.2s switch times validates the model shape:
  N = U/(d_eff·P) with peak-sizing baked into P.
