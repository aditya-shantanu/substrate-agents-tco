package main

// Swap mode (turnover at a fixed fill): the node is held at --swap-fill
// resident, idle actors (each with --mem-target of RAM, so a wake restores a
// real snapshot); every --swap-every, N resident actors are parked (suspend
// or pause per --lifecycle-mode) and N parked actors are woken. N starts at
// --swap-start and is multiplied by --swap-mult per level (one level per
// --wave-interval). Every swap is one park + one wake, so "activations per
// second" is the swap rate. Gates per level: wake and park P90 within
// --fail-rel-p90 of the first level, wake P99 under --fail-wake-p99-ms,
// errors/refusals, node memory/PSI/crashes, and a backlog rule (a level
// whose swaps cannot finish inside their tick is failing even if each swap
// is fast). After a failing level the ramp holds at the last clean N.

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// swapGate coordinates the resident agents' own steps (the assistant day at
// its real pace) with the swap ticks: a tick parks only actors that are not
// inside a step, and an agent whose actor was parked waits at its next step
// until a later tick wakes it. first-lap agents start at a random point of
// the day so the fleet is spread over it.
type swapGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	busy   map[int]bool
	parked map[int]bool
	lapped map[int]bool
}

func newSwapGate() *swapGate {
	g := &swapGate{busy: map[int]bool{}, parked: map[int]bool{}, lapped: map[int]bool{}}
	g.cond = sync.NewCond(&g.mu)
	return g
}

func (g *swapGate) firstLap(id int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lapped[id] {
		return false
	}
	g.lapped[id] = true
	return true
}

// enter blocks while the actor is parked, then marks it busy; false if ctx ended.
func (g *swapGate) enter(ctx context.Context, id int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.parked[id] {
		if ctx.Err() != nil {
			return false
		}
		done := make(chan struct{})
		go func() { // wake the waiter if ctx ends
			select {
			case <-ctx.Done():
				g.mu.Lock()
				g.cond.Broadcast()
				g.mu.Unlock()
			case <-done:
			}
		}()
		g.cond.Wait()
		close(done)
	}
	g.busy[id] = true
	return true
}

func (g *swapGate) leave(id int) {
	g.mu.Lock()
	g.busy[id] = false
	g.mu.Unlock()
}

// pick takes up to n actors from the front of the resident queue that are
// not inside a step and marks them parked; returns them and the remaining queue.
func (g *swapGate) pick(resident []int, n int) (chosen, rest []int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range resident {
		if len(chosen) < n && !g.busy[id] {
			g.parked[id] = true
			chosen = append(chosen, id)
		} else {
			rest = append(rest, id)
		}
	}
	return chosen, rest
}

func (g *swapGate) unpark(id int) {
	g.mu.Lock()
	g.parked[id] = false
	g.cond.Broadcast()
	g.mu.Unlock()
}

type swapStat struct {
	tag                                string
	n                                  int
	targetPerS, achievedPerS           float64
	wakes, parks, errors, refusals     int
	stepActs, stepErrors               int // the resident agents' own steps in the window (not swaps)
	wakeP50, wakeP90, wakeP99          float64
	parkP50, parkP90, parkP99          float64
	backlog                            int
	memAvailPct, psiCPU, psiMem, psiIO float64
	running, crashed                   int
	failedOn                           string
}

