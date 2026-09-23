# PostgreSQL DBM development stack

Rebuilt and deployed from `dinesh.gurumurthy/poc-dbm-only` on 2026-09-23
using the workspace `.envrc_dev` (`DD_SITE=datad0g.com`).
The stack remains running on context `kind-otel-dd`, namespace `dbm-otlp-dev`.
It exports real OTLP to `https://otlp.datad0g.com`.

| Component | Kubernetes deployment/service | Identity / configuration |
| --- | --- | --- |
| PostgreSQL 16.15 | `postgres` | Database `dbm`, `pg_stat_statements`, `pg_monitor` role, 1 GiB PVC |
| Python workload | `workload` | Datadog service `dbm-otlp-dev-workload`; ddtrace 4.13.0rc1 / psycopg2 2.9.11 |
| Changed Collector | `collector` | Database service `dbm-otlp-dev-postgres`; standard OTLP HTTP exporter |

```text
workload --SQL + real traceparent comments--> PostgreSQL
    |                                          |
    | OTLP traces                              | PostgreSQL receiver
    v                                          v
Collector: traces + metrics + query_metrics/activity log records
    |
    +-- HTTPS OTLP --> otlp.datad0g.com/v1/{traces,metrics,logs}
```

The workload keeps an idle session alongside a session running a three-second
query and a five-row query. The Collector scrapes every five seconds using the
new `query_monitoring` contract. Optional bounded EXPLAIN collection is enabled.
The synthetic DBM records are also logged by a detailed debug exporter for local
inspection. Normal PostgreSQL metrics and workload traces have separate pipelines.

## Verified behavior and limits

[Deployment evidence](evidence/deployment.json) records the deployed images and
binary hash. [Stack verification](evidence/verification.json) records readiness,
Collector counters and an initial 15-minute Datadog API readback. The separate
[fresh readback](evidence/fresh-readback.json) uses `17:00:20Z–17:03:28Z`, starting
five seconds after the new Collector container started. Results:

- All three deployments are ready; the API key and site match `.envrc_dev`.
- The running Collector binary matches the clean DBM-only build.
- The new Collector exported 22 log records, 187 metric points and 39 spans at
  the first check, with no exporter failure counters and an empty queue.
- New-pod debug output contains 24 `db.server.query_metrics` and 24
  `db.server.activity` events at the provenance check.
- The initial 15-minute logs query returned both event families, optional plans
  and 37 matches to SDK SQL context. That query window includes the prior Collector.
- The strict post-rollout metrics query returned 37 non-null points, all after
  the new Collector started.
- The strict post-rollout logs query returned HTTP 429 (rate limited). Indexed
  span search returned HTTP 500 on the initial check and one follow-up. Fresh
  log/plan/context and indexed-span API readback could not be established during
  this rollout; successful exports and local events are recorded separately.

The workload's initial connection retries occurred while PostgreSQL initialized.
Its status retains that cumulative startup error count; `last_error_type: null`
and increasing iterations indicate successful subsequent queries.

This deployment does **not** deploy the changed backend OTLP intake services,
enable their DBM feature gates, or deploy the DBM private-intake authorization
change. Ordinary log/metric/span readback does not prove DBM processing or DBM UI
visibility. The raw-log-retention header does not enable DBM routing. That later
backend rollout retains the [documented reliability gate](../PLAN.md#reliability-gate).

## Reproduce

Run from the `dinesh.gurumurthy/poc-dbm-only` checkout. Prerequisites: Docker, kind, kubectl,
Python 3, an existing `kind-otel-dd` cluster, development intake credentials, and
a Collector binary built from this receiver branch. The deployment script pulls
the pinned PostgreSQL image and builds the workload image. It loads only the
selected architecture into kind.

The current deployment uses the standalone DBM-only Collector:

- Clean build source commit: `659a40eb55b8ed6321d5f6cbfa381eb6e38dd196`.
- Binary SHA-256: `453a95ef677baeae2fa8ec5319c2ec39c9dc914ca139dae0b4625f3efa5eab74`.
- Image: `dbm-otlp-dev-collector:453a95ef677b`.
- The running container's `/collector` hash matches the built binary.
- PostgreSQL's pod and persistent volume were preserved during rollout.

[Build provenance](evidence/collector-build.json) and
[rollout verification](evidence/rollout-provenance.json) record the exact source,
image and running binary. Historical branch-validation evidence in the parent
folder describes earlier local tests.
Use the [Collector build instructions](../README.md#build-and-exercise-the-collector-service),
then deploy, passing the resulting binary:

```sh
python3 research/implementation/database-monitoring/otlp-intake/dev-stack/deploy.py \
  --env-file /home/bits/otel/opentelemetry-collector-contrib/.envrc_dev \
  --collector-binary /tmp/dbm-only-collector/collector/dbm-otlp-collector

python3 research/implementation/database-monitoring/otlp-intake/dev-stack/verify.py \
  --env-file /home/bits/otel/opentelemetry-collector-contrib/.envrc_dev
```

Credentials are parsed as literal assignments, not executed as shell code.
Only `DD_API_KEY` and `DD_SITE` enter Secret `dbm-otlp-dev/datadog`; `DD_APP_KEY`
is used locally for readback. PostgreSQL has a generated password in Secret
`dbm-otlp-dev/postgres`. Values are never written into tracked files or image
build contexts. Rerunning preserves the database password and PVC and rolls out
the Collector when the Datadog credential Secret changes.

## Inspect

```sh
kubectl --context kind-otel-dd -n dbm-otlp-dev get pods,services,pvc
kubectl --context kind-otel-dd -n dbm-otlp-dev logs deployment/workload --tail=10
kubectl --context kind-otel-dd -n dbm-otlp-dev logs deployment/collector --tail=100
kubectl --context kind-otel-dd -n dbm-otlp-dev port-forward service/workload 18080:8080
```

The last command makes `http://127.0.0.1:18080/readyz` available locally. Status
contains iteration, row and propagated-context counts without credentials.

- [Database collection logs](https://app.datad0g.com/logs?query=service%3Adbm-otlp-dev-postgres)
- [Workload traces](https://app.datad0g.com/apm/traces?query=service%3Adbm-otlp-dev-workload)
- Metrics query: `avg:postgresql.backends{service:dbm-otlp-dev-postgres}`.

The fixed namespace and ownership label keep this stack separate from the older
`ddot-poc` workloads. The deployment script does not change kubectl's current
context. To stop traffic while retaining database storage:

```sh
kubectl --context kind-otel-dd -n dbm-otlp-dev scale deployment/workload deployment/collector --replicas=0
```

Deleting the namespace also deletes its PVC and synthetic database data; do that
only when discarding this stack is intended.
