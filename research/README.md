# PostgreSQL DBM through OTLP intake

This branch contains the PostgreSQL receiver changes and DBM test assets used by
the current proof of concept. Start with the [implementation and test guide](implementation/database-monitoring/otlp-intake/README.md)
or the [running development stack](implementation/database-monitoring/otlp-intake/dev-stack/README.md).

The receiver emits vendor-neutral OTLP collection records. Datadog DBM mapping
and private-intake publishing live in the separate backend repositories documented
in the guide. The Collector uses the standard OTLP HTTP exporter.

Branch `dinesh.gurumurthy/poc-dbm-only` starts from the fork's `main` commit
`16fa3257d56` and extracts the DBM scope from `f36c956fe80`. Other product work and
shared forwarding changes are excluded. Historical evidence records the original
runs; DBM backend deployment and DBM product readback remain pending.
