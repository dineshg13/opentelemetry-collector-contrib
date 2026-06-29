// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const DefaultHistogramReservoirSize = 1028

type MaterializerOption func(*Materializer)

func WithClock(clock func() time.Time) MaterializerOption {
	return func(m *Materializer) {
		if clock != nil {
			m.clock = clock
		}
	}
}

func WithWindowStart(start time.Time) MaterializerOption {
	return func(m *Materializer) {
		if !start.IsZero() {
			m.windowStart = start
		}
	}
}

// Materializer accumulates normalized WideEvent records, keeps aggregate state
// over the full stream, and retains sampled rows for the same flush window.
//
// Materializer is not safe for concurrent use.
type Materializer struct {
	clock       func() time.Time
	windowStart time.Time
	seq         uint64

	sampledRows []WideRow
	buckets     map[string]*aggregateBucket
	schemas     map[string]map[string]FieldSchema
	children    map[string]map[string]struct{}
}

func NewMaterializer(opts ...MaterializerOption) *Materializer {
	m := &Materializer{
		clock:    time.Now,
		buckets:  make(map[string]*aggregateBucket),
		schemas:  make(map[string]map[string]FieldSchema),
		children: make(map[string]map[string]struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.windowStart.IsZero() {
		m.windowStart = m.clock()
	}
	return m
}

func (m *Materializer) Add(event WideEvent) error {
	if err := validateEvent(event); err != nil {
		return err
	}

	identity := identityFor(event)
	if err := m.recordSchema(identity, event); err != nil {
		return err
	}
	m.recordChild(event)

	m.seq++
	if event.Sampled {
		m.sampledRows = append(m.sampledRows, sampledRow(event))
	}

	if len(event.Facts) == 0 {
		return nil
	}
	path := eventPath(event)
	key := aggregateKey(identity, path, event.Dimensions)
	bucket := m.buckets[key]
	if bucket == nil {
		bucket = &aggregateBucket{
			identity:   identity,
			path:       path,
			dimensions: copyValues(event.Dimensions),
			facts:      make(map[string]*aggregateValue),
		}
		m.buckets[key] = bucket
	}
	for name, fact := range event.Facts {
		if err := bucket.addFact(name, fact, event.Timestamp, m.seq); err != nil {
			return err
		}
	}
	return nil
}

func (m *Materializer) Flush(ctx context.Context) ([]WideTable, error) {
	tables, windowEnd, err := m.flushSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	m.reset(windowEnd)
	return tables, nil
}

func (m *Materializer) flushSnapshot(ctx context.Context) ([]WideTable, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}

	windowEnd := m.clock()
	windowStart := m.windowStart
	if windowEnd.Before(windowStart) {
		windowEnd = windowStart
	}

	tables, err := m.buildTables(ctx, windowStart, windowEnd)
	if err != nil {
		return nil, time.Time{}, err
	}
	return tables, windowEnd, nil
}

func (m *Materializer) reset(windowStart time.Time) {
	m.sampledRows = nil
	m.buckets = make(map[string]*aggregateBucket)
	m.schemas = make(map[string]map[string]FieldSchema)
	m.children = make(map[string]map[string]struct{})
	m.windowStart = windowStart
}

func (m *Materializer) buildTables(ctx context.Context, windowStart, windowEnd time.Time) ([]WideTable, error) {
	tablesByKey := make(map[string]*WideTable)

	for _, bucket := range m.buckets {
		tableKey := tableKey(TableKindAggregated, bucket.identity)
		table := tablesByKey[tableKey]
		if table == nil {
			table = &WideTable{
				Kind:        TableKindAggregated,
				Identity:    bucket.identity,
				WindowStart: windowStart,
				WindowEnd:   windowEnd,
				Schema:      m.tableSchema(bucket.identity, TableKindAggregated),
			}
			tablesByKey[tableKey] = table
		}
		table.Rows = append(table.Rows, bucket.row(windowStart, windowEnd))
	}

	for _, row := range m.sampledRows {
		identity := TableIdentity{EventType: row.EventType}
		tableKey := tableKey(TableKindSampled, identity)
		table := tablesByKey[tableKey]
		if table == nil {
			table = &WideTable{
				Kind:        TableKindSampled,
				Identity:    identity,
				WindowStart: windowStart,
				WindowEnd:   windowEnd,
				Schema:      m.tableSchema(identity, TableKindSampled),
			}
			tablesByKey[tableKey] = table
		}
		row.WindowStart = windowStart
		row.WindowEnd = windowEnd
		table.Rows = append(table.Rows, row)
	}

	keys := make([]string, 0, len(tablesByKey))
	for key := range tablesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	tables := make([]WideTable, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		table := *tablesByKey[key]
		sortRows(table.Rows)
		tables = append(tables, table)
	}
	return tables, nil
}

func validateEvent(event WideEvent) error {
	if !event.Kind.valid() {
		return fmt.Errorf("invalid event kind %q", event.Kind)
	}
	if event.EventType == "" && event.SpanName == "" {
		return fmt.Errorf("event_type or span_name is required")
	}
	if event.Sampled {
		if event.TraceID == "" {
			return fmt.Errorf("trace_id is required for sampled rows")
		}
		if event.SpanID == "" {
			return fmt.Errorf("span_id is required for sampled rows")
		}
	}
	if event.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}
	if math.IsNaN(event.SampleProbability) || event.SampleProbability < 0 || event.SampleProbability > 1 {
		return fmt.Errorf("sample_probability must be between 0 and 1, got %v", event.SampleProbability)
	}
	for name, value := range event.Dimensions {
		if err := validateFieldName(name); err != nil {
			return fmt.Errorf("dimension %q: %w", name, err)
		}
		if !value.valid() {
			return fmt.Errorf("dimension %q has invalid value kind %q", name, value.Kind)
		}
	}
	for name, value := range event.Attributes {
		if err := validateFieldName(name); err != nil {
			return fmt.Errorf("attribute %q: %w", name, err)
		}
		if !value.valid() {
			return fmt.Errorf("attribute %q has invalid value kind %q", name, value.Kind)
		}
	}
	for name, fact := range event.Facts {
		if err := validateFieldName(name); err != nil {
			return fmt.Errorf("fact %q: %w", name, err)
		}
		if err := fact.validate(); err != nil {
			return fmt.Errorf("fact %q: %w", name, err)
		}
	}
	return nil
}

