package agentclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RecordedSchemaVersion is the FROZEN version of the classify.recorded.json
// fixture (SPEC §12 recorded-response mode, §13 freeze the seams). Bump it only
// with a deliberate, documented format change. The fixture is committed and
// reviewed, so its shape is a contract just like the trace.
const RecordedSchemaVersion = 1

// RecordedResponse is one recorded {entry_type, params, rationale} keyed to an
// event_id (SPEC §8, §12). It is the on-disk form of a ClassifyResult for ONE
// event: the agent's classification captured (live mode) or generated from the
// recovery source (recover.go) so REPLAY can return it byte-for-byte without any
// network/LLM. Unclassifiable + Reason record a recorded ESCALATION (the agent
// declined) so replay reproduces an escalation deterministically too.
//
// JSON key order is fixed by struct tags; Params is a map, so the writer sorts /
// the marshaller emits keys in sorted order (encoding/json sorts map keys), which
// keeps the file byte-stable across regenerations (SPEC §12).
type RecordedResponse struct {
	EventID        string           `json:"event_id"`
	EntryType      string           `json:"entry_type,omitempty"`
	Params         map[string]int64 `json:"params,omitempty"` // paise (int64), never float
	Rationale      string           `json:"rationale,omitempty"`
	ToolsUsed      []string         `json:"tools_used,omitempty"`
	Unclassifiable bool             `json:"unclassifiable,omitempty"`
	Reason         string           `json:"reason,omitempty"`
}

// RecordedFile is the committed classify.recorded.json: a version stamp plus the
// recorded responses, sorted by event_id so the file is byte-stable and reviewable
// (a stable diff per event). It is keyed-by-event_id logically; on disk it is a
// sorted slice (not a JSON object) so key ORDER is deterministic and a reviewer
// reads events in a stable order. The reader rebuilds the event_id -> response
// index in memory.
type RecordedFile struct {
	SchemaVersion int                `json:"schema_version"`
	World         string             `json:"world"`
	Period        string             `json:"period"`
	Responses     []RecordedResponse `json:"responses"`
}

// index builds the event_id -> RecordedResponse lookup the replay client uses.
// On a duplicate event_id the FIRST wins (the file is generated sorted+unique, so
// this is defensive only); it never panics.
func (f RecordedFile) index() map[string]RecordedResponse {
	m := make(map[string]RecordedResponse, len(f.Responses))
	for _, r := range f.Responses {
		if _, dup := m[r.EventID]; !dup {
			m[r.EventID] = r
		}
	}
	return m
}

// sortResponses orders the responses by event_id in place, giving the committed
// file a deterministic, reviewable order (SPEC §12). It is called by both the
// generator and the live recorder before marshalling.
func (f *RecordedFile) sortResponses() {
	sort.Slice(f.Responses, func(i, j int) bool {
		return f.Responses[i].EventID < f.Responses[j].EventID
	})
}

// upsert inserts or replaces the recorded response for r.EventID, keeping the
// responses unique by event_id. Live RECORD mode uses it to fold a freshly-fetched
// response into the existing fixture without duplicating an event.
func (f *RecordedFile) upsert(r RecordedResponse) {
	for i := range f.Responses {
		if f.Responses[i].EventID == r.EventID {
			f.Responses[i] = r
			return
		}
	}
	f.Responses = append(f.Responses, r)
}

// recordedFileName is the committed recorded-response fixture's base name; agentDir
// is the per-period directory that holds it. The recovery source (orders.json)
// lives under razorpay/; the agent's recorded responses live under agent/, kept
// separate so it is clear these are agent artifacts, not raw Razorpay objects.
const (
	recordedFileName = "classify.recorded.json"
	agentDir         = "agent"
)

// RecordedPath resolves worlds/<world>/<period>/agent/classify.recorded.json under
// root — the committed recorded-response fixture (SPEC §12). It does no IO; the
// reader/writer surface a missing file with a clear error.
func RecordedPath(root, world, period string) string {
	return filepath.Join(root, "worlds", world, period, agentDir, recordedFileName)
}

// ReadRecorded loads and decodes the recorded-response fixture at path. A missing
// file is reported with a clear, actionable error (the period has no committed
// agent responses), and a malformed file or wrong schema version is rejected — a
// corrupt replay oracle is never silently used. It does NOT read truth.
func ReadRecorded(path string) (RecordedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RecordedFile{}, fmt.Errorf("agentclient: recorded responses %s not found (was the period recorded?)", path)
		}
		return RecordedFile{}, fmt.Errorf("agentclient: read %s: %w", path, err)
	}
	var f RecordedFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return RecordedFile{}, fmt.Errorf("agentclient: decode %s: %w", path, err)
	}
	if dec.More() {
		return RecordedFile{}, fmt.Errorf("agentclient: %s has trailing data after the JSON value", path)
	}
	if f.SchemaVersion != RecordedSchemaVersion {
		return RecordedFile{}, fmt.Errorf("agentclient: %s schema_version=%d, want %d (frozen)", path, f.SchemaVersion, RecordedSchemaVersion)
	}
	return f, nil
}

// WriteRecorded marshals f to stable, indented JSON (sorted responses, trailing
// newline, no HTML escaping) and writes it atomically to path, creating the parent
// agent/ directory as needed. "Stable" gives a byte-reproducible committed fixture
// (SPEC §12): struct fields emit in declaration order, the responses are sorted by
// event_id, and encoding/json emits map keys (Params) in sorted order. The write
// is atomic (temp file + rename) so an interrupted record never leaves a half file.
func WriteRecorded(path string, f RecordedFile) error {
	f.sortResponses()
	data, err := marshalStable(f)
	if err != nil {
		return fmt.Errorf("agentclient: marshal recorded responses: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("agentclient: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+recordedFileName+".tmp-*")
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

// marshalStable encodes v as 2-space-indented JSON with a trailing newline and
// HTML escaping disabled — the canonical on-disk form shared with the seeder's
// fixtures so the recorded file matches the project's committed-fixture style. It
// is defined here (not borrowed from internal/seed) to keep agentclient free of a
// seed dependency.
func marshalStable(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
