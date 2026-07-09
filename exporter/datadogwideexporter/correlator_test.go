// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCorrelatorLinksMetricSampleToLateSpan(t *testing.T) {
	ctx := t.Context()
	var batches []observationBatch
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   time.Nanosecond,
		OrphanTimeout: time.Hour,
		SweepInterval: time.Hour,
	}, func(_ context.Context, batch observationBatch) error {
		batches = append(batches, batch)
		return nil
	}, nil)

	ref := spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"}
	err := c.onMetric(ctx, linkedMetricObservation{
		Metric: metricDescriptor{Name: "calendar.requests", Type: metricTypeCounter},
		Aggregate: &metricAggregateFact{
			Value:     12,
			Timestamp: time.Unix(10, 0),
		},
		Samples: []linkedMetricSample{{
			Span: ref,
			Sample: metricSampleFact{
				Value:     1,
				Timestamp: time.Unix(9, 0),
			},
		}},
	})
	require.NoError(t, err)
	c.onSpan(spanObservation{
		Ref:               ref,
		Name:              "calendar.get_date",
		Start:             time.Unix(8, 0),
		End:               time.Unix(10, 0),
		Sampled:           true,
		SampleProbability: 1,
	})
	require.NoError(t, c.drainAll(ctx))

	require.Len(t, batches, 1)
	require.Len(t, batches[0].Spans, 1)
	require.Len(t, batches[0].Spans[0].Metrics, 1)
	require.Len(t, batches[0].Spans[0].Metrics[0].Samples, 1)
	require.Equal(t, 1.0, batches[0].Spans[0].Metrics[0].Samples[0].Value)
	require.Len(t, batches[0].Metrics, 1)
	require.NotNil(t, batches[0].Metrics[0].Aggregate)
	require.Equal(t, 12.0, batches[0].Metrics[0].Aggregate.Value)
}

func TestCorrelatorEmitsOrphanLogsForMissingSpan(t *testing.T) {
	ctx := t.Context()
	var batches []observationBatch
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   time.Nanosecond,
		OrphanTimeout: 0,
		SweepInterval: time.Hour,
	}, func(_ context.Context, batch observationBatch) error {
		batches = append(batches, batch)
		return nil
	}, nil)

	// A log referencing a span that never arrives must still be exported as a
	// standalone row rather than being dropped when the bundle times out.
	ref := spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"}
	c.onLog(logObservation{
		Ref:       ref,
		Timestamp: time.Unix(9, 0),
		Severity:  "INFO",
		Body:      "hello",
	})
	require.NoError(t, c.flushDue(ctx))

	require.Len(t, batches, 1)
	require.Empty(t, batches[0].Spans)
	require.Len(t, batches[0].Logs, 1)
	require.Equal(t, "hello", batches[0].Logs[0].Body)
	require.Equal(t, ref, batches[0].Logs[0].Ref)
}

// A duplicate span for the same ref must not clobber metrics/logs already
// correlated onto the previously-seen span object.
func TestCorrelatorDuplicateSpanPreservesMetrics(t *testing.T) {
	ctx := t.Context()
	var batches []observationBatch
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   0,
		OrphanTimeout: time.Hour,
		SweepInterval: time.Hour,
	}, func(_ context.Context, batch observationBatch) error {
		batches = append(batches, batch)
		return nil
	}, nil)

	ref := spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"}
	c.onSpan(spanObservation{Ref: ref, Name: "op", Sampled: true, SampleProbability: 1})
	require.NoError(t, c.onMetric(ctx, linkedMetricObservation{
		Metric: metricDescriptor{Name: "m", Type: metricTypeCounter},
		Samples: []linkedMetricSample{{
			Span:   ref,
			Sample: metricSampleFact{Value: 1, Timestamp: time.Unix(9, 0)},
		}},
	}))
	// Duplicate span for the same ref.
	c.onSpan(spanObservation{Ref: ref, Name: "op", Sampled: true, SampleProbability: 1})
	require.NoError(t, c.drainAll(ctx))

	require.Len(t, batches, 1)
	require.Len(t, batches[0].Spans, 1)
	require.Len(t, batches[0].Spans[0].Metrics, 1)
}

