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

// blockingSender blocks in Send until release is closed, signaling entry on started.
type blockingSender struct {
	started chan struct{}
	release chan struct{}
}

func (s *blockingSender) Send(_ context.Context, _ []SerializedEnvelope) error {
	close(s.started)
	<-s.release
	return nil
}

func (*blockingSender) Close() error { return nil }

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
	require.NoError(t, exp.correlator.onMetric(t.Context(), linkedMetricObservation{
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
	require.NoError(t, exp.correlator.drainAll(t.Context()))
}

// A transient send failure must not discard the buffered window: the next
// forceFlush retries the same data instead of permanently wedging the loop.
func TestExporterRetriesAfterTransientSendError(t *testing.T) {
	exp := newTestExporter(t)
	sender := &flakySender{failures: 1}
	exp.sender = sender

	materializeSampleData(t, exp)

	require.Error(t, exp.forceFlush(t.Context()))
	require.Empty(t, sender.envelopes)

	require.NoError(t, exp.forceFlush(t.Context()))
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

	err := exp.forceFlush(t.Context())
	require.ErrorContains(t, err, "prior background error")
	require.NotEmpty(t, sender.envelopes)

	require.NoError(t, exp.forceFlush(t.Context()))
}

// A persistent intake outage must not grow memory without bound: the retry
// buffer drops oldest batches once it exceeds its byte cap.
func TestExporterRetryBufferBounded(t *testing.T) {
	exp := newTestExporter(t)
	exp.cfg.Wide.MaxRetryBufferBytes = 1 // forces drop-oldest down to the newest batch
	exp.retry = newRetryBuffer(exp.cfg.Wide.MaxRetryBufferBytes)
	exp.sender = &flakySender{failures: 1000} // always fails

	for range 5 {
		materializeSampleData(t, exp)
		require.Error(t, exp.forceFlush(t.Context()))
	}

	require.LessOrEqual(t, len(exp.retry.batches), 1)
	require.Positive(t, exp.retry.dropped)
}

// The blocking HTTP send must not hold the ingest lock, so the consume path and
// the correlator sweep keep making progress during a slow/hung send.
func TestExporterFlushDoesNotHoldIngestLock(t *testing.T) {
	exp := newTestExporter(t)
	bs := &blockingSender{started: make(chan struct{}), release: make(chan struct{})}
	exp.sender = bs
	materializeSampleData(t, exp)

	flushDone := make(chan error, 1)
	go func() { flushDone <- exp.forceFlush(t.Context()) }()

	<-bs.started // send is now blocking

	ingestDone := make(chan struct{})
	go func() {
		_ = exp.exportObservationBatch(t.Context(), observationBatch{
			Metrics: []linkedMetricObservation{{
				Metric:    metricDescriptor{Name: "svc.other", Type: metricTypeCounter},
				Aggregate: &metricAggregateFact{Value: 1, Timestamp: time.Unix(1, 0)},
			}},
		})
		close(ingestDone)
	}()

	select {
	case <-ingestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("exportObservationBatch blocked on e.mu while a send was in progress")
	}

	close(bs.release)
	require.NoError(t, <-flushDone)
}

// A permanent serialization failure (e.g. a single oversized table) is surfaced
// and dropped, not enqueued for retry, and does not wedge the flush loop.
func TestExporterSerializeErrorDropsAndSurfaces(t *testing.T) {
	exp := newTestExporter(t)
	exp.cfg.Wide.MaxEnvelopeBytes = 1 // any table exceeds this
	sender := &recordingSender{}
	exp.sender = sender

	materializeSampleData(t, exp)
	require.Error(t, exp.forceFlush(t.Context()))
	require.Empty(t, sender.envelopes)
	require.Empty(t, exp.retry.batches) // permanent error is not retried

	// Loop not wedged: the window was reset, so the next flush is clean.
	require.NoError(t, exp.forceFlush(t.Context()))
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
	require.NoError(t, exp.correlator.onMetric(t.Context(), linkedMetricObservation{
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
	require.NoError(t, exp.correlator.drainAll(t.Context()))
	require.NoError(t, exp.forceFlush(t.Context()))

	require.NotEmpty(t, sender.envelopes)
	require.Equal(t, "host-a", sender.envelopes[0].Host)
	require.Equal(t, "calendar", sender.envelopes[0].Service)
	require.Positive(t, sender.envelopes[0].TableCount)
	require.NotEmpty(t, sender.envelopes[0].Payload)
}
