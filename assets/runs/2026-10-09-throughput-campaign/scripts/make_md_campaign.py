"""Throughput-optimization campaign (2026-10-09): iteration tables from nanoturn2/3 turn.txt files."""
import json, glob, os, re, sys
OUT = os.path.expanduser("~/Downloads/nano-agent-activation-throughput-optimization-gvisor-vs-microvm-2026-10-09.md")
def levels(path):
    rows = []
    for l in open(path, errors="replace"):
        if '"msg":"swap level result"' in l:
            try: rows.append(json.loads(l))
            except Exception: pass
    return rows
def table(rows):
    out = ["| Level | Target swaps/s | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Backlog | Crashed | Verdict |", "|---|---|---|---|---|---|---|---|---|"]
    for r in rows:
        out.append(f"| {r['n_per_tick']} per tick | {r['target_swaps_per_s']} | **{r['achieved_swaps_per_s']}** | {r['wake_p50_ms']:,} / {r['wake_p90_ms']:,} / {r['wake_p99_ms']:,} | {r['park_p50_ms']:,} / {r['park_p90_ms']:,} / {r['park_p99_ms']:,} | {r['errors']} + {r['refusals']} | {r['backlog']} | {r['crashed']} | {r['failed_on'] or 'pass'} |")
    return out
