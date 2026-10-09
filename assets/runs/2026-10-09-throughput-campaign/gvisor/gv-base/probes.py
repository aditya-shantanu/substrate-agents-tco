#!/usr/bin/env python3
"""probes.py TURN_FILE: per-phase medians of the node probes (fork+exec µs, cgroup mkdir/attach-pair/rmdir ms, mount bind+umount pair µs, unshare -m µs)."""
import json, datetime, statistics, sys, os
G='/tmp/tco-runs/fill/gv-base'
def ts(s):
    s=s.rstrip('Z'); a,b=s.split('.'); return datetime.datetime.fromisoformat(a+'.'+b[:6]).replace(tzinfo=datetime.timezone.utc).timestamp()
lv=[]
for line in open(sys.argv[1]):
    if ('"swap level"' in line and '"n_per_tick"' in line) or '"swap hold"' in line or '"swap fill"' in line:
        j=json.loads(line[line.index('{'):]); lab = j.get('n_per_tick') if 'swap level"' in line else ('hold' if 'hold' in line else 'sweep/fill')
        lv.append((ts(j['time']), lab))
lv.sort()
t_start=lv[0][0]-600 if lv else 0; t_end=lv[-1][0]+300 if lv else 0
def bucket(t):
    if t<t_start or t>t_end: return None
    lab='pre'
    for (a,l) in lv:
        if t>=a: lab=l
    return lab
def load(path, parse):
    d={}
    if not os.path.exists(path): return d
    for l in open(path):
        p=l.split()
        if len(p)<4 or p[0]!='T': continue
        b=bucket(int(p[1]))
        if b is None: continue
        v=parse(p)
        if v is not None: d.setdefault(b,[]).append(v)
    return d
forks=load(G+'/forkprobe.log', lambda p: int(p[3]))
cg=load(G+'/cgprobe-full.log', lambda p: (int(p[3]),int(p[7]),int(p[9])) if len(p)>=10 else None)
mt=load(G+'/mountprobe.log', lambda p: (int(p[3]),int(p[5])) if len(p)>=6 else None)
order=['pre','sweep/fill',2,3,4,5,7,8,9,12,15,18,27,'hold']
print("| phase | fork+exec µs med/p90 | cgroup mkdir ms | attach pair ms | rmdir ms | bind+umount pair µs | unshare -m µs |")
print("|---|---|---|---|---|---|---|")
for k in order:
    f=forks.get(k,[]); c=cg.get(k,[]); m=mt.get(k,[])
    if not (f or c or m): continue
    fm=f"{statistics.median(f):.0f} / {sorted(f)[int(0.9*len(f))]:.0f}" if f else "-"
    cm=lambda i: f"{statistics.median([x[i] for x in c]):.0f}" if c else "-"
    mm=lambda i: f"{statistics.median([x[i] for x in m]):.0f}" if m else "-"
    print(f"| {k} | {fm} | {cm(0)} | {cm(1)} | {cm(2)} | {mm(0)} | {mm(1)} |")
