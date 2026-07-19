# ledger-viz

`ledger-viz` is the read-only visual layer for `ledger-flow`. It explains how raw
payment activity becomes a balanced ledger, shows the evidence and review path
around difficult cases, and sketches how completed runs can later support a
self-improving harness.

The application keeps three ideas separate:

1. **Posted ledger state** — entries that passed the deterministic posting and
   ledger boundaries.
2. **Recovery and review state** — evidence, agent recommendations, human review,
   and escalations that do not automatically change the books.
3. **Learning state** — a draft future projection in which completed run evidence
   becomes learning episodes and bounded improvement proposals.

## Views

| Route | Purpose |
| --- | --- |
| `/` | Explains the deterministic posting, recovery, and judgment harness. |
| `/self-improving` | Draft explanation of run evidence, learning episodes, bounded proposals, and prompt optimization. |
| `/ledger` | Plays a posted double-entry ledger as a transaction matrix. |
| `/run` | Shows rule misses, prepared evidence, recommendations, and escalations for example runs. |
| `/rto` | Shows the COD/RTO recovery and review story. |

## The accounting harness

```text
event
  → posting engine
      → known rule → ledger
      → rule miss  → recovery engine
          → safe candidate → posting engine → ledger
          → review required / unresolved → judgment agent → review or escalation
  → reconciliation
```

The boundaries are deliberate:

- The recovery engine finds and validates evidence; it never posts.
- The judgment agent recommends or escalates; it never writes directly to the ledger.
- Every accepted entry still goes through the posting templates and the ledger's
  balance and idempotency checks.
- A recommendation remains separate from posted ledger state.

## The self-improving harness — draft

The `/self-improving` page assumes the prerequisite run and observation contracts
described in [`../docs/SELF-IMPROVEMENT-PREREQUISITES.md`](../docs/SELF-IMPROVEMENT-PREREQUISITES.md)
already exist. They are not implemented by this visualization.

The proposed evidence flow is:

```text
recovery observations
  + agent traces
  + human review
  + ledger and reconciliation outcomes
  → learning episode
  → repeated pattern
  → bounded proposal or no_change
```

A deterministic coordinator selects and groups comparable episodes. A meta-agent
can then diagnose whether a repeated problem points to:

- a missing recovery policy,
- weak agent guidance,
- poor or missing source data, or
- genuine ambiguity that should remain with a human.

The meta-agent only proposes. A separate executor and evaluator handle an accepted
proposal, and promotion remains explicitly gated. The initial proposal surface is
limited to recovery-policy, agent-guideline, and data-quality improvements.

### GEPA and related optimization

[GEPA](https://arxiv.org/abs/2507.19457) is a complementary path for improving the
judgment agent's natural-language instructions. It uses execution traces and rich
textual feedback to reflect on failures, evolve prompt candidates, and test them.
It does not replace deterministic recovery or accounting policy.

Other possible paths include DSPy-style few-shot optimization, which selects useful
reviewed demonstrations, and later fine-tuning once a sufficiently large, curated
dataset exists. All candidates still require frozen evaluation rules, disjoint
held-out runs, and independent promotion. See the
[DSPy optimizer overview](https://github.com/stanfordnlp/dspy/blob/main/docs/docs/learn/optimization/optimizers.md)
for the distinction between instruction, few-shot, and weight optimization.

## Workspace layout

```text
ledger-viz/
  packages/ledger-model/   # framework-free TypeScript contracts and playback
  apps/web/                # Next.js 15 + Tailwind v4 renderer
```

### `@ledger-viz/model`

The shared model is pure TypeScript with no React dependency.

- **`LedgerFilm`** — `{ meta, accounts[], steps[] }`; each step carries balanced
  `Posting[]` values in integer minor units.
- **`RunFilm`** — the review story around a close: misses, recovery context,
  recommendations, and escalations. It does not mutate `LedgerFilm`.
- **`RtoFilm`** — the purpose-built COD/RTO recovery story.
- **`buildColumns(accounts)`** — groups ledger accounts by type.
- **`buildFrames(film)`** — produces the opening state and cumulative balance after
  each posted step. Signed convention: **Dr = +, Cr = −**.
- **`Playback`** — `frameAt / clamp / next / prev / first / last` over the frame range.
- **`formatMoney`** and film loaders — formatting and runtime validation.

Playback is a pure function of `(film, index)`; the ledger UI holds only the
selected film and playhead.

## Fixtures and current boundary

The JSON files under `packages/ledger-model/src/fixtures/` are generated or
committed projections of representative `ledger-flow` runs. The web app imports
them statically; there is currently no run database or live ingestion API.

This means `ledger-viz` is a presentation layer, not the operational ledger,
evidence store, or workflow controller. As the canonical run package is built,
films should be generated deterministically from those records rather than becoming
their source of truth.

## Run locally

```sh
cd ledger-viz
pnpm install
pnpm dev              # http://localhost:3000
pnpm build            # model typecheck + optimized Next.js build
```

## Main renderer areas

- `apps/web/components/ledger/` — transaction matrix, balances, playback, and entry inspection.
- `apps/web/components/run/` — review queue, decisions, pipeline stages, and impact.
- `apps/web/components/rto/` — COD/RTO remittance and deduction story.
- `apps/web/components/learn/` — reusable prose and flow-diagram renderer for the Harness pages.
- `apps/web/lib/explainer.ts` — accounting-harness page content.
- `apps/web/lib/self-improving.ts` — self-improving-harness draft content.

## Related design notes

- [`SELF-IMPROVEMENT-DRAFT.md`](SELF-IMPROVEMENT-DRAFT.md) — compact architectural draft.
- [`../docs/SELF-IMPROVEMENT-PREREQUISITES.md`](../docs/SELF-IMPROVEMENT-PREREQUISITES.md) — changes required before the learning layer can rely on run evidence.
- [`../docs/HARNESS.md`](../docs/HARNESS.md) — recovery/context harness responsibilities and invariants.
- [`../docs/ROADMAP-v2-v3.md`](../docs/ROADMAP-v2-v3.md) — broader ledger-flow evolution plan.
