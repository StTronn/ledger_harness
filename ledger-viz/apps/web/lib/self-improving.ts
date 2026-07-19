import type { Section } from "@/lib/explainer";

export const selfImprovingArticle: {
  title: string;
  lede: string;
  sections: Section[];
} = {
  title: "Self-improving harness",
  lede: "Completed close runs become structured evidence. Repeated, supported patterns become bounded proposals—without giving the learning agent permission to change the books or production system.",

  sections: [
    {
      id: "evidence",
      eyebrow: "The evidence",
      title: "One close becomes a learning episode",
      blocks: [
        {
          kind: "p",
          text: "Every completed close leaves behind more than a final ledger. Recovery records what the deterministic system tried, the judgment agent records how it used the prepared evidence, the reviewer records the accepted or corrected answer, and outcomes show what was actually supported. The harness joins these records into one traceable learning episode.",
        },
        {
          kind: "self-improvement-evidence",
        },
        {
          kind: "note",
          text: "The learning episode is a joined view, not a new source of truth. Every summary points back to the immutable recovery, agent, review, and outcome records from the run.",
        },
      ],
    },
    {
      id: "example",
      eyebrow: "A worked example",
      title: "A reviewer corrects a partial-refund recommendation",
      blocks: [
        {
          kind: "p",
          text: "A partial refund reaches the judgment agent because the recovery evidence cannot prove whether it is a product return or a goodwill credit. The agent recommends a refund reversal. The reviewer sees an authoritative goodwill annotation and edits the answer to a price adjustment.",
        },
        {
          kind: "steps",
          items: [
            "Recovery records the rule miss, the line-item match, the cited GST rate, and review_required.",
            "The agent trace records the supplied context, tool path, and refund_reversal recommendation.",
            "The human review binds to that exact recommendation and edits it to price_adjustment with reason goodwill_credit.",
            "A shadow outcome confirms that the corrected decision binds, balances, and clears the expected receivable.",
            "The episode keeps the full chain together so later analysis can compare it with similar reviewed refunds.",
          ],
        },
        {
          kind: "code",
          text: `{ "trigger":  "partial_refund_requires_review",
  "recovery": "line item matched; intent still ambiguous",
  "agent":    "recommended refund_reversal",
  "human":    "edited to price_adjustment: goodwill_credit",
  "outcome":  "shadow validated; human confirmed",
  "evidence": ["recovery_42", "agent_18", "review_7"] }`,
        },
      ],
    },
    {
      id: "learning",
      eyebrow: "Across runs",
      title: "Repeated evidence becomes a proposal",
      blocks: [
        {
          kind: "p",
          text: "A deterministic coordinator groups comparable episodes. The meta-agent studies the repeated behavior, checks contradictions, and decides whether the cause is a missing recovery policy, weak agent guidance, poor source data, or genuine ambiguity that should remain with a human.",
        },
        {
          kind: "code",
          text: `pattern: goodwill partial refunds
support: 31 reviewed episodes across 4 periods
signal: 24 agent recommendations edited to price_adjustment
authority: refund.notes.reason = "goodwill"
counterexamples: 0 confirmed cases with the same annotation`,
        },
        {
          kind: "self-improvement-flow",
        },
        {
          kind: "note",
          text: "The learning agent proposes; it does not post, edit production, approve its own work, or deploy. A separate executor and evaluator handle an accepted proposal through the same propose → validate → review boundary used by the accounting harness.",
        },
      ],
    },
    {
      id: "optimization",
      eyebrow: "A later extension",
      title: "GEPA and other optimization paths",
      blocks: [
        {
          kind: "p",
          text: "GEPA is useful for a different part of the loop: improving the judgment agent's natural-language instructions. It reflects on execution traces, failures, and textual feedback, proposes prompt variants, and tests them while retaining candidates that work well on different cases.",
        },
        {
          kind: "code",
          text: `deterministic learning   repeated evidence → recovery policy
GEPA                     traces + feedback → improved instructions
few-shot optimization    approved examples → better demonstrations
fine-tuning              larger curated dataset → adapted model weights`,
        },
        {
          kind: "p",
          text: "For this harness, GEPA could use agent traces, human edits, validator failures, and outcome feedback to improve classify or investigate guidance. Few-shot optimizers could select strong reviewed examples, while fine-tuning may become useful only after enough high-quality data exists.",
        },
        {
          kind: "steps",
          items: [
            "Choose a narrow agent behavior to improve and freeze the evaluation rules first.",
            "Keep the learning episodes used for optimization separate from held-out evaluation runs.",
            "Compare candidates on correctness, escalation behavior, evidence use, cost, and latency—not one score alone.",
            "Send the winning candidate through the same independent review and promotion gate as any other change.",
          ],
        },
        {
          kind: "note",
          text: "Prompt optimization improves how the agent handles judgment; it does not replace deterministic recovery, accounting policy, or human authority. This is a rough future option, not a prerequisite for the first self-improving loop.",
        },
      ],
    },
  ],
};
