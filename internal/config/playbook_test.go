package config

import (
	"os"
	"path/filepath"
	"testing"
)

// realPlaybookPath is the committed playbook relative to this package dir.
func realPlaybookPath() string { return filepath.Join("..", "..", "config", "playbook.json") }

// TestLoadRealPlaybook loads the committed config/playbook.json and asserts it
// encodes SPEC §4.1 / §4.2 exactly: the right accounts with the right roots, and
// the four entry types with their balanced template lines.
func TestLoadRealPlaybook(t *testing.T) {
	pb, err := Load(realPlaybookPath())
	if err != nil {
		t.Fatalf("Load(real playbook): %v", err)
	}

	wantAccounts := map[string]RootType{
		"assets/bank":                           RootAssets,
		"assets/razorpay-settlement-receivable": RootAssets,
		"liabilities/gst-output-payable":        RootLiabilities,
		"liabilities/dispute-reserve":           RootLiabilities,
		"income/product-sales":                  RootIncome,
		"income/shipping-revenue":               RootIncome,
		"income/sales-returns":                  RootIncome,
		"income/discounts-allowances":           RootIncome,
		"expense/processor-fees":                RootExpense,
		"expense/gst-input":                     RootExpense,
		"expense/chargeback-loss":               RootExpense,
		"assets/cod-receivable":                 RootAssets,
		"expense/cod-collection-fees":           RootExpense,
		"expense/reverse-logistics":             RootExpense,
	}
	if len(pb.Accounts) != len(wantAccounts) {
		t.Fatalf("len(Accounts) = %d, want %d", len(pb.Accounts), len(wantAccounts))
	}
	for path, wantRoot := range wantAccounts {
		a, ok := pb.Account(path)
		if !ok {
			t.Errorf("missing account %q", path)
			continue
		}
		if a.Root() != wantRoot {
			t.Errorf("account %q root = %q, want %q", path, a.Root(), wantRoot)
		}
	}

	// Normal-balance convention, derived from root type.
	for path, wantSide := range map[string]Side{
		"assets/bank":                    Debit,
		"expense/processor-fees":         Debit,
		"liabilities/gst-output-payable": Credit,
		"income/product-sales":           Credit,
	} {
		a, _ := pb.Account(path)
		if got := a.NormalBalance(); got != wantSide {
			t.Errorf("account %q normal balance = %q, want %q", path, got, wantSide)
		}
	}

	for _, name := range []string{
		"dtc_sale", "razorpay_settlement", "refund_reversal", "price_adjustment", "chargeback_loss",
		"cod_sale", "cod_remittance", "rto_fee", "weight_adjustment",
	} {
		if _, ok := pb.EntryType(name); !ok {
			t.Errorf("missing entry type %q", name)
		}
	}
	if len(pb.EntryTypes) != 9 {
		t.Errorf("len(EntryTypes) = %d, want 9", len(pb.EntryTypes))
	}
}

// TestRealPlaybookTemplatesBalance asserts every entry type balances by
// construction for a realistic, internally-consistent binding (SPEC §4.2). The
// templates use +/- only and balance precisely when the caller binds the
// cross-param relationship the entry type assumes (e.g. dtc_sale needs
// gross = net + gst; razorpay_settlement needs net_deposit + fee + gst_on_fee =
// gross). These bindings are the contract the rule engine must honor; here we
// bind them and assert ΣDr == ΣCr — the same numeric check the ledger's post()
// will perform. The id-carrying params (payment_id, …) are bound to 0 since they
// never appear in amount expressions.
func TestRealPlaybookTemplatesBalance(t *testing.T) {
	pb, err := Load(realPlaybookPath())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	bindings := map[string]map[string]int64{
		// gross = net + gst  => 236000 = 200000 + 36000
		"dtc_sale": {"gross": 236000, "net": 200000, "gst": 36000, "payment_id": 0},
		// net_deposit + fee + gst_on_fee = gross => 230688 + 4400 + 792 = 235880... pick consistent
		"razorpay_settlement": {"net_deposit": 230808, "fee": 4400, "gst_on_fee": 792, "gross": 236000, "bank_tx_id": 0},
		// Cr net+gst must equal Dr net + Dr gst.
		"refund_reversal": {"net": 50000, "gst": 9000, "refund_id": 0},
		// Same credit-note shape as refund_reversal, against discounts-allowances.
		"price_adjustment": {"net": 16949, "gst": 3051, "refund_id": 0},
		"chargeback_loss":  {"net": 50000, "gst": 9000, "dispute_id": 0},
		// COD rail mirrors: cod_sale like dtc_sale (gross = net + gst);
		// cod_remittance like razorpay_settlement (net_deposit + fee + gst_on_fee = gross).
		"cod_sale":       {"gross": 150000, "net": 127119, "gst": 22881, "shipment_id": 0},
		"cod_remittance": {"net_deposit": 141200, "fee": 5000, "gst_on_fee": 900, "gross": 147100, "remittance_id": 0},
		// rto_fee: Cr net+gst == Dr net + Dr gst. weight_adjustment: single amount both sides.
		"rto_fee":           {"net": 10000, "gst": 1800, "shipment_id": 0},
		"weight_adjustment": {"amount": 4000, "shipment_id": 0},
	}

	for _, e := range pb.EntryTypes {
		params, ok := bindings[e.Name]
		if !ok {
			t.Fatalf("test missing a binding for entry type %q", e.Name)
		}
		var dr, cr int64
		for _, l := range e.Lines {
			var sum int64
			for _, tm := range l.Terms() {
				v, declared := params[tm.Param]
				if !declared {
					t.Fatalf("entry %q line references param %q with no test binding", e.Name, tm.Param)
				}
				if tm.Plus {
					sum += v
				} else {
					sum -= v
				}
			}
			if l.Side == Debit {
				dr += sum
			} else {
				cr += sum
			}
		}
		if dr != cr {
			t.Errorf("entry type %q does not balance for a consistent binding: ΣDr=%d ΣCr=%d", e.Name, dr, cr)
		}
	}
}

