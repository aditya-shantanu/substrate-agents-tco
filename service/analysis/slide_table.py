#!/usr/bin/env python3
"""Render one or more runs' summary.json as the "$ / agent-month: how we get
there" table (the deck's slide 3): one column per run, one row per step of
the arithmetic, every latency as P50 / P90 / P99. Markdown out.

  slide_table.py gVisor=results/2026…-gvisor microVM=results/2026…-microvm
"""
import json
import os
import sys


def load(path):
    p = path if path.endswith(".json") else os.path.join(path, "summary.json")
    with open(p) as f:
        return json.load(f)


def ms3(q):
    if not q:
        return "—"
    return f"{q['p50']:,.0f} / {q['p90']:,.0f} / {q['p99']:,.0f} ms (n={q['n']})"


def fmt_machine(s):
    return f"{s['machine_type']}, ${s['node_mo']:,.1f}/mo ({s['price_model']})"


def rows(s):
    prof = s["profile"]
    script = prof.get("workload") == "script"
    if script:
        profile = (f"{prof['script']}: {float(prof['tasks_per_day']):g} tasks × {prof['steps']} steps/day, "
                   f"think ×{float(prof['think_scale']):g} ({float(prof['think_scaled_s']):.0f} s/task), "
                   f"{prof.get('suspend_mode')} suspend, {prof.get('min_actor_memory')} actors")
    elif prof.get("workload") == "ping":
        profile = (f"one-ping GluttonUser loop: {prof['actors_per_user']} actors/user, {float(prof['wait_s']):.0f} s wait, "
                   f"{float(prof.get('live_s', 0)):g} s live, no memory fill")
    else:
        profile = (f"personal: {float(prof.get('sessions_per_day', 3)):g} × {float(prof.get('session_minutes', 8)):g}-min sessions "
                   f"+ {float(prof.get('wakes_per_day', 40)):g} × 15-s check-ins/day")
    overhead = s["overhead_s"]
    n = s["agents_per_worker"]
    return {
        "Host, 3-yr CUD": f"{fmt_machine(s)} · {s['pool_nodes']} node(s)",
        "Workers per host → $ per worker-month":
            f"{s['workers_per_node']:.0f} → ${s['worker_mo']:.2f}",
        "Agent profile": profile,
        "Run": (f"{s['agents']} agents, time ×{s['compress']:.0f}, {s['window_min']:.0f} min; "
                f"{s['activations']} activations, {s['errors']} errors, {s['refusals']} refusals"),
        "Resume P50 / P90 / P99": ms3(s["resume_ms"]) + " ← T_r",
        "Park P50 / P90 / P99 (pause = node-local, suspend = bucket)":
            (f"**{s.get('lifecycle', 'suspend')}** "
             + (ms3(s["suspend_ms"]) if s.get("suspend_ms") else "—")
             + f" ← T_s; avg {s['t_s'] * 1000:,.0f} ms ({s['t_s_source']}, n={s['suspends']:.0f})"),
        "Step work P50 / P90 / P99": ms3(s["step_ms"]) if s.get("step_ms") else "—",
        "Worker time per wake-up": f"{overhead:.1f} s (wait + T_s + T_r)",
        "Occupancy (worker time per agent)": f"{s['occupancy'] * 100:.2f} %  ({s['day']})",
        "Measured density": (f"{s['density_mean']:.1f}:1 mean · {s['density_p99']:.1f}:1 at P99 "
                             f"(busy workers mean {s['busy_mean']:.1f}, P99 {s['busy_p99']}, peak {s['busy_peak']} of {s['pool_workers']})"
                             if s.get("density_mean") else "n/a (no occupancy samples in the window)"),
        "Agents per worker (overcommit)":
            f"{n:.1f} = {s['utilization']:.2f} ÷ ({s['occupancy'] * 100:.2f} % × {s['peak_value']:g} {'peak' if s['peak_model'] == 'mult' else 'herd'})",
        "Agents per host": f"{s['agents_per_host']:.0f}",
        "$ / agent-month": (f"${s['worker_mo']:.2f} ÷ {n:.1f} = ${s['compute']:.2f} compute + ${s['ops']:.2f} GCS ops "
                            f"+ ${s['storage']:.3f} snapshots = **${s['total']:.2f}**"),
        "Multi-actor projection (roadmap)": f"≈{s['multi_actor_agents_per_host']} agents/host → ≈${s['multi_actor_total']:.2f}",
    }


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    cols = []
    for arg in sys.argv[1:]:
        name, _, path = arg.partition("=")
        if not path:
            name, path = os.path.basename(arg.rstrip("/")), arg
        cols.append((name, rows(load(path))))
    keys = []
    for _, r in cols:  # union of rows, first column's order first
        keys += [k for k in r if k not in keys]
    print("| Step | " + " | ".join(n for n, _ in cols) + " |")
    print("|---|" + "---|" * len(cols))
    for k in keys:
        print(f"| {k} | " + " | ".join(r.get(k, "—") for _, r in cols) + " |")


if __name__ == "__main__":
    main()
