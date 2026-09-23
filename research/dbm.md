# DBM + APM — Low-Level Implementation

> Part 2 of 3. Assumes [[01-mental-map]]. Every agent-repo claim below carries a
> `file:line` reference verified against the source at
> `~/go/src/github.com/DataDog/datadog-agent`. Tracer-side sqlcommenter details
> (§7) are **spec/docs-sourced** — the `apm-libraries-dcs-mcp` server was
> unavailable this session, so they were not re-verified against tracer source.

---

## 1. Collection side — Postgres 15 walkthrough

The DBM check is a client of the database. Setup (from the Postgres 15 self-hosted
guide) exists to give that client read access to the internal stats views, and
each setup step unlocks one of the four data products.

```
postgresql.conf                          → unlocks
──────────────────────────────────────────────────────────────────────
shared_preload_libraries=pg_stat_statements → QUERY METRICS
    (aggregated per-normalized-query counters: calls, total_time, rows,
     shared_blks_*, wal_*). The check diffs successive snapshots.
track_activity_query_size=4096               → longer query text in ACTIVITY
    (default 1024 truncates; DBM wants the full statement)
track_io_timing=on (optional)                → block read/write timing in metrics

SQL grants (per database)                → unlocks
──────────────────────────────────────────────────────────────────────
CREATE USER datadog ...; GRANT pg_monitor  → read pg_stat_* views  → ACTIVITY,
    ALTER ROLE datadog INHERIT                METRICS, database state
CREATE EXTENSION pg_stat_statements          → QUERY METRICS source
datadog.explain_statement(...) SECURITY      → EXPLAIN PLANS (attached to
    DEFINER function                            query SAMPLES). Runs
                                                EXPLAIN (FORMAT JSON) via a
                                                read-only cursor as a definer
                                                role, so the low-priv datadog
                                                user can get plans safely.
datadog.column_statistics() (optional)      → COLUMN STATISTICS
    + collect_column_statistics.enabled        (dbm-column-statistics events)
```

Agent config — `conf.d/postgres.d/conf.yaml`:

```yaml
init_config:
instances:
  - dbm: true # THE switch: turns a plain PG integration into a DBM one
    host: localhost # must be the DB host directly …
    port: 5432
    username: datadog
    password: "ENC[...]" # secret backend reference
    # dbname: '<DB>'          # optional (for custom_queries)
```

Hard rule from the docs: the Agent **must connect directly to the monitored host**
(`127.0.0.1` or socket), never through pgbouncer / a load balancer / a proxy —
because failover or connection multiplexing would attribute stats to the wrong
host and corrupt the per-host metrics.

Where `dbm: true` leads in the agent: Postgres/MySQL/SQLServer/Mongo checks are
**Python** (they live in the separate `integrations-core` repo). Oracle is the one
**Go** DBM check in this repo, at
`pkg/collector/corechecks/oracle/` — a faithful reference for what every DBM check
does: run stats queries, obfuscate SQL with `pkg/obfuscate`, and emit `dbm-*`
events via `sender.EventPlatformEvent`
(`oracle/statements.go:635,820` → `dbm-samples`; `:873` + `oracle/locks.go:163`
→ `dbm-metrics`; `oracle/activity.go:93` → `dbm-activity`;
`oracle/metadata.go:68` → `dbm-metadata`).

AWS RDS/Aurora **autodiscovery** (find instances to monitor, not emit payloads) is
Go too: `pkg/databasemonitoring/aws/{aurora,rds,config,aws}.go`, driven by
`database_monitoring.autodiscovery.aurora.*` config keys.

---

## 2. Agent transport pipeline (DBM events)

A DBM event is produced in a check and shipped as an opaque blob. The full path,
from Python check to intake:

