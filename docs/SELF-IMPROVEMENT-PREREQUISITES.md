# Self-Improvement Prerequisites

## Purpose

This document defines the changes required in the existing close, recovery, agent, review, scoring, and visualization surfaces before a self-improving layer can be designed safely.

It deliberately does **not** define the meta-agent, improvement-proposal workflow, executor, or promotion architecture. Those are downstream design questions. The prerequisite is a trustworthy evidence contract: every run must be immutable, reproducible, causally attributable, and rich enough to explain what happened.

## Current baseline

The current `main` branch already has useful safety boundaries:

- Every posting, whether produced by a rule or deterministic recovery, goes through the same `ledger.Bind` and `Ledger.Post` path in `internal/ledgerflow/run/core.go`.
- Recovery is read-only and routes rule misses to `safe_to_post`, `review_required`, or `unresolved` in `internal/ledgerflow/recovery/recovery.go`.
- Agent classify and investigate trace v1 records are frozen and versioned.
- Only `internal/score` reads hidden truth and produces the frozen `errors.json` record.
- The playbook is data-driven, with a canonical JSON file and an embedded copy guarded by a test.
- `ledger-viz` keeps posted-ledger playback separate from the review story.

The current evidence is nevertheless incomplete. Runs are stored under `runs/<world>-<period>` and therefore reruns overwrite one another. Successful recovery is counted as ordinary classification rather than durably recorded. Existing traces summarize an agent decision but do not preserve ordered model/tool execution. Human verdicts do not exist on `main`. `BuildRecoveryEngine` calls the full close, which scores and writes artifacts even when a caller only wants read-only context. Finally, `errors.json` is produced from fixture truth and cannot be treated as a production signal.

## 1. Introduce immutable run identity and storage

Replace the mutable `runs/<world>-<period>` output with an immutable package for each execution:

```text
runs/<run_id>/
  manifest.json
  recovery-observations.json
  agent-observations.json
  reviews.json
  outcomes.json
  artifacts/
  blobs/
```

`run_id` identifies an execution, not a business period. World, period, and tenant scope are attributes in the manifest. Running the same period twice must create two distinct run packages; an existing package must never be updated in place.

The writer must use an atomic lifecycle:

1. Create a staging directory for the new `run_id`.
2. Write and validate every record.
3. Compute checksums and finalize the manifest.
4. Atomically publish the directory as complete.
5. Reject a duplicate `run_id` rather than overwrite it.

Incomplete or crashed runs must be visibly marked as incomplete and excluded from normal evidence queries.

The existing `errors.json`, `trace.json`, and `investigate-trace.json` formats must remain readable. During migration they can be emitted under `artifacts/legacy/` or projected from the new records, but their v1 schemas must not be silently widened.

## 2. Add a lineage manifest

`manifest.json` is the causal identity of a run. At minimum it must contain:

- manifest schema version
- `run_id`
- world, period, and tenant/scope identifiers
- run kind: baseline, candidate, replay, shadow, or production
- parent run ID and baseline run ID, when applicable
- immutable input snapshot hash and the hashes of the constituent inputs
- source commit/build hash
- playbook hash and playbook schema/version
- recovery-policy registry hash and individual policy versions
- prompt, instructions, and SKILL hash
- model provider, model identifier, decoding parameters, and agent implementation version
- recorded-response fixture hash when replay mode is used
- declared change-set/executor ID when evaluating a candidate
- runtime environment and tool-version hashes
- evaluation-protocol version
- start/completion timestamps for operations, while keeping deterministic business output independent of wall-clock values
- references and checksums for all records and blobs in the package
- completion status and terminal failure information

For paired comparison, baseline and candidate runs must reference the same input snapshot and evaluation protocol. The manifest must make the declared change set explicit; a comparison with multiple undeclared differences is not causal evidence.

Secrets and raw credentials must never be included. Sensitive source content should be stored through redacted, access-controlled blob references rather than copied into the manifest.

## 3. Persist recovery observations for every rule miss

