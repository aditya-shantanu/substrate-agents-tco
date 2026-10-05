package main

// One-ping workload: a faithful re-creation of the GluttonUser loop that
// substrate's benchmarking suite (and the Sep 2026 Prow 200K run) drives
// — internal/benchmarking/boomer/glutton/lifecycle.go upstream. A virtual
// user owns --ping-actors-per-user actors and serves them round-robin, one
// at a time: wake the actor with a ping through the router (implicit
// resume), hold it for --ping-live (default 0: suspend right after the
// ping), SuspendActor, then sleep --ping-wait before touching the next
// actor. The agent does no other work and fills no memory ("nomem"), so a
// wake is the bare restore of an idle sandbox. Overcommit is therefore a
// construction: at most one actor per user is awake, so N actors per user
// is N× by design; what the run measures is the switch cost and the
// control plane under that churn. Neither the wait nor the live window is
// time-compressed.

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
)

func (s *sim) runPing(ctx context.Context, deadline time.Time) {
	n := s.cfg.pingActorsPerUser
	var wg sync.WaitGroup
	for start := 0; start < len(s.ready); start += n {
		ids := s.ready[start:min(start+n, len(s.ready))]
		wg.Add(1)
		go func(vu int, ids []int) {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(vu), 0x9e11))
			// Stagger users across one wait window so cycles don't align.
			time.Sleep(time.Duration(rng.Float64() * float64(s.cfg.pingWait)))
			for i := 0; ; i = (i + 1) % len(ids) {
				if time.Now().After(deadline) {
					return
				}
				s.pingCycle(ctx, ids[i], rng)
				time.Sleep(s.cfg.pingWait)
			}
		}(start/n, ids)
	}
	wg.Wait()
}

// pingCycle is one resume/ping/suspend cycle of one actor: a result row of
// kind "ping" whose first_req_ms is the wake, step_ms the live window
// actually held, and suspend_ms the SuspendActor call.
func (s *sim) pingCycle(ctx context.Context, id int, rng *rand.Rand) {
	name := actorName(id)
	r := result{unixMs: time.Now().UnixMilli(), agent: id, kind: "ping"}
	r.wasSuspended = s.actorSuspended(ctx, name)
	s.touch(name, "begin")

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
		slog.Warn("ping wake failed", "actor", name, "err", err)
	} else if s.cfg.pingLive > 0 {
		// Live window, as upstream: more pings at 200 ms–1 s gaps until it
		// closes, then sleep whatever remains so the actor stays awake for
		// the whole window.
		liveStart := time.Now()
		dl := liveStart.Add(s.cfg.pingLive)
		for {
			gap := 200*time.Millisecond + time.Duration(rng.Float64()*float64(800*time.Millisecond))
			if time.Now().Add(gap).After(dl) {
				break
			}
			time.Sleep(gap)
			if _, err := s.post(ctx, name, "/ping", nil); err != nil {
				r.errors++
			}
			r.pings++
		}
		if rem := time.Until(dl); rem > 0 {
			time.Sleep(rem)
		}
		r.stepMs = float64(time.Since(liveStart).Microseconds()) / 1000
	}
	s.touch(name, "end")

	if ms, err := s.suspendNow(ctx, name); err != nil {
		r.errors++
		slog.Warn("ping suspend failed", "actor", name, "err", err)
	} else {
		r.suspendMs = ms
	}
	s.mu.Lock()
	s.results = append(s.results, r)
	s.mu.Unlock()
}

// isWake says whether a result row's first request was a wake from
// suspension: ping-only activations always are; script steps and one-ping
// cycles are when the actor was seen suspended (or in an unknown state).
func isWake(r result) bool {
	return r.kind == "wake" || ((r.kind == "step" || r.kind == "ping") && r.wasSuspended != 0)
}
