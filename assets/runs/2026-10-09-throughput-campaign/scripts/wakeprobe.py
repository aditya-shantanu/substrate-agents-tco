import sys, time, threading, urllib.request, datetime
names = sys.argv[1].split()
R = "http://atenet-router.ate-system.svc.cluster.local/ping"
def wake(n, out, t0):
    req = urllib.request.Request(R, data=b"", method="POST", headers={"ate-target-actor": "agents-sim/" + n, "Content-Type": "application/x-protobuf"})
    s = time.time()
    try:
        with urllib.request.urlopen(req, timeout=120) as r: code = r.status
    except urllib.error.HTTPError as e: code = e.code
    except Exception as e: code = str(e)[:40]
    out.append((n, s - t0, time.time() - t0, code))
def batch(ns, label):
    out = []; t0 = time.time()
    print(f"== {label}: {len(ns)} wakes issued at {datetime.datetime.utcnow().isoformat()}Z", flush=True)
    th = [threading.Thread(target=wake, args=(n, out, t0)) for n in ns]
    [t.start() for t in th]; [t.join() for t in th]
    for n, s, e, c in sorted(out, key=lambda x: x[2]): print(f"  {n[-4:]} start +{s*1000:6.0f} ms  done +{e*1000:6.0f} ms  took {(e-s)*1000:6.0f} ms  {c}", flush=True)
batch(names[:1], "single wake (baseline)")
time.sleep(5)
batch(names[1:], f"{len(names)-1} concurrent wakes")
