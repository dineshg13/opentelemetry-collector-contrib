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
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

func newTestTracesConnector(t *testing.T, opts ...func(*tracesConnector)) (*tracesConnector, *consumertest.TracesSink) {
	t.Helper()
	sink := &consumertest.TracesSink{}
	c := &tracesConnector{
		logger:         zap.NewNop(),
		tracesConsumer: sink,
	}
	for _, o := range opts {
		o(c)
	}
	return c, sink
}

func TestStripMetricAndKeepLogEvents(t *testing.T) {
	c, sink := newTestTracesConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("foo.shape")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.foo.shape.count", "counter", 1.0, ts, nil)

	logEvent := span.Events().AppendEmpty()
	logEvent.SetName("log.info")
	logEvent.Attributes().PutStr("message", "rendered shape")

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	require.Equal(t, 1, sink.SpanCount())

	forwarded := sink.AllTraces()[0]
	fSpan := forwarded.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, 1, fSpan.Events().Len())
	assert.Equal(t, "log.info", fSpan.Events().At(0).Name())
}

func TestStripAllMetricEvents(t *testing.T) {
	c, sink := newTestTracesConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("foo.shape")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.foo.shape.count", "counter", 1.0, ts, nil)
	addMetricEvent(span, "traced.foo.shape.height", "histogram", 42.0, ts, nil)

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	fSpan := sink.AllTraces()[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, 0, fSpan.Events().Len())
}

func TestNoMetricEventsUnchanged(t *testing.T) {
	c, sink := newTestTracesConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("foo.shape")

	logEvent := span.Events().AppendEmpty()
	logEvent.SetName("log.info")
	logEvent.Attributes().PutStr("message", "hello")

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	fSpan := sink.AllTraces()[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, 1, fSpan.Events().Len())
	assert.Equal(t, "log.info", fSpan.Events().At(0).Name())
}

func TestMultipleSpansStripped(t *testing.T) {
	c, sink := newTestTracesConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	ss := rs.ScopeSpans().AppendEmpty()

	ts := pcommon.Timestamp(1711900000000000000)

	span1 := ss.Spans().AppendEmpty()
	span1.SetName("span1")
	addMetricEvent(span1, "traced.span1.count", "counter", 1.0, ts, nil)
	logEvent := span1.Events().AppendEmpty()
	logEvent.SetName("log.info")

	span2 := ss.Spans().AppendEmpty()
	span2.SetName("span2")
	addMetricEvent(span2, "traced.span2.height", "histogram", 42.0, ts, nil)

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	forwarded := sink.AllTraces()[0]
	fSpan1 := forwarded.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	fSpan2 := forwarded.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(1)

	assert.Equal(t, 1, fSpan1.Events().Len())
	assert.Equal(t, "log.info", fSpan1.Events().At(0).Name())
	assert.Equal(t, 0, fSpan2.Events().Len())
}

func TestTracesForwardedToConsumer(t *testing.T) {
	c, sink := newTestTracesConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("test")

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)
	assert.Equal(t, 1, len(sink.AllTraces()))
}

// --- New tests for attribute promotion ---

func TestPromoteEventAttrToSpan(t *testing.T) {
	c, sink := newTestTracesConnector(t, func(tc *tracesConnector) {
		tc.promoteToSpan = []string{"color"}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("test")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{"color": "blue"})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	fSpan := sink.AllTraces()[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	colorVal, ok := fSpan.Attributes().Get("color")
	assert.True(t, ok, "promoted attr should be on span")
	assert.Equal(t, "blue", colorVal.Str())
}

func TestPromoteCollectsUniqueValues(t *testing.T) {
	c, sink := newTestTracesConnector(t, func(tc *tracesConnector) {
		tc.promoteToSpan = []string{"color"}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("test")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{"color": "red"})
	addMetricEvent(span, "traced.count", "counter", 2.0, ts, map[string]string{"color": "blue"})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	fSpan := sink.AllTraces()[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	colorVal, ok := fSpan.Attributes().Get("color")
	assert.True(t, ok, "promoted attr should be on span")
	assert.Equal(t, pcommon.ValueTypeSlice, colorVal.Type(), "multiple unique values should become a slice")
	assert.Equal(t, 2, colorVal.Slice().Len())

	// Collect values to check both are present (order may vary).
	vals := make(map[string]bool)
	for i := 0; i < colorVal.Slice().Len(); i++ {
		vals[colorVal.Slice().At(i).Str()] = true
	}
	assert.True(t, vals["red"])
	assert.True(t, vals["blue"])
}

func TestPromoteDoesNotOverwriteSpanAttr(t *testing.T) {
	c, sink := newTestTracesConnector(t, func(tc *tracesConnector) {
		tc.promoteToSpan = []string{"color"}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("test")
	span.Attributes().PutStr("color", "red") // existing attr

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{"color": "blue"})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	fSpan := sink.AllTraces()[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	colorVal, ok := fSpan.Attributes().Get("color")
	assert.True(t, ok)
	assert.Equal(t, "red", colorVal.Str(), "existing span attr should not be overwritten")
}

func TestPromoteSingleValueIsScalar(t *testing.T) {
	c, sink := newTestTracesConnector(t, func(tc *tracesConnector) {
		tc.promoteToSpan = []string{"color"}
	})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("test")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{"color": "blue"})
	// Add duplicate event with same color value — should still be scalar.
	addMetricEvent(span, "traced.count", "counter", 2.0, ts, map[string]string{"color": "blue"})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	fSpan := sink.AllTraces()[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	colorVal, ok := fSpan.Attributes().Get("color")
	assert.True(t, ok)
	assert.Equal(t, pcommon.ValueTypeStr, colorVal.Type(), "single unique value should be scalar, not slice")
	assert.Equal(t, "blue", colorVal.Str())
}

func TestDefaultConfigNoPromotion(t *testing.T) {
	c, sink := newTestTracesConnector(t)

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("test")

	ts := pcommon.Timestamp(1711900000000000000)
	addMetricEvent(span, "traced.count", "counter", 1.0, ts, map[string]string{"color": "blue"})

	err := c.ConsumeTraces(context.Background(), td)
	require.NoError(t, err)

	fSpan := sink.AllTraces()[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	_, ok := fSpan.Attributes().Get("color")
	assert.False(t, ok, "no promotion should happen with default config")
}
