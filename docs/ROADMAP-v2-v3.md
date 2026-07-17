# ledger-flow — v2 / v3 Roadmap & Scaling Plan

> **Status:** forward-looking. v1 (the Razorpay→books deterministic engine + judgment agent, SPEC Phases 0–8) is the current build.
> **Purpose:** capture where this goes after v1 so we don't lose the thinking. Pairs with `SPEC.md` (v1) and §13 of it ("How it grows from v1").
> **Golden rule, unchanged across all versions:** the **ledger is the only source of money truth**; the **agent never writes raw postings** (it selects `{entry_type, params}`, the ledger validates balance-or-reject); growth is **additive** (new data + new entry types), not rewrites.

---

## 0. Where v1 lands (the baseline this builds on)

- One world (DTC), one channel (Razorpay), one currency (INR), one period at a time.
- Deterministic Go spine: ingest → normalize → classify(rules) → post(balance-or-reject) → reconcile(3 checks) → reports → score.
- Judgment agent (Flue/TS) confined to two single-shot endpoints: `classify` (long-tail/ambiguous) and `investigate` (reconcile breaks).
- Two **frozen seams** already in place — the entire contract the future learning layer hangs off:
  - `errors.json` (schema_version 1) — per-error records `{event_id, error_class, got, want}` + totals + per-account deltas.
  - **trace schema** (versioned) — per agent call: input, tools used, decision, rationale.
- Fixed ~10–15 node chart; 4 entry types; integer paise everywhere; `truth/` isolated to the scorer.

**Load-bearing invariants to preserve through v2/v3** (do not regress these):
1. Deterministic spine never crosses the LLM boundary.
2. Ledger validates everything (balance-or-reject); agent proposes, ledger disposes.
3. Agent emits only `{entry_type, params}`, never raw debits/credits.
4. All Razorpay/external data snapshotted to fixtures; reproducible over live calls.
5. Money is integer minor units; no floats touch money.
6. `errors.json` + trace schemas are frozen — version-bump in lockstep with consumers.
7. Reports are pure functions of posted entries; posting idempotent on `ik`.

---

## 1. v1.5 — Multi-channel (Amazon / Shopify / COD / wallets)

**The cleanest, most additive next step.** Each new channel is, by design:
- a new **feed** in the seeder/ingest (its settlement report shape),
- a new **linked account** (its settlement receivable / payout account),
- 2–3 new **entry types** (`amazon_sale`, `amazon_settlement`, …),
- a new **fee-audit reconcile check**: does the platform's fee match the contracted rate/tier? (MDR/commission tiers, referral fees, FBA fees.)

The workflow, ledger core, and agent contract are **unchanged**. The agent's classify/investigate just gain new entry types in the playbook skill.

**Decide now / already designed in v1:** keep the income tree **channel-segmentable** (`income/product-sales/<channel>`) so per-channel P&L is free later. (v1 keeps paths segmentable.)

**India-specific channel realities to model:**
- **COD / RTO** (return-to-origin) — huge for Indian DTC; a returns/RTO lifecycle and its accounting (reverse logistics fees, cash-on-delivery remittance lag). **BUILT — `worlds/dtc/2026-02`; deep-dive: §8.3.**
- Marketplace **split settlements** (Razorpay Route), held balances, instant settlements.

---

## 2. v2 — Inventory / COGS, tax depth, and scale

### 2a. Inventory / COGS (the one growth needing a NEW data model)
DTC lives on **contribution margin**, so this is the highest-value accounting expansion.
- New data: **product catalog** (unit costs) + **inventory movements**.
- New accounts: `assets/inventory`, `expense/cogs`.
- New entry types: `inventory_purchase`, `record_cogs`.
- New sub-ledger: **inventory** with a **cost-flow method** (weighted-avg / FIFO).
- Agent's new judgment: cost-flow edge cases, **negative-inventory** investigation, landed-cost allocation.

