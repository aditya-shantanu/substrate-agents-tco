package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const pfPort = "18085"

/* ----------------------------- stages ----------------------------- */

type stageStatus int

const (
	stPending stageStatus = iota
	stRunning
	stDone
	stSkipped
	stFailed
)

type stage struct {
	title  string
	script string          // relative to experiment/
	skip   func(*App) bool // true = already in place
	status stageStatus
	dur    time.Duration
}

func newStages() []stage {
	return []stage{
		{title: "GKE cluster + bucket + IAM", script: "10-bootstrap.sh",
			skip: func(a *App) bool {
				st := a.sh(`gcloud container clusters describe "$CLUSTER_NAME" --location "$CLUSTER_LOCATION" --project "$PROJECT_ID" --format='value(status)' 2>/dev/null`)
				return strings.TrimSpace(st) == "RUNNING" && a.shOK(`kubectl get ns >/dev/null 2>&1`)
			}},
		{title: "Substrate control plane", script: "20-install-substrate.sh",
			skip: func(a *App) bool {
				return a.shOK(`[ "$(kubectl -n ate-system get deploy ate-api-server -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" -ge 1 ]`)
			}},
		{title: "Glutton workload + worker pool", script: "30-deploy-workloads.sh",
			skip: func(a *App) bool {
				return a.shOK(`[ "$(kubectl -n benchmark-workloads get workerpool benchmark-ateom -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" = "$WORKER_COUNT" ]`)
			}},
		{title: "Clear previous run artifacts", script: "clean.sh",
			skip: func(a *App) bool { return false }},
		{title: "Auto-suspender + agent fleet", script: "40-deploy-experiment.sh",
			skip: func(a *App) bool { return false }},
	}
}

// sh runs a snippet with the config env and returns stdout.
func (a *App) sh(script string) string {
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), a.cfg.env()...)
	cmd.Dir = a.expDir
	out, _ := cmd.Output()
	return string(out)
}

func (a *App) shOK(script string) bool {
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), a.cfg.env()...)
	cmd.Dir = a.expDir
	return cmd.Run() == nil
}

type lineMsg string
type stageEndMsg struct{ err error }

func (a *App) startStage(i int) tea.Cmd {
	a.stages[i].status = stRunning
	a.stageStart = time.Now()
	script := filepath.Join(a.expDir, a.stages[i].script)
	cmd := exec.Command("bash", script)
	cmd.Dir = a.expDir
	// SKIP_CLEAN: the TUI runs clean.sh as its own visible stage, so
	// 40-deploy-experiment.sh must not clean a second time.
	cmd.Env = append(append(os.Environ(), a.cfg.env()...), "SKIP_CLEAN=true")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return func() tea.Msg { return stageEndMsg{err} }
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return func() tea.Msg { return stageEndMsg{err} }
	}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			a.ch <- lineMsg(sc.Text())
		}
		a.ch <- stageEndMsg{cmd.Wait()}
	}()
	return a.waitCh()
}

func (a *App) waitCh() tea.Cmd {
	return func() tea.Msg { return <-a.ch }
}

/* ----------------------------- live phase ----------------------------- */

type liveSample struct {
	T            int64 `json:"t"`
	WorkersTotal int   `json:"workers_total"`
	Assigned     int   `json:"assigned"`
	ActorsTotal  int   `json:"actors_total"`
	Running      int   `json:"running"`
	Resuming     int   `json:"resuming"`
	Suspending   int   `json:"suspending"`
	Suspended    int   `json:"suspended"`
	Crashed      int   `json:"crashed"`
}

type liveState struct {
	Suspends     int64        `json:"suspends"`
	SuspendErrs  int64        `json:"suspend_errors"`
	SuspendAvgMs float64      `json:"suspend_avg_ms"`
	Wedged       int64        `json:"wedged"`
	Samples      []liveSample `json:"samples"`
}

type liveMsg struct {
	st  liveState
	err error
}
type simMsg struct {
	line     string
	waves    []string
	verdict  string
	refusals int
	errors   int
	done     bool
	failed   bool
}
type tickMsg time.Time
type reportMsg struct {
	text string
	err  error
}