```
 Python check (integrations-core)
   aggregator.submit_event_platform_event(raw_json, "dbm-metrics")
        │  (rtloader CGO bridge; callback registered in
        │   pkg/collector/python/init.go:133-139, C signature in
        │   rtloader/include/rtloader_types.h:103)
        ▼
 //export SubmitEventPlatformEvent
   pkg/collector/aggregator/aggregator.go:168-182
   → sender.EventPlatformEvent(C.GoBytes(raw), C.GoString(eventType))
        │
        ▼
 checkSender.EventPlatformEvent(rawEvent []byte, eventType string)
   pkg/aggregator/sender.go:428-437
   → pushes senderEventPlatformEvent{id,rawEvent,eventType} onto eventPlatformOut
   → metricStats.EventPlatformEvents[eventType]++       (throughput accounting)
        │
        ▼
 BufferedAggregator.handleEventPlatformEvent
   pkg/aggregator/aggregator.go:490-498
   → message.NewMessage(rawEvent, nil, "", 0)
   → forwarder.SendEventPlatformEvent(m, eventType)
        │  (expvar counters aggregatorEventPlatformEvents / …Errors track this)
        ▼
 defaultEventPlatformForwarder.SendEventPlatformEvent
   comp/forwarder/eventplatform/impl/epforwarder.go:98
   → pipelines[eventType].in <- message   (non-blocking; error if full)
        │
        ▼
 passthroughPipeline (newHTTPPassthroughPipeline, epforwarder.go:285)
   → logs-library HTTP batch/stream strategy, NO log processing
   → config.BuildHTTPEndpointsWithCompressionOverride(intakeTrackType)
        │
        ▼
 POST https://dbm-metrics-intake.<site>/api/v2/<intakeTrackType>   (gzip JSON)
```

Two things worth internalizing:

1. **Event type is a routing string, nothing more.** The forwarder holds a
   `map[string]*passthroughPipeline` keyed by that string
   (`epforwarder.go:90`). No schema, no validation — the check and the backend own
   the schema.
2. **DBM does not use `pkg/serializer`.** That serialization path is for
   metrics/events/service-checks. DBM (like NDM, netflow, CWS) rides the _logs
   library's_ HTTP transport via the Event Platform Forwarder. This is why DBM
   payloads are free-form JSON, not the agent's metric protobufs.

---

## 3. The six DBM event types → intake tracks

Verified verbatim in `comp/forwarder/eventplatform/impl/pipelines_dbm.go`
(constants at `:13-20`, pipeline descriptors in `getDBMPipelines()` `:22-114`):

| event type              | `intakeTrackType`     | endpoints config prefix          | host prefix           | maxContentSize |
| ----------------------- | --------------------- | -------------------------------- | --------------------- | -------------- |
| `dbm-samples`           | `databasequery`       | `database_monitoring.samples.`   | `dbm-metrics-intake.` | 10 MB          |
| `dbm-metrics`           | `dbmmetrics`          | `database_monitoring.metrics.`   | `dbm-metrics-intake.` | 20 MB          |
| `dbm-activity`          | `dbmactivity`         | `database_monitoring.activity.`  | `dbm-metrics-intake.` | 20 MB          |
| `dbm-metadata`          | `dbmmetadata`         | `database_monitoring.metrics.`\* | `dbm-metrics-intake.` | 20 MB          |
| `dbm-health`            | `dbmhealth`           | `database_monitoring.metrics.`\* | `dbm-metrics-intake.` | 20 MB          |
| `dbm-column-statistics` | `dbmcolumnstatistics` | `database_monitoring.metrics.`\* | `dbm-metrics-intake.` | 20 MB          |

\* Metadata / health / column-statistics deliberately reuse the `.metrics.` config
prefix — a source comment (`pipelines_dbm.go:55-58`) notes they hit the same
intake, so no separate config endpoint is needed. The `intakeTrackType` is still
distinct per product and becomes the v2 intake URL path segment.

Tuning for DBM's high event rate: every DBM pipeline sets
`defaultBatchMaxConcurrentSend: 10` and `defaultInputChanSize: 500`
(comments: "handle 4k events/s"). Samples cap at 10 MB, all others at 20 MB.

