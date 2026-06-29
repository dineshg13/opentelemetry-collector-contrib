// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"math"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func (e *wideExporter) tracesToObservations(td ptrace.Traces) ([]spanObservation, []linkedMetricObservation) {
	var spans []spanObservation
	var metrics []linkedMetricObservation

	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		resourceDimensions, resourceAttrs := resourceWideValues(rs.Resource().Attributes())
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			scopeSpans := rs.ScopeSpans().At(j)
			for k := 0; k < scopeSpans.Spans().Len(); k++ {
				span := scopeSpans.Spans().At(k)
				spanMetrics, linked := metricEventsFromSpan(span, resourceDimensions)
				metrics = append(metrics, linked...)
				if span.Name() == metricFlushSpanName || span.Name() == logFlushSpanName {
					continue
				}
				if span.TraceID().IsEmpty() || span.SpanID().IsEmpty() {
					continue
				}
				dimensions := cloneWideValues(resourceDimensions)
				attrs := cloneWideValues(resourceAttrs)
				spanWeight := 1.0
				span.Attributes().Range(func(k string, v pcommon.Value) bool {
					if k == sampleWeightAttribute {
						spanWeight = numericPcommonValue(v, 1)
						return true
					}
					if strings.HasPrefix(k, defaultWideDimensionPrefix+".") {
						dimensions[dimensionName(k)] = typedValueFromPcommon(v)
						return true
					}
					attrs[attributeName(k)] = typedValueFromPcommon(v)
					return true
				})
				sampleProbability := sampleProbabilityForWeight(spanWeight)
				spans = append(spans, spanObservation{
					Ref: spanRef{
						traceID: span.TraceID().String(),
						spanID:  span.SpanID().String(),
					},
					Name:              span.Name(),
					Start:             timestampOrNow(span.StartTimestamp()),
					End:               timestampOrNow(span.EndTimestamp()),
					Sampled:           span.Flags()&1 == 1,
					SampleProbability: sampleProbability,
					Dimensions:        dimensions,
					Attributes:        attrs,
					Metrics:           spanMetrics,
				})
			}
		}
	}
	return spans, metrics
}

func (e *wideExporter) metricsToObservations(md pmetric.Metrics) []linkedMetricObservation {
	var out []linkedMetricObservation
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		rm := md.ResourceMetrics().At(i)
		resourceDimensions, _ := resourceWideValues(rm.Resource().Attributes())
		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			scopeMetrics := rm.ScopeMetrics().At(j)
			for k := 0; k < scopeMetrics.Metrics().Len(); k++ {
				metric := scopeMetrics.Metrics().At(k)
				out = append(out, metricToObservations(metric, resourceDimensions)...)
			}
		}
	}
	return out
}

func (e *wideExporter) logsToObservations(ld plog.Logs) []logObservation {
	var out []logObservation
	for i := 0; i < ld.ResourceLogs().Len(); i++ {
		rl := ld.ResourceLogs().At(i)
		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			scopeLogs := rl.ScopeLogs().At(j)
			for k := 0; k < scopeLogs.LogRecords().Len(); k++ {
				record := scopeLogs.LogRecords().At(k)
				attrs := make(map[string]TypedValue)
				record.Attributes().Range(func(k string, v pcommon.Value) bool {
					attrs[attributeName(k)] = typedValueFromPcommon(v)
					return true
				})
				out = append(out, logObservation{
					Ref: spanRef{
						traceID: idString(record.TraceID()),
						spanID:  idString(record.SpanID()),
					},
					Timestamp:  timestampOrNow(firstTimestamp(record.Timestamp(), record.ObservedTimestamp())),
					Severity:   firstNonEmpty(record.SeverityText(), record.SeverityNumber().String()),
					Body:       record.Body().AsString(),
					Attributes: attrs,
				})
			}
		}
	}
	return out
}

