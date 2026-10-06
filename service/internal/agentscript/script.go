// Package agentscript loads and validates agent-session scripts: a coding
// agent's task spelled out as steps, each naming what the agent is doing,
// the LLM "think" gap before it, and the resource operations (glutton RPCs)
// that act it out in the sandbox.
//
// Vendored from agent-substrate/substrate@bec46812
// internal/benchmarking/boomer/agentsession (script.go, scriptfile.go,
// validate.go, scripts.go), which this module cannot import because the
// package is internal to that tree. The YAML format, op kinds and
// invariants are kept identical so a script written for the upstream
// benchmark (`locust/deploy.sh --agentsession-script FILE`) runs unchanged
// here, and vice versa. Differences: exported field names, no YAML encoder,
// Kubernetes quantities parsed locally (no apimachinery dependency).
//
// Upstream copyright 2026 Google LLC, Apache License 2.0.
package agentscript

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Kind is the kind of one resource effect inside a step.
type Kind int

const (
	// KindIngest pushes bytes from the driver through the router into the
	// actor, which writes them to disk: real network ingress + disk write,
	// the shape of a download.
	KindIngest Kind = iota
	// KindBurnCPU spins the sandbox's CPU for a wall-clock duration.
	KindBurnCPU
	// KindWriteDisk writes locally generated random bytes to a sandbox file.
	KindWriteDisk
	// KindReadDiskDigest reads and sha256-hashes a sandbox file without
	// shipping the bytes back: disk read I/O only.
	KindReadDiskDigest
	// KindReadDiskData reads a sandbox file AND returns its bytes to the
	// driver: disk read + network egress through the router response.
	KindReadDiskData
	// KindFillRAM allocates a resident RAM array of random bytes.
	KindFillRAM
	// KindChurnRAM re-randomizes part of an existing RAM array in place,
	// dirtying pages so the next suspend snapshot has fresh content.
	KindChurnRAM
	// KindWalkRAM touches one byte per page of a RAM array, forcing every
	// page resident — after a resume this measures demand-paging cost.
	KindWalkRAM
	// KindPing is a minimal round-trip through the router.
	KindPing
	// KindDwell keeps the actor resident and idle for Millis without any
	// request: a gateway sitting in a model round trip or a typing gap.
	KindDwell
)

// Op is one resource effect inside a step, executed as a single glutton
// RPC through the router.
type Op struct {
	Kind     Kind
	Key      string // file or RAM-array name inside the sandbox
	Bytes    int64  // payload / file / RAM size
	Millis   int64  // CPU burn wall-clock
	Parallel int32  // CPU burn goroutines
}

// Ping is the wake probe sent ahead of every step.
func Ping() Op { return Op{Kind: KindPing} }

// Step is one agent action: what a coding agent would be doing, the think
// time that precedes it (the LLM producing this step), and the resource
// operations acting it out.
type Step struct {
	// Name keys the step's stats row.
	Name string
	// Agent says what the coding agent is doing, for humans.
	Agent string
	// Think is the suspended gap before the step.
	Think time.Duration
	Ops   []Op
}

// Script is a loaded agent-session script: a named step sequence plus the
// actor memory it needs.
//
// The YAML form:
//
//	name: coding-session
//	min_actor_memory: 1Gi
//	steps:
//	- name: 01_read_task
//	  agent: Boots, reads the task prompt, loads its context window
//	  think: 2s
//	  ops:
//	  - fill_ram: {key: agent_context, size: 32Mi}
//	  - ping: {}
//
// Each op is a one-key map whose key is the op kind and whose value holds
// that kind's arguments. Sizes are Kubernetes quantities (16Mi, 64Ki);
// think times are Go durations (2s, 1.5s). Unknown kinds, unknown fields,
// and arguments a kind does not take are errors.
type Script struct {
	Name string
	// MinActorMemory is the smallest actor memory limit the script is known
	// to run under, in bytes. The driver refuses to start against a smaller
	// template. Required, and at least the script's declared RAM plus disk
	// (the data dir is tmpfs).
	MinActorMemory int64
	Steps          []Step
}

type scriptDoc struct {
	Name           string    `yaml:"name"`
	MinActorMemory string    `yaml:"min_actor_memory"`
	Steps          []stepDoc `yaml:"steps"`
}

type stepDoc struct {
	Name  string  `yaml:"name"`
	Agent string  `yaml:"agent"`
	Think string  `yaml:"think"`
	Ops   []opDoc `yaml:"ops"`
}

// opDoc is one op: exactly one entry, kind name to arguments.
type opDoc map[string]opArgs

type opArgs struct {
	Key      string `yaml:"key,omitempty"`
	Size     string `yaml:"size,omitempty"`
	Millis   int64  `yaml:"millis,omitempty"`
	Parallel int32  `yaml:"parallel,omitempty"`
}

