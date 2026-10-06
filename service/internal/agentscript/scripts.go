package agentscript

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
)

// DefaultScript is the built-in 20-step coding session (upstream's default).
const DefaultScript = "coding-session"

// scriptFS holds the built-in script variants. A file scripts/<name>.yaml
// is selectable as --script=<name>; TestEmbeddedScriptsAreValid keeps every
// one of them loadable.
//
//go:embed scripts/*.yaml
var scriptFS embed.FS

// Names lists the built-in script variants.
func Names() []string {
	entries, err := fs.ReadDir(scriptFS, "scripts")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".yaml"); ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Load returns a built-in script by name.
func Load(name string) (*Script, error) {
	data, err := scriptFS.ReadFile("scripts/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("no built-in script %q (have %s)", name, strings.Join(Names(), ", "))
	}
	s, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("built-in script %q: %w", name, err)
	}
	return s, nil
}

// LoadFile reads and validates a script from a YAML file.
func LoadFile(path string) (*Script, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read script: %w", err)
	}
	s, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("script %s: %w", path, err)
	}
	return s, nil
}

// Resolve loads a script from a source that is either a built-in name
// ("coding-session") or a file path (anything with a path separator or a
// .yaml/.yml suffix).
func Resolve(source string) (*Script, error) {
	if strings.ContainsAny(source, `/\`) || strings.HasSuffix(source, ".yaml") || strings.HasSuffix(source, ".yml") {
		return LoadFile(source)
	}
	return Load(source)
}

// Summary is the shape of a script in the units the cost model uses.
type Summary struct {
	Steps int
	// Think is the sum of the unscaled think gaps: idle time per task.
	Think time.Duration
	// BurnWall is the wall-clock the burn_cpu ops pin the sandbox for.
	BurnWall time.Duration
	// BurnCPU is burn wall × parallelism: CPU-seconds per task.
	BurnCPU time.Duration
	// Dwell is resident idle time inside steps (worker held, no request).
	Dwell time.Duration
	// Ingest, DiskWrite and RAMFill are the bytes moved per task.
	Ingest, DiskWrite, RAMFill int64
	// MaxIngest is the largest single ingest payload (the driver's buffer).
	MaxIngest int64
}

// Summarize totals a script.
func Summarize(s *Script) Summary {
	var sum Summary
	sum.Steps = len(s.Steps)
	for _, st := range s.Steps {
		sum.Think += st.Think
		for _, o := range st.Ops {
			switch o.Kind {
			case KindBurnCPU:
				w := time.Duration(o.Millis) * time.Millisecond
				sum.BurnWall += w
				sum.BurnCPU += w * time.Duration(max(o.Parallel, 1))
			case KindDwell:
				sum.Dwell += time.Duration(o.Millis) * time.Millisecond
			case KindIngest:
				sum.Ingest += o.Bytes
				sum.MaxIngest = max(sum.MaxIngest, o.Bytes)
			case KindWriteDisk:
				sum.DiskWrite += o.Bytes
			case KindFillRAM:
				sum.RAMFill += o.Bytes
			}
		}
	}
	return sum
}
