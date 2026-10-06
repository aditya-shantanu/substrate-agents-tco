# ARTIFACT — gVisor / pause, personal-assistant, ACTOR_MEMORY=1536Mi (failed leg)

Not a valid measurement. 30 agents, starts staggered over 1200 s, 55-min window on
c3-standard-192-metal. From the second park onward, `runsc checkpoint` exited 128
(`checkpoint failed: checkpointing container "_pause": urpc method
"containerManager.Checkpoint" failed: EOF`): the node's OOM killer killed
`gvisor_sentry` inside the actor's own memory cgroup during the checkpoint
(dmesg: `Memory cgroup out of memory: Killed process … (gvisor_sentry)`,
`gfp_mask=…|__GFP_WRITE`, memcg `<uid>-_pause`). The ~1 GiB checkpoint file's dirty
page cache is charged to the actor's 1536Mi limit on top of its ~740 MB resident
shmem. A crashed actor is absorbing, so every later step of that agent is a 503:
7/30 crashed by 13:23, refusals and errors then dominate. The valid gVisor legs
were rerun with ACTOR_MEMORY=3Gi (see `../2026-10-06-pa-gvisor-c3-metal-{pause,suspend}/`).

snapshots.json here is still a valid per-agent size for a gVisor pause checkpoint
of this workload: pages.img ≈ 956 MiB (the resident set, uncompressed) + checkpoint.img 2 MiB.
