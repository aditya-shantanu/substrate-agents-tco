#!/usr/bin/env python3
"""Summarize agentsim 'swap level result' lines (markdown table)."""
import json, sys
cols=["n_per_tick","target_swaps_per_s","achieved_swaps_per_s","wakes","parks","wake_p50_ms","wake_p90_ms","wake_p99_ms","park_p50_ms","park_p90_ms","park_p99_ms","errors","refusals","backlog","crashed","failed","failed_on"]
short=["N/tick","target/s","achieved/s","wakes","parks","wake P50","P90","P99","park P50","P90","P99","err","refused","backlog","crashed","failed","failed_on"]
rows={}
for path in sys.argv[1:]:
    for line in open(path):
        if '"swap level result"' not in line: continue
        try: j=json.loads(line[line.index('{'):])
        except Exception: continue
        rows[(j.get("time"),j.get("n_per_tick"))]=j
print("| "+" | ".join(short)+" |"); print("|"+"---|"*len(short))
for k in sorted(rows):
    j=rows[k]; print("| "+" | ".join(str(j.get(c,"")) for c in cols)+" |")
