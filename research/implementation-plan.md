# Implementation sequence and team dependencies

Planning proposal; no production rollout is included. Product reports and the shared report
are authoritative for exact supported versions and experiment limits.

## Phase 0: agree on contracts and validation

**Owners:** OTel team, OTel Agent team, DDOT SDK team, and product owners.

- Agree on the principles boundary for stateful RC and native trace processing. HTTP routing
  and metadata enrichment alone do not cover every product in the inventory.
- Choose a source/version policy. Contrib's imported Agent modules and researched Agent main
  differ; do not promise main-branch behavior from an older linked library.
- Establish a test organization, enabled products, API/application key permissions, and a safe
  synthetic workload. Record site and organization without recording keys.
- Define compatible host/container/service/environment/runtime/trace identity across SDK,
  Collector and backend. Static host headers are inadequate for a multi-host gateway.
- Decide the exact disablement guarantee: managed ingress/control-plane/processing versus SDK
  instrumentation and agentless traffic. Require explicit off controls and honest `/info`.
- Add an evidence matrix for each supported SDK version and mode: source inspection, real SDK
  execution, Collector request/response checks, and backend/UI acceptance.

Exit: owners accept the protocol and product acceptance criteria; language support is explicit.

## Phase 1: validate existing paths and generic forwarding fixes

**Owners:** OTel team with HTTP forwarder maintainers; DDOT SDK/product teams for compatibility.

- Keep supported OTLP signals on upstream receiver/processor/exporter pipelines. Validate LLM
  GenAI schema and DB-related trace identity against backend product acceptance separately.
- Extend the local DBM experiment through the complete connector/exporter boundary: verify
  the demonstrated SQL resource bridge, 128-bit IDs, native span type, sampling and final
  trace/stats payloads. This local next step does not require backend credentials; the current
  result exercises the receiver and an imported mapping helper only.
- Use the existing HTTP forwarder to an actual Agent as a transitional transport option,
  preserving request compression and Agent responses. This still depends on the Agent.
- Consider upstream generic improvements only where product tests require them: response header
  multiplicity, explicit path transformation/routing, payload size/timeouts, and configuration
  validation. Do not add Datadog-specific RC/auth semantics to a generic forwarder.
- Keep experiments outside shipped components until maintainers agree on upstream changes.

Exit: executed native SDK workloads demonstrate the selected mode; no mock-only E2E claims.

## Phase 2: optional opaque product ingress

**Owners:** OTel Agent team and OTel team; Profiling, DSM, Data Jobs and DDOT SDK teams.

- Implement the proposed Datadog-owned SDK HTTP entry point with an explicit route registry,
  per-product allow/deny decisions, one source of discovery truth, secret-owned authentication,
  correct backend path/subdomain and identity enrichment.
- Start with protocols confirmed to be opaque in Agent handlers. Profiling, DSM SDK pathway
  statistics and OpenLineage are candidates, subject to each product report's prerequisites.
- Preserve response status, body and required headers; bound compressed bytes and request
  duration. Validate enabled and disabled modes and prevent forwarding arbitrary subdomains.
- Reuse Agent handler logic where dependency footprint, lifecycle and configuration contracts
  fit. Otherwise extract small protocol adapters with differential tests against the Agent.
- Do not move Agent DBM checks or messaging-system integration collection into this proxy.

Exit: all three applicable SDKs validated through the Collector to authenticated product intake,
with correct tags/correlation and explicit per-product off behavior.

## Phase 3: control plane and trace-carried products

**Owners:** OTel Agent team, Remote Configuration team, DDOT SDK team, APM, AppSec,
Live Debugging, CI Visibility, LLM Observability and Data Jobs teams.

- Reuse a single optional RC client/cache/trust implementation. Preserve capability/version
  negotiation, product targeting, acknowledgements, polling and error semantics.
- Agree on feature controls sharing an RC product such as `APM_TRACING`; dropping the whole
  subscription can affect permitted features. Use supported targeting/client enforcement,
  and never rewrite signed configuration to simulate an independent feature switch.
- Add Live Debugging upload/control routes together, including diagnostics and supported symbol,
  metric and span paths. A snapshot upload alone is not acceptance.
- Add CI data/control routes together and test test-discovery/settings/optimization responses.
- Validate native trace preservation for AppSec, LLM and Data Jobs before routing native SDK
  traces through `datadogreceiver`. Preserve structured metadata, IDs, event/link fields,
  sampling/retention and product extraction contracts; test every conversion boundary.
- Resolve Agent library upgrades or extraction gaps as separately owned implementation PRs.

Exit: UI-driven and response-driven workflows function with the correct product-specific
sampling/extraction; Collector disablement prevents managed product traffic.

## Phase 4: upstream signal development and parity

**Owners:** OTel team with DBM, DSM, Profiling and OTel Agent teams.

- Evaluate upstream database and messaging receivers for collection coverage without equating
  generic metrics with DBM query samples/plans/correlation or DSM pathway sketches.
- Evaluate upstream profiling data model/receiver/exporter support separately from native SDK
  profile upload compatibility.
- Plan parity work such as dual shipping, aggregation and metrics V3 with OTel Agent ownership,
  as required by the principles; it is not automatically solved by product HTTP support.
- Add distribution integration tests and neutral configuration tests. Product enable settings
  must never implicitly re-enable proprietary functions omitted from the distribution.

Exit: documented upstream coverage and precise residual Agent dependencies.

## Decisions requiring cross-team resolution

| Decision | Why it is needed | Owner |
| --- | --- | --- |
| Datadog extension versus separate SDK ingress component | Existing metadata extension has no such HTTP product API | OTel + OTel Agent |
| Shared control plane packaging | Remote config is stateful and exceeds opaque forwarding | OTel Agent + RC + SDK |
| Native structured-metadata preservation | Current native-to-OTel translation is not product parity | APM + AppSec + LLM + SDK |
| DBM/DSM placement wording | Principles retain Agent collection while SDK side paths exist | DBM + DSM + OTel |
| Strict feature disablement contract | Shared trace pipelines can still trigger backend products | SDK + product + OTel Agent |
| Backend test access and acceptance ownership | Local transport cannot establish the user experience | Each product team |
| Human PR text and attestation | Repository-specific authorship rules gate requested PR workflow | User |
