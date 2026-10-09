import sys, time, threading, urllib.request, urllib.parse, subprocess, os
objs = sys.argv[1].split()
tok = subprocess.check_output(["gcloud","auth","print-access-token"]).decode().strip()
def get(o, out):
    b, k = o.split('/',1)
    req = urllib.request.Request(f"https://storage.googleapis.com/{b}/{urllib.parse.quote(k)}", headers={"Authorization": "Bearer "+tok})
    s=time.time(); n=0
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            while True:
                c=r.read(1<<20)
                if not c: break
                n+=len(c)
    except Exception as e:
        n=-1
        if not out: print('ERR', repr(e)[:300], req.full_url[:200], flush=True)
    out.append((n, time.time()-s))
for conc in (1, 20, 40, 80, 160):
    out=[]; t0=time.time()
    th=[threading.Thread(target=get, args=(objs[i % len(objs)], out)) for i in range(conc)]
    [t.start() for t in th]; [t.join() for t in th]
    el=time.time()-t0; tot=sum(n for n,_ in out if n>0); per=sorted(d for n,d in out if n>0)
    print(f"conc {conc:3d}: {tot/1e6:7.0f} MB in {el:5.2f} s = {tot/1e6/el:6.0f} MB/s aggregate; per-object p50 {per[len(per)//2]*1000:5.0f} ms max {per[-1]*1000:5.0f} ms; failures {sum(1 for n,_ in out if n<0)}", flush=True)
