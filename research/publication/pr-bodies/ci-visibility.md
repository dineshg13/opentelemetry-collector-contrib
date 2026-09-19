#### Description

Adds research and local prototypes for CI Visibility / Test Optimization through the Collector, with no production changes. Recommends shared native proxy routes with discovery and bidirectional settings/API support, alongside the separate legacy trace path.

#### Link to tracking issue

None provided.

#### Testing

Exercised real Python ddtrace 4.13.0rc1 pytest and Java agent 1.66.0 manual CI events through the actual HTTP forwarder plus a prototype adapter, and pinned JavaScript discovery/writer components with explicit stubs. Local mocks covered event payloads, settings, Git metadata, failures, and route disablement; no authenticated Datadog backend or UI validation was performed.

#### Documentation

[CI Visibility research, configuration proposal, and validation details](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-ci-visibility/research/products/ci-visibility/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
