package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/razorpay/ledger-flow/internal/money"
)

// TestReadFixtures reads the committed worlds/dtc/2026-05 fixtures and asserts
// the counts and a couple of decoded field values per slice (the Phase-3 gate's
// "table-driven tests that read the committed fixtures and assert counts + a
// couple of decoded field values"). It pins the on-disk JSON → typed raw
// contract that ingest depends on.
func TestReadFixtures(t *testing.T) {
	raw, err := Read(repoRoot(t), "dtc", "2026-05")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	counts := []struct {
		name string
		got  int
		want int
	}{
		{"payments", len(raw.Payments), 31},
		{"refunds", len(raw.Refunds), 5},
		{"settlements", len(raw.Settlements), 6},
		{"disputes", len(raw.Disputes), 3},
		{"bank credits", len(raw.BankFeed.Credits), 6},
		{"bank debits", len(raw.BankFeed.Debits), 3},
	}
	for _, c := range counts {
		if c.got != c.want {
			t.Errorf("%s count = %d, want %d", c.name, c.got, c.want)
		}
	}

	// Spot-check decoded field values against the committed fixtures. Payments
	// are returned in file order (Read does not sort), so [0] is the first
	// object in payments.json.
	p0 := raw.Payments[0]
	if p0.ID != "pay_EuwmPrU7cL6U2q" {
		t.Errorf("payment[0].ID = %q, want pay_EuwmPrU7cL6U2q", p0.ID)
	}
	if p0.Amount != money.FromPaise(328117) {
		t.Errorf("payment[0].Amount = %d, want 328117", p0.Amount.Paise())
	}
	if p0.Fee != money.FromPaise(6562) || p0.Tax != money.FromPaise(1181) {
		t.Errorf("payment[0] fee/tax = %d/%d, want 6562/1181", p0.Fee.Paise(), p0.Tax.Paise())
	}
	if p0.Notes.SKU != "TONER-200" || p0.Notes.GSTRate != "5" {
		t.Errorf("payment[0].Notes = %+v, want {TONER-200 5}", p0.Notes)
	}
	if p0.Currency != "INR" || p0.Status != "captured" || !p0.Captured {
		t.Errorf("payment[0] currency/status/captured = %q/%q/%v", p0.Currency, p0.Status, p0.Captured)
	}

	r0 := raw.Refunds[0]
	if r0.ID != "rfnd_HNNBwnkgMLvXuE" || r0.PaymentID != "pay_Xw1AYD2VXqrsJn" {
		t.Errorf("refund[0] id/payment_id = %q/%q", r0.ID, r0.PaymentID)
	}
	if r0.Amount != money.FromPaise(226031) {
		t.Errorf("refund[0].Amount = %d, want 226031", r0.Amount.Paise())
	}

	s0 := raw.Settlements[0]
	if s0.ID != "setl_prSZo6GCwDyjfv" || s0.UTR != "UTRprSZo6GCwDyjfv" {
		t.Errorf("settlement[0] id/utr = %q/%q", s0.ID, s0.UTR)
	}
	if s0.Amount != money.FromPaise(1487676) || s0.Fee != money.FromPaise(35098) || s0.Tax != money.FromPaise(6315) {
		t.Errorf("settlement[0] amount/fee/tax = %d/%d/%d", s0.Amount.Paise(), s0.Fee.Paise(), s0.Tax.Paise())
	}
	if len(s0.PaymentIDs) != 7 || len(s0.RefundIDs) != 1 {
		t.Errorf("settlement[0] payment_ids/refund_ids = %d/%d, want 7/1", len(s0.PaymentIDs), len(s0.RefundIDs))
	}

	d0 := raw.Disputes[0]
	if d0.ID != "disp_nlKU3LoBNSLEHf" || d0.PaymentID != "pay_0A3mU80ujoi88x" || d0.Status != "lost" {
		t.Errorf("dispute[0] id/payment_id/status = %q/%q/%q", d0.ID, d0.PaymentID, d0.Status)
	}

	if raw.BankFeed.Period != "2026-05" || raw.BankFeed.Account != "XXXXXXXXDTC" {
		t.Errorf("bank feed account/period = %q/%q", raw.BankFeed.Account, raw.BankFeed.Period)
	}
	if raw.BankFeed.Credits[0].Ref != "UTRprSZo6GCwDyjfv" {
		t.Errorf("bank credit[0].Ref = %q, want the first settlement UTR", raw.BankFeed.Credits[0].Ref)
	}
}

