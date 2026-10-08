package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adityashantanu/substrate-agents-tco/service/internal/agentscript"
	"github.com/charmbracelet/lipgloss"
)

// The config screen: arrow-key form with prefilled choices. Every field maps
// to an env var consumed by experiment/*.sh, so the TUI is presentation over
// the same scripts people can run by hand.

type config struct {
	// infra (pre-filled, editable text)
	project  string
	zone     string
	cluster  string
	subsRepo string

	sandbox   string // gvisor | microvm
	machine   string
	nodes     int
	workers   int
	agents    int
	compress  int
	duration  string
	idle      string
	price     string
	peak      string
	loadTest  bool
	waveStart int
	waveStep  int
	waveIntvl string

	// workload: personal-agent turns, or the coding-session script
	workload      string // personal | coding-session
	tasksPerDay   int    // script mode: tasks per agent-day
	thinkScale    float64 // script mode: × every LLM think gap (fractional for one-day scripts)
	scriptSuspend string // script mode: driver | idle
}

// scriptSums caches the shape of each built-in script, for the load
// estimate and cost preview in script mode.
var scriptSums = map[string]agentscript.Summary{}

func scriptSum(name string) agentscript.Summary {
	if sum, ok := scriptSums[name]; ok {
		return sum
	}
	s, err := agentscript.Load(name)
	if err != nil {
		panic(err)
	}
	scriptSums[name] = agentscript.Summarize(s)
	return scriptSums[name]
}

// scripted says whether the workload is an agent-session script.
func (c config) scripted() bool { return c.workload == "coding-session" || c.workload == "nano-personal-agent" || c.workload == "personal-assistant" }
func (c config) coding() bool   { return c.workload == "coding-session" }
func (c config) assistant() bool { return c.workload == "personal-assistant" }

// scriptTasksPerDay is laps per agent-day: the coding task count the user
// picks, or exactly one for the personal-assistant's one-day script.
func (c config) scriptTasksPerDay() float64 {
	if c.assistant() {
		return 1
	}
	return float64(c.tasksPerDay)
}
func (c config) ping() bool   { return c.workload == "ping" }

// pingShape is the one-ping loop in model units: lib.sh defaults of 20
// actors per user and a 10 s wait, a cycle of suspend + resume + a ping.
const pingActorsPerUser, pingWaitSec = 20.0, 10.0

func (c config) pingCycleSec() float64 { return estSuspend + estResume + 0.3 }

func defaultConfig() config {
	proj := strings.TrimSpace(shellOut("gcloud config get-value project 2>/dev/null"))
	if proj == "" || proj == "(unset)" {
		proj = "my-project"
	}
	home, _ := os.UserHomeDir()
	return config{
		project: proj, zone: "us-central1-c", cluster: "agents-tco",
		subsRepo: filepath.Join(home, "repos", "substrate"),
		sandbox:  "gvisor", machine: "c3-standard-4", nodes: 2, workers: 10,
		agents: 120, compress: 6, duration: "30m", idle: "2s", price: "cud3",
		peak:     "×2 average",
		loadTest: false, waveStart: 10, waveStep: 10, waveIntvl: "3m",
		// The coding-agent script is the default workload (Aditya, 2026-10-05);
		// "personal" keeps the OpenClaw-shaped profile the $1.21 baseline used.
		workload: "coding-session", tasksPerDay: 8, thinkScale: 4, scriptSuspend: "driver",
	}
}

// peakParams parses the "Provision for peaks" choice into (model, value)
// for the final report: mult+multiplier, or herd+fraction.
func (c config) peakParams() (string, float64) {
	if strings.HasPrefix(c.peak, "herd") {
		var pct float64
		fmt.Sscanf(c.peak, "herd %f%%", &pct)
		return "herd", pct / 100
	}
	var mult float64
	fmt.Sscanf(c.peak, "×%f average", &mult)
	if mult == 0 {
		mult = 2
	}
	return "mult", mult
}

func shellOut(cmd string) string {
	out, _ := exec.Command("bash", "-c", cmd).Output()
	return string(out)
}

// dayIn renders how long a simulated day lasts at compression k.
func dayIn(k int) string {
	if k <= 0 {
		k = 1
	}
	h := 24.0 / float64(k)
	if h >= 1 {
		return fmt.Sprintf("%.0fh", h)
	}
	return fmt.Sprintf("%.0f min", h*60)
}

func regionOf(zone string) string {
	if i := strings.LastIndex(zone, "-"); i > 0 {
		return zone[:i]
	}
	return zone
}