func (s *sim) runSwap(ctx context.Context, deadline time.Time) {
	if s.cfg.swapFill >= len(s.ready) {
		slog.Error("swap: --swap-fill must leave parked spares", "fill", s.cfg.swapFill, "ready", len(s.ready))
		return
	}
	resident := append([]int(nil), s.ready[:s.cfg.swapFill]...)
	parked := append([]int(nil), s.ready[s.cfg.swapFill:]...)

	// ---- fill: wake the resident set and leave it idle
	// Gentle fill: 8 wakes in flight (each restores ~1 GiB from the node disk,
	// which is still absorbing the setup writes) and retry a failed wake until
	// the actor is really resident; an actor that will not come up stays
	// parked so the queues reflect reality.
	// The resident agents do what they are supposed to do, at the script's
	// real pace (think ×1 → ~2.5 steps per hour each); an agent's loop starts
	// the moment its own fill wake succeeds (so the first-lap catch-up is
	// spread over the fill), parked agents wait at their next step until a
	// tick wakes them.
	loopCtx, cancelLoops := context.WithCancel(ctx)
	defer cancelLoops()
	var lw sync.WaitGroup
	startLoop := func(id int) {
		if s.script == nil {
			return
		}
		lw.Add(1)
		go func() {
			defer lw.Done()
			rng := rand.New(rand.NewPCG(uint64(id), 0x5a7a))
			for time.Now().Before(deadline) && loopCtx.Err() == nil {
				s.runTask(loopCtx, id, actorName(id), rng, deadline)
			}
		}()
	}
	if s.script != nil {
		s.swapGate = newSwapGate()
		s.swapGate.mu.Lock()
		for _, id := range s.ready { // everything starts parked; a successful fill wake un-parks
			s.swapGate.parked[id] = true
		}
		s.swapGate.mu.Unlock()
	}

	slog.Info("swap fill", "resident", len(resident), "parked", len(parked))
	t0 := time.Now()
	var fw sync.WaitGroup
	var fmu sync.Mutex
	var up, down []int
	sem := make(chan struct{}, 8)
	for _, id := range resident {
		fw.Add(1)
		go func(id int) {
			defer fw.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var err error
			for attempt := 0; attempt < 12 && ctx.Err() == nil; attempt++ {
				if err = s.swapWake(ctx, id, "fill"); err == nil {
					break
				}
				if permanentWakeError(err) { // crashed / deleting / gone: no amount of retrying wakes it
					break
				}
				time.Sleep(time.Duration(5+attempt*5) * time.Second)
			}
			fmu.Lock()
			if err == nil {
				up = append(up, id)
			} else {
				down = append(down, id)
			}
			fmu.Unlock()
			if err == nil {
				if s.swapGate != nil {
					s.swapGate.unpark(id)
				}
				startLoop(id)
			}
		}(id)
	}
	fw.Wait()
	sort.Ints(up)
	resident = up
	parked = append(parked, down...)
	for _, id := range parked { // pool agents: loops exist but block until a tick wakes them
		startLoop(id)
	}
	slog.Info("swap fill done", "took", time.Since(t0).String(), "resident", len(resident), "parked", len(parked), "fill_failures", len(down), "think_scale", s.cfg.thinkScale)

	var mu sync.Mutex // guards resident/parked queues and the park samples
	var parkSamples []tsample
	var inFlight int
	ever := map[int]bool{} // actors that have been resident at least once since the fill
	for _, id := range resident {
		ever[id] = true
	}

	// one tick: park N oldest residents, wake N oldest parked; runs
	// asynchronously so a slow tick does not delay the next one
	tick := func(n int) {
		mu.Lock()
		if n > len(resident) || n > len(parked) {
			n = min(len(resident), len(parked))
		}
		var toPark []int
		if s.swapGate != nil {
			toPark, resident = s.swapGate.pick(resident, n)
			n = len(toPark)
		} else {
			toPark = append([]int(nil), resident[:n]...)
			resident = resident[n:]
		}
		toWake := append([]int(nil), parked[:n]...)
		parked = parked[n:]
		inFlight += n
		mu.Unlock()
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(2)
			go func(id int) {
				defer wg.Done()
				ms, err := s.swapPark(ctx, id)
				mu.Lock()
				if err == nil {
					parkSamples = append(parkSamples, tsample{time.Now().UnixMilli(), ms})
					parked = append(parked, id)
				} else if status.Code(err) == codes.FailedPrecondition {
					parked = append(parked, id) // was already parked (a failed earlier wake)
				} else {
					resident = append(resident, id) // park failed: it is still awake
					if s.swapGate != nil {
						s.swapGate.unpark(id)
					}
				}
				mu.Unlock()
			}(toPark[i])
			go func(id int) {
				defer wg.Done()
				err := s.swapWake(ctx, id, "swap")
				mu.Lock()
				if err == nil {
					resident = append(resident, id)
					ever[id] = true
				} else {
					parked = append(parked, id) // still parked: back to the pool
				}
				mu.Unlock()
				if err == nil && s.swapGate != nil {
					s.swapGate.unpark(id)
				}
			}(toWake[i])
		}
		wg.Wait()
		mu.Lock()
		inFlight -= n
		mu.Unlock()
	}

	var levels []swapStat
	var baseWakeP90, baseParkP90 float64
	score := func(sinceMs int64, n int, window time.Duration) swapStat {
		st := swapStat{n: n, targetPerS: float64(n) / s.cfg.swapEvery.Seconds()}
		s.mu.Lock()
		var wakes []float64
		for _, r := range s.results {
			if r.unixMs < sinceMs {
				continue
			}
			if r.kind == "step" {
				st.stepActs++
				st.stepErrors += r.errors
				continue
			}
			if r.kind != "swap" {
				continue
			}
			st.wakes++
			st.errors += r.errors
			st.refusals += r.refusals
			if r.errors == 0 {
				wakes = append(wakes, r.firstReqMs)
			}
		}
		s.mu.Unlock()
		mu.Lock()
		var parks []float64
		for _, p := range parkSamples {
			if p.t >= sinceMs {
				parks = append(parks, p.ms)
			}
		}
		st.backlog = inFlight
		mu.Unlock()
		st.parks = len(parks)
		sort.Float64s(wakes)
		sort.Float64s(parks)
		qz := func(v []float64, p float64) float64 {
			if len(v) == 0 {
				return 0
			}
			return q(v, p)
		}
		st.wakeP50, st.wakeP90, st.wakeP99 = qz(wakes, .5), qz(wakes, .9), qz(wakes, .99)
		st.parkP50, st.parkP90, st.parkP99 = qz(parks, .5), qz(parks, .9), qz(parks, .99)
		st.achievedPerS = float64(st.wakes) / window.Seconds()
		st.memAvailPct = -1
		if h, ok := s.fetchHostState(ctx); ok {
			st.memAvailPct, st.psiCPU, st.psiMem, st.psiIO = h.memAvailPct(), h.PsiCPUSome, h.PsiMemFull, h.PsiIOSome
			st.running, st.crashed = h.Running, h.Crashed
		}
		var reasons []string
		acts := max(st.wakes, 1)
		// a single transient error at a small level is not a failure: the rate
		// gates need at least 3 occurrences before they can trip
		if st.refusals >= 3 && 100*float64(st.refusals)/float64(acts) > s.cfg.failRefusalPct {
			reasons = append(reasons, "refusals")
		}
		if st.errors >= 3 && 100*float64(st.errors)/float64(acts) > s.cfg.failErrPct {
			reasons = append(reasons, "errors")
		}
		if s.cfg.failWakeP99Ms > 0 && len(wakes) > 0 && st.wakeP99 > s.cfg.failWakeP99Ms {
			reasons = append(reasons, "wake-p99")
		}
		if s.cfg.failRelP90 > 0 && baseWakeP90 > 0 && len(wakes) >= 20 && st.wakeP90 > s.cfg.failRelP90*baseWakeP90 {
			reasons = append(reasons, "wake-p90-vs-baseline")
		}
		if s.cfg.failRelP90 > 0 && baseParkP90 > 0 && len(parks) >= 20 && st.parkP90 > s.cfg.failRelP90*baseParkP90 {
			reasons = append(reasons, "park-p90-vs-baseline")
		}
		// backlog gate: more than two ticks' worth — or, with short ticks, more than
		// five seconds' worth — of swaps still in flight at the end of the level
		// means arrivals are outrunning the service rate
		backlogTicks := max(2, int(math.Ceil(5/s.cfg.swapEvery.Seconds())))
		if st.backlog > backlogTicks*n {
			reasons = append(reasons, "backlog")
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
		slog.Info("swap level result", "tag", st.tag, "n_per_tick", st.n, "target_swaps_per_s", fmt.Sprintf("%.2f", st.targetPerS),
			"achieved_swaps_per_s", fmt.Sprintf("%.2f", st.achievedPerS), "wakes", st.wakes, "parks", st.parks,
			"wake_p50_ms", int(st.wakeP50), "wake_p90_ms", int(st.wakeP90), "wake_p99_ms", int(st.wakeP99),
			"park_p50_ms", int(st.parkP50), "park_p90_ms", int(st.parkP90), "park_p99_ms", int(st.parkP99),
			"errors", st.errors, "refusals", st.refusals, "backlog", st.backlog, "step_acts", st.stepActs, "step_errors", st.stepErrors,
			"mem_avail_pct", fmt.Sprintf("%.1f", st.memAvailPct), "psi_cpu_some10", st.psiCPU, "psi_mem_full10", st.psiMem, "psi_io_some10", st.psiIO,
			"running", st.running, "crashed", st.crashed, "failed", st.failedOn != "", "failed_on", st.failedOn)
		return st
	}
	runLevel := func(n int, window time.Duration, tag string) swapStat {
		start := time.Now()
		tk := time.NewTicker(s.cfg.swapEvery)
		defer tk.Stop()
		var wg sync.WaitGroup
	loop:
		for {
			select {
			case <-ctx.Done():
				break loop
			case <-tk.C:
				if time.Since(start) >= window {
					break loop
				}
				wg.Add(1)
				go func() { defer wg.Done(); tick(n) }()
			}
		}
		st := score(start.UnixMilli(), n, time.Since(start))
		st.tag = tag
		wg.Wait() // let the level's last ticks drain before the next level starts
		return st
	}
	printTable := func(verdict string) {
		fmt.Println("=== swap levels ===")
		fmt.Println("tag,n_per_tick,target_swaps_per_s,achieved_swaps_per_s,wakes,parks,wake_p50_ms,wake_p90_ms,wake_p99_ms,park_p50_ms,park_p90_ms,park_p99_ms,errors,refusals,backlog,step_acts,step_errors,mem_avail_pct,psi_cpu_some10,psi_mem_full10,psi_io_some10,running,crashed,failed_on")
		for _, l := range levels {
			fmt.Printf("%s,%d,%.2f,%.2f,%d,%d,%.0f,%.0f,%.0f,%.0f,%.0f,%.0f,%d,%d,%d,%d,%d,%.1f,%.2f,%.2f,%.2f,%d,%d,%s\n",
				l.tag, l.n, l.targetPerS, l.achievedPerS, l.wakes, l.parks, l.wakeP50, l.wakeP90, l.wakeP99, l.parkP50, l.parkP90, l.parkP99,
				l.errors, l.refusals, l.backlog, l.stepActs, l.stepErrors, l.memAvailPct, l.psiCPU, l.psiMem, l.psiIO, l.running, l.crashed, l.failedOn)
		}
		fmt.Println("=== end swap ===")
		fmt.Println(verdict)
	}

	defer lw.Wait()
	defer cancelLoops()
	lastGood := 0
	n := s.cfg.swapStart
	for {
		if n > len(parked) || n > len(resident) {
			printTable(fmt.Sprintf("SWAP VERDICT: pool exhausted before failure; last clean level = %d per %s (%.2f swaps/s)", lastGood, s.cfg.swapEvery, float64(lastGood)/s.cfg.swapEvery.Seconds()))
			return
		}
		if time.Now().After(deadline) {
			printTable(fmt.Sprintf("SWAP VERDICT: deadline before failure; last clean level = %d per %s", lastGood, s.cfg.swapEvery))
			return
		}
		slog.Info("swap level", "n_per_tick", n, "target_swaps_per_s", fmt.Sprintf("%.2f", float64(n)/s.cfg.swapEvery.Seconds()), "observing_for", s.cfg.waveInterval.String())
		st := runLevel(n, s.cfg.waveInterval, "")
		if len(levels) == 0 {
			baseWakeP90, baseParkP90 = st.wakeP90, st.parkP90
		}
		levels = append(levels, st)
		if st.failedOn == "" {
			lastGood = n
			n = int(math.Ceil(float64(n) * s.cfg.swapMult))
			continue
		}
		verdict := fmt.Sprintf("SWAP VERDICT: failure at %d per %s (%.2f swaps/s target, %.2f achieved; %s); last clean level = %d per %s (%.2f swaps/s)",
			st.n, s.cfg.swapEvery, st.targetPerS, st.achievedPerS, st.failedOn, lastGood, s.cfg.swapEvery, float64(lastGood)/s.cfg.swapEvery.Seconds())
		if s.cfg.holdAfterFail > 0 && lastGood > 0 {
			slog.Info("swap hold", "n_per_tick", lastGood, "for", s.cfg.holdAfterFail.String())
			hs := runLevel(lastGood, s.cfg.holdAfterFail, "hold")
			levels = append(levels, hs)
			if hs.failedOn != "" {
				verdict += fmt.Sprintf("; hold at %d also failed (%s)", lastGood, hs.failedOn)
			} else {
				verdict += fmt.Sprintf("; hold at %d clean for %s", lastGood, s.cfg.holdAfterFail)
			}
		}
		if s.cfg.swapCycleAll && lastGood > 0 {
			verdict += "; " + s.swapCycle(ctx, deadline, lastGood, &ever, &mu, tick, score, &levels)
		}
		printTable(verdict)
		return
	}
}

