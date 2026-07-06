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

## Memory bounds

Because the exporter aggregates and correlates signals in memory per flush
window, its accumulation and egress-retry buffers are explicitly bounded so an
intake slowdown or outage degrades gracefully (drop-oldest / drop-new with a
throttled warning) instead of growing without limit. All caps live under `wide:`
and default to non-zero values; `0` means "unbounded" for the accumulation caps
and "no buffering" for the retry buffer.

| Setting | Default | Meaning |
| --- | --- | --- |
| `wide.max_sampled_rows` | `100000` | Max sampled rows retained per flush window (drop-new). |
| `wide.max_aggregate_buckets` | `100000` | Max distinct aggregate buckets (dimension cardinality) per window (drop-new; existing buckets keep aggregating). |
| `wide.max_schemas` | `10000` | Max distinct table identities tracked per window (drop-new). |
| `wide.max_retry_buffer_bytes` | `33554432` | Max bytes of serialized envelopes held for retry after a failed intake send (drop-oldest). |

> Note: `sending_queue` and `retry_on_failure` govern ingestion into the
> aggregation window — they wrap the consume path, which returns before any wide
> intake POST. They do **not** retry the intake delivery. Wide-intake egress
> durability is governed solely by `wide.max_retry_buffer_bytes`.
