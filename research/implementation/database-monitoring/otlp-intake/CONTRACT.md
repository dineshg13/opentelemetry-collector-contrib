# Experimental PostgreSQL collection contract, version 1

This is a vendor-neutral receiver contract transported as OTLP logs. These event
names and collection attributes are experimental receiver conventions, not newly
claimed OpenTelemetry semantic conventions. A producer must emit version 1
explicitly; the DBM mapper must not guess this schema from legacy query logs.

## Envelope

Scope name: `github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver`.

Resources contain `service.instance.id` (the monitored database instance, never
the Collector pod), `server.address`, `server.port`, `service.name`, and
`db.system.name=postgresql`. Optional `db.version` is the PostgreSQL version.

Each LogRecord timestamp is collection end. Required record attributes:

| Attribute | OTLP type | Meaning |
| --- | --- | --- |
| `db.collection.schema.version` | string | `1` |
| `db.collection.id` | string | UUID generated once per source collection |
| `db.collection.start_time_unix_nano` | int | Interval start, nanoseconds since Unix epoch |
| `db.collection.end_time_unix_nano` | int | Interval end, nanoseconds since Unix epoch |
| `db.collection.complete` | bool | True only for successful, untruncated collection |
| `db.collection.rows_observed` | int | Number of source rows observed (lower bound if capped) |
| `db.collection.rows_emitted` | int | Number of rows in the body |

End must be later than start. A completed zero-row collection is emitted as an
empty array with complete=true. Failed collections return scrape errors, not
empty successful snapshots. Truncated records are retained for diagnostics but
are not silently converted to complete DBM snapshots. The backend rejects them
from product routing with observable validation outcomes.

Source IDs and timestamps survive OTLP batching and retries. Different
observations of a running query have different collection IDs. A record is a
bounded collection unit; it is never split into multiple activity records.

## Query statistics

Event name: `db.server.query_metrics`.
Body: a map containing `queries`, an array of maps. All counters are interval
deltas, never cumulative snapshots or rates. Each query map contains:

- `db.namespace`, `user.name`, `db.query.text` (obfuscated SQL).
- `postgresql.queryid`, `postgresql.dbid`, `postgresql.userid` as strings;
  `postgresql.toplevel` as a boolean.
- `postgresql.calls`, `postgresql.rows`, `postgresql.shared_blks_dirtied`,
  `postgresql.shared_blks_hit`, `postgresql.shared_blks_read`,
  `postgresql.shared_blks_written`, `postgresql.temp_blks_read`,
  `postgresql.temp_blks_written`: nonnegative integers.
- `postgresql.total_exec_time`, `postgresql.total_plan_time`: nonnegative finite
  seconds. A consumer must not apply legacy query-sample millisecond semantics.
- Optional `postgresql.query_plan`: obfuscated EXPLAIN JSON string.
- Optional `db.query.tables` and `db.query.commands`: arrays of strings.

New or reappearing statements and detectable resets establish baselines before
contributing deltas. Monitoring statistics require `pg_stat_statements` extension
API 1.9 or newer for the global reset timestamp. Reading that epoch before and
after collection detects global resets even when counters have already exceeded
the previous sample; a reset during collection invalidates that collection.
When extension API 1.11 exposes per-row `stats_since`, a changed entry epoch also
invalidates its previous baseline. Counter decreases remain an additional reset
signal. On older extension APIs, a targeted reset of only some statements, or an eviction and re-creation
between scrapes, can be undetectable if every counter overtakes its previous
value before the next scrape.
Server version alone does not establish the installed extension API. See the
[global reset API upgrade](https://github.com/postgres/postgres/blob/REL_14_STABLE/contrib/pg_stat_statements/pg_stat_statements--1.8--1.9.sql)
and [PostgreSQL 17 additions](https://www.postgresql.org/docs/17/release-17.html#RELEASE-17-CONTRIB).
Initial collection can therefore be a successful empty query-metrics snapshot.
Intake derives DBM signatures from the obfuscated query, groups compatible rows
within this collection unit, and converts durations from seconds to milliseconds.
Native PostgreSQL query IDs are never treated as Datadog signatures.

## Activity

Event name: `db.server.activity`.
Body: a map containing `sessions` and `connections`, both arrays of maps.

Sessions use existing receiver row attributes: `db.namespace`, `user.name`,
`db.query.text`, `postgresql.pid`, `postgresql.state`,
`postgresql.application_name`, `postgresql.query_start` (RFC3339 with timezone),
`postgresql.query_id`, `postgresql.wait_event`, `postgresql.wait_event_type`,
`postgresql.blocking.pids`, `network.peer.address`, `network.peer.port`, and
`postgresql.client_hostname`. `postgresql.total_exec_time` is seconds in this
version, even though the legacy per-query sample event used milliseconds.

`postgresql.blocking.pids` is an array of integer PIDs. Optional structured SQL
metadata uses `db.query.tables` and `db.query.commands`. Optional `trace_id` and
`span_id` are lowercase W3C hexadecimal strings (32 and 16 characters), and
`trace_flags` is an integer in [0,255]. Both IDs must be nonzero and valid when
present. They describe the session row, not the enclosing collection record.

Connection summaries have `db.namespace`, `user.name`,
`postgresql.application_name`, `postgresql.state`, and nonnegative integer
`postgresql.connections`. Collect these from the complete visible connection
population, separately from activity-row sampling. Do not infer total connection
counts from the selected activity rows.

The mapper converts only validated context into the existing DBM trace-correlation
metadata. It must not forward arbitrary SQL comments or introduce raw literals.
Application service/environment/version association is optional until a neutral
source for those values is provided and verified; a trace link must still be
validated end to end rather than inferred from matching IDs alone.

## Limits and compatibility

The PoC source emits one bounded record per statistics/activity collection. A
default 1 MiB encoded OTLP limit and configurable row cap protect the pipeline.
The limit is checked after encoding, including envelope overhead. Over-limit
collections report a scrape error or an explicitly incomplete descriptor; they
must never masquerade as a full snapshot. An empty array cannot stand in for a
failed or oversized snapshot.

The intake mapper validates strict types and required fields. Unsupported schema,
invalid identity/context, negative/nonfinite counters, or incomplete snapshots
remain outside the DBM product route with validation telemetry. It does not infer
intervals from request arrival times or maintain previous counters per intake pod.
