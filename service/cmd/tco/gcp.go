package main

// Live GCP probing for the config screen (idea borrowed from substrate-gke's
// installer): before the user commits to a run, show whether the named
// cluster already exists and what node pools it carries, and pre-fill the
// machine/node knobs from reality.

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type poolInfo struct {
	Name    string
	Machine string
	Count   int
	Status  string
}

type clusterInfo struct {
	probedKey string // project|zone|cluster this result belongs to
	exists    bool
	status    string
	version   string
	nodes     int
	pools     []poolInfo
	err       error
}

type clusterInfoMsg clusterInfo

func (c config) probeKey() string {
	return c.project + "|" + c.zone + "|" + c.cluster
}

// probeCluster describes the cluster with gcloud in the background.
func probeCluster(c config) tea.Cmd {
	key := c.probeKey()
	return func() tea.Msg {
		cmd := exec.Command("gcloud", "container", "clusters", "describe", c.cluster,
			"--location", c.zone, "--project", c.project, "--format=json")
		out, err := cmd.Output()
		if err != nil {
			msg := ""
			if ee, ok := err.(*exec.ExitError); ok {
				msg = string(ee.Stderr)
			}
			if strings.Contains(msg, "No cluster named") || strings.Contains(msg, "NOT_FOUND") ||
				strings.Contains(msg, "not found") {
				return clusterInfoMsg{probedKey: key, exists: false}
			}
			return clusterInfoMsg{probedKey: key, err: fmt.Errorf("gcloud describe: %s",
				strings.TrimSpace(firstLine(msg)))}
		}
		var d struct {
			Status               string `json:"status"`
			CurrentMasterVersion string `json:"currentMasterVersion"`
			CurrentNodeCount     int    `json:"currentNodeCount"`
			NodePools            []struct {
				Name             string `json:"name"`
				Status           string `json:"status"`
				InitialNodeCount int    `json:"initialNodeCount"`
				Config           struct {
					MachineType string `json:"machineType"`
				} `json:"config"`
				Autoscaling struct {
					Enabled bool `json:"enabled"`
				} `json:"autoscaling"`
			} `json:"nodePools"`
		}
		if err := json.Unmarshal(out, &d); err != nil {
			return clusterInfoMsg{probedKey: key, err: err}
		}
		ci := clusterInfoMsg{probedKey: key, exists: true,
			status: d.Status, version: shortVersion(d.CurrentMasterVersion), nodes: d.CurrentNodeCount}
		for _, p := range d.NodePools {
			ci.pools = append(ci.pools, poolInfo{
				Name: p.Name, Machine: p.Config.MachineType,
				Count: p.InitialNodeCount, Status: p.Status,
			})
		}
		return ci
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func shortVersion(v string) string {
	if i := strings.Index(v, "-gke"); i > 0 {
		return v[:i]
	}
	return v
}

// clusterPanel renders the probe result for the config screen.
func (a *App) clusterPanel() string {
	ci := a.cluster
	switch {
	case ci == nil || ci.probedKey != a.cfg.probeKey():
		return sPanel.Render(sSubtle.Render(
			spinnerFrames[a.spin%len(spinnerFrames)] + " checking cluster " +
				a.cfg.cluster + " in " + a.cfg.zone + "…  " +
				sFaint.Render("(r to re-check)")))
	case ci.err != nil:
		return sPanel.Render(sWarn.Render("cluster check failed: ") +
			sSubtle.Render(ci.err.Error()) + sFaint.Render("   [r] retry"))
	case !ci.exists:
		return sPanel.Render(
			sSubtle.Render(glyphPending+" cluster ") + sBig.Render(a.cfg.cluster) +
				sSubtle.Render(" not found — stage 1 will create it in "+a.cfg.zone) +
				sFaint.Render("  (~10–15 min)"))
	default:
		var b strings.Builder
		state := sGood.Render(glyphDone + " " + ci.status)
		if ci.status != "RUNNING" {
			state = sWarn.Render("! " + ci.status)
		}
		b.WriteString(state + sSubtle.Render(" cluster ") + sBig.Render(a.cfg.cluster) +
			sSubtle.Render(fmt.Sprintf("  ·  GKE %s  ·  %d node(s) — will be reused", ci.version, ci.nodes)))
		for _, p := range ci.pools {
			extra := ""
			if p.Status != "" && p.Status != "RUNNING" {
				extra = "  " + sWarn.Render(p.Status)
			}
			nested := ""
			if m := machineByName(p.Machine); m.nested {
				nested = sFaint.Render("  µVM-ok")
			}
			b.WriteString("\n   " + sSubtle.Render("pool ") + sAccent.Render(p.Name) +
				sSubtle.Render(fmt.Sprintf("  %s × %d", p.Machine, p.Count)) + nested + extra)
		}
		return sPanel.Render(b.String())
	}
}

// prefillFromCluster copies discovered reality into the knobs, once, and
// only where the user hasn't already changed the defaults.
func (a *App) prefillFromCluster(ci clusterInfo) {
	if a.prefilled || !ci.exists || len(ci.pools) == 0 {
		return
	}
	a.prefilled = true
	def := defaultConfig()
	pool := ci.pools[0]
	for _, p := range ci.pools {
		if strings.Contains(p.Name, "substrate") {
			pool = p
			break
		}
	}
	if a.cfg.machine == def.machine && pool.Machine != "" {
		a.cfg.machine = pool.Machine
		ensureCatalog(pool.Machine)
	}
	if a.cfg.nodes == def.nodes && ci.nodes > 0 {
		a.cfg.nodes = ci.nodes
	}
}

var _ = time.Now
