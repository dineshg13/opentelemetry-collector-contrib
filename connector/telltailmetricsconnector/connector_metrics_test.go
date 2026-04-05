// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telltailmetricsconnector

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

func newTestMetricsConnector(t *testing.T, opts ...func(*metricsConnector)) (*metricsConnector, *consumertest.MetricsSink) {
	t.Helper()
	sink := &consumertest.MetricsSink{}
	c := &metricsConnector{
		logger:             zap.NewNop(),
		metricsConsumer:    sink,
		excludeFromMetrics: map[string]struct{}{},
	}
	for _, o := range opts {
		o(c)
	}
	return c, sink
}

func addMetricEvent(span ptrace.Span, name, typ string, value float64, ts pcommon.Timestamp, extraAttrs map[string]string) {
	event := span.Events().AppendEmpty()
	event.SetName("metric")
	event.SetTimestamp(ts)
	event.Attributes().PutStr(attrMetricName, name)
	event.Attributes().PutStr(attrMetricType, typ)
	event.Attributes().PutDouble(attrMetricValue, value)
	for k, v := range extraAttrs {
		event.Attributes().PutStr(k, v)
	}
}

func TestExtractSingleCounter(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test-svc")
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("foo.shape")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.foo.shape.count", "counter", 1.0, ts, map[string]string{
		"env":    "dev",
		"status": "ok",
	})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	require.Equal(t, 1, sink.DataPointCount())

	md := sink.AllMetrics()[0]
	rm := md.ResourceMetrics().At(0)
	sm := rm.ScopeMetrics().At(0)
	m := sm.Metrics().At(0)

	assert.Equal(t, "traced.foo.shape.count", m.Name())
	assert.Equal(t, pmetric.MetricTypeSum, m.Type())
	assert.Equal(t, pmetric.AggregationTemporalityDelta, m.Sum().AggregationTemporality())
	assert.True(t, m.Sum().IsMonotonic())

	dp := m.Sum().DataPoints().At(0)
	assert.Equal(t, 1.0, dp.DoubleValue())
	assert.Equal(t, ts, dp.Timestamp())
	assert.Equal(t, ts, dp.StartTimestamp())

	// Check dimensions
	envVal, ok := dp.Attributes().Get("env")
	assert.True(t, ok)
	assert.Equal(t, "dev", envVal.Str())

	statusVal, ok := dp.Attributes().Get("status")
	assert.True(t, ok)
	assert.Equal(t, "ok", statusVal.Str())
}

func TestExtractSingleHistogram(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("foo.shape")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.foo.shape.height", "histogram", 42.0, ts, map[string]string{
		"env": "dev",
	})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	require.Equal(t, 1, sink.DataPointCount())

	md := sink.AllMetrics()[0]
	m := md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0)

	assert.Equal(t, "traced.foo.shape.height", m.Name())
	assert.Equal(t, pmetric.MetricTypeExponentialHistogram, m.Type())
	assert.Equal(t, pmetric.AggregationTemporalityDelta, m.ExponentialHistogram().AggregationTemporality())

	dp := m.ExponentialHistogram().DataPoints().At(0)
	assert.Equal(t, uint64(1), dp.Count())
	assert.Equal(t, 42.0, dp.Sum())
	assert.Equal(t, 42.0, dp.Min())
	assert.Equal(t, 42.0, dp.Max())
	assert.Equal(t, ts, dp.Timestamp())
	assert.Equal(t, ts, dp.StartTimestamp())

	envVal, ok := dp.Attributes().Get("env")
	assert.True(t, ok)
	assert.Equal(t, "dev", envVal.Str())
}

func TestMixedEvents(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("foo.shape")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.foo.shape.count", "counter", 1.0, ts, nil)
	addMetricEvent(span, "traced.foo.shape.height", "histogram", 42.0, ts, nil)

	// Add a non-metric event
	logEvent := span.Events().AppendEmpty()
	logEvent.SetName("log.info")
	logEvent.Attributes().PutStr("message", "rendered shape")

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	require.Equal(t, 2, sink.DataPointCount())
}

func TestDimensionExtraction(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.foo.shape.height", "histogram", 42.0, ts, map[string]string{
		"env":   "dev",
		"team":  "example",
		"shape": "square",
		"color": "blue",
	})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	dp := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).ExponentialHistogram().DataPoints().At(0)

	// Reserved attributes should NOT be present
	_, hasName := dp.Attributes().Get(attrMetricName)
	assert.False(t, hasName)
	_, hasType := dp.Attributes().Get(attrMetricType)
	assert.False(t, hasType)
	_, hasValue := dp.Attributes().Get(attrMetricValue)
	assert.False(t, hasValue)

	// Dimension attributes should be present
	for _, key := range []string{"env", "team", "shape", "color"} {
		_, ok := dp.Attributes().Get(key)
		assert.True(t, ok, "expected dimension %q to be present", key)
	}
}

func TestMissingRequiredAttributes(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	// Event missing metric.value
	event := span.Events().AppendEmpty()
	event.SetName("metric")
	event.Attributes().PutStr(attrMetricName, "traced.foo.count")
	event.Attributes().PutStr(attrMetricType, "counter")

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	assert.Empty(t, sink.AllMetrics())
}

func TestEmptyTraces(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	assert.Empty(t, sink.AllMetrics())
}

func TestSyntheticFlushSpan(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("telltail.metric_flush")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.foo.shape.height", "histogram", 42.0, ts, map[string]string{"env": "dev"})
	addMetricEvent(span, "traced.foo.shape.count", "counter", 1.0, ts, map[string]string{"env": "dev", "status": "ok"})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	assert.Equal(t, 2, sink.DataPointCount())
}