var reservedColumns = map[string]struct{}{
	"_trace_id":           {},
	"_event_id":           {},
	"_parent_event_id":    {},
	"_path":               {},
	"_start_ms":           {},
	"_end_ms":             {},
	"_sample_probability": {},
	"_duration_ms":        {},
	"_kind":               {},
	"_span_name":          {},
	"_event_name":         {},
	"kind":                {},
	"span_name":           {},
	"event_name":          {},
	"timestamp_ms":        {},
	"trace_id":            {},
	"span_id":             {},
	"start_ms":            {},
	"end_ms":              {},
	"window_start_ms":     {},
	"window_end_ms":       {},
}

func validateFieldName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if _, ok := reservedColumns[name]; ok {
		return fmt.Errorf("reserved column name")
	}
	return nil
}

func (m *Materializer) recordChild(event WideEvent) {
	parent := event.ParentEventType
	if parent == "" || parent == event.EventType {
		return
	}
	child := identityFor(event).EventTypeName()
	if child == "" {
		return
	}
	children := m.children[parent]
	if children == nil {
		children = make(map[string]struct{})
		m.children[parent] = children
	}
	children[child] = struct{}{}
}

func (m *Materializer) recordSchema(identity TableIdentity, event WideEvent) error {
	key := identity.key()
	existing := m.schemas[key]
	fields := make(map[string]FieldSchema, len(existing)+len(event.Dimensions)+len(event.Attributes)+len(event.Facts))
	for name, field := range existing {
		fields[name] = field
	}

	for name, value := range event.Dimensions {
		if err := recordField(fields, FieldSchema{Name: name, Role: FieldRoleDimension, Type: value.Kind}); err != nil {
			return err
		}
	}
	for name, value := range event.Attributes {
		if err := recordField(fields, FieldSchema{Name: name, Role: FieldRoleAttribute, Type: value.Kind}); err != nil {
			return err
		}
	}
	for name, fact := range event.Facts {
		valueType := ValueFloat64
		if fact.Kind().logLike() {
			valueType = ValueString
		}
		if err := recordField(fields, FieldSchema{
			Name:     name,
			Role:     FieldRoleFact,
			Type:     valueType,
			FactKind: fact.Kind(),
			Unit:     fact.Unit(),
			Pattern:  fact.LogPattern(),
		}); err != nil {
			return err
		}
	}
	m.schemas[key] = fields
	return nil
}

