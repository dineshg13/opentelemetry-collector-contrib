#### Description

Combines research, local prototypes, architecture recommendations, and a proposed configuration model for eight Datadog products through the Collector: Live Debugging, LLM Observability, Application Security, CI Visibility, Data Jobs Monitoring, Continuous Profiling, Database Monitoring, and Data Streams Monitoring. Research only; no production components were changed.

This draft uses `dinesh.gurumurthy/poc-review-snapshot` into this fork's `main` so the complete result can be reviewed while individual product drafts retain their own diffs against `dinesh.gurumurthy/poc-all-products`.

#### Link to tracking issue

None provided. Related product drafts:

- [Live Debugging](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/11)
- [LLM Observability](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/12)
- [Application Security](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/13)
- [CI Visibility](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/14)
- [Data Jobs Monitoring](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/15)
- [Continuous Profiling](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/16)
- [Database Monitoring](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/17)
- [Data Streams Monitoring](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/18)

#### Testing

Independent integration review and local product prototypes completed. Documentation inventory and pinned source-link checks passed for all eight reports; prototype syntax, formatting, and whitespace checks passed. No authenticated Datadog backend or UI validation was performed.

#### Documentation

[Research index, product reports, coverage, implementation plan, and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/tree/dinesh.gurumurthy/poc-review-snapshot/research).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
