// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type captured struct {
	method, uri, host string
	header            http.Header
	body              []byte
}

func runningForwarder(t *testing.T, backend, ingressExtra string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	// Port is reserved briefly, then released because the real extension owns ToListener.
	cfg, err := loadConfig([]byte(fmt.Sprintf(`ingress:
  endpoint: %s
%s
egress:
  endpoint: %s
  headers:
    DD-API-KEY: collector-key
    X-Datadog-Additional-Tags: host:research-host,default_env:research
  timeout: 2s
`, addr, ingressExtra, backend)))
	require.NoError(t, err)
	ext, err := start(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ext.Shutdown(context.Background())) })
	return "http://" + addr
}

func TestCurrentForwarderWireContract(t *testing.T) {
	requests := make(chan captured, 8)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read backend body: %v", err)
		}
		requests <- captured{r.Method, r.RequestURI, r.Host, r.Header.Clone(), body}
		w.Header().Set("Retry-After", "13")
		w.Header().Add("Set-Cookie", "first=1")
		w.Header().Add("Set-Cookie", "second=2")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"retry":true}`))
	}))
	defer backend.Close()
	proxy := runningForwarder(t, backend.URL+"/api/v2/rewritten?configured=true", "  compression_algorithms: []")
	payload := []byte{0x81, 0xa3, 'k', 'e', 'y', 0xc4, 0x02, 0, 0xff}
	for _, path := range []string{"/profiling/v1/input", "/debugger/v1/input", "/evp_proxy/v2/api/v2/llmobs", "/v0.1/pipeline_stats", "/info", "/v0.7/config"} {
		t.Run(path, func(t *testing.T) {
			uri := path + "?ddtags=env%3Atest%2Cservice%3Aorders&repeated=1&repeated=2"
			req, err := http.NewRequest(http.MethodPost, proxy+uri, bytes.NewReader(payload))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/msgpack")
			req.Header.Set("DD-API-KEY", "untrusted-incoming-key")
			req.Header.Set("X-Datadog-EVP-Subdomain", "llmobs-intake")
			req.Header.Set("Datadog-Container-ID", "synthetic-container")
			req.Header.Set("Authorization", "Bearer incoming")
			response, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
			require.NoError(t, err)
			defer response.Body.Close()
			got := <-requests
			require.Equal(t, "POST", got.method)
			require.Equal(t, uri, got.uri, "endpoint path/query are ignored; original request URI survives")
			require.Equal(t, strings.TrimPrefix(backend.URL, "http://"), got.host)
			require.Equal(t, payload, got.body)
			require.Equal(t, []string{"collector-key"}, got.header.Values("DD-API-KEY"))
			require.Equal(t, "Bearer incoming", got.header.Get("Authorization"))
			require.Equal(t, "synthetic-container", got.header.Get("Datadog-Container-ID"))
			require.Equal(t, "llmobs-intake", got.header.Get("X-Datadog-EVP-Subdomain"))
			require.Equal(t, "host:research-host,default_env:research", got.header.Get("X-Datadog-Additional-Tags"))
			require.NotEmpty(t, got.header.Get("Via"))
			require.Equal(t, 429, response.StatusCode)
			require.Equal(t, "13", response.Header.Get("Retry-After"))
			require.Equal(t, []string{"first=1"}, response.Header.Values("Set-Cookie"), "current code collapses repeated response headers")
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"retry":true}`, string(body))
		})
	}
}

func TestCompressionConfiguration(t *testing.T) {
	plain := []byte("opaque SDK body with repeated words words words")
	var zipped bytes.Buffer
	gz := gzip.NewWriter(&zipped)
	_, err := gz.Write(plain)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	for _, tc := range []struct {
		name, config string
		expected     []byte
		encoding     string
	}{
		{"default_decompresses", "", plain, ""},
		{"explicit_empty_preserves", "  compression_algorithms: []", zipped.Bytes(), "gzip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan captured, 1)
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read backend body: %v", err)
				}
				requests <- captured{header: r.Header.Clone(), body: body}
				w.WriteHeader(202)
			}))
			defer backend.Close()
			proxy := runningForwarder(t, backend.URL, tc.config)
			req, err := http.NewRequest("POST", proxy+"/api/v2/llmobs", bytes.NewReader(zipped.Bytes()))
			require.NoError(t, err)
			req.Header.Set("Content-Encoding", "gzip")
			res, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
			require.NoError(t, err)
			defer res.Body.Close()
			require.Equal(t, 202, res.StatusCode)
			got := <-requests
			require.Equal(t, tc.expected, got.body)
			require.Equal(t, tc.encoding, got.header.Get("Content-Encoding"))
		})
	}
}

func TestUnsupportedRoutingConfigurationIgnored(t *testing.T) {
	for _, extra := range []string{"  endpoints: [http://127.0.0.1:1, http://127.0.0.1:2]", "  routes: [{path: /debugger/v1/input, target: /api/v2/logs}]"} {
		cfg, err := loadConfig([]byte("egress:\n  endpoint: http://127.0.0.1:1\n" + extra + "\n"))
		require.NoError(t, err, "confighttp's custom unmarshaller ignores unknown nested keys at the researched version")
		baseline, err := loadConfig([]byte("egress:\n  endpoint: http://127.0.0.1:1\n"))
		require.NoError(t, err)
		require.Equal(t, baseline, cfg, "invented routes and destinations have no effect")
	}
}
