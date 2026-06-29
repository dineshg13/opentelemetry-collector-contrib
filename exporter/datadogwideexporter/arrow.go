// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"bytes"
	"fmt"
	"math"
	"time"

	"github.com/DataDog/sketches-go/ddsketch"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/protobuf/proto"
)

const wideDDSketchRelativeAccuracy = 0.0075

// EncodeArrowIPC encodes a materialized table using the wide-track Arrow
// contract. Serializer normally calls the internal encoder so protobuf
// schema_json and Arrow metadata stay identical.
func EncodeArrowIPC(table WideTable) ([]byte, error) {
	schemaText, err := schemaJSONFor(table.Schema)
	if err != nil {
		return nil, err
	}
	return encodeArrowIPC(table, schemaText)
}

func encodeArrowIPC(table WideTable, schemaJSON string) ([]byte, error) {
	fields, err := arrowFields(table)
	if err != nil {
		return nil, err
	}
	metadata := arrow.NewMetadata(
		[]string{
			"evp.telemetry.schema",
			"evp.table.kind",
			"evp.event_type",
			"evp.window.start_ms",
			"evp.window.end_ms",
		},
		[]string{
			schemaJSON,
			string(table.Kind),
			table.Identity.EventTypeName(),
			fmt.Sprintf("%d", millis(table.WindowStart)),
			fmt.Sprintf("%d", millis(table.WindowEnd)),
		},
	)
	schema := arrow.NewSchema(fields, &metadata)

	pool := memory.NewGoAllocator()
	builders := make([]array.Builder, 0, len(fields))
	for _, field := range fields {
		builders = append(builders, array.NewBuilder(pool, field.Type))
	}
	defer func() {
		for _, builder := range builders {
			builder.Release()
		}
	}()

	for rowIndex, row := range table.Rows {
		offset := appendReserved(builders, table.Kind, row)
		for i, field := range table.Schema.Fields {
			if err := appendWideDynamic(builders[offset+i], table.Kind, row, field); err != nil {
				return nil, fmt.Errorf("row %d field %q: %w", rowIndex, field.Name, err)
			}
		}
	}

	columns := make([]arrow.Array, 0, len(builders))
	for _, builder := range builders {
		columns = append(columns, builder.NewArray())
	}
	defer func() {
		for _, column := range columns {
			column.Release()
		}
	}()

	record := array.NewRecord(schema, columns, int64(len(table.Rows)))
	defer record.Release()

	var out bytes.Buffer
	writer := ipc.NewWriter(&out, ipc.WithSchema(schema), ipc.WithZstd())
	if err := writer.Write(record); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func arrowFields(table WideTable) ([]arrow.Field, error) {
	fields := reservedWideFields(table.Kind)
	for _, field := range table.Schema.Fields {
		arrowType, err := wideArrowType(table.Kind, field)
		if err != nil {
			return nil, err
		}
		fields = append(fields, arrow.Field{Name: field.Name, Type: arrowType, Nullable: true})
	}
	return fields, nil
}

func reservedWideFields(kind TableKind) []arrow.Field {
	switch kind {
	case TableKindAggregated:
		return []arrow.Field{
			{Name: "_path", Type: arrow.BinaryTypes.String, Nullable: false},
		}
	case TableKindSampled:
		return []arrow.Field{
			{Name: "_trace_id", Type: arrow.BinaryTypes.String, Nullable: false},
			{Name: "_event_id", Type: arrow.BinaryTypes.String, Nullable: false},
			{Name: "_parent_event_id", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "_path", Type: arrow.BinaryTypes.String, Nullable: false},
			{Name: "_start_ms", Type: arrow.PrimitiveTypes.Uint64, Nullable: false},
			{Name: "_end_ms", Type: arrow.PrimitiveTypes.Uint64, Nullable: false},
			{Name: "_sample_probability", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
			{Name: "_duration_ms", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
		}
	default:
		return nil
	}
}

func wideArrowType(tableKind TableKind, field FieldSchema) (arrow.DataType, error) {
	switch field.Role {
	case FieldRoleDimension, FieldRoleAttribute:
		return arrow.BinaryTypes.String, nil
	case FieldRoleFact:
		return factArrowType(tableKind, field)
	default:
		return nil, fmt.Errorf("field %q has unsupported role %q", field.Name, field.Role)
	}
}

func factArrowType(tableKind TableKind, field FieldSchema) (arrow.DataType, error) {
	switch field.FactKind {
	case FactKindCounter:
		if tableKind == TableKindAggregated {
			return arrow.PrimitiveTypes.Int64, nil
		}
		return arrow.PrimitiveTypes.Float64, nil
	case FactKindGauge:
		return arrow.PrimitiveTypes.Float64, nil
	case FactKindHistogram:
		if tableKind == TableKindSampled {
			return arrow.PrimitiveTypes.Float64, nil
		}
		return arrow.BinaryTypes.Binary, nil
	case FactKindError, FactKindWarning, FactKindInfo:
		if tableKind == TableKindSampled {
			return arrow.StructOf(
				arrow.Field{Name: "timestamp", Type: arrow.PrimitiveTypes.Uint64, Nullable: false},
				arrow.Field{Name: "values", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: false},
			), nil
		}
		return arrow.PrimitiveTypes.Int64, nil
	default:
		return nil, fmt.Errorf("fact %q has unsupported kind %q", field.Name, field.FactKind)
	}
}

func appendReserved(builders []array.Builder, kind TableKind, row WideRow) int {
	switch kind {
	case TableKindAggregated:
		appendString(builders[0], rowPath(row))
		return len(reservedWideFields(kind))
	case TableKindSampled:
		appendString(builders[0], row.TraceID)
		appendString(builders[1], row.EventID)
		if row.ParentEventID == "" {
			builders[2].AppendNull()
		} else {
			appendString(builders[2], row.ParentEventID)
		}
		appendString(builders[3], rowPath(row))
		start, end := rowBounds(row)
		appendUint64(builders[4], millisUint64(start))
		appendUint64(builders[5], millisUint64(end))
		appendFloat64(builders[6], row.SampleProbability)
		appendFloat64(builders[7], durationMillis(start, end))
		return len(reservedWideFields(kind))
	default:
		return 0
	}
}

func rowBounds(row WideRow) (time.Time, time.Time) {
	start := row.StartTime
	if start.IsZero() {
		start = row.Timestamp
	}
	end := row.EndTime
	if end.IsZero() {
		end = start
	}
	return start, end
}

func rowPath(row WideRow) string {
	if row.Path != "" {
		return row.Path
	}
	if row.SpanName != "" && row.EventType != "" && row.SpanName != row.EventType {
		return childEventPath(row.SpanName, row.EventType)
	}
	if row.EventType != "" {
		return row.EventType
	}
	if row.SpanName != "" {
		return row.SpanName
	}
	return "unknown"
}

func durationMillis(start, end time.Time) float64 {
	if end.Before(start) {
		return 0
	}
	return float64(end.Sub(start)) / float64(time.Millisecond)
}

func appendWideDynamic(builder array.Builder, tableKind TableKind, row WideRow, field FieldSchema) error {
	var (
		value TypedValue
		ok    bool
	)
	switch field.Role {
	case FieldRoleDimension:
		value, ok = row.Dimensions[field.Name]
		if ok {
			appendString(builder, value.String())
			return nil
		}
	case FieldRoleAttribute:
		value, ok = row.Attributes[field.Name]
		if ok {
			appendString(builder, value.String())
			return nil
		}
	case FieldRoleFact:
		if tableKind == TableKindAggregated && field.FactKind == FactKindHistogram {
			aggregate, ok := row.Histograms[field.Name]
			if ok {
				return appendHistogramAggregate(builder, aggregate)
			}
			break
		}
		if tableKind == TableKindSampled && field.FactKind.logLike() {
			fact, ok := row.LogFacts[field.Name]
			if ok {
				return appendLogFact(builder, fact)
			}
			break
		}
		fact, ok := row.Facts[field.Name]
		if ok {
			return appendFact(builder, fact)
		}
	}
	builder.AppendNull()
	return nil
}

func appendFact(builder array.Builder, value float64) error {
	switch b := builder.(type) {
	case *array.Float64Builder:
		b.Append(value)
	case *array.Int64Builder:
		value, err := roundedInt64(value)
		if err != nil {
			return err
		}
		b.Append(value)
	default:
		return fmt.Errorf("unsupported fact column builder %T", builder)
	}
	return nil
}

func appendLogFact(builder array.Builder, fact Fact) error {
	structBuilder, ok := builder.(*array.StructBuilder)
	if !ok {
		return fmt.Errorf("unsupported log fact builder %T", builder)
	}
	structBuilder.Append(true)
	timestampBuilder := structBuilder.FieldBuilder(0).(*array.Uint64Builder)
	timestampBuilder.Append(millisUint64(fact.LogTimestamp()))

	listBuilder := structBuilder.FieldBuilder(1).(*array.ListBuilder)
	listBuilder.Append(true)
	valueBuilder := listBuilder.ValueBuilder().(*array.StringBuilder)
	for _, value := range fact.LogValues() {
		valueBuilder.Append(value)
	}
	return nil
}

func appendHistogramAggregate(builder array.Builder, value HistogramAggregate) error {
	binaryBuilder, ok := builder.(*array.BinaryBuilder)
	if !ok {
		return fmt.Errorf("unsupported histogram aggregate builder %T", builder)
	}
	encoded, err := encodeDDSketch(value)
	if err != nil {
		return err
	}
	binaryBuilder.Append(encoded)
	return nil
}

func encodeDDSketch(value HistogramAggregate) ([]byte, error) {
	if value.Count == 0 {
		return nil, fmt.Errorf("histogram aggregate count is zero")
	}
	sketch, err := ddsketch.NewDefaultDDSketch(wideDDSketchRelativeAccuracy)
	if err != nil {
		return nil, err
	}
	if len(value.Samples) == 0 {
		mean := 0.0
		if value.Sum != 0 {
			mean = value.Sum / float64(value.Count)
		}
		if err := sketch.AddWithCount(mean, float64(value.Count)); err != nil {
			return nil, err
		}
		return proto.Marshal(sketch.ToProto())
	}
	countPerSample := float64(value.Count) / float64(len(value.Samples))
	for _, sample := range value.Samples {
		if err := sketch.AddWithCount(sample, countPerSample); err != nil {
			return nil, err
		}
	}
	return proto.Marshal(sketch.ToProto())
}

func roundedInt64(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("counter value must be finite, got %v", value)
	}
	rounded := math.Round(value)
	maxInt64 := int64(^uint64(0) >> 1)
	minInt64 := -maxInt64 - 1
	if rounded >= float64(maxInt64) || rounded < float64(minInt64) {
		return 0, fmt.Errorf("counter value %v is outside int64 range", value)
	}
	return int64(rounded), nil
}

func appendString(builder array.Builder, value string) {
	builder.(*array.StringBuilder).Append(value)
}

func appendNullableString(builder array.Builder, value string) {
	if value == "" {
		builder.AppendNull()
		return
	}
	appendString(builder, value)
}

func appendUint64(builder array.Builder, value uint64) {
	builder.(*array.Uint64Builder).Append(value)
}

func appendFloat64(builder array.Builder, value float64) {
	builder.(*array.Float64Builder).Append(value)
}

func millis(value time.Time) int64 {
	return value.UnixNano() / int64(time.Millisecond)
}

func millisUint64(value time.Time) uint64 {
	if value.IsZero() {
		return 0
	}
	ms := millis(value)
	if ms < 0 {
		return 0
	}
	return uint64(ms)
}
