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
}

type sim struct {
	cfg  cfg
	api  ateapipb.ControlClient
	http *http.Client

	mu      sync.Mutex
	results []result
}

func main() {
	var c cfg
	var endpoint, serverName, caFile, credBundle string
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
	flag.IntVar(&c.agents, "agents", 50, "number of simulated agents")
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
	flag.Float64Var(&c.failRefusalPct, "fail-refusal-pct", 5, "load-test: stop when router refusals exceed this % of activations in a wave")
	flag.Float64Var(&c.failErrPct, "fail-error-pct", 2, "load-test: stop when request errors exceed this % of activations in a wave")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

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

	if err := s.setup(ctx); err != nil {
		slog.Error("setup", "err", err)
		os.Exit(1)
	}

	slog.Info("starting load", "agents", c.agents, "duration", c.duration.String(),
		"compress", c.compress, "duty_cycle_uncompressed",
		fmt.Sprintf("%.2f%%", 100*(c.sessionsPerDay*c.sessionMin*60+c.wakesPerDay*15)/86400))

	deadline := time.Now().Add(c.duration)
	progressCtx, stopProgress := context.WithCancel(ctx)
	go s.progressLoop(progressCtx)
	if c.loadTest {
		s.runLoadTest(ctx, deadline)
	} else {
		var wg sync.WaitGroup
		for i := 0; i < c.agents; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				s.agentLoop(ctx, id, deadline)
			}(i)
		}
		wg.Wait()
	}
	stopProgress()

	s.report()
}

func actorName(id int) string { return fmt.Sprintf("sim-%04d", id) }

/* ---------------- setup: atespace, actors, first boot, RAM fill ---------------- */

func (s *sim) setup(ctx context.Context) error {
	_, err := s.api.CreateAtespace(ctx, &ateapipb.CreateAtespaceRequest{
		Atespace: &ateapipb.Atespace{Metadata: &ateapipb.ResourceMetadata{Name: s.cfg.atespace}},
	})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("CreateAtespace: %w", err)
	}
	sem := make(chan struct{}, 8)
	errCh := make(chan error, s.cfg.agents)
	var wg sync.WaitGroup
	for i := 0; i < s.cfg.agents; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer func() { <-sem; wg.Done() }()
			errCh <- s.setupOne(ctx, id)
		}(i)
	}
	wg.Wait()
	close(errCh)
	failed := 0
	for e := range errCh {
		if e != nil {
			failed++
			slog.Warn("agent setup failed", "err", e)
		}
	}
	if failed > s.cfg.agents/10 {
		return fmt.Errorf("%d/%d agents failed setup", failed, s.cfg.agents)
	}
	slog.Info("setup complete", "agents", s.cfg.agents-failed, "failed", failed)
	return nil
}

func (s *sim) setupOne(ctx context.Context, id int) error {
	name := actorName(id)
	ref := &ateapipb.ObjectRef{Atespace: s.cfg.atespace, Name: name}
	_, err := s.api.CreateActor(ctx, &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: s.cfg.atespace, Name: name},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: s.cfg.tmplAtespace, Name: s.cfg.tmpl},
	}})
	created := err == nil
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
			_, err := s.api.ResumeActor(cctx, &ateapipb.ResumeActorRequest{Actor: ref, Boot: true})
			if err == nil {
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
		if s.cfg.memTarget != "" {
			if _, err := s.post(cctx, name, "/writeram",
				writeRAMBody("memload", s.cfg.memTarget, writeModeTruncate)); err != nil {
				return fmt.Errorf("fill RAM %s: %w", name, err)
			}
		}
		if _, err := s.api.SuspendActor(cctx, &ateapipb.SuspendActorRequest{Actor: ref}); err != nil &&
			status.Code(err) != codes.FailedPrecondition {
			return fmt.Errorf("first suspend %s: %w", name, err)
		}
	}
	return nil
}

/* ---------------- the workload ---------------- */

