package agentclient

import (
	"strings"
	"testing"
)

// TestGenerateRecordedPartialRefunds pins the deterministic generator's behavior
// on the partial-refund world (dtc/2026-01): R1 (amount == a line item) is booked
// as a refund_reversal at the item's rate with the item citation in its
// rationale; R2 (annotated goodwill) and R3 (matches nothing) are recorded
// escalations — the agent never books goodwill, and never guesses.
func TestGenerateRecordedPartialRefunds(t *testing.T) {
	const root = "../.."
	f, err := GenerateRecorded(root, "dtc", "2026-01")
	if err != nil {
		t.Fatalf("GenerateRecorded: %v", err)
	}
	byID := map[string]RecordedResponse{}
	for _, r := range f.Responses {
		byID[r.EventID] = r
	}
	if len(f.Responses) != 3 {
		t.Fatalf("responses = %d, want 3 (the three partial refunds)", len(f.Responses))
	}

	r1 := byID["rfnd_ZtHFpyTP2I9NSz"] // item-match: 28128 @18 -> net 23837, gst 4291
	if r1.Unclassifiable || r1.EntryType != "refund_reversal" {
		t.Fatalf("R1 = %+v, want refund_reversal", r1)
	}
	if r1.Params["net"] != 23837 || r1.Params["gst"] != 4291 || r1.Params["refund_id"] != 0 {
		t.Errorf("R1 params = %v, want net=23837 gst=4291 refund_id=0", r1.Params)
	}
	if !strings.Contains(r1.Rationale, "items.0") && !strings.Contains(r1.Rationale, "line item") {
		t.Errorf("R1 rationale %q does not cite the matched item", r1.Rationale)
	}

	r2 := byID["rfnd_YODsk1r0xPB47u"] // goodwill-annotated
	if !r2.Unclassifiable || !strings.Contains(r2.Reason, "goodwill") {
		t.Errorf("R2 = %+v, want a goodwill escalation", r2)
	}

	r3 := byID["rfnd_jWwN1ErzEObBK2"] // unexplained
	if !r3.Unclassifiable {
		t.Errorf("R3 = %+v, want an escalation", r3)
	}
}
