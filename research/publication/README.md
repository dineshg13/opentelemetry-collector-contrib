# Publication handoff

Research branches may be pushed and reviewed locally without PR text. Creating each PR needs
human-provided Description, Link to tracking issue, Testing and Documentation sections under
[AGENTS.md](../../AGENTS.md) and [CONTRIBUTING.md](../../CONTRIBUTING.md). These source rules
require verbatim use and prohibit AI-generated issue/PR comments. The human author must check
the authorship box before readiness; the assistant must not check it.

The [pr-bodies](pr-bodies) directory contains **unmodified repository templates**, not drafted
PR descriptions. Supply each section in the corresponding file or in chat. An explicitly
declined section stays unmodified. Absence of a reply is not a decline.

| Branch | Target | Body file | PR |
| --- | --- | --- | --- |
| `dinesh.gurumurthy/poc-live-debugging` | `dinesh.gurumurthy/poc-all-products` | [live-debugging](pr-bodies/live-debugging.md) | awaiting human sections |
| `dinesh.gurumurthy/poc-llm-observability` | `dinesh.gurumurthy/poc-all-products` | [llm-observability](pr-bodies/llm-observability.md) | awaiting human sections |
| `dinesh.gurumurthy/poc-application-security` | `dinesh.gurumurthy/poc-all-products` | [application-security](pr-bodies/application-security.md) | awaiting human sections |
| `dinesh.gurumurthy/poc-ci-visibility` | `dinesh.gurumurthy/poc-all-products` | [ci-visibility](pr-bodies/ci-visibility.md) | awaiting human sections |
| `dinesh.gurumurthy/poc-data-jobs-monitoring` | `dinesh.gurumurthy/poc-all-products` | [data-jobs-monitoring](pr-bodies/data-jobs-monitoring.md) | awaiting human sections |
| `dinesh.gurumurthy/poc-continuous-profiling` | `dinesh.gurumurthy/poc-all-products` | [continuous-profiling](pr-bodies/continuous-profiling.md) | awaiting human sections |
| `dinesh.gurumurthy/poc-database-monitoring` | `dinesh.gurumurthy/poc-all-products` | [database-monitoring](pr-bodies/database-monitoring.md) | awaiting human sections |
| `dinesh.gurumurthy/poc-data-streams-monitoring` | `dinesh.gurumurthy/poc-all-products` | [data-streams-monitoring](pr-bodies/data-streams-monitoring.md) | awaiting human sections |
| `dinesh.gurumurthy/poc-all-products` | `main` | [all-products](pr-bodies/all-products.md) | awaiting human sections |

## Integration while PR text is pending

A separate review snapshot may assemble completed product branches for local testing and
independent review while the combined branch retains a useful base for product PRs. Such a
snapshot is not a GitHub PR merge and must not be reported as one. PR creation, readiness and
merge remain pending human sections, human attestation, and applicable checks.

The final combined PR must remain draft/open. Do not merge this work to `main` on the user's
behalf. Do not consume product branch commits into the combined branch before their PRs exist;
that would erase the product PR diff and require reconstructing the requested workflow.
