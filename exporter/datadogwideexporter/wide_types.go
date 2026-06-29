// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// EventKind identifies the OTel source shape that produced a WideEvent.
type EventKind string

const (
	EventKindSpan   EventKind = "span"
	EventKindEvent  EventKind = "event"
	EventKindLog    EventKind = "log"
	EventKindMetric EventKind = "metric"
)

func (k EventKind) valid() bool {
	return k == EventKindSpan || k == EventKindEvent || k == EventKindLog || k == EventKindMetric
}

// TableKind distinguishes sampled raw rows from aggregate rows.
type TableKind string

const (
	TableKindSampled    TableKind = "SAMPLED"
	TableKindAggregated TableKind = "AGGREGATED"
)

// FactKind describes the semantic metric-like shape of a fact.
type FactKind string

const (
	FactKindCounter   FactKind = "counter"
	FactKindGauge     FactKind = "gauge"
	FactKindHistogram FactKind = "histogram"
	FactKindError     FactKind = "error"
	FactKindWarning   FactKind = "warning"
	FactKindInfo      FactKind = "info"
)

func (k FactKind) valid() bool {
	switch k {
	case FactKindCounter, FactKindGauge, FactKindHistogram, FactKindError, FactKindWarning, FactKindInfo:
		return true
	default:
		return false
	}
}

func (k FactKind) logLike() bool {
	return k == FactKindError || k == FactKindWarning || k == FactKindInfo
}

// ValueKind is the scalar type tag for a TypedValue.
type ValueKind string

const (
	ValueString  ValueKind = "string"
	ValueBool    ValueKind = "bool"
	ValueInt64   ValueKind = "int64"
	ValueFloat64 ValueKind = "float64"
)

// TypedValue is a tagged union for scalar values supported by the wide tables.
type TypedValue struct {
	Kind  ValueKind
	Str   string
	Bool  bool
	Int   int64
	Float float64
}

func StringValue(value string) TypedValue {
	return TypedValue{Kind: ValueString, Str: value}
}

func BoolValue(value bool) TypedValue {
	return TypedValue{Kind: ValueBool, Bool: value}
}

func Int64Value(value int64) TypedValue {
	return TypedValue{Kind: ValueInt64, Int: value}
}

func Float64Value(value float64) TypedValue {
	return TypedValue{Kind: ValueFloat64, Float: value}
}

func (v TypedValue) valid() bool {
	switch v.Kind {
	case ValueString, ValueBool, ValueInt64, ValueFloat64:
		return true
	default:
		return false
	}
}

func (v TypedValue) key() string {
	switch v.Kind {
	case ValueString:
		return "s:" + v.Str
	case ValueBool:
		return "b:" + strconv.FormatBool(v.Bool)
	case ValueInt64:
		return "i:" + strconv.FormatInt(v.Int, 10)
	case ValueFloat64:
		return "f:" + strconv.FormatFloat(v.Float, 'g', -1, 64)
	default:
		return "invalid"
	}
}

func (v TypedValue) String() string {
	switch v.Kind {
	case ValueString:
		return v.Str
	case ValueBool:
		return strconv.FormatBool(v.Bool)
	case ValueInt64:
		return strconv.FormatInt(v.Int, 10)
	case ValueFloat64:
		return strconv.FormatFloat(v.Float, 'f', -1, 64)
	default:
		return "<invalid>"
	}
}

// HistogramAggregate is the wide aggregate value for histogram facts. It keeps
// the total observation count, sum, and a bounded sample reservoir. Arrow
// encoding turns this into DDSketch protobuf bytes for the v2 wide schema.
type HistogramAggregate struct {
	Count   uint64
	Sum     float64
	Samples []float64
}

// Fact is a measured numeric value plus the semantic fact kind needed to
// materialize aggregate rows from the same underlying event stream.
type Fact struct {
	kind           FactKind
	value          float64
	aggregateValue float64
	unit           string

	histogramCount   uint64
	histogramSum     float64
	histogramSamples []float64

	logTimestamp time.Time
	logPattern   string
	logValues    []string
}

func CountFact(unit string) Fact {
	return CounterFact(1, unit)
}

func CounterFact(value float64, unit string) Fact {
	return CounterFactWithAggregate(value, value, unit)
}

func CounterFactWithAggregate(value, aggregateValue float64, unit string) Fact {
	return Fact{kind: FactKindCounter, value: value, aggregateValue: aggregateValue, unit: unit}
}

func GaugeFact(value float64, unit string) Fact {
	return Fact{kind: FactKindGauge, value: value, aggregateValue: value, unit: unit}
}

func HistogramFact(value float64, unit string) Fact {
	return HistogramFactWithAggregate(value, value, 1, []float64{value}, unit)
}

func HistogramFactWithSamples(value float64, count uint64, samples []float64, unit string) Fact {
	sum := value
	if count > 1 && len(samples) == 1 && samples[0] == value {
		sum = value * float64(count)
	}
	return HistogramFactWithAggregate(value, sum, count, samples, unit)
}

func HistogramFactWithAggregate(value, aggregateValue float64, count uint64, samples []float64, unit string) Fact {
	if count == 0 && len(samples) > 0 {
		count = uint64(len(samples))
	}
	return Fact{
		kind:             FactKindHistogram,
		value:            value,
		aggregateValue:   aggregateValue,
		unit:             unit,
		histogramCount:   count,
		histogramSum:     aggregateValue,
		histogramSamples: append([]float64(nil), samples...),
	}
}

