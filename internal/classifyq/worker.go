package classifyq

import (
	"fmt"

	"github.com/razorpay/close-agent/internal/agentclient"
)

// worker.go is the async WORK stage — the "agent brain", v1 STUB edition. It drains
// the proposals store and, for each work item, recovers what the deterministic
// rules could not and writes a Result with a machine-checkable citation. The real
// LLM/Flue brain (later) drops into classifyOne's slot without changing the stage,
// the stores, or the downstream validation — that is the whole point of building
// the harness first.
//
// The stub brain models exactly what the live agent will do for the v1 long tail:
// a payment arrived with no gst_rate, so fetch its order and recover the rate. It
// emits the recovered FACT (rate) plus a citation; it does NOT compute money (the
// APPLY stage derives net/gst from the rate). Anything it cannot recover is an
// honest escalation, never a guess.

// orderFetchTool is the read-only tool the worker cites when it recovers a rate
// from an order.
const orderFetchTool = "orders.fetch"

// RunWorker is the async WORK stage: read the proposals store for (world, period),
// process every work item through the stub brain, and write the results store. It
// processes items sequentially (the architecture is decoupled/async; concurrency is
// a later knob). It returns the number of results written.
//
// The recovery source is orders.json (agentclient.OrderGSTRates) — an agent input,
// never truth.
func RunWorker(root, world, period string) (int, error) {
	pf, err := ReadProposals(ProposalsPath(root, world, period))
	if err != nil {
		return 0, err
	}
	rates, err := agentclient.OrderGSTRates(root, world, period)
	if err != nil {
		return 0, err
	}
	rf := ResultsFile{SchemaVersion: SchemaVersion, World: world, Period: period, Results: make([]Result, 0, len(pf.Items))}
	for _, item := range pf.Items {
		rf.Results = append(rf.Results, classifyOne(item, rates))
	}
	if err := WriteResults(ResultsPath(root, world, period), rf); err != nil {
		return 0, err
	}
	return len(rf.Results), nil
}

// classifyOne is the stub brain for a single work item. v1 recovers ONLY payments
// whose gst_rate was missing: it fetches the event's order, reads the true rate,
// and proposes a dtc_sale citing where the rate came from. Every other case — a
// non-payment, no order link, or an order with no rate — is a deterministic
// escalation with a clear reason.
func classifyOne(item WorkItem, rates map[string]string) Result {
	ev := item.Event
	if ev.Type != "payment" {
		return escalate(ev.EventID, fmt.Sprintf("v1 classify worker recovers only payments; %q is not supported", ev.Type))
	}
	if ev.OrderID == "" {
		return escalate(ev.EventID, "payment has no order_id to recover the gst_rate from")
	}
	rate, ok := rates[ev.OrderID]
	if !ok {
		return escalate(ev.EventID, fmt.Sprintf("no order %q to recover the gst_rate from", ev.OrderID))
	}
	if rate == "" {
		return escalate(ev.EventID, fmt.Sprintf("order %q has no gst_rate", ev.OrderID))
	}
	return Result{
		EventID:   ev.EventID,
		Status:    StatusProposed,
		EntryType: "dtc_sale",
		Recovered: []Recovered{{
			Field: "gst_rate",
			Value: rate,
			Source: Source{
				Tool:   orderFetchTool,
				Object: ev.OrderID,
				Path:   "notes.gst_rate",
			},
		}},
		ToolsUsed: []string{orderFetchTool},
		Rationale: fmt.Sprintf("payment %s arrived with no gst_rate; fetched order %s, recovered gst_rate=%s%% and proposed a dtc_sale",
			ev.EventID, ev.OrderID, rate),
	}
}

// escalate builds an escalated Result (the worker still consulted the order tool, so
// tools_used records the attempt).
func escalate(eventID, reason string) Result {
	return Result{EventID: eventID, Status: StatusEscalated, Reason: reason, ToolsUsed: []string{orderFetchTool}}
}
