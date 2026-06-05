package agentclient

import (
	"os"
	"testing"

	"github.com/razorpay/close-agent/internal/seed"
)

// hard period under test: the committed missing-metadata period (SPEC §11 Phase 7a).
const (
	hardWorld  = "dtc"
	hardPeriod = "2026-04"
	repoRoot   = "../.." // module root relative to internal/agentclient
)

// seedHard seeds the hard period (with the ambiguity transform) into a fresh temp
// root and returns it, so the recovery generator runs against a self-contained
// period and never touches the repo's worlds/.
func seedHard(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	res, err := seed.SeedWith(root, hardWorld, hardPeriod, seed.Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("seed hard: %v", err)
	}
	if res.Ambiguity.NumStripped == 0 {
		t.Fatal("seeded hard period stripped no gst_rate; not actually hard")
	}
	return root
}

// TestGenerateRecoveredFromOrders is the core recovery gate: every rule-missed
// (gst_rate-stripped) payment is recovered from its order into a dtc_sale whose
// params satisfy net+gst==gross, and the count matches the stripped payments. The
// recovery reads orders.json (the legitimate source), NOT truth.
func TestGenerateRecoveredFromOrders(t *testing.T) {
	root := seedHard(t)

	f, err := GenerateRecorded(root, hardWorld, hardPeriod)
	if err != nil {
		t.Fatalf("GenerateRecorded: %v", err)
	}
	if f.SchemaVersion != RecordedSchemaVersion {
		t.Errorf("schema_version = %d, want %d", f.SchemaVersion, RecordedSchemaVersion)
	}
	if len(f.Responses) == 0 {
		t.Fatal("no recovered responses; the hard period must have rule misses")
	}
	for _, r := range f.Responses {
		if r.Unclassifiable {
			t.Errorf("event %s could not be recovered: %s", r.EventID, r.Reason)
			continue
		}
		if r.EntryType != "dtc_sale" {
			t.Errorf("event %s entry_type = %q, want dtc_sale", r.EventID, r.EntryType)
		}
		gross, net, gst := r.Params["gross"], r.Params["net"], r.Params["gst"]
		if gross == 0 {
			t.Errorf("event %s has zero gross", r.EventID)
		}
		if net+gst != gross {
			t.Errorf("event %s net+gst (%d+%d) != gross %d", r.EventID, net, gst, gross)
		}
		if len(r.ToolsUsed) != 1 || r.ToolsUsed[0] != orderFetchTool {
			t.Errorf("event %s tools_used = %v, want [%s]", r.EventID, r.ToolsUsed, orderFetchTool)
		}
	}
}

// TestGenerateRecoveredDeterministic asserts the generator is deterministic: two
// runs over the same seeded period produce byte-identical recorded files (the
// reproducibility the committed fixture relies on, SPEC §12).
func TestGenerateRecoveredDeterministic(t *testing.T) {
	root := seedHard(t)

	a, err := GenerateRecorded(root, hardWorld, hardPeriod)
	if err != nil {
		t.Fatalf("GenerateRecorded a: %v", err)
	}
	b, err := GenerateRecorded(root, hardWorld, hardPeriod)
	if err != nil {
		t.Fatalf("GenerateRecorded b: %v", err)
	}
	ba, err := marshalStable(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bb, err := marshalStable(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	if string(ba) != string(bb) {
		t.Errorf("recorded file not byte-identical across two generations:\n--- A ---\n%s\n--- B ---\n%s", ba, bb)
	}
}

// TestCommittedRecordedByteIdentical asserts the COMMITTED classify.recorded.json
// for the hard period is byte-identical to a fresh generation from the committed
// fixtures (SPEC §2, §12). If this fails, either the committed recorded file or the
// hard substrate drifted; regenerate via `close-agent record-responses`. Skipped
// when the committed fixtures are not present (a stripped checkout).
func TestCommittedRecordedByteIdentical(t *testing.T) {
	committedPath := RecordedPath(repoRoot, hardWorld, hardPeriod)
	onDisk, err := os.ReadFile(committedPath)
	if err != nil {
		t.Skipf("committed recorded responses not present (%v); skipping byte-identity guard", err)
	}

	f, err := GenerateRecorded(repoRoot, hardWorld, hardPeriod)
	if err != nil {
		t.Fatalf("GenerateRecorded from committed fixtures: %v", err)
	}
	fresh, err := marshalStable(f)
	if err != nil {
		t.Fatalf("marshal fresh: %v", err)
	}
	if string(fresh) != string(onDisk) {
		t.Errorf("committed classify.recorded.json is no longer byte-identical to a fresh generation;\nrun: close-agent record-responses --world %s --period %s", hardWorld, hardPeriod)
	}
}

// TestRoundTripThroughReplay asserts the full loop: generate the recorded file
// from orders, then replay it — every recovered event classifies the same way the
// rule engine would have, so the recorded fixture is a faithful agent stand-in.
func TestRoundTripThroughReplay(t *testing.T) {
	root := seedHard(t)
	f, err := GenerateRecorded(root, hardWorld, hardPeriod)
	if err != nil {
		t.Fatalf("GenerateRecorded: %v", err)
	}
	c := NewReplayClient(f)
	for _, r := range f.Responses {
		ev := EventSummary{EventID: r.EventID, Type: "payment"}
		res, _, err := c.Classify(ev)
		if err != nil {
			t.Fatalf("replay %s: %v", r.EventID, err)
		}
		if !res.Classifiable() {
			t.Errorf("recorded event %s did not classify on replay: %+v", r.EventID, res)
			continue
		}
		if res.Params["net"].Add(res.Params["gst"]) != res.Params["gross"] {
			t.Errorf("event %s replayed params do not balance", r.EventID)
		}
	}
}
