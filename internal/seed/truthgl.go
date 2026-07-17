package seed

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/config"
	"github.com/razorpay/ledger-flow/internal/ledger"
	"github.com/razorpay/ledger-flow/internal/money"
	"github.com/razorpay/ledger-flow/internal/truth"
)

// This file is the TRUTH-GL EMITTER (SPEC §2 Phase 2, §4.4, §4.2). It builds the
// hidden ground-truth ledger from the SAME generated events by BINDING the
// playbook entry types (dtc_sale / razorpay_settlement / refund_reversal /
// chargeback_loss) with the correct params and POSTING them through the real
// internal/ledger engine. Because every emitted entry is one the ledger's post()
// accepted, the truth GL is GUARANTEED balanced by construction: an unbalanced
// entry would be REJECTED here (a seeder bug), never silently written.
//
// # Why bind the playbook instead of hand-writing lines
//
// The playbook (config/playbook.json, mirrored by config.DefaultPlaybook) is the
// single source of entry-type structure: the rule engine, the ledger, and the
// agent Skill all derive from it. Building the truth GL from the SAME templates
// means the ground truth cannot drift from the books the close workflow is meant
// to produce. The binder owns no line structure of its own — it only computes
// the integer-paise params each template needs and lets the engine expand them.
//
// # The GST division that templates forbid
//
// Template arithmetic is +/- only; any derived amount needing × or ÷ — the GST
// split out of a gross — is computed HERE, at bind time, in integer paise
// (splitGSTInclusive), then passed in as the net/gst params. No float touches it
// (SPEC §1, §4.2).
//
// # Reading back is forbidden
//
// The truth GL is produced from events, never read back from its own file
// (SPEC §2). This emitter takes events in and returns truth.Entry values; it does
// not open truth/gl.json.

// truthBinder turns generated events into balanced truth.Entry values by binding
// playbook templates and posting them through a real ledger. It holds the engine,
// the template source, and a monotonic entry sequence for stable GL ids.
//
// It is not safe for concurrent use; the generator drives it single-threaded so
// the posting order (and therefore the GL entry order and ids) is deterministic.
type truthBinder struct {
	lg      *ledger.Ledger
	tmpls   ledger.Templates
	entries []truth.Entry
	seq     int
}

// newTruthBinder builds a binder over the given playbook: a fresh ledger bound to
// the playbook chart (so posts are validated against the real chart of accounts)
// and the playbook's templates for binding.
func newTruthBinder(pb *config.Playbook) *truthBinder {
	return &truthBinder{
		lg:    ledger.New(ledger.NewPlaybookChart(pb)),
		tmpls: ledger.NewPlaybookTemplates(pb),
	}
}

// nextEntryID returns the next stable, human-readable GL entry id (gl_0001, …).
// Ids are sequential in posting order so the truth GL is easy to read and the
// scorer can reference entries stably (SPEC §9).
func (b *truthBinder) nextEntryID() string {
	b.seq++
	return fmt.Sprintf("gl_%04d", b.seq)
}

// bind binds entryType with params and posts it through the ledger, then records
// the posted entry as a truth.Entry carrying the human-readable id, the source
// EventID/TxID strings, and the event timestamp.
//
// ik is the idempotency key for the post (unique per event so distinct events
// never collapse). eventID is the source Razorpay/bank event id this entry
// derives from; txID is the external transaction id to surface on the truth entry
// (e.g. payment_id / settlement UTR / dispute id) — these are the real id STRINGS,
// kept separate from the ledger's opaque integer tx-id channel.
//
// A bind or post failure is a GENERATION bug (the seeder fed inconsistent params,
// or wrote a template that does not balance), not bad external input, so it is
// returned as an error and aborts the whole seed rather than being papered over.
func (b *truthBinder) bind(entryType, ik, eventID, txID string, ts int64, params map[string]money.Money) error {
	entry, err := ledger.Bind(b.tmpls, entryType, ik, params)
	if err != nil {
		return fmt.Errorf("seed: bind %s for event %s: %w", entryType, eventID, err)
	}
	entry.Ts = ts
	// Post enforces ΣDr == ΣCr (and known accounts / non-negative amounts). A
	// rejection here means the bound params do not balance — by construction they
	// must, so surface it as a seeder error.
	posted, err := b.lg.Post(entry)
	if err != nil {
		return fmt.Errorf("seed: post %s for event %s: %w", entryType, eventID, err)
	}
	b.entries = append(b.entries, b.toTruthEntry(entryType, eventID, txID, posted))
	return nil
}

// toTruthEntry converts a posted ledger.Entry into a truth.Entry, copying its
// validated lines (side/account/amount) verbatim and stamping the human-readable
// GL id plus the real source EventID/TxID strings and the event timestamp.
//
// The ledger and truth Side constants share the same "Dr"/"Cr" string values, so
// the translation is a direct cast — the two types stay decoupled (the truth
// schema does not depend on the posting engine) while remaining wire-compatible.
func (b *truthBinder) toTruthEntry(entryType, eventID, txID string, posted ledger.Entry) truth.Entry {
	lines := make([]truth.Line, len(posted.Lines))
	for i, l := range posted.Lines {
		lines[i] = truth.Line{
			Side:    truth.Side(l.Side),
			Account: l.Account,
			Amount:  l.Amount,
		}
	}
	return truth.Entry{
		ID:        b.nextEntryID(),
		EntryType: entryType,
		EventID:   eventID,
		TxID:      txID,
		Ts:        posted.Ts,
		Lines:     lines,
	}
}

// txIDParam is the placeholder value passed for a template's tx_param. The ledger
// carries tx ids through the money.Money param channel (it only needs the param
// PRESENT, and renders it as an opaque integer); the real id STRING is supplied
// separately to bind() and stamped on the truth entry. Using zero keeps the param
// present without inventing a meaningless integer id.
func txIDParam() money.Money { return money.FromPaise(0) }
