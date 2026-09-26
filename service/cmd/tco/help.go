package main

// The knob explainer, toggled with '?' from the config screen. One page,
// grouped like the form, written for someone who has never seen the model.

import "strings"

func (a *App) viewHelp() string {
	h := func(s string) string { return sTitle.Render(s) }
	k := func(s string) string { return sAccent.Render(s) }
	d := func(s string) string { return sSubtle.Render(s) }

	var b strings.Builder
	b.WriteString(h(" What every knob means") + "\n\n")

	b.WriteString(h(" Cluster identity") + "\n")
	b.WriteString(k("  GCP project / Zone / Cluster / Substrate repo") + "\n")
	b.WriteString(d("  Where the lab runs. The panel below these fields shows what already\n"+
		"  exists (cluster status, node pools) — existing infra is reused, missing\n"+
		"  infra is created by the setup stages. Type to edit; 'r' re-checks.") + "\n\n")

	b.WriteString(h(" Machines & sandbox") + "\n")
	b.WriteString(k("  Sandbox") + d("  gVisor runs on any machine. microVM needs nested\n"+
		"  virtualization, so choosing it filters the machine list to capable types\n"+
		"  (Intel N1/N2/N4/C2/C3/C4, AMD N4D) — that's why the order matters.") + "\n")
	b.WriteString(k("  Machine type / Nodes") + d("  The node pool the workers run on; the label\n"+
		"  shows the real us-central1 price at your pricing model.") + "\n")
	b.WriteString(k("  Workers") + d("  Fixed-size sandbox slots carved from the nodes. One live\n"+
		"  agent per worker at a time — the pool you oversubscribe.") + "\n\n")

	b.WriteString(h(" Workload") + "\n")
	b.WriteString(k("  Simulated agents") + d("  Personal-agent profile, fixed: ~3 chat sessions\n"+
		"  (8 min) + ~40 short check-ins per day, ~2.4% awake — plus real work per\n"+
		"  turn: RAM working set walked and dirtied, files written and read.") + "\n")
	b.WriteString(k("  Time compression ×k") + d("  Plays a DAY of agent behavior in 24/k hours:\n"+
		"  the gaps between wake-ups and session lengths shrink ÷k. What does NOT\n"+
		"  compress: suspend (~2.5s), resume (~1.4s), the idle wait — those are\n"+
		"  physical. So ×6 = a day in 4h with overhead 6× overweighted; ×60 = a day\n"+
		"  in 24 min but each wake-up's ~7s overhead acts like ~7 MINUTES of a real\n"+
		"  day, and the pool saturates. The fits/tight/overload line accounts for\n"+
		"  exactly this. Real-world cost is computed by putting the MEASURED\n"+
		"  suspend/resume times back into the model at real duty cycle — the\n"+
		"  compressed run exists to measure those, not to be realistic itself.") + "\n")
	b.WriteString(k("  Test length") + d("  Wall-clock run time. 2m/5m = smoke test (a handful of\n"+
		"  activations, latencies indicative only); 10m+ = usable p50s; 30m+ = the\n"+
		"  p99s and density numbers worth quoting. At ×6, 50 agents ≈ 8\n"+
		"  activations/min, so a 30m run scores ~250 of them.") + "\n")
	b.WriteString(k("  Idle wait") + d("  How long the auto-suspender waits after an agent's last\n"+
		"  request before suspending it. Short = denser packing but suspends can\n"+
		"  race mid-conversation traffic; the OpenClaw demo used 2s.") + "\n\n")

	b.WriteString(h(" Economics") + "\n")
	b.WriteString(k("  Pricing") + d("  on-demand, or 1-/3-year committed-use discounts (37%/55%\n"+
		"  off). Only changes the $ math, never the run.") + "\n\n")

	b.WriteString(h(" Mode & waves") + "\n")
	b.WriteString(k("  baseline") + d("  All agents active the whole run → one density + cost\n"+
		"  measurement for the configured fleet.") + "\n")
	b.WriteString(k("  load-test") + d("  Finds the pool's ceiling: starts 'wave start' agents,\n"+
		"  watches for one 'wave window', then adds 'wave step' more, repeating\n"+
		"  until a wave fails its thresholds (refusals >5% or errors >2% of\n"+
		"  activations). Verdict = the last level that held, i.e. the maximum\n"+
		"  sustainable agents for this pool. The three wave fields are dimmed —\n"+
		"  and skipped by the cursor — unless Mode is load-test, because they do\n"+
		"  nothing in baseline mode.") + "\n\n")

	b.WriteString(h(" The two footer panels") + "\n")
	b.WriteString(d("  fits/tight/overload — predicted average busy workers vs your pool, at\n"+
		"  the COMPRESSED rate (the run you are about to start). model preview —\n"+
		"  the Phase 1 cost model at REAL duty cycle on your machine choice; the\n"+
		"  run's final card should land near it if the model is honest.") + "\n")

	b.WriteString("\n" + hints([][2]string{{"?/esc/q", "back"}, {"ctrl+c", "exit"}}))
	return b.String()
}
