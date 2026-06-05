package agentclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// investigate_recordfile.go is the on-disk form of recorded INVESTIGATE responses
// (SPEC §8, §12), parallel to recordfile.go for classify. The committed
// investigate.recorded.json lets a CI run REPLAY the investigate agent's
// resolutions byte-for-byte with no LLM, keyed by break id.

// InvestigateRecordedSchemaVersion is the FROZEN version of the
// investigate.recorded.json fixture (SPEC §12, §13). Bump it only with a
// deliberate, documented format change — the fixture is committed and reviewed, so
// its shape is a contract just like the trace.
const InvestigateRecordedSchemaVersion = 1

// RecordedPosting is one recorded {entry_type, params} a resolution adds, keyed to
// the source event_id it books for (SPEC §8). Params are integer paise (int64),
// never float — the on-disk form of a Posting.
type RecordedPosting struct {
	EventID   string           `json:"event_id"`
	EntryType string           `json:"entry_type"`
	Params    map[string]int64 `json:"params,omitempty"`
}

// RecordedResolution is one recorded investigate decision keyed to a break id
// (SPEC §8, §12). Either Resolution is the postings to add (a resolution) OR
// Escalate+Reason record a recorded escalation (the agent declined) so replay
// reproduces an escalation deterministically too. ToolsUsed records the read-only
// tools the recovery consulted (e.g. "orders.fetch").
type RecordedResolution struct {
	BreakKey   string            `json:"break_key"`
	Resolution []RecordedPosting `json:"resolution,omitempty"`
	Rationale  string            `json:"rationale,omitempty"`
	ToolsUsed  []string          `json:"tools_used,omitempty"`
	Escalate   bool              `json:"escalate,omitempty"`
	Reason     string            `json:"reason,omitempty"`
}

// RecordedInvestigateFile is the committed investigate.recorded.json: a version
// stamp plus the recorded resolutions, sorted by break_key so the file is
// byte-stable and reviewable. It is keyed-by-break logically; on disk it is a
// sorted slice (not a JSON object) so key ORDER is deterministic. The reader
// rebuilds the break_key -> resolution index in memory.
type RecordedInvestigateFile struct {
	SchemaVersion int                  `json:"schema_version"`
	World         string               `json:"world"`
	Period        string               `json:"period"`
	Resolutions   []RecordedResolution `json:"resolutions"`
}

// index builds the break_key -> RecordedResolution lookup the replay client uses.
// On a duplicate key the FIRST wins (the file is generated sorted+unique, so this
// is defensive only); it never panics.
func (f RecordedInvestigateFile) index() map[string]RecordedResolution {
	m := make(map[string]RecordedResolution, len(f.Resolutions))
	for _, r := range f.Resolutions {
		if _, dup := m[r.BreakKey]; !dup {
			m[r.BreakKey] = r
		}
	}
	return m
}

// sortResolutions orders the resolutions by break_key in place, giving the
// committed file a deterministic, reviewable order (SPEC §12).
func (f *RecordedInvestigateFile) sortResolutions() {
	sort.Slice(f.Resolutions, func(i, j int) bool {
		return f.Resolutions[i].BreakKey < f.Resolutions[j].BreakKey
	})
}

// upsert inserts or replaces the recorded resolution for r.BreakKey, keeping the
// resolutions unique by break_key. Live RECORD mode uses it to fold a freshly
// fetched resolution into the existing fixture without duplicating a break.
func (f *RecordedInvestigateFile) upsert(r RecordedResolution) {
	for i := range f.Resolutions {
		if f.Resolutions[i].BreakKey == r.BreakKey {
			f.Resolutions[i] = r
			return
		}
	}
	f.Resolutions = append(f.Resolutions, r)
}

// investigateRecordedFileName is the committed recorded-investigation fixture's
// base name; it lives under the same per-period agent/ directory as the classify
// fixture (agentDir, defined in recordfile.go).
const investigateRecordedFileName = "investigate.recorded.json"

// InvestigateRecordedPath resolves
// worlds/<world>/<period>/agent/investigate.recorded.json under root. It does no
// IO; the reader/writer surface a missing file with a clear error.
func InvestigateRecordedPath(root, world, period string) string {
	return filepath.Join(root, "worlds", world, period, agentDir, investigateRecordedFileName)
}

// ReadInvestigateRecorded loads and decodes the recorded-investigation fixture at
// path. A missing file is reported with a clear, actionable error; a malformed
// file or wrong schema version is rejected — a corrupt replay oracle is never
// silently used. It does NOT read truth.
func ReadInvestigateRecorded(path string) (RecordedInvestigateFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RecordedInvestigateFile{}, fmt.Errorf("agentclient: recorded investigations %s not found (was the period recorded?)", path)
		}
		return RecordedInvestigateFile{}, fmt.Errorf("agentclient: read %s: %w", path, err)
	}
	var f RecordedInvestigateFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return RecordedInvestigateFile{}, fmt.Errorf("agentclient: decode %s: %w", path, err)
	}
	if dec.More() {
		return RecordedInvestigateFile{}, fmt.Errorf("agentclient: %s has trailing data after the JSON value", path)
	}
	if f.SchemaVersion != InvestigateRecordedSchemaVersion {
		return RecordedInvestigateFile{}, fmt.Errorf("agentclient: %s schema_version=%d, want %d (frozen)", path, f.SchemaVersion, InvestigateRecordedSchemaVersion)
	}
	return f, nil
}

// WriteInvestigateRecorded marshals f to stable, indented JSON (sorted
// resolutions, sorted map keys, trailing newline, no HTML escaping) and writes it
// atomically to path, creating the parent agent/ directory as needed — a
// byte-reproducible committed fixture (SPEC §12). It reuses marshalStable from
// recordfile.go.
func WriteInvestigateRecorded(path string, f RecordedInvestigateFile) error {
	f.sortResolutions()
	data, err := marshalStable(f)
	if err != nil {
		return fmt.Errorf("agentclient: marshal recorded investigations: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("agentclient: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+investigateRecordedFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("agentclient: temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("agentclient: write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("agentclient: close %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("agentclient: finalize %s: %w", path, err)
	}
	return nil
}
