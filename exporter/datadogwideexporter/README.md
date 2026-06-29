# Datadog Wide Events Exporter

The Datadog Wide Events exporter receives traces, metrics, and logs and
correlates them in memory before exporting Datadog wide event envelopes.

The exporter follows the same high-level semantic model as the wide-events SDKs:
spans become sampled span rows, exemplar-linked metric points become sampled
metric rows parented to spans, and metric datapoint aggregates become aggregate
metric rows. Aggregate metric rows are never attached to representative spans.

```yaml
exporters:
  datadogwide:
    api:
      key: ${env:DD_API_KEY}
      site: ${env:DD_SITE}
    # Optional override while the public wide intake endpoint is finalized.
    # wide:
    #   endpoint: https://wide-intake.datadoghq.com/api/v2/wide/events

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [datadogwide]
    metrics:
      receivers: [otlp]
      exporters: [datadogwide]
    logs:
      receivers: [otlp]
      exporters: [datadogwide]
```
