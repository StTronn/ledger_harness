package reconcile

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/money"
)

// checkBatchSum is SPEC §7 check #2 (Batch-sum): for each settlement, the batch
// members must account for the deposit exactly —
//
//	Σ(payment.amount)            // gross captured in the batch
//	  − Σ(refund.amount)         // refunds netted out of the batch
//	  − Σ(payment.fee + payment.tax)  // Razorpay's fee + GST-on-fee on the batch
//	  == net_deposit
//
// This is the internal-consistency check on the settlement record itself: it
// proves the deposit equals what the batch's own payments/refunds/fees imply,
// independent of the ledger. A mismatch is a break carrying the batch member ids
// as candidates (so the investigator can spot, e.g., a refund that was processed
// but omitted from the batch's refund_ids — the refund-in-batch break class).
//
// All sums are exact integer paise; Expected is the member-implied deposit and
// Actual is the settlement's stated net_deposit, so a positive (Expected −
// Actual) means the batch claims MORE than it deposited (e.g. a missing refund
// inflates the implied deposit).
func checkBatchSum(in Input) []Break {
	payByID := indexPayments(in.Payments)
	refByID := indexRefunds(in.Refunds)

	var breaks []Break
	for _, s := range in.Settlements {
		var (
			payGross money.Money // Σ payment.amount
			feeTax   money.Money // Σ payment.fee + payment.tax
			refGross money.Money // Σ refund.amount
		)
		// Candidate ids: the batch members, in the settlement's own order
		// (payments then refunds), so the investigator gets the full batch.
		candidates := make([]string, 0, len(s.PaymentIDs)+len(s.RefundIDs))

		var missing []string // member ids the substrate has no record for
		for _, pid := range s.PaymentIDs {
			candidates = append(candidates, pid)
			p, ok := payByID[pid]
			if !ok {
				missing = append(missing, pid)
				continue
			}
			payGross = payGross.Add(p.Amount)
			feeTax = feeTax.Add(p.Fee).Add(p.Tax)
		}
		for _, rid := range s.RefundIDs {
			candidates = append(candidates, rid)
			r, ok := refByID[rid]
			if !ok {
				missing = append(missing, rid)
				continue
			}
			refGross = refGross.Add(r.Amount)
		}

		// Member-implied deposit: gross − refunds − (fee + tax).
		expected := payGross.Sub(refGross).Sub(feeTax)
		if expected == s.Amount {
			continue
		}

		detail := fmt.Sprintf(
			"settlement %s batch-sum %s (Σpay %s − Σrefund %s − Σfees %s) != net_deposit %s",
			s.ID, expected, payGross, refGross, feeTax, s.Amount)
		if len(missing) > 0 {
			detail += fmt.Sprintf("; batch references unknown members %v", missing)
		}
		breaks = append(breaks, Break{
			Check:             CheckBatchSum,
			Kind:              KindBatchSumMismatch,
			SettlementID:      s.ID,
			Expected:          expected,
			Actual:            s.Amount,
			CandidateEventIDs: candidates,
			Detail:            detail,
		})
	}
	return breaks
}
