// agentsim emulates a fleet of "personal agents" (OpenClaw / Hermes shaped:
// a few interactive sessions plus many short scheduled wakes per day, idle
// otherwise) against Glutton actors on a Substrate cluster, to measure how
// much density/oversubscription auto-suspend actually buys.
//
// Per simulated agent it creates one Glutton actor, then generates a Poisson
// stream of activations. Every activation is plain HTTP through the atenet
// router (Host: <actor>.<atespace>.<domain>), so a suspended actor is resumed
// *implicitly* by Substrate — the first request's latency is the activation
// cost the end user would feel. The autosuspender (separate service) puts
// actors back to sleep; agentsim reports each request to it as an activity
// signal, playing the role a real gateway/ingress would.
//
// Time compression: --compress k divides every workload interval by k so a
// day of personal-agent behavior plays out in 86400/k seconds of wall clock.
// Suspend/resume times do NOT compress, so run the model with the compressed
// workload when comparing predictions (see MODEL.md).
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adityashantanu/substrate-agents-tco/service/internal/agentscript"
	"github.com/adityashantanu/substrate-agents-tco/service/internal/ateclient"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cfg struct {
	routerURL, actorDomain, suspenderURL string
	atespace, tmplAtespace, tmpl         string
	agents                               int
	compress                             float64
	sessionsPerDay, wakesPerDay          float64
	sessionMin, sessionPingSec           float64
	memTarget, memChurn, memRead         string
	diskBytes                            int
	duration                             time.Duration
	rampSec                              float64

	// load-test mode: activate agents in waves until failure thresholds trip
	loadTest                   bool
	waveStart, waveStep        int
	waveInterval               time.Duration
	failRefusalPct, failErrPct float64

	// starvation SLO gates (load-test waves) and the fixed-work CPU probe
	failWakeP99Ms, failTurnP99Ms float64
	// resident-fill gates: latency relative to the first wave, absolute probe
	// p99, node MemAvailable / PSI (autosuspender /state.json), crashed
	// actors; holdAfterFail keeps the last clean level running and scores it.
	failRelP90, failProbeP99Ms, failMemAvailPct, failPsiMemFull, failPsiCPUSome float64
	failCrashed                                                                 int
	holdAfterFail                                                               time.Duration
	// swap mode (see swap.go): turnover at a fixed resident fill
	swapFill, swapStart int
	swapMult            float64
	swapEvery           time.Duration
	probeBytes          int
	setupSuspend        bool

	// script mode (see script.go): sessions play an agent-session script
	script        string  // built-in name or YAML path; empty = personal-agent turns
	thinkScale    float64 // multiplier on every think gap
	scriptSuspend string  // driver | idle
	scriptLoop    bool    // play the script back-to-back instead of as Poisson tasks (a one-day script)

	// one-ping mode (see ping.go): the benchmarking suite's GluttonUser loop
	pingActorsPerUser  int // 0 = disabled
	pingWait, pingLive time.Duration
	// pingIndependent: every agent wakes on its own Poisson schedule with
	// mean gap pingWait (no user serializing N actors), so concurrency is
	// random and the pool can saturate — the mode for finding a host's
	// real ceiling with load-test waves.
	pingIndependent  bool
	setupConcurrency int

	// lifecycle: how the driver parks an actor between activations —
	// suspend (durable checkpoint to the bucket) or pause (node-local
	// checkpoint; the actor must resume on the same node)
	lifecycle string

	// coldStartOnly: time every actor's first life (CreateActor → first
	// ResumeActor from the golden snapshot → first answered ping), print the
	// percentiles, and exit without running a workload.
	coldStartOnly bool
}

// tsample is a timestamped latency sample (for windowed wave scoring).
type tsample struct {
	t  int64
	ms float64
}

type result struct {
	unixMs     int64
	agent      int
	kind       string
	firstReqMs float64
	pings      int
	errors     int
	refusals   int     // 503/504 from the router: pool full or resume outran the park budget
	readRAMMs  float64 // post-resume working-set walk latency (demand-paging cost)

	// script mode, kind == "step"
	step         string  // step name
	stepMs       float64 // wall time of the step's ops (think gap and wake excluded)
	suspendMs    float64 // driver-mode SuspendActor call duration (0 = none/failed)
	wasSuspended int     // actor state before the wake: 1 suspended, 0 running, -1 unknown
}

type sim struct {
	cfg  cfg
	api  ateapipb.ControlClient
	http *http.Client

	mu      sync.Mutex
	results []result
	turns   []tsample // every in-session ping, timed (service under contention)
	probes  []tsample // fixed-work CPU probe per activation (throttle detector)
	ready   []int     // agent ids that completed setup; only these run/are activated

	script    *agentscript.Script // non-nil in script mode
	ingestBuf []byte              // random payload for ingest ops (largest the script needs)
	colds     []coldStart         // every actor's first life, timed (cold.go)
}

