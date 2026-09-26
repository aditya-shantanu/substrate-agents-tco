package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The config screen: arrow-key form with prefilled choices. Every field maps
// to an env var consumed by experiment/*.sh, so the TUI is presentation over
// the same scripts people can run by hand.

type config struct {
	sandbox   string // gvisor | microvm
	machine   string
	nodes     int
	workers   int
	agents    int
	compress  int
	duration  string
	idle      string
	price     string
	loadTest  bool
	waveStart int
	waveStep  int
	waveIntvl string
}

func defaultConfig() config {
	return config{
		sandbox: "gvisor", machine: "c3-standard-4", nodes: 2, workers: 10,
		agents: 50, compress: 6, duration: "30m", idle: "2s", price: "cud3",
		loadTest: false, waveStart: 10, waveStep: 10, waveIntvl: "3m",
	}
}

func (c config) env() []string {
	lt := "false"
	if c.loadTest {
		lt = "true"
	}
	return []string{
		"SANDBOX_CLASS=" + c.sandbox,
		"GVISOR_NODE_MACHINE_TYPE=" + c.machine,
		fmt.Sprintf("NODE_COUNT=%d", c.nodes),
		fmt.Sprintf("WORKER_COUNT=%d", c.workers),
		fmt.Sprintf("AGENTS=%d", c.agents),
		fmt.Sprintf("COMPRESS=%d", c.compress),
		"DURATION=" + c.duration,
		"IDLE_TIMEOUT=" + c.idle,
		"PRICE_MODEL=" + c.price,
		"LOAD_TEST=" + lt,
		fmt.Sprintf("WAVE_START=%d", c.waveStart),
		fmt.Sprintf("WAVE_STEP=%d", c.waveStep),
		"WAVE_INTERVAL=" + c.waveIntvl,
	}
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
	estSuspend  = 2.7    // measured on this stack (glutton, gVisor)
	estResume   = 1.5
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
func (c config) demand() float64 {
	arrivalPerSec := float64(c.agents) * wActsPerDay * float64(c.compress) / 86400
	holdSec := parseSec(c.idle) + estSuspend + estResume + 4 // +work per wake
	sessShare := 3.0 / wActsPerDay
	sessExtra := (8*60/float64(c.compress) - 4) * sessShare // sessions dwell longer
	return arrivalPerSec * (holdSec + sessExtra)
}

// costPreview runs the Phase 1 model at REAL duty cycle with measured
// switch estimates on the chosen machine.
func (c config) costPreview() (perAgent, workerMo float64, agentsPerWorker float64) {
	m := machineByName(c.machine)
	occ := (wLiveSecDay + wActsPerDay*(10+estSuspend+estResume)) / 86400 // 10s real-world idle wait
	n := 0.70 / (occ * 2)                                               // utilization 0.7, peak 2
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
}

func cycleStr(cur string, opts []string, dir int) string {
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

var fields = []field{
	{"Sandbox", func(c *config) string { return c.sandbox },
		func(c *config, d int) {
			c.sandbox = cycleStr(c.sandbox, []string{"gvisor", "microvm"}, d)
			if c.sandbox == "microvm" && !machineByName(c.machine).nested {
				c.machine = c.machineChoices()[0].name
			}
		}, nil},
	{"Machine type", func(c *config) string { return machineByName(c.machine).label(c.price) },
		func(c *config, d int) {
			ch := c.machineChoices()
			names := make([]string, len(ch))
			for i, m := range ch {
				names[i] = m.name
			}
			c.machine = cycleStr(c.machine, names, d)
		}, nil},
	{"Nodes in worker pool", func(c *config) string { return fmt.Sprintf("%d", c.nodes) },
		func(c *config, d int) { c.nodes = clampInt(c.nodes+d, 1, 20) }, nil},
	{"Workers (sandbox slots)", func(c *config) string { return fmt.Sprintf("%d", c.workers) },
		func(c *config, d int) { c.workers = clampInt(c.workers+5*d, 5, 200) }, nil},
	{"Simulated agents", func(c *config) string { return fmt.Sprintf("%d", c.agents) },
		func(c *config, d int) { c.agents = clampInt(c.agents+10*d, 10, 1000) }, nil},
	{"Time compression", func(c *config) string { return fmt.Sprintf("×%d", c.compress) },
		func(c *config, d int) { c.compress = cycleInt(c.compress, []int{1, 3, 6, 12, 20, 60}, d) }, nil},
	{"Test length", func(c *config) string { return c.duration },
		func(c *config, d int) { c.duration = cycleStr(c.duration, []string{"10m", "20m", "30m", "45m", "60m"}, d) }, nil},
	{"Idle wait before suspend", func(c *config) string { return c.idle },
		func(c *config, d int) { c.idle = cycleStr(c.idle, []string{"2s", "5s", "10s", "30s"}, d) }, nil},
	{"Pricing", func(c *config) string { return c.price },
		func(c *config, d int) { c.price = cycleStr(c.price, priceModels, d) }, nil},
	{"Mode", func(c *config) string {
		if c.loadTest {
			return "load-test (waves until failure)"
		}
		return "baseline (fixed fleet)"
	}, func(c *config, d int) { c.loadTest = !c.loadTest }, nil},
	{"  wave start", func(c *config) string { return fmt.Sprintf("%d agents", c.waveStart) },
		func(c *config, d int) { c.waveStart = clampInt(c.waveStart+5*d, 5, 500) },
		func(c *config) bool { return !c.loadTest }},
	{"  wave step", func(c *config) string { return fmt.Sprintf("+%d agents", c.waveStep) },
		func(c *config, d int) { c.waveStep = clampInt(c.waveStep+5*d, 5, 100) },
		func(c *config) bool { return !c.loadTest }},
	{"  wave window", func(c *config) string { return c.waveIntvl },
		func(c *config, d int) { c.waveIntvl = cycleStr(c.waveIntvl, []string{"2m", "3m", "5m"}, d) },
		func(c *config) bool { return !c.loadTest }},
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
			b.WriteString(sFaint.Render(label+"  "+val) + "\n")
		case i == a.cursor:
			b.WriteString(sKey.Render("▸ ") + sSelected.Render(fmt.Sprintf("%-26s", f.label)) +
				"  " + sSelected.Render("‹ "+val+" ›") + "\n")
		default:
			b.WriteString("  " + sSubtle.Render(fmt.Sprintf("%-26s", f.label)) + "  " + sBig.Render(val) + "\n")
		}
	}

	// live feasibility + model preview
	d := a.cfg.demand()
	per, workerMo, n := a.cfg.costPreview()
	feas := sGood.Render(fmt.Sprintf("fits: ~%.1f of %d workers busy on average", d, a.cfg.workers))
	if d > 0.75*float64(a.cfg.workers) {
		feas = sWarn.Render(fmt.Sprintf("tight: ~%.1f of %d workers busy — expect refusals (compression makes switch overhead loom large)", d, a.cfg.workers))
	}
	if d > float64(a.cfg.workers) {
		feas = sBad.Render(fmt.Sprintf("overload: needs ~%.1f workers, pool has %d — this run WILL saturate", d, a.cfg.workers))
	}
	preview := fmt.Sprintf("model preview @ real duty: %.1f agents/worker · worker $%.2f/mo → ≈ $%.2f/agent/mo",
		n, workerMo, per)

	b.WriteString("\n" + sPanel.Render(feas+"\n"+sSubtle.Render(preview)) + "\n")
	b.WriteString("\n" + hints([][2]string{{"↑/↓", "field"}, {"←/→", "change"}, {"enter", "run"}, {"q", "quit"}}))
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
