#!/usr/bin/env python3
"""nano-personal-agent on Substrate main: cold start + turnover at 650 awake of 5000 + cycle → markdown with every
assumption/configuration. Inputs: /tmp/tco-runs/fill/nanoturn-<tag>/{cold.txt,simlog-cold.txt,turn.txt,simlog-turn.txt,
bucket-early.txt,bucket-final.txt,hostprobe-turn.log}."""
import datetime, json, csv, io, os, re, json, collections, statistics as st
OUT = "/Users/adityashantanu/Downloads/nano-personal-agent-cold-start-and-turnover-5000-gvisor-vs-microvm-main-2026-10-08.md"
MIB = 2**20
CELLS = [("east", "gVisor", "agents-tco-east (us-east4-a)", "2 vCPU, 2 GiB"), ("metal2", "microVM", "agents-tco-euw4 (europe-west4-c)", "2 vCPU, 256 MiB")]
def read(p): return open(p, errors="replace").read() if os.path.exists(p) else ""
def cold(D):
    t = read(D + "/cold.txt") + read(D + "/simlog-cold.txt")
    m = re.search(r"ready ms\s+p50=(\d+) p90=(\d+) p99=(\d+) max=(\d+)", t)
    rows = re.findall(r"^(\d{13}),(\d+),(\d+),(\d+),(\d+),(\d+),(\d+),(\d+),", t, re.M)
    return (m.groups() if m else None), rows
def levels(D):
    t = read(D + "/turn.txt") + read(D + "/simlog-turn.txt")
    m = re.search(r"=== swap levels ===\n(.*?)=== end swap ===", t, re.S)
    rows = list(csv.DictReader(io.StringIO(m.group(1)))) if m else []
    if not rows:  # the job was ended before the sim printed its table: rebuild the rows from the captured JSON lines
        tag = ""
        for line in t.splitlines():
            if '"msg":"swap hold"' in line: tag = "hold"
            elif '"msg":"swap cycle"' in line and '"msg":"swap cycle progress"' not in line: tag = "cycle"
            elif '"msg":"swap level result"' in line:
                try: d = json.loads(line)
                except Exception: continue
                d = {k: ("" if v is None else str(v)) for k, v in d.items()}; d["tag"] = tag; rows.append(d)
    v = re.findall(r"SWAP VERDICT: (.*)", t); fill = re.search(r'"msg":"swap fill done".*?"took":"([^"]+)".*?"resident":(\d+).*?"parked":(\d+).*?"fill_failures":(\d+)', t)
    cyc = re.search(r'"msg":"swap cycle done","msg":"([^"]+)"', t)
    setup = re.search(r'"msg":"setup complete","agents":(\d+),"failed":(\d+)', t)
    return rows, (v[-1] if v else ""), fill, (cyc.group(1) if cyc else ""), setup
def ts(s): return datetime.datetime.strptime(s[:19], "%Y-%m-%dT%H:%M:%S")
def cycle(D):
    """Timeline of the cycle phase from the JSON lines: start, already-resident-once count, plateau count and when it was reached."""
    t = read(D + "/turn.txt") + read(D + "/simlog-turn.txt")
    start = re.search(r'\{"time":"([^"]+)".*?"msg":"swap cycle","n_per_tick":(\d+),"already_resident_once":(\d+),"pool":(\d+)', t)
    filld = re.search(r'\{"time":"([^"]+)".*?"msg":"swap fill done"', t)
    prog = [(ts(a), int(b), c) for a, b, c in re.findall(r'\{"time":"([^"]+)".*?"msg":"swap cycle progress","resident_once":(\d+),"pool":\d+,"elapsed":"([^"]+)"', t)]
    if not start or not prog: return None
    top = max(b for _, b, _ in prog); first = next(x for x in prog if x[1] == top); prev = [x for x in prog if x[1] < top]
    return {"start": ts(start.group(1)), "n": int(start.group(2)), "already": int(start.group(3)), "pool": int(start.group(4)), "top": top, "at": first[0], "elapsed": first[2],
            "lower": (prev[-1][0] if prev else ts(start.group(1))), "last": prog[-1][0], "fill_done": ts(filld.group(1)) if filld else None}
