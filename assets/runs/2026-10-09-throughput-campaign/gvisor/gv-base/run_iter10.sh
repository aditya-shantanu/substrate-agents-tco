#!/usr/bin/env bash
# after iteration 9: archive it, build the build-4-semantics image from source defaults, roll, start iteration 10
set -u; G=/tmp/tco-runs/fill/gv-base; export KUBECONFIG=/tmp/tco-runs/east.kubeconfig
$G/archive_iter.sh 9 buildCrepeat 2026-10-09T09:55:00
cd /Users/adityashantanu/repos/substrate-east
sed -i '' 's/leanTeardownDefault      = true/leanTeardownDefault      = false/; s/subcontainerCgroupDefault = false/subcontainerCgroupDefault = true/' cmd/ateom-gvisor/experiment_defaults.go
gofmt -w cmd/ateom-gvisor/experiment_defaults.go
grep -E "leanTeardownDefault|subcontainerCgroupDefault" cmd/ateom-gvisor/experiment_defaults.go
export KO_DOCKER_REPO=gcr.io/gke-ai-eco-dev/ate-images KO_DEFAULTPLATFORMS=linux/amd64; VERSION=$(git describe --tags --always --dirty)
REF=$(hack/run-tool.sh ko build ./cmd/ateom-gvisor --base-import-paths --ldflags="-X=github.com/agent-substrate/substrate/internal/version.Version=${VERSION}" 2>$G/ko-build4sem.log | tail -1); echo "$REF" | tee $G/ateom-build4sem-ref.txt
$G/roll_pool.sh "$REF" 2>&1 | grep -E "^== |readyReplicas"
echo "== settling 90 s before the pipeline $(date +%T)"; sleep 90
cd /tmp/tco-runs/fill && export TAG=east ITER=10 FLEET_PREFIX=turn-1791517949-gvisor CLS=gvisor CLUSTER=agents-tco-east ZONE=us-east4-a SUBSTRATE_REPO=/Users/adityashantanu/repos/substrate-east DASH_PORT=18080 PURGE_PORT=18098
nohup bash ./nanoturn3.sh > $G/pipeline-iter10.log 2>&1 & echo "pipeline iter10 pid $!" | tee $G/pipeline-iter10.pid; date
