# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real WAF and actual Collector checks; loopback results are not backend proof."""

import argparse
import base64
from datetime import datetime, timezone
import gzip
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.metadata
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


def attributes(values):
    return {item["key"]: next(iter(item["value"].values())) for item in values}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--collector", required=True)
    parser.add_argument("--python-runtime", required=True, help="PYTHONPATH containing ddtrace and Flask")
    parser.add_argument("--node-image", help="Optional built Node AppSec workload image")
    parser.add_argument("--java-agent", help="Optional Java agent for the structured metadata regression probe")
    parser.add_argument("--java-home", default=os.environ.get("JAVA_HOME", ""))
    parser.add_argument("--output")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    requests = []

    class Capture(BaseHTTPRequestHandler):
        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            if self.headers.get("Content-Encoding") == "gzip":
                body = gzip.decompress(body)
            content_type = self.headers.get("Content-Type", "")
            decoded = (msgpack.unpackb(body, raw=False) if "msgpack" in content_type else
                       json.loads(body) if "json" in content_type else None)
            requests.append({"path": self.path, "content_type": content_type, "body": decoded})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"{}")

        do_PUT = do_POST

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Capture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    endpoint = f"http://127.0.0.1:{server.server_port}"
    environment = {key: value for key, value in os.environ.items() if not key.startswith(("DD_", "_DD_", "OTEL_"))}
    environment.update(PYTHONPATH=args.python_runtime, PYTHONDONTWRITEBYTECODE="1")
    results = []

    def exercise(enabled, ingress, path, language="python"):
        requests.clear()
        env = dict(environment, DD_APPSEC_ENABLED=str(enabled).lower(), DD_TRACE_AGENT_URL=endpoint,
                   OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=ingress + "/v1/traces")
        command = [sys.executable, str(root / "app.py"), "--iterations", "1"]
        if language == "javascript":
            command = ["docker", "run", "--rm", "--network", "host", "-e", "DD_TRACE_STARTUP_LOGS=false",
                       "-e", "DD_APPSEC_ENABLED=" + str(enabled).lower(),
                       "-e", "DD_TRACE_AGENT_URL=" + endpoint,
                       "-e", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=" + ingress + "/v1/traces", args.node_image]
        child = subprocess.run(command,
                               env=env, cwd=root, capture_output=True, text=True, timeout=45)
        assert child.returncode == 0, child.stderr
        deadline = time.monotonic() + 5
        while not requests and time.monotonic() < deadline:
            time.sleep(0.05)
        assert requests, "SDK or Collector emitted no OTLP traces"
        spans = []
        for request in requests:
            assert request["path"] == "/v1/traces", request["path"]
            assert "json" in request["content_type"], request["content_type"]
            for resource in request["body"]["resourceSpans"]:
                assert attributes(resource["resource"]["attributes"])["service.name"] == (
                    "ddot-appsec-python" if language == "python" else "ddot-appsec-node")
                for scope in resource["scopeSpans"]:
                    spans.extend(scope["spans"])
        roots = [span for span in spans if attributes(span.get("attributes", []))
                 .get("operation.name") == ("flask.request" if language == "python" else "web.request")]
        assert len(roots) == 2, len(roots)
        events = [span for span in roots if attributes(span["attributes"]).get("appsec.event") == "true"]
        check = {"language": language, "appsec_enabled": enabled, "path": path, "root_spans": len(roots),
                 "security_events": len(events), "native_trace_requests": 0,
                 "sdk_stdout": child.stdout.strip().splitlines()}
        if enabled:
            assert len(events) == 1, events
            span = events[0]
            attrs = attributes(span["attributes"])
            if language == "python":
                raw = next(item["value"] for item in span["attributes"] if item["key"] == "appsec")
                assert set(raw) == {"bytesValue"}, raw
                event = msgpack.unpackb(base64.b64decode(raw["bytesValue"]), raw=False)
            else:
                event = json.loads(attrs["_dd.appsec.json"])
            triggers = event["triggers"]
            assert triggers and triggers[0]["rule"]["id"] == "ua0-600-55x", triggers
            if language == "python":
                assert triggers[0]["span_id"] == int(span["spanId"], 16), triggers
                assert attrs["_dd.p.ts"] == "02" and attrs["_dd.p.dm"] == "-5", attrs
            assert float(attrs["_sampling_priority_v1"]) == 2, attrs
            schema_keys = sorted({key for item in roots for key in attributes(item["attributes"])
                                  if key.startswith("_dd.appsec.s.")})
            assert schema_keys, "Real API schema derivatives missing"
            check.update(trigger_rule=triggers[0]["rule"]["id"],
                         structured_metadata_type=("OTLP bytesValue containing MessagePack" if language == "python"
                                                   else "OTLP stringValue containing legacy _dd.appsec.json"),
                         sampling_priority=2, decision_maker=attrs.get("_dd.p.dm"), trace_source=attrs.get("_dd.p.ts"),
                         api_schema_attributes=schema_keys)
            if language == "python":
                check["trigger_span_id_matches"] = True
        else:
            assert not events, events
            for span in spans:
                assert not any(key == "appsec" or key.startswith(("appsec.", "_dd.appsec."))
                               for key in attributes(span.get("attributes", []))), span
            check["ordinary_traces_continue"] = True
        results.append(check)

    try:
        exercise(True, endpoint, "SDK -> local OTLP capture")
        exercise(False, endpoint, "SDK -> local OTLP capture")
        if args.java_agent:
            requests.clear()
            java = str(Path(args.java_home) / "bin/java") if args.java_home else "java"
            javac = str(Path(args.java_home) / "bin/javac") if args.java_home else "javac"
            env = dict(environment, DD_TRACE_OTEL_ENABLED="true", DD_TRACE_ENABLED="true",
                       DD_TRACE_AGENT_URL=endpoint,
                       DD_REMOTE_CONFIGURATION_ENABLED="false", DD_INSTRUMENTATION_TELEMETRY_ENABLED="false",
                       DD_DYNAMIC_INSTRUMENTATION_ENABLED="false", DD_CODE_ORIGIN_FOR_SPANS_ENABLED="false",
                       DD_RUNTIME_METRICS_ENABLED="false", DD_PROFILING_ENABLED="false",
                       DD_TRACE_STARTUP_LOGS="false", OTEL_TRACES_EXPORTER="otlp",
                       OTEL_EXPORTER_OTLP_TRACES_PROTOCOL="http/json",
                       OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=endpoint + "/v1/traces")
            with tempfile.TemporaryDirectory(prefix="ddot-appsec-java-") as classes:
                subprocess.run([javac, "-cp", args.java_agent, "-d", classes, str(root / "JavaStructuredProbe.java")], check=True)
                child = subprocess.run([java, "-javaagent:" + args.java_agent,
                                        "-cp", classes + os.pathsep + args.java_agent, "JavaStructuredProbe"],
                                       env=env, text=True, capture_output=True, timeout=30)
            assert child.returncode == 0 and "STRUCTURED_METADATA_SET" in child.stdout, child.stderr
            native_probes = [request for request in requests if request["path"] == "/v0.4/traces"]
            assert all(request["body"] == [[], []] for request in native_probes), native_probes
            assert all(request["path"] in ("/v1/traces", "/v0.4/traces") for request in requests), [
                {key: request[key] for key in ("path", "content_type")} for request in requests]
            selected = [span for request in requests if request["path"] == "/v1/traces"
                        for resource in request["body"]["resourceSpans"]
                        for scope in resource["scopeSpans"] for span in scope["spans"]
                        if span["name"] == "appsec-structured-contract"]
            assert len(selected) == 1, selected
            attrs = attributes(selected[0]["attributes"])
            assert attrs["poc.scalar"] == "retained", attrs
            assert "poc.structured" not in attrs, "The Java gap is fixed; update the blocked status and assertions"
            results.append({"language": "java", "scope": "synthetic real SDK encoder regression probe, no WAF event",
                            "structured_metadata_set_on_span": True, "structured_metadata_exported": False,
                            "scalar_exported": True, "status": "SDK structured metadata gap reproduced",
                            "native_empty_capability_probes": len(native_probes), "native_ordinary_spans": 0,
                            "java_runtime": subprocess.check_output([java, "-version"], stderr=subprocess.STDOUT, text=True).strip()})
        with socket.socket() as port:
            port.bind(("127.0.0.1", 0))
            ingress_port = port.getsockname()[1]
        with tempfile.TemporaryDirectory(prefix="ddot-appsec-otlp-") as temporary:
            config = Path(temporary) / "collector.yaml"
            config.write_text(f"""receivers:
  otlp:
    protocols:
      http:
        endpoint: 127.0.0.1:{ingress_port}
processors:
  batch:
    timeout: 100ms
exporters:
  otlp_http:
    endpoint: {endpoint}
    encoding: json
    compression: none
    sending_queue:
      enabled: false
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp_http]
""")
            with (Path(temporary) / "collector.log").open("w+") as log:
                collector = subprocess.Popen([args.collector, "--config", str(config)], stdout=log, stderr=log)
                try:
                    deadline = time.monotonic() + 15
                    while time.monotonic() < deadline:
                        try:
                            with socket.create_connection(("127.0.0.1", ingress_port), timeout=0.2):
                                break
                        except OSError:
                            if collector.poll() is not None:
                                log.seek(0)
                                raise RuntimeError(log.read())
                            time.sleep(0.05)
                    else:
                        raise RuntimeError("Collector did not start")
                    collector_endpoint = f"http://127.0.0.1:{ingress_port}"
                    exercise(True, collector_endpoint, "SDK -> actual otlp receiver -> batch -> otlp_http exporter -> capture")
                    exercise(False, collector_endpoint, "SDK -> actual otlp receiver -> batch -> otlp_http exporter -> capture")
                    if args.node_image:
                        exercise(True, collector_endpoint, "SDK -> actual otlp receiver -> batch -> otlp_http exporter -> capture", "javascript")
                        exercise(False, collector_endpoint, "SDK -> actual otlp receiver -> batch -> otlp_http exporter -> capture", "javascript")
                finally:
                    collector.terminate()
                    collector.wait(timeout=15)
    finally:
        server.shutdown()
        server.server_close()
    result = {"timestamp": datetime.now(timezone.utc).isoformat(),
              "scope": "Real SDK/WAF and actual Collector service with local wire assertions",
              "backend_verified": False,
              "runtime_versions": {name: importlib.metadata.version(name) for name in ("ddtrace", "Flask", "msgpack")},
              "collector": subprocess.check_output([args.collector, "--version"], text=True).strip(),
              "checks": results}
    rendered = json.dumps(result, indent=2) + "\n"
    if args.output:
        Path(args.output).write_text(rendered)
    print(rendered)


if __name__ == "__main__":
    main()
