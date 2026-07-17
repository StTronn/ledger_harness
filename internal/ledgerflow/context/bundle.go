package flowcontext

import (
	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/posting"
	"github.com/razorpay/ledger-flow/internal/ledgerflow/recovery/policychecks"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/reconcile"
)

// bundle.go builds the Tier-1 CONTEXT BUNDLES the agent harness ships to the §8
// agents: a single pre-joined view that collapses the agent's multi-hop graph walk
// to zero hops for the common case (a rule-missed event, a reconcile break). The
// bundles carry the two derived facts only the read model can cheaply compute —
// `booked` and the recovered gst_rate (with a citation the apply stage can
// re-verify) — alongside the raw event/settlement and the applicable entry types.
//
// The shapes here are the JSON contract the `close` read CLI prints and the agent
// consumes (option-2 CLI-as-tool). money.Money marshals as integer paise.

// Citation / candidate types are defined ONCE in the policy layer
// (internal/harness/policychecks) and aliased here, so the bundle JSON and the
// policies that produce its contents can never drift.
type (
	Citation        = policychecks.Citation
	CandidateKind   = policychecks.CandidateKind
	RefundCandidate = policychecks.Candidate
)

const (
	CandidateItemMatch = policychecks.CandidateItemMatch
	CandidatePairMatch = policychecks.CandidatePairMatch
	CandidateNoMatch   = policychecks.CandidateNoMatch
)

// RecoveredRate is the bundle's LEGACY shape for the gst_rate recovery (the §8
// brains and the run-view UI read recovered.gst_rate); Policy tags which registry
// entry produced it. New, non-rate facts ride the bundle's generic Facts list.
type RecoveredRate struct {
	GSTRate string   `json:"gst_rate"`
	Source  Citation `json:"_source"`
	Policy  string   `json:"policy,omitempty"`
}

