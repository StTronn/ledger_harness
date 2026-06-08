# close-agent — Build Handover / Resume State

> **Read this first if you're resuming the build.** It captures what's done, the locked decisions, how the work is being executed, how to verify it, and the exact next task. Pairs with `docs/SPEC.md` (the v1 design, source of truth) and `docs/ROADMAP-v2-v3.md` (post-v1 growth).
> **Last updated at:** Phase 8 complete (the `investigate` agent — Go side, record/replay).

---

## 1. Where we are

| Phase | What | Status |
|---|---|---|
| 0 | Scaffolding (cobra CLI, config, layout) | ✅ committed `3d7e316` |
| 1 | Ledger core (money, playbook, post=balance-or-reject+idempotent, reports) | ✅ committed `e185ed9` |
| 2 | Seeder (deterministic fixtures + truth GL + `truth/` isolation guard) | ✅ committed `69deedd` |
| 3 | Ingest + normalize (§4.3 journal, golden-tested) | ✅ committed `87a8f80` |
| 4 | Rule-engine classify + `close` wiring + minimal score | ✅ committed `1c4efd9` |
| 5 | Reconcile (3 checks) + bank feed + seeded break (`--inject refund-in-batch`) | ✅ committed `6cc14f0` |
| 6 | Scorer + reports CLI + frozen `errors.json` ⭐ deterministic product complete | ✅ committed `3a2e194` |
| 7a | Agent seam (Go side): long tail, `orders.json`, record/replay, traces | ✅ committed `7b49ba6` |
| 8 | `investigate` agent (resolve reconcile breaks), Go side record/replay | ✅ done |
| 8.5 | Async classify pipeline (`classify work`/`apply`) + provenance citations + re-verifying validator + review seam; `close --agent off` parks the queue | ✅ done (see §9 + ROADMAP §7) |
| **7b** | **Flue TS `classify` service (live agent behind §8)** | **⏭ DO THIS NEXT** |
| 9 | (optional) light live Razorpay test-mode seeding | not started |
| 10 | (optional) polish: audit trail, nicer output | not started |

**Decision (locked):** do **Phase 8 first, then Phase 7b.** Rationale: Phase 8 reuses the proven Go record/replay pattern from 7a, so it's fully CI-verifiable without a live LLM; the Flue service (7b) can only be built/typechecked under the recorded-response-only constraint, so it's lower verified-value to do first.

---

## 2. Locked decisions (do not re-litigate without the user)

- **Agent host = Flue** (`flue@0.2.6`, TS). The user explicitly kept Flue over a hand-rolled Anthropic-SDK agent. Keep the agent behind the §8 interface so it stays swappable (SPEC §14), but the chosen host is Flue.
- **LLM execution = recorded-response only** for CI. No live LLM in CI. The deterministic seam lives in **Go at the §8 boundary**: `replay` mode reads committed response fixtures; `live` mode (Flue) is built but not run in CI. A live eval would need an `ANTHROPIC_API_KEY` and is out of CI scope.
- **Tech:** Go stdlib + `github.com/spf13/cobra` only (deterministic core is dependency-light). Money is **int64 paise**, never float. JSON for playbook/fixtures.
- **Scope discipline:** fixed ~10–15 node chart, 4 entry types, one world (dtc), one channel (Razorpay), INR only. Don't expand without a real case (SPEC anti-haywire #5).

---

## 3. How the build is being executed (the workflow + gate pattern)

Each phase is built by a **dynamic workflow** then **gated by the main agent**:

1. **Workflow** (`/Users/rishav/projects/razorpay/.orchestration/close-agent-phase.js`): a reusable per-phase script. Invoke it with the **Workflow tool**, passing the phase spec as `args` (JSON object: `{id,title,goal,modules:[{name,instructions}],gate}`). It runs dev modules **sequentially** (shared greenfield code → avoid file conflicts) → a correctness **fix** stage (fires only if dev self-reports red) → ONE light advisory high-level **review**. Review is intentionally light per the user ("focus on correctness; review just notes high-level scaffolding/abstractions/modules").
2. **Main agent gate (this is the real verification):** after the workflow returns, the main agent runs the actual gate itself — `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./... -count=1`, then *exercises the CLI* and does an **independent check** (e.g. recompute expected numbers in Python and diff vs output). Only commit when the gate genuinely passes.

