// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWideDimensionPrefix = "dimensions"
	defaultWideAttributePrefix = "attributes"
	defaultWideMetricPrefix    = "metric"
	logEventSuffix             = "log"
	logMessagePattern          = "{}"
)

func wideEventsFromBatch(batch observationBatch) []WideEvent {
	spanNames := make(map[spanRef]string, len(batch.Spans))
	for _, span := range batch.Spans {
		if span.Ref.valid() {
			spanNames[span.Ref] = span.Name
		}
	}

	exportedSamples := make(map[string]struct{})
	events := make([]WideEvent, 0, len(batch.Spans)+len(batch.Metrics)+len(batch.Logs))
	for _, span := range batch.Spans {
		events = append(events, spanWideEvents(span, exportedSamples)...)
	}
	for _, metric := range batch.Metrics {
		events = append(events, linkedMetricWideEvents(metric, spanNames, exportedSamples)...)
	}
	for _, record := range batch.Logs {
		if event, ok := logWideEvent(record, "", "", "", ""); ok {
			events = append(events, event)
		}
	}
	return events
}

func spanWideEvents(obs spanObservation, exportedSamples map[string]struct{}) []WideEvent {
	if !obs.Ref.valid() || obs.Name == "" {
		return nil
	}
	if obs.Name == metricFlushSpanName || obs.Name == logFlushSpanName {
		return nil
	}
	weight := effectiveWideWeight(1 / obs.SampleProbability)
	if obs.SampleProbability == 0 {
		weight = 1
	}
	duration := obs.End.Sub(obs.Start).Seconds()
	if duration < 0 {
		duration = 0
	}
	durationCount := weightedWideHistogramCount(1, weight)
	spanEvent := WideEvent{
		Kind:              EventKindSpan,
		EventType:         obs.Name,
		Path:              obs.Name,
		SpanName:          obs.Name,
		Timestamp:         obs.End,
		TraceID:           obs.Ref.traceID,
		SpanID:            obs.Ref.spanID,
		EventID:           obs.Ref.spanID,
		StartTime:         obs.Start,
		EndTime:           obs.End,
		Sampled:           obs.Sampled,
		SampleProbability: obs.SampleProbability,
		Dimensions:        cloneWideValues(obs.Dimensions),
		Attributes:        cloneWideValues(obs.Attributes),
		Facts: map[string]Fact{
			factName("count"):    CounterFactWithAggregate(1, weight, "1"),
			factName("duration"): HistogramFactWithAggregate(duration, duration*float64(durationCount), durationCount, []float64{duration}, "s"),
		},
	}
	events := []WideEvent{spanEvent}

	for _, metric := range obs.Metrics {
		for _, sample := range metric.Samples {
			if event, ok := metricSampleWideEvent(metric.Metric, sample, obs.Name, obs.Ref.traceID, obs.Ref.spanID, obs.Ref.spanID); ok {
				exportedSamples[sampleIdentity(obs.Ref, metric.Metric, sample)] = struct{}{}
				events = append(events, event)
			}
		}
		if metric.Aggregate != nil {
			if event, ok := metricAggregateWideEvent(metric.Metric, *metric.Aggregate); ok {
				events = append(events, event)
			}
		}
	}
	for _, record := range obs.Logs {
		if event, ok := logWideEvent(record, obs.Name, obs.Ref.traceID, obs.Ref.spanID, obs.Ref.spanID); ok {
			events = append(events, event)
		}
	}
	return events
}

func linkedMetricWideEvents(obs linkedMetricObservation, spanNames map[spanRef]string, exportedSamples map[string]struct{}) []WideEvent {
	events := make([]WideEvent, 0, 1+len(obs.Samples))
	for _, linked := range obs.Samples {
		spanName, ok := spanNames[linked.Span]
		if !ok {
			continue
		}
		key := sampleIdentity(linked.Span, obs.Metric, linked.Sample)
		if _, ok := exportedSamples[key]; ok {
			continue
		}
		if event, ok := metricSampleWideEvent(obs.Metric, linked.Sample, spanName, linked.Span.traceID, linked.Span.spanID, linked.Span.spanID); ok {
			exportedSamples[key] = struct{}{}
			events = append(events, event)
		}
	}
	if obs.Aggregate != nil {
		if event, ok := metricAggregateWideEvent(obs.Metric, *obs.Aggregate); ok {
			events = append(events, event)
		}
	}
	return events
}

