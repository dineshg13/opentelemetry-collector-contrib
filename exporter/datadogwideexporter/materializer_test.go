// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func aggEvent(eventType string, dims map[string]TypedValue) WideEvent {
	return WideEvent{
		Kind:       EventKindMetric,
		EventType:  eventType,
		Timestamp:  time.Unix(10, 0),
		Dimensions: dims,
		Facts:      map[string]Fact{"count": CounterFact(1, "")},
	}
}

func sampledSpanEvent(spanID string) WideEvent {
	return WideEvent{
		Kind:              EventKindSpan,
		EventType:         "svc.op",
		SpanName:          "svc.op",
		Timestamp:         time.Unix(10, 0),
		TraceID:           "01020300000000000000000000000000",
		SpanID:            spanID,
		Sampled:           true,
		SampleProbability: 1,
		Facts:             map[string]Fact{"count": CounterFact(1, "")},
	}
}

func TestMaterializerCapsAggregateBuckets(t *testing.T) {
	m := NewMaterializer(WithLimits(MaterializerLimits{MaxAggregateBuckets: 2}))

	for i := range 3 {
		require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"k": Int64Value(int64(i))})))
	}
	require.Len(t, m.buckets, 2)
	require.Equal(t, uint64(1), m.droppedBuckets)

	// An admitted bucket keeps aggregating; the cap does not evict it.
	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"k": Int64Value(0)})))
	require.Len(t, m.buckets, 2)
	require.Equal(t, uint64(1), m.droppedBuckets)
}

func TestMaterializerCapsSampledRows(t *testing.T) {
	m := NewMaterializer(WithLimits(MaterializerLimits{MaxSampledRows: 2}))

	for i := range 3 {
		require.NoError(t, m.Add(sampledSpanEvent(fmt.Sprintf("040506000000000%d", i))))
	}
	require.Len(t, m.sampledRows, 2)
	require.Equal(t, uint64(1), m.droppedSampledRows)
}

func TestMaterializerCapsSchemas(t *testing.T) {
	m := NewMaterializer(WithLimits(MaterializerLimits{MaxSchemas: 2}))

	for i := range 3 {
		require.NoError(t, m.Add(aggEvent(fmt.Sprintf("t%d", i), nil)))
	}
	require.Len(t, m.schemas, 2)
	require.Equal(t, uint64(1), m.droppedSchemas)
	// The event for the dropped identity is fully dropped, so no bucket leaks in.
	require.Len(t, m.buckets, 2)
}

func TestMaterializerLogsDropsOncePerWindow(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	m := NewMaterializer(
		WithLimits(MaterializerLimits{MaxAggregateBuckets: 1}),
		WithLogger(zap.New(core)),
	)

	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"k": Int64Value(0)})))
	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"k": Int64Value(1)}))) // dropped

	_, windowEnd, err := m.flushSnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, logs.FilterMessageSnippet("dropped telemetry").Len())

	// Counters reset after reset; a clean window logs nothing.
	m.reset(windowEnd)
	require.Zero(t, m.droppedBuckets)
	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"k": Int64Value(0)})))
	_, _, err = m.flushSnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, logs.FilterMessageSnippet("dropped telemetry").Len())
}

func TestMaterializerDropsDimensionTypeConflict(t *testing.T) {
	m := NewMaterializer()

	// First-seen type wins; the conflicting event is dropped whole and counted,
	// with no error surfaced to the caller (so the batch is not abandoned).
	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"dimensions.cpu": StringValue("cpu0")})))
	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"dimensions.cpu": Int64Value(0)})))

	require.Equal(t, uint64(1), m.droppedConflicts)
	require.Equal(t, ValueString, m.schemas["t"]["dimensions.cpu"].Type)
	require.Len(t, m.buckets, 1)
}

func TestMaterializerDropsUnitConflict(t *testing.T) {
	m := NewMaterializer()

	unitEvent := func(unit string) WideEvent {
		return WideEvent{
			Kind:      EventKindMetric,
			EventType: "t",
			Timestamp: time.Unix(10, 0),
			Facts:     map[string]Fact{"count": CounterFact(1, unit)},
		}
	}

	require.NoError(t, m.Add(unitEvent("connections")))
	require.NoError(t, m.Add(unitEvent("1")))

	require.Equal(t, uint64(1), m.droppedConflicts)
	require.Equal(t, "connections", m.schemas["t"]["count"].Unit)
	require.Len(t, m.buckets, 1)
}

func TestMaterializerLogsSchemaConflictsOncePerWindow(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	m := NewMaterializer(WithLogger(zap.New(core)))

	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"dimensions.cpu": StringValue("cpu0")})))
	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"dimensions.cpu": Int64Value(0)}))) // conflict

	_, windowEnd, err := m.flushSnapshot(t.Context())
	require.NoError(t, err)
	entries := logs.FilterField(zap.Uint64("dropped_schema_conflicts", 1))
	require.Equal(t, 1, entries.Len())

	// reset zeroes the conflict counter; a clean window logs nothing new.
	m.reset(windowEnd)
	require.Zero(t, m.droppedConflicts)
	require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"dimensions.cpu": StringValue("cpu0")})))
	_, _, err = m.flushSnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, logs.FilterMessageSnippet("dropped telemetry").Len())
}

func TestMaterializerUnboundedByDefault(t *testing.T) {
	m := NewMaterializer()
	for i := range 50 {
		require.NoError(t, m.Add(aggEvent("t", map[string]TypedValue{"k": Int64Value(int64(i))})))
	}
	require.Len(t, m.buckets, 50)
	require.Zero(t, m.droppedBuckets)
}
