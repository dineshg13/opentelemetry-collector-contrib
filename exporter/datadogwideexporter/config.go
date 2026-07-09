// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const (
	defaultSite             = "datadoghq.com"
	defaultFlushInterval    = 10 * time.Second
	defaultGraceWindow      = 10 * time.Second
	defaultOrphanTimeout    = 5 * time.Second
	defaultSweepInterval    = 50 * time.Millisecond
	defaultHTTPTimeout      = 15 * time.Second
	defaultWideEndpointTmpl = "https://wide-intake.%s/api/v2/wide/events"

	defaultMaxSampledRows      = 100_000
	defaultMaxAggregateBuckets = 100_000
	defaultMaxSchemas          = 10_000
	defaultMaxRetryBufferBytes = 32 * 1024 * 1024
)

var (
	errUnsetAPIKey = errors.New("api.key is not set")
	errEmptySite   = errors.New("api.site is not set")
)

type APIConfig struct {
	Key  configopaque.String `mapstructure:"key"`
	Site string              `mapstructure:"site"`
	_    struct{}
}

type WideConfig struct {
	Endpoint         string        `mapstructure:"endpoint"`
	FlushInterval    time.Duration `mapstructure:"flush_interval"`
	MaxEnvelopeBytes int           `mapstructure:"max_envelope_bytes"`
	// MaxSampledRows bounds the number of sampled rows retained per flush window.
	// Excess rows are dropped (drop-new). 0 means unbounded.
	MaxSampledRows int `mapstructure:"max_sampled_rows"`
	// MaxAggregateBuckets bounds the number of distinct aggregate buckets
	// (identity+path+dimensions cardinality) per flush window. Excess new buckets
	// are dropped (drop-new); existing buckets keep aggregating. 0 means unbounded.
	MaxAggregateBuckets int `mapstructure:"max_aggregate_buckets"`
	// MaxSchemas bounds the number of distinct table identities whose schema is
	// tracked per flush window. Excess new identities are dropped. 0 means unbounded.
	MaxSchemas int `mapstructure:"max_schemas"`
	// MaxRetryBufferBytes bounds the in-memory buffer of serialized envelopes held
	// for retry after a failed wide-intake send. When exceeded, the oldest batches
	// are dropped. 0 disables retry buffering (failed sends are dropped immediately).
	//
	// Note: this governs wide-intake egress. The sending_queue/retry_on_failure
	// settings only govern ingestion into the aggregation window, not the intake POST.
	MaxRetryBufferBytes int `mapstructure:"max_retry_buffer_bytes"`
	// LogWideEventsJSON, when true, logs each flushed batch of wide events as JSON at
	// debug level before it is sent to the intake. Intended for debugging payload
	// contents; verbose, so it is off by default and also requires the collector's
	// log level to be debug.
	LogWideEventsJSON bool `mapstructure:"log_wide_events_json"`
	// WideEventsJSONFile, when set, appends each flushed batch of wide events to this
	// file as newline-delimited JSON (one JSON array per flush), independent of the
	// collector log level. Use this to capture payload contents to a clean file
	// without turning on verbose debug logging.
	WideEventsJSONFile string `mapstructure:"wide_events_json_file"`
	// WideEventsBinaryFile, when set, appends the raw serialized WideTelemetryEnvelope
	// protobuf bytes (exactly what is POSTed to the intake) to this file. Each envelope
	// is framed by a 4-byte big-endian length prefix so multiple envelopes can be read
	// back. Use this to replay or decode payloads with protobuf tooling.
	WideEventsBinaryFile string `mapstructure:"wide_events_binary_file"`
	_                    struct{}
}

type CorrelationConfig struct {
	GraceWindow   time.Duration `mapstructure:"grace_window"`
	OrphanTimeout time.Duration `mapstructure:"orphan_timeout"`
	SweepInterval time.Duration `mapstructure:"sweep_interval"`
	_             struct{}
}

type Config struct {
	QueueSettings   configoptional.Optional[exporterhelper.QueueBatchConfig] `mapstructure:"sending_queue"`
	BackOffConfig   configretry.BackOffConfig                                `mapstructure:"retry_on_failure"`
	TimeoutSettings exporterhelper.TimeoutConfig                             `mapstructure:",squash"`

	API         APIConfig         `mapstructure:"api"`
	Wide        WideConfig        `mapstructure:"wide"`
	Correlation CorrelationConfig `mapstructure:"correlation"`

	Hostname string `mapstructure:"hostname"`
	Service  string `mapstructure:"service"`
}

var _ component.Config = (*Config)(nil)

func (c *Config) Validate() error {
	c.API.Key = configopaque.String(strings.TrimSpace(string(c.API.Key)))
	c.API.Site = strings.TrimSpace(c.API.Site)
	c.Wide.Endpoint = strings.TrimSpace(c.Wide.Endpoint)

	if c.API.Key == "" {
		return errUnsetAPIKey
	}
	if c.API.Site == "" {
		return errEmptySite
	}
	if c.Wide.Endpoint != "" {
		if err := validateHTTPEndpoint(c.Wide.Endpoint); err != nil {
			return fmt.Errorf("wide.endpoint: %w", err)
		}
	}
	if c.Wide.FlushInterval <= 0 {
		return errors.New("wide.flush_interval must be positive")
	}
	if c.Wide.MaxEnvelopeBytes < 0 {
		return errors.New("wide.max_envelope_bytes must be non-negative")
	}
	if c.Wide.MaxSampledRows < 0 {
		return errors.New("wide.max_sampled_rows must be non-negative")
	}
	if c.Wide.MaxAggregateBuckets < 0 {
		return errors.New("wide.max_aggregate_buckets must be non-negative")
	}
	if c.Wide.MaxSchemas < 0 {
		return errors.New("wide.max_schemas must be non-negative")
	}
	if c.Wide.MaxRetryBufferBytes < 0 {
		return errors.New("wide.max_retry_buffer_bytes must be non-negative")
	}
	if c.Correlation.GraceWindow < 0 {
		return errors.New("correlation.grace_window must be non-negative")
	}
	if c.Correlation.OrphanTimeout < 0 {
		return errors.New("correlation.orphan_timeout must be non-negative")
	}
	if c.Correlation.SweepInterval <= 0 {
		return errors.New("correlation.sweep_interval must be positive")
	}
	return nil
}

func (c *Config) wideEndpoint() string {
	if c.Wide.Endpoint != "" {
		return c.Wide.Endpoint
	}
	return fmt.Sprintf(defaultWideEndpointTmpl, c.API.Site)
}

func validateHTTPEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("host is required")
	}
	return nil
}