func metricSampleWideEvent(desc metricDescriptor, sample metricSampleFact, spanName, traceID, spanID, parentEventID string) (WideEvent, bool) {
	fact, ok := sampleFact(desc, sample)
	if !ok {
		return WideEvent{}, false
	}
	dimensions := cloneWideValues(desc.Dimensions)
	mergeWideValues(dimensions, sample.Dimensions)
	eventType := metricEventType(desc.Name)
	return WideEvent{
		Kind:              EventKindMetric,
		EventType:         eventType,
		ParentEventType:   spanName,
		Path:              childEventPath(spanName, eventType),
		SpanName:          spanName,
		EventName:         eventType,
		Timestamp:         sample.Timestamp,
		TraceID:           traceID,
		SpanID:            spanID,
		EventID:           metricEventID(spanID, desc, sample.Timestamp, sample.Dimensions),
		ParentEventID:     parentEventID,
		StartTime:         sample.Timestamp,
		EndTime:           sample.Timestamp,
		Sampled:           true,
		SampleProbability: sampleProbabilityForWeight(sample.Weight),
		Dimensions:        dimensions,
		Facts: map[string]Fact{
			metricFactName(desc.Name): fact,
		},
	}, true
}

func metricAggregateWideEvent(desc metricDescriptor, aggregate metricAggregateFact) (WideEvent, bool) {
	fact, ok := aggregateFact(desc, aggregate)
	if !ok {
		return WideEvent{}, false
	}
	eventType := metricEventType(desc.Name)
	return WideEvent{
		Kind:       EventKindMetric,
		EventType:  eventType,
		Path:       eventType,
		EventName:  eventType,
		Timestamp:  aggregate.Timestamp,
		Dimensions: cloneWideValues(desc.Dimensions),
		Facts: map[string]Fact{
			metricFactName(desc.Name): fact,
		},
	}, true
}

func logWideEvent(record logObservation, parentEventType, traceID, spanID, parentEventID string) (WideEvent, bool) {
	timestamp := record.Timestamp
	if timestamp.IsZero() {
		return WideEvent{}, false
	}
	if traceID == "" {
		traceID = record.Ref.traceID
	}
	if spanID == "" {
		spanID = record.Ref.spanID
	}
	eventType := logEventType(parentEventType)
	kind := logFactKind(record.Severity)
	sampled := traceID != "" && spanID != ""
	return WideEvent{
		Kind:              EventKindLog,
		EventType:         eventType,
		ParentEventType:   parentEventType,
		Path:              childEventPath(parentEventType, eventType),
		EventName:         eventType,
		Timestamp:         timestamp,
		TraceID:           traceID,
		SpanID:            spanID,
		EventID:           logEventID(spanID, record),
		ParentEventID:     parentEventID,
		StartTime:         timestamp,
		EndTime:           timestamp,
		Sampled:           sampled,
		SampleProbability: 1,
		Attributes:        cloneWideValues(record.Attributes),
		Facts: map[string]Fact{
			string(kind): LogFact(kind, timestamp, logMessagePattern, []string{record.Body}),
		},
	}, true
}

func logEventType(parentEventType string) string {
	parentEventType = strings.TrimSpace(parentEventType)
	if parentEventType == "" {
		return logEventSuffix
	}
	return parentEventType + "." + logEventSuffix
}

func logFactKind(severity string) FactKind {
	severity = strings.ToLower(severity)
	switch {
	case strings.Contains(severity, "fatal"), strings.Contains(severity, "error"):
		return FactKindError
	case strings.Contains(severity, "warn"):
		return FactKindWarning
	default:
		return FactKindInfo
	}
}

func sampleFact(desc metricDescriptor, sample metricSampleFact) (Fact, bool) {
	unit := normalizedWideUnit(desc.Unit)
	weight := effectiveWideWeight(sample.Weight)
	switch desc.Type {
	case metricTypeCounter:
		return CounterFactWithAggregate(sample.Value, sample.Value*weight, unit), true
	case metricTypeGauge:
		return GaugeFact(sample.Value, unit), true
	case metricTypeHistogram:
		count := weightedWideHistogramCount(1, weight)
		return HistogramFactWithAggregate(sample.Value, sample.Value*float64(count), count, []float64{sample.Value}, unit), true
	default:
		return Fact{}, false
	}
}

func aggregateFact(desc metricDescriptor, aggregate metricAggregateFact) (Fact, bool) {
	unit := normalizedWideUnit(desc.Unit)
	weight := effectiveWideWeight(aggregate.Weight)
	switch desc.Type {
	case metricTypeCounter:
		return CounterFactWithAggregate(aggregate.Value, aggregate.Value*weight, unit), true
	case metricTypeGauge:
		return GaugeFact(aggregate.Value, unit), true
	case metricTypeHistogram:
		count := uint64(1)
		if aggregate.Histogram != nil && aggregate.Histogram.Count > 0 {
			count = aggregate.Histogram.Count
		}
		weightedCount := weightedWideHistogramCount(count, weight)
		return HistogramFactWithAggregate(aggregate.Value, aggregate.Value*weight, weightedCount, aggregate.HistogramSamples, unit), true
	default:
		return Fact{}, false
	}
}

