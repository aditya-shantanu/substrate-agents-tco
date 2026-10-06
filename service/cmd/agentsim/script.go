package main

// Script mode: instead of the personal-agent turns (ping + RAM churn + file
// write), each "session" plays an agent-session script — the coding-agent
// workload from substrate's benchmarking suite (internal/agentscript is a
// vendored copy of its loader). Every step is: an LLM think gap, a wake
// ping through the router (the user-visible resume), the step's glutton
// ops, and — in driver mode — an explicit SuspendActor, exactly as the
// upstream boomer driver does. In idle mode the autosuspender's idle
// timeout decides when the actor sleeps, which is how a production gateway
// would behave; steps whose think gap is shorter than that wait then find
// the actor still awake.
//
// Think gaps are scaled by --think-scale and jittered ±20%, and are NOT
// time-compressed: like suspend/resume they are the quantity under test
// (how much idle a wake costs to reclaim). --compress still governs the
// Poisson gaps between tasks.

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/adityashantanu/substrate-agents-tco/service/internal/agentscript"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	writeModeOverwrite = 1 // WRITE_MODE_OVERWRITE: grow on first touch, re-randomize in place after
	readModeData       = 0 // READ_MODE_DATA (proto3 zero: omitted on the wire)
	readModeDigest     = 1 // READ_MODE_DIGEST_ONLY
)

// checkScriptFile is --check-script: the deploy step's pre-flight for a
// user-supplied YAML. Prints the script's shape and returns the exit code.
func checkScriptFile(source string) int {
	sc, err := agentscript.Resolve(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "script check failed:", err)
		return 1
	}
	sum := agentscript.Summarize(sc)
	b := agentscript.Budgets(sc.Steps)
	fmt.Printf("script %q: %d steps, think %s/task, burn_cpu %s wall (%s cpu), ingest %s, disk writes %s; declared RAM %s + disk %s, min_actor_memory %s\n",
		sc.Name, sum.Steps, sum.Think, sum.BurnWall, sum.BurnCPU, agentscript.FormatSize(sum.Ingest),
		agentscript.FormatSize(sum.DiskWrite), agentscript.FormatSize(b.RAM), agentscript.FormatSize(b.Disk),
		agentscript.FormatSize(sc.MinActorMemory))
	return 0
}

