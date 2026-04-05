# Datadog Exporter Breaking Changes (2024–2026)

This document catalogs breaking changes in the Datadog exporter ecosystem across
both **opentelemetry-collector-contrib** and **datadog-agent** over approximately
the last two years. It is intended as a migration reference for teams upgrading
their Datadog pipeline.

---

## Collector-Contrib Breaking Changes

Changes in the `exporter/datadog`, `connector/datadog`, and related packages
within [opentelemetry-collector-contrib](https://github.com/open-telemetry/opentelemetry-collector-contrib).

| # | Contrib Version | Commit | PR | Description |
|---|-----------------|--------|----|-------------|
| 1 | v0.110.0 | `8a29fa4548` | — | **Remove `connector.datadogconnector.performance` feature gate.** The performance-tuning gate is removed; the optimized path is now the only path. |
| 2 | v0.116.0 | `d0f4d09549` | — | **Stop prefixing HTTP metrics.** HTTP client metrics emitted by the exporter no longer carry the `datadog.` prefix, aligning with OTel semantic conventions. Dashboards or alerts keyed on the old metric names will break. |
| 3 | v0.118.0 | `34914ece2b` | — | **Remove feature gate `exporter.datadog.hostname.preview`.** The preview hostname-resolution logic is now the default; the gate can no longer be used to toggle behavior. |
| 4 | v0.119.0 | `eb9d654a36` | — | **Remove connector `TracesConfig` struct.** The `TracesConfig` type is deleted from the connector's public API. Any code importing or embedding this struct must be updated. |
| 5 | v0.122.0 | `d9dd32d7dc` | — | **Metric export serializer promoted to beta.** The new serializer becomes the default. Users who relied on the alpha serializer's quirks may see changed metric payloads. |
| 6 | v0.123.0 | `cd34262019` | — | **Remove deprecated config fields.** Several long-deprecated configuration keys are deleted. Configs referencing them will fail to unmarshal. |
| 7 | v0.129.0 | `fee4bb8c7e` | — | **Logs agent exporter graduation / old logs exporter removal.** The legacy logs export path is removed. All log pipelines must use the new logs-agent-based exporter. |
| 8 | v0.135.0 | `8c98af2417` | — | **Zorkian API removal.** The `zorkian` (v1) Datadog API client dependency and all code paths using it are deleted. Only the `datadog-api-client-go` v2 client remains. |
| 9 | v0.137.0 | `d5c0a92198` | — | **Remove `logs::dump_payloads` config option.** The debug config field for dumping raw log payloads is removed. Use collector-level debug logging instead. |
| 10 | v0.137.0 | `f00dd6ca60` | — | **Mark connector `NativeIngest` as stable.** The `NativeIngest` feature gate is locked to enabled and can no longer be disabled. |
| 11 | v0.148.0 | `743cc79059` | — | **`datadogsemantics` processor removal.** The standalone semantics processor is deleted; its logic is folded into the exporter/connector directly. |
| 12 | v0.148.0 | `dc54b82e9f` | — | **Feature gates moved to `pkg/datadog`.** Several feature-gate registrations are relocated from exporter/connector packages to the shared `pkg/datadog` module, changing their fully-qualified gate IDs. |

### New / Graduated Feature Gates

These feature gates were introduced or promoted during the same window and may
alter default behavior on upgrade:

| Gate | Introduced | Status | Effect |
|------|-----------|--------|--------|
| `exporter.datadogexporter.metricremappingdisabled` | — | Alpha | **Deprecated.** Original gate to disable OTel→Datadog metric name remapping. Use `DisableAllMetricRemapping` instead. |
| `exporter.datadogexporter.DisableAllMetricRemapping` | — | Alpha | Supersedes `metricremappingdisabled`. Disables **all** automatic OTel-to-Datadog metric name remapping, including system and runtime metrics. When enabled, metric names pass through unchanged. |
| `exporter.datadogexporter.InferIntervalForDeltaMetrics` | — | Alpha | Automatically infers the aggregation interval for OTLP delta sums using a heuristic instead of requiring explicit configuration. |
| `exporter.datadogexporter.EnableAttributeSliceMultiTagExporting` | v0.142.0 | Alpha | Exports slice-valued resource attributes as multiple Datadog `key:value` tags rather than a single JSON-encoded tag. |
| `datadog.EnableReceiveResourceSpansV2` | v0.118.0 | Beta | Refactored span processing implementation in exporter/connector with ~10% performance improvement. |
| `datadog.EnableOperationAndResourceNameV2` | v0.118.0 | Beta | Improved logic for computing span operation name and resource name in exporter/connector. |
| `receiver.datadogreceiver.EnableMultiTagParsing` | v0.142.0 | Alpha | Parses `key:value` tags with duplicate keys into a slice attribute (receiver side). |

---

## OTel → Datadog Attribute Mapping Reference

The canonical attribute translation logic lives in
[`opentelemetry-mapping-go`](https://github.com/DataDog/opentelemetry-mapping-go)
`pkg/otlp/attributes/` (v0.27.0), consumed by the exporter, connector, and
receiver. The receiver performs the **reverse** mapping (Datadog → OTel) in
`receiver/datadogreceiver/internal/translator/tags.go`.

### Unified Service Tags (env / service / version)

These are the highest-impact mappings — they drive Datadog's service catalog.

| OTel Attribute | Datadog Tag | Notes |
|---|---|---|
| `deployment.environment.name` | `env` | Primary; semconv v1.27+ |
| `deployment.environment` | `env` | Legacy fallback (deprecated semconv) |
| `service.name` | `service` | |
| `service.version` | `version` | |
| `tags.datadoghq.com/env` | `env` | K8s label override (highest priority) |
| `tags.datadoghq.com/service` | `service` | K8s label override |
| `tags.datadoghq.com/version` | `version` | K8s label override |

**Note:** A raw `env` resource attribute in OTLP payloads has **never** been
supported as a mapping source. The supported path has always been through OTel
semantic conventions (`deployment.environment`, then `deployment.environment.name`).
The short-lived `datadog.env` attribute (Feb 2025 – Feb 2026) was a different
mechanism — see "The `datadog.*` Namespace Experiment" below.

### Container

| OTel Attribute | Datadog Tag |
|---|---|
| `container.id` | `container_id` |
| `container.name` | `container_name` |
| `container.image.name` | `image_name` |
| `container.image.tags` | `image_tag` |
| `container.runtime` | `runtime` |

### Cloud

| OTel Attribute | Datadog Tag |
|---|---|
| `cloud.provider` | `cloud_provider` |
| `cloud.region` | `region` |
| `cloud.availability_zone` | `zone` |

### AWS ECS

| OTel Attribute | Datadog Tag |
|---|---|
| `aws.ecs.task.family` | `task_family` |
| `aws.ecs.task.arn` | `task_arn` |
| `aws.ecs.cluster.arn` | `ecs_cluster_name` |
| `aws.ecs.task.revision` | `task_version` |
| `aws.ecs.container.arn` | `ecs_container_name` |

### Kubernetes

| OTel Attribute | Datadog Tag |
|---|---|
| `k8s.container.name` | `kube_container_name` |
| `k8s.cluster.name` | `kube_cluster_name` |
| `k8s.deployment.name` | `kube_deployment` |
| `k8s.replicaset.name` | `kube_replica_set` |
| `k8s.statefulset.name` | `kube_stateful_set` |
| `k8s.daemonset.name` | `kube_daemon_set` |
| `k8s.job.name` | `kube_job` |
| `k8s.cronjob.name` | `kube_cronjob` |
| `k8s.namespace.name` | `kube_namespace` |
| `k8s.pod.name` | `pod_name` |

### K8s Standard Labels

| OTel Attribute | Datadog Tag |
|---|---|
| `app.kubernetes.io/name` | `kube_app_name` |
| `app.kubernetes.io/instance` | `kube_app_instance` |
| `app.kubernetes.io/version` | `kube_app_version` |
| `app.kubernetes.io/component` | `kube_app_component` |
| `app.kubernetes.io/part-of` | `kube_app_part_of` |
| `app.kubernetes.io/managed-by` | `kube_app_managed_by` |

### HTTP (receiver reverse-mapping, semconv v1.38+)

The receiver maps Datadog tag names back to current OTel semconv. The "Pre-v1.30"
column shows the old OTel attribute that was used before the semconv bump.

| Datadog Tag | OTel (current, v1.38) | OTel (pre-v1.30) |
|---|---|---|
| `http.client_ip` | `client.address` | — |
| `http.response.content_length` | `http.response.body.size` | — |
| `http.status_code` | `http.response.status_code` | `http.status_code` |
| `http.method` | `http.request.method` | `http.method` |
| `http.url` | `url.full` | `http.url` |
| `http.useragent` | `user_agent.original` | — |
| `http.server_name` | `server.address` | — |
| `http.route` | `http.route` | `http.route` |
| `http.version` | `network.protocol.version` | — |

### Database (receiver reverse-mapping, semconv v1.38+)

| Datadog Tag | OTel (current, v1.38) | OTel (pre-v1.30) |
|---|---|---|
| `db.type` | `db.system.name` | `db.type` |
| `db.operation` | `db.operation.name` | `db.operation` |
| `db.instance` | `db.namespace` | `db.instance` |
| `db.sql.table` | `db.collection.name` | — |
| `db.statement` | `db.query.text` | `db.statement` |

### Hostname Resolution Priority

The exporter resolves the Datadog hostname using this priority chain
(from `opentelemetry-mapping-go` `pkg/otlp/attributes/source.go`):

1. `datadog.host.name` — custom Datadog override (highest priority)
2. AWS ECS Fargate: `aws.ecs.task.arn` (when `aws.ecs.launch_type == "fargate"`)
3. AWS EC2: `host.id` (EC2 instance ID)
4. GCP: `host.name` + `cloud.account.id` → `hostname.project_id`
5. Azure: `host.id` (VM ID), fallback `host.name`
6. Kubernetes: `k8s.node.name` (optionally suffixed with cluster name)
7. Generic: `host.id`
8. Fallback: `host.name`

### Special Attributes

- `datadog.container.tag.*` — extracted as custom container tags
- `ec2.tag.*` — extracted as EC2 instance tags
- `container.id` / `k8s.pod.uid` — used as origin ID for container tagging

---

## Attribute & Semantic Convention Breaking Changes

Chronological list of attribute-mapping and semantic convention changes that may
require migration action.

| Date | PR | Change | Impact |
|---|---|---|---|
| 2024-09-12 | #35147 | `deployment.environment.name` support added | New semconv v1.27 attribute now maps to `env` alongside the deprecated `deployment.environment`. No action unless you need to stop sending the old attribute. |
| 2024-09-16 | #35025 | `metricremappingdisabled` feature gate introduced | Alpha gate to disable OTel→Datadog metric name remapping. Internal use only. |
| 2024-09-24 | #35269 | Connector semconv bump v1.17 → v1.27 | Connector adopts `deployment.environment.name`; `deployment.environment` still accepted as fallback. |
| 2025-02-27 | agent #33753 | Agent adds `datadog.*` namespace attributes | Agent OTLP receiver now checks `datadog.env`, `datadog.service`, `datadog.version` etc. as primary source, falling back to OTel semconv. Config option `IgnoreMissingDatadogFields` added. |
| 2025-03-12 | #36918 | `datadogsemanticsprocessor` introduced | New processor produces `datadog.*` attributes from OTel conventions (e.g. `deployment.environment.name` → `datadog.env`). Designed as the companion to the agent's `datadog.*` consumption. |
| 2025-04-02 | #39069 | `host_metadata::first_resource` deprecated | Users should migrate to new host metadata approach. |
| 2025-05-14 | #39678 | Receiver semconv bump to v1.30 | HTTP/DB attribute names changed in receiver reverse-mapping (see tables above). `http.status_code` → `http.response.status_code`, `db.type` → `db.system.name`, etc. |
| 2025-12-11 | #44859 | `EnableAttributeSliceMultiTagExporting` gate added (v0.142.0) | Slice attributes export as multiple tags instead of JSON. |
| 2025-12-16 | #44990 | Global semconv bump to v1.38.0 | All components updated; attribute key constants may change. |
| 2026-02-06 | agent #45833 | Agent removes all `datadog.*` namespace attributes | All `KeyDatadog*` constants (`datadog.env`, `datadog.service`, etc.) and `IgnoreMissingDatadogFields` deleted from agent v7.77. `GetOTelEnv()` now only checks standard OTel semconv via semantic registry. |
| 2026-02-10 | #45943 | `DisableAllMetricRemapping` gate added | Supersedes `metricremappingdisabled`; covers system and runtime metrics too. |
| 2026-02-12 | #46052 | `datadogsemanticsprocessor` deprecated | Never reached stable; `datadog.*` namespace removed from agent side. |
| 2026-03-13 | #46893 | `datadogsemanticsprocessor` removed (v0.148.0) | Processor deleted. The `datadog.*` namespace experiment is fully unwound. |

### The `datadog.*` Namespace Experiment (Feb 2025 – Feb 2026)

For roughly one year, the agent and contrib experimented with a `datadog.*`
attribute namespace as an intermediary between OTel semantic conventions and
Datadog's internal representation. This experiment never reached stable and was
fully removed.

**How it worked (two-step pipeline):**

1. **`datadogsemanticsprocessor`** (contrib side) transformed OTel conventions
   into `datadog.*` attributes on the OTLP payload:
   - `deployment.environment.name` → `datadog.env`
   - `service.name` → `datadog.service` (fallback: `"otlpresourcenoservicename"`)
   - `service.version` → `datadog.version`
   - `vcs.ref.head.revision` → `git.commit.sha`
   - `vcs.repository_url` → `git.repository_url` (protocol stripped)
   - Span-level: `datadog.name`, `datadog.resource`, `datadog.type`, `datadog.span.kind`
   - Error: `datadog.error`, `datadog.error.msg`, `datadog.error.type`, `datadog.error.stack`
   - HTTP: `datadog.http_status_code`

2. **Agent OTLP receiver** checked `datadog.env` (and other `datadog.*` keys)
   **first**, then fell back to standard OTel semconv if the config option
   `IgnoreMissingDatadogFields` was false (the default).

**All `datadog.*` constants that were defined and removed:**
`datadog.env`, `datadog.service`, `datadog.version`, `datadog.container_id`,
`datadog.name`, `datadog.resource`, `datadog.type`, `datadog.span.kind`,
`datadog.error`, `datadog.error.msg`, `datadog.error.type`, `datadog.error.stack`,
`datadog.http_status_code`

**Lifecycle:**

| Date | Event | Repo | PR / Commit |
|---|---|---|---|
| Feb 27, 2025 | `datadog.*` constants and `IgnoreMissingDatadogFields` config introduced | agent | #33753 (`bbb4fb6e11`) |
| Mar 12, 2025 | `datadogsemanticsprocessor` introduced (produces `datadog.*` attrs) | contrib | #36918 (`feecafa203`) |
| May 13, 2025 | Processor enhanced: populates `datadog.*` fields even when blank | contrib | #39596 (`4bc0aab647`) |
| Feb 6, 2026 | All `KeyDatadog*` constants and `IgnoreMissingDatadogFields` removed | agent | #45833 (`75b84fca83`) — agent v7.77 |
| Feb 12, 2026 | `datadogsemanticsprocessor` deprecated | contrib | #46052 |
| Mar 13, 2026 | `datadogsemanticsprocessor` removed | contrib | #46893 (`743cc79059`) — contrib v0.148.0 |

**Current state:** The agent now uses a semantic registry
(`pkg/trace/semantics/mappings.json`) that resolves `deployment.environment.name`
→ `deployment.environment` in precedence order, with no `datadog.*` intermediary.
The `GetOTelEnv()` function only checks standard OTel semconv.

---

## Datadog Agent Breaking Changes

Changes in the **datadog-agent** repository that affect the OTel collector
integration surface (OTLP ingest pipeline, shared libraries consumed by
collector-contrib).

| # | Agent Version | Commit | PR | Description |
|---|---------------|--------|----|-------------|
| 1 | v7.75 | `2b6ebeb278` | — | **`HTTPClientFunc` removal.** The `HTTPClientFunc` type used to inject custom HTTP clients into the OTLP pipeline is deleted. Callers must use the new `http.Client`-based configuration API. |
| 2 | v7.76 | `da72e32a2b` | — | **OTLP span error mapping simplification.** The span-status-to-error mapping logic is simplified; edge cases that previously mapped `Unset` status to errors no longer do so. |
| 3 | v7.76 | `c849643269` | — | **Config `UnmarshalKey` API removal.** The `UnmarshalKey` helper on the agent config object is removed. Consumers must use the standard `Unmarshal` path. |
| 4 | v7.77 | `75b84fca83` | #45833 | **Removal of `datadog.*` OTLP namespace attributes.** All `KeyDatadog*` constants (`datadog.env`, `datadog.service`, `datadog.version`, `datadog.container_id`, etc.) and the `IgnoreMissingDatadogFields` config option are deleted. The agent OTLP receiver now relies solely on standard OTel semantic conventions. See "The `datadog.*` Namespace Experiment" section below. |
| 5 | v7.77 | `857a6f8efe` | — | **New v1.0 trace payload format.** The trace payload serialization switches to a v1.0 wire format. Downstream consumers expecting the legacy format will fail to decode. |
| 6 | v7.77 | `0bdd0963d1` | — | **Protobuf library migration.** Internal protobuf usage migrates from `github.com/golang/protobuf` to `google.golang.org/protobuf`. Any code sharing proto messages across the boundary must use the new library. |
| 7 | v7.77 | `1e51c3d1dd` | — | **Container tags v2 feature gate.** A new container-tag resolution path becomes the default. The v1 tag provider is removed. |
| 8 | v7.77 | `b85706485a` | — | **Semantics library integration.** A shared `semantics` library is introduced and replaces ad-hoc attribute mapping scattered across packages. Import paths change. |
| 9 | v7.77 | `29ab9b1658` | — | **OTel module isolation / refactoring.** The `comp/otelcol` module is restructured with stricter internal boundaries. Several previously-public types become internal. |
| 10 | v7.77 | `6d49aaf084` | — | **OTel utility function relocation.** Helper functions in `pkg/otlp/…` are moved to new package paths. Old import paths no longer resolve. |
| 11 | v7.77 | `e558c062c6` | — | **`TracerMetadata` struct relocation.** The `TracerMetadata` type moves from its original package to a new home under the trace pipeline module. |

---

## Cross-Reference: Agent ↔ Contrib Version Alignment

The collector-contrib Datadog packages pin specific datadog-agent module
versions. The table below maps the approximate contrib release to the agent
module version it consumes, highlighting where agent-side breaks land in
contrib.

| Contrib Version | Agent Module Version | Notable Agent Breaks Absorbed |
|-----------------|---------------------|-------------------------------|
| v0.135.0 – v0.137.0 | ~v7.75 | `HTTPClientFunc` removal |
| v0.138.0 – v0.144.0 | ~v7.76 | Span error mapping simplification, `UnmarshalKey` removal |
| v0.145.0 – v0.148.0 | ~v7.77 | `datadog.*` attribute removal, v1.0 trace format, protobuf migration, container tags v2, module refactoring |

---

## Migration Guidance

1. **Search configs for removed fields.** Grep your collector YAML for
   `dump_payloads`, `hostname.preview`, and any keys flagged in v0.123.0
   deprecation notices.

2. **Update dashboards and monitors.** If you rely on `datadog.`-prefixed HTTP
   metrics (removed in v0.116.0) or `datadog.*` resource attributes (removed in
   agent v7.77), update queries before upgrading.

3. **Pin or test feature gates.** Gates like `DisableAllMetricRemapping` change
   metric names globally. Test with `--feature-gates=-<gate>` to compare
   before/after in staging. Note: the correct gate ID is
   `exporter.datadogexporter.DisableAllMetricRemapping`, not `exporter.datadog.metrics.*`.

4. **Rebuild custom components.** If you import collector-contrib Datadog
   packages in custom builds, watch for the `TracesConfig` removal (v0.119.0),
   Zorkian API deletion (v0.135.0), and `pkg/datadog` gate relocation
   (v0.148.0).

5. **Validate trace payloads.** The agent v7.77 v1.0 trace format is a wire-level
   break. Ensure any custom trace consumers can handle the new format before
   upgrading the agent.

6. **Migrate `deployment.environment` to `deployment.environment.name`.** The
   old `deployment.environment` attribute is deprecated in semconv v1.27+. Both
   still map to the Datadog `env` tag, but you should update your
   instrumentation to use `deployment.environment.name` for forward
   compatibility.

7. **Remove `datadogsemanticsprocessor` and stop relying on `datadog.*`
   attributes.** The processor was removed in contrib v0.148.0 and the agent
   stopped consuming `datadog.*` attributes in v7.77. If your pipeline used the
   processor to produce `datadog.env`, `datadog.service`, or `datadog.version`,
   remove it — the exporter and agent now read `deployment.environment.name`,
   `service.name`, and `service.version` directly. If any custom processors or
   routing rules reference `datadog.env` or other `datadog.*` keys, update them
   to use standard OTel semconv.

8. **Account for HTTP/DB semconv renames in the receiver.** If you upgraded past
   the v1.30 semconv bump and consume data from the Datadog receiver, attribute
   names changed: `http.status_code` → `http.response.status_code`,
   `http.method` → `http.request.method`, `db.type` → `db.system.name`, etc.
   Update any downstream processors or queries accordingly.

9. **Migrate `metricremappingdisabled` to `DisableAllMetricRemapping`.** The
   original `exporter.datadogexporter.metricremappingdisabled` gate is
   deprecated. Switch to `exporter.datadogexporter.DisableAllMetricRemapping`,
   which also covers system and runtime metric remapping.
