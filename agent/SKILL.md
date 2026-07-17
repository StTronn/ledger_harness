# ledger-flow SKILL — the §8 classify/investigate playbook

This file is GENERATED from `config/playbook.json` so the agent and the Go rule
engine share one source of truth and cannot drift. It describes the ledger entry
types, the inclusive-GST split, and the decision rules the agent must follow.

The agent's ONLY way to affect the ledger is returning `{entry_type, params}`;
the Go ledger expands those into a balanced double-entry and validates it
(balance-or-reject). The agent never emits raw debits/credits. Its tools are
read-only Razorpay lookups exposed as the `ledger-flow context` CLI.

When a context bundle does not pre-solve the case, EXPLORE with the tier-2
lookup `getEntity(id)`: any event id (raw object + booked + graph edges), an
order id (line items + authoritative rate), `ratecard/<channel>` (the
contracted fee row), or an account path (its period balance). Cite what you
find; never state a value you did not read from a tool.

## Entry types (from the playbook)

### `dtc_sale`

Customer purchase captured by Razorpay: gross receivable from Razorpay, split into net product revenue and output GST.

- Params: `gross`, `net`, `gst`, `payment_id`
- Transaction param (idempotency key): `payment_id`
- Lines:
    - Dr `assets/razorpay-settlement-receivable` = gross
    - Cr `income/product-sales` = net
    - Cr `liabilities/gst-output-payable` = gst

### `razorpay_settlement`

Razorpay deposits net cash to the bank, having netted its fee and GST-on-fee out of the gross receivable. Posted via reconcileTx (bank is the linked account).

- Params: `net_deposit`, `fee`, `gst_on_fee`, `gross`, `bank_tx_id`
- Transaction param (idempotency key): `bank_tx_id`
- Lines:
    - Dr `assets/bank` = net_deposit
    - Dr `expense/processor-fees` = fee
    - Dr `expense/gst-input` = gst_on_fee
    - Cr `assets/razorpay-settlement-receivable` = gross

### `refund_reversal`

Customer refund: reverse net revenue (into the contra-revenue account) and the output GST, clearing the receivable for the refunded amount.

- Params: `net`, `gst`, `refund_id`
- Transaction param (idempotency key): `refund_id`
- Lines:
    - Dr `income/sales-returns` = net
    - Dr `liabilities/gst-output-payable` = gst
    - Cr `assets/razorpay-settlement-receivable` = net+gst

### `price_adjustment`

Goodwill credit / price adjustment refund (credit note): reduce revenue via discounts-allowances and the output GST, clearing the receivable for the credited amount. Truth-only in v1 agent policy: the agent escalates goodwill rather than booking it.

- Params: `net`, `gst`, `refund_id`
- Transaction param (idempotency key): `refund_id`
- Lines:
    - Dr `income/discounts-allowances` = net
    - Dr `liabilities/gst-output-payable` = gst
    - Cr `assets/razorpay-settlement-receivable` = net+gst

### `chargeback_loss`

Lost dispute: recognise the loss and reverse the output GST, with cash leaving the bank.

- Params: `net`, `gst`, `dispute_id`
- Transaction param (idempotency key): `dispute_id`
- Lines:
    - Dr `expense/chargeback-loss` = net
    - Dr `liabilities/gst-output-payable` = gst
    - Cr `assets/bank` = net+gst

## The inclusive-GST split (canonical — must match Go to the paise)

Catalogue grosses already INCLUDE GST. Given integer-paise `gross` and an integer
percentage `ratePercent` (> 0):

```
net = floor(gross * 100 / (100 + ratePercent))   // truncate toward zero
gst = gross - net                                  // remainder folds into GST
```

`net + gst === gross` always holds. Worked vectors:
- `split(265878, 18) => { net: 225320, gst: 40558 }`
- `split(248591, 18) => { net: 210670, gst: 37921 }`

The agent recovers only the RATE (from the order's notes); the split itself is
mechanical and must agree with the seeder, or the ledger will reject the entry.

## Param shapes per entry type

- `dtc_sale` (books a payment): `{ gross, net, gst, payment_id: 0 }` where
  `gross = event.amount` and `(net, gst) = split(gross, rate)`.
- `refund_reversal` (books a refund): `{ net, gst, refund_id: 0 }` where
  `gross = refund.amount` and `(net, gst) = split(gross, rate)`.

(The `*_id` param is always `0` here — a placeholder. The Go orchestrator carries
the real transaction id separately and derives the idempotency key.)

## CLASSIFY decision rule (POST /agents/classify)

The classify agent recovers payments (missing rates) and judges PARTIAL refunds.

1. Use the supplied event recovery context as the primary evidence. Use `ledger-flow context event <event_id> --world <world> --period <period>` only for a deeper lookup.
2. PAYMENT: read the recovered rate (`recovered.gst_rate`, or the event's own
   `gst_rate` if it already carries one), and book a `dtc_sale` with
   `gross = event.amount` and `(net, gst) = split(gross, rate)`. If no rate can
   be recovered, return `{ unclassifiable: true, reason }`.
3. PARTIAL REFUND (the context's `event.parent_amount` is set — the refund is
   smaller than its payment). The ambiguity is the ENTRY TYPE: one line item
   returned (`refund_reversal`) vs a goodwill/manual credit note
   (`price_adjustment`). The bundle ships `order_items` and precomputed
   `candidates`. Policy — NEVER guess:
   a. If `event.reason` is annotated (e.g. "goodwill"), a manual credit is a
      HUMAN policy call: return `{ unclassifiable: true, reason }`. The agent
      never books `price_adjustment` on its own.
   b. If EXACTLY ONE candidate is an `item-match`/`pair-match`, that is strong
      evidence of a line-item return: book `refund_reversal` with
      `gross = refund.amount` at the MATCHED ITEM's rate, citing the candidate's
      `_source`.
   c. Multiple matches (ambiguous) or no match (unexplained): return
      `{ unclassifiable: true, reason }`.
4. Any OTHER event type (full refund, settlement, dispute) is not something
   classify recovers in v1. Return `{ unclassifiable: true, reason }`.

## INVESTIGATE decision rule (POST /agents/investigate)

For a `receivable-residual` break (the settlement receivable did not clear to ~0):

1. Use the supplied break recovery context as the primary evidence. Use `ledger-flow context break <break_key> --world <world> --period <period>` only for a deeper lookup.
2. In the settlement `batch`, find members with `booked == false` whose `type` is
   `refund` — an unbooked refund leaves exactly its gross stuck in the receivable.
3. A member with `parent_amount` set is a PARTIAL refund: its intent (return vs
   goodwill) is a classify-side judgment — NEVER reverse it here. If every
   unbooked refund is partial, return `{ escalate: true, reason }`.
4. For each FULL unbooked refund, recover its rate from `recovered.gst_rate`,
   compute `(net, gst) = split(refund.amount, rate)`, and emit one
   `refund_reversal` posting with `event_id = <refund id>` and
   `params = { net, gst, refund_id: 0 }`.
5. If no unbooked refund is found, return `{ escalate: true, reason }` — the
   orchestrator lists the break unresolved.
