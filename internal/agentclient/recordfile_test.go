package agentclient

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecordedFileRoundTrip asserts the recorded-response fixture writes and reads
// back losslessly through the frozen on-disk schema, and that the write is
// byte-stable (two writes of the same file produce identical bytes — sorted
// responses, sorted map keys, SPEC §12).
func TestRecordedFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, agentDir, recordedFileName)

	// Insert out of event-id order to prove the writer sorts.
	f := RecordedFile{
		SchemaVersion: RecordedSchemaVersion,
		World:         "dtc",
		Period:        "2026-04",
		Responses: []RecordedResponse{
			{EventID: "pay_Z", EntryType: "dtc_sale", Params: map[string]int64{"gross": 100, "net": 90, "gst": 10}},
			{EventID: "pay_A", Unclassifiable: true, Reason: "no rate"},
		},
	}
	if err := WriteRecorded(path, f); err != nil {
		t.Fatalf("WriteRecorded: %v", err)
	}

	got, err := ReadRecorded(path)
	if err != nil {
		t.Fatalf("ReadRecorded: %v", err)
	}
	if got.SchemaVersion != RecordedSchemaVersion || got.World != "dtc" || got.Period != "2026-04" {
		t.Errorf("header mismatch: %+v", got)
	}
	if len(got.Responses) != 2 {
		t.Fatalf("responses = %d, want 2", len(got.Responses))
	}
	// Responses are sorted by event_id on write.
	if got.Responses[0].EventID != "pay_A" || got.Responses[1].EventID != "pay_Z" {
		t.Errorf("responses not sorted by event_id: %q, %q", got.Responses[0].EventID, got.Responses[1].EventID)
	}

	// The index resolves both events.
	idx := got.index()
	if _, ok := idx["pay_A"]; !ok {
		t.Errorf("index missing pay_A")
	}
	if r := idx["pay_Z"]; r.EntryType != "dtc_sale" || r.Params["gross"] != 100 {
		t.Errorf("pay_Z recovered wrong: %+v", r)
	}

	// Byte-stability: re-marshal and compare bytes.
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	if err := WriteRecorded(path, got); err != nil {
		t.Fatalf("re-write: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("recorded file not byte-stable across two writes:\n--- 1 ---\n%s\n--- 2 ---\n%s", first, second)
	}
}

// TestReadRecordedMissing asserts a missing fixture is a clear error (not silently
// empty), and a wrong schema version is rejected.
func TestReadRecordedMissing(t *testing.T) {
	if _, err := ReadRecorded(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("ReadRecorded on a missing file did not error")
	}

	dir := t.TempDir()
	bad := filepath.Join(dir, recordedFileName)
	if err := os.WriteFile(bad, []byte(`{"schema_version":999,"world":"dtc","period":"2026-04","responses":[]}`), 0o644); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	if _, err := ReadRecorded(bad); err == nil {
		t.Error("ReadRecorded accepted a wrong schema_version")
	}
}

// TestNewReplayClientFromPathMissing asserts the replay client surfaces a missing
// fixture as an error rather than answering every event unclassifiable.
func TestNewReplayClientFromPathMissing(t *testing.T) {
	if _, err := NewReplayClientFromPath(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("NewReplayClientFromPath on a missing file did not error")
	}
}
