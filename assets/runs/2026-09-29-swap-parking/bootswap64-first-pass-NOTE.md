# Boot-disk swap 64 GiB, resident-parking design (early read, old simulator build)

Node: c3-standard-4-lssd (4 vCPU, 16 GB), 64 GiB swap on the pd-balanced boot disk (200 GB).
Config: 60 workers (25m/128Mi → 1/2Gi), 60 agents, 512Mi working set, actor limit 2Gi,
SETUP_SUSPEND=false (agents stay resident), idle timeout 2h.

Observed during setup (03:11–03:42 PDT):
- 03:37: 43 agents resident, node working set 13.6 GiB (RAM full), swap used 10.9 GB,
  first OOM kills (cgroup) as per-pod swap allowance (128Mi × 64/16 = 512 MiB) ran out.
- 03:42: all 60 actors RUNNING (resident) on the node; swap used 16.8 GB; RAM working set
  down to 5.9 GiB (kernel paged most agents out). 51/60 worker pods ready at that moment
  (some restarting after OOM kills).
- Setup "failures" (23/60) were all router-side errors during the 512 MiB fill under paging
  pressure — 11× HTTP 504 upstream timeout, 6× 503 connect reset, 6× 421 misdirected — not
  placement failures. The old simulator build aborted the job at >10% setup failures, so no
  waves ran; the rebuilt simulator retries these and runs waves over the agents that fit.

Takeaway: even boot-disk swap lets the node HOLD 3× the 512 MiB agents RAM allows (60 vs ~20);
what it cannot yet show is whether wakes (page-in from a 140 MB/s disk) meet the SLO gates.
That is what the identical no-swap / Local-SSD / boot-disk arms in the next driver measure.