func main() {
	var c cfg
	var endpoint, serverName, caFile, credBundle, checkScript string
	flag.StringVar(&checkScript, "check-script", "", "validate this agent-session script (built-in name or YAML path), print its shape, and exit; nothing else runs")
	flag.StringVar(&endpoint, "api-endpoint", ateclient.DefaultEndpoint, "ateapi gRPC dial target")
	flag.StringVar(&serverName, "server-name", ateclient.DefaultServerName, "TLS server name of ateapi")
	flag.StringVar(&caFile, "ca-file", ateclient.DefaultCAFile, "CA bundle verifying ateapi")
	flag.StringVar(&credBundle, "cred-bundle", ateclient.DefaultCredBundle, "pod certificate credential bundle")
	flag.StringVar(&c.routerURL, "router-url", "http://atenet-router.ate-system.svc.cluster.local", "atenet router base URL")
	flag.StringVar(&c.actorDomain, "actor-domain", "actors.resources.substrate.ate.dev", "actor host suffix")
	flag.StringVar(&c.suspenderURL, "suspender-url", "http://autosuspender.agent-sim.svc.cluster.local:8080", "autosuspender activity API; empty disables signals")
	flag.StringVar(&c.atespace, "atespace", "agents-sim", "atespace for the simulated agents")
	flag.StringVar(&c.tmplAtespace, "template-atespace", "benchmark-workloads", "atespace of the actor template")
	flag.StringVar(&c.tmpl, "template", "glutton", "actor template name (deploy glutton via substrate/benchmarking/workloads)")
	flag.IntVar(&c.agents, "agents", 120, "number of simulated agents")
	flag.Float64Var(&c.compress, "compress", 60, "time compression factor (60 = a day plays in 24 min)")
	flag.Float64Var(&c.sessionsPerDay, "sessions-per-day", 3, "interactive sessions per agent-day")
	flag.Float64Var(&c.sessionMin, "session-minutes", 8, "interactive session length, uncompressed minutes")
	flag.Float64Var(&c.sessionPingSec, "session-ping-seconds", 30, "turn cadence within a session, uncompressed seconds")
	flag.Float64Var(&c.wakesPerDay, "wakes-per-day", 40, "scheduled wakes (heartbeats/crons) per agent-day")
	flag.StringVar(&c.memTarget, "mem-target", "128Mi", "RAM working set per actor (glutton WriteRAM at boot); empty skips")
	flag.StringVar(&c.memRead, "mem-read", "all", "walk this much of the working set right after each activation's first request (demand-paging cost of the restore); 'all', a size like '64Mi', or empty to skip")
	flag.StringVar(&c.memChurn, "mem-churn", "16Mi", "dirty this much RAM (rotate) on every turn, so snapshots change like a live app's; empty skips")
	flag.IntVar(&c.diskBytes, "disk-bytes", 65536, "bytes written to the actor's filesystem on every turn (glutton WriteDisk) and digest-read back once per activation; 0 skips")
	flag.DurationVar(&c.duration, "duration", 20*time.Minute, "wall-clock run length after setup (load-test: upper bound)")
	flag.Float64Var(&c.rampSec, "ramp-seconds", 60, "spread agent start offsets over this many wall-clock seconds")
	flag.BoolVar(&c.loadTest, "load-test", false, "wave mode: activate agents in steps until failure thresholds trip; reports the last sustainable level")
	flag.IntVar(&c.waveStart, "wave-start", 10, "load-test: agents active in the first wave")
	flag.IntVar(&c.waveStep, "wave-step", 10, "load-test: agents added per wave")
	flag.DurationVar(&c.waveInterval, "wave-interval", 3*time.Minute, "load-test: observation window per wave")
	flag.Float64Var(&c.failWakeP99Ms, "fail-wake-p99-ms", 10000, "load-test: a wave fails if wake p99 exceeds this (0 disables) — soft-starvation gate")
	flag.Float64Var(&c.failRelP90, "fail-rel-p90", 0, "load-test: a wave fails if its CPU-probe or turn P90 exceeds this multiple of the first wave's (0 disables)")
	flag.Float64Var(&c.failProbeP99Ms, "fail-probe-p99-ms", 0, "load-test: a wave fails if the fixed-work CPU probe p99 exceeds this many ms (0 disables)")
	flag.Float64Var(&c.failMemAvailPct, "fail-mem-avail-pct", 0, "load-test: a wave fails if the node's MemAvailable is below this % of MemTotal (from the autosuspender's /state.json; 0 disables)")
	flag.Float64Var(&c.failPsiMemFull, "fail-psi-mem-full", 0, "load-test: a wave fails if node memory PSI full avg10 exceeds this % (0 disables)")
	flag.Float64Var(&c.failPsiCPUSome, "fail-psi-cpu-some", 0, "load-test: a wave fails if node CPU PSI some avg10 exceeds this % (0 disables)")
	flag.IntVar(&c.failCrashed, "fail-crashed", 0, "load-test: a wave fails if at least this many actors are CRASHED (0 disables)")
	flag.DurationVar(&c.holdAfterFail, "hold-after-fail", 0, "load-test: after a failed wave, stop the agents that wave added and keep the last clean level running for this long, scoring it (0 = end at once)")
	flag.IntVar(&c.swapFill, "swap-fill", 0, "swap mode: hold this many actors resident and idle, then park N and wake N every --swap-every (0 = off)")
	flag.IntVar(&c.swapStart, "swap-start", 5, "swap mode: N per tick at the first level")
	flag.Float64Var(&c.swapMult, "swap-mult", 2, "swap mode: multiply N by this per level (one level per --wave-interval)")
	flag.DurationVar(&c.swapEvery, "swap-every", 10*time.Second, "swap mode: tick length — N parks + N wakes are started every tick")
	flag.Float64Var(&c.failTurnP99Ms, "fail-turn-p99-ms", 2000, "load-test: a wave fails if in-session turn p99 exceeds this (0 disables)")
	flag.IntVar(&c.probeBytes, "probe-bytes", 8<<20, "fixed-work CPU probe: sha256 over this many bytes once per activation; drift = CPU throttling (0 disables)")
	flag.BoolVar(&c.setupSuspend, "setup-suspend", true, "suspend each agent right after setup (snapshot design); false keeps agents resident on their workers (parking design: workers >= agents, long idle timeout)")
	flag.StringVar(&c.script, "script", "", "agent-session script: a built-in name ("+strings.Join(agentscript.Names(), ", ")+") or a path to a YAML file; sessions then play the script (one task each) instead of personal-agent turns; empty = personal-agent workload")
	flag.Float64Var(&c.thinkScale, "think-scale", 1, "script mode: multiplier on every think gap (LLM latency); gaps get ±20% jitter and are NOT time-compressed")
	flag.BoolVar(&c.scriptLoop, "script-loop", false, "script mode: each agent plays the script back-to-back for the whole run (for scripts that are one day of life, e.g. personal-assistant with --think-scale 0.02) instead of one task per Poisson session")
	flag.StringVar(&c.scriptSuspend, "script-suspend", "driver", "script mode: who suspends between steps — driver (SuspendActor right after each step, like the upstream benchmark) or idle (the autosuspender's idle timeout decides, like a production gateway)")
	flag.IntVar(&c.pingActorsPerUser, "ping-actors-per-user", 0, "one-ping workload (substrate benchmarking's GluttonUser loop, the Prow 200K run): group agents into virtual users of this many actors; each user serially wakes an actor with a ping, holds it for --ping-live, suspends it, sleeps --ping-wait, then moves to its next actor; no memory fill, no other work. 0 = disabled")
	flag.DurationVar(&c.pingWait, "ping-wait", 10*time.Second, "one-ping: gap between an actor's suspend and the user's next wake (wall clock, not compressed)")
	flag.DurationVar(&c.pingLive, "ping-live", 0, "one-ping: how long the actor stays awake after its first ping (0 = suspend right after the ping)")
	flag.BoolVar(&c.pingIndependent, "ping-independent", false, "one-ping without the user loop: every agent wakes on its own Poisson schedule with mean gap --ping-wait, pings, is parked by the driver; concurrency is random, so with --load-test the waves find the pool's real ceiling")
	flag.IntVar(&c.setupConcurrency, "setup-concurrency", 8, "actors booted and parked concurrently during setup")
	flag.StringVar(&c.lifecycle, "lifecycle-mode", "suspend", "how the driver parks an actor it has finished with (script driver mode, one-ping, and the first park after setup): suspend = SuspendActor, durable checkpoint in the bucket; pause = PauseActor, node-local checkpoint, resumes on the same node (upstream's --lifecycle-mode)")
	flag.StringVar(&actorPrefix, "actor-prefix", "sim", "actor name prefix (<prefix>-NNNN); use a unique one per run so leftovers of a previous fleet cannot be mistaken for this run's actors")
	flag.BoolVar(&c.coldStartOnly, "cold-start-only", false, "measure cold starts only: create every agent, time CreateActor, the first ResumeActor (golden-snapshot restore) and the first answered ping, print P50/P90/P99 and the per-actor CSV, then exit (no workload)")
	flag.Float64Var(&c.failRefusalPct, "fail-refusal-pct", 5, "load-test: stop when router refusals exceed this % of activations in a wave")
	flag.Float64Var(&c.failErrPct, "fail-error-pct", 2, "load-test: stop when request errors exceed this % of activations in a wave")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if checkScript != "" {
		os.Exit(checkScriptFile(checkScript))
	}

	conn, api, err := ateclient.Dial(endpoint, caFile, credBundle, serverName)
	if err != nil {
		slog.Error("dial ateapi", "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Keep-alives are off on purpose: an idle TCP connection held open into
	// the sandbox makes the next gVisor checkpoint fail ("runsc checkpoint:
	// exit status 128"), wedging the actor in SUSPENDING and pinning its
	// worker. Closing connections per request costs a handshake but keeps
	// suspends clean.
	s := &sim{cfg: c, api: api, http: &http.Client{
		Timeout:   60 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}}
	ctx := context.Background()

	if err := s.loadScript(ctx); err != nil {
		slog.Error("script", "err", err)
		os.Exit(1)
	}
	s.printProfile()

	if err := s.setup(ctx); err != nil {
		slog.Error("setup", "err", err)
		os.Exit(1)
	}

	s.printColdStarts()
	if c.coldStartOnly {
		slog.Info("cold-start-only: done", "agents", len(s.ready))
		return
	}

	slog.Info("starting load", "agents", c.agents, "duration", c.duration.String(),
		"compress", c.compress, "duty_cycle_uncompressed",
		fmt.Sprintf("%.2f%%", 100*(c.sessionsPerDay*c.sessionMin*60+c.wakesPerDay*15)/86400))

	deadline := time.Now().Add(c.duration)
	progressCtx, stopProgress := context.WithCancel(ctx)
	go s.progressLoop(progressCtx)
	if c.swapFill > 0 {
		s.runSwap(ctx, deadline)
	} else if c.pingActorsPerUser > 0 {
		s.runPing(ctx, deadline)
	} else if c.loadTest {
		s.runLoadTest(ctx, deadline)
	} else {
		var wg sync.WaitGroup
		for _, id := range s.ready {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				s.agentLoop(ctx, id, deadline)
			}(id)
		}
		wg.Wait()
	}
	stopProgress()

	s.report()
}

// actorPrefix names the fleet's actors (<prefix>-NNNN). A unique prefix per
// run guarantees CreateActor never hits AlreadyExists on leftovers of a
// previous fleet still being deleted — which would silently skip the boot
// (and the cold-start measurement) for those actors.
var actorPrefix = "sim"

func actorName(id int) string { return fmt.Sprintf("%s-%04d", actorPrefix, id) }

/* ---------------- setup: atespace, actors, first boot, RAM fill ---------------- */

func (s *sim) setup(ctx context.Context) error {
	_, err := s.api.CreateAtespace(ctx, &ateapipb.CreateAtespaceRequest{
		Atespace: &ateapipb.Atespace{Metadata: &ateapipb.ResourceMetadata{Name: s.cfg.atespace}},
	})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("CreateAtespace: %w", err)
	}
	type outcome struct {
		id  int
		err error
	}
	sem := make(chan struct{}, max(1, s.cfg.setupConcurrency))
	outCh := make(chan outcome, s.cfg.agents)
	var wg sync.WaitGroup
	for i := 0; i < s.cfg.agents; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer func() { <-sem; wg.Done() }()
			outCh <- outcome{id, s.setupOne(ctx, id)}
		}(i)
	}
	wg.Wait()
	close(outCh)
	failed := 0
	s.ready = s.ready[:0]
	for o := range outCh {
		if o.err != nil {
			failed++
			slog.Warn("agent setup failed", "err", o.err)
			continue
		}
		s.ready = append(s.ready, o.id)
	}
	sort.Ints(s.ready)
	if !s.setupSuspend() {
		// Parking design: placement can OOM-kill a sandbox after setup
		// succeeded, and the platform keeps reporting the actor RUNNING.
		// Waves must measure live residents only — ping each one and keep
		// the survivors; that count is the node's resident capacity.
		var live []int
		for _, id := range s.ready {
			pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			_, err := s.post(pctx, actorName(id), "/ping", nil)
			cancel()
			if err == nil {
				live = append(live, id)
			} else {
				failed++
				slog.Warn("agent dead after placement", "actor", actorName(id), "err", err)
			}
		}
		s.ready = live
	}
	// Snapshot design: setup failures are noise, so more than 10% is a
	// broken run. Parking design: how many agents the node can hold resident
	// IS the measurement — run with whoever fit.
	if s.setupSuspend() && failed > s.cfg.agents/10 {
		return fmt.Errorf("%d/%d agents failed setup", failed, s.cfg.agents)
	}
	if len(s.ready) == 0 {
		return fmt.Errorf("no agent completed setup (%d failed)", failed)
	}
	slog.Info("setup complete", "agents", len(s.ready), "failed", failed, "resident", !s.setupSuspend())
	return nil
}

