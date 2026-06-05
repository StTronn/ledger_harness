package agentclient

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/razorpay/close-agent/internal/money"
)

// TestTraceRoundTrips asserts the FROZEN trace marshals and unmarshals losslessly
// through JSON (the learning seam's contract, SPEC §9, §13): a trace built from a
// classification survives a round trip with its schema version, event id, mode,
// input, tools, decision, and rationale intact.
func TestTraceRoundTrips(t *testing.T) {
	in := EventSummary{
		EventID: "pay_A",
		Type:    "payment",
		Amount:  money.FromPaise(236000),
		OrderID: "order_A",
		SKU:     "SERUM-30",
	}
	res := ClassifyResult{
		EntryType: "dtc_sale",
		Params:    map[string]money.Money{"gross": money.FromPaise(236000), "net": money.FromPaise(200000), "gst": money.FromPaise(36000)},
		Rationale: "recovered gst_rate=18% from order",
	}
	tr := newTrace(ModeReplay, in, []string{orderFetchTool}, res)

	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	var got Trace
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal trace: %v", err)
	}

	if got.SchemaVersion != TraceSchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, TraceSchemaVersion)
	}
	if got.EventID != in.EventID {
		t.Errorf("event_id = %q, want %q", got.EventID, in.EventID)
	}
	if got.Mode != ModeReplay {
		t.Errorf("mode = %q, want replay", got.Mode)
	}
	if got.Input != in {
		t.Errorf("input = %+v, want %+v", got.Input, in)
	}
	if got.Decision.EntryType != "dtc_sale" {
		t.Errorf("decision.entry_type = %q, want dtc_sale", got.Decision.EntryType)
	}
	if got.Decision.Params["gross"] != money.FromPaise(236000) {
		t.Errorf("decision gross = %s, want 2360.00", got.Decision.Params["gross"])
	}
	if got.Rationale != res.Rationale {
		t.Errorf("rationale = %q, want %q", got.Rationale, res.Rationale)
	}
	if len(got.ToolsUsed) != 1 || got.ToolsUsed[0] != orderFetchTool {
		t.Errorf("tools_used = %v, want [%s]", got.ToolsUsed, orderFetchTool)
	}
}

// TestTraceEscalationHasNoDecision asserts an escalation trace carries the reason
// in rationale and an empty decision (no entry type, no params), so the learning
// layer can tell a recovery from an escalation by the decision being empty.
func TestTraceEscalationHasNoDecision(t *testing.T) {
	in := EventSummary{EventID: "pay_B", Type: "payment", Amount: money.FromPaise(100000)}
	tr := newTrace(ModeReplay, in, []string{orderFetchTool}, Unclassified("no usable gst_rate"))

	if tr.Decision.EntryType != "" || len(tr.Decision.Params) != 0 {
		t.Errorf("escalation trace has a non-empty decision: %+v", tr.Decision)
	}
	if tr.Rationale != "no usable gst_rate" {
		t.Errorf("escalation rationale = %q, want the reason", tr.Rationale)
	}
}

// TestTraceToolsUsedNeverNil asserts a trace built with nil tools serialises
// tools_used as [] (never null/absent), keeping the frozen schema stable for the
// learning layer.
func TestTraceToolsUsedNeverNil(t *testing.T) {
	tr := newTrace(ModeReplay, EventSummary{EventID: "pay_X"}, nil, Unclassified("none"))
	if tr.ToolsUsed == nil {
		t.Fatalf("tools_used is nil, want non-nil empty slice")
	}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(data, []byte(`"tools_used":[]`)) {
		t.Errorf("tools_used did not serialise as []: %s", data)
	}
}
