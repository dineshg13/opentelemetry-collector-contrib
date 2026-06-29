// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type envelopeSender interface {
	Send(context.Context, []SerializedEnvelope) error
	Close() error
}

type httpEnvelopeSender struct {
	endpoint string
	apiKey   string
	client   *http.Client
	logger   *zap.Logger
}

func newHTTPEnvelopeSender(endpoint, apiKey string, timeout time.Duration, logger *zap.Logger) *httpEnvelopeSender {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return &httpEnvelopeSender{
		endpoint: endpoint,
		apiKey:   apiKey,
		client: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

func (s *httpEnvelopeSender) Send(ctx context.Context, envelopes []SerializedEnvelope) error {
	for _, envelope := range envelopes {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(envelope.Payload))
		if err != nil {
			return err
		}
		req.Header.Set("DD-API-KEY", s.apiKey)
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("X-Datadog-Wide-Host", envelope.Host)
		req.Header.Set("X-Datadog-Wide-Service", envelope.Service)

		resp, err := s.client.Do(req)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("wide intake returned %s: %s", resp.Status, string(body))
		}
		s.logger.Debug("Sent Datadog wide envelope",
			zap.String("host", envelope.Host),
			zap.String("service", envelope.Service),
			zap.Int("tables", envelope.TableCount),
			zap.Int("bytes", envelope.EncodedBytes))
	}
	return nil
}

func (s *httpEnvelopeSender) Close() error {
	s.client.CloseIdleConnections()
	return nil
}
