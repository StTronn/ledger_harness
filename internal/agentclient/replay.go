package agentclient

import "github.com/razorpay/close-agent/internal/money"

// ReplayClient is the DEFAULT, CI-safe Client (SPEC §11 Phase 7, §12): it answers
// a Classify call from the committed recorded-response fixture
// (classify.recorded.json) keyed by event_id, with NO network, NO LLM, NO wall
// clock, and NO randomness. Given the same fixture it returns byte-identical
// results, which is what keeps the close deterministic in CI.
//
// It holds the parsed fixture as an immutable in-memory index, so repeated
// Classify calls are pure lookups. It never reads internal/truth — the recorded
// responses were generated from orders.json (the recovery source), not the answer
// key (SPEC §4.4).
type ReplayClient struct {
	index map[string]RecordedResponse
}

// NewReplayClient builds a ReplayClient from an already-loaded RecordedFile. It is
// the constructor tests use directly; NewReplayClientFromPath wraps file loading.
func NewReplayClient(f RecordedFile) *ReplayClient {
	return &ReplayClient{index: f.index()}
}

// NewReplayClientFromPath loads the recorded-response fixture at path and builds a
// ReplayClient over it. A missing or malformed fixture is surfaced as an error
// (the caller cannot replay a period that was never recorded), not silently
// treated as "every event unclassifiable".
func NewReplayClientFromPath(path string) (*ReplayClient, error) {
	f, err := ReadRecorded(path)
	if err != nil {
		return nil, err
	}
	return NewReplayClient(f), nil
}

// Mode reports replay.
func (c *ReplayClient) Mode() Mode { return ModeReplay }

// Classify returns the recorded classification for ev.EventID (SPEC §8). When the
// fixture has a recorded response for the event it is replayed verbatim — a
// classification ({entry_type, params, rationale}) or a recorded escalation
// ({unclassifiable, reason}). When the fixture has NO entry for the event, the
// result is an explicit {unclassifiable, reason} ("no recorded response …"): replay
// never invents an answer for an unrecorded event. The FROZEN trace is always
// returned, recording the replayed decision (or the missing-record escalation).
//
// It returns an error only if a recorded response is internally malformed (a
// classification with no params); a normal missing-record is an unclassifiable
// result, not an error.
func (c *ReplayClient) Classify(ev EventSummary) (ClassifyResult, Trace, error) {
	rec, ok := c.index[ev.EventID]
	if !ok {
		res := Unclassified("no recorded response for event " + ev.EventID)
		return res, newTrace(ModeReplay, ev, nil, res), nil
	}

	// A recorded escalation replays as an escalation.
	if rec.Unclassifiable {
		res := Unclassified(rec.Reason)
		return res, newTrace(ModeReplay, ev, rec.ToolsUsed, res), nil
	}

	// A recorded classification: rebuild the paise Params map (int64 on disk ->
	// money.Money in Go) and return it. An entry type with no params is a corrupt
	// record, surfaced as an error rather than posted as an empty entry.
	if rec.EntryType == "" {
		res := Unclassified("recorded response for " + ev.EventID + " has no entry_type")
		return res, newTrace(ModeReplay, ev, rec.ToolsUsed, res), nil
	}
	params := make(map[string]money.Money, len(rec.Params))
	for k, v := range rec.Params {
		params[k] = money.FromPaise(v)
	}
	res := ClassifyResult{
		EntryType: rec.EntryType,
		Params:    params,
		Rationale: rec.Rationale,
	}
	return res, newTrace(ModeReplay, ev, rec.ToolsUsed, res), nil
}
