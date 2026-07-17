package seed

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/config"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/truth"
)

// Generate is the seeder's single source of activity: from one seeded RNG it
// produces the Razorpay-shaped Fixtures, the independent BankFeed, AND the
// matching ground-truth GL — all from the SAME generation rules, so the truth GL
// is exactly the correct books for the fixtures (SPEC §2, §4.4). It is pure with
// respect to (world, period): same inputs => identical outputs (the RNG is
// seeded from world+period; no wall clock).
//
// # The model (one internally-consistent DTC month)
//
// Activity is generated as a sequence of SETTLEMENT BATCHES across the month. A
// batch is a set of payments captured a couple of days earlier that Razorpay
// settles to the bank on one day, netting its fee + GST-on-fee. Some payments in
// a batch are refunded before settlement (the refund gross is netted out of that
// batch's deposit). Separately, a few captured payments are later DISPUTED and
// lost, which claws cash back out of the bank.
//
// Every event maps to exactly one balanced playbook entry in the truth GL:
//
//	payment            -> dtc_sale         (Dr receivable; Cr sales, gst-output)
//	settlement         -> razorpay_settlement (Dr bank, fees, gst-input; Cr receivable)
//	refund             -> refund_reversal  (Dr sales-returns, gst-output; Cr receivable)
//	lost dispute       -> chargeback_loss  (Dr chargeback-loss, gst-output; Cr bank)
//
// # Why the books tie out (balanced + receivable clears)
//
// For a batch, the receivable accrues +Σgross from its sales and −Σrefund_gross
// from refund reversals; the settlement credits the receivable by exactly the
// remainder (Σgross − Σrefund_gross), so the receivable nets to ~0 at period end
// (SPEC §7 check #3) and net_deposit + fee + gst_on_fee == that remainder, making
// the settlement entry balanced by construction. Disputes touch bank + expense +
// gst only, never the receivable.
func Generate(world, period string) (Fixtures, BankFeed, truth.GL, error) {
	fx, feed, gl, _, _, err := GenerateWith(world, period, Options{})
	return fx, feed, gl, err
}

// GenerateWith is Generate plus the Options knobs (SPEC §1, §2, §5, §7, §12): it
// generates the clean substrate exactly as Generate does — same seeded RNG
// stream, so the clean case is byte-identical — and then applies the requested
// post-generation transforms to the agent-input FIXTURES only:
//
//   - opts.Inject seeds a reconciliation break (see inject.go);
//   - opts.Ambiguity strips gst_rate from a deterministic ~15% of payments,
//     producing the missing-metadata long tail the agent fills (see ambiguity.go).
//
// The hidden truth GL is produced from the CLEAN generation and is NEVER
// perturbed, so it stays balanced and still describes the correct world (every
// sale at its true rate, including any refund a break later hides from a
// settlement batch). orders.json likewise records each payment's true rate. The
// returned InjectResult / AmbiguityResult record what (if anything) was perturbed.
func GenerateWith(world, period string, opts Options) (Fixtures, BankFeed, truth.GL, InjectResult, AmbiguityResult, error) {
	fx, feed, gl, res, err := GenerateFull(world, period, opts)
	return fx, feed, gl, res.Inject, res.Ambiguity, err
}

// Results bundles what the optional generation knobs produced. Adding a knob
// adds a field here instead of growing GenerateWith's return list (which has
// two dozen call sites pinned to the 6-value shape).
type Results struct {
	Inject    InjectResult
	Ambiguity AmbiguityResult
	Partial   PartialResult
}

