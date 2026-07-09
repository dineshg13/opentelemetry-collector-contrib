// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/DataDog/sketches-go/ddsketch"
	"github.com/DataDog/sketches-go/ddsketch/pb/sketchpb"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"google.golang.org/protobuf/proto"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter/internal/widepb"
)

// wireEnvelopeDump is the JSON-friendly view of a serialized WideTelemetryEnvelope.
// Field names mirror the wide-ingest schema (WIDE_INGEST_SCHEMA.md) so a dump can be
// diffed directly against the contract. It is produced by decoding the exact protobuf
// + Arrow IPC bytes that are POSTed to the intake, so it reflects the wire format
// rather than the exporter's internal structs.
type wireEnvelopeDump struct {
	Version            uint32            `json:"version"`
	Host               string            `json:"host"`
	Service            string            `json:"service"`
	FlushWindowStartMs uint64            `json:"flush_window_start_ms"`
	FlushWindowEndMs   uint64            `json:"flush_window_end_ms"`
	Tags               map[string]string `json:"tags,omitempty"`
	Tables             []wireTableDump   `json:"tables"`
}

type wireTableDump struct {
	EventType string           `json:"event_type"`
	Kind      string           `json:"kind"`
	Schema    json.RawMessage  `json:"schema_json"`
	Rows      []map[string]any `json:"rows"`
}

// decodeWireEnvelope reverses serialization: protobuf → tables → decoded Arrow rows.
func decodeWireEnvelope(se SerializedEnvelope) (wireEnvelopeDump, error) {
	var env widepb.WideTelemetryEnvelope
	if err := proto.Unmarshal(se.Payload, &env); err != nil {
		return wireEnvelopeDump{}, fmt.Errorf("unmarshal envelope: %w", err)
	}
	dump := wireEnvelopeDump{
		Version:            env.GetVersion(),
		Host:               env.GetHost(),
		Service:            env.GetService(),
		FlushWindowStartMs: env.GetFlushWindowStartMs(),
		FlushWindowEndMs:   env.GetFlushWindowEndMs(),
		Tags:               env.GetTags(),
	}
	for _, table := range env.GetTables() {
		rows, err := decodeArrowRows(table.GetArrowIpc())
		if err != nil {
			return wireEnvelopeDump{}, fmt.Errorf("decode arrow for %q: %w", table.GetEventType(), err)
		}
		dump.Tables = append(dump.Tables, wireTableDump{
			EventType: table.GetEventType(),
			Kind:      table.GetKind().String(),
			Schema:    json.RawMessage(table.GetSchemaJson()),
			Rows:      rows,
		})
	}
	return dump, nil
}

// decodeArrowRows reads an Arrow IPC stream into one map per row, keyed by column name.
func decodeArrowRows(ipcBytes []byte) ([]map[string]any, error) {
	if len(ipcBytes) == 0 {
		return nil, nil
	}
	reader, err := ipc.NewReader(bytes.NewReader(ipcBytes))
	if err != nil {
		return nil, err
	}
	defer reader.Release()

	var rows []map[string]any
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		fields := record.Schema().Fields()
		columns := record.Columns()
		for r := 0; r < int(record.NumRows()); r++ {
			row := make(map[string]any, len(columns))
			for c, column := range columns {
				row[fields[c].Name] = arrowValueAt(column, r)
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// arrowValueAt extracts one cell as a JSON-friendly Go value. DDSketch histogram bytes
// are decoded to a numeric summary; log-like structs to {timestamp, values}.
func arrowValueAt(column arrow.Array, i int) any {
	if column.IsNull(i) {
		return nil
	}
	switch arr := column.(type) {
	case *array.String:
		return arr.Value(i)
	case *array.Int64:
		return arr.Value(i)
	case *array.Uint64:
		return arr.Value(i)
	case *array.Float64:
		return arr.Value(i)
	case *array.Binary:
		return decodeDDSketchSummary(arr.Value(i))
	case *array.Struct:
		return decodeLogStruct(arr, i)
	default:
		return fmt.Sprintf("<unsupported arrow type %s>", column.DataType())
	}
}

// decodeLogStruct renders a sampled log-like fact (Struct<timestamp, values>) as a map.
func decodeLogStruct(arr *array.Struct, i int) any {
	out := map[string]any{}
	if ts, ok := arr.Field(0).(*array.Uint64); ok {
		out["timestamp"] = ts.Value(i)
	}
	list, ok := arr.Field(1).(*array.List)
	if !ok {
		return out
	}
	values, ok := list.ListValues().(*array.String)
	if !ok {
		return out
	}
	start, end := list.ValueOffsets(i)
	strs := make([]string, 0, end-start)
	for j := start; j < end; j++ {
		strs = append(strs, values.Value(int(j)))
	}
	out["values"] = strs
	return out
}

// decodeDDSketchSummary decodes an aggregated histogram column (DDSketch protobuf) into
// a lossy numeric summary that is readable in a JSON dump.
func decodeDDSketchSummary(raw []byte) any {
	var pb sketchpb.DDSketch
	if err := proto.Unmarshal(raw, &pb); err != nil {
		return map[string]any{"ddsketch_decode_error": err.Error(), "bytes": len(raw)}
	}
	sketch, err := ddsketch.FromProto(&pb)
	if err != nil {
		return map[string]any{"ddsketch_decode_error": err.Error(), "bytes": len(raw)}
	}
	summary := map[string]any{
		"count": sketch.GetCount(),
		"sum":   sketch.GetSum(),
		"zeros": sketch.GetZeroCount(),
	}
	if min, err := sketch.GetMinValue(); err == nil {
		summary["min"] = min
	}
	if max, err := sketch.GetMaxValue(); err == nil {
		summary["max"] = max
	}
	for _, q := range []float64{0.5, 0.75, 0.9, 0.95, 0.99} {
		if v, err := sketch.GetValueAtQuantile(q); err == nil {
			summary[fmt.Sprintf("p%02d", int(q*100))] = v
		}
	}
	return summary
}