func metricToObservations(metric pmetric.Metric, resourceDimensions map[string]TypedValue) []linkedMetricObservation {
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		points := metric.Gauge().DataPoints()
		out := make([]linkedMetricObservation, 0, points.Len())
		for i := 0; i < points.Len(); i++ {
			out = append(out, numberPointObservation(metric.Name(), metric.Unit(), metricTypeGauge, points.At(i), resourceDimensions))
		}
		return out
	case pmetric.MetricTypeSum:
		points := metric.Sum().DataPoints()
		out := make([]linkedMetricObservation, 0, points.Len())
		for i := 0; i < points.Len(); i++ {
			out = append(out, numberPointObservation(metric.Name(), metric.Unit(), metricTypeCounter, points.At(i), resourceDimensions))
		}
		return out
	case pmetric.MetricTypeHistogram:
		points := metric.Histogram().DataPoints()
		out := make([]linkedMetricObservation, 0, points.Len())
		for i := 0; i < points.Len(); i++ {
			if obs, ok := histogramPointObservation(metric.Name(), metric.Unit(), points.At(i), resourceDimensions); ok {
				out = append(out, obs)
			}
		}
		return out
	case pmetric.MetricTypeExponentialHistogram:
		points := metric.ExponentialHistogram().DataPoints()
		out := make([]linkedMetricObservation, 0, points.Len())
		for i := 0; i < points.Len(); i++ {
			if obs, ok := expHistogramPointObservation(metric.Name(), metric.Unit(), points.At(i), resourceDimensions); ok {
				out = append(out, obs)
			}
		}
		return out
	default:
		return nil
	}
}

func numberPointObservation(name, unit string, typ metricType, point pmetric.NumberDataPoint, resourceDimensions map[string]TypedValue) linkedMetricObservation {
	dimensions := cloneWideValues(resourceDimensions)
	weight := mergeAttributes(dimensions, point.Attributes())
	value := numberDataPointValue(point)
	timestamp := timestampOrNow(point.Timestamp())
	obs := linkedMetricObservation{
		Metric: metricDescriptor{
			Name:       name,
			Type:       typ,
			Unit:       unit,
			Dimensions: dimensions,
		},
		Aggregate: &metricAggregateFact{
			Value:     value,
			Timestamp: timestamp,
			Weight:    weight,
		},
	}
	obs.Samples = exemplarSamples(point.Exemplars(), timestamp, dimensions, value)
	return obs
}

func histogramPointObservation(name, unit string, point pmetric.HistogramDataPoint, resourceDimensions map[string]TypedValue) (linkedMetricObservation, bool) {
	if point.Count() == 0 {
		return linkedMetricObservation{}, false
	}
	dimensions := cloneWideValues(resourceDimensions)
	weight := mergeAttributes(dimensions, point.Attributes())
	timestamp := timestampOrNow(point.Timestamp())
	value := 0.0
	if point.HasSum() {
		value = point.Sum()
	}
	hist := &histogramAggregate{
		Count:          point.Count(),
		Sum:            value,
		BucketCounts:   uint64Slice(point.BucketCounts()),
		ExplicitBounds: float64Slice(point.ExplicitBounds()),
	}
	if point.HasMin() {
		min := point.Min()
		hist.Min = &min
	}
	if point.HasMax() {
		max := point.Max()
		hist.Max = &max
	}
	obs := linkedMetricObservation{
		Metric: metricDescriptor{
			Name:       name,
			Type:       metricTypeHistogram,
			Unit:       unit,
			Dimensions: dimensions,
		},
		Aggregate: &metricAggregateFact{
			Value:            value,
			Timestamp:        timestamp,
			Histogram:        hist,
			HistogramSamples: exemplarValues(point.Exemplars()),
			Weight:           weight,
		},
	}
	obs.Samples = exemplarSamples(point.Exemplars(), timestamp, dimensions, value)
	return obs, true
}

func expHistogramPointObservation(name, unit string, point pmetric.ExponentialHistogramDataPoint, resourceDimensions map[string]TypedValue) (linkedMetricObservation, bool) {
	if point.Count() == 0 {
		return linkedMetricObservation{}, false
	}
	dimensions := cloneWideValues(resourceDimensions)
	weight := mergeAttributes(dimensions, point.Attributes())
	timestamp := timestampOrNow(point.Timestamp())
	value := 0.0
	if point.HasSum() {
		value = point.Sum()
	}
	scale := point.Scale()
	zeroCount := point.ZeroCount()
	zeroThreshold := point.ZeroThreshold()
	hist := &histogramAggregate{
		Count:         point.Count(),
		Sum:           value,
		Scale:         &scale,
		ZeroCount:     &zeroCount,
		ZeroThreshold: &zeroThreshold,
		Positive: &exponentialBuckets{
			Offset: point.Positive().Offset(),
			Counts: uint64Slice(point.Positive().BucketCounts()),
		},
		Negative: &exponentialBuckets{
			Offset: point.Negative().Offset(),
			Counts: uint64Slice(point.Negative().BucketCounts()),
		},
	}
	if point.HasMin() {
		min := point.Min()
		hist.Min = &min
	}
	if point.HasMax() {
		max := point.Max()
		hist.Max = &max
	}
	obs := linkedMetricObservation{
		Metric: metricDescriptor{
			Name:       name,
			Type:       metricTypeHistogram,
			Unit:       unit,
			Dimensions: dimensions,
		},
		Aggregate: &metricAggregateFact{
			Value:            value,
			Timestamp:        timestamp,
			Histogram:        hist,
			HistogramSamples: exemplarValues(point.Exemplars()),
			Weight:           weight,
		},
	}
	obs.Samples = exemplarSamples(point.Exemplars(), timestamp, dimensions, value)
	return obs, true
}

