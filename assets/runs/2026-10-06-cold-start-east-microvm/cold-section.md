
## Cold start: a new actor's first life, 2026-10-06

Substrate has no per-actor boot: an actor is created logically and its first `ResumeActor` restores the **template's golden snapshot** onto a free worker. "Cold start" here is therefore measured the way the upstream spawn benchmark measures `ActorTimeToReady`: `CreateActor` → first successful `ResumeActor` (the golden-snapshot restore) → first ping answered through the router. 100 new actors per scenario, 4 in flight at a time, the one-ping workload's own glutton template (256 MiB actors), unsized workers (50 on the metal host, 10 on the c3-standard-4 pool over 4 nodes). Node-level cold costs (first gVisor/kata asset fetch, first image unpack on a fresh node) are *not* in these numbers: every node had served actors before. Each scenario used a fresh actor-name prefix so no actor could pre-exist.

| Cold start (ms, P50 / P90 / P99) | c3-standard-4 nested virt · gVisor | c3-standard-4 nested virt · microVM | bare metal · gVisor | bare metal · microVM |
|---|---|---|---|---|
| Ready: CreateActor → first answered ping | 274 / 355 / 586 (max 610) | 710 / 1,268 / 1,752 (max 1,914) | 340 / 379 / 456 (max 458) | 596 / 666 / 800 (max 1,360) |
| of which first ResumeActor (golden-snapshot restore) | 254 / 341 / 555 (max 578) | 691 / 1,240 / 1,734 (max 1,883) | 330 / 369 / 445 (max 446) | 586 / 640 / 790 (max 1,350) |
| of which resume → first answered ping | 15 / 24 / 37 (max 46) | 18 / 30 / 40 (max 45) | 9 / 12 / 18 (max 32) | 8 / 10 / 32 (max 33) |
| of which CreateActor | 3 / 4 / 14 (max 14) | 2 / 4 / 7 (max 16) | 1 / 2 / 2 (max 3) | 1 / 2 / 3 (max 10) |
| n (failed) · retries (resume / ping) | 100 (0) · 0 / 0 | 100 (0) · 0 / 0 | 100 (0) · 0 / 0 | 100 (0) · 0 / 0 |

Reading it: the first ping is milliseconds everywhere, so a cold start *is* the golden-snapshot restore plus control-plane bookkeeping, no boot. gVisor cold-starts in 274 ms (nested) / 340 ms (metal) at the median; microVM in 710 ms (nested) / 596 ms (metal). Bare metal helps microVM (−16 % median, far tighter tail: P99 800 vs 1,752 ms) and costs gVisor a little at the median while tightening its tail. Against the warm wakes in the table above, a cold start is roughly one extra restore's worth of time.

Artifacts: `assets/runs/2026-10-06-cold-start-{central,east}-{gvisor,microvm}/` (run.log, cold-starts.csv, cold-summary.json).

