package main

// Post-exit diagnostics: when a run saw anomalies (wedged actors, suspend
// errors, request failures, a failed stage), the done screen offers to dump
// the relevant logs from the run and the cluster to stdout after the TUI
// exits (the alt-screen owns the terminal until then). The same bundle is
// written to <results>/diagnostics.txt.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type diagSection struct {
	title string
	cmd   string
}

var diagSections = []diagSection{
	{"autosuspender: suspends, wedges, medic interventions",
		`kubectl -n agent-sim logs deploy/autosuspender --tail=500 2>/dev/null | grep -E '"level":"(WARN|ERROR)"|medic|wedged' | tail -40`},
	{"agentsim: warnings, verdicts, final summary",
		`kubectl -n agent-sim logs job/agentsim --tail=2000 2>/dev/null | grep -vE '"msg":"progress"' | grep -E '"level":"(WARN|ERROR)"|VERDICT|summary|=== ' | tail -40`},
	{"ate-api-server: failed RPCs (deduplicated)",
		`for p in $(kubectl -n ate-system get pods -l app=ate-api-server -o name 2>/dev/null); do kubectl -n ate-system logs $p --tail=1500 2>/dev/null; done | grep '"err":"' | grep -oE '"method":"[^"]*","req":.{0,90}|"err":"[^"]{0,160}' | paste - - 2>/dev/null | sort | uniq -c | sort -rn | head -25`},
	{"atelet (node agents): errors",
		`for p in $(kubectl -n ate-system get pods -o name 2>/dev/null | grep atelet); do kubectl -n ate-system logs $p --tail=800 2>/dev/null; done | grep -iE '"level":"(warn|error)"|failed' | tail -30`},
	{"actor states",
		`kubectl -n agent-sim exec deploy/autosuspender -- true 2>/dev/null; curl -sf --max-time 5 localhost:` + pfPort + `/metrics 2>/dev/null | grep autosuspend || echo "(autosuspender metrics unreachable — port-forward closed)"`},
	{"kubernetes warning events (all namespaces)",
		`kubectl get events -A --field-selector type=Warning --sort-by=.lastTimestamp 2>/dev/null | tail -25`},
	{"node pressure",
		`kubectl top nodes 2>/dev/null; kubectl get nodes 2>/dev/null`},
}

// dumpDiagnostics runs each section with a timeout, prints to stdout and
// mirrors into <outDir>/diagnostics.txt.
func dumpDiagnostics(a *App) {
	var mirror strings.Builder
	emit := func(s string) {
		fmt.Println(s)
		mirror.WriteString(s + "\n")
	}

	emit("")
	emit("════════ diagnostics from this run (requested on exit) ════════")
	emit("anomalies seen: " + strings.Join(a.anomalies, " · "))
	for _, s := range diagSections {
		emit("")
		emit("── " + s.title + " ──")
		cmd := exec.Command("bash", "-c", s.cmd)
		cmd.Env = append(os.Environ(), a.cfg.env()...)
		done := make(chan []byte, 1)
		go func() {
			out, _ := cmd.CombinedOutput()
			done <- out
		}()
		select {
		case out := <-done:
			text := strings.TrimSpace(string(out))
			if text == "" {
				text = "(nothing)"
			}
			emit(text)
		case <-time.After(25 * time.Second):
			_ = cmd.Process.Kill()
			emit("(timed out)")
		}
	}
	emit("")
	if a.outDir != "" {
		path := filepath.Join(a.outDir, "diagnostics.txt")
		if err := os.WriteFile(path, []byte(mirror.String()), 0o644); err == nil {
			fmt.Println("saved to " + path)
		}
	}
}

// noteAnomaly records an anomaly for the done screen. key dedups (counts in
// the display text change tick to tick; the latest display wins).
func (a *App) noteAnomaly(key, display string) {
	if a.anomalyIdx == nil {
		a.anomalyIdx = map[string]int{}
	}
	if i, ok := a.anomalyIdx[key]; ok {
		a.anomalies[i] = display
		return
	}
	a.anomalyIdx[key] = len(a.anomalies)
	a.anomalies = append(a.anomalies, display)
	a.captureDiag = true // default ON once something went wrong
}
