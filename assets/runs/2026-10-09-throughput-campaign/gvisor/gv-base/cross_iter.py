#!/usr/bin/env python3
import json, statistics, datetime, os
def ts(s):
    s=s.rstrip('Z'); a,b=s.split('.'); return datetime.datetime.fromisoformat(a+'.'+b[:6])
iters=[("1 baseline","turn-iter1-baseline.txt","timing-iter1-baseline.txt"),("2 +plugin","turn-iter2-plugin.txt","timing-iter2-plugin.txt"),("3 +netns pool","turn-iter3-netpool.txt","timing-iter3-netpool.txt"),("4 +shared-root","turn-iter4-sharedroot.txt","timing-iter4-sharedroot.txt"),("5 +ignore-cgroups (rejected)","turn-iter5-nocgroups.txt","timing-iter5-nocgroups.txt"),("6 build4 + favordynmods","turn-iter6-favordynmods.txt","timing-iter6-favordynmods.txt"),("7 +cgroup slots","turn-iter7-cgslots.txt","timing-iter7-cgslots.txt"),("8 build C: runsc-owned cgroups, lean teardown (no leak), no app-container cgroup","turn-iter8-varC.txt","timing-iter8-varC.txt"),("9 build C repeat","turn-iter9-buildCrepeat.txt","timing-iter9-buildCrepeat.txt"),("10 build D, catch-up OFF","turn-iter10-D-nocatchup.txt","timing-iter10-D-nocatchup.txt"),("11 build D, x1.25 ramp","turn-iter11-D-ramp125.txt","timing-iter11-D-ramp125.txt")]
iters=[i for i in iters if os.path.exists(i[1]) and os.path.exists(i[2])]
rows={}
for name,turn,timing in iters:
    starts=[]
    for line in open(turn):
        if '"swap level"' in line and '"n_per_tick"' in line:
            j=json.loads(line[line.index('{'):]); starts.append((ts(j['time']), j['n_per_tick']))
        elif '"swap hold"' in line:
            j=json.loads(line[line.index('{'):]); starts.append((ts(j['time']),'hold'))
    starts.sort()
    lv=[(t, starts[i+1][0] if i+1<len(starts) else t+datetime.timedelta(minutes=4), n) for i,(t,n) in enumerate(starts)]
    recs=[]
    for line in open(timing):
        if 'timing breakdown' not in line: continue
        try: j=json.loads(line[line.index('{'):])
        except Exception: continue
        recs.append((ts(j['time']),j))
    for (a,b,n) in lv:
        if n not in (5,8,12,18): continue
        R=[j for t,j in recs if a<=t<b and j['msg']=='Restore timing breakdown' and 'ateom.actor.restore.duration.total' in j]
        C=[j for t,j in recs if a<=t<b and j['msg']=='Checkpoint timing breakdown' and 'ateom.actor.checkpoint.duration.total' in j]
        A=[j for t,j in recs if a<=t<b and j['msg']=='Restore timing breakdown' and 'ate.actor.restore.duration.total' in j]
        m=lambda L,k: f"{statistics.median([j[k]*1000 for j in L]):.0f}" if L else "-"
        p='ateom.actor.restore.duration.'; q='ateom.actor.checkpoint.duration.'
        rows[(n,name)]=[m(R,p+'net_setup'),m(R,p+'pause_create'),m(R,p+'pause_restore'),m(R,p+'app_create'),m(R,p+'app_restore'),m(R,p+'total'),m(C,q+'checkpoint'),m(C,q+'teardown'),m(C,q+'total'),m(A,'ate.actor.restore.duration.download'),m(A,'ate.actor.restore.duration.total')]
print("| swaps/s | build | net_setup | pause_create | pause_restore | app_create | app_restore | worker restore total | checkpoint | teardown | worker checkpoint total | atelet download | atelet restore total |")
print("|---|---|---|---|---|---|---|---|---|---|---|---|---|")
order=[i[0] for i in iters]
for k in sorted(rows, key=lambda k:(k[0],order.index(k[1]))):
    print(f"| {k[0]} | {k[1]} | "+" | ".join(rows[k])+" |")
