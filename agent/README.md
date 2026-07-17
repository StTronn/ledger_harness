# ledger-flow-flue — the §8 LLM agent service

The judgment ~5% of the close, behind a thin HTTP seam. The Go orchestrator calls
this service on a rule-miss (`classify`) or a reconcile break (`investigate`); the
service asks an LLM to decide, and returns only `{entry_type, params}` — which the
Go ledger then validates (balance-or-reject). It never emits raw debits/credits and
never sees ground truth.

Everything except the live LLM call runs **without a key** (a deterministic stub
brain), so the plumbing, the §8 contract, and the GST split are all verifiable
offline.

## Endpoints (the §8 seam)

```
POST /agents/classify      { event, context, world, period }            → { entry_type, params, rationale } | { unclassifiable, reason }
POST /agents/investigate   { break, context, candidates, world, period } → { resolution: [{event_id, entry_type, params}], rationale } | { escalate, reason }
```

`params` values are integer paise. The split (`net = floor(gross·100/(100+rate))`,
`gst = gross−net`) is computed here deterministically and matches the Go
`gstsplit.SplitInclusive` byte-for-byte (guarded by `npm test`).

## Architecture — one `Brain`, two implementations

The LLM is isolated behind a `Brain` interface (`src/brain.ts`) so it stays
swappable (SPEC §14):

- **`stubBrain`** (`src/brain.ts`) — deterministic, no key. Reads the recovery
  context bundle supplied by Go and applies the decision rules directly. This is what
  makes the whole flow CI-verifiable.
- **`makeAiBrain`** (`src/brain_ai.ts`) — the live LLM, via the **Vercel AI SDK**
  (`ai` + `@ai-sdk/openai`). Go supplies the primary recovery context bundle. The
  agent's tools are read-only `ledger-flow context` commands for optional deeper
  lookup; its instructions are the
  generated `SKILL.md`; its output is a schema-validated decision.

> **Why the AI SDK and not Flue?** The real `@flue/runtime` (v0.11.x) has no inline
> "prompt + tools + structured output" call — sessions only exist inside its
> internal workflow/server harness, and its one HTTP entry can't carry a result
> schema. Per SPEC §14 ("keep the agent behind the §8 interface so it can be
> swapped") the AI SDK is wired behind the identical `Brain`. Swapping to Flue,
> Anthropic, or a local model later is one file — nothing else changes.

`SKILL.md` is generated from `../config/playbook.json` on startup (`src/skill.ts`),
so the agent's playbook knowledge can never drift from the ledger's.

## Run it live

```sh
cd close-agent/agent
npm install
go build -o bin/ledger-flow ../cmd/ledger-flow      # the CLI the agent's tools call

# Put OPENAI_API_KEY in ledger-flow/../.env  (i.e. /Users/rishav/projects/razorpay/.env)
# Start the service FROM THE REPO ROOT so the CLI tool defaults --root to the worlds/ dir:
cd ..                                                # -> repository root
PORT=8791 node --experimental-strip-types agent/src/main.ts
#   → ledger-flow-flue: §8 agent listening on http://localhost:8791 (brain=ai-sdk, ...)
```

Then, in another shell, run the close against it:

```sh
/tmp/ca close --world dtc --period 2026-04 --agent live --agent-url http://localhost:8791   # score = 100%
/tmp/ca close --world dtc --period 2026-03 --agent live --agent-url http://localhost:8791   # score = 100%
```

Live mode does **not** overwrite the committed recorded fixtures (it records to an
empty path), so a live run never disturbs the deterministic `replay` baseline.

### Knobs

| env | default | meaning |
|---|---|---|
| `PORT` | `8787` | HTTP port |
| `LEDGER_FLOW_BRAIN` | auto | `stub` \| `ai` (auto: `ai` when `OPENAI_API_KEY` is set, else `stub`) |
| `LEDGER_FLOW_MODEL` | `openai/gpt-4o-mini` | provider/model (the `openai/` prefix is stripped for the SDK) |
| `LEDGER_FLOW_BIN` | `agent/bin/ledger-flow` | the CLI binary the exploration tools shell out to |

## Verify (no key needed)

```sh
npm run typecheck      # tsc --noEmit, clean
npm test               # the GST split vectors match the Go formula
LEDGER_FLOW_BRAIN=stub PORT=8799 node --experimental-strip-types src/main.ts   # then curl the endpoints
```