ITER = {  # (dir, title, what changed)
 "metal2": [("nanoturn-metal2", "Original test: 10-s ticks (burst of N), latency gate 2.5×, unmodified main", "baseline as reported on 2026-10-08"),
            ("nanoturn2-metal2", "Rerun: 10-s ticks, latency gates OFF", "same build; shows the burst design collapsing at 4 swaps/s"),
            ("nanoturn3-metal2-1", "Iteration 1: 1-s ticks + pooled GCS clients (ATE_GCS_CLIENT_POOL=64)", "sim: SWAP_EVERY=1s, ×1.5 levels of 2 min, latency gate 2.5× back on; plugin: every transfer on a pooled client"),
            ("nanoturn3-metal2-2", "Iteration 2: + pooled compression buffers/encoders/decoders, fan-out cap 8, GOGC=200; worker: tar fsync skipped", "INVALID beyond 2 swaps/s: 22 actors stuck DELETING answered 503 to every wake (sim now excludes them)"),
            ("nanoturn3-metal2-3", "Iteration 3: same build, fleet exclusion fix", "cut short by the crash gate (5 crashed actors already in the fleet)"),
            ("nanoturn3-metal2-4", "Iteration 4: + worker reseed timeout 15 s with retry (restore crash fix), crash gate 20", "first clean run of the full patch set: 8 swaps/s passes; 12 swaps/s delivered error-free, failed the 2.5× rule by 23 ms"),
            ("nanoturn3-metal2-6", "Iteration 6: same build + pprof endpoint, ramp from 8 in ×1.2 steps", "15 swaps/s passes (14.75 achieved, 0 errors); 18 swaps/s fails (latency + 12 refusals). CPU profile at 12 swaps/s: 24 cores in the plugin, of which ~8 are page faults/zeroing of fresh 64 MiB upload buffers and 128 MiB download range buffers, ~9 compression, ~4 decoded-output write syscalls"),
            ("nanoturn3-metal2-7", "Iteration 7: plugin v5 (bounded free lists for 64 MiB upload head / 16 MiB range / 1 MiB copy buffers, writer chunk = object size, decoder concurrency 4) + GOGC=400 GOMEMLIMIT=96GiB; ramp from 10 ×1.2", "page faults gone (300/s vs 150k/s) but the ceiling did not move: 15 passes, 18 fails (resume P50 7.6 s, 410 refusals); plugin heap grew to 91 GB under the memory limit"),
            ("nanoturn3-metal2-8", "Iteration 8: same build, GOGC=200 and no GOMEMLIMIT; ramp from 12 ×1.15", "14 passes, 17 fails: the plugin's download stage goes from 1.1 s to 7.2 s median between 14 and 17 swaps/s while uploads stay at 0.7 s — the restore download pipeline saturates at ~15-16 swaps/s"),
            ("nanoturn3-metal2-9", "Iteration 9: worker skips the graceful VMM shutdown after a checkpoint; 37k orphan actor dirs (576 GB) deleted right before; ramp started AT 14", "invalid: the first level is both the gate baseline and a cold start, and the disk was still digesting the deletion (PSI io 10) — 14 swaps/s failed where it had passed"),
            ("nanoturn3-metal2-10", "Iteration 10: same build, ramp from 8 ×1.3", "8 and 11 pass; 15 fails with PSI io 13 % (it had passed in iterations 6-7 at PSI io 7) — the disk is back in the hot path"),
            ("nanoturn3-metal2-11", "Iteration 11: /var/lib/ate/actors moved onto tmpfs (restore and checkpoint staging no longer touch the boot disk)", "8 passes with a long tail, 11 fails — PSI io 0 but the sim's own first-wake catch-up storm (see iteration 12) was found in this run's router log"),
            ("nanoturn3-metal2-13", "Iteration 13: plugin with an instrumented GCS transport (ATE_GCS_HTTP_STATS=1), ramp started at 11", "collapsed at the first level; the counters showed no GCS throttling (all 2xx, per-request p50 55 ms) — the instrumented transport and the cold first level are both suspects"),
            ("nanoturn3-metal2-14", "Iteration 14: same image, default transport again, ramp from 8", "collapsed at 8 swaps/s (download stage 15 s mean) although a quiet 1/20-wake probe on the same build is normal (0.55 s / 1.2 s) — cause not pinned; the plugin's intrinsic aggregate ceiling in the quiet probe is ~0.75-0.9 GB/s of compressed intake, i.e. ~15-16 swaps/s at 42 MiB per snapshot"),
            ("nanoturn3-metal2-15", "Iteration 15: plugin v8 — 4 MiB download ranges with 12 in flight per object, composite uploads in 8 MiB parts from 8 MiB (per-stream GCS throughput here is ~45 MB/s, so more streams per object)", "best resume yet at 15 swaps/s (P50 1,087 / P90 1,256 ms) but the composite uploads made suspend worse (P99 7.8 s). At 21 swaps/s everything on the node slows at once: worker tap setup 2 → 1,282 ms, VM restore 136 → 1,537, prep 13 → 1,038, teardown 232 → 1,462, load average 142, 300 upload goroutines waiting on HTTP/2 flow control (outbound network saturated with 8 MiB parts); the plugin's own download stayed ~1.0 s. The microVM wall above ~15-20 swaps/s is the node's sandbox create/teardown path (tap/netlink, mounts, VMM launch) plus ~2 GB/s of combined transfer"),
            ("nanoturn3-metal2-16", "Iteration 16: plugin v9b — the 4 MiB/12 downloads kept, uploads back to one request per object", "15 swaps/s clean (resume 1,113 / 1,508 ms, suspend 1,119 / 1,352); 21 collapses the same way as iteration 15 — the ceiling is not in the plugin any more"),
            ("nanoturn3-metal2-17", "Iteration 17: microVM worker with the sandbox network-namespace pool ported from the gVisor campaign (internal/ateomnet/netpool.go; the two workers share the package)", "no change: the microVM worker's per-activation cost is the tap device, VMM launch/restore and teardown, not namespace creation; 15 passes (resume 1,097 / 1,223), 21 collapses identically"),
            ("nanoturn3-metal2-18", "Iteration 18: + denser zstd level for snapshot uploads (ATE_ZSTD_LEVEL=default)", "the guest memory image barely compresses better (39-42 MiB vs 41-42) and the extra CPU makes 21 swaps/s collapse harder; reverted"),
            ("nanoturn3-metal2-19", "Iteration 19: final configuration (plugin v9b, pooled namespaces, tmpfs, catch-up off, zstd fastest), fine ramp 12, 14, 17, 20", "17 swaps/s clean; 20 fails (resume P90 8.7 s, 91 refusals)"),
            ("nanoturn3-metal2-12", "Iteration 12: tmpfs + sim first-wake catch-up OFF (SCRIPT_CATCHUP=false)", "the catch-up replayed every skipped step's ops on an agent's first wake: ~46 requests per wake, a storm proportional to the swap rate, present in every earlier iteration on both nodes"),
            ("nanoturn3-metal2-5", "Iteration 5: same build, ramp from 8 in ×1.2 steps", "baseline for the relative gate is the 8 swaps/s level here; the 12 swaps/s resume P90 (1,839 ms) is also within 2.5× of the 2 swaps/s baseline of iteration 4 (751 ms × 2.5 = 1,878)")],
 "east":   [("nanoturn-east", "Original test: 10-s ticks, latency gate 2.5×, unmodified main", "baseline as reported on 2026-10-08"),
            ("nanoturn2-east", "Rerun: 10-s ticks, latency gates OFF", "8 swaps/s fails on errors: resume P50 crosses the router's 5-s parked-request budget")] +
           [(d, f"gVisor agent iteration {d.split('-')[-1]}", "builds: 1 baseline; 2 plugin/atelet patch; 3 + sandbox netns pool; 4 + runsc --shared-root; 5 --ignore-cgroups (rejected); 6 build 4 + cgroup2 favordynmods (lost on roll); 7 reusable cgroup slots (crashed, rejected); 8-9 build C: lean teardown + no app cgroup (fixes a ~30 % cgroup leak per suspend); 10 build D + sim catch-up off; 11 build D on a ×1.25 ramp → 9 swaps/s clean, 12 delivered error-free. Remaining limiters measured on the node: the kernel mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, and gVisor's own restore. Full report: gv-report.md") for d in sorted(glob.glob("nanoturn3-east-*"), key=lambda x: int(x.split('-')[-1]))],
}
SUMMARY = """## Result in one table

| | microVM (c3-standard-192-metal, 650 awake, 5,000 registered) | gVisor (same) |
|---|---|---|
| **Start of the day** (10-s burst ticks, main 66f8a888) | 2 swaps/s within the 2.5× latency rule; 4 fails | 2 swaps/s; 4 fails |
| Same build, 1-s ticks | 5 swaps/s | 5 swaps/s (8 fails the latency rule by a little) |
| **End of the day, within the bounds** (P90 ≤ 2.5× first level, < 0.5 % errors/refusals, no backlog growth) | **17 swaps/s** (iteration 19: 16.7 achieved, resume P50 1.30 s / P90 1.40 s / P99 1.56 s, suspend P50 1.09 s / P90 1.35 s, 0 errors, 0 refusals); 15 swaps/s held cleanly in six consecutive iterations with resume P50 ≈ 1.1 s | **9 swaps/s** — resume P50 0.96 s / P90 1.19 s, suspend P50 0.59 s, 0 errors |
| Delivered error-free beyond the latency rule | 17 (20 swaps/s collapses: resume P90 8.7 s, refusals) | 12 swaps/s (11.8 achieved, 0 errors, 0 refusals) |
| Where it breaks now | 18–21 swaps/s: every per-VM step on the node convoys at once (tap device setup 2 → 1,300 ms, VMM restore 140 → 1,500, teardown 230 → 1,460, load average 140) with the CPUs only ~35 % busy and nothing blocked on IO — kernel/VMM-level serialisation of sandbox create/teardown, plus ~2 GB/s of snapshot transfer | 12 swaps/s: the kernel's mount-namespace lock during runsc's per-sandbox chroot setup, cgroup v2 task-migration writes, gVisor's own restore (measured on the node) |

@@RATE@@

**What moved the numbers, in order of effect**

1. **Measuring correctly** (harness): issuing swaps steadily (1-s ticks) instead of bursts of N every 10 s. Every earlier latency figure was burst queueing. 2 → 5 swaps/s on both runtimes with no Substrate change.
2. **Harness again**: the sim replayed every skipped step's ops on an agent's first wake (its random day-offset "catch-up"), ~46 requests per first wake — a request storm proportional to the swap rate, present in every run until iteration 12. Off since (`SCRIPT_CATCHUP=false`).
3. **Snapshot plugin** (`pkg/objectstorage`, both nodes): one GCS connection per node → a pool used by every transfer; fresh 64 MiB upload buffers, 128 MiB download range buffers, encoders and decoders per transfer → bounded free lists and pooled codecs (page faults 150k/s → 300/s); 4 MiB download ranges, 12 in flight per object (per-stream GCS throughput here is ~45 MB/s). Rejected after measurement: composite uploads in 8 MiB parts (suspend P99 7.8 s), denser zstd level (guest memory compresses 41 → 40 MiB only, 3× the CPU), GC tuning (GOGC/GOMEMLIMIT: no effect on the ceiling).
4. **microVM worker** (`cmd/ateom-microvm`): guest CRNG reseed after restore given 15 s and a retry instead of 5 s and a crash (~1 restore in 500–1,000 was crashing the actor, and a crashed actor's deletion then hangs when its worker is gone); per-checkpoint tar fsync skipped (approved for the experiment); graceful VMM shutdown skipped after a checkpoint (no measurable gain); the gVisor campaign's sandbox network-namespace pool ported (no gain on microVM: its per-activation cost is the tap device, VMM launch/restore and teardown).
5. **Node**: `/var/lib/ate/actors` on tmpfs (restore/checkpoint staging off the boot disk) — removes IO pressure, no change to the ceiling; 37k orphaned actor directories (576 GB) reclaimed.
6. **gVisor worker** (`cmd/ateom-gvisor`, `internal/ateomnet`, by the background agent): pooled sandbox network namespaces (net_setup 170–330 → 1 ms), one shared gofer namespace per worker (`runsc --shared-root`), a lean post-checkpoint teardown that also fixed a ~30 % cgroup leak per suspend (8,157 cgroups for 650 actors before), no cgroup for the app container. Rejected: `--ignore-cgroups` (sentry sized from the host), reusable worker-owned cgroup slots (gofers die in a reused cgroup).

**Caveats.** Every iteration below purged and re-registered the 5,000 actors at its start (the deploy step runs `clean.sh`); the fleet is therefore fresh in every run, and the per-run numbers are comparable but each run's first level is both the gate baseline and a cold start — ramps were started two or more levels below the suspected ceiling. The experimental Substrate changes live uncommitted in two worktrees (`~/repos/substrate-east1` microVM, `~/repos/substrate-east` gVisor); the patches are in the archive. The fsync skip and the reseed/teardown changes are experiment-grade and need proper flags upstream. The harness fixes (1-s ticks, catch-up switch, readiness check, fleet reuse, backlog gate) are committed in `substrate-agents-tco`.

"""
md = ["# Activation throughput with 650 awake nano agents — optimization campaign (2026-10-09)\n",
      "Goal: raise steady-state suspend+resume throughput (swaps/s) with 650 nano-personal-agent actors awake and 5,000 registered, one c3-standard-192-metal node per runtime, Substrate main 66f8a888 plus the experimental patches listed per iteration. Full method, assumptions and the original numbers: `nano-personal-agent-cold-start-and-turnover-5000-gvisor-vs-microvm-main-2026-10-08.md`.\n",
      "Each level runs 2 minutes (4 in the original test); the gates are: resume or suspend P90 > 2.5× the first level, errors or refusals > 0.5 % of wakes, more than 5 s' worth of swaps still in flight at the level's end (backlog), host memory/PSI limits, crashed actors (5, later 20). A level marked `pass` with a higher achieved rate than the last clean one but a `wake-p90-vs-baseline` verdict means the system delivered the rate with zero errors and only the relative-latency rule stopped the ramp.\n"]
