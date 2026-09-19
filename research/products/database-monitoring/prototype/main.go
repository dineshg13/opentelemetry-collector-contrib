// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Run the real Python SDK against the unmodified native Datadog receiver.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DataDog/datadog-agent/pkg/trace/transform"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogreceiver"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/featuregate"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func check(ok bool, message string) {
	if !ok {
		panic(message)
	}
}

func run(mode string, fullID bool, forwarder string) map[string]any {
	must(featuregate.GlobalRegistry().Set("receiver.datadogreceiver.Enable128BitTraceID", fullID))
	ctx := context.Background()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	address := l.Addr().String()
	must(l.Close())
	factory := datadogreceiver.NewFactory()
	cfg := factory.CreateDefaultConfig().(*datadogreceiver.Config)
	cfg.ServerConfig.NetAddr.Endpoint = address
	ch := make(chan ptrace.Traces, 4)
	sink, err := consumer.NewTraces(func(_ context.Context, td ptrace.Traces) error {
		copy := ptrace.NewTraces()
		td.CopyTo(copy)
		ch <- copy
		return nil
	})
	must(err)
	rcv, err := factory.CreateTraces(ctx, receivertest.NewNopSettings(factory.Type()), cfg, sink)
	must(err)
	must(rcv.Start(ctx, componenttest.NewNopHost()))
	defer func() { must(rcv.Shutdown(ctx)) }()
	childCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if forwarder != "" {
		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		must(err)
		proxyAddress := proxyListener.Addr().String()
		must(proxyListener.Close())
		cfgFile, err := os.CreateTemp("", "dbm-forwarder-*.yaml")
		must(err)
		defer os.Remove(cfgFile.Name())
		_, err = fmt.Fprintf(cfgFile, "ingress:\n  endpoint: %s\negress:\n  endpoint: http://%s\n", proxyAddress, address)
		must(err)
		must(cfgFile.Close())
		proxy := exec.CommandContext(childCtx, forwarder, "-config", cfgFile.Name())
		proxy.Stderr = os.Stderr
		must(proxy.Start())
		defer func() { _ = proxy.Process.Signal(os.Interrupt); _ = proxy.Wait() }()
		ready := false
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			conn, err := net.DialTimeout("tcp", proxyAddress, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				ready = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		check(ready, "forwarder never became ready")
		address = proxyAddress
	}
	cmd := exec.CommandContext(childCtx, "python3", "emit.py")
	// Keep PYTHONPATH and executable discovery but eliminate inherited SDK options.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "DD_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env,
		"DD_DBM_PROPAGATION_MODE="+mode, "DD_SERVICE=checkout", "DD_ENV=research", "DD_VERSION=1",
		"DD_TRACE_AGENT_URL=http://"+address, "DD_TRACE_API_VERSION=v0.4",
		"DD_TRACE_128_BIT_TRACEID_GENERATION_ENABLED=true", "DD_TRACE_SAMPLE_RATE=1",
		"DD_REMOTE_CONFIGURATION_ENABLED=false", "DD_INSTRUMENTATION_TELEMETRY_ENABLED=false")
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	must(err)
	var sdk map[string]any
	must(json.Unmarshal(output, &sdk))
	var td ptrace.Traces
	select {
	case td = <-ch:
	case <-time.After(5 * time.Second):
		panic("receiver produced no trace")
	}
	check(td.SpanCount() == 1, "expected one SQL span")
	span := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	attrs := span.Attributes().AsRaw()
	sql := sdk["sql"].(string)
	check(attrs["db.system.name"] == "postgresql", "lost database type")
	check(attrs["db.name"] == "orders", "lost database name")
	check(attrs["out.host"] == "database.local", "lost database hostname")
	check(attrs["peer.service"] == "orders-db", "lost database peer service")
	check(attrs["dd.span.Resource"] == "SELECT * FROM orders WHERE id = 42", "resource unexpectedly transformed")
	check(span.SpanID().String() == sdk["span_id"], "lost span ID")
	match := span.TraceID().String() == sdk["trace_id"]
	check(match == fullID, "128-bit trace ID gate behavior changed")
	resource := td.ResourceSpans().At(0).Resource()
	before := transform.GetOTelResourceV2(span, resource)
	check(before == "postgres.query", "re-evaluate receiver/exporter resource mapping")
	span.Attributes().PutStr("resource.name", attrs["dd.span.Resource"].(string))
	after := transform.GetOTelResourceV2(span, resource)
	check(after == attrs["dd.span.Resource"], "explicit resource.name bridge failed")
	if mode == "disabled" {
		check(sql == "SELECT * FROM orders WHERE id = 42", "disabled propagation changed SQL")
		check(sdk["marker"] == nil, "disabled mode added marker")
	} else {
		check(strings.HasPrefix(sql, "/*") && strings.Contains(sql, "ddps='checkout'"), "missing identity comment")
		if mode == "full" {
			check(strings.Contains(sql, "traceparent='"+sdk["traceparent"].(string)+"'"), "SQL lost traceparent")
			check(attrs["_dd.dbm_trace_injected"] == "true", "receiver lost marker")
		} else {
			check(!strings.Contains(sql, "traceparent="), "service mode unexpectedly injected trace ID")
			check(sdk["marker"] == nil, "service mode added marker")
		}
	}
	return map[string]any{"mode": mode, "receiver_128bit_gate": fullID, "via_forwarder": forwarder != "", "sql": sql,
		"exporter_resource_helper_before_bridge": before, "exporter_resource_helper_after_bridge": after,
		"sdk_version": sdk["sdk_version"], "sdk_trace_id": sdk["trace_id"], "sdk_span_id": sdk["span_id"],
		"receiver_trace_id": span.TraceID().String(), "trace_id_matches": match, "receiver_attributes": attrs}
}

func main() {
	results := []map[string]any{}
	for _, mode := range []string{"disabled", "service", "full"} {
		results = append(results, run(mode, true, ""))
	}
	results = append(results, run("full", false, ""))
	if forwarder := os.Getenv("DBM_FORWARDER_BINARY"); forwarder != "" {
		results = append(results, run("full", true, forwarder))
	}
	must(featuregate.GlobalRegistry().Set("receiver.datadogreceiver.Enable128BitTraceID", true))
	encoded, err := json.MarshalIndent(map[string]any{"evidence": "real Python propagator + native writer -> actual datadogreceiver -> pdata", "cases": results, "database_driver_validated": false, "backend_validated": false}, "", "  ")
	must(err)
	fmt.Println(string(encoded))
}
