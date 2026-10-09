#!/usr/bin/env bash
# Regenerates /tmp/tco-runs/fill/gv-report.md from the pieces in gv-base (run after every iteration).
set -u; cd /tmp/tco-runs/fill/gv-base
R=/tmp/tco-runs/fill/gv-report.md
{
cat <<'HDR'
# gVisor activation-throughput campaign — agents-tco-east (c3-standard-192-metal, 1 node, 100 worker pods)

Workload: nano-personal-agent, 5,000 registered actors (`turn-1791517949-gvisor-NNNN`, atespace agents-sim), 650 awake, 2 vCPU + 2 GiB.
Sim: swap mode, 1-s ticks, SWAP_START=2 x1.5 (2,3,5,8,12,18… swaps/s), 2-min levels, hold 4 min at the last clean level.
Gates: wake P90 > 2.5x the first level's P90 (FAIL_REL_P90=2.5), errors/refusals > 0.5 %, backlog (> 5 s of swaps in flight at level end), node memory/PSI/crashed.
Substrate: upstream main 66f8a888 in worktree /Users/adityashantanu/repos/substrate-east (+ the ACTOR_CPU deploy patch), uncommitted.
Raw data: /tmp/tco-runs/fill/gv-base/ (turn-iter*.txt = sim JSON lines, timing-iter*.txt = node "timing breakdown" lines, hostprobe-iter*.log, worker log samples, probe YAMLs, this report's generator assemble_report.sh) and /tmp/tco-runs/fill/nanoturn3-east-<n>/.
HDR
echo; echo "_Last updated: $(date '+%Y-%m-%d %H:%M %Z')._"; echo
[ -f status.md ] && cat status.md
cat <<'SUM'

## Summary of levels per build (wake = resume as seen by the sim, park = suspend; ms)

| build | 5/s wake P50/P90 | 8/s wake P50/P90 | 12/s wake P50/P90 | 12/s park P50 | first failing level (reason) | last clean level |
|---|---|---|---|---|---|---|
SUM
python3 - <<'PY'
import json,glob,os
names=[("1 baseline","turn-iter1-baseline.txt"),("2 +plugin/atelet patch","turn-iter2-plugin.txt"),("3 +netns pool","turn-iter3-netpool.txt"),("4 +shared-root","turn-iter4-sharedroot.txt"),("5 +ignore-cgroups (diagnostic, rejected)","turn-iter5-nocgroups.txt"),("6 build 4 + node cgroup2 favordynmods","turn-iter6-favordynmods.txt"),("7 + reusable cgroup slots","turn-iter7-cgslots.txt"),("8 build C: no leak + no app-container cgroup","turn-iter8-varC.txt"),("9 build C repeat (harness re-deployed the worktree)","turn-iter9-buildCrepeat.txt"),("10 build D (C + netns by descriptor), sim catch-up OFF","turn-iter10-D-nocatchup.txt"),("11 build D, catch-up OFF, x1.25 ramp","turn-iter11-D-ramp125.txt")]
for name,f in names:
    if not os.path.exists(f): continue
    rows={}
    for l in open(f):
        if '"swap level result"' in l:
            j=json.loads(l[l.index('{'):]); rows[j['n_per_tick']]=j
    g=lambda n: f"{rows[n]['wake_p50_ms']}/{rows[n]['wake_p90_ms']}" if n in rows else "-"
    fail=[(n,rows[n]['failed_on']) for n in sorted(rows) if rows[n].get('failed')]
    clean=[n for n in sorted(rows) if not rows[n].get('failed')]
    print(f"| {name} | {g(5)} | {g(8)} | {g(12)} | {rows[12]['park_p50_ms'] if 12 in rows else '-'} | {fail[0][0] if fail else '-'} ({fail[0][1] if fail else 'none'}) | {max(clean) if clean else '-'} |")
PY
echo; echo "## Where the time goes — stage medians (ms) per build at 5 / 8 / 12 swaps/s"; echo
python3 cross_iter.py
[ -f conclusions.md ] && cat conclusions.md
[ -f findings.md ] && cat findings.md
[ -f changes.md ] && cat changes.md
for f in probes-iter10.md probes-iter11.md; do [ -f $f ] && { echo; echo "### Node probes per phase (${f%.md})"; echo; cat $f; }; done
echo; echo "## Per-iteration detail"
for it in "1 baseline (main 66f8a888 + ACTOR_CPU patch)|turn-iter1-baseline.txt|timing-iter1-baseline.txt|hostprobe-iter1.log" "2 + snapshot-plugin/atelet patch|turn-iter2-plugin.txt|timing-iter2-plugin.txt|hostprobe-iter2.log" "3 + sandbox network-namespace pool (ATE_NETNS_POOL=16)|turn-iter3-netpool.txt|timing-iter3-netpool.txt|hostprobe-iter3.log" "4 + runsc --shared-root per worker|turn-iter4-sharedroot.txt|timing-iter4-sharedroot.txt|hostprobe-iter4.log" "5 + runsc --ignore-cgroups (DIAGNOSTIC)|turn-iter5-nocgroups.txt|timing-iter5-nocgroups.txt|hostprobe-iter5.log" "6 build 4 + node cgroup2 favordynmods remount|turn-iter6-favordynmods.txt|timing-iter6-favordynmods.txt|hostprobe-iter6.log" "7 + reusable cgroup slots (worker-owned, runsc neither creates nor removes cgroups)|turn-iter7-cgslots.txt|timing-iter7-cgslots.txt|hostprobe-iter7.log" "8 build C = pool + shared-root + lean teardown (pause always deleted, no cgroup leak) + no cgroup for the app container|turn-iter8-varC.txt|timing-iter8-varC.txt|hostprobe-iter8.log" "9 build C again (the harness re-deployed a fresh build of the worktree = build C; repeatability on the same fleet)|turn-iter9-buildCrepeat.txt|timing-iter9-buildCrepeat.txt|hostprobe-iter9.log" "10 build D = build C + pooled netns handed to runsc by descriptor path (no bind mounts), with the sim first-lap catch-up disabled (SCRIPT_CATCHUP=false) — the new reference|turn-iter10-D-nocatchup.txt|timing-iter10-D-nocatchup.txt|hostprobe-iter10.log" "11 build D, catch-up off, finer x1.25 ramp (2,3,4,5,7,9,12,15…) to locate the ceiling|turn-iter11-D-ramp125.txt|timing-iter11-D-ramp125.txt|hostprobe-iter11.log"; do
  IFS='|' read -r title turn timing host <<<"$it"; [ -f "$turn" ] || continue
  echo; echo "### Iteration $title"; echo; python3 levels.py $turn
  if [ -f "$timing" ]; then echo; echo "Stage medians per level (worker = ateom-gvisor lines; atelet = node-agent lines, partly lost to kubelet log rotation):"; echo; python3 timing.py $timing $turn | sed 's/^worker restore: //; s/^worker checkpoint: //; s/^atelet restore: //; s/^atelet checkpoint: //' | sed 's/^| level | n |/\n| level | n |/'; fi
  if [ -f "$host" ]; then echo; echo "Host (hostprobe, 15-s samples):"; echo; python3 hostprobe.py $host $turn; fi
done
[ -f caveats.md ] && cat caveats.md
} > $R
wc -l $R
