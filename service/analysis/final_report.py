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


def machine_hr(mtype, model):
    # e.g. c3-standard-4, e2-standard-16, c4-highcpu-16
    fam, kind, cpus = mtype.split("-")
    cpus = int(cpus)
    gib_per_cpu = {"standard": 4, "highcpu": 2, "highmem": 8}[kind]
    if fam == "c4" and kind == "standard":
        gib_per_cpu = 3.75
    cpu_rate, gib_rate = FAM[fam]
    return (cpus * cpu_rate + cpus * gib_per_cpu * gib_rate) * PRICE_MODEL[model]


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
        elif active:
            rows.append(line)
    if len(rows) < 2:
        sys.exit(f"no agentsim csv section in {path} — did the job finish?")
    return list(csv.DictReader(io.StringIO("\n".join(rows))))


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
    agents = len({r["agent"] for r in sim})
    acts_measured = len(sim)
    errors = sum(int(r["errors"]) for r in sim)
    refusals = sum(int(r.get("refusals", 0)) for r in sim)
    t0 = min(int(r["unix_ms"]) for r in sim)
    t1 = max(int(r["unix_ms"]) for r in sim)
    window_min = (t1 - t0) / 60000 or 1

    wake_ms = sorted(float(r["first_req_ms"]) for r in sim if r["kind"] == "wake")
    sess_ms = sorted(float(r["first_req_ms"]) for r in sim if r["kind"] == "session")
    walk_ms = sorted(float(r["readram_ms"]) for r in sim if float(r["readram_ms"]) > 0)

    # suspend time from the autosuspender's own measurement
    m = dict(l.split(" ", 1) for l in open(a.metrics) if " " in l.strip()
             and not l.startswith("#") and "{" not in l.split(" ", 1)[0])
    suspends = float(m.get("autosuspend_suspends_total", 0))
    t_s = (float(m.get("autosuspend_suspend_seconds_sum", 0)) / suspends) if suspends else float("nan")
    t_r = pct(wake_ms, 0.5) / 1000 if wake_ms else float("nan")  # wake p50 = resume + routing

    occ = [r for r in csv.DictReader(open(a.occupancy)) if t0 <= int(r["unix_ms"]) <= t1]
    busy = sorted(int(r["workers_assigned"]) for r in occ) or [0]
    mean_busy, p99_busy, peak_busy = statistics.fmean(busy), pct(busy, .99), busy[-1]

    # ---- real-world projection from measured T_s / T_r ----
    live = a.sessions_per_day * a.session_minutes * 60 + a.wakes_per_day * a.wake_seconds
    A = a.sessions_per_day + a.wakes_per_day
    overhead = a.idle_timeout_real + t_s + t_r
    occ_real = (live + A * overhead) / 86400
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
    agents simulated        {agents}   (personal-agent profile, time ×{a.compress:.0f})
    worker pool             {a.pool_workers} workers on {a.pool_nodes} × {a.machine_type}
    window                  {window_min:.1f} min wall = {window_min * a.compress / 60:.1f} h of agent-life
    activations served      {acts_measured}  ({acts_measured / agents:.1f}/agent; errors {errors}, router refusals {refusals})

  What it measured
    resume (wake p50/p99)   {pct(wake_ms, .5):,.0f} / {pct(wake_ms, .99):,.0f} ms   ← T_r, user-visible wake-up
    suspend (avg)           {t_s * 1000:,.0f} ms                ← T_s, from {suspends:.0f} suspends
    RAM walk after resume   {pct(walk_ms, .5):,.0f} ms p50 (demand paging)
    workers busy            mean {mean_busy:.2f} · p99 {p99_busy} · peak {peak_busy} of {a.pool_workers}
    density (this run)      {agents / max(mean_busy, 1e-9):.1f}:1 mean · {agents / max(p99_busy, 1e-9):.1f}:1 at p99 ← bankable

  Real-world cost per agent (measured T_s/T_r plugged into the model)
    one agent/day           {live / 60:.0f} min live over {A:.0f} wake-ups
    overhead per wake-up    {a.idle_timeout_real:.0f}s wait + {t_s:.1f}s suspend + {t_r:.1f}s resume = {overhead:.1f}s
    worker-time per agent   ({live:.0f}s + {A:.0f}×{overhead:.1f}s)/86400 = {occ_real * 100:.2f}%
    agents per worker       {peak_desc}
    worker cost             ${node_hr:.4f}/hr ÷ {wpn:.1f} per node × 730h = ${worker_mo:.2f}/mo ({a.price_model})
    compute {D}${worker_mo:.2f} ÷ {n_real:.1f}{R}   ${compute:.2f}
    snapshot at rest        ${storage:.3f}   ({a.snap_gib:.2f} GiB × ${GCS_GIB_MO}/GiB-mo)
    GCS ops                 ${ops:.3f}""")
    # Multi-actor projection: pack by CPU (per-phase weights, restore is the
    # peak); suspended agents hold no RAM. Roadmap upside, not today's price.
    cpu_avg = (live * a.cpu_active + A * (t_s * a.cpu_suspend + t_r * a.cpu_restore)) / 86400
    if a.peak_model == "herd":
        blend_cpu = a.peak_value * a.cpu_restore + (1 - a.peak_value) * cpu_avg
        active_frac = a.peak_value + (1 - a.peak_value) * occ_real
    else:
        blend_cpu = cpu_avg * a.peak_value
        active_frac = occ_real * a.peak_value
    fam, kind, cpus = a.machine_type.split("-")
    gib_per_cpu = {"standard": 4, "highcpu": 2, "highmem": 8}.get(kind, 4)
    alloc_cpu, alloc_mem = int(cpus) * 0.85, int(cpus) * gib_per_cpu * 0.85
    ma_node = max(1, int(min(alloc_cpu / max(blend_cpu, 1e-9),
                             alloc_mem / max(a.active_mem_gib * active_frac, 1e-9)) * a.utilization))
    ma_cost = (node_hr * 730) / ma_node + storage + ops

    print(f"""  {B}╔{line}╗
  ║   COST PER AGENT PER MONTH  ≈  ${total:.2f}{' ' * (30 - len(f'{total:.2f}'))}║
  ╚{line}╝{R}
  {D}vs ${worker_mo:.2f}/mo for a dedicated always-on worker — {worker_mo / total:.0f}× cheaper.
  Multi-actor workers (roadmap, CPU-packed): ≈{ma_node} agents/machine →
  ≈${ma_cost:.2f}/agent/mo — a projection, not today's price.
  Excludes LLM tokens and amortized cluster fee/control plane (add
  ~$573/mo ÷ fleet size). Utilization/peak are assumptions; T_s, T_r,
  snapshot size and density above are measured.{R}
""")


if __name__ == "__main__":
    main()