// loadScript resolves --script, checks the actor template's memory limit
// against the script's floor (the upstream driver's guard), and switches
// off the personal-agent per-turn work, which the script replaces.
func (s *sim) loadScript(ctx context.Context) error {
	switch s.cfg.lifecycle {
	case "suspend", "pause":
	default:
		return fmt.Errorf("--lifecycle-mode must be suspend or pause, got %q", s.cfg.lifecycle)
	}
	if s.cfg.pingActorsPerUser > 0 || s.cfg.pingIndependent {
		if s.cfg.script != "" {
			return fmt.Errorf("--script and the one-ping modes are mutually exclusive")
		}
		if s.cfg.pingActorsPerUser > 0 && s.cfg.pingIndependent {
			return fmt.Errorf("--ping-actors-per-user and --ping-independent are mutually exclusive")
		}
		// "nomem": the one-ping agent is a bare glutton process. No RAM
		// fill, no per-turn work, no CPU-probe file.
		s.cfg.memTarget, s.cfg.memChurn, s.cfg.memRead, s.cfg.diskBytes, s.cfg.probeBytes = "", "", "", 0, 0
		slog.Info("one-ping workload", "actors_per_user", s.cfg.pingActorsPerUser,
			"wait", s.cfg.pingWait.String(), "live", s.cfg.pingLive.String())
		return nil
	}
	if s.cfg.script == "" {
		return nil
	}
	switch s.cfg.scriptSuspend {
	case "driver", "idle":
	default:
		return fmt.Errorf("--script-suspend must be driver or idle, got %q", s.cfg.scriptSuspend)
	}
	if s.cfg.thinkScale <= 0 {
		s.cfg.thinkScale = 1
	}
	sc, err := agentscript.Resolve(s.cfg.script)
	if err != nil {
		return err
	}
	s.script = sc
	sum := agentscript.Summarize(sc)
	s.ingestBuf = make([]byte, sum.MaxIngest)
	if _, err := crand.Read(s.ingestBuf); err != nil {
		slog.Warn("ingest payload not randomized; zeros still cross the wire", "err", err)
	}

	// The script fills its own RAM and files; the per-turn knobs would only
	// add work the script's author did not ask for.
	if s.cfg.memTarget != "" || s.cfg.memChurn != "" || s.cfg.memRead != "" || s.cfg.diskBytes > 0 {
		slog.Info("script mode: ignoring --mem-target/--mem-read/--mem-churn/--disk-bytes (the script governs the sandbox work)")
		s.cfg.memTarget, s.cfg.memChurn, s.cfg.memRead, s.cfg.diskBytes = "", "", "", 0
	}

	// Refuse a template too small for the script rather than OOM-flaking.
	tmpl, err := s.api.GetActorTemplate(ctx, &ateapipb.GetActorTemplateRequest{
		ActorTemplate: &ateapipb.ObjectRef{Atespace: s.cfg.tmplAtespace, Name: s.cfg.tmpl}})
	if err != nil {
		return fmt.Errorf("GetActorTemplate %s/%s: %w", s.cfg.tmplAtespace, s.cfg.tmpl, err)
	}
	limit, found := int64(0), false
	for _, l := range tmpl.GetResources().GetLimits() {
		if l.GetName() != "memory" {
			continue
		}
		n, err := agentscript.ParseSize(l.GetQuantity())
		if err != nil {
			return fmt.Errorf("template %s/%s memory limit %q: %w", s.cfg.tmplAtespace, s.cfg.tmpl, l.GetQuantity(), err)
		}
		limit, found = n, true
	}
	switch {
	case !found:
		slog.Warn("template sets no memory limit; cannot check it against the script",
			"template", s.cfg.tmpl, "min_actor_memory", agentscript.FormatSize(sc.MinActorMemory))
	case limit < sc.MinActorMemory:
		return fmt.Errorf("template %s/%s memory limit %s is below script %q min_actor_memory %s; redeploy the workloads with ACTOR_MEMORY=%s",
			s.cfg.tmplAtespace, s.cfg.tmpl, agentscript.FormatSize(limit), sc.Name,
			agentscript.FormatSize(sc.MinActorMemory), agentscript.FormatSize(sc.MinActorMemory))
	}
	slog.Info("script loaded", "script", sc.Name, "steps", sum.Steps,
		"think_per_task", (time.Duration(float64(sum.Think) * s.cfg.thinkScale)).Round(time.Second).String(),
		"think_scale", s.cfg.thinkScale, "burn_cpu_wall", sum.BurnWall.String(),
		"ingest", agentscript.FormatSize(sum.Ingest), "suspend", s.cfg.scriptSuspend,
		"template_memory", agentscript.FormatSize(limit))
	return nil
}

