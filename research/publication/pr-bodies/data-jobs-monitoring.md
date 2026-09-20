#### Description

Adds native Java/Spark DJM spans over OTLP and managed OpenLineage forwarding, plus a separate Python/OpenLineage example. Both proxy alternatives are implemented. Product joining, live long-running updates, distributed executor coverage and complete backend joining and UI coverage remain incomplete.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets `dinesh.gurumurthy/poc-all-products`; [combined integration draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20).

#### Testing

Actual Spark 4.0.0 with Java agent 1.66.0 preserves native Spark spans, task metrics and SQL-plan attributes through the built Collector. All eight Python/Spark kind Jobs passed across both alternatives and SDK-disabled controls; native lineage intake returned 201. Disabled native Spark instrumentation retained ordinary OTLP traces in local checks.

**Authenticated readback, 2026-09-20:** Both exact Spark runs return job health with duration, CPU, shuffle and stage metrics; the PoC application is in the product catalog. Four queried stage plans lack _dd.spark.sql_plan, and the bounded lineage graph has zero edges. [Evidence and limits](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/backend-readback/README.md).

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-data-jobs-monitoring/research/implementation/data-jobs-monitoring/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
