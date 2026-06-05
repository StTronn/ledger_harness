# close-agent — v2 / v3 Roadmap & Scaling Plan

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
- **COD / RTO** (return-to-origin) — huge for Indian DTC; a returns/RTO lifecycle and its accounting (reverse logistics fees, cash-on-delivery remittance lag).
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
v1 closes the **payments slice** only. A full monthly close also has bank charges, vendor bills, payroll, accruals, **deferred revenue (subscriptions)**, gift cards / store credit / loyalty. Be explicit that close-agent is "Razorpay→books" until we deliberately expand toward "the whole GL."

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
