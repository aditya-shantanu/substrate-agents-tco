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
// state counts) on an interval; the samples are served at /occupancy.csv and
// are the raw material for the density/oversubscription report.
package main

import (
	"context"
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

type activity struct {
	lastTouch time.Time
	inflight  int
}

type server struct {
	api      ateapipb.ControlClient
	atespace string

	idleTimeout time.Duration
	maxRunning  time.Duration
	maxParallel int

	mu       sync.Mutex
	acts     map[string]*activity // actor name -> activity
	inFlight map[string]bool      // suspends currently running

	suspends     atomic.Int64
	suspendErrs  atomic.Int64
	suspendNsSum atomic.Int64

	csvMu   sync.Mutex
	csvRows []string

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
		api:         api,
		atespace:    *atespace,
		idleTimeout: *idleTimeout,
		maxRunning:  *maxRunning,
		maxParallel: *maxParallel,
		acts:        map[string]*activity{},
		inFlight:    map[string]bool{},
	}
	s.stateCounts.Store(map[ateapipb.ActorState]int{})
	s.csvRows = []string{"unix_ms,workers_total,workers_active,workers_assigned,actors_total,running,resuming,suspending,suspended,paused,crashed"}

	ctx := context.Background()
	go s.reconcileLoop(ctx, *poll)
	go s.sampleLoop(ctx, *sampleEvery)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /touch", s.handleActivity("touch"))
	mux.HandleFunc("POST /begin", s.handleActivity("begin"))
	mux.HandleFunc("POST /end", s.handleActivity("end"))
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /occupancy.csv", s.handleOccupancy)
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
		for _, a := range actors {
			st := a.GetStatus().GetState()
			counts[st]++
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
	}
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
		row := fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d",
			time.Now().UnixMilli(), total, active, assigned,
			c[ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED],
			c[ateapipb.ActorState_ACTOR_STATE_RUNNING],
			c[ateapipb.ActorState_ACTOR_STATE_RESUMING],
			c[ateapipb.ActorState_ACTOR_STATE_SUSPENDING],
			c[ateapipb.ActorState_ACTOR_STATE_SUSPENDED],
			c[ateapipb.ActorState_ACTOR_STATE_PAUSED],
			c[ateapipb.ActorState_ACTOR_STATE_CRASHED])
		s.csvMu.Lock()
		s.csvRows = append(s.csvRows, row)
		s.csvMu.Unlock()
	}
}

func (s *server) handleOccupancy(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv")
	s.csvMu.Lock()
	body := strings.Join(s.csvRows, "\n")
	s.csvMu.Unlock()
	fmt.Fprintln(w, body)
}

func (s *server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	c, _ := s.stateCounts.Load().(map[ateapipb.ActorState]int)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "autosuspend_suspends_total %d\n", s.suspends.Load())
	fmt.Fprintf(w, "autosuspend_suspend_errors_total %d\n", s.suspendErrs.Load())
	fmt.Fprintf(w, "autosuspend_suspend_seconds_sum %f\n", float64(s.suspendNsSum.Load())/1e9)
	fmt.Fprintf(w, "autosuspend_suspend_seconds_count %d\n", s.suspends.Load())
	for st, n := range c {
		if st == ateapipb.ActorState_ACTOR_STATE_UNSPECIFIED {
			fmt.Fprintf(w, "autosuspend_actors_total %d\n", n)
			continue
		}
		fmt.Fprintf(w, "autosuspend_actors{state=%q} %d\n",
			strings.TrimPrefix(st.String(), "ACTOR_STATE_"), n)
	}
}
