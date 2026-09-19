# Data Jobs Monitoring through the Collector

**Implementation update (2026-09-19):** [runnable implementation and evidence](../../implementation/data-jobs-monitoring/README.md)
now exercise actual Spark 4.0.0 with Datadog Java 1.66.0 and OpenLineage Spark 1.45.0.
Native Spark spans preserve `span.type=spark`, DJM markers, task metrics and SQL-plan data
through the SDK's own OTLP writer and the actual Collector. Real OpenLineage jobs run in
kind through both forwarding alternatives and receive US5 intake 201 responses. This
supersedes the earlier native-Spark-unexecuted and native-trace-receiver recommendations
below. Native Spark also built and ran inside kind through both alternatives: four Spark
jobs completed, including both SDK-disabled controls, and enabled native lineage received
US5 intake 201 responses. Backend product readback, long-running updates and
lineage/performance joins remain unverified; DJM is incomplete.

The remaining report records the original source investigation and its historical limits;
the implementation update above supersedes its runtime and transport blockers.

Status: source investigation complete; real **OSS OpenLineage Python HTTP transport**
validated through the current forwarder against a local capture server. Native Java Spark
runtime, real Agent runtime, authenticated Datadog ingestion, and product UI/correlation remain
unverified. This report does **not** establish native Data Jobs Monitoring support in dd-trace-py
or dd-trace-js. Owner: dedicated Data Jobs Monitoring research agent.

Data Jobs Monitoring has two distinct data paths. Java Spark performance/job spans use the
normal Agent trace processing pipeline. Optional OpenLineage events use an HTTP proxy. A
lineage-only proxy cannot replace the complete Spark product. The immediate recommendation
is a Datadog extension OpenLineage route with credentials and enrichment, composed with a
separately validated native trace pipeline. A generic upstream forwarder can already relay
an independently configured OpenLineage client that sends the backend path, but cannot
transparently handle the Java integration's Agent-prefixed path.

## Sources and scope

