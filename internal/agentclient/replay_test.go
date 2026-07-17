package agentclient

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// fixtureFile builds a small recorded-response fixture for the replay tests: one
// recovered dtc_sale, and one recorded escalation, so both replay paths are
// covered from one table.
func fixtureFile() RecordedFile {
	return RecordedFile{
		SchemaVersion: RecordedSchemaVersion,
		World:         "dtc",
		Period:        "2026-04",
		Responses: []RecordedResponse{
			{
				EventID:   "pay_A",
				EntryType: "dtc_sale",
				Params:    map[string]int64{"gross": 236000, "net": 200000, "gst": 36000, "payment_id": 0},
				Rationale: "recovered gst_rate=18% from order",
				ToolsUsed: []string{orderFetchTool},
			},
			{
				EventID:        "pay_B",
				Unclassifiable: true,
				Reason:         "order had no usable gst_rate",
				ToolsUsed:      []string{orderFetchTool},
			},
		},
	}
}

// TestReplayClassify is the table-driven replay test (the module's core gate): a
// known recorded event replays its classification; a recorded escalation replays
// as unclassifiable; and an event with NO recorded entry is unclassifiable
// (never invented). Every case returns the FROZEN trace.
func TestReplayClassify(t *testing.T) {
	c := NewReplayClient(fixtureFile())

	cases := []struct {
		name           string
		event          EventSummary
		wantClass      bool   // expect a usable classification
		wantEntryType  string // when wantClass
		wantUnclass    bool   // expect an escalation
		wantToolsCount int    // tools_used length in the trace
	}{
		{
			name:           "recorded classification replays verbatim",
			event:          EventSummary{EventID: "pay_A", Type: "payment", Amount: money.FromPaise(236000)},
			wantClass:      true,
			wantEntryType:  "dtc_sale",
			wantToolsCount: 1,
		},
		{
			name:           "recorded escalation replays as unclassifiable",
			event:          EventSummary{EventID: "pay_B", Type: "payment", Amount: money.FromPaise(100000)},
			wantUnclass:    true,
			wantToolsCount: 1,
		},
		{
			name:           "unrecorded event is unclassifiable, never invented",
			event:          EventSummary{EventID: "pay_MISSING", Type: "payment", Amount: money.FromPaise(50000)},
			wantUnclass:    true,
			wantToolsCount: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, tr, err := c.Classify(tc.event)
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if tc.wantClass {
				if !res.Classifiable() {
					t.Fatalf("want classification, got %+v", res)
				}
				if res.EntryType != tc.wantEntryType {
					t.Errorf("entry_type = %q, want %q", res.EntryType, tc.wantEntryType)
				}
				// Params must round-trip from int64 paise to money.Money exactly.
				if got := res.Params["gross"]; got != money.FromPaise(236000) {
					t.Errorf("gross param = %s, want 2360.00", got)
				}
				if res.Params["net"].Add(res.Params["gst"]) != res.Params["gross"] {
					t.Errorf("net+gst != gross: %s + %s != %s",
						res.Params["net"], res.Params["gst"], res.Params["gross"])
				}
			}
			if tc.wantUnclass && res.Classifiable() {
				t.Fatalf("want unclassifiable, got classification %+v", res)
			}
			// The trace is always frozen-versioned, attributed to the event, in replay mode.
			if tr.SchemaVersion != TraceSchemaVersion {
				t.Errorf("trace schema_version = %d, want %d", tr.SchemaVersion, TraceSchemaVersion)
			}
			if tr.EventID != tc.event.EventID {
				t.Errorf("trace event_id = %q, want %q", tr.EventID, tc.event.EventID)
			}
			if tr.Mode != ModeReplay {
				t.Errorf("trace mode = %q, want replay", tr.Mode)
			}
			if tr.ToolsUsed == nil {
				t.Errorf("trace tools_used is nil; want a non-nil slice")
			}
			if len(tr.ToolsUsed) != tc.wantToolsCount {
				t.Errorf("trace tools_used = %v, want %d entries", tr.ToolsUsed, tc.wantToolsCount)
			}
		})
	}
}

// TestReplayDeterministic asserts replay is a pure function of the fixture: two
// Classify calls for the same event return identical results (no hidden state,
// no map-iteration leak) — the determinism the CI gate relies on (SPEC §12).
func TestReplayDeterministic(t *testing.T) {
	c := NewReplayClient(fixtureFile())
	ev := EventSummary{EventID: "pay_A", Type: "payment", Amount: money.FromPaise(236000)}

	a, _, err := c.Classify(ev)
	if err != nil {
		t.Fatalf("Classify a: %v", err)
	}
	b, _, err := c.Classify(ev)
	if err != nil {
		t.Fatalf("Classify b: %v", err)
	}
	if a.EntryType != b.EntryType || len(a.Params) != len(b.Params) {
		t.Fatalf("replay not deterministic: %+v vs %+v", a, b)
	}
	for k, v := range a.Params {
		if b.Params[k] != v {
			t.Errorf("param %q differs: %s vs %s", k, v, b.Params[k])
		}
	}
}

// TestReplayMode asserts the replay client reports its mode.
func TestReplayMode(t *testing.T) {
	if got := NewReplayClient(fixtureFile()).Mode(); got != ModeReplay {
		t.Errorf("Mode() = %q, want replay", got)
	}
}
