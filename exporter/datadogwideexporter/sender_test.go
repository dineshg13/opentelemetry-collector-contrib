// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestHTTPEnvelopeSenderLogsWideEventCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	core, logs := observer.New(zap.InfoLevel)
	sender := newHTTPEnvelopeSender(server.URL, "test-key", time.Second, zap.New(core))
	defer sender.Close()

	err := sender.Send(t.Context(), []SerializedEnvelope{{
		Payload:      []byte("payload"),
		Host:         "host-a",
		Service:      "calendar",
		TableCount:   2,
		RowCount:     5,
		EncodedBytes: 7,
	}})
	require.NoError(t, err)

	entries := logs.FilterMessage("Sent Datadog wide events to intake")
	require.Equal(t, 1, entries.Len())
	require.Equal(t, int64(5), entries.All()[0].ContextMap()["wide_events"])
	require.Equal(t, int64(2), entries.All()[0].ContextMap()["tables"])
}
