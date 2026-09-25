#!/usr/bin/env python3
"""Density/oversubscription report for a Phase 2 run.

Inputs:
  --sim-log      output of `kubectl -n agent-sim logs job/agentsim` (contains
                 the "=== agentsim csv ===" section)
  --occupancy    CSV fetched from the autosuspender:
                 kubectl -n agent-sim port-forward svc/autosuspender 8080 &
                 curl -s localhost:8080/occupancy.csv > occupancy.csv
  --compress     the --compress the sim ran with (default 60)
  --worker-cost-hr  $/hr of one worker (node $/hr ÷ workers per node), to
                 print a measured cost-per-agent line (optional)

Methodology follows always-on-agent's demo/measure/density.py: report the
PEAK, not the average — the average density equals 1/duty-cycle and is a
property of the workload's idleness, not of the platform's packing. Size (and
price) a pool on the peak.
"""
import argparse
import csv
import io
import statistics
import sys


def pct(sorted_vals, p):
    if not sorted_vals:
        return float("nan")
    i = min(len(sorted_vals) - 1, max(0, int(p * len(sorted_vals)) - 1))
    return sorted_vals[i]


def extract_sim_csv(path):
    rows, active = [], False
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line == "=== agentsim csv ===":
                active = True
                continue
            if line == "=== end csv ===":
                break
            if active:
                rows.append(line)
    if not rows:
        sys.exit(f"no agentsim csv section found in {path}")
    return list(csv.DictReader(io.StringIO("\n".join(rows))))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--sim-log", required=True)
    ap.add_argument("--occupancy", required=True)
    ap.add_argument("--compress", type=float, default=60.0)
    ap.add_argument("--worker-cost-hr", type=float, default=None)
    args = ap.parse_args()

    sim = extract_sim_csv(args.sim_log)
    agents = len({r["agent"] for r in sim})
    acts = len(sim)
    errors = sum(int(r["errors"]) for r in sim)
    refusals = sum(int(r.get("refusals", 0)) for r in sim)
    t0 = min(int(r["unix_ms"]) for r in sim)
    t1 = max(int(r["unix_ms"]) for r in sim)
    window_s = (t1 - t0) / 1000 or 1

    print("== workload ==")
    print(f"agents={agents} activations={acts} window={window_s/60:.1f} min "
          f"(compressed x{args.compress:.0f} -> {window_s*args.compress/3600:.1f} h simulated)")
    print(f"errors={errors} router refusals (503/504, retried)={refusals}")
    for kind in sorted({r["kind"] for r in sim}):
        v = sorted(float(r["first_req_ms"]) for r in sim if r["kind"] == kind)
        print(f"  {kind:8s} n={len(v):5d} first-request ms "
              f"p50={pct(v,.5):.0f} p90={pct(v,.9):.0f} p99={pct(v,.99):.0f}")
    walks = sorted(float(r["readram_ms"]) for r in sim if float(r["readram_ms"]) > 0)
    if walks:
        print(f"  post-resume RAM walk ms p50={pct(walks,.5):.0f} p99={pct(walks,.99):.0f}")

    with open(args.occupancy) as f:
        occ = [r for r in csv.DictReader(f)
               if t0 <= int(r["unix_ms"]) <= t1]
    if not occ:
        sys.exit("no occupancy samples inside the sim window — was the "
                 "autosuspender sampling while the job ran?")
    assigned = sorted(int(r["workers_assigned"]) for r in occ)
    running = [int(r["running"]) + int(r["resuming"]) + int(r["suspending"]) for r in occ]
    total_workers = int(occ[-1]["workers_total"])

    mean_assigned = statistics.fmean(assigned)
    p99_assigned = pct(assigned, .99)
    peak_assigned = assigned[-1]

    print("\n== occupancy (sampled) ==")
    print(f"samples={len(occ)} worker pool={total_workers}")
    print(f"workers assigned: mean={mean_assigned:.2f} p99={p99_assigned} peak={peak_assigned}")
    print(f"actors holding/claiming a worker (running+resuming+suspending): "
          f"mean={statistics.fmean(running):.2f} peak={max(running)}")

    print("\n== density ==")
    duty_eff = mean_assigned / agents
    print(f"effective per-agent worker occupancy (compressed): {100*duty_eff:.2f}%")
    print(f"achieved density  (mean):  {agents/max(mean_assigned,1e-9):6.1f} : 1   <- workload idleness, not bankable")
    print(f"achieved density  (p99):   {agents/max(p99_assigned,1e-9):6.1f} : 1   <- size the pool on this")
    print(f"achieved density  (peak):  {agents/max(peak_assigned,1e-9):6.1f} : 1")
    print(f"decompressed duty estimate: {100*duty_eff/args.compress:.3f}% "
          f"-> naive real-world mean density ~{args.compress*agents/max(mean_assigned,1e-9):,.0f}:1 "
          f"(upper bound: assumes switch overhead scales with duty, which it does not; "
          f"re-run at lower compression to tighten)")

    if args.worker_cost_hr is not None:
        pool = max(p99_assigned, 1)
        cost = args.worker_cost_hr * 730 * pool / agents
        print("\n== measured cost (at compressed duty cycle) ==")
        print(f"pool sized to p99 peak = {pool} workers @ ${args.worker_cost_hr:.4f}/hr")
        print(f"compute cost/agent-month = ${cost:.2f} (+ snapshot storage + ops; "
              f"real-world duty is {args.compress:.0f}x lower -> proportionally cheaper, "
              f"floor set by switch overhead per activation)")


if __name__ == "__main__":
    main()
