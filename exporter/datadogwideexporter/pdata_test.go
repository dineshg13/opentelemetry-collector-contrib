// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestTraceMetricAggregateEventIsTopLevelOnly(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.API.Key = configopaque.String("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	exp, err := newWideExporter(exportertest.NewNopSettings(exportertest.NopType), cfg)
	require.NoError(t, err)

	traces := ptrace.NewTraces()
	span := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID([16]byte{1})
	span.SetSpanID([8]byte{2})
	span.SetName("calendar.get_date")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Unix(9, 0)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Unix(10, 0)))
	event := span.Events().AppendEmpty()
	event.SetName(metricAggregateEventName)
	event.SetTimestamp(pcommon.NewTimestampFromTime(time.Unix(10, 0)))
	event.Attributes().PutStr("metric.name", "calendar.requests")
	event.Attributes().PutStr("metric.type", string(metricTypeCounter))
	event.Attributes().PutDouble("metric.value", 5)

	spans, metrics := exp.tracesToObservations(traces)
	require.Len(t, spans, 1)
	require.Empty(t, spans[0].Metrics)
	require.Len(t, metrics, 1)
	require.NotNil(t, metrics[0].Aggregate)
	require.Empty(t, metrics[0].Samples)
}
