// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httpforwarderextension

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/common/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configopaque"
)

func startRouteForwarder(t *testing.T, cfg *Config) string {
	t.Helper()
	cfg.Ingress.NetAddr.Endpoint = testutil.GetAvailableLocalAddress(t)
	cfg.Ingress.CompressionAlgorithms = []string{}
	ext, err := NewForwarder(cfg, componenttest.NewNopTelemetrySettings())
	require.NoError(t, err)
	require.NoError(t, ext.Start(t.Context(), componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, ext.Shutdown(t.Context())) })
	return "http://" + cfg.Ingress.NetAddr.Endpoint
}

func TestRoutePayloadCredentialsAndResponse(t *testing.T) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	_, err := gzipWriter.Write([]byte("opaque product payload\x00\xff"))
	require.NoError(t, err)
	require.NoError(t, gzipWriter.Close())
	var called atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, compressed.Bytes(), body)
		require.Equal(t, "/native", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("api-version"))
		require.Equal(t, "keep", r.URL.Query().Get("other"))
		require.Equal(t, "service:sdk,host:collector", r.URL.Query().Get("ddtags"))
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		require.Equal(t, []string{"trusted"}, r.Header.Values("DD-API-KEY"))
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("X-Private-Hop"))
		_, err = uuid.Parse(r.Header.Get("X-Request-ID"))
		require.NoError(t, err)
		w.Header().Add("Set-Cookie", "first=1")
		w.Header().Add("Set-Cookie", "second=2")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer backend.Close()
	cfg := createDefaultConfig().(*Config)
	cfg.Routes = []Route{{Path: "/input", Method: "POST", MatchHeaders: map[string]string{"X-Product": "profile"}, Endpoint: backend.URL + "/native?api-version=2", Headers: map[string]configopaque.String{"DD-API-KEY": "trusted"}, RemoveHeaders: []string{"Authorization"}, ResponseStatus: map[int]int{202: 200}, AppendQuery: map[string]string{"ddtags": "host:collector"}, RequestIDHeader: "X-Request-ID"}}
	address := startRouteForwarder(t, cfg)
	request, err := http.NewRequest("POST", address+"/input?api-version=bad&other=keep&ddtags=service:sdk", bytes.NewReader(compressed.Bytes()))
	require.NoError(t, err)
	request.Header.Set("X-Product", "profile")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Add("DD-API-KEY", "untrusted")
	request.Header.Add("DD-API-KEY", "also-untrusted")
	request.Header.Set("Authorization", "untrusted")
	request.Header.Set("Connection", "X-Private-Hop")
	request.Header.Set("X-Private-Hop", "secret")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, 200, response.StatusCode)
	require.Equal(t, []string{"first=1", "second=2"}, response.Header.Values("Set-Cookie"))
	require.Equal(t, "7", response.Header.Get("Retry-After"))
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "accepted", string(body))
	require.EqualValues(t, 1, called.Load())
	for _, path := range []string{"/input/extra", "/other", "/%69nput", "/input"} {
		response, err := http.Get(address + path)
		require.NoError(t, err)
		require.Equal(t, 404, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
	require.EqualValues(t, 1, called.Load())
}

func TestRoutesDisableDiscoveryErrorsAndRedirect(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("retry later"))
	}))
	defer backend.Close()
	cfg := createDefaultConfig().(*Config)
	cfg.Routes = []Route{
		{Path: "/disabled", Disabled: true, Endpoint: destination.URL},
		{Path: "/info", Response: &StaticResponse{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Body: `{"endpoints":[]}`}},
		{Path: "/redirect", Endpoint: backend.URL + "/redirect", Headers: map[string]configopaque.String{"DD-API-KEY": "trusted"}},
		{Path: "/error", Endpoint: backend.URL + "/error", ResponseStatus: map[int]int{202: 200}},
	}
	address := startRouteForwarder(t, cfg)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for path, status := range map[string]int{"/disabled": 404, "/info": 200, "/redirect": 307, "/error": 429} {
		response, err := client.Get(address + path)
		require.NoError(t, err)
		require.Equal(t, status, response.StatusCode, path)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		if path == "/error" {
			require.Equal(t, "3", response.Header.Get("Retry-After"))
			require.Equal(t, "retry later", string(body))
		}
	}
	require.EqualValues(t, 0, destinationCalls.Load())
}

func TestRouteValidation(t *testing.T) {
	for _, route := range []Route{
		{Path: "relative", Endpoint: "https://example.com"},
		{Path: "/input?query", Endpoint: "https://example.com"},
		{Path: "/input", Endpoint: "file:///tmp/file"},
		{Path: "/input", Endpoint: "https://user:password@example.com"},
		{Path: "/input"},
		{Path: "/input", Endpoint: "https://example.com", Response: &StaticResponse{Status: 200}},
		{Path: "/info", Response: &StaticResponse{Status: 0}},
		{Path: "/input", Endpoint: "https://example.com", ResponseStatus: map[int]int{202: 999}},
	} {
		cfg := createDefaultConfig().(*Config)
		cfg.Routes = []Route{route}
		require.Error(t, cfg.Validate())
	}
	cfg := createDefaultConfig().(*Config)
	cfg.Routes = []Route{{Path: "/disabled", Disabled: true, Endpoint: "https://example.com"}}
	address := startRouteForwarder(t, cfg)
	response, err := http.Post(address+"/disabled", "text/plain", nil)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, 404, response.StatusCode)
}
