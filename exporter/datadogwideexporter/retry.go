// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import "go.uber.org/zap"

// retryBuffer is a byte-bounded FIFO of serialized envelope batches held for
// retry after a failed wide-intake send. When the buffer exceeds maxBytes the
// oldest batches are dropped (drop-oldest keeps the freshest telemetry).
//
// It is not safe for concurrent use; callers must serialize access (the exporter
// only touches it under flushMu).
type retryBuffer struct {
	batches  [][]SerializedEnvelope
	bytes    int
	maxBytes int
	dropped  uint64
}

func newRetryBuffer(maxBytes int) *retryBuffer {
	return &retryBuffer{maxBytes: maxBytes}
}

func batchBytes(batch []SerializedEnvelope) int {
	total := 0
	for i := range batch {
		total += batch[i].EncodedBytes
	}
	return total
}

// enqueue appends a batch, then drops the oldest batches while the buffer
// exceeds maxBytes (always keeping at least the most recently enqueued batch).
// If maxBytes is 0, buffering is disabled and the batch is dropped immediately.
func (r *retryBuffer) enqueue(batch []SerializedEnvelope, logger *zap.Logger) {
	if len(batch) == 0 {
		return
	}
	if r.maxBytes <= 0 {
		r.dropped += uint64(len(batch))
		logger.Warn("Datadog wide exporter dropped envelopes (retry buffer disabled)",
			zap.Int("dropped_envelopes", len(batch)))
		return
	}

	r.batches = append(r.batches, batch)
	r.bytes += batchBytes(batch)

	droppedEnvelopes := 0
	for r.bytes > r.maxBytes && len(r.batches) > 1 {
		oldest := r.batches[0]
		r.batches = r.batches[1:]
		r.bytes -= batchBytes(oldest)
		droppedEnvelopes += len(oldest)
	}
	if droppedEnvelopes > 0 {
		r.dropped += uint64(droppedEnvelopes)
		logger.Warn("Datadog wide exporter dropped oldest envelopes to bound retry buffer",
			zap.Int("dropped_envelopes", droppedEnvelopes),
			zap.Int("buffer_bytes", r.bytes),
			zap.Int("max_bytes", r.maxBytes))
	}
}

// take returns all buffered batches (oldest first) and clears the buffer.
func (r *retryBuffer) take() [][]SerializedEnvelope {
	if len(r.batches) == 0 {
		return nil
	}
	out := r.batches
	r.batches = nil
	r.bytes = 0
	return out
}
