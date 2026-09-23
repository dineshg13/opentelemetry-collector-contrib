# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Full SDK container tests; the local capture fixture is never a deployment destination."""
import argparse
from datetime import datetime, timezone
import gzip
import hashlib
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time

import msgpack


def read_body(handler):
    if handler.headers.get("Transfer-Encoding", "").lower() != "chunked":
        return handler.rfile.read(int(handler.headers.get("Content-Length", "0")))
    chunks = []
    while True:
        size = int(handler.rfile.readline().split(b";", 1)[0], 16)
        if size == 0:
            while handler.rfile.readline() not in (b"\r\n", b"\n", b""):
                pass
            return b"".join(chunks)
        chunks.append(handler.rfile.read(size))
        assert handler.rfile.read(2) == b"\r\n"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--languages", nargs="+", default=["python", "java", "node"])
    parser.add_argument("--collector", help="Actual Collector binary: exercise SDK -> generic forwarder and OTLP pipeline -> fixture")
    parser.add_argument("--proxy-disabled", action="store_true", help="With --collector, deny the DSM proxy while keeping OTLP")
    parser.add_argument("--python-runtime", help="Use this isolated ddtrace install for Python instead of Docker")
    args = parser.parse_args()
    records = []
    advertise = True

    class Capture(BaseHTTPRequestHandler):
        def do_GET(self):
            endpoints = ["/v0.1/pipeline_stats"] if advertise else []
            body = json.dumps({"version": "otel-collector-product-proxy", "endpoints": endpoints}).encode()
            self.send_response(200 if self.path == "/info" else 404)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            body = read_body(self)
            records.append({"path": self.path, "headers": {key.lower(): value for key, value in self.headers.items()}, "body": body})
            self.send_response(200 if self.path in ("/v0.1/pipeline_stats", "/api/v0.1/pipeline_stats", "/v1/traces") else 404)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"{}")

        do_PUT = do_POST

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Capture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{server.server_port}"
    results = []

    def run(language, enabled, discovery=True):
        nonlocal advertise
        advertise = discovery
        records.clear()
        process = None
        directory = tempfile.TemporaryDirectory(prefix="ddot-dsm-collector-")
        proxy_url, trace_url = url, url
        log = None
        if args.collector:
            def port():
                with socket.socket() as reservation:
                    reservation.bind(("127.0.0.1", 0))
                    return reservation.getsockname()[1]

            proxy_port, otlp_port = port(), port()
            proxy_url, trace_url = f"http://127.0.0.1:{proxy_port}", f"http://127.0.0.1:{otlp_port}"
            config = {
                "extensions": {"http_forwarder": {
                    "ingress": {"endpoint": f"127.0.0.1:{proxy_port}", "compression_algorithms": []},
                    "egress": {"endpoint": url}, "routes": [
                        {"path": "/info", "method": "GET", "response": {"status": 200,
                         "headers": {"Content-Type": "application/json"}, "body": json.dumps({
                         "version": "otel-collector-product-proxy", "endpoints": ["/v0.1/pipeline_stats"] if discovery and not args.proxy_disabled else []})}},
                        {"path": "/v0.1/pipeline_stats", "method": "POST", "disabled": args.proxy_disabled,
                         "endpoint": url + "/api/v0.1/pipeline_stats", "headers": {
                             "DD-API-KEY": "test-fixture-only", "X-Datadog-Additional-Tags": "host:fixture,default_env:ddot-poc"}}]}},
                "receivers": {"otlp": {"protocols": {"http": {"endpoint": f"127.0.0.1:{otlp_port}"}}}},
                "processors": {"batch": {"timeout": "100ms"}},
                "exporters": {"otlphttp": {"endpoint": url, "encoding": "json"}},
                "service": {"extensions": ["http_forwarder"], "pipelines": {"traces": {
                    "receivers": ["otlp"], "processors": ["batch"], "exporters": ["otlphttp"]}}},
            }
            config_path = Path(directory.name) / "config.yaml"
            config_path.write_text(json.dumps(config))
            log = (Path(directory.name) / "collector.log").open("w+")
            process = subprocess.Popen([args.collector, "--config", str(config_path)], stdout=log, stderr=log)
            deadline = time.monotonic() + 10
            while True:
                if process.poll() is not None:
                    log.seek(0)
                    raise RuntimeError(log.read())
                try:
                    with socket.create_connection(("127.0.0.1", proxy_port), timeout=0.1):
                        break
                except OSError:
                    if time.monotonic() > deadline:
                        raise TimeoutError("Collector failed to start")
                    time.sleep(0.05)
            if args.proxy_disabled:
                connection = http.client.HTTPConnection("127.0.0.1", proxy_port, timeout=3)
                connection.request("POST", "/v0.1/pipeline_stats", b"disabled-route-probe")
                response = connection.getresponse()
                assert response.status == 404, response.status
                response.read()
                connection.close()
                connection = http.client.HTTPConnection("127.0.0.1", proxy_port, timeout=3)
                connection.request("GET", "/info")
                response = connection.getresponse()
                assert "/v0.1/pipeline_stats" not in json.loads(response.read())["endpoints"]
                connection.close()
        command = ["docker", "run", "--rm", "--network=host", "-e", "DD_TRACE_AGENT_URL=" + proxy_url,
                   "-e", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=" + trace_url + "/v1/traces",
                   "-e", "DD_DATA_STREAMS_ENABLED=" + str(enabled).lower(),
                   "-e", "WORKLOAD_ITERATIONS=2", "ddot-dsm-" + language + ":poc"]
        environment = None
        if language == "python" and args.python_runtime:
            environment = {key: value for key, value in os.environ.items() if not key.startswith(("DD_", "_DD_", "OTEL_"))}
            environment.update({"PYTHONPATH": args.python_runtime, "PYTHONDONTWRITEBYTECODE": "1",
                "DD_SERVICE": "ddot-dsm-python", "DD_ENV": "ddot-poc", "DD_VERSION": "1",
                "DD_TRACE_ENABLED": "true", "OTEL_TRACES_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/json",
                "DD_TRACE_AGENT_URL": proxy_url, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": trace_url + "/v1/traces",
                "DD_DATA_STREAMS_ENABLED": str(enabled).lower(), "WORKLOAD_ITERATIONS": "2",
                "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false", "DD_REMOTE_CONFIGURATION_ENABLED": "false"})
            command = [sys.executable, str(Path(__file__).resolve().parents[1] / "apps/python/app.py")]
        try:
            child = subprocess.run(command, env=environment, capture_output=True, text=True, timeout=40)
        finally:
            if process:
                # Collector shutdown flushes pending batches before parsing the evidence.
                process.terminate()
                process.wait(timeout=10)
            if log:
                log.close()
            directory.cleanup()
        assert child.returncode == 0, child.stdout + child.stderr
        points, payloads, traces, empty_probes = [], [], [], []
        for request in records:
            if request["path"] in ("/v0.1/pipeline_stats", "/api/v0.1/pipeline_stats"):
                if args.collector:
                    assert request["path"] == "/api/v0.1/pipeline_stats"
                    assert request["headers"]["dd-api-key"] == "test-fixture-only"
                    assert "host:fixture" in request["headers"]["x-datadog-additional-tags"]
                assert request["headers"]["content-encoding"] == "gzip"
                payload = msgpack.unpackb(gzip.decompress(request["body"]), raw=False, strict_map_key=False)
                assert payload["Service"] == "ddot-dsm-" + language, payload["Service"]
                payloads.append(payload)
                points.extend(point for bucket in payload["Stats"] for point in bucket["Stats"])
            elif request["path"] == "/v1/traces":
                body = request["body"]
                if request["headers"].get("content-encoding") == "gzip":
                    body = gzip.decompress(body)
                for resource in json.loads(body)["resourceSpans"]:
                    for scope in resource["scopeSpans"]:
                        traces.extend(scope["spans"])
            else:
                if request["path"].endswith("/traces"):
                    # Java discovery can probe a native endpoint with an empty list.
                    # Never accept ordinary native spans as a substitute for OTLP.
                    probe = msgpack.unpackb(request["body"], raw=False)
                    assert isinstance(probe, list) and not any(probe), (request["path"], request["body"])
                    empty_probes.append(request["path"])
        assert len(traces) >= 4, (language, records, child.stderr)
        expected_stats = enabled and not args.proxy_disabled and (language != "java" or discovery)
        assert bool(points) == expected_stats, (language, payloads, child.stderr)
        linked = False
        if expected_stats:
            assert len(points) >= 2
            hashes = {point["Hash"] for point in points}
            linked = any(point["ParentHash"] in hashes for point in points)
            assert linked, points
            assert any("direction:out" in point["EdgeTags"] for point in points)
            assert any("direction:in" in point["EdgeTags"] for point in points)
            for point in points:
                for sketch in ("PathwayLatency", "EdgeLatency"):
                    assert isinstance(point[sketch], bytes) and len(point[sketch]) > 0
            if language == "python":
                offsets = [backlog["Value"] for payload in payloads for bucket in payload["Stats"] for backlog in bucket["Backlogs"]]
                assert sorted(offsets) == [8, 12], offsets
        result = {"language": language, "enabled": enabled, "advertise_pipeline_stats": discovery,
                  "stats_requests": len(payloads), "pathway_points": len(points), "linked_points": linked,
                  "otlp_spans": len(traces), "native_trace_requests": 0,
                  "empty_native_discovery_probes": empty_probes,
                  "paths": sorted({record["path"] for record in records}), "stdout": child.stdout.splitlines()}
        results.append(result)
        print(json.dumps({key: result[key] for key in ("language", "enabled", "advertise_pipeline_stats", "pathway_points", "otlp_spans")}), flush=True)

    try:
        for language in args.languages:
            run(language, True)
            run(language, False)
        if "java" in args.languages:
            run("java", True, discovery=False)
    finally:
        server.shutdown()
        server.server_close()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps({"timestamp": datetime.now(timezone.utc).isoformat(),
        "scope": "actual SDK images through Collector generic proxy/OTLP pipeline to fixture" if args.collector else "actual SDK images against local HTTP wire fixture", "backend_verified": False,
        "collector_sha256": hashlib.sha256(Path(args.collector).read_bytes()).hexdigest() if args.collector else None,
        "proxy_disabled": args.proxy_disabled,
        "checks": results}, indent=2) + "\n")


if __name__ == "__main__":
    main()
