// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httpforwarderextension // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/httpforwarderextension"

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configopaque"
)

// Config defines configuration for http forwarder extension.
type Config struct {
	// Ingress holds config settings for HTTP server listening for requests.
	Ingress confighttp.ServerConfig `mapstructure:"ingress"`

	// Egress holds config settings to use for forwarded requests.
	Egress confighttp.ClientConfig `mapstructure:"egress"`

	// Routes enables an exact-path allowlist. Unmatched requests receive 404.
	// Without routes, the existing single-origin forwarding behavior is retained.
	Routes []Route `mapstructure:"routes"`
}

// Route maps an exact incoming path and optional method/headers to a complete
// destination URL, or serves a static response. First match wins.
type Route struct {
	// Disabled retains allowlist mode while denying this route.
	Disabled       bool                           `mapstructure:"disabled"`
	Path           string                         `mapstructure:"path"`
	Method         string                         `mapstructure:"method"`
	MatchHeaders   map[string]string              `mapstructure:"match_headers"`
	Endpoint       string                         `mapstructure:"endpoint"`
	Headers        map[string]configopaque.String `mapstructure:"headers"`
	RemoveHeaders  []string                       `mapstructure:"remove_headers"`
	ResponseStatus map[string]int                 `mapstructure:"response_status"`
	Response       *StaticResponse                `mapstructure:"response"`
	// AppendQuery appends comma-separated metadata to existing query values.
	AppendQuery map[string]string `mapstructure:"append_query"`
	// RequestIDHeader replaces this header with a fresh UUID on each request.
	RequestIDHeader string `mapstructure:"request_id_header"`
}

// StaticResponse serves local discovery without forwarding to an upstream.
type StaticResponse struct {
	Status  int               `mapstructure:"status"`
	Headers map[string]string `mapstructure:"headers"`
	Body    string            `mapstructure:"body"`
}

// Validate rejects ambiguous routes and invalid upstream URLs before listening.
func (c *Config) Validate() error {
	if c.Egress.Endpoint == "" && len(c.Routes) == 0 {
		return errors.New("'egress.endpoint' config option cannot be empty")
	}
	if c.Egress.Endpoint != "" {
		if err := validateURL(c.Egress.Endpoint); err != nil {
			return fmt.Errorf("egress.endpoint: %w", err)
		}
	}
	for i, r := range c.Routes {
		if !strings.HasPrefix(r.Path, "/") || strings.ContainsAny(r.Path, "?#") {
			return fmt.Errorf("routes[%d].path must be an absolute path without query or fragment", i)
		}
		if (r.Endpoint == "") == (r.Response == nil) {
			return fmt.Errorf("routes[%d] requires exactly one of endpoint or response", i)
		}
		if r.Endpoint != "" {
			if err := validateURL(r.Endpoint); err != nil {
				return fmt.Errorf("routes[%d].endpoint: %w", i, err)
			}
		}
		if r.Response != nil && (r.Response.Status < 200 || r.Response.Status > 599) {
			return fmt.Errorf("routes[%d].response.status must be between 200 and 599", i)
		}
		for from, to := range r.ResponseStatus {
			code, err := strconv.Atoi(from)
			if err != nil || code < 200 || code > 599 || to < 200 || to > 599 {
				return fmt.Errorf("routes[%d].response_status must contain status codes between 200 and 599", i)
			}
		}
	}
	return nil
}

func validateURL(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("must be an absolute HTTP(S) URL without user information or fragment")
	}
	return nil
}
