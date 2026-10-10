"""Resident fill with the nano-personal-agent: how many awake agents one metal node sustains (gVisor vs microVM)."""
import json, os, re
OUT = os.path.expanduser("~/Downloads/resident-fill-nano-personal-agent-gvisor-vs-microvm-metal-2026-10-10.md")
F = "/tmp/tco-runs/fill"
HDR = "tag,active_agents,activations,refusals,errors,wake_p50_ms,wake_p90_ms,wake_p99_ms,turn_p90_ms,turn_p99_ms,probe_p50_ms,probe_p90_ms,probe_p99_ms,mem_avail_pct,psi_cpu_some10,psi_mem_full10,psi_io_some10,running,crashed,failed_on".split(",")
GATES = {"mem-avail": "memory available < 10 %", "psi-mem": "memory PSI full > gate", "psi-mem-full": "memory PSI full > gate", "psi-cpu": "CPU PSI some > 50 %", "probe-p99": "CPU probe P99 > 1 s", "turn-p99": "turn P99 > gate", "wake-p99": "wake P99 > gate", "wake-p90-vs-baseline": "wake P90 > 2× first wave", "turn-p90-vs-baseline": "turn P90 > 2× first wave", "probe-p90-vs-baseline": "probe P90 > 2× first wave", "refusals": "refusals > gate", "errors": "errors > gate", "crashed": "crashed actors > gate", "backlog": "backlog"}
def verdict(v): return "pass" if not v else ", ".join(GATES.get(k, k) for k in v.split("+"))
def num(x):
    try: return float(x)
    except Exception: return x
def rows(d):
    """wave results (json) + hold result (csv) of one run dir, chronological."""
    out, seen = [], set()
    for fn in ("fill.txt", "simlog-fill.txt"):
        p = f"{F}/{d}/{fn}"
        if not os.path.exists(p): continue
        for l in open(p, errors="replace"):
            if '"msg":"wave result"' in l:
                try: r = json.loads(l)
                except Exception: continue
                k = (r["tag"], r["active_agents"], r["time"][:19])
                if k in seen: continue
                seen.add(k); out.append(r)
            elif l.startswith("hold,") and ("hold", d) not in seen:
                v = l.strip().split(",")
                if len(v) == len(HDR):
                    r = {h: num(x) for h, x in zip(HDR, v)}; r["active_agents"] = int(r["active_agents"]); r["running"] = int(r["running"]); r["crashed"] = int(r["crashed"]); r["activations"] = int(r["activations"])
                    for k in ("wake_p50_ms", "wake_p90_ms", "wake_p99_ms", "turn_p90_ms", "turn_p99_ms"): r[k] = int(r[k])
                    r["time"] = "9"; r["failed_on"] = r["failed_on"] or ""
                    seen.add(("hold", d)); out.append(r)
    out.sort(key=lambda r: r["time"]); return out
def setup(d):
    for fn in ("fill.txt", "simlog-fill.txt"):
        p = f"{F}/{d}/{fn}"
        if not os.path.exists(p): continue
        for l in open(p, errors="replace"):
            if '"msg":"setup complete"' in l:
                try: return json.loads(l)
                except Exception: pass
def memtotal(d):
    p = f"{F}/{d}/hostprobe-fill.log"
    if os.path.exists(p):
        for l in open(p, errors="replace"):
            if l.startswith("MemTotal:"): return int(l.split()[1]) * 1024
    return 791846896 * 1024
def slope(W, tot):
    """GiB of host memory per additional running sandbox, least squares over the clean waves."""
    pts = [(w["running"], (100 - float(w["mem_avail_pct"])) / 100 * tot) for w in W if w["tag"] != "hold" and w["running"] > 0]
    if len(pts) < 2: return None
    n = len(pts); sx = sum(p[0] for p in pts); sy = sum(p[1] for p in pts)
    sxx = sum(p[0] ** 2 for p in pts); sxy = sum(p[0] * p[1] for p in pts)
    den = n * sxx - sx * sx
    return None if den == 0 else (n * sxy - sx * sy) / den
def fmt_row(w):
    lab = f"hold at {w['active_agents']:,} (10 min)" if w["tag"] == "hold" else f"{w['active_agents']:,}"
    return f"| {lab} | {w['activations']:,} | {w['wake_p50_ms']:,} / {w['wake_p90_ms']:,} / {w['wake_p99_ms']:,} | {w['turn_p90_ms']} / {w['turn_p99_ms']} | {w['probe_p50_ms']} / {w['probe_p90_ms']} / {w['probe_p99_ms']} | {w['error_pct'] if 'error_pct' in w else '—'} | {w['refusal_pct'] if 'refusal_pct' in w else '—'} | {w['mem_avail_pct']} % | {w['psi_cpu_some10']} / {w['psi_mem_full10']} / {w['psi_io_some10']} | {w['running']:,} | {w['crashed']} | {verdict(w['failed_on'])} |"