func (c config) env() []string {
	lt := "false"
	if c.loadTest {
		lt = "true"
	}
	env := []string{
		"PROJECT_ID=" + c.project,
		"CLUSTER_LOCATION=" + c.zone,
		"GCE_REGION=" + regionOf(c.zone),
		"CLUSTER_NAME=" + c.cluster,
		"SUBSTRATE_REPO=" + c.subsRepo,
		"BUCKET_NAME=snapshot-" + c.cluster + "-" + c.project,
		"KO_DOCKER_REPO=gcr.io/" + c.project + "/ate-images",
		"SANDBOX_CLASS=" + c.sandbox,
		"GVISOR_NODE_MACHINE_TYPE=" + c.machine,
		fmt.Sprintf("NODE_COUNT=%d", c.nodes),
		fmt.Sprintf("WORKER_COUNT=%d", c.workers),
		fmt.Sprintf("AGENTS=%d", c.agents),
		fmt.Sprintf("COMPRESS=%d", c.compress),
		"DURATION=" + c.duration,
		"IDLE_TIMEOUT=" + c.idle,
		"PRICE_MODEL=" + c.price,
		func() string { m, _ := c.peakParams(); return "PEAK_MODEL=" + m }(),
		func() string { _, v := c.peakParams(); return fmt.Sprintf("PEAK_VALUE=%g", v) }(),
		"LOAD_TEST=" + lt,
		fmt.Sprintf("WAVE_START=%d", c.waveStart),
		fmt.Sprintf("WAVE_STEP=%d", c.waveStep),
		"WAVE_INTERVAL=" + c.waveIntvl,
		"WORKLOAD=" + c.workload,
		fmt.Sprintf("THINK_SCALE=%g", c.thinkScale),
		"SCRIPT_SUSPEND=" + c.scriptSuspend,
	}
	if c.coding() {
		// tasks per day; no heartbeat-style check-ins for a coding agent.
		// lib.sh also raises ACTOR_MEMORY to the script's 1Gi floor.
		env = append(env, fmt.Sprintf("SESSIONS_PER_DAY=%d", c.tasksPerDay), "WAKES_PER_DAY=0")
	}
	// personal-assistant: lib.sh defaults apply (one lap per day, played
	// back-to-back, 1536Mi actors); only the think scale is ours to send.
	return env
}

func (c config) machineChoices() []machine {
	if c.sandbox != "microvm" {
		return machines
	}
	var out []machine
	for _, m := range machines {
		if m.nested {
			out = append(out, m)
		}
	}
	return out
}

/* ------- feasibility + cost preview (the model, inline) ------- */

const (
	wActsPerDay = 43.0   // 3 sessions + 40 check-ins (agentsim defaults)
	wLiveSecDay = 2040.0 // 3×8min + 40×15s
	estSuspend  = 2.46 // 2026-09-25 baseline run (MODEL.md Appendix B)
	estResume   = 1.43
)

func parseSec(s string) float64 {
	var v float64
	var u string
	fmt.Sscanf(s, "%f%s", &v, &u)
	switch u {
	case "m":
		return v * 60
	case "h":
		return v * 3600
	}
	return v
}

// demand estimates mean busy workers during the compressed test.
// demandCalibration anchors the estimate to reality: the 2026-09-25 baseline
// (50 agents, ×6, 10 workers) measured mean 3.38 busy vs 2.27 predicted with
// the measured switch times (2.46s/1.43s).
const demandCalibration = 1.49

// scriptShape is the chosen script in model units: steps per lap,
// estimated work per step (burn_cpu wall + resident dwell + ~1 s of I/O),
// and the scaled think gap per step.
func (c config) scriptShape() (steps, workSec, gapSec float64) {
	sum := scriptSum(c.workload)
	steps = float64(sum.Steps)
	workSec = (sum.BurnWall.Seconds()+sum.Dwell.Seconds())/steps + 1.0
	gapSec = sum.Think.Seconds() * c.thinkScale / steps
	return
}

