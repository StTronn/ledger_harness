package posting

import "github.com/razorpay/ledger-flow/internal/ingest"

// ikfor.go is the CANONICAL idempotency-key scheme for ledger-flow — the single
// source of the "sale:/settle:/refund:/dispute:" prefixes that mirror the seeder's
// truth IK scheme (internal/seed). Every code path that posts an entry (the rule
// engine here, the agent classify seam, the investigate seam) and every path that
// must recognise an already-posted entry (the read model's `booked` predicate)
// derives its IK from IKFor, so the key can never drift between producer and
// reader. Before this existed the scheme was duplicated across three packages; a
// fourth consumer (the read model) made one source mandatory.

// ikPrefix maps an event type to its IK prefix. It is the ONE place the prefix
// strings live. An unknown type returns "" — callers that need to reject unknown
// types guard on that (or on EventTypeForEntryType's ok) rather than minting a
// malformed ":id" key.
func ikPrefix(t ingest.EventType) string {
	switch t {
	case ingest.EventPayment:
		return "sale"
	case ingest.EventSettlement:
		return "settle"
	case ingest.EventRefund:
		return "refund"
	case ingest.EventDispute:
		return "dispute"
	case ingest.EventCODDelivery:
		return "codsale"
	case ingest.EventCODRemittance:
		return "codremit"
	default:
		return ""
	}
}

// IKFor returns the canonical idempotency key for an event of type t with id id —
// e.g. IKFor(EventPayment, "pay_A1") == "sale:pay_A1". It is the single source of
// the IK scheme the seeder's truth GL, the rule engine, the agent seams, and the
// read model all share, so a posted entry and a `booked` lookup compute the same
// key by construction. An unknown type yields a ":id" sentinel (empty prefix);
// callers over untrusted types should guard via EventTypeForEntryType.
func IKFor(t ingest.EventType, id string) string {
	return ikPrefix(t) + ":" + id
}

// EventTypeForEntryType maps a playbook entry-type name to the event type it
// books, for callers (the investigate seam) that know only the entry type they are
// about to post. ok is false for an entry type outside the v1 playbook, mirroring
// the per-site switches it replaces so an unknown type is a clear error, not a
// silently-wrong key. The mapping is the inverse of the rule engine's
// event-type -> entry-type rules (classifyPayment -> dtc_sale, etc.).
func EventTypeForEntryType(entryType string) (ingest.EventType, bool) {
	switch entryType {
	case "dtc_sale":
		return ingest.EventPayment, true
	case "razorpay_settlement":
		return ingest.EventSettlement, true
	case "refund_reversal":
		return ingest.EventRefund, true
	case "chargeback_loss":
		return ingest.EventDispute, true
	case "cod_sale":
		return ingest.EventCODDelivery, true
	case "cod_remittance":
		return ingest.EventCODRemittance, true
	default:
		return "", false
	}
}

// EntryTypeForEventType maps an event type to the playbook entry type the rule
// engine books it as — the inverse of EventTypeForEntryType, 1:1 in v1. The read
// model's bundlers use it to tell the agent which entry type applies to an event
// (or which resolves an unbooked one) without re-encoding the rule engine's
// mapping. ok is false for an unknown event type.
func EntryTypeForEventType(t ingest.EventType) (string, bool) {
	switch t {
	case ingest.EventPayment:
		return "dtc_sale", true
	case ingest.EventSettlement:
		return "razorpay_settlement", true
	case ingest.EventRefund:
		return "refund_reversal", true
	case ingest.EventDispute:
		return "chargeback_loss", true
	case ingest.EventCODDelivery:
		return "cod_sale", true
	case ingest.EventCODRemittance:
		return "cod_remittance", true
	default:
		return "", false
	}
}
