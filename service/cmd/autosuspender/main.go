// autosuspender is a light, workload-agnostic auto-suspend service for Agent
// Substrate. Substrate has no built-in idle detection: resume-on-request is
// handled by the atenet router, but something must decide to SuspendActor.
// This service is that something, generalized from the gateway-embedded
// idle-suspender in the always-on-agent (OpenClaw) integration:
//
//   - Activity signal: whatever fronts the actors (a gateway, an ingress, or
//     the agentsim load generator) POSTs /touch, /begin and /end per actor.
//     An actor is suspended once it has been RUNNING with no touches and no
//     in-flight turns for --idle-timeout.
//   - Fallback: an actor RUNNING longer than --max-running is suspended even
//     without signals (TTL garbage collection — the roadmap's "GC of idle
//     actors"), so lost signals can't leak workers forever.
//
// It also samples worker-pool occupancy (workers assigned vs total, actor
// state counts) on an interval. The samples power three read-only views:
// a live dashboard at "/", machine-readable /state.json, and /occupancy.csv
// (the raw material for analysis/report.py).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adityashantanu/substrate-agents-tco/service/internal/ateclient"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//go:embed dashboard.html
var dashboardHTML []byte

type activity struct {
	lastTouch time.Time
	inflight  int
}

// sample is one occupancy observation; JSON field names are the dashboard's
// contract (and the /occupancy.csv column order).
type sample struct {
	T            int64 `json:"t"`
	WorkersTotal int   `json:"workers_total"`
	WorkersActiv int   `json:"workers_active"`
	Assigned     int   `json:"assigned"`
	ActorsTotal  int   `json:"actors_total"`
	Running      int   `json:"running"`
	Resuming     int   `json:"resuming"`
	Suspending   int   `json:"suspending"`
	Suspended    int   `json:"suspended"`
	Paused       int   `json:"paused"`
	Crashed      int   `json:"crashed"`
}

// maxSamples bounds the in-memory window: 6h at the default 5s interval.
const maxSamples = 4320

type server struct {
	api      ateapipb.ControlClient
	atespace string

	idleTimeout  time.Duration
	maxRunning   time.Duration
	maxParallel  int
	unwedgeAfter time.Duration

	mu       sync.Mutex
	acts     map[string]*activity // actor name -> activity
	inFlight map[string]bool      // suspends currently running

	suspends     atomic.Int64
	suspendErrs  atomic.Int64
	suspendNsSum atomic.Int64
	wedged       atomic.Int64
	unwedged     atomic.Int64 // medic delete+recreate interventions

	samplesMu sync.Mutex
	samples   []sample

	stateCounts atomic.Value // map[ateapipb.ActorState]int
}

func main() {
	var (
		endpoint    = flag.String("api-endpoint", ateclient.DefaultEndpoint, "ateapi gRPC dial target")
		serverName  = flag.String("server-name", ateclient.DefaultServerName, "TLS server name of ateapi")
		caFile      = flag.String("ca-file", ateclient.DefaultCAFile, "CA bundle verifying ateapi")
		credBundle  = flag.String("cred-bundle", ateclient.DefaultCredBundle, "pod certificate credential bundle (client mTLS)")
		atespace    = flag.String("atespace", "agents-sim", "atespace whose actors are managed")
		idleTimeout = flag.Duration("idle-timeout", 15*time.Second, "suspend after this long without activity (in the workload's clock — compress it with the sim)")
		maxRunning  = flag.Duration("max-running", 10*time.Minute, "suspend any RUNNING actor older than this even with activity signals missing")
		poll        = flag.Duration("poll", 2*time.Second, "control-plane reconcile interval")
		sampleEvery = flag.Duration("sample-interval", 5*time.Second, "worker occupancy sampling interval")
		maxParallel = flag.Int("max-concurrent", 8, "max concurrent SuspendActor calls")
		listen      = flag.String("listen", ":8080", "HTTP listen address (touch API, /metrics, /occupancy.csv)")
		unwedge     = flag.Duration("unwedge-after", 3*time.Minute, "medic: delete+recreate an actor stuck in SUSPENDING longer than this, freeing its pinned worker (0 disables). Works around a substrate race where a resume arriving mid-suspend leaves the suspend workflow uncommitted; the actor loses its state (recreated from the golden template) and the event is counted in metrics")
	)
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	conn, api, err := ateclient.Dial(*endpoint, *caFile, *credBundle, *serverName)
	if err != nil {
		slog.Error("dial ateapi", "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	s := &server{
		api:          api,
		atespace:     *atespace,
		idleTimeout:  *idleTimeout,
		maxRunning:   *maxRunning,
		maxParallel:  *maxParallel,
		unwedgeAfter: *unwedge,
		acts:         map[string]*activity{},
		inFlight:     map[string]bool{},
	}
	s.stateCounts.Store(map[ateapipb.ActorState]int{})

	ctx := context.Background()
	go s.reconcileLoop(ctx, *poll)
	go s.sampleLoop(ctx, *sampleEvery)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /touch", s.handleActivity("touch"))
	mux.HandleFunc("POST /begin", s.handleActivity("begin"))
	mux.HandleFunc("POST /end", s.handleActivity("end"))
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /occupancy.csv", s.handleOccupancy)
	mux.HandleFunc("GET /state.json", s.handleState)
	mux.HandleFunc("POST /purge", s.handlePurge)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dashboardHTML)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	slog.Info("autosuspender up", "listen", *listen, "atespace", *atespace,
		"idle_timeout", idleTimeout.String(), "max_running", maxRunning.String())
	if err := http.ListenAndServe(*listen, mux); err != nil {
		slog.Error("http server", "err", err)
		os.Exit(1)
	}
}

