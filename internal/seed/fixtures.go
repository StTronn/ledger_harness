package seed

import "github.com/razorpay/ledger-flow/internal/money"

// This file declares the Razorpay-shaped fixture types and the independent bank
// feed — the seeder's AGENT INPUTS (SPEC §4.4). Field names, id prefixes, and
// the int-paise amount convention are modelled on the real Razorpay API objects
// (entity, id, amount in paise, fee, tax, created_at epoch seconds, notes,
// status) so ingest/normalize can later read either fixtures or live api
// responses through the same shapes. We do NOT import razorpay-cli/api (its
// client returns raw JSON, not typed structs); we only mirror its object shapes.
//
// MONEY: every amount is integer paise (money.Money / int64), never a float
// (SPEC §1). money.Money marshals as its underlying int64, so the on-disk JSON
// carries plain integers like the real API.
//
// JSON KEY ORDERING: struct field order fixes the marshalled key order, and the
// writer marshals with stable indentation, so fixtures are byte-stable across
// runs (SPEC §12 determinism).

// Payment is a Razorpay-shaped captured payment (id prefix pay_). amount is the
// gross the customer paid in paise (GST-inclusive); fee is Razorpay's processing
// fee on that gross; tax is Razorpay's GST on its fee (SPEC §4.3 "tax = GST on
// Razorpay's fee"). Notes carry the SKU and the GST rate the product was sold at
// (mirroring how a merchant stamps order metadata onto a payment).
type Payment struct {
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
	Notes     Notes       `json:"notes"`      // sku + gst_rate
}

// Order is a Razorpay-shaped order (id prefix order_): the merchant-created
// order a payment is captured against. Unlike a payment's free-form notes (which
// a real integration may stamp inconsistently or omit), the order is the
// AUTHORITATIVE record of what was sold and at what tax rate — it carries the
// SKU, the GST rate, and the order amount the customer was charged (SPEC §1, §2:
// when a payment is missing tax metadata, the agent "fetches the order" to
// recover it). It is therefore the legitimate, snapshotted recovery source the
// classify-fallback agent reads on a rule miss; it is NOT ground truth (the
// scorer-only truth/gl.json), just another agent-input fixture under razorpay/.
//
// One order is emitted per payment, and Payment.OrderID references Order.ID. The
// order's GSTRate/SKU are captured at the payment's TRUE values at generation
// time, BEFORE any ambiguity transform strips a payment's notes — so even after
// the gst_rate is removed from a payment's notes, the order still holds the true
// rate (which is what makes recovery-from-order possible without reading truth).
type Order struct {
	Entity    string      `json:"entity"`     // always "order"
	ID        string      `json:"id"`         // order_XXXXXXXXXXXXXX
	Amount    money.Money `json:"amount"`     // order amount, paise (== payment gross, GST-inclusive)
	Currency  string      `json:"currency"`   // "INR"
	Status    string      `json:"status"`     // "paid"
	Receipt   string      `json:"receipt"`    // merchant receipt no. (deterministic)
	CreatedAt int64       `json:"created_at"` // Unix seconds (order precedes its payment)
	Notes     Notes       `json:"notes"`      // AUTHORITATIVE sku + gst_rate
	// Items is the order's line-item breakdown, present ONLY in the
	// partial-refunds world (omitempty keeps the committed clean worlds
	// byte-identical). Same rate across items in v1; amounts sum to Amount.
	Items []OrderItem `json:"items,omitempty"`
}

// Refund is a Razorpay-shaped refund (id prefix rfnd_) against a captured
// payment. amount is the gross refunded to the customer in paise; PaymentID
// links it back to the original payment. Refunds reduce a later settlement batch
// or are clawed back, which the truth GL books as a refund_reversal.
type Refund struct {
	Entity    string      `json:"entity"`     // always "refund"
	ID        string      `json:"id"`         // rfnd_XXXXXXXXXXXXXX
	Amount    money.Money `json:"amount"`     // gross refunded, paise (GST-inclusive)
	Currency  string      `json:"currency"`   // "INR"
	PaymentID string      `json:"payment_id"` // pay_... this refund reverses
	Status    string      `json:"status"`     // "processed"
	CreatedAt int64       `json:"created_at"` // Unix seconds
	Notes     Notes       `json:"notes"`      // sku + gst_rate copied from the payment
}