func tick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (a *App) startLive() tea.Cmd {
	a.pf = exec.Command("kubectl", "-n", "agent-sim", "port-forward", "svc/autosuspender", pfPort+":8080")
	a.pf.Stdout, a.pf.Stderr = nil, nil
	_ = a.pf.Start()
	a.liveStart = time.Now()
	return tick()
}

func fetchLive() tea.Msg {
	cl := http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://localhost:" + pfPort + "/state.json")
	if err != nil {
		return liveMsg{err: err}
	}
	defer resp.Body.Close()
	var st liveState
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return liveMsg{err: err}
	}
	return liveMsg{st: st}
}

// pollSim tails the job log for progress/wave lines and checks completion.
func (a *App) pollSim() tea.Msg {
	out := a.sh(`kubectl -n agent-sim logs job/agentsim --tail=400 2>/dev/null`)
	m := simMsg{}
	var waves []string
	inWaves := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(l, `"msg":"progress"`):
			var j map[string]any
			if json.Unmarshal([]byte(l), &j) == nil {
				m.line = fmt.Sprintf("activations %v · wake p50 %.1fs p99 %.1fs · turn p50 %vms · refusals %v · errors %v",
					j["activations"], num(j["wake_p50_ms"])/1000, num(j["wake_p99_ms"])/1000,
					j["session_p50_ms"], j["refusals"], j["errors"])
				m.refusals, m.errors = int(num(j["refusals"])), int(num(j["errors"]))
			}
		case strings.Contains(l, `"msg":"wave result"`):
			var j map[string]any
			if json.Unmarshal([]byte(l), &j) == nil {
				status := sGood.Render("ok")
				if j["failed"] == true {
					status = sBad.Render("FAILED")
				}
				waves = append(waves, fmt.Sprintf("  %3v agents  %4v acts  refus %4v%%  err %4v%%  wake p99 %5.1fs  %s",
					j["active_agents"], j["activations"], j["refusal_pct"], j["error_pct"],
					num(j["wake_p99_ms"])/1000, status))
			}
		case strings.HasPrefix(l, "LOADTEST VERDICT:"):
			m.verdict = l
		case l == "=== loadtest waves ===":
			inWaves = true
			_ = inWaves
		}
	}
	m.waves = waves
	if a.shOK(`kubectl -n agent-sim wait --for=condition=complete job/agentsim --timeout=0s >/dev/null 2>&1`) {
		m.done = true
	} else if a.shOK(`kubectl -n agent-sim wait --for=condition=failed job/agentsim --timeout=0s >/dev/null 2>&1`) {
		m.done, m.failed = true, true
	}
	return m
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

// collect gathers artifacts and renders the final cost card. It brings up
// its own short-lived port-forward: the long-running one may have died when
// pods rolled (kubectl port-forward does not reconnect).
func (a *App) collect() tea.Msg {
	script := fmt.Sprintf(`
set -e
OUT=%q
mkdir -p "$OUT"
kubectl -n agent-sim logs job/agentsim > "$OUT/run.log"
kubectl -n agent-sim port-forward svc/autosuspender 18086:8080 >/dev/null 2>&1 &
PF=$!; trap 'kill $PF 2>/dev/null || true' EXIT
sleep 3
curl -sf --retry 3 localhost:18086/occupancy.csv > "$OUT/occupancy.csv"
curl -sf --retry 3 localhost:18086/metrics > "$OUT/metrics.txt"
MACHINE_TYPE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.labels.node\.kubernetes\.io/instance-type}')
POOL_NODES=$(kubectl -n benchmark-workloads get pods -l ate.dev/worker-pool -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | sort -u | grep -c . || echo 1)
SNAP_GIB=$(gcloud storage du -s "gs://${BUCKET_NAME}/benchmark-workloads/glutton/atespaces/agents-sim/**" 2>/dev/null | awk -v n="${AGENTS}" '$1>0 {printf "%%.3f", $1/n/1073741824}')
python3 "%s/analysis/final_report.py" \
  --run-log "$OUT/run.log" --occupancy "$OUT/occupancy.csv" --metrics "$OUT/metrics.txt" \
  --compress "${COMPRESS}" --machine-type "$MACHINE_TYPE" --pool-workers "${WORKER_COUNT}" \
  --pool-nodes "$POOL_NODES" --snap-gib "${SNAP_GIB:-0.05}" --price-model "${PRICE_MODEL}" \
  | tee "$OUT/report.txt"
`, a.outDir, a.svcDir)
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = a.expDir
	cmd.Env = append(os.Environ(), a.cfg.env()...)
	out, err := cmd.CombinedOutput()
	return reportMsg{text: string(out), err: err}
}

