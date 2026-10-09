#!/usr/bin/env bash
set -u; export KUBECONFIG=/tmp/tco-runs/metal2.kubeconfig
perl -e 'alarm 590; exec @ARGV' -- ~/go/bin/kubectl-ate get actors -a agents-sim -o json 2>/dev/null | python3 -c "
import sys,json; d=json.load(sys.stdin); items=d.get('actors',d) if isinstance(d,dict) else d
open('/tmp/tco-runs/fill/to-park.txt','w').write('\n'.join(a['metadata']['name'] for a in items if a.get('status',{}).get('state')=='ACTOR_STATE_RUNNING')+'\n')
import collections; print(collections.Counter(a.get('status',{}).get('state') for a in items))"
n=$(grep -c . /tmp/tco-runs/fill/to-park.txt); echo "parking $n running actors"; [[ $n -gt 0 ]] && cat /tmp/tco-runs/fill/to-park.txt | xargs -P 16 -I{} sh -c '~/go/bin/kubectl-ate ${PARK_VERB:-suspend} actor {} -a agents-sim >/dev/null 2>&1 || echo "park failed {}"' | tail -3; sleep 15
