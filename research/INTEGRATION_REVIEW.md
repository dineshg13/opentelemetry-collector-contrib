# Independent integration review

Publication statements below record the pre-publication review checkpoint. The subsequent
user-authorized nine draft PRs and their current targets are in the [publication index](publication/README.md).

Reviewer: `/root/integration_review`, 2026-09-19. This review is independent of the product
owners and coordinator. Status: **technical research review passed for all eight products**.
All actionable review findings are resolved, and final documentation packaging checks pass.
This review does not certify backend compatibility or PR readiness.

The review reads the assembled `dinesh.gurumurthy/poc-review-snapshot` at
`/tmp/ddot-research-review` and coordinator-owned common documents at
`/tmp/ddot-research-all`. The review snapshot deliberately preserves product diffs against
the intended combined PR target. It is not a GitHub PR merge or the final integration PR.

## Findings and disposition

| Finding | Severity | Required correction / disposition |
| --- | --- | --- |
| Live Debugging primary YAML used singular `mode` | Medium | Corrected to `modes: [native_proxy]` in product commit `625ccdcece9`; independently re-read in snapshot. |
| CI primary YAML opted in while other primary examples default off | Low | Corrected both switches to false in `4bfb449d7ec`; independently re-read. Explicit secondary opt-in examples remain appropriate. |
| Shared Java runtime wording could imply profiling ran on coordinator's JDK | Medium | Common manifest/docs now distinguish coordinator Temurin 25.0.4 inventory and actual profiling Microsoft OpenJDK 21.0.11. CI explicitly labels its historical JDK unrecorded; independently re-read. That historical provenance gap remains a documented limitation. |
| CI reproduction command hard-coded `/home/bits` | Low | Corrected to `$HOME/dd/dd-trace-js` in `e00a24d4931`; independently re-read. |
| Integrity checker depends on recorded absolute source paths | Low | Coordinator added `--source-root` mapping and documented its scope; independently executed successfully against all eight products after integration. |
| DSM primary shared-listener sample enables the listener; Java execution provenance is not recorded | Low | Corrected in `7914650dd0a`: sample disabled, shared artifact linked, actual Java/javac Microsoft 21.0.11 paths/versions and matching jar checksum recorded. Independently re-read. |
| DSM fixture processes inherit SDK environment settings | Low | Corrected in `7914650dd0a`: both fixtures remove inherited `DD_`, `_DD_` and `OTEL_` settings; Java also excludes injected option variables. Owner reran all five Python and two Java cases successfully; code and resulting records independently reviewed. |

No unresolved technical review finding remains. All eight principles products and all three
requested SDK languages are accounted for, with unsupported/source-only combinations explicit.
Shared configuration consistently uses mode lists, keyed pipeline references and disabled
primary examples. Product proposals reuse shared routing, discovery, RC, identity and trace
pipeline work; separate experimental adapters are labeled fixtures, not competing production
implementations. The final common README, coverage, implementation plan and publication
narrative preserve the evidence and publication boundaries.

## Independent checks performed

- Read the instructions, principles, repository rules, shared design/configuration, architecture,
  implementation plan, eight product reports, their prototype code and result summaries.
  DSM's report and final correction commit were reviewed in its isolated product worktree.
- Independently re-ran the final assembled documentation checker with
  `--source-root /home/bits/dd`: **8/8 reports, 102 local links and 93 pinned source-file targets
  passed**. An earlier missing self-link was resolved by integrating this review document.
  The check verifies target existence, not whether each source supports every claim.
- Independently re-ran `git diff --check` against the recorded Collector baseline: passed.
  The final progress table records all eight products complete/reviewed, while PRs remain
  accurately labeled pending human sections.
- Re-ran the CI JavaScript source-component checks: four discovery cases and two writer cases
  passed. Network and encoders remain stubbed; this is not a full JavaScript SDK run.
- Re-ran all shared Go forwarder tests with `-mod=readonly`: request/path/response contract,
  compression defaults and explicit preservation, and ignored unknown routing keys passed.
  The first sandboxed attempt was blocked by loopback socket permissions; an authorized
  escalated rerun passed. No production network or backend credential was used.
- Re-ran the synthetic native receiver prototype with `go run -mod=readonly .`: scalar
  security markers survived, structured metadata disappeared, discovery advertised
  `span_meta_structs:false`, and unknown/RC paths returned empty 200 through the catch-all.
  This independently corroborates the component limitation; it does not repeat the real WAF
  generation or validate final exporter/backend behavior.
- Checked the Git diff and history: changes are under `research/`; inspected commits retain
  `Assisted-by` trailers. PR templates and publication instructions distinguish pending human
  authorship/attestation from completed research and local review integration.

Primary-source spot checks independently confirmed:

| Critical claim | Source checked |
| --- | --- |
| Current forwarder replaces scheme/host, preserves path/query, and collapses repeated response headers | Contrib `extension/httpforwarderextension/extension.go:forwardRequest` at recorded baseline |
| Native receiver does not map `MetaStruct`; discovery reports unsupported structured metadata | Contrib `receiver/datadogreceiver/internal/translator/traces_translator.go` and `receiver.go:buildInfoResponse` |
| Agent EVP strips prefixes, allowlists headers, selects subdomain/site and injects its API key; no application-key relay was implemented in the pinned handler | Agent `pkg/trace/api/evp_proxy.go` at recorded baseline |
| Java profiling exact URL overrides Agent URL; Agent profile proxy normalizes 202 to 200 | Java `Config.java:getFinalProfilingUrl`; Agent `pkg/trace/api/profiles.go:multiTransport.RoundTrip` |
| Node profiler hardcodes `/profiling/v1/input` despite an HTTP Agent URL pathname | JavaScript `profiling/exporters/agent.js:AgentExporter.export` |
| Node debugger rejects non-LOG_PROBE types | JavaScript `debugger/devtools_client/remote_config.js:processMsg` |
| Python WAF/IAST defaults use structured metadata | Python `ddtrace/internal/settings/asm.py` |
| Spark OpenLineage is a separate managed gzip transport with `emit_spans:false`; Python's DJM sampling enum is unused | Java `AbstractDatadogSparkListener.setupOpenLineage`; Python `ddtrace/internal/constants.py` |
| DBM's six Event Platform tracks and batching descriptors live outside the trace proxy | Agent `comp/forwarder/eventplatform/impl/pipelines_dbm.go` |
| Existing PostgreSQL/MySQL/SQLServer receivers collect development query logs as well as beta metrics; their context extraction is not universal Datadog DBM compatibility | Receiver `metadata.yaml` files; PostgreSQL `client.go`, SQLServer `scraper.go` |
| DBM correlation depends on default-enabled native receiver 128-bit reconstruction; dynamic-service propagation is language-specific | Receiver generated feature gate and translator; Python DBM settings and JavaScript `plugins/database.js` |
| DSM SDK statistics are opaque through the trace proxy; broker messages/schema events use a separate Event Platform route | Agent `pkg/trace/api/pipeline_stats.go`, `comp/forwarder/eventplatform/impl/pipelines_datastreams.go`; integrations-core `cluster_metadata.py:_emit_schema_registry_events` |
| DSM JavaScript hashes differ from FNV-based SDKs and must remain opaque in the proxy | JavaScript `datastreams/pathway.js:shaHash` |

Official documentation independently corroborates the distinct supported GenAI/OpenInference
OTLP ingestion path and conversion suppression attribute, without establishing native SDK or
backend runtime success. [Datadog OpenTelemetry instrumentation](https://docs.datadoghq.com/llm_observability/instrument/otel_instrumentation/).
The upstream profiles specification still labels the signal Alpha; its data model does not
prove Datadog native multipart intake accepts OTLP. [OpenTelemetry Profiles specification](https://opentelemetry.io/docs/specs/otel/profiles/).

## Evidence boundaries retained

The reviewed prototypes run actual forwarder/receiver components in small hosts, not a full
Collector service or distribution. The Agent main checkout is a source reference, while
Collector-linked Agent modules and executed released SDKs have separately recorded versions.
Full Python instrumentation, selected real Java workloads and JavaScript source components
are distinguished from synthetic fixtures and experimental adapters. The external OpenLineage
Python client does not imply native dd-trace-py Data Jobs support.
The DBM experiment uses the actual Python propagator on a manually created span, not a
database driver. Its resource-name bridge executes an imported mapping helper, not the full
exporter/connector, and the report explicitly preserves this boundary. Historical DBM notes'
unverified backend-join claims are superseded by the new report.
DSM statistics tests exercise actual manual checkpoint APIs and serialized transport, with
broker-less offset observations. Java's discovery test uses an Agent-shaped mock and does
not validate the Agent or its native trace processing. Neither test validates automatic broker
instrumentation, cross-language propagation, numerical DDSketch merging or backend topology.

No inspected result proves authenticated Datadog ingestion, UI workflows, backend joins,
signed remote configuration, full Agent equivalence or production lifecycle/load behavior.
Missing credentials and unexecuted language/framework combinations are documented acceptance
gaps. The proposed shared configuration and product gates are not shipping configuration.
Disabling managed routes does not disable SDK instrumentation, agentless uploads, independent
pipelines or all product fields in shared native traces.
Shared `APM_TRACING` RC activation is explicitly treated as a cross-product design issue:
dropping its entire subscription cannot enforce independent product disablement. The shared
contract forbids rewriting signed configurations and requires a supported RC/SDK boundary.

## Handoff limits

Backend acceptance remains unverified for every product. Access to an approved test
organization, applicable entitlements/credentials and the product-specific broker/database/
framework workloads is required for that acceptance. Some next local checks, especially the
full DBM connector/exporter boundary, do not require credentials and are explicitly listed as
future implementation work rather than misclassified as credential blockers.

CI's original Java experiment did not record its exact JDK; this is an acknowledged provenance
limitation. Source snapshots and executed release versions remain separate throughout.

No requested product or final PR exists yet. The assembled snapshot is reviewable research,
while human-authored PR sections and the human authorship checkbox still gate the requested
publication/readiness workflow. The final PR must use the intended combined branch after
product PR merges and remain draft/open. This publication blocker does not imply product
backend readiness, and passing this research review does not remove either requirement.
