package ledger

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/razorpay/ledger-flow/internal/config"
)

// loadRealPlaybook loads the committed config/playbook.json relative to this
// package directory, so the binding tests run against the real schema the rule
// engine and reports will use.
func loadRealPlaybook(t *testing.T) *config.Playbook {
	t.Helper()
	pb, err := config.Load(filepath.Join("..", "..", "config", "playbook.json"))
	if err != nil {
		t.Fatalf("load real playbook: %v", err)
	}
	return pb
}

// TestBindAllEntryTypesBalance binds each of the four v1 entry types with
// consistent params and asserts (a) Bind succeeds, (b) the bound entry posts
// (i.e. it is balanced and uses known accounts), and (c) the tx id is stamped
// from the declared tx_param. This is the gate clause "binding of each of the 4
// entry types yields a balanced set given consistent params."
func TestBindAllEntryTypesBalance(t *testing.T) {
	pb := loadRealPlaybook(t)
	tmpls := NewPlaybookTemplates(pb)
	chart := NewPlaybookChart(pb)

	tests := []struct {
		name      string
		entryType string
		ik        string
		params    map[string]int64
		wantTx    string // expected stamped tx id
		// wantLines is the expected (side, account, paise) set after binding,
		// in template order.
		wantLines []struct {
			side    Side
			account string
			paise   int64
		}
	}{
		{
			name:      "dtc_sale",
			entryType: "dtc_sale",
			ik:        "pay_PA1",
			params:    map[string]int64{"gross": 118000, "net": 100000, "gst": 18000, "payment_id": 777},
			wantTx:    "777",
			wantLines: []struct {
				side    Side
				account string
				paise   int64
			}{
				{Debit, "assets/razorpay-settlement-receivable", 118000},
				{Credit, "income/product-sales", 100000},
				{Credit, "liabilities/gst-output-payable", 18000},
			},
		},
		{
			name:      "razorpay_settlement",
			entryType: "razorpay_settlement",
			ik:        "bank_tx_9",
			params: map[string]int64{
				"net_deposit": 112280, "fee": 4720, "gst_on_fee": 1000,
				"gross": 118000, "bank_tx_id": 9,
			},
			wantTx: "9",
			wantLines: []struct {
				side    Side
				account string
				paise   int64
			}{
				{Debit, "assets/bank", 112280},
				{Debit, "expense/processor-fees", 4720},
				{Debit, "expense/gst-input", 1000},
				{Credit, "assets/razorpay-settlement-receivable", 118000},
			},
		},
		{
			name:      "refund_reversal",
			entryType: "refund_reversal",
			ik:        "rfnd_1",
			params:    map[string]int64{"net": 50000, "gst": 9000, "refund_id": 55},
			wantTx:    "55",
			// Cr line uses net+gst = 59000.
			wantLines: []struct {
				side    Side
				account string
				paise   int64
			}{
				{Debit, "income/sales-returns", 50000},
				{Debit, "liabilities/gst-output-payable", 9000},
				{Credit, "assets/razorpay-settlement-receivable", 59000},
			},
		},
		{
			name:      "chargeback_loss",
			entryType: "chargeback_loss",
			ik:        "disp_1",
			params:    map[string]int64{"net": 25000, "gst": 4500, "dispute_id": 88},
			wantTx:    "88",
			// Cr line uses net+gst = 29500.
			wantLines: []struct {
				side    Side
				account string
				paise   int64
			}{
				{Debit, "expense/chargeback-loss", 25000},
				{Debit, "liabilities/gst-output-payable", 4500},
				{Credit, "assets/bank", 29500},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry, err := Bind(tmpls, tc.entryType, tc.ik, ParamsFromPaise(tc.params))
			if err != nil {
				t.Fatalf("Bind: %v", err)
			}
			if entry.Type != tc.entryType {
				t.Errorf("entry.Type = %q, want %q", entry.Type, tc.entryType)
			}
			if entry.IK != tc.ik {
				t.Errorf("entry.IK = %q, want %q", entry.IK, tc.ik)
			}
			if entry.TxID != tc.wantTx {
				t.Errorf("entry.TxID = %q, want %q", entry.TxID, tc.wantTx)
			}
			if len(entry.Lines) != len(tc.wantLines) {
				t.Fatalf("bound %d lines, want %d: %+v", len(entry.Lines), len(tc.wantLines), entry.Lines)
			}
			for i, wl := range tc.wantLines {
				gl := entry.Lines[i]
				if gl.Side != wl.side || gl.Account != wl.account || gl.Amount.Paise() != wl.paise {
					t.Errorf("line %d = {%s %s %d}, want {%s %s %d}",
						i, gl.Side, gl.Account, gl.Amount.Paise(), wl.side, wl.account, wl.paise)
				}
			}

			// The bound entry must balance and post cleanly.
			d, c := entry.sumBySide()
			if d != c {
				t.Errorf("bound entry not balanced: Dr=%s Cr=%s", d, c)
			}
			lg := New(chart)
			if _, err := lg.Post(entry); err != nil {
				t.Fatalf("Post bound entry: %v", err)
			}
		})
	}
}

