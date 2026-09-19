#### Description

Adds research and local prototypes for Database Monitoring correlation through the Collector, with no production changes. Recommends retaining Agent database collection and separately validating SDK trace correlation, including resource mapping and 128-bit trace IDs.

#### Link to tracking issue

None provided.

#### Testing

Exercised five real Python ddtrace 4.13.0rc1 propagation/native-writer cases through the actual Datadog receiver, including forwarding and a 128-bit trace-ID regression; an Agent resource helper test exposed a SQL resource mapping gap. No database-service workload or authenticated Datadog backend/UI validation was performed.

#### Documentation

[Database Monitoring research, configuration proposal, and validation details](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-database-monitoring/research/products/database-monitoring/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
