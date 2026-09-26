# Filing-ready: actors wedge in SUSPENDING under traffic; workers pinned

Prepared 2026-09-25 from live reproduction (GKE cluster `agents-tco`,
substrate main @ `e3a041bc`) plus a full source trace and an upstream issue
survey. Everything below is verifiable: cluster evidence in
[`runs/2026-09-25-baseline-x6/`](runs/2026-09-25-baseline-x6/) and
[`FINDINGS.md`](FINDINGS.md); file/line references are against `e3a041bc`.

## One-paragraph summary

`ACTOR_STATE_SUSPENDING` is an absorbing state. The suspend workflow is an
in-RPC step chain with no persisted record, no rollback, no server-side
retry, and no reconciler over actors. Any failure between the MarkSuspending
commit (`workflow_suspend.go:152-156`) and the FinalizeSuspended commit
(`:425-437`) leaves the actor SUSPENDING with its worker assignment intact —
forever, because the only thing that can move it forward is another
`SuspendActor` call, which nothing issues automatically, and `ResumeActor`
hard-rejects SUSPENDING at AssignWorker (`workflow_resume.go:304`,
"prerequisite not met … got: ACTOR_STATE_SUSPENDING"). Under Poisson traffic
a few of these wedge per 10 minutes; each pins one worker; a 10-worker pool
collapses (we observed 7/10 pinned, ages 70+ min).

## The two production entry points we hit

**A. `runsc checkpoint: exit status 128` on restored sandboxes.**
`-allow-connected-on-save` is passed only on `runsc start`
(`cmd/ateom-gvisor/runsc.go:130`) — not on `runsc restore` (`:262-298`). So
any actor that has suspended/resumed at least once runs *without* the flag,
and a checkpoint taken while guest TCP endpoints are connected (traffic
arriving mid-suspend) fails. atelet then returns the error **without** the
crash directive (`cmd/atelet/main.go:650-655`, with the in-code TODO:
"Ateom should classify checkpoint failures, and set 'should-crash' …"), so
the actor is neither crashed nor finalized: wedged. (Upload failures, by
contrast, do attach the crash directive and land in CRASHED.)

**B. Post-checkpoint workflow death.** The checkpoint and zstd upload
succeed on the worker (we have atelet logs proving it), but the ateapi-side
chain dies before the single UpdateActor that flips SUSPENDED and clears the
assignment: caller context cancel/disconnect (the workflow runs on the
caller's ctx), the atelet conn-LRU closing an in-flight connection
(`dialer.go`, `newAteletConnCache(1024)` — "Closing on eviction can fail an
RPC still in flight"), the **synchronous GCS DeletePrefix of the previous
snapshot placed before the commit** (`releaseReplacedSnapshot`,
`workflow_suspend.go:417,454-479`), or a version conflict in the finalize
window.

Common aftermath: error returned to the caller and forgotten; per-actor
lease released; every subsequent ResumeActor (the router auto-resumes on
traffic and retries FailedPrecondition inside its 5s parking budget,
`resumer.go:152-161`) dies at AssignWorker; the pool drains.

Note on a tempting wrong theory: concurrent resumes do **not** corrupt the
suspend commit — workflows are serialized by a per-actor lease
(`workflow.go:170-184`), and resumes write nothing before AssignWorker. The
observed resume failures are the *symptom* proving the suspend workflow had
already died and released its lease.

## Existing coverage upstream (survey of agent-substrate/substrate issues)

| Ref | State | Overlap |
|---|---|---|
| #1665 | OPEN | Same symptom family (stuck RESUMING/SUSPENDING/DELETING, worker allocated, no API recovery) — triggers listed are node-side failures, not this pair of entry points |
| #1502 | OPEN | "Stalled SUSPENDING actors have no system-driven recovery" — the missing-reaper gap |
| #1527 | OPEN | Same shape via a different failing step (snapshot GC before the commit) |
| #50 | OPEN | Oldest report of exit-128 → stuck SUSPENDING |
| #818 | CLOSED | Prior race-during-suspend (crash-detection racer) |
| #1443 / PR #1444 | OPEN | Active workflow-race fixes in the same files (worker-delete vs finalizer) |
| #372 / #1778 | OPEN | Maintainers' umbrella: ateapi/atelet contract & idempotency rework |
| #1572 | OPEN | Documents the trigger scenario (traffic during suspend) but only the data-plane 502s |

**Verdict: the symptom class is tracked; these specifics are not** —
(1) the `-allow-connected-on-save` start/restore inconsistency as a root
cause of exit-128 on previously-resumed actors, and (2) the enumerated
post-checkpoint death window (caller-ctx tail, conn-LRU eviction, DeletePrefix
in the critical path) with the router's retry loop as the amplifier. Worth
filing as a new issue cross-referencing the table above.

## Suggested fixes (matching the code structure; the workflow is already re-entrant)

1. **Reaper** over transitional actors: list SUSPENDING older than a
   threshold and re-drive `SuspendActor` — re-entry fast-forwards
   (`workflow_suspend.go:130-132`) and FinalizeSuspended completes the
   half-done work. (This is #1502; our harness ships a client-side stopgap.)
2. **Let ResumeActor repair**: on SUSPENDING with an existing remote
   manifest, run FinalizeSuspended (it holds the actor lease already) and
   fall through to a normal resume.
3. **Detach the tail from the caller**: after CallAteletSuspend succeeds,
   run DetachVolumes + FinalizeSuspended under `context.WithoutCancel`
   (the router already does this for resume), and move the GCS DeletePrefix
   out of the critical path (async GC).
4. **Close entry point A**: pass `-allow-connected-on-save` on
   `runsc create`/`restore` too, and implement the atelet TODO so
   non-retryable checkpoint failures set the crash directive → CRASHED
   (worker released) instead of an invisible wedge.

## Our mitigation (this repo)

The autosuspender's **medic** (`--unwedge-after`, default 3m): detect
SUSPENDING older than the threshold, `DeleteActor(any_state)` + recreate
from the template. Frees the worker at the cost of actor state; every
intervention is counted (`autosuspend_unwedged_total`) and surfaced in the
TUI/dashboard, and wedged workers are excluded from capacity displays.
With the medic on, a 30-minute 50-agent run completed cleanly (2 wedges
healed) and produced the $1.21/agent/month measurement.