// TestOrdersNotNormalizedIntoJournal asserts that orders.json — present under
// razorpay/ as the agent's tax-metadata recovery source (SPEC §2) — is NOT an
// accounting event: ingest does not read it and normalize never emits an order
// event. The journal must contain exactly payments+refunds+settlements+disputes,
// and no event may carry an order_ id (orders are referenced via a payment's
// links, never normalized as their own event). This is what keeps the committed
// 2026-05 golden journal byte-identical after orders.json was added.
func TestOrdersNotNormalizedIntoJournal(t *testing.T) {
	root := repoRoot(t)
	raw, events, err := IngestAndNormalize(root, "dtc", "2026-05")
	if err != nil {
		t.Fatalf("IngestAndNormalize: %v", err)
	}

	wantLen := len(raw.Payments) + len(raw.Refunds) + len(raw.Settlements) + len(raw.Disputes)
	if len(events) != wantLen {
		t.Errorf("journal has %d events, want %d (orders must not be normalized)", len(events), wantLen)
	}
	for _, e := range events {
		if string(e.Type) == "order" {
			t.Errorf("journal contains an order event %s; orders are not accounting events", e.ID)
		}
		if len(e.ID) >= 6 && e.ID[:6] == "order_" {
			t.Errorf("journal event %s has an order_ id; orders must not be normalized", e.ID)
		}
	}
}

// TestReadMissingFile asserts a missing expected fixture is a clear, hard error
// naming the file — never a silently-empty result (a half-seeded period must not
// close as if complete). It is table-driven over which file is absent.
func TestReadMissingFile(t *testing.T) {
	cases := []struct {
		name       string
		removeName string // path relative to the period dir to delete
	}{
		{"payments", "razorpay/payments.json"},
		{"refunds", "razorpay/refunds.json"},
		{"settlements", "razorpay/settlements.json"},
		{"disputes", "razorpay/disputes.json"},
		{"bank feed", "bank-feed.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := seedFixturesInto(t)
			target := filepath.Join(root, "worlds", "dtc", "2026-05", c.removeName)
			if err := os.Remove(target); err != nil {
				t.Fatalf("remove %s: %v", target, err)
			}
			_, err := Read(root, "dtc", "2026-05")
			if err == nil {
				t.Fatalf("Read with missing %s succeeded, want error", c.name)
			}
			if !contains(err.Error(), "not found") {
				t.Errorf("error for missing %s = %q, want a 'not found' message", c.name, err)
			}
		})
	}
}

// TestReadMalformedJSON asserts a corrupt fixture (invalid JSON, or trailing
// garbage after a valid value) is rejected with a decode error naming the file,
// not partially read.
func TestReadMalformedJSON(t *testing.T) {
	cases := []struct {
		name     string
		relPath  string
		contents string
	}{
		{"invalid json", "razorpay/payments.json", "[ {not json} ]"},
		{"trailing garbage", "razorpay/payments.json", "[]\n[]\n"},
		{"object not array", "razorpay/refunds.json", "{}"},
		{"bank feed not object", "bank-feed.json", "[]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := seedFixturesInto(t)
			target := filepath.Join(root, "worlds", "dtc", "2026-05", c.relPath)
			if err := os.WriteFile(target, []byte(c.contents), 0o644); err != nil {
				t.Fatalf("write %s: %v", target, err)
			}
			if _, err := Read(root, "dtc", "2026-05"); err == nil {
				t.Fatalf("Read with %s succeeded, want decode error", c.name)
			}
		})
	}
}

// seedFixturesInto copies the committed worlds/dtc/2026-05 fixtures into a fresh
// temp root so a test can mutate them without touching the repo. It deliberately
// copies only the agent-input files ingest reads (the razorpay/ files and
// bank-feed.json), never truth/, keeping the test within the isolation boundary.
func seedFixturesInto(t *testing.T) string {
	t.Helper()
	src := repoRoot(t)
	dst := t.TempDir()

	files := []string{
		"razorpay/payments.json",
		"razorpay/refunds.json",
		"razorpay/settlements.json",
		"razorpay/disputes.json",
		"bank-feed.json",
	}
	base := filepath.Join("worlds", "dtc", "2026-05")
	for _, f := range files {
		from := filepath.Join(src, base, f)
		to := filepath.Join(dst, base, f)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(to), err)
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatalf("read fixture %s: %v", from, err)
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", to, err)
		}
	}
	return dst
}

// contains is a tiny substring check kept local so the test file stays
// dependency-free.
func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
