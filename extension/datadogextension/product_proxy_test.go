// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !aix

package datadogextension

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/confighttp"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/httpforwarderextension"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/common/testutil"
)

func productTestConfig() *Config {
	cfg := NewFactory().CreateDefaultConfig().(*Config)
	cfg.API.Key = "collector-key"
	cfg.ProductProxy = &ProductProxyConfig{ServerConfig: confighttp.NewDefaultServerConfig(), Tags: []string{"env:poc"}, DefaultEnv: "poc"}
	cfg.ProductProxy.ServerConfig.NetAddr.Endpoint = "127.0.0.1:8126"
	cfg.ProductProxy.ServerConfig.CompressionAlgorithms = nil
	return cfg
}

func startProductTestForwarder(t *testing.T, cfg *httpforwarderextension.Config) string {
	t.Helper()
	cfg.Ingress.NetAddr.Endpoint = testutil.GetAvailableLocalAddress(t)
	ext, err := httpforwarderextension.NewForwarder(cfg, componenttest.NewNopTelemetrySettings())
	require.NoError(t, err)
	require.NoError(t, ext.Start(t.Context(), componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, ext.Shutdown(t.Context())) })
	return "http://" + cfg.Ingress.NetAddr.Endpoint
}

func TestProductProxyDefaultOffAndValidation(t *testing.T) {
	cfg := productTestConfig()
	require.NoError(t, cfg.Validate())
	routes := productForwarderConfig(cfg, "collector")
	require.Len(t, routes.Routes, 1)
	address := startProductTestForwarder(t, routes)
	for _, path := range []string{"/profiling/v1/input", "/v0.1/pipeline_stats", "/openlineage/api/v1/lineage", "/evp_proxy/v2/api/v2/llmobs", "/v0.4/traces", "/v0.7/config"} {
		response, err := http.Post(address+path, "application/octet-stream", nil)
		require.NoError(t, err)
		require.Equal(t, 404, response.StatusCode, path)
		require.NoError(t, response.Body.Close())
	}
	response, err := http.Get(address + "/info")
	require.NoError(t, err)
	defer response.Body.Close()
	var info struct {
		Endpoints []string `json:"endpoints"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&info))
	require.Empty(t, info.Endpoints)
	cfg.Products.ContinuousProfiling.Enabled = true
	cfg.ProductProxy = nil
	require.ErrorContains(t, cfg.Validate(), "require product_proxy")
	cfg = productTestConfig()
	cfg.API.Site = "https://attacker.example"
	require.ErrorContains(t, cfg.Validate(), "DNS suffix")
	cfg = productTestConfig()
	cfg.Products.ApplicationSecurity.Enabled = true
	require.ErrorContains(t, cfg.Validate(), "pipeline-owned")
	cfg = productTestConfig()
	cfg.ProductProxy.ServerConfig.CompressionAlgorithms = []string{"gzip"}
	require.ErrorContains(t, cfg.Validate(), "preserve compression")
}

func TestProductProxyPolicyAndOpaqueTransport(t *testing.T) {
	cfg := productTestConfig()
	cfg.ProductProxy.ApplicationKey = "collector-app-key"
	enabled := ProductConfig{Enabled: true}
	cfg.Products = ProductsConfig{ContinuousProfiling: enabled, DataStreamsMonitoring: enabled, DataJobsMonitoring: enabled, LLMObservability: enabled, CIVisibility: enabled, LiveDebugging: enabled}
	forwarder := productForwarderConfig(cfg, "collector")
	require.NoError(t, forwarder.Validate())
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte("opaque product payload\x00\xff"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, compressed.Bytes(), body)
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		require.Equal(t, "collector", r.Header.Get("X-Datadog-Hostname"))
		require.Empty(t, r.Header.Get("Cookie"))
		require.Empty(t, r.Header.Get("X-Datadog-EVP-Subdomain"))
		if r.URL.Path == "/api/v1/lineage" {
			require.Equal(t, "Bearer collector-key", r.Header.Get("Authorization"))
			require.Empty(t, r.Header.Get("DD-API-KEY"))
			require.Equal(t, "2", r.URL.Query().Get("api-version"))
		} else {
			require.Equal(t, []string{"collector-key"}, r.Header.Values("DD-API-KEY"))
			require.Empty(t, r.Header.Get("Authorization"))
		}
		if r.URL.Path == "/api/intake/llm-obs/v2/eval-metric" {
			require.Equal(t, "collector-app-key", r.Header.Get("DD-APPLICATION-KEY"))
		} else {
			require.Empty(t, r.Header.Get("DD-APPLICATION-KEY"))
		}
		if r.URL.Path == "/api/v2/debugger" {
			require.NotEmpty(t, r.Header.Get("DD-REQUEST-ID"))
			require.Contains(t, r.URL.Query().Get("ddtags"), "service:sdk,host:collector")
		}
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(202)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer backend.Close()
	expected := map[string]string{
		"/profiling/v1/input":                             "https://intake.profile.datadoghq.com/api/v2/profile",
		"/v0.1/pipeline_stats":                            "https://trace.agent.datadoghq.com/api/v0.1/pipeline_stats",
		"/openlineage/api/v1/lineage":                     "https://data-obs-intake.datadoghq.com/api/v1/lineage?api-version=2",
		"/evp_proxy/v2/api/v2/llmobs":                     "https://llmobs-intake.datadoghq.com/api/v2/llmobs",
		"/evp_proxy/v2/api/intake/llm-obs/v2/eval-metric": "https://api.datadoghq.com/api/intake/llm-obs/v2/eval-metric",
		"/evp_proxy/v4/api/v2/citestcycle":                "https://citestcycle-intake.datadoghq.com/api/v2/citestcycle",
		"/debugger/v1/diagnostics":                        "https://debugger-intake.datadoghq.com/api/v2/debugger",
	}
	selectors := map[string]string{}
	for i := range forwarder.Routes {
		r := &forwarder.Routes[i]
		if target, ok := expected[r.Path]; ok {
			require.Equal(t, target, r.Endpoint)
			selectors[r.Path] = r.MatchHeaders["X-Datadog-EVP-Subdomain"]
		}
		if r.Endpoint != "" {
			u, err := url.Parse(r.Endpoint)
			require.NoError(t, err)
			r.Endpoint = backend.URL + u.RequestURI()
		}
	}
	address := startProductTestForwarder(t, forwarder)
	for path := range expected {
		request, err := http.NewRequest("POST", address+path+"?ddtags=service:sdk", bytes.NewReader(compressed.Bytes()))
		require.NoError(t, err)
		request.Header.Set("Content-Encoding", "gzip")
		request.Header.Set("DD-API-KEY", "untrusted")
		request.Header.Set("DD-APPLICATION-KEY", "untrusted")
		request.Header.Set("Authorization", "untrusted")
		request.Header.Set("Cookie", "private=1")
		if selector := selectors[path]; selector != "" {
			request.Header.Set("X-Datadog-EVP-Subdomain", selector)
		}
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		status := 202
		if path == "/profiling/v1/input" {
			status = 200
		}
		require.Equal(t, status, response.StatusCode, path)
		require.Equal(t, "5", response.Header.Get("Retry-After"))
		require.NoError(t, response.Body.Close())
	}
	require.EqualValues(t, len(expected), calls.Load())
	request, err := http.NewRequest("POST", address+"/evp_proxy/v2/api/v2/llmobs", nil)
	require.NoError(t, err)
	request.Header.Set("X-Datadog-EVP-Subdomain", "attacker")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, 404, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.EqualValues(t, len(expected), calls.Load())
}
