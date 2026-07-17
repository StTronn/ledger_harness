package seed

import (
	"path/filepath"
	"testing"

	"github.com/razorpay/ledger-flow/internal/config"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/truth"
)

// loadPlaybook loads the committed playbook for cross-validating fixtures against
// the real chart of accounts and entry-type templates.
func loadPlaybook(t *testing.T) *config.Playbook {
	t.Helper()
	pb, err := config.Load(filepath.Join("..", "..", "config", "playbook.json"))
	if err != nil {
		t.Fatalf("load playbook: %v", err)
	}
	return pb
}

// TestGenerateTruthGLBalances is the headline gate (SPEC §2 Phase 2): the
// generated truth GL must balance ΣDr==ΣCr, both per-entry and overall.
func TestGenerateTruthGLBalances(t *testing.T) {
	_, _, gl, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(gl.Entries) == 0 {
		t.Fatal("Generate produced no GL entries")
	}
	for _, e := range gl.Entries {
		if !e.IsBalanced() {
			dr, cr := e.SumBySide()
			t.Errorf("entry %s (%s) unbalanced: Dr=%s Cr=%s", e.ID, e.EntryType, dr, cr)
		}
	}
	if !gl.IsBalanced() {
		dr, cr := gl.SumBySide()
		t.Fatalf("GL total unbalanced: Dr=%s Cr=%s", dr, cr)
	}
	if gl.Version != truth.SchemaVersion || gl.World != "dtc" || gl.Period != "2026-05" {
		t.Errorf("GL header wrong: %+v", struct {
			V int
			W string
			P string
		}{gl.Version, gl.World, gl.Period})
	}
}

// TestGenerateReceivableClears asserts the razorpay-settlement-receivable nets to
// zero at period end (SPEC §7 check #3): every captured sale is either settled or
// refunded, so the clearing account holds no residual.
func TestGenerateReceivableClears(t *testing.T) {
	_, _, gl, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var dr, cr money.Money
	const acct = "assets/razorpay-settlement-receivable"
	for _, e := range gl.Entries {
		for _, l := range e.Lines {
			if l.Account != acct {
				continue
			}
			if l.Side == truth.Debit {
				dr = dr.Add(l.Amount)
			} else {
				cr = cr.Add(l.Amount)
			}
		}
	}
	if dr != cr {
		t.Errorf("receivable does not clear: Dr=%s Cr=%s (net %s)", dr, cr, dr.Sub(cr))
	}
}

// TestGenerateDeterministic asserts two independent Generate calls for the same
// (world, period) yield byte-identical marshalled artifacts — the reproducibility
// invariant (SPEC §2, §12). It marshals through MarshalStable, the exact path the
// writer uses, so this also covers stable JSON key ordering.
func TestGenerateDeterministic(t *testing.T) {
	fx1, feed1, gl1, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate #1: %v", err)
	}
	fx2, feed2, gl2, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate #2: %v", err)
	}
	pairs := []struct {
		name string
		a, b any
	}{
		{"payments", fx1.Payments, fx2.Payments},
		{"refunds", fx1.Refunds, fx2.Refunds},
		{"settlements", fx1.Settlements, fx2.Settlements},
		{"disputes", fx1.Disputes, fx2.Disputes},
		{"bank-feed", feed1, feed2},
		{"truth-gl", gl1, gl2},
	}
	for _, p := range pairs {
		ba, err := MarshalStable(p.a)
		if err != nil {
			t.Fatalf("marshal %s a: %v", p.name, err)
		}
		bb, err := MarshalStable(p.b)
		if err != nil {
			t.Fatalf("marshal %s b: %v", p.name, err)
		}
		if string(ba) != string(bb) {
			t.Errorf("%s not byte-identical across runs", p.name)
		}
	}
}

// TestGenerateDifferentPeriodDiffers asserts a different period yields different
// substrate (the seed is period-sensitive end to end).
func TestGenerateDifferentPeriodDiffers(t *testing.T) {
	_, _, gl1, _ := Generate("dtc", "2026-05")
	_, _, gl2, _ := Generate("dtc", "2026-06")
	b1, _ := MarshalStable(gl1)
	b2, _ := MarshalStable(gl2)
	if string(b1) == string(b2) {
		t.Error("different periods produced identical truth GL")
	}
}