func TestSameMetricMultipleDataPoints(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	ts1 := pcommon.Timestamp(1711900000000000000)
	ts2 := pcommon.Timestamp(1711900001000000000)
	addMetricEvent(span, "traced.foo.shape.height", "histogram", 42.0, ts1, nil)
	addMetricEvent(span, "traced.foo.shape.height", "histogram", 13.0, ts2, nil)

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	md := sink.AllMetrics()[0]
	// Both data points should be aggregated into one (same dims = empty map).
	m := md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0)
	assert.Equal(t, "traced.foo.shape.height", m.Name())
	// With aggregation, same empty dims → single data point with count=2
	dp := m.ExponentialHistogram().DataPoints().At(0)
	assert.Equal(t, uint64(2), dp.Count())
	assert.Equal(t, 55.0, dp.Sum()) // 42 + 13
}

func TestUnknownMetricType(t *testing.T) {
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.foo.shape.height", "gauge", 42.0, ts, nil)

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	assert.Empty(t, sink.AllMetrics())
}

// --- New tests for attribute routing ---

func TestExcludeAttributeFromMetrics(t *testing.T) {
	c, sink := newTestMetricsConnector(t, func(mc *metricsConnector) {
		mc.excludeFromMetrics = map[string]struct{}{"color": {}}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{
		"env":   "dev",
		"color": "blue",
	})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	dp := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints().At(0)

	_, hasColor := dp.Attributes().Get("color")
	assert.False(t, hasColor, "color should be excluded from metrics")

	envVal, ok := dp.Attributes().Get("env")
	assert.True(t, ok)
	assert.Equal(t, "dev", envVal.Str())
}

func TestCounterAggregationOnExclusion(t *testing.T) {
	c, sink := newTestMetricsConnector(t, func(mc *metricsConnector) {
		mc.excludeFromMetrics = map[string]struct{}{"color": {}}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	ts := pcommon.Timestamp(1711900000000000000)
	// Two events that differ only by "color" (which is excluded) → should aggregate.
	addMetricEvent(span, "traced.count", "counter", 3.0, ts, map[string]string{
		"env":   "dev",
		"color": "red",
	})
	addMetricEvent(span, "traced.count", "counter", 7.0, ts, map[string]string{
		"env":   "dev",
		"color": "blue",
	})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	m := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0)
	assert.Equal(t, 1, m.Sum().DataPoints().Len(), "should be aggregated into a single data point")
	assert.Equal(t, 10.0, m.Sum().DataPoints().At(0).DoubleValue())
}

func TestHistogramAggregationOnExclusion(t *testing.T) {
	c, sink := newTestMetricsConnector(t, func(mc *metricsConnector) {
		mc.excludeFromMetrics = map[string]struct{}{"color": {}}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.height", "histogram", 10.0, ts, map[string]string{
		"env":   "dev",
		"color": "red",
	})
	addMetricEvent(span, "traced.height", "histogram", 20.0, ts, map[string]string{
		"env":   "dev",
		"color": "blue",
	})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	m := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0)
	assert.Equal(t, 1, m.ExponentialHistogram().DataPoints().Len(), "should be aggregated into a single data point")

	dp := m.ExponentialHistogram().DataPoints().At(0)
	assert.Equal(t, uint64(2), dp.Count())
	assert.Equal(t, 30.0, dp.Sum()) // 10 + 20
}

func TestIncludeSpanAttrAsDimension(t *testing.T) {
	c, sink := newTestMetricsConnector(t, func(mc *metricsConnector) {
		mc.includeSpanAttrs = []string{"http.method"}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.Attributes().PutStr("http.method", "GET")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{"env": "dev"})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	dp := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints().At(0)

	methodVal, ok := dp.Attributes().Get("http.method")
	assert.True(t, ok, "span attr should be included as dimension")
	assert.Equal(t, "GET", methodVal.Str())
}

func TestIncludeSpanAttrMissing(t *testing.T) {
	c, sink := newTestMetricsConnector(t, func(mc *metricsConnector) {
		mc.includeSpanAttrs = []string{"http.method"}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	// Span does NOT have http.method

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{"env": "dev"})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	dp := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints().At(0)

	_, ok := dp.Attributes().Get("http.method")
	assert.False(t, ok, "missing span attr should not appear as dimension")
}

func TestEventAttrWinsOverSpanAttr(t *testing.T) {
	c, sink := newTestMetricsConnector(t, func(mc *metricsConnector) {
		mc.includeSpanAttrs = []string{"http.method"}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.Attributes().PutStr("http.method", "GET")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{
		"http.method": "POST",
	})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	dp := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints().At(0)

	methodVal, ok := dp.Attributes().Get("http.method")
	assert.True(t, ok)
	assert.Equal(t, "POST", methodVal.Str(), "event attribute should win over span attribute")
}

func TestDefaultConfigUnchanged(t *testing.T) {
	// Empty config (no MoveToTraces, no MoveToMetrics) should produce identical
	// output to the original behavior.
	c, sink := newTestMetricsConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 5.0, ts, map[string]string{
		"color": "red",
		"env":   "dev",
	})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	dp := sink.AllMetrics()[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints().At(0)
	assert.Equal(t, 5.0, dp.DoubleValue())

	// Both color and env should be present (nothing excluded).
	_, hasColor := dp.Attributes().Get("color")
	assert.True(t, hasColor)
	_, hasEnv := dp.Attributes().Get("env")
	assert.True(t, hasEnv)
}
