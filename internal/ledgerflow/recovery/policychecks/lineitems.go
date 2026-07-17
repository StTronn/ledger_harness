package policychecks

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/world/feeds"
)

// refund-line-item-match: the partial-refund evidence assembler. A refund
// smaller than its payment is ambiguous BY INTENT (line-item return vs goodwill
// credit); the strongest available evidence is the order's line items — a
// refund equal to exactly one item (or one pair; matching is capped at pairs)
// is very likely that item returned. The policy contributes CANDIDATES, never a
// decision: choosing among them is the agent's judgment.

type refundLineItemMatch struct{}

func (refundLineItemMatch) Name() string { return "refund-line-item-match" }

func (refundLineItemMatch) AppliesTo(ev Event) bool {
	return ev.Type == "refund" && ev.ParentAmount != nil
}

func (p refundLineItemMatch) Recover(ev Event, g Graph) Finding {
	payID, ok := g.PaymentIDForRefund(ev.EventID)
	if !ok {
		return Finding{}
	}
	orderID, ok := g.OrderIDForPayment(payID)
	if !ok {
		return Finding{}
	}
	o, ok := g.OrderInfo(orderID)
	if !ok || len(o.Items) == 0 {
		return Finding{}
	}
	cands := MatchLineItems(ev.Amount, orderID, o.Items)
	for i := range cands {
		cands[i].Policy = p.Name()
	}
	return Finding{Candidates: cands}
}

// MatchLineItems is the PURE matcher shared by this policy and the
// recorded-response generator (one matcher, no drift): exact single-item
// matches, then same-rate pair sums (capped at pairs), else an explicit
// no-match. Each match cites the item(s) it rests on.
func MatchLineItems(refund money.Money, orderID string, items []feeds.OrderItem) []Candidate {
	var out []Candidate
	for i, it := range items {
		if it.Amount == refund {
			out = append(out, Candidate{
				Kind: CandidateItemMatch, EntryType: "refund_reversal", GSTRate: it.GSTRate,
				Items:  []int{i},
				Source: Citation{Object: orderID, Path: fmt.Sprintf("items.%d", i)},
				Note:   fmt.Sprintf("refund equals line item %d (%s)", i, it.SKU),
			})
		}
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Amount.Add(items[j].Amount) == refund && items[i].GSTRate == items[j].GSTRate {
				out = append(out, Candidate{
					Kind: CandidatePairMatch, EntryType: "refund_reversal", GSTRate: items[i].GSTRate,
					Items:  []int{i, j},
					Source: Citation{Object: orderID, Path: fmt.Sprintf("items.%d+items.%d", i, j)},
					Note:   fmt.Sprintf("refund equals line items %d+%d", i, j),
				})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, Candidate{
			Kind: CandidateNoMatch,
			Note: "no line item or pair sums to the refund — goodwill credit (price_adjustment) or escalation territory",
		})
	}
	return out
}
