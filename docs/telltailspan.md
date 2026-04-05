# Telltail Span Schema

Schema reference for building an OTel Collector connector that extracts metrics from telltail spans.

## OTLP Span Structure

Each telltail span arrives as a standard OTLP `Span` with metrics encoded as **span events**.

### Span-Level Fields

| Field                  | Source                                     | Example               |
| ---------------------- | ------------------------------------------ | --------------------- |
| `name`                 | Span name passed to `telemetry()`          | `"foo.shape"`         |
| `kind`                 | `span_kind` parameter (default `INTERNAL`) | `SPAN_KIND_INTERNAL`  |
| `start_time_unix_nano` | Auto-captured                              | `1711900000000000000` |
| `end_time_unix_nano`   | Auto-captured                              | `1711900000100000000` |
| `status.code`          | `STATUS_CODE_OK` or `STATUS_CODE_ERROR`    | `STATUS_CODE_OK`      |

### Span Attributes

All telltail-generated attributes are prefixed with `dimensions.`.

| Attribute Key               | Type     | Source Method                  | Description                                               |
| --------------------------- | -------- | ------------------------------ | --------------------------------------------------------- |
| `dimensions.<tag_name>`     | `string` | `.tag(name, value)`            | Low-cardinality dimension. Also present on metric events. |
| `dimensions.<field_name>`   | `string` | `.field(name, value)`          | High-cardinality context. **Not** on metric events.       |
| `dimensions.<metric_name>`  | `double` | `.gauge()` / `.distribution()` | Last recorded value for the measurement.                  |
| `dimensions.<counter_name>` | `double` | `.incr()`                      | Running total for the counter across the span.            |
| `dimensions.count`          | `double` | Auto                           | Always `1.0`.                                             |
| `dimensions.duration`       | `double` | Auto                           | Span duration in seconds (float).                         |

### Metric Events

Metric events have:

- **Event name**: `"metric"` (literal string — use this to filter)
- **Event `time_unix_nano`**: timestamp when the measurement was recorded

#### Event Attributes

| Attribute      | Type     | Required    | Description                                                           |
| -------------- | -------- | ----------- | --------------------------------------------------------------------- |
| `metric.name`  | `string` | yes         | Fully qualified metric name: `{namespace}.{span_name}.{metric_name}`  |
| `metric.type`  | `string` | yes         | Either `"histogram"` or `"counter"`                                   |
| `metric.value` | `double` | yes         | The numeric value                                                     |
| _tag keys_     | `string` | varies      | All tags from `.tag()` calls, flattened as top-level event attributes |
| `status`       | `string` | on counters | `"ok"` or `"error"` — added automatically to counter events           |

### Metric Name Format

```
{metrics_namespace}.{sanitized_span_name}.{sanitized_metric_name}
```

- Default `metrics_namespace`: `"traced"`
- Sanitization: spaces replaced with `_`
- Example: span `"foo.shape"`, metric `"height"` → `"traced.foo.shape.height"`

### Non-Metric Events

Log events use a different event name pattern and should be ignored by the connector:

| Event Name    | Source        | Attributes                   |
| ------------- | ------------- | ---------------------------- |
| `log.info`    | `.info(msg)`  | `message`, `level`, `source` |
| `log.warning` | `.warn(msg)`  | `message`, `level`, `source` |
| `log.error`   | `.error(msg)` | `message`, `level`, `source` |

## Connector Logic

For each span in the incoming trace batch:

```
for each span in traces:
    for each event in span.events:
        if event.name != "metric":
            continue

        name  = event.attributes["metric.name"]   // string
        type  = event.attributes["metric.type"]    // "histogram" | "counter"
        value = event.attributes["metric.value"]   // double

        // Remaining attributes are metric dimensions (tags)
        tags = event.attributes - {"metric.name", "metric.type", "metric.value"}

        if type == "histogram":
            emit Histogram data point:
                name:       name
                value:      value
                attributes: tags
                timestamp:  event.time_unix_nano
                temporality: DELTA

        if type == "counter":
            emit Sum data point:
                name:       name
                value:      value
                attributes: tags
                timestamp:  event.time_unix_nano
                temporality:    DELTA
                is_monotonic:   true
```

## Example: Full Span Payload

Given this application code:

```python
with telemetry("foo.shape", tags={"env": "dev", "team": "example"},
               emit_metrics_as_events=True) as span:
    span.tag("shape", "square")
    span.tag("color", "blue")
    span.field("label", "xyz")
    span.gauge("height", 42.0)
    span.distribution("width", 13.0)
    span.incr("very_wide")
    span.info("rendered shape")
```

The resulting OTLP span contains:

```
Span:
  name: "foo.shape"
  status: OK
  attributes:
    dimensions.env:       "dev"
    dimensions.team:      "example"
    dimensions.shape:     "square"
    dimensions.color:     "blue"
    dimensions.label:     "xyz"        # field — high cardinality
    dimensions.height:    42.0
    dimensions.width:     13.0
    dimensions.very_wide: 1.0
    dimensions.count:     1.0
    dimensions.duration:  0.105        # actual elapsed seconds

  events:
    # --- metric events (connector should process these) ---
    - name: "metric"
      attributes:
        metric.name:  "traced.foo.shape.height"
        metric.type:  "histogram"
        metric.value: 42.0
        env:          "dev"
        team:         "example"
        shape:        "square"
        color:        "blue"

    - name: "metric"
      attributes:
        metric.name:  "traced.foo.shape.width"
        metric.type:  "histogram"
        metric.value: 13.0
        env:          "dev"
        team:         "example"
        shape:        "square"
        color:        "blue"

    - name: "metric"
      attributes:
        metric.name:  "traced.foo.shape.very_wide"
        metric.type:  "counter"
        metric.value: 1.0
        env:          "dev"
        team:         "example"
        shape:        "square"
        color:        "blue"
        status:       "ok"

    - name: "metric"
      attributes:
        metric.name:  "traced.foo.shape.duration"
        metric.type:  "histogram"
        metric.value: 0.105
        env:          "dev"
        team:         "example"
        shape:        "square"
        color:        "blue"

    - name: "metric"
      attributes:
        metric.name:  "traced.foo.shape.count"
        metric.type:  "counter"
        metric.value: 1.0
        env:          "dev"
        team:         "example"
        shape:        "square"
        color:        "blue"
        status:       "ok"

    # --- log event (connector should SKIP) ---
    - name: "log.info"
      attributes:
        message: "rendered shape"
        level:   "info"
        source:  "foo.shape"
```

## Head-Based Sampling Behavior

When head-based sampling is enabled (e.g. `TraceIdRatioBased`), some spans are
dropped by the SDK (`NonRecordingSpan` where `add_event()` is a no-op).
telltail handles this transparently — **no metrics are ever lost, and no Meter
pipeline is used in events mode**.

### Flow

| Span state                      | Metric path                                   |
| ------------------------------- | --------------------------------------------- |
| **Sampled in** (recording)      | Metrics encoded as span events on this span   |
| **Sampled out** (not recording) | Metrics buffered in a **thread-local buffer** |

Buffered metrics from sampled-out spans are **drained onto the next recording
span** in the same thread. This means a recording span may carry metric events
from earlier, different spans — the `metric.name` prefix will differ from the
span's own name.

### Safety valve: synthetic flush span

If no recording span arrives within 30 seconds (configurable via
`_MetricEventBuffer.DEFAULT_FLUSH_TIMEOUT`), telltail creates a **synthetic
span** named `telltail.metric_flush` and flushes all buffered metric events
onto it. This span flows through the normal traces export pipeline.

If the synthetic span is also sampled out, the events are re-buffered and
retried on the next opportunity.

### Connector implications

1. **Process all `"metric"` events regardless of span name.** A span may carry
   metric events from other spans (buffered flush). The `metric.name` attribute
   is authoritative, not the span name.

2. **Handle `telltail.metric_flush` spans.** These are synthetic carrier spans
   with no semantic meaning — they exist only to transport buffered metric
   events. They have no meaningful attributes or duration. The connector should
   extract their metric events and discard the span itself.

### Example: flush span payload

```
Span:
  name: "telltail.metric_flush"
  events:
    - name: "metric"
      attributes:
        metric.name:  "traced.foo.shape.height"
        metric.type:  "histogram"
        metric.value: 42.0
        env:          "dev"
        color:        "blue"

    - name: "metric"
      attributes:
        metric.name:  "traced.foo.shape.count"
        metric.type:  "counter"
        metric.value: 1.0
        env:          "dev"
        color:        "blue"
        status:       "ok"
```

## OTel Collector Pipeline

All telemetry (including metrics from sampled-out spans) flows through the
traces pipeline. The connector extracts metrics from span events.

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

connectors:
  telltail_metrics:
    # Extract metrics from "metric" span events
    # Must handle both app spans and telltail.metric_flush spans

exporters:
  otlp/traces:
    endpoint: datadog-agent:4317
  otlp/metrics:
    endpoint: datadog-agent:4317

service:
  pipelines:
    traces/in:
      receivers: [otlp]
      exporters: [telltail_metrics, otlp/traces]
    metrics/derived:
      receivers: [telltail_metrics]
      exporters: [otlp/metrics]
```
