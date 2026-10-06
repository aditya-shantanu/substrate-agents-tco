package main

// Cold starts: an actor's first life. Substrate has no per-actor boot — a
// new actor is created logically and its first ResumeActor restores the
// template's golden snapshot onto a free worker — so "cold start" here is
// measured the way the upstream spawn benchmark measures ActorTimeToReady:
// CreateActor, then the first successful ResumeActor (the golden restore),
// then the first ping answered through the router. Every setup records one
// row; --cold-start-only stops after printing them.

import (
	"fmt"
	"time"
)

type coldStart struct {
	unixMs         int64
	agent          int
	createMs       float64 // CreateActor call
	resumeMs       float64 // the successful ResumeActor call (golden-snapshot restore)
	resumeAttempts int     // ResumeActor calls incl. ResourceExhausted/Aborted retries
	pingMs         float64 // from resume success to the first answered ping (incl. retries)
	pingAttempts   int     // failed pings before the first answer
	readyMs        float64 // CreateActor start → first answered ping
	pingErr        string  // set when no ping ever succeeded
}

func msSince(t time.Time) float64 { return float64(time.Since(t).Microseconds()) / 1000 }

// printColdStarts writes the percentiles and the per-actor CSV block that
// analysis/cold_report.py reads.
func (s *sim) printColdStarts() {
	s.mu.Lock()
	rows := append([]coldStart(nil), s.colds...)
	s.mu.Unlock()
	if len(rows) == 0 {
		return
	}
	var create, resume, ping, ready []float64
	failed := 0
	for _, r := range rows {
		if r.pingErr != "" {
			failed++
			continue
		}
		create = append(create, r.createMs)
		resume = append(resume, r.resumeMs)
		ping = append(ping, r.pingMs)
		ready = append(ready, r.readyMs)
	}
	for _, v := range [][]float64{create, resume, ping, ready} {
		sortFloats(v)
	}
	fmt.Println("=== cold starts ===")
	fmt.Printf("n=%d failed=%d\n", len(rows), failed)
	fmt.Printf("create ms      p50=%.0f p90=%.0f p99=%.0f max=%.0f\n", q(create, .5), q(create, .9), q(create, .99), q(create, 1))
	fmt.Printf("resume ms      p50=%.0f p90=%.0f p99=%.0f max=%.0f   (first ResumeActor: golden-snapshot restore)\n", q(resume, .5), q(resume, .9), q(resume, .99), q(resume, 1))
	fmt.Printf("first ping ms  p50=%.0f p90=%.0f p99=%.0f max=%.0f\n", q(ping, .5), q(ping, .9), q(ping, .99), q(ping, 1))
	fmt.Printf("ready ms       p50=%.0f p90=%.0f p99=%.0f max=%.0f   (CreateActor → first answered ping)\n", q(ready, .5), q(ready, .9), q(ready, .99), q(ready, 1))
	fmt.Println("unix_ms,agent,create_ms,resume_ms,resume_attempts,first_ping_ms,ping_attempts,ready_ms,ping_err")
	for _, r := range rows {
		fmt.Printf("%d,%d,%.1f,%.1f,%d,%.1f,%d,%.1f,%q\n", r.unixMs, r.agent, r.createMs, r.resumeMs,
			r.resumeAttempts, r.pingMs, r.pingAttempts, r.readyMs, r.pingErr)
	}
	fmt.Println("=== end cold starts ===")
}
