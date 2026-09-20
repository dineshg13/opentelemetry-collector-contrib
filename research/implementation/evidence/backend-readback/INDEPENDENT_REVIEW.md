# Independent backend-readback review

Reviewed on 2026-09-20 by `implementation_review`, independently of the agents that
executed the authenticated queries. Scope: the five evidence files linked below, their
recorded query windows, returned fields, identities and stated limitations. This reviewer
made no network calls, accessed no credentials and changed no runtime resources.

**Six products now have positive authenticated evidence for a tested core product path.**
This does not establish complete support for all eight products, all SDKs or all features.
AppSec has partial stored metadata evidence; DBM product records remain unverified.

| Product | Evidence supported by the results | Exact remaining limits |
| --- | --- | --- |
| LLM Observability | Eight workload matches: Python/Java/Node OTLP through each Collector, plus Python native spans and score evaluations through each proxy. Model, input/output checks, token values 4/1/5 and both native scores of 1 match. | Node matches emitted low 64 trace bits and exact span ID. OTLP-derived product trace IDs are remapped; original IDs survive as `otel.trace_id`. Broader features and UI navigation are unverified. |
| CI Visibility | Both Python services return stored pass/skip tests and session/module/suite hierarchy, with expected SDK version, fixture commit and shared hierarchy fingerprints. | No demonstrated coverage UI, optimization decisions or test-to-APM parenting; Java/Node compatibility gaps remain. |
| Data Streams Monitoring | Native DSM latency metrics for all three SDK services; payload-size series for Python/Node; synthetic offset lag equals 5. | Shared tags cannot distinguish proxies. Java payload-size series was not returned. No real broker integration, topology UI, schema/action coverage. |
| Live Debugging | Five bounded snapshot records cover both services and the exact local probe UUID, including snapshot IDs and captures objects. | Captures-object presence does not verify individual variable values. Cloud probe delivery/RC, diagnostics readback, UI rendering and trace linking remain unverified. |
| Continuous Profiling | Official MCP reads return three CPU flamegraphs with nonzero contribution from the known Python, Java and Node hot-loop functions. | Shared service/env/version prevent proxy attribution. Stacktrace counts are not upload counts. Trace/profile linking and interactive UI navigation remain unverified. |
| Data Jobs Monitoring | Official MCP returns successful job-health results for both exact final Spark run trace IDs, with duration, CPU, shuffle and two stages each. The current PoC Spark application also appears in the catalog. | Four stage SQL-plan calls return tool errors for missing `_dd.spark.sql_plan`; HTTP 200 is not success. One-hop lineage returns the anchor and zero edges. No demonstrated lineage/performance join, queryable SQL plan, long-running updates or distributed executor coverage. |
| Application Security | Python/Node scanner-request APM markers and Python ASM/OpenAPI schema metadata are stored. | This does not verify full WAF trigger/rule findings, AAP inventory, blocking or security-product correlation. Zero security-signal matches in the scoped queries do not prove absence elsewhere. |
| Database Monitoring | Two successfully authenticated, scoped DBM product searches return zero matching events. | No positive DBM product record established. Generic database-log search returned HTTP 429, establishing no result count. The OTLP-to-DBM mapping gap remains. |

Separately, all **nine SDK/signal combinations** have backend evidence: three emitted
span identities, three matching log bodies with stored trace/span IDs, and three counter
values of 3. This verifies those synthetic records, not automatic UI cross-linking or
every product's use of them.

The reviewer recomputed the stored SDK identity comparisons and all eight LLM matches
at their declared trace-ID width. Both Spark run IDs match their requests; per-stage CPU
times sum to the reported run CPU time. All three flamegraph results report successful
tool execution and matching application frames. Evidence files:

- [SDK signals](sdk-signals.json)
- [AppSec and CI](appsec-ci.json)
- [LLM, DSM and initial Spark REST reads](data-products.json)
- [Profiling, debugging and DBM](profiling-debugging-dbm.json)
- [Spark health, catalog, SQL plans and lineage MCP reads](data-products-mcp.json)

The official MCP results supersede the earlier claim that profiling and Spark product
readback lacked an available supported read path. They do not resolve the explicit
negative/untested cases above. SQL-plan errors do not establish a root cause; bounded
zero-result queries and rate limits cannot establish global product absence.

The two final MCP-related evidence files were checked for retained account-switch URLs
and credential/session query parameters; none were found. Their links target ordinary
product pages, and saved data is limited to the synthetic workload evidence described.
No blanket “all products verified” or “all features complete” statement is supported.