// printProfile writes the workload's shape as a key=value block the
// analysis reads back (analysis/final_report.py), so the cost model prices
// the workload that actually ran rather than defaults.
func (s *sim) printProfile() {
	fmt.Println("=== agentsim profile ===")
	fmt.Printf("compress=%g\n", s.cfg.compress)
	fmt.Printf("wakes_per_day=%g\n", s.cfg.wakesPerDay)
	fmt.Println("wake_seconds=15")
	fmt.Printf("lifecycle=%s\n", s.cfg.lifecycle)
	if s.cfg.pingActorsPerUser > 0 || s.cfg.pingIndependent {
		fmt.Println("workload=ping")
		fmt.Printf("actors_per_user=%d\n", s.cfg.pingActorsPerUser)
		fmt.Printf("independent=%t\n", s.cfg.pingIndependent)
		fmt.Printf("wait_s=%.3f\n", s.cfg.pingWait.Seconds())
		fmt.Printf("live_s=%.3f\n", s.cfg.pingLive.Seconds())
		fmt.Println("suspend_mode=driver")
		fmt.Println("=== end profile ===")
		os.Stdout.Sync()
		return
	}
	if s.script == nil {
		fmt.Println("workload=personal")
		fmt.Printf("sessions_per_day=%g\n", s.cfg.sessionsPerDay)
		fmt.Printf("session_minutes=%g\n", s.cfg.sessionMin)
	} else {
		sum := agentscript.Summarize(s.script)
		fmt.Println("workload=script")
		fmt.Printf("script=%s\n", s.script.Name)
		fmt.Printf("steps=%d\n", sum.Steps)
		fmt.Printf("think_s=%.3f\n", sum.Think.Seconds())
		fmt.Printf("think_scale=%g\n", s.cfg.thinkScale)
		fmt.Printf("think_scaled_s=%.3f\n", sum.Think.Seconds()*s.cfg.thinkScale)
		fmt.Printf("burn_wall_s=%.3f\n", sum.BurnWall.Seconds())
		fmt.Printf("burn_cpu_s=%.3f\n", sum.BurnCPU.Seconds())
		fmt.Printf("tasks_per_day=%g\n", s.cfg.sessionsPerDay)
		fmt.Printf("dwell_s=%.3f\n", sum.Dwell.Seconds())
		fmt.Printf("script_loop=%t\n", s.cfg.scriptLoop)
		fmt.Printf("suspend_mode=%s\n", s.cfg.scriptSuspend)
		fmt.Printf("min_actor_memory=%s\n", agentscript.FormatSize(s.script.MinActorMemory))
	}
	fmt.Println("=== end profile ===")
	os.Stdout.Sync()
}

// runTask plays the script once for one agent: one task of a coding
// agent. Each step appends a result row of kind "step" whose first_req_ms
// is the wake ping (the user-visible resume when was_suspended=1), step_ms
// the wall time of the step's ops, and suspend_ms the explicit suspend in
// driver mode.
func (s *sim) runTask(ctx context.Context, id int, name string, rng *rand.Rand, deadline time.Time) {
	taskStart := time.Now()
	consecutiveFailures := 0
	steps, errs := 0, 0
	for _, st := range s.script.Steps {
		// The LLM is producing this step; the actor is asleep (driver mode)
		// or about to be (idle mode).
		gap := time.Duration(float64(st.Think) * s.cfg.thinkScale * (0.8 + 0.4*rng.Float64()))
		if time.Now().Add(gap).After(deadline) {
			break
		}
		time.Sleep(gap)

		r := result{unixMs: time.Now().UnixMilli(), agent: id, kind: "step", step: st.Name}
		r.wasSuspended = s.actorSuspended(ctx, name)
		s.touch(name, "begin")

		// The wake: the step's first request. A saturated pool answers 503
		// (parked past the budget) or 504 (restore outran the route timeout);
		// retry with backoff like a gateway would and count the refusals.
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
			slog.Warn("step wake failed", "actor", name, "step", st.Name, "err", err)
		} else {
			stepStart := time.Now()
			for i, o := range st.Ops {
				if err := s.execOp(ctx, name, o); err != nil {
					r.errors++
					slog.Warn("step op failed", "actor", name, "step", st.Name, "op", i+1, "kind", o.Kind.String(), "err", err)
					break
				}
				r.pings++
			}
			r.stepMs = float64(time.Since(stepStart).Microseconds()) / 1000
		}
		s.touch(name, "end")

		if s.cfg.scriptSuspend == "driver" {
			if ms, err := s.suspendNow(ctx, name); err != nil {
				r.errors++
				slog.Warn("step suspend failed", "actor", name, "step", st.Name, "err", err)
			} else {
				r.suspendMs = ms
			}
		}

		s.mu.Lock()
		s.results = append(s.results, r)
		s.mu.Unlock()
		steps++
		errs += r.errors
		if r.errors > 0 {
			consecutiveFailures++
			if consecutiveFailures >= 3 {
				// Three failed steps in a row: the actor has most likely lost
				// its state (recreated by the medic) and the rest of the
				// script would fail on missing files/arrays. Give up the task.
				slog.Warn("abandoning task after 3 failed steps", "actor", name, "step", st.Name)
				break
			}
		} else {
			consecutiveFailures = 0
		}
	}
	slog.Info("task done", "actor", name, "steps", steps, "errors", errs,
		"wall", time.Since(taskStart).Round(time.Second).String())
}

