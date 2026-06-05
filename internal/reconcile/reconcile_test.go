package reconcile

import (
	"testing"

	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/money"
)

// p builds a money value from paise for terse test fixtures.
func p(paise int64) money.Money { return money.FromPaise(paise) }

// cleanInput is one hand-built, internally-consistent batch that ties out on all
// three checks, used as the baseline scenario and as the starting point each
// failure case perturbs. The numbers: two payments grossing 100000 + 50000, one
// of which (pay_A, 100000) is refunded 100000; fees 2000 + 1000 = 3000 and
// tax 360 + 180 = 540. So the receivable for the batch nets:
//
//	+100000 +50000  (sales)
//	-100000          (refund reversal: net+gst == refund gross)
//	-? (settlement credits the remaining gross)
//
// gross-after-refund = 150000 - 100000 = 50000; net_deposit = 50000 - 3000 - 540
// = 46460. The settlement credits the receivable by the remaining gross 50000, so
// the receivable clears to 0. Batch-sum: Σpay 150000 − Σrefund 100000 − Σfees
// 3540 = 46460 == net_deposit. Bank credit of 46460 on the settle date matches.
func cleanInput() Input {
	return Input{
		Settlements: []ingest.RawSettlement{{
			Entity:     "settlement",
			ID:         "setl_1",
			Amount:     p(46460), // net_deposit
			Fee:        p(3000),
			Tax:        p(540),
			UTR:        "UTR1",
			CreatedAt:  dayEpoch("2026-05-10"),
			PaymentIDs: []string{"pay_A", "pay_B"},
			RefundIDs:  []string{"rfnd_A"},
		}},
		Payments: []ingest.RawPayment{
			{ID: "pay_A", Amount: p(100000), Fee: p(2000), Tax: p(360)},
			{ID: "pay_B", Amount: p(50000), Fee: p(1000), Tax: p(180)},
		},
		Refunds: []ingest.RawRefund{
			{ID: "rfnd_A", Amount: p(100000), PaymentID: "pay_A"},
		},
		BankFeed: ingest.RawBankFeed{
			Period: "2026-05",
			Credits: []ingest.RawBankFeedLine{
				{Amount: p(46460), Date: "2026-05-10", Ref: "UTR1"},
			},
		},
		ReceivableBalance: p(0),
		PeriodEnd:         "2026-06-01",
		DateToleranceDays: 2,
	}
}

// dayEpoch turns a YYYY-MM-DD into the Unix-seconds at UTC midnight, so a test
// settlement's CreatedAt and its bank credit's Date describe the same day.
func dayEpoch(s string) int64 {
	t, err := parseDate(s)
	if err != nil {
		panic("dayEpoch: " + err.Error())
	}
	return t.Unix()
}

// TestReconcileCleanIsZeroBreaks is the SPEC §7 happy path: an internally
// consistent batch reconciles fully (0 breaks) on all three checks.
func TestReconcileCleanIsZeroBreaks(t *testing.T) {
	breaks := Reconcile(cleanInput())
	if len(breaks) != 0 {
		t.Fatalf("clean scenario produced %d breaks, want 0: %+v", len(breaks), breaks)
	}
}

