package main

import "fmt"

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
	return fmt.Sprintf("%s  (%d vCPU · %.0f GiB · $%.0f/mo %s%s)",
		m.name, m.vcpu, m.gib, m.hourly(model)*730, model, tag)
}

func machineByName(name string) machine {
	for _, m := range machines {
		if m.name == name {
			return m
		}
	}
	return machines[0]
}