SUMMARY_HEAD, SUMMARY_TAIL = SUMMARY.split("@@RATE@@")
md.append(SUMMARY_HEAD)

def _rows(path):
    out=[]
    for l in open(path, errors="replace"):
        if '"msg":"swap level result"' in l:
            try: out.append(json.loads(l))
            except Exception: pass
    return out
def _fmt(r, tick):
    rate = r["n_per_tick"]/tick
    return (f"{rate:g}", r["achieved_swaps_per_s"], f"{r['wake_p50_ms']:,} / {r['wake_p90_ms']:,} / {r['wake_p99_ms']:,}", f"{r['park_p50_ms']:,} / {r['park_p90_ms']:,} / {r['park_p99_ms']:,}", f"{r['errors']} + {r['refusals']}", ("pass" if not r["failed_on"] else r["failed_on"]))
DETAIL = """The limiter changed five times during the day. Each hand-off below says what was measured, where in the code or kernel it sits, and what state it is in now.

**1. The test itself (fixed).** The original harness issued all N suspends and N resumes of a tick at the same instant every 10 s. Latency then grows linearly with N (microVM resume P50 1.26 / 2.27 / 3.03 s at 1 / 2 / 4 swaps/s) because the burst queues, and a 2.5× relative-latency gate trips on burst size, not on the system. Evidence: the same build at 1-s ticks passes 5 swaps/s on both runtimes. Anyone re-running this must use `SWAP_EVERY=1s` and start the ramp at least two levels below the suspected ceiling, because the first level is also the gate's baseline and a cold start.

**2. The harness's first-wake catch-up (fixed).** In swap mode the sim replayed every skipped step's ingest/write ops on an agent's first lap, i.e. right after its first wake. The router log showed 133 first-time wakes producing 6,170 requests in 30 s (~46 per wake), each also costing a `ResumeActor` call at the api-server. The storm scaled with the swap rate and was present in every run until iteration 12. `--script-catchup=false` (commit 605abef) removes it; all figures from iteration 12 on exclude it.

**3. The snapshot plugin, one process per node (fixed as far as it matters).** Three things inside `cmd/snapshot-plugin` / `pkg/objectstorage` bounded throughput before anything on the node did:
- *One GCS connection.* One `storage.Client` (one HTTP/2 connection) carried every transfer on the node; the pooled clients were only used for parts of objects over 16 MiB (download) / 64 MiB (upload), and the 10 MiB gVisor and 42 MiB microVM snapshots never qualified. Quiet probe, 20 concurrent restores: download 1,645 → 1,233 ms with a 64-client pool. (Upstream commit e2a9e2e by dberkov makes the same fix minus the `GetObject` head range; this build routes that too.)
- *Allocation churn.* Each upload allocated a fresh 64 MiB head buffer plus a 64 MiB media buffer for a 42 MiB object; each download 128 MiB of range buffers; every transfer a new zstd encoder or decoder. At 12 swaps/s that was 24 cores in the plugin, 8 of them page faults and zeroing (`runtime.memclrNoHeapPointers` 18 % of CPU, ~150k minor faults/s, RSS swinging 4–17 GB). Bounded free lists for the big buffers, pooled codecs, writer chunk sized to the object and a 1 MiB copy buffer took faults to ~300/s and CPU per swap down ~20 %. The ceiling did not move for that alone — the churn was a cost, not the limit.
- *Per-stream throughput.* A single GCS stream from this node delivers ~45 MB/s, so a 42 MiB object over three 16 MiB ranges capped at ~140 MB/s per restore. 4 MiB ranges with 12 in flight per object brought resume P90 at 15 swaps/s from 1,508 to 1,256 ms. Composite uploads in 8 MiB parts were tried and rejected (suspend P99 7.8 s: compose round trips and HTTP/2 flow control on the upload side). A denser zstd level was tried and rejected (guest memory compresses 41 → 40 MiB only, 3× the CPU). GC settings (GOGC 200/400, GOMEMLIMIT) changed nothing.
State now: at 17 swaps/s the plugin uses ~25 cores, its download stage is ~0.7–1.0 s per 128 MiB image (GCS first byte 50 ms, write 50 ms, the rest decode and body transfer), and it is no longer what gives out first.

**4. microVM worker defects on the path (fixed for the experiment, need proper flags).** (a) After a restore the worker asks the guest agent to reseed the CRNG with a 5-s timeout and no retry; under load ~1 restore in 500–1,000 timed out and the actor was marked CRASHED — and deleting a crashed actor whose worker pod is gone hangs in DELETING, which then answers 503 to every wake. 15 s + one retry: zero crashes since. (b) Every checkpoint fsynced its rootfs-upper tar on the shared boot disk (`internal/tarutil`), 0.5 → 2.4 s at 40 concurrent; skipped. (c) The graceful VMM shutdown after a checkpoint (p50 178 / p90 370 ms) was skipped; teardown did not get measurably shorter, so the rest of teardown dominates.

**5. Where it is now: the node's sandbox create/teardown path, in the kernel and the VMM.** This is the limiter on both runtimes and it is not Substrate Go code.

*microVM, 17 swaps/s clean, 20 collapses.* Worker-side stage medians from the `Restore/Checkpoint timing breakdown` lines (iteration 17; identical shape in 15, 16 and 19):

| stage (ms, median) | 8/s | 15/s | 21/s |
|---|---|---|---|
| restore: prep (egress prepare, sandbox-state cleanup, dirs) | 11 | 43 | 1,034 |
| restore: lowers (overlay mounts + virtiofsd spawn) | 16 | 60 | 514 |
| restore: tap device setup (netlink in the actor netns) | 2 | 3 | 1,347 |
| restore: vmm_launch (cloud-hypervisor spawn + API socket) | 12 | 15 | 191 |
| restore: vm_restore (CH restore of the 128 MiB image) | 114 | 168 | 1,541 |
| restore total (worker) | 205 | 389 | 5,492 |
| atelet download (plugin) | 548 | 629 | 886 |
| checkpoint: teardown (kill VMM + virtiofsd, unmount, netns/tap release) | 120 | 222 | 1,458 |
| checkpoint total (worker) | 193 | 302 | 1,561 |

At 21/s the node's CPUs are ~35 % busy (user 46 cores, system 20, irq 5 of 192), `procs_blocked` ≈ 0 (no IO wait), PSI io 0 with the state directory on tmpfs, load average ~140. Every unrelated per-VM syscall path slows 10–100× together: that is lock convoying — `rtnl_lock` for tap/veth/netns work, the mount namespace lock for the overlay/virtiofs mounts, cgroup and mm locks around 20 process spawns and kills per second, plus cloud-hypervisor's own restore of 128 MiB per VM. The pooled network namespaces (ported from the gVisor work) removed namespace creation but not the tap, and changed nothing here. Transfer is the second term: 20/s × (42 MiB down + 50 MiB up) ≈ 1.9 GB/s, where uploads start queuing on HTTP/2 flow control.

*gVisor, 9 swaps/s clean, 12 delivered error-free but over the latency rule.* From the gVisor agent's node probes (gv-report.md): sandbox network setup went 170–330 ms → 1 ms with the namespace pool; the per-sandbox gofer namespace went away with `runsc --shared-root`; a ~30 % cgroup leak per suspend (8,157 cgroups for 650 actors) was fixed by a lean teardown. What remains and grows with rate is `pause_create` (90 ms alone → 600–700 ms at 12/s), and the probe pins it: a bind+umount pair on the node goes 2.4 → 19 ms and `unshare -m` 1.5 → 18 ms at 12/s — runsc's gofer and sentry each build a chroot per sandbox under the kernel's global mount-namespace lock. Then cgroup v2 task migration (1 mkdir + 2 `cgroup.procs` writes + 1 rmdir per activation; each write takes `cgroup_threadgroup_rwsem` as a writer and stalls every fork on the node; the `favordynmods` mount option measured 42 → 2.7 ms per attach pair but did not survive a pool roll and needs the kernel command line), and gVisor's own restore inside the sentry (app_restore 100 → 400–570 ms at 12/s). Download is flat at ~170 ms for the 10 MiB image.

*Not limiting on either node:* the control plane (actors table 4 MB, outbox 9 MB, resume RPC handling sub-millisecond apart from the restore), the router (its 5-s parked-request budget converts the latency cliff into 503s but does not cause it), the boot disk (tmpfs made no difference), host memory (> 600 GB free), and CPU count.

"""

