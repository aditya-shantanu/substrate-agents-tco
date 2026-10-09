#!/usr/bin/env python3
"""hostprobe.py HOSTPROBE_LOG TURN_FILE: per-level host CPU busy %, disk MB/s (read+write), MemAvailable GiB"""
import sys, json, datetime
samples=[]; cur=None
for line in open(sys.argv[1]):
    p=line.split()
    if not p: continue
    if p[0]=='T': cur={'t':int(p[1])}
    elif cur is None: continue
    elif p[0]=='cpu': cur['cpu']=list(map(int,p[1:]))
    elif p[0]=='MemAvailable:': cur['mem']=int(p[1])
    elif len(p)>=14 and p[2].startswith('nvme0n1') and p[2]=='nvme0n1': cur['rd']=int(p[5]); cur['wr']=int(p[9])
    elif p[0]=='END' and 'cpu' in cur: samples.append(cur); cur=None
def ts(s):
    s=s.rstrip('Z'); a,b=s.split('.'); return datetime.datetime.fromisoformat(a+'.'+b[:6]).replace(tzinfo=datetime.timezone.utc).timestamp()
lv=[]
for line in open(sys.argv[2]):
    if ('"swap level"' in line and '"n_per_tick"' in line) or '"swap hold"' in line:
        j=json.loads(line[line.index('{'):]); lv.append((ts(j['time']), j.get('n_per_tick') if 'swap level"' in line else 'hold'))
lv.sort()
print("| level | samples | CPU busy % | disk MB/s | MemAvail GiB |"); print("|---|---|---|---|---|")
for i,(t0,lab) in enumerate(lv):
    t1 = lv[i+1][0] if i+1<len(lv) else t0+240
    sel=[s for s in samples if t0<=s['t']<t1 and 'rd' in s]
    if len(sel)<2: continue
    a,b=sel[0],sel[-1]
    d=[y-x for x,y in zip(a['cpu'],b['cpu'])]; tot=sum(d); idle=d[3]+d[4]
    secs=b['t']-a['t']; mb=((b['rd']-a['rd'])+(b['wr']-a['wr']))*512/1e6/secs
    print(f"| {lab} | {len(sel)} | {100*(1-idle/tot):.1f} | {mb:.0f} | {min(s['mem'] for s in sel)/2**20:.0f} |")