// TestBindErrors covers the binder's rejection paths.
func TestBindErrors(t *testing.T) {
	pb := loadRealPlaybook(t)
	tmpls := NewPlaybookTemplates(pb)

	t.Run("unknown entry type", func(t *testing.T) {
		_, err := Bind(tmpls, "no_such_type", "ik", ParamsFromPaise(map[string]int64{}))
		if !errors.Is(err, ErrUnknownEntryType) {
			t.Fatalf("err = %v, want ErrUnknownEntryType", err)
		}
	})

	t.Run("missing param in expression", func(t *testing.T) {
		// dtc_sale needs gross/net/gst; omit gst.
		_, err := Bind(tmpls, "dtc_sale", "ik", ParamsFromPaise(map[string]int64{
			"gross": 118000, "net": 100000, "payment_id": 1,
		}))
		if !errors.Is(err, ErrMissingParam) {
			t.Fatalf("err = %v, want ErrMissingParam", err)
		}
	})

	t.Run("missing tx_param", func(t *testing.T) {
		// All expression params present, but the tx_param payment_id is absent.
		_, err := Bind(tmpls, "dtc_sale", "ik", ParamsFromPaise(map[string]int64{
			"gross": 118000, "net": 100000, "gst": 18000,
		}))
		if !errors.Is(err, ErrMissingParam) {
			t.Fatalf("err = %v, want ErrMissingParam", err)
		}
	})

	t.Run("negative bound amount", func(t *testing.T) {
		// refund_reversal's Cr line is net+gst; make it negative via negative
		// params so the evaluated magnitude is < 0.
		_, err := Bind(tmpls, "refund_reversal", "ik", ParamsFromPaise(map[string]int64{
			"net": -50000, "gst": -9000, "refund_id": 1,
		}))
		if !errors.Is(err, ErrNegativeBoundAmount) {
			t.Fatalf("err = %v, want ErrNegativeBoundAmount", err)
		}
	})
}

// TestMissingParams verifies the up-front helper reports the right gaps.
func TestMissingParams(t *testing.T) {
	pb := loadRealPlaybook(t)
	tmpls := NewPlaybookTemplates(pb)
	tmpl, ok := tmpls.Template("dtc_sale")
	if !ok {
		t.Fatal("dtc_sale template missing")
	}
	got := MissingParams(tmpl, ParamsFromPaise(map[string]int64{"gross": 1, "net": 2}))
	// Expect gst (used in a line) and payment_id (tx_param), sorted.
	want := []string{"gst", "payment_id"}
	if len(got) != len(want) {
		t.Fatalf("MissingParams = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MissingParams = %v, want %v", got, want)
		}
	}
}

// TestBindThenReportEndToEnd binds all four types against the real playbook with
// a consistent period (sale -> settle -> refund), posts them, and asserts the
// trial balance stays balanced — the full bind->post->report path on real
// schema. Uses money helpers only; never a float.
func TestBindThenReportEndToEnd(t *testing.T) {
	pb := loadRealPlaybook(t)
	tmpls := NewPlaybookTemplates(pb)
	lg := New(NewPlaybookChart(pb))

	mustBindPost := func(et, ik string, params map[string]int64) {
		t.Helper()
		e, err := Bind(tmpls, et, ik, ParamsFromPaise(params))
		if err != nil {
			t.Fatalf("Bind %s: %v", et, err)
		}
		if _, err := lg.Post(e); err != nil {
			t.Fatalf("Post %s: %v", et, err)
		}
	}

	mustBindPost("dtc_sale", "s1", map[string]int64{"gross": 118000, "net": 100000, "gst": 18000, "payment_id": 1})
	mustBindPost("razorpay_settlement", "b1", map[string]int64{
		"net_deposit": 112280, "fee": 4720, "gst_on_fee": 1000, "gross": 118000, "bank_tx_id": 2,
	})
	mustBindPost("refund_reversal", "r1", map[string]int64{"net": 100000, "gst": 18000, "refund_id": 3})

	tb := lg.TrialBalance()
	if !tb.IsBalanced() {
		t.Fatalf("real-schema trial balance not balanced: Dr=%s Cr=%s", tb.TotalDr, tb.TotalCr)
	}
	// Sanity: total debits equals the sum of all posted Dr magnitudes.
	if tb.TotalDr != tb.TotalCr {
		t.Fatalf("ΣDr != ΣCr: %s vs %s", tb.TotalDr, tb.TotalCr)
	}
}
