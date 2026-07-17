package policychecks

import (
	"fmt"
	"strconv"

	"github.com/razorpay/ledger-flow/internal/money"
)

// fee-tier-from-ratecard: the first NEW-STYLE registry entry (the v1.5 fee-audit
// seam). For a settlement, the merchant's CONTRACTED fee is computable from the
// rate card: expected_fee = batch gross * fee_bps / 10000. The policy recovers
// that expectation and validates the settlement's ACTUAL fee against it — an
// overcharge surfaces as a fact with Valid=false, the evidence an agent (or a
// human) disputes the platform invoice with.
//
// Gross is reconstructed from the settlement itself (net deposit + fee + tax),
// the same identity the rule engine books settlements with.

type feeTierFromRateCard struct{}

func (feeTierFromRateCard) Name() string { return "fee-tier-from-ratecard" }

func (feeTierFromRateCard) AppliesTo(ev Event) bool {
	return ev.Type == "settlement" && ev.Fee != nil && ev.Tax != nil
}

func (p feeTierFromRateCard) Recover(ev Event, g Graph) Finding {
	rc, ok := g.RateCard()
	if !ok {
		return Finding{} // no contracted card snapshotted; nothing to check against
	}
	ch, ok := rc.Channel("razorpay") // v1: one channel; v1.5 keys this by feed
	if !ok {
		return Finding{}
	}
	payIDs, _, ok := g.SettlementMembers(ev.EventID)
	if !ok {
		return Finding{}
	}

	// Fees are charged PER PAYMENT on the full gross (integer floor per payment)
	// and are NOT returned when a refund nets the batch — so the contracted
	// expectation is the per-member sum, exactly how the platform prices it.
	var expected money.Money
	var members int
	for _, id := range payIDs {
		amt, ok := g.PaymentAmount(id)
		if !ok {
			continue
		}
		members++
		expected = expected.Add(money.FromPaise(amt.Paise() * int64(ch.FeeBps) / 10000))
	}
	actual := *ev.Fee
	valid := expected == actual

	note := fmt.Sprintf("contracted %d bps per payment across %d batch members", ch.FeeBps, members)
	if !valid {
		note = fmt.Sprintf("contracted %d bps per payment across %d batch members => %s, but the settlement charged %s (delta %s)",
			ch.FeeBps, members, expected, actual, actual.Sub(expected))
	}
	return Finding{Facts: []Fact{{
		Field:  "expected_fee",
		Value:  strconv.FormatInt(expected.Paise(), 10),
		Source: Citation{Object: "ratecard/razorpay", Path: "fee_bps"},
		Valid:  valid,
		Note:   note,
		Policy: p.Name(),
	}}}
}
