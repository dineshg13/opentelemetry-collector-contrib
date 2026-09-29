// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httpforwarderextension // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/httpforwarderextension"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"
)

type httpForwarder struct {
	forwardTo  *url.URL
	httpClient *http.Client
	server     *http.Server
	settings   component.TelemetrySettings
	config     *Config
	shutdownWG sync.WaitGroup
}

var _ extension.Extension = (*httpForwarder)(nil)

func (h *httpForwarder) Start(ctx context.Context, host component.Host) error {
	listener, err := h.config.Ingress.ToListener(ctx)
	if err != nil {
		return fmt.Errorf("failed to bind to address %s: %w", h.config.Ingress.NetAddr.Endpoint, err)
	}

	clientConfig := h.config.Egress
	// Headers are applied explicitly below so route headers take precedence.
	// A client transport header wrapper would overwrite them a second time.
	clientConfig.Headers = nil
	httpClient, err := clientConfig.ToClient(ctx, host.GetExtensions(), h.settings)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to create HTTP Client: %w", err)
	}
	h.httpClient = httpClient
	// Redirects must be returned to the caller. Following one could disclose
	// configured credentials to a destination outside the route allowlist.
	h.httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	handler := http.NewServeMux()
	handler.HandleFunc("/", h.forwardRequest)

	h.server, err = h.config.Ingress.ToServer(ctx, host.GetExtensions(), h.settings, handler)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to create HTTP Client: %w", err)
	}

	h.shutdownWG.Go(func() {
		if errHTTP := h.server.Serve(listener); !errors.Is(errHTTP, http.ErrServerClosed) && errHTTP != nil {
			componentstatus.ReportStatus(host, componentstatus.NewFatalErrorEvent(errHTTP))
		}
	})

	return nil
}

func (h *httpForwarder) Shutdown(_ context.Context) error {
	if h.server == nil {
		return nil
	}
	err := h.server.Close()
	if h.httpClient != nil {
		h.httpClient.CloseIdleConnections()
	}
	h.shutdownWG.Wait()
	return err
}

func (h *httpForwarder) forwardRequest(writer http.ResponseWriter, request *http.Request) {
	var matched *Route
	if len(h.config.Routes) > 0 {
		for i := range h.config.Routes {
			r := &h.config.Routes[i]
			if r.Disabled {
				continue
			}
			if request.URL.EscapedPath() != r.Path || (r.Method != "" && r.Method != request.Method) {
				continue
			}
			matches := true
			for key, value := range r.MatchHeaders {
				if request.Header.Get(key) != value {
					matches = false
					break
				}
			}
			if matches {
				matched = r
				break
			}
		}
		if matched == nil {
			http.NotFound(writer, request)
			return
		}
		if matched.Response != nil {
			for k, v := range matched.Response.Headers {
				writer.Header().Set(k, v)
			}
			writer.WriteHeader(matched.Response.Status)
			_, _ = io.WriteString(writer, matched.Response.Body)
			return
		}
	}
	forwarderRequest := request.Clone(request.Context())
	target := h.forwardTo
	if matched != nil {
		target, _ = url.Parse(matched.Endpoint) // Validated at construction.
		forwarderRequest.URL.Path = target.Path
		forwarderRequest.URL.RawPath = target.RawPath
		// The route's configured query parameters override the caller's values.
		query := forwarderRequest.URL.Query()
		for k, values := range target.Query() {
			query[k] = values
		}
		for k, value := range matched.AppendQuery {
			if previous := query.Get(k); previous != "" {
				value = previous + "," + value
			}
			query.Set(k, value)
		}
		forwarderRequest.URL.RawQuery = query.Encode()
	}
	forwarderRequest.URL.Host = target.Host
	forwarderRequest.URL.Scheme = target.Scheme
	forwarderRequest.Host = target.Host
	// Clear RequestURI to avoid getting "http: Request.RequestURI can't be set in client requests" error.
	forwarderRequest.RequestURI = ""

	removeHopHeaders(forwarderRequest.Header)
	// Configured headers replace caller-supplied values, including credentials.
	for k, v := range h.config.Egress.Headers.Iter {
		forwarderRequest.Header.Set(k, string(v))
	}
	if matched != nil {
		for _, k := range matched.RemoveHeaders {
			forwarderRequest.Header.Del(k)
		}
		for k, v := range matched.Headers {
			forwarderRequest.Header.Set(k, string(v))
		}
		if matched.RequestIDHeader != "" {
			forwarderRequest.Header.Set(matched.RequestIDHeader, uuid.NewString())
		}
	}

	// Add "Via" header for tracking purposes on both the outgoing requests and responses.
	// See https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Via.
	addViaHeader(forwarderRequest.Header, request.Proto, request.Host)

	response, err := h.httpClient.Do(forwarderRequest)
	if err != nil {
		if matched != nil {
			h.settings.Logger.Debug("HTTP product forwarding failed", zap.String("route", matched.Path), zap.String("upstream_host", target.Host), zap.Int("status_code", http.StatusBadGateway))
		}
		http.Error(writer, "upstream request failed", http.StatusBadGateway)
		return
	}

	if response == nil {
		return
	}
	defer response.Body.Close()

	// Copy over response from the final destination.
	removeHopHeaders(response.Header)
	for k, values := range response.Header {
		writer.Header()[k] = append([]string(nil), values...)
	}
	addViaHeader(writer.Header(), response.Proto, request.Host)

	status := response.StatusCode
	if matched != nil {
		if mapped, ok := matched.ResponseStatus[strconv.Itoa(status)]; ok {
			status = mapped
		}
		h.settings.Logger.Debug("Forwarded HTTP product request", zap.String("route", matched.Path), zap.String("upstream_host", target.Host), zap.Int("upstream_status_code", response.StatusCode), zap.Int("status_code", status))
	}
	writer.WriteHeader(status)
	written, err := io.Copy(writer, response.Body)
	if err != nil {
		h.settings.Logger.Warn("Error writing HTTP response message", zap.Error(err))
	}

	if response.ContentLength >= 0 && response.ContentLength != written {
		h.settings.Logger.Warn("Response from target not fully copied, body might be corrupted")
	}
}

func addViaHeader(header http.Header, protocol, host string) {
	header.Add("Via", fmt.Sprintf("%s %s", protocol, host))
}

func newHTTPForwarder(config *Config, settings component.TelemetrySettings) (extension.Extension, error) {
	return NewForwarder(config, settings)
}

// NewForwarder constructs the same extension used by NewFactory. Other
// extensions may compose it to keep HTTP forwarding in this reusable component.
func NewForwarder(config *Config, settings component.TelemetrySettings) (extension.Extension, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	url, err := url.Parse(config.Egress.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("enter a valid URL for 'egress.endpoint': %w", err)
	}

	h := &httpForwarder{
		config:    config,
		forwardTo: url,
		settings:  settings,
	}

	return h, nil
}

func removeHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}
