package main

// The knob explainer, toggled with '?' from the config screen. Plain
// language, one thought per knob, air between entries.

import "strings"

type helpItem struct{ term, desc string }
type helpSection struct {
	title string
	items []helpItem
}

var helpSections = []helpSection{
	{"Cluster", []helpItem{
		{"GCP project · Zone · Cluster · Substrate repo",
			"Where everything runs. The panel under these fields shows what already exists — existing pieces are reused, missing ones get created."},
	}},
	{"Machines", []helpItem{
		{"Sandbox",
			"gVisor runs on any machine. microVM needs special machines (nested virtualization), so picking it shortens the machine list."},
		{"Machine type · Nodes",
			"The VMs that host the workers, with real monthly prices in the label."},
		{"Workers",
			"Slots that each run one awake agent. Having far more agents than workers is the whole point — sleeping agents don't need one."},
	}},
	{"Workload", []helpItem{
		{"Simulated agents",
			"Each behaves like a personal assistant: a few chats plus ~40 quick check-ins a day, awake ~2% of the time, doing real memory and file work while awake."},
		{"Time compression",
			"Fast-forward. ×6 squeezes a day of agent-life into 4 hours by shrinking the quiet gaps. Suspend and resume still take their real seconds, so high compression works the pool much harder than real life would — the fits/overload line under the form warns you. The final $ number is unaffected: it uses the measured suspend/resume times at the real pace."},
		{"Test length",
			"2m/5m proves the machinery works; 10m gives decent medians; 30m+ gives numbers worth quoting."},
		{"Idle wait",
			"How long an agent must stay quiet before it is put to sleep. Shorter packs tighter but risks suspending mid-conversation."},
	}},
	{"Money", []helpItem{
		{"Pricing",
			"On-demand, or 1-/3-year committed-use discounts (37% / 55% off). Only changes the math, never the run."},
	}},
	{"Mode", []helpItem{
		{"baseline",
			"The whole fleet runs for the whole test: one density and cost measurement."},
		{"load-test",
			"Finds the ceiling: start with 'wave start' agents, add 'wave step' more every 'wave window', until too many requests fail (refusals >5% or errors >2%). The last healthy wave is the most this pool can sustain. The wave fields only matter here — that's why they're dimmed in baseline mode."},
	}},
	{"The two panels under the form", []helpItem{
		{"estimated load (x/10)",
			"Predicts how busy the pool will be during THIS compressed run, calibrated against a measured baseline. 10 = fully busy on average. 6-9 is the sweet spot; above 10 the run will saturate and mostly measure queueing."},
		{"model preview",
			"Predicts the real-world $ per agent per month for your choices. The final card should land near it."},
	}},
}

// wrap breaks s into lines of at most w runes, on spaces.
func wrap(s string, w int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= w:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func (a *App) viewHelp() string {
	width := a.width - 10
	if width < 40 {
		width = 64
	}
	if width > 78 {
		width = 78
	}

	var b strings.Builder
	b.WriteString(sTitle.Render(" What every knob means") + "\n")
	for _, sec := range helpSections {
		b.WriteString("\n" + sTitle.Render(" "+sec.title) + "\n")
		b.WriteString(sFaint.Render(" "+strings.Repeat("─", len(sec.title)+1)) + "\n")
		for _, it := range sec.items {
			b.WriteString("\n  " + sAccent.Render(it.term) + "\n")
			for _, l := range wrap(it.desc, width-6) {
				b.WriteString("      " + sSubtle.Render(l) + "\n")
			}
		}
	}
	b.WriteString("\n" + hints([][2]string{{"?/esc/q", "back"}, {"ctrl+c", "exit"}}))
	return b.String()
}
