# Implemented product HTTP forwarding

The Datadog extension now owns an optional SDK product listener implemented by the existing
`http_forwarder` extension. No new `pkg/trace` imports, Trace Agent, Datadog receiver,
Datadog exporter or connector are required. Ordinary traces, logs and metrics use explicit
standard OTLP receivers and exporters in the Collector configuration.

```yaml
extensions:
  datadog:
    api:
      site: ${env:DD_SITE}
      key: ${env:DD_API_KEY}
    hostname: ${env:DD_HOSTNAME}
    product_proxy:
      endpoint: 0.0.0.0:8126
      default_env: poc
      tags: [env:poc]
    products:
      continuous_profiling: {enabled: true}
      data_streams_monitoring: {enabled: true}
      data_jobs_monitoring: {enabled: true}
      llm_observability: {enabled: true}
      ci_visibility: {enabled: true}
      live_debugging: {enabled: true}
service:
  extensions: [datadog]
```

Every product defaults off. Omitting `product_proxy` creates no product listener. Enabling a
product without a listener fails validation. With a listener and all products off, only
`GET /info` is served; all product requests receive 404. Disabling a route does not stop SDK
instrumentation, alter remote configuration, or disable independent OTLP pipelines or SDK
agentless traffic. `application_security.enabled: true` and `database_monitoring.enabled:
true` fail with an instruction to configure the appropriate pipelines explicitly. The
extension cannot dynamically create those pipelines.

The existing Datadog extension metadata service and serializer remain active independently
of product switches. A vendor-neutral deployment uses only the generic extension and
standard telemetry components.

| Product | Implemented protocol | Deliberate boundary |
| --- | --- | --- |
| Profiling | `/profiling/v1/input` to profile intake `/api/v2/profile`; 202 becomes 200 | Opaque multipart, no profiling decoding or profile/trace joining |
| DSM | `/v0.1/pipeline_stats` to trace intake `/api/v0.1/pipeline_stats`; truthful discovery | No broker collection or schema/APM conversion |
| DJM | `/openlineage/api/v1/lineage` to data-obs intake `/api/v1/lineage?api-version=2`, Bearer authentication | No Spark native trace conversion |
| LLM | Exact EVP v2/v4 span and v1/v2 evaluation routes with subdomain matching | No native APM MetaStruct path; evaluations may require optional `application_key` |
| CI | Exact EVP test event, coverage, settings, skip, known/flaky/test-management, git search/packfile and coverage-report routes | No dynamic test media, native CI trace fallback, replay control or SDK feature emulation |
| Debugging | Snapshot, diagnostic, v2 debugger and symbol uploads with origin/request identity and tags | No remote configuration service or probe installation; SDK local probe installation must be explicit |

Discovery never advertises native trace ingestion or remote configuration. Generic EVP
capability advertisement does not mean every EVP path is accepted: only product-owned exact
paths and expected subdomains are allowed. All unknown paths, wrong methods, escaped path
aliases and disabled routes are rejected.

The generic implementation supplies exact routes, trusted per-route headers, endpoint path
and query replacement, metadata query append, optional request UUID, static discovery,
response status mapping and route disablement. `disabled: true` keeps the route table
present and denies the route; do not remove the final route to disable a product because an
empty route table preserves legacy single-origin pass-through behavior.

`compression_algorithms: []` is mandatory for opaque generic forwarding. The Datadog
extension enforces that behavior internally. Both implementations stream request bodies,
respect ingress size bounds, preserve error bodies, Retry-After and duplicate response
headers, strip hop-by-hop headers, and refuse redirects to avoid credential disclosure.
They use the standard Collector HTTP client TLS/proxy settings. SDK retry policies remain
SDK-owned. There is no durable queue, additional-endpoint fan-out, dynamic container tag
lookup, or signed remote configuration handling. Host/environment/additional tags are
explicit static collector-level metadata; this is suitable for a scoped PoC, not proof of
multi-host identity parity with the Agent.

The Datadog configuration is recommended for shared multi-product deployments because it
centralizes product allowlists, discovery and enablement. The generic extension is the
reusable vendor-neutral alternative for explicitly configured routes. They share transport
code but are independently constructed and tested.

## Build and tests

From each component directory, run `go test ./...` with Go 1.26.4. The local test suite uses
loopback `httptest` destinations; those destinations are tests only, never deployment
configuration. Both complete module suites passed on 2026-09-19. Tests cover compressed
binary payload identity, credential replacement, route selection, query rewriting,
profiling response adaptation, default-off behavior, subdomain rejection, duplicate
headers, Retry-After, redirect refusal and local capability discovery. These are component
results, not evidence of backend product behavior. The coordinator owns the real kind
Collector build/deployment and product agents own SDK and backend verification.

Exact build dependency: datadogextension v0.161.0 now directly requires the local
httpforwarderextension v0.161.0 via its checked-in relative `replace`. Any Collector
Builder manifest must replace both modules with the corresponding local directories.
