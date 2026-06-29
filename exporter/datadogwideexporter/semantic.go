// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import "time"

const (
	metricTypeCounter   metricType = "counter"
	metricTypeGauge     metricType = "gauge"
	metricTypeHistogram metricType = "histogram"

	metricSampleEventName    = "metric.sample"
	metricAggregateEventName = "metric.aggregate"
	metricLegacyEventName    = "metric"
	metricFlushSpanName      = "telltail.metric_flush"
	logFlushSpanName         = "telltail.log_flush"
	sampleWeightAttribute    = "sample.weight"
	metricSampleWeightAttr   = "metric.sample_weight"
)

type metricType string

type spanRef struct {
	traceID string
	spanID  string
}

func (r spanRef) valid() bool {
	return r.traceID != "" && r.spanID != ""
}

type spanObservation struct {
	Ref               spanRef
	Name              string
	Start             time.Time
	End               time.Time
	Sampled           bool
	SampleProbability float64
	Dimensions        map[string]TypedValue
	Attributes        map[string]TypedValue
	Metrics           []metricObservation
	Logs              []logObservation
}

type logObservation struct {
	Ref        spanRef
	Timestamp  time.Time
	Severity   string
	Body       string
	Attributes map[string]TypedValue
}

type metricDescriptor struct {
	Name       string
	Type       metricType
	Unit       string
	Dimensions map[string]TypedValue
}

type metricObservation struct {
	Metric    metricDescriptor
	Aggregate *metricAggregateFact
	Samples   []metricSampleFact
}

type linkedMetricSample struct {
	Span   spanRef
	Sample metricSampleFact
}

type linkedMetricObservation struct {
	Metric    metricDescriptor
	Aggregate *metricAggregateFact
	Samples   []linkedMetricSample
}

type metricAggregateFact struct {
	Value            float64
	Timestamp        time.Time
	Histogram        *histogramAggregate
	HistogramSamples []float64
	Weight           float64
}

type metricSampleFact struct {
	Value      float64
	Timestamp  time.Time
	Dimensions map[string]TypedValue
	Weight     float64
}

type histogramAggregate struct {
	Count          uint64
	Sum            float64
	Min            *float64
	Max            *float64
	BucketCounts   []uint64
	ExplicitBounds []float64
	Scale          *int32
	ZeroCount      *uint64
	ZeroThreshold  *float64
	Positive       *exponentialBuckets
	Negative       *exponentialBuckets
}

type exponentialBuckets struct {
	Offset int32
	Counts []uint64
}

type observationBatch struct {
	Spans   []spanObservation
	Metrics []linkedMetricObservation
	Logs    []logObservation
}
