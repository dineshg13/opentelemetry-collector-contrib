// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telltailmetricsconnector // import "github.com/open-telemetry/opentelemetry-collector-contrib/connector/telltailmetricsconnector"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

type tracesConnector struct {
	component.StartFunc
	component.ShutdownFunc
	logger         *zap.Logger
	tracesConsumer consumer.Traces
	promoteToSpan  []string
}

func (*tracesConnector) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

func (c *tracesConnector) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	if len(c.promoteToSpan) > 0 {
		c.promoteAttrsToSpans(td)
	}
	c.stripMetricEvents(td)
	return c.tracesConsumer.ConsumeTraces(ctx, td)
}

// promoteAttrsToSpans collects specified attributes from metric events and
// promotes them onto the parent span. If multiple events carry different values
// for the same key, the values are collected into a slice attribute.
func (c *tracesConnector) promoteAttrsToSpans(td ptrace.Traces) {
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				c.promoteForSpan(span)
			}
		}
	}
}

func (c *tracesConnector) promoteForSpan(span ptrace.Span) {
	// Collect unique values per promoted key from metric events.
	collected := make(map[string][]pcommon.Value, len(c.promoteToSpan))

	events := span.Events()
	for l := 0; l < events.Len(); l++ {
		event := events.At(l)
		if event.Name() != eventNameMetric {
			continue
		}
		for _, key := range c.promoteToSpan {
			v, ok := event.Attributes().Get(key)
			if !ok {
				continue
			}
			// Deduplicate: only add if not already seen with same string repr.
			vals := collected[key]
			dup := false
			for _, existing := range vals {
				if existing.Equal(v) {
					dup = true
					break
				}
			}
			if !dup {
				cp := pcommon.NewValueEmpty()
				v.CopyTo(cp)
				collected[key] = append(collected[key], cp)
			}
		}
	}

	// Apply to span (don't overwrite existing span attrs).
	for key, vals := range collected {
		if _, exists := span.Attributes().Get(key); exists {
			continue // don't overwrite existing span attribute
		}
		if len(vals) == 1 {
			vals[0].CopyTo(span.Attributes().PutEmpty(key))
		} else {
			// Multiple unique values → slice attribute.
			sliceVal := span.Attributes().PutEmptySlice(key)
			sliceVal.EnsureCapacity(len(vals))
			for _, v := range vals {
				v.CopyTo(sliceVal.AppendEmpty())
			}
		}
	}
}

// stripMetricEvents removes all span events named "metric" from the trace data in-place.
func (c *tracesConnector) stripMetricEvents(td ptrace.Traces) {
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				span.Events().RemoveIf(func(event ptrace.SpanEvent) bool {
					return event.Name() == eventNameMetric
				})
			}
		}
	}
}
