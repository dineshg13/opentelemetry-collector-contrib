# Implementation checkpoint

Active request: [new-instr.md](../new-instr.md). Earlier research-only harnesses are
historical references, not product completion evidence. Final work is on
`dinesh.gurumurthy/poc-all-products`; product PRs target it in the user's fork.

## Milestone: actual distribution and SDK workloads built

- Coordinator: combined Collector, configurations, deployment and final PR.
- Shared owner `implement_shared`: reusable HTTP forwarding and Datadog policy adapter implemented; both component suites passed. Coordinator is correcting configuration decoding problems found with the actual built service.
- Standard OTLP receiver/exporter carry ordinary traces/logs/metrics. No Datadog receiver, exporter, connector or embedded Trace Agent in the distribution.
- Actual OCB distribution and image built; PostgreSQL change being rebuilt into it.
- Context `kind-otel-dd` checked before mutations. Removed `observability/fake-datadog` Deployment/Service and the unused mock routing ConfigMap. Updated SDK injection and rolled the three existing apps successfully; unrelated database data preserved.
- Real US5 API key validated successfully. It is referenced through Kubernetes Secrets and never committed. Product readback requires account access or a read-scoped application key, currently unavailable.

| Product | Dedicated owner | Implementation / verification status | Draft PR |
| --- | --- | --- | --- |
| Live Debugging | queued | control/backend workflow incomplete | [11](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/11) |
| LLM Observability | implement_llm | Python/Java/Node actual OTLP SDK tests pass; Python native span/evaluation fixture passes; images built; kind pending | [12](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/12) |
| Application Security | implement_appsec | Python and Node real WAF detections preserved through OTLP; Java structured metadata SDK export gap reproduced; kind pending | [13](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/13) |
| CI Visibility | queued | dedicated workload/control verification pending | [14](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/14) |
| Data Jobs Monitoring | queued | dedicated workload/backend verification pending | [15](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/15) |
| Continuous Profiling | implement_continuous_profiling | real Python/Java/Node profiles and OTLP traces, SDK disable tests pass; all images built; kind pending | [16](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/16) |
| Database Monitoring | implement_dbm | PostgreSQL SQL-comment context extraction and receiver suite pass; isolated real database/application Ready; combined Collector correlation pending | [17](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/17) |
| Data Streams Monitoring | implement_llm (dedicated subsequent assignment) | all three SDK checkpoint/OTLP/disable tests pass; three images built; proxy and kind checks ongoing | [18](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/18) |

All products remain incomplete until real backend product behavior is evidenced.
A successful intake response will be reported separately from product visibility.
Product changes are staged into the combined branch for joint testing; blocked
product PRs remain drafts rather than being marked complete or merged.

Next: deploy both independently configured Collector variants, generate all
implemented workloads, record real upstream outcomes and disabled behavior,
complete queued product implementations, then independent architecture review.
The final draft must use `poc-all-products` as its head, replacing the earlier
research-only review-snapshot PR.
