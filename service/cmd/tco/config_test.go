package main

import (
	"math"
	"strings"
	"testing"
)

func testConfig() config {
	return config{
		sandbox: "gvisor", machine: "c3-standard-4", nodes: 2, workers: 10,
		agents: 120, compress: 6, duration: "30m", idle: "2s", price: "cud3",
		peak: "×2 average", waveStart: 10, waveStep: 10, waveIntvl: "3m",
		workload: "personal", tasksPerDay: 8, thinkScale: 4, scriptSuspend: "driver",
	}
}

func TestCodingWorkloadEnvAndPreview(t *testing.T) {
	c := testConfig()
	pPer, _, pN := c.costPreview()
	pDemand := c.demand()

	c.workload = "coding-session"
	env := strings.Join(c.env(), "\n")
	for _, want := range []string{"WORKLOAD=coding-session", "THINK_SCALE=4", "SCRIPT_SUSPEND=driver", "SESSIONS_PER_DAY=8", "WAKES_PER_DAY=0"} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %s:\n%s", want, env)
		}
	}
	per, _, n := c.costPreview()
	if math.IsNaN(per) || math.IsInf(per, 0) || per <= 0 || n <= 0 {
		t.Errorf("coding preview per=%v n=%v", per, n)
	}
	if per == pPer || n == pN {
		t.Errorf("coding preview should differ from personal: per %v vs %v, n %v vs %v", per, pPer, n, pN)
	}
	if d := c.demand(); d <= 0 || d == pDemand {
		t.Errorf("coding demand %v (personal %v)", d, pDemand)
	}
	// idle mode holds workers through short think gaps: never cheaper than driver
	c.scriptSuspend = "idle"
	perIdle, _, _ := c.costPreview()
	if perIdle < per {
		t.Errorf("idle preview %v cheaper than driver %v", perIdle, per)
	}
	if c.demand() < 0 {
		t.Error("negative demand")
	}
}

func TestPersonalEnvHasNoTaskOverrides(t *testing.T) {
	env := strings.Join(testConfig().env(), "\n")
	if strings.Contains(env, "SESSIONS_PER_DAY") || strings.Contains(env, "WAKES_PER_DAY") {
		t.Errorf("personal workload must leave lib.sh defaults alone:\n%s", env)
	}
	if !strings.Contains(env, "WORKLOAD=personal") {
		t.Error("WORKLOAD=personal missing")
	}
}
