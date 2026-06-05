package seed

import (
	"testing"

	"github.com/razorpay/close-agent/internal/truth"
)

// hardWorld/hardPeriod are the committed missing-metadata hard period (SPEC §11
// Phase 7): worlds/dtc/2026-04, seeded with --ambiguity.
const (
	hardWorld  = "dtc"
	hardPeriod = "2026-04"
)

// TestAmbiguityStripsRoughly15Percent asserts the ambiguity transform strips
// gst_rate from a deterministic count of payments that lands near 15% (SPEC §1,
// §2). The count is a deterministic function of (world, period); we recompute it
// with the same selection logic and assert (a) the result matches that count
// exactly, (b) it equals the number of payments whose notes.gst_rate is now
// empty, and (c) it is within a sane band around 15% (no runaway).
func TestAmbiguityStripsRoughly15Percent(t *testing.T) {
	fx, _, _, _, amb, err := GenerateWith(hardWorld, hardPeriod, Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("GenerateWith ambiguity: %v", err)
	}
	if !amb.Enabled {
		t.Fatal("AmbiguityResult.Enabled = false, want true when Ambiguity requested")
	}

	// (a) The reported count matches an independent recomputation of the selection.
	want := expectedStripCount(t, hardWorld, hardPeriod)
	if amb.NumStripped != want {
		t.Errorf("NumStripped = %d, want %d (deterministic selection)", amb.NumStripped, want)
	}
	if len(amb.PaymentIDs) != amb.NumStripped {
		t.Errorf("PaymentIDs has %d ids, NumStripped = %d", len(amb.PaymentIDs), amb.NumStripped)
	}

	// (b) Exactly NumStripped payments now have an empty gst_rate.
	emptyRate := 0
	for _, p := range fx.Payments {
		if p.Notes.GSTRate == "" {
			emptyRate++
		}
	}
	if emptyRate != amb.NumStripped {
		t.Errorf("%d payments have empty gst_rate, NumStripped = %d", emptyRate, amb.NumStripped)
	}

	// (c) The strip fraction is in a sane band around 15% (5%..30%), guarding the
	// probability/seed from a regression that would strip none or nearly all.
	frac10000 := amb.NumStripped * 10000 / len(fx.Payments) // basis points, integer
	if frac10000 < 500 || frac10000 > 3000 {
		t.Errorf("stripped %d/%d = %d bps, want within [500, 3000] bps (~15%%)",
			amb.NumStripped, len(fx.Payments), frac10000)
	}
	if amb.NumStripped == 0 {
		t.Error("ambiguity stripped zero payments; the long tail would be empty")
	}
}

// TestAmbiguityDeterministic asserts the same (world, period) strips the SAME
// payments every time (SPEC §2, §12 determinism) — both the count and the exact
// set of ids, in the same order.
func TestAmbiguityDeterministic(t *testing.T) {
	_, _, _, _, a, err := GenerateWith(hardWorld, hardPeriod, Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("GenerateWith #1: %v", err)
	}
	_, _, _, _, b, err := GenerateWith(hardWorld, hardPeriod, Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("GenerateWith #2: %v", err)
	}
	if a.NumStripped != b.NumStripped {
		t.Fatalf("strip count differs across runs: %d vs %d", a.NumStripped, b.NumStripped)
	}
	if !equalSlices(a.PaymentIDs, b.PaymentIDs) {
		t.Errorf("stripped ids differ across runs:\n %v\n %v", a.PaymentIDs, b.PaymentIDs)
	}
}

// TestAmbiguityKeepsSkuAndOrderAndTrueRate is the core SPEC §2 invariant: a
// stripped payment loses ONLY its gst_rate — its sku and its order_id stay intact
// — and the order it references still carries the TRUE rate (so the agent can
// "fetch the order" to recover it without ever reading truth).
func TestAmbiguityKeepsSkuAndOrderAndTrueRate(t *testing.T) {
	clean, _, _, err := Generate(hardWorld, hardPeriod)
	if err != nil {
		t.Fatalf("clean Generate: %v", err)
	}
	fx, _, _, _, amb, err := GenerateWith(hardWorld, hardPeriod, Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("GenerateWith ambiguity: %v", err)
	}

	cleanPayByID := map[string]Payment{}
	for _, p := range clean.Payments {
		cleanPayByID[p.ID] = p
	}
	payByID := map[string]Payment{}
	for _, p := range fx.Payments {
		payByID[p.ID] = p
	}
	orderByID := map[string]Order{}
	for _, o := range fx.Orders {
		orderByID[o.ID] = o
	}

	strippedSet := map[string]bool{}
	for _, id := range amb.PaymentIDs {
		strippedSet[id] = true
		p := payByID[id]
		cp := cleanPayByID[id]

		if p.Notes.GSTRate != "" {
			t.Errorf("stripped payment %s still has gst_rate %q", id, p.Notes.GSTRate)
		}
		if p.Notes.SKU == "" || p.Notes.SKU != cp.Notes.SKU {
			t.Errorf("stripped payment %s sku changed: %q -> %q", id, cp.Notes.SKU, p.Notes.SKU)
		}
		if p.OrderID == "" || p.OrderID != cp.OrderID {
			t.Errorf("stripped payment %s order_id changed: %q -> %q", id, cp.OrderID, p.OrderID)
		}
		// The order holds the TRUE rate (the rate the clean payment had).
		o, ok := orderByID[p.OrderID]
		if !ok {
			t.Errorf("stripped payment %s references order %s with no matching order", id, p.OrderID)
			continue
		}
		if o.Notes.GSTRate == "" {
			t.Errorf("order %s for stripped payment %s has no gst_rate; recovery impossible", o.ID, id)
		}
		if o.Notes.GSTRate != cp.Notes.GSTRate {
			t.Errorf("order %s gst_rate %q != stripped payment's TRUE rate %q", o.ID, o.Notes.GSTRate, cp.Notes.GSTRate)
		}
	}

	// Every NON-stripped payment is byte-equal to clean (only the selected ~15%
	// were touched), and every order is byte-equal to clean (orders never stripped).
	for _, p := range fx.Payments {
		if strippedSet[p.ID] {
			continue
		}
		cp := cleanPayByID[p.ID]
		if p.Notes.GSTRate != cp.Notes.GSTRate {
			t.Errorf("non-stripped payment %s gst_rate changed: %q -> %q", p.ID, cp.Notes.GSTRate, p.Notes.GSTRate)
		}
	}
}

