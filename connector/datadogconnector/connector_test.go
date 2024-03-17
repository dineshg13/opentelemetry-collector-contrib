// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogconnector

import (
	"context"
	"testing"
	"time"

	pb "github.com/DataDog/datadog-agent/pkg/proto/pbgo/trace"
	"github.com/DataDog/opentelemetry-mapping-go/pkg/otlp/attributes"
	otlpmetrics "github.com/DataDog/opentelemetry-mapping-go/pkg/otlp/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/connector/connectortest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/collector/semconv/v1.17.0"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var _ component.Component = (*traceToMetricConnector)(nil) // testing that the connectorImp properly implements the type Component interface

// create test to create a connector, check that basic code compiles
func TestNewConnector(t *testing.T) {
	factory := NewFactory()

	creationParams := connectortest.NewNopCreateSettings()
	cfg := factory.CreateDefaultConfig().(*Config)

	traceToMetricsConnector, err := factory.CreateTracesToMetrics(context.Background(), creationParams, cfg, consumertest.NewNop())
	assert.NoError(t, err)

	_, ok := traceToMetricsConnector.(*traceToMetricConnector)
	assert.True(t, ok) // checks if the created connector implements the connectorImp struct
}

func TestTraceToTraceConnector(t *testing.T) {
	factory := NewFactory()

	creationParams := connectortest.NewNopCreateSettings()
	cfg := factory.CreateDefaultConfig().(*Config)

	traceToTracesConnector, err := factory.CreateTracesToTraces(context.Background(), creationParams, cfg, consumertest.NewNop())
	assert.NoError(t, err)

	_, ok := traceToTracesConnector.(*traceToTraceConnector)
	assert.True(t, ok) // checks if the created connector implements the connectorImp struct
}

var (
	spanStartTimestamp = pcommon.NewTimestampFromTime(time.Date(2020, 2, 11, 20, 26, 12, 321, time.UTC))
	spanEventTimestamp = pcommon.NewTimestampFromTime(time.Date(2020, 2, 11, 20, 26, 13, 123, time.UTC))
	spanEndTimestamp   = pcommon.NewTimestampFromTime(time.Date(2020, 2, 11, 20, 26, 13, 789, time.UTC))
)

func GenerateTrace() ptrace.Traces {
	td := ptrace.NewTraces()
	res := td.ResourceSpans().AppendEmpty().Resource()
	res.Attributes().EnsureCapacity(3)
	res.Attributes().PutStr("resource-attr1", "resource-attr-val1")
	res.Attributes().PutStr("container.id", "my-container-id")
	res.Attributes().PutStr("cloud.availability_zone", "my-zone")
	res.Attributes().PutStr("cloud.region", "my-region")

	ss := td.ResourceSpans().At(0).ScopeSpans().AppendEmpty().Spans()
	ss.EnsureCapacity(1)
	fillSpanOne(ss.AppendEmpty())
	return td
}

func fillSpanOne(span ptrace.Span) {
	span.SetName("operationA")
	span.SetStartTimestamp(spanStartTimestamp)
	span.SetEndTimestamp(spanEndTimestamp)
	span.SetDroppedAttributesCount(1)
	span.SetTraceID([16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10})
	span.SetSpanID([8]byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18})
	evs := span.Events()
	ev0 := evs.AppendEmpty()
	ev0.SetTimestamp(spanEventTimestamp)
	ev0.SetName("event-with-attr")
	ev0.Attributes().PutStr("span-event-attr", "span-event-attr-val")
	ev0.SetDroppedAttributesCount(2)
	ev1 := evs.AppendEmpty()
	ev1.SetTimestamp(spanEventTimestamp)
	ev1.SetName("event")
	ev1.SetDroppedAttributesCount(2)
	span.SetDroppedEventsCount(1)
	status := span.Status()
	status.SetCode(ptrace.StatusCodeError)
	status.SetMessage("status-cancelled")
}

func TestContainerTags(t *testing.T) {
	factory := NewFactory()

	creationParams := connectortest.NewNopCreateSettings()
	cfg := factory.CreateDefaultConfig().(*Config)
	cfg.Traces.ResourceAttributesAsContainerTags = []string{semconv.AttributeCloudAvailabilityZone, semconv.AttributeCloudRegion}
	metricsSink := &consumertest.MetricsSink{}

	traceToMetricsConnector, err := factory.CreateTracesToMetrics(context.Background(), creationParams, cfg, metricsSink)
	assert.NoError(t, err)

	connector, ok := traceToMetricsConnector.(*traceToMetricConnector)
	err = connector.Start(context.Background(), componenttest.NewNopHost())
	if err != nil {
		t.Errorf("Error starting connector: %v", err)
		return
	}
	defer func() {
		_ = connector.Shutdown(context.Background())
	}()

	assert.True(t, ok) // checks if the created connector implements the connectorImp struct
	trace1 := GenerateTrace()

	err = connector.ConsumeTraces(context.Background(), trace1)
	assert.NoError(t, err)

	// Send two traces to ensure unique container tags are added to the cache
	trace2 := GenerateTrace()
	err = connector.ConsumeTraces(context.Background(), trace2)
	assert.NoError(t, err)
	// check if the container tags are added to the cache
	assert.Equal(t, 1, len(connector.containerTagCache.Items()))
	assert.Equal(t, 2, len(connector.containerTagCache.Items()["my-container-id"].Object.(map[string]struct{})))

	for {
		if len(metricsSink.AllMetrics()) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// check if the container tags are added to the metrics
	metrics := metricsSink.AllMetrics()
	assert.Equal(t, 1, len(metrics))

	ch := make(chan []byte, 100)
	tr := newTranslatorWithStatsChannel(t, zap.NewNop(), ch)
	_, err = tr.MapMetrics(context.Background(), metrics[0], nil)
	require.NoError(t, err)
	msg := <-ch
	sp := &pb.StatsPayload{}

	err = proto.Unmarshal(msg, sp)
	require.NoError(t, err)

	tags := sp.Stats[0].Tags
	assert.Equal(t, 2, len(tags))
	assert.ElementsMatch(t, []string{"region:my-region", "zone:my-zone"}, tags)
}

func newTranslatorWithStatsChannel(t *testing.T, logger *zap.Logger, ch chan []byte) *otlpmetrics.Translator {
	options := []otlpmetrics.TranslatorOption{
		otlpmetrics.WithHistogramMode(otlpmetrics.HistogramModeDistributions),

		otlpmetrics.WithNumberMode(otlpmetrics.NumberModeCumulativeToDelta),
		otlpmetrics.WithHistogramAggregations(),
		otlpmetrics.WithStatsOut(ch),
	}

	set := componenttest.NewNopTelemetrySettings()
	set.Logger = logger

	attributesTranslator, err := attributes.NewTranslator(set)
	require.NoError(t, err)
	tr, err := otlpmetrics.NewTranslator(
		set,
		attributesTranslator,
		options...,
	)

	require.NoError(t, err)
	return tr
}
