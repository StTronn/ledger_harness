// Package agentclient is the Go side of the §8 judgment-agent interface — the
// thin, swappable seam between the deterministic close spine and the Flue/LLM
// classify-fallback agent (SPEC §3, §8, §11 Phase 7). It exposes ONE call the
// orchestrator reaches for on a rule miss:
//
//	Classify(EventSummary) -> {entry_type, params, rationale} | {unclassifiable, reason}
//
// exactly the §8 `POST /agents/classify` contract. The agent returns an
// {entry_type, params} recommendation for review; it NEVER emits raw
// debits/credits and never posts. A ClassifyResult carries an entry-type name and
// a paise Params map keyed to the playbook, while the deterministic or approved
// posting path remains responsible for ledger writes.
//
// # Modes (SPEC §11 Phase 7 "recorded-response mode"; §12)
//
// The client is swappable behind the Client interface, with two implementations:
//
//   - REPLAY (the DEFAULT for CI): reads a committed, reviewed recorded-response
//     fixture keyed by event_id from worlds/<world>/<period>/agent/classify.recorded.json.
//     It is PURE and DETERMINISTIC — no network, no LLM, no wall clock, no
//     randomness — so the same fixtures + recorded responses yield byte-identical
//     output (SPEC §5, §12). A missing recorded entry is returned as an explicit
//     {unclassifiable, reason}, never a guess.
//
//   - LIVE / RECORD (built, NOT exercised in CI): posts the event to a
//     configurable Flue HTTP endpoint (POST /agents/classify) and RECORDS the
//     response back into the same fixture, so a later CI run can replay it. Live
//     LLM calls happen only in a separate, non-CI eval (SPEC §12).
//
// # The legitimate recovery source: orders.json, NOT truth (SPEC §1, §2, §4.4)
//
// The committed classify.recorded.json for the hard period is GENERATED, not
// hand-typed: for each gst_rate-stripped payment, the recovery generator
// (recover.go) "fetches the order" from worlds/<world>/<period>/razorpay/orders.json
// — the authoritative, snapshotted agent-input recovery source — recovers the true
// rate, and produces the same {entry_type: dtc_sale, params} the rule engine would
// have produced had the rate been present. orders.json is an agent INPUT, not
// ground truth: this package MUST NOT import or read internal/truth (the
// truth-isolation guard enforces it at the package boundary, SPEC §4.4, §12).
//
// # Frozen trace schema (SPEC §9, §13)
//
// Every Classify call yields a versioned, FROZEN Trace (trace.go): the learning
// seam. Freezing it now (schema_version stamped, fields locked) keeps the future
// learning layer's contract stable.
//
// # Money invariant (SPEC §1, §4)
//
// Params are integer minor units — paise — as int64 on the wire and money.Money
// in Go. No float ever touches money here; a guard test asserts that statically.
package agentclient