// actorSuspended reports the actor's state just before a wake: 1 =
// SUSPENDED (the ping will resume it), 0 = RUNNING (still awake), -1 =
// anything else or unknown.
func (s *sim) actorSuspended(ctx context.Context, name string) int {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	a, err := s.api.GetActor(cctx, &ateapipb.GetActorRequest{
		Actor: &ateapipb.ObjectRef{Atespace: s.cfg.atespace, Name: name}})
	if err != nil {
		return -1
	}
	switch a.GetStatus().GetState() {
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDED, ateapipb.ActorState_ACTOR_STATE_PAUSED:
		return 1
	case ateapipb.ActorState_ACTOR_STATE_RUNNING:
		return 0
	}
	return -1
}

// hibernateRPC parks an actor per --lifecycle-mode: SuspendActor (durable,
// bucket) or PauseActor (node-local checkpoint). Both are re-entrant.
func (s *sim) hibernateRPC(ctx context.Context, ref *ateapipb.ObjectRef) error {
	if s.cfg.lifecycle == "pause" {
		_, err := s.api.PauseActor(ctx, &ateapipb.PauseActorRequest{Actor: ref})
		return err
	}
	_, err := s.api.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: ref})
	return err
}

// suspendNow is the driver-mode park after a step or ping cycle (suspend or
// pause per --lifecycle-mode). It retries transient control-plane
// conflicts; an actor already parked (FailedPrecondition) counts as success
// without a timing. Returns the call's wall ms.
func (s *sim) suspendNow(ctx context.Context, name string) (float64, error) {
	ref := &ateapipb.ObjectRef{Atespace: s.cfg.atespace, Name: name}
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		t := time.Now()
		err = s.hibernateRPC(cctx, ref)
		cancel()
		if err == nil {
			return float64(time.Since(t).Microseconds()) / 1000, nil
		}
		if status.Code(err) == codes.FailedPrecondition {
			return 0, nil
		}
		if code := status.Code(err); code != codes.Aborted && code != codes.Unavailable {
			return 0, err
		}
		time.Sleep(time.Duration(1000+rand.IntN(2000)) * time.Millisecond)
	}
	return 0, err
}