// GenerateFull is GenerateWith returning the full Results (including the
// partial-refunds designations). New callers should prefer it.
func GenerateFull(world, period string, opts Options) (Fixtures, BankFeed, truth.GL, Results, error) {
	if err := validateInject(opts.Inject); err != nil {
		return Fixtures{}, BankFeed{}, truth.GL{}, Results{}, err
	}

	cal, err := newPeriodCalendar(period)
	if err != nil {
		return Fixtures{}, BankFeed{}, truth.GL{}, Results{}, err
	}

	// The truth GL is built by binding the SAME playbook entry types the rest of
	// the system uses and posting them through the real ledger (SPEC §4.2, §4.4).
	// The embedded default playbook keeps this filesystem-free, so Generate stays
	// pure with respect to (world, period).
	pb, err := config.DefaultPlaybook()
	if err != nil {
		return Fixtures{}, BankFeed{}, truth.GL{}, Results{}, fmt.Errorf("seed: load playbook for truth GL: %w", err)
	}

	g := &generator{
		rng:    NewRNG(world, period),
		cal:    cal,
		world:  world,
		period: period,
		binder: newTruthBinder(pb),
		// Initialise output slices as non-nil so they marshal as JSON arrays
		// ([]) even when empty, keeping the on-disk fixture schema stable.
		payments:    make([]Payment, 0),
		refunds:     make([]Refund, 0),
		settlements: make([]Settlement, 0),
		disputes:    make([]Dispute, 0),
		orders:      make([]Order, 0),
		bankCredits: make([]BankFeedEntry, 0),
		bankDebits:  make([]BankFeedEntry, 0),
		partial:     opts.PartialRefunds,
		partialOut:  make([]PartialRefund, 0),
		cod:         opts.COD,
	}
	g.ids = newIDGen(g.rng)

	if err := g.run(); err != nil {
		return Fixtures{}, BankFeed{}, truth.GL{}, Results{}, err
	}

	gl := truth.GL{
		Version: truth.SchemaVersion,
		World:   world,
		Period:  period,
		Entries: g.binder.entries,
	}
	feed := BankFeed{
		Account: "XXXXXXXX" + maskWorld(world),
		Period:  period,
		Credits: g.bankCredits,
		Debits:  g.bankDebits,
	}
	fx := Fixtures{
		Payments:    g.payments,
		Refunds:     g.refunds,
		Settlements: g.settlements,
		Disputes:    g.disputes,
		Orders:      g.orders,
		Courier:     g.courier,
	}

	// Defensive by-construction check: the truth GL MUST balance. This is a
	// generation invariant, so a failure here is a seeder bug, not bad input. The
	// check runs on the CLEAN truth GL, before any transform — and neither the
	// break injection nor the ambiguity transform touches truth, so truth stays
	// balanced regardless (tests assert it).
	if !gl.IsBalanced() {
		dr, cr := gl.SumBySide()
		return Fixtures{}, BankFeed{}, truth.GL{}, Results{},
			fmt.Errorf("seed: internal error — generated truth GL does not balance (ΣDr=%s ΣCr=%s)", dr, cr)
	}

	// Seed any requested reconciliation break by perturbing the agent-input
	// fixtures only (SPEC §5, §7). truth GL and bank feed are left as generated;
	// the break lives in the inconsistency between them and the perturbed fixture.
	inj, err := applyInject(opts.Inject, &fx)
	if err != nil {
		return Fixtures{}, BankFeed{}, truth.GL{}, Results{}, err
	}

	// Seed the missing-metadata long tail by stripping gst_rate from a
	// deterministic ~15% of payments (SPEC §1, §2). Drawn from a SEPARATE RNG, so
	// the clean stream — and truth/gl.json + orders.json — is untouched; the
	// stripped payments' true rates survive in their orders for the agent to fetch.
	amb := applyAmbiguity(opts.Ambiguity, world, period, &fx)

	return fx, feed, gl, Results{Inject: inj, Ambiguity: amb, Partial: PartialResult{Refunds: g.partialOut}}, nil
}

// generator carries the RNG, calendar, id minter, and the growing output slices
// through the generation rules. Keeping it a struct lets the per-batch helpers
// append to shared slices in a fixed order, which keeps the stream and output
// deterministic.
type generator struct {
	rng    *RNG
	cal    periodCalendar
	ids    *idGen
	world  string
	period string

	// Razorpay-shaped fixtures (agent input).
	payments    []Payment
	refunds     []Refund
	settlements []Settlement
	disputes    []Dispute
	orders      []Order

	// Independent bank feed (agent input).
	bankCredits []BankFeedEntry
	bankDebits  []BankFeedEntry

	// binder builds the ground-truth GL (scorer only) by binding playbook entry
	// types and posting them through the real ledger, in posting order.
	binder *truthBinder

	// Partial-refunds world (partial.go). partial gates the generation knob;
	// partialOut records the designated refunds in designation order.
	partial    bool
	partialOut []PartialRefund

	// COD rail (cod.go). cod gates the generation knob; courier holds the emitted
	// courier feed (nil when the knob is off).
	cod     bool
	courier *CourierFeed
}