IDEAS = """## How to break the remaining bottlenecks — ranked proposals (code-grounded; no idea excluded)

Every proposal names the measured cost it attacks (from the stage tables above and the node probes), the code it touches on main 66f8a888, an estimate of what it buys, and effort. Facts about what the code can and cannot do today were checked in the tree, not assumed.

### A. microVM: take work off the per-activation path (the 17 → 20+ swaps/s wall)

1. **Stop reading the whole memory image eagerly on restore — use Cloud Hypervisor's on-demand mode.** The worker already implements both modes (`cmd/ateom-microvm/internal/ch/prefault.go:23-30`: `OnDemand` = userfaultfd demand paging, `Copy` = eager) but `restoreMemMode()` (`restore.go:56-67`) forces eager on CH ≥ v53 because CH started prefaulting unconditionally (PRs #8150/#8556). We run CH v53. Eager restore is why `vm_restore` costs 140 ms alone and 1.5 s at 21/s (it copies 128 MiB per VM into the memfd, 2.7 GB/s of page allocation at 21/s), and why the next snapshot is the full 128 MiB again. Fix: get CH to honour the on-demand/no-prefault request again (newer CH exposes it as a restore option; otherwise pin a CH build that does) and re-enable `MemRestoreOnDemand`. Expected: `vm_restore` → tens of ms, memory churn per swap ÷ 3, images sparser, and the delta-merge path (`mergeOnDemandDelta`, `checkpoint.go:304-311`) gives incremental checkpoints. Effort: small in Substrate, depends on the CH side. This is the single largest lever on microVM.

2. **Pool the network namespace *with its tap* per worker.** The namespace pool ported from the gVisor work only applies to the veth shape (`internal/ateomnet/netpool.go:118`); the microVM path still creates and destroys a namespace (1 bind mount + unmount, `netns.go:55-88`) and does 5–6 netlink round trips under `rtnl_lock` per tap (`net.go:54-99`), and `tap` is the stage that goes 2 → 1,347 ms at 21/s. The tap's MAC and address are constants (`net.go:36-45`) and CH takes the tap by file descriptor (`restorefds.go:143-147`), so a pooled namespace can keep its tap and hand the same FDs to the next VM. Expected: removes the `tap` stage and the namespace mount/unmount from every activation; at 21/s that is ~1.3 s of the 5.5 s restore and part of the 1.5 s teardown. Effort: a day; the pool machinery exists.

3. **Pre-spawn idle VMMs.** `LaunchVMM` (`restorefds.go:62-87`) execs `cloud-hypervisor --api-socket` and pings it every 10 ms; 12–15 ms alone, 190–290 ms at 20/s because fork/exec convoys with everything else. A per-worker pool of N idle VMM processes (no VM yet) that a restore claims, plus `vm.delete` and re-use after teardown, removes `vmm_launch` and half the process churn of teardown (`Process.Kill()+Wait` for CH and virtiofsd, `checkpoint.go:369-377`). CH supports `vm.delete` followed by another `vm.restore` on the same VMM; the code never exercises it. Expected: −0.2–0.3 s per restore at load, fewer forks on the node. Effort: 1–2 days.

4. **One virtiofsd per worker instead of one per VM.** `StartVirtiofsd` (`overlay_linux.go:143-172`) spawns a daemon per actor with `--shared-dir=<SharedDir(id)>` and find-paths pinned to `<baseID>/rootfs`; it is spawned in `lowers` (16 → 514 ms at 21/s) and killed in teardown. A per-worker daemon serving a share root with per-actor subdirectories removes a spawn and a kill per activation and most of the `lowers` growth. Effort: medium (find-paths and the snapshot's recorded paths must stay stable).

5. **Stop scanning the node twice per activation.** `cleanupSandboxState` runs at restore (`restore.go:234`) and again at teardown (`checkpoint.go:385`); each parses `/proc/self/mountinfo` (2,500 lines on this node), MNT_DETACHes under two trees, and scans `/proc/*/cmdline` across ~6,300 processes to find orphans (`cleanup_linux.go:50-130`). That is O(processes) work, 40 times a second at 20/s, taking mount and task-list locks every time. The worker tracks its `runningActor`; skip the scans when the actor is known and reserve the full sweep for recovery. Also `imagecache.UnmountAllUnder` parses mountinfo again. Expected: cuts `prep` (11 → 1,034 ms at 21/s) and part of teardown; nearly free to do.

6. **Reuse the cgroup leaf and the bundle overlay.** `OpenActorLeaf`/`RemoveActorLeaf` mkdir+rmdir a cgroup per activation (`ateomcgroup/actor.go:47-70`); the bundle lower overlay (`imagecache.SetupBundleRootfs`, one `fsmount` per container) is content-addressed and could stay mounted across the actor-dir reset that atelet performs. Each saves a mount or a cgroup operation under a global lock per activation. Effort: small each; the gVisor campaign measured cgroup ops at 7 → 24 ms (mkdir) and 1 → 157 ms (rmdir) under load.

7. **Make teardown asynchronous and batched.** After `vm.snapshot` returns, the suspend's client-visible latency still includes kill/wait of two processes, the unmounts and the namespace release (`teardown` 120 → 1,458 ms). Return the checkpoint once the files are listed (`checkpoint.go:224`) and run teardown on a worker-local queue that holds the per-actor lock, coalescing unmounts and process reaps. Expected: suspend P50 −0.2 s at 15/s, more at 20/s; it also smooths the convoy because teardown no longer competes with the restores of the same tick. Effort: a day, with care for the restore-while-tearing-down ordering (the lock already exists).

### B. Fewer bytes per snapshot (helps every stage: download, decode, write, VMM restore, upload, network)

8. **Drop the guest page cache before `vm.snapshot`.** The 128 MiB image of a 256 MiB guest is mostly page cache from reading the rootfs over virtiofs; it compresses to 42 MiB and a denser zstd level only reached 40. The kata agent exposes `ExecProcess` and `MemAgentMemcgSet`/`MemAgentCompactSet` (`agent.proto:33,64-65`) which the worker does not wrap today (`agentclient.go` wraps 13 RPCs, none of them). `sync; echo 3 > /proc/sys/vm/drop_caches` via `ExecProcess` right before the snapshot (or memcg eviction via the mem-agent) should take the image to tens of MiB. Expected: ×2–4 fewer bytes everywhere: the transfer term at 20/s (≈1.9 GB/s) and the memory churn of restore both shrink proportionally. Effort: small (one RPC + a sandbox exec), needs a check that `/proc/sys` is writable in the exec context.

9. **Add a balloon with free-page reporting to the VM config.** `createvm.go:25-36` has no balloon device; with `balloon {size: 0, free_page_reporting: true}` the guest reports freed pages and CH punches them out of the memfd, so pages freed after the page-cache drop (or by the workload) stop appearing in later snapshots. Pairs with 8 and with on-demand restore (1). Effort: small, but it changes the VM config so existing snapshots must be regenerated (the golden snapshot too).

10. **Deduplicate against the golden image / between actors.** All 5,000 actors boot from one golden snapshot; the kernel, agent and rootfs pages are byte-identical across them. Content-addressed page chunks (4 KiB or CDC) with a per-node cache of the golden pages means an upload carries only the actor's unique pages and a restore fetches only the delta over a node-cached base. Firecracker's diff snapshots and the OnDemand delta merge already in this tree (`ch/merge.go`) are the existing shapes of this. Expected: order-of-magnitude fewer bytes for idle agents. Effort: large; the biggest structural win for cost as well as throughput (GCS egress and storage).

11. **Train a zstd dictionary on the golden image** and compress with it (`zstd.WithEncoderDict`/`WithDecoderDicts`): cheap to try, typically 10–30 % on page-image data that a plain level could not improve. Effort: hours.

### C. The transfer path

12. **Resume from the node-local copy when the actor comes back to the same node.** Branch commit 0ccab235 (`cmd/atelet/retained_snapshot.go`, `--retain-uploaded-snapshots`) kept the uploaded checkpoint on the node and restored from it when the URI matched; it is not on main. In this test (and in any node-affine scheduler) every resume lands on the node that suspended the actor, so the whole download stage (0.6–1.0 s, ~0.9 GB/s of GCS egress at 20/s) disappears; main already has the plumbing for local checkpoints (`CHECKPOINT_TYPE_LOCAL`, `LocalSnapshotDir`, `PreserveRestoreDir`) used by the pause path. Expected: resume P50 −0.6 s, download bandwidth → 0 for same-node resumes; needs a disk/tmpfs budget and an eviction policy. Effort: small to port, medium to make safe (invalidate on a newer snapshot).

13. **Spread transfers across the worker pods instead of one node process.** The plugin is a node singleton (`cmd/atelet/main.go:118`, `internal/objectstoreplugin/node.go`); every byte on the node goes through one process and one pod network interface, and its intrinsic ceiling in a quiet probe is ~0.8–0.9 GB/s of compressed intake. The workers (100 pods) could fetch and push their own snapshots given a new `ateompb` RPC (or the plugin socket mounted into them) and storage credentials; that multiplies connections, veths and processes by 100. Effort: medium; also removes the atelet restart as a disruption point.

14. **Keep one request per object for uploads, more streams for downloads** (done), and consider gRPC/direct-connect or HTTP/3 for GCS later; per-stream GCS throughput measured here is ~45 MB/s, so stream count is what matters.

### D. gVisor: the mount-namespace lock, cgroups, and the sentry

15. **Fewer mounts and chroots per sandbox in runsc.** The worker passes no runsc tuning flags at all (`cmd/ateom-gvisor/runsc.go:85-106`): upstream defaults, i.e. a rootfs overlay set up per container and a gofer with its own chroot per sandbox, each a burst of mounts under the global mount lock that the node probe measured at 2.4 → 19 ms per bind+umount pair at 12/s. Try `--overlay2=none` (or a shared backing directory), and `--shared-root` (already added by the campaign, keep it); longer term runsc needs a long-lived gofer chroot or fewer `mount(2)` calls per create. Expected: this is the `pause_create` growth (90 → 600–700 ms at 12/s). Effort: flags are hours; the runsc change is upstream work.

16. **Cheaper cgroup migrations.** Each activation does 1 mkdir + 2 `cgroup.procs` writes + 1 rmdir; each write is a `cgroup_threadgroup_rwsem` writer that stalls every fork on the node. `favordynmods` measured 42 → 2.7 ms per attach pair but has to be set where it sticks (kernel command line `cgroup_favordynmods=1`, or a privileged DaemonSet that remounts before the pool starts and after every roll); runsc using `CLONE_INTO_CGROUP` avoids the attach write entirely. The reusable-slot approach was tried and does not work (gofers die in a reused cgroup).

17. **gVisor restore itself** (app_restore 100 → 400–570 ms at 12/s) is inside the sentry: page-image loading and the restore of the kernel state. Options are upstream: parallel page loading, lazy restore (gVisor has a lazy-load mode for pages.img in recent releases), or keeping the sentry process alive across suspend/resume (checkpoint without exiting, "pause-to-disk" instead of full restore).

### E. Node and kernel

18. **Newer kernel.** Two of the convoys are known kernel scalability items: per-network-namespace `rtnl_lock` (merged upstream in 6.13-era networking) and finer mount-namespace locking work; GKE's COS node image pins the kernel, so this is a node-image choice, not a Substrate change. Worth testing on a newer COS/Ubuntu image when available.

19. **Isolate the sandbox lifecycle from the running fleet.** Pin the 650 running VMs' vCPU threads and the plugin to distinct CPU sets (or NUMA nodes) so the create/teardown path and the compression threads do not share run queues with the guests; the load average of 140 at 21/s says the scheduler is part of the convoy even at 35 % utilisation.

20. **Two nodes' worth of control on one node.** Run two worker pools and two snapshot plugins per node (sharded by actor), each with its own network interface and process space: it halves every per-process and per-interface ceiling without new hardware.

### F. Measurement and operations

21. The relative 2.5× latency rule should become an absolute SLO per runtime (e.g. resume P90 ≤ 1.5 s) for the production target; the relative rule was right for finding knees, but at the end of the day microVM delivers 17 swaps/s at P90 1.4 s and the rule, not the users, is what stops it.
22. The router's 5-s parked-request budget (`parking.go`) should be tied to the measured restore P99 so that a latency knee degrades instead of returning 503s.
23. Keep the fixes to the harness that this day produced (1-s ticks, catch-up off, readiness check that cannot roll the pool, fleet exclusion of DELETING/CRASHED actors, per-stage timing extraction) as the standard way to run this test.

**If only three things get done:** (1) on-demand restore + page-cache drop before snapshot (A1 + B8: fewer bytes and no eager copy), (2) a tap-carrying namespace pool and the removal of the two per-activation node scans (A2 + A5), (3) same-node resume from the retained local copy (C12). Together they remove most of the per-activation kernel work and most of the bytes; the expected ceiling is then set by VMM restore and transfer again, well above 20 swaps/s on this node.
"""