def bucket(p):
    per = collections.defaultdict(lambda: collections.defaultdict(dict))
    for line in read(p).splitlines():
        m = re.match(r"\s*(\d+)\s+(\S+)\s+gs://\S+/actors/([^/]+)/snapshots/([^/]+)/(\S+)", line)
        if m: per[m[3]][m[4]][m[5]] = int(m[1])
    tot, parts = [], collections.defaultdict(list)
    for uid, snaps in per.items():
        for sid, files in snaps.items():
            tot.append(sum(files.values()))
            for n, b in files.items(): parts[n].append(b)
    return tot, parts, len(per)
def f0(x):
    try: v = float(x); return "—" if v != v else f"{v:,.0f}"
    except Exception: return "—"
def disk(p):
    rows, cur = [], None
    for line in read(p).splitlines():
        q = line.split()
        if not q: continue
        if q[0] == "T": cur = {"ts": int(q[1])}
        elif cur is None: continue
        elif len(q) >= 14 and q[2] == "nvme0n1": cur["d"] = [int(x) for x in q[3:]]
        elif q[0] in ("MemTotal:", "MemAvailable:"): cur[q[0][:-1]] = int(q[1]) * 1024
        elif q[0] == "cpu": cur["cpu"] = [int(x) for x in q[1:]]
        elif q[0] == "END": rows.append(cur); cur = None
    out = []
    for a, b in zip(rows, rows[1:]):
        if "d" not in a or "d" not in b or "cpu" not in a: continue
        dt = b["ts"] - a["ts"]; da, db = a["d"], b["d"]
        if dt <= 0: continue
        tot = sum(b["cpu"]) - sum(a["cpu"]); idle = (b["cpu"][3] + b["cpu"][4]) - (a["cpu"][3] + a["cpu"][4])
        out.append({"w": (db[6] - da[6]) * 512 / dt / MIB, "r": (db[2] - da[2]) * 512 / dt / MIB, "util": 100 * (db[9] - da[9]) / (dt * 1000), "aqu": (db[10] - da[10]) / (dt * 1000),
                    "cpu": 100 * (1 - idle / tot) if tot else 0, "mem": (b.get("MemTotal", 0) - b.get("MemAvailable", 0)) / 2**30})
    return out