// execOp performs one scripted op as a glutton RPC through the router.
func (s *sim) execOp(ctx context.Context, name string, o agentscript.Op) error {
	var err error
	switch o.Kind {
	case agentscript.KindIngest:
		payload := s.ingestBuf
		if int64(len(payload)) < o.Bytes {
			payload = make([]byte, o.Bytes)
		}
		_, err = s.post(ctx, name, "/ingest", ingestBody(o.Key, payload[:o.Bytes]))
	case agentscript.KindBurnCPU:
		_, err = s.post(ctx, name, "/burncpu", burnCPUBody(o.Millis, o.Parallel))
	case agentscript.KindWriteDisk:
		_, err = s.post(ctx, name, "/writedisk", writeDiskBody(o.Key, int(o.Bytes)))
	case agentscript.KindReadDiskDigest:
		_, err = s.post(ctx, name, "/readdisk", readDiskModeBody(o.Key, readModeDigest))
	case agentscript.KindReadDiskData:
		_, err = s.post(ctx, name, "/readdisk", readDiskModeBody(o.Key, readModeData))
	case agentscript.KindFillRAM:
		// OVERWRITE grows the array on first touch and re-randomizes it in
		// place on later laps; TRUNCATE would reallocate and transiently
		// double the guest heap, which OOMs a tightly-sized sandbox.
		_, err = s.post(ctx, name, "/writeram", writeRAMBody(o.Key, fmt.Sprintf("%d", o.Bytes), writeModeOverwrite))
	case agentscript.KindChurnRAM:
		_, err = s.post(ctx, name, "/writeram", writeRAMBody(o.Key, fmt.Sprintf("%d", o.Bytes), writeModeRotate))
	case agentscript.KindWalkRAM:
		_, err = s.post(ctx, name, "/readram", readRAMBody(o.Key, ""))
	case agentscript.KindPing:
		_, err = s.post(ctx, name, "/ping", nil)
	case agentscript.KindDwell:
		// No request: the actor stays resident while the driver waits, the
		// way a gateway sits in a model round trip or a typing gap.
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(time.Duration(o.Millis) * time.Millisecond):
		}
	default:
		return fmt.Errorf("unknown op kind %d", o.Kind)
	}
	return err
}

// IngestRequest{key=1 string, payload=2 bytes}
func ingestBody(key string, payload []byte) []byte {
	b := make([]byte, 0, len(key)+len(payload)+16)
	b = appendString(b, 1, key)
	b = appendBytes(b, 2, payload)
	return b
}

// BurnCPURequest{duration_ms=1 int64, parallelism=2 int32}
func burnCPUBody(millis int64, parallel int32) []byte {
	var b []byte
	b = appendVarint(b, 1, uint64(millis))
	b = appendVarint(b, 2, uint64(parallel))
	return b
}

// ReadDiskRequest{key=1 string, read_mode=2 enum}
func readDiskModeBody(key string, mode int) []byte {
	var b []byte
	b = appendString(b, 1, key)
	b = appendVarint(b, 2, uint64(mode))
	return b
}

func appendBytes(b []byte, field int, p []byte) []byte {
	b = append(b, byte(field<<3|2))
	b = binary.AppendUvarint(b, uint64(len(p)))
	return append(b, p...)
}

// stepTable renders per-step wake/work/suspend quantiles from the result
// rows, in script order, for the end-of-run summary.
func (s *sim) stepTable(rs []result) string {
	if s.script == nil {
		return ""
	}
	byStep := map[string][]result{}
	for _, r := range rs {
		if r.kind == "step" {
			byStep[r.step] = append(byStep[r.step], r)
		}
	}
	var b strings.Builder
	b.WriteString("=== agentsim steps ===\n")
	b.WriteString("step,n,suspended_pct,wake_p50_ms,wake_p99_ms,step_p50_ms,step_p99_ms,suspend_p50_ms,errors\n")
	for _, st := range s.script.Steps {
		rows := byStep[st.Name]
		if len(rows) == 0 {
			continue
		}
		var wake, step, susp []float64
		suspended, errs := 0, 0
		for _, r := range rows {
			wake = append(wake, r.firstReqMs)
			if r.stepMs > 0 {
				step = append(step, r.stepMs)
			}
			if r.suspendMs > 0 {
				susp = append(susp, r.suspendMs)
			}
			if r.wasSuspended == 1 {
				suspended++
			}
			errs += r.errors
		}
		sortFloats(wake)
		sortFloats(step)
		sortFloats(susp)
		fmt.Fprintf(&b, "%s,%d,%.0f,%.0f,%.0f,%.0f,%.0f,%.0f,%d\n", st.Name, len(rows),
			100*float64(suspended)/float64(len(rows)), q(wake, 0.5), q(wake, 0.99),
			q(step, 0.5), q(step, 0.99), q(susp, 0.5), errs)
	}
	b.WriteString("=== end steps ===\n")
	return b.String()
}