All source inspection uses the [baseline commits](../../README.md#baseline-sources):

| Repository | Commit |
| --- | --- |
| Collector contrib | `16fa3257d56c299e115a558b1a668018a39990d5` |
| dd-trace-java | `7b903a53644abc39f55b2fb21283546ae9801f35` |
| dd-trace-py | `d6ab482ea3fb3c8666e75ac1905a3325b8f80098` |
| dd-trace-js | `d62655e12494634bb53f8c0cd440f087b004ca63` |
| datadog-agent | `761050392a732097645b76d9845d36629f1ef3dc` |

Supplemental executed client: `openlineage-python==1.45.0`, CPython 3.12.13, installed under
`/tmp/ddot-research-openlineage-runtime`, with [exact dependency versions](requirements.txt).
Its installed `openlineage/client/transport/http.py` SHA-256 is
`02f790525412ef5fb9f546d00a766dc7409597619f72d471d43574f12cf0aec2`; distribution `METADATA`
SHA-256 is `6c242588fa9fc45c4646a26f290e633d09cd2d152175c784cc1653e9c2869e81`.
These are installed-file hashes, not wheel archive hashes. The supplemental client is separate
from all four Datadog repositories, which were only read.

| SDK / workload | Evidence-backed coverage | Collector implication |
| --- | --- | --- |
| Java SDK, Apache Spark JVM | Native DJM instrumentation, application/job/stage/task/SQL/streaming spans, launcher support, task metrics, optional OpenLineage integration | Needs native trace processing; OpenLineage route additionally needed when enabled |
| Python SDK | No native DJM, Airflow, PySpark, or OpenLineage instrumentation found in the pinned source. The sampling enum explicitly says DJM is unused | Do not advertise Python ddtrace DJM based on ordinary traces |
| Python Spark application | Java instrumentation explicitly handles the PySpark JVM launch/exit lifecycle | JVM agent prerequisites remain; not a Python SDK implementation |
| JavaScript SDK | No DJM, Spark, or OpenLineage instrumentation/configuration found in pinned `packages/` | Unsupported as a native SDK product in this investigation |
| External OpenLineage producer | Standard HTTP client is independently configurable; a real Python client was executed | Validates a standard protocol route only, not ddtrace or any framework integration |

Negative-source searches used `rg -n -i 'openlineage|data.jobs|data_jobs|pyspark|airflow' ddtrace`
and corresponding `packages/` searches in JS. The Python
[`SamplingMechanism.DATA_JOBS_MONITORING`](https://github.com/DataDog/dd-trace-py/blob/d6ab482ea3fb3c8666e75ac1905a3325b8f80098/ddtrace/internal/constants.py#L176)
comment is positive evidence against inferring support from the enum. Searches exclude
unrelated AppSec rule text. An external Airflow/OpenLineage integration, a custom job span,
or an SDK's generic OTLP ability is not evidence of native product support. Other frameworks
are outside the implementations identified here and need their own source/runtime audit.

## Current architecture

```mermaid
flowchart LR
  J[Java agent in Spark JVM] -->|native trace HTTP, discovery and responses| A[Agent pkg/trace API]
  A --> P[normalization, enrichment, sampling, stats, protobuf writer]
  P --> T[trace.agent.site /api/v0.2/traces]
  J -->|configures and delegates events| O[OpenLineage Spark listener]
  O -->|gzip JSON /openlineage/api/v1/lineage| L[Agent OpenLineage reverse proxy]
  L -->|Bearer + host/container tags, path/query rewrite| B[data-obs-intake.site /api/v1/lineage?api-version=2]
  T --> D[Datadog job/performance processing]
  B --> D
```

Backend product availability and joining these streams depend on the Datadog organization
and matching identity fields. Backend processors are not present in the inspected repositories;
the SDK's explicit correlation tags and source comments establish the intent, not a verified
backend/UI outcome.

## Java collection, configuration, and identity

[`Agent.configureDataJobsMonitoring`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-java-agent/agent-bootstrap/src/main/java/datadog/trace/bootstrap/Agent.java#L437)
enables Spark and executor instrumentation and long-running traces when DJM is enabled. It
also sets the Spark OpenLineage integration default when `dd.data.jobs.enabled` is explicitly
true. An optional command regex prevents installing the tracer on nonmatching JVM commands.
These integration defaults remain independently overrideable; collector configuration cannot
install the Java agent or instrument executors/drivers.

Relevant JVM properties and their environment equivalents:

| JVM property | Environment variable | Source default / effect |
| --- | --- | --- |
| `dd.data.jobs.enabled` | `DD_DATA_JOBS_ENABLED` | false; enables DJM bootstrap behavior |
| `dd.data.jobs.command.pattern` | `DD_DATA_JOBS_COMMAND_PATTERN` | Optional Java-command regex filter; absence allows startup |
| `dd.data.jobs.openlineage.enabled` | `DD_DATA_JOBS_OPENLINEAGE_ENABLED` | false; integrates a compatible OpenLineage Spark listener |
| `dd.data.jobs.openlineage.timeout.enabled` | `DD_DATA_JOBS_OPENLINEAGE_TIMEOUT_ENABLED` | true; configures OL timeout circuit breaker when supported |
| `dd.data.jobs.parse_spark_plan.enabled` | `DD_DATA_JOBS_PARSE_SPARK_PLAN_ENABLED` | true; plan parsing |
| `dd.spark.task-histogram.enabled` | `DD_SPARK_TASK_HISTOGRAM_ENABLED` | true; task histogram tags |
| `dd.spark.app-name-as-service` | `DD_SPARK_APP_NAME_AS_SERVICE` | false; optional Spark application service naming |
| `dd.agent.host`, `dd.trace.agent.port` | `DD_AGENT_HOST`, `DD_TRACE_AGENT_PORT` | Point at the SDK listener |
| `dd.trace.agent.url` | `DD_TRACE_AGENT_URL` | Standard trace destination; OL's helper specifically passes through `unix:` URLs and otherwise builds HTTP from Agent host/port |
| `dd.service`, `dd.env` | `DD_SERVICE`, `DD_ENV` | Service/environment identity used in traces and lineage correlation |

Property names and defaults are defined in
[`GeneralConfig`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-trace-api/src/main/java/datadog/trace/api/config/GeneralConfig.java#L95),
[`TraceInstrumentationConfig`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-trace-api/src/main/java/datadog/trace/api/config/TraceInstrumentationConfig.java#L188),
and [`ConfigDefaults`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-trace-api/src/main/java/datadog/trace/api/ConfigDefaults.java#L278).
These source configurations were not all executed in a Spark runtime.

The Scala 2.12 instrumentation's
[`build.gradle`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-java-agent/instrumentation/spark/spark_2.12/build.gradle)
declares a Spark 2.4.0+ compatibility check; the Scala 2.13
[`build.gradle`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-java-agent/instrumentation/spark/spark_2.13/build.gradle)
starts at Spark 3.2 and includes separate Spark 4 tests/muzzle constraints. These are repository
compatibility declarations, not a newly tested matrix. `sparkjava-2.3` is a web framework
integration and is unrelated to Apache Spark DJM.

[`Spark212Instrumentation.InjectListener`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-java-agent/instrumentation/spark/spark_2.12/src/main/java/datadog/trace/instrumentation/spark/Spark212Instrumentation.java#L77)
registers the Datadog Spark listener and conditionally adds OpenLineage only when required
OpenLineage classes are loadable. The SDK does not create a complete OL dependency from an
enable flag. The corresponding 2.13 implementation serves its Scala/Spark variants.
[`AbstractSparkInstrumentation`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-java-agent/instrumentation/spark/spark-common/src/main/java/datadog/trace/instrumentation/spark/AbstractSparkInstrumentation.java)
handles SparkSubmit, YARN, PySpark shutdown and SQL analysis failures.

[`AbstractDatadogSparkListener`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-java-agent/instrumentation/spark/spark-common/src/main/java/datadog/trace/instrumentation/spark/AbstractDatadogSparkListener.java)
creates `type=spark` spans and sets `USER_KEEP` with sampling mechanism `DATA_JOBS=10`.
[`CoreTracer.withTracerTags`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-trace-core/src/main/java/datadog/trace/core/CoreTracer.java#L2607)
adds `_dd.djm.enabled=1`. Preserve names, resources, all IDs, parentage, service/env, sampling
priority/decision, `_dd.djm.enabled`, Spark/Databricks/EMR/OL identity tags, SQL-plan fields,
and numeric task metrics. The
[`SparkAggregatedTaskMetrics`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-java-agent/instrumentation/spark/spark-common/src/main/java/datadog/trace/instrumentation/spark/SparkAggregatedTaskMetrics.java#L237)
collector also serializes histograms as base64 `_dd.spark.*` tags; these are not ordinary
OTel metric datapoints. Task metrics are aggregated in the SDK, not the OpenLineage proxy.

`setupOpenLineage` overwrites the managed agent transport with `type=http`, the Agent URL,
`endpoint=openlineage/api/v1/lineage`, and `compression=gzip`, under a composite transport
with `continueOnFailure=true`. It also sets run tags:
`_dd.trace_id`, `_dd.ol_intake.emit_spans:false`, `_dd.ol_service`,
`_dd.ol_intake.process_tags`, `_dd.ol_app_id`, and `_dd.ol_env` when env is set.
`emit_spans:false` matters: lineage sent by this integration cannot stand in for a missing
native trace stream. Other independently configured OL producers may use different tags
and backend behavior; the capture test does not prove those product semantics.

[`OpenlineageParentContext.from`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-java-agent/instrumentation/spark/spark-common/src/main/java/datadog/trace/instrumentation/spark/OpenlineageParentContext.java)
validates parent/root run UUIDs and derives trace/span IDs using FNV-1a; preserve these exact
relationships. Databricks context and application ID handling add another identity source.
Do not regenerate IDs during translation. `getAgentHttpUrl` documents OL 1.54+ for UDS; the
executed Python 1.45.0 HTTP client test does not validate Java UDS support.

## Wire protocols and Agent responsibilities

| Path | Request and authentication | Response/control | Agent responsibility |
| --- | --- | --- | --- |
| Java native traces to Agent | `PUT` selected `/v0.4/traces`, `/v0.5/traces`, or `/v1.0/traces`; Datadog MessagePack mappings, `application/msgpack`. Standard Java/language/version, trace-count, client-computed flags and container/entity headers. Local SDK-to-Agent path does not require the backend API key | `/info` discovery selects protocol and `long_running_spans`; `Datadog-Agent-State` triggers rediscovery. Trace success requires HTTP 200; JSON responses deliver rates | Decode, normalize, enrich, apply filters/obfuscation/truncation, preserve priority, compute stats, buffer/serialize/compress/retry |
| OpenLineage to Agent | `POST /openlineage/api/v1/lineage`, OpenLineage JSON; managed Java transport uses gzip | Backend response passed through; no DJM-specific RC or separate OL capability negotiation identified in this integration | Rewrite target/path/query, overwrite Bearer auth, add host/container tags, optional multiple destinations |
| Agent native trace egress | Default `https://trace.agent.<site>/api/v0.2/traces`, compressed protobuf AgentPayload, `DD-Api-Key` | Agent handles backend sending/retries; SDK responses are generated by receiver | SDK MessagePack is not the same as backend protobuf |
| Agent OL egress | Default `https://data-obs-intake.<site>/api/v1/lineage?api-version=2`; `Authorization: Bearer <API key>`; original body preserved by this handler | Main endpoint status/headers/body returned; additional endpoint responses discarded | No lineage JSON transformation, aggregation or sampling in this handler |

Java protocol evidence:
[`DDAgentApi.sendSerializedTraces`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-trace-core/src/main/java/datadog/trace/common/writer/ddagent/DDAgentApi.java),
[`DDAgentFeaturesDiscovery`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/communication/src/main/java/datadog/communication/ddagent/DDAgentFeaturesDiscovery.java),
[`OkHttpUtils`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/communication/src/main/java/datadog/communication/http/OkHttpUtils.java),
and [`LongRunningTracesTracker`](https://github.com/DataDog/dd-trace-java/blob/7b903a53644abc39f55b2fb21283546ae9801f35/dd-trace-core/src/main/java/datadog/trace/core/LongRunningTracesTracker.java#L82).
The last source gates periodic running-span reporting on the Agent capability; finished spans
alone do not validate the live-job experience. Generic tracer RC can coexist, but no dedicated
DJM remote configuration dependency was found; do not invent one for OL forwarding.

The Agent investigation begins at
[`pkg/trace/api/endpoints.go`](https://github.com/DataDog/datadog-agent/blob/761050392a732097645b76d9845d36629f1ef3dc/pkg/trace/api/endpoints.go#L122),
which registers the OL path, then follows
[`openLineageProxyHandler`, `openLineageEndpoints`, `newOpenLineageProxy`, `openLineageTransport.RoundTrip`](https://github.com/DataDog/datadog-agent/blob/761050392a732097645b76d9845d36629f1ef3dc/pkg/trace/api/openlineage.go).
The handler:

- Uses `ol_proxy_config.enabled` (Agent default true), `dd_url`, `api_key`,
  `additional_endpoints` and `api_version` (default 2). Per-product API key falls back to
  the main Agent key; an explicit URL can replace the whole primary destination.
- Replaces the whole destination URL, rather than appending the incoming Agent path/query;
  API versions >=2 add `api-version=<n>`. Version 1 does not add that parameter.
- Overwrites `Authorization` with the configured destination's Bearer key. This is not
  `DD-API-KEY`, unlike trace egress. Adds `Via: trace-agent <version>`, forwarded headers,
  `X-Datadog-Additional-Tags: host:<host>,default_env:<env>,agent_version:<version>` and
  normalized `X-Datadog-Container-Tags` when container lookup succeeds.
- Streams the single-target body; for multiple destinations it buffers through a limited
  reader, sends sequentially with per-target keys, and returns only the primary response.
  This handler contains no durable retry queue or OL parser.
- Returns HTTP 500 with an OFF message when disabled or destination setup fails. Because
  the endpoint registration has no `IsEnabled` predicate, it can remain visible in `/info`
  while disabled. A new Collector design should not copy that misleading capability behavior.

Configuration plumbing is in
[`comp/trace/config/impl/setup.go`](https://github.com/DataDog/datadog-agent/blob/761050392a732097645b76d9845d36629f1ef3dc/comp/trace/config/impl/setup.go#L720)
and defaults in
[`pkg/config/schema/yaml/core_schema.yaml`](https://github.com/DataDog/datadog-agent/blob/761050392a732097645b76d9845d36629f1ef3dc/pkg/config/schema/yaml/core_schema.yaml#L8496).
The schema additionally has delegated-auth keys, but the inspected OL handler does not
consume them; this report makes no delegated-auth implementation claim.

Native spans instead enter
[`Agent.Process` / `ProcessV1`](https://github.com/DataDog/datadog-agent/blob/761050392a732097645b76d9845d36629f1ef3dc/pkg/trace/agent/agent.go#L492).
Those paths modify data, resolve container identity, apply filtering and sampling, compute
stats and invoke writers. The
[`PrioritySampler.Sample`](https://github.com/DataDog/datadog-agent/blob/761050392a732097645b76d9845d36629f1ef3dc/pkg/trace/sampler/prioritysampler.go)
respects user keep; other configured filters can still discard spans. The normal
[`TraceWriter.serialize`](https://github.com/DataDog/datadog-agent/blob/761050392a732097645b76d9845d36629f1ef3dc/pkg/trace/writer/trace.go#L317)
produces compressed protobuf and
[`sender.do`](https://github.com/DataDog/datadog-agent/blob/761050392a732097645b76d9845d36629f1ef3dc/pkg/trace/writer/sender.go#L456)
adds the key. Newer v1 trace handling has its own writer path; advertise only a format that
the selected Collector pipeline truly supports, rather than forwarding arbitrary SDK versions.

## Placement and concrete alternatives

| Approach | Correctness and compatibility | Configuration / maintenance / principles | Recommendation |
| --- | --- | --- | --- |
| Current `http_forwarder`, no code changes | Works for independently configured OL client sending `/api/v1/lineage` and optional query; injects static Bearer/tags, preserves response. Cannot rewrite the Java-managed `/openlineage/...` path or add `api-version=2` via egress endpoint. No dynamic container lookup or native-trace transformation | Simple upstream component; dedicated listener and explicit client endpoint. Static tags insufficient for multiple workload hosts. Can also relay all paths to a real Agent, retaining Agent dependency | Usable transport-only standards path or Agent relay; not transparent Java DJM replacement |
| Extend generic forwarder | Add explicit route match/path/query rewrite, precise header overwrite/removal, independent targets, optional enrichment hooks. Could preserve Java OL requests; still cannot implement native trace semantics by rewriting paths | Upstream-friendly generic routing; substantial route/config tests, avoid embedding Datadog schema/site/discovery policy in generic component | Contribute generic routing only if broader users justify it; product adapter still required |
| Datadog extension OL proxy + native pipeline | Own `/openlineage/api/v1/lineage` → data-obs destination, API version, Bearer, identity and disable/discovery behavior. Route native trace traffic separately | Consistent product opt-in and shared Agent metadata; vendor-specific code belongs here. Current extension is metadata handling, not an existing SDK product proxy | Preferred product entry point; implement OL forwarding first and gate complete Spark claims on native trace validation |
| Existing receiver / upstream signal path | `apache_spark` receiver scrapes Spark REST performance metrics; it does not collect native DJM traces or OL JSON. `datadog` receiver translates native traces to OTel but has compatibility gaps. OTLP is not the OL protocol | Upstream Spark metrics are valuable under principle 3; proposing a standards OL receiver requires an explicit signal/data-model/backend contract. More conversion code than opaque OL forwarding needs | Reuse upstream metrics as complementary collection. Do not sell it, or generic OTLP acceptance, as DJM parity |

Current forwarder evidence is its
[`Config`](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/16fa3257d56c299e115a558b1a668018a39990d5/extension/httpforwarderextension/config.go)
and [`forwardRequest`](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/16fa3257d56c299e115a558b1a668018a39990d5/extension/httpforwarderextension/extension.go),
plus the [shared executable experiment](../../shared/forwarder/README.md).
The Datadog extension's
[`httpserver.NewServer`](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/16fa3257d56c299e115a558b1a668018a39990d5/extension/datadogextension/internal/httpserver/httpserver.go)
registers its metadata handler, not these SDK routes. No product `enabled` struct was added.

The existing
[`datadogreceiver.buildInfoResponse`](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/16fa3257d56c299e115a558b1a668018a39990d5/receiver/datadogreceiver/receiver.go#L291)
sets `long_running_spans:false`, does not advertise the Java `/v1.0/traces` route, and must
not be relabeled product-complete. Its
[`ToTraces`](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/16fa3257d56c299e115a558b1a668018a39990d5/receiver/datadogreceiver/internal/translator/traces_translator.go)
copies span meta/metrics and IDs, but uses `span.Type` only for web/http kind inference;
`type=spark` is not preserved as a dedicated attribute there. Recovery by an exporter cannot
be assumed. Native protocol fallback, Spark type/name/resource, long-running updates,
sampling, histograms, identity and backend joining need a targeted round-trip test.

The pinned
[`apache_spark` receiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/16fa3257d56c299e115a558b1a668018a39990d5/receiver/apachesparkreceiver/README.md)
is metrics-only and there is no OpenLineage receiver in the pinned receiver inventory.
Standard OpenLineage HTTP is independently usable; the
[OpenLineage client configuration documentation](https://openlineage.io/docs/client/python/configuration/)
specifies configurable URL/endpoint, Bearer auth and gzip. It does not establish a conversion
to OTLP or a Datadog DJM experience.

## Proposed configuration and disablement

This is a **proposal**, consistent with the [shared configuration contract](../../shared/configuration.md):

```yaml
extensions:
  datadog:
    products:
      data_jobs_monitoring:
        enabled: false
        modes: [openlineage_proxy]
```

The default is disabled, even though the Agent's existing OL proxy defaults enabled. To
describe a future complete Java Spark deployment, enable both independently validated paths:

```yaml
# Design sketch, not executable current Collector configuration.
products:
  data_jobs_monitoring:
    enabled: true
    modes: [openlineage_proxy, native_traces]
    pipelines:
      native_traces: traces/data_jobs
```

`openlineage_proxy` needs a listener, site/credential selection, exact rewrite and enrichment;
it has no pdata pipeline. `native_traces` needs an explicitly configured receiver/processors/
exporter pipeline with native semantics proven. The extension cannot silently instantiate
that pipeline. `enabled:true` must reject missing modes, unsupported modes, invalid pipeline
references, or unimplemented compatibility. Label OL-only support as lineage transport,
not complete native Spark monitoring. Per-product endpoint overrides/additional destinations
should reuse the shared credential/routing model; do not add an independent product serializer.

Disabled OL support must remove its managed route and discovery advertisement and produce no
managed OL egress. There is no DJM-specific RC subscription identified to remove. Disabling
this extension cannot stop SDK-local Spark collection, independent agentless OL clients, or
DJM fields traversing an unrelated native trace pipeline. Suppressing mixed APM/DJM fields
requires explicit receiver/processor policy and a test preserving ordinary APM; this is
additional processing under principle 4, not an HTTP proxy feature under principle 2.

## Executed prototype and reproducibility

[`prototype.py`](prototype.py) uses the real OpenLineage `RunEvent` model and `HttpTransport`,
the unmodified shared `http_forwarder` binary, and a local backend-shaped capture server.
No Java agent, Spark framework, Datadog Python tracer, Agent, or Datadog service is mocked as
if it were real product validation. The capture server's 202/404/429 responses are fixture
choices; they are not observations of Datadog backend acceptance/rejection.

```sh
# From repository root; build the shared forwarder first using its README.
uv pip install --target /tmp/ddot-research-openlineage-runtime \
  --cache-dir /tmp/ddot-research-openlineage-cache --index-url https://pypi.org/simple \
  -r research/products/data-jobs-monitoring/requirements.txt
PYTHONPATH=/tmp/ddot-research-openlineage-runtime \
  python research/products/data-jobs-monitoring/prototype.py \
  --forwarder /tmp/ddot-research-forwarder
```

The [recorded results](results.json) summarize the successful assertions. The test starts
only loopback listeners on ephemeral ports, cleans up its processes/temp
configuration and uses synthetic keys. The host restricts socket creation in the default
sandbox; the successful test used approved execution outside that sandbox. The first package
download attempt hit an unavailable default package mirror; explicit PyPI installation into
`/tmp` succeeded. A temporary sandbox launcher no-space error cleared without cleanup.

| Assertion | Observed 2026-09-19 |
| --- | --- |
| Agent-prefixed `openlineage/api/v1/lineage` with egress URL `/ignored?api-version=99` | Original path reaches capture unchanged; client observes fixture HTTP 404 |
| Standard `api/v1/lineage` | Original backend path reaches capture; client observes fixture HTTP 202 |
| Standard path with `?api-version=2` | Client query preserved; ignored egress query does not override it |
| Gzip body | Exact prepared client bytes unchanged at capture with `compression_algorithms: []`; decoded run ID, job name and event type match |
| Credential override | Exactly one `Authorization: Bearer collector-research-key` at capture, replacing client's synthetic Bearer |
| Metadata | Configured static host/env header present; no runtime container-tags header created |
| Error return | Fixture 429, JSON body, `Retry-After: 7` and Via returned; real client raises HTTPError; retry disabled for deterministic probe |

The runnable [current-forwarder YAML](current-forwarder.yaml) is for an independent OSS client.
Equivalent client config uses `url=http://127.0.0.1:18146`,
`endpoint=api/v1/lineage?api-version=2`, `compression=gzip`. It cannot simply override the
managed Java integration because `setupOpenLineage` writes the Agent-prefixed endpoint.
For the current Agent relay option, keep the Java path and set forwarder egress to the Agent's
host/port; that retains all Agent requirements and was source-reasoned, not run here.

## Follow-up and exact blockers

| Priority / owner | Deliverable | Dependency or exit criterion |
| --- | --- | --- |
| P0 DDOT SDK + DJM teams | Approve scope labels: Java native Spark, Java-managed OL, external OL; confirm which UI capabilities require both streams | No Python/JS native support claim based on generic HTTP/tracing |
| P0 OTel Agent + Collector extension teams | Implement OL route, Bearer selection, API-version query, metadata, disable/discovery and main/additional response semantics | SDK replay, invalid-config/disable tests; explicit decision on additional endpoints |
| P0 APM pipeline + OTel Agent teams | Native Spark receiver/exporter compatibility, long-running capability, preservation of Spark type and DJM sampling/identity fields | Actual Java Spark → Collector → captured Datadog payload round-trip, not only generic receiver acceptance |
| P1 Java SDK + DJM teams | Run small Spark batch/SQL/streaming examples with OL on/off, PySpark JVM case, supported Scala variants and compatible OL JAR | Blocked here by absent Spark workload/runtime and OpenLineage Java runtime. A supplemental `/tmp/ddot-research-java-agent-1.66.0.jar` is now available (tag commit `a099fffb31657bb6e8b4d04ee741491f3480829d`), but differs from primary Java HEAD and was not executed for DJM. Provide/build the missing dependencies in isolation and label release differences |
| P1 DJM backend/product team | Authenticated product test with job/performance, lineage, SQL metrics and identity joining | Blocked by no Datadog API key/test organization/backend access. Confirm entitlement/intake contract and inspect UI; record independent failure of either stream |
| P1 Collector upstream + standards teams | Generic forwarder routing if broadly useful; evaluate standard lineage signal model and complementary `apache_spark` metrics | No existing upstream OL receiver or proven OL→OTLP→DJM mapping in this source baseline |
| P2 Runtime validation owner | Agent parity test using `dda inv` tasks; gzip, multi-target, tags, large body, backend failure, UDS/HTTPS where supported | Agent source behavior inspected only; no Agent binary/runtime executed. Do not run raw Go in the Agent repository |

Backend credentials alone do not finish validation: native Java runtime, a implemented and
validated Collector route/pipeline, real Spark events, and product/UI access are all required.
Publication/integration is coordinator-owned; this product branch contains only research and
isolated protocol artifacts, with no change to Collector production support.
