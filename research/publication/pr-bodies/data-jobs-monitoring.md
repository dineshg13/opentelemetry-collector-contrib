#### Description

Adds native Java/Spark DJM spans over OTLP and managed OpenLineage forwarding, plus a separate Python/OpenLineage example. Both proxy alternatives are implemented. Product joining, live long-running updates, distributed executor coverage and backend UI/readback remain incomplete.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets the combined implementation branch `dinesh.gurumurthy/poc-all-products`.

#### Testing

Actual Spark 4.0.0 with Java agent 1.66.0 preserves native Spark spans, task metrics and SQL-plan attributes through the built Collector. All eight Python/Spark kind Jobs passed across both alternatives and SDK-disabled controls; native lineage intake returned 201. Disabled native Spark instrumentation retained ordinary OTLP traces in local checks.

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-data-jobs-monitoring/research/implementation/data-jobs-monitoring/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