def pq(v, p): v = sorted(v); return v[min(len(v) - 1, int(p * len(v)))] if v else 0
out = ["# nano-personal-agent on Substrate main — cold start, and suspend/resume turnover at 650 awake of 5,000 registered (measured 2026-10-08)\n",
"## What was asked\n",
"Suspend path only, gVisor and microVM on the two bare-metal nodes, Substrate from **main**, `nano-personal-agent` workload. Measure P50/P90/P99 of (1) cold start, (2) resume, (3) suspend. Structure: cold starts as an independent step; then register 5,000 agents, keep 650 awake, find the maximum sustainable activation throughput with N replacements per tick (N suspends + N resumes), and at that rate cycle through all remaining agents, recording the total time.\n",
"## Assumptions and configuration (everything that shaped the numbers)\n",
"- **Substrate:** upstream `main` @ 66f8a888 (2026-10-08, \"Egress gateway can mint and cache actor JWTs (#2135)\"), fresh install on both clusters (the September-branch control plane, its Postgres and its SandboxConfigs were removed first). gVisor: `gvisor-default` SandboxConfig from main (gVisor nightly 2026-09-02). microVM: main's `microvm` SandboxConfig with kata 4.1.0 + Cloud Hypervisor v53.0 assets assembled on the node and verified against main's pinned sha256. No retained node-local snapshot copy on main: every resume from suspend downloads from the bucket.",
"- **Machines:** one GKE bare-metal node per runtime, c3-standard-192-metal (192 vCPU, 768 GiB, 3 TB Hyperdisk Balanced boot disk at 100k IOPS / 2,400 MiB/s, COS, GKE 1.36.4). Node tuning carried over from earlier runs: `net.ipv{4,6}.neigh.default.gc_thresh` 4096/8192/16384 (the ARP table overflowed at ~1,000 live sandboxes at the COS default) and `fs.inotify.max_user_{instances,watches}` 65536/1048576 (exhausted at ~800 sandboxes at the default). Orphaned node directories from previous runs were reclaimed before the run; Substrate itself does not reclaim them (#641).",
"- **Workload:** `nano-personal-agent` — the personal-assistant day (61 events over 24 h, real think gaps) with every action mocked: model call = dwell ≤ 4 s with the actor resident, API call = a ping, file open = read or ≤ 32 KiB write of KiB-sized files (256 KiB session store and index), CPU burns ≤ 50 ms, **no synthetic heap**. Per day: 4.75 CPU-s, 4.8 MiB written, 848 KiB of files, declared RAM 0. Resident agents play it at **think ×1** (real pace): ~0.5 mocked actions per second across 650 agents. The driver never parks after a step (idle mode; autosuspender idle timeout 24 h): the only suspends/resumes are the test's swaps.",
"- **Actor shape:** gVisor 2 vCPU + 2 GiB limit; microVM 2 vCPU + **256 MiB** (the memory limit is the guest's RAM on microVM and the guest's touched pages are the snapshot, so it was kept at the minimum, per Aditya). Worker pool: 100 unsized worker pods per node (multi-actor, `--max-actors` 1000).",
"- **Registration:** 5,000 actors created (cold boot from the golden snapshot, first ping, one suspend to the bucket), 32 in flight. **Fill:** 650 woken from the pool, 8 in flight, retried until resident. Pool for swaps: the other 4,350.",
"- **Turnover ramp:** every 10 s park N awake actors (oldest-awake first, never one mid-step) with SuspendActor and wake N parked ones (oldest-parked first) with a request; N = 10, 20, 40, 80, 160, 320, 640 per tick (1 … 64 swaps/s), one level per 4 minutes, levels back to back. Gates per level (any one fails it): wake P90 or suspend P90 above 2.5× the first level; wake P99 above 5 s; errors or router refusals above 0.5 % of wakes (≥ 3 occurrences); more than two ticks' worth of swaps still in flight at the end of the level (backlog); node MemAvailable < 10 %, memory PSI full > 10 %, CPU PSI some > 50 %; ≥ 5 crashed actors. After the first failed level: 10-minute hold at the last clean N, then the **cycle**: keep swapping at that N until every one of the 5,000 has been resident at least once; the cycle's wall time and latencies are recorded.",
"- **Cold start:** 100 fresh actors per runtime, 4 in flight, create → golden-snapshot restore → first answered ping.",
"- **Latency definitions:** resume = the wake request's round trip through the router including the restore (client-side, up to 5 refusal retries); suspend = the SuspendActor call's wall time (checkpoint + upload); cold start as above. Host figures from a node probe every 15 s; actor states and node memory/PSI from the autosuspender every 5 s.\n"]
CAVEATS = {
 "east": "The sim's resident-once count stopped at 4,999 and its cycle loop waited for the last actor until the run was ended 10 min later. The actor states after the run show every one of the 5,000 was in fact resumed at least once (actor version histogram 1 × 7, 3,619 × 9, 649 × 11, 731 × 13 — none left at the registration version; the one at version 7, turn-…-4499, is RUNNING since its fill wake and was never parked), so this is a bookkeeping gap in the sim's fill path, not a Substrate failure. The sim's cumulative counter showed 1 error for the whole run; every measured level shows 0 wake/suspend errors and 0 step errors. The sim's final quantile table was not printed (job ended), so the level rows above come from the sim's per-level JSON lines, which carry the same fields.",
 "metal2": "Actors crashed over the run: 2 during the 650 fill (their first wake), 4 by the end of the hold (the hold row also shows 2 wake errors), 13 by the time the run was ended (10 of them never got past their first wake; the gate was ≥ 5 crashed per level and no level after the hold was scored). The sim's resident-once count plateaued at 4,995 of 5,000, so 5 actors were never counted resident; the cycle loop waited for them and the run was ended 3 min after the plateau. 20 actors were left SUSPENDING (their parks were in flight when the job was ended); final states are in actors-final.txt in the archive. The sim's final quantile table was not printed (job ended); level rows come from the per-level JSON lines.",
}
SUMMARY = {}
for tag, cls, node, shape in CELLS:
    D = f"/tmp/tco-runs/fill/nanoturn-{tag}"
    if not os.path.isdir(D): continue
    out.append(f"## {cls} — {node}, actor {shape}\n")
    c, rows = cold(D)
    out.append(f"**1. Cold start (100 actors, 4 in flight):** P50 / P90 / P99 / max = **{c[0]} / {c[1]} / {c[2]} / {c[3]} ms**\n" if c else "**1. Cold start:** not captured\n")
    L, verdict, fill, cyc, setup = levels(D)
    if setup: out.append(f"**Registration:** {setup.group(1)} actors created, {setup.group(2)} failed." + (f" **Fill:** {fill.group(2)} resident, {fill.group(3)} parked, {fill.group(4)} fill failures, took {fill.group(1)}.\n" if fill else "\n"))
    tot, parts, nact = bucket(D + "/bucket-early.txt")
    if tot:
        out.append(f"**Snapshot in the bucket (suspend, zstd; {len(tot)} snapshots of {nact} actors sampled during registration):** mean **{st.fmean(tot)/MIB:.1f} MiB**, p50 {st.median(tot)/MIB:.1f}, max {max(tot)/MIB:.1f}. Composition (mean per snapshot): " + ", ".join(f"{n} {st.fmean(v)/MIB:.2f} MiB" for n, v in sorted(parts.items(), key=lambda kv: -st.fmean(kv[1]))) + ".\n")
    tot2, parts2, nact2 = bucket(D + "/bucket-final.txt")
    if tot2:
        out.append(f"**Snapshot at the end of the run ({len(tot2)} snapshots of {nact2} actors):** mean {st.fmean(tot2)/MIB:.1f} MiB, max {max(tot2)/MIB:.1f}; " + ", ".join(f"{n} {st.fmean(v)/MIB:.2f} MiB" for n, v in sorted(parts2.items(), key=lambda kv: -st.fmean(kv[1]))[:3]) + ".\n")
    if L:
        out.append("**2 + 3. Resume and suspend under turnover (one level = 4 min; hold = 10 min; cycle = until all 5,000 have been resident once):**\n")
        out.append("| Level | N per 10 s | Target swaps/s | Achieved swaps/s | Resume P50 / P90 / P99 ms | Suspend P50 / P90 / P99 ms | Wakes / suspends | Errors + refusals | Backlog | Node mem avail | PSI cpu / mem / io | Verdict |")
        out.append("|---|---|---|---|---|---|---|---|---|---|---|---|")
        for l in L:
            lab = {"": "ramp 4 min", "hold": "hold 10 min", "cycle": "cycle"}.get(l["tag"], l["tag"])
            out.append(f"| {lab} | {l['n_per_tick']} | {l['target_swaps_per_s']} | **{l['achieved_swaps_per_s']}** | {f0(l['wake_p50_ms'])} / {f0(l['wake_p90_ms'])} / {f0(l['wake_p99_ms'])} | {f0(l['park_p50_ms'])} / {f0(l['park_p90_ms'])} / {f0(l['park_p99_ms'])} | {l['wakes']} / {l['parks']} | {l['errors']} + {l['refusals']} | {l['backlog']} | {l['mem_avail_pct']} % | {l['psi_cpu_some10']} / {l['psi_mem_full10']} / {l['psi_io_some10']} | {l['failed_on'] or 'pass'} |")
        out.append("")
    if verdict: out.append(f"**Verdict line:** {verdict}\n")
    if cyc: out.append(f"**Cycle through all 5,000:** {cyc}\n")
    cy = cycle(D)
    if cy:
        new = cy["top"] - cy["already"]; secs = (cy["at"] - cy["start"]).total_seconds(); lo = (cy["lower"] - cy["start"]).total_seconds()
        short = cy["pool"] - cy["top"]
        out.append(f"**Cycle phase (measured from the sim's progress lines, 70-s sampling):** started at {cy['start']:%H:%M:%S} UTC at {cy['n']} per 10 s with {cy['already']:,} of {cy['pool']:,} actors already resident at least once (fill + ramp + hold); reached **{cy['top']:,} of {cy['pool']:,}** at {cy['at']:%H:%M:%S} UTC, i.e. **{new:,} further actors in {lo/60:.1f}–{secs/60:.1f} min** ({new/secs:.2f} swaps/s; the resident 650 kept playing their day throughout)."
                   + (f" The remaining **{short}** actor{'s' if short != 1 else ''} never became resident (see caveats); the cycle loop kept waiting for {'them' if short != 1 else 'it'} until the run was ended at {cy['last']:%H:%M:%S} UTC." if short else "")
                   + (f" Wall time from the end of the 650 fill to the last actor being resident once: **{(cy['at'] - cy['fill_done']).total_seconds()/60:.1f} min** (this includes the 4-min ramp levels at 1, 2 and 4 swaps/s and the 10-min hold)." if cy["fill_done"] else "") + "\n")
    if tag in CAVEATS: out.append(f"**Caveats:** {CAVEATS[tag]}\n")
    clean = [l for l in L if not l["failed_on"] and l["tag"] == ""]; hold = [l for l in L if l["tag"] == "hold"]; best = hold[-1] if hold else (clean[-1] if clean else None)
    fails = [l for l in L if l["failed_on"]]
    SUMMARY[tag] = {"cls": cls, "cold": c, "best": best, "fail": fails[0] if fails else None, "cy": cy, "snap": (st.fmean(tot)/MIB if tot else None), "snap2": (st.fmean(tot2)/MIB if tot2 else None)}
    dk = disk(D + "/hostprobe-turn.log")
    if dk: out.append(f"**Host over the run (15-s samples):** disk write {pq([x['w'] for x in dk], .5):,.0f} / {pq([x['w'] for x in dk], .9):,.0f} MiB/s (p50 / p90), read {pq([x['r'] for x in dk], .5):,.0f} / {pq([x['r'] for x in dk], .9):,.0f}, busy {pq([x['util'] for x in dk], .5):.0f} / {pq([x['util'] for x in dk], .9):.0f} %, queue depth {pq([x['aqu'] for x in dk], .5):.0f} / {pq([x['aqu'] for x in dk], .9):.0f}; CPU {pq([x['cpu'] for x in dk], .5):.0f} / {pq([x['cpu'] for x in dk], .9):.0f} %; memory used max {max(x['mem'] for x in dk):.0f} GiB.\n")
