#### Description

Adds research and local prototypes for Application Security through the Collector, with no production changes. Finds that the current Datadog receiver drops structured security detection payloads and recommends resolving native trace preservation and processing before claiming product support.

#### Link to tracking issue

None provided.

#### Testing

Captured a real Python ddtrace 4.13.0rc1 Flask WAF event through the actual HTTP forwarder and replayed its exact payload through the actual Datadog receiver; synthetic fixtures also demonstrated structured payload loss. No authenticated Datadog backend or UI validation was performed.

#### Documentation

[Application Security research, configuration proposal, and validation details](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-application-security/research/products/application-security/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