func (s *sim) setupSuspend() bool { return s.cfg.setupSuspend }

func (s *sim) setupOne(ctx context.Context, id int) error {
	name := actorName(id)
	ref := &ateapipb.ObjectRef{Atespace: s.cfg.atespace, Name: name}
	t0 := time.Now()
	_, err := s.api.CreateActor(ctx, &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: s.cfg.atespace, Name: name},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: s.cfg.tmplAtespace, Name: s.cfg.tmpl},
	}})
	created := err == nil
	cs := coldStart{unixMs: t0.UnixMilli(), agent: id, createMs: msSince(t0)}
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("CreateActor %s: %w", name, err)
	}
	if created {
		// The pool is (deliberately) much smaller than the fleet, and a booted
		// actor holds its worker until suspended — so boots beyond the pool
		// size see ResourceExhausted until earlier ones are suspended. Retry
		// with backoff, and suspend explicitly after the fill instead of
		// waiting for the autosuspender, so setup pipelines through the pool.
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		for {
			// No boot flag: the glutton template has a golden snapshot, so a
			// plain ResumeActor cold-starts a fresh actor from it on old and
			// new control planes alike (Boot was removed from the API).
			cs.resumeAttempts++
			tr := time.Now()
			_, err := s.api.ResumeActor(cctx, &ateapipb.ResumeActorRequest{Actor: ref})
			if err == nil {
				cs.resumeMs = msSince(tr) // the successful restore call only
				break
			}
			switch status.Code(err) {
			case codes.ResourceExhausted, codes.Aborted, codes.Unavailable:
				select {
				case <-cctx.Done():
					return fmt.Errorf("boot %s: %w", name, err)
				case <-time.After(time.Duration(2000+rand.IntN(3000)) * time.Millisecond):
				}
			default:
				return fmt.Errorf("boot %s: %w", name, err)
			}
		}
		// First answered ping: the actor is serving (the spawn benchmark's
		// "ready"). Retried on transient router errors; the whole wait counts.
		tp := time.Now()
		for attempt := 0; ; attempt++ {
			_, perr := s.post(cctx, name, "/ping", nil)
			if perr == nil || attempt >= 30 {
				if perr != nil {
					cs.pingErr = perr.Error()
				}
				break
			}
			cs.pingAttempts++
			time.Sleep(time.Duration(200+rand.IntN(300)) * time.Millisecond)
		}
		cs.pingMs = msSince(tp)
		cs.readyMs = msSince(t0)
		s.mu.Lock()
		s.colds = append(s.colds, cs)
		s.mu.Unlock()

		// Setup is not the measurement: a router 502/503/504 while a big
		// working set is being written (upstream timeout, "another operation
		// is in progress") must not cost the run an agent. Retry with backoff.
		if s.cfg.memTarget != "" {
			if err := s.setupPost(cctx, name, "/writeram",
				writeRAMBody("memload", s.cfg.memTarget, writeModeTruncate)); err != nil {
				return fmt.Errorf("fill RAM %s: %w", name, err)
			}
		}
		if s.cfg.probeBytes > 0 {
			if err := s.setupPost(cctx, name, "/writedisk",
				writeDiskBody("cpuprobe", s.cfg.probeBytes)); err != nil {
				return fmt.Errorf("write CPU probe %s: %w", name, err)
			}
		}
		if !s.cfg.setupSuspend {
			// Resident-parking mode: the agent stays on its worker; the kernel
			// (swap) decides what stays in RAM. Wake = page-in, not restore.
			return nil
		}
		for attempt := 0; ; attempt++ {
			err := s.hibernateRPC(cctx, ref)
			if err == nil || status.Code(err) == codes.FailedPrecondition {
				break
			}
			if code := status.Code(err); (code != codes.Aborted && code != codes.Unavailable) || attempt >= 8 {
				return fmt.Errorf("first suspend %s: %w", name, err)
			}
			select {
			case <-cctx.Done():
				return fmt.Errorf("first suspend %s: %w", name, err)
			case <-time.After(time.Duration(3000+rand.IntN(4000)) * time.Millisecond):
			}
		}
	}
	return nil
}