// Settlement is a Razorpay-shaped settlement payout (id prefix setl_): Razorpay
// nets its fees out of the gross receivable and deposits the remainder to the
// bank. Amount is the NET deposited to the bank in paise; Fee and Tax are the
// aggregate Razorpay fee and GST-on-fee netted out across the batch. The batch's
// member payment/refund ids are carried so the recon batch-sum check (SPEC §7
// check #2) can verify Σpayments − Σrefunds − Σfees == net_deposit.
type Settlement struct {
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

// Dispute is a Razorpay-shaped dispute/chargeback (id prefix disp_) against a
// payment. Amount is the disputed gross in paise; PaymentID links the original
// payment. Status "lost" means the merchant lost the dispute (the truth GL books
// a chargeback_loss); other statuses are open/won and book no loss in v1.
type Dispute struct {
	Entity    string      `json:"entity"`     // always "dispute"
	ID        string      `json:"id"`         // disp_XXXXXXXXXXXXXX
	Amount    money.Money `json:"amount"`     // disputed gross, paise
	Currency  string      `json:"currency"`   // "INR"
	PaymentID string      `json:"payment_id"` // pay_... under dispute
	Status    string      `json:"status"`     // "lost" | "won" | "open"
	Reason    string      `json:"reason"`     // chargeback reason code
	CreatedAt int64       `json:"created_at"` // Unix seconds
	Notes     Notes       `json:"notes"`      // sku + gst_rate copied from the payment
}

// Notes mirrors Razorpay's free-form notes map but is typed to the keys the
// seeder stamps (SPEC §4.3 "notes incl. sku + gst_rate"): the product SKU and
// the GST rate percentage as a string ("18", "12", "5"). Keeping it a struct
// (not a map) fixes JSON key order and keeps the schema explicit.
type Notes struct {
	SKU     string `json:"sku"`
	GSTRate string `json:"gst_rate"` // percent as a string, e.g. "18"
	// Reason is an optional merchant-ops annotation on a refund (e.g. "goodwill"
	// on a manual partial credit). It is stamped ONLY by the partial-refunds world
	// (its R2 refund); omitempty keeps every existing fixture byte-identical.
	Reason string `json:"reason,omitempty"`
}

// OrderItem is one line item of an order in the partial-refunds world: what was
// sold, for how much (paise, GST-inclusive), at which rate. Line items are the
// matching substrate for the refund-intent judgment — a partial refund whose
// amount equals a line item is (very likely) that item returned. Items appear
// ONLY when the world is seeded with Options.PartialRefunds (omitempty keeps the
// committed clean worlds byte-identical).
type OrderItem struct {
	SKU     string      `json:"sku"`
	Amount  money.Money `json:"amount"`   // paise, GST-inclusive
	GSTRate string      `json:"gst_rate"` // percent as a string; same rate across an order's items in v1
}

// BankFeed is the independent second record of cash movement (SPEC §4.4): the
// merchant's bank statement for the period, with credits (settlement deposits in)
// and debits (chargeback claw-backs out). It is produced by the SAME generation
// rules as the Razorpay fixtures but is a SEPARATE artifact, so reconciliation
// has two independent sources to tie out (SPEC §7).
type BankFeed struct {
	Account string          `json:"account"` // masked bank account label
	Period  string          `json:"period"`  // YYYY-MM this statement covers
	Credits []BankFeedEntry `json:"credits"` // money in (settlement deposits)
	Debits  []BankFeedEntry `json:"debits"`  // money out (chargeback claw-backs)
}

// BankFeedEntry is one line on the bank statement: a positive Amount in paise, a
// value Date (YYYY-MM-DD), and a Ref the settlement/dispute can be matched on
// (the settlement UTR for a credit; the dispute id for a debit). Direction is
// expressed by which list (Credits/Debits) the entry sits in, never by the sign
// of Amount — consistent with the ledger's "no negative amounts" rule.
type BankFeedEntry struct {
	Amount money.Money `json:"amount"` // positive paise; direction is the list it's in
	Date   string      `json:"date"`   // YYYY-MM-DD value date
	Ref    string      `json:"ref"`    // UTR (credit) or dispute id (debit) to match on
}

// CourierFeed is the COD rail's agent-input feed (ROADMAP §8.3): the report a
// logistics aggregator gives the merchant, with every COD shipment's lifecycle
// and the courier's netted remittance(s). Its JSON shape mirrors ingest's
// RawCourierFeed exactly (the on-disk contract the two packages share). It is
// emitted only for COD worlds; a Razorpay-only period has no courier-feed.json.
type CourierFeed struct {
	Channel     string          `json:"channel"`
	Period      string          `json:"period"`
	Shipments   []CODShipment   `json:"shipments"`
	Remittances []CODRemittance `json:"remittances"`
}

// CODShipment is one COD parcel's lifecycle. Status is "delivered" (cash
// collected, books a cod_sale) or "rto" (returned, no cash — lifecycle only).
type CODShipment struct {
	Entity     string      `json:"entity"`      // always "shipment"
	ID         string      `json:"id"`          // shp_...
	OrderID    string      `json:"order_id"`    // order_... this parcel fulfils
	CODAmount  money.Money `json:"cod_amount"`  // gross collected at door, paise (0 for rto)
	Status     string      `json:"status"`      // "delivered" | "rto"
	ShippedAt  int64       `json:"shipped_at"`  // Unix seconds dispatched
	ResolvedAt int64       `json:"resolved_at"` // Unix seconds delivered or returned
	Notes      Notes       `json:"notes"`       // sku + gst_rate
}

// CODRemittance is one courier payout: the courier wires the collected COD cash
// net of its collection fee + GST and per-shipment deductions. By construction
// GrossCollected = NetDeposit + CollectionFee + GSTOnFee + Σ Deductions.
type CODRemittance struct {
	Entity         string         `json:"entity"`          // always "remittance"
	ID             string         `json:"id"`              // rem_...
	GrossCollected money.Money    `json:"gross_collected"` // total COD cash collected, paise
	CollectionFee  money.Money    `json:"collection_fee"`  // courier's per-shipment COD fee, paise
	GSTOnFee       money.Money    `json:"gst_on_fee"`      // GST on the collection fee, paise
	NetDeposit     money.Money    `json:"net_deposit"`     // cash wired to bank, paise
	UTR            string         `json:"utr"`             // bank UTR; matches a bank-feed credit ref
	CreatedAt      int64          `json:"created_at"`      // Unix seconds (remittance date)
	Deductions     []CODDeduction `json:"deductions"`      // per-shipment charges netted out
}

// CODDeduction is one charge the courier netted out beyond its collection fee:
// an RTO fee ("RTO_CHG") or a weight-dispute adjustment ("WT_ADJ").
type CODDeduction struct {
	ID         string      `json:"id"`          // ded_... unique id (the entry's event id when booked)
	Code       string      `json:"code"`        // "RTO_CHG" | "WT_ADJ"
	ShipmentID string      `json:"shipment_id"` // shp_... this charge concerns
	Amount     money.Money `json:"amount"`      // gross deducted, paise
}

// Fixtures bundles the Razorpay-shaped slices the seeder emits to razorpay/. It
// is a convenience for passing the agent-input substrate around as one value;
// each slice is written to its own file (SPEC §4.4). Orders are an agent-input
// recovery source (SPEC §2) but are NOT accounting events: ingest/normalize do
// not read them into the event journal, so adding orders.json never changes the
// normalized journal or the books.
type Fixtures struct {
	Payments    []Payment
	Refunds     []Refund
	Settlements []Settlement
	Disputes    []Dispute
	Orders      []Order
	// Courier is the optional COD-rail feed (cod.go); nil for Razorpay-only
	// periods, in which case no courier-feed.json is written.
	Courier *CourierFeed
}