// swapWake is the wake half of a swap: the first request after a park,
// with the same refusal retries as a gateway; recorded as a result of the
// given kind (wasSuspended = 1 so the report treats it as a wake).
func (s *sim) swapWake(ctx context.Context, id int, kind string) error {
	name := actorName(id)
	r := result{unixMs: time.Now().UnixMilli(), agent: id, kind: kind, wasSuspended: 1}
	start := time.Now()
	var err error
	for attempt := 0; ; attempt++ {
		_, err = s.post(ctx, name, "/ping", nil)
		if err == nil || attempt >= 5 || !isRefusal(err) {
			break
		}
		r.refusals++
		time.Sleep(time.Duration(150+attempt*150) * time.Millisecond)
	}
	r.firstReqMs = float64(time.Since(start).Microseconds()) / 1000
	r.pings = 1
	if err != nil {
		r.errors++
		slog.Warn("swap wake failed", "actor", name, "err", err)
	}
	s.touch(name, "touch")
	s.mu.Lock()
	s.results = append(s.results, r)
	s.mu.Unlock()
	return err
}

// swapPark is the park half: SuspendActor or PauseActor per --lifecycle-mode,
// retried on transient control-plane conflicts; returns the call's wall ms.
func (s *sim) swapPark(ctx context.Context, id int) (float64, error) {
	ref := &ateapipb.ObjectRef{Atespace: s.cfg.atespace, Name: actorName(id)}
	var err error
	t := time.Now()
	for attempt := 0; attempt < 5; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		err = s.hibernateRPC(cctx, ref)
		cancel()
		if err == nil {
			return float64(time.Since(t).Microseconds()) / 1000, nil
		}
		if code := status.Code(err); code != codes.Aborted && code != codes.Unavailable {
			break
		}
		time.Sleep(time.Duration(500+attempt*500) * time.Millisecond)
	}
	slog.Warn("swap park failed", "actor", actorName(id), "err", err)
	return 0, err
}

