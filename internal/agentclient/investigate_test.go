package agentclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// sampleBreak is a receivable-residual BreakSummary used across the investigate
// tests.
func sampleBreak() BreakSummary {
	return BreakSummary{
		Key:        "check3:receivable-residual:",
		Check:      3,
		Kind:       "receivable-residual",
		Expected:   money.FromPaise(0),
		Actual:     money.FromPaise(248591),
		Candidates: []string{"setl_x"},
		Detail:     "receivable residual",
	}
}

// sampleResolutionFile builds a recorded fixture with one refund_reversal
// resolution for the sample break.
func sampleResolutionFile() RecordedInvestigateFile {
	return RecordedInvestigateFile{
		SchemaVersion: InvestigateRecordedSchemaVersion,
		World:         "dtc",
		Period:        "2026-03",
		Resolutions: []RecordedResolution{{
			BreakKey: "check3:receivable-residual:",
			Resolution: []RecordedPosting{{
				EventID:   "rfnd_1",
				EntryType: "refund_reversal",
				Params:    map[string]int64{"net": 210670, "gst": 37921, "refund_id": 0},
			}},
			Rationale: "recovered the unbooked refund",
			ToolsUsed: []string{"orders.fetch"},
		}},
	}
}

// TestReplayInvestigateResolution replays a recorded resolution and checks the
// postings, trace, and money conversion (int64 paise -> money.Money).
func TestReplayInvestigateResolution(t *testing.T) {
	c := NewReplayInvestigateClient(sampleResolutionFile())
	if c.Mode() != ModeReplay {
		t.Fatalf("Mode = %q, want replay", c.Mode())
	}
	out, tr, err := c.Investigate(sampleBreak(), nil)
	if err != nil {
		t.Fatalf("Investigate: %v", err)
	}
	if !out.Resolvable() {
		t.Fatalf("result not resolvable: %+v", out)
	}
	if len(out.Resolution) != 1 {
		t.Fatalf("got %d postings, want 1", len(out.Resolution))
	}
	p := out.Resolution[0]
	if p.EventID != "rfnd_1" || p.EntryType != "refund_reversal" {
		t.Errorf("posting = %+v, want refund_reversal for rfnd_1", p)
	}
	if got := p.Params["net"]; got != money.FromPaise(210670) {
		t.Errorf("net = %s, want 2106.70", got)
	}
	if tr.SchemaVersion != InvestigateTraceSchemaVersion || tr.BreakKey != "check3:receivable-residual:" {
		t.Errorf("trace not stamped correctly: %+v", tr)
	}
	if len(tr.Decision.Resolution) != 1 {
		t.Errorf("trace decision missing the posting: %+v", tr.Decision)
	}
	if tr.ToolsUsed == nil || tr.Candidates == nil {
		t.Errorf("trace tools_used/candidates must be non-nil for stable JSON")
	}
}

// TestReplayInvestigateEscalation replays a recorded escalation.
func TestReplayInvestigateEscalation(t *testing.T) {
	f := RecordedInvestigateFile{
		SchemaVersion: InvestigateRecordedSchemaVersion,
		Resolutions: []RecordedResolution{{
			BreakKey: "check2:batch-sum-mismatch:setl_x",
			Escalate: true,
			Reason:   "cannot resolve a batch-sum break by posting",
		}},
	}
	c := NewReplayInvestigateClient(f)
	brk := BreakSummary{Key: "check2:batch-sum-mismatch:setl_x", Check: 2, Kind: "batch-sum-mismatch"}
	out, tr, err := c.Investigate(brk, nil)
	if err != nil {
		t.Fatalf("Investigate: %v", err)
	}
	if out.Resolvable() {
		t.Fatalf("escalation should not be resolvable: %+v", out)
	}
	if !out.Escalate || out.Reason == "" {
		t.Errorf("escalation not populated: %+v", out)
	}
	if tr.Rationale != out.Reason {
		t.Errorf("trace rationale = %q, want the escalate reason %q", tr.Rationale, out.Reason)
	}
}

