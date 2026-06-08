package classifyq

import (
	"fmt"
)

// review.go is the human-in-the-loop REVIEW gate the APPLY stage runs AFTER the
// (automatic) Validator and BEFORE deriving money + posting. It is a swappable
// seam, exactly like the agent transport: an Auto reviewer (approve everything —
// the default, today's behavior) and a Recorded reviewer (replay committed human
// verdicts for deterministic CI / audit). An Interactive reviewer (prompt a human)
// is a later drop-in that satisfies the same interface.
//
// v1 verdicts are approve / reject (an Edit verdict — a human tweaking the recovered
// rate — is a natural later addition). A rejected proposal is skipped (never posted),
// with the reason recorded — the same honest "list it, don't guess" discipline.

// Verdict is a reviewer's decision on one proposed Result.
type Verdict string

const (
	// VerdictApprove: book the proposal (after deriving money).
	VerdictApprove Verdict = "approve"
	// VerdictReject: do not book; skip the event and record the reason.
	VerdictReject Verdict = "reject"
)

// Decision is a reviewer's verdict plus an optional human note/reason.
type Decision struct {
	Verdict Verdict
	Reason  string
}

// Reviewer decides whether a proposed Result should be booked. It is consulted once
// per proposal at APPLY time.
type Reviewer interface {
	Review(r Result) Decision
}

// AutoReviewer approves every proposal — the default (it makes the async pipeline
// behave like the inline path, where the agent's accepted output is booked
// directly). It is the zero-friction reviewer for CI and for runs with no human gate.
type AutoReviewer struct{}

// Review approves unconditionally.
func (AutoReviewer) Review(Result) Decision { return Decision{Verdict: VerdictApprove} }

// Approval is one recorded human verdict keyed to an event_id (the committed form a
// Recorded reviewer replays). Reviewer/At record who decided and when, for the
// audit trail.
type Approval struct {
	EventID  string  `json:"event_id"`
	Verdict  Verdict `json:"verdict"`
	Reason   string  `json:"reason,omitempty"`
	Reviewer string  `json:"reviewer,omitempty"`
	At       string  `json:"at,omitempty"`
}

// ApprovalsFile is the on-disk approvals store the Recorded reviewer reads.
type ApprovalsFile struct {
	SchemaVersion int        `json:"schema_version"`
	World         string     `json:"world"`
	Period        string     `json:"period"`
	Approvals     []Approval `json:"approvals"`
}

// RecordedReviewer replays committed human verdicts keyed by event_id, for
// deterministic CI and audit. A proposal with NO recorded verdict is rejected by
// default ("no recorded approval") — fail-closed, never auto-approve an unreviewed
// proposal.
type RecordedReviewer struct {
	index map[string]Approval
}

// NewRecordedReviewer builds a Recorded reviewer from an approvals file.
func NewRecordedReviewer(f ApprovalsFile) *RecordedReviewer {
	m := make(map[string]Approval, len(f.Approvals))
	for _, a := range f.Approvals {
		if _, dup := m[a.EventID]; !dup {
			m[a.EventID] = a
		}
	}
	return &RecordedReviewer{index: m}
}

// Review returns the recorded verdict for the event, or a fail-closed reject when
// none is recorded.
func (rr *RecordedReviewer) Review(r Result) Decision {
	a, ok := rr.index[r.EventID]
	if !ok {
		return Decision{Verdict: VerdictReject, Reason: "no recorded approval for " + r.EventID}
	}
	reason := a.Reason
	if a.Verdict == VerdictReject && reason == "" {
		reason = fmt.Sprintf("rejected by %s", a.Reviewer)
	}
	return Decision{Verdict: a.Verdict, Reason: reason}
}

// ReadApprovals loads an approvals store from path.
func ReadApprovals(path string) (ApprovalsFile, error) {
	var f ApprovalsFile
	if err := readStrict(path, &f); err != nil {
		return ApprovalsFile{}, err
	}
	if f.SchemaVersion != SchemaVersion {
		return ApprovalsFile{}, fmt.Errorf("classifyq: %s schema_version=%d, want %d", path, f.SchemaVersion, SchemaVersion)
	}
	return f, nil
}
