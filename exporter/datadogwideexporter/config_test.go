// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configopaque"
)

func TestConfigValidate(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.API.Key = configopaque.String("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, cfg.Validate())
	require.Equal(t, "https://wide-intake.datadoghq.com/api/v2/wide/events", cfg.wideEndpoint())

	cfg.API.Site = "datadoghq.eu"
	require.NoError(t, cfg.Validate())
	require.Equal(t, "https://wide-intake.datadoghq.eu/api/v2/wide/events", cfg.wideEndpoint())

	cfg.Wide.Endpoint = "https://wide.example.test/custom"
	require.NoError(t, cfg.Validate())
	require.Equal(t, "https://wide.example.test/custom", cfg.wideEndpoint())
}

func TestConfigValidateErrors(t *testing.T) {
	t.Run("missing api key", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		require.ErrorIs(t, cfg.Validate(), errUnsetAPIKey)
	})

	t.Run("invalid endpoint", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.API.Key = configopaque.String("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		cfg.Wide.Endpoint = "ftp://example.test"
		require.ErrorContains(t, cfg.Validate(), "scheme must be http or https")
	})

	t.Run("invalid durations", func(t *testing.T) {
		cfg := createDefaultConfig().(*Config)
		cfg.API.Key = configopaque.String("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		cfg.Wide.FlushInterval = 0
		require.ErrorContains(t, cfg.Validate(), "wide.flush_interval")

		cfg = createDefaultConfig().(*Config)
		cfg.API.Key = configopaque.String("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		cfg.Correlation.OrphanTimeout = -time.Second
		require.ErrorContains(t, cfg.Validate(), "orphan_timeout")
	})
}
