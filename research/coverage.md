# Product coverage and evidence

**No product has authenticated Datadog backend/UI end-to-end validation in this work.**
“Source support” and local experiments are separate. Source snapshots are in
[sources.json](sources.json); the supplemental Python wheel is in
[python-runtime.json](python-runtime.json).

| Product | Python | Java | JavaScript | Collector/Agent requirement | Local evidence |
| --- | --- | --- | --- | --- | --- |
| Live Debugging | Source support; metric/span/snapshot modes | Source support; metric/span/snapshot/symbol modes | LOG_PROBE support; other types rejected; no SymDB found | Proxy + discovery + optional/shared RC; trace/metric side paths | Full Python 4.13.0rc1 local probe + JS uploader components through actual forwarder and adapter to mock |
| LLM Observability | Native APM + EVP event/evaluation modes | Native gzip MessagePack, distinct evaluation versions | Native JSON span/evaluation writers | OTLP GenAI path plus native proxy and lossless native APM path | Full Python 4.13.0rc1 + JS writer components prove current-forwarder path mismatch; manual controls pass |
| Application Security | Native APM security data | Native APM security data | Native APM security data | Preserve structured security payloads, sampling, optional RC; WAF remains in SDK | Full Python WAF payload replay proves structured trigger details lost by actual receiver |
| CI Visibility | Dedicated CI support | Dedicated CI support | Dedicated CI support | Data uploads plus discovery and synchronous CI API responses | Real Python legacy/default pytest + Java manual APIs through adapter; JS source components; mock intake |
| Data Jobs Monitoring | No dedicated SDK DJM collector found; PySpark uses Java JVM path | Spark + optional OpenLineage | No dedicated SDK DJM collector found | Native Spark trace processing; separate standard OpenLineage forwarding | Real OpenLineage client backend-path gzip/Bearer/response tests pass; Agent path fails mock |
| Continuous Profiling | Native pprof profiles; runtime exercised | Native JFR/profiles with exact URL override; runtime exercised | Native profiles; source only | Opaque proxy + path/enrichment/response adapter; direct Java URL is narrow alternative | Full Java direct-path profile transport passes mock; real Python Agent paths require rewrite; no UI/correlation validation |
| Database Monitoring | SQL-comment correlation; propagator/writer exercised | SQL comments or DB-specific session context; source only | SQL-comment correlation; source only | Explicit trace pipeline preserving correlation and SQL resources; Agent database checks retained | Five Python SDK/receiver cases pass; 128-bit gate negative control; SQL resource bridge validated at mapping helper only; no database/driver/backend |
| Data Streams Monitoring | Checkpoint statistics + schema span features; runtime statistics exercised | Checkpoint statistics, discovery gate and additional metadata; runtime discovery exercised | Checkpoint statistics + schema span features; source only | Opaque SDK statistics adapter plus required native trace paths; Agent broker collection retained | Real Python gzip/MessagePack tests prove path rewrite and compression requirements; Java /info advertisement gates output; no broker/backend topology |

Framework and feature subsets are product-specific; a language row is not a promise of universal coverage.
An external Python/JS OpenLineage library does not establish native Datadog SDK DJM support.
AppSec covers WAF/IAST/RASP/API Security source paths, with runtime WAF evidence; broader SCA
scope is unresolved. Java and JavaScript DBM, JS profiling/DSM, and native Spark remain
source-only. CI's Java run uses manual APIs, not a complete framework integration. A full
SDK execution does not imply every feature or the inspected newer source snapshot was run.

## Evidence levels

1. Source inspection at a pinned commit.
2. Synthetic protocol fixture exercising a real component or an explicitly bounded model.
3. Selected unchanged SDK source components with documented ancillary stubs.
4. Full SDK instrumentation/capture and local transport to mock intake, with exact runtime version.
5. Authenticated backend records, joins and product UI/workflows.

Levels 1–4 do not establish level 5. Each experiment identifies which parts are actual SDK/
Collector code and which are models or mocks. Missing runtime tests are validation gaps,
not evidence that a source-supported language is unsupported.