// TestReplayInvestigateMissingRecord escalates (never invents) a break with no
// recorded resolution.
func TestReplayInvestigateMissingRecord(t *testing.T) {
	c := NewReplayInvestigateClient(RecordedInvestigateFile{SchemaVersion: InvestigateRecordedSchemaVersion})
	out, _, err := c.Investigate(sampleBreak(), nil)
	if err != nil {
		t.Fatalf("Investigate: %v", err)
	}
	if out.Resolvable() || !out.Escalate {
		t.Errorf("missing record must escalate, got %+v", out)
	}
}

// TestInvestigateRecordRoundTrip writes then reads the fixture and checks it round
// trips and is byte-stable across two writes.
func TestInvestigateRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "investigate.recorded.json")
	in := sampleResolutionFile()
	if err := WriteInvestigateRecorded(path, in); err != nil {
		t.Fatalf("WriteInvestigateRecorded: %v", err)
	}
	got, err := ReadInvestigateRecorded(path)
	if err != nil {
		t.Fatalf("ReadInvestigateRecorded: %v", err)
	}
	if len(got.Resolutions) != 1 || got.Resolutions[0].BreakKey != "check3:receivable-residual:" {
		t.Fatalf("round trip lost the resolution: %+v", got)
	}
	// Byte-stable: a second write of the read-back value equals the first.
	path2 := filepath.Join(dir, "investigate.recorded.2.json")
	if err := WriteInvestigateRecorded(path2, got); err != nil {
		t.Fatalf("second write: %v", err)
	}
	a, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	b, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("read %s: %v", path2, err)
	}
	if string(a) != string(b) {
		t.Errorf("recorded investigation fixture is not byte-stable across writes")
	}
}

// TestLiveInvestigateAndRecord posts to a mock Flue endpoint, checks the request
// shape and decoded result, and that the recorded response replays equal.
func TestLiveInvestigateAndRecord(t *testing.T) {
	var gotReq investigateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != investigateEndpoint {
			t.Errorf("path = %q, want %q", r.URL.Path, investigateEndpoint)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(InvestigateResult{
			Resolution: []Posting{{
				EventID:   "rfnd_1",
				EntryType: "refund_reversal",
				Params:    map[string]money.Money{"net": money.FromPaise(210670), "gst": money.FromPaise(37921), "refund_id": money.FromPaise(0)},
			}},
			Rationale: "live recovered",
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	recPath := filepath.Join(dir, "investigate.recorded.json")
	live := NewLiveInvestigateClient(srv.URL, "dtc", "2026-03", recPath, srv.Client())

	brk := sampleBreak()
	cands := []EventSummary{{EventID: "rfnd_1", Type: "refund", Amount: money.FromPaise(248591)}}
	out, tr, err := live.Investigate(brk, cands)
	if err != nil {
		t.Fatalf("live Investigate: %v", err)
	}
	if gotReq.Break.Key != brk.Key || len(gotReq.Candidates) != 1 {
		t.Errorf("request body not shaped as {break, candidates}: %+v", gotReq)
	}
	if !out.Resolvable() || out.Resolution[0].Params["net"] != money.FromPaise(210670) {
		t.Errorf("decoded result wrong: %+v", out)
	}
	if tr.Mode != ModeLive {
		t.Errorf("trace mode = %q, want live", tr.Mode)
	}

	// Flush and replay: the recorded response must reproduce the live result.
	if err := live.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	rc, err := NewReplayInvestigateClientFromPath(recPath)
	if err != nil {
		t.Fatalf("replay from recorded: %v", err)
	}
	rout, _, err := rc.Investigate(brk, nil)
	if err != nil {
		t.Fatalf("replay Investigate: %v", err)
	}
	if !rout.Resolvable() || rout.Resolution[0].EventID != "rfnd_1" {
		t.Errorf("replayed result does not match the recorded live response: %+v", rout)
	}
}
