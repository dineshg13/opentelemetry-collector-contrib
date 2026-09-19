#### Description

Adds research and a local OpenLineage transport prototype for Data Jobs Monitoring, with no production changes. Recommends an OpenLineage proxy composed with a separately validated native Spark trace pipeline; lineage forwarding alone does not establish complete product support.

#### Link to tracking issue

None provided.

#### Testing

Exercised the real OpenLineage Python 1.45.0 HTTP client through the actual HTTP forwarder against local mocks, covering backend-path success, Agent-prefixed path failure, compressed payloads, and error responses. Native Spark runtime and authenticated Datadog backend/UI validation were not performed.

#### Documentation

[Data Jobs Monitoring research, configuration proposal, and validation details](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-data-jobs-monitoring/research/products/data-jobs-monitoring/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