func (c config) demand() float64 {
	if c.ping() {
		// One user per 20 actors, at most one awake per user; a user holds a
		// worker for the cycle out of every cycle + wait. Not compressed.
		cycle := c.pingCycleSec()
		return float64(c.agents) / pingActorsPerUser * cycle / (cycle + pingWaitSec) * demandCalibration
	}
	if c.scripted() {
		// Tasks arrive Poisson at the compressed rate (the one-day assistant
		// script plays back-to-back: one lap per scaled day); think gaps and
		// switch times do not compress. Driver mode frees the worker for
		// every gap; idle mode holds it until the idle wait elapses.
		steps, work, gap := c.scriptShape()
		tasksPerSec := float64(c.agents) * c.scriptTasksPerDay() * float64(c.compress) / 86400
		if c.assistant() {
			tasksPerSec = float64(c.agents) / (scriptSum(c.workload).Think.Seconds() * c.thinkScale) // laps per second at this think scale
		}
		hold := steps * (work + estSuspend + estResume)
		if c.scriptSuspend == "idle" {
			hold += steps * math.Min(gap, parseSec(c.idle)+estSuspend)
		}
		return tasksPerSec * hold * demandCalibration
	}
	arrivalPerSec := float64(c.agents) * wActsPerDay * float64(c.compress) / 86400
	holdSec := parseSec(c.idle) + estSuspend + estResume + 4 // +work per wake
	sessShare := 3.0 / wActsPerDay
	sessExtra := (8*60/float64(c.compress) - 4) * sessShare // sessions dwell longer
	return arrivalPerSec * (holdSec + sessExtra) * demandCalibration
}

// loadScore is the 0-10 "how hard will this run push the pool" number shown
// on the config screen (10 = pool fully busy on average).
func (c config) loadScore() float64 {
	return c.demand() / float64(c.workers) * 10
}

// costPreview runs the Phase 1 model at REAL duty cycle with measured
// switch estimates on the chosen machine, honoring the chosen peak lens.
func (c config) costPreview() (perAgent, workerMo float64, agentsPerWorker float64) {
	m := machineByName(c.machine)
	occ := (wLiveSecDay + wActsPerDay*(10+estSuspend+estResume)) / 86400 // 10s real-world idle wait
	if c.ping() {
		cycle := c.pingCycleSec()
		occ = cycle / (pingActorsPerUser * (cycle + pingWaitSec)) // worker held for the cycle, once per period
	}
	if c.scripted() {
		// per step: the work, plus (driver) suspend+resume with no wait, or
		// (idle) the run's idle wait before the suspend as well.
		steps, work, _ := c.scriptShape()
		wait := 0.0
		if c.scriptSuspend == "idle" {
			wait = parseSec(c.idle)
		}
		occ = c.scriptTasksPerDay() * steps * (work + wait + estSuspend + estResume) / 86400
	}
	model, v := c.peakParams()
	var n float64
	if model == "herd" {
		n = 0.70 / (v + (1-v)*occ)
	} else {
		n = 0.70 / (occ * v) // utilization 0.7
	}
	wpn := float64(c.workers) / float64(c.nodes)
	workerMo = m.hourly(c.price) * 730 / wpn
	return workerMo/n + 0.001 + 0.05, workerMo, n
}

/* ------------------------- the form ------------------------- */

type field struct {
	label string
	get   func(c *config) string
	cycle func(c *config, dir int)
	dim   func(c *config) bool
	text  func(c *config) *string // non-nil = free-text field (type to edit)
}

func cycleStr(cur string, opts []string, dir int) string {
	for i, o := range opts {
		if o == cur {
			return opts[(i+dir+len(opts))%len(opts)]
		}
	}
	return opts[0]
}

func cycleFloat(cur float64, opts []float64, dir int) float64 {
	for i, o := range opts {
		if o == cur {
			return opts[(i+dir+len(opts))%len(opts)]
		}
	}
	return opts[0]
}

func cycleInt(cur int, opts []int, dir int) int {
	for i, o := range opts {
		if o == cur {
			return opts[(i+dir+len(opts))%len(opts)]
		}
	}
	return opts[0]
}

func textField(label string, get func(c *config) *string) field {
	return field{
		label: label,
		get:   func(c *config) string { return *get(c) },
		cycle: func(c *config, d int) {},
		text:  get,
	}
}