**Critical lesson:** the workflow harness's success/failure verdict is **not** ground truth. Phase 7a's workflow reported `failed` (a dev agent skipped its final StructuredOutput, so fix+review never ran) — yet the code was fully written and passed the gate. Conversely earlier phases reported success but still needed gating. **Always gate empirically; never trust the workflow verdict or the editor diagnostics.**

**Recurring false alarms to ignore:** (a) editor LSP `undefined: X` / "unused function" diagnostics are almost always **stale mid-edit** snapshots — verify with a real `go build`/`grep` before acting. (b) The shell is **zsh**: unquoted `$var` does **not** word-split, so `binary $cmd` passes the whole string as one arg — use `${=cmd}` or literal args when testing the CLI in a loop. (c) Don't put backtick-quoted words in `git commit -m` (zsh runs them as command substitution).

---

## 4. Architecture & invariants (enforce these in every phase)

Deterministic Go spine, LLM confined to a tiny typed surface:
```
ingest → normalize → classify(rules) → [agent on rule-miss] → post → reconcile → [agent on break] → reports → score
```
1. Deterministic spine never crosses the LLM boundary.
2. Ledger validates everything: every entry **balances or is rejected**; posting **idempotent on IK**; reports are **pure** functions of entries.
3. Agent emits only `{entry_type, params}` — never raw debits/credits. The ledger binds+validates.
4. `truth/gl.json` is **scorer-only**. Enforced by `internal/truth/isolation_test.go` (`TestTruthIsolation`) — it scans the import graph and fails if any package outside the allow-list (`seed`, `truth`, `score`) imports `internal/truth`. **When adding a new truth reader, update the allow-list there.** `agentclient`, `classify`, `closer`, `reconcile` must NOT import it.
5. **GST parity:** the inclusive split lives once in `internal/gstsplit.SplitInclusive` (`net = gross*100/(100+rate)`, `gst = gross-net`). Both the seeder and the classifier call it so produced entries equal truth to the paise. Never re-implement the formula.
6. Money int64 paise everywhere; a `nofloat_test.go` (AST scan) in money/seed/ingest/reconcile/gstsplit bans float tokens.
7. Frozen seams (version-bump in lockstep with consumers): **`errors.json` schema v1** (`internal/score`) and the **trace schema v1** (`internal/agentclient`/closer).
8. Income tree is channel-segmentable (`income/product-sales` as a path) for future per-channel P&L.
9. Known seam: tx-id params (payment_id etc.) are passed to `ledger.Bind` as a `money.Money` placeholder (0); the real **string** id rides on `Entry.TxID` (mirrors the seeder's truthBinder). This is intentional, not a bug.

---

## 5. Package map (`internal/`)

| Package | Role |
|---|---|
| `money` | int64-paise Money type (Parse/Format/Add/Sub, float-free) |
| `gstsplit` | canonical inclusive GST split (single source for seeder + classifier) |
| `config` | playbook loader (chart of accounts + entry-type templates) from `config/playbook.json` |
| `ledger` | Bind (template→Entry), Post (balance-or-reject + idempotent), reports (TB/BS/IS/journal), `AccountBalance` |
| `truth` | ground-truth GL types + the ONLY allowed truth reader (isolation guard lives here) |
| `seed` | deterministic seeder: RNG from (world,period), Razorpay-shaped fixtures, `orders.json`, ambiguity injection, break injection (`--inject`), truth GL emitter |
| `ingest` | read fixtures → raw types; normalize → §4.3 event journal (golden-pinned) |
| `classify` | per-event rule engine → Classification {entry_type, params, ik, tx_id, ts}; the SAME shape the agent fills |
| `agentclient` | §8 clients: `Classify` and `Investigate`, each with `replay` (committed fixture) / `live` (Flue HTTP) modes; frozen classify + investigate trace emission |
| `closer` | the `close` orchestrator: ingest→normalize→classify→[agent]→post→reconcile→[investigate agent]→score; writes errors.json + traces; `GenerateInvestigateRecorded` builds the investigate fixture |
| `reconcile` | §7 three checks → []Break (with context for the investigator) |
| `score` | diff produced vs truth by event_id; %correct + TB-match + per-account deltas + errors.json |
| `cli` | cobra commands: seed / close / report / diff / show / record |

**CLI surface:** `seed --world --period [--inject <class>] [--ambiguity ...] [--root]`, `close --world --period --agent off|replay|live`, `report --world --period --kind trial-balance|balance-sheet|income|journal`, `diff --world --period`, `show playbook|trace <path>`, `record-responses --world --period` (hidden; regenerates the recorded classify fixture from orders.json), `record-investigations --world --period` (hidden; regenerates the recorded investigate fixture from the snapshotted fixtures).

**Periods on disk:** `worlds/dtc/2026-05` (clean, 100%), `worlds/dtc/2026-04` (hard: ~15% gst_rate-stripped payments + `orders.json` recovery source + committed `agent/classify.recorded.json`), `worlds/dtc/2026-03` (Phase-8 break: one gst_rate-stripped refund → check#3 residual + committed `agent/classify.recorded.json` (escalates the refund) + `agent/investigate.recorded.json` (resolves it)). `runs/` is gitignored (errors.json, trace.json, investigate-trace.json land there).

---

## 6. How to verify the current state (sanity check before resuming)

```sh
cd /Users/rishav/projects/razorpay/close-agent
gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1   # all clean / pass (13 pkgs)
go build -o /tmp/ca ./cmd/close-agent
/tmp/ca close --world dtc --period 2026-05 --agent off      # 45/45, 0 breaks, score = 100%
/tmp/ca close --world dtc --period 2026-04 --agent off      # 36/41, 5 skips, 1 break, score = 87%
/tmp/ca close --world dtc --period 2026-04 --agent replay   # 41/41, 0 breaks, 5 traces, score = 100%
/tmp/ca close --world dtc --period 2026-03 --agent off       # 37/38, 1 skip, 1 break (check#3 residual), score = 97%
/tmp/ca close --world dtc --period 2026-03 --agent replay    # investigate resolves 1 break -> 0 breaks, 38/38, score = 100%
go test ./internal/truth/ -run TestTruthIsolation -count=1  # PASS (truth stays scorer-only; agentclient+closer never import it)
```

---

## 7. DONE — Phase 8: the `investigate` agent (Go side, record/replay)

Resolves reconcile breaks via the agent, mirroring 7a. §8 investigate interface:
```
in:  { break: ReconBreak, candidates: Event[] }
out: { resolution: {entry_type, params}[], rationale } | { escalate: true, reason }
```

**Key design decision (made with the user):** the committed break is a **check #3
"settled-but-not-booked" residual**, NOT the check #2 batch-sum break the original
plan named. Reason discovered during the build: check #2 (batch-sum) is a PURE
function of the raw settlement/refund files — the ledger never enters it — so **no
ledger posting can ever clear a check #2 break**, and the refund it concerns is
already booked (so "score rises" is impossible there). Only check #3 reads the
posted ledger, so it's the one break an investigate POSTING can resolve. See
`docs/reconcile-explainer.html` for the full walkthrough (interactive).

**What shipped:**
1. **New inject `unbooked-refund`** (`internal/seed/inject.go`): strips `gst_rate`
   from a refund a settlement netted, leaving everything else intact (still in
   refunds.json + its batch, deposit unchanged). The deterministic rules can't book
   it (no rate → no GST split) → it's skipped → the receivable is short by its gross
   → **check #3 residual**; check #2 stays green (it never reads `gst_rate`). Truth is
   unperturbed and still books the refund at its true rate. Committed period:
   **`worlds/dtc/2026-03`** (+ `agent/classify.recorded.json` escalating the refund,
   `agent/investigate.recorded.json` resolving it).
2. **`agentclient.Investigate`** (`investigate*.go`): `BreakSummary`/`Posting`/
   `InvestigateResult` types, `ReplayInvestigateClient` (keyed by break id) +
   `LiveInvestigateClient` (POST `/agents/investigate`), `investigate.recorded.json`
   read/write (frozen schema v1), and a frozen `InvestigateTrace` (schema v1).
   Never imports `internal/truth`. Recovery source = `orders.json` (rate) +
   `refunds.json` (the refund), via `agentclient.OrderGSTRates`.
3. **Wired into `closer`** (`internal/closer/investigate.go`): after reconcile, each
   break → `Investigate` → bind+post the resolution(s) (closer owns IK/TxID/Ts; agent
   supplies only `{entry_type, params}`) → **re-reconcile** → break clears; escalate
   cleanly otherwise (break stays listed, recorded as a `closer.Escalation`, never
   guessed). Investigate traces → `runs/<w>-<p>/investigate-trace.json`. The
   deterministic generator is `closer.GenerateInvestigateRecorded` (reproduces the
   committed fixture byte-for-byte); CLI: hidden `record-investigations`.

**Phase 8 gate — all verified green:**
- `2026-03 --agent off` → 1 break (check#3 residual), score 97% (refund_reversal missing).
- `2026-03 --agent replay` → investigate books the refund_reversal → **0 breaks, score 100%**, byte-deterministic, both classify + investigate traces emitted.
- an unresolvable break (check #2) **escalates cleanly** (tested in `TestUnresolvableBreakEscalates`).
- 2026-05 / 2026-04 unchanged; gofmt/vet/build/test + golden + truth-isolation all green.

**Note for 7b:** the live investigate response shape adds an `event_id` per posting
(the refund whose reversal is missing) so the orchestrator can attribute/IK the
entry — the SPEC §8 `{entry_type, params}[]` plus the source event id. The Flue
investigate agent must return that. `entryRefsForType` maps entry type → IK/TxID
scheme.

## 8. NEXT TASK — Phase 7b: the Flue `classify` service (TS)

Stand up the Flue TS service implementing §8 `classify` (and later `investigate`): `createAgent({ model: 'anthropic/...', instructions, tools, skills })`, `session.prompt(input, {result: schema})` for `{entry_type, params, rationale}`. Generate `SKILL.md` from `config/playbook.json` (so playbook and skill can't drift). Read-only tools (`getOrder`/`getPayment`) call a Go read API over snapshotted fixtures. Flue auto-exposes `POST /agents/<name>/<id>`; `agentclient` live mode posts there and records responses for replay. **Verification under recorded-only:** build/typecheck + confirm the §8 request/response shape and that live→record reproduces the committed fixtures; classification *quality* needs a live key (non-CI eval), which is out of CI scope. Note: the Go-centric `close-agent-phase` workflow is Go-shaped — for the TS service, either pass TS-specific instructions/gate in the phase args or build it with direct agents using a TS gate (`npm i`, `tsc`, `flue build`).

## 9. Async classify pipeline (Phase 8.5, done — beyond the original plan)

The classify agent can now run ASYNCHRONOUSLY, decoupled from the close, with the
§8 surface hardened. Full design in `docs/ROADMAP-v2-v3.md` §7. Shape:

```
close --agent off  → books the bulk, PARKS its skipped events as runs/<w-p>/proposals.json (the queue)
classify work      → async worker (stub brain): proposals.json -> results.json  (recover rate, CITE the source)
classify apply     → validate citation -> review -> derive money (gstsplit) -> Bind+Post -> reconcile -> score
```

- `internal/classifyq`: keyed stores (`proposals.json`/`results.json`), stub-brain
  worker (`classifyOne` is the slot the live LLM replaces), `ValidateRate`
  (re-reads the cited `orders.json` field — rejects forged/stale citations + non-slab
  rates), `Reviewer` seam (`AutoReviewer` default, `RecordedReviewer` fail-closed).
- `closer.RunWith` now parks skips → `proposals.json` (via `writeProposalsQueue`);
  `closer.RunApply` is the APPLY stage (option A: re-close that books parked misses
  from `results.json`). The agent emits the RATE only; APPLY derives net/gst.
- CLI: `classify work` + `classify apply` (the queue comes from `close --agent off`).
- Gates: `2026-04` off=87% (parks 5) → work → apply=100%, byte-deterministic; a
  forged citation is rejected by the validator (skipped, score drops); recorded
  reviewer reject skips a valid proposal. truth-isolation green (classifyq clean).

**Known debt (do before extending — see ROADMAP §7.5):** `RunWith`/`RunApply`
duplicate ~70% of the spine; factor `runCore` + a pluggable `MissResolver`. The
investigate agent is still sync-only and not yet provenance/validator-hardened.
