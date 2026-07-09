// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"context"
	"encoding/binary"
	"encoding/json"
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
	"go.uber.org/zap/zapcore"
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
	exp.correlator = newCorrelator(cfg.Correlation, exp.exportObservationBatch, set.Logger)
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
			// This sink only accumulates in memory; the network POST happens
			// out-of-band in forceFlush. Any error here is a per-event data
			// problem, not a transient send failure, so dropping the event and
			// continuing avoids abandoning the rest of the batch and prevents the
			// collector from retrying an unresolvable batch forever. Deterministic
			// schema conflicts are already dropped-and-counted inside Add; this
			// guards against any other unexpected per-event error.
			e.set.Logger.Debug("Datadog wide exporter dropped event", zap.Error(err))
			continue
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
		e.dumpWireEnvelopes(current)
		e.writeWideEventsBinary(current)
	}
	return errors.Join(pending, e.sendWithRetry(ctx, current))
}

// dumpWireEnvelopes renders the serialized envelopes (the exact protobuf + Arrow bytes
// being POSTed) as wire-schema-shaped JSON for debugging, when either the
// wide.log_wide_events_json (debug log) or wide.wide_events_json_file (file) toggle is
// set. Decoding the on-the-wire bytes — rather than the internal structs — means the
// dump reflects what the intake actually receives. forceFlush is serialized by flushMu,
// so file writes never interleave; failures are logged and swallowed.
func (e *wideExporter) dumpWireEnvelopes(envelopes []SerializedEnvelope) {
	logEnabled := e.cfg.Wide.LogWideEventsJSON && e.set.Logger.Core().Enabled(zapcore.DebugLevel)
	fileEnabled := e.cfg.Wide.WideEventsJSONFile != ""
	if !logEnabled && !fileEnabled {
		return
	}

	var file *os.File
	if fileEnabled {
		f, err := os.OpenFile(e.cfg.Wide.WideEventsJSONFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			e.set.Logger.Warn("Datadog wide exporter failed to open wide events file",
				zap.String("path", e.cfg.Wide.WideEventsJSONFile), zap.Error(err))
			fileEnabled = false
		} else {
			file = f
			defer file.Close()
		}
	}

	for i := range envelopes {
		dump, err := decodeWireEnvelope(envelopes[i])
		if err != nil {
			e.set.Logger.Warn("Datadog wide exporter failed to decode envelope for debug dump", zap.Error(err))
			continue
		}
		payload, err := json.Marshal(dump)
		if err != nil {
			e.set.Logger.Warn("Datadog wide exporter failed to marshal envelope for debug dump", zap.Error(err))
			continue
		}
		if logEnabled {
			e.set.Logger.Debug("Datadog wide events payload",
				zap.Int("wide_events", envelopes[i].RowCount),
				zap.ByteString("envelope", payload))
		}
		if fileEnabled {
			if _, err := file.Write(append(payload, '\n')); err != nil {
				e.set.Logger.Warn("Datadog wide exporter failed to write wide events file",
					zap.String("path", e.cfg.Wide.WideEventsJSONFile), zap.Error(err))
			}
		}
	}
}

// writeWideEventsBinary appends the raw serialized envelope bytes (the exact protobuf
// POSTed to the intake) to the configured file when wide.wide_events_binary_file is
// set. Each envelope is written as a 4-byte big-endian length prefix followed by the
// payload, so a reader can split a multi-envelope flush back into messages. forceFlush
// is serialized by flushMu, so writes never interleave; failures are logged and swallowed.
func (e *wideExporter) writeWideEventsBinary(envelopes []SerializedEnvelope) {
	if e.cfg.Wide.WideEventsBinaryFile == "" || len(envelopes) == 0 {
		return
	}
	f, err := os.OpenFile(e.cfg.Wide.WideEventsBinaryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		e.set.Logger.Warn("Datadog wide exporter failed to open wide events binary file",
			zap.String("path", e.cfg.Wide.WideEventsBinaryFile), zap.Error(err))
		return
	}
	defer f.Close()
	var lenPrefix [4]byte
	for i := range envelopes {
		payload := envelopes[i].Payload
		binary.BigEndian.PutUint32(lenPrefix[:], uint32(len(payload)))
		if _, err := f.Write(lenPrefix[:]); err != nil {
			e.set.Logger.Warn("Datadog wide exporter failed to write wide events binary file",
				zap.String("path", e.cfg.Wide.WideEventsBinaryFile), zap.Error(err))
			return
		}
		if _, err := f.Write(payload); err != nil {
			e.set.Logger.Warn("Datadog wide exporter failed to write wide events binary file",
				zap.String("path", e.cfg.Wide.WideEventsBinaryFile), zap.Error(err))
			return
		}
	}
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
				e.set.Logger.Warn("Datadog wide flush failed", zap.String("endpoint", e.cfg.wideEndpoint()), zap.Error(err))
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
