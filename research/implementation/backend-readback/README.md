# Authenticated backend readback — 2026-09-20

The newly supplied API/application-key pair validates against US5 (`status: ok`).
The application key is stored at `ddot-poc/datadog-application-key:application-key`.
Its value matches the supplied file; that file's API key matches the existing Secrets
in both `ddot-poc` and `observability`. Credentials were never written to evidence.
This removes the earlier missing-application-key blocker. It does not establish every
product feature or resolve the existing SDK/control/DBM mapping gaps.

## What the backend actually returned

| Product | Authenticated evidence | Remaining verification |
| --- | --- | --- |
| LLM Observability | Eight kind workload identities matched across both Collectors: Python/Java/Node OTLP conversion and Python native spans. Correct model, input/output, 4 input tokens, 1 output token; both native evaluations have score 1. | Browser UI and features outside the deterministic manual instrumentation PoC were not exercised. Node exposes the low 64 trace bits; those and the exact span ID match. OTLP-converted LLM trace IDs change, with originals retained in `otel.trace_id`. |
| CI Visibility | Each Python forwarding service has 10 stored test records: two each of session, module, suite, passed test and skipped test. Ordinary APM spans are also indexed. | Coverage/optimization outcomes, automatic test-to-APM parenting, and full Java/Node framework parity. |
| Data Streams Monitoring | 19 native DSM latency series cover all three SDKs; Python/Node payload-size series and synthetic consumer lag are stored. | Shared tags do not separately attribute Collector alternatives. Java payload-size series, real broker integration, UI topology, schemas and actions remain unverified. |
| Live Debugging | Five bounded snapshot results include both forwarding services, the exact probe UUID and captured locals. | Backend trace correlation, diagnostics readback, signed remote configuration and UI-created probes. The inspected snapshot records did not expose the checked trace-ID fields. |
| Application Security | Python/Node scanner spans retain `appsec.event=true`; Python ASM/OpenAPI schema metadata is indexed. | Full WAF rule/trigger findings and security UI behavior. Two scoped security-signal searches returned zero matches; this is not proof that no detection occurred. Java structured metadata and other documented security paths remain incomplete. |
| Database Monitoring | Both documented DBM product queries succeeded with HTTP 200 and zero scoped records. | DBM event mapping and query/plan product behavior remain unverified. A generic OTLP query-log read hit HTTP 429; that is not a successful empty search. |
| Data Jobs Monitoring | Both exact Spark runs return product job health with duration, CPU, shuffle and stage metrics; the PoC Spark application is present in the data catalog. | SQL-plan reads for four returned stages lack `_dd.spark.sql_plan`; the bounded lineage graph has its anchor and zero edges. Long-running updates and distributed executor coverage remain incomplete. |
| Continuous Profiling | CPU flamegraphs contain the actual Python, Java and Node workload functions with nonzero contributions. | Shared service tags prevent per-Collector attribution in this query; trace-to-profile correlation remains unverified. |

The three-signal fixture has stronger evidence too: the stored Python, Java and Node spans
match their emitted IDs; all three logs retain matching `otel.trace_id` and `otel.span_id`;
all three counters have value **3**. This verifies those exact nine language/signal cases.

All 17 existing PoC Deployments were Ready during this readback. No cluster workload,
product setting, retention rule or backend resource was changed for these checks.

## Evidence and boundaries

- [Authentication](../evidence/backend-readback/authentication.json)
- [Exact SDK signals and correlation](../evidence/backend-readback/sdk-signals.json)
- [AppSec and CI queries](../evidence/backend-readback/appsec-ci.json)
- [LLM, DSM and Spark/APM queries](../evidence/backend-readback/data-products.json)
- [Profiling, debugger and DBM reads](../evidence/backend-readback/profiling-debugging-dbm.json)
- [Spark product health, catalog, SQL plans and lineage](../evidence/backend-readback/data-products-mcp.json)
- [Independent readback review](../evidence/backend-readback/INDEPENDENT_REVIEW.md)
- [Current kind readiness](../evidence/backend-readback/kind-readiness.json)

Counts are bounded returned samples or series, not total account counts. Queries use known
PoC services, probe identities, job identities and time windows. Synthetic captured values,
unrelated telemetry and credential values are omitted. Some later APM/log searches returned
HTTP 429; those queries were stopped and are not treated as negative product results.

The independent reviewer recomputed the nine SDK cases and eight LLM identity/token/evaluation
checks, and reviewed the CI, DSM, AppSec, debugger and DBM claim boundaries. A final delta
review covers the profiling and Spark product MCP results. The earlier
missing-access statements elsewhere describe the September 19 checkpoint; this page and
its dated evidence are the current backend-readback record. Product drafts remain open.

## Reproduce a bounded query

[client.py](client.py) loads only the two named Kubernetes Secrets in memory and sends them
to US5 over HTTPS without following redirects. The documented DBM query endpoint on the
US5 application host is separately allowed. It performs read/search calls only.
Use the exact request bodies, filters and time windows recorded in the evidence; old
records may eventually age out of the account's retention window. Stop on HTTP 429.

For example, from this directory, repeat a recorded CI query without printing event payloads:

```sh
python3 - <<'PY'
import json
from pathlib import Path
from client import Client
evidence = json.loads(Path('../evidence/backend-readback/appsec-ci.json').read_text())
query = next(q for q in evidence['searches'] if q['category'] == 'ci-test-events')
status, response = Client().request(query['method'], query['path'], query['request'])
print({'http_status': status, 'returned_records': len(response.get('data', []))})
PY
```

For profiling and Spark product reads, [mcp_client.py](mcp_client.py) supports only the
official US5 endpoint and an explicit allowlist of read tools. Initialize the client, then
replay a `tools/call` using the recorded tool name and arguments. HTTP 200 alone is not
success: inspect RPC errors, tool `isError`, and the returned product data. For example:

```python
from mcp_client import MCPClient
client = MCPClient("profiling")
status, initialized = client.initialize()
assert status == 200 and "result" in initialized
# Use the exact bounded query arguments in profiling-debugging-dbm.json.
```

Official contracts: [LLM export](https://docs.datadoghq.com/llm_observability/investigate/export_api/),
[CI test search](https://docs.datadoghq.com/api/latest/ci-visibility-tests/search-tests-events/),
[span search](https://docs.datadoghq.com/api/latest/spans/search-spans/),
[log search](https://docs.datadoghq.com/api/latest/logs/search-logs-post/),
[metrics query](https://docs.datadoghq.com/api/latest/metrics/query-timeseries-points/),
[DBM product queries](https://docs.datadoghq.com/database_monitoring/guide/build_apps_with_dbm_api/),
and [Datadog MCP read tools](https://docs.datadoghq.com/mcp_server/tools/).