TH = ["| Awake agents | Requests in the window | Wake P50 / P90 / P99 ms | Turn P90 / P99 ms | CPU probe P50 / P90 / P99 ms | Errors % | Refusals % | Node mem avail | PSI cpu some / mem full / io some | Sandboxes running on the node | Crashed | Verdict |", "|---|---|---|---|---|---|---|---|---|---|---|---|"]

# ---- cells: final run (headline) + earlier runs (run log)
CELLS = [
 dict(tag="east", cls="gVisor", node="agents-tco-east, us-east4-a", shape="2 vCPU + 2 GiB limit (runsc, two network namespaces per actor)", final="nanofill-east", ext="nanofill-east-ext", fleet=9000,
      earlier=[("nanofill-east-waves250", "250-agent waves every 4 min from 250, original gates", "1,000 failed: turn P99 19.7 s and 0.5 % errors — the 250-agent wake burst parks requests in the router past its 5-s budget; gates relaxed (see below)"),
               ("nanofill-east-waves200", "200-agent waves every 2 min from 750, relaxed gates", "1,150 passed, 1,350 failed: 7.2 % client timeouts, wake P90 10.5 s, turn P99 36 s with 87 % memory free"),
               ("nanofill-east-fine-neighwall", "50-agent waves every 90 s from 1,150", "1,200 failed at once: 12.3 % timeouts — cause found: the node reboot had reset the ARP neighbour table to gc_thresh3 = 1,024 (2,692 'neighbor table overflow' lines in dmesg); limits raised again, inotify limits too"),
               ("nanofill-east", "200-agent waves every 2 min from 1,150, neighbour table fixed", None),
               ("nanofill-east-ext", "extension: 200-agent waves every 2 min from 5,950, 2×-first-wave rules off, turn P99 gate 1 s", "EXT")]),
 dict(tag="metal2", cls="microVM", node="agents-tco-euw4, europe-west4-c", shape="2 vCPU + 256 MiB guest (Cloud Hypervisor, memfd guest RAM)", final="nanofill-metal2", fleet=5000,
      earlier=[("nanofill-metal2-psi10", "500-agent waves every 4 min from 500, original gates", "500 failed on memory PSI full 10 % — not the agents: 591 GB of pause checkpoints from the previous test were still in the page cache; caches dropped, PSI gate raised to 30 %"),
               ("nanofill-metal2-psi10", "rerun", "2,000 passed, 2,500 failed on memory PSI full 15.9 % (page cache again)"),
               ("nanofill-metal2-psi30", "500-agent waves every 4 min from 2,000", "3,500 passed, 4,000 failed: 2.6 % refusals, wake P90 35 s — the 500-wake burst, not the host (17.7 % memory free)"),
               ("nanofill-metal2", "250-agent waves every 3 min from 3,500", None)])]
md = ["# Resident fill with the nano-personal-agent — awake agents per bare-metal node, gVisor vs microVM (2026-10-10)\n",
"How many nano-personal-agent actors one c3-standard-192-metal node (192 vCPU, 755 GiB) keeps awake at once, with no parking at all. Same method as the October 7 resident-fill test with the personal-assistant actor: each node has its fleet registered (cold boot from the golden snapshot, first ping, one park); the harness wakes a wave of agents, lets them stay resident playing the assistant day (61 events per 24 h at think ×0.02, mocked model/API/file ops, no synthetic heap), measures a window, and if the window passes every gate wakes the next wave. The last clean wave is the node's number. Gates: node MemAvailable < 10 %, memory PSI full above the gate, CPU PSI some > 50 %, CPU-probe P99 > 1 s, turn P99 above the gate, wake P99 above the gate, wake or turn P90 > 2× the first wave, errors or refusals above the gate, crashed actors above the gate. Substrate main 66f8a888 plus each node's end-of-campaign experimental patches (gVisor: network-namespace pool, shared gofer namespace, lean teardown, no app cgroup; microVM: pooled namespaces, raised socket waits, reseed retry, no tar fsync; both: pooled snapshot plugin). 100 unsized worker pods per node, catch-up off, the two nodes ran in parallel.\n"]
S = {}
for c in CELLS:
    W = rows(c["final"]); c["W"] = W; c["setup"] = setup(c["final"]); c["tot"] = memtotal(c["final"])
    waves = [w for w in W if w["tag"] != "hold"]
    c["clean"] = [w for w in waves if not w["failed_on"]]; c["fail"] = next((w for w in waves if w["failed_on"]), None)
    c["hold"] = next((w for w in W if w["tag"] == "hold"), None); c["best"] = c["clean"][-1] if c["clean"] else None
    c["slope"] = slope(c["clean"], c["tot"])
    E = rows(c["ext"]) if c.get("ext") else []; c["E"] = E
    ew = [w for w in E if w["tag"] != "hold"]; c["eclean"] = [w for w in ew if not w["failed_on"]]; c["efail"] = next((w for w in ew if w["failed_on"]), None); c["ehold"] = next((w for w in E if w["tag"] == "hold"), None)