/* ----------------------------- views ----------------------------- */

func (a *App) railView() string {
	var b strings.Builder
	b.WriteString(sSubtle.Render(" Stages") + "\n\n")
	for i := range a.stages {
		g, style := glyphPending, sFaint
		suffix := ""
		switch a.stages[i].status {
		case stRunning:
			g, style = spinnerFrames[a.spin%len(spinnerFrames)], sTitle
		case stDone:
			g, style = glyphDone, sGood
			suffix = sFaint.Render(fmt.Sprintf("  %ds", int(a.stages[i].dur.Seconds())))
		case stSkipped:
			g, style = glyphDone, sGood
			suffix = sFaint.Render("  skipped")
		case stFailed:
			g, style = glyphFail, sBad
		}
		b.WriteString(fmt.Sprintf(" %s %s%s\n", style.Render(g), style.Render(a.stages[i].title), suffix))
	}
	// live + results rows
	g, style := glyphPending, sFaint
	label := "Test running"
	if a.scr == scrRun && a.curStage >= len(a.stages) {
		g, style = spinnerFrames[a.spin%len(spinnerFrames)], sTitle
		label = fmt.Sprintf("Test running  %s", time.Since(a.liveStart).Round(time.Second))
	}
	if a.scr == scrDone {
		g, style, label = glyphDone, sGood, "Test complete"
	}
	b.WriteString(fmt.Sprintf(" %s %s\n", style.Render(g), style.Render(label)))
	g2, s2 := glyphPending, sFaint
	if a.scr == scrDone {
		g2, s2 = glyphDone, sGood
	}
	b.WriteString(fmt.Sprintf(" %s %s\n", s2.Render(g2), s2.Render("$$ per agent per month")))
	return b.String()
}

func (a *App) headerView() string {
	mode := "baseline"
	if a.cfg.loadTest {
		mode = "load-test"
	}
	left := sTitle.Render(" AGENTS TCO ") + sSubtle.Render("· substrate density lab")
	right := strings.Join([]string{
		sSubtle.Render("sandbox: ") + sAccent.Render(a.cfg.sandbox),
		sSubtle.Render("machine: ") + sAccent.Render(a.cfg.machine),
		sSubtle.Render("mode: ") + sAccent.Render(mode),
	}, sFaint.Render("  │  "))
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right + "\n" +
		sFaint.Render(strings.Repeat("─", max(a.width, 1)))
}

