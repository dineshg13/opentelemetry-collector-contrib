#### Description

Extends the existing PostgreSQL receiver to extract W3C traceparent from SQL comments before obfuscation, preserving application_name precedence. Adds an isolated real database and Datadog Python/psycopg OTLP workload. A supported Datadog DBM event mapping and authenticated product readback remain blocked.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets `dinesh.gurumurthy/poc-all-products`; [combined integration draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20).

#### Testing

The receiver suite and meaningful SQL parser edge cases pass. Kind produced 11 exact matches among SDK SQL comments, receiver query-log IDs and OTLP span IDs in an earlier controlled detailed-log window. SDK-disabled queries contained zero contexts. Current shared basic logging cannot reproduce exact record readback; the historical limitation is explicit.

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-database-monitoring/research/implementation/database-monitoring/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
