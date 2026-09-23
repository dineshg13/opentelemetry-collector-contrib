# Draft PR publication

All publication targets the user's fork `dineshg13/opentelemetry-collector-contrib`.
The user authorized assistant-written basic summaries for these eight product drafts
and the final integration draft. Human-authorship checkboxes remain unchecked.
No issue/PR discussion comments or default-branch merges are part of this work.

| Product | Draft PR | Target |
| --- | --- | --- |
| Live Debugging | [11](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/11) | poc-all-products |
| LLM Observability | [12](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/12) | poc-all-products |
| Application Security | [13](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/13) | poc-all-products |
| CI Visibility | [14](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/14) | poc-all-products |
| Data Jobs Monitoring | [15](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/15) | poc-all-products |
| Continuous Profiling | [16](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/16) | poc-all-products |
| Database Monitoring | [17](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/17) | poc-all-products |
| Data Streams Monitoring | [18](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/18) | poc-all-products |

**[Final integration draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20)**
is open from `dinesh.gurumurthy/poc-all-products` to fork `main`. The obsolete
research-only [PR19](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/19),
whose head was `poc-review-snapshot`, is closed without merge; its branch is preserved.
[Publication verification](verification.json) records exact targets, prepared-body matches,
unchecked authorship boxes and unchanged `main`. [Machine-readable links](pull-requests.json)
include all eight products and the replacement final draft.

Full eight-product verification remains incomplete. The [latest authenticated readback](../implementation/backend-readback/README.md) verifies six core product paths and records all remaining gaps.
Their implementation content is staged into the combined branch for joint testing;
blocked product drafts are not marked complete or merged. See the
[implementation checkpoint](../implementation/PROGRESS.md),
[independent review](../implementation/INDEPENDENT_REVIEW.md), and
[exact prepared bodies](pr-bodies).