Config keys bound in `pkg/config/setup/common_settings.go:1859-1868`
(`bindEnvAndSetLogsConfigKeys` for `.samples.`/`.activity.`/`.metrics.` plus
aurora autodiscovery). **FIPS mode** (`pkg/config/setup/config.go:927-932`) reroutes
DBM through a local FIPS proxy: metrics+activity → proxy id 6, samples → proxy id 7
(two ports kept for backward-compat because samples historically used a different
intake host).

---

## 4. Representative payload shapes

The Agent ships these verbatim; shapes below are representative of the DBM JSON
contract (illustrative field selection, not an exhaustive schema).

**`dbm-metrics`** — aggregated per-normalized-query counters over an interval:

```json
{
  "host": "db-prod-1",
  "timestamp": 1720440000000,
  "min_collection_interval": 10,
  "tags": ["db:orders", "env:prod"],
  "postgres_version": "15.4",
  "kind": "postgres_queries",
  "sequence_number": 42,
  "postgres_rows": [
    {
      "query_signature": "a1b2c3d4",
      "datname": "orders",
      "rolname": "app",
      "query": "SELECT * FROM orders WHERE id = ?",
      "calls": 901,
      "total_time": 2612.4,
      "rows": 901,
      "shared_blks_hit": 5400,
      "shared_blks_read": 12
    }
  ]
}
```

Note: the check _does_ attach a `query_signature` here — a per-check convenience
key. This is distinct from the APM side, where the join key is the raw normalized
string (§6). The backend reconciles the two.

**`dbm-samples`** — one sampled execution + its plan, plus any propagated trace
context lifted from the SQL comment:

```json
{
  "host": "db-prod-1",
  "timestamp": 1720440000123,
  "ddsource": "postgres",
  "dbm_type": "plan",
  "db": {
    "instance": "orders",
    "query_signature": "a1b2c3d4",
    "statement": "SELECT * FROM orders WHERE id = 42",
    "plan": {
      "definition": "{...EXPLAIN FORMAT JSON...}",
      "signature": "9f8e7d"
    }
  },
  "network": { "client": { "ip": "10.0.0.5" } },
  "duration": 3100000,
  "dd": {
    // ← lifted from the sqlcommenter comment (full mode)
    "trace_id": "6f0c...",
    "span_id": "12ab...",
    "service": "web",
    "env": "prod",
    "version": "1.4"
  }
}
```

The `dd.trace_id`/`span_id` block is what powers the APM badge on a query sample —
it only appears when the tracer ran in `full` propagation mode (§7).

**`dbm-activity`** — a snapshot of currently-running sessions (waits/blocking):

```json
{
  "host": "db-prod-1",
  "timestamp": 1720440000000,
  "ddsource": "postgres",
  "postgres_activity": [
    {
      "pid": 8123,
      "state": "active",
      "wait_event_type": "Lock",
      "wait_event": "relation",
      "query_signature": "a1b2c3d4",
      "query": "SELECT * FROM orders WHERE id = ?",
      "blocking_pids": [8100],
      "query_start": "2026-07-08T12:00:00Z"
    }
  ]
}
```

---

## 5. SQL obfuscation deep-dive (`pkg/obfuscate`)

This is the load-bearing shared component. It is a **standalone Go module** at
`pkg/obfuscate` (not `pkg/trace/obfuscate`, which does not exist), pulled into the
trace-agent via a `replace` directive (`pkg/trace/go.mod:17,239`). The DBM checks
call the _same_ library. Identical code + identical config ⇒ identical normalized
output on both sides ⇒ the join works.

Entry points (`pkg/obfuscate/sql.go`):

- `ObfuscateSQLString(in)` `:299`
- `ObfuscateSQLStringForDBMS(in, dbms)` `:304` — the one the trace-agent uses; sets
  the SQL dialect so dialect-specific tokens normalize correctly.
