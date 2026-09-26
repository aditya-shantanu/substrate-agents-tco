// tco is the one-command terminal UI for the Substrate agent-density lab:
// configure a run (machine type, gVisor vs microVM, fleet size, baseline or
// load-test), watch the cluster come up stage by stage, watch the live test
// (awake/asleep agents, worker occupancy, suspend/resume latency,
// throughput), and end on the number that matters: cost per agent per month,
// measured.
//
// It is presentation over the same scripts in service/experiment/ — anything
// the TUI does can be reproduced by hand with ./run.sh.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type screenID int

const (
	scrConfig screenID = iota
	scrHelp
	scrRun
	scrDone
)

type App struct {
	scr    screenID
	cfg    config
	cursor int

	width, height int
	spin          int

	expDir, svcDir, outDir string
	logF                   *os.File

	stages     []stage
	curStage   int
	stageStart time.Time
	outLines   []string
	ch         chan tea.Msg

	pf        *exec.Cmd
	liveStart time.Time
	live      liveState
	busyHist  []int
	simLine   string
	waves     []string
	verdict   string
	simTick   int
	liveErrs  int

	cluster   *clusterInfo
	prefilled bool

	anomalies   []string
	anomalyIdx  map[string]int
	captureDiag bool

	report string
	err    error
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "screenshots" {
		out := ""
		if len(os.Args) > 2 {
			out = os.Args[2]
		}
		runUIShots(out)
		return
	}
	svcDir, err := findServiceDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	a := &App{
		cfg:    defaultConfig(),
		expDir: filepath.Join(svcDir, "experiment"),
		svcDir: svcDir,
		stages: newStages(),
		ch:     make(chan tea.Msg, 256),
	}
	p := tea.NewProgram(a, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// The alt-screen owns the terminal while the TUI runs; the diagnostics
	// bundle the user opted into prints only now, after teardown.
	if a.captureDiag && len(a.anomalies) > 0 {
		fmt.Fprintln(os.Stderr, "capturing diagnostics from the cluster…")
		dumpDiagnostics(a)
	}
}

// findServiceDir locates the service module root from cwd or the binary's
// source layout, so `go run ./cmd/tco` works from the service dir and the
// built binary works from the repo root.
func findServiceDir() (string, error) {
	cwd, _ := os.Getwd()
	for _, c := range []string{cwd, filepath.Join(cwd, "service")} {
		if _, err := os.Stat(filepath.Join(c, "experiment", "run.sh")); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("run from the repo root or service/ (experiment/run.sh not found)")
}

func (a *App) Init() tea.Cmd { return tea.Batch(spinTick(), probeCluster(a.cfg)) }

type spinMsg struct{}

func spinTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinMsg{} })
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		return a, nil

	case spinMsg:
		a.spin++
		return a, spinTick()

	case tea.KeyMsg:
		return a.onKey(m)

	case clusterInfoMsg:
		if m.probedKey == a.cfg.probeKey() { // drop stale probes
			ci := clusterInfo(m)
			a.cluster = &ci
			a.prefillFromCluster(ci)
		}
		return a, nil

	case lineMsg:
		a.outLines = append(a.outLines, string(m))
		if a.logF != nil {
			fmt.Fprintln(a.logF, string(m))
		}
		return a, a.waitCh()

	case stageEndMsg:
		if m.err != nil {
			a.stages[a.curStage].status = stFailed
			a.noteAnomaly("stage-fail", "stage failed: "+a.stages[a.curStage].title)
			a.err = fmt.Errorf("stage %q failed: %w (see %s/ui.log)", a.stages[a.curStage].title, m.err, a.outDir)
			a.report = lastLines(a.outLines, 20)
			a.scr = scrDone
			return a, nil
		}
		a.stages[a.curStage].status = stDone
		a.stages[a.curStage].dur = time.Since(a.stageStart)
		a.curStage++
		return a, a.advance()

	case tickMsg:
		cmds := []tea.Cmd{tick(), func() tea.Msg { return fetchLive() }}
		a.simTick++
		if a.simTick%5 == 0 {
			cmds = append(cmds, func() tea.Msg { return a.pollSim() })
		}
		return a, tea.Batch(cmds...)

	case liveMsg:
		if m.err == nil {
			a.liveErrs = 0
			a.live = m.st
			if m.st.Wedged > 0 {
				a.noteAnomaly("wedged", fmt.Sprintf("actors wedged in SUSPENDING (workers pinned; count %d)", m.st.Wedged))
			}
			if m.st.SuspendErrs > 0 {
				a.noteAnomaly("suspend-errs", fmt.Sprintf("%d suspend RPC errors", m.st.SuspendErrs))
			}
			if n := len(m.st.Samples); n > 0 {
				a.busyHist = append(a.busyHist, m.st.Samples[n-1].Assigned)
				if len(a.busyHist) > 120 {
					a.busyHist = a.busyHist[len(a.busyHist)-120:]
				}
			}
		} else {
			// kubectl port-forward dies when pods roll; restart it after a
			// few consecutive failures.
			a.liveErrs++
			if a.liveErrs >= 3 {
				a.liveErrs = 0
				if a.pf != nil && a.pf.Process != nil {
					_ = a.pf.Process.Kill()
				}
				a.pf = exec.Command("kubectl", "-n", "agent-sim", "port-forward", "svc/autosuspender", pfPort+":8080")
				_ = a.pf.Start()
			}
		}
		return a, nil

	case simMsg:
		if m.line != "" {
			a.simLine = m.line
		}
		if m.refusals > 0 {
			a.noteAnomaly("refusals", fmt.Sprintf("%d router refusals (503/504)", m.refusals))
		}
		if m.errors > 0 {
			a.noteAnomaly("errors", fmt.Sprintf("%d request errors", m.errors))
		}
		if m.failed {
			a.noteAnomaly("job-failed", "agentsim job failed")
		}
		a.waves = m.waves
		if m.verdict != "" {
			a.verdict = m.verdict
		}
		if m.done {
			if a.pf != nil && a.pf.Process != nil {
				defer func() { _ = a.pf.Process.Kill() }()
			}
			_ = m.failed // failed jobs still get collected; the card shows errors
			return a, func() tea.Msg { return a.collect() }
		}
		return a, nil

	case reportMsg:
		a.report, a.err = m.text, m.err
		if a.pf != nil && a.pf.Process != nil {
			_ = a.pf.Process.Kill()
		}
		a.scr = scrDone
		return a, nil
	}
	return a, nil
}

