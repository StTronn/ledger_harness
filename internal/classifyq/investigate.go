package classifyq

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/razorpay/close-agent/internal/agentclient"
)

// investigate.go adds the ASYNC investigate stores, parallel to the classify ones.
// The investigate agent runs on a SECOND queue — reconcile BREAKS (which only exist
// after the books are built) — and writes resolutions the apply stage validates +
// books. Same keyed-store determinism anchor as classify.

// BreakWork is one parked reconcile break plus the candidate events the agent
// inspects (the unbooked suspects). Break/Candidates reuse the agentclient shapes.
type BreakWork struct {
	Break      agentclient.BreakSummary   `json:"break"`
	Candidates []agentclient.EventSummary `json:"candidates"`
}

// BreaksFile is the on-disk break queue (emitted when a close run ends with breaks).
type BreaksFile struct {
	SchemaVersion int         `json:"schema_version"`
	World         string      `json:"world"`
	Period        string      `json:"period"`
	Breaks        []BreakWork `json:"breaks"`
}

// Resolution status values.
const (
	// StatusResolved: the agent proposes postings that resolve the break.
	StatusResolved = "resolved"
)

// ResolutionPosting is one {entry_type, recovered facts + citation} the investigate
// agent proposes to add, keyed to the source event it books for (e.g. the unbooked
// refund). Like classify, it carries recovered FACTS, not money — apply derives it.
type ResolutionPosting struct {
	EventID   string      `json:"event_id"`
	EntryType string      `json:"entry_type"`
	Recovered []Recovered `json:"recovered,omitempty"`
}

// Resolution is the agent's answer for one break: postings to add (resolved) or an
// escalation. Keyed by break_key.
type Resolution struct {
	BreakKey  string              `json:"break_key"`
	Status    string              `json:"status"` // resolved | escalated
	Postings  []ResolutionPosting `json:"postings,omitempty"`
	ToolsUsed []string            `json:"tools_used,omitempty"`
	Rationale string              `json:"rationale,omitempty"`
	Reason    string              `json:"reason,omitempty"`
}

// Resolved reports whether the resolution carries usable postings.
func (r Resolution) Resolved() bool { return r.Status == StatusResolved && len(r.Postings) > 0 }

// ResolutionsFile is the on-disk results store the investigate agent writes.
type ResolutionsFile struct {
	SchemaVersion int          `json:"schema_version"`
	World         string       `json:"world"`
	Period        string       `json:"period"`
	Resolutions   []Resolution `json:"resolutions"`
}

// Index builds the break_key -> Resolution lookup the apply stage uses.
func (f ResolutionsFile) Index() map[string]Resolution {
	m := make(map[string]Resolution, len(f.Resolutions))
	for _, r := range f.Resolutions {
		if _, dup := m[r.BreakKey]; !dup {
			m[r.BreakKey] = r
		}
	}
	return m
}

const (
	breaksFileName      = "breaks.json"
	resolutionsFileName = "resolutions.json"
)

// BreaksPath / ResolutionsPath resolve the two investigate store files.
func BreaksPath(root, world, period string) string {
	return filepath.Join(runDir(root, world, period), breaksFileName)
}
func ResolutionsPath(root, world, period string) string {
	return filepath.Join(runDir(root, world, period), resolutionsFileName)
}

// WriteBreaks sorts (by break key) and writes the break queue atomically.
func WriteBreaks(path string, f BreaksFile) error {
	sort.Slice(f.Breaks, func(i, j int) bool { return f.Breaks[i].Break.Key < f.Breaks[j].Break.Key })
	return writeStable(path, f, breaksFileName)
}

// ReadBreaks loads the break queue.
func ReadBreaks(path string) (BreaksFile, error) {
	var f BreaksFile
	if err := readStrict(path, &f); err != nil {
		return BreaksFile{}, err
	}
	if f.SchemaVersion != SchemaVersion {
		return BreaksFile{}, fmt.Errorf("classifyq: %s schema_version=%d, want %d", path, f.SchemaVersion, SchemaVersion)
	}
	return f, nil
}

// WriteResolutions sorts (by break key) and writes the investigate results store.
// In production the TS flue-agent writes resolutions.json; this is used by tests and
// any future Go-side generator.
func WriteResolutions(path string, f ResolutionsFile) error {
	sort.Slice(f.Resolutions, func(i, j int) bool { return f.Resolutions[i].BreakKey < f.Resolutions[j].BreakKey })
	return writeStable(path, f, resolutionsFileName)
}

// ReadResolutions loads the investigate results store.
func ReadResolutions(path string) (ResolutionsFile, error) {
	var f ResolutionsFile
	if err := readStrict(path, &f); err != nil {
		return ResolutionsFile{}, err
	}
	if f.SchemaVersion != SchemaVersion {
		return ResolutionsFile{}, fmt.Errorf("classifyq: %s schema_version=%d, want %d", path, f.SchemaVersion, SchemaVersion)
	}
	return f, nil
}