- `ObfuscateSQLStringWithOptions(in, opts, optsStr)` `:314` — caches on
  `MemHashString(in) ^ MemHashString(optsStr)` `:324` (cache key only — _not_ a
  query signature), then dispatches to one of two backends.
- `ObfuscateSQLExecPlan(jsonPlan, normalize)` `:461` — for EXPLAIN plan JSON.

Two backends:

**(a) Legacy tokenizer + ordered filter chain** (`attemptObfuscation` `:391-457`,
tokenizer `sql_tokenizer.go`). Each token passes through, in order:

```
  metadataFinderFilter  → collect tables (after FROM/JOIN/UPDATE/INTO), commands,
   (sql.go filters)        comments, procedures — no token mutation
        ▼
  discardFilter         → drop comments, "AS" aliases (unless KeepSQLAlias),
                          MSSQL [bracketed] identifiers
        ▼
  replaceFilter         → String/Number/Null/Variable/PreparedStmt/Boolean/
                          DollarQuoted/EscapeSeq  →  "?"
                          (+ scrub digits in identifiers if ReplaceDigits)
        ▼
  groupingFilter        → collapse (?, ?, ?) → ( ? )  and  (?,?),(?,?) → ( ? )
                          so IN-lists of ANY arity normalize identically
```

Result → `ObfuscatedQuery{ Query string, Metadata SQLMetadata }`
(`sql.go:369-373`; `SQLMetadata` = `Size, TablesCSV, Commands, Comments,
Procedures`, `obfuscate.go:244`).

**(b) Modern `go-sqllexer` path** (`ObfuscateWithSQLLexer` `:470-539`) — delegates
to `github.com/DataDog/go-sqllexer` with `WithReplaceDigits`,
`WithReplacePositionalParameter`, `WithReplaceBoolean`, `WithReplaceNull`, etc., and
a normalizer that collects the same metadata.

**Obfuscation modes** (`obfuscate.go:160-162`): `normalize_only`, `obfuscate_only`,
`obfuscate_and_normalize`. Selected by `apm_config.sql_obfuscation_mode` /
`DD_APM_SQL_OBFUSCATION_MODE` (`apm_settings.go:166`) or, absent that, defaults to
`obfuscate_only` when the `sqllexer` feature flag is on
(`config.go:EffectiveSQLObfuscationMode`).

> **The parity constraint that makes DBM↔APM work:** whatever mode/flags the
> trace-agent uses, the DBM checks must use compatible settings, or the two sides
> produce different normalized strings and the join silently degrades. This is why
> the DBM↔APM docs warn that changing `sql_obfuscation_mode` (Agent 7.63+ →
> `obfuscate_and_normalize`) can alter normalized text and break text-based
> monitors. Normalization determinism _is_ the contract.

---

## 6. APM side — SQL span → per-query stats

In the trace-agent processing loop, obfuscation runs **before** stats
concentration, so the stat's `Resource` is the normalized SQL
(`pkg/trace/agent/agent.go:541` obfuscate, then `:598` `Concentrator.Add`).

```
 raw span: Type="sql", Resource="SELECT * FROM orders WHERE id=42", Meta[db.type]="postgresql"
        │  ObfuscateSpan → obfuscateSQLSpan (pkg/trace/agent/obfuscate.go:95)
        │    o.ObfuscateSQLStringForDBMS(Resource, dbms)
        ▼
 span.Resource = "SELECT * FROM orders WHERE id = ?"     ← normalized (= join key)
 span.Meta["sql.query"]  = same normalized string
 span.Meta["sql.tables"] = "orders"   (from SQLMetadata.TablesCSV)
        │  (on parse failure → "Non-parsable SQL query", raw SQL never leaks)
        ▼
 Concentrator.Add → RawBucket.HandleSpan (pkg/trace/stats/statsraw.go:237)
        │  builds BucketsAggregationKey{
        │     Service, Name, Resource(=normalized SQL), Type, SpanKind,
        │     PeerTagsHash = fnv(sorted peer_tags), ... }   (aggregation.go:34-49)
        │  folds into groupedStats: hits, errors, duration,
        │     + DDSketch okDistribution/errDistribution (rel accuracy 0.01, 2048 bins)
        ▼
 groupedStats.export → pb.ClientGroupedStats (statsraw.go:55)
   { service, name, resource(=normalized SQL), type, DB_type,
     hits, errors, duration, okSummary/errorSummary(DDSketch bytes),
     peer_tags[], span_kind, is_trace_root, ... }
        ▼
 pb.StatsPayload → POST /api/v0.2/stats   (pkg/trace/writer/stats.go:31,161)
```

