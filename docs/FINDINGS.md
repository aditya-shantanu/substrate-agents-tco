# Findings from the live runs (cluster agents-tco, gke-ai-eco-dev)

Running log of what the Phase 2 harness surfaced. Substrate build under test:
local main @ `v0.1.0-7-ge3a041bc`, GKE 1.36.4, 2× c3-standard-4, 10 gVisor
workers, glutton actors with 128Mi working set, real memory paging + file
I/O per turn.

## 1. Platform bug: resume-during-suspend wedges the actor (worker pinned)

**Symptom.** Under Poisson traffic, actors intermittently stick in
`SUSPENDING` forever. Their checkpoint **completed** (atelet logs the zstd
upload and the Checkpoint RPC returns success in 2–4s), but the suspend
workflow never commits `SUSPENDED`. Every later resume fails with
`workflow failed at step AssignWorker: FailedPrecondition (got
ACTOR_STATE_SUSPENDING, want ACTOR_STATE_SUSPENDED)`. The worker stays
assigned indefinitely (observed 70+ minutes).

**Mechanism (source-traced, corrected).** SUSPENDING is an absorbing state:
the suspend workflow is an in-RPC step chain with no rollback, no retry and
no reconciler; any failure between the MarkSuspending commit and the
FinalizeSuspended commit leaves the actor wedged. Concurrent resumes do
*not* corrupt the commit (workflows serialize on a per-actor lease) — the
AssignWorker failures are the symptom proving the suspend already died. Two
entry points hit us: (A) `runsc checkpoint: exit status 128` — the
`-allow-connected-on-save` flag is passed on `runsc start` but NOT on
`restore`, so previously-resumed actors fail checkpoints when traffic holds
sockets open, and atelet deliberately doesn't crash-mark checkpoint
failures; (B) post-checkpoint workflow death (caller ctx, atelet conn-LRU
eviction, or the synchronous GCS DeletePrefix placed before the commit).
Full trace + fix sketch: [UPSTREAM-BUG-suspending-wedge.md](UPSTREAM-BUG-suspending-wedge.md).

**Blast radius.** Each wedge permanently pins one worker. On a 10-worker
pool this cascades: fewer workers → refusals → client retries → more
arrivals landing mid-suspend → more wedges. Three runs collapsed this way
(healthy for ~5 min, then 503/504 storms). At one point 7 of 10 workers
were pinned by wedged actors.

**Not the cause:** node CPU (10–13% busy), bucket IAM (both SAs have
objectAdmin), client keep-alives (still wedges with keep-alives disabled),
`runsc checkpoint: exit status 128` appeared in *some* early wedges but the
later ones checkpoint cleanly — the stuck state is control-plane-side.

**Workaround in this harness.** The autosuspender's medic
(`--unwedge-after=3m`): force-delete (`any_state`) + recreate from the
template. Frees the worker at the cost of the actor's state; interventions
are counted (`autosuspend_unwedged_total`) and surfaced in the dashboard
and results so measurements stay honest.

**Upstream status (surveyed).** The symptom family is tracked (#1665 stuck
lifecycle states/no recovery, #1502 no reaper for stalled SUSPENDING, #1527
same shape via snapshot-GC, #50 the old exit-128 report; PR #1444 fixes
adjacent races in the same files; #372/#1778 are the maintainers' contract
umbrella). **Untracked specifics worth filing:** the start-vs-restore
`-allow-connected-on-save` inconsistency as the exit-128 root cause on
previously-resumed actors, and the enumerated post-checkpoint death window.
Filing-ready text: [UPSTREAM-BUG-suspending-wedge.md](UPSTREAM-BUG-suspending-wedge.md).

## 2. Time compression amplifies the switching tax exactly as modeled

Suspend (~2.6s), resume (~1.4s incl. routing) and the idle wait do not
compress; agent think-time does. At ×60 each agent needs ~45% of a worker
(50 agents ⇒ ~24 workers); at ×20 ~9 workers; at ×6 ~2.5 workers. The tco
TUI now computes this feasibility check from the model before a run starts.

## 3. Measured numbers so far (gVisor, glutton + 128Mi working set)

| Quantity | Value |
|---|---|
| Suspend (SuspendActor, avg over 200+) | **2.5–2.9s** |
| Resume via router, healthy pool (wake p50) | **1.3–1.7s** |
| Snapshot upload | ~110–155 MB raw, sparse zstd, 2–4s |
| Healthy sawtooth density (×6, 50 agents) | 7–17:1 instantaneous on 10 workers |

These sit between the small-glutton benchmark numbers (0.4s/0.24s) and the
real OpenClaw measurements (2.2s/3.4s) — the 128Mi working set makes
glutton behave much more like a real agent than an empty ping server.