/* ---------------- the workload ---------------- */

func (s *sim) agentLoop(ctx context.Context, id int, deadline time.Time) {
	name := actorName(id)
	rng := rand.New(rand.NewPCG(uint64(id), 0xa9e1))
	time.Sleep(time.Duration(rng.Float64() * s.cfg.rampSec * float64(time.Second)))

	if s.script != nil && s.cfg.scriptLoop {
		// One lap after another until the deadline (runTask stops at it).
		for time.Now().Before(deadline) && ctx.Err() == nil {
			s.runTask(ctx, id, name, rng, deadline)
		}
		return
	}

	if s.cfg.pingIndependent {
		// Independent one-ping: exponential gaps with mean --ping-wait (wall
		// clock, not compressed), one wake+park per activation. Stop as soon
		// as the context is cancelled (load-test waves cancel the loops when a
		// wave fails) instead of spinning on failed requests until the deadline.
		for {
			gap := time.Duration(rng.ExpFloat64() * float64(s.cfg.pingWait))
			if time.Now().Add(gap).After(deadline) {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(gap):
			}
			s.pingCycle(ctx, id, rng)
		}
	}

	ratePerSec := (s.cfg.sessionsPerDay + s.cfg.wakesPerDay) / 86400.0 * s.cfg.compress
	pSession := s.cfg.sessionsPerDay / (s.cfg.sessionsPerDay + s.cfg.wakesPerDay)

	for {
		gap := time.Duration(rng.ExpFloat64() / ratePerSec * float64(time.Second))
		if time.Now().Add(gap).After(deadline) {
			return
		}
		select {
		case <-ctx.Done(): // a failed load-test wave cancels the loops
			return
		case <-time.After(gap):
		}
		if rng.Float64() < pSession {
			s.activation(ctx, id, name, "session", rng, deadline)
		} else {
			s.activation(ctx, id, name, "wake", rng, deadline)
		}
	}
}