Recovery currently returns only `Decision{Kind, Candidate, Reason}` to the run loop. Introduce a versioned `RecoveryObservation` and write one for **every** posting-rule miss, including a miss that recovery resolves safely and posts successfully.

Each observation must include:

- observation schema version and stable observation ID
- `run_id`, event ID, event type, and input snapshot reference
- posting rule/rule-set version
- structured miss code plus the human-readable miss reason
- ordered recovery policies evaluated, with policy name and version
- each policy's applicability result and reason
- evidence/source reads attempted, including success, absent data, malformed data, timeout, and permission/transport failure as distinct outcomes
- facts recovered, their typed values, and citations to immutable source object/path/blob hashes
- candidates considered, validation results, and rejection reasons
- selected route: `safe_to_post`, `review_required`, or `unresolved`
- selected candidate, if any, before it is bound
- bind/post validation result
- resulting ledger posting ID and idempotency key, or the downstream recommendation/consultation ID
- duration and deterministic diagnostic counters where useful

Typed fields are authoritative; a concise operator message can be included but must not be the only explanation.

The run counters must distinguish at least:

- direct rule hit
- deterministic recovery followed by post
- agent recommendation
- human-approved application
- unresolved/skipped

This avoids the current loss of provenance where a successful recovery increments `classified` exactly like a direct rule hit.

## 4. Add rich, versioned agent execution observations

Keep the frozen classify `Trace` v1 and `InvestigateTrace` v1 contracts compatible. Do not add fields to those structs without their documented version bump and migration.

Add a separate richer `AgentObservation` contract that captures an execution rather than only its summary. It must support both classify and investigate roles and include:

- observation schema version, observation ID, `run_id`, subject kind, and subject ID
- parent recovery observation or reconciliation-break reference
- exact context bundle reference and hash
- prompt/instruction/SKILL/model/tool-registry hashes from the manifest
- ordered steps: model request, model response, tool call, tool result, retry, validation, and terminal decision
- tool name/version, typed arguments, result reference, status, latency, and error class
- raw model/tool payload references when retention policy permits
- final structured recommendation or escalation
- deterministic output-validator results
- rationale as supporting narrative, not as the sole structured result
- tokens, cost, and latency
- retry/fallback history and terminal failure classification
- links to any derived legacy v1 trace

Large results must be content-addressed under `blobs/` and referenced rather than duplicated. Blob metadata must record content type, checksum, size, redaction state, retention class, and access classification.

Replay and live execution must use the same observation envelope. Replay must additionally identify the exact recorded-response fixture and response record used.

## 5. Add structured human review records

Human input must be durable evidence, not an annotation appended to an agent rationale. Introduce a versioned `ReviewRecord` with:

- review ID, `run_id`, and exact subject type/ID
- hash of the recommendation or candidate that was reviewed
- verdict: `approve`, `reject`, `edit`, `defer`, or `escalate`
- original structured recommendation
- corrected structured decision for an edit
- normalized reason codes plus optional free-form notes
- evidence the reviewer viewed and any evidence they added
- reviewer identity, role, and applicable authority scope
- reviewer confidence
- applicability label: case-only, tenant-specific, or potentially general
- review timestamp
- `supersedes` link for corrections or changed verdicts
- adjudication links when reviewers disagree

Missing review must not be represented as approval. Application paths must fail closed when review is required and no authorized verdict exists.

The review contract should reuse only the useful concepts from `v2-preview`: a swappable reviewer boundary, recorded verdict replay, audit identity/time, and fail-closed behavior. Do not copy its auto-approve default or its approve/reject-only limitation into the canonical design.

## 6. Add shadow application and graded outcomes

An agent recommendation does not change the ledger on `main`; therefore the unchanged run score cannot validate that recommendation. Add a shadow application path that can apply an exact recommendation to an isolated ledger derived from the same input snapshot, then reconcile and evaluate it without affecting the authoritative run.

The shadow path must:

- re-run deterministic validation and citation checks
- bind through canonical templates and post through the normal balance/idempotency checks
- preserve the recommendation-to-posting lineage
- record rejection without partially mutating authoritative state
- recompute reconciliation from the shadow ledger
- emit an outcome even when application fails

`outcomes.json` must contain typed, graded evidence rather than one success flag. Supported evidence grades should include:

- hidden-truth fixture evaluation
- reconciliation result
- human-confirmed correctness
- shadow-ledger result
- invariant and test results
- sampled QA result
- subsequent reversal/correction
- unresolved operational proxy or drift signal

Each `OutcomeRecord` must identify the subject being evaluated, evaluation protocol/version, evidence grade, metrics before and after when paired, failure/rejection reasons, and the source observation/review/posting IDs.

Balance is an invariant, not proof of semantic correctness. Outcomes must keep balance, reconciliation, truth match, and human judgment as separate signals.

## 7. Separate pure execution/context from scoring and persistence

Refactor the current orchestration into explicit phases so read-only callers do not cause scoring or artifact writes:

```text
build snapshot/context
  -> execute deterministic close
  -> reconcile
  -> optionally consult agents/review/apply
  -> optionally evaluate against an allowed evaluator
  -> persist a run package
```

In particular, `BuildRecoveryEngine` must not call `Run` merely to obtain a ledger and breaks, because `Run` currently reads truth through scoring and writes `errors.json`. Provide a pure execution result that can construct the posted ledger, events, raw inputs, recovery graph, and reconciliation breaks without evaluating against truth or writing files.

Artifact persistence should be a dedicated run-recorder boundary. Scoring should return an evaluation record to that boundary. CLI commands may compose the phases, but context exploration and reports must remain read-only projections.

## 8. Establish canonical versioned configuration inputs

The manifest hashes only help if every runtime input is canonical and discoverable.

- Keep `config/playbook.json` as the playbook source of truth.
- Replace the manual copy workflow for `internal/config/playbook.embed.json` with deterministic generation or a build step that cannot publish a stale embed. Retain a byte-equality drift test.
- Generate agent-facing playbook/SKILL material from the same canonical playbook. The generated artifact must carry its source playbook hash.
- Give recovery policies stable IDs and explicit versions; build a deterministic registry whose hash is recorded in the manifest.
- Version posting rules, context-bundle schemas, tool contracts, validators, reconciliation policy, and evaluation protocols.
- Record the actual loaded configuration hashes, not only repository paths or intended versions.

A change to the playbook, generated skill, policy registry, prompt, or evaluator must therefore be visible in lineage even when source code is unchanged.

## 9. Make ledger-viz a projection of canonical evidence

`ledger-viz` must not become the evidence store. Its `LedgerFilm`, `RunFilm`, and RTO models are presentation contracts backed today by committed JSON fixtures.

Add a deterministic projection step that derives visualization models from an immutable run package. It should:

- render posted-ledger state from posting/outcome evidence
- render rule misses, recovery, agent activity, review, and application as separate stages
- never infer that a recommendation was posted
- link visual stages to canonical observation/review/posting IDs
- show baseline and shadow/candidate states only when their lineage proves a valid comparison
- validate generated films deeply enough to reject missing IDs, impossible transitions, and counter drift

Remove or regenerate hand-authored/stale fixture semantics that disagree with current execution behavior. Add golden tests proving that visualization fixture generation from a known run is deterministic and that review-only recommendations do not alter booked counts or score.

Existing user-edited web components are outside this prerequisite work; cleanup belongs in the projection/generator and fixture contracts.

## 10. Preserve production truth isolation

`errors.json` is valid for synthetic fixture evaluation because the scorer has access to hidden `truth/gl.json`. Production does not have that oracle.

