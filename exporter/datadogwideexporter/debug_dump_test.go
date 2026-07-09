// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// decodeWireEnvelope must reproduce the wire schema: envelope fields, per-table
// event_type/kind/schema_json, reserved Arrow columns, tag/fact columns, and a
// readable DDSketch summary for aggregated histograms.
func TestDecodeWireEnvelopeMatchesSchema(t *testing.T) {
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

	envelopes, err := NewSerializer(EnvelopeIdentity{Host: "h", Service: "svc"}).
		Serialize(t.Context(), []WideTable{table})
	require.NoError(t, err)
	require.Len(t, envelopes, 1)

	dump, err := decodeWireEnvelope(envelopes[0])
	require.NoError(t, err)

	require.Equal(t, uint32(2), dump.Version)
	require.Equal(t, "svc", dump.Service)
	require.Len(t, dump.Tables, 1)

	tbl := dump.Tables[0]
	require.Equal(t, "netflow.processEvent", tbl.EventType)
	require.Equal(t, "TABLE_KIND_AGGREGATED", tbl.Kind)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(tbl.Schema, &schema))
	require.Contains(t, schema, "columns")

	require.Len(t, tbl.Rows, 1)
	row := tbl.Rows[0]
	require.Equal(t, "netflow.process/netflow.processEvent", row["_path"]) // reserved column
	require.Equal(t, "us-east-1", row["region"])                           // tag column
	require.EqualValues(t, 12, row["requests"])                            // counter -> Int64

	// Aggregated histogram decodes to a numeric DDSketch summary, not raw bytes.
	summary, ok := row["latency_ms"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 2, summary["count"])
	require.Contains(t, summary, "p50")
}
