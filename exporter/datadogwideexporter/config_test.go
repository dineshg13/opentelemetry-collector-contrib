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

	// Path correctness is a config concern, not a validation error: a custom
	// endpoint whose path omits the intake's expected suffix still validates
	// (the intake, not Validate, rejects a wrong path at request time).
	cfg.Wide.Endpoint = "https://event-platform-intake.example.test/api/v2/wide"
	require.NoError(t, cfg.Validate())
	require.Equal(t, "https://event-platform-intake.example.test/api/v2/wide", cfg.wideEndpoint())
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

	t.Run("negative accumulation caps", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			mutate func(*Config)
			substr string
		}{
			{"max_sampled_rows", func(c *Config) { c.Wide.MaxSampledRows = -1 }, "wide.max_sampled_rows"},
			{"max_aggregate_buckets", func(c *Config) { c.Wide.MaxAggregateBuckets = -1 }, "wide.max_aggregate_buckets"},
			{"max_schemas", func(c *Config) { c.Wide.MaxSchemas = -1 }, "wide.max_schemas"},
			{"max_retry_buffer_bytes", func(c *Config) { c.Wide.MaxRetryBufferBytes = -1 }, "wide.max_retry_buffer_bytes"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				cfg := createDefaultConfig().(*Config)
				cfg.API.Key = configopaque.String("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				tc.mutate(cfg)
				require.ErrorContains(t, cfg.Validate(), tc.substr)
			})
		}
	})
}

func TestConfigDefaultsHaveCaps(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	require.Equal(t, defaultMaxSampledRows, cfg.Wide.MaxSampledRows)
	require.Equal(t, defaultMaxAggregateBuckets, cfg.Wide.MaxAggregateBuckets)
	require.Equal(t, defaultMaxSchemas, cfg.Wide.MaxSchemas)
	require.Equal(t, defaultMaxRetryBufferBytes, cfg.Wide.MaxRetryBufferBytes)
}
