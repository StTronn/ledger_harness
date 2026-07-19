# Self-Improvement Layer — Draft

> Status: early architectural draft. This assumes the run and observation prerequisites in `../docs/SELF-IMPROVEMENT-PREREQUISITES.md` already exist.

## The basic idea

Each close run produces a complete evidence package. The self-improvement layer reads completed runs, finds repeated patterns, and proposes a bounded improvement.

It does not write to the ledger or change production behavior directly.

```text
close run
  → recovery observations
  → agent traces
  → human review
  → ledger and reconciliation outcomes
  → learning episode
  → improvement proposal
```

## Data available

### Run context

Identifies the exact execution and everything that influenced it:

- run, world, period, and tenant
- input snapshot
- code, playbook, recovery-policy, prompt, tool, and model versions
- baseline or parent run

### Recovery observations

Explain what happened before the agent was involved:

- why the posting rule missed
- recovery policies evaluated
- facts recovered and their citations
- candidates considered
- source-data failures
- route selected: `safe_to_post`, `review_required`, or `unresolved`

### Agent traces

Explain how the judgment agent handled the unresolved case:

- context supplied to the agent
- tools called and evidence returned
- final recommendation or escalation
- output validation result
- model, prompt, cost, latency, and failure information

The trace stores structured steps and a concise rationale, not private chain-of-thought.

### Human review

Records what the reviewer did with the exact recommendation:

- approve, reject, edit, defer, or escalate
- corrected decision, when edited
- reason and supporting evidence
- reviewer identity and authority
- whether the answer applies only to this case or may generalize

### Outcomes

Records what was actually learned after the decision:

- whether the candidate could be safely bound and posted in shadow
- reconciliation before and after
- human-confirmed result
- fixture truth result, when available offline
- later correction or reversal

These signals remain separate. A balanced ledger or successful reconciliation alone does not prove the accounting decision was correct.

## Learning episode

The improvement layer does not reason over loose files. It receives a joined view of one problem:

```text
trigger
  + recovery evidence
  + agent behavior
  + human response
  + observed outcome
  = learning episode
```

Roughly:

```json
{
  "subject": "refund_123",
  "trigger": "partial_refund_requires_review",
  "recovery": "line item matched; intent remained ambiguous",
  "agent": "recommended refund_reversal",
  "human": "edited to price_adjustment because it was goodwill",
  "outcome": "human confirmed",
  "evidence": ["recovery_42", "agent_18", "review_7"]
}
```

Every summary links back to the canonical run evidence.

## How the layer acts

A deterministic coordinator selects eligible learning episodes and groups similar cases. The meta-agent then:

1. Identifies a repeated pattern.
2. Determines whether the cause is a missing recovery policy, weak agent guidance, missing data, or genuine ambiguity.
3. Checks supporting cases and counterexamples.
4. Produces one bounded proposal, or returns `no_change` when evidence is insufficient.

```text
episodes
  → group repeated cases
  → diagnose the cause
  → check counterexamples
  → propose one change or no change
```

The proposal is handed to a separate executor and evaluator. The meta-agent cannot apply, approve, or deploy it.

## Role of ledger-viz

`ledger-viz` remains a read-only projection. It can show:

- the original close run
- recovery and agent activity
- human review
- the resulting learning episode
- later, the proposal and its evaluation

It is not the evidence store or workflow controller. Its views are generated from canonical run records.

## Initial boundary

The first version should analyze reviewed cases and propose only:

- recovery-policy improvements
- agent-guideline improvements
- data-quality improvements

It should not automatically change posting templates, edit production code, promote proposals, or post ledger entries.
