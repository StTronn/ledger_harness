// Package ingest reads a seeded period's substrate off disk and flattens the
// Razorpay-shaped objects into the ordered, normalized event journal of SPEC
// §4.3. It is the first stage of the close workflow (SPEC §5):
//
//	raw    = ingest(world, period)   // this package: read fixtures
//	events = normalize(raw)          // this package: → ordered event journal
//
// # Fixtures only (Phase 3); live api later (Phase 9)
//
// In v1 Phase 3 ingest reads ONLY the committed fixtures under
// worlds/<world>/<period>/ (no live Razorpay). The raw Go types in this file
// mirror the razorpay-cli/api object shapes (entity, id, amount in integer
// paise, fee, tax, created_at epoch seconds, notes, status, utr, …) so the
// Phase 9 live path can swap the SOURCE of these bytes — a real api response or
// a fixture file — without changing normalize. We deliberately do NOT import
// internal/seed: ingest and seed communicate only through the on-disk JSON
// contract, and a golden test guards against the two drifting apart. (razorpay-
// cli/api itself returns untyped JSON, not Go structs, so there is nothing to
// import there either; we model the shapes here.)
//
// # truth/ isolation (SPEC §4.4, §12)
//
// ingest MUST NOT read or import internal/truth. It reads only the agent-input
// fixtures (the razorpay/ files and bank-feed.json), never truth/gl.json. The
// package import graph is policed by internal/truth/isolation_test.go.
//
// # Money invariant (SPEC §1, §4)
//
// Every amount is integer minor units — paise — as internal/money.Money
// (int64). No float type touches money in this package.
package ingest

import "github.com/razorpay/close-agent/internal/money"

// RawPayment is a Razorpay-shaped captured payment (id prefix pay_). It mirrors
// the fixture JSON written by the seeder (and the live api payment object): the
// gross the customer paid (Amount, GST-inclusive paise), Razorpay's processing
// fee on that gross (Fee), the GST on that fee (Tax), and the order metadata in
// Notes (sku + gst_rate). These are the fields normalize consumes for SPEC
// §4.3; unknown fields in the source are tolerated (we do not DisallowUnknown
// here, so a richer live api object still decodes).
type RawPayment struct {
	Entity    string      `json:"entity"`     // always "payment"
	ID        string      `json:"id"`         // pay_XXXXXXXXXXXXXX
	Amount    money.Money `json:"amount"`     // gross paid by customer, paise (GST-inclusive)
	Currency  string      `json:"currency"`   // "INR" (v1 is INR-only, SPEC §2)
	Status    string      `json:"status"`     // "captured"
	OrderID   string      `json:"order_id"`   // order_XXXXXXXXXXXXXX
	Method    string      `json:"method"`     // upi / card / netbanking
	Captured  bool        `json:"captured"`   // true for settled-into-books payments
	Fee       money.Money `json:"fee"`        // Razorpay fee on gross, paise
	Tax       money.Money `json:"tax"`        // GST on the Razorpay fee, paise
	CreatedAt int64       `json:"created_at"` // Unix seconds (deterministic, not wall-clock)
	Notes     RawNotes    `json:"notes"`      // sku + gst_rate
}

// RawRefund is a Razorpay-shaped refund (id prefix rfnd_) against a captured
// payment. Amount is the gross refunded to the customer (paise); PaymentID
// links it back to the original payment so normalize can carry the link.
type RawRefund struct {
	Entity    string      `json:"entity"`     // always "refund"
	ID        string      `json:"id"`         // rfnd_XXXXXXXXXXXXXX
	Amount    money.Money `json:"amount"`     // gross refunded, paise (GST-inclusive)
	Currency  string      `json:"currency"`   // "INR"
	PaymentID string      `json:"payment_id"` // pay_... this refund reverses
	Status    string      `json:"status"`     // "processed"
	CreatedAt int64       `json:"created_at"` // Unix seconds
	Notes     RawNotes    `json:"notes"`      // sku + gst_rate copied from the payment
}

