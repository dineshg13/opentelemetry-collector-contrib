#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real SDK local probes and OTLP traces; observe HTTP without changing transport."""
import email.parser
import email.policy
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
import signal
import threading
import time
from urllib.parse import urlsplit

observed = []
lock = threading.Lock()
original_request = http.client.HTTPConnection.request
original_response = http.client.HTTPConnection.getresponse


def payload_summary(body, headers):
    content_type = next((value for key, value in headers.items() if key.lower() == "content-type"), "")
    raw = body.encode() if isinstance(body, str) else bytes(body or b"")
    documents = []
    if "multipart/" in content_type:
        message = email.parser.BytesParser(policy=email.policy.default).parsebytes(
            b"Content-Type: " + content_type.encode() + b"\r\n\r\n" + raw)
        for part in message.iter_parts():
            if part.get_param("name", header="content-disposition") == "event":
                documents = json.loads(part.get_payload(decode=True))
    elif raw:
        documents = json.loads(raw)
    if not isinstance(documents, list):
        documents = []
    snapshots = [entry["debugger"]["snapshot"] for entry in documents if entry.get("debugger", {}).get("snapshot")]
    diagnostics = [entry["debugger"]["diagnostics"] for entry in documents if entry.get("debugger", {}).get("diagnostics")]
    return {"bytes": len(raw), "snapshots": len(snapshots),
            "captured_snapshots": sum(bool(snapshot.get("captures")) for snapshot in snapshots),
            "trace_correlated_snapshots": sum(bool(entry.get("dd", {}).get("trace_id")) for entry in documents),
            "probe_ids": sorted({snapshot.get("probe", {}).get("id", "") for snapshot in snapshots}
                                | {diagnostic.get("probeId", "") for diagnostic in diagnostics}),
            "diagnostics": sorted({diagnostic.get("status", "") for diagnostic in diagnostics})}


def observe_request(connection, method, url, body=None, headers=None, *, encode_chunked=False):
    path = urlsplit(url).path
    summary = {"event": "sdk_http", "method": method, "path": path}
    if path.startswith("/debugger/"):
        try:
            summary.update(payload_summary(body, headers or {}))
        except Exception as error:
            summary["observation_error"] = type(error).__name__
    connection._poc_observation = summary
    return original_request(connection, method, url, body=body, headers=headers or {}, encode_chunked=encode_chunked)


def observe_response(connection):
    response = original_response(connection)
    summary = getattr(connection, "_poc_observation", {})
    if summary.get("path", "").startswith(("/debugger/", "/v0.7/config", "/info")):
        summary = dict(summary, status=response.status)
        with lock:
            observed.append(summary)
            # Keep health response and process memory bounded for a continuous deployment.
            del observed[:-100]
        print(json.dumps(summary), flush=True)
    return response


# These wrappers always call the original HTTP transport and return its real
# response. They never redirect, acknowledge, replace or modify SDK requests.
http.client.HTTPConnection.request = observe_request
http.client.HTTPConnection.getresponse = observe_response

import ddtrace.auto  # noqa: E402,F401
import ddtrace  # noqa: E402
from ddtrace import tracer  # noqa: E402
import workload  # noqa: E402

stopped = threading.Event()
iterations = 0


class Health(BaseHTTPRequestHandler):
    def do_GET(self):
        with lock:
            body = json.dumps({"iterations": iterations, "sdk": ddtrace.__version__, "http": list(observed)}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass


def main():
    global iterations
    assert getattr(tracer._span_aggregator.writer, "_otlp_endpoint", None) == os.environ["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]
    threading.Thread(target=ThreadingHTTPServer(("0.0.0.0", 8080), Health).serve_forever, daemon=True).start()
    signal.signal(signal.SIGTERM, lambda *_: stopped.set())
    signal.signal(signal.SIGINT, lambda *_: stopped.set())
    count = int(os.getenv("ITERATIONS", "0"))
    print(json.dumps({"event": "ready", "sdk": ddtrace.__version__, "sdk_di_enabled": os.getenv("DD_DYNAMIC_INSTRUMENTATION_ENABLED"),
                      "product_proxy": os.environ["DD_TRACE_AGENT_URL"], "otlp_endpoint": os.environ["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]}), flush=True)
    while not stopped.is_set() and (not count or iterations < count):
        with tracer.trace("debugger.workload") as span:
            result = workload.calculate(iterations)
            assert result["doubled"] == iterations * 2
            print(json.dumps({"event": "activity", "iteration": iterations,
                              "trace_id": f"{span.trace_id:032x}", "span_id": f"{span.span_id:016x}"}), flush=True)
        tracer.flush()
        iterations += 1
        stopped.wait(2)
    # Give the real SDK background uploader time to finish its last batch.
    time.sleep(2)
    tracer.shutdown()
    with lock:
        print(json.dumps({"event": "summary", "iterations": iterations, "observed_requests": list(observed)}), flush=True)


if __name__ == "__main__":
    main()
