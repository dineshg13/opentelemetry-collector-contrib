#### Description

Adds real Python, Java and Node profiling workloads with ordinary traces over OTLP, plus independently built Datadog-extension and generic-forwarder paths. Profile payloads stay opaque; backend 202 is mapped to SDK 200. Datadog flamegraph visibility and trace correlation remain unverified.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets the combined implementation branch `dinesh.gurumurthy/poc-all-products`.

#### Testing

All three released SDKs generated real profiles and OTLP traces in local semantic tests and both kind alternatives. Real US5 profile intake returned 202. SDK disable and both proxy gates passed. Java JFR temporary storage and memory bounds were corrected after a documented disk incident.

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-continuous-profiling/research/implementation/continuous-profiling/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
