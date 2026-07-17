# ledger-flow — v1 Spec & Execution Handover

> **Status:** design locked, ready to build.
> **Audience:** the execution agent(s) building this. Read this top-to-bottom before writing code.
> **One-liner:** Ingest one month of a DTC brand's Razorpay activity → produce a balanced, reconciled, double-entry ledger + financial reports → score it against hidden ground truth. A deterministic Go engine does the routine 95%; a small Flue/TS agent handles the judgment 5%.

---

## 0. How to read this document

- §1–§3 are the **what and why**. §4–§10 are the **component contracts**. §11 is the **incremental build plan** — this is the part that keeps the project from going haywire; follow the phase order and don't skip the test gates. §12–§14 are **testing, growth, and risks**.
- **Golden rule of this project:** the **ledger is the only source of money truth**, and the **agent never writes raw postings** — it only *selects an entry type and binds parameters*, which the ledger validates (balanced-or-rejected). If you find yourself letting the LLM emit debits/credits directly, stop — that's the design going wrong.
- Build **deterministic-first**. The entire close must run, reconcile, and score with **no LLM at all** before any agent is added. The agent is layered on last, behind a thin interface, against a known baseline score.

---

## 1. Problem & value proposition

A DTC brand running payments through Razorpay has to turn raw payment activity into books each month. For hundreds of events that means: pick the right journal entry, **gross up fees** (Razorpay nets fees out of settlements, so the cash that hits the bank hides the real gross + fee), **split GST** out of revenue, **reconcile every payout to the bank**, and chase anything that doesn't tie out. It's tedious, needs accounting judgment, and one wrong assumption throws the trial balance out of balance.

**ledger-flow v1:** point it at one month of Razorpay activity → get correct, balanced, double-entry books (trial balance, balance sheet, income statement, journal), fully reconciled to the bank, with anything that doesn't reconcile **investigated**, not just flagged.

**Where the agent earns its keep** (everything else is deterministic):
1. **Ambiguous/novel classification** — an event no rule covers (e.g. missing tax metadata) → agent reasons from context (fetch the order) and picks the entry type.
2. **Reconciliation breaks** — a deposit that doesn't match a settlement → agent forms hypotheses (timing lag / refund-in-batch / dispute hold / mis-booked fee), checks via tools, and resolves or escalates.

---

## 2. Scope

### In scope (v1)
- **One world:** DTC eCommerce. **One channel:** Razorpay only.
- **One period at a time** (a month).
- Full **double-entry** ledger, fixed chart of accounts (~15 nodes).
- Entry-type templates (the "playbook"), **hand-written and static**.
- Deterministic the bookkeeper + **agent fallback** for the long tail.
- 3-check reconciliation against a seeder-generated **bank feed**.
- Reports: trial balance, balance sheet, income statement, journal export.
- Scoring vs a hidden ground-truth ledger; per-error records emitted.
- Terminal-first CLI surface.

### Explicitly OUT of scope (v1) — do not build these yet
- ❌ The **learning layer / meta-agent** (no rule authoring, no GEPA/DSPy). The per-error record format is the *only* seam left for it.
- ❌ **Multi-channel** (Amazon/Shopify).
- ❌ **Inventory / COGS**.
- ❌ Other worlds (SaaS, Marketplace).
- ❌ Flue **autonomy / sandbox** features (we use only Workflows/Tools/Skills).
- ❌ HTML reports / dashboards (terminal-first; HTML is a later pass).
- ❌ Multi-currency (INR only in v1).

§13 describes how each of these grows in later versions. **The whole point of the v1 architecture is that these are additive (new data + new entry types), not rewrites.**

---

## 3. Architecture