# ---- summary table, inserted after "What was asked"
if SUMMARY:
    T = ["## Summary\n", "| | " + " | ".join(v["cls"] for v in SUMMARY.values()) + " |", "|---|" + "---|" * len(SUMMARY)]
    def row(name, f): T.append(f"| {name} | " + " | ".join(f(v) for v in SUMMARY.values()) + " |")
    row("Cold start P50 / P90 / P99 (ms)", lambda v: f"**{v['cold'][0]} / {v['cold'][1]} / {v['cold'][2]}**" if v["cold"] else "—")
    row("Snapshot in the bucket, zstd (mean, registration → end of run)", lambda v: f"**{v['snap']:.1f} → {v['snap2']:.1f} MiB**" if v["snap"] and v["snap2"] else (f"{v['snap']:.1f} MiB" if v["snap"] else "—"))
    row("Sustainable turnover (last clean level, held 10 min)", lambda v: f"**{v['best']['achieved_swaps_per_s']} swaps/s** ({v['best']['n_per_tick']} suspends + {v['best']['n_per_tick']} resumes per 10 s)" if v["best"] else "—")
    row("Resume P50 / P90 / P99 at that rate (ms)", lambda v: f"**{f0(v['best']['wake_p50_ms'])} / {f0(v['best']['wake_p90_ms'])} / {f0(v['best']['wake_p99_ms'])}**" if v["best"] else "—")
    row("Suspend P50 / P90 / P99 at that rate (ms)", lambda v: f"**{f0(v['best']['park_p50_ms'])} / {f0(v['best']['park_p90_ms'])} / {f0(v['best']['park_p99_ms'])}**" if v["best"] else "—")
    row("First failing level", lambda v: f"{v['fail']['n_per_tick']} per 10 s ({v['fail']['achieved_swaps_per_s']} swaps/s): {v['fail']['failed_on']}; errors {v['fail']['errors']}, refusals {v['fail']['refusals']}" if v["fail"] else "—")
    row("Cycle: actors resident at least once", lambda v: f"**{v['cy']['top']:,} of {v['cy']['pool']:,}**" if v["cy"] else "—")
    row("Cycle phase at the sustainable rate (from the hold's end)", lambda v: f"**{(v['cy']['at'] - v['cy']['start']).total_seconds()/60:.1f} min** for the last {v['cy']['top'] - v['cy']['already']:,}" if v["cy"] else "—")
    row("Fill end → last actor resident once (ramp + hold + cycle)", lambda v: f"**{(v['cy']['at'] - v['cy']['fill_done']).total_seconds()/60:.1f} min**" if v["cy"] and v["cy"]["fill_done"] else "—")
    T.append("")
    i = out.index("## Assumptions and configuration (everything that shaped the numbers)\n"); out[i:i] = T
