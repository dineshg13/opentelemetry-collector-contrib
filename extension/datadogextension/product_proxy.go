// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !aix

package datadogextension

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/extension"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/httpforwarderextension"
)

// ProductConfig controls only the extension-owned HTTP forwarding path.
// SDK features and Collector telemetry pipelines must be configured independently.
type ProductConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// ProductsConfig deliberately has no implicit enablement.
type ProductsConfig struct {
	// These products require telemetry pipelines, not an HTTP product proxy.
	ApplicationSecurity   ProductConfig `mapstructure:"application_security"`
	DatabaseMonitoring    ProductConfig `mapstructure:"database_monitoring"`
	ContinuousProfiling   ProductConfig `mapstructure:"continuous_profiling"`
	DataStreamsMonitoring ProductConfig `mapstructure:"data_streams_monitoring"`
	DataJobsMonitoring    ProductConfig `mapstructure:"data_jobs_monitoring"`
	LLMObservability      ProductConfig `mapstructure:"llm_observability"`
	CIVisibility          ProductConfig `mapstructure:"ci_visibility"`
	LiveDebugging         ProductConfig `mapstructure:"live_debugging"`
}

// ProductProxyConfig configures the optional HTTP product listener. Compression
// is always preserved; product payloads are opaque to this extension.
type ProductProxyConfig struct {
	ServerConfig   confighttp.ServerConfig `mapstructure:",squash"`
	Tags           []string                `mapstructure:"tags"`
	DefaultEnv     string                  `mapstructure:"default_env"`
	ApplicationKey configopaque.String     `mapstructure:"application_key"`
}

// Unmarshal initializes the optional listener before recursive validation.
// HTTP payloads must remain compressed exactly as the SDK sent them.
func (p *ProductProxyConfig) Unmarshal(conf *confmap.Conf) error {
	p.ServerConfig = confighttp.NewDefaultServerConfig()
	p.ServerConfig.CompressionAlgorithms = []string{}
	p.ServerConfig.MaxRequestBodySize = 32 * 1024 * 1024
	return conf.Unmarshal(p)
}

func (c *Config) validateProducts() error {
	p := c.Products
	if p.ApplicationSecurity.Enabled || p.DatabaseMonitoring.Enabled {
		return errors.New("application_security and database_monitoring are pipeline-owned; configure their receivers, processors and exporters explicitly instead of enabling a product proxy")
	}
	enabled := p.ContinuousProfiling.Enabled || p.DataStreamsMonitoring.Enabled || p.DataJobsMonitoring.Enabled || p.LLMObservability.Enabled || p.CIVisibility.Enabled || p.LiveDebugging.Enabled
	if enabled && c.ProductProxy == nil {
		return errors.New("enabled products require product_proxy configuration")
	}
	if c.ProductProxy == nil {
		return nil
	}
	if c.ProductProxy.ServerConfig.NetAddr.Endpoint == "" {
		return errors.New("product_proxy.endpoint is required")
	}
	// Site is a DNS suffix, never a URL or client-selected upstream.
	if strings.ContainsAny(c.API.Site, "/:@?#\\ \t\r\n") || strings.Trim(c.API.Site, ".") != c.API.Site {
		return errors.New("api.site must be a DNS suffix when product_proxy is configured")
	}
	if len(c.ProductProxy.ServerConfig.CompressionAlgorithms) > 0 {
		return errors.New("product_proxy must preserve compression; omit compression_algorithms or set it to []")
	}
	return nil
}

func newProductForwarder(c *Config, hostname string, settings component.TelemetrySettings) (extension.Extension, error) {
	return httpforwarderextension.NewForwarder(productForwarderConfig(c, hostname), settings)
}