// RawSettlement is a Razorpay-shaped settlement payout (id prefix setl_):
// Razorpay nets its fees out of the gross receivable and deposits the remainder
// to the bank. Amount is the NET deposited; Fee and Tax are the batch-aggregate
// Razorpay fee and GST-on-fee netted out. PaymentIDs/RefundIDs carry the batch
// members so the Phase 5 batch-sum recon check can verify the deposit.
type RawSettlement struct {
	Entity     string      `json:"entity"`      // always "settlement"
	ID         string      `json:"id"`          // setl_XXXXXXXXXXXXXX
	Amount     money.Money `json:"amount"`      // NET deposited to bank, paise
	Currency   string      `json:"currency"`    // "INR"
	Status     string      `json:"status"`      // "processed"
	Fee        money.Money `json:"fee"`         // aggregate Razorpay fee in batch, paise
	Tax        money.Money `json:"tax"`         // aggregate GST-on-fee in batch, paise
	UTR        string      `json:"utr"`         // bank UTR; matches a bank-feed credit ref
	CreatedAt  int64       `json:"created_at"`  // Unix seconds (settlement date)
	PaymentIDs []string    `json:"payment_ids"` // pay_... captured in this batch
	RefundIDs  []string    `json:"refund_ids"`  // rfnd_... netted in this batch
}

// RawDispute is a Razorpay-shaped dispute/chargeback (id prefix disp_) against a
// payment. Amount is the disputed gross (paise); PaymentID links the original
// payment; Status "lost" means the merchant lost it (Phase 4 books a
// chargeback_loss), other statuses are open/won.
type RawDispute struct {
	Entity    string      `json:"entity"`     // always "dispute"
	ID        string      `json:"id"`         // disp_XXXXXXXXXXXXXX
	Amount    money.Money `json:"amount"`     // disputed gross, paise
	Currency  string      `json:"currency"`   // "INR"
	PaymentID string      `json:"payment_id"` // pay_... under dispute
	Status    string      `json:"status"`     // "lost" | "won" | "open"
	Reason    string      `json:"reason"`     // chargeback reason code
	CreatedAt int64       `json:"created_at"` // Unix seconds
	Notes     RawNotes    `json:"notes"`      // sku + gst_rate copied from the payment
}

// RawNotes mirrors Razorpay's free-form notes map, typed to the two keys the
// substrate stamps (SPEC §4.3 "notes incl. sku + gst_rate"): the product SKU and
// the GST rate percent as a string ("18", "12", "5"). Modelled as a struct (not
// a map) so the key order is fixed when re-marshalled into a normalized event's
// notes, keeping the journal byte-stable (SPEC §12 determinism).
type RawNotes struct {
	SKU     string `json:"sku"`
	GSTRate string `json:"gst_rate"` // percent as a string, e.g. "18"
}

// RawBankFeed is the independent second record of cash movement (SPEC §4.4): the
// merchant's bank statement for the period, with credits (settlement deposits)
// and debits (chargeback claw-backs). It is an AGENT INPUT for Phase 5 recon,
// NOT part of the §4.3 event journal — normalize ignores it and Raw carries it
// through to the reconcile stage.
type RawBankFeed struct {
	Account string            `json:"account"` // masked bank account label
	Period  string            `json:"period"`  // YYYY-MM this statement covers
	Credits []RawBankFeedLine `json:"credits"` // money in (settlement deposits)
	Debits  []RawBankFeedLine `json:"debits"`  // money out (chargeback claw-backs)
}

// RawBankFeedLine is one line on the bank statement: a positive Amount (paise),
// a value Date (YYYY-MM-DD), and a Ref the settlement/dispute is matched on (the
// settlement UTR for a credit; the dispute id for a debit). Direction is which
// list the line sits in, never the sign of Amount.
type RawBankFeedLine struct {
	Amount money.Money `json:"amount"` // positive paise; direction is the list it's in
	Date   string      `json:"date"`   // YYYY-MM-DD value date
	Ref    string      `json:"ref"`    // UTR (credit) or dispute id (debit) to match on
}

// Raw is the bundle ingest returns: the four Razorpay-shaped slices plus the
// independent bank feed for one (world, period). The slices feed normalize into
// the §4.3 event journal; BankFeed is held for Phase 5 reconcile and is NOT part
// of the journal. Slices are non-nil even when a fixture file is an empty array,
// so callers can range over them without nil checks.
type Raw struct {
	Payments    []RawPayment
	Refunds     []RawRefund
	Settlements []RawSettlement
	Disputes    []RawDispute
	BankFeed    RawBankFeed
}
