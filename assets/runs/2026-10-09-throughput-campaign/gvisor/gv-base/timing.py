#!/usr/bin/env python3
"""Per-level stage medians from Substrate 'timing breakdown' JSON lines.
usage: timing.py TIMING_FILE TURN_FILE [--p90]
Levels come from the sim's 'swap level' (start) and 'swap level result' lines in TURN_FILE."""
import json, sys, statistics, datetime
def ts(s):
    s=s.rstrip('Z'); 
    if '.' in s: a,b=s.split('.'); s=a+'.'+b[:6]
    return datetime.datetime.fromisoformat(s)
tf, turn = sys.argv[1], sys.argv[2]
p90 = '--p90' in sys.argv
levels=[]  # (start, end, label)
starts=[]
for line in open(turn):
    if '"swap level"' in line and '"n_per_tick"' in line:
        j=json.loads(line[line.index('{'):]); starts.append((ts(j['time']), j['n_per_tick']))
    elif '"swap hold"' in line:
        j=json.loads(line[line.index('{'):]); starts.append((ts(j['time']), 'hold'))
starts.sort()
for i,(t,n) in enumerate(starts):
    end = starts[i+1][0] if i+1 < len(starts) else t+datetime.timedelta(minutes=2 if n!='hold' else 4)
    levels.append((t,end,n))
recs=[]
for line in open(tf):
    if 'timing breakdown' not in line: continue
    try: j=json.loads(line[line.index('{'):])
    except Exception: continue
    recs.append((ts(j['time']), j))
stages={
 'worker restore':('Restore timing breakdown','ateom.actor.restore.duration.',['net_setup','pause_create','pause_restore','app_create','app_restore','total']),
 'worker checkpoint':('Checkpoint timing breakdown','ateom.actor.checkpoint.duration.',['checkpoint','teardown','total']),
 'atelet restore':('Restore timing breakdown','ate.actor.restore.duration.',['manifest_fetch','download','ateom_restore','total']),
 'atelet checkpoint':('Checkpoint timing breakdown','ate.actor.checkpoint.duration.',['ateom_checkpoint','persist','total']),
}
agg = (lambda v: sorted(v)[int(0.9*len(v))-1 if len(v)>1 else 0]) if p90 else statistics.median
print(("P90" if p90 else "median")+" ms per stage per level")
for name,(msg,prefix,keys) in stages.items():
    print(f"\n{name}: | level | n | "+" | ".join(keys)+" |")
    for (a,b,lab) in levels:
        sel=[j for (t,j) in recs if a<=t<b and j.get('msg')==msg and (prefix+'total') in j]
        if not sel: continue
        vals=[]
        for k in keys:
            v=[j[prefix+k]*1000 for j in sel if prefix+k in j]
            vals.append(f"{agg(v):.0f}" if v else "-")
        print(f"| {lab} | {len(sel)} | "+" | ".join(vals)+" |")