- Keep truth readers confined to the evaluator/scorer boundary.
- Label every truth-derived outcome explicitly as offline/fixture evaluation.
- Prevent truth data, expected postings, or truth-derived error details from entering operational recovery context, agent observations, human review context, prompts, or candidate-generation inputs.
- Keep proposal/training evidence disjoint from held-out evaluation periods.
- Add tests that scan dependencies and serialized operational records for truth leakage.
- Allow production outcomes to use reconciliation, review, reversals, sampled QA, and other explicitly graded signals without pretending they are ground truth.

Legacy `errors.json` remains a useful evaluator artifact, not the universal canonical input to future learning.

## 11. Schema, storage, privacy, and query requirements

Every top-level record family must have:

- an independent schema name and integer version
- strict decoding that rejects unknown or invalid required fields
- canonical serialization and deterministic ordering
- stable IDs and explicit cross-record references
- migration readers for supported older versions
- golden fixtures and round-trip tests
- documented compatibility rules and retention policy

Storage must provide:

- append-only publication for completed runs
- content-addressed blobs with checksum verification
- atomic package completion
- an index/catalog that can find runs by run ID, world, period, tenant, lineage, change set, and outcome without rewriting the packages
- tenant and role-based access control before real customer data is admitted
- field/blob redaction and configurable retention
- integrity verification for a complete package
- export/replay without requiring the original live services

The filesystem package is sufficient initially. A generic streaming platform or event bus is not a prerequisite.

## 12. Selective reuse from `v2-preview`

Prior work on `v2-preview` is reference material, not a branch to merge wholesale. Reuse selectively:

- propose/work/apply separation
- keyed deterministic result lookup
- citation revalidation before deriving money
- canonical money derivation in trusted code
- bind/post through the existing ledger boundary
- fail-closed recorded reviewer behavior
- re-reconciliation after isolated application

Do not inherit:

- automatic approval as the default review policy
- mutable world-period artifact paths
- review records keyed only by event ID rather than exact recommendation hash
- approve/reject-only feedback
- coupling of apply directly to truth scoring/writes
- schemas that lack run, configuration, and causal lineage

## Implementation order

1. Define run IDs, manifest schema, immutable package writer, checksums, and no-overwrite tests.
2. Split pure close/context/reconcile execution from scoring and persistence.
3. Add recovery observations and provenance-aware counters.
4. Add agent execution observations while preserving legacy v1 traces.
5. Add structured human reviews and a fail-closed review lookup.
6. Add isolated shadow application and graded outcome records.
7. Canonicalize and hash playbook, generated SKILL, policies, prompts, tools, and evaluation protocols.
8. Generate ledger-viz projections from canonical packages and remove fixture drift.
9. Add truth-leakage, replay, migration, privacy, and integrity gates.

## Prerequisite completion criteria

These prerequisites are complete only when all of the following are true:

- Two executions of the same world and period create distinct immutable run packages and neither overwrites the other.
- A package can be integrity-checked and replayed using only its manifest, referenced snapshots, configuration, recorded responses, and blobs.
- Baseline and candidate runs can prove that they share an input snapshot and identify every declared difference.
- Every posting-rule miss has one recovery observation, including successful deterministic recovery.
- Every agent consultation has an ordered execution observation or an explicit terminal infrastructure-failure observation.
- Frozen v1 trace and `errors.json` consumers continue to work through compatibility output/readers.
- Human verdicts bind to the exact reviewed recommendation and support edit, supersession, disagreement, and fail-closed absence.
- A recommendation can be shadow-applied and independently reconciled/evaluated without changing authoritative ledger state.
- Outcomes clearly distinguish truth, reconciliation, human, invariant, reversal, and proxy evidence.
- Building recovery context or rendering reports performs no truth read and writes no run artifacts.
- The runtime playbook, generated agent guidance, policies, prompts, tools, source build, and evaluator are all hash-addressable from the manifest.
- `ledger-viz` can be regenerated deterministically from canonical run evidence and never presents a recommendation as a posting.
- Automated guards prove that hidden truth cannot enter operational context, traces, reviews, or production evidence.

Only after these gates hold is the system ready for a separate discussion of how a self-improving layer should function.
