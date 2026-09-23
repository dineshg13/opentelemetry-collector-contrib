#### Description

Builds and deploys an OTLP-native proof of concept for all eight Datadog product workstreams, replacing the earlier research-only proposal. Ordinary traces, logs and metrics use standard Collector components. Product HTTP traffic uses either the Datadog extension's policy adapter or an independently built generic HTTP forwarder. The PostgreSQL receiver extracts SQL-comment trace context; native Spark, real WAF, profiling, CI, LLM, DSM and debugger workloads are included.

The generic distribution has no Trace Agent dependencies. The Datadog-extension distribution retains two existing transitive utilities and does not embed the Agent. Both run in kind with real US5 destinations; fake Datadog and mock routing were removed. Existing unrelated workloads/data were preserved.

**Full verification across all eight products remains incomplete.** Authenticated REST/MCP readback now confirms six core paths: LLM, CI, DSM, debugger snapshots, profiling flamegraphs and Spark job health/catalog. AppSec markers/schema are indexed; full security findings and DBM product records remain unverified. Additional correlation, SDK, mapping, signed-control, SQL-plan and lineage gaps are documented. Product drafts remain open and are staged for combined testing, not marked complete or merged.

#### Link to tracking issue

No tracking issue was provided. Product drafts: [Live Debugging](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/11), [LLM Observability](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/12), [Application Security](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/13), [CI Visibility](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/14), [Data Jobs Monitoring](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/15), [Continuous Profiling](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/16), [Database Monitoring](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/17), [Data Streams Monitoring](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/18).

#### Testing

Both actual Collector distributions build and their configurations validate. Meaningful extension and PostgreSQL tests pass. Real Python 4.13.0rc1, Java 1.66.0 and Node 6.16.0 traces/logs/metrics pass local semantic and correlation checks; all three SDK workloads also ran through kind and the real OTLP exporter. Node correlated logs require HTTP/protobuf.

Real kind SDK workloads cover all eight workstreams, including native Spark; observed HTTP intake responses, control responses, SDK opt-out behavior and image identities are recorded separately from product readback. Both alternatives deny all 33 product routes when disabled while ordinary OTLP continues. An independent implementation review checked architecture, source, configurations, tests and cluster state; actionable findings were corrected. Disk exhaustion and recovery are documented rather than omitted.

Authenticated September 20 readback matches all nine SDK trace/log/metric cases, eight LLM workload identities and native evaluation scores, Python CI pass/skip hierarchies, native DSM metrics, debugger snapshots, three SDK hot-loop flamegraphs, and both final Spark job-health results. An independent review audited the exact evidence and remaining gaps. [Backend matrix and query evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/backend-readback/README.md).

#### Documentation

[Implementation, architecture, coverage, alternatives and reproduction](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md), [progress and blockers](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/PROGRESS.md), and [independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/INDEPENDENT_REVIEW.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
