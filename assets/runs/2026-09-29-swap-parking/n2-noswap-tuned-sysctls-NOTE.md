# N2 no-swap + recommended reclaim sysctls: node livelock (reproduced twice)

Pool `tco-n2-noswap`: n2-standard-4 (4 vCPU / 16 GB), nested virt, no swap,
`vm.watermark_scale_factor=2000`, `vm.min_free_kbytes=614400`, `vm.swappiness=120`
(the sysctls recommended alongside the dedicated-SSD swap / zswap setup).
Same resident-parking arm as every other run (60 workers = 60 agents, 512 MiB
working set, 2 GiB actor limit).

Attempt 1 (11:00 PDT): kubelet's last log line 11:03:34, node NotReady, no
warnings before; GKE auto-repair recreated the VM at 11:16.

Attempt 2 (12:10 PDT), evidence captured before repair:
- kubelet log rate: 1,760 lines/min at 12:11 → 110 → 62 → 1/min from 12:14.
- Node conditions: `Ready Unknown — Kubelet stopped posting node status`
  (last heartbeat 12:11:16); node-problem-detector plugins `Timeout when running
  plugin "/bin/bash": signal: killed` (host so starved even short shell
  plugins cannot complete).
- Serial console: repeating `systemd-journald[193]: Under memory pressure,
  flushing caches.`; no kernel panic, no soft-lockup, no OOM-kill messages.

Control: pool `tco-n2-noswap-def` (identical except default sysctls) under the
same load: 14 OOM kills, node stays Ready, arm completes (15 resident agents,
waves 10 and 15 clean).

Reading: with no swap to reclaim into, a 600 MB `min_free_kbytes` plus a 20 %
watermark gap keeps the kernel in reclaim instead of letting the OOM killer
resolve pressure — a livelock that takes the whole node (kubelet included)
down. These sysctls presuppose swap/zswap; do not apply them to swapless nodes.
