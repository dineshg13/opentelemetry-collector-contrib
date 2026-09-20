# Independent implementation review

Reviewer: dedicated `implementation_review` agent, separate from component and product
implementation owners. Initial and final delta reviews: 2026-09-19. Scope is the active combined
implementation against [new-instr.md](../new-instr.md) and [principles.md](../principles.md),
including actual source, build records, deployment scripts and recorded kind evidence.
This is an implementation audit, not an independent Datadog account/UI verification.

## Architecture assessment

The implemented architecture follows the requested separation. Standard OTLP receiver
and exporter pipelines handle ordinary telemetry. The distribution contains no Datadog
receiver, Datadog exporter, Datadog connector or embedded Trace Agent. The Datadog
extension composes the reusable `httpforwarderextension` engine; its product policy is
small, explicit and separate from pipeline creation. The independent generic build's
actual dependency record contains zero `datadog-agent/pkg/trace` packages. The Datadog
extension build retains only the documented existing `pkg/trace/log` and
`pkg/trace/traceutil/normalize` utility dependencies. PostgreSQL SQL-comment context
extraction reuses the receiver's existing lexer and W3C propagator rather than importing
Agent DBM processing.

No hidden mock destination was found in active combined or product deployment
configurations. Local test fixtures are identified as fixtures. Static `/info` responses
advertise implemented capabilities; missing RC and native trace ingestion are not
advertised. Configured backend credentials replace caller credentials, upstream redirects
are not followed, and bounded forwarding diagnostics omit headers, query strings and
payloads. Secrets are referenced through Kubernetes Secrets rather than committed values.

The current product documents correctly distinguish actual SDK activity, local semantic
tests, real intake acceptance and missing backend product readback. None of the eight
products is complete under the user's definition. Signed remote configuration, the DBM
event mapping and documented SDK gaps require work beyond supplying account access.

## Findings and verified resolutions

The descriptions below retain the original issue; all five findings were resolved in
the reviewed source, documentation or later evidence. Historical line numbers identify
the initial finding and may differ after fixes.

1. **DBM bootstrap relies on an already cached, mutable database image.**
   `database-monitoring/deploy.py:24` saves `postgres:16-alpine` without first obtaining it.
   A fresh Docker cache fails before deployment; future runs can also obtain a different
   PostgreSQL patch release. **Resolved in reviewed source:** the script pulls digest
   `sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685`
   for the node platform, tags it `ddot-postgres:16.15` and imports that exact image
   to the worker selected by the Deployment.
2. **DBM verification documentation requires full shared telemetry logging.**
   `database-monitoring/README.md:81` instructs enabling detailed debug output on the
   combined Collector, and `database-monitoring/verify.py:34` depends on that output.
   The final combined deployment deliberately uses basic logging, and enabling full
   shared payload logging was rejected during this task. Replace this instruction with
   scoped synthetic-only verification or explicitly retain the earlier result as
   historical evidence that cannot be rerun against the current shared Collector.
   **Resolved in reviewed documentation:** the README now explicitly preserves the
   earlier result as historical, forbids enabling full shared payload logging, and
   limits current rerunnable verification to the SDK-disabled check. Fresh exact
   record correlation remains unverified and is stated as a limitation.
3. **The initial combined evidence is an incomplete observation window.**
   `evidence/kind-integration.json` at 22:20 UTC contains no DSM forwarding outcomes and
   no CI forwarding outcomes for the generic Collector. It also records database scrape
   errors and workload restarts. The coordinator identified disk exhaustion during image
   imports, recovered space and bounded profiling temporary storage. **Resolved in
   reviewed evidence:** the 22:27:40 UTC window records DSM 202, CI event 202 and CI
   control 200 outcomes through both Collectors, with zero malformed forwarding records
   and no database scrape errors. The remaining primary warning concerns an existing
   PostgreSQL schema feature gate. `evidence/disk-recovery.json` preserves the incident
   and recovery explanation. These remain transport outcomes, not product completion.
4. **CI alternative's ordinary traces target the primary Collector.**
   The coordinator identified, and this review confirmed, that the generic Job only
   overrides `DD_TRACE_AGENT_URL`; `run.py` defaults its OTLP endpoint to `collector`.
   Set each Job's explicit OTLP endpoint to its corresponding Collector before claiming
   independent execution of the complete alternative. **Resolved in reviewed manifest:**
   each Job now sets its corresponding complete OTLP endpoint and explicit US5 site.
   Fresh completed `-final` Jobs and the later window under finding 3 were verified.
