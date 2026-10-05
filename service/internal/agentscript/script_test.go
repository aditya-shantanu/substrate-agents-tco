package agentscript

import (
	"strings"
	"testing"
	"time"
)

func TestEmbeddedScriptsAreValid(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("no embedded scripts")
	}
	for _, n := range names {
		s, err := Load(n)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if s.Name != n {
			t.Errorf("%s: script name %q should match its file name", n, s.Name)
		}
	}
}

// The upstream default must round-trip to the totals its README quotes:
// 20 steps, ~96Mi RAM, 1Gi floor, 69s of think.
func TestCodingSessionShape(t *testing.T) {
	s, err := Load(DefaultScript)
	if err != nil {
		t.Fatal(err)
	}
	sum := Summarize(s)
	if sum.Steps != 20 {
		t.Errorf("steps = %d, want 20", sum.Steps)
	}
	if sum.Think != 69*time.Second {
		t.Errorf("think = %s, want 69s", sum.Think)
	}
	if s.MinActorMemory != 1<<30 {
		t.Errorf("min_actor_memory = %s, want 1Gi", FormatSize(s.MinActorMemory))
	}
	b := Budgets(s.Steps)
	if b.RAM != 96<<20 {
		t.Errorf("declared RAM = %s, want 96Mi", FormatSize(b.RAM))
	}
	if sum.MaxIngest != 32<<20 {
		t.Errorf("max ingest = %s, want 32Mi", FormatSize(sum.MaxIngest))
	}
	if sum.BurnWall <= 0 || sum.BurnCPU < sum.BurnWall {
		t.Errorf("burn wall %s / cpu %s look wrong", sum.BurnWall, sum.BurnCPU)
	}
}

func TestResolveFileVsName(t *testing.T) {
	if _, err := Resolve("coding-session"); err != nil {
		t.Errorf("built-in by name: %v", err)
	}
	if _, err := Resolve("/definitely/not/here.yaml"); err == nil || !strings.Contains(err.Error(), "read script") {
		t.Errorf("missing file should fail with a read error, got %v", err)
	}
	if _, err := Resolve("nope"); err == nil || !strings.Contains(err.Error(), "no built-in script") {
		t.Errorf("unknown name should list built-ins, got %v", err)
	}
}

func TestDecodeRejects(t *testing.T) {
	base := func(ops string) string {
		return "name: t\nmin_actor_memory: 1Gi\nsteps:\n  - name: a\n    agent: x\n    think: 1s\n    ops:\n" + ops
	}
	cases := map[string]struct{ yaml, want string }{
		"unknown kind":      {base("      - frobnicate: {}\n"), "unknown op kind"},
		"unknown field":     {base("      - ping: {}\n    extra: 1\n"), "field extra not found"},
		"arg not taken":     {base("      - ping: {key: a}\n"), "takes no key"},
		"read before write": {base("      - read_disk_digest: {key: f}\n"), "before any ingest or write_disk"},
		"walk before fill":  {base("      - walk_ram: {key: r}\n"), "before any fill_ram"},
		"bad millis":        {base("      - burn_cpu: {millis: 0}\n"), "millis must be positive"},
		"no think": {"name: t\nmin_actor_memory: 1Gi\nsteps:\n  - name: a\n    agent: x\n    ops:\n      - ping: {}\n",
			"think time must be positive"},
		"duplicate step": {base("      - ping: {}\n  - name: a\n    agent: y\n    think: 1s\n    ops:\n      - ping: {}\n"),
			"duplicate step name"},
		"memory floor": {"name: t\nmin_actor_memory: 16Mi\nsteps:\n  - name: a\n    agent: x\n    think: 1s\n    ops:\n      - fill_ram: {key: r, size: 32Mi}\n",
			"below the declared RAM"},
		"no floor": {"name: t\nsteps:\n  - name: a\n    agent: x\n    think: 1s\n    ops:\n      - ping: {}\n",
			"min_actor_memory is required"},
	}
	for name, c := range cases {
		_, err := Decode([]byte(c.yaml))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", name, c.want, err)
		}
	}
}

func TestDecodeAccepts(t *testing.T) {
	y := `name: mini
min_actor_memory: 256Mi
steps:
  - name: 01_clone
    agent: git clone
    think: 1.5s
    ops:
      - ingest: {key: repo, size: 4Mi}
      - burn_cpu: {millis: 200, parallel: 2}
  - name: 02_build
    agent: build
    think: 2s
    ops:
      - fill_ram: {key: heap, size: 8Mi}
      - read_disk_data: {key: repo}
      - churn_ram: {key: heap, size: 1Mi}
      - walk_ram: {key: heap}
      - burn_cpu: {millis: 100}
      - ping: {}
`
	s, err := Decode([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Steps[0].Ops[1]; got.Parallel != 2 || got.Millis != 200 {
		t.Errorf("burn_cpu decoded as %+v", got)
	}
	if got := s.Steps[1].Ops[4]; got.Parallel != 1 {
		t.Errorf("parallel should default to 1, got %d", got.Parallel)
	}
	if s.Steps[0].Think != 1500*time.Millisecond {
		t.Errorf("think = %s", s.Steps[0].Think)
	}
	sum := Summarize(s)
	if sum.BurnWall != 300*time.Millisecond || sum.BurnCPU != 500*time.Millisecond {
		t.Errorf("burn wall %s cpu %s", sum.BurnWall, sum.BurnCPU)
	}
}

func TestParseSize(t *testing.T) {
	good := map[string]int64{"64Ki": 64 << 10, "32Mi": 32 << 20, "1Gi": 1 << 30, "1.5Gi": 3 << 29, "500M": 500_000_000, "4096": 4096}
	for in, want := range good {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "Mi", "-1Mi", "0", "1.5", "3Xi", "12 apples"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) should fail", bad)
		}
	}
	if FormatSize(1<<30) != "1Gi" || FormatSize(96<<20) != "96Mi" || FormatSize(1500) != "1500" {
		t.Errorf("FormatSize: %s %s %s", FormatSize(1<<30), FormatSize(96<<20), FormatSize(1500))
	}
}
