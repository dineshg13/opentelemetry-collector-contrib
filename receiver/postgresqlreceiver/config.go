// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"errors"
	"fmt"
	"net"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/scraper/scraperhelper"
	"go.uber.org/multierr"

	"github.com/open-telemetry/opentelemetry-collector-contrib/config/configdbauth"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/dbauth"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// Errors for missing required config parameters.
const (
	ErrNoUsername          = "invalid config: missing username"
	ErrNoPassword          = "invalid config: missing password" // #nosec G101 - not hardcoded credentials
	ErrNotSupported        = "invalid config: field '%s' not supported"
	ErrTransportsSupported = "invalid config: 'transport' must be 'tcp' or 'unix'"
	ErrHostPort            = "invalid config: 'endpoint' must be in the form <host>:<port> no matter what 'transport' is configured"
	// #nosec G101 - not hardcoded credentials
	ErrPasswordAndDBAuth = "invalid config: set either 'password' or 'db_auth', not both"
)

type TopQueryCollection struct {
	MaxRowsPerQuery        int64         `mapstructure:"max_rows_per_query"`
	TopNQuery              int64         `mapstructure:"top_n_query"`
	MaxExplainEachInterval int64         `mapstructure:"max_explain_each_interval"`
	QueryPlanCacheSize     int           `mapstructure:"query_plan_cache_size"`
	QueryPlanCacheTTL      time.Duration `mapstructure:"query_plan_cache_ttl"`
	CollectionInterval     time.Duration `mapstructure:"collection_interval"`
	// prevent unkeyed literal initialization
	_ struct{}
}

type QuerySampleCollection struct {
	MaxRowsPerQuery int64 `mapstructure:"max_rows_per_query"`
	// prevent unkeyed literal initialization
	_ struct{}
}

type QueryMonitoringCollection struct {
	Enabled         bool                 `mapstructure:"enabled"`
	MaxRows         int64                `mapstructure:"max_rows"`
	MaxPayloadBytes int                  `mapstructure:"max_payload_bytes"`
	QueryPlans      QueryMonitoringPlans `mapstructure:"query_plans"`
}

type QueryMonitoringPlans struct {
	Enabled          bool          `mapstructure:"enabled"`
	MaxPerCollection int           `mapstructure:"max_per_collection"`
	Timeout          time.Duration `mapstructure:"timeout"`
	MaxPlanBytes     int           `mapstructure:"max_plan_bytes"`
	CacheSize        int           `mapstructure:"cache_size"`
	CacheTTL         time.Duration `mapstructure:"cache_ttl"`
}

type Config struct {
	ControllerConfig      scraperhelper.ControllerConfig `mapstructure:",squash"`
	Username              string                         `mapstructure:"username"`
	Password              configopaque.String            `mapstructure:"password"`
	Databases             []string                       `mapstructure:"databases"`
	ExcludeDatabases      []string                       `mapstructure:"exclude_databases"`
	AddrConfig            confignet.AddrConfig           `mapstructure:",squash"`       // provides Endpoint and Transport
	ClientConfig          configtls.ClientConfig         `mapstructure:"tls,omitempty"` // provides SSL details
	ConnectionPool        ConnectionPool                 `mapstructure:"connection_pool,omitempty"`
	MetricsBuilderConfig  metadata.MetricsBuilderConfig  `mapstructure:",squash"`
	LogsBuilderConfig     metadata.LogsBuilderConfig     `mapstructure:",squash"`
	QuerySampleCollection QuerySampleCollection          `mapstructure:"query_sample_collection,omitempty"`
	TopQueryCollection    TopQueryCollection             `mapstructure:"top_query_collection,omitempty"`
	QueryMonitoring       QueryMonitoringCollection      `mapstructure:"query_monitoring,omitempty"`
	// DBAuth optionally sources the connection credential from a db_auth provider
	// extension (e.g. AWS IAM) instead of a static password. When set, the provider
	// supplies the password at connection-open time. Mutually exclusive with the
	// top-level password field.
	DBAuth configdbauth.ID `mapstructure:"db_auth,omitempty"`
}

type ConnectionPool struct {
	MaxIdleTime *time.Duration `mapstructure:"max_idle_time,omitempty"`
	MaxLifetime *time.Duration `mapstructure:"max_lifetime,omitempty"`
	MaxIdle     *int           `mapstructure:"max_idle,omitempty"`
	MaxOpen     *int           `mapstructure:"max_open,omitempty"`
}

