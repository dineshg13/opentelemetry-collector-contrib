// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DataDog/sketches-go/ddsketch/pb/sketchpb"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter/internal/widepb"
)

func TestSerializerEmitsWideV2AggregatedSchema(t *testing.T) {
	start := time.UnixMilli(1000).UTC()
	end := time.UnixMilli(11000).UTC()
	table := WideTable{
		Kind:        TableKindAggregated,
		Identity:    TableIdentity{EventType: "netflow.processEvent"},
		WindowStart: start,
		WindowEnd:   end,
		Schema: TableSchema{
			Fields: []FieldSchema{
				{Name: "region", Role: FieldRoleDimension, Type: ValueString},
				{Name: "requests", Role: FieldRoleFact, Type: ValueFloat64, FactKind: FactKindCounter},
				{Name: "latency_ms", Role: FieldRoleFact, Type: ValueFloat64, FactKind: FactKindHistogram},
			},
			Children: []string{"netflow.processEvent.dnsLookup"},
		},
		Rows: []WideRow{{
			EventType:  "netflow.processEvent",
			Path:       "netflow.process/netflow.processEvent",
			Dimensions: map[string]TypedValue{"region": StringValue("us-east-1")},
			Facts:      map[string]float64{"requests": 12},
			Histograms: map[string]HistogramAggregate{
				"latency_ms": {Count: 2, Sum: 30, Samples: []float64{10, 20}},
			},
		}},
	}

	envelopes, err := NewSerializer(EnvelopeIdentity{
		Host:    "worker-pod-abc",
		Service: "event-workers-netflow-worker",
		Tags:    map[string]string{"env": "prod", "empty": ""},
	}).Serialize(context.Background(), []WideTable{table})
	require.NoError(t, err)
	require.Len(t, envelopes, 1)

	var envelope widepb.WideTelemetryEnvelope
	require.NoError(t, proto.Unmarshal(envelopes[0].Payload, &envelope))
	require.Equal(t, uint32(2), envelope.GetVersion())
	require.Equal(t, "worker-pod-abc", envelope.GetHost())
	require.Equal(t, "event-workers-netflow-worker", envelope.GetService())
	require.Equal(t, uint64(1000), envelope.GetFlushWindowStartMs())
	require.Equal(t, uint64(11000), envelope.GetFlushWindowEndMs())
	require.Equal(t, map[string]string{"env": "prod"}, envelope.GetTags())
	require.Len(t, envelope.GetTables(), 1)

	pbTable := envelope.GetTables()[0]
	require.Equal(t, "netflow.processEvent", pbTable.GetEventType())
	require.Equal(t, widepb.TableKind_TABLE_KIND_AGGREGATED, pbTable.GetKind())
	assertWrappedSchemaJSON(t, pbTable.GetSchemaJson(), map[string]string{
		"region":     "tag",
		"requests":   "counter",
		"latency_ms": "histogram",
	}, []string{"netflow.processEvent.dnsLookup"})

	record := readSingleArrowRecord(t, pbTable.GetArrowIpc())
	defer record.Release()

	requireFieldNames(t, record.Schema().Fields(), []string{"_path", "region", "requests", "latency_ms"})
	require.Equal(t, arrow.STRING, record.Schema().Field(0).Type.ID())
	require.Equal(t, arrow.INT64, record.Schema().Field(2).Type.ID())
	require.Equal(t, arrow.BINARY, record.Schema().Field(3).Type.ID())
	require.Equal(t, "netflow.process/netflow.processEvent", record.Column(0).(*array.String).Value(0))
	require.Equal(t, int64(12), record.Column(2).(*array.Int64).Value(0))

	sketchBytes := record.Column(3).(*array.Binary).Value(0)
	require.NotEmpty(t, sketchBytes)
	var sketch sketchpb.DDSketch
	require.NoError(t, proto.Unmarshal(sketchBytes, &sketch))
	require.NotNil(t, sketch.Mapping)
}

