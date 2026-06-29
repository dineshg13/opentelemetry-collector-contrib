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
		cfg:          cfg,
		set:          set,
		materializer: NewMaterializer(),
		identity:     identity,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
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
	for _, span := range spans {
		e.correlator.onSpan(span)
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
	for _, event := range events {
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
	e.mu.Lock()
	defer e.mu.Unlock()
	var pending error
	if e.lastErr != nil {
		pending = e.lastErr
		e.lastErr = nil
	}
	// Always attempt the flush and surface any background error alongside its
	// result. flushLocked retains the materializer state when a send fails, so
	// the next tick retries instead of permanently wedging the loop.
	return errors.Join(pending, e.flushLocked(ctx))
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

func (e *wideExporter) flushLocked(ctx context.Context) error {
	tables, windowEnd, err := e.materializer.flushSnapshot(ctx)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		e.materializer.reset(windowEnd)
		return nil
	}
	identity := e.identity
	if identity.Service == "" {
		identity.Service = unknownService
	}
	serializer := NewSerializer(identity, WithMaxEnvelopeBytes(e.cfg.Wide.MaxEnvelopeBytes))
	envelopes, err := serializer.Serialize(ctx, tables)
	if err != nil {
		return err
	}
	if err := e.sender.Send(ctx, envelopes); err != nil {
		return err
	}
	e.materializer.reset(windowEnd)
	return nil
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