5. **Spark's archive build and deployment command disagree.**
   In the Data Jobs worktree's current delta, `build_spark.py` exports a Docker archive
   without loading a Docker image, while the README next calls `kind load docker-image`.
   **Resolved in reviewed documentation:** the command now uses `kind load image-archive`
   with `/tmp/ddot-djm-spark.tar`. The supplied workload manifests document the nonroot
   passwd entry and writable working directory required by Hadoop/Spark. All four
   enabled/disabled Spark Jobs completed using the actual built image.

The durable checkpoint now reflects the actual product assignments, kind execution,
shared controls, incident, remaining access/SDK boundaries and draft publication state.

## Checks performed by the independent reviewer

- Read the modified HTTP forwarder, Datadog policy/lifecycle integration, PostgreSQL
  context parser and relevant tests, together with combined configuration and deployment,
  migration, disabled-gate and evidence-capture scripts.
- Validated all 15 complete standalone/combined Collector configurations available in
  this pass against the actual built binary, with explicit non-secret validation values.
  All passed. The DBM receiver fragment is intentionally not a complete service config;
  Kubernetes resource YAML is not a Collector config.
- Re-ran `go test ./... -run 'Test(QuerySampleTraceContext|SQLCommentTraceparent|ScraperQuery)'
  -count=1` in `receiver/postgresqlreceiver`; the actual context-parser table passed,
  including quoted SQL, duplicate/nested comments, encoded context, invalid IDs and
  application-name precedence.
- Inspected the actual primary and generic `build.json` dependency records, including
  the generic build's explicit dirty-tree marker. Compared active configuration with
  product capabilities and recorded limitations; no forbidden native APM fallback found.
- `git diff --check` passed at the initial review point.
- At 22:26:52 UTC, independently read bounded Kubernetes status in verified context
  `kind-otel-dd`: all 17 active PoC Deployments had their one requested replica Ready,
  including both Collectors; neither `observability` nor `ddot-poc` had a Deployment
  or Service named `fake-datadog`; no `ddot-poc` ConfigMap data referenced that mock.

## Final delta review

Reviewed the combined tree at `6356f97178a1496424943642d24ae0f53e707045` plus the
coordinator's pending integration/documentation fixes. This includes all eight product
implementations, the completed native Spark build/kind artifacts and the three-signal
SDK fixture. No additional actionable architecture or reproduction finding remained
in this delta.

- Validated the two newly integrated Data Jobs configurations and both updated combined
  configurations with the actual Collector binary. All passed, bringing complete unique
  configuration coverage to 17. Embedded Kubernetes ConfigMap configurations match the
  corresponding combined source configurations.
- Parsed all new SDK-signals and Data Jobs Python files without syntax errors and repeated
  `git diff --check`; both checks passed. The unchanged component source did not require
  another repetition of the earlier component tests.
- Read the nine language/signal semantic results, log trace/span correlation assertions,
  disabled log/metric results and recorded Node HTTP/JSON regression. Documentation
  correctly identifies Python's installed upstream log/metric SDK dependencies, Java's
  Datadog API shims, Node's HTTP/protobuf log workaround and the released artifact pins.
  Host processes sent these signals through the kind Collector; the documents accurately
  distinguish this from running the signal fixture itself in Kubernetes pods.
- Read Spark's real image/artifact identity, successful SQL output, native lineage run IDs,
  disabled controls and both real intake 201 observations. Exact Spark span semantics
  come from the isolated actual-Collector test, not from aggregate kind telemetry.
- At 22:33:35 UTC, independently read bounded cluster status: every active PoC Deployment
  was Ready, all eight Data Jobs Python/Spark Jobs succeeded, and fresh CI/DSM `-final`
  Jobs succeeded with no failed attempts in those Job statuses. This is runtime execution
  evidence, not authenticated product readback.

The coordinator still owns final clean-source provenance, the last bounded observation
snapshot and GitHub publication verification. This audit does not certify those later
external operations. All eight product PRs must remain incomplete/draft because actual
Datadog product behavior is unverified, with additional documented RC, DBM mapping and
SDK gaps. No review result upgrades transport acceptance to end-to-end product success.

This reviewer did not mutate the cluster, enable payload logging, read credential values,
post PR comments or independently reproduce backend product UI results.