// TestParseValidationErrors is the malformed-variant table. Every case must be
// rejected by Parse with a non-nil Playbook never returned.
func TestParseValidationErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "unknown field rejected (strict decode)",
			input: `{"accounts":[],"entry_types":[],"surprise":true}`,
		},
		{
			name:  "malformed json",
			input: `{not json}`,
		},
		{
			name:  "wrong type for accounts",
			input: `{"accounts":"oops","entry_types":[]}`,
		},
		{
			name:  "account with unknown root",
			input: `{"accounts":[{"path":"equity/retained-earnings"}],"entry_types":[]}`,
		},
		{
			name:  "account with no child segment",
			input: `{"accounts":[{"path":"assets"}],"entry_types":[]}`,
		},
		{
			name:  "account with empty segment",
			input: `{"accounts":[{"path":"assets//bank"}],"entry_types":[]}`,
		},
		{
			name:  "duplicate account path",
			input: `{"accounts":[{"path":"assets/bank"},{"path":"assets/bank"}],"entry_types":[]}`,
		},
		{
			name: "line references unknown account",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a"],"lines":[{"side":"Dr","account":"assets/nope","amount":"a"}]}]}`,
		},
		{
			name: "invalid side",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a"],"lines":[{"side":"Debit","account":"assets/bank","amount":"a"}]}]}`,
		},
		{
			name: "amount references undeclared param",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a"],"lines":[{"side":"Dr","account":"assets/bank","amount":"b"}]}]}`,
		},
		{
			name: "amount uses multiplication",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a","b"],"lines":[{"side":"Dr","account":"assets/bank","amount":"a*b"}]}]}`,
		},
		{
			name: "amount uses division",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["gross","rate"],"lines":[{"side":"Dr","account":"assets/bank","amount":"gross/rate"}]}]}`,
		},
		{
			name: "amount has dangling operator",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a"],"lines":[{"side":"Dr","account":"assets/bank","amount":"a+"}]}]}`,
		},
		{
			name: "amount has empty term",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a","b"],"lines":[{"side":"Dr","account":"assets/bank","amount":"a++b"}]}]}`,
		},
		{
			name: "amount is empty string",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a"],"lines":[{"side":"Dr","account":"assets/bank","amount":""}]}]}`,
		},
		{
			name: "amount has whitespace",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a","b"],"lines":[{"side":"Dr","account":"assets/bank","amount":"a + b"}]}]}`,
		},
		{
			name: "entry type with no lines",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a"],"lines":[]}]}`,
		},
		{
			name: "duplicate entry type name",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[
					{"name":"x","params":["a"],"lines":[{"side":"Dr","account":"assets/bank","amount":"a"}]},
					{"name":"x","params":["a"],"lines":[{"side":"Dr","account":"assets/bank","amount":"a"}]}]}`,
		},
		{
			name: "duplicate param name",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a","a"],"lines":[{"side":"Dr","account":"assets/bank","amount":"a"}]}]}`,
		},
		{
			name: "tx_param not a declared param",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a"],"tx_param":"ref","lines":[{"side":"Dr","account":"assets/bank","amount":"a"}]}]}`,
		},
		{
			name:  "empty entry type name",
			input: `{"accounts":[{"path":"assets/bank"}],"entry_types":[{"name":"","params":[],"lines":[{"side":"Dr","account":"assets/bank","amount":"a"}]}]}`,
		},
		{
			name:  "trailing data after object",
			input: `{"accounts":[],"entry_types":[]} {"extra":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb, err := Parse([]byte(tt.input))
			if err == nil {
				t.Fatalf("Parse(%s) = nil error, want error", tt.name)
			}
			if pb != nil {
				t.Errorf("Parse(%s) returned non-nil Playbook on error", tt.name)
			}
		})
	}
}

// TestParseValid covers well-formed variants, including the +/- expression forms
// and an empty (but structurally valid) playbook.
func TestParseValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, pb *Playbook)
	}{
		{
			name:  "empty playbook is valid",
			input: `{"accounts":[],"entry_types":[]}`,
			check: func(t *testing.T, pb *Playbook) {
				if len(pb.Accounts) != 0 || len(pb.EntryTypes) != 0 {
					t.Errorf("want empty playbook, got %+v", pb)
				}
			},
		},
		{
			name:  "missing keys default to empty",
			input: `{}`,
			check: func(t *testing.T, pb *Playbook) {
				if len(pb.Accounts) != 0 || len(pb.EntryTypes) != 0 {
					t.Errorf("want empty playbook, got %+v", pb)
				}
			},
		},
		{
			name: "plus expression parses to two positive terms",
			input: `{"accounts":[{"path":"assets/bank"},{"path":"income/product-sales"}],
				"entry_types":[{"name":"x","params":["net","gst"],"lines":[
					{"side":"Dr","account":"assets/bank","amount":"net+gst"},
					{"side":"Cr","account":"income/product-sales","amount":"net+gst"}]}]}`,
			check: func(t *testing.T, pb *Playbook) {
				e, _ := pb.EntryType("x")
				terms := e.Lines[0].Terms()
				if len(terms) != 2 || !terms[0].Plus || !terms[1].Plus {
					t.Errorf("net+gst terms = %+v, want two positive terms", terms)
				}
				if terms[0].Param != "net" || terms[1].Param != "gst" {
					t.Errorf("net+gst params = %q,%q, want net,gst", terms[0].Param, terms[1].Param)
				}
			},
		},
		{
			name: "leading minus and subtraction parse with correct signs",
			input: `{"accounts":[{"path":"assets/bank"},{"path":"income/product-sales"}],
				"entry_types":[{"name":"x","params":["a","b"],"lines":[
					{"side":"Dr","account":"assets/bank","amount":"-a-b"},
					{"side":"Cr","account":"income/product-sales","amount":"-a-b"}]}]}`,
			check: func(t *testing.T, pb *Playbook) {
				e, _ := pb.EntryType("x")
				terms := e.Lines[0].Terms()
				if len(terms) != 2 || terms[0].Plus || terms[1].Plus {
					t.Errorf("-a-b terms = %+v, want two negative terms", terms)
				}
			},
		},
		{
			name:  "channel-segmentable account path is accepted",
			input: `{"accounts":[{"path":"income/product-sales/web"}],"entry_types":[]}`,
			check: func(t *testing.T, pb *Playbook) {
				a, ok := pb.Account("income/product-sales/web")
				if !ok {
					t.Fatal("missing channel-segmented account")
				}
				if a.Root() != RootIncome {
					t.Errorf("root = %q, want income", a.Root())
				}
			},
		},
		{
			name: "tx_param that is a declared param is accepted",
			input: `{"accounts":[{"path":"assets/bank"}],
				"entry_types":[{"name":"x","params":["a","ref"],"tx_param":"ref","lines":[{"side":"Dr","account":"assets/bank","amount":"a"}]}]}`,
			check: func(t *testing.T, pb *Playbook) {
				e, _ := pb.EntryType("x")
				if e.TxParam != "ref" {
					t.Errorf("TxParam = %q, want ref", e.TxParam)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb, err := Parse([]byte(tt.input))
			if err != nil {
				t.Fatalf("Parse(%s) unexpected error: %v", tt.name, err)
			}
			tt.check(t, pb)
		})
	}
}

// TestRootNormalBalance pins the documented sign convention.
func TestRootNormalBalance(t *testing.T) {
	tests := []struct {
		root RootType
		want Side
	}{
		{RootAssets, Debit},
		{RootExpense, Debit},
		{RootLiabilities, Credit},
		{RootIncome, Credit},
	}
	for _, tt := range tests {
		if got := tt.root.NormalBalance(); got != tt.want {
			t.Errorf("%q.NormalBalance() = %q, want %q", tt.root, got, tt.want)
		}
		if !tt.root.Valid() {
			t.Errorf("%q.Valid() = false, want true", tt.root)
		}
	}
	if RootType("equity").Valid() {
		t.Error(`RootType("equity").Valid() = true, want false`)
	}
	if RootType("equity").NormalBalance() != "" {
		t.Error(`unknown root NormalBalance should be ""`)
	}
}

// TestLoad covers the filesystem path: a written fixture and a missing file.
func TestLoad(t *testing.T) {
	t.Run("reads and validates a file from disk", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "playbook.json")
		body := `{"accounts":[{"path":"assets/bank"}],
			"entry_types":[{"name":"x","params":["a"],"lines":[{"side":"Dr","account":"assets/bank","amount":"a"}]}]}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		pb, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if _, ok := pb.Account("assets/bank"); !ok {
			t.Error("loaded playbook missing assets/bank")
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
			t.Fatal("Load(missing) = nil error, want error")
		}
	})

	t.Run("invalid file content surfaces a wrapped error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(path, []byte(`{"accounts":[{"path":"equity/x"}],"entry_types":[]}`), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("Load(bad content) = nil error, want error")
		}
	})
}