// EventView is the agent-facing snapshot of one event in a bundle: its id, type,
// gross amount, links, the event's OWN (possibly empty) gst_rate and sku, whether
// an entry has already been posted for it, and — for a PARTIAL refund — the
// parent payment's gross plus any ops annotation (reason) the refund carries.
type EventView struct {
	EventID      string       `json:"event_id"`
	Type         string       `json:"type"`
	Amount       money.Money  `json:"amount"`
	OrderID      string       `json:"order_id,omitempty"`
	GSTRate      string       `json:"gst_rate,omitempty"`
	SKU          string       `json:"sku,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	ParentAmount *money.Money `json:"parent_amount,omitempty"`
	Booked       bool         `json:"booked"`
}

// EventContextBundle is the Tier-1 bundle for the §8 classify seam: everything the
// agent needs to classify one rule-missed event in a single shot — the event, the
// recovered rate (when its own was stripped), the order's line items plus the
// precomputed match candidates (for a partial refund), and the entry type(s) that
// apply.
type EventContextBundle struct {
	Event                EventView           `json:"event"`
	Recovered            *RecoveredRate      `json:"recovered,omitempty"`
	OrderItems           []OrderItem         `json:"order_items,omitempty"`
	Candidates           []RefundCandidate   `json:"candidates,omitempty"`
	Facts                []policychecks.Fact `json:"facts,omitempty"`
	ApplicableEntryTypes []string            `json:"applicable_entry_types"`
}

// BatchMember is one payment/refund in a settlement's batch, as the investigate
// agent sees it: the event, whether it is booked, and its rate (own or recovered).
// An unbooked member with a recovered rate is the smoking gun for a "settled but
// not booked" residual.
type BatchMember struct {
	EventID string      `json:"event_id"`
	Type    string      `json:"type"`
	Amount  money.Money `json:"amount"`
	Booked  bool        `json:"booked"`
	GSTRate string      `json:"gst_rate,omitempty"`
	// ParentAmount marks a PARTIAL refund (set to the parent payment's gross):
	// the investigate agent must NOT book a reversal for it — partial intent is a
	// classify-side judgment (return vs goodwill), so it escalates instead.
	ParentAmount *money.Money   `json:"parent_amount,omitempty"`
	Recovered    *RecoveredRate `json:"recovered,omitempty"`
}

// BreakView is the agent-facing snapshot of one reconcile break.
type BreakView struct {
	Key          string      `json:"key"`
	Check        int         `json:"check"`
	Kind         string      `json:"kind"`
	SettlementID string      `json:"settlement_id,omitempty"`
	Expected     money.Money `json:"expected"`
	Actual       money.Money `json:"actual"`
	Detail       string      `json:"detail,omitempty"`
}

// SettlementView is the agent-facing snapshot of the settlement a break concerns.
type SettlementView struct {
	ID         string      `json:"id"`
	Amount     money.Money `json:"amount"`
	UTR        string      `json:"utr"`
	PaymentIDs []string    `json:"payment_ids"`
	RefundIDs  []string    `json:"refund_ids"`
}

// DeductionView is one courier-remittance deduction line as the investigate agent
// sees it (ROADMAP §8.3): the charge's id (the entry's event id when booked), its
// code, the shipment it concerns and that shipment's lifecycle status, the gross
// deducted, and the rate-card verdict — whether it is bookable (and as what, at
// what rate, with what citation) or must be escalated. It is the COD analogue of a
// RefundCandidate: the pre-solved evidence the agent judges.
type DeductionView struct {
	ID             string      `json:"id"`
	Code           string      `json:"code"`
	ShipmentID     string      `json:"shipment_id"`
	ShipmentStatus string      `json:"shipment_status,omitempty"`
	Amount         money.Money `json:"amount"`
	RateCardBacked bool        `json:"rate_card_backed"`
	EntryType      string      `json:"entry_type,omitempty"`
	GSTRate        string      `json:"gst_rate,omitempty"`
	Source         Citation    `json:"_source,omitempty"`
	Note           string      `json:"note,omitempty"`
}

// ReconContextBundle is the Tier-1 bundle for the §8 investigate seam: the break,
// the settlement it concerns, every batch member with its booked/recovered facts,
// the break's candidate event ids, the entry types that could resolve it, and the
// relevant account balance(s). For a COD residual break it also carries the
// remittance's Deductions, each pre-classified against the rate card.
type ReconContextBundle struct {
	Break                BreakView              `json:"break"`
	Settlement           *SettlementView        `json:"settlement,omitempty"`
	Batch                []BatchMember          `json:"batch"`
	Deductions           []DeductionView        `json:"deductions,omitempty"`
	Candidates           []string               `json:"candidates"`
	ApplicableEntryTypes []string               `json:"applicable_entry_types"`
	Accounts             map[string]money.Money `json:"accounts"`
}

// receivableAccount is the chart path the check-3 residual concerns; surfaced in the
// recon bundle so the agent sees the balance it must clear. Named here (not read
// from truth) to keep the projection truth-free.
const receivableAccount = "assets/razorpay-settlement-receivable"

// codReceivableAccount is the COD-rail receivable surfaced in a COD residual
// bundle so the agent sees the balance its postings must clear (ROADMAP §8.3).
const codReceivableAccount = "assets/cod-receivable"

// policyEvent projects a normalized event onto the policy layer's input.
func policyEvent(ev ingest.NormalizedEvent) policychecks.Event {
	pe := policychecks.Event{
		EventID:      ev.ID,
		Type:         string(ev.Type),
		Amount:       ev.Amount,
		ParentAmount: ev.ParentAmount,
		Fee:          ev.Fee,
		Tax:          ev.Tax,
	}
	if ev.Notes != nil {
		pe.GSTRate = ev.Notes.GSTRate
		pe.Reason = ev.Notes.Reason
	}
	return pe
}

// runPolicies runs the registry for one event and splits the finding into the
// bundle's channels: the legacy recovered-rate slot (the first VALID gst_rate
// fact — the shape the §8 brains and the run-view UI consume), the candidates
// list, and the generic facts list (everything else, including any fact that
// failed validation — visibly wrong beats silently dropped).
func (m *Graph) runPolicies(ev ingest.NormalizedEvent) (*RecoveredRate, []RefundCandidate, []policychecks.Fact) {
	f := m.policies.Run(policyEvent(ev), m)
	var recovered *RecoveredRate
	var rest []policychecks.Fact
	for _, fact := range f.Facts {
		if recovered == nil && fact.Field == "gst_rate" && fact.Valid {
			recovered = &RecoveredRate{GSTRate: fact.Value, Source: fact.Source, Policy: fact.Policy}
			continue
		}
		rest = append(rest, fact)
	}
	return recovered, f.Candidates, rest
}

// ownGSTRate returns an event's own gst_rate (possibly empty).
func ownGSTRate(ev ingest.NormalizedEvent) string {
	if ev.Notes == nil {
		return ""
	}
	return ev.Notes.GSTRate
}

// ownSKU returns an event's own sku (possibly empty).
func ownSKU(ev ingest.NormalizedEvent) string {
	if ev.Notes == nil {
		return ""
	}
	return ev.Notes.SKU
}

// orderIDForEvent resolves the parent order for a payment (1 hop) or a refund
// (2 hops via the parent payment).
func (m *Graph) orderIDForEvent(ev ingest.NormalizedEvent) string {
	switch ev.Type {
	case ingest.EventPayment:
		id, _ := m.OrderIDForPayment(ev.ID)
		return id
	case ingest.EventRefund:
		if payID, ok := m.PaymentIDForRefund(ev.ID); ok {
			id, _ := m.OrderIDForPayment(payID)
			return id
		}
	}
	return ""
}

// EventContext builds the Tier-1 classify bundle for one event id. ok is false when
// the event is unknown.
func (m *Graph) EventContext(eventID string) (EventContextBundle, bool) {
	ev, ok := m.Event(eventID)
	if !ok {
		return EventContextBundle{}, false
	}
	orderID := m.orderIDForEvent(ev)
	view := EventView{
		EventID:      ev.ID,
		Type:         string(ev.Type),
		Amount:       ev.Amount,
		OrderID:      orderID,
		GSTRate:      ownGSTRate(ev),
		SKU:          ownSKU(ev),
		Booked:       m.Booked(ev),
		ParentAmount: ev.ParentAmount,
	}
	if ev.Notes != nil {
		view.Reason = ev.Notes.Reason
	}
	recovered, candidates, facts := m.runPolicies(ev)
	b := EventContextBundle{
		Event:      view,
		Recovered:  recovered,
		Candidates: candidates,
		Facts:      facts,
	}
	if et, ok := posting.EntryTypeForEventType(ev.Type); ok {
		b.ApplicableEntryTypes = []string{et}
	}
	// PARTIAL refund: ship the matching substrate (the order's line items) and
	// widen the menu with the credit-note treatment — choosing among the policy
	// layer's candidates is exactly the judgment the agent owns.
	if ev.Type == ingest.EventRefund && ev.ParentAmount != nil && orderID != "" {
		b.OrderItems = m.OrderItems(orderID)
		b.ApplicableEntryTypes = append(b.ApplicableEntryTypes, "price_adjustment")
	}
	return b, true
}

// ReconciliationContext builds the Tier-1 investigate bundle for one break key. ok
// is false when no break with that key exists.
func (m *Graph) ReconciliationContext(breakKey string) (ReconContextBundle, bool) {
	brk, ok := m.Break(breakKey)
	if !ok {
		return ReconContextBundle{}, false
	}
	b := ReconContextBundle{
		Break: BreakView{
			Key:          brk.Key(),
			Check:        int(brk.Check),
			Kind:         brk.Kind,
			SettlementID: brk.SettlementID,
			Expected:     brk.Expected,
			Actual:       brk.Actual,
			Detail:       brk.Detail,
		},
		Candidates: brk.CandidateEventIDs,
		Accounts:   map[string]money.Money{receivableAccount: m.AccountBalance(receivableAccount)},
	}

	// Collect the settlements to expand: the break's own settlement (settlement-keyed
	// breaks like check #1/#2) plus any candidate ids that name a settlement (the
	// period-wide check #3 residual carries the settlement batches that did not
	// clear). The first settlement seen becomes the headline Settlement view.
	settleIDs := []string{}
	if brk.SettlementID != "" {
		settleIDs = append(settleIDs, brk.SettlementID)
	}
	settleIDs = append(settleIDs, brk.CandidateEventIDs...)

	seenMember := map[string]struct{}{}
	seenType := map[string]struct{}{}
	addMember := func(id string) {
		if _, dup := seenMember[id]; dup {
			return
		}
		ev, ok := m.Event(id)
		if !ok {
			return
		}
		seenMember[id] = struct{}{}
		recovered, _, _ := m.runPolicies(ev)
		mem := BatchMember{
			EventID: ev.ID, Type: string(ev.Type), Amount: ev.Amount,
			Booked: m.Booked(ev), GSTRate: ownGSTRate(ev), Recovered: recovered,
			ParentAmount: ev.ParentAmount,
		}
		b.Batch = append(b.Batch, mem)
		// Applicable resolution types: the entry types of UNBOOKED members.
		if !mem.Booked {
			if et, ok := posting.EntryTypeForEventType(ev.Type); ok {
				if _, dup := seenType[et]; !dup {
					seenType[et] = struct{}{}
					b.ApplicableEntryTypes = append(b.ApplicableEntryTypes, et)
				}
			}
		}
	}

	for _, id := range settleIDs {
		s, ok := m.settlements[id]
		if !ok {
			// A candidate that is an event (not a settlement) is itself a batch member.
			addMember(id)
			continue
		}
		if b.Settlement == nil {
			b.Settlement = &SettlementView{
				ID: s.ID, Amount: s.Amount, UTR: s.UTR,
				PaymentIDs: s.PaymentIDs, RefundIDs: s.RefundIDs,
			}
		}
		for _, mid := range append(append([]string{}, s.PaymentIDs...), s.RefundIDs...) {
			addMember(mid)
		}
	}

	// COD residual break (ROADMAP §8.3): decompose the remittance's deductions.
	// Each is pre-classified against the rate card via the SAME shared rule the
	// rto-fee policy uses, so the agent gets a bookable rto_fee (with its rate and
	// citation) or an explicit "escalate" — never an unexplained gap. cod-receivable
	// is surfaced so the agent sees the balance it must clear.
	if brk.Kind == reconcile.KindCODReceivableResidual {
		channel, _ := m.CourierChannel()
		rc, _ := m.RateCard()
		seenRem := map[string]struct{}{}
		for _, id := range settleIDs {
			if _, dup := seenRem[id]; dup {
				continue
			}
			deds, ok := m.RemittanceDeductions(id)
			if !ok {
				continue
			}
			seenRem[id] = struct{}{}
			for _, d := range deds {
				v := policychecks.ClassifyDeduction(d, channel, rc)
				b.Deductions = append(b.Deductions, DeductionView{
					ID: d.ID, Code: d.Code, ShipmentID: d.ShipmentID,
					ShipmentStatus: d.ShipmentStatus, Amount: d.Amount,
					RateCardBacked: v.Backed, EntryType: v.EntryType,
					GSTRate: v.GSTRate, Source: v.Source, Note: v.Note,
				})
				if v.Backed && v.EntryType != "" {
					if _, dup := seenType[v.EntryType]; !dup {
						seenType[v.EntryType] = struct{}{}
						b.ApplicableEntryTypes = append(b.ApplicableEntryTypes, v.EntryType)
					}
				}
			}
		}
		b.Accounts[codReceivableAccount] = m.AccountBalance(codReceivableAccount)
	}
	return b, true
}
