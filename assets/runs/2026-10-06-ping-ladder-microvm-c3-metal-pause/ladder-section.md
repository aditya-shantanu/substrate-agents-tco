## Measured overcommit on the bare-metal microVM host (no extrapolation), 2026-10-06

Question: how many agents can the c3-standard-192-metal host really serve with microVM + PauseActor? Method: fix a worker shape and fill the host, park a large fleet, let agents wake **independently** (Poisson, not the serialized 20-per-user loop, which can never saturate anything), and add agents in waves until wakes fail. Setup: 200 microVM workers shaped 0.4 vCPU request / 2 vCPU limit / 1 GiB (node pool recreated with `--max-pods-per-node 256`; GKE's default 110 caps the pool), 2,000 parked 256 MiB actors, each agent waking every 25 s on average, +50 agents every 3 minutes, failure gate refusals > 5 % or errors > 2 % or resume P99 > 10 s. Pause = node-local checkpoint; boot disk Hyperdisk Balanced 3 TB at 100,000 IOPS / 2,400 MiB/s.

| Active agents | Wakes/s (host) | Wakes in window | Refusals | Errors | Resume P50 / P90 / P99 |
|---|---|---|---|---|---|
| 50 | 2 | 295 | 0 % | 0 % | 149 / 255 / 470 ms |
| 100 | 4 | 633 | 0 % | 0 % | 149 / 226 / 426 ms |
| 150 | 6 | 1,015 | 0 % | 0 % | 156 / 284 / 521 ms |
| 200 | 8 | 1,377 | 0 % | 0 % | 174 / 444 / 1,169 ms |
| 250 | 10 | 1,565 | 0 % | 0 % | 210 / 1,040 / 3,028 ms |
| **300** | **12** | 1,673 | 0.1 % | 0 % | **382 / 2,729 / 4,778 ms** ← last sustainable |
| 350 | 14 | 1,412 | 36.8 % | 0.1 % | 5,080 / 17,581 / 31,400 ms ← collapse |

**What the host can do:** about **12 wakes per second** with pause/resume on microVM before the router starts refusing (its 5 s park budget) and resume P99 passes 5 s; at 14 wakes/s it collapses. Host CPU stayed under 5 % throughout, but the workers did fill up: busy workers climbed to a P99 of 142 and a peak of 154 of 200 as each cycle's pause and restore stretched (worker-side VM checkpoint P90 32 s, VM restore P90 17 s during the collapse, every other phase at milliseconds). So the pool saturates not because 12 wakes/s is a lot of work, but because the per-node checkpoint/restore path slows down under concurrency and each slowed cycle holds a worker — over the whole ladder, mean pause was 2.8 s and resume P50/P90/P99 209 / 4,392 / 21,389 ms (n = 8,115).

**Translating 12 wakes/s into agents per host** (this is the only place arithmetic enters, and it is division, not a model):

- at the hot rate of this ladder, one wake per 25 s per agent: **300 agents** per host, i.e. 1.5 per worker;
- at the Prow loop's effective rate, one wake per ~220 s per agent: ≈ **2,600 agents** per host (12 × 220), ≈ 13 per worker;
- at the coding-session rate measured on 2026-10-05 (160 wakes per agent-day = one per 540 s): ≈ 6,500 agents per host.

**The second bound is disk, not CPU.** A node-local pause checkpoint of a 256 MiB microVM guest is on the order of its memory: 4,000 parked actors filled the original 1 TB boot disk (904 GiB), tainted the node and evicted everything, including Substrate's control plane. Deleting actors does not reclaim their node directories (9,082 stale actor dirs, 894 GiB, after the fleet had been deleted through the API). With the 3 TB disk the 2,000-actor ladder used ~1.3 TB including the leftovers. Any pause-based density claim has to carry a GB-per-parked-agent figure next to it.