// Generation-rule constants. These fix the SHAPE of a synthetic month; changing
// them changes the substrate but not its determinism. They are integers (no
// floats) so all derived money math stays exact.
const (
	numBatches       = 6      // settlement batches across the month
	paymentsLo       = 3      // min payments per batch
	paymentsHi       = 7      // max payments per batch
	grossLoPaise     = 49900  // ₹499.00 minimum order gross
	grossHiPaise     = 499900 // ₹4,999.00 maximum order gross
	feeBps           = 200    // Razorpay fee = 2.00% of gross
	refundChanceNum  = 1      // ~1-in-6 payments in a batch get refunded
	refundChanceDen  = 6
	disputeChanceNum = 1 // ~1-in-25 payments are disputed and lost
	disputeChanceDen = 25
	captureLeadDays  = 2 // payments captured this many days before settlement
	disputeLagDays   = 5 // a lost dispute hits the bank this many days after capture
)

// skuOptions is the fixed catalogue of product SKUs a payment may carry in its
// notes (SPEC §4.3). Picked from deterministically.
var skuOptions = []string{"SERUM-30", "CREAM-50", "TONER-200", "MASK-5PK", "KIT-DELUXE"}

// methodOptions is the fixed set of Razorpay payment methods. Picked from
// deterministically.
var methodOptions = []string{"upi", "card", "netbanking"}

// run drives the whole month: one settlement batch per settlement day. Draw
// order is fixed (batch by batch, payment by payment) so the RNG stream — and
// therefore every id, amount, and date — is reproducible. It returns an error
// only if a truth-GL bind/post fails, which is a generation bug (the binder
// guarantees balanced entries by construction).
func (g *generator) run() error {
	// Spread the batches across the month: batch i settles on a day roughly
	// i/numBatches through the month, in increasing order.
	for i := 0; i < numBatches; i++ {
		settleDayOffset := g.batchSettleDayOffset(i)
		if err := g.generateBatch(settleDayOffset); err != nil {
			return err
		}
	}
	// The partial-refunds world needs all three judgment slots filled; fewer
	// refunds than designations is a generation-shape regression to fail loudly
	// on (mirrors the inject.go "cannot inject" errors).
	if g.partial && len(g.partialOut) < len(partialClasses) {
		return fmt.Errorf("seed: partial-refunds world produced only %d of %d designated refunds",
			len(g.partialOut), len(partialClasses))
	}
	// The COD rail (cod.go) is appended after the Razorpay batches so a COD period
	// has both rails; periods without the knob never draw from the RNG here, so
	// they stay byte-identical.
	if g.cod {
		if err := g.generateCOD(); err != nil {
			return err
		}
	}
	return nil
}

// batchSettleDayOffset returns the day-offset (0-based from the 1st) on which
// batch i settles, spread evenly across the month with a small deterministic
// jitter. It never exceeds the last day of the month for the settlement itself.
func (g *generator) batchSettleDayOffset(i int) int {
	// Evenly space batches; +1 so the first batch settles a few days in (after
	// its payments are captured), leaving room for the captureLeadDays.
	base := captureLeadDays + 1 + (g.cal.daysInMon-captureLeadDays-2)*i/numBatches
	jitter := g.rng.IntRange(0, 1)
	off := base + jitter
	if off > g.cal.daysInMon-1 {
		off = g.cal.daysInMon - 1
	}
	return off
}