func metricEventsFromSpan(span ptrace.Span, resourceDimensions map[string]TypedValue) ([]metricObservation, []linkedMetricObservation) {
	var samples []metricObservation
	var linked []linkedMetricObservation
	ref := spanRef{traceID: idString(span.TraceID()), spanID: idString(span.SpanID())}
	for i := 0; i < span.Events().Len(); i++ {
		event := span.Events().At(i)
		if event.Name() != metricSampleEventName && event.Name() != metricAggregateEventName && event.Name() != metricLegacyEventName {
			continue
		}
		obs, ok := metricEventObservation(event, resourceDimensions)
		if !ok {
			continue
		}
		if event.Name() == metricSampleEventName {
			if len(obs.Samples) > 0 && ref.valid() && span.Name() != metricFlushSpanName {
				samples = append(samples, metricObservation{Metric: obs.Metric, Samples: []metricSampleFact{obs.Samples[0].Sample}})
			}
			continue
		}
		obs.Samples = nil
		linked = append(linked, obs)
	}
	return samples, linked
}

func metricEventObservation(event ptrace.SpanEvent, resourceDimensions map[string]TypedValue) (linkedMetricObservation, bool) {
	var (
		name   string
		typ    metricType
		value  float64
		weight = 1.0
	)
	dimensions := cloneWideValues(resourceDimensions)
	event.Attributes().Range(func(k string, v pcommon.Value) bool {
		switch k {
		case "metric.name":
			name = v.AsString()
		case "metric.type":
			typ = metricType(v.AsString())
		case "metric.value":
			value = numericPcommonValue(v, 0)
		case metricSampleWeightAttr:
			weight = numericPcommonValue(v, 1)
		default:
			if strings.HasPrefix(k, "metric.") {
				return true
			}
			dimensions[dimensionName(k)] = typedValueFromPcommon(v)
		}
		return true
	})
	if name == "" || typ == "" || !isFinite(value) {
		return linkedMetricObservation{}, false
	}
	timestamp := timestampOrNow(event.Timestamp())
	obs := linkedMetricObservation{
		Metric: metricDescriptor{
			Name:       name,
			Type:       typ,
			Dimensions: dimensions,
		},
		Aggregate: &metricAggregateFact{
			Value:     value,
			Timestamp: timestamp,
			Weight:    weight,
		},
	}
	if typ == metricTypeHistogram {
		count := uint64(1)
		if v, ok := event.Attributes().Get("metric.aggregate.count"); ok {
			if parsed := numericPcommonValue(v, 0); parsed > 0 {
				count = uint64(parsed)
			}
		}
		obs.Aggregate.Histogram = &histogramAggregate{Count: count, Sum: value}
		obs.Aggregate.HistogramSamples = []float64{value}
	}
	obs.Samples = []linkedMetricSample{{
		Sample: metricSampleFact{
			Value:      value,
			Timestamp:  timestamp,
			Dimensions: dimensions,
			Weight:     weight,
		},
	}}
	return obs, true
}