func recordField(fields map[string]FieldSchema, next FieldSchema) error {
	existing, ok := fields[next.Name]
	if !ok {
		fields[next.Name] = next
		return nil
	}
	if existing.Role != next.Role {
		return fmt.Errorf("field %q role conflict: %s vs %s", next.Name, existing.Role, next.Role)
	}
	if existing.Type != next.Type {
		return fmt.Errorf("field %q type conflict: %s vs %s", next.Name, existing.Type, next.Type)
	}
	if existing.Role == FieldRoleFact {
		if existing.FactKind != next.FactKind {
			return fmt.Errorf("fact %q kind conflict: %s vs %s", next.Name, existing.FactKind, next.FactKind)
		}
		if existing.Unit != next.Unit {
			return fmt.Errorf("fact %q unit conflict: %q vs %q", next.Name, existing.Unit, next.Unit)
		}
		if existing.Pattern != next.Pattern {
			return fmt.Errorf("fact %q pattern conflict: %q vs %q", next.Name, existing.Pattern, next.Pattern)
		}
	}
	return nil
}

func (m *Materializer) tableSchema(identity TableIdentity, kind TableKind) TableSchema {
	fieldsByName := m.schemas[identity.key()]
	fields := make([]FieldSchema, 0, len(fieldsByName))
	for _, field := range fieldsByName {
		if kind == TableKindAggregated && field.Role == FieldRoleAttribute {
			continue
		}
		fields = append(fields, field)
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Role != fields[j].Role {
			return roleOrder(fields[i].Role) < roleOrder(fields[j].Role)
		}
		return fields[i].Name < fields[j].Name
	})
	children := make([]string, 0, len(m.children[identity.key()]))
	for child := range m.children[identity.key()] {
		children = append(children, child)
	}
	sort.Strings(children)
	return TableSchema{Fields: fields, Children: children}
}

func roleOrder(role FieldRole) int {
	switch role {
	case FieldRoleDimension:
		return 0
	case FieldRoleAttribute:
		return 1
	case FieldRoleFact:
		return 2
	default:
		return 3
	}
}

func sampledRow(event WideEvent) WideRow {
	facts := make(map[string]float64, len(event.Facts))
	logFacts := make(map[string]Fact)
	for name, fact := range event.Facts {
		if fact.Kind().logLike() {
			logFacts[name] = fact
			continue
		}
		facts[name] = fact.Value()
	}
	if len(facts) == 0 {
		facts = nil
	}
	if len(logFacts) == 0 {
		logFacts = nil
	}
	sampleProbability := event.SampleProbability
	if sampleProbability == 0 {
		sampleProbability = 1
	}
	identity := identityFor(event)
	return WideRow{
		Kind:              event.Kind,
		EventType:         identity.EventType,
		Path:              eventPath(event),
		SpanName:          event.SpanName,
		EventName:         event.EventName,
		Timestamp:         event.Timestamp,
		TraceID:           event.TraceID,
		SpanID:            event.SpanID,
		EventID:           eventIDFor(event),
		ParentEventID:     parentEventIDFor(event),
		StartTime:         event.StartTime,
		EndTime:           event.EndTime,
		SampleProbability: sampleProbability,
		Dimensions:        copyValues(event.Dimensions),
		Attributes:        copyValues(event.Attributes),
		Facts:             facts,
		LogFacts:          logFacts,
	}
}

func eventIDFor(event WideEvent) string {
	if event.EventID != "" {
		return event.EventID
	}
	if event.Kind == EventKindSpan {
		return event.SpanID
	}
	return fmt.Sprintf("%s/%s/%s/%d", event.SpanID, event.Kind, event.EventName, event.Timestamp.UnixNano())
}

func parentEventIDFor(event WideEvent) string {
	if event.ParentEventID != "" {
		return event.ParentEventID
	}
	if event.Kind == EventKindSpan {
		return ""
	}
	return event.SpanID
}

func eventPath(event WideEvent) string {
	if event.Path != "" {
		return event.Path
	}
	identity := identityFor(event)
	if event.ParentEventType != "" && event.ParentEventType != identity.EventType {
		return childEventPath(event.ParentEventType, identity.EventType)
	}
	return identity.EventType
}