### 2b. Tax depth (v1's GST is a toy — flat inclusive split)
Real Indian GST is materially more complex; needs a proper **tax engine**:
- **CGST / SGST vs IGST** split by **place-of-supply** (intra- vs inter-state).
- **HSN codes**, exempt / zero-rated / nil-rated SKUs, reverse charge.
- **GST rate changes over time** (rate effective-dating).
- **GSTR-1 / GSTR-3B reconciliation**, e-invoicing (IRN), credit/debit notes.
- **TCS u/s 194-O** (e-commerce), TDS, per-line vs aggregate **rounding rules**.
- **Multi-currency** (v1 is INR-only): txn vs settlement currency, FX gain/loss, once selling internationally.

### 2c. Scale & persistence (v1 is in-memory, full-rebuild per close)
At real DTC volume (tens of thousands → millions of txns/month):
- **Persistent append-only journal** (DB) instead of in-memory rebuild.
- **Incremental posting** + **materialized account balances** (don't fold all entries per report).
- **Streaming/paginated ingest** with rate-limit handling, retries, resumable backfills, large-month snapshots.
- Bounded memory; indexed report queries.

### 2d. Period-close discipline & audit (needed the moment this touches real books)
- **Immutable closed periods** — once filed, locked; corrections become audited **reversing entries**, never edits. Close → lock → reopen workflow.
- **Full audit trail**: every posting traceable to source event + rule/agent + rationale (the trace seam).
- **Playbook / chart versioning**: entries record the schema version they were posted under; rollback support.

---

## 3. v3 — The learning layer / meta-agent (the big deferred piece)

This is the payoff the whole v1 architecture was shaped for — and why the seams were frozen early.

- **Consumes `errors.json`** (clustered) and **authors new entry types** — i.e. mutates the **playbook schema file, not code** (keep the playbook fully data-driven / machine-editable).
- Every authored entry type is **auto-validated by the ledger's balance check** before being kept.
- **Held-out periods** guard against overfitting; promote a rule only if it generalizes.
- Net effect at scale: the agent's per-event LLM share **shrinks over time** as repeated judgments are promoted into deterministic rules — this is what makes the economics work on millions of events.
- **DSPy / GEPA (prompt optimization)** is a *different axis* (keeps the LLM per-item, tunes its prompt). If wanted, run as a **Python sidecar over traces** — do **not** entangle with the Go spine.
- **Flue autonomy / subagents / skills-evolution** (unused in v1) is the natural home: subagents = sub-workflows + meta-agent; evolving `SKILL.md` = the growing playbook; trace export = the learning fuel. Because the agent already lives behind the thin §8 interface, this is expansion, not a port.

**Hard prerequisite (do not break):** the `errors.json` and trace schemas must stay frozen/versioned. Changing them silently breaks the learner.

---

## 4. Cross-cutting concerns at scale (apply across v2/v3)

### 4a. The agent: cost, determinism, safety (the crux)
- **Cost/latency:** per-event LLM calls don't scale linearly to millions; the learning loop (promote → rules) is the answer, plus batching and response caching keyed by event.
- **Determinism in production:** books numbers must not change because the model was non-deterministic → recorded-response/replay + version-pinned prompts. (v1 already builds record/replay.)
- **No ground truth in prod:** there is no `truth/gl.json` for a live month. Need **proxy signals**: did it reconcile? did a human approve? variance vs prior months? + **confidence thresholds → human-in-the-loop** + sampling QA.
- **Wrong-but-balanced risk:** balance-or-reject catches *unbalanced* mistakes, **not** a balanced-but-misclassified entry — hence confidence scoring + review queues.

### 4b. Multi-tenant SaaS (one DTC → many brands → many worlds)
- Tenant **data isolation**, per-tenant chart + playbook, per-tenant vs shared rule learning.
- RBAC, PII handling (payment + customer data), encryption, retention, SOC2.
- Noisy-neighbor / per-tenant rate limits and cost attribution.
- **More worlds** (SaaS, Marketplace) — each is a new seeder + schema; the same engine closes them. The "generalize across worlds" demo.

### 4c. Human-in-the-loop & operations
- Accountant UI: review breaks, **approve/override** agent resolutions and classifications — and those overrides **feed the learning layer**.
- **Observability**: classify hit-rate, break rate, agent-invocation count, accuracy/score trend, **drift detection** month-over-month.
- Idempotent/resumable pipelines, partial-failure handling, schema migrations.
- Near-real-time continuous accrual vs monthly batch close (product decision).

### 4d. Scope honesty
v1 closes the **payments slice** only. A full monthly close also has bank charges, vendor bills, payroll, accruals, **deferred revenue (subscriptions — deep-dive: §8.2)**, gift cards / store credit / loyalty. Be explicit that ledger-flow is "Razorpay→books" until we deliberately expand toward "the whole GL."

---

## 5. Rough sequencing & dependencies

```
v1   (now)         deterministic spine + classify/investigate agent (Phases 0–8)
  │
v1.5  multi-channel ── additive: new feed + linked account + entry types + fee-audit check
  │                    (income tree already channel-segmentable)
  │
v2a  persistence/scale ── DB journal, incremental posting, materialized balances, streaming ingest
v2b  tax engine        ── CGST/SGST/IGST, HSN, place-of-supply, GSTR recon, TCS, multi-currency
v2c  inventory/COGS    ── NEW data model: catalog + inventory sub-ledger + cost-flow
v2d  close discipline  ── immutable periods, reversing entries, playbook versioning, audit trail
  │
v3a  learning layer    ── cluster errors.json → author entry types (schema, not code) → ledger-validate → held-out guard
v3b  multi-tenant SaaS ── isolation, RBAC, per-tenant playbooks, more worlds
v3c  human-in-the-loop ── review/override UI feeding the learner; observability + drift detection
```

**Critical path note:** v3a (learning layer) depends only on the frozen `errors.json` + trace seams (already in v1) — it can start independently of v2. v2c (inventory) is the only item needing a genuinely new data model; everything else is additive feeds + entry types + a tax engine + a persistence swap behind the existing ledger contract.

---

## 6. What v1 already gets right for all of this

Don't lose these — they're why the above is additive rather than a rewrite:
- Deterministic 95% never crosses the LLM boundary (fast, auditable, single money-truth).
- LLM confined to a tiny, well-typed surface (`{entry_type, params}`); ledger validates.
- `truth/`, `errors.json`, trace seams isolated and frozen.
- Data-driven playbook (machine-editable by the future learner).
- Channel-segmentable income tree; integer-paise money; idempotent, pure reports.

---

## 7. The §8 agent seam: execution models & hardening (design notes)

> **Update:** the read-surface side of this section is now BUILT on main as the
> harness subsystem (`internal/harness/{ledgergraph, policychecks, feeds}`) —
> the policy/recovery registry, the rate-card feed + fee-tier check, and the
> tier-2 `context entity` exploration tool. See `docs/HARNESS.md`. The novelty
> ladder: tier-1 policy hit → tier-2 agent exploration → escalate; repeated
> tier-2 walks are what v3a promotes into new policies.

Captured from the post-Phase-8 design discussion. All of the below was **prototyped
end-to-end (classify AND investigate) and parked on the `v2-preview` branch**
(`git checkout v2-preview`); v1 (main) stays the synchronous core. This is v2/v3
growth, not needed for the v1 deterministic close.

### 7.1 Asynchronous execution (BUILT for classify)

The agent need not run inline. The close is split at the agent boundary into stages
joined by KEYED stores (keyed by `event_id` / `break_key`):

```
close --agent off  → books the bulk, PARKS its skips → proposals.json   (front door)
classify work      → async worker (the agent) processes the queue → results.json
classify apply     → validate → review → derive → post → reconcile → score
```

- **The keyed store is the determinism anchor:** APPLY only does a keyed lookup, so
  HOW/WHEN the worker ran (sync, async, concurrent, batched, remote) never changes
  the booked result. Same results in → byte-identical books out.
- `close --agent off` is the front door, not a throwaway baseline: the events it
  cannot book ARE the work queue.
- Built in `internal/classifyq` (stores, stub worker, validator, reviewer) +
  `closer.RunApply`. Investigate is still sync-only — same pattern applies later.

### 7.2 Numeric-surface hardening (BUILT for async classify)

The agent must emit **recovered FACTS, not money**. The worker returns the
`gst_rate` (a value from a closed slab set {5,12,18}); the APPLY stage DERIVES
`net`/`gst` via the canonical `gstsplit`. The agent has no channel to inject an
arbitrary rupee value. (The sync path still takes full params — fold this in there
too when the live brain lands.)

### 7.3 Provenance citations + re-verifying Validator (BUILT for async classify)

Every recovered fact carries a machine-checkable citation `{tool, object, path}`
(e.g. `orders.fetch / order_X / notes.gst_rate`). At APPLY the Validator RE-READS
that exact field from the snapshot and confirms the value (and that it's a real
slab). A fabricated value needs a fabricated citation, which the re-read catches →
rejected, skipped, never posted. Strongest form (future): the agent may only emit
values that are *references* into tool outputs, never literals.

### 7.4 Human review gate (seam BUILT, UI later)

A `Reviewer` seam sits AFTER the Validator and BEFORE posting: `auto` (approve all,
default), `recorded` (replay committed verdicts, fail-closed, CI/audit), and a
future `interactive` (CLI/web). Verdicts (approve/edit/reject + who/when) are gold
training data for the v3a learner and form the audit trail (v2d).

### 7.5 Debt to pay before extending

`closer.RunWith` (sync) and `closer.RunApply` (async) duplicate ~70% of the spine;
they differ only in HOW a rule miss is resolved (inline agent vs results lookup).
Factor a single `runCore` + a pluggable `MissResolver` so sync/async/investigate
share one spine and one set of invariants.

---

## 8. Stateful-channel deep-dives (design notes, 2026-06)

Captured from the post-v1 design discussion on what the *real* 5% looks like and
how the two big stateful channels (subscriptions, COD/RTO) land on this
architecture. **Headline convergence:** each expansion adds exactly ONE stateful
primitive — subscriptions add the *recognition schedule*, COD/RTO adds the
*shipment lifecycle*, Route adds the *held balance* — while the contract layer
(balance-or-reject, `{entry_type, params}`, IK, citations, breaks→investigate,
escalate-never-guess) absorbs all three unchanged. All three independently
require the same base investments: **persistence (2a), period locking (2d), and
the recovery registry** — those three are the real v1.5/v2 critical path, ahead
of any individual channel.

### 8.1 The real classify-misses (fixture-world ladder)

The v1 `gst_rate`-stripped miss was a pedagogical stand-in (real merchants keep
rates in a product master, not payment notes). The real ambiguity classes, as
fixture worlds, cheapest-credible first:

| Case | Why rules can't book it | Status |
|---|---|---|
| **Partial refund** — ₹350 of ₹1,180; return vs goodwill vs shipping refund | the refund object never says WHY | **BUILT** — `worlds/dtc/2026-01`, `seed.Options.PartialRefunds` (R1 item-match books `refund_reversal`; R2 goodwill + R3 unexplained escalate; honest 95% replay score) |
| **Duplicate capture** — checkout retry charged twice | balanced-as-revenue is *wrong-but-balanced*; agent must REFUSE revenue → `customer_advance` liability | queued: cheapest next world (1 account + 1 entry type + sibling `orderPayments` index) |
| **Orphan payment** — payment link / QR, no `order_id` | nothing to join; needs amount+timing+customer matching or suspense | the natural tier-2 (search-tool) demo |
| **Bank-offer top-up** — paid ₹900, received ₹1,000 | revenue is 1,000 + bank receivable: policy + new concept | later |
| **Mixed-rate bundle** — one line, two slabs | composite-vs-mixed supply is genuinely contested → can't write an honest truth GL | not a fixture; escalation-only |

### 8.2 Subscriptions / deferred revenue (the *schedule* primitive)

Breaks v1's quiet assumption: one event → one entry, at event time. Annual
prepay ₹11,800 books GST on the advance immediately + ₹10,000 to
`liabilities/deferred-revenue`, then a **clock-triggered** entry each month
(Dr deferred / Cr subscription-revenue, IK `revrec:<sub_id>:<YYYY-MM>`).

```
v1:    event ──classify──► entry
subs:  event ──classify──► entry + SCHEDULE ──recognize(period)──► entry/month
```

- The **schedule** is a first-class deterministic object (start, end, amount,
  method) living in the ENGINE, not the agent; a new spine stage
  `recognize(period)` walks open schedules at each close.
- Load-bearing check: **deferred-revenue roll-forward** — `opening + new billings
  − recognized − refunded == closing` (per sub & total); its investigate bundle is
  the per-subscription expected-vs-actual schedule, structurally the v1
  unbooked-refund batch view.
- Agent judgment surface: mid-cycle upgrade proration (modification vs new
  obligation), cancellation refund splits (clawback of recognized revenue),
  dunning/failed-renewal matching, GST rate change mid-schedule, usage overage
  (unbilled-revenue asset). The §8-interface extension is one level up:
  `{schedule_action, params}` (create/truncate/split), ledger-validated.
- Base delta: schedule object + `recognize` stage (the one true architectural
  addition), deferred-revenue **sub-ledger** (second customer after inventory →
  build sub-ledger as a generic capability), multi-period state (pulls 2a/2d
  forward), canonical rounding/proration functions (the `gstsplit` discipline
  applied to time-spreading; last-month-absorbs-remainder must be ONE function).
- Razorpay Subscriptions object-graph misses (wrong plan, duplicate subscription
  from checkout retry) are ordinary classify-misses on the new feed — downstream
  of the schedule layer, ladder unchanged.

### 8.3 COD / RTO (the *lifecycle* primitive)

**Positioning (from Razorpay's own RTO material):** Razorpay's RTO suite (Magic
Checkout, COD Intelligence, RTO Protection) lives BEFORE the order — predict,
prevent, insure. Nothing touches what happens AFTER the parcel bounces: booking
the costs, reconciling the courier's netted remittance, closing the month.
**Magic Checkout reduces RTO; ledger-flow accounts for the RTO that happens
anyway.** The courier feed is not hypothetical: merchants already pipe
Shiprocket / Delhivery / ClickPost / Unicommerce / iThink data into Razorpay for
the RTO Analytics "Delivery Data" widget — seed THAT shape (one aggregator feed,
not multi-courier multi-feed). Scale, per Razorpay's published numbers: COD ≈
66% of Indian e-retail orders, ~33% of COD undelivered, RTO adds 15–20%
logistics cost per order. RTO Analytics shows the RTO *rate*; nobody shows the
RTO *cost line in the P&L* — that report is ours.

COD cash is collected by the courier and remitted in weekly netted batches — a
**second money rail** with its own feed, receivable (`assets/cod-receivable` →
third sub-ledger voter), and reconciliation. First true test of §1's "new
channel = new feed + linked account + entry types + checks" claim. The demo
exhibits exactly three things: (1) lifecycle booking under a book-at-delivery
policy (RTO'd orders never create fake revenue), (2) every remittance proved to
the rupee — break → agent decomposes the batch itself, (3) the RTO burn report.

**BUILT — `worlds/dtc/2026-02` (`seed --cod`).** The whole vertical shipped and
is gated like every other period:
- **Vocabulary:** accounts `assets/cod-receivable`, `expense/cod-collection-fees`,
  `expense/reverse-logistics`; entry types `cod_sale` (mirrors `dtc_sale`),
  `cod_remittance` (mirrors `razorpay_settlement`), `rto_fee`, `weight_adjustment`.
- **Feed:** `courier-feed.json` (Shiprocket-shaped: shipment lifecycle +
  netted remittances with per-shipment deduction lines), read OPTIONALLY by
  ingest (absent ⇒ Razorpay-only periods byte-unchanged). New event types
  `cod_delivery` / `cod_remittance`; `EventType` confirmed data-extensible.
- **The residual falls out, no inject needed:** rules book deliveries +
  the remittance's collection-fee portion; the RTO fee + weight-dispute
  deductions have no per-event rule, so `cod-receivable` is left short by exactly
  their sum (₹158). A new ledger-aware check (`cod-receivable-residual`, the
  COD twin of check #3) raises it.
- **Recovery is a registry row, not new plumbing:** `rto-fee-from-ratecard`
  policy (the COD twin of `fee-tier-from-ratecard`) validates each deduction
  against the rate card + the shipment lifecycle — a backed RTO charge → book
  `rto_fee` (cited to `ratecard/<courier>/rto_fee_paise`); anything else →
  escalate. The recon bundle surfaces the deductions pre-classified.
- **Investigation = decompose, resolve part, escalate the rest:** the investigate
  seam now composes postings AND an escalation in one pass. Replay books the
  ₹118 RTO fee and escalates the ₹40 weight dispute ("request the courier's
  reweigh report"); the residual shrinks 158→40, the ₹40 stays listed.
- **Gate (committed):** `2026-02 --agent off` = 95% / one ₹158 residual break;
  `--agent replay` = 97% / ₹118 booked + ₹40 escalated (the designed honest
  sub-100%). All four pre-existing periods byte/score-identical; full suite +
  gofmt + vet green.

- **Policy decision that dominates everything** (playbook-encoded, never the
  agent's call): book revenue at shipment vs **delivery**. With 20–30% Indian RTO
  rates, book-at-delivery keeps most RTOs from ever creating revenue.
- **Remittance netting:** one batch = Σ collected − collection fees − RTO fees
  (for FAILED orders, deducted from successful orders' cash) − weight-dispute
  adjustments. The settlement template handles it; reconcile must explain every
  deduction.
- Checks: remittance batch-sum (check#2 twin), bank UTR match (check#1 twin),
  RTO completeness, and the genuinely new **lifecycle check** — every shipment
  older than (delivery SLA + remittance cycle) must be in a terminal state
  (remitted / RTO-complete / claimed). Inherently cross-period → second voter
  for pulling 2a/2d forward.
- **This is where the system first LOOKS like an agent** (investigation, not
  one-bundle judgment): a short-remittance break has no pre-computable bundle —
  the agent decomposes the batch (members → lifecycle per member → rate card →
  hypothesis → verify the residual closes), proposes MULTIPLE entries, *declines*
  a revenue reversal by citing book-at-delivery policy, and partially solves the
  break (books the rate-card-confirmed RTO fee, escalates the ₹40 weight
  adjustment with the exact document to ask for — "explain 75%, hand a human the
  precise remainder").
- Base delta: one aggregator-shaped courier feed (forces the `ingest.EventType`
  enum→data change); lifecycle state index in the read model; recovery-registry
  entries (shipment→SLA/rate-card; remittance line→shipment→order). The
  escalation case is the weight-dispute deduction (no rate-card basis, no
  document in any feed → human).
- **Prepaid RTO** (the cross-rail case): payment captured on Razorpay, parcel
  bounces on the courier rail ⇒ refund out (existing `refund_reversal` path,
  MDR not returned — pure loss), RTO fee in, and the two must be LINKED. A
  courier feed turns a class of "unexplained" refunds (the partial-refund R3
  shape) into explainable ones — cross-feed investigation, the realistic answer
  to "where does missing refund context come from in production." New check:
  every prepaid `rto_delivered` has a matching refund within policy SLA, and
  vice versa.
- Deferred (real, but not the demo): delivered-but-never-remitted aging
  judgment, status flapping across locked periods (§2d), damaged-return
  restock-vs-writeoff (§2c), RTO Protection reimbursements (a Razorpay
  receivable — one entry type when it matters).
