package main

// Theme ported from substrate-gke's installer ("Electric Sky over Obsidian")
// so the two tools feel like siblings.

import "github.com/charmbracelet/lipgloss"

var (
	cSky    = lipgloss.Color("#38BDF8")
	cIndigo = lipgloss.Color("#818CF8")
	cMint   = lipgloss.Color("#34D399")
	cAmber  = lipgloss.Color("#FBBF24")
	cRose   = lipgloss.Color("#F43F5E")
	cSlate  = lipgloss.Color("#94A3B8")
	cFaint  = lipgloss.Color("#475569")
	cInk    = lipgloss.Color("#E2E8F0")
)

var (
	sTitle    = lipgloss.NewStyle().Bold(true).Foreground(cSky)
	sSubtle   = lipgloss.NewStyle().Foreground(cSlate)
	sFaint    = lipgloss.NewStyle().Foreground(cFaint)
	sAccent   = lipgloss.NewStyle().Foreground(cIndigo)
	sGood     = lipgloss.NewStyle().Bold(true).Foreground(cMint)
	sWarn     = lipgloss.NewStyle().Foreground(cAmber)
	sBad      = lipgloss.NewStyle().Foreground(cRose)
	sKey      = lipgloss.NewStyle().Bold(true).Foreground(cSky)
	sSelected = lipgloss.NewStyle().Bold(true).Foreground(cInk).Background(lipgloss.Color("#1E293B"))
	sBig      = lipgloss.NewStyle().Bold(true).Foreground(cInk)

	sPanel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cFaint).
		Padding(0, 1)
	sAccentPanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cSky).
			Padding(0, 1)
	sMoneyPanel = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(cMint).
			Padding(0, 2)
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	glyphDone    = "✓"
	glyphFail    = "✗"
	glyphPending = "○"
	glyphActive  = "●"
)

var logoLines = []string{
	`▄▀█ █▀▀ █▀▀ █▄░█ ▀█▀ █▀   ▄▀█ ▀█▀   █▀▀ █▀█ █▀ ▀█▀`,
	`█▀█ █▄█ ██▄ █░▀█ ░█░ ▄█   █▀█ ░█░   █▄▄ █▄█ ▄█ ░█░`,
}

func logo() string {
	grad := []lipgloss.Color{cSky, cIndigo}
	out := ""
	for i, l := range logoLines {
		out += lipgloss.NewStyle().Foreground(grad[i%len(grad)]).Render(l)
		if i < len(logoLines)-1 {
			out += "\n"
		}
	}
	return out
}

var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// sparkline renders vals scaled to max as unicode blocks, last n entries.
func sparkline(vals []int, maxVal, n int) string {
	if len(vals) > n {
		vals = vals[len(vals)-n:]
	}
	if maxVal < 1 {
		maxVal = 1
	}
	out := make([]rune, 0, len(vals))
	for _, v := range vals {
		i := v * (len(sparkBlocks) - 1) / maxVal
		if i < 0 {
			i = 0
		}
		if i >= len(sparkBlocks) {
			i = len(sparkBlocks) - 1
		}
		out = append(out, sparkBlocks[i])
	}
	return string(out)
}
