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

**Trigger.** A request arriving for the actor *while its suspend is in
flight* (the router's implicit ResumeActor racing the suspend workflow).
Memoryless arrivals guarantee some fraction of suspends race — at ~15
suspends/min with a ~5s suspend window and per-agent arrival rates of a few
per hour, a handful wedge per 10 minutes.

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

**Upstream note (to file).** Suspend workflow should either commit
SUSPENDED once the checkpoint is durable regardless of concurrent resume
attempts, or roll back to RUNNING; SUSPENDING must not be a terminal state.
Related prior art: always-on-agent hit a *different* wedge (bucket IAM) with
the same terminal-SUSPENDING signature and pinned release-0.1 (`c48b3a3c`),
reporting reliable suspends there — this may be a main-branch regression.

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
