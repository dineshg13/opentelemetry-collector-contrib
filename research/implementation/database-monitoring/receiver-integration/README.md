# Real PostgreSQL receiver verification

`TestQueryMonitoringLivePostgres` runs the actual PostgreSQL receiver SQL and
snapshot serialization against PostgreSQL 16. It creates and drops one uniquely
named synthetic database. It does not reset shared statistics or change existing
database, Collector, or Kubernetes configuration.

The server must preload `pg_stat_statements`, have its extension installed in the
`postgres` database, and provide the PoC roles `dbm_monitor` (with `pg_monitor`)
and `dbm_app`. The test expects these roles and the `postgres` administrator to
share the password referenced by the configured Kubernetes Secret. Credentials
are passed in process environment, never command arguments or evidence files.

In one terminal, open a loopback forward using the explicit PoC context:

```sh
kubectl --context kind-otel-dd -n ddot-poc port-forward service/dbm-postgres 25432:5432 --address 127.0.0.1
```

In another terminal, run from the repository root:

```sh
python3 research/implementation/database-monitoring/receiver-integration/verify-kind.py
```

Add `--query-plans --output-directory /tmp/dbm-live-plans` to additionally exercise
the real prepared EXPLAIN path and assert that the emitted optional plan is valid
JSON without `ANALYZE` execution fields. This also verifies repeated parameters,
parameter-like text inside a literal, and an UPDATE plan leaving the table unchanged.
`fixtures/otlp-collections-plans.json` and `fixtures/source-evidence-plans.json`
preserve the successful run with plans enabled.

The script reads `ddot-poc/dbm-postgres-password:password` using `kind-otel-dd`.
Its arguments allow explicit endpoint, context, and Secret references. Go cache
and proxy settings are inherited from the environment. Close the port-forward
after testing.

The test verifies:

- Initial and restarted receivers establish empty counter baselines.
- Three controlled executions produce a calls delta of 3 and a rows delta of 15.
- Activity reports one active session and independently counts two idle connections.
- Standard W3C SQL comment context survives obfuscation as structured trace IDs.
- A successful idle observation emits an empty sessions array.
- A row cap reports an error while retaining the other complete family.
- A failed database connection emits no false empty snapshot.

Output is standard OTLP JSON at `/tmp/dbm-live-collections.json` and evidence at
`/tmp/dbm-live-evidence.json`, both mode 0600. The capture contains only the
synthetic database and obfuscated statements, with genuine collection times and
counts. `fixtures/` preserves the successful PostgreSQL 16.15 run from
2026-09-22. The UUID database name in that capture belongs to a database removed
by the successful test cleanup.

The trace context is synthetic standard SQLcommenter context. This test does not
claim SDK instrumentation, Datadog ingestion, or UI verification. The captured
OTLP records can be fed directly to the local intake mapper and DBM decoders.
