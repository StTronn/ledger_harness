package agentclient

import (
	"encoding/json"

	"github.com/razorpay/ledger-flow/internal/money"
)

// ReplayInvestigateClient is the DEFAULT, CI-safe InvestigateClient (SPEC §11
// Phase 8, §12), parallel to ReplayClient: it answers an Investigate call from the
// committed investigate.recorded.json fixture keyed by break id, with NO network,
// NO LLM, NO wall clock, and NO randomness. Given the same fixture it returns
// byte-identical results. It never reads internal/truth — the recorded
// resolutions were generated from the snapshotted fixtures (refunds.json /
// orders.json), not the answer key (SPEC §4.4).
type ReplayInvestigateClient struct {
	index map[string]RecordedResolution
}

// NewReplayInvestigateClient builds a ReplayInvestigateClient from an
// already-loaded RecordedInvestigateFile. NewReplayInvestigateClientFromPath wraps
// file loading.
func NewReplayInvestigateClient(f RecordedInvestigateFile) *ReplayInvestigateClient {
	return &ReplayInvestigateClient{index: f.index()}
}

// NewReplayInvestigateClientFromPath loads the recorded-investigation fixture at
// path and builds a client over it. A missing or malformed fixture is surfaced as
// an error (a period with breaks cannot replay investigations it never recorded).
func NewReplayInvestigateClientFromPath(path string) (*ReplayInvestigateClient, error) {
	f, err := ReadInvestigateRecorded(path)
	if err != nil {
		return nil, err
	}
	return NewReplayInvestigateClient(f), nil
}

// Mode reports replay.
func (c *ReplayInvestigateClient) Mode() Mode { return ModeReplay }

// Investigate returns the recorded resolution for brk.Key (SPEC §8). A recorded
// resolution replays its postings verbatim (int64 paise -> money.Money); a
// recorded escalation replays as an escalation; and a break with NO recorded entry
// is an explicit escalation ("no recorded investigation …") — replay never invents
// a resolution. The FROZEN trace is always returned.
//
// It returns an error only if a recorded resolution is internally malformed (a
// non-escalation with no postings is treated as an escalation, not an error).
func (c *ReplayInvestigateClient) Investigate(brk BreakSummary, candidates []EventSummary, _ ...json.RawMessage) (InvestigateResult, InvestigateTrace, error) {
	rec, ok := c.index[brk.Key]
	if !ok {
		res := EscalatedInvestigation("no recorded investigation for break " + brk.Key)
		return res, newInvestigateTrace(ModeReplay, brk, candidates, nil, res), nil
	}

	// A recorded resolution may carry BOTH postings and an escalation: a COD
	// remittance break books the rate-card-backed deductions (rto_fee) AND
	// escalates the unverified one (the weight dispute) in a single investigation
	// (ROADMAP §8.3). Postings and escalation compose — the closer applies the
	// postings and records the escalation.
	postings := make([]Posting, 0, len(rec.Resolution))
	for _, p := range rec.Resolution {
		params := make(map[string]money.Money, len(p.Params))
		for k, v := range p.Params {
			params[k] = money.FromPaise(v)
		}
		postings = append(postings, Posting{EventID: p.EventID, EntryType: p.EntryType, Params: params})
	}
	if len(postings) == 0 && !rec.Escalate {
		res := EscalatedInvestigation("recorded investigation for " + brk.Key + " has no postings")
		return res, newInvestigateTrace(ModeReplay, brk, candidates, rec.ToolsUsed, res), nil
	}
	res := InvestigateResult{
		Resolution: postings,
		Rationale:  rec.Rationale,
		Escalate:   rec.Escalate,
		Reason:     rec.Reason,
	}
	return res, newInvestigateTrace(ModeReplay, brk, candidates, rec.ToolsUsed, res), nil
}