func (s *sim) agentLoop(ctx context.Context, id int, deadline time.Time) {
	name := actorName(id)
	rng := rand.New(rand.NewPCG(uint64(id), 0xa9e1))
	time.Sleep(time.Duration(rng.Float64() * s.cfg.rampSec * float64(time.Second)))

	ratePerSec := (s.cfg.sessionsPerDay + s.cfg.wakesPerDay) / 86400.0 * s.cfg.compress
	pSession := s.cfg.sessionsPerDay / (s.cfg.sessionsPerDay + s.cfg.wakesPerDay)

	for {
		gap := time.Duration(rng.ExpFloat64() / ratePerSec * float64(time.Second))
		if time.Now().Add(gap).After(deadline) {
			return
		}
		time.Sleep(gap)
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
			if _, err := s.post(ctx, name, "/ping", nil); err != nil {
				r.errors++
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

// runLoadTest activates agent loops in waves and watches each wave's
// refusal/error rates. When a wave trips the failure thresholds (or all
// agents are active, or the deadline passes) it stops and logs the verdict:
// the last level that stayed under the thresholds is the pool's sustainable
// maximum for this workload.
func (s *sim) runLoadTest(ctx context.Context, deadline time.Time) {
	type waveStat struct {
		level, acts, refusals, errors int
		wakeP50, wakeP99              float64
	}
	loopCtx, cancelLoops := context.WithCancel(ctx)
	defer cancelLoops()
	var wg sync.WaitGroup
	active := 0
	lastGood := 0
	var waves []waveStat

	activate := func(n int) {
		for ; active < n && active < s.cfg.agents; active++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				s.agentLoop(loopCtx, id, deadline)
			}(active)
		}
	}

	for level := s.cfg.waveStart; ; level += s.cfg.waveStep {
		if level > s.cfg.agents {
			level = s.cfg.agents
		}
		activate(level)
		slog.Info("wave", "active_agents", active, "observing_for", s.cfg.waveInterval.String())
		waveStartMs := time.Now().UnixMilli()
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.cfg.waveInterval):
		}
		// score the wave from results inside its window
		s.mu.Lock()
		st := waveStat{level: active}
		var wakes []float64
		for _, r := range s.results {
			if r.unixMs < waveStartMs {
				continue
			}
			st.acts++
			st.refusals += r.refusals
			st.errors += r.errors
			if r.kind == "wake" {
				wakes = append(wakes, r.firstReqMs)
			}
		}
		s.mu.Unlock()
		sort.Float64s(wakes)
		st.wakeP50, st.wakeP99 = q(wakes, 0.5), q(wakes, 0.99)
		waves = append(waves, st)
		refPct, errPct := 0.0, 0.0
		if st.acts > 0 {
			refPct = 100 * float64(st.refusals) / float64(st.acts)
			errPct = 100 * float64(st.errors) / float64(st.acts)
		}
		failed := refPct > s.cfg.failRefusalPct || errPct > s.cfg.failErrPct
		slog.Info("wave result", "active_agents", st.level, "activations", st.acts,
			"refusal_pct", fmt.Sprintf("%.1f", refPct), "error_pct", fmt.Sprintf("%.1f", errPct),
			"wake_p50_ms", int(st.wakeP50), "wake_p99_ms", int(st.wakeP99), "failed", failed)
		if failed || st.level >= s.cfg.agents || time.Now().After(deadline) {
			cancelLoops()
			fmt.Println("=== loadtest waves ===")
			fmt.Println("active_agents,activations,refusals,errors,wake_p50_ms,wake_p99_ms")
			for _, w := range waves {
				fmt.Printf("%d,%d,%d,%d,%.0f,%.0f\n",
					w.level, w.acts, w.refusals, w.errors, w.wakeP50, w.wakeP99)
			}
			fmt.Println("=== end loadtest ===")
			if failed {
				fmt.Printf("LOADTEST VERDICT: failure at %d active agents; last sustainable level = %d\n",
					st.level, lastGood)
			} else {
				fmt.Printf("LOADTEST VERDICT: no failure up to %d active agents (raise --agents to push further)\n", st.level)
			}
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
			if r.kind == "wake" {
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
		qi := func(v []float64, p float64) int {
			if len(v) == 0 {
				return 0
			}
			return int(q(v, p))
		}
		slog.Info("progress",
			"activations", len(wake)+len(sess),
			"wake_p50_ms", qi(wake, 0.5),
			"wake_p99_ms", qi(wake, 0.99),
			"session_p50_ms", qi(sess, 0.5),
			"errors", errs, "refusals", refs)
	}
}

/* ---------------- report ---------------- */

func (s *sim) report() {
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
	fmt.Println("=== agentsim csv ===")
	fmt.Println("unix_ms,agent,kind,first_req_ms,readram_ms,pings,errors,refusals")
	for _, r := range rs {
		fmt.Printf("%d,%d,%s,%.1f,%.1f,%d,%d,%d\n",
			r.unixMs, r.agent, r.kind, r.firstReqMs, r.readRAMMs, r.pings, r.errors, r.refusals)
	}
	fmt.Println("=== end csv ===")
}

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
