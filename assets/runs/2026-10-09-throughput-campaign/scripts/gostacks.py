"""Summarize a pprof goroutine?debug=1 dump: count goroutines by their top frames of interest."""
import sys, re, collections
txt=open(sys.argv[1]).read()
blocks=[b for b in txt.split('\n\n') if b.strip()]
c=collections.Counter(); total=0
for b in blocks:
    m=re.match(r'(\d+) @', b)
    if not m: continue
    n=int(m.group(1)); total+=n
    frames=re.findall(r'#\s+0x[0-9a-f]+\s+(\S+)\+', b)
    frames=[f.split('/')[-1] for f in frames]
    key=' <- '.join(frames[:3])
    # tag the state by the deepest frame
    c[key]+=n
print('total goroutines', total)
for k,v in c.most_common(int(sys.argv[2]) if len(sys.argv)>2 else 25): print(f'{v:5d}  {k}')