// A transient sink error must re-queue captured data instead of dropping it, so
// a later flush still exports it.
func TestCorrelatorRequeuesOnSinkError(t *testing.T) {
	ctx := t.Context()
	failNext := true
	var batches []observationBatch
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   0,
		OrphanTimeout: 0,
		SweepInterval: time.Hour,
	}, func(_ context.Context, batch observationBatch) error {
		if failNext {
			failNext = false
			return errors.New("sink boom")
		}
		batches = append(batches, batch)
		return nil
	}, nil)

	ref := spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"}
	c.onLog(logObservation{Ref: ref, Timestamp: time.Unix(9, 0), Severity: "INFO", Body: "hello"})

	require.Error(t, c.flushDue(ctx))   // orphan bundle sink fails → re-queued
	require.NoError(t, c.flushDue(ctx)) // retried successfully

	require.Len(t, batches, 1)
	require.Len(t, batches[0].Logs, 1)
	require.Equal(t, "hello", batches[0].Logs[0].Body)
}

func TestCorrelatorKeepsOrphanAggregateAndDropsSample(t *testing.T) {
	ctx := t.Context()
	var batches []observationBatch
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   time.Nanosecond,
		OrphanTimeout: 0,
		SweepInterval: time.Hour,
	}, func(_ context.Context, batch observationBatch) error {
		batches = append(batches, batch)
		return nil
	}, nil)

	err := c.onMetric(ctx, linkedMetricObservation{
		Metric: metricDescriptor{Name: "calendar.requests", Type: metricTypeCounter},
		Aggregate: &metricAggregateFact{
			Value:     7,
			Timestamp: time.Unix(10, 0),
		},
		Samples: []linkedMetricSample{{
			Span: spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"},
			Sample: metricSampleFact{
				Value:     1,
				Timestamp: time.Unix(9, 0),
			},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, c.flushDue(ctx))

	require.Len(t, batches, 1)
	require.Empty(t, batches[0].Spans)
	require.Len(t, batches[0].Metrics, 1)
	require.NotNil(t, batches[0].Metrics[0].Aggregate)
	require.Equal(t, 7.0, batches[0].Metrics[0].Aggregate.Value)
}

// The orphan path (bundle timed out with no span) must be observable: a
// throttled warning reports how many bundles and exemplar samples were
// flushed without ever correlating to a span.
func TestCorrelatorLogsOrphanFlush(t *testing.T) {
	ctx := t.Context()
	core, logs := observer.New(zap.WarnLevel)
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   time.Nanosecond,
		OrphanTimeout: 0,
		SweepInterval: time.Hour,
	}, func(_ context.Context, _ observationBatch) error {
		return nil
	}, zap.New(core))

	err := c.onMetric(ctx, linkedMetricObservation{
		Metric: metricDescriptor{Name: "calendar.requests", Type: metricTypeCounter},
		Aggregate: &metricAggregateFact{
			Value:     7,
			Timestamp: time.Unix(10, 0),
		},
		Samples: []linkedMetricSample{{
			Span: spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"},
			Sample: metricSampleFact{
				Value:     1,
				Timestamp: time.Unix(9, 0),
			},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, c.flushDue(ctx))

	entries := logs.FilterMessageSnippet("without span correlation").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.EqualValues(t, 1, fields["orphan_bundles"])
	require.EqualValues(t, 1, fields["orphan_samples_dropped"])
}

// A sustained trickle of orphaned bundles must not spam the log: only the
// first report within correlatorStatsLogInterval is emitted.
func TestCorrelatorThrottlesOrphanLog(t *testing.T) {
	ctx := t.Context()
	core, logs := observer.New(zap.WarnLevel)
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   0,
		OrphanTimeout: 0,
		SweepInterval: time.Hour,
	}, func(_ context.Context, _ observationBatch) error {
		return nil
	}, zap.New(core))

	ref := spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"}
	c.onLog(logObservation{Ref: ref, Timestamp: time.Unix(9, 0), Severity: "INFO", Body: "one"})
	require.NoError(t, c.flushDue(ctx))
	require.Equal(t, 1, logs.FilterMessageSnippet("without span correlation").Len())

	c.onLog(logObservation{Ref: ref, Timestamp: time.Unix(9, 0), Severity: "INFO", Body: "two"})
	require.NoError(t, c.flushDue(ctx))
	require.Equal(t, 1, logs.FilterMessageSnippet("without span correlation").Len())
}