def col(fn): return " | ".join(fn(c) for c in CELLS) + " |"
T = ["## Result in one table\n", "| | " + " | ".join(c["cls"] for c in CELLS) + " |", "|---|" + "---|" * len(CELLS)]
T.append("| **Awake nano agents per node, last clean wave** | " + col(lambda c: f"**{c['best']['active_agents']:,}**" if c["best"] else "—"))
T.append("| First failing wave and why | " + col(lambda c: f"{c['fail']['active_agents']:,}: {verdict(c['fail']['failed_on'])}" if c["fail"] else ("not reached — the registered fleet (%s) was exhausted" % f"{c['fleet']:,}" if c["best"] and c["best"]["active_agents"] >= c["fleet"] - 50 else "run still in progress")))
T.append("| 10-min hold at the last clean count | " + col(lambda c: ("clean" if not c["hold"]["failed_on"] else f"{verdict(c['hold']['failed_on'])} (probe P90 {c['hold']['probe_p90_ms']} ms, turn P90 {c['hold']['turn_p90_ms']} ms, {c['hold']['mem_avail_pct']} % memory free)") if c["hold"] else "—"))
T.append("| Node memory available at the last clean wave | " + col(lambda c: f"{c['best']['mem_avail_pct']} %" if c["best"] else "—"))
T.append("| Sandboxes resident on the node at the last clean wave | " + col(lambda c: f"{c['best']['running']:,}" if c["best"] else "—"))
T.append("| Host memory per resident sandbox (slope over the clean waves) | " + col(lambda c: f"{c["slope"]/2**20:.0f} MiB" if c["slope"] else "—"))
T.append("| CPU probe P99 / turn P99 at the last clean wave | " + col(lambda c: f"{c['best']['probe_p99_ms']} / {c['best']['turn_p99_ms']} ms" if c["best"] else "—"))
T.append("| Wake P50 / P90 at the last clean wave (whole wave woken together) | " + col(lambda c: f"{c['best']['wake_p50_ms']:,} / {c['best']['wake_p90_ms']:,} ms" if c["best"] else "—"))
T.append("| Extension with the 2×-first-wave rules switched off (absolute gates only: probe P99 1 s, turn P99 1 s, memory 10 %, PSI, errors / refusals 5 %) | " + col(lambda c: (f"**{c['eclean'][-1]['active_agents']:,}** passed" + (f", {c['efail']['active_agents']:,} failed: {verdict(c['efail']['failed_on'])}" if c["efail"] else (", fleet exhausted" if c["eclean"][-1]["active_agents"] >= c["fleet"] - 50 else ", in progress")) + f"; probe P99 {c['eclean'][-1]['probe_p99_ms']} ms, turn P99 {c['eclean'][-1]['turn_p99_ms']} ms, {c['eclean'][-1]['mem_avail_pct']} % memory free, CPU PSI some {c['eclean'][-1]['psi_cpu_some10']} %" + (f"; **10-min hold at {c['ehold']['active_agents']:,}: {'clean' if not c['ehold']['failed_on'] else verdict(c['ehold']['failed_on'])}** (probe P90 / P99 {c['ehold']['probe_p90_ms']} / {c['ehold']['probe_p99_ms']} ms, turn P90 / P99 {c['ehold']['turn_p90_ms']} / {c['ehold']['turn_p99_ms']} ms)" if c.get("ehold") else "")) if c.get("eclean") else ("not run — the microVM node is out of memory, there is nothing to switch off" if c["tag"] == "metal2" else "in progress")))
T.append("| Wave size / interval of the final run | " + col(lambda c: "200 / 2 min" if c["tag"] == "east" else "250 / 3 min"))
T.append("| October 7 reference: personal-assistant actor (~1 GiB resident), 1 vCPU | 650 | 650 |")
T.append("| October 8 reference: ping actor (256 MiB, idle) | ≥ 4,000 (fleet exhausted) | 3,550 |")
T.append("")
md += T
md.append("## What limited each node\n")
md.append("""**microVM: guest memory, and nothing else.** Every microVM nano agent costs the host the slope below in resident memory (the 256 MiB guest is memfd-backed and the agent touches most of it after a few events, plus the VMM, virtiofsd and the tap). At 4,250 awake agents (4,876 VMs resident) the node had 12.8 % of 755 GiB left and the kernel was already reclaiming: the CPU probe P99 went from 26–35 ms to 60 ms and the turn P99 from 12–14 ms to 55 ms, and in the 10-minute hold both P90s were more than 2× the first wave's (still only tens of milliseconds in absolute terms). The next wave, 4,500, dropped MemAvailable to 8.6 % and failed the 10 % rule. CPU was idle (PSI cpu some ≤ 8 %), disk idle, no refusals, 0.0–0.1 % errors throughout; the 21 crashed actors are the leftovers of earlier runs (virtiofsd / reseed start-up failures). So the microVM ceiling is a straight memory budget: ≈ 755 GiB × 0.9 ÷ (memory per VM) minus the worker pods and the page cache the node needs; a node with 1.5 TB would roughly double it, as would a smaller guest (the agent itself needs well under 100 MiB).

**gVisor: CPU contention from the agents' own work, with half the memory still free.** A resident gVisor nano agent costs the host only 59 MiB (the sentry backs just the pages the application touches), so at the rule ceiling of 5,950 awake agents the node still had 51.6 % of 755 GiB available; the 9,000-actor fleet would fit in memory twice over. What moved was CPU: the node went from 2 % busy at 1,150 agents to 33 % busy (peak 48 %, half of it kernel time) at 5,950, load average 43 on 192 vCPUs, CPU PSI some 7–13 %. The agents' assistant day runs at think ×0.02, so each agent issues an event every ~28 s, and every event is a tool turn through runsc (netstack, gofer, syscall interception) — about 0.01 vCPU per agent, 60 vCPUs for 6,000 agents. With that much steady background load the latency floor rises: the CPU probe P99 went 17 → 75 ms and the turn P99 7 → 90 ms between 1,150 and 5,950, and at 6,150 the turn P90 crossed 2× the first wave's (the rule that stopped the run; the absolute numbers were still below 100 ms). The 10-minute hold at 5,950 confirmed it: probe P90 41 ms, turn P90 31 ms, 0 refusals, 0.2 % errors, 51 % memory free. The extension run (same fleet, 2×-first-wave rules off, absolute gates only) shows where the hard wall is: waves kept passing up to 7,550 awake agents, but by then the node was 54–76 % busy with kernel time larger than user time (sys 17–40 %: runsc's syscall interception, netstack and the gofer, plus the scheduler shuffling ~7,500 sandboxes), CPU PSI some reached 32–45 %, wakes took 7–10 s at P90 and 1–2 % of them came back as router 502s because the sentry was not ready inside the router's budget; at 7,750 the 502s reached 9.2 % and the wave failed on errors; and the 10-minute hold at 7,550 failed outright with probe and turn P99 around 2 s. So for this agent on this build a gVisor node sustains about 6,000 awake nano agents within the rules and tops out between 6,000 and 7,500 on CPU, with roughly half the memory unused; a faster per-event path in runsc (or a lighter workload per event) would move that number, memory would not. Two walls had to be removed on the way and were not actor costs: (1) the wake burst — the whole wave is woken in the same second and the router parks requests to not-yet-ready actors for at most 5 s, so bursts of 250+ gVisor wakes produce 503s / client timeouts in the window, which is why the waves were cut to 200 and the error / turn gates relaxed; (2) the kernel ARP neighbour table — every gVisor actor adds two network namespaces and veths on the host, and at ~1,100 live sandboxes the host's neighbour cache hits the default gc_thresh3 = 1,024 and new sandboxes' traffic times out ('neighbor table overflow' in dmesg). This wall had been raised on both nodes on October 8, but the east node rebooted at 19:30 (cause unknown, the kernel log of the previous boot is gone) and lost the sysctl, which produced two spurious failures at 1,200–1,350 before it was found and raised again (gc_thresh3 = 16,384, inotify watches 1,048,576). Both are node-prep items for Substrate, not actor costs.
""")
md.append("## Changes made for this test\n")
md.append("""- **Gates relaxed, both nodes:** errors and refusals 0.5 % → 5 %, turn P99 1 s → 30 s, wake P99 10 s → 60 s. Reason: the fill wakes a whole wave in one burst; with 200–500 wakes in the same second the router's 5-s parked-request budget is exceeded for the tail of the burst and the first turns of the just-woken agents dominate the window's turn P99. These gates only mask the burst, not resident degradation — the probe P99 (1 s), memory (10 %), PSI and the 2×-baseline rules stayed in force.
- **Memory PSI gate, microVM node:** 10 % → 30 % after dropping the page cache. The node still held 591 GB of pause checkpoints from the previous test on the Hyperdisk Extreme volume and the first fills failed on memory PSI from page-cache writeback, not from the agents.
- **Wave size:** gVisor 500 → 250 → 200 agents (burst limit above); microVM 500 → 250 for the top of the ladder.
- **East node after its reboot:** neighbour-table and inotify sysctls re-applied (`neightune`, `inotifytune` node-critical pods), worker pool / router / egress restarted, 907 crashed actors deleted.
- **Harness:** fill mode gives up on actors whose wake error is permanent (CRASHED / DELETING / not found) instead of retrying them 12 times (commit 1b118c8); `make_md_nanofill.py` writes this file.
""")
md.append("## Run log\n")
md.append("| Node | Run | Ladder | Outcome |"); md.append("|---|---|---|---|")
for c in CELLS:
    for d, ladder, outcome in c["earlier"]:
        if outcome == "EXT":
            b, f, h = (c["eclean"][-1] if c.get("eclean") else None), c.get("efail"), c.get("ehold")
            outcome = ((f"**{b['active_agents']:,} passed**" if b else "no clean wave") + (f", {f['active_agents']:,} failed: {verdict(f['failed_on'])}" if f else (", fleet exhausted" if b and b["active_agents"] >= c["fleet"] - 50 else ", in progress")) + (f"; hold at {h['active_agents']:,}: {'clean' if not h['failed_on'] else verdict(h['failed_on'])}" if h else "")) if c.get("E") else "in progress"
        elif outcome is None:
            b, f, h = c["best"], c["fail"], c["hold"]
            outcome = (f"**{b['active_agents']:,} passed**" if b else "no clean wave") + (f", {f['active_agents']:,} failed: {verdict(f['failed_on'])}" if f else (", fleet exhausted" if b and b["active_agents"] >= c["fleet"] - 50 else ", in progress")) + (f"; hold at {h['active_agents']:,}: {'clean' if not h['failed_on'] else verdict(h['failed_on'])}" if h else "")
        md.append(f"| {c['cls']} | `{d}` | {ladder} | {outcome} |")