func (a *App) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		if a.pf != nil && a.pf.Process != nil {
			_ = a.pf.Process.Kill()
		}
		return a, tea.Quit
	case "q":
		// 'q' quits everywhere except mid-run and while typing in a text field.
		if a.scr == scrDone || (a.scr == scrConfig && fields[a.cursor].text == nil) {
			return a, tea.Quit
		}
	case "d":
		if a.scr == scrDone && len(a.anomalies) > 0 {
			a.captureDiag = !a.captureDiag
			return a, nil
		}
	}
	if a.scr == scrHelp {
		switch k.String() {
		case "?", "esc", "q", "enter":
			a.scr = scrConfig
		}
		return a, nil
	}
	if a.scr != scrConfig {
		return a, nil
	}
	// '?' opens the knob explainer (except while typing in a text field).
	if k.String() == "?" && fields[a.cursor].text == nil {
		a.scr = scrHelp
		return a, nil
	}
	// Free-text fields (project, zone, …): type to edit, backspace to erase.
	if t := fields[a.cursor].text; t != nil {
		switch k.Type {
		case tea.KeyRunes, tea.KeySpace:
			*t(&a.cfg) += string(k.Runes)
			return a, nil
		case tea.KeyBackspace:
			s := t(&a.cfg)
			if len(*s) > 0 {
				*s = (*s)[:len(*s)-1]
			}
			return a, nil
		}
	}
	// Leaving one of the cluster-identity fields re-probes GCP; 'r' does it
	// on demand.
	reprobe := func() tea.Cmd {
		if a.cluster == nil || a.cluster.probedKey != a.cfg.probeKey() {
			return probeCluster(a.cfg)
		}
		return nil
	}
	switch k.String() {
	case "r":
		if fields[a.cursor].text == nil { // 'r' types into text fields
			a.cluster = nil
			return a, probeCluster(a.cfg)
		}
	case "up", "k":
		for {
			a.cursor = (a.cursor - 1 + len(fields)) % len(fields)
			if f := fields[a.cursor]; f.dim == nil || !f.dim(&a.cfg) {
				break
			}
		}
		return a, reprobe()
	case "down", "j", "tab":
		for {
			a.cursor = (a.cursor + 1) % len(fields)
			if f := fields[a.cursor]; f.dim == nil || !f.dim(&a.cfg) {
				break
			}
		}
		return a, reprobe()
	case "left", "h":
		fields[a.cursor].cycle(&a.cfg, -1)
	case "right", "l", " ":
		fields[a.cursor].cycle(&a.cfg, +1)
	case "enter":
		return a.startRun()
	}
	return a, nil
}

func (a *App) startRun() (tea.Model, tea.Cmd) {
	a.outDir = filepath.Join(a.expDir, "results", time.Now().Format("20060102-150405"))
	_ = os.MkdirAll(a.outDir, 0o755)
	a.logF, _ = os.Create(filepath.Join(a.outDir, "ui.log"))
	a.scr = scrRun
	a.curStage = 0
	return a, a.advance()
}

// advance runs the next non-skipped stage, or transitions to the live phase.
func (a *App) advance() tea.Cmd {
	for a.curStage < len(a.stages) {
		st := &a.stages[a.curStage]
		if st.skip != nil && st.skip(a) {
			st.status = stSkipped
			a.curStage++
			continue
		}
		a.outLines = nil
		return a.startStage(a.curStage)
	}
	return a.startLive()
}

func (a *App) View() string {
	switch a.scr {
	case scrConfig:
		return a.viewConfig()
	case scrHelp:
		return a.viewHelp()
	case scrRun:
		return a.viewRun()
	default:
		return a.viewDone()
	}
}

func lastLines(lines []string, n int) string {
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