// opSpec says which arguments an op kind takes.
type opSpec struct {
	kind   Kind
	key    bool // takes key
	size   bool // takes size
	millis bool // takes millis and parallel
}

var opSpecs = map[string]opSpec{
	"ingest":           {kind: KindIngest, key: true, size: true},
	"burn_cpu":         {kind: KindBurnCPU, millis: true},
	"write_disk":       {kind: KindWriteDisk, key: true, size: true},
	"read_disk_digest": {kind: KindReadDiskDigest, key: true},
	"read_disk_data":   {kind: KindReadDiskData, key: true},
	"fill_ram":         {kind: KindFillRAM, key: true, size: true},
	"churn_ram":        {kind: KindChurnRAM, key: true, size: true},
	"walk_ram":         {kind: KindWalkRAM, key: true},
	"ping":             {kind: KindPing},
	"dwell":            {kind: KindDwell, millis: true},
}

var kindNames = func() map[Kind]string {
	m := make(map[Kind]string, len(opSpecs))
	for name, spec := range opSpecs {
		m[spec.kind] = name
	}
	return m
}()

// String is the op kind's YAML name.
func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// keyRE mirrors glutton's diskKeyRE: keys name files under the actor's data
// dir, so nothing that could escape it is accepted.
var keyRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// scriptNameRE bounds script names, which double as the --script knob
// value and the embedded file name.
var scriptNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Decode parses a YAML script and validates it.
func Decode(data []byte) (*Script, error) {
	var doc scriptDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse script: %w", err)
	}
	s := &Script{Name: doc.Name}
	if doc.MinActorMemory != "" {
		n, err := ParseSize(doc.MinActorMemory)
		if err != nil {
			return nil, fmt.Errorf("min_actor_memory: %w", err)
		}
		s.MinActorMemory = n
	}
	for i, sd := range doc.Steps {
		step, err := decodeStep(sd)
		if err != nil {
			return nil, fmt.Errorf("step %d (%q): %w", i+1, sd.Name, err)
		}
		s.Steps = append(s.Steps, step)
	}
	if err := Validate(s); err != nil {
		return nil, err
	}
	return s, nil
}

func decodeStep(sd stepDoc) (Step, error) {
	step := Step{Name: sd.Name, Agent: sd.Agent}
	if sd.Think != "" {
		d, err := time.ParseDuration(sd.Think)
		if err != nil {
			return step, fmt.Errorf("think: %w", err)
		}
		step.Think = d
	}
	for i, od := range sd.Ops {
		o, err := decodeOp(od)
		if err != nil {
			return step, fmt.Errorf("op %d: %w", i+1, err)
		}
		step.Ops = append(step.Ops, o)
	}
	return step, nil
}

func decodeOp(od opDoc) (Op, error) {
	if len(od) != 1 {
		return Op{}, fmt.Errorf("an op is a single-key map, got %d keys", len(od))
	}
	var name string
	var args opArgs
	for name, args = range od {
	}
	spec, ok := opSpecs[name]
	if !ok {
		return Op{}, fmt.Errorf("unknown op kind %q", name)
	}
	o := Op{Kind: spec.kind}
	switch {
	case spec.key && args.Key == "":
		return Op{}, fmt.Errorf("%s: key is required", name)
	case !spec.key && args.Key != "":
		return Op{}, fmt.Errorf("%s: takes no key", name)
	}
	if args.Key != "" {
		if !keyRE.MatchString(args.Key) {
			return Op{}, fmt.Errorf("%s: key %q must match %s", name, args.Key, keyRE)
		}
		o.Key = args.Key
	}
	switch {
	case spec.size && args.Size == "":
		return Op{}, fmt.Errorf("%s: size is required", name)
	case !spec.size && args.Size != "":
		return Op{}, fmt.Errorf("%s: takes no size", name)
	}
	if args.Size != "" {
		n, err := ParseSize(args.Size)
		if err != nil {
			return Op{}, fmt.Errorf("%s: size: %w", name, err)
		}
		o.Bytes = n
	}
	if spec.millis {
		if args.Millis <= 0 {
			return Op{}, fmt.Errorf("%s: millis must be positive", name)
		}
		if spec.kind == KindDwell && args.Parallel != 0 {
			return Op{}, fmt.Errorf("%s: takes no parallel", name)
		}
		if args.Parallel < 0 {
			return Op{}, fmt.Errorf("%s: parallel cannot be negative", name)
		}
		o.Millis = args.Millis
		o.Parallel = max(args.Parallel, 1)
	} else if args.Millis != 0 || args.Parallel != 0 {
		return Op{}, fmt.Errorf("%s: takes no millis or parallel", name)
	}
	return o, nil
}

