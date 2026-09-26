package main

import (
	"fmt"
	"strconv"
	"strings"
)

// FAM carries us-central1 per-vCPU / per-GiB on-demand rates and nested-virt
// support per family (docs/RESEARCH-gcp-pricing.md, retrieved 2026-09-25).
var FAM = map[string]struct {
	cpu, gib float64
	nested   bool
}{
	"e2":  {0.021811, 0.002923, false},
	"n1":  {0.031611, 0.004237, true},
	"n2":  {0.031611, 0.004237, true},
	"n2d": {0.027502, 0.003686, false},
	"n4":  {0.031190, 0.003540, true},
	"c3":  {0.034650, 0.003938, true},
	"c3d": {0.029563, 0.003959, false},
	"c4":  {0.034650, 0.003938, true},
	"c4d": {0.032704, 0.003753, false},
	"t2d": {0.027502, 0.003686, false},
}

// Machine catalog with us-central1 prices (retrieved 2026-09-25, see
// docs/RESEARCH-gcp-pricing.md). nested = supports nested virtualization,
// which the microVM sandbox class requires.
type machine struct {
	name       string
	vcpu       int
	gib        float64
	cpuHr      float64 // $/vCPU-hr on-demand
	gibHr      float64 // $/GiB-hr on-demand
	nested     bool
}

var machines = []machine{
	{"c3-standard-4", 4, 16, 0.034650, 0.003938, true},
	{"c3-standard-8", 8, 32, 0.034650, 0.003938, true},
	{"n2-standard-4", 4, 16, 0.031611, 0.004237, true},
	{"n2-standard-8", 8, 32, 0.031611, 0.004237, true},
	{"n4-standard-4", 4, 16, 0.031190, 0.003540, true},
	{"c4-standard-8", 8, 30, 0.034650, 0.003938, true},
	{"e2-standard-4", 4, 16, 0.021811, 0.002923, false},
	{"e2-standard-8", 8, 32, 0.021811, 0.002923, false},
	{"n2d-standard-8", 8, 32, 0.027502, 0.003686, false},
	{"c3d-standard-8", 8, 32, 0.029563, 0.003959, false},
	{"t2d-standard-8", 8, 32, 0.027502, 0.003686, false},
}

var priceModels = []string{"cud3", "cud1", "od"}

func priceMult(model string) float64 {
	switch model {
	case "cud1":
		return 0.63
	case "cud3":
		return 0.45
	}
	return 1.0
}

func (m machine) hourly(model string) float64 {
	return (float64(m.vcpu)*m.cpuHr + m.gib*m.gibHr) * priceMult(model)
}

func (m machine) label(model string) string {
	tag := ""
	if m.nested {
		tag = " · µVM-ok"
	}
	price := fmt.Sprintf("$%.0f/mo %s", m.hourly(model)*730, model)
	if m.cpuHr == 0 {
		price = "price n/a"
	}
	return fmt.Sprintf("%s  (%d vCPU · %.0f GiB · %s%s)",
		m.name, m.vcpu, m.gib, price, tag)
}

func machineByName(name string) machine {
	for _, m := range machines {
		if m.name == name {
			return m
		}
	}
	if m, ok := synthesize(name); ok {
		return m
	}
	return machines[0]
}

// nestedFamilies supporting nested virtualization (docs/RESEARCH-gcp-pricing.md).
var nestedFamilies = map[string]bool{
	"n1": true, "n2": true, "n4": true, "n4d": true, "c2": true,
	"c3": true, "c4": true, "c4n": true, "a2": true, "g2": true,
	"h3": true, "m4": true, "z3": true,
}

// synthesize builds a catalog entry for a machine type discovered on a live
// cluster but absent from the built-in list (e.g. e2-medium, c2d-standard-16).
// Prices come from the family table when known, else 0 (label shows n/a).
func synthesize(name string) (machine, bool) {
	parts := strings.Split(name, "-")
	if len(parts) != 3 {
		return machine{}, false
	}
	fam, kind := parts[0], parts[1]
	cpus, err := strconv.Atoi(parts[2])
	if err != nil || cpus <= 0 {
		return machine{}, false
	}
	gibPerCPU := map[string]float64{"standard": 4, "highcpu": 2, "highmem": 8}[kind]
	if fam == "c4" && kind == "standard" {
		gibPerCPU = 3.75
	}
	if gibPerCPU == 0 {
		gibPerCPU = 4
	}
	m := machine{name: name, vcpu: cpus, gib: float64(cpus) * gibPerCPU,
		nested: nestedFamilies[fam]}
	if f, ok := FAM[fam]; ok {
		m.cpuHr, m.gibHr = f.cpu, f.gib
		m.nested = f.nested
	}
	return m, true
}

// ensureCatalog makes a discovered machine selectable in the dropdown.
func ensureCatalog(name string) {
	for _, m := range machines {
		if m.name == name {
			return
		}
	}
	if m, ok := synthesize(name); ok {
		machines = append([]machine{m}, machines...)
	}
}
