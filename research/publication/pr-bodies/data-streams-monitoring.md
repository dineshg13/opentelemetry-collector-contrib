#### Description

Adds research and local prototypes for Data Streams Monitoring through the Collector, with no production changes. Recommends an SDK statistics proxy with discovery and metadata support, while retaining Agent broker collection and handling trace correlation separately.

#### Link to tracking issue

None provided.

#### Testing

Exercised real Python ddtrace 4.13.0rc1 and Java agent 1.66.0 pathway statistics through the actual HTTP forwarder, a prototype path adapter where needed, and local mocks, covering path rewriting, compressed payloads, errors, disablement, and Java discovery gating. No authenticated Datadog backend or UI validation was performed.

#### Documentation

[Data Streams Monitoring research, configuration proposal, and validation details](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-data-streams-monitoring/research/products/data-streams-monitoring/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
