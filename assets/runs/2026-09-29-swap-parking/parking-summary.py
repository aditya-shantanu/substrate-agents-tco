import json,sys,re
p=sys.argv[1]; placed=live=failed=None; dead=0; waves=[]; verdict=""
for l in open(p,errors="replace"):
    if '"setup complete"' in l:
        j=json.loads(l); placed=j.get("agents"); failed=j.get("failed")
    elif "dead after placement" in l: dead+=1
    elif '"wave result"' in l:
        j=json.loads(l); waves.append(j)
    elif "LOADTEST VERDICT" in l: verdict=l.strip()
print(f"placed(live)={placed} setup_failed={failed} (of which dead-after-placement={dead})")
for w in waves:
    print(f"  wave {w['active_agents']:>3} active: acts={w['activations']:>3} refusal={w['refusal_pct']}% err={w['error_pct']}% wake p50={w['wake_p50_ms']}ms p99={w['wake_p99_ms']}ms turn p99={w.get('turn_p99_ms')} probe p50={w.get('probe_p50_ms')}ms failed={w['failed']} {w.get('failed_on','')}")
print(" ", verdict)
