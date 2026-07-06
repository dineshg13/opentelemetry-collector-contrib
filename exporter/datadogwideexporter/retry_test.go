// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func envBatch(bytes int) []SerializedEnvelope {
	return []SerializedEnvelope{{EncodedBytes: bytes}}
}

func TestRetryBufferRoundTrip(t *testing.T) {
	r := newRetryBuffer(1000)
	r.enqueue(envBatch(100), zap.NewNop())
	r.enqueue(envBatch(200), zap.NewNop())
	require.Equal(t, 300, r.bytes)

	batches := r.take()
	require.Len(t, batches, 2)
	require.Equal(t, 100, batches[0][0].EncodedBytes) // oldest first
	require.Equal(t, 200, batches[1][0].EncodedBytes)
	require.Zero(t, r.bytes)
	require.Empty(t, r.take())
}

func TestRetryBufferDropsOldestOverCap(t *testing.T) {
	r := newRetryBuffer(100)
	r.enqueue(envBatch(60), zap.NewNop())
	r.enqueue(envBatch(60), zap.NewNop()) // 120 > 100 → drop oldest 60

	require.LessOrEqual(t, r.bytes, 100)
	require.Equal(t, uint64(1), r.dropped)
	batches := r.take()
	require.Len(t, batches, 1)
	require.Equal(t, 60, batches[0][0].EncodedBytes)
}

func TestRetryBufferKeepsNewestEvenIfOversized(t *testing.T) {
	r := newRetryBuffer(10)
	r.enqueue(envBatch(50), zap.NewNop()) // single batch larger than cap is retained
	require.Len(t, r.batches, 1)
	require.Zero(t, r.dropped)
}

func TestRetryBufferDisabledDropsImmediately(t *testing.T) {
	r := newRetryBuffer(0)
	r.enqueue(envBatch(10), zap.NewNop())
	require.Empty(t, r.take())
	require.Equal(t, uint64(1), r.dropped)
}
