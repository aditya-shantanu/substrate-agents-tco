# Research: Agent Substrate internals (for cost model + Phase 2 service)

Compiled 2026-09-25 from the local repo at `~/repos/substrate` (subagent
deep-dive). File paths refer to that repo.

## Architecture in one paragraph

Substrate maps many **actors** (sandbox instances, rows in Postgres behind
`ate-api-server`) onto a pool of pre-warmed **workers** (pods from a
`WorkerPool` CRD). `atelet` (DaemonSet) supervises workers and moves snapshots
to/from object storage; `atenet` (Envoy + ext_proc router) routes
`<actor>.<atespace>.actors.…` traffic and **auto-resumes actors on inbound
requests**; `ateom-gvisor` / `ateom-microvm` inside each worker pod does
checkpoint/restore. Actor states: SUSPENDED → RESUMING → RUNNING → SUSPENDING →
SUSPENDED, plus PAUSED (node-local snapshot, resume pinned to node).

## Facts that drive the cost model

| Fact | Value | Source |
|---|---|---|
| Actors per worker (today) | **1** (`actorsPerAteom = 1`) | `internal/ateomcapacity/ateomcapacity.go:45`; multi-actor epic #1266 control-plane-ready only |
| gVisor node requirements | **none special** — runsc runs unprivileged inside ordinary pods (no GKE Sandbox RuntimeClass, no nested virt) | `cmd/atecontroller/internal/controllers/workerpool_apply.go` |
| microVM node requirements | **`/dev/kvm` → nested virtualization**; extended resource `ate.dev/kvm` via atelet device plugin | `internal/deviceplugin/deviceplugin.go:56`, `docs/dev/microvm-local.md` ("GCE N2/N2D with nested virt") |
| microVM hypervisor | Cloud Hypervisor (direct REST, Kata guest assets), snapshot = sparse memory-ranges + rootfs upper tar; restore supports userfaultfd on-demand paging | `cmd/ateom-microvm/{run,checkpoint,restore}.go` |
| Snapshot storage | GCS (zstd-compressed), per-template `snapshotsConfig.storageLocation: gs://…` | `cmd/atelet/internal/ategcs/` |
| Snapshot size | ∝ resident RAM + rootfs delta; ~**30 MB** per small (256Mi) glutton FULL snapshot | `docs/dev/scale-test-readiness.md` |
| GCS ops per cycle | 5–9 ops/suspend, 3–6 ops/resume | scale-test-readiness |
| gVisor per-worker overhead | ~**200 MiB** (sentry/gofer/ateom) — actor limit ≤ pod limit − 200Mi | scale doc |
| microVM overhead | **128 MiB** VMM reserve per actor (`--vmm-mem-reserve-mib`); min actor memory 256Mi | `cmd/ateom-microvm/run.go` |
| Workers/node target | 10 workers/node on c4-standard-4 at 300m/1Gi each | scale-test-readiness |
| Default GKE setup | `tools/setup-gcp`: c3-standard-4 pool, GCS bucket, Workload Identity; gVisor only (microVM via `hack/install-microvm-deps.sh`) | `tools/setup-gcp/cmd/cluster.go` |
| Latency claims | sub-second suspend/resume; North Star 100ms p95 activation; demo: ~250 actors on 8 pods (30×+) | README, `docs/architecture.md` |
| Cold-boot opt | microVM boot 21.6s → 1.9s (nested-virt arm64 kind) | `cmd/ateom-microvm/run.go` comment |
| Saturation behavior | `ResourceExhausted` → router **parks** request (5s budget, 1024/replica) → 503 | `docs/request-parking.md` |

Two suspend flavors matter for the model:
- **SuspendActor** — durable: checkpoint + zstd + GCS upload, frees the worker
  anywhere. This is the density lever.
- **PauseActor** — node-local checkpoint (no GCS), resume pinned to that node.
  Faster, but frees worker capacity only on that node and survives less.

Snapshot scopes: `FULL` (memory + fs + durable) vs `DATA` (durable dir only →
cold boot on resume, much smaller/cheaper).

## API surface (for the Phase 2 service)

gRPC `service Control` in `pkg/proto/ateapipb/ateapi.proto` (mTLS, pod
certificates; Python stubs already generated in
`benchmarking/locust/common/ateapi_pb2*.py`, channel helper
`ateapi_channel.py` → `api.ate-system.svc:443`):