```
┌─ GO SERVICE (deterministic spine + orchestrator) ────────────────┐
│  reuses razorpay-cli/api directly (no subprocess)                │
│                                                                  │
│  ingest → normalize → classify(bookkeeper) → post → reconcile   │
│           → reports → score        ▲              │              │
│                                    │ rule MISS    │ break        │
│                                    ▼              ▼              │
│                         calls Flue agent endpoints (HTTP)        │
│                                                                  │
│  Ledger core: chart of accounts · entry templates (playbook) ·   │
│    post()=balance-or-error · linked-account reconcile · reports  │
└───────────────┬──────────────────────────────────────────────────┘
                │ HTTP (thin interface)
┌───────────────▼──────────────────────────────────────────────────┐
│  FLUE AGENT LAYER (TS) — the judgment surface only                │
│  agent: classify     in: one event   out: {entry_type, params}    │
│  agent: investigate  in: one break   out: resolution / escalate   │
│  tools:  read-only Razorpay lookups → call Go read API (so all    │
│          data stays snapshotted/deterministic)                    │
│  skills: SKILL.md = playbook + sub-workflow expertise             │
│  observability: traces → the (deferred) learning seam             │
└───────────────────────────────────────────────────────────────────┘
```

| Layer | Tech | Responsibility |
|---|---|---|
| ingest, normalize | **Go** | import `razorpay-cli/api`; pull period data; flatten to event journal |
| the bookkeeper | **Go** | book the ~95% deterministically via entry templates |
| ledger core | **Go** | post (balance-or-error), linked-account reconcile, report queries |
| orchestrator | **Go** | drive the DAG; call Flue only on rule-miss / reconcile-break |
| score | **Go** | diff vs truth GL → %, per-error records |
| classify-fallback agent | **Flue/TS** | one event → `{entry_type, params}` |
| investigate agent | **Flue/TS** | one reconcile break → resolution / escalate |

**Orchestration ownership:** **Go leads.** The Go orchestrator runs the workflow and calls the Flue agents as HTTP endpoints. Flue is used *only* as the agent harness, not as the workflow engine. (This keeps ingest/normalize/rules/orchestration in Go and makes Flue a swappable agent host.)

**Why this split:** the deterministic 95% never crosses a language boundary (fast, auditable, single source of money truth). The LLM is confined to a tiny, well-typed surface. Later learning grows *inside* Flue without touching the Go spine.

---

## 4. Data model

All money is **integer minor units (paise)**, encoded as needed. **Never use floats for money.** `236000` = ₹2,360.00.

### 4.1 Chart of accounts (fixed, v1)
A tree (Fragment-style paths). Four root types: `assets`, `liabilities`, `income`, `expense`. Balanced by the accounting equation `Assets − Liabilities = Income − Expenses`.

```
assets/
  bank                              (linked account → reconciles to bank feed)
  razorpay-settlement-receivable    (Razorpay owes us; should clear ~0 at period end)
liabilities/
  gst-output-payable                (GST collected from customers)
  dispute-reserve
income/
  product-sales
  shipping-revenue
  sales-returns                     (contra-revenue; refunds reduce revenue here)
expense/
  processor-fees
  gst-input                         (GST on Razorpay's fee, recoverable)
  chargeback-loss
```
Keep it ~10–15 nodes in v1. Do not expand without a reason.

### 4.2 Entry types (the chart of accounts (COA) — hand-written, static in v1)
Declarative, parameterized, **balanced-by-construction** templates. Arithmetic in templates is `+`/`-` only — **derived amounts (e.g. GST split via division) are computed at bind time**, not in the template. Minimum v1 set:

- `dtc_sale` — params `{gross, net, gst, payment_id}`
  - `Dr assets/razorpay-settlement-receivable {gross}` (tx: payment_id)
  - `Cr income/product-sales {net}`
  - `Cr liabilities/gst-output-payable {gst}`
- `razorpay_settlement` — params `{net_deposit, fee, gst_on_fee, gross, bank_tx_id}` — posted via `reconcileTx` (bank is linked)
  - `Dr assets/bank {net_deposit}` (tx: bank_tx_id)
  - `Dr expense/processor-fees {fee}`
  - `Dr expense/gst-input {gst_on_fee}`
  - `Cr assets/razorpay-settlement-receivable {gross}`