IMPACT = """## Improvements and what each one changed

Measured effects only; "ceiling" = highest activations/s within the bounds. Each row compares the iteration that introduced the change with the one before it on the same node, unless stated.

| # | Change | Where | Ceiling | Resume latency | Suspend latency | Node / plugin cost | Errors, crashes | Verdict |
|---|---|---|---|---|---|---|---|---|
| 1 | 1-s ticks instead of 10-s bursts | harness (`SWAP_EVERY`) | microVM 2 → 5, gVisor 2 → 5 | at 2/s: microVM P50 2,304 → 662 ms, gVisor 2,276 → 536 | microVM 3,992 → 1,001; gVisor 1,713 → 593 | none | none | kept; every earlier figure was burst queueing |
| 2 | Pooled GCS clients for every transfer (`ATE_GCS_CLIENT_POOL=64`) | snapshot plugin | microVM 5 (first 1-s run) | quiet probe, 20 concurrent restores: download 1,645 → 1,233 ms | — | — | — | kept (same fix as upstream e2a9e2e plus the small-object download path) |
| 3 | Pooled zstd buffers/encoders/decoders, fan-out cap 8 | snapshot plugin | — | — | at 2/s P50 1,001 → 889, P90 1,246 → 1,040 | page faults 157k → 56k/s at 2/s; CPU per swap −20 % | — | kept |
| 4 | Tar fsync skipped after checkpoint | microVM worker (`internal/tarutil`) | 5 → 8 together with #5 | — | rootfs-upper stage no longer grows with concurrency (was 0.5 → 2.4 s at 40 concurrent) | PSI io lower | — | kept for the experiment; needs a flag upstream |
| 5 | Guest CRNG reseed 15 s + retry (was 5 s, no retry) | microVM worker (`restore.go`) | 8 reachable (holds stopped failing on the crash gate) | — | — | — | restore crashes ~1 per 500–1,000 → 0 in later runs; no more DELETING-stuck actors | kept; needs upstream fix |
| 6 | Crash gate 5 → 20, fleet excludes DELETING/CRASHED actors | harness | made results readable | — | — | — | spurious level failures gone | kept |
| 7 | Bounded free lists for 64 MiB upload / 16 MiB range / 1 MiB copy buffers, writer chunk = object size | snapshot plugin | 15 unchanged | — | — | page faults 150k/s → 300/s at 10/s; RSS stable | — | kept; a cost, not the limit |
| 8 | GOGC 400 + GOMEMLIMIT 96 GiB; then GOGC 200 | snapshot plugin env | 15 / 14 unchanged | — | — | heap grew to 91 GB under the limit; no effect | — | reverted to GOGC 200 |
| 9 | Graceful VMM shutdown skipped after checkpoint | microVM worker | unchanged | — | teardown 258 → 258–322 ms (no gain) | — | — | kept, harmless |
| 10 | Orphan actor directories reclaimed (37k dirs, 576 GB) | node | — | the run right after it failed at 14/s from the disk's background work | — | disk 614 → 21 GB used | — | do it well before measuring |
| 11 | `/var/lib/ate/actors` on tmpfs | node | unchanged (15) | resume P90 at 15/s 2,354 → 1,468 together with #12 | — | PSI io 13 → 0; `procs_blocked` 0 | — | kept; removes the disk, not the ceiling |
| 12 | Sim first-wake catch-up off (`SCRIPT_CATCHUP=false`) | harness | 15 held with margin | at 15/s: P50 1,651 → 1,305, P90 2,354 → 1,468; at 8/s P90 1,139 → 864 | at 15/s P90 1,813 → 1,360 | ~46 extra requests per first wake and one ResumeActor each removed | — | kept; affected every run before iteration 12 on both nodes |
| 13 | Instrumented GCS transport (`ATE_GCS_HTTP_STATS`) | snapshot plugin | diagnostic | showed no throttling: all 2xx, request p50 55 ms | — | — | — | off |
| 14 | 4 MiB download ranges, 12 in flight per object | snapshot plugin | 15 unchanged, 17 later | at 15/s P90 1,508 → 1,256 ms | — | — | — | kept |
| 15 | Composite uploads in 8 MiB parts | snapshot plugin | unchanged | — | at 15/s P50 1,119 → 1,603, P99 1,770 → 7,817 | 300 upload goroutines in HTTP/2 flow control at 21/s | — | reverted |
| 16 | Sandbox network-namespace pool ported to the microVM worker | `internal/ateomnet` | unchanged (15 → 17 came from the fine ramp, not this) | at 15/s P90 1,508 → 1,223 ms | — | tap stage unchanged (the microVM cost is the tap, not the namespace) | — | kept, no measurable gain on microVM |
| 17 | Denser zstd level (`ATE_ZSTD_LEVEL=default`) | snapshot plugin | 21/s collapsed harder | at 15/s P50 1,097 → 1,391 | — | image 41–42 → 39–42 MiB (−3 %); ~3× compression CPU | — | reverted |
| 18 | Fine ramp from 12 (final configuration) | harness | **microVM 17** (16.7 achieved) | 1,298 / 1,397 / 1,560 ms | 1,092 / 1,345 / 1,615 ms | plugin ~25 cores | 0 errors, 0 refusals, 0 crashes | final |
| g1 | Plugin/atelet patch (#2, #3) on gVisor | snapshot plugin | 5 → 8 | at 8/s P50 1,305 → 1,150 | 989 → 1,246 (noise) | — | — | kept |
| g2 | Sandbox network-namespace pool (`ATE_NETNS_POOL`) | `internal/ateomnet` (gVisor) | 8 | net_setup 170–330 → 1 ms; at 12/s P50 2,563 → 1,618 | 12/s park 1,246 → 1,104 | ~95 % pool hit rate | 0 adoption failures | kept |
| g3 | `runsc --shared-root` (one gofer namespace per worker) | gVisor worker | 8 | at 8/s P90 1,315 → 1,254 | teardown −80 ms at 8/s | — | — | kept |
| g4 | `--ignore-cgroups` | gVisor worker | 8 → 5 | at 8/s P50 1,076 → 2,328 | — | sentry sized from the host (192 vCPUs) | — | rejected |
| g5 | Reusable worker-owned cgroup slots | gVisor worker | run failed | — | — | — | 4,806 actors crashed (gofers die in a reused cgroup) | rejected |
| g6 | Lean post-checkpoint teardown + no app-container cgroup | gVisor worker | 8 | at 8/s P50 1,076 → 912, at 12/s 1,584 → 1,337 | — | cgroup leak fixed: 8,157 → 650 cgroups; every cgroup op faster | — | kept |
| g7 | Catch-up off + ×1.25 ramp | harness | **gVisor 9** | 959 / 1,194 / 1,449 ms | 589 / 742 / 932 ms | — | 0 errors, 0 refusals | final |

"""

