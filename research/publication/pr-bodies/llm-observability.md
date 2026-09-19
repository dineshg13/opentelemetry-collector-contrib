#### Description

Adds research and local transport prototypes for LLM Observability through the Collector, with no production changes. Recommends shared native event proxy routes and separate trace compatibility work for Python LLM payloads carried in native traces; documents the distinct OTLP option.

#### Link to tracking issue

None provided.

#### Testing

Exercised pinned JavaScript writer components with explicit stubs and real Python ddtrace 4.13.0rc1 span/evaluation uploads through the actual HTTP forwarder against local mocks, including missing path rewrites. No authenticated Datadog backend or UI validation was performed.

#### Documentation

[LLM Observability research, configuration proposal, and validation details](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-llm-observability/research/products/llm-observability/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