// activation performs one wake or session: the first request implicitly
// resumes a suspended actor (its latency is the activation cost), sessions
// then keep pinging at the turn cadence for the session length.
func (s *sim) activation(ctx context.Context, id int, name, kind string, rng *rand.Rand, deadline time.Time) {
	if kind == "session" && s.script != nil {
		s.runTask(ctx, id, name, rng, deadline) // script mode: a session is one task
		return
	}
	r := result{unixMs: time.Now().UnixMilli(), agent: id, kind: kind}
	s.touch(name, "begin")
	// A saturated pool answers 503 (no free worker, request parked at most
	// --parked-request-budget) or 504 (restore outran the route timeout).
	// Retry with backoff like a real gateway would; count refusals so the
	// report can separate "slow resume" from "pool too small".
	start := time.Now()
	var err error
	for attempt := 0; ; attempt++ {
		_, err = s.post(ctx, name, "/ping", nil)
		if err == nil || attempt >= 5 || !isRefusal(err) {
			break
		}
		r.refusals++
		time.Sleep(time.Duration(150+rng.IntN(350)) * time.Millisecond)
	}
	r.firstReqMs = float64(time.Since(start).Microseconds()) / 1000
	r.pings = 1
	if err != nil {
		r.errors++
		slog.Warn("activation first request failed", "actor", name, "err", err)
	}
	s.touch(name, "touch")

	if err == nil {
		// Walk the working set right after the resume, before anything
		// dirties it: under a demand-paged restore every touched page must
		// come back in before this returns, so its latency is the real cost
		// of reaching the previous snapshot's memory.
		if s.cfg.memRead != "" {
			sz := s.cfg.memRead
			if sz == "all" {
				sz = "" // glutton walks the whole array on empty size
			}
			t := time.Now()
			if _, err := s.post(ctx, name, "/readram", readRAMBody("memload", sz)); err != nil {
				// Missing key = the actor was recreated (e.g. medic'd after a
				// wedged suspend) and lost its working set; restore it so the
				// workload stays realistic instead of erroring forever.
				if s.cfg.memTarget != "" {
					if _, ferr := s.post(ctx, name, "/writeram",
						writeRAMBody("memload", s.cfg.memTarget, writeModeTruncate)); ferr != nil {
						r.errors++
					}
				} else {
					r.errors++
				}
			} else {
				r.readRAMMs = float64(time.Since(t).Microseconds()) / 1000
			}
		}
		s.turnWork(ctx, name, &r) // first turn's memory churn + file write
		// Fixed-work CPU probe: sha256 over probe-bytes of pagecache-warm
		// file. Same work every time, so latency drift = CPU starvation.
		if s.cfg.probeBytes > 0 {
			t := time.Now()
			if _, err := s.post(ctx, name, "/readdisk", readDiskBody("cpuprobe")); err != nil {
				// Probe file lost (actor recreated by the medic): restore it.
				_, _ = s.post(ctx, name, "/writedisk", writeDiskBody("cpuprobe", s.cfg.probeBytes))
			} else {
				s.mu.Lock()
				s.probes = append(s.probes, tsample{time.Now().UnixMilli(), float64(time.Since(t).Microseconds()) / 1000})
				s.mu.Unlock()
			}
		}
	}

	if kind == "session" {
		durSec := s.cfg.sessionMin * 60 / s.cfg.compress
		pingEvery := s.cfg.sessionPingSec / s.cfg.compress
		for elapsed := 0.0; elapsed < durSec; elapsed += pingEvery {
			// Do not stretch wall time past the window: an in-flight session
			// stops pinging at the deadline (its stats so far still count).
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Duration(pingEvery * float64(time.Second)))
			tp := time.Now()
			if _, err := s.post(ctx, name, "/ping", nil); err != nil {
				r.errors++
			} else {
				s.mu.Lock()
				s.turns = append(s.turns, tsample{time.Now().UnixMilli(), float64(time.Since(tp).Microseconds()) / 1000})
				s.mu.Unlock()
			}
			r.pings++
			s.turnWork(ctx, name, &r)
			s.touch(name, "touch")
		}
	}
	// Read back what the activation wrote (digest only — content check
	// without shipping the bytes), like an agent consulting its files.
	if s.cfg.diskBytes > 0 {
		if _, err := s.post(ctx, name, "/readdisk", readDiskBody("workfile")); err != nil {
			r.errors++
		}
	}
	s.touch(name, "end")

	s.mu.Lock()
	s.results = append(s.results, r)
	s.mu.Unlock()
}

/* ---------------- HTTP plumbing ---------------- */

// post sends a protobuf-over-HTTP request to the actor through the router.
// A nil body is a valid empty PingRequest.
func (s *sim) post(ctx context.Context, actor, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.routerURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// Send both addressing forms: release-0.1 atenet routes by Host and
	// ignores the header; main routes by the ate-target-actor header and
	// ignores Host (lesson from the always-on-agent integration).
	req.Host = actor + "." + s.cfg.atespace + "." + s.cfg.actorDomain
	req.Header.Set("ate-target-actor", s.cfg.atespace+"/"+actor)
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: HTTP %d: %s", path, resp.StatusCode, bytes.TrimSpace(b))
	}
	return b, nil
}

// setupPost is post with retries on transient 5xx, for setup-time writes only
// (in-window requests are measured and must not be retried).
func (s *sim) setupPost(ctx context.Context, actor, path string, body []byte) error {
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		_, err = s.post(ctx, actor, path, body)
		if err == nil {
			return nil
		}
		msg := err.Error()
		// 421 (misdirected) also shows up from envoy while a sandbox is
		// unresponsive under paging pressure; transient in this setting.
		if !strings.Contains(msg, "HTTP 502") && !strings.Contains(msg, "HTTP 503") &&
			!strings.Contains(msg, "HTTP 504") && !strings.Contains(msg, "HTTP 421") {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(3000+rand.IntN(5000)) * time.Millisecond):
		}
	}
	return err
}

func isRefusal(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "HTTP 503") || strings.Contains(msg, "HTTP 504")
}

