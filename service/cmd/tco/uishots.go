// uishots.go renders the README screenshots for the tco TUI, the same
// way substrate-gke's installer does: compose each screen's state directly,
// write one ANSI capture per screen, and let utils/screenshots.sh turn the
// captures into SVGs with freeze. No cluster or gcloud needed.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func runUIShots(out string) {
	if out == "" {
		out = "../docs/screenshots"
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fatal(err)
	}
	// Captures are piped, not shown on a terminal; without this lipgloss
	// would detect no TTY and strip every color from the frames.
	lipgloss.SetColorProfile(termenv.TrueColor)

	// ---- config screen: probed cluster, cursor on the machine dropdown ----
	a := &App{cfg: defaultConfig(), stages: newStages(), width: 110, height: 40}
	a.cfg.project = "my-gcp-project"
	a.cfg.subsRepo = "~/repos/substrate"
	a.cursor = 5 // Machine type
	a.cluster = &clusterInfo{
		probedKey: a.cfg.probeKey(), exists: true, status: "RUNNING",
		version: "1.36.4", nodes: 2,
		pools: []poolInfo{{Name: "substrate-node-pool", Machine: "c3-standard-4", Count: 2, Status: "RUNNING"}},
	}
	shot(out, "config", a.viewConfig())

	// ---- help screen ----
	shot(out, "help", a.viewHelp())

	// ---- live test screen: hot pool, one wedged worker, medic on duty ----
	live := &App{cfg: a.cfg, stages: newStages(), width: 110, height: 40}
	live.scr = scrRun
	for i := range live.stages {
		live.stages[i].status = stSkipped
	}
	live.stages[3].status = stDone
	live.stages[3].dur = 12 * time.Second
	live.stages[4].status = stDone
	live.stages[4].dur = 41 * time.Second
	live.curStage = len(live.stages)
	live.liveStart = time.Now().Add(-14 * time.Minute)
	now := time.Now().UnixMilli()
	var samples []liveSample
	saw := []int{5, 6, 8, 7, 6, 8, 9, 7, 6, 7, 8, 9, 8, 7, 8}
	for i, b := range saw {
		samples = append(samples, liveSample{
			T: now - int64((len(saw)-i)*5000), WorkersTotal: 10, Assigned: b,
			ActorsTotal: 120, Running: b - 2, Resuming: 1, Suspending: 1,
			Suspended: 120 - b - 1,
		})
		live.busyHist = append(live.busyHist, b)
	}
	live.live = liveState{
		Suspends: 641, SuspendAvgMs: 2480, SuspendErrs: 1, Wedged: 1,
		Samples: samples,
	}
	live.simLine = "activations 512 · wake p50 1.4s p99 3.9s · turn p50 12ms · refusals 3 · errors 1"
	shot(out, "live", live.viewRun())

	// ---- results screen: the real measured card + diagnostics offer ----
	done := &App{cfg: a.cfg, stages: live.stages, width: 110, height: 46}
	done.scr = scrDone
	done.curStage = len(done.stages)
	done.outDir = "service/experiment/results/20260925-2036"
	if card, err := os.ReadFile(filepath.Join("..", "docs", "runs", "2026-09-25-baseline-x6", "report.txt")); err == nil {
		done.report = string(card)
	} else {
		done.report = "COST PER AGENT PER MONTH ≈ $1.21"
	}
	done.noteAnomaly("wedged", "actors wedged in SUSPENDING (workers pinned; count 2)")
	done.noteAnomaly("refusals", "25 router refusals (503/504)")
	shot(out, "results", done.viewDone())
}

func shot(dir, name, view string) {
	path := filepath.Join(dir, name+".ans")
	if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("wrote", path)
}


func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
