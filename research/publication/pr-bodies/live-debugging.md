#### Description

Adds real Python local probes, native snapshot/diagnostic forwarding through the Datadog extension and generic HTTP forwarder, and ordinary traces through standard OTLP. Signed remote configuration and UI-managed probe workflows remain blocked.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets `dinesh.gurumurthy/poc-all-products`; [combined integration draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20).

#### Testing

Real Python ddtrace 4.13.0rc1 workloads ran through both kind alternatives and received 202 for captured, trace-correlated snapshots and installed diagnostics. SDK-disabled jobs produced zero uploads. Transport-integrity tests preserve real errors and Retry-After. Backend trace correlation and remote control remain unverified.

**Authenticated readback, 2026-09-20:** Actual snapshots with the exact probe ID and captured locals are stored for both forwarding alternatives. Backend trace correlation, diagnostics readback, signed remote configuration and UI-managed probes remain unverified. [Evidence and limits](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/backend-readback/README.md).

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-live-debugging/research/implementation/live-debugging/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