// Budget is what a script declares it will hold in the sandbox: resident
// RAM (the largest fill per array) and files in the data dir (the largest
// object per key). The data dir is tmpfs, so both come out of the actor's
// memory limit. It does not model the guest's real peak: kernel, sandbox
// runtime, and Go allocator transients sit on top.
type Budget struct {
	RAM  int64
	Disk int64
}

// Budgets sums the bytes a step sequence declares.
func Budgets(steps []Step) Budget {
	ramMax := map[string]int64{}
	diskMax := map[string]int64{}
	for _, s := range steps {
		for _, o := range s.Ops {
			switch o.Kind {
			case KindFillRAM:
				ramMax[o.Key] = max(ramMax[o.Key], o.Bytes)
			case KindIngest, KindWriteDisk:
				diskMax[o.Key] = max(diskMax[o.Key], o.Bytes)
			}
		}
	}
	var b Budget
	for _, v := range ramMax {
		b.RAM += v
	}
	for _, v := range diskMax {
		b.Disk += v
	}
	return b
}

// Validate pins a script's invariants: a usable name, a memory floor that
// covers what the script declares, unique non-empty steps with positive
// think times, and no op that consumes a sandbox object before an earlier
// step created it. A broken ordering would fail at run time with NotFound
// from glutton; this catches it when the script is loaded.
func Validate(s *Script) error {
	if !scriptNameRE.MatchString(s.Name) {
		return fmt.Errorf("script name %q must match %s", s.Name, scriptNameRE)
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("script %q has no steps", s.Name)
	}
	seen := map[string]bool{}
	ramFilled := map[string]bool{}
	diskWritten := map[string]bool{}
	for i, st := range s.Steps {
		where := fmt.Sprintf("step %d (%q)", i+1, st.Name)
		if st.Name == "" || st.Agent == "" {
			return fmt.Errorf("%s: name and agent must be set", where)
		}
		if seen[st.Name] {
			return fmt.Errorf("%s: duplicate step name", where)
		}
		seen[st.Name] = true
		if st.Think <= 0 {
			return fmt.Errorf("%s: think time must be positive", where)
		}
		if len(st.Ops) == 0 {
			return fmt.Errorf("%s: has no ops", where)
		}
		for j, o := range st.Ops {
			switch o.Kind {
			case KindFillRAM:
				ramFilled[o.Key] = true
			case KindChurnRAM, KindWalkRAM:
				if !ramFilled[o.Key] {
					return fmt.Errorf("%s op %d: %s of RAM array %q before any fill_ram", where, j+1, o.Kind, o.Key)
				}
			case KindIngest, KindWriteDisk:
				diskWritten[o.Key] = true
			case KindReadDiskDigest, KindReadDiskData:
				if !diskWritten[o.Key] {
					return fmt.Errorf("%s op %d: %s of %q before any ingest or write_disk", where, j+1, o.Kind, o.Key)
				}
			}
		}
	}
	b := Budgets(s.Steps)
	if s.MinActorMemory <= 0 {
		return fmt.Errorf("script %q: min_actor_memory is required (declared RAM %s + disk %s)", s.Name, FormatSize(b.RAM), FormatSize(b.Disk))
	}
	if declared := b.RAM + b.Disk; s.MinActorMemory < declared {
		return fmt.Errorf("script %q: min_actor_memory %s is below the declared RAM %s + disk %s", s.Name, FormatSize(s.MinActorMemory), FormatSize(b.RAM), FormatSize(b.Disk))
	}
	return nil
}

var sizeSuffix = map[string]int64{
	"":  1,
	"k": 1000, "K": 1000, "M": 1000 * 1000, "G": 1000 * 1000 * 1000, "T": 1000 * 1000 * 1000 * 1000,
	"Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40,
}

// ParseSize reads a Kubernetes-style quantity (64Ki, 32Mi, 1Gi, 500M, or a
// bare byte count) as a positive whole byte count.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	numPart, unit := s[:i], s[i:]
	mult, ok := sizeSuffix[unit]
	if !ok || numPart == "" {
		return 0, fmt.Errorf("%q is not a size (use a byte count or a Ki/Mi/Gi, k/M/G quantity)", s)
	}
	f, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size: %w", s, err)
	}
	n := f * float64(mult)
	if n <= 0 || n != float64(int64(n)) {
		return 0, fmt.Errorf("%q is not a positive whole byte count", s)
	}
	return int64(n), nil
}

// FormatSize renders a byte count as a binary quantity (32Mi, 1Gi) when it
// divides evenly, else as plain bytes.
func FormatSize(n int64) string {
	for _, u := range []struct {
		suffix string
		div    int64
	}{{"Ti", 1 << 40}, {"Gi", 1 << 30}, {"Mi", 1 << 20}, {"Ki", 1 << 10}} {
		if n >= u.div && n%u.div == 0 {
			return fmt.Sprintf("%d%s", n/u.div, u.suffix)
		}
	}
	return strconv.FormatInt(n, 10)
}