func (a *App) liveView() string {
	if len(a.live.Samples) == 0 {
		return sSubtle.Render("waiting for first sample…")
	}
	l := a.live.Samples[len(a.live.Samples)-1]
	awake := l.Running + l.Resuming + l.Suspending
	density := "–"
	if l.Assigned > 0 {
		density = fmt.Sprintf("%.1f:1", float64(l.ActorsTotal)/float64(l.Assigned))
	}

	// A worker pinned by a wedged suspend is not real capacity: report
	// busy/pool net of the wedged ones, and draw them as their own segment.
	wedged := int(a.live.Wedged)
	if wedged > l.Assigned {
		wedged = l.Assigned
	}
	busyOK := l.Assigned - wedged
	usable := l.WorkersTotal - wedged

	workerNote := "1 live agent each"
	if wedged > 0 {
		workerNote = fmt.Sprintf("✗ %d pinned by stuck suspends", wedged)
	}
	tile := func(label, val, note string) string {
		return sPanel.Render(sSubtle.Render(label) + "\n" + sBig.Render(val) + "\n" + sFaint.Render(note))
	}
	workerTile := sPanel.Render(sSubtle.Render("workers busy") + "\n" +
		sBig.Render(fmt.Sprintf("%d / %d", busyOK, usable)) + "\n" +
		map[bool]string{true: sBad.Render(workerNote), false: sFaint.Render(workerNote)}[wedged > 0])
	tiles := lipgloss.JoinHorizontal(lipgloss.Top,
		tile("agents awake", fmt.Sprintf("%d", awake), fmt.Sprintf("%d asleep", l.Suspended)),
		workerTile,
		tile("suspends", fmt.Sprintf("%d", a.live.Suspends), fmt.Sprintf("avg %.1fs · %d errs", a.live.SuspendAvgMs/1000, a.live.SuspendErrs)),
		tile("density now", density, "agents ÷ busy workers"),
	)

	bar := sAccent.Render(strings.Repeat("█", busyOK)) +
		sBad.Render(strings.Repeat("✗", wedged)) +
		sFaint.Render(strings.Repeat("░", max(l.WorkersTotal-l.Assigned, 0)))
	spark := sAccent.Render(sparkline(a.busyHist, l.WorkersTotal, 60))

	var body strings.Builder
	body.WriteString(tiles + "\n\n")
	body.WriteString(" workers  " + bar + "\n")
	body.WriteString(" history  " + spark + sFaint.Render("  (busy workers, last 2 min)") + "\n\n")
	if a.simLine != "" {
		body.WriteString(" " + sSubtle.Render("sim  ") + sBig.Render(a.simLine) + "\n")
	}
	if a.live.Wedged > 0 {
		body.WriteString(" " + sBad.Render(fmt.Sprintf(
			"⚠ %d actor(s) wedged in SUSPENDING — their workers are pinned and excluded from the pool above; the medic deletes+recreates them after the unwedge threshold",
			a.live.Wedged)) + "\n")
	}
	if len(a.waves) > 0 {
		body.WriteString("\n " + sSubtle.Render("waves") + "\n")
		for _, w := range a.waves {
			body.WriteString(w + "\n")
		}
	}
	if a.verdict != "" {
		body.WriteString("\n " + sWarn.Render(a.verdict) + "\n")
	}
	return body.String()
}

func (a *App) viewRun() string {
	main := ""
	if a.curStage < len(a.stages) {
		lines := a.outLines
		if len(lines) > 16 {
			lines = lines[len(lines)-16:]
		}
		main = sPanel.Width(max(a.width-34, 40)).Render(sFaint.Render(strings.Join(lines, "\n")))
	} else {
		main = a.liveView()
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(30).Render(a.railView()), main)
	return a.headerView() + "\n" + body + "\n" +
		sFaint.Render(strings.Repeat("─", max(a.width, 1))) + "\n" +
		hints([][2]string{{"ctrl+c", "abort"}})
}

func (a *App) viewDone() string {
	card := sMoneyPanel.Render(a.report)
	if a.err != nil {
		card = sPanel.Render(sBad.Render("collection error: "+a.err.Error()) + "\n\n" + a.report)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(30).Render(a.railView()), card)

	extra := ""
	hs := [][2]string{{"q", "quit"}}
	if len(a.anomalies) > 0 {
		state := sGood.Render("ON — logs print to your terminal when you quit")
		if !a.captureDiag {
			state = sFaint.Render("off")
		}
		extra = "\n" + sPanel.Render(
			sWarn.Render("⚠ this run had issues: ")+sSubtle.Render(strings.Join(a.anomalies, " · "))+
				"\n"+sSubtle.Render("capture relevant logs from the run/cluster on exit: ")+state) + "\n"
		hs = [][2]string{{"d", "toggle diagnostics on exit"}, {"q", "quit"}}
	}
	return a.headerView() + "\n" + body + "\n" + extra +
		sSubtle.Render(" artifacts: "+a.outDir) + "\n" +
		hints(hs)
}