out.append("""## Reading the numbers

- **Activations per second = swaps per second** at a constant 650 awake: each swap is one SuspendActor (checkpoint + bucket upload) and one resume (bucket download + restore + first request). The last clean ramp level is the sustainable rate; the first failing level shows what gives out.
- **Why the nano agent changes the picture:** the suspend path moves ~10 MiB (gVisor) or ~50 MiB (microVM at 256 MiB) per swap instead of 1–2.5 GiB, so the boot disk — the limiter of every earlier run — stops being the constraint and the control plane / router / per-worker wake path take over.
- **Cycle time** is the wall time to get the remaining 4,350 parked agents resident at the sustainable rate, with the resident agents still playing their day.
- **Caveats** are listed next to each cell where they apply.

## Observations

- **Latency grows linearly with N per tick, on both runtimes.** gVisor resume P50 1.2 / 2.2 / 4.1 s at N = 10 / 20 / 40, suspend P50 1.0 / 1.4 / 2.5 s; microVM resume 1.3 / 2.3 / 3.0 s, suspend 2.2 / 3.9 / 8.1 s. All N requests of a tick are issued at the same instant and the latencies are the bursts' queueing: they complete at roughly 100–110 ms per gVisor resume and ~200 ms per microVM suspend, i.e. a largely serialised path that drains about 9–10 resumes/s (gVisor) and ~5 suspends/s (microVM) — not a per-request cost of seconds. A single cold start is 0.3 s (gVisor) / 0.6 s (microVM) on the same hosts.
- **The gate that stopped the ramp was the relative-latency gate, not an error.** The N = 40 level (3.83 swaps/s achieved) completed with 0 errors, 0 refusals and no backlog on both runtimes; it failed because P90 was > 2.5× the N = 10 level (and, on microVM, resume P99 > 5 s). So **2 swaps/s is the sustainable rate under the latency rule as written; the systems delivered 3.8 swaps/s error-free.** A finer step (25, 30 per 10 s) or spreading the N swaps across the tick instead of a burst would land the gated limit between 2 and 4 swaps/s.
- **The host is nowhere near its limits.** CPU 5–10 % on both nodes; gVisor disk write 25 / 45 MiB/s (p50 / p90), queue depth 1–2; microVM 66 / 228 MiB/s with queue depth 5 / 45 at the bursts (the 128 MiB memory-ranges file is staged on disk before compression) — against 1–1.6 GiB/s and queue depths in the hundreds in every personal-assistant run; > 82 % of memory available. The nano agent's 10 MiB (gVisor) / 50 MiB (microVM) snapshot takes the boot disk out of the equation; what remains is the control plane / router / worker wake path.
- **Cycle:** at 2 swaps/s the remaining pool drains at the expected pace — 4,350 agents in ~36 min from the end of the fill (gVisor 36.2 min, microVM 36.3 min measured from the fill's end, including the slower first level and the faster failed level; the cycle phase alone moved the last ~1,560 agents in ~14 min on both).
""")
out.append("""
## Where the time goes (follow-up, measured after the run)

The load generator issues all N wakes and N suspends of a tick concurrently (one goroutine each, no client-side limit), so the N-linear latency is on the Substrate side. Two sources, both per node, both confirmed from Substrate's own timing lines (`Restore timing breakdown` / `Checkpoint timing breakdown` in the worker pods and the node agent) and from a quiet probe after the run (1 wake, then 20 at once, nothing else running):

| Stage (median ms) | gVisor N=10 | gVisor N=20 | gVisor N=40 | microVM N=10 | microVM N=20 | microVM N=40 |
|---|---|---|---|---|---|---|
| Resume as seen by the client | 1,243 | 2,200 | 4,089 | 1,257 | 2,273 | 3,030 |
| Worker restore total | 848 | 1,376 | 2,449 | 176 | 600 | 732 |
| — of which network namespace setup | 293 | 495 | 800 | — | — | — |
| — of which pause-sandbox create | 265 | 460 | 847 | — | — | — |
| — of which page / VM restore | 167 | 316 | 536 | 97 | 161 | 471 |
| Suspend as seen by the client | 951 | 1,399 | 2,507 | 2,209 | 3,931 | 8,128 |
| Worker checkpoint total | 349 | 408 | 577 | 583 | 1,235 | 3,314 |
| — of which checkpoint itself | 48 | 51 | 61 | 89 | 90 | 113 |
| — of which network teardown | 299 | 354 | 510 | 109 | 224 | 720 |
| — of which rootfs upper-layer tar | — | — | — | 475 | 1,006 | 2,448 |

Quiet probe (no other traffic): gVisor 1 wake 778 ms (manifest 265, download 281, worker 231); 20 at once 1,377 ms median (manifest 67, **download 496**, worker 778 of which pause-create 249 and page restore 208). microVM 1 wake 801 ms (manifest 167, download 463, worker 170); 20 at once 1,938 ms median, all 20 finishing within 250 ms of each other (manifest 66, **download 1,645**, worker 215). The single wake on an idle node is 0.8 s on both runtimes, not 0.3 s: resume from suspend is a bucket download plus a restore, while the 0.3/0.6 s cold start is a restore from the node-cached golden snapshot.

1. **One HTTP/2 connection per node for every snapshot transfer.** The `snapshot-plugin` sidecar of the node agent holds a single GCS `storage.Client`; its pooled multi-connection path is only used for objects over 16 MiB (download) / 64 MiB (upload), so the 10 MiB gVisor and 50 MiB microVM snapshots all share one TCP connection and finish together at N × size ÷ one-flow bandwidth. microVM: 20 × 50 MiB = 1 GB in 1.65 s ≈ 600 MB/s for the whole node. This is the dominant term for microVM wakes (1.6 of 2.3 s at N=20) and for microVM suspends (upload of 50 MiB each, plus the upper-layer tar). Code: `pkg/objectstorage/gcs.go:31-38`, `gcsranged.go:47-58`, `rangedget.go:35`, `gcscompose.go:35,68` on main 66f8a888.
2. **Kernel network-namespace create/teardown convoy (gVisor).** Each gVisor wake creates two named netns, a veth pair and an nftables table (`internal/ateomnet/sandbox.go:86-250`); each suspend tears them down. Those syscalls serialise on node-global kernel locks (`rtnl_lock`, `pernet_ops_rwsem`, the single `cleanup_net` worker) across all 100 worker pods, so N simultaneous wakes + N suspends queue there: netns setup 11 ms → 293 / 495 / 800 ms and pause-sandbox create 75 → 265 / 460 / 847 ms at N = 10 / 20 / 40, and checkpoint teardown 299 → 510 ms. No CPU or disk signal, which is why the host looked idle. The microVM path pays less here (one tap per VM) but its checkpoint tars the rootfs upper layer on the shared boot disk (475 ms → 2.4 s at N=40).
3. **Not the bottleneck:** control plane and router. Placement samples two random workers (no packing; the 20 probe wakes landed on 19 different pods), the worker-row lock is held for a few statements, the actor lease is per actor, there is no reconciler, rate limiter or semaphore on the request path, and the router's 100 ms retry cadence only applies to retryable errors. The router does carry a **5 s parked-request budget** (and a 5 s Envoy ext-proc timeout): at N=40 the P99 was 4.6 s, so around N≈45 per 10 s wakes start failing with 503 regardless of the latency gate.

What would move the number: give the snapshot plugin per-transfer connections (or route small objects through its existing 8-client pool) — directly multiplies microVM wake/suspend throughput; cap or amortise netns create/teardown per node (or stop paying two namespaces per actor) for gVisor; spread the N swaps across the tick instead of a burst (the sim's choice, not Substrate's) to turn the convoy into a pipeline.
""")
open(OUT, "w").write("\n".join(out)); print("wrote", OUT)