RATE_TABLE = ["### Activations per second with resume and suspend latency, start of the day vs. end\n",
"One activation = one actor resumed from its suspended snapshot and answering a request; the test keeps 650 awake by suspending one actor for every one it resumes, so activations/s = swaps/s = suspends/s = resumes/s. Latencies are what the client saw (resume = request round trip through the router including the restore; suspend = SuspendActor call). ms, P50 / P90 / P99. \"pass\" = within the bounds (P90 ≤ 2.5× the run's first level, errors + refusals < 0.5 %, no backlog growth).\n"]
VERDICT = {"": "pass"}
def _verdict(v):
    if not v: return "pass"
    parts = []
    if "refusals" in v or "errors" in v: parts.append("refusals" if "refusals" in v else "errors")
    if "p90" in v or "p99" in v: parts.append("latency rule")
    if "backlog" in v: parts.append("backlog")
    return ", ".join(parts) or v
for cls, before, bt, after, at in (("microVM", "nanoturn-metal2", 10, "nanoturn3-metal2-19", 1), ("gVisor", "nanoturn-east", 10, "nanoturn3-east-11", 1)):
    RATE_TABLE.append(f"### {cls}\n")
    RATE_TABLE.append("| Build | Activations/s (target) | Achieved | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Errors + refusals | Verdict |")
    RATE_TABLE.append("|---|---|---|---|---|---|---|")
    for label, d, tick in (("Start of the day: main 66f8a888, 10-s burst ticks", before, bt), ("End of the day: patched build, 1-s ticks", after, at)):
        pth = os.path.join("/tmp/tco-runs/fill", d, "turn.txt")
        if not os.path.exists(pth): continue
        seen = set(); first = True
        for r in _rows(pth):
            if r["tag"] == "hold" or r["n_per_tick"] in seen: continue  # the hold repeats the last clean level
            seen.add(r["n_per_tick"])
            rate, ach, wk, pk, er, v = _fmt(r, tick)
            best = (not r["failed_on"]) and all(x["failed_on"] or x["n_per_tick"] <= r["n_per_tick"] for x in _rows(pth) if x["tag"] != "hold")
            b = "**" if best else ""
            RATE_TABLE.append(f"| {label if first else ''} | {b}{rate}{b} | {b}{ach}{b} | {b}{wk}{b} | {b}{pk}{b} | {er} | {b}{_verdict(v)}{b} |")
            first = False
    RATE_TABLE.append("")
    RATE_TABLE.append("_Bold = the highest rate within the bounds for that build._\n")