- `refund_reversal` — params `{net, gst, refund_id}`
  - `Dr income/sales-returns {net}`
  - `Dr liabilities/gst-output-payable {gst}`
  - `Cr assets/razorpay-settlement-receivable {net+gst}`
- `chargeback_loss` — params `{net, gst, dispute_id}`
  - `Dr expense/chargeback-loss {net}`
  - `Dr liabilities/gst-output-payable {gst}`
  - `Cr assets/bank {net+gst}`

> Sign/normal-balance conventions: pick one convention (e.g. store signed amounts per the accounting equation) and enforce it in `post()`. Document it once in code. The template author and the validator must agree.

### 4.3 Normalized event (output of `normalize`)
```jsonc
{
  "id": "pay_PA1",
  "type": "payment" | "refund" | "settlement" | "dispute",
  "ts": 1714560000,
  "amount": 236000,            // minor units
  "fee": 4720,                  // where applicable
  "tax": 720,                   // Razorpay's GST on its fee
  "links": { "order_id": "order_A1", "payment_id": "pay_PA1" },
  "notes": { "sku": "SERUM-30", "gst_rate": "18" },
  "raw": { /* original Razorpay object */ }
}
```

### 4.4 Seeder artifacts
```
worlds/dtc/<period>/
  razorpay/        payments[], refunds[], settlements[], disputes[]   (agent input)
  bank-feed.json   bank credits/debits (independent 2nd record)        (agent input)
  truth/gl.json    hidden ground-truth ledger (entries + lines)        (SCORER ONLY)
```
**`truth/` must never be read by ingest, classify, reconcile, or any agent.** Only the scorer reads it. Consider enforcing this in code (separate package / no import path).

---

## 5. The close workflow (Go orchestrator)

```
close(world, period):
  raw        = ingest(world, period)         # CLI/api calls (or fixtures)
  events     = normalize(raw)                # → ordered event journal
  for e in events:
    entry = ruleEngine.classify(e)           # try templates by rule
    if entry == nil:                         # rule miss
      entry = flue.classify(e)               # AGENT (returns {entry_type, params})
    ledger.post(entry)                       # balance-or-error (Go ledger)
  breaks = reconcile(ledger, raw.settlements, bankFeed)   # 3 checks
  for b in breaks:
    res = flue.investigate(b)                # AGENT sub-workflow
    apply(res) or escalate(b)
  reports = ledger.reports(period)           # trial balance, BS, P&L, journal
  return score(ledger, truth(world, period)) # %, per-error records
```

Determinism requirement: given the same fixtures **and** the same recorded agent responses, `close` must produce byte-identical ledger + score. (See §12 on recorded agent responses.)

---

## 6. Ledger core (Go) — the contract

The ledger is a standalone Go package + a thin HTTP service. It knows **nothing** about Razorpay. Surface:

- `POST /ledger/entries` `{type, ik, params}` → expand template, validate balance, post. Idempotent on `ik`. Reject (don't silently fix) unbalanced entries.
- `POST /ledger/reconcile` `{type, params}` → post + reconcile linked-account tx (the Razorpay→bank case). Match the linked line against a known external tx; unmatched = break.
- `GET /ledger/reports/trial-balance?at=…` → accounts + balances; assert ΣDr = ΣCr.
- `GET /ledger/reports/balance-sheet?at=…`
- `GET /ledger/reports/income-statement?period=…`
- `GET /ledger/reports/journal?period=…`
- `GET /ledger/accounts/:path/balance?at=…` (used by reconcile check #3)

Schema (chart of accounts + entry types) is loaded from a config file at startup — this file **is the chart of accounts (COA)**. Keep it human-readable (JSON/YAML).

**Money-safety invariants (enforce in code, test hard):**
- integer minor units only;
- every entry balances or is rejected;
- reports are pure functions of posted entries (no side state);
- posting is idempotent on `ik`.

---

## 7. Reconciliation — the 3 checks

Run after all events post. Each failure is a **break** → `investigate` agent.
1. **Settlement → Bank:** every settlement's bank line matched a `bank-feed` credit of equal amount/date. (Largely handled inline by `reconcileTx`.)
2. **Batch-sum:** for each settlement, `Σpayments − Σrefunds − Σfees == net_deposit` (use `settlements recon` breakdown).
3. **Receivable-clears:** `assets/razorpay-settlement-receivable` balance at period end ≈ 0, except genuine T+2 in-transit amounts. A non-zero residual beyond in-transit ⇒ something captured-but-not-settled or settled-but-not-booked.

A break carries enough context for the agent to investigate: the settlement, its expected vs actual, and the candidate related events.

---

## 8. The agent interface (Flue/TS) — thin and stateless

Two endpoints. Both are **single-shot, stateless** in v1 (no long sessions).

```
POST /agents/classify
  in:  { event: NormalizedEvent }
  out: { entry_type: string, params: {...}, rationale: string }
       | { unclassifiable: true, reason: string }   // → escalate

POST /agents/investigate
  in:  { break: ReconBreak, candidates: Event[] }
  out: { resolution: { entry_type, params }[] , rationale }   // postings to add
       | { escalate: true, reason: string }
```

- The agent's **only** way to affect the ledger is returning `{entry_type, params}` that the Go ledger then validates. It cannot emit raw debits/credits.
- Tools available to the agent are **read-only Razorpay lookups** (orders/refunds/disputes fetch), implemented as calls to Go's read API so all data stays snapshotted.
- The **playbook is provided to the agent as a Skill** (SKILL.md) describing the entry types and when each applies — same source of truth the bookkeeper uses; generate the skill text from the schema file so they can't drift.
- Every agent call emits a **trace** (input, tools used, decision, rationale). Freeze the trace schema early (§13) — it's the learning seam.

---

## 9. Scoring

- Diff the agent's ledger against `truth/gl.json`. Primary metric: **% of journal entries correct** (account + amount). Secondary: **trial balance matches** (boolean) and **per-account balance deltas**.
- Emit `runs/<world>-<period>/errors.json`: one record per wrong/missing/extra entry, with `{event_id, got, want, error_class}`. **This is the only artifact the future learning layer consumes — freeze its schema.**
- Score must be deterministic given fixtures + recorded agent responses.

---

## 10. CLI surface (terminal-first)

```
ledger-flow seed   --world dtc --period 2026-05      # generate substrate + bank-feed + truth
ledger-flow run    --world dtc --period 2026-05      # run the workflow, print score
ledger-flow report --world dtc --period 2026-05 --kind trial-balance|balance-sheet|income|journal
ledger-flow show   playbook                          # print the entry types (schema file)
ledger-flow show   trace runs/<...>                  # print an agent trajectory
ledger-flow diff   --world dtc --period 2026-05       # agent ledger vs truth, line by line
```

---

## 11. Incremental build plan — controlled & testable

**Build bottom-up. Each phase has a test gate that must pass before the next. The agent is added LAST, against a known deterministic baseline.** This ordering is the primary defense against the project going haywire.

> Each phase is an independently reviewable unit of work. Phases 1–6 contain **no LLM** — the system fully runs and scores deterministically by the end of Phase 6. Phases 7–8 add the agent and can only *raise* the score.

### Phase 0 — scaffolding
- Go module (`ledger-flow`), folder layout, config loading, CLI skeleton (commands stubbed).
- **Gate:** `ledger-flow --help` lists commands; CI builds.

### Phase 1 — Ledger core (Go), no Razorpay, no agent ⭐ foundation
- Chart of accounts loader, entry-type template engine, `post()` with balance validation, idempotency, the four report queries.
- **Gate:** unit tests post a hand-written set of entries and assert exact trial balance / balance sheet / income statement / journal. Unbalanced entry is rejected. Money is integer-only. **This is the most important gate — do not proceed until the ledger is rock-solid.**

### Phase 2 — Seeder (deterministic fixtures + truth GL)
- Start with **synthetic fixtures only** (no live Razorpay). Generate payments/refunds/settlements/disputes + bank-feed + the matching `truth/gl.json` from the same generation rules.
- **Gate:** truth GL balances (ΣDr=ΣCr); fixtures validate against schemas; seeding is reproducible (seeded RNG / fixed inputs — no wall-clock randomness).

### Phase 3 — ingest + normalize (Go) over fixtures
- Read fixtures (later: live api), flatten to the event journal.
- **Gate:** golden test — fixtures → expected normalized event journal.

### Phase 4 — the bookkeeper + post wiring (Go, NO agent)
- Hand-written rules mapping events → entry types + computing derived params (GST split, gross-up). Unmatched events are **flagged and skipped** (not sent anywhere yet).
- **Gate:** `close` runs end to end on a fixture period and scores vs truth. Expect a **partial score** (whatever the rules cover, e.g. ~80%). Record this as the deterministic baseline. Skipped/unmatched events are reported, not crashed.

### Phase 5 — reconcile (3 checks) + bank feed
- Implement the 3 checks; detect breaks. No agent yet — breaks are listed.
- **Gate:** clean fixture period reconciles fully; a fixture with a **seeded break** (e.g. refund-in-batch) is detected and reported as a break.

### Phase 6 — scorer + reports CLI ✅ deterministic system complete
- Wire `score`, `report`, `diff`, `show`. Emit `errors.json` with the **frozen** schema.
- **Gate:** full deterministic pipeline produces score + reports + error records from a single command. **At this point the project is a complete, testable, agent-free product.**

### Phase 7 — Flue agent: classify-fallback only
- Stand up the Flue TS service with the `classify` endpoint; generate the SKILL.md from the schema; wire Go→Flue HTTP for rule-misses; read-only tools call Go's read API.
- **Testability:** support a **recorded-response mode** — capture real agent responses to a fixture and replay them so `close` stays deterministic in CI. Live LLM calls only in a separate, non-CI eval.
- **Gate:** previously-unmatched events now get classified; score **rises above the Phase-4 baseline**; replay mode is byte-deterministic.

### Phase 8 — Flue agent: investigate sub-workflow
- Add the `investigate` endpoint for reconcile breaks; apply resolutions or escalate.
- **Gate:** the seeded break from Phase 5 is **resolved by the agent** (postings added, reconciliation passes), verified in replay mode. Unresolvable breaks escalate cleanly (no guessing).

### Phase 9 — light live test-mode (optional, "a bit of live")
- Seeder can create a *small* number of real entities in Razorpay **test mode** via `razorpay-cli/api`, then snapshot responses to fixtures. Everything downstream still runs on fixtures.
- **Gate:** one small live-seeded period snapshots and closes reproducibly.

### Phase 10 — polish
- Audit trail (what the agent touched + rationale), nicer terminal output. HTML reports remain deferred.

**Milestone markers for the dynamic workflow:** Phases 1, 6, and 8 are natural checkpoints. Phase 6 = "deterministic product done." Phase 8 = "v1 feature-complete."

---

## 12. Testing strategy

- **Ground truth everywhere:** because the seeder emits `truth/gl.json`, *every* run is automatically scoreable — lean on this as the primary test oracle.
- **Golden fixtures** for normalize, classify (rule path), reports, reconcile.
- **Ledger property tests:** every posted entry balances; reports are pure functions of entries; idempotency holds.
- **Determinism:** fixtures + recorded agent responses ⇒ byte-identical ledger + score. CI must not call a live LLM.
- **Agent eval (separate, non-CI):** run live against a fixture period, record responses, assert score ≥ threshold. This is where real LLM quality is measured.
- **Reconcile break tests:** seed each break class (refund-in-batch, dispute-hold, timing-lag, mis-booked fee) and assert detection (Phase 5) then resolution (Phase 8).
- **The `truth/` isolation test:** assert (e.g. via package boundaries) that no non-scorer code path can read `truth/`.

---

## 13. How it grows from v1 (caveats & forward design)

The architecture is built so growth is **additive** (new data + new entry types), not rewrites. Each item below is OUT of v1 but should not be *blocked* by v1 choices.

- **Multi-channel (v1.5 — Amazon/Shopify):** each channel = a new **feed** (settlement report) in the seeder + a new **linked account** + 2–3 new **entry types** (`amazon_sale`, `amazon_settlement`, …) + a **fee-audit** reconcile check (does the platform's fee match the contracted rate?). Workflow, ledger core, and agent contract are unchanged.
  - *Caveat:* keep the income tree channel-segmentable now (`income/product-sales/<channel>`), so per-channel P&L is free later. Decide this in Phase 1 or it's a migration later.
- **Inventory / COGS (v2):** the only growth needing a **new data model** — a product catalog (unit costs) + inventory movements. Adds `assets/inventory`, `expense/cogs`, entry types (`inventory_purchase`, `record_cogs`), and an inventory sub-ledger + cost-flow method (weighted-avg/FIFO). The agent handles cost-flow judgment and negative-inventory investigation.
- **Learning layer / meta-agent (the big deferred piece):** consumes `errors.json` (clustered) and **authors new entry types** (schema mutations), each auto-validated by the ledger's balance check before being kept; held-out periods guard against overfitting.
  - *Caveat:* **freeze the `errors.json` and trace schemas early** (Phases 6–7). They are the entire contract the learning layer hangs off. Changing them later breaks the learner.
  - *Caveat:* the learner edits the **schema file (playbook)**, not code. Keep the chart of accounts (COA) fully data-driven so it's machine-editable.
  - DSPy/GEPA (prompt optimization) is a *different axis* (keeps the LLM per-item, tunes its prompt). If wanted, run it as a **Python sidecar** over traces — do not entangle it with the Go spine.
- **Flue autonomy / subagents / skills-evolution:** unused in v1; the natural home for the learning phase (subagents = sub-workflows + meta-agent; evolving SKILL.md = the growing playbook; trace export = the learning fuel). Because the agent already lives in Flue behind a thin interface, this is an expansion, not a port.
- **More worlds (SaaS, Marketplace):** each is a new seeder + schema; the same engine closes them. This is the "generalize across worlds" demo — enabled, not built, in v1.

### Anti-haywire principles (carry these through every phase)
1. **Deterministic-first, agent-last.** Never let an unfinished agent hide a broken spine.
2. **Ledger validates everything.** The agent proposes; the ledger disposes (balance-or-reject).
3. **Agent never emits raw postings** — only `{entry_type, params}`.
4. **Snapshot all Razorpay data to fixtures.** Reproducibility over live calls.
5. **Small, fixed chart of accounts & playbook in v1.** Resist scope creep into more accounts/entry types until a real case demands it.
6. **Freeze the seams** (`errors.json`, trace schema) before building anything on top of them.
7. **One world, one channel, one currency** until v1 is feature-complete and tested.
8. **Money is integer minor units, always.** No floats touch money.

---

## 14. Open decisions / notes for the executor
- **Project name:** `ledger-flow` (default; rename if desired before scaffolding).
- **Sign convention** in the ledger: pick one, document in code, enforce in `post()`. (§4.2)
- **Go↔Flue transport:** plain HTTP/JSON is fine for v1; keep the interface (§8) stable regardless.
- **Flue version risk:** Flue is early-stage. Keep the agent behind the §8 interface so it can be swapped for a hand-rolled Anthropic-SDK agent if Flue blocks you. Do **not** let Flue specifics leak into the Go orchestrator.
- **Razorpay test-mode credentials** needed only for Phase 9; Phases 1–8 run on synthetic fixtures.

---

## 15. Definition of done (v1)
- `seed → close → report` works for the DTC world from a single fixture period.
- Deterministic (agent-free) baseline score recorded (Phase 4/6).
- Agent raises the score above baseline; reconcile breaks resolved or cleanly escalated (Phase 7/8).
- All reports balance; `truth/` isolation holds; CI is deterministic (no live LLM).
- `errors.json` + trace schemas frozen and documented.
