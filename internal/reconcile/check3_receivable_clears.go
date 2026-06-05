package reconcile

import (
	"fmt"

	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/money"
)

// checkReceivableClears is SPEC §7 check #3 (Receivable-clears): the
// assets/razorpay-settlement-receivable balance at period end must be ~0, EXCEPT
// for genuine T+2 in-transit amounts — money captured this period whose
// settlement deposit legitimately lands in the next period. A residual beyond
// that in-transit allowance means something is captured-but-not-settled or
// settled-but-not-booked, and is a break (SPEC §7).
//
// # The in-transit allowance
//
// The receivable accrues +gross on each sale and is credited −gross by each
// settlement (and reduced by refund reversals). If every batch settles within
// the period the receivable nets to exactly 0. A batch whose bank credit lands
// ON OR AFTER the period-end cutoff is genuine in-transit: its gross is still
// legitimately owed at period end. That batch's gross equals its settlement's
// net_deposit + fee + tax (the seeder's identity: net_deposit = gross − fee −
// tax), so the allowed residual is the sum of (net_deposit + fee + tax) over the
// settlements whose bank credit date is on/after PeriodEnd.
//
// A break is raised iff the actual receivable balance differs from that allowance
// (exact integer paise). The candidate event ids are the settlements that DID
// clear in-period plus any in-transit ones, so the investigator can see which
// batch left the receivable un-cleared.
func checkReceivableClears(in Input) []Break {
	creditDateByRef := creditDatesByRef(in.BankFeed)

	var inTransit money.Money
	var inTransitIDs []string
	var inPeriodIDs []string
	for _, s := range in.Settlements {
		creditDate, hasCredit := creditDateByRef[s.UTR]
		late := hasCredit && onOrAfter(creditDate, in.PeriodEnd)
		if late {
			// Genuine T+2 in-transit: gross still owed = net_deposit + fee + tax.
			gross := s.Amount.Add(s.Fee).Add(s.Tax)
			inTransit = inTransit.Add(gross)
			inTransitIDs = append(inTransitIDs, s.ID)
		} else {
			inPeriodIDs = append(inPeriodIDs, s.ID)
		}
	}

	if in.ReceivableBalance == inTransit {
		return nil // clears to the allowed in-transit amount — no break
	}

	residual := in.ReceivableBalance.Sub(inTransit)
	candidates := append(append([]string{}, inPeriodIDs...), inTransitIDs...)
	detail := fmt.Sprintf(
		"settlement-receivable residual %s at period end exceeds in-transit allowance %s (balance %s)",
		residual, inTransit, in.ReceivableBalance)
	return []Break{{
		Check:             CheckReceivableClears,
		Kind:              KindReceivableResidual,
		SettlementID:      "", // period-wide, not tied to one settlement
		Expected:          inTransit,
		Actual:            in.ReceivableBalance,
		CandidateEventIDs: candidates,
		Detail:            detail,
	}}
}

// creditDatesByRef maps each bank credit's ref (a settlement UTR) to its value
// date, so check #3 can tell whether a settlement's deposit landed on/after the
// period-end cutoff (genuine in-transit). First occurrence wins on a duplicate
// ref, matching check #1's index.
func creditDatesByRef(feed ingest.RawBankFeed) map[string]string {
	m := make(map[string]string, len(feed.Credits))
	for _, c := range feed.Credits {
		if _, dup := m[c.Ref]; !dup {
			m[c.Ref] = c.Date
		}
	}
	return m
}
