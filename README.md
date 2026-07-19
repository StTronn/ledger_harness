# ledger-flow

Turn one month of a DTC brand's Razorpay activity into **balanced, reconciled, double-entry books** — then score the result against hidden ground truth. A deterministic posting and recovery engine handles routine accounting and prepares evidence for review. A judgment agent reviews ambiguous classifications and reconciliation breaks; it never posts. The accounts and posting templates are the **chart of accounts (COA)**.

Note: The agent will be migrated to pi and there are a lot of changes in agent architecture and how it integrates with the system


---

## The shape of it

```
ingest → normalize → posting engine → ledger → reconcile → reports → score
                         │ miss/break
                         ▼
                  recovery engine → agent review → trace / human queue
         └────────────── deterministic Go spine; agent output never posts ──────────────┘
```

The agent receives prepared recovery context and returns a review recommendation such as `{entry_type, params}` or an escalation. It never affects the ledger directly, never writes raw debits/credits, and never sees ground truth. Only deterministic or explicitly approved posting paths send entries to the ledger.

## Current boundaries and direction

The system keeps three states separate:

1. **Posted ledger state** — entries that passed the deterministic posting and ledger boundaries.
2. **Recovery and review state** — evidence, agent recommendations, human review, and escalations that do not automatically change the books.
3. **Learning state** — a future projection in which completed run evidence becomes learning episodes and bounded improvement proposals.

> **Agent-layer status:** the current [`agent/`](agent/) implementation is
> transitional.
>
> - **Pi migration:** the agent runtime is expected to move to **Pi**.
> - **Agent changes:** the execution, tool, trace, and orchestration boundaries
>   still need to be redesigned around the new runtime.

Three agent modes, all producing byte-identical results given the same inputs:

| Mode | What it does | Needs a key? |
|---|---|---|
| `off` | deterministic baseline — rule-misses are flagged & skipped | no |
| `replay` | consults the agent from committed recorded responses (CI-safe) | no |
| `live` | calls the real LLM agent service (OpenAI) | yes |

---

## Quickstart

```sh
cd close-agent
go build -o /tmp/ca ./cmd/ledger-flow

# Deterministic close (no agent, no key)
/tmp/ca close --world dtc --period 2026-05 --agent off       # score = 100% (clean period)

# The agent reviews the hard periods via committed recorded responses:
/tmp/ca close --world dtc --period 2026-04 --agent off        # score =  87%  (5 payments missing gst_rate)
/tmp/ca close --world dtc --period 2026-04 --agent replay     # recommendations logged; score remains 87%

/tmp/ca close --world dtc --period 2026-03 --agent off         # score =  97%  (1 unbooked refund → reconcile break)
/tmp/ca close --world dtc --period 2026-03 --agent replay      # recommendation logged; break remains for review
```

Reports, diff, and the chart of accounts (COA):

```sh
/tmp/ca report --world dtc --period 2026-05 --kind trial-balance   # balance-sheet | income | journal
/tmp/ca diff   --world dtc --period 2026-05                        # produced ledger vs truth, line by line
/tmp/ca show   playbook                                            # the chart of accounts (COA) + posting templates
```

---

## The agent harness — the "close graph" as a tool surface

The agent doesn't get raw fixtures dumped into its context. It gets **Tier-1 context bundles**: a pre-joined view of the close graph with the two derived facts it can't cheaply compute itself — `booked` (is an entry already posted for this event?) and the recovered `gst_rate` (with a citation back to the source object). These are exposed as a read-only CLI the agent calls as a tool (and a human can run to investigate):

```sh
# What breaks need investigating?
/tmp/ca context breaks --world dtc --period 2026-03
#   { "breaks": ["check3:receivable-residual:"] }

# The investigate bundle for a break — settlement, batch members, who's unbooked, recovered rates:
/tmp/ca context break "check3:receivable-residual:" --world dtc --period 2026-03
#   → surfaces rfnd_… with "booked": false and "recovered": { "gst_rate": "18", "_source": {...} }

# The classify bundle for a rule-missed event:
/tmp/ca context event <pay_id> --world dtc --period 2026-04
#   → the event + recovered rate + citation + applicable posting template
```

This is the agent's entire world: read-only, snapshotted, truth-free. Same surface for humans and the LLM.

---

## Running the live agent (OpenAI)

> These instructions describe the current transitional agent service. They will
> change as the agent layer moves to Pi.

The deterministic + replay paths above need no key. To run the **live** LLM agent end-to-end, see [`agent/README.md`](agent/README.md). In short:

```sh
# 1. start the agent service (reads OPENAI_API_KEY from ../.env)
cd close-agent && PORT=8791 node --experimental-strip-types agent/src/main.ts &

# 2. run the close against it
/tmp/ca close --world dtc --period 2026-04 --agent live --agent-url http://localhost:8791   # score = 100%
/tmp/ca close --world dtc --period 2026-03 --agent live --agent-url http://localhost:8791   # score = 100%
```

The live agent reasons over the same context bundles, returns `{entry_type, params}` or an escalation, and the Go side records the recommendation for review. It does not post or change the ledger.

---

---

## Invariants (enforced in code)

- **Money is int64 paise**, never float (AST-scanned guard tests).
- **Every entry balances or is rejected**; posting is idempotent on its IK.
- **`truth/gl.json` is scorer-only** — enforced by an import-graph guard test; ingest/classify/reconcile/agent/readmodel never import it.
- **One canonical source per seam**: the IK scheme (`classify.IKFor`), the GST split (`gstsplit.SplitInclusive`), the break key (`reconcile.Break.Key`) each live in exactly one place so producer and reader can't drift.
- **Determinism**: same fixtures + same recorded responses ⇒ byte-identical ledger + score. CI never calls a live LLM.

## Tests

```sh
gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1   # all green
go test ./internal/truth/ -run TestTruthIsolation                        # truth stays scorer-only
```