var fields = []field{
	textField("GCP project", func(c *config) *string { return &c.project }),
	textField("Zone", func(c *config) *string { return &c.zone }),
	textField("Cluster name", func(c *config) *string { return &c.cluster }),
	textField("Substrate repo", func(c *config) *string { return &c.subsRepo }),
	{"Sandbox", func(c *config) string { return c.sandbox },
		func(c *config, d int) {
			c.sandbox = cycleStr(c.sandbox, []string{"gvisor", "microvm"}, d)
			if c.sandbox == "microvm" && !machineByName(c.machine).nested {
				c.machine = c.machineChoices()[0].name
			}
		}, nil, nil},
	{"Machine type", func(c *config) string { return machineByName(c.machine).label(c.price) },
		func(c *config, d int) {
			ch := c.machineChoices()
			names := make([]string, len(ch))
			for i, m := range ch {
				names[i] = m.name
			}
			c.machine = cycleStr(c.machine, names, d)
		}, nil, nil},
	{"Nodes in worker pool", func(c *config) string { return fmt.Sprintf("%d", c.nodes) },
		func(c *config, d int) { c.nodes = clampInt(c.nodes+d, 1, 20) }, nil, nil},
	{"Workers (sandbox slots)", func(c *config) string { return fmt.Sprintf("%d", c.workers) },
		func(c *config, d int) { c.workers = clampInt(c.workers+5*d, 5, 200) }, nil, nil},
	{"Simulated agents", func(c *config) string { return fmt.Sprintf("%d", c.agents) },
		func(c *config, d int) { c.agents = clampInt(c.agents+10*d, 10, 1000) }, nil, nil},
	{"Workload", func(c *config) string {
		switch {
		case c.coding():
			return fmt.Sprintf("coding agent — %d-step task script (substrate#1934)", scriptSum(c.workload).Steps)
		case c.assistant():
			return fmt.Sprintf("personal assistant — one day in %d steps, ~1 GiB resident (substrate#2230)", scriptSum(c.workload).Steps)
		case c.ping():
			return "one-ping — GluttonUser loop (wake · ping · suspend · 10 s wait; 20 actors/user)"
		}
		return "personal agent — 3 chats + 40 check-ins a day"
	}, func(c *config, d int) {
		c.workload = cycleStr(c.workload, []string{"personal", "coding-session", "personal-assistant", "nano-personal-agent", "ping"}, d)
		if c.assistant() && c.thinkScale >= 1 {
			c.thinkScale = 0.02 // a day in ~30 min
		} else if c.coding() && c.thinkScale < 1 {
			c.thinkScale = 4
		}
	}, nil, nil},
	{"  tasks per agent-day", func(c *config) string { return fmt.Sprintf("%d", c.tasksPerDay) },
		func(c *config, d int) { c.tasksPerDay = cycleInt(c.tasksPerDay, []int{1, 2, 4, 8, 16, 32}, d) },
		func(c *config) bool { return !c.coding() }, nil},
	{"  LLM think scale", func(c *config) string {
		if c.assistant() {
			return fmt.Sprintf("×%g — a day of the assistant's life every %s", c.thinkScale, dayIn(int(1/c.thinkScale+0.5)))
		}
		return fmt.Sprintf("×%g — think gaps %.0f–%.0f s between steps", c.thinkScale, 2*c.thinkScale, 8*c.thinkScale)
	}, func(c *config, d int) {
		c.thinkScale = cycleFloat(c.thinkScale, []float64{0.02, 0.05, 0.1, 1, 2, 4, 8, 10, 20}, d)
	}, func(c *config) bool { return !c.scripted() }, nil},
	{"  suspend between steps", func(c *config) string {
		if c.scriptSuspend == "idle" {
			return "idle wait decides (production-like)"
		}
		return "driver, right after each step (benchmark-like)"
	}, func(c *config, d int) { c.scriptSuspend = cycleStr(c.scriptSuspend, []string{"driver", "idle"}, d) },
		func(c *config) bool { return !c.scripted() }, nil},
	{"Time compression", func(c *config) string {
		return fmt.Sprintf("×%d — a day of agent-life every %s", c.compress, dayIn(c.compress))
	},
		func(c *config, d int) { c.compress = cycleInt(c.compress, []int{1, 3, 6, 12, 20, 60}, d) }, nil, nil},
	{"Test length", func(c *config) string {
		if c.duration == "2m" || c.duration == "5m" {
			return c.duration + " (smoke test — thin statistics)"
		}
		return c.duration
	},
		func(c *config, d int) {
			c.duration = cycleStr(c.duration, []string{"2m", "5m", "10m", "20m", "30m", "45m", "60m"}, d)
		}, nil, nil},
	{"Idle wait before suspend", func(c *config) string { return c.idle },
		func(c *config, d int) { c.idle = cycleStr(c.idle, []string{"2s", "5s", "10s", "30s"}, d) }, nil, nil},
	{"Pricing", func(c *config) string { return c.price },
		func(c *config, d int) { c.price = cycleStr(c.price, priceModels, d) }, nil, nil},
	{"Provision for peaks", func(c *config) string { return c.peak },
		func(c *config, d int) {
			c.peak = cycleStr(c.peak, []string{"×1.5 average", "×2 average", "×3 average", "herd 15%", "herd 25%", "herd 40%"}, d)
		}, nil, nil},
	{"Mode", func(c *config) string {
		if c.loadTest {
			return "load-test (waves until failure)"
		}
		return "baseline (fixed fleet)"
	}, func(c *config, d int) { c.loadTest = !c.loadTest }, nil, nil},
	{"  wave start", func(c *config) string { return fmt.Sprintf("%d agents", c.waveStart) },
		func(c *config, d int) { c.waveStart = clampInt(c.waveStart+5*d, 5, 500) },
		func(c *config) bool { return !c.loadTest }, nil},
	{"  wave step", func(c *config) string { return fmt.Sprintf("+%d agents", c.waveStep) },
		func(c *config, d int) { c.waveStep = clampInt(c.waveStep+5*d, 5, 100) },
		func(c *config) bool { return !c.loadTest }, nil},
	{"  wave window", func(c *config) string { return c.waveIntvl },
		func(c *config, d int) { c.waveIntvl = cycleStr(c.waveIntvl, []string{"2m", "3m", "5m"}, d) },
		func(c *config) bool { return !c.loadTest }, nil},
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (a *App) viewConfig() string {
	var b strings.Builder
	b.WriteString(logo() + "\n")
	b.WriteString(sSubtle.Render("How much does an always-available agent cost per month? Configure, run, find out.") + "\n\n")

	for i, f := range fields {
		dim := f.dim != nil && f.dim(&a.cfg)
		label := fmt.Sprintf("  %-26s", f.label)
		val := f.get(&a.cfg)
		switch {
		case dim:
			why := "  (enabled in load-test mode)"
			if !strings.HasPrefix(f.label, "  wave") {
				why = "  (scripted workloads only)"
			}
			b.WriteString(sFaint.Render(label+"  "+val) + sFaint.Render(why) + "\n")
		case i == a.cursor:
			b.WriteString(sKey.Render("▸ ") + sSelected.Render(fmt.Sprintf("%-26s", f.label)) +
				"  " + sSelected.Render("‹ "+val+" ›") + "\n")
		default:
			b.WriteString("  " + sSubtle.Render(fmt.Sprintf("%-26s", f.label)) + "  " + sBig.Render(val) + "\n")
		}
		// Right under the cluster-identity fields: what actually exists in GCP.
		if f.label == "Substrate repo" {
			b.WriteString("\n" + a.clusterPanel() + "\n\n")
		}
	}

	// live load estimate + model preview
	d := a.cfg.demand()
	score := a.cfg.loadScore()
	per, workerMo, n := a.cfg.costPreview()
	gauge := func() string {
		filled := int(score + 0.5)
		if filled > 10 {
			filled = 10
		}
		if filled < 0 {
			filled = 0
		}
		return strings.Repeat("▮", filled) + strings.Repeat("▯", 10-filled)
	}()
	head := fmt.Sprintf("estimated load %s %.1f/10  (~%.1f of %d workers busy on average)",
		gauge, score, d, a.cfg.workers)
	var feas string
	switch {
	case score < 6:
		feas = sSubtle.Render(head) + "\n" + sSubtle.Render("light — the pool will be mostly idle; raise agents or compression for a fuller test")
	case score < 9:
		feas = sGood.Render(head) + "\n" + sGood.Render("good — busy pool with headroom for peaks")
	case score <= 10.5:
		feas = sWarn.Render(head) + "\n" + sWarn.Render("hot — peaks will queue; expect some refusals")
	default:
		feas = sBad.Render(head) + "\n" + sBad.Render("overload — this run WILL saturate the pool (switch overhead doesn't compress)")
	}
	preview := fmt.Sprintf("model preview @ real duty: %.1f agents/worker · worker $%.2f/mo → ≈ $%.2f/agent/mo",
		n, workerMo, per)

	b.WriteString("\n" + sPanel.Render(feas+"\n"+sSubtle.Render(preview)) + "\n")
	b.WriteString("\n" + hints([][2]string{{"↑/↓", "field"}, {"←/→", "change"}, {"?", "what do these mean"}, {"r", "re-check cluster"}, {"enter", "run"}, {"q", "quit"}}))
	return b.String()
}

func hints(hs [][2]string) string {
	parts := make([]string, 0, len(hs))
	for _, h := range hs {
		parts = append(parts, sKey.Render("["+h[0]+"]")+" "+sSubtle.Render(h[1]))
	}
	return " " + strings.Join(parts, sFaint.Render("  ·  "))
}

var _ = lipgloss.Width // keep import if unused elsewhere