// TestReconcileChecks is the per-check detection matrix: each case perturbs the
// clean input to trip exactly one check, and asserts the break's check number,
// kind, settlement id, expected/actual money, and candidate event ids.
func TestReconcileChecks(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(in *Input)
		wantCheck     CheckNum
		wantKind      string
		wantSettleID  string
		wantExpected  money.Money
		wantActual    money.Money
		wantCandidate []string
	}{
		{
			name: "check1 unmatched settlement (no bank credit)",
			mutate: func(in *Input) {
				in.BankFeed.Credits = nil // the deposit never hit the bank record
			},
			wantCheck:     CheckSettlementBank,
			wantKind:      KindSettlementBankMismatch,
			wantSettleID:  "setl_1",
			wantExpected:  p(46460),
			wantActual:    p(0),
			wantCandidate: []string{"setl_1"},
		},
		{
			name: "check1 amount mismatch (bank credit differs)",
			mutate: func(in *Input) {
				in.BankFeed.Credits[0].Amount = p(46000) // bank got a different amount
			},
			wantCheck:     CheckSettlementBank,
			wantKind:      KindSettlementBankMismatch,
			wantSettleID:  "setl_1",
			wantExpected:  p(46460),
			wantActual:    p(46000),
			wantCandidate: []string{"setl_1"},
		},
		{
			name: "check1 date outside tolerance",
			mutate: func(in *Input) {
				in.BankFeed.Credits[0].Date = "2026-05-20" // 10 days late, tol 2
			},
			wantCheck:     CheckSettlementBank,
			wantKind:      KindSettlementBankMismatch,
			wantSettleID:  "setl_1",
			wantExpected:  p(46460),
			wantActual:    p(46460),
			wantCandidate: []string{"setl_1"},
		},
		{
			name: "check2 refund omitted from batch (refund-in-batch class)",
			mutate: func(in *Input) {
				// The refund was processed but dropped from the batch's refund_ids,
				// so the deposit still nets the refund out but the batch members no
				// longer account for it: Σpay 150000 − Σrefund 0 − Σfees 3540 =
				// 146460 != net_deposit 46460.
				in.Settlements[0].RefundIDs = nil
			},
			wantCheck:     CheckBatchSum,
			wantKind:      KindBatchSumMismatch,
			wantSettleID:  "setl_1",
			wantExpected:  p(146460), // member-implied deposit without the refund
			wantActual:    p(46460),  // stated net_deposit
			wantCandidate: []string{"pay_A", "pay_B"},
		},
		{
			name: "check3 receivable residual beyond in-transit",
			mutate: func(in *Input) {
				// A captured-but-not-settled amount leaves the receivable non-zero at
				// period end with no in-transit settlement to excuse it.
				in.ReceivableBalance = p(50000)
			},
			wantCheck:     CheckReceivableClears,
			wantKind:      KindReceivableResidual,
			wantSettleID:  "",
			wantExpected:  p(0),     // allowed in-transit
			wantActual:    p(50000), // actual receivable balance
			wantCandidate: []string{"setl_1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := cleanInput()
			tt.mutate(&in)
			breaks := Reconcile(in)
			if len(breaks) != 1 {
				t.Fatalf("got %d breaks, want exactly 1: %+v", len(breaks), breaks)
			}
			b := breaks[0]
			if b.Check != tt.wantCheck {
				t.Errorf("Check = %d, want %d", b.Check, tt.wantCheck)
			}
			if b.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", b.Kind, tt.wantKind)
			}
			if b.SettlementID != tt.wantSettleID {
				t.Errorf("SettlementID = %q, want %q", b.SettlementID, tt.wantSettleID)
			}
			if b.Expected != tt.wantExpected {
				t.Errorf("Expected = %s, want %s", b.Expected, tt.wantExpected)
			}
			if b.Actual != tt.wantActual {
				t.Errorf("Actual = %s, want %s", b.Actual, tt.wantActual)
			}
			if !equalStrings(b.CandidateEventIDs, tt.wantCandidate) {
				t.Errorf("CandidateEventIDs = %v, want %v", b.CandidateEventIDs, tt.wantCandidate)
			}
			if b.Detail == "" {
				t.Errorf("Detail is empty; a break must carry a human-readable explanation")
			}
		})
	}
}

// TestReconcileInTransitAllowed asserts the genuine T+2 case is NOT a break: a
// settlement whose bank credit lands on/after the period-end cutoff is in-transit,
// so a receivable residual equal to that batch's gross clears check #3 cleanly.
func TestReconcileInTransitAllowed(t *testing.T) {
	in := cleanInput()
	// The settlement credits the bank in the next period (T+2 spanning month end).
	in.Settlements[0].CreatedAt = dayEpoch("2026-05-31")
	in.BankFeed.Credits[0].Date = "2026-06-02" // on/after the 2026-06-01 cutoff
	// Its gross (net_deposit + fee + tax) is still owed at period end.
	gross := in.Settlements[0].Amount.Add(in.Settlements[0].Fee).Add(in.Settlements[0].Tax)
	in.ReceivableBalance = gross

	breaks := Reconcile(in)
	// Check #1 still fires (the credit date is outside the ±2d tolerance for a
	// month-spanning T+2), but check #3 must NOT: the residual is allowed in-transit.
	for _, b := range breaks {
		if b.Check == CheckReceivableClears {
			t.Errorf("receivable-clears raised a break for a genuine in-transit residual: %+v", b)
		}
	}
}

// TestReconcileDeterministicOrder asserts the break slice order is stable (by
// check number then settlement id) regardless of input order, so the output is
// byte-identical across runs (SPEC §12).
func TestReconcileDeterministicOrder(t *testing.T) {
	in := cleanInput()
	// Trip two checks at once: drop the bank credit (check #1) AND leave a residual
	// (check #3). Reconcile must return them check-1-before-check-3.
	in.BankFeed.Credits = nil
	in.ReceivableBalance = p(50000)

	breaks := Reconcile(in)
	if len(breaks) != 2 {
		t.Fatalf("got %d breaks, want 2: %+v", len(breaks), breaks)
	}
	if breaks[0].Check != CheckSettlementBank || breaks[1].Check != CheckReceivableClears {
		t.Fatalf("break order = [%d, %d], want [1, 3]", breaks[0].Check, breaks[1].Check)
	}
}

// equalStrings reports whether two string slices are element-wise equal (order
// matters — candidate ids are emitted in a deterministic order).
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