- `CreateActor`, `GetActor`, `ListActors`, `DeleteActor(any_state)`
- `SuspendActor(actor)` — no options
- `PauseActor(actor)` — node-local
- `ResumeActor(actor, boot)` — `boot=true` = cold OCI boot, skip snapshot;
  response `resumed=false` if already RUNNING
- `ListWorkers`, `ListWorkerActorAssignments`, `DrainWorker`
- `CreateAtespace`, `CreateActorTemplate`, Tags CRUD (immutable snapshot pins)

**There is no built-in idle detection or auto-suspend.** (`docs/roadmap.md`
lists "GC of idle actors based on TTL" as a future idea.) Suspends must be
driven by a client — this is exactly the Phase 2 service.

**Resume-on-request exists already**: the router resumes a suspended actor when
traffic arrives for it (implicit resume), with request parking while it waits.
So the minimal viable auto-suspend service = idle-watcher that calls
`SuspendActor`; resume comes for free via the router.

## Glutton (workload emulator)

- Server: `cmd/benchmarking/glutton/main.go`; proto
  `internal/proto/glutton/glutton.proto`. HTTP mode (`--mode=http`) routes:
  `/ping /writeram /readram /writedisk /readdisk`, `/readyz` (gates resume).
  RPCs: `WriteRAM(size, TRUNCATE|OVERWRITE|OVERWRITE_ROTATE)`, `ReadRAM`
  (touch 1 byte/4KiB page — measures demand-paging after resume), `WriteDisk`,
  `ReadDisk`, `OpenFD`, `Gossip`.
- ActorTemplate: `benchmarking/workloads/manifests/glutton-template.yaml.tmpl`
  (HTTP on :80, FULL snapshots to `gs://$BUCKET/benchmark-workloads/glutton/`,
  default actor memory 256Mi).
- Load driver: Go boomer worker (`cmd/benchmarking/boomer-worker`,
  `internal/benchmarking/boomer/glutton/lifecycle.go`). Per-VU loop:
  Resume → fill RAM to `--mem-target` → read RAM → churn `--mem-churn` →
  HTTP Ping via router → Suspend → sleep(min..max wait). Metrics per op:
  `ResumeActor`, `SuspendActor`, `GluttonPing`, `GluttonFillRAM`,
  `GluttonChurnRAM`, `GluttonReadRAM`, cold-start variants.
- Existing test configs (`benchmarking/automation/tests.yaml`): baselines
  1/5/10 users ↔ 1/5/10 workers, 1 min, wait 1.0s;
  `glutton_oversubscribe_15_users` = 15 users on 10 workers (only 1.5×!);
  1Gi/2Gi memory suites for both gvisor and microvm; durdir suite with
  `--resume-mode implicit` (router-triggered resume).
- Repo stores **no benchmark results** (automation writes them to GCS).
  Colleague-reported numbers: avg suspend ≈ 0.4 s, avg resume ≈ 0.24 s
  (small gVisor actors), giving ≈0.61 QPS/worker at 1s wait, matching
  `QPS = workers / (live + suspend + resume)`.

## Quantitative tidbits from `docs/dev/scale-test-readiness.md`

- Largest live fleet so far: 1,000 actors / 1,000 workers (scheduler Aborted
  conflicts at that scale, since fixed-ish).
- 88 concurrent restores on n2-standard-48: p50 190s → 15s after fixes.
- Store microbench: 1M workers, UpdateWorker 1k QPS p99 27.5 ms.
- Cold restore can blow the 5s park budget (issue #1383).
- Scheduler picks uniformly at random among eligible workers (O(workers) scan).

## Deployment/ops notes for Phase 2

- Cluster: `go run ./tools/setup-gcp bootstrap` then
  `hack/install-ate.sh --deploy-ate-system`. Env: `hack/ate-dev-env.sh`.
  Kind alternative: `hack/create-kind-cluster.sh` + `hack/install-ate-kind.sh`
  (Postgres + rustfs S3) — good for local dev of the service.
- Benchmark deploy: `benchmarking/deploy_locust.sh --worker-count N`;
  workloads via `benchmarking/workloads/deploy.sh`.
- Worker pods must have resource **limits** (capacity comes from limits;
  a worker without limits is never schedulable for a template with limits).
- HPA on WorkerPool `/scale` via external metric
  `ate_workerpool_workers{state=assigned}` (see `demos/autoscaled-workerpool/`),
  e.g. averageValue 0.7 ⇒ 30% idle headroom.
- Nodes need label `ate.dev/substrate-version=<build>`.
