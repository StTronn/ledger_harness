package policychecks

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// rtofee.go is the COD-rail recovery policy (ROADMAP §8.3): the registry row that
// turns a courier remittance's per-shipment deductions into evidence the §8
// investigate agent judges. It is the COD twin of feetier.go — same shape
// ("for THIS deduction, the authority is the rate card, and a sane value is the
// contracted RTO fee"), just on the courier rail.
//
// A remittance nets two kinds of deduction the deterministic rules cannot book:
//   - RTO_CHG: the return-to-origin fee. It is rate-card-backed — the contracted
//     RTO fee + GST — AND corroborated by the shipment's lifecycle (it must
//     actually be an RTO). When both hold, the policy says "book rto_fee", citing
//     ratecard/<channel>/rto_fee_paise.
//   - WT_ADJ (or any deduction the rate card does not explain): a weight-dispute
//     reweigh charge with no contracted basis and no supporting document in the
//     feed. The policy returns it as NOT backed — the honest "needs a human"
//     answer the agent escalates, never guesses.

// Deduction is the policy-input projection of one remittance deduction line: the
// charge's id (the booked entry's event id), its code, the shipment it concerns
// and that shipment's lifecycle status, and the gross deducted (paise).
type Deduction struct {
	ID             string
	Code           string
	ShipmentID     string
	Amount         money.Money
	ShipmentStatus string // "delivered" | "rto" | "" (unknown)
}

// DeductionVerdict is the shared classification of one deduction against the rate
// card — used both by the rtoFeeFromRateCard policy (to emit facts) and by the
// recon bundle (to surface structured, agent-ready deduction views). Defining it
// ONCE keeps the policy's facts and the bundle's views from drifting (the COD
// analogue of MatchLineItems being shared with the recorded-response generator).
type DeductionVerdict struct {
	EntryType string   // "rto_fee" when bookable; "" when it must be escalated
	GSTRate   string   // the rate to split the gross at (when bookable)
	Backed    bool     // rate-card-backed AND lifecycle-corroborated
	Source    Citation // provenance for the re-verifying validator
	Note      string   // human-readable rationale
}

// ClassifyDeduction is the single source of the deduction-classification rule,
// shared by the policy and the bundle. channel is the courier channel name; rc is
// the merchant's rate card. It never guesses: an unrecognised code, a rate-card
// miss, or a lifecycle mismatch all return Backed=false with a note explaining
// what is missing.
func ClassifyDeduction(d Deduction, channel string, rc feeds.RateCardFile) DeductionVerdict {
	ch, ok := rc.Channel(channel)
	if !ok {
		return DeductionVerdict{Note: fmt.Sprintf("no rate card for channel %q — cannot verify deduction", channel)}
	}
	if d.Code != "RTO_CHG" {
		return DeductionVerdict{
			Note: fmt.Sprintf("deduction %s has no rate-card basis (not a contracted charge); needs the courier's supporting document", d.Code),
		}
	}
	// RTO fee: contracted net + GST, and the shipment must actually be an RTO.
	expected := money.FromPaise(ch.RTOFeePaise + ch.RTOFeePaise*int64(ch.FeeGSTRate)/100)
	if d.Amount != expected {
		return DeductionVerdict{
			Note: fmt.Sprintf("RTO charge %s does not match the contracted RTO fee %s — escalate", d.Amount, expected),
		}
	}
	if d.ShipmentStatus != "rto" {
		return DeductionVerdict{
			Note: fmt.Sprintf("RTO charge on shipment %s whose status is %q, not rto — escalate", d.ShipmentID, d.ShipmentStatus),
		}
	}
	return DeductionVerdict{
		EntryType: "rto_fee",
		GSTRate:   fmt.Sprintf("%d", ch.FeeGSTRate),
		Backed:    true,
		Source:    Citation{Object: "ratecard/" + channel, Path: "rto_fee_paise"},
		Note: fmt.Sprintf("RTO fee matches the contracted rate card (%s) and shipment %s is confirmed returned to origin",
			expected, d.ShipmentID),
	}
}

// rtoFeeFromRateCard is the registry row. It applies to a cod_remittance event,
// walks that remittance's deductions through the read-only Graph lens, and emits
// one Fact per deduction: a VALID "rto_fee" fact for a rate-card-backed RTO
// charge (cited to the rate card), or an INVALID fact for anything it cannot
// verify (surfaced, never dropped — the agent escalates it).
type rtoFeeFromRateCard struct{}

func (rtoFeeFromRateCard) Name() string { return "rto-fee-from-ratecard" }

func (rtoFeeFromRateCard) AppliesTo(ev Event) bool { return ev.Type == "cod_remittance" }

func (p rtoFeeFromRateCard) Recover(ev Event, g Graph) Finding {
	deds, ok := g.RemittanceDeductions(ev.EventID)
	if !ok || len(deds) == 0 {
		return Finding{}
	}
	channel, _ := g.CourierChannel()
	rc, _ := g.RateCard()

	var facts []Fact
	for _, d := range deds {
		v := ClassifyDeduction(d, channel, rc)
		value := v.EntryType
		if value == "" {
			value = "escalate"
		}
		facts = append(facts, Fact{
			Field:  "deduction:" + d.ID,
			Value:  value,
			Source: v.Source,
			Valid:  v.Backed,
			Note:   v.Note,
			Policy: p.Name(),
		})
	}
	return Finding{Facts: facts}
}
