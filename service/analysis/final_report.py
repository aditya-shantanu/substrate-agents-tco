#!/usr/bin/env python3
"""Final results card for a Phase 2 run: what happened, and what an agent
costs per month in the real world based on the measured numbers.

Inputs are the artifacts 50-collect/run.sh gather: the agentsim log (with its
embedded CSV), the autosuspender occupancy CSV and metrics, plus the workload
parameters and machine facts of the run. All arithmetic is printed, nothing
is hidden.
"""
import argparse
import csv
import io
import json
import os
import statistics
import sys

# $/hr per vCPU / GiB, us-central1, retrieved 2026-09-25
# (MODEL.md price book). cud multipliers apply to on-demand.
FAM = {
    "e2":  (0.021811, 0.002923), "n1": (0.031611, 0.004237),
    "n2":  (0.031611, 0.004237), "n2d": (0.027502, 0.003686),
    "n4":  (0.031190, 0.003540), "c3": (0.034650, 0.003938),
    "c3d": (0.029563, 0.003959), "c4": (0.034650, 0.003938),
    "c4d": (0.032704, 0.003753), "t2d": (0.027502, 0.003686),
}
PRICE_MODEL = {"od": 1.0, "cud1": 0.63, "cud3": 0.45}
GCS_GIB_MO = 0.020
OPS_WRITE, OPS_READ = 5.0e-6, 4.0e-7  # $ per op, class A / B


LOCAL_SSD_HR = 0.08 * 375 / 730  # one 375 GB local NVMe, $0.08/GB-mo


def machine_hr(mtype, model):
    # e.g. c3-standard-4, e2-standard-16, c4-highcpu-16, c3-standard-4-lssd,
    # c3-standard-192-metal (bare metal: same per-vCPU/GiB price as the VM shapes)
    parts = mtype.split("-")
    if parts[-1] == "metal":
        parts = parts[:-1]
    lssd = parts[-1] == "lssd"
    if lssd:
        parts = parts[:-1]
    fam, kind, cpus = parts
    cpus = int(cpus)
    gib_per_cpu = {"standard": 4, "highcpu": 2, "highmem": 8}[kind]
    if fam == "c4" and kind == "standard":
        gib_per_cpu = 3.75
    cpu_rate, gib_rate = FAM[fam]
    hr = cpus * cpu_rate + cpus * gib_per_cpu * gib_rate
    if lssd:
        hr += LOCAL_SSD_HR
    return hr * PRICE_MODEL[model]


def pct(sorted_vals, p):
    if not sorted_vals:
        return float("nan")
    return sorted_vals[min(len(sorted_vals) - 1, max(0, int(p * len(sorted_vals)) - 1))]


def extract_sim_csv(path):
    rows, active = [], False
    for line in open(path):
        line = line.strip()
        if line == "=== agentsim csv ===":
            active = True
        elif line == "=== end csv ===":
            break
        elif active and line and not line.startswith("{"):
            # In-flight goroutines may still log JSON lines while the CSV is
            # being printed (a cancelled load-test wave); keep only rows with
            # the header's field count.
            if not rows or line.count(",") == rows[0].count(","):
                rows.append(line)
    if len(rows) < 2:
        sys.exit(f"no agentsim csv section in {path} — did the job finish?")
    return list(csv.DictReader(io.StringIO("\n".join(rows))))


def extract_profile(path):
    """The key=value block agentsim prints before the load (what workload ran)."""
    prof, active = {}, False
    for line in open(path):
        line = line.strip()
        if line == "=== agentsim profile ===":
            active = True
        elif line == "=== end profile ===":
            break
        elif active and "=" in line:
            k, v = line.split("=", 1)
            prof[k] = v
    return prof


