#!/usr/bin/env python3
"""Cold-start table from agentsim logs: name=path/to/run.log ... → markdown
with P50 / P90 / P99 / max (and n) per phase per scenario, plus a CSV per
scenario next to each log (cold-starts.csv).

Phases, per new actor: create (CreateActor call), resume (the first
successful ResumeActor — the golden-snapshot restore onto a free worker),
first ping (resume success → first answered ping through the router),
ready (CreateActor start → first answered ping; the spawn benchmark's
ActorTimeToReady).
"""
import csv
import io
import json
import os
import sys


def pct(v, p):
    if not v:
        return float("nan")
    return v[min(len(v) - 1, max(0, int(p * len(v)) - 1))]


def load(path):
    rows, active = [], False
    for line in open(path):
        line = line.rstrip("\n")
        if line == "=== cold starts ===":
            active = True
        elif line == "=== end cold starts ===":
            break
        elif active and line.startswith(("unix_ms,", "17", "18", "19", "20")):
            rows.append(line)
    if len(rows) < 2:
        sys.exit(f"no cold-start block in {path}")
    recs = list(csv.DictReader(io.StringIO("\n".join(rows))))
    ok = [r for r in recs if not r.get("ping_err")]
    out = {"n": len(recs), "failed": len(recs) - len(ok)}
    for col, key in (("create_ms", "create"), ("resume_ms", "resume"), ("first_ping_ms", "first_ping"), ("ready_ms", "ready")):
        v = sorted(float(r[col]) for r in ok)
        out[key] = {"p50": pct(v, .5), "p90": pct(v, .9), "p99": pct(v, .99), "max": v[-1] if v else float("nan")}
    out["resume_retries"] = sum(int(r["resume_attempts"]) - 1 for r in ok)
    out["ping_retries"] = sum(int(r["ping_attempts"]) for r in ok)
    with open(os.path.join(os.path.dirname(os.path.abspath(path)), "cold-starts.csv"), "w") as f:
        f.write("\n".join(rows) + "\n")
    with open(os.path.join(os.path.dirname(os.path.abspath(path)), "cold-summary.json"), "w") as f:
        json.dump(out, f, indent=1)
    return out


def ms3(q):
    return f"{q['p50']:,.0f} / {q['p90']:,.0f} / {q['p99']:,.0f} (max {q['max']:,.0f})"


def main():
    cols = []
    for arg in sys.argv[1:]:
        name, _, path = arg.partition("=")
        cols.append((name, load(path)))
    print("| Cold start (ms, P50 / P90 / P99) | " + " | ".join(n for n, _ in cols) + " |")
    print("|---|" + "---|" * len(cols))
    for label, key in (("Ready: CreateActor → first answered ping", "ready"), ("of which first ResumeActor (golden-snapshot restore)", "resume"),
                       ("of which resume → first answered ping", "first_ping"), ("of which CreateActor", "create")):
        print(f"| {label} | " + " | ".join(ms3(c[key]) for _, c in cols) + " |")
    print("| n (failed) · retries (resume / ping) | " + " | ".join(f"{c['n']} ({c['failed']}) · {c['resume_retries']} / {c['ping_retries']}" for _, c in cols) + " |")


if __name__ == "__main__":
    main()