func TestEncodeArrowIPCUsesLatestSampledReservedColumns(t *testing.T) {
	start := time.UnixMilli(1700000010000).UTC()
	end := time.UnixMilli(1700000010050).UTC()
	table := WideTable{
		Kind:        TableKindSampled,
		Identity:    TableIdentity{EventType: "netflow.processEvent"},
		WindowStart: start,
		WindowEnd:   end,
		Schema: TableSchema{
			Fields: []FieldSchema{
				{Name: "component", Role: FieldRoleDimension, Type: ValueString},
				{Name: "duration_ms", Role: FieldRoleFact, Type: ValueFloat64, FactKind: FactKindHistogram},
			},
		},
		Rows: []WideRow{{
			EventType:         "netflow.processEvent",
			Path:              "netflow.process/netflow.processEvent",
			TraceID:           "trace-1",
			EventID:           "event-1",
			ParentEventID:     "parent-1",
			StartTime:         start,
			EndTime:           end,
			SampleProbability: 0.5,
			Dimensions:        map[string]TypedValue{"component": StringValue("api")},
			Facts:             map[string]float64{"duration_ms": 50},
		}},
	}

	payload, err := EncodeArrowIPC(table)
	require.NoError(t, err)
	record := readSingleArrowRecord(t, payload)
	defer record.Release()

	requireFieldNames(t, record.Schema().Fields(), []string{
		"_trace_id",
		"_event_id",
		"_parent_event_id",
		"_path",
		"_start_ms",
		"_end_ms",
		"_sample_probability",
		"_duration_ms",
		"component",
		"duration_ms",
	})
	require.Equal(t, "trace-1", record.Column(0).(*array.String).Value(0))
	require.Equal(t, "event-1", record.Column(1).(*array.String).Value(0))
	require.Equal(t, "parent-1", record.Column(2).(*array.String).Value(0))
	require.Equal(t, "netflow.process/netflow.processEvent", record.Column(3).(*array.String).Value(0))
	require.Equal(t, uint64(1700000010000), record.Column(4).(*array.Uint64).Value(0))
	require.Equal(t, uint64(1700000010050), record.Column(5).(*array.Uint64).Value(0))
	require.Equal(t, 0.5, record.Column(6).(*array.Float64).Value(0))
	require.Equal(t, 50.0, record.Column(7).(*array.Float64).Value(0))
}

func TestEncodeArrowIPCUsesLogLikeFactShapes(t *testing.T) {
	timestamp := time.UnixMilli(1700000010025).UTC()
	table := WideTable{
		Kind:        TableKindSampled,
		Identity:    TableIdentity{EventType: "netflow.processEvent.log"},
		WindowStart: timestamp,
		WindowEnd:   timestamp.Add(10 * time.Second),
		Schema: TableSchema{
			Fields: []FieldSchema{{
				Name:     "error",
				Role:     FieldRoleFact,
				Type:     ValueString,
				FactKind: FactKindError,
				Pattern:  "{}",
			}},
		},
		Rows: []WideRow{{
			EventType:         "netflow.processEvent.log",
			Path:              "netflow.processEvent/netflow.processEvent.log",
			TraceID:           "trace-1",
			EventID:           "event-1",
			ParentEventID:     "parent-1",
			StartTime:         timestamp,
			EndTime:           timestamp,
			SampleProbability: 1,
			LogFacts: map[string]Fact{
				"error": LogFact(FactKindError, timestamp, "{}", []string{"api"}),
			},
		}},
	}

	schemaJSON, err := schemaJSONFor(table.Schema)
	require.NoError(t, err)
	assertWrappedSchemaJSON(t, schemaJSON, map[string]string{"error": "error"}, []string{})
	require.Contains(t, schemaJSON, `"pattern":"{}"`)

	payload, err := EncodeArrowIPC(table)
	require.NoError(t, err)
	record := readSingleArrowRecord(t, payload)
	defer record.Release()

	requireFieldNames(t, record.Schema().Fields(), []string{
		"_trace_id",
		"_event_id",
		"_parent_event_id",
		"_path",
		"_start_ms",
		"_end_ms",
		"_sample_probability",
		"_duration_ms",
		"error",
	})
	require.Equal(t, arrow.STRUCT, record.Schema().Field(8).Type.ID())

	logColumn := record.Column(8).(*array.Struct)
	require.False(t, logColumn.IsNull(0))
	require.Equal(t, uint64(1700000010025), logColumn.Field(0).(*array.Uint64).Value(0))
	values := logColumn.Field(1).(*array.List)
	start, end := values.ValueOffsets(0)
	valueData := values.ListValues().(*array.String)
	actual := make([]string, 0, end-start)
	for i := int(start); i < int(end); i++ {
		actual = append(actual, valueData.Value(i))
	}
	require.Equal(t, []string{"api"}, actual)
}

func assertWrappedSchemaJSON(t *testing.T, schemaJSON string, columns map[string]string, children []string) {
	t.Helper()
	var schema struct {
		Columns map[string]struct {
			Type string `json:"type"`
		} `json:"columns"`
		Children []string `json:"children"`
	}
	require.NoError(t, json.Unmarshal([]byte(schemaJSON), &schema))
	require.Len(t, schema.Columns, len(columns))
	for name, typ := range columns {
		require.Equal(t, typ, schema.Columns[name].Type)
	}
	require.Equal(t, children, schema.Children)
}

func readSingleArrowRecord(t *testing.T, payload []byte) arrow.Record {
	t.Helper()
	reader, err := ipc.NewReader(bytes.NewReader(payload))
	require.NoError(t, err)
	defer reader.Release()
	record, err := reader.Read()
	require.NoError(t, err)
	record.Retain()
	return record
}

func requireFieldNames(t *testing.T, fields []arrow.Field, expected []string) {
	t.Helper()
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	require.Equal(t, expected, names)
}
