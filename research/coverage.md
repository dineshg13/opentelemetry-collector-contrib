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
| Continuous Profiling | Native profiles | Native JFR/profiles with exact URL override | Native profiles | Opaque proxy + path/enrichment/response adapter; direct Java URL is narrow alternative | Full Java direct-path profile transport passes mock; Python Agent paths require rewrite; report finalizing |
| Database Monitoring | Investigating SDK correlation | Investigating SDK correlation | Investigating SDK correlation | Agent database collection retained; SDK correlation separate | Investigation active |
| Data Streams Monitoring | SDK checkpoint path investigated | SDK checkpoint path investigated | SDK checkpoint path investigated | Agent broker collection retained; SDK statistics are separate opaque path | Investigation and real Python transport test active |

Reports will replace queued/in-progress entries as each investigation finishes. Framework and
feature subsets are product-specific; a language row is not a promise of universal coverage.
An external Python/JS OpenLineage library does not establish native Datadog SDK DJM support.

## Evidence levels

1. Source inspection at a pinned commit.
2. Synthetic protocol fixture exercising a real component or an explicitly bounded model.
3. Selected unchanged SDK source components with documented ancillary stubs.
4. Full SDK instrumentation/capture and local transport to mock intake, with exact runtime version.
5. Authenticated backend records, joins and product UI/workflows.

Levels 1–4 do not establish level 5. Each experiment identifies which parts are actual SDK/
Collector code and which are models or mocks. Missing runtime tests are validation gaps,
not evidence that a source-supported language is unsupported.