// swapCycle keeps swapping at n per tick until every actor in the pool has
// been resident at least once (or the deadline), then reports the cycle:
// wall time, swaps, and wake/park quantiles over the whole cycle.
func (s *sim) swapCycle(ctx context.Context, deadline time.Time, n int, ever *map[int]bool, mu *sync.Mutex,
	tick func(int), score func(int64, int, time.Duration) swapStat, levels *[]swapStat) string {
	total := len(s.ready)
	mu.Lock()
	done := len(*ever)
	mu.Unlock()
	slog.Info("swap cycle", "n_per_tick", n, "already_resident_once", done, "pool", total)
	start := time.Now()
	tk := time.NewTicker(s.cfg.swapEvery)
	defer tk.Stop()
	var wg sync.WaitGroup
	lastLog := time.Now()
	stall := time.Now()
	lastDone := -1
	for {
		mu.Lock()
		done = len(*ever)
		mu.Unlock()
		if done != lastDone {
			lastDone, stall = done, time.Now()
		}
		// actors that cannot be woken (crashed, stuck) would hold the cycle open forever: stop once nothing has
		// joined the resident-once set for three minutes and report the shortfall
		if done >= total || time.Since(stall) > 3*time.Minute || time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-tk.C:
			wg.Add(1)
			go func() { defer wg.Done(); tick(n) }()
		}
		if time.Since(lastLog) > time.Minute {
			slog.Info("swap cycle progress", "resident_once", done, "pool", total, "elapsed", time.Since(start).Round(time.Second).String())
			lastLog = time.Now()
		}
	}
	wg.Wait()
	st := score(start.UnixMilli(), n, time.Since(start))
	st.tag = "cycle"
	*levels = append(*levels, st)
	mu.Lock()
	done = len(*ever)
	mu.Unlock()
	if done < total {
		slog.Warn("swap cycle: not every actor could be woken", "missing", total-done)
	}
	msg := fmt.Sprintf("cycle at %d per %s: %d of %d actors resident at least once in %s (%d wakes, %d parks; wake p50/p90/p99 %.0f/%.0f/%.0f ms, park p50/p90/p99 %.0f/%.0f/%.0f ms, errors %d, refusals %d)",
		n, s.cfg.swapEvery, done, total, time.Since(start).Round(time.Second), st.wakes, st.parks, st.wakeP50, st.wakeP90, st.wakeP99, st.parkP50, st.parkP90, st.parkP99, st.errors, st.refusals)
	slog.Info("swap cycle done", "msg", msg)
	return msg
}

// permanentWakeError reports a wake failure that retrying cannot fix: the actor
// is CRASHED, on its way out, or gone. (Measured: a handful of such actors held
// the 8 fill slots for 12 attempts × up to 60 s each and stalled a run's fill
// for 15+ minutes.)
func permanentWakeError(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	for _, s := range []string{"ACTOR_STATE_CRASHED", "ACTOR_STATE_DELETING", "not found", "NotFound"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}
