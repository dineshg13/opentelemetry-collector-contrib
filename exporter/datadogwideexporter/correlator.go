// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"context"
	"sync"
	"time"
)

type pendingSpan struct {
	span          *spanObservation
	samples       []metricObservation
	aggregates    []linkedMetricObservation
	logs          []logObservation
	spanArrivedAt time.Time
	firstSignalAt time.Time
}

type correlator struct {
	sink          func(context.Context, observationBatch) error
	grace         time.Duration
	orphanTimeout time.Duration
	sweep         time.Duration

	mu         sync.Mutex
	pending    map[spanRef]*pendingSpan
	orphanLogs []logObservation

	stop    chan struct{}
	done    chan struct{}
	running bool
}

func newCorrelator(cfg CorrelationConfig, sink func(context.Context, observationBatch) error) *correlator {
	return &correlator{
		sink:          sink,
		grace:         cfg.GraceWindow,
		orphanTimeout: cfg.OrphanTimeout,
		sweep:         cfg.SweepInterval,
		pending:       make(map[spanRef]*pendingSpan),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
}

func (c *correlator) start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()
	go c.loop()
}

func (c *correlator) onSpan(span spanObservation) {
	if !span.Ref.valid() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.pendingSpan(span.Ref)
	span.Metrics = append(span.Metrics, b.samples...)
	span.Logs = append(span.Logs, b.logs...)
	b.samples = nil
	b.logs = nil
	b.span = &span
	b.spanArrivedAt = time.Now()
}

func (c *correlator) onLog(record logObservation) {
	if !record.Ref.valid() {
		c.mu.Lock()
		c.orphanLogs = append(c.orphanLogs, record)
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.pendingSpan(record.Ref)
	if b.span != nil {
		b.span.Logs = append(b.span.Logs, record)
		return
	}
	b.logs = append(b.logs, record)
}

func (c *correlator) onMetric(ctx context.Context, obs linkedMetricObservation) error {
	if len(obs.Samples) == 0 {
		return c.sink(ctx, observationBatch{Metrics: []linkedMetricObservation{obs}})
	}

	var firstCarrier spanRef
	retained := obs
	retained.Samples = nil

	c.mu.Lock()
	for _, sample := range obs.Samples {
		if !sample.Span.valid() {
			continue
		}
		retained.Samples = append(retained.Samples, sample)
		if !firstCarrier.valid() {
			firstCarrier = sample.Span
		}
		pending := c.pendingSpan(sample.Span)
		metric := metricObservation{
			Metric:  obs.Metric,
			Samples: []metricSampleFact{sample.Sample},
		}
		if pending.span != nil {
			pending.span.Metrics = append(pending.span.Metrics, metric)
			continue
		}
		pending.samples = append(pending.samples, metric)
	}
	if !firstCarrier.valid() {
		c.mu.Unlock()
		return c.sink(ctx, observationBatch{Metrics: []linkedMetricObservation{retained}})
	}
	if obs.Aggregate != nil {
		pending := c.pendingSpan(firstCarrier)
		pending.aggregates = append(pending.aggregates, retained)
	}
	c.mu.Unlock()
	return nil
}

func (c *correlator) forceFlush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.flushDue(ctx); err != nil {
		return err
	}
	return nil
}

func (c *correlator) shutdown(ctx context.Context) error {
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		select {
		case <-c.done:
		default:
			close(c.stop)
			<-c.done
		}
	}
	return c.drainAll(ctx)
}

func (c *correlator) pendingSpan(key spanRef) *pendingSpan {
	b := c.pending[key]
	if b == nil {
		b = &pendingSpan{firstSignalAt: time.Now()}
		c.pending[key] = b
	}
	return b
}

func (c *correlator) loop() {
	ticker := time.NewTicker(c.sweep)
	defer ticker.Stop()
	defer close(c.done)
	for {
		select {
		case <-ticker.C:
			_ = c.flushDue(context.Background())
		case <-c.stop:
			return
		}
	}
}

func (c *correlator) flushDue(ctx context.Context) error {
	now := time.Now()
	var ready []*pendingSpan
	var orphans []*pendingSpan

	c.mu.Lock()
	for key, b := range c.pending {
		if b.span != nil && !b.spanArrivedAt.IsZero() {
			if now.Sub(b.spanArrivedAt) >= c.grace {
				ready = append(ready, b)
				delete(c.pending, key)
			}
		} else if now.Sub(b.firstSignalAt) >= c.orphanTimeout {
			orphans = append(orphans, b)
			delete(c.pending, key)
		}
	}
	orphanLogs := c.orphanLogs
	c.orphanLogs = nil
	c.mu.Unlock()

	if len(ready) > 0 {
		if err := c.emit(ctx, ready); err != nil {
			return err
		}
	}
	if len(orphans) > 0 {
		if err := c.exportOrphanBundles(ctx, orphans); err != nil {
			return err
		}
	}
	if len(orphanLogs) > 0 {
		return c.sink(ctx, observationBatch{Logs: orphanLogs})
	}
	return nil
}

func (c *correlator) drainAll(ctx context.Context) error {
	var ready []*pendingSpan
	var orphans []*pendingSpan

	c.mu.Lock()
	for key, b := range c.pending {
		if b.span != nil {
			ready = append(ready, b)
		} else {
			orphans = append(orphans, b)
		}
		delete(c.pending, key)
	}
	orphanLogs := c.orphanLogs
	c.orphanLogs = nil
	c.mu.Unlock()

	if len(ready) > 0 {
		if err := c.emit(ctx, ready); err != nil {
			return err
		}
	}
	if len(orphans) > 0 {
		if err := c.exportOrphanBundles(ctx, orphans); err != nil {
			return err
		}
	}
	if len(orphanLogs) > 0 {
		return c.sink(ctx, observationBatch{Logs: orphanLogs})
	}
	return nil
}

func (c *correlator) emit(ctx context.Context, bundles []*pendingSpan) error {
	batch := observationBatch{Spans: make([]spanObservation, 0, len(bundles))}
	for _, b := range bundles {
		span := *b.span
		span.Metrics = append(span.Metrics, b.samples...)
		span.Logs = append(span.Logs, b.logs...)
		batch.Spans = append(batch.Spans, span)
		batch.Metrics = append(batch.Metrics, b.aggregates...)
	}
	return c.sink(ctx, batch)
}

func (c *correlator) exportOrphanBundles(ctx context.Context, bundles []*pendingSpan) error {
	batch := observationBatch{}
	for _, b := range bundles {
		batch.Metrics = append(batch.Metrics, b.aggregates...)
		// Logs buffered against a span that never arrived can still stand on
		// their own: they retain their spanRef, so logWideEvent recovers the
		// trace/span IDs and emits them as standalone rows rather than dropping
		// them. (b.samples are inherently span-linked and have no carrier here,
		// so they are intentionally not emitted.)
		batch.Logs = append(batch.Logs, b.logs...)
	}
	return c.sink(ctx, batch)
}
