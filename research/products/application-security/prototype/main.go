// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This executable exercises the unmodified native receiver over HTTP.
// Its payload is a synthetic security span, not output from a running SDK/WAF.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogreceiver"
	"github.com/vmihailenco/msgpack/v5"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func request(client *http.Client, method, url string, body []byte) (int, []byte) {
	r, err := http.NewRequest(method, url, bytes.NewReader(body))
	must(err)
	r.Header.Set("Content-Type", "application/msgpack")
	r.Header.Set("X-Datadog-Trace-Count", "1")
	r.Header.Set("Datadog-Meta-Lang", "fixture")
	resp, err := client.Do(r)
	must(err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	must(err)
	return resp.StatusCode, data
}

func main() {
	payloadFile := flag.String("payload", "", "optional real SDK v0.4 payload to replay")
	flag.Parse()
	ctx := context.Background()
	// Reserve an ephemeral port, then release it for the component's own listener.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	address := l.Addr().String()
	must(l.Close())
	factory := datadogreceiver.NewFactory()
	cfg := factory.CreateDefaultConfig().(*datadogreceiver.Config)
	cfg.ServerConfig.NetAddr.Endpoint = address
	traces := make(chan ptrace.Traces, 1)
	sink, err := consumer.NewTraces(func(_ context.Context, td ptrace.Traces) error {
		copy := ptrace.NewTraces()
		td.CopyTo(copy)
		traces <- copy
		return nil
	})
	must(err)
	rcv, err := factory.CreateTraces(ctx, receivertest.NewNopSettings(factory.Type()), cfg, sink)
	must(err)
	must(rcv.Start(ctx, componenttest.NewNopHost()))
	defer func() { must(rcv.Shutdown(ctx)) }()
	client := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + address
	status, infoBody := request(client, "GET", base+"/info", nil)
	if status != 200 {
		panic("info failed")
	}
	var info map[string]any
	must(json.Unmarshal(infoBody, &info))
	if info["span_meta_structs"] != false {
		panic("source changed: re-evaluate structural preservation")
	}
	structured, err := msgpack.Marshal(map[string]any{"exploit": []any{map[string]any{"id": "fixture-stack"}}})
	must(err)
	span := map[string]any{
		"service": "appsec-fixture", "name": "web.request", "resource": "GET /fixture", "type": "web",
		"trace_id": uint64(42), "span_id": uint64(7), "parent_id": uint64(0),
		"start": time.Now().UnixNano(), "duration": int64(1000000), "error": int32(0),
		"meta":        map[string]string{"_dd.appsec.json": "{\"triggers\":[{\"rule\":{\"id\":\"fixture\"}}]}", "appsec.event": "true", "_dd.p.dm": "-5", "_dd.p.ts": "02", "_dd.iast.json": "{\"vulnerabilities\":[]}", "_dd.appsec.s.req.body": "fixture-schema"},
		"metrics":     map[string]float64{"_sampling_priority_v1": 2, "_dd.appsec.enabled": 1, "_dd.apm.enabled": 0},
		"meta_struct": map[string][]byte{"_dd.stack": structured, "appsec": structured, "iast": structured},
	}
	body, err := msgpack.Marshal([]any{[]any{span}})
	must(err)
	if *payloadFile != "" {
		body, err = os.ReadFile(*payloadFile)
		must(err)
	}
	status, traceResponse := request(client, "POST", base+"/v0.4/traces", body)
	if status != 200 {
		panic(fmt.Sprintf("trace status=%d body=%s", status, traceResponse))
	}
	var td ptrace.Traces
	select {
	case td = <-traces:
	case <-time.After(5 * time.Second):
		panic("missing traces")
	}
	got := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	if *payloadFile != "" {
		found := false
		for _, resource := range td.ResourceSpans().All() {
			for _, scope := range resource.ScopeSpans().All() {
				for _, candidate := range scope.Spans().All() {
					if value, ok := candidate.Attributes().Get("appsec.event"); ok && value.Str() == "true" {
						got = candidate
						found = true
					}
				}
			}
		}
		if !found {
			panic("no security span in SDK replay")
		}
	}
	attrs := got.Attributes().AsRaw()
	if *payloadFile == "" {
		for _, key := range []string{"_dd.appsec.json", "appsec.event", "_dd.p.dm", "_dd.p.ts", "_dd.iast.json", "_dd.appsec.s.req.body", "_dd.appsec.enabled", "_dd.apm.enabled", "sampling.priority"} {
			if _, ok := attrs[key]; !ok {
				panic("missing expected scalar: " + key)
			}
		}
	}
	for _, key := range []string{"meta_struct", "_dd.stack", "appsec", "iast"} {
		if _, ok := attrs[key]; ok {
			panic("source changed: structured data retained")
		}
	}
	rcStatus, rcBody := request(client, "POST", base+"/v0.7/config", []byte(`{"client":{"products":["ASM_FEATURES"]}}`))
	unknownStatus, _ := request(client, "POST", base+"/appsec/unsupported", []byte(`{}`))
	if rcStatus != 200 || len(rcBody) != 0 || unknownStatus != 200 {
		panic("catch-all behavior changed")
	}
	result := map[string]any{"evidence": "synthetic native msgpack -> actual datadogreceiver -> captured pdata", "info": info, "span_attributes": attrs, "meta_struct_dropped": true, "trace_response": string(traceResponse), "rc_status": rcStatus, "rc_body": string(rcBody), "unknown_route_status": unknownStatus, "backend_validated": false}
	if *payloadFile != "" {
		result["evidence"] = "real SDK payload replay -> actual datadogreceiver -> captured pdata"
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	must(err)
	fmt.Println(string(encoded))
}
