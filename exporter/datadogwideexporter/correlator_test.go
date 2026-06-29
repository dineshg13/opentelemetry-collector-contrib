// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCorrelatorLinksMetricSampleToLateSpan(t *testing.T) {
	ctx := context.Background()
	var batches []observationBatch
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   time.Nanosecond,
		OrphanTimeout: time.Hour,
		SweepInterval: time.Hour,
	}, func(_ context.Context, batch observationBatch) error {
		batches = append(batches, batch)
		return nil
	})

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
	ctx := context.Background()
	var batches []observationBatch
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   time.Nanosecond,
		OrphanTimeout: 0,
		SweepInterval: time.Hour,
	}, func(_ context.Context, batch observationBatch) error {
		batches = append(batches, batch)
		return nil
	})

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

func TestCorrelatorKeepsOrphanAggregateAndDropsSample(t *testing.T) {
	ctx := context.Background()
	var batches []observationBatch
	c := newCorrelator(CorrelationConfig{
		GraceWindow:   time.Nanosecond,
		OrphanTimeout: 0,
		SweepInterval: time.Hour,
	}, func(_ context.Context, batch observationBatch) error {
		batches = append(batches, batch)
		return nil
	})

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