func (cfg *Config) Validate() error {
	var err error
	if cfg.QueryMonitoring.Enabled {
		if cfg.ControllerConfig.CollectionInterval <= 0 {
			err = multierr.Append(err, errors.New("query_monitoring requires a positive collection_interval"))
		}
		if cfg.QueryMonitoring.MaxRows <= 0 || cfg.QueryMonitoring.MaxRows > 100000 {
			err = multierr.Append(err, errors.New("query_monitoring.max_rows must be between 1 and 100000"))
		}
		if cfg.QueryMonitoring.MaxPayloadBytes < 1024 || cfg.QueryMonitoring.MaxPayloadBytes > 4*1024*1024 {
			err = multierr.Append(err, errors.New("query_monitoring.max_payload_bytes must be between 1024 and 4194304"))
		}
		plans := cfg.QueryMonitoring.QueryPlans
		if plans.Enabled {
			if plans.MaxPerCollection < 1 || plans.MaxPerCollection > 20 {
				err = multierr.Append(err, errors.New("query_monitoring.query_plans.max_per_collection must be between 1 and 20"))
			}
			if plans.Timeout <= 0 || plans.Timeout > 5*time.Second {
				err = multierr.Append(err, errors.New("query_monitoring.query_plans.timeout must be positive and no greater than 5s"))
			}
			if plans.MaxPlanBytes < 1024 || plans.MaxPlanBytes > cfg.QueryMonitoring.MaxPayloadBytes {
				err = multierr.Append(err, errors.New("query_monitoring.query_plans.max_plan_bytes must be between 1024 and max_payload_bytes"))
			}
			if plans.CacheSize < 1 || plans.CacheSize > 10000 || plans.CacheTTL <= 0 {
				err = multierr.Append(err, errors.New("query_monitoring.query_plans requires cache_size between 1 and 10000 and positive cache_ttl"))
			}
		}
	}
	if cfg.Username == "" {
		err = multierr.Append(err, errors.New(ErrNoUsername))
	}

	// Credential source precedence (R12): a static password and a db_auth block
	// are mutually exclusive. A username alongside a db_auth block is expected —
	// the provider may use it as a mint input. When a db_auth block is configured,
	// the password is supplied by the provider, so the top-level password is not
	// required.
	dbAuthConfigured := !cfg.DBAuth.IsEmpty()
	switch {
	case dbAuthConfigured && cfg.Password != "":
		err = multierr.Append(err, errors.New(ErrPasswordAndDBAuth))
	case !dbAuthConfigured && cfg.Password == "":
		err = multierr.Append(err, errors.New(ErrNoPassword))
	}

	// The lib/pq module does not support overriding ServerName or specifying supported TLS versions
	if cfg.ClientConfig.ServerName != "" {
		err = multierr.Append(err, fmt.Errorf(ErrNotSupported, "ServerName"))
	}
	if cfg.ClientConfig.MaxVersion != "" {
		err = multierr.Append(err, fmt.Errorf(ErrNotSupported, "MaxVersion"))
	}
	if cfg.ClientConfig.MinVersion != "" {
		err = multierr.Append(err, fmt.Errorf(ErrNotSupported, "MinVersion"))
	}

	switch cfg.AddrConfig.Transport {
	case confignet.TransportTypeTCP, confignet.TransportTypeUnix:
		_, _, endpointErr := net.SplitHostPort(cfg.AddrConfig.Endpoint)
		if endpointErr != nil {
			err = multierr.Append(err, errors.New(ErrHostPort))
		}
	default:
		err = multierr.Append(err, errors.New(ErrTransportsSupported))
	}

	return err
}

// resolveCredentialProvider resolves the db_auth credential provider named in the
// db_auth block from the host extension map, or returns (nil, nil) when no db_auth
// block is configured (the receiver then uses its static password). The receiver
// imports no provider packages — the provider is referenced by component ID and
// resolved from the declared extensions. Provider-wide inputs (such as the AWS IAM
// provider's region) live on the extension's own config; the per-connection inputs
// (endpoint and username) travel with each GetCredential call, keeping the receiver
// agnostic to any provider's config.
func (cfg *Config) resolveCredentialProvider(extensions map[component.ID]component.Component) (dbauth.Provider, error) {
	if cfg.DBAuth.IsEmpty() {
		return nil, nil
	}
	return cfg.DBAuth.GetProvider(extensions)
}
