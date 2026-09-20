#### Description

Adds actual Python, Java and Node linked checkpoints, opaque DSM statistics through both forwarding alternatives, and ordinary OTLP traces. Backend topology, broker lag, schema/action collection and product UI/readback remain incomplete.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets `dinesh.gurumurthy/poc-all-products`; [combined integration draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20).

#### Testing

All three SDKs passed enabled/disabled checkpoint tests and actual Collector semantics. Kind Jobs completed for both alternatives, and fresh bounded logs show real US5 DSM intake 202 on each. Disabled proxy routes return 404 while ordinary OTLP traces continue; Java empty discovery probes are distinguished from native trace data.

**Authenticated readback, 2026-09-20:** Native DSM latency metrics are stored for all three SDKs, with Python/Node payload-size series and synthetic consumer lag. Separate proxy attribution, Java payload sizes, broker integration and UI topology remain unverified. [Evidence and limits](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/backend-readback/README.md).

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-data-streams-monitoring/research/implementation/data-streams-monitoring/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
