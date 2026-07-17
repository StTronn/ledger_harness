# ledger-flow: Mental Model

## What It Does

`ledger-flow` turns a period of Razorpay activity into balanced journal entries
in a double-entry ledger.

Routine accounting stays deterministic. The judgment agent is used only when the
system cannot safely decide what an event means.

## Main Flow

```text
event
  ↓
POSTING ENGINE
  ├─ known rule
  │    ↓
  │  LEDGER
  │
  └─ no rule
       ↓
  RECOVERY ENGINE
       ├─ safe deterministic candidate
       │    ↓
       │  POSTING ENGINE
       │    ↓
       │  LEDGER
       │
       └─ no safe candidate
            ↓
       JUDGMENT AGENT
            ↓
       review or escalation
```

The important switch is the recovery result:

```text
SafeToPost       → posting engine → ledger
ReviewRequired   → judgment agent
Unresolved       → judgment agent
```

The judgment agent is not called for a safe recovery result.

## Ownership

- The posting engine handles known accounting rules.
- The recovery engine decides whether the available facts are sufficient.
- The posting engine applies a safe recovery candidate.
- The ledger validates and records balanced entries.
- The judgment agent explains, recommends, or escalates unresolved cases.
- The judgment agent never writes directly to the ledger.

The recovery engine does not post entries itself. It returns a structured
candidate. The posting engine turns that candidate into a journal entry, and the
ledger accepts or rejects it.

## High-Level Architecture

```text
raw Razorpay and bank data
        ↓
internal/ingest
        ↓
internal/ledgerflow/run
        ├─ internal/ledgerflow/posting
        ├─ internal/ledgerflow/recovery
        │    └─ internal/ledgerflow/context
        ├─ internal/ledgerflow/judgment
        ├─ internal/ledger
        └─ internal/reconcile
                ↓
        internal/score
```

The main folders have focused responsibilities:

- `internal/ingest`: reads and normalizes external events
- `internal/ledgerflow/run`: coordinates one complete ledger flow
- `internal/ledgerflow/posting`: applies known rules and journal templates
- `internal/ledgerflow/recovery`: finds and validates missing facts
- `internal/ledgerflow/context`: provides read-only event and break context
- `internal/world/feeds`: reads orders, rate cards, and other source snapshots
- `internal/ledger`: validates and records double-entry journal entries
- `internal/reconcile`: checks whether the ledger agrees with settlements and bank data
- `internal/score`: compares the result with the expected ledger
- `internal/agentclient`: connects the Go flow to the judgment agent

The TypeScript agent lives under `agent/src`. It receives prepared context over
the agent client boundary. It can use read-only exploration tools for cases the
prepared context does not fully explain.

## Normal Event

```text
payment captured
  → posting engine matches the payment rule
  → journal template is selected
  → ledger validates debit and credit lines
  → entry is recorded
```

The same path handles other known events such as ordinary refunds, settlements,
fees, and COD sales.

## Missing GST Example

The payment is missing its GST rate.

```text
missing GST rate
  → posting engine reports a rule miss
  → recovery follows payment → order
  → recovery finds and validates the order's GST rate
  → recovery returns a safe candidate
  → posting engine uses the candidate with the sale template
  → ledger validates and posts
```

The judgment agent is not called because the facts are sufficient and the
recovery policy allows automatic posting.

The GST split itself is deterministic:

```text
gross amount + GST rate
  → net amount and GST amount
  → net + GST equals gross exactly
```

## Partial Refund Example

A partial refund may match one or more order items, but the match does not prove
the business intent. It could be a return, goodwill credit, or price adjustment.

```text
partial refund
  → posting engine reports a rule miss
  → recovery checks payment → order → line items
  → recovery finds evidence but policy requires review
  → judgment agent reviews and recommends
  → entry remains unposted
```

Even an exact item match can remain review-required when policy says partial
refunds need human judgment.

## RTO Example

RTO deductions arrive through the COD remittance.

```text
COD remittance
  → reconciliation finds an unexplained deduction
  → recovery checks the deduction, shipment status, and rate card
```

If the deduction is a contracted RTO charge, its amount matches the rate card,
and the shipment is confirmed as RTO:

```text
safe deterministic candidate
  → posting engine applies the RTO fee template
  → ledger posts the entry
  → reconciliation runs again
```

If the deduction is an unsupported weight adjustment or the shipment status is
unclear:

```text
no safe candidate
  → judgment agent reviews the evidence
  → recommendation or escalation
  → no automatic posting
```

## Reconciliation Breaks

Reconciliation breaks use the same ownership model as event misses:

```text
reconciliation break
  → recovery gathers and validates the related facts
  ├─ safe candidate → posting engine → ledger → reconcile again
  └─ no safe candidate → judgment agent → review or escalation
```

The agent can help explain a break, but an agent recommendation does not change
the ledger automatically.

## Simple Principle

```text
Posting engine handles known accounting.
Recovery engine finds and validates evidence.
Posting engine applies safe recovery candidates.
Judgment agent handles uncertainty.
Ledger validates every entry.
Reconciliation checks whether the books agree with reality.
```

The goal is simple: automate clear accounting, preserve evidence, and keep
uncertain decisions visible for review.
