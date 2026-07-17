package reconcile

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/ingest"
	"github.com/razorpay/ledger-flow/internal/money"
)

// checkSettlementBank is SPEC §7 check #1 (Settlement → Bank): every settlement's
// net_deposit must match a bank-feed CREDIT of equal amount, matched by UTR/ref
// with the value date within DateToleranceDays. Two failure modes are breaks:
//
//   - no bank credit references this settlement's UTR at all (the deposit never
//     hit the bank, or hit it under a different ref) — an unmatched settlement;
//   - a credit references the UTR but its amount differs from net_deposit (the
//     bank received a different amount than the settlement claims).
//
// A ref-matched credit whose date is OUTSIDE the tolerance is also a break: the
// money tied out but on the wrong day, which the investigator should see.
//
// Amounts are compared EXACTLY (integer paise) — tolerance is on the date only.
func checkSettlementBank(in Input) []Break {
	credits := indexCreditsByRef(in.BankFeed.Credits)

	var breaks []Break
	for _, s := range in.Settlements {
		matched, ok := credits[s.UTR]
		if !ok {
			// No bank credit carries this settlement's UTR: the deposit is missing
			// from the independent record. Expected is the net_deposit we booked;
			// Actual is zero (nothing matched).
			breaks = append(breaks, Break{
				Check:             CheckSettlementBank,
				Kind:              KindSettlementBankMismatch,
				SettlementID:      s.ID,
				Expected:          s.Amount,
				Actual:            money.FromPaise(0),
				CandidateEventIDs: []string{s.ID},
				Detail: fmt.Sprintf(
					"settlement %s (UTR %s) net_deposit %s has no matching bank credit",
					s.ID, s.UTR, s.Amount),
			})
			continue
		}

		if matched.Amount != s.Amount {
			// A credit matched by UTR but for a different amount: the bank received
			// something other than the settlement's net_deposit.
			breaks = append(breaks, Break{
				Check:             CheckSettlementBank,
				Kind:              KindSettlementBankMismatch,
				SettlementID:      s.ID,
				Expected:          s.Amount,
				Actual:            matched.Amount,
				CandidateEventIDs: []string{s.ID},
				Detail: fmt.Sprintf(
					"settlement %s (UTR %s) net_deposit %s != bank credit %s",
					s.ID, s.UTR, s.Amount, matched.Amount),
			})
			continue
		}

		// Amount tied out; verify the value date is within tolerance. An
		// unparseable date or a gap beyond tolerance is a break (right money,
		// wrong/garbled day).
		gap, dated := dayGap(matched.Date, dateString(s.CreatedAt))
		if !dated || gap > in.DateToleranceDays {
			breaks = append(breaks, Break{
				Check:             CheckSettlementBank,
				Kind:              KindSettlementBankMismatch,
				SettlementID:      s.ID,
				Expected:          s.Amount,
				Actual:            matched.Amount,
				CandidateEventIDs: []string{s.ID},
				Detail: fmt.Sprintf(
					"settlement %s (UTR %s) amount tied out but bank credit date %s is outside ±%dd of %s",
					s.ID, s.UTR, matched.Date, in.DateToleranceDays, dateString(s.CreatedAt)),
			})
		}
	}
	return breaks
}

// indexCreditsByRef builds a ref -> credit-line map for O(1) UTR lookup. If two
// credits share a ref (not expected in the v1 substrate, where each settlement
// has a unique UTR) the FIRST is kept, so the index is deterministic regardless
// of any later duplicate; a genuine duplicate would surface as an amount/date
// mismatch on whichever the settlement compares against.
func indexCreditsByRef(credits []ingest.RawBankFeedLine) map[string]ingest.RawBankFeedLine {
	m := make(map[string]ingest.RawBankFeedLine, len(credits))
	for _, c := range credits {
		if _, dup := m[c.Ref]; !dup {
			m[c.Ref] = c
		}
	}
	return m
}