func aggregateKey(identity TableIdentity, path string, dimensions map[string]TypedValue) string {
	keys := sortedValueKeys(dimensions)
	parts := make([]string, 0, len(keys)+2)
	parts = append(parts, identity.key())
	parts = append(parts, path)
	for _, key := range keys {
		parts = append(parts, key+"="+dimensions[key].key())
	}
	return strings.Join(parts, "\x00")
}

func tableKey(kind TableKind, identity TableIdentity) string {
	return string(kind) + "\x00" + identity.key()
}

func sortedValueKeys(values map[string]TypedValue) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyValues(values map[string]TypedValue) map[string]TypedValue {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]TypedValue, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func sortRows(rows []WideRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		if rows[i].SpanName != rows[j].SpanName {
			return rows[i].SpanName < rows[j].SpanName
		}
		if rows[i].EventName != rows[j].EventName {
			return rows[i].EventName < rows[j].EventName
		}
		if !rows[i].Timestamp.Equal(rows[j].Timestamp) {
			return rows[i].Timestamp.Before(rows[j].Timestamp)
		}
		return dimensionsDisplay(rows[i].Dimensions) < dimensionsDisplay(rows[j].Dimensions)
	})
}

func dimensionsDisplay(values map[string]TypedValue) string {
	keys := sortedValueKeys(values)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key].String())
	}
	return strings.Join(parts, ",")
}

type aggregateBucket struct {
	identity   TableIdentity
	path       string
	dimensions map[string]TypedValue
	facts      map[string]*aggregateValue
}

func (b *aggregateBucket) addFact(name string, fact Fact, timestamp time.Time, seq uint64) error {
	value := b.facts[name]
	if value == nil {
		value = newAggregateValue(fact)
		b.facts[name] = value
	}
	return value.add(fact, timestamp, seq)
}

func (b *aggregateBucket) row(windowStart, windowEnd time.Time) WideRow {
	facts := make(map[string]float64, len(b.facts))
	histograms := make(map[string]HistogramAggregate)
	for name, value := range b.facts {
		if value.kind == FactKindHistogram {
			histograms[name] = HistogramAggregate{
				Count:   value.histogramCount,
				Sum:     value.histogramSum,
				Samples: append([]float64(nil), value.histogramSamples...),
			}
			continue
		}
		facts[name] = value.value
	}
	if len(facts) == 0 {
		facts = nil
	}
	if len(histograms) == 0 {
		histograms = nil
	}
	return WideRow{
		EventType:   b.identity.EventType,
		Path:        b.path,
		Dimensions:  copyValues(b.dimensions),
		Facts:       facts,
		Histograms:  histograms,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	}
}

type aggregateValue struct {
	kind      FactKind
	unit      string
	value     float64
	timestamp time.Time
	seq       uint64
	seen      bool

	histogramCount   uint64
	histogramSum     float64
	histogramSamples []float64
}

func newAggregateValue(fact Fact) *aggregateValue {
	return &aggregateValue{kind: fact.Kind(), unit: fact.Unit()}
}

func (a *aggregateValue) add(fact Fact, timestamp time.Time, seq uint64) error {
	if fact.Kind() != a.kind {
		return fmt.Errorf("fact kind changed from %s to %s", a.kind, fact.Kind())
	}
	if fact.Unit() != a.unit {
		return fmt.Errorf("unit changed from %q to %q", a.unit, fact.Unit())
	}

	switch a.kind {
	case FactKindCounter:
		a.value += fact.aggregate()
	case FactKindGauge:
		if !a.seen || timestamp.After(a.timestamp) || (timestamp.Equal(a.timestamp) && seq > a.seq) {
			a.value = fact.Value()
			a.timestamp = timestamp
			a.seq = seq
		}
	case FactKindHistogram:
		a.histogramCount += fact.HistogramCount()
		a.histogramSum += fact.HistogramSum()
		a.histogramSamples = appendReservoir(a.histogramSamples, fact.HistogramSamples(), DefaultHistogramReservoirSize)
	case FactKindError, FactKindWarning, FactKindInfo:
		a.value += fact.aggregate()
	default:
		return fmt.Errorf("unsupported fact kind %q", a.kind)
	}
	a.seen = true
	return nil
}

func appendReservoir(existing, samples []float64, limit int) []float64 {
	if limit <= 0 || len(samples) == 0 || len(existing) >= limit {
		return existing
	}
	remaining := limit - len(existing)
	if len(samples) > remaining {
		samples = samples[:remaining]
	}
	return append(existing, samples...)
}
