// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

const unknownService = "unknown_service"

type wideExporter struct {
	cfg    *Config
	set    exporter.Settings
	sender envelopeSender

	materializer *Materializer
	correlator   *correlator

	mu       sync.Mutex
	identity EnvelopeIdentity

	// flushMu serializes flushes (ticker vs shutdown) so serialization and the
	// blocking HTTP send run without holding mu, keeping the ingest path and the
	// correlator sweep unblocked during network I/O. retry is only touched under
	// flushMu.
	flushMu sync.Mutex
	retry   *retryBuffer

	startOnce    sync.Once
	shutdownOnce sync.Once
	stop         chan struct{}
	done         chan struct{}
	started      bool
	lastErr      error

	refs int
}

func newWideExporter(set exporter.Settings, cfg *Config) (*wideExporter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	identity := EnvelopeIdentity{
		Host:    cfg.Hostname,
		Service: cfg.Service,
	}
	if identity.Host == "" {
		identity.Host, _ = os.Hostname()
	}
	exp := &wideExporter{
		cfg: cfg,
		set: set,
		materializer: NewMaterializer(
			WithLimits(MaterializerLimits{
				MaxSampledRows:      cfg.Wide.MaxSampledRows,
				MaxAggregateBuckets: cfg.Wide.MaxAggregateBuckets,
				MaxSchemas:          cfg.Wide.MaxSchemas,
			}),
			WithLogger(set.Logger),
		),
		retry:    newRetryBuffer(cfg.Wide.MaxRetryBufferBytes),
		identity: identity,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	exp.sender = newHTTPEnvelopeSender(cfg.wideEndpoint(), string(cfg.API.Key), cfg.TimeoutSettings.Timeout, set.Logger)
	exp.correlator = newCorrelator(cfg.Correlation, exp.exportObservationBatch)
	return exp, nil
}

func (e *wideExporter) start(context.Context, component.Host) error {
	e.startOnce.Do(func() {
		e.correlator.start()
		e.mu.Lock()
		e.started = true
		e.mu.Unlock()
		go e.loop()
		e.set.Logger.Info("Datadog wide exporter started", zap.String("endpoint", e.cfg.wideEndpoint()))
	})
	return nil
}

func (e *wideExporter) shutdown(ctx context.Context) error {
	var err error
	e.shutdownOnce.Do(func() {
		e.mu.Lock()
		started := e.started
		e.mu.Unlock()
		if started {
			select {
			case <-e.done:
			default:
				close(e.stop)
				<-e.done
			}
		}
		if drainErr := e.correlator.shutdown(ctx); drainErr != nil {
			err = errors.Join(err, drainErr)
		}
		flushCtx := ctx
		if _, ok := flushCtx.Deadline(); !ok {
			timeout := min(e.cfg.TimeoutSettings.Timeout, 2*time.Second)
			if timeout <= 0 {
				timeout = 2 * time.Second
			}
			var cancel context.CancelFunc
			flushCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		if flushErr := e.forceFlush(flushCtx); flushErr != nil {
			e.set.Logger.Warn("Datadog wide shutdown flush failed", zap.Error(flushErr))
		}
		if closeErr := e.sender.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	})
	return err
}

func (e *wideExporter) consumeTraces(ctx context.Context, td ptrace.Traces) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spans, metrics := e.tracesToObservations(td)
	for _, metric := range metrics {
		if err := e.correlator.onMetric(ctx, metric); err != nil {
			return err
		}
	}
	for i := range spans {
		e.correlator.onSpan(spans[i])
	}
	return e.correlator.forceFlush(ctx)
}

func (e *wideExporter) consumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, metric := range e.metricsToObservations(md) {
		if err := e.correlator.onMetric(ctx, metric); err != nil {
			return err
		}
	}
	return e.correlator.forceFlush(ctx)
}

func (e *wideExporter) consumeLogs(ctx context.Context, ld plog.Logs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, record := range e.logsToObservations(ld) {
		e.correlator.onLog(record)
	}
	return e.correlator.forceFlush(ctx)
}

func (e *wideExporter) exportObservationBatch(ctx context.Context, batch observationBatch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	events := wideEventsFromBatch(batch)
	if len(events) == 0 {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range events {
		event := events[i]
		e.observeIdentity(event)
		if err := e.materializer.Add(event); err != nil {
			e.lastErr = err
			return err
		}
	}
	return nil
}

func (e *wideExporter) forceFlush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// flushMu serializes concurrent flushes (ticker vs shutdown). Serialization
	// and the blocking send happen without holding e.mu so the ingest path and
	// the correlator sweep are never blocked on network I/O.
	e.flushMu.Lock()
	defer e.flushMu.Unlock()

	// Short critical section: surface any background error, snapshot the window,
	// and reset the materializer regardless of send outcome. Retention of failed
	// sends lives in the bounded retry buffer, not the (unbounded) materializer.
	e.mu.Lock()
	var pending error
	if e.lastErr != nil {
		pending = e.lastErr
		e.lastErr = nil
	}
	tables, windowEnd, snapErr := e.materializer.flushSnapshot(ctx)
	if snapErr == nil {
		e.materializer.reset(windowEnd)
	}
	identity := e.identity
	e.mu.Unlock()

	if snapErr != nil {
		return errors.Join(pending, snapErr)
	}

	var current []SerializedEnvelope
	if len(tables) > 0 {
		if identity.Service == "" {
			identity.Service = unknownService
		}
		serializer := NewSerializer(identity, WithMaxEnvelopeBytes(e.cfg.Wide.MaxEnvelopeBytes))
		envelopes, err := serializer.Serialize(ctx, tables)
		if err != nil {
			// Serialization failures are permanent (e.g. a single oversized
			// table); drop the window and surface the error rather than retrying
			// forever.
			return errors.Join(pending, err)
		}
		current = envelopes
	}
	return errors.Join(pending, e.sendWithRetry(ctx, current))
}

// sendWithRetry drains the retry buffer (oldest first), appends the current
// window, and sends each batch. On the first send failure it re-enqueues the
// failed batch and everything after it (oldest first) into the bounded retry
// buffer and returns the error. Only called under flushMu, so retry needs no
// additional locking.
func (e *wideExporter) sendWithRetry(ctx context.Context, current []SerializedEnvelope) error {
	batches := e.retry.take()
	if len(current) > 0 {
		batches = append(batches, current)
	}
	for i := range batches {
		if err := e.sender.Send(ctx, batches[i]); err != nil {
			for _, remaining := range batches[i:] {
				e.retry.enqueue(remaining, e.set.Logger)
			}
			return err
		}
	}
	return nil
}

func (e *wideExporter) loop() {
	ticker := time.NewTicker(e.cfg.Wide.FlushInterval)
	defer ticker.Stop()
	defer close(e.done)
	for {
		select {
		case <-ticker.C:
			if err := e.forceFlush(context.Background()); err != nil {
				e.set.Logger.Warn("Datadog wide flush failed", zap.Error(err))
			}
		case <-e.stop:
			return
		}
	}
}

func (e *wideExporter) observeIdentity(event WideEvent) {
	if e.identity.Service == "" {
		if value, ok := event.Dimensions[dimensionName("service.name")]; ok {
			e.identity.Service = value.String()
		}
	}
	if e.identity.Host == "" {
		for _, key := range []string{"host.name", "datadog.host.name", "k8s.node.name"} {
			if value, ok := event.Dimensions[dimensionName(key)]; ok {
				e.identity.Host = value.String()
				return
			}
		}
	}
}