func (s *sim) touch(actor, kind string) {
	if s.cfg.suspenderURL == "" {
		return
	}
	req, err := http.NewRequest(http.MethodPost,
		s.cfg.suspenderURL+"/"+kind+"?actor="+actor+"&atespace="+s.cfg.atespace, nil)
	if err != nil {
		return
	}
	if resp, err := s.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

// turnWork is the per-turn "real work": dirty a rotating window of RAM (so
// every suspend snapshots changed memory, like a live application) and write
// a file to the sandbox filesystem (so the rootfs delta is exercised too).
func (s *sim) turnWork(ctx context.Context, name string, r *result) {
	if s.cfg.memChurn != "" {
		if _, err := s.post(ctx, name, "/writeram",
			writeRAMBody("memload", s.cfg.memChurn, writeModeRotate)); err != nil {
			r.errors++
		}
	}
	if s.cfg.diskBytes > 0 {
		if _, err := s.post(ctx, name, "/writedisk",
			writeDiskBody("workfile", s.cfg.diskBytes)); err != nil {
			r.errors++
		}
	}
}

/* ---------------- hand-rolled glutton proto bodies ----------------
   Field numbers from substrate/internal/proto/glutton/glutton.proto (that
   package is internal, so we encode the four tiny messages ourselves).   */

const (
	writeModeTruncate = 0 // WRITE_MODE_TRUNCATE
	writeModeRotate   = 2 // WRITE_MODE_OVERWRITE_ROTATE
)

// WriteRAMRequest{key=1 string, size=2 string, write_mode=3 enum}
func writeRAMBody(key, size string, mode int) []byte {
	var b []byte
	b = appendString(b, 1, key)
	b = appendString(b, 2, size)
	b = appendVarint(b, 3, uint64(mode))
	return b
}

// ReadRAMRequest{key=1 string, size=2 string} — empty size walks everything.
func readRAMBody(key, size string) []byte {
	var b []byte
	b = appendString(b, 1, key)
	b = appendString(b, 2, size)
	return b
}

// WriteDiskRequest{key=1 string, size=2 int32, write_mode=3 enum}
func writeDiskBody(key string, size int) []byte {
	var b []byte
	b = appendString(b, 1, key)
	b = appendVarint(b, 2, uint64(size))
	b = appendVarint(b, 3, writeModeTruncate)
	return b
}

// ReadDiskRequest{key=1 string, read_mode=2 enum} — DIGEST_ONLY=1.
func readDiskBody(key string) []byte {
	var b []byte
	b = appendString(b, 1, key)
	b = appendVarint(b, 2, 1)
	return b
}

func appendString(b []byte, field int, s string) []byte {
	b = append(b, byte(field<<3|2))
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

func appendVarint(b []byte, field int, v uint64) []byte {
	if v == 0 {
		return b // proto3 zero value is omitted
	}
	b = append(b, byte(field<<3|0))
	return binary.AppendUvarint(b, v)
}

// hostState is the newest autosuspender sample: actor states plus the node's
// PSI and memory (host-global /proc values from the autosuspender's node).
type hostState struct {
	MemTotal     int64   `json:"mem_total_bytes"`
	MemAvailable int64   `json:"mem_available_bytes"`
	PsiCPUSome   float64 `json:"psi_cpu_some10"`
	PsiMemFull   float64 `json:"psi_mem_full10"`
	PsiIOSome    float64 `json:"psi_io_some10"`
	Running      int     `json:"running"`
	Crashed      int     `json:"crashed"`
}

func (h hostState) memAvailPct() float64 {
	if h.MemTotal <= 0 {
		return -1
	}
	return 100 * float64(h.MemAvailable) / float64(h.MemTotal)
}

// fetchHostState reads the autosuspender's /state.json "latest" sample.
func (s *sim) fetchHostState(ctx context.Context) (hostState, bool) {
	if s.cfg.suspenderURL == "" {
		return hostState{}, false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, s.cfg.suspenderURL+"/state.json", nil)
	if err != nil {
		return hostState{}, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return hostState{}, false
	}
	defer resp.Body.Close()
	var body struct {
		Latest *hostState `json:"latest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Latest == nil {
		return hostState{}, false
	}
	return *body.Latest, true
}

// runLoadTest activates agent loops in waves and scores each wave's window:
// refusal/error rates, wake/turn/probe latency (absolute SLOs and, with
// --fail-rel-p90, relative to the first wave), and the node's memory, PSI and
// crashed-actor counts from the autosuspender. The first wave that trips a
// gate ends the ramp; with --hold-after-fail the agents that wave added are
// stopped and the last clean level keeps running for the hold window and is
// scored again. The last level that passed every gate is the ceiling.
func (s *sim) runLoadTest(ctx context.Context, deadline time.Time) {
	type waveStat struct {
		level, acts, refusals, errors      int
		wakeP50, wakeP90, wakeP99          float64
		turnP90, turnP99                   float64
		probeP50, probeP90, probeP99       float64
		memAvailPct, psiCPU, psiMem, psiIO float64
		running, crashed                   int
		failedOn                           string
		tag                                string // "" | "hold"
	}
	loopCtx, cancelLoops := context.WithCancel(ctx)
	defer cancelLoops()
	var wg sync.WaitGroup
	cancels := map[int]context.CancelFunc{} // per activated agent (index into s.ready)
	active := 0
	lastGood := 0
	var waves []waveStat
	var baseProbeP90, baseTurnP90 float64

	activate := func(n int) {
		for ; active < n && active < len(s.ready); active++ {
			actx, acancel := context.WithCancel(loopCtx)
			cancels[active] = acancel
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				s.agentLoop(actx, id, deadline)
			}(s.ready[active])
		}
	}
	// score the window since sinceMs and return the stat (level = active)
	score := func(sinceMs int64) waveStat {
		s.mu.Lock()
		st := waveStat{level: active}
		var wakes, turns, probes []float64
		for _, r := range s.results {
			if r.unixMs < sinceMs {
				continue
			}
			st.acts++
			st.refusals += r.refusals
			st.errors += r.errors
			if isWake(r) {
				wakes = append(wakes, r.firstReqMs)
			}
		}
		for _, t := range s.turns {
			if t.t >= sinceMs {
				turns = append(turns, t.ms)
			}
		}
		for _, t := range s.probes {
			if t.t >= sinceMs {
				probes = append(probes, t.ms)
			}
		}
		s.mu.Unlock()
		sort.Float64s(wakes)
		sort.Float64s(turns)
		sort.Float64s(probes)
		qz := func(v []float64, p float64) float64 {
			if len(v) == 0 {
				return 0
			}
			return q(v, p)
		}
		st.wakeP50, st.wakeP90, st.wakeP99 = qz(wakes, 0.5), qz(wakes, 0.9), qz(wakes, 0.99)
		st.turnP90, st.turnP99 = qz(turns, 0.9), qz(turns, 0.99)
		st.probeP50, st.probeP90, st.probeP99 = qz(probes, 0.5), qz(probes, 0.9), qz(probes, 0.99)
		st.memAvailPct = -1
		if h, ok := s.fetchHostState(ctx); ok {
			st.memAvailPct, st.psiCPU, st.psiMem, st.psiIO = h.memAvailPct(), h.PsiCPUSome, h.PsiMemFull, h.PsiIOSome
			st.running, st.crashed = h.Running, h.Crashed
		}
		refPct, errPct := 0.0, 0.0
		if st.acts > 0 {
			refPct = 100 * float64(st.refusals) / float64(st.acts)
			errPct = 100 * float64(st.errors) / float64(st.acts)
		}
		var reasons []string
		if refPct > s.cfg.failRefusalPct {
			reasons = append(reasons, "refusals")
		}
		if errPct > s.cfg.failErrPct {
			reasons = append(reasons, "errors")
		}
		if s.cfg.failWakeP99Ms > 0 && len(wakes) > 0 && st.wakeP99 > s.cfg.failWakeP99Ms {
			reasons = append(reasons, "wake-p99")
		}
		if s.cfg.failTurnP99Ms > 0 && len(turns) > 0 && st.turnP99 > s.cfg.failTurnP99Ms {
			reasons = append(reasons, "turn-p99")
		}
		if s.cfg.failProbeP99Ms > 0 && len(probes) > 0 && st.probeP99 > s.cfg.failProbeP99Ms {
			reasons = append(reasons, "probe-p99")
		}
		if s.cfg.failRelP90 > 0 {
			if baseProbeP90 > 0 && len(probes) >= 20 && st.probeP90 > s.cfg.failRelP90*baseProbeP90 {
				reasons = append(reasons, "probe-p90-vs-baseline")
			}
			if baseTurnP90 > 0 && len(turns) >= 20 && st.turnP90 > s.cfg.failRelP90*baseTurnP90 {
				reasons = append(reasons, "turn-p90-vs-baseline")
			}
		}
		if s.cfg.failMemAvailPct > 0 && st.memAvailPct >= 0 && st.memAvailPct < s.cfg.failMemAvailPct {
			reasons = append(reasons, "mem-available")
		}
		if s.cfg.failPsiMemFull > 0 && st.psiMem > s.cfg.failPsiMemFull {
			reasons = append(reasons, "psi-mem-full")
		}
		if s.cfg.failPsiCPUSome > 0 && st.psiCPU > s.cfg.failPsiCPUSome {
			reasons = append(reasons, "psi-cpu-some")
		}
		if s.cfg.failCrashed > 0 && st.crashed >= s.cfg.failCrashed {
			reasons = append(reasons, "crashed")
		}
		st.failedOn = strings.Join(reasons, "+")
		slog.Info("wave result", "tag", st.tag, "active_agents", st.level, "activations", st.acts,
			"refusal_pct", fmt.Sprintf("%.1f", refPct), "error_pct", fmt.Sprintf("%.1f", errPct),
			"wake_p50_ms", int(st.wakeP50), "wake_p90_ms", int(st.wakeP90), "wake_p99_ms", int(st.wakeP99),
			"turn_p90_ms", int(st.turnP90), "turn_p99_ms", int(st.turnP99),
			"probe_p50_ms", int(st.probeP50), "probe_p90_ms", int(st.probeP90), "probe_p99_ms", int(st.probeP99),
			"mem_avail_pct", fmt.Sprintf("%.1f", st.memAvailPct), "psi_cpu_some10", st.psiCPU, "psi_mem_full10", st.psiMem, "psi_io_some10", st.psiIO,
			"running", st.running, "crashed", st.crashed,
			"failed", st.failedOn != "", "failed_on", st.failedOn)
		return st
	}
	printTable := func(verdict string) {
		fmt.Println("=== loadtest waves ===")
		fmt.Println("tag,active_agents,activations,refusals,errors,wake_p50_ms,wake_p90_ms,wake_p99_ms,turn_p90_ms,turn_p99_ms,probe_p50_ms,probe_p90_ms,probe_p99_ms,mem_avail_pct,psi_cpu_some10,psi_mem_full10,psi_io_some10,running,crashed,failed_on")
		for _, w := range waves {
			fmt.Printf("%s,%d,%d,%d,%d,%.0f,%.0f,%.0f,%.0f,%.0f,%.1f,%.1f,%.1f,%.1f,%.2f,%.2f,%.2f,%d,%d,%s\n",
				w.tag, w.level, w.acts, w.refusals, w.errors, w.wakeP50, w.wakeP90, w.wakeP99, w.turnP90, w.turnP99,
				w.probeP50, w.probeP90, w.probeP99, w.memAvailPct, w.psiCPU, w.psiMem, w.psiIO, w.running, w.crashed, w.failedOn)
		}
		fmt.Println("=== end loadtest ===")
		fmt.Println(verdict)
	}

	for level := s.cfg.waveStart; ; level += s.cfg.waveStep {
		if level > len(s.ready) {
			level = len(s.ready)
		}
		prevActive := active
		activate(level)
		slog.Info("wave", "active_agents", active, "observing_for", s.cfg.waveInterval.String())
		waveStartMs := time.Now().UnixMilli()
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.cfg.waveInterval):
		}
		st := score(waveStartMs)
		if len(waves) == 0 {
			baseProbeP90, baseTurnP90 = st.probeP90, st.turnP90
		}
		waves = append(waves, st)
		failed := st.failedOn != ""
		if failed || st.level >= len(s.ready) || time.Now().After(deadline) {
			verdict := fmt.Sprintf("LOADTEST VERDICT: no failure up to %d active agents (raise --agents to push further)", st.level)
			if failed {
				verdict = fmt.Sprintf("LOADTEST VERDICT: failure at %d active agents (%s); last sustainable level = %d", st.level, st.failedOn, lastGood)
				if s.cfg.holdAfterFail > 0 && lastGood > 0 {
					// Drop the agents this wave added, keep the last clean level running, score it.
					for i := prevActive; i < active; i++ {
						if c, ok := cancels[i]; ok {
							c()
						}
					}
					// Stopping a loop leaves its actor resident; park the dropped
					// agents so the hold really runs at the last clean level.
					var pw sync.WaitGroup
					sem := make(chan struct{}, 16)
					for i := prevActive; i < active; i++ {
						pw.Add(1)
						go func(id int) {
							defer pw.Done()
							sem <- struct{}{}
							defer func() { <-sem }()
							pctx, pcancel := context.WithTimeout(ctx, 3*time.Minute)
							defer pcancel()
							if err := s.hibernateRPC(pctx, &ateapipb.ObjectRef{Atespace: s.cfg.atespace, Name: actorName(id)}); err != nil && status.Code(err) != codes.FailedPrecondition {
								slog.Warn("park dropped agent", "actor", actorName(id), "err", err)
							}
						}(s.ready[i])
					}
					pw.Wait()
					active = prevActive
					slog.Info("hold", "active_agents", active, "for", s.cfg.holdAfterFail.String())
					holdStartMs := time.Now().UnixMilli()
					select {
					case <-ctx.Done():
						printTable(verdict)
						return
					case <-time.After(s.cfg.holdAfterFail):
					}
					hs := score(holdStartMs)
					hs.tag = "hold"
					waves = append(waves, hs)
					if hs.failedOn != "" {
						verdict += fmt.Sprintf("; hold at %d also failed (%s)", hs.level, hs.failedOn)
					} else {
						verdict += fmt.Sprintf("; hold at %d clean for %s", hs.level, s.cfg.holdAfterFail)
					}
				}
			}
			cancelLoops()
			printTable(verdict)
			break
		}
		lastGood = st.level
	}
	wg.Wait()
}

// progressLoop logs a machine-readable progress line every 20s so a live
// ticker (experiment/run.sh) can show latency and throughput mid-run.
func (s *sim) progressLoop(ctx context.Context) {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		var wake, sess []float64
		errs, refs := 0, 0
		for _, r := range s.results {
			if isWake(r) {
				wake = append(wake, r.firstReqMs)
			} else {
				sess = append(sess, r.firstReqMs)
			}
			errs += r.errors
			refs += r.refusals
		}
		s.mu.Unlock()
		sort.Float64s(wake)
		sort.Float64s(sess)
		var turns, probes []float64
		s.mu.Lock()
		for _, t := range s.turns {
			turns = append(turns, t.ms)
		}
		for _, t := range s.probes {
			probes = append(probes, t.ms)
		}
		s.mu.Unlock()
		sort.Float64s(turns)
		sort.Float64s(probes)
		qi := func(v []float64, p float64) int {
			if len(v) == 0 {
				return 0
			}
			return int(q(v, p))
		}
		slog.Info("progress",
			"activations", len(wake)+len(sess),
			"wake_p50_ms", qi(wake, 0.5),
			"wake_p90_ms", qi(wake, 0.9),
			"wake_p99_ms", qi(wake, 0.99),
			"session_p50_ms", qi(sess, 0.5),
			"turn_p99_ms", qi(turns, 0.99),
			"probe_p50_ms", qi(probes, 0.5),
			"errors", errs, "refusals", refs)
	}
}

/* ---------------- report ---------------- */

func (s *sim) report() {
	// The profile block again, next to the results: the kubelet rotates
	// long container logs and `kubectl logs` then returns only the newest
	// segment, which would lose the block printed at startup.
	s.printProfile()
	s.mu.Lock()
	rs := s.results
	s.mu.Unlock()

	byKind := map[string][]float64{}
	totalErr, totalPings, totalRefusals := 0, 0, 0
	for _, r := range rs {
		byKind[r.kind] = append(byKind[r.kind], r.firstReqMs)
		totalErr += r.errors
		totalPings += r.pings
		totalRefusals += r.refusals
	}
	fmt.Println("=== agentsim summary ===")
	fmt.Printf("agents=%d activations=%d pings=%d errors=%d refusals_503_504=%d compress=%.0f\n",
		s.cfg.agents, len(rs), totalPings, totalErr, totalRefusals, s.cfg.compress)
	for kind, v := range byKind {
		sort.Float64s(v)
		fmt.Printf("%-8s n=%-5d first-request ms p50=%.0f p90=%.0f p99=%.0f max=%.0f\n",
			kind, len(v), q(v, 0.5), q(v, 0.9), q(v, 0.99), q(v, 1))
	}
	var walks []float64
	for _, r := range rs {
		if r.readRAMMs > 0 {
			walks = append(walks, r.readRAMMs)
		}
	}
	if len(walks) > 0 {
		sort.Float64s(walks)
		fmt.Printf("post-resume RAM walk ms p50=%.0f p90=%.0f p99=%.0f (demand-paging cost)\n",
			q(walks, 0.5), q(walks, 0.9), q(walks, 0.99))
	}
	s.mu.Lock()
	var turnsAll, probesAll []float64
	for _, t := range s.turns {
		turnsAll = append(turnsAll, t.ms)
	}
	for _, t := range s.probes {
		probesAll = append(probesAll, t.ms)
	}
	s.mu.Unlock()
	if len(turnsAll) > 0 {
		sort.Float64s(turnsAll)
		fmt.Printf("in-session turn ms p50=%.0f p90=%.0f p99=%.0f (service under contention)\n",
			q(turnsAll, 0.5), q(turnsAll, 0.9), q(turnsAll, 0.99))
	}
	if len(probesAll) > 0 {
		sort.Float64s(probesAll)
		fmt.Printf("CPU probe ms p50=%.0f p90=%.0f p99=%.0f (fixed work; drift = throttling)\n",
			q(probesAll, 0.5), q(probesAll, 0.9), q(probesAll, 0.99))
	}
	if tbl := s.stepTable(rs); tbl != "" {
		var stepsAll, suspAll []float64
		for _, r := range rs {
			if r.kind == "step" && r.stepMs > 0 {
				stepsAll = append(stepsAll, r.stepMs)
			}
			if r.suspendMs > 0 {
				suspAll = append(suspAll, r.suspendMs)
			}
		}
		sortFloats(stepsAll)
		sortFloats(suspAll)
		if len(stepsAll) > 0 {
			fmt.Printf("step work ms p50=%.0f p90=%.0f p99=%.0f (the agent's own ops, wake excluded)\n",
				q(stepsAll, 0.5), q(stepsAll, 0.9), q(stepsAll, 0.99))
		}
		if len(suspAll) > 0 {
			fmt.Printf("driver suspend ms p50=%.0f p90=%.0f p99=%.0f (SuspendActor after each step)\n",
				q(suspAll, 0.5), q(suspAll, 0.9), q(suspAll, 0.99))
		}
		fmt.Print(tbl)
	}
	fmt.Println("=== agentsim csv ===")
	fmt.Println("unix_ms,agent,kind,first_req_ms,readram_ms,pings,errors,refusals,step,step_ms,suspend_ms,was_suspended")
	for _, r := range rs {
		fmt.Printf("%d,%d,%s,%.1f,%.1f,%d,%d,%d,%s,%.1f,%.1f,%d\n",
			r.unixMs, r.agent, r.kind, r.firstReqMs, r.readRAMMs, r.pings, r.errors, r.refusals,
			r.step, r.stepMs, r.suspendMs, r.wasSuspended)
	}
	fmt.Println("=== end csv ===")
}

func sortFloats(v []float64) { sort.Float64s(v) }

func q(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	i := int(p*float64(len(sorted))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}
