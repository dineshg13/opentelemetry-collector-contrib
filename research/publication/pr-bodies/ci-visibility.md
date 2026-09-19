#### Description

Adds real pytest lifecycle, coverage and synchronous control forwarding, while ordinary application traces use OTLP. The Python path requires an existing private context-provider flag; Java OTLP writer selection bypasses native CI envelopes. These SDK limitations and backend optimization/UI verification remain open.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets `dinesh.gurumurthy/poc-all-products`; [combined integration draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20).

#### Testing

Actual Python SDK/Collector tests cover test hierarchy, pass/skip, coverage, settings/git controls, 429/Retry-After, SDK disable and proxy disable. Corrected kind Jobs ran through both alternatives with separate OTLP endpoints; real settings returned 200 and CI intake returned 202. Java 1.66.0 writer-selection limitation was reproduced.

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-ci-visibility/research/implementation/ci-visibility/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