// TestFixturesValidateAgainstPlaybook re-derives the playbook entry for every
// fixture event (sale/refund/settlement/dispute) and posts it through the real
// ledger Bind+Post path. If every event binds, balances, and posts against the
// committed chart of accounts, the fixtures validate against the seed-model and
// playbook schemas (gate: "Fixtures validate against the seed-model schemas").
func TestFixturesValidateAgainstPlaybook(t *testing.T) {
	pb := loadPlaybook(t)
	fx, _, _, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	lg := ledger.New(ledger.NewPlaybookChart(pb))
	tmpls := ledger.NewPlaybookTemplates(pb)

	paymentByID := map[string]Payment{}
	for _, p := range fx.Payments {
		paymentByID[p.ID] = p
	}

	rateOf := func(notes Notes) int { // local int parse, float-free
		n := 0
		for i := 0; i < len(notes.GSTRate); i++ {
			n = n*10 + int(notes.GSTRate[i]-'0')
		}
		return n
	}

	// dtc_sale per payment.
	for _, p := range fx.Payments {
		net, gst := splitGSTInclusive(p.Amount, rateOf(p.Notes))
		params := map[string]money.Money{
			"gross":      p.Amount,
			"net":        net,
			"gst":        gst,
			"payment_id": txParam(p.ID),
		}
		mustBindPost(t, lg, tmpls, "dtc_sale", "sale:"+p.ID, params)
	}
	// refund_reversal per refund.
	for _, r := range fx.Refunds {
		net, gst := splitGSTInclusive(r.Amount, rateOf(r.Notes))
		params := map[string]money.Money{
			"net":       net,
			"gst":       gst,
			"refund_id": txParam(r.ID),
		}
		mustBindPost(t, lg, tmpls, "refund_reversal", "refund:"+r.ID, params)
	}
	// razorpay_settlement per settlement.
	for _, s := range fx.Settlements {
		gross := s.Amount.Add(s.Fee).Add(s.Tax)
		params := map[string]money.Money{
			"net_deposit": s.Amount,
			"fee":         s.Fee,
			"gst_on_fee":  s.Tax,
			"gross":       gross,
			"bank_tx_id":  txParam(s.ID),
		}
		mustBindPost(t, lg, tmpls, "razorpay_settlement", "settle:"+s.ID, params)
	}
	// chargeback_loss per lost dispute.
	for _, d := range fx.Disputes {
		if d.Status != "lost" {
			continue
		}
		net, gst := splitGSTInclusive(d.Amount, rateOf(d.Notes))
		params := map[string]money.Money{
			"net":        net,
			"gst":        gst,
			"dispute_id": txParam(d.ID),
		}
		mustBindPost(t, lg, tmpls, "chargeback_loss", "dispute:"+d.ID, params)
	}

	// Every posted entry balanced (Post would have rejected otherwise), so the
	// trial balance is balanced — the books built from fixtures tie out.
	if tb := lg.TrialBalance(); !tb.IsBalanced() {
		t.Errorf("trial balance from fixtures not balanced: Dr=%s Cr=%s", tb.TotalDr, tb.TotalCr)
	}
}

// txParam encodes an id string into the money.Money param channel the binder
// uses for tx ids. The binder only renders it as an integer; the value just must
// be present, so we pass zero — the tx id text itself is validated elsewhere.
func txParam(_ string) money.Money { return money.FromPaise(0) }

// mustBindPost binds an entry type with params and posts it, failing the test on
// any bind/post error.
func mustBindPost(t *testing.T, lg *ledger.Ledger, tmpls ledger.Templates, entryType, ik string, params map[string]money.Money) {
	t.Helper()
	entry, err := ledger.Bind(tmpls, entryType, ik, params)
	if err != nil {
		t.Fatalf("bind %s (%s): %v", entryType, ik, err)
	}
	if _, err := lg.Post(entry); err != nil {
		t.Fatalf("post %s (%s): %v", entryType, ik, err)
	}
}

// TestFixturesAndBankFeedCrossCheck asserts the independent bank feed ties out to
// the Razorpay fixtures: every settlement has a matching bank credit (UTR +
// amount), every lost dispute a matching bank debit (id + amount), and each
// settlement's batch-sum (Σpay − Σrefund − fee − tax) equals its net deposit
// (SPEC §7 checks #1 and #2).
func TestFixturesAndBankFeedCrossCheck(t *testing.T) {
	fx, feed, _, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	payByID := map[string]Payment{}
	for _, p := range fx.Payments {
		payByID[p.ID] = p
	}
	refByID := map[string]Refund{}
	for _, r := range fx.Refunds {
		refByID[r.ID] = r
	}
	credByRef := map[string]money.Money{}
	for _, c := range feed.Credits {
		credByRef[c.Ref] = c.Amount
	}
	debByRef := map[string]money.Money{}
	for _, d := range feed.Debits {
		debByRef[d.Ref] = d.Amount
	}

	for _, s := range fx.Settlements {
		// Check #1: matching bank credit.
		if got, ok := credByRef[s.UTR]; !ok || got != s.Amount {
			t.Errorf("settlement %s: no matching bank credit (utr %s amount %s, got %s ok=%v)",
				s.ID, s.UTR, s.Amount, got, ok)
		}
		// Check #2: batch-sum.
		var grossSum, refundSum money.Money
		for _, pid := range s.PaymentIDs {
			grossSum = grossSum.Add(payByID[pid].Amount)
		}
		for _, rid := range s.RefundIDs {
			refundSum = refundSum.Add(refByID[rid].Amount)
		}
		want := grossSum.Sub(refundSum).Sub(s.Fee).Sub(s.Tax)
		if want != s.Amount {
			t.Errorf("settlement %s batch-sum %s != net deposit %s", s.ID, want, s.Amount)
		}
	}
	for _, d := range fx.Disputes {
		if d.Status != "lost" {
			continue
		}
		if got, ok := debByRef[d.ID]; !ok || got != d.Amount {
			t.Errorf("lost dispute %s: no matching bank debit (got %s ok=%v)", d.ID, got, ok)
		}
	}
}
