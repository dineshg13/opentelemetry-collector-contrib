# Progress checkpoint

Updated: 2026-09-19. Coordinator: `/root`.

Combined branch: `dinesh.gurumurthy/poc-all-products`, worktree `/tmp/ddot-research-all`.
Started from fork default `main` at `16fa3257d56c299e115a558b1a668018a39990d5`.
Original checkout and untracked research inputs remain untouched.

| Product | Agent | Branch suffix | Status | PR |
| --- | --- | --- | --- | --- |
| Live Debugging | `/root/live_debugging` | live-debugging | investigating | pending human sections |
| LLM Observability | `/root/llm_observability` | llm-observability | investigating | pending human sections |
| Application Security | queued | application-security | queued | pending human sections |
| CI Visibility | queued | ci-visibility | queued | pending human sections |
| Data Jobs Monitoring | queued | data-jobs-monitoring | queued | pending human sections |
| Continuous Profiling | queued | continuous-profiling | queued | pending human sections |
| Database Monitoring | queued | database-monitoring | queued | pending human sections |
| Data Streams Monitoring | queued | data-streams-monitoring | queued | pending human sections |

Product branches use `dinesh.gurumurthy/poc-` + suffix. Product worktrees use
`/tmp/ddot-research-` + suffix. Shared worktree: `/tmp/ddot-research-shared`.

## Blockers and constraints

- PR descriptions: user must supply Description, Link to tracking issue, Testing, Documentation
  verbatim for each PR (or explicitly decline sections). Human must check authorship checkbox.
  Prepare deliverables first; do not post generated descriptions/comments or mark ready.
- Backend credentials/access: not yet established. Do not mistake local HTTP tests for product
  success in Datadog. Never record credential values.
- Sandboxed Git metadata is read-only; authorized worktree creation succeeded with escalation.
  Sandboxed GitHub network unavailable; escalated read-only API access succeeded.

## Completed

- Read principles, instructions, AGENTS.md, CONTRIBUTING.md, existing DBM/DSM research.
- Recorded clean source checkouts and exact commits.
- Identified default branch and created isolated combined branch.

## Next

Assign shared components plus two products concurrently; queue remaining six products.
Review product evidence, integrate isolated commits locally, execute focused checks, obtain an
independent integration review, push authorized research branches, then request human PR text.

## Shared ownership and early evidence

`/root/shared_components` owns `research/shared/**`, current extension research, reusable
forwarder harness, and proposed configuration model in its own isolated worktree.

- Current contrib Datadog extension is metadata/status; SDK product proxy is proposed.
- Existing HTTP forwarder selects one origin and preserves incoming path/query.
- Datadog receiver mounts a catch-all `/` returning 200; unrecognized product requests can
  appear successful without being handled. `/info` capability evidence matters.
- Live Debugging needs snapshots/diagnostics/symbols and RC; existing logs disable gate is
  insufficient for unequivocal product disablement.
- Pinned Python LLM code can use APM transport by default; native LLM traffic is not
  universally EVP JSON forwarding. Language defaults must be documented separately.
- Conventional Datadog API/application key environment variables are absent. Backend product
  validation remains blocked pending an approved test organization and credentials.
