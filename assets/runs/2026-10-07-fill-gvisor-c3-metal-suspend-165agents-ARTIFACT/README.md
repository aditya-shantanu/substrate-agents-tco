# ARTIFACT — host fill, gVisor / suspend, 165 agents on 50 unsized workers (collapsed)

First leg of the 2026-10-07 host-fill ladder (Workload TCO Calculations.md), sized at
3.3 agents per worker from the Oct 6 busy-worker mean. c3-standard-192-metal, personal-assistant
actor (3 GiB limit), think ×0.02, 35-min window, 10-min stagger.

Outcome: resume P90 41.9 s, errors + refusals above the activation count, busy peak 50/50,
up to **134 live actors on 48 workers** (occupancy.csv: running+resuming+suspending vs
workers_assigned) — the pool is multi-actor (ateom --max-actors 1000, unsized pool =
unconstrained), so this is a host/suspend-path collapse at ~130 live 1 GiB actors, not pool
saturation. Host at the time: memory 169 GB (22 %), CPU p90 21 %, Hyperdisk write p90 1,000 MiB/s.
The ladder was redesigned afterwards: pool fixed at 50 workers, agent count as the knob.
