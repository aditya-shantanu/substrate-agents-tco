import sys,csv,io
p=sys.argv[1]; rows=[]; active=False
for l in open(p,errors="replace"):
    l=l.strip()
    if l=="=== agentsim csv ===": active=True; continue
    if l=="=== end csv ===": break
    if active: rows.append(l)
if len(rows)<2:
    # live job: csv not yet flushed; fall back to raw csv-looking lines
    rows=["unix_ms,agent,kind,first_req_ms,readram_ms,pings,errors,refusals"]+[l.strip() for l in open(p,errors="replace") if l[:13].isdigit() and l.count(",")>=7]
rs=list(csv.DictReader(io.StringIO("\n".join(rows))))
def pct(v,p): v=sorted(v); return v[min(len(v)-1,int(p*len(v)))] if v else float('nan')
rr=[float(r["readram_ms"]) for r in rs if r.get("readram_ms") not in (None,"","0","0.0")]
fr=[float(r["first_req_ms"]) for r in rs if r.get("first_req_ms")]
err=sum(int(r["errors"]) for r in rs if r.get("errors","").isdigit())
print(f"activations={len(rs)} errors={err} first_req p50={pct(fr,.5):.0f}ms p99={pct(fr,.99):.0f}ms | readram(512Mi page-in) n={len(rr)} p50={pct(rr,.5):.0f}ms p90={pct(rr,.9):.0f}ms max={max(rr) if rr else 0:.0f}ms")
