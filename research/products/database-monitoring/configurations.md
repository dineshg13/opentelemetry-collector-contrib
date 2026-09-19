# DBM configuration ownership

## Current native trace route (example, full export not executed)

These are existing Collector component fields, not a new DBM collection feature.
The prototype validates the receiver and mapping-helper boundary only. In particular,
it does not validate the following full connector/exporter configuration against intake.

```yaml
receivers:
  datadog:
    endpoint: 127.0.0.1:8126
processors:
  transform/dbm_resource:
    trace_statements:
      - context: span
        statements:
          - set(attributes["resource.name"], attributes["dd.span.Resource"]) where attributes["dd.span.Resource"] != nil
connectors:
  datadog/connector:
exporters:
  datadog:
    api:
      key: ${env:DD_API_KEY}
service:
  pipelines:
    traces/native:
      receivers: [datadog]
      processors: [transform/dbm_resource]
      exporters: [datadog/connector, datadog]
    metrics/apm:
      receivers: [datadog/connector]
      exporters: [datadog]
```

Keep `receiver.datadogreceiver.Enable128BitTraceID` enabled (current default). Configure
stats computation before any tail sampling. Confirm `resource.name` policy with the
shared trace team before rollout; this example bridges the demonstrated native-resource
mapping gap, not every possible DBM field or semantic-convention combination.

SDK process configuration (all three languages recognize the mode; driver support
differs as described in the report):

```sh
DD_TRACE_AGENT_URL=http://127.0.0.1:8126
DD_DBM_PROPAGATION_MODE=full
DD_SERVICE=checkout
DD_ENV=research
DD_VERSION=1
```

For the Python experiment specifically, add `DD_TRACE_API_VERSION=v0.4`,
`DD_REMOTE_CONFIGURATION_ENABLED=false`, `DD_INSTRUMENTATION_TELEMETRY_ENABLED=false`
and `DD_TRACE_SAMPLE_RATE=1`. The Collector cannot set these inside a running SDK.

## Existing forwarder (executed)

An optional public proxy in front of the receiver uses the current extension schema:

```yaml
extensions:
  http_forwarder/dbm_trace_transport:
    ingress:
      endpoint: 127.0.0.1:8127
    egress:
      endpoint: http://127.0.0.1:8126
service:
  extensions: [http_forwarder/dbm_trace_transport]
```

Point the SDK at port 8127 if using this layout. This single egress relays normal APM
paths and responses. It needs no Datadog backend API key because the destination is
the local receiver. Backend auth is exporter-owned. The executable generates the
equivalent extension subtree with ephemeral ports, never binds port 8126 globally.
The current forwarder accepts all paths; it is not a per-product opt-out policy engine.

## Agent collection stays separate

Illustrative Agent `conf.d/postgres.d/conf.yaml`, not executed without a database:

```yaml
init_config:
instances:
  - dbm: true
    host: database.local
    port: 5432
    username: datadog_monitor
    password: ENC[db_monitor_password]
```

Resolve the monitoring password through an appropriately configured Agent secret
backend. Configure DB-specific read privileges, statistics extensions and explain
helpers according to the check's version and desired sample/plan features; this
research does not execute grants or configure a database. Global Agent API/site and
`database_monitoring.{samples,metrics,activity}.*` configure authenticated DBM intake
separately. Do not confuse the SDK's APM destination with the Agent's database endpoint.

## Future extension stanza (proposal, rejected by current schema)

```yaml
extensions:
  datadog:
    products:
      database_monitoring:
        enabled: false
        modes: [correlation_only]
        pipelines:
          correlation_only: traces/native
```

This is a partial stanza to combine with the canonical shared proposal, not a runnable
current config. Default/absent is disabled. When enabled, fail startup for missing or
incompatible pipelines, a disabled required 128-bit ID path, unavailable receiver/exporter
components, or unsupported `collection` mode. No SQL credentials belong in this entry.
It does not create the trace pipeline, start database collection, configure SDK mode,
or globally remove DBM fields from separately configured mixed trace traffic.

To stop SDK comment injection use `DD_DBM_PROPAGATION_MODE=disabled`; to stop Agent
DBM collection disable its check's `dbm` option and any separately enabled features.
Stop/remove the forwarder and independent proprietary trace/export components if those
paths should also be absent. Product off, transport off and database collection off
are distinct controls.