func exemplarSamples(exemplars pmetric.ExemplarSlice, fallbackTimestamp time.Time, baseDimensions map[string]TypedValue, fallbackValue float64) []linkedMetricSample {
	out := make([]linkedMetricSample, 0, exemplars.Len())
	for i := 0; i < exemplars.Len(); i++ {
		exemplar := exemplars.At(i)
		if exemplar.TraceID().IsEmpty() || exemplar.SpanID().IsEmpty() {
			continue
		}
		dimensions := cloneWideValues(baseDimensions)
		mergeAttributes(dimensions, exemplar.FilteredAttributes())
		value := exemplarValue(exemplar)
		if !isFinite(value) {
			value = fallbackValue
		}
		out = append(out, linkedMetricSample{
			Span: spanRef{
				traceID: exemplar.TraceID().String(),
				spanID:  exemplar.SpanID().String(),
			},
			Sample: metricSampleFact{
				Value:      value,
				Timestamp:  timestampOrDefault(exemplar.Timestamp(), fallbackTimestamp),
				Dimensions: dimensions,
				Weight:     1,
			},
		})
	}
	return out
}

func exemplarValues(exemplars pmetric.ExemplarSlice) []float64 {
	values := make([]float64, 0, exemplars.Len())
	for i := 0; i < exemplars.Len(); i++ {
		value := exemplarValue(exemplars.At(i))
		if isFinite(value) {
			values = append(values, value)
		}
	}
	return values
}

func resourceWideValues(attrs pcommon.Map) (map[string]TypedValue, map[string]TypedValue) {
	dimensions := make(map[string]TypedValue)
	attributes := make(map[string]TypedValue)
	attrs.Range(func(k string, v pcommon.Value) bool {
		value := typedValueFromPcommon(v)
		dimensions[dimensionName(k)] = value
		attributes[attributeName(k)] = value
		return true
	})
	return dimensions, attributes
}

func mergeAttributes(dst map[string]TypedValue, attrs pcommon.Map) float64 {
	weight := 1.0
	attrs.Range(func(k string, v pcommon.Value) bool {
		if k == metricSampleWeightAttr || k == sampleWeightAttribute {
			weight = numericPcommonValue(v, 1)
			return true
		}
		dst[dimensionName(k)] = typedValueFromPcommon(v)
		return true
	})
	return weight
}

func typedValueFromPcommon(value pcommon.Value) TypedValue {
	switch value.Type() {
	case pcommon.ValueTypeStr:
		return StringValue(value.Str())
	case pcommon.ValueTypeBool:
		return BoolValue(value.Bool())
	case pcommon.ValueTypeInt:
		return Int64Value(value.Int())
	case pcommon.ValueTypeDouble:
		return Float64Value(value.Double())
	default:
		return StringValue(value.AsString())
	}
}

func numberDataPointValue(point pmetric.NumberDataPoint) float64 {
	switch point.ValueType() {
	case pmetric.NumberDataPointValueTypeInt:
		return float64(point.IntValue())
	case pmetric.NumberDataPointValueTypeDouble:
		return point.DoubleValue()
	default:
		return 0
	}
}

func exemplarValue(exemplar pmetric.Exemplar) float64 {
	switch exemplar.ValueType() {
	case pmetric.ExemplarValueTypeInt:
		return float64(exemplar.IntValue())
	case pmetric.ExemplarValueTypeDouble:
		return exemplar.DoubleValue()
	default:
		return math.NaN()
	}
}

func numericPcommonValue(value pcommon.Value, fallback float64) float64 {
	switch value.Type() {
	case pcommon.ValueTypeInt:
		return float64(value.Int())
	case pcommon.ValueTypeDouble:
		return value.Double()
	case pcommon.ValueTypeStr:
		parsed, err := strconv.ParseFloat(value.Str(), 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func timestampOrNow(ts pcommon.Timestamp) time.Time {
	if ts == 0 {
		return time.Now()
	}
	return ts.AsTime()
}

func timestampOrDefault(ts pcommon.Timestamp, fallback time.Time) time.Time {
	if ts == 0 {
		return fallback
	}
	return ts.AsTime()
}

func firstTimestamp(values ...pcommon.Timestamp) pcommon.Timestamp {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func idString[T interface {
	IsEmpty() bool
	String() string
}](id T) string {
	if id.IsEmpty() {
		return ""
	}
	return id.String()
}

func uint64Slice(values pcommon.UInt64Slice) []uint64 {
	out := make([]uint64, values.Len())
	for i := 0; i < values.Len(); i++ {
		out[i] = values.At(i)
	}
	return out
}

func float64Slice(values pcommon.Float64Slice) []float64 {
	out := make([]float64, values.Len())
	for i := 0; i < values.Len(); i++ {
		out[i] = values.At(i)
	}
	return out
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
