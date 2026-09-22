# Local PostgreSQL → OTLP intake → DBM implementation

See [PLAN.md](PLAN.md) for the approved milestones and [PROGRESS.md](PROGRESS.md)
for commits and verification. Backend deployment and Datadog application testing
are deferred by the user. The local implementation is exercised against real
PostgreSQL, the standard Collector service/exporter, and the actual DBM decoders.

The receiver emits neutral, versioned collection records. The shared backend
mapper translates them into `dbmmetrics`, `dbmactivity`, and `databasequery`
payloads. OTLP logs intake owns product routing and authenticated private-intake
publishing. Ordinary PostgreSQL metrics retain their existing metrics-intake path.

## Checkouts

| Repository | Execution branch | Local checkout |
| --- | --- | --- |
| Collector contrib | `dinesh.gurumurthy/poc-dbm-otlp-intake` | `/tmp/ddot-dbm-otlp` |
| dd-source | `dinesh.gurumurthy/dbm-otlp-intake` | `/tmp/dd-source-dbm-otlp` |
| dd-go | `dinesh.gurumurthy/dbm-otlp-intake` | `/tmp/dd-go-dbm` |

These are isolated worktrees; original checkouts are preserved. Build artifacts
under `/tmp` are reproducible and are not committed.

## Reproduce real receiver collection

The existing PostgreSQL 16 PoC has `pg_stat_statements`, `dbm_monitor`, and
`dbm_app` provisioned. Open its port-forward in a separate terminal:

```sh
kubectl --context kind-otel-dd -n ddot-poc port-forward service/dbm-postgres 25432:5432 --address 127.0.0.1
```

From the Collector checkout:

```sh
GOCACHE=/tmp/dbm-go-cache GOTMPDIR=/tmp/dbm-go-tmp GOMAXPROCS=2 GOPROXY=off GOSUMDB=off \
  python3 research/implementation/database-monitoring/receiver-integration/verify-kind.py
```

The runner reads the `ddot-poc/dbm-postgres-password:password` Secret reference
without printing the value. It creates and drops one uniquely named synthetic
database. It validates exact calls/rows deltas, active and idle connections, W3C
context, empty/failure distinctions, limits, and restart baselines. See its
[README](../receiver-integration/README.md) for prerequisites and output files.

## Map actual OTLP and decode it as DBM

From the dd-source checkout:

```sh
bzl run //domains/otel/libs/go/dbm/cmd/dbm-map -- \
  -input /tmp/dbm-live-collections.json -format json \
  -output-dir /tmp/dbm-mapped-local
```

The command uses the same mapper as intake. It writes `payloads.json`, native
per-instance/track JSON arrays, and a manifest. It makes no network requests.
`-format protobuf` also accepts a serialized OTLP ExportLogsServiceRequest.

From the dd-go checkout:

```sh
DBM_RECEIVER_CAPTURE_FILE=/tmp/dbm-live-collections.json \
DBM_RECEIVER_PAYLOADS_FILE=/tmp/dbm-mapped-local/payloads.json \
GOCACHE=/tmp/dd-go-dbm-gocache GOPROXY=off \
  go test -race ./database-monitoring/apps/dbm-metrics-intake/grpcservice \
  -run '^TestOTLPReceiverCaptureConsumerCompatibility$' -count=1
```

This exercises the real DBM metrics, activity and FQT decoders and PostgreSQL
trace parser. It validates calls=3, rows=15, active=1, idle=2, duration units,
collection intervals, database/role identity and query-signature correlation.
Without environment overrides it tests the checked-in real capture and mapper
output. Trace context is synthetic; no SDK or UI trace-link success is claimed.

## Build and exercise the Collector service

From the Collector checkout:

```sh
GOCACHE=/tmp/dbm-go-cache GOTMPDIR=/tmp/dbm-go-tmp GOPROXY=off GOSUMDB=off \
  python3 research/implementation/scripts/build-collector.py \
  --output /tmp/dbm-local-collector --generic-only --skip-image
python3 research/implementation/database-monitoring/otlp-intake/verify-collector-local.py \
  --binary /tmp/dbm-local-collector/collector/ddot-products-collector \
  --output /tmp/dbm-collector-verification-new
```

The second command uses the existing loopback PostgreSQL forward, starts and
stops its own loopback OTLP JSON capture, runs the Collector with
[collector-local.yaml](collector-local.yaml), and verifies that disabling
`query_monitoring` emits no new requests. Choose a fresh output directory each
run. Captured `wire/logs-*.json` files can be passed to `dbm-map` individually.
The current generic distribution contains no Datadog exporter, receiver,
connector, Datadog extension, or `pkg/trace` dependency.

## Intake regression suite

From dd-source:

```sh
bzl test //domains/otel/libs/go/dbm:all
bzl test //domains/otel/libs/go/dbm/cmd/dbm-map:all
bzl test //domains/database-monitoring/shared/libs/intake:all
rapid test -s otlp-intake-logs
```

Run Bazel commands sequentially. The service suite covers org and process gates,
ordinary-log fallback, raw retention, trusted org identity, complete snapshot
batching, encoded size limits, private RPC deadlines, and streaming failure
handling. These capture senders test local publishing behavior; they do not
represent a running DBM ingestion service.

## Later deployment and reliability work

Publishing requires `OTLP_INTAKE_LOGS_DBM_PUBLISH_ENABLED=true` and org experiment
`enable-otlp-intake-dbm-routing`. The DBM server authorization commit and service
identity/connectivity must be deployed together. Details are in dd-source's
`domains/otel/apps/apis/otlp-intake-logs/DBM.md`.

Private intake has no durable request deduplication contract. Timeout/partial
acceptance followed by replay can duplicate output; a regression explicitly
records that limitation. Keep production publishing disabled until the durable
contract and replay tests are complete. Local decode success does not establish
Kafka/indexing acceptance, DBM product visibility, UI trace linking, or Agent
feature parity. Native activity query IDs also pass through a legacy float64
consumer field; canonical string query signatures drive correlation.
