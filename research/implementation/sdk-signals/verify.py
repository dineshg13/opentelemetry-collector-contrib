# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real SDKs -> actual Collector OTLP pipelines -> semantic wire assertions."""
import argparse
from datetime import datetime, timezone
import gzip
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


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def attrs(values):
    return {item["key"]: next(iter(item["value"].values())) for item in values}


def environment(language, endpoint):
    env = {key: value for key, value in os.environ.items() if not key.startswith(("DD_", "_DD_", "OTEL_"))
           and key not in ("JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS")}
    env.update(DD_SERVICE="ddot-signals-" + language, DD_ENV="ddot-poc", DD_VERSION="1",
               DD_TRACE_ENABLED="true", DD_TRACE_OTEL_ENABLED="true", DD_LOGS_OTEL_ENABLED="true",
               DD_METRICS_OTEL_ENABLED="true", DD_REMOTE_CONFIGURATION_ENABLED="false",
               DD_INSTRUMENTATION_TELEMETRY_ENABLED="false", DD_TRACE_STARTUP_LOGS="false",
               DD_DYNAMIC_INSTRUMENTATION_ENABLED="false", DD_CODE_ORIGIN_FOR_SPANS_ENABLED="false",
               DD_PROFILING_ENABLED="false", DD_RUNTIME_METRICS_ENABLED="false", DD_DATA_STREAMS_ENABLED="false",
               DD_TRACE_METRICS_ENABLED="false",
               DD_LLMOBS_ENABLED="false", DD_APPSEC_ENABLED="false", DD_IAST_ENABLED="false",
               OTEL_TRACES_EXPORTER="otlp", OTEL_LOGS_EXPORTER="otlp", OTEL_METRICS_EXPORTER="otlp",
               OTEL_BSP_SCHEDULE_DELAY="100", OTEL_BLRP_SCHEDULE_DELAY="100", OTEL_METRIC_EXPORT_INTERVAL="200",
               OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=endpoint + "/v1/traces",
               OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=endpoint + "/v1/logs",
               OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=endpoint + "/v1/metrics",
               OTEL_EXPORTER_OTLP_TRACES_PROTOCOL="http/json",
               OTEL_EXPORTER_OTLP_LOGS_PROTOCOL="http/protobuf" if language in ("python", "javascript") else "http/json",
               OTEL_EXPORTER_OTLP_METRICS_PROTOCOL="http/protobuf" if language == "python" else "http/json")
    if language == "python":
        # These are not accepted aliases in this released Python SDK. The explicit
        # DD logs gate configures the provider; app.py explicitly flushes logs.
        env.pop("OTEL_LOGS_EXPORTER")
        env.pop("OTEL_BSP_SCHEDULE_DELAY")
        env.pop("OTEL_BLRP_SCHEDULE_DELAY")
    return env


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--collector", required=True)
    parser.add_argument("--python-runtime", required=True)
    parser.add_argument("--node-runtime", required=True)
    parser.add_argument("--java-agent", required=True)
    parser.add_argument("--java-api-dir", required=True)
    parser.add_argument("--java-home", default=os.environ.get("JAVA_HOME", ""))
    parser.add_argument("--output", required=True)
    parser.add_argument("--javascript-logs-protocol", choices=("http/protobuf", "http/json"), default="http/protobuf")
    parser.add_argument("--disable-logs-metrics", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    captures = []

    class Capture(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            if self.headers.get("Content-Encoding") == "gzip":
                body = gzip.decompress(body)
            captures.append({"path": self.path, "body": json.loads(body)})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"{}")

    server = ThreadingHTTPServer(("127.0.0.1", 0), Capture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    ingress = free_port()
    results = []
    with tempfile.TemporaryDirectory(prefix="ddot-sdk-signals-") as temporary:
        directory = Path(temporary)
        config = directory / "collector.yaml"
        config.write_text(f"""receivers:
  otlp:
    protocols:
      http:
        endpoint: 127.0.0.1:{ingress}
processors:
  batch:
    timeout: 100ms
exporters:
  otlp_http:
    endpoint: http://127.0.0.1:{server.server_port}
    encoding: json
    sending_queue:
      enabled: false
service:
  telemetry:
    metrics:
      level: none
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp_http]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp_http]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp_http]
""")
        java = str(Path(args.java_home) / "bin/java") if args.java_home else "java"
        javac = str(Path(args.java_home) / "bin/javac") if args.java_home else "javac"
        java_classpath = os.pathsep.join(str(path) for path in sorted(Path(args.java_api_dir).glob("*.jar")))
        subprocess.run([javac, "-cp", java_classpath, "-d", temporary, str(root / "Signals.java")], check=True)
        with (directory / "collector.log").open("w+") as log:
            collector = subprocess.Popen([args.collector, "--config", str(config)], stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 15
                while time.monotonic() < deadline:
                    try:
                        with socket.create_connection(("127.0.0.1", ingress), timeout=0.2):
                            break
                    except OSError:
                        if collector.poll() is not None:
                            log.seek(0)
                            raise RuntimeError(log.read())
                        time.sleep(0.05)
                else:
                    raise TimeoutError("Temporary Collector did not become ready")
                for language, command in (
                    ("python", [sys.executable, str(root / "app.py")]),
                    ("javascript", ["node", str(root / "app.cjs")]),
                    ("java", [java, "-javaagent:" + args.java_agent, "-cp", temporary + os.pathsep + java_classpath, "Signals"]),
                ):
                    captures.clear()
                    env = environment(language, f"http://127.0.0.1:{ingress}")
                    if language == "javascript":
                        env["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"] = args.javascript_logs_protocol
                    if args.disable_logs_metrics:
                        env.update(DD_LOGS_OTEL_ENABLED="false", DD_METRICS_OTEL_ENABLED="false")
                    env.update(PYTHONPATH=args.python_runtime, PYTHONDONTWRITEBYTECODE="1",
                               NODE_PATH=str(Path(args.node_runtime) / "node_modules"))
                    child = subprocess.run(command, env=env, text=True, capture_output=True, timeout=45)
                    time.sleep(0.4)
                    spans, logs, points = [], [], []
                    for capture in captures:
                        if capture["path"] == "/v1/traces":
                            for resource in capture["body"]["resourceSpans"]:
                                for scope in resource["scopeSpans"]:
                                    spans.extend(span for span in scope["spans"] if span["name"] == "poc.sdk.operation")
                        if capture["path"] == "/v1/logs":
                            for resource in capture["body"]["resourceLogs"]:
                                for scope in resource["scopeLogs"]:
                                    logs.extend(item for item in scope["logRecords"] if item["body"].get("stringValue") == "ddot-signals-" + language)
                        if capture["path"] == "/v1/metrics":
                            for resource in capture["body"]["resourceMetrics"]:
                                for scope in resource["scopeMetrics"]:
                                    for metric in scope["metrics"]:
                                        if metric["name"] == "poc.sdk.counter":
                                            points.extend(metric["sum"]["dataPoints"])
                    valid_spans = [span for span in spans if attrs(span["attributes"]).get("poc.language") == language]
                    valid_logs = [item for item in logs if attrs(item["attributes"]).get("poc.language") == language]
                    valid_points = [point for point in points if float(point.get("asInt", point.get("asDouble", 0))) == 3
                                    and attrs(point["attributes"]).get("poc.language") == language]
                    result = {"language": language, "exit_code": child.returncode,
                              "trace_spans": len(valid_spans), "log_records": len(valid_logs),
                              "counter_points_with_value_3": len(valid_points),
                              "paths": sorted({capture["path"] for capture in captures}),
                              "stdout": child.stdout.strip(),
                              "stderr_without_startup_inventory": "\n".join(line for line in child.stderr.splitlines()
                                                                             if "DATADOG TRACER CONFIGURATION" not in line),
                              "all_signals_verified": child.returncode == 0 and bool(valid_spans and valid_logs and valid_points)}
                    if valid_spans and valid_logs:
                        result["log_trace_id_matches"] = valid_logs[0].get("traceId") == valid_spans[0]["traceId"]
                        result["log_span_id_matches"] = valid_logs[0].get("spanId") == valid_spans[0]["spanId"]
                        result["all_signals_verified"] &= result["log_trace_id_matches"] and result["log_span_id_matches"]
                    result["expected_signals_verified"] = (child.returncode == 0 and bool(valid_spans) and not logs and not points
                                                           if args.disable_logs_metrics else result["all_signals_verified"])
                    results.append(result)
                    print(json.dumps(result, indent=2), flush=True)
            finally:
                collector.terminate()
                collector.wait(timeout=15)
                server.shutdown(); server.server_close()
    record = {"timestamp": datetime.now(timezone.utc).isoformat(), "backend_verified": False,
              "logs_metrics_enabled": not args.disable_logs_metrics,
              "scope": "Real SDKs through the actual standard OTLP Collector receiver/batch/exporter; local semantic assertions",
              "checks": results}
    Path(args.output).write_text(json.dumps(record, indent=2) + "\n")
    if not all(result["expected_signals_verified"] for result in results):
        raise SystemExit("Some SDK signals failed; inspect recorded runtime evidence")


if __name__ == "__main__":
    main()