func effectiveWideWeight(weight float64) float64 {
	if weight <= 0 || math.IsInf(weight, 0) || math.IsNaN(weight) {
		return 1
	}
	return weight
}

func sampleProbabilityForWeight(weight float64) float64 {
	weight = effectiveWideWeight(weight)
	if weight <= 1 {
		return 1
	}
	return 1 / weight
}

func weightedWideHistogramCount(count uint64, weight float64) uint64 {
	if count == 0 {
		return 0
	}
	weighted := math.Round(float64(count) * effectiveWideWeight(weight))
	if weighted < 1 {
		return 1
	}
	return uint64(weighted)
}

func normalizedWideUnit(unit string) string {
	if unit == "" {
		return "1"
	}
	return unit
}

func dimensionName(name string) string {
	return prefixedWideName(defaultWideDimensionPrefix, name)
}

func attributeName(name string) string {
	return prefixedWideName(defaultWideAttributePrefix, name)
}

func factName(name string) string {
	return sanitizeWideName(name)
}

func metricFactName(name string) string {
	return sanitizeWideName(name)
}

func metricEventType(name string) string {
	return prefixedWideName(defaultWideMetricPrefix, sanitizeWideName(name))
}

func childEventPath(parent, child string) string {
	parent = strings.TrimSpace(parent)
	child = strings.TrimSpace(child)
	switch {
	case parent == "":
		return child
	case child == "":
		return parent
	default:
		return parent + "/" + child
	}
}

func prefixedWideName(prefix, name string) string {
	name = sanitizeWideName(name)
	prefix = strings.Trim(prefix, ".")
	if prefix == "" || strings.HasPrefix(name, prefix+".") {
		return name
	}
	return prefix + "." + name
}

func metricEventID(spanID string, desc metricDescriptor, timestamp time.Time, dims map[string]TypedValue) string {
	keys := make([]string, 0, len(desc.Dimensions)+len(dims))
	for _, key := range sortedValueKeys(desc.Dimensions) {
		keys = append(keys, key+"="+desc.Dimensions[key].String())
	}
	for _, key := range sortedValueKeys(dims) {
		keys = append(keys, key+"="+dims[key].String())
	}
	sort.Strings(keys)
	h := fnv.New64a()
	_, _ = h.Write([]byte(desc.Name))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.FormatInt(timestamp.UnixNano(), 10)))
	for _, key := range keys {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(key))
	}
	return spanID + "/metric/" + sanitizeWideName(desc.Name) + "/" + strconv.FormatUint(h.Sum64(), 16)
}

func logEventID(spanID string, record logObservation) string {
	keys := sortedValueKeys(record.Attributes)
	h := fnv.New64a()
	_, _ = h.Write([]byte(record.Timestamp.Format(time.RFC3339Nano)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(record.Severity))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(record.Body))
	for _, key := range keys {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte("="))
		_, _ = h.Write([]byte(record.Attributes[key].String()))
	}
	if spanID == "" {
		spanID = "orphan"
	}
	return spanID + "/log/" + strconv.FormatUint(h.Sum64(), 16)
}

func sampleIdentity(span spanRef, desc metricDescriptor, sample metricSampleFact) string {
	keys := make([]string, 0, len(desc.Dimensions)+len(sample.Dimensions))
	for _, key := range sortedValueKeys(desc.Dimensions) {
		keys = append(keys, key+"="+desc.Dimensions[key].String())
	}
	for _, key := range sortedValueKeys(sample.Dimensions) {
		keys = append(keys, key+"="+sample.Dimensions[key].String())
	}
	sort.Strings(keys)
	return span.traceID + "/" + span.spanID + "/" + desc.Name + "/" + strconv.FormatInt(sample.Timestamp.UnixNano(), 10) + "/" + strconv.FormatFloat(sample.Value, 'g', -1, 64) + "/" + strings.Join(keys, "\x00")
}

func sanitizeWideName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" {
		return "unknown"
	}
	return name
}

func cloneWideValues(values map[string]TypedValue) map[string]TypedValue {
	out := make(map[string]TypedValue, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func mergeWideValues(dst map[string]TypedValue, src map[string]TypedValue) map[string]TypedValue {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]TypedValue, len(src))
	}
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