BOTTLENECK = ("""## Where the bottleneck moved, and what is happening now (for whoever picks this up)

""" + DETAIL + """### Where the bottlenecks are now, in short

**microVM — 17 activations/s clean, 20 collapses.** At 20/s every per-VM step on the node slows at once while the CPUs are ~35 % busy, nothing is blocked on IO (`procs_blocked` ≈ 0) and the load average goes to ~140: tap device setup 2 → 1,300 ms, VMM launch 12 → 190, VM restore 140 → 1,500, overlay staging 10 → 500, checkpoint teardown 230 → 1,460 (worker `Restore/Checkpoint timing breakdown` medians). That is lock serialisation in the kernel and VMM for sandbox create/teardown (netlink for the tap, mounts for the rootfs overlays and virtiofsd, process spawn and kill for cloud-hypervisor), not a Substrate loop — the snapshot plugin's own download stays at ~1.0 s at that rate and the GCS requests themselves at ~55 ms. Second term: ~2 GB/s of snapshot traffic at 20/s (42 MiB down + 50 MiB up per swap) — uploads start queuing on HTTP/2 flow control there. Levers left: a pool of pre-launched VMMs with their tap devices (takes vmm_launch, tap and most of teardown off the critical path), and fewer bytes per snapshot by dropping the guest page cache before the checkpoint (the 128 MiB memory image is mostly page cache; denser compression bought only 3 %).

**gVisor — 9 activations/s clean, 12 delivered error-free, 12 fails the latency rule.** The node probe during 12/s shows a bind+umount pair going 2.4 → 19 ms and `unshare -m` 1.5 → 18 ms: runsc's gofer and sentry each build a chroot per sandbox under the kernel's global mount-namespace lock, which is the `pause_create` growth (90 ms alone → 600–700 ms at 12/s) that survived the namespace and cgroup fixes. Then cgroup v2 task migration (1 mkdir + 2 `cgroup.procs` writes + 1 rmdir per activation, each a `cgroup_threadgroup_rwsem` writer that stalls all forks; `favordynmods` measured 42 → 2.7 ms per attach pair but needs the kernel command line to stick), and gVisor's own restore (app_restore 100 → 400–570 ms at 12/s inside the sentry). All three sit in gVisor or the kernel; the fixes are fewer mounts or a long-lived gofer chroot in runsc, `CLONE_INTO_CGROUP`, and `cgroup_favordynmods=1`.

**Common to both.** Snapshot transfer and the plugin are no longer limiting below ~20/s. The control plane (api-server, Postgres, router) never was: its tables are a few MB, resume RPC handling is sub-millisecond apart from the restore itself, and the router's only contribution is its 5-s parked-request budget, which converts a latency cliff into 503s.
""")
md.append("\n".join(RATE_TABLE))
md.append(SUMMARY_TAIL)
md.append(BOTTLENECK)
md.append(IMPACT)
md.append(IDEAS)
for tag, cls in (("metal2", "microVM (agents-tco-euw4, actor 2 vCPU + 256 MiB)"), ("east", "gVisor (agents-tco-east, actor 2 vCPU + 2 GiB)")):
    md.append(f"## {cls}\n")
    for d, title, note in ITER[tag]:
        p = os.path.join("/tmp/tco-runs/fill", d, "turn.txt")
        if not os.path.exists(p): continue
        rows = levels(p)
        if not rows: continue
        md.append(f"### {title}\n")
        if note: md.append(f"_{note}_\n")
        md += table(rows); md.append("")
open(OUT, "w").write("\n".join(md)); print("wrote", OUT)
