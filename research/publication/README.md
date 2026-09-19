# Published draft PRs

The user explicitly authorized overriding the human-written-description rule for these nine
PRs and publishing the prepared basic summaries in their fork. All nine PRs are open drafts
in `dineshg13/opentelemetry-collector-contrib`. Authorship checkboxes remain unchecked; no
issue/PR comments, readiness changes or PR merges were performed.

The [pr-bodies](pr-bodies) directory records the published descriptions. This authorization
applies to these nine drafts; it is not a general change to [AGENTS.md](../../AGENTS.md).

| Product | Head | Base | Draft PR |
| --- | --- | --- | --- |
| Live Debugging | `dinesh.gurumurthy/poc-live-debugging` | `dinesh.gurumurthy/poc-all-products` | [#11](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/11) |
| LLM Observability | `dinesh.gurumurthy/poc-llm-observability` | `dinesh.gurumurthy/poc-all-products` | [#12](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/12) |
| Application Security | `dinesh.gurumurthy/poc-application-security` | `dinesh.gurumurthy/poc-all-products` | [#13](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/13) |
| CI Visibility | `dinesh.gurumurthy/poc-ci-visibility` | `dinesh.gurumurthy/poc-all-products` | [#14](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/14) |
| Data Jobs Monitoring | `dinesh.gurumurthy/poc-data-jobs-monitoring` | `dinesh.gurumurthy/poc-all-products` | [#15](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/15) |
| Continuous Profiling | `dinesh.gurumurthy/poc-continuous-profiling` | `dinesh.gurumurthy/poc-all-products` | [#16](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/16) |
| Database Monitoring | `dinesh.gurumurthy/poc-database-monitoring` | `dinesh.gurumurthy/poc-all-products` | [#17](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/17) |
| Data Streams Monitoring | `dinesh.gurumurthy/poc-data-streams-monitoring` | `dinesh.gurumurthy/poc-all-products` | [#18](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/18) |
| Integration | `dinesh.gurumurthy/poc-review-snapshot` | `main` | [#19](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/19) |

## Integration while product PRs remain drafts

The complete integration draft uses `dinesh.gurumurthy/poc-review-snapshot` into the fork's
`main`. This preserves useful product-only diffs for the eight draft PRs targeting
`dinesh.gurumurthy/poc-all-products`, which contains the shared research. Merging product
heads into their target now would consume those diffs. This publication arrangement replaces
the earlier plan to merge product PRs before opening the integration draft, accommodating
the user's request to leave all nine PRs in draft state.

No PR targets the OpenTelemetry project. Do not mark ready, merge, or check the human-authorship
box on the user's behalf. Backend/UI validation remains unperformed; local protocol and SDK
results retain the evidence limits in the product reports and independent review.
