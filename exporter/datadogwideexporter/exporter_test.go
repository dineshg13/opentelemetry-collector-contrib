// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/exporter/exportertest"
)

type recordingSender struct {
	envelopes []SerializedEnvelope
}

func (s *recordingSender) Send(_ context.Context, envelopes []SerializedEnvelope) error {
	s.envelopes = append(s.envelopes, envelopes...)
	return nil
}

func (*recordingSender) Close() error { return nil }

// flakySender fails its first failures Send calls, then records the rest.
type flakySender struct {
	failures  int
	calls     int
	envelopes []SerializedEnvelope
}

func (s *flakySender) Send(_ context.Context, envelopes []SerializedEnvelope) error {
	s.calls++
	if s.calls <= s.failures {
		return errors.New("transient send failure")
	}
	s.envelopes = append(s.envelopes, envelopes...)
	return nil
}

func (*flakySender) Close() error { return nil }

func newTestExporter(t *testing.T) *wideExporter {
	t.Helper()
	cfg := createDefaultConfig().(*Config)
	cfg.API.Key = configopaque.String("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	cfg.Service = "calendar"
	cfg.Hostname = "host-a"
	cfg.Wide.FlushInterval = time.Hour
	cfg.Correlation.GraceWindow = 0
	cfg.Correlation.SweepInterval = time.Hour
	cfg.Correlation.OrphanTimeout = time.Hour

	exp, err := newWideExporter(exportertest.NewNopSettings(exportertest.NopType), cfg)
	require.NoError(t, err)
	return exp
}

// materializeSampleData drives one aggregate+sample pair through the correlator
// so the materializer holds a flushable window.
func materializeSampleData(t *testing.T, exp *wideExporter) {
	t.Helper()
	ref := spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"}
	require.NoError(t, exp.correlator.onMetric(context.Background(), linkedMetricObservation{
		Metric:    metricDescriptor{Name: "calendar.requests", Type: metricTypeCounter},
		Aggregate: &metricAggregateFact{Value: 3, Timestamp: time.Unix(10, 0)},
		Samples: []linkedMetricSample{{
			Span:   ref,
			Sample: metricSampleFact{Value: 1, Timestamp: time.Unix(9, 0)},
		}},
	}))
	exp.correlator.onSpan(spanObservation{
		Ref:               ref,
		Name:              "calendar.get_date",
		Start:             time.Unix(8, 0),
		End:               time.Unix(10, 0),
		Sampled:           true,
		SampleProbability: 1,
	})
	require.NoError(t, exp.correlator.drainAll(context.Background()))
}

// A transient send failure must not discard the buffered window: the next
// forceFlush retries the same data instead of permanently wedging the loop.
func TestExporterRetriesAfterTransientSendError(t *testing.T) {
	exp := newTestExporter(t)
	sender := &flakySender{failures: 1}
	exp.sender = sender

	materializeSampleData(t, exp)

	require.Error(t, exp.forceFlush(context.Background()))
	require.Empty(t, sender.envelopes)

	require.NoError(t, exp.forceFlush(context.Background()))
	require.NotEmpty(t, sender.envelopes)
}

// A stored background error is surfaced exactly once, but the flush still runs
// and clears it, so the loop keeps flushing on subsequent ticks.
func TestExporterForceFlushSurfacesPriorErrorButStillFlushes(t *testing.T) {
	exp := newTestExporter(t)
	sender := &recordingSender{}
	exp.sender = sender

	materializeSampleData(t, exp)

	exp.mu.Lock()
	exp.lastErr = errors.New("prior background error")
	exp.mu.Unlock()

	err := exp.forceFlush(context.Background())
	require.ErrorContains(t, err, "prior background error")
	require.NotEmpty(t, sender.envelopes)

	require.NoError(t, exp.forceFlush(context.Background()))
}

func TestExporterMaterializesAndSendsWideEnvelope(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.API.Key = configopaque.String("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	cfg.Service = "calendar"
	cfg.Hostname = "host-a"
	cfg.Wide.FlushInterval = time.Hour
	cfg.Correlation.GraceWindow = 0
	cfg.Correlation.SweepInterval = time.Hour
	cfg.Correlation.OrphanTimeout = time.Hour

	exp, err := newWideExporter(exportertest.NewNopSettings(exportertest.NopType), cfg)
	require.NoError(t, err)
	sender := &recordingSender{}
	exp.sender = sender

	ref := spanRef{traceID: "01020300000000000000000000000000", spanID: "0405060000000000"}
	require.NoError(t, exp.correlator.onMetric(context.Background(), linkedMetricObservation{
		Metric: metricDescriptor{
			Name:       "calendar.requests",
			Type:       metricTypeCounter,
			Dimensions: map[string]TypedValue{dimensionName("route"): StringValue("/date")},
		},
		Aggregate: &metricAggregateFact{Value: 3, Timestamp: time.Unix(10, 0)},
		Samples: []linkedMetricSample{{
			Span:   ref,
			Sample: metricSampleFact{Value: 1, Timestamp: time.Unix(9, 0)},
		}},
	}))
	exp.correlator.onSpan(spanObservation{
		Ref:               ref,
		Name:              "calendar.get_date",
		Start:             time.Unix(8, 0),
		End:               time.Unix(10, 0),
		Sampled:           true,
		SampleProbability: 1,
	})
	require.NoError(t, exp.correlator.drainAll(context.Background()))
	require.NoError(t, exp.forceFlush(context.Background()))

	require.NotEmpty(t, sender.envelopes)
	require.Equal(t, "host-a", sender.envelopes[0].Host)
	require.Equal(t, "calendar", sender.envelopes[0].Service)
	require.Positive(t, sender.envelopes[0].TableCount)
	require.NotEmpty(t, sender.envelopes[0].Payload)
}
