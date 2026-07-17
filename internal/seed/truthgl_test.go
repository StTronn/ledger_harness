package seed

import (
	"testing"

	"github.com/razorpay/ledger-flow/internal/config"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/truth"
)

// TestTruthGLBoundFromPlaybook is the module's headline assertion (SPEC §2 Phase
// 2, §4.2): the truth GL is produced by BINDING the playbook entry types and
// POSTING them through the real ledger. We re-bind+re-post every truth entry's
// (entry_type, line amounts) against the committed playbook and assert each one
// is accepted by the engine — i.e. balances and uses known accounts. If the
// emitter had hand-written a line that diverged from the playbook (wrong account,
// wrong split), the re-post would be rejected here.
func TestTruthGLBoundFromPlaybook(t *testing.T) {
	pb, err := config.DefaultPlaybook()
	if err != nil {
		t.Fatalf("DefaultPlaybook: %v", err)
	}
	_, _, gl, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(gl.Entries) == 0 {
		t.Fatal("no truth GL entries")
	}

	lg := ledger.New(ledger.NewPlaybookChart(pb))
	tmpls := ledger.NewPlaybookTemplates(pb)

	for _, e := range gl.Entries {
		// Recover the playbook params from the emitted entry's lines so we can
		// re-bind through the engine. This works precisely because the truth lines
		// ARE the playbook template expanded — the test would fail otherwise.
		params, err := paramsFromTruthEntry(e)
		if err != nil {
			t.Fatalf("entry %s (%s): %v", e.ID, e.EntryType, err)
		}
		bound, err := ledger.Bind(tmpls, e.EntryType, "reck:"+e.ID, params)
		if err != nil {
			t.Fatalf("entry %s (%s): bind: %v", e.ID, e.EntryType, err)
		}
		posted, err := lg.Post(bound)
		if err != nil {
			t.Fatalf("entry %s (%s): post rejected (truth entry not playbook-consistent): %v", e.ID, e.EntryType, err)
		}
		// The re-bound lines must match the emitted truth lines exactly (same
		// side/account/amount, same order) — proving the emitter wrote the
		// playbook expansion, not something else.
		if len(posted.Lines) != len(e.Lines) {
			t.Fatalf("entry %s: line count %d != %d", e.ID, len(posted.Lines), len(e.Lines))
		}
		for i, l := range e.Lines {
			pl := posted.Lines[i]
			if truth.Side(pl.Side) != l.Side || pl.Account != l.Account || pl.Amount != l.Amount {
				t.Errorf("entry %s line %d: re-bound (%s %s %s) != truth (%s %s %s)",
					e.ID, i, pl.Side, pl.Account, pl.Amount, l.Side, l.Account, l.Amount)
			}
		}
	}

	// Every truth entry posted, so the engine's trial balance is balanced.
	if tb := lg.TrialBalance(); !tb.IsBalanced() {
		t.Errorf("trial balance of re-posted truth GL not balanced: Dr=%s Cr=%s", tb.TotalDr, tb.TotalCr)
	}
}

// paramsFromTruthEntry recovers the playbook params for an entry type from its
// emitted lines. It maps each entry type's lines back to the param names the
// template declares; the tx-id param is supplied as the zero placeholder (the
// real id string lives on the truth entry, not in the param channel).
func paramsFromTruthEntry(e truth.Entry) (map[string]money.Money, error) {
	amt := func(side truth.Side, account string) money.Money {
		for _, l := range e.Lines {
			if l.Side == side && l.Account == account {
				return l.Amount
			}
		}
		return 0
	}
	switch e.EntryType {
	case "dtc_sale":
		return map[string]money.Money{
			"gross":      amt(truth.Debit, "assets/razorpay-settlement-receivable"),
			"net":        amt(truth.Credit, "income/product-sales"),
			"gst":        amt(truth.Credit, "liabilities/gst-output-payable"),
			"payment_id": txIDParam(),
		}, nil
	case "refund_reversal":
		return map[string]money.Money{
			"net":       amt(truth.Debit, "income/sales-returns"),
			"gst":       amt(truth.Debit, "liabilities/gst-output-payable"),
			"refund_id": txIDParam(),
		}, nil
	case "razorpay_settlement":
		net := amt(truth.Debit, "assets/bank")
		fee := amt(truth.Debit, "expense/processor-fees")
		gstOnFee := amt(truth.Debit, "expense/gst-input")
		return map[string]money.Money{
			"net_deposit": net,
			"fee":         fee,
			"gst_on_fee":  gstOnFee,
			"gross":       amt(truth.Credit, "assets/razorpay-settlement-receivable"),
			"bank_tx_id":  txIDParam(),
		}, nil
	case "chargeback_loss":
		return map[string]money.Money{
			"net":        amt(truth.Debit, "expense/chargeback-loss"),
			"gst":        amt(truth.Debit, "liabilities/gst-output-payable"),
			"dispute_id": txIDParam(),
		}, nil
	default:
		return nil, errUnknownEntryType{e.EntryType}
	}
}

type errUnknownEntryType struct{ name string }

func (e errUnknownEntryType) Error() string { return "unknown entry type " + e.name }

// TestTruthGLEntryMetadata asserts the emitter stamps the human-readable id, the
// real source event/tx id strings, and the event timestamp on each truth entry —
// not the ledger's opaque integer tx id (SPEC §9: entries attributable to events).
func TestTruthGLEntryMetadata(t *testing.T) {
	fx, feed, gl, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Sequential gl_0001.. ids in posting order.
	for i, e := range gl.Entries {
		want := nthEntryID(i + 1)
		if e.ID != want {
			t.Errorf("entry %d id = %q, want %q", i, e.ID, want)
		}
		if e.EventID == "" {
			t.Errorf("entry %s has empty event id", e.ID)
		}
		if e.Ts == 0 {
			t.Errorf("entry %s (%s) has zero timestamp", e.ID, e.EntryType)
		}
	}

	// Settlement truth entries carry the UTR (the real bank tx ref), which must
	// match a bank-feed credit ref — proving TxID is the real string, not "0".
	credByRef := map[string]bool{}
	for _, c := range feed.Credits {
		credByRef[c.Ref] = true
	}
	setlByID := map[string]Settlement{}
	for _, s := range fx.Settlements {
		setlByID[s.ID] = s
	}
	for _, e := range gl.Entries {
		if e.EntryType != "razorpay_settlement" {
			continue
		}
		s, ok := setlByID[e.EventID]
		if !ok {
			t.Errorf("settlement entry %s event id %q not a known settlement", e.ID, e.EventID)
			continue
		}
		if e.TxID != s.UTR {
			t.Errorf("settlement entry %s tx id = %q, want UTR %q", e.ID, e.TxID, s.UTR)
		}
		if !credByRef[e.TxID] {
			t.Errorf("settlement entry %s tx id %q has no matching bank credit", e.ID, e.TxID)
		}
	}
}

// nthEntryID renders the nth (1-based) GL entry id, mirroring the emitter's
// gl_%04d format, so the metadata test does not depend on a private helper.
func nthEntryID(n int) string {
	const digits = "0123456789"
	b := []byte("gl_0000")
	for i := 6; i >= 3; i-- {
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b)
}
