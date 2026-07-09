// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// correlatorStatsLogInterval throttles the "flushed without correlation"
// warning so a sustained trickle of orphaned bundles logs at most once per
// interval instead of once per sweep tick.
const correlatorStatsLogInterval = 10 * time.Second

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
	logger        *zap.Logger

	mu         sync.Mutex
	pending    map[spanRef]*pendingSpan
	orphanLogs []logObservation

	// Stats accumulated since the last logged report; guarded by mu.
	correlatedBundles    uint64
	orphanBundles        uint64
	orphanSamplesDropped uint64
	orphanLogsFlushed    uint64
	lastStatsLogAt       time.Time

	stop    chan struct{}
	done    chan struct{}
	running bool
}

func newCorrelator(cfg CorrelationConfig, sink func(context.Context, observationBatch) error, logger *zap.Logger) *correlator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &correlator{
		sink:          sink,
		grace:         cfg.GraceWindow,
		orphanTimeout: cfg.OrphanTimeout,
		sweep:         cfg.SweepInterval,
		logger:        logger,
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
	if b.span != nil {
		// Duplicate span for the same ref: preserve anything already attached to
		// the previously-seen span object (metrics/logs correlated between the two
		// onSpan calls) instead of clobbering it.
		span.Metrics = append(span.Metrics, b.span.Metrics...)
		span.Logs = append(span.Logs, b.span.Logs...)
	}
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

// keyedBundle pairs a pending bundle with its map key so a failed sink call can
// re-insert it rather than dropping the data.
type keyedBundle struct {
	key spanRef
	b   *pendingSpan
}

func (c *correlator) flushDue(ctx context.Context) error {
	now := time.Now()
	var ready []keyedBundle
	var orphans []keyedBundle

	c.mu.Lock()
	for key, b := range c.pending {
		if b.span != nil && !b.spanArrivedAt.IsZero() {
			if now.Sub(b.spanArrivedAt) >= c.grace {
				ready = append(ready, keyedBundle{key, b})
				delete(c.pending, key)
			}
		} else if now.Sub(b.firstSignalAt) >= c.orphanTimeout {
			orphans = append(orphans, keyedBundle{key, b})
			delete(c.pending, key)
		}
	}
	orphanLogs := c.orphanLogs
	c.orphanLogs = nil
	c.mu.Unlock()

	return c.emitBundles(ctx, ready, orphans, orphanLogs)
}

func (c *correlator) drainAll(ctx context.Context) error {
	var ready []keyedBundle
	var orphans []keyedBundle

	c.mu.Lock()
	for key, b := range c.pending {
		if b.span != nil {
			ready = append(ready, keyedBundle{key, b})
		} else {
			orphans = append(orphans, keyedBundle{key, b})
		}
		delete(c.pending, key)
	}
	orphanLogs := c.orphanLogs
	c.orphanLogs = nil
	c.mu.Unlock()

	return c.emitBundles(ctx, ready, orphans, orphanLogs)
}

// emitBundles sinks ready bundles, orphan bundles, and orphan logs in order. On
// the first sink error it re-queues everything not yet successfully sent so a
// transient failure (e.g. context cancellation) doesn't drop captured data.
func (c *correlator) emitBundles(ctx context.Context, ready, orphans []keyedBundle, orphanLogs []logObservation) error {
	if len(ready) > 0 {
		if err := c.emit(ctx, bundlesOf(ready)); err != nil {
			c.requeue(ready, orphans, orphanLogs)
			return err
		}
		c.recordCorrelated(len(ready))
	}
	if len(orphans) > 0 {
		bundles := bundlesOf(orphans)
		if err := c.exportOrphanBundles(ctx, bundles); err != nil {
			c.requeue(nil, orphans, orphanLogs)
			return err
		}
		c.recordOrphans(bundles)
	}
	if len(orphanLogs) > 0 {
		if err := c.sink(ctx, observationBatch{Logs: orphanLogs}); err != nil {
			c.requeue(nil, nil, orphanLogs)
			return err
		}
		c.recordOrphanLogs(len(orphanLogs))
	}
	return nil
}

// recordCorrelated tracks bundles flushed with their span successfully
// correlated, for context alongside the orphan stats below.
func (c *correlator) recordCorrelated(n int) {
	c.mu.Lock()
	c.correlatedBundles += uint64(n)
	c.mu.Unlock()
	c.maybeLogStats()
}

// recordOrphans tracks bundles flushed without ever seeing their span, plus
// the exemplar samples that go with them (exportOrphanBundles never sinks
// b.samples, so once these bundles are sunk the samples are gone for good).
func (c *correlator) recordOrphans(bundles []*pendingSpan) {
	dropped := 0
	for _, b := range bundles {
		dropped += len(b.samples)
	}
	c.mu.Lock()
	c.orphanBundles += uint64(len(bundles))
	c.orphanSamplesDropped += uint64(dropped)
	c.mu.Unlock()
	c.maybeLogStats()
}

// recordOrphanLogs tracks logs exported standalone because they never
// correlated to any span (invalid ref, or the ref's span never arrived).
func (c *correlator) recordOrphanLogs(n int) {
	c.mu.Lock()
	c.orphanLogsFlushed += uint64(n)
	c.mu.Unlock()
	c.maybeLogStats()
}

// maybeLogStats emits a throttled warning summarizing flushes that happened
// without span correlation since the last report. It is a no-op when there is
// nothing to report, and at most one report is emitted per
// correlatorStatsLogInterval.
func (c *correlator) maybeLogStats() {
	c.mu.Lock()
	if c.orphanBundles == 0 && c.orphanSamplesDropped == 0 && c.orphanLogsFlushed == 0 {
		c.mu.Unlock()
		return
	}
	now := time.Now()
	if !c.lastStatsLogAt.IsZero() && now.Sub(c.lastStatsLogAt) < correlatorStatsLogInterval {
		c.mu.Unlock()
		return
	}
	orphanBundles := c.orphanBundles
	orphanSamplesDropped := c.orphanSamplesDropped
	orphanLogsFlushed := c.orphanLogsFlushed
	correlatedBundles := c.correlatedBundles
	c.orphanBundles = 0
	c.orphanSamplesDropped = 0
	c.orphanLogsFlushed = 0
	c.correlatedBundles = 0
	c.lastStatsLogAt = now
	c.mu.Unlock()

	c.logger.Warn("Datadog wide exporter flushed telemetry without span correlation",
		zap.Uint64("orphan_bundles", orphanBundles),
		zap.Uint64("orphan_samples_dropped", orphanSamplesDropped),
		zap.Uint64("orphan_logs", orphanLogsFlushed),
		zap.Uint64("correlated_bundles", correlatedBundles),
	)
}

func bundlesOf(kbs []keyedBundle) []*pendingSpan {
	out := make([]*pendingSpan, 0, len(kbs))
	for _, kb := range kbs {
		out = append(out, kb.b)
	}
	return out
}

// requeue re-inserts un-sent bundles and prepends un-sent orphan logs. Bundles
// are only restored when no newer bundle already occupies the key (a newer one
// arrived while we were flushing); in that rare race the flushed copy is
// dropped in favor of the fresher entry.
func (c *correlator) requeue(ready, orphans []keyedBundle, orphanLogs []logObservation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, kb := range ready {
		if _, exists := c.pending[kb.key]; !exists {
			c.pending[kb.key] = kb.b
		}
	}
	for _, kb := range orphans {
		if _, exists := c.pending[kb.key]; !exists {
			c.pending[kb.key] = kb.b
		}
	}
	if len(orphanLogs) > 0 {
		c.orphanLogs = append(orphanLogs, c.orphanLogs...)
	}
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