def parse_seconds(s):
    """'2s' / '10s' / '1m' / '90' → seconds."""
    s = str(s).strip()
    if s.endswith("ms"):
        return float(s[:-2]) / 1000
    if s.endswith("s"):
        return float(s[:-1])
    if s.endswith("m"):
        return float(s[:-1]) * 60
    return float(s)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--run-log", required=True)
    ap.add_argument("--occupancy", required=True)
    ap.add_argument("--metrics", required=True)
    ap.add_argument("--compress", type=float, required=True)
    ap.add_argument("--machine-type", required=True)
    ap.add_argument("--price-model", default="cud3", choices=list(PRICE_MODEL))
    ap.add_argument("--pool-workers", type=int, required=True)
    ap.add_argument("--pool-nodes", type=int, required=True,
                    help="distinct nodes hosting the worker pods")
    ap.add_argument("--snap-gib", type=float, default=0.05,
                    help="measured avg snapshot GiB per agent (pass from GCS du)")
    # real-world workload shape (defaults = agentsim defaults)
    ap.add_argument("--sessions-per-day", type=float, default=3)
    ap.add_argument("--session-minutes", type=float, default=8)
    ap.add_argument("--wakes-per-day", type=float, default=40)
    ap.add_argument("--wake-seconds", type=float, default=15)
    ap.add_argument("--idle-timeout-real", type=float, default=10,
                    help="idle wait a real deployment would use, seconds")
    ap.add_argument("--idle-timeout-run", default=None,
                    help="the run's autosuspender idle timeout (e.g. 2s); a script run in "
                         "idle mode is priced with it (think gaps are not time-compressed)")
    ap.add_argument("--utilization", type=float, default=0.70)
    ap.add_argument("--peak-model", default="mult", choices=["mult", "herd"],
                    help="mult: busiest-hour multiplier; herd: fraction waking simultaneously")
    ap.add_argument("--peak-value", type=float, default=2.0,
                    help="the multiplier (mult) or herd fraction 0-1 (herd)")
    # per-phase CPU (GKE Agent Runtime Benchmark defaults) for the
    # multi-actor projection; restore is the peak
    ap.add_argument("--cpu-active", type=float, default=0.25)
    ap.add_argument("--cpu-suspend", type=float, default=0.30)
    ap.add_argument("--cpu-restore", type=float, default=1.22)
    ap.add_argument("--active-mem-gib", type=float, default=1.0,
                    help="RAM held per ACTIVE agent (suspended agents hold none)")
    a = ap.parse_args()

    sim = extract_sim_csv(a.run_log)
    prof = extract_profile(a.run_log)
    script_mode = prof.get("workload") == "script"
    ping_mode = prof.get("workload") == "ping"
    if "wakes_per_day" in prof:
        a.wakes_per_day = float(prof["wakes_per_day"])
    if not script_mode and "sessions_per_day" in prof:
        a.sessions_per_day = float(prof["sessions_per_day"])
        a.session_minutes = float(prof.get("session_minutes", a.session_minutes))
    # Density divides the whole fleet by busy workers: every agent that
    # completed setup can wake, whether or not its Poisson schedule gave it
    # an activation inside the window (short windows at low rates leave
    # some agents quiet). The summary line carries the configured count.
    active_agents = len({r["agent"] for r in sim})
    agents = active_agents
    for line in open(a.run_log):
        if line.startswith("agents=") and " activations=" in line:
            agents = int(line.split()[0].split("=")[1])
            break
    acts_measured = len(sim)
    errors = sum(int(r["errors"]) for r in sim)
    refusals = sum(int(r.get("refusals", 0)) for r in sim)
    t0 = min(int(r["unix_ms"]) for r in sim)
    t1 = max(int(r["unix_ms"]) for r in sim)
    window_min = (t1 - t0) / 60000 or 1

    # A wake is a ping-only activation, or a script step whose actor was
    # suspended when the step's first request arrived (was_suspended=1;
    # unknown states count too, running ones do not).
    wake_ms = sorted(float(r["first_req_ms"]) for r in sim
                     if r["kind"] == "wake"
                     or (r["kind"] in ("step", "ping") and r.get("was_suspended", "1") != "0"))
    step_rows = [r for r in sim if r["kind"] in ("step", "ping")]
    step_ms = sorted(float(r["step_ms"]) for r in step_rows if float(r.get("step_ms") or 0) > 0)
    susp_ms = sorted(float(r["suspend_ms"]) for r in sim if float(r.get("suspend_ms") or 0) > 0)
    known = [r for r in step_rows if r.get("was_suspended") in ("0", "1")]
    # share of script steps that really woke from suspension (driver mode ≈ 1)
    f_susp = (sum(r["was_suspended"] == "1" for r in known) / len(known)) if known else 1.0
    sess_ms = sorted(float(r["first_req_ms"]) for r in sim if r["kind"] == "session")
    walk_ms = sorted(float(r["readram_ms"]) for r in sim if float(r["readram_ms"]) > 0)

    # suspend time from the autosuspender's own measurement
    m = dict(l.split(" ", 1) for l in open(a.metrics) if " " in l.strip()
             and not l.startswith("#") and "{" not in l.split(" ", 1)[0])
    suspends = float(m.get("autosuspend_suspends_total", 0))
    t_s = (float(m.get("autosuspend_suspend_seconds_sum", 0)) / suspends) if suspends else float("nan")
    t_s_src = "autosuspender"
    if susp_ms and (not suspends or prof.get("suspend_mode") == "driver"):
        # script driver mode: agentsim suspended after every step itself
        t_s, suspends, t_s_src = statistics.fmean(susp_ms) / 1000, len(susp_ms), "driver"
    t_r = pct(wake_ms, 0.5) / 1000 if wake_ms else float("nan")  # wake p50 = resume + routing

    # Every latency is captured as P50 / P90 / P99 (+ max), on the card and
    # in latency.csv next to the run log, so a run can be quoted directly.
    def p3(v):
        return f"{pct(v, .5):,.0f} / {pct(v, .9):,.0f} / {pct(v, .99):,.0f}"
    latency_rows = [("resume_wake", wake_ms), ("resume_session_first_request", sess_ms),
                    ("suspend_driver", susp_ms), ("step_work", step_ms), ("ram_walk", walk_ms)]
    latency_csv = os.path.join(os.path.dirname(os.path.abspath(a.run_log)), "latency.csv")
    with open(latency_csv, "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["latency", "n", "p50_ms", "p90_ms", "p99_ms", "max_ms"])
        for name, v in latency_rows:
            if v:
                w.writerow([name, len(v), f"{pct(v, .5):.0f}", f"{pct(v, .9):.0f}", f"{pct(v, .99):.0f}", f"{v[-1]:.0f}"])

    try:
        occ = [r for r in csv.DictReader(open(a.occupancy)) if t0 <= int(r["unix_ms"]) <= t1]
    except (OSError, KeyError, ValueError):
        occ = []
    busy = sorted(int(r["workers_assigned"]) for r in occ) or [0]
    mean_busy, p99_busy, peak_busy = statistics.fmean(busy), pct(busy, .99), busy[-1]
    # No occupancy samples inside the window (sampler restarted, collect
    # failed): say so instead of dividing by zero.
    density_desc = (f"{agents / mean_busy:.1f}:1 mean · {agents / max(p99_busy, 1e-9):.1f}:1 at p99 ← bankable"
                    if mean_busy > 0 else "n/a (no worker-occupancy samples inside the window)")
    hib = "pause" if prof.get("lifecycle") == "pause" else "suspend"

    # ---- real-world projection from measured T_s / T_r ----
    if script_mode:
        # A coding agent: tasks/day, each the script's S steps. Per step the
        # worker holds the measured step work W; a step that woke from
        # suspension pays wait + T_s + T_r; a step that found the actor awake
        # (idle mode, think gap shorter than the wait) held the worker for
        # its think gap instead.
        S = int(prof["steps"])
        tasks = float(prof.get("tasks_per_day", a.sessions_per_day))
        think_task = float(prof["think_scaled_s"])
        mode = prof.get("suspend_mode", "driver")
        W = statistics.fmean(step_ms) / 1000 if step_ms else float(prof.get("burn_wall_s", 0)) / S + 1.0
        if mode == "driver":
            idle_wait = 0.0  # the driver suspends the moment the turn ends
        else:
            idle_wait = parse_seconds(a.idle_timeout_run) if a.idle_timeout_run else a.idle_timeout_real
        steps_day = tasks * S
        A = steps_day * f_susp + a.wakes_per_day
        live = steps_day * W + a.wakes_per_day * a.wake_seconds
        held_think = steps_day * (1 - f_susp) * think_task / S
        overhead = idle_wait + t_s + t_r
        occ_real = (live + held_think + A * overhead) / 86400
        ran_desc = (f"{prof.get('script')} script, think ×{float(prof.get('think_scale', 1)):g}, "
                    f"{mode} suspend, time ×{a.compress:.0f}")
        day_desc = (f"{tasks:g} tasks × {S} steps = {steps_day:.0f} steps, {W:.1f}s work each (measured)"
                    + (f" + {a.wakes_per_day:.0f} check-ins" if a.wakes_per_day else ""))
        think_desc = (f"    think per task          {think_task:.0f}s "
                      + ("(agent asleep: free)" if f_susp >= 0.999 else
                         f"({f_susp:.0%} of steps woke from suspension; the rest held a worker "
                         f"through the gap: +{held_think:.0f}s/day)") + "\n")
        wait_desc = f"{idle_wait:.0f}s wait" + (" (driver suspends at once)" if mode == "driver" else "")
        occ_desc = (f"({live:.0f}s" + (f" + {held_think:.0f}s" if held_think else "")
                    + f" + {A:.0f}×{overhead:.1f}s)/86400 = {occ_real * 100:.2f}%")
    elif ping_mode:
        # The GluttonUser loop: a user serializes N actors, each cycle is
        # wake + live window + suspend followed by the wait, so one actor
        # wakes every N × (cycle + wait) seconds and holds a worker for the
        # cycle only. The driver suspends at once; there is no idle wait.
        N = int(prof["actors_per_user"])
        independent = prof.get("independent") == "true" or N == 0
        wait = float(prof["wait_s"])
        W = statistics.fmean(step_ms) / 1000 if step_ms else float(prof.get("live_s", 0))
        cycle = W + t_s + t_r
        # user loop: N actors share one serial loop; independent: each
        # agent's own Poisson schedule with mean gap `wait`
        period = (cycle + wait) if independent else N * (cycle + wait)
        wakes_day = 86400 / period
        A = wakes_day * f_susp
        live = wakes_day * W
        overhead = t_s + t_r
        occ_real = (live + A * overhead) / 86400
        if independent:
            ran_desc = f"one-ping, independent Poisson wakes every {wait:.0f} s mean, {float(prof.get('live_s', 0)):g} s live, time ×{a.compress:.0f}"
            day_desc = f"{wakes_day:.0f} wakes/day of 1 ping; an agent's period = {cycle:.1f} s cycle + {wait:.0f} s mean gap = {period:.0f} s"
            occ_desc = f"({live:.0f}s + {A:.0f}×{overhead:.1f}s)/86400 = {occ_real * 100:.2f}%  (≈ cycle ÷ period)"
        else:
            ran_desc = (f"one-ping GluttonUser loop: {N} actors/user, {wait:.0f} s wait, "
                        f"{float(prof.get('live_s', 0)):g} s live, time ×{a.compress:.0f}")
            day_desc = (f"{wakes_day:.0f} wakes/day of 1 ping; an actor's period = {N} × ({cycle:.1f} s cycle + {wait:.0f} s wait) = {period:.0f} s")
            occ_desc = f"({live:.0f}s + {A:.0f}×{overhead:.1f}s)/86400 = {occ_real * 100:.2f}%  (≈ cycle ÷ period; {N}:1 by construction)"
        think_desc = ""
        wait_desc = "0s wait (driver suspends after the ping)"
    else:
        live = a.sessions_per_day * a.session_minutes * 60 + a.wakes_per_day * a.wake_seconds
        A = a.sessions_per_day + a.wakes_per_day
        overhead = a.idle_timeout_real + t_s + t_r
        occ_real = (live + A * overhead) / 86400
        ran_desc = f"personal-agent profile, time ×{a.compress:.0f}"
        day_desc = f"{live / 60:.0f} min live over {A:.0f} wake-ups"
        think_desc = ""
        wait_desc = f"{a.idle_timeout_real:.0f}s wait"
        occ_desc = f"({live:.0f}s + {A:.0f}×{overhead:.1f}s)/86400 = {occ_real * 100:.2f}%"
    if a.peak_model == "herd":
        H = a.peak_value
        n_real = a.utilization / (H + (1 - H) * occ_real)
        peak_desc = f"{a.utilization:.2f} ÷ ({H:.0%} herd + rest × {occ_real*100:.2f}%) = {n_real:.1f}"
    else:
        n_real = a.utilization / (occ_real * a.peak_value)
        peak_desc = f"{a.utilization:.2f} ÷ ({occ_real*100:.2f}% × {a.peak_value:.1f} peak) = {n_real:.1f}"

    node_hr = machine_hr(a.machine_type, a.price_model)
    wpn = a.pool_workers / a.pool_nodes
    worker_mo = node_hr * 730 / wpn
    compute = worker_mo / n_real
    if hib == "pause":
        # Node-local checkpoints: nothing crosses the bucket per cycle, so no
        # GCS operations and no snapshot at rest (the trade: node pinning).
        storage, ops = 0.0, 0.0
    else:
        storage = a.snap_gib * GCS_GIB_MO
        ops = A * 30.44 * (7 * OPS_WRITE + 4 * OPS_READ)
    total = compute + storage + ops

    B = "\033[1m"; D = "\033[2m"; R = "\033[0m"
    line = "─" * 66
    print()
    print(f"{B}┌{line}┐{R}")
    print(f"{B}│  SUBSTRATE DENSITY RUN — RESULTS{' ' * 33}│{R}")
    print(f"{B}└{line}┘{R}")
    print(f"""
  What ran
    agents simulated        {agents}   ({ran_desc}){f'; {active_agents} were activated inside the window' if active_agents != agents else ''}
    worker pool             {a.pool_workers} workers on {a.pool_nodes} × {a.machine_type}
    window                  {window_min:.1f} min wall = {window_min * a.compress / 60:.1f} h of agent-life
    activations served      {acts_measured}  ({acts_measured / agents:.1f}/agent; errors {errors}, router refusals {refusals})

  What it measured
    resume P50 / P90 / P99  {p3(wake_ms)} ms  (n={len(wake_ms)})   ← T_r, user-visible wake-up
    {hib:7s} (avg)           {t_s * 1000:,.0f} ms                ← T_s, from {suspends:.0f} {hib}s ({t_s_src})""" + (f"""
    {hib:7s} P50 / P90 / P99 {p3(susp_ms)} ms  (n={len(susp_ms)})""" if susp_ms else "") + f"""
""" + (f"""
    RAM walk P50 / P90 / P99 {p3(walk_ms)} ms  (demand paging)""" if walk_ms else "") + (f"""
    step work P50 / P90 / P99 {p3(step_ms)} ms  ← the agent's own ops per step""" if step_ms else "") + f"""
    workers busy            mean {mean_busy:.2f} · p99 {p99_busy} · peak {peak_busy} of {a.pool_workers}
    density (this run)      {density_desc}

  Real-world cost per agent (measured T_s/T_r plugged into the model)
    one agent/day           {day_desc}
{think_desc}    overhead per wake-up    {wait_desc} + {t_s:.1f}s suspend + {t_r:.1f}s resume = {overhead:.1f}s
    worker-time per agent   {occ_desc}
    agents per worker       {peak_desc}
    worker cost             ${node_hr:.4f}/hr ÷ {wpn:.1f} per node × 730h = ${worker_mo:.2f}/mo ({a.price_model})
    compute {D}${worker_mo:.2f} ÷ {n_real:.1f}{R}   ${compute:.2f}
    snapshot at rest        ${storage:.3f}   ({'node-local pause: nothing in the bucket' if hib == 'pause' else f'{a.snap_gib:.2f} GiB × ${GCS_GIB_MO}/GiB-mo'})
    GCS ops                 ${ops:.3f}{'   (pause: no bucket round trip)' if hib == 'pause' else ''}""")
    # Multi-actor projection: pack by CPU (per-phase weights, restore is the
    # peak); suspended agents hold no RAM. Roadmap upside, not today's price.
    cpu_avg = (live * a.cpu_active + A * (t_s * a.cpu_suspend + t_r * a.cpu_restore)) / 86400
    if a.peak_model == "herd":
        blend_cpu = a.peak_value * a.cpu_restore + (1 - a.peak_value) * cpu_avg
        active_frac = a.peak_value + (1 - a.peak_value) * occ_real
    else:
        blend_cpu = cpu_avg * a.peak_value
        active_frac = occ_real * a.peak_value
    fam, kind, cpus = [p for p in a.machine_type.split("-") if p not in ("metal", "lssd")][:3]
    gib_per_cpu = {"standard": 4, "highcpu": 2, "highmem": 8}.get(kind, 4)
    if fam == "c4" and kind == "standard":
        gib_per_cpu = 3.75
    # 0.85 mirrors the calculator's default node-allocatable fraction.
    alloc_cpu, alloc_mem = int(cpus) * 0.85, int(cpus) * gib_per_cpu * 0.85
    ma_node = max(1, int(min(alloc_cpu / max(blend_cpu, 1e-9),
                             alloc_mem / max(a.active_mem_gib * active_frac, 1e-9)) * a.utilization))
    ma_cost = (node_hr * 730) / ma_node + storage + ops

    # Machine-readable twin of the card (slide tables, cross-run comparisons).
    def q3(v):
        return {"n": len(v), "p50": pct(v, .5), "p90": pct(v, .9), "p99": pct(v, .99), "max": v[-1]} if v else None
    summary = {
        "profile": prof, "ran": ran_desc, "agents": agents, "active_agents": active_agents, "activations": acts_measured,
        "errors": errors, "refusals": refusals, "window_min": window_min, "compress": a.compress,
        "machine_type": a.machine_type, "price_model": a.price_model, "node_hr": node_hr,
        "node_mo": node_hr * 730, "pool_workers": a.pool_workers, "pool_nodes": a.pool_nodes,
        "workers_per_node": wpn, "worker_mo": worker_mo, "snap_gib": a.snap_gib,
        "resume_ms": q3(wake_ms), "suspend_ms": q3(susp_ms), "step_ms": q3(step_ms), "ram_walk_ms": q3(walk_ms),
        "t_s": t_s, "t_s_source": t_s_src, "t_r": t_r, "suspends": suspends, "lifecycle": hib,
        "busy_mean": mean_busy, "busy_p99": p99_busy, "busy_peak": peak_busy,
        "density_mean": (agents / mean_busy) if mean_busy > 0 else None,
        "density_p99": (agents / p99_busy) if p99_busy > 0 else None,
        "day": day_desc, "wakes_per_day": A, "live_s": live, "overhead_s": overhead,
        "occupancy": occ_real, "agents_per_worker": n_real, "agents_per_host": n_real * wpn,
        "peak_model": a.peak_model, "peak_value": a.peak_value, "utilization": a.utilization,
        "compute": compute, "storage": storage, "ops": ops, "total": total,
        "multi_actor_agents_per_host": ma_node, "multi_actor_total": ma_cost,
    }
    with open(os.path.join(os.path.dirname(latency_csv), "summary.json"), "w") as f:
        json.dump(summary, f, indent=1, default=lambda v: None if v != v else v)

    print(f"""  {B}╔{line}╗
  ║   COST PER AGENT PER MONTH  ≈  ${total:.2f}{' ' * (30 - len(f'{total:.2f}'))}║
  ╚{line}╝{R}
  {D}vs ${worker_mo:.2f}/mo for a dedicated always-on worker — {worker_mo / total:.0f}× cheaper.
  Multi-actor workers (roadmap, CPU-packed): ≈{ma_node} agents/machine →
  ≈${ma_cost:.2f}/agent/mo — a projection, not today's price.
  Excludes LLM tokens and amortized cluster fee/control plane (add
  ~$573/mo ÷ fleet size). Utilization/peak are assumptions; T_s, T_r,
  snapshot size and density above are measured.
  Latency percentiles (P50/P90/P99/max, with n): {latency_csv}{R}
""")


if __name__ == "__main__":
    main()
