# Proposed product configuration contract

The [example YAML](proposed-config.yaml) is a **design sketch**, not a current working
Collector config. Only `api`, `http` metadata settings and `service.extensions` in that
example exist today. See [working current forwarder config](forwarder/config.yaml) for the
actual executed configuration. No `products` struct or product switch was implemented.

## Defaults and ownership

All eight products default disabled when absent. A future `native_sdk_proxy.enabled:false`
opens no SDK listener; each `products.<name>.enabled:false` registers no managed upload
routes, excludes product-specific discovery entries and RC subscriptions/capabilities, and
starts no product workers or outbound product traffic. Shared RC runs only when explicitly
enabled and required by an enabled feature. It must reject disabled product requests even
if the SDK keeps sending after an earlier capability response. Shared routes such as EVP
must enforce per-product allowlists, not merely remain open because another product uses
EVP. A control request can contain multiple products; RC integration must preserve the
protocol while constraining subscriptions to enabled products, not discard signed response
metadata arbitrarily.

The Datadog extension owns policy, site/credentials for its outbound product requests,
listener/discovery, enrichment and RC provider selection. Receivers, processors, exporters,
connectors and service pipelines remain ordinary explicit Collector config. Mode/pipeline
references let the extension validate dependencies; they cannot silently instantiate a
pipeline or make a receiver preserve fields it currently drops. SDK instrumentation and
Agent database/message-system checks keep their own configuration.

Current datadogextension requires an API key/site and sends fleet metadata/liveness even
without product handling. Consequently **all product flags false does not mean zero Datadog
traffic** if that existing extension is enabled. A neutral deployment must omit proprietary
components from service configuration (and may use an upstream build). Product work needs
an explicit decision whether to decouple optional SDK service startup from existing metadata
service/serializer startup; do not silently change existing defaults.

## Inventory, modes, and dependencies

| Product key | Proposed modes / default in example | What enabling must validate; remaining Agent responsibility |
| --- | --- | --- |
| `live_debugging` | `native_proxy` | Debugger logs/diagnostics/v2 and symbol upload routes plus their separate destinations; SDK /info contract; RC provider for backend-managed probes. Local static probe cases, where supported, do not prove managed live debugging. Trace/metric probes require separate signal paths. |
| `llm_observability` | `otlp`; alternatives `native_proxy`, `native_traces` | OTLP mode references a configured trace pipeline and supported GenAI/OpenInference mapping/export. Native events/evaluations require EVP path/subdomain routing. SDK/native-trace mode requires exact LLM field preservation in trace processing. Experiment/API-specific credentials need independent validation. |
| `application_security` | `native_traces` | Product-complete trace receiver/pipeline preserving native security metadata and sampling; RC provider for remote rules/activation where used. SDK-local WAF/IAST is still SDK-owned; no separate generic AppSec upload endpoint solves the trace path. |
| `ci_visibility` | `native_proxy` | CI EVP event/coverage/control routes, synchronous responses and discovery; optional native trace fallback must reference a proven pipeline. Product-specific optimization/settings calls are not RC solely because they return configuration. |
| `data_jobs_monitoring` | `openlineage_proxy`; additional native SDK paths subject to report | Explicit OpenLineage route/backend and enrichment when used; language/framework native spans or Spark listener payloads may require separate routes/pipelines. An HTTP route cannot create absent JavaScript/Python instrumentation. |
| `continuous_profiling` | `native_proxy`; upstream profile mode only when verified | Multipart/pprof upload compatibility, profile path rewrite, tags, sizes/timeouts and optional extra destinations. Standard OTLP profile support must be validated separately against SDK collection and Datadog ingestion; accepting OTLP traces is unrelated. |
| `database_monitoring` | `correlation_only` | Validate a native/standard trace path carrying DBM correlation fields; clearly label UI/config scope as SDK correlation, never database collection. Database checks and query/sample/plan collection stay in Agent. Attempting `collection` here must fail as unsupported. |
| `data_streams_monitoring` | `sdk_stats_proxy` | SDK pipeline_stats route, msgpack/gzip body, discovery and enrichment; separate schema/message-related paths need explicit support. Broker integrations, lag collection and remote Kafka actions stay in Agent. Attempting Agent collection here must fail as unsupported. |

This table is a shared dependency model. Product reports determine exact SDK version/language
support and feature completeness. `enabled:true` must fail startup for an unsupported mode,
unavailable built component, missing required provider/credential, listener conflict or
missing/miswired pipeline; emitting only a warning would create false success. Disabled
stanzas are inert and do not require an otherwise-unused pipeline or credentials beyond
the existing extension's own requirements. Unknown product/mode keys should be rejected.

## Combining transports

`modes` is a list, with pipeline dependencies keyed by mode in `pipelines`. This is required
because a single SDK product can use more than one transport at once. For example, Python
LLM APM spans can coexist with EVP fallback/evaluation events; OTLP LLM spans can coexist
with native evaluations. A proposed combined setting is:

```yaml
products:
  llm_observability:
    enabled: true
    modes: [native_traces, native_proxy, otlp]
    pipelines:
      native_traces: traces/native
      otlp: traces/llm
```

This does not mean exporting every span three times. Validate distinct data responsibilities:
APM-carried fields use the native path, OTLP producers use the OTLP path, and native event/
evaluation routes use the proxy; SDK-selected fallback is respected. Reject duplicate modes,
unknown modes, pipeline references for unrelated modes, and unsupported combinations. Enabled
products need at least one supported mode. Disabled entries remain inert. A product flag may
cover several separately registered data/control paths; there is no inferred generic fallback.

## Runtime and disable boundaries

Use an immutable route/capability plan constructed after validation. /info reports only the
successfully enabled plan, including required protocol fields and a deterministic state
hash; never advertise `/v0.7/config` if the RC provider is absent. Bind the optional public
SDK listener once. If native traces are forwarded to an existing HTTP receiver, use a
different private address with an explicit dependency reference. The current extension API
does not supply access to receiver handlers; a new registration API or explicit private HTTP
target is required. Validate the effective pipeline configuration before accepting traffic.

Disabling this extension's managed feature does not undo SDK-local WAF execution, remove SDK
instrumentation or prevent a separately configured SDK agentless upload. Nor can a flag on an
extension suppress LLM/DBM/AppSec fields moving through independently configured OTLP/native
trace pipelines. Shared native APM payloads may carry several products at once. A contract
promising suppression of those product fields needs explicit product-aware receiver or
processor enforcement, with tests that ordinary APM remains valid. That is additional
transformation work outside the simple proxy/enrichment path and requires an architecture
decision under the principles. Until implemented, the flag must be described as disabling
managed ingress/control/extraction, not globally disabling the product.

No generic filter recipe is asserted to solve all mixed payload cases. A neutral offering is
achievable now by using upstream SDKs/receivers/pipelines and excluding proprietary extensions
and backends; fine-grained disablement while retaining native mixed-product APM remains an
explicit compatibility requirement. Export destination controls are separate from SDK control.

## Required tests before shipping

For each supported mode: fail invalid dependencies at config time; observe route rejection
and no managed product egress when disabled; assert /info and RC capability consistency;
exercise provider failure and shutdown; capture real language SDK requests/responses; verify
backend product records/UI and correlation identity. For native/OTLP mixed payload modes,
test both preservation when enabled and the documented disable scope. Cross-product tests
must confirm enabling CI does not inadvertently enable LLM merely because both use EVP.