// handleActivity records touch/begin/end signals: ?actor=<name> (atespace is
// fixed per deployment; cross-atespace signals are rejected to keep the state
// keyed simply).
func (s *server) handleActivity(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := r.URL.Query().Get("actor")
		if actor == "" {
			http.Error(w, "missing actor", http.StatusBadRequest)
			return
		}
		if a := r.URL.Query().Get("atespace"); a != "" && a != s.atespace {
			http.Error(w, "wrong atespace", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		act := s.acts[actor]
		if act == nil {
			act = &activity{}
			s.acts[actor] = act
		}
		act.lastTouch = time.Now()
		switch kind {
		case "begin":
			act.inflight++
		case "end":
			if act.inflight > 0 {
				act.inflight--
			}
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) reconcileLoop(ctx context.Context, every time.Duration) {
	sem := make(chan struct{}, s.maxParallel)
	for tick := time.NewTicker(every); ; <-tick.C {
		actors, err := s.listActors(ctx)
		if err != nil {
			slog.Warn("ListActors", "err", err)
			continue
		}
		counts := map[ateapipb.ActorState]int{}
		now := time.Now()
		wedged := 0
		for _, a := range actors {
			st := a.GetStatus().GetState()
			counts[st]++
			// A suspend whose final commit lost a race (e.g. a resume arriving
			// mid-suspend) leaves the actor in SUSPENDING forever, pinning its
			// worker. Surface these — they eat the pool — and, past
			// --unwedge-after, medic them: delete any-state + recreate from
			// the template (state is lost, the worker is freed).
			if st == ateapipb.ActorState_ACTOR_STATE_SUSPENDING {
				age := now.Sub(a.GetMetadata().GetUpdateTime().AsTime())
				if age > 2*time.Minute {
					wedged++
					name := a.GetMetadata().GetName()
					slog.Warn("actor wedged in SUSPENDING (worker pinned)",
						"actor", name, "age", age.Round(time.Second).String())
					s.mu.Lock()
					busy := s.inFlight[name]
					if !busy && s.unwedgeAfter > 0 && age > s.unwedgeAfter {
						s.inFlight[name] = true
						go s.medic(ctx, name, a.GetActorTemplate())
					}
					s.mu.Unlock()
				}
			}
			if st != ateapipb.ActorState_ACTOR_STATE_RUNNING {
				continue
			}
			name := a.GetMetadata().GetName()
			// Baseline for actors with no signals yet: the record's own
			// update_time, which moved when the resume committed RUNNING.
			// (Router-served traffic does not bump update_time — that is
			// exactly why the touch API exists.)
			base := a.GetMetadata().GetUpdateTime().AsTime()
			s.mu.Lock()
			act := s.acts[name]
			last, inflight := base, 0
			if act != nil {
				if act.lastTouch.After(last) {
					last = act.lastTouch
				}
				inflight = act.inflight
			}
			busy := s.inFlight[name]
			s.mu.Unlock()
			if busy {
				continue
			}
			idle := now.Sub(last)
			overCap := s.maxRunning > 0 && now.Sub(base) > s.maxRunning
			if (idle > s.idleTimeout && inflight == 0) || overCap {
				s.mu.Lock()
				s.inFlight[name] = true
				s.mu.Unlock()
				sem <- struct{}{}
				go func(name string, forced bool) {
					defer func() {
						<-sem
						s.mu.Lock()
						delete(s.inFlight, name)
						s.mu.Unlock()
					}()
					s.suspend(ctx, name, forced)
				}(name, overCap && !(idle > s.idleTimeout && inflight == 0))
			}
		}
		counts[ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED] = len(actors) // total under key 0
		s.stateCounts.Store(counts)
		s.wedged.Store(int64(wedged))
	}
}

// handlePurge deletes every actor in the managed atespace (any state) —
// the fast path for experiment/clean.sh: one in-cluster gRPC connection and
// 8-way parallel deletes instead of one port-forward per CLI invocation.
// Requires ?atespace=<ours> as a confirmation guard.
func (s *server) handlePurge(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("atespace") != s.atespace {
		http.Error(w, "pass ?atespace="+s.atespace+" to confirm", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	var deleted atomic.Int64
	for {
		actors, err := s.listActors(ctx)
		if err != nil {
			http.Error(w, "ListActors: "+err.Error(), http.StatusBadGateway)
			return
		}
		if len(actors) == 0 {
			break
		}
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for _, a := range actors {
			name := a.GetMetadata().GetName()
			// DELETING actors are already on their way out; don't re-delete.
			if a.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_DELETING {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(name string) {
				defer func() { <-sem; wg.Done() }()
				if _, err := s.api.DeleteActor(ctx, &ateapipb.DeleteActorRequest{
					Actor:    &ateapipb.ObjectRef{Atespace: s.atespace, Name: name},
					AnyState: true,
				}); err == nil {
					deleted.Add(1)
				}
			}(name)
		}
		wg.Wait()
		// Wait for DELETING workflows to drain before re-listing.
		select {
		case <-ctx.Done():
			remaining, _ := s.listActors(context.Background())
			w.WriteHeader(http.StatusGatewayTimeout)
			fmt.Fprintf(w, "{\"deleted\":%d,\"remaining\":%d}\n", deleted.Load(), len(remaining))
			return
		case <-time.After(3 * time.Second):
		}
	}
	fmt.Fprintf(w, "{\"deleted\":%d,\"remaining\":0}\n", deleted.Load())
}

// medic frees a worker pinned by a wedged suspend: force-delete the actor
// and recreate it (empty, from its template's golden snapshot). The agent's
// in-memory state is lost — acceptable for a benchmark harness, counted in
// autosuspend_unwedged_total so results stay honest.
func (s *server) medic(ctx context.Context, name string, tmpl *ateapipb.ObjectRef) {
	defer func() {
		s.mu.Lock()
		delete(s.inFlight, name)
		s.mu.Unlock()
	}()
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := s.api.DeleteActor(cctx, &ateapipb.DeleteActorRequest{
		Actor: &ateapipb.ObjectRef{Atespace: s.atespace, Name: name}, AnyState: true,
	}); err != nil {
		slog.Warn("medic delete failed", "actor", name, "err", err)
		return
	}
	// Deletion is a workflow; give it a moment before recreating the name.
	time.Sleep(10 * time.Second)
	for attempt := 0; attempt < 6; attempt++ {
		_, err := s.api.CreateActor(cctx, &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
			Metadata:      &ateapipb.ResourceMetadata{Atespace: s.atespace, Name: name},
			ActorTemplate: tmpl,
		}})
		if err == nil || status.Code(err) == codes.AlreadyExists {
			s.unwedged.Add(1)
			slog.Info("medic recreated wedged actor", "actor", name)
			return
		}
		select {
		case <-cctx.Done():
			slog.Warn("medic recreate timed out", "actor", name)
			return
		case <-time.After(5 * time.Second):
		}
	}
	slog.Warn("medic recreate gave up", "actor", name)
}

func (s *server) suspend(ctx context.Context, name string, forced bool) {
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err := s.api.SuspendActor(cctx, &ateapipb.SuspendActorRequest{
		Actor: &ateapipb.ObjectRef{Atespace: s.atespace, Name: name},
	})
	elapsed := time.Since(start)
	if err != nil {
		// Racing the actor's own lifecycle is normal: it may already be
		// SUSPENDING (FailedPrecondition/Aborted) or deleted (NotFound).
		if c := status.Code(err); c == codes.FailedPrecondition || c == codes.NotFound || c == codes.Aborted {
			return
		}
		s.suspendErrs.Add(1)
		slog.Warn("SuspendActor", "actor", name, "err", err)
		return
	}
	s.suspends.Add(1)
	s.suspendNsSum.Add(elapsed.Nanoseconds())
	slog.Info("suspended", "actor", name, "elapsed_ms", elapsed.Milliseconds(), "forced", forced)
}

func (s *server) listActors(ctx context.Context) ([]*ateapipb.Actor, error) {
	var out []*ateapipb.Actor
	tok := ""
	for {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		resp, err := s.api.ListActors(cctx, &ateapipb.ListActorsRequest{
			Atespace: s.atespace, PageSize: 1000, PageToken: tok,
		})
		cancel()
		if err != nil {
			return nil, err
		}
		out = append(out, resp.GetActors()...)
		tok = resp.GetNextPageToken()
		if tok == "" {
			return out, nil
		}
	}
}

func (s *server) sampleLoop(ctx context.Context, every time.Duration) {
	for tick := time.NewTicker(every); ; <-tick.C {
		total, active, assigned := 0, 0, 0
		tok := ""
		ok := true
		for {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			resp, err := s.api.ListWorkers(cctx, &ateapipb.ListWorkersRequest{PageSize: 1000, PageToken: tok})
			cancel()
			if err != nil {
				slog.Warn("ListWorkers", "err", err)
				ok = false
				break
			}
			for _, w := range resp.GetWorkers() {
				total++
				if w.GetStatus().GetState() == ateapipb.WorkerState_WORKER_STATE_ACTIVE {
					active++
				}
				if w.GetStatus().GetAllocated().GetActors() > 0 {
					assigned++
				}
			}
			tok = resp.GetNextPageToken()
			if tok == "" {
				break
			}
		}
		if !ok {
			continue
		}
		c, _ := s.stateCounts.Load().(map[ateapipb.ActorState]int)
		smp := sample{
			T:            time.Now().UnixMilli(),
			WorkersTotal: total,
			WorkersActiv: active,
			Assigned:     assigned,
			ActorsTotal:  c[ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED],
			Running:      c[ateapipb.ActorState_ACTOR_STATE_RUNNING],
			Resuming:     c[ateapipb.ActorState_ACTOR_STATE_RESUMING],
			Suspending:   c[ateapipb.ActorState_ACTOR_STATE_SUSPENDING],
			Suspended:    c[ateapipb.ActorState_ACTOR_STATE_SUSPENDED],
			Paused:       c[ateapipb.ActorState_ACTOR_STATE_PAUSED],
			Crashed:      c[ateapipb.ActorState_ACTOR_STATE_CRASHED],
		}
		s.samplesMu.Lock()
		s.samples = append(s.samples, smp)
		if len(s.samples) > maxSamples {
			s.samples = s.samples[len(s.samples)-maxSamples:]
		}
		s.samplesMu.Unlock()
	}
}

func (s *server) snapshotSamples() []sample {
	s.samplesMu.Lock()
	defer s.samplesMu.Unlock()
	out := make([]sample, len(s.samples))
	copy(out, s.samples)
	return out
}

func (s *server) handleOccupancy(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv")
	fmt.Fprintln(w, "unix_ms,workers_total,workers_active,workers_assigned,actors_total,running,resuming,suspending,suspended,paused,crashed")
	for _, r := range s.snapshotSamples() {
		fmt.Fprintf(w, "%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d\n",
			r.T, r.WorkersTotal, r.WorkersActiv, r.Assigned, r.ActorsTotal,
			r.Running, r.Resuming, r.Suspending, r.Suspended, r.Paused, r.Crashed)
	}
}

// handleState feeds the live dashboard: config, suspend counters, and the
// most recent samples (capped so the payload stays small at long uptimes).
func (s *server) handleState(w http.ResponseWriter, _ *http.Request) {
	samples := s.snapshotSamples()
	const maxOut = 900 // 75 min at 5s — plenty for a live view
	if len(samples) > maxOut {
		samples = samples[len(samples)-maxOut:]
	}
	avgMs := 0.0
	if n := s.suspends.Load(); n > 0 {
		avgMs = float64(s.suspendNsSum.Load()) / float64(n) / 1e6
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"atespace":       s.atespace,
		"idle_timeout":   s.idleTimeout.String(),
		"suspends":       s.suspends.Load(),
		"suspend_errors": s.suspendErrs.Load(),
		"suspend_avg_ms": avgMs,
		"wedged":         s.wedged.Load(),
		"unwedged":       s.unwedged.Load(),
		"samples":        samples,
	})
}

func (s *server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	c, _ := s.stateCounts.Load().(map[ateapipb.ActorState]int)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "autosuspend_suspends_total %d\n", s.suspends.Load())
	fmt.Fprintf(w, "autosuspend_suspend_errors_total %d\n", s.suspendErrs.Load())
	fmt.Fprintf(w, "autosuspend_suspend_seconds_sum %f\n", float64(s.suspendNsSum.Load())/1e9)
	fmt.Fprintf(w, "autosuspend_suspend_seconds_count %d\n", s.suspends.Load())
	fmt.Fprintf(w, "autosuspend_wedged_suspending %d\n", s.wedged.Load())
	fmt.Fprintf(w, "autosuspend_unwedged_total %d\n", s.unwedged.Load())
	for st, n := range c {
		if st == ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED {
			fmt.Fprintf(w, "autosuspend_actors_total %d\n", n)
			continue
		}
		fmt.Fprintf(w, "autosuspend_actors{state=%q} %d\n",
			strings.TrimPrefix(st.String(), "ACTOR_STATE_"), n)
	}
}
