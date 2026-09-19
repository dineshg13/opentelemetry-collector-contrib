#### Description

Adds research and local transport prototypes for Continuous Profiling through the Collector, with no production changes. Recommends a native profile proxy and documents a narrow configuration-only path using Java's exact profiling URL override.

#### Link to tracking issue

None provided.

#### Testing

Exercised real Python ddtrace 4.13.0rc1 and Java agent 1.66.0 profile uploads through the actual HTTP forwarder against local mocks: default Agent paths failed and Java's exact backend-path override succeeded. No authenticated Datadog backend or UI validation was performed.

#### Documentation

[Continuous Profiling research, configuration proposal, and validation details](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-continuous-profiling/research/products/continuous-profiling/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
