package seed

// This file implements the deterministic AMBIGUITY (missing-metadata) long tail
// of SPEC §1 / §2 ("an event no rule covers — e.g. missing tax metadata — → the
// agent reasons from context (fetch the order) and picks the entry type") and
// SPEC §11 Phase 7. Like a reconciliation break (see inject.go), the ambiguity
// is a post-generation TRANSFORM of the otherwise-clean substrate:
//
//  1. the deterministic generator runs UNTOUCHED, so the RNG stream, ids,
//     amounts, dates, the truth GL, AND the authoritative orders.json (built from
//     each payment's true notes) are byte-identical to a clean seed; THEN
//  2. the ambiguity transform STRIPS the gst_rate from a deterministic ~15% of
//     payments' notes, leaving sku and order_id intact.
//
// The hidden truth GL is built from the CLEAN generation and is never touched, so
// it still records every sale at its true rate (the seeder knows the true rate).
// The matching order in orders.json also still carries the true rate. The result
// is that close --agent off rule-MISSES every stripped payment (classify cannot
// split GST without a rate; see internal/classify), scoring PARTIAL; and a later
// agent can RECOVER the true rate by fetching the order — not by reading truth.
//
// Determinism (SPEC §2, §12): the selection of which payments to strip is drawn
// from a SEPARATE RNG seeded from (world, period, ambiguitySalt), so it never
// perturbs the generator's main stream — turning ambiguity OFF reproduces the
// clean substrate byte-for-byte. Same (world, period) ⇒ same payments stripped.

// ambiguity tuning constants. ambiguityNum/ambiguityDen is the per-payment
// probability a payment's gst_rate is stripped (~15%). The probability is applied
// per payment in fixture order via the dedicated RNG, so the count is a
// deterministic function of (world, period) and lands near 15% of payments.
const (
	ambiguityNum  = 3
	ambiguityDen  = 20 // 3/20 = 15%
	ambiguitySalt = "ambiguity"
)

// AmbiguityResult records what the ambiguity transform stripped: the count of
// affected payments and their ids (in fixture order), so the CLI can report it
// and a test can assert the expected payments were targeted and that each one's
// order still holds the true rate. It is the zero value when ambiguity was off.
type AmbiguityResult struct {
	Enabled     bool     // whether the ambiguity transform ran
	NumStripped int      // payments whose gst_rate was removed
	PaymentIDs  []string // ids of the stripped payments, in fixture order
}

// newAmbiguityRNG builds the dedicated, isolated RNG that selects which payments
// to strip. Seeding it from (world, period, ambiguitySalt) keeps the selection
// deterministic per period while keeping it OFF the generator's main stream, so
// the clean substrate is unchanged whether or not ambiguity is requested.
func newAmbiguityRNG(world, period string) *RNG {
	return NewRNG(world+"\x00"+ambiguitySalt, period)
}

// applyAmbiguity strips the gst_rate from a deterministic ~15% of payments in the
// freshly generated fixtures, in place. It runs AFTER Generate has produced the
// clean substrate (and the orders, which captured the true rate), so it perturbs
// a known-good world. It draws its selection from the dedicated ambiguity RNG, so
// the main generation stream — and therefore truth/gl.json, orders.json, and
// every other field — is untouched. sku and order_id are intentionally left
// intact: only the gst_rate string is cleared, modelling a payment whose tax
// metadata never got stamped while the order still records it.
//
// It returns the ids of the stripped payments. It is a no-op (returning a
// disabled result) when enabled is false.
func applyAmbiguity(enabled bool, world, period string, fx *Fixtures) AmbiguityResult {
	if !enabled {
		return AmbiguityResult{}
	}
	rng := newAmbiguityRNG(world, period)
	res := AmbiguityResult{Enabled: true, PaymentIDs: make([]string, 0)}
	for i := range fx.Payments {
		p := &fx.Payments[i]
		// Only a payment that actually has a gst_rate can have it stripped; this is
		// always true for the seeder's payments, but guarding keeps the count honest
		// if a future payment is minted without one.
		if p.Notes.GSTRate == "" {
			continue
		}
		if rng.Chance(ambiguityNum, ambiguityDen) {
			p.Notes.GSTRate = "" // strip ONLY the gst_rate; sku + order_id stay intact
			res.NumStripped++
			res.PaymentIDs = append(res.PaymentIDs, p.ID)
		}
	}
	return res
}
