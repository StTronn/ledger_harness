// explainer.ts — the content of the Harness page, as data.
//
// The article is an array of sections, each a list of typed blocks. To extend
// the page you edit THIS file only — add a section, add a block — and the
// renderer (components/learn/article.tsx) picks it up. Keep it plain prose; the
// goal is a simple, modifiable starting point, not a CMS.

export type Block =
  | { kind: "p"; text: string }
  | { kind: "note"; text: string } // a brand-accented callout
  | { kind: "code"; text: string } // a mono block (ascii diagrams / json / shell)
  | { kind: "steps"; items: string[] } // an ordered list
  | { kind: "flow" } // the main component-based decision flow
  | { kind: "evidence" } // the recovery engine evidence paths
  | { kind: "context" } // the harness context schemas
  | { kind: "gst-policy" } // the GST recovery policy path
  | { kind: "partial-refund-policy" }; // the partial-refund review path

export interface Section {
  id: string;
  eyebrow: string;
  title: string;
  blocks: Block[];
}

export const article: { title: string; lede: string; sections: Section[] } = {
  title: "Harness on top of  ledger",
  lede: "A simple path from Razorpay events to a balanced double-entry ledger, with deterministic recovery and judgment only where it is needed.",

  sections: [
    {
      id: "shape",
      eyebrow: "The data flow",
      title: "From events to posted entries",
      blocks: [
        {
          kind: "p",
          text: "ledger-flow turns a period of Razorpay activity into balanced journal entries. The posting engine handles known cases. When an event does not fit a rule, the recovery engine decides whether the available facts are enough for a safe deterministic candidate. Only unresolved cases reach the judgment agent.",
        },
        {
          kind: "flow",
        },
        {
          kind: "note",
          text: "The recovery engine never posts. It returns a candidate. The posting engine applies that candidate, and the ledger validates it. The judgment agent never writes directly to the ledger.",
        },
      ],
    },
    {
      id: "recovery",
      eyebrow: "Recovery engine",
      title: "Constantly updated deterministic linter",
      blocks: [
        {
          kind: "p",
          text: "For each known edge case, it follows a defined evidence path, validates the result against policy, and returns either a safe candidate or a clear reason to ask for judgment.",
        },
        { kind: "evidence" },
        {
          kind: "note",
          text: "Regarding the constantly update part as we keep on seeing edge cases we can keep on adding solution for each through agent, it didn't use to make sense before but with continuos tracking and because agent can maintain it, this layer helps a lot. Eventually actuall fixes should be made in the codebase",
        },
        {
          kind: "note",
          text: "This layer serves as the buffer to have changes done by loop agentic cycles making fixes, and then reviewing them and doing actuall long term fixes",
        },
      ],
    },
    {
      id: "harness",
      eyebrow: "The harness",
      title: "Recovery tools become the agent's harness",
      blocks: [
        {
          kind: "p",
          text: "The harness is the agent-facing layer around the recovery engine. It exposes the same prepared, read-only context to the judgment agent and to a human reviewer.",
        },
        {
          kind: "context",
        },
        {
          kind: "note",
          text: "The harness does not create new accounting facts or change the books. It gives the judgment agent a prepared, traceable source for review.",
        },
      ],
    },
    {
      id: "example",
      eyebrow: "A worked example",
      title: "A payment arrives without its GST rate",
      blocks: [
        {
          kind: "p",
          text: "A payment of ₹2,658.78 arrives without a GST rate. The posting engine cannot split revenue and GST, so it records a miss instead of guessing. The recovery engine follows the order link, validates the missing rate, and returns a safe candidate:",
        },
        {
          kind: "steps",
          items: [
            "The recovery engine follows payment → order and reads the order's authoritative gst_rate.",
            "A policy check validates the rate against the allowed GST slabs {5, 12, 18}.",
            "The context includes the source citation and the payment's booked status, so the evidence can be checked again.",
            "Recovery returns a safe candidate with the recovered rate; the judgment agent is not called.",
            "The posting engine computes the split, binds the sale template, and sends it to the ledger.",
          ],
        },
        {
          kind: "code",
          text: `{ "event":     { "amount": 265878, "gst_rate": "", "booked": false },
  "recovered": { "gst_rate": "18",
                 "_source": { "object": "order_rIg…",
                              "path": "notes.gst_rate" },
                 "policy": "gst-rate-from-order" } }`,
        },
        {
          kind: "gst-policy",
        },
        {
          kind: "p",
          text: "The important separation is clear: recovering and validating the rate is deterministic; the posting engine applies it; the judgment agent is reserved for cases where the evidence is not sufficient.",
        },
      ],
    },
    {
      id: "partial-refund",
      eyebrow: "A worked example",
      title: "A partial refund needs review",
      blocks: [
        {
          kind: "p",
          text: "A partial refund is different. The recovery engine can link the refund to its original payment and order, then prepare the amount, line items, and booked status. But a refund amount alone does not safely say which item, tax, fee, or adjustment should be reversed.",
        },
        {
          kind: "steps",
          items: [
            "The posting engine sees a refund with a parent payment and no complete partial-refund rule, so it records a miss.",
            "The recovery engine follows refund → payment → order and prepares the linked order items and amounts.",
            "Partial-refund policy requires review, so recovery returns review required rather than a safe candidate.",
            "The harness gives that prepared evidence to the judgment agent and human reviewer.",
            "The agent recommends the appropriate treatment or escalates the ambiguity. Nothing is posted automatically.",
          ],
        },
        {
          kind: "note",
          text: "GST recovery has one validated missing fact and can return safely to posting. A partial refund may have several valid interpretations, so it remains a review case until a policy or explicit approval resolves it.",
        },
        {
          kind: "partial-refund-policy",
        },
      ],
    },
    {
      id: "scale",
      eyebrow: "When rules stop",
      title: "The agent can investigate, propose, or escalate",
      blocks: [
        {
          kind: "p",
          text: "The same boundary covers partial refunds, RTOs, and reconciliation breaks. The recovery engine prepares the available facts and links. If the evidence supports a safe candidate, the posting engine applies it. Otherwise the agent records a recommendation or escalates. It never posts the recommendation.",
        },
        {
          kind: "code",
          text: `partial refund   refund → order items     policy requires review
RTO               return → courier/order  rate-card-backed or escalate
break             settlement → batch      safe candidate or escalate`,
        },
        {
          kind: "p",
          text: "A recovery engine miss is not permission to guess. The agent can explore further, but it must return a supported recommendation or an escalation. Repeated cases can later become deterministic posting or recovery rules.",
        },
        {
          kind: "note",
          text: "The goal is simple: keep routine accounting deterministic and use the agent only where judgment is still needed.",
        },
      ],
    },
  ],
};
