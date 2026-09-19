# Implementation checkpoint

Active request: [new-instr.md](../new-instr.md). The coordinator owns the combined
Collector builds, shared deployment, integration checks, index and final draft from
`dinesh.gurumurthy/poc-all-products` into the user's fork `main`.

**All products remain incomplete** under the required backend-product completion bar.
Real intake acknowledgements and local semantics are separated from authenticated
product UI/readback, which is blocked by missing account/application-key access.
Some SDK and control gaps also need implementation beyond credentials.

| Product | Dedicated implementation assignment | Executed result and remaining boundary | Draft |
| --- | --- | --- | --- |
| Live Debugging | `implement_dbm`, subsequent dedicated assignment | Python probes, trace-correlated snapshots and diagnostics through both kind alternatives; SDK disable passes. Signed RC and UI-created probes remain blocked. | [11](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/11) |
| LLM Observability | `implement_llm` | All three SDKs emit GenAI OTLP spans; kind workloads run; Python native spans/evaluations receive 202 through both alternatives. Product conversion/readback unverified. | [12](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/12) |
| Application Security | `implement_appsec` | Python/Node real WAF events survive OTLP; kind workloads run. Java structured encoding gap reproduced; full security product parity and UI/RC unverified. | [13](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/13) |
| CI Visibility | `implement_appsec`, subsequent dedicated assignment | Real pytest lifecycle, controls and separate OTLP traces; both kind Jobs complete with real 200/202 responses. Python private flag and Java writer-selection gaps remain. | [14](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/14) |
| Data Jobs Monitoring | `implement_llm`, subsequent dedicated assignment | Eight Python/OpenLineage and native Java/Spark kind Jobs complete across both alternatives and disabled controls. Real lineage intake returns 201; joining, long-running updates and UI unverified. | [15](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/15) |
| Continuous Profiling | `implement_continuous_profiling` | Python/Java/Node profiles and OTLP traces run through both kind alternatives; real 202 is translated to SDK 200. SDK disable passes; backend flamegraph/correlation unverified. | [16](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/16) |
| Database Monitoring | `implement_dbm` | Receiver tests pass; real PostgreSQL yields 11 exact SQL/query-log/OTLP-span matches in the earlier detailed window. Disabled queries contain no context. DBM event mapping/readback remains blocked. | [17](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/17) |
| Data Streams Monitoring | `implement_llm`, subsequent dedicated assignment | Three SDK checkpoint/OTLP/disable tests and kind Jobs pass; both intakes return 202. Backend topology and broker/schema/action coverage remain incomplete. | [18](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/18) |

Shared owner `implement_shared` implemented the reusable HTTP forwarder and Datadog policy
adapter. The coordinator fixed two configuration-decoding regressions found with the real
service; both full extension suites pass. Product threads were reused sequentially within
the four-agent limit, with isolated worktrees and dedicated file ownership per assignment.

Cross-SDK owner `implement_appsec` verified all nine trace/log/metric combinations through
the actual Collector, including matching log trace/span IDs and counter value 3. Log/metric
disable checks retain ordinary traces. All three SDKs also ran through kind and the real
US5 exporter with positive bounded counters and no new failure/refusal counters. Node
6.16 correlated logs require HTTP/protobuf; its JSON encoding failure is recorded.

## Shared deployment and controls

- Both actual distributions are built and deployed: `ddot-poc/collector` and
  `collector-http-forwarder`. The latter's independently linked binary contains zero
  Trace Agent dependencies; the former retains two existing utility dependencies.
- Ordinary signals use standard OTLP pipelines. PostgreSQL collection is explicitly
  configured. No Datadog receiver/exporter/connector or embedded Trace Agent is built.
- All six HTTP product flags are enabled together; AppSec and DBM remain SDK/pipeline owned.
- Both variants returned 404 for all 33 disabled product routes, forwarded no product
  requests, and accepted an ordinary OTLP trace with LLM conversion explicitly disabled.
  Temporary verification resources were removed.
- Real US5 credentials are referenced through Kubernetes Secrets, never committed.
- Fake Datadog Deployment/Service and the unused mock route were removed. Existing
  applications rolled out; unrelated workloads and persistent database data were preserved.
- Shared logging is basic and bounded. Route/host/status diagnostics are unsampled;
  forwarding never logs payloads, headers, query strings or credentials. Exact fresh
  full-record readback is not claimed from aggregate counters.

## Incident, review and publication

Disk exhaustion during duplicate image imports and an initial Spark build caused transient
PostgreSQL recovery, JFR exits and truncated logs. [Recovery evidence](evidence/disk-recovery.json)
records scoped cleanup and preserved artifacts. Images now load only on the worker; Java
profiling uses bounded memory-backed temporary storage; Spark exports to a Docker archive.
Fresh CI/DSM runs and a later observation window replace missing earlier outcome counts.

Independent reviewer `implementation_review` checked architecture, 17 complete Collector
configs, parser tests and bounded cluster readiness. DB image bootstrap, shared logging
instructions, CI alternate routing and Spark archive instructions were corrected. The
[review report](INDEPENDENT_REVIEW.md) records all five findings as resolved. All 17
active Deployments were Ready in the final bounded check; both Collectors had zero errors
in the last 120 seconds. An earlier planned database rollout scrape error remains in the
longer observation window and is not represented as a steady-state failure.

Product changes are staged into the combined branch for joint testing. Blocked product
PRs stay drafts; none is represented as complete or merged. The obsolete research-only
`poc-review-snapshot` integration draft #19 is closed without merge and replaced by
[final draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20)
from the required `poc-all-products` head. All PR targets are in `dineshg13/opentelemetry-collector-contrib`; human-authorship
checkboxes stay unchecked, and no issue/PR discussion comments or default-branch merges
are performed.

Both clean-source builds at `f34e6a129cb` succeeded and exactly match the executed
binary hashes; [provenance](evidence/build-reproducibility.json) links them to the original
image IDs. Independent review and the final bounded readiness check are complete.
All eight product drafts and the corrected integration draft are published and verified
in the fork; [publication evidence](../publication/verification.json) records their state.
Remaining work: obtain read-scoped Datadog account/application-key access and verify each
product UI/readback, then address the SDK/control/mapping gaps listed in each product README.
No product is marked complete merely because transport and local checks pass.