Verified structure: `BucketsAggregationKey` and `PayloadAggregationKey`
(`aggregation.go:34-62`) exactly as listed above; the only FNV hash the agent
computes is over _peer tags_, via `tagsFnvHash` — again, **not** a SQL query hash.

`ClientGroupedStats` proto (`pkg/proto/datadog/trace/stats.proto`) carries
`resource` (normalized SQL), `DB_type` (field 6, "used to help in the obfuscation
step"), and `peer_tags` (field 16); `peer_service` (field 14) is reserved /
deprecated in favor of `peer_tags`.

If a tracer sends _pre-computed_ stats, the agent re-obfuscates their `Resource`
(`obfuscateStatsGroup`, `agent/obfuscate.go:285-300`) so client-side and
agent-side normalization converge to the same string.

---

## 7. The join, concretely + tracer propagation (spec/docs-sourced)

**Mechanism A — normalized-query join (statistical, always on).** The backend
groups APM `ClientGroupedStats.resource` and DBM `query` by identical normalized
text, disambiguated by `db_type` and host/peer tags. This attributes a fraction of
an endpoint's latency to specific DB queries with no app changes beyond running the
tracer.

**Mechanism B — sqlcommenter traceparent injection (per-execution, opt-in).**
Controlled by `DD_DBM_PROPAGATION_MODE`. The tracer rewrites the outgoing SQL to
append a comment. The DB records the comment in its stats views; the DBM check
lifts it into the `dbm-samples` payload; the backend links sample ↔ trace.

**How the trace ID reaches the Agent — the database is the message bus.** The
Agent's DB connection is read-only and knows nothing about the application. The
_only_ reason it ever sees a trace ID is that the app tracer physically embedded
it in the SQL string, the DB stored that string in `pg_stat_activity.query`, and
the Agent read it back out. The comment is a piggyback channel _through the
database_:

```
 ┌─────────────┐                                          ┌──────────────┐
 │ Application │                                          │   Database   │
 │  + tracer   │                                          │  (Postgres)  │
 └──────┬──────┘                                          └──────┬───────┘
        │                                                        │
   (1)  │ app issues: SELECT * FROM orders WHERE id = 42         │
        │                                                        │
   (2)  │ tracer's DB-driver instrumentation intercepts the      │
        │ query BEFORE the wire. It reads the CURRENTLY ACTIVE    │
        │ span context (trace_id, span_id) and appends:          │
        │   SELECT * FROM orders WHERE id = 42                    │
        │   /*dddbs='orders-db',...,traceparent='00-<trace_id>-  │
        │     <span_id>-01'*/                                     │
        │                                                        │
   (3)  │ ───── SQL-with-comment goes over the wire ───────────▶ │
        │                                                        │  (4) executes it.
        │                                                        │      While running,
        │                                                        │      the FULL text
        │                                                        │      (incl. comment)
        │                                                        │      sits in
        │                                                        │      pg_stat_activity
        │                                                        │      .query
 ┌──────┴───────┐                                                │
 │Datadog Agent │                                                │
 │  DBM check   │  (5) read-only scrape of pg_stat_activity ────▶│
 │              │ ◀──── row incl. "SELECT ... /*... traceparent   │
 │              │        =00-<trace_id>-...*/"                    │
 │              │                                                │
 │              │  (6) check PARSES the comment out of the query │
 │              │      text → extracts trace_id/span_id → puts    │
 │              │      them in the dbm-samples payload:          │
 │              │      { "dd": { "trace_id": "...", ... } }      │
 └──────┬───────┘                                                │
        │  (7) dbm-samples → dbm-metrics-intake                  │
        ▼                                                        │
   Datadog backend: dbm-samples.dd.trace_id ─── links to ──▶ APM trace
```

Three points that make this precise:

1. **The comment is generated fresh per execution.** At step (2) the tracer reads
   whatever span is active _right now_ (the DB-client span, whose parent is the
   request span), so the `traceparent` is that specific execution's live context —
   not something reconstructed later. That is what lets it pinpoint one exact trace.
2. **This only works for samples/activity, NOT for metrics.** `pg_stat_statements`
   (source of `dbm-metrics`) strips comments and normalizes — it merges all
   executions of a query shape into one row, so the per-execution `traceparent` is
   gone. `pg_stat_activity` (source of `dbm-samples`/`dbm-activity`) captures the
   **raw, verbatim** query text of a sampled/live execution — comment intact. So
   per-execution APM linking rides on samples, and Mechanism A (normalized text) is
   the only correlation available for metrics.
3. **This is why prepared statements are a caveat.** With server-side prepared
   statements the SQL text is fixed at prepare time and identical across
   executions, so a per-execution comment can't be varied cleanly — some tracers
   downgrade prepared statements to `service` mode (no `traceparent`), as noted in
   the caveats below.

> **Code-verified against dd-trace-go** (`ddtrace/tracer/sqlcomment.go`,
> `contrib/database/sql/{option,conn}.go`). Other languages follow the same
> sqlcommenter contract but were not individually re-verified; small differences
> exist (e.g. comment position, prepared-statement handling).

Injected sqlcommenter comment. In **dd-trace-go the comment is _prepended_**
(sqlcommenter allows either position; some tracers append):

```sql
/*dddbs='orders-db',dde='prod',ddps='web',ddpv='1.4',
  traceparent='00-00000000000000000000000000deadbeef-00000000cafebabe-01',
  ddh='db-prod-1',dddb='orders',ddprs='orders-db'*/ SELECT * FROM orders WHERE id = 42
```

Formatting (`commentQuery()`): keys/values are **URL-percent-encoded**, values are
single-quoted (literal `'` → `\'`), and tags are emitted in a **fixed hardcoded
order** (not alphabetical): `dddbs, dde, ddps, ddpv, ddsh, traceparent, ddh, dddb,
ddprs`.

Tag keys:

| key           | Go constant               | meaning                                                                  |
| ------------- | ------------------------- | ------------------------------------------------------------------------ |
| `dddbs`       | `sqlCommentDBService`     | database service (the DB's `DD_SERVICE` / db service name)               |
| `dde`         | `sqlCommentEnv`           | env (`DD_ENV`)                                                           |
| `ddps`        | `sqlCommentParentService` | **parent** service = the _app's_ `DD_SERVICE`                            |
| `ddpv`        | `sqlCommentParentVersion` | version (`DD_VERSION`)                                                   |
| `ddh`         | `sqlCommentPeerHostname`  | DB host (`ext.TargetHost`)                                               |
| `dddb`        | `sqlCommentPeerDBName`    | database name (`ext.DBName`)                                             |
| `ddprs`       | `sqlCommentPeerService`   | **peer** service (distinct from `ddps`)                                  |
| `ddsh`        | `sqlCommentBaseHash`      | base hash — only in `dynamic_service` mode (when process tags available) |
| `traceparent` | `sqlCommentTraceParent`   | **W3C** `00-<trace_id>-<span_id>-<flags>` — **`full` mode only**         |

Mode matrix (`DBMPropagationMode`; env `DD_DBM_PROPAGATION_MODE`, legacy fallback
`DD_TRACE_SQL_COMMENT_INJECTION_MODE`):

- `disabled` (also the empty/undefined default) — inject nothing.
- `service` — inject the identity tags (`dddbs, dde, ddps, ddpv, ddh, dddb, ddprs`)
  via `injectServiceTags()`. Enough for the DBM "active connections by service"
  breakdown and load attribution; **no** `traceparent`, so no per-execution link.
- `full` — identity tags **plus** `traceparent`. Powers the APM badge on individual
  query samples. The tracer sets `_dd.dbm_trace_injected: true` on the **SQL span**
  (via `withDBMTraceInjectedTag`, full mode only) so the backend knows a
  `traceparent` was embedded.
- `dynamic_service` — identity tags plus `ddsh` (base hash) but **no** `traceparent`.

How the `traceparent` is built (`Inject()` → `encodeTraceParent()`): a **fresh span
ID is generated up front** and becomes _both_ the `traceparent`'s span-id _and_ the
eventual SQL span's ID (so the comment and the span it creates share identity). The
trace-id is the **lower 64 bits of the active trace**, zero-padded to 32 hex; flags
= `01` when sampling priority > 0, else `00`. This is what makes point (1) above —
"fresh per execution" — concrete: each execution gets a newly minted span id woven
into the comment.

Requirements & caveats (from the DBM↔APM docs):

- Agent **7.46+**; support varies by DB × language. Postgres/MySQL support
  `full`+`service` across Go/Java/.NET/Node/PHP/Python/Ruby. SQL Server & Oracle
  are more limited (e.g. Go = `service` only for both).
- Performance gotchas in `full` mode: SQL Server (Java/.NET) issues an extra
  `SET context_info` round-trip and _overwrites_ any app use of `context_info`;
  Oracle (Java) overwrites `V$SESSION.ACTION`; Java prepared statements on Postgres
  (tracer 1.44+) overwrite the `Application` property and add a round-trip, which
  can increase connection pinning on RDS Proxy (below 1.44 they downgrade to
  `service`).

Why two mechanisms instead of one: A is cheap, universal, and statistical (great
for "what dominates this endpoint"); B is targeted and exact (great for "show me
_this_ slow execution's trace") but costs an in-band SQL rewrite and only covers
sampled executions. Together: aggregate always, drill-down on demand.

---

## 8. Cross-reference of the key files

| Concern                  | File                                                                             |
| ------------------------ | -------------------------------------------------------------------------------- |
| CGO bridge / Go export   | `pkg/collector/python/init.go:133`, `pkg/collector/aggregator/aggregator.go:168` |
| checkSender / aggregator | `pkg/aggregator/sender.go:428`, `pkg/aggregator/aggregator.go:490`               |
| Event Platform Forwarder | `comp/forwarder/eventplatform/impl/epforwarder.go:90,98,285`                     |
| DBM event types & tracks | `comp/forwarder/eventplatform/impl/pipelines_dbm.go:13,22`                       |
| Config keys / FIPS       | `pkg/config/setup/common_settings.go:1859`, `pkg/config/setup/config.go:927`     |
| SQL obfuscation          | `pkg/obfuscate/sql.go:299,304,314,391,470`, `pkg/obfuscate/obfuscate.go:160,244` |
| trace-agent obfuscation  | `pkg/trace/agent/obfuscate.go:95`, `pkg/trace/transform/obfuscate.go:43,72`      |
| stats aggregation        | `pkg/trace/stats/aggregation.go:34,52`, `statsraw.go:55,237`                     |
| stats proto / writer     | `pkg/proto/datadog/trace/stats.proto`, `pkg/trace/writer/stats.go:31,161`        |
| Go DBM check (Oracle)    | `pkg/collector/corechecks/oracle/{statements,activity,locks,metadata}.go`        |
| RDS/Aurora autodiscovery | `pkg/databasemonitoring/aws/{aurora,rds,config,aws}.go`                          |
