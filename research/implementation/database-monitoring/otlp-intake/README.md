# Local PostgreSQL → OTLP intake → DBM implementation

A live development stack now runs in `kind-otel-dd/dbm-otlp-dev`, exporting to
`otlp.datad0g.com`. See [setup and verified readback](dev-stack/README.md). Backend
DBM routing deployment and DBM UI verification remain deferred.

See [PLAN.md](PLAN.md) for the approved milestones and [PROGRESS.md](PROGRESS.md)
for commits and verification. The [DBM endpoint reference](PLAN.md#dbm-endpoint-and-route-reference)
lists the five Agent HTTP paths, private gRPC route values, and PoC coverage.
Backend DBM deployment and DBM application testing
are deferred by the user. The local implementation is exercised against real
PostgreSQL, the standard Collector service/exporter, and the actual DBM decoders.

The receiver emits neutral, versioned collection records. For the DBM intake,
keep receiver limits at or below **10,000 rows and 1 MiB per collection**; the
mapper rejects larger collections even though the receiver permits higher caps. The shared backend
mapper translates them into `dbmmetrics`, `dbmactivity`, and `databasequery`
payloads. OTLP logs intake owns product routing and authenticated private-intake
publishing. Ordinary PostgreSQL metrics retain their existing metrics-intake path.

## Checkouts

| Repository | Execution branch | Local checkout |
| --- | --- | --- |
| Collector contrib | `dinesh.gurumurthy/poc-dbm-otlp-intake` | `/tmp/ddot-dbm-otlp` |
| dd-source | `dinesh.gurumurthy/dbm-poc` | `/home/bits/dd/dd-source` |
| dd-go | `dinesh.gurumurthy/dbm-otlp-intake` | `/tmp/dd-go-dbm` |

Collector and dd-go use isolated worktrees. The backend uses the user's requested
`~/dd/dd-source` checkout. Build artifacts under `/tmp` are reproducible and are
not committed.

## Reproduce real receiver collection

Monitoring requires PostgreSQL 14+ with `pg_stat_statements` extension API 1.9+
for global reset detection. The existing PostgreSQL 16 PoC has the extension, `dbm_monitor`, and
`dbm_app` provisioned. Open its port-forward in a separate terminal:

```sh
kubectl --context kind-otel-dd -n ddot-poc port-forward service/dbm-postgres 25432:5432 --address 127.0.0.1
```

From the Collector checkout (the offline flags assume cached dependencies):

```sh
GOCACHE=/tmp/dbm-go-cache GOTMPDIR=/tmp GOMAXPROCS=2 GOPROXY=off GOSUMDB=off \
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
output. Final captured source/native payloads and hashes are also pinned in this
folder under `evidence/final-*`. Use separate core and optional-plan fixtures;
the core-only test expects four payloads and the plan capture contains five. Trace context is synthetic; no SDK or UI trace-link success is claimed.

## Build and exercise the Collector service

From the Collector checkout:

```sh
GOCACHE=/tmp/dbm-go-cache GOTMPDIR=/tmp GOPROXY=off GOSUMDB=off \
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

## Optional execution plans

Plans are disabled by default. Add `query_plans: {enabled: true}` under the
receiver's `query_monitoring` configuration to enable bounded prepared EXPLAIN.
Defaults permit two attempts per collection, 500 ms per attempt, 64 KiB per plan,
and a 1,000-entry cache with a one-hour TTL. Plans use the monitor's planning
context and generic parameter plans. Failures omit the optional plan while
preserving the statistics collection.

Run the live receiver check with `--query-plans` and a separate output directory,
then map that captured OTLP using the same `dbm-map` command. The separate dd-go
`TestOTLPReceiverPlanConsumerCompatibility` test checks the real sample decoder
and PostgreSQL plan parser.
The recorded PostgreSQL test also covers repeated parameters, parameter-looking
string literals, and EXPLAIN UPDATE without executing the update.

The Collector wire harness above keeps plans disabled and verifies the core
record families. Plan evidence comes from the separate live-source → mapper →
actual-plan-parser test; no plan UI visibility is claimed.

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