func LogFact(kind FactKind, timestamp time.Time, pattern string, values []string) Fact {
	return Fact{
		kind:           kind,
		aggregateValue: 1,
		logTimestamp:   timestamp,
		logPattern:     pattern,
		logValues:      append([]string(nil), values...),
	}
}

// Kind returns the fact aggregation kind.
func (f Fact) Kind() FactKind {
	return f.kind
}

// Value returns the sampled scalar value for this fact.
func (f Fact) Value() float64 {
	return f.value
}

// Unit returns the semantic unit associated with this fact.
func (f Fact) Unit() string {
	return f.unit
}

func (f Fact) aggregate() float64 {
	return f.aggregateValue
}

// HistogramCount returns the aggregate count for a histogram fact.
func (f Fact) HistogramCount() uint64 {
	return f.histogramCount
}

// HistogramSum returns the aggregate sum for a histogram fact.
func (f Fact) HistogramSum() float64 {
	return f.histogramSum
}

// HistogramSamples returns a copy of the aggregate sample reservoir for a
// histogram fact.
func (f Fact) HistogramSamples() []float64 {
	return append([]float64(nil), f.histogramSamples...)
}

func (f Fact) LogTimestamp() time.Time {
	return f.logTimestamp
}

func (f Fact) LogPattern() string {
	return f.logPattern
}

func (f Fact) LogValues() []string {
	return append([]string(nil), f.logValues...)
}

func (f Fact) validate() error {
	if !f.kind.valid() {
		return fmt.Errorf("invalid fact kind %q", f.kind)
	}
	if f.kind.logLike() {
		if f.logPattern == "" {
			return fmt.Errorf("log-like fact pattern is required")
		}
		if f.logTimestamp.IsZero() {
			return fmt.Errorf("log-like fact timestamp is required")
		}
		return nil
	}
	if math.IsNaN(f.value) || math.IsInf(f.value, 0) {
		return fmt.Errorf("fact value must be finite, got %v", f.value)
	}
	if math.IsNaN(f.aggregateValue) || math.IsInf(f.aggregateValue, 0) {
		return fmt.Errorf("aggregate fact value must be finite, got %v", f.aggregateValue)
	}
	if f.kind == FactKindHistogram {
		if f.histogramCount == 0 {
			return fmt.Errorf("histogram count must be positive")
		}
		for _, sample := range f.histogramSamples {
			if math.IsNaN(sample) || math.IsInf(sample, 0) {
				return fmt.Errorf("histogram samples must be finite, got %v", sample)
			}
		}
	}
	return nil
}

// WideEvent is the normalized, fact-bearing input record consumed by the
// materializer. It is intentionally post-OTel-normalization: callers have
// already decided which fields are aggregate dimensions, sampled-only
// attributes, and aggregatable facts.
type WideEvent struct {
	Kind EventKind
	// EventType is the Event Platform table identity. Span rows normally use the
	// span name; standalone metric aggregates use a metric-derived event type.
	EventType       string
	ParentEventType string
	Path            string
	SpanName        string
	EventName       string

	Timestamp time.Time
	TraceID   string
	SpanID    string

	EventID       string
	ParentEventID string
	StartTime     time.Time
	EndTime       time.Time

	Sampled           bool
	SampleProbability float64

	Dimensions map[string]TypedValue
	Attributes map[string]TypedValue
	Facts      map[string]Fact
}

// TableIdentity identifies the logical table for one event type.
type TableIdentity struct {
	EventType string `json:"event_type"`
}

func identityFor(event WideEvent) TableIdentity {
	eventType := event.EventType
	if eventType == "" {
		eventType = event.SpanName
	}
	return TableIdentity{EventType: eventType}
}

func (id TableIdentity) key() string {
	return id.EventType
}

func (id TableIdentity) EventTypeName() string {
	return id.EventType
}

// FieldRole records how a dynamic column participates in materialization.
type FieldRole string

const (
	FieldRoleDimension FieldRole = "dimension"
	FieldRoleAttribute FieldRole = "attribute"
	FieldRoleFact      FieldRole = "fact"
)

// FieldSchema describes one dynamic column observed for a table identity within
// the current materialization window.
type FieldSchema struct {
	Name     string    `json:"name"`
	Role     FieldRole `json:"role"`
	Type     ValueKind `json:"type"`
	FactKind FactKind  `json:"fact_kind,omitempty"`
	Unit     string    `json:"unit,omitempty"`
	Pattern  string    `json:"pattern,omitempty"`
}

type TableSchema struct {
	Fields   []FieldSchema `json:"fields"`
	Children []string      `json:"children,omitempty"`
}

// WideRow is a materialized row in either sampled or aggregated form.
type WideRow struct {
	Kind              EventKind
	EventType         string
	Path              string
	SpanName          string
	EventName         string
	Timestamp         time.Time
	TraceID           string
	SpanID            string
	EventID           string
	ParentEventID     string
	StartTime         time.Time
	EndTime           time.Time
	SampleProbability float64
	Dimensions        map[string]TypedValue
	Attributes        map[string]TypedValue
	Facts             map[string]float64
	LogFacts          map[string]Fact
	Histograms        map[string]HistogramAggregate
	WindowStart       time.Time
	WindowEnd         time.Time
}

// WideTable is the in-memory table returned by the materializer.
type WideTable struct {
	Kind        TableKind
	Identity    TableIdentity
	WindowStart time.Time
	WindowEnd   time.Time
	Schema      TableSchema
	Rows        []WideRow
}
