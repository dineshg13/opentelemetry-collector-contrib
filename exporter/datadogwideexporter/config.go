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
	defaultGraceWindow      = 300 * time.Millisecond
	defaultOrphanTimeout    = 5 * time.Second
	defaultSweepInterval    = 50 * time.Millisecond
	defaultHTTPTimeout      = 15 * time.Second
	defaultWideEndpointTmpl = "https://wide-intake.%s/api/v2/wide/events"
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
	_                struct{}
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