// generateBatch produces one settlement batch: a handful of captured payments,
// any refunds netted in the batch, the settlement payout itself + its bank
// credit, plus the truth-GL entries for each (bound through the playbook + posted
// via the ledger). It also seeds occasional lost disputes against the batch's
// payments (with their own bank debit + GL entry). It returns an error only if a
// truth-GL bind/post fails (a generation bug).
func (g *generator) generateBatch(settleDayOffset int) error {
	captureDayOffset := settleDayOffset - captureLeadDays
	if captureDayOffset < 0 {
		captureDayOffset = 0
	}
	captureTs := g.cal.epochForDayOffset(captureDayOffset)
	settleTs := g.cal.epochForDayOffset(settleDayOffset)

	n := g.rng.IntRange(paymentsLo, paymentsHi)

	var (
		grossSum    money.Money // Σ payment gross in batch
		feeSum      money.Money // Σ Razorpay fee in batch
		taxSum      money.Money // Σ GST-on-fee in batch
		refundGross money.Money // Σ refund gross netted out of this batch
	)
	// payment_ids / refund_ids are initialised as non-nil empty slices so they
	// always marshal as JSON arrays ([]), never null — a clean, stable fixture
	// schema regardless of whether a batch happened to have any refunds.
	paymentIDs := make([]string, 0, n)
	refundIDs := make([]string, 0)

	for k := 0; k < n; k++ {
		pay := g.makePayment(captureTs)
		paymentIDs = append(paymentIDs, pay.ID)
		g.payments = append(g.payments, pay)

		// Emit the authoritative order this payment was captured against (SPEC
		// §2). It mirrors the payment's TRUE notes (sku + gst_rate) and amount, so
		// even after the ambiguity transform later strips a payment's gst_rate the
		// order still holds the true rate the agent recovers from. Built from the
		// payment value (no new RNG draw) so the clean RNG stream is unchanged.
		g.orders = append(g.orders, g.makeOrder(pay))

		grossSum = grossSum.Add(pay.Amount)
		feeSum = feeSum.Add(pay.Fee)
		taxSum = taxSum.Add(pay.Tax)

		// Truth GL: the dtc_sale entry for this payment.
		net, gst := splitGSTInclusive(pay.Amount, gstRatePercentOf(pay))
		if err := g.addSaleEntry(pay, net, gst); err != nil {
			return err
		}

		// A fraction of payments are refunded before settlement; the refund is
		// netted out of this batch's deposit.
		if g.rng.Chance(refundChanceNum, refundChanceDen) {
			rf := g.makeRefund(pay, settleTs)
			// In the partial-refunds world the first three refunds become the
			// R1/R2/R3 judgment spectrum (partial.go): the amount is mutated BEFORE
			// any downstream use, so batch netting, the deposit, the bank feed, and
			// truth all flow from the same partial value by construction.
			entryType := "refund_reversal"
			if g.partial && len(g.partialOut) < len(partialClasses) {
				class, err := g.designatePartial(&rf, pay)
				if err != nil {
					return err
				}
				if class != PartialItemMatch {
					entryType = "price_adjustment"
				}
			}
			g.refunds = append(g.refunds, rf)
			refundIDs = append(refundIDs, rf.ID)
			refundGross = refundGross.Add(rf.Amount)
			rnet, rgst := splitGSTInclusive(rf.Amount, gstRatePercentOf(pay))
			if err := g.addRefundEntryAs(entryType, rf, rnet, rgst); err != nil {
				return err
			}
		}

		// A small fraction of payments are later disputed and lost; this is an
		// independent cash claw-back from the bank (not part of this settlement).
		if g.rng.Chance(disputeChanceNum, disputeChanceDen) {
			disp := g.makeDispute(pay, captureDayOffset)
			g.disputes = append(g.disputes, disp)
			dnet, dgst := splitGSTInclusive(disp.Amount, gstRatePercentOf(pay))
			if err := g.addDisputeEntry(disp, dnet, dgst); err != nil {
				return err
			}
			g.addBankDebitForDispute(disp)
		}
	}

	// The settlement deposits the batch gross less refunds, fees, and GST-on-fee.
	// grossBatch is what remains owed on the receivable for this batch after
	// refunds; net_deposit = grossBatch − fee − tax.
	grossBatch := grossSum.Sub(refundGross)
	netDeposit := grossBatch.Sub(feeSum).Sub(taxSum)

	setl := g.makeSettlement(netDeposit, feeSum, taxSum, settleTs, paymentIDs, refundIDs)
	g.settlements = append(g.settlements, setl)
	if err := g.addSettlementEntry(setl, grossBatch); err != nil {
		return err
	}
	g.addBankCreditForSettlement(setl)
	return nil
}

// maskWorld returns a short, stable suffix derived from the world name for the
// masked bank-account label. It is deterministic (first up-to-4 upper-cased
// bytes), not random.
func maskWorld(world string) string {
	up := make([]byte, 0, 4)
	for i := 0; i < len(world) && len(up) < 4; i++ {
		c := world[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		up = append(up, c)
	}
	if len(up) == 0 {
		return "0000"
	}
	return string(up)
}