// TestAmbiguityLeavesTruthAndOrdersIntact asserts the ambiguity transform NEVER
// touches the hidden truth GL or orders.json (SPEC §2, §4.4): the seeder knows
// the true rate, so truth still books every sale correctly and balances, and the
// ambiguous-vs-clean GLs are byte-identical. Likewise orders are byte-identical.
func TestAmbiguityLeavesTruthAndOrdersIntact(t *testing.T) {
	cleanFx, _, cleanGL, _, _, err := GenerateWith(hardWorld, hardPeriod, Options{})
	if err != nil {
		t.Fatalf("clean GenerateWith: %v", err)
	}
	ambFx, _, ambGL, _, amb, err := GenerateWith(hardWorld, hardPeriod, Options{Ambiguity: true})
	if err != nil {
		t.Fatalf("ambiguous GenerateWith: %v", err)
	}
	if amb.NumStripped == 0 {
		t.Fatal("ambiguity stripped nothing; the rest of this test is vacuous")
	}

	// Truth GL: byte-identical and still balanced.
	cb, _ := MarshalStable(cleanGL)
	ab, _ := MarshalStable(ambGL)
	if string(cb) != string(ab) {
		t.Error("ambiguity changed the truth GL; truth must be left intact")
	}
	if !ambGL.IsBalanced() {
		dr, cr := ambGL.SumBySide()
		t.Errorf("truth GL under ambiguity does not balance: Dr=%s Cr=%s", dr, cr)
	}
	// Truth still books a dtc_sale for every stripped payment at its true rate.
	for _, id := range amb.PaymentIDs {
		if !truthHasSaleEntry(ambGL.Entries, id) {
			t.Errorf("truth GL missing the dtc_sale for stripped payment %s", id)
		}
	}

	// Orders: byte-identical (orders are never stripped; the true rate survives).
	co, _ := MarshalStable(cleanFx.Orders)
	ao, _ := MarshalStable(ambFx.Orders)
	if string(co) != string(ao) {
		t.Error("ambiguity changed orders.json; orders must be left intact")
	}
}

// TestAmbiguityOffByteIdenticalToClean asserts the no-ambiguity path is
// byte-identical to the legacy Generate, so the committed clean 2026-05 fixtures
// and goldens are unaffected by the ambiguity plumbing.
func TestAmbiguityOffByteIdenticalToClean(t *testing.T) {
	a, fa, ga, err := Generate("dtc", "2026-05")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, fb, gb, _, amb, err := GenerateWith("dtc", "2026-05", Options{Ambiguity: false})
	if err != nil {
		t.Fatalf("GenerateWith(ambiguity=false): %v", err)
	}
	if amb.Enabled {
		t.Error("AmbiguityResult.Enabled = true with Ambiguity:false")
	}
	for _, pair := range []struct {
		name string
		x, y any
	}{
		{"payments", a.Payments, b.Payments},
		{"refunds", a.Refunds, b.Refunds},
		{"settlements", a.Settlements, b.Settlements},
		{"disputes", a.Disputes, b.Disputes},
		{"orders", a.Orders, b.Orders},
		{"bank-feed", fa, fb},
		{"truth-gl", ga, gb},
	} {
		bx, _ := MarshalStable(pair.x)
		by, _ := MarshalStable(pair.y)
		if string(bx) != string(by) {
			t.Errorf("%s differs between Generate and GenerateWith(ambiguity=false)", pair.name)
		}
	}
}

// expectedStripCount recomputes, independently of GenerateWith, how many payments
// the ambiguity selection strips — using the SAME dedicated RNG and per-payment
// probability over the clean payments in fixture order. It is the oracle for the
// deterministic-count assertion.
func expectedStripCount(t *testing.T, world, period string) int {
	t.Helper()
	clean, _, _, err := Generate(world, period)
	if err != nil {
		t.Fatalf("clean Generate for oracle: %v", err)
	}
	rng := newAmbiguityRNG(world, period)
	n := 0
	for _, p := range clean.Payments {
		if p.Notes.GSTRate == "" {
			continue
		}
		if rng.Chance(ambiguityNum, ambiguityDen) {
			n++
		}
	}
	return n
}

// truthHasSaleEntry reports whether the truth GL includes a dtc_sale attributed
// to the given payment id, proving the stripped sale is still booked in truth.
func truthHasSaleEntry(entries []truth.Entry, paymentID string) bool {
	for _, e := range entries {
		if e.EntryType == "dtc_sale" && e.EventID == paymentID {
			return true
		}
	}
	return false
}