// productForwarderConfig contains product policy only. The existing generic
// extension owns HTTP lifecycle, body streaming, TLS, proxying and responses.
func productForwarderConfig(c *Config, hostname string) *httpforwarderextension.Config {
	p := c.ProductProxy
	ingress := p.ServerConfig
	if ingress.NetAddr.Transport == "" {
		ingress.NetAddr.Transport = confignet.TransportTypeTCP
	}
	ingress.CompressionAlgorithms = []string{}
	if ingress.MaxRequestBodySize == 0 {
		ingress.MaxRequestBodySize = 32 * 1024 * 1024
	}
	client := c.ClientConfig
	client.Endpoint = "https://api." + c.API.Site
	cfg := &httpforwarderextension.Config{Ingress: ingress, Egress: client}
	tags := append([]string{"host:" + hostname, "default_env:" + p.DefaultEnv}, p.Tags...)
	metadata := strings.Join(tags, ",")
	endpoints := []string{}
	add := func(path, subdomain, destination string, match bool) *httpforwarderextension.Route {
		route := httpforwarderextension.Route{
			Path: path, Method: http.MethodPost,
			Endpoint: "https://" + subdomain + "." + c.API.Site + destination,
			Headers: map[string]configopaque.String{
				"DD-API-KEY":                c.API.Key,
				"X-Datadog-Hostname":        configopaque.String(hostname),
				"X-Datadog-AgentDefaultEnv": configopaque.String(p.DefaultEnv),
				"X-Datadog-Additional-Tags": configopaque.String(metadata),
			},
			RemoveHeaders: []string{"Authorization", "DD-APPLICATION-KEY", "Cookie", "X-Datadog-EVP-Subdomain", "X-Datadog-NeedsAppKey", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"},
		}
		if match {
			route.MatchHeaders = map[string]string{"X-Datadog-EVP-Subdomain": subdomain}
		}
		cfg.Routes = append(cfg.Routes, route)
		return &cfg.Routes[len(cfg.Routes)-1]
	}
	if c.Products.ContinuousProfiling.Enabled {
		r := add("/profiling/v1/input", "intake.profile", "/api/v2/profile", false)
		r.ResponseStatus = map[string]int{"202": http.StatusOK}
		endpoints = append(endpoints, "/profiling/v1/input")
	}
	if c.Products.DataStreamsMonitoring.Enabled {
		add("/v0.1/pipeline_stats", "trace.agent", "/api/v0.1/pipeline_stats", false)
		endpoints = append(endpoints, "/v0.1/pipeline_stats")
	}
	if c.Products.DataJobsMonitoring.Enabled {
		r := add("/openlineage/api/v1/lineage", "data-obs-intake", "/api/v1/lineage?api-version=2", false)
		delete(r.Headers, "DD-API-KEY")
		r.RemoveHeaders = append(r.RemoveHeaders, "DD-API-KEY")
		r.Headers["Authorization"] = configopaque.String("Bearer " + string(c.API.Key))
		endpoints = append(endpoints, "/openlineage/api/v1/lineage")
	}
	evp := func(domain, path string) {
		for _, version := range []string{"v2", "v4"} {
			r := add("/evp_proxy/"+version+path, domain, path, true)
			if domain == "api" && p.ApplicationKey != "" && strings.HasPrefix(path, "/api/intake/llm-obs/") {
				r.Headers["DD-APPLICATION-KEY"] = p.ApplicationKey
			}
		}
	}
	if c.Products.LLMObservability.Enabled {
		evp("llmobs-intake", "/api/v2/llmobs")
		evp("api", "/api/intake/llm-obs/v1/eval-metric")
		evp("api", "/api/intake/llm-obs/v2/eval-metric")
	}
	if c.Products.CIVisibility.Enabled {
		evp("citestcycle-intake", "/api/v2/citestcycle")
		evp("citestcov-intake", "/api/v2/citestcov")
		evp("ci-intake", "/api/v2/cicovreprt")
		for _, path := range []string{"/api/v2/libraries/tests/services/setting", "/api/v2/ci/tests/skippable", "/api/v2/ci/libraries/tests", "/api/v2/ci/libraries/tests/flaky", "/api/v2/test/libraries/test-management/tests", "/api/v2/git/repository/search_commits", "/api/v2/git/repository/packfile"} {
			evp("api", path)
		}
	}
	if c.Products.LLMObservability.Enabled || c.Products.CIVisibility.Enabled {
		endpoints = append(endpoints, "/evp_proxy/v2/", "/evp_proxy/v4/")
	}
	if c.Products.LiveDebugging.Enabled {
		for _, route := range []struct{ path, domain string }{{"/debugger/v1/input", "http-intake.logs"}, {"/debugger/v1/diagnostics", "debugger-intake"}, {"/debugger/v2/input", "debugger-intake"}, {"/symdb/v1/input", "debugger-intake"}} {
			destination := "/api/v2/debugger"
			if route.domain == "http-intake.logs" {
				destination = "/api/v2/logs"
			}
			r := add(route.path, route.domain, destination, false)
			r.AppendQuery = map[string]string{"ddtags": metadata}
			r.RequestIDHeader = "DD-REQUEST-ID"
			r.Headers["DD-EVP-ORIGIN"] = "agent-debugger"
			if route.path == "/symdb/v1/input" {
				r.Headers["DD-EVP-ORIGIN"] = "agent-symdb"
			}
			endpoints = append(endpoints, route.path)
		}
	}
	// Do not advertise native trace ingestion, remote configuration, or fabricated
	// Agent versions. Discovery states only the routes this listener implements.
	body, _ := json.Marshal(map[string]any{"endpoints": endpoints, "client_drop_p0s": false, "span_meta_structs": false, "long_running_spans": false, "config": map[string]string{"default_env": p.DefaultEnv}, "version": "otel-collector-product-proxy"})
	cfg.Routes = append(cfg.Routes, httpforwarderextension.Route{Path: "/info", Method: http.MethodGet, Response: &httpforwarderextension.StaticResponse{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Body: string(body)}})
	return cfg
}
