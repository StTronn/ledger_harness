# Harness Over Ledger

The harness is the agent-facing layer around the recovery engine. It prepares
the same traceable, read-only context for the judgment agent and a human
reviewer. It supplies evidence; it does not create accounting facts or post to
the ledger.

The core split is simple:

```text
safe recovery candidate    → posting engine → ledger
review required/unresolved → harness → judgment agent → recommendation or escalation
```

For known edge cases, recovery follows a defined evidence path and validates it
against policy. GST is the deterministic example: payment → order → GST rate,
then a validated rate can return to the posting engine. Partial refunds are the
review example: refund → payment → order items may provide useful evidence, but
policy requires review because that evidence does not prove which accounting
treatment to reverse. The agent never posts either case directly.

These packages are **read-only and truth-free**: nothing under them can write
to the ledger or reach `truth/gl.json` (the import-graph guard enforces it).
Pairs with `docs/SPEC.md` §8 and `docs/ROADMAP-v2-v3.md` §7.

```
internal/ledgerflow/
  context/        the GRAPH      projection of one period's run: events, edges
                                 (payment↔order↔refund, settlement batches), the
                                 posted-IK set (`booked`), breaks, balances;
                                 assembles the Tier-1 context bundles
  recovery/       the ENGINE     deterministic recovery decisions and policies
    policychecks/ the POLICIES   per problem class, WHERE the authority lives
                                 and WHAT a sane value looks like
internal/world/
  feeds/          the SOURCES    canonical readers for the snapshotted business
                                 fixtures (orders.json, ratecard.json)
```

## The three layers, by responsibility

**`feeds`** — one reader per fixture, defined once. Every consumer (the graph,
the recorded-response generator, the CLI) reads through it, so a feed's shape
cannot drift between consumers. Current feeds: `orders.json` (authoritative tax
metadata + line items), `ratecard.json` (the merchant's contracted fee schedule,
emitted by the seeder from the same constants it prices fees with).

**`context`** — the mechanism. Pure projection of a period's run built by
`run.BuildRecoveryEngine` (an agent-off run + the raw fixtures + the feeds). It
knows *how things connect* and the two derived facts everything hinges on:

- `Booked(event)` — is an entry posted under this event's canonical idempotency
  key (`posting.IKFor`)? O(1), consistent with the posters by construction.
- the graph edges — `refund → payment → order`, settlement batch membership,
  account balances.

**`recovery/policychecks`** — the knowledge. Each `Policy` is one rule of the form
*"for THIS kind of gap, THE authority lives THERE, and a sane value looks like
THIS"*: self-selecting (`AppliesTo`), walking the graph through the read-only
`Graph` lens, contributing a `Finding` of **facts** (each with a citation and a
validation verdict) and/or **candidates** (evidence, e.g. line-item matches).
Policies never guess; an empty finding or a `valid: false` fact is a
first-class honest answer. The registry (`Default()`) is the one table the
recovery knowledge lives in:

| Policy | Applies to | Walk | Validation |
|---|---|---|---|
| `gst-rate-from-order` | payment/refund with no own rate | event → order → `notes.gst_rate` | closed slab set {5, 12, 18} |
| `refund-line-item-match` | partial refunds | refund → order → line items (exact item / pair-capped) | amount equality, cited per item |
| `fee-tier-from-ratecard` | settlements | batch members × ratecard `fee_bps` (fees price PER PAYMENT, floored each, never refunded) | expected == charged |

Growth model (roadmap §7): a new problem class = a new policy in the table —
data-shaped code, not new bundler logic. A *novel* case with no policy degrades
to tier-2 exploration (below), and a repeated exploration is what the v3
learning layer promotes into the next policy.

## The agent-facing surface (the harness CLI)

```
context breaks                      discovery: what needs investigating
context event  <id>                 Tier-1 classify bundle  (pre-solved)
context break  <key>                Tier-1 investigate bundle (pre-solved)
context entity <id>                 Tier-2 self-directed lookup (exploration)
```

Tier-1 bundles are the recovery engine's prepared output per event or break.
They include the source event, recovered facts and citations, policy verdicts,
candidate evidence, booked state, relevant breaks, and balances. Common cases
therefore arrive with a defined evidence path rather than asking the agent to
assemble one.

Tier-2 (`entity`) resolves *anything the agent can name* for a novel case: an
event's raw snapshot plus `booked` state and graph edges, an order with its
items, `ratecard/<channel>`, or an account path's balance. It is a deeper,
read-only lookup, not a path to write to the ledger. The TypeScript agent calls
these through `getEventContext`, `getBreakContext`, and `getEntity`.

## Invariants

1. Nothing under `ledgerflow/context`, `ledgerflow/recovery`, or `world/feeds`
   imports `internal/truth` (guard-tested).
2. Policies reach data only through the `Graph` lens — pure and unit-testable.
3. Every recovered fact carries `_source: {object, path}` — re-checkable, so a
   fabricated value needs a fabricated citation.
4. A fact that fails validation is surfaced `valid: false`, never dropped.
5. One implementation per concern: one feed reader, one matcher
   (`policychecks.MatchLineItems`, shared with the recorded-response
   generator), one IK scheme (`posting.IKFor`).
