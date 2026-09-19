#### Description

Adds research and local transport prototypes for Live Debugging through the Collector, with no production changes. Recommends a shared Datadog extension proxy for uploads, together with discovery, remote configuration, and required metrics/trace paths.

#### Link to tracking issue

None provided.

#### Testing

Exercised pinned JavaScript uploader components with explicit stubs and a real Python ddtrace 4.13.0rc1 local probe through the actual HTTP forwarder plus a prototype adapter against local mocks. No authenticated Datadog backend or UI validation was performed.

#### Documentation

[Live Debugging research, configuration proposal, and validation details](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-live-debugging/research/products/live-debugging/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