md.append("")
for c in CELLS:
    md.append(f"## {c['cls']} — {c['node']}, actor {c['shape']}: final run, wave by wave\n")
    if c["setup"]: md.append(f"Registered fleet {c['setup'].get('agents'):,} actors ({c['setup'].get('failed')} failed to register). 'Sandboxes running on the node' counts every resident sandbox of the fleet, including the ones left awake by the earlier runs on the same fleet, so the memory slope uses that column.\n")
    md += TH
    for w in c["W"]: md.append(fmt_row(w))
    md.append("")
    if c.get("E"):
        md.append(f"### {c['cls']} extension beyond the rule ceiling (2×-first-wave rules off)\n"); md += TH
        for w in c["E"]: md.append(fmt_row(w))
        md.append("")
md.append("## Earlier runs, wave by wave\n")
for c in CELLS:
    done = set()
    for d, ladder, _ in c["earlier"]:
        if d == c["final"] or d == c.get("ext") or d in done: continue
        done.add(d); W = rows(d)
        if not W: continue
        md.append(f"### {c['cls']} `{d}` — {ladder}\n"); md += TH
        for w in W: md.append(fmt_row(w))
        md.append("")
md.append("## Data\n")
md.append("Per run directory under `/tmp/tco-runs/fill/` (archived in `assets/runs/2026-10-10-nano-resident-fill/`): `fill.txt` (wave results, JSON), `simlog-fill.txt` (full agentsim log incl. the hold CSV and the verdict), `hostprobe-fill.log` (node meminfo / cpu / diskstats every 30 s), `run-fill.log`. Pipeline: `nanofill.sh` with the per-run launchers `nanofill_east_{d,e,f}.sh`; sysctl pods `sysctl-neigh.yaml`, `sysctl-inotify.yaml`.\n")
open(OUT, "w").write("\n".join(md)); print("wrote", OUT)
