# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Actual SDK/application -> actual Collector tests with explicit local capture destinations."""
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


def read_body(handler):
    if handler.headers.get("Transfer-Encoding", "").lower() != "chunked":
        return handler.rfile.read(int(handler.headers.get("Content-Length", "0")))
    chunks = []
    while True:
        length = int(handler.rfile.readline().split(b";", 1)[0], 16)
        if not length:
            while handler.rfile.readline() not in (b"\r\n", b"\n", b""):
                pass
            return b"".join(chunks)
        chunks.append(handler.rfile.read(length))
        assert handler.rfile.read(2) == b"\r\n"


def free_port():
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        return reservation.getsockname()[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--collector", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--spark-runtime")
    parser.add_argument("--java-agent", default="/tmp/ddot-research-java-agent-1.66.0.jar")
    parser.add_argument("--java-home", default="/usr/local/sdkman/candidates/java/current")
    parser.add_argument("--openlineage-jar")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    records = []

    class Capture(BaseHTTPRequestHandler):
        def do_POST(self):
            wire = read_body(self)
            body = gzip.decompress(wire) if self.headers.get("Content-Encoding") == "gzip" else wire
            records.append({"path": self.path, "headers": {key.lower(): value for key, value in self.headers.items()}, "body": body})
            response = b"{}"
            self.send_response(202 if self.path.startswith("/api/v1/lineage") else 200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(response)

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Capture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    backend = f"http://127.0.0.1:{server.server_port}"
    results = []

    def run(name, sdk_enabled=True, proxy_enabled=True, spark=False):
        records.clear()
        proxy_port, otlp_port = free_port(), free_port()
        proxy, otlp = f"http://127.0.0.1:{proxy_port}", f"http://127.0.0.1:{otlp_port}"
        config = {
            "extensions": {"http_forwarder": {"ingress": {"endpoint": f"127.0.0.1:{proxy_port}", "compression_algorithms": []},
                "egress": {"endpoint": backend}, "routes": [
                    {"path": "/info", "method": "GET", "response": {"status": 200, "headers": {"Content-Type": "application/json"},
                        "body": json.dumps({"endpoints": ["/openlineage/api/v1/lineage"] if proxy_enabled else [], "long_running_spans": False})}},
                    {"path": "/openlineage/api/v1/lineage", "method": "POST", "disabled": not proxy_enabled,
                        "endpoint": backend + "/api/v1/lineage?api-version=2", "headers": {"Authorization": "Bearer local-test-key", "X-Datadog-Additional-Tags": "host:fixture,default_env:ddot-poc"},
                        "remove_headers": ["DD-API-KEY"]}]}},
            "receivers": {"otlp": {"protocols": {"http": {"endpoint": f"127.0.0.1:{otlp_port}"}}}},
            "processors": {"batch": {"timeout": "100ms"}},
            "exporters": {"otlphttp": {"endpoint": backend, "encoding": "json"}},
            "service": {"extensions": ["http_forwarder"], "pipelines": {"traces": {"receivers": ["otlp"], "processors": ["batch"], "exporters": ["otlphttp"]}}},
        }
        with tempfile.TemporaryDirectory(prefix="ddot-djm-test-") as temporary:
            config_path = Path(temporary) / "collector.yaml"
            config_path.write_text(json.dumps(config))
            with (Path(temporary) / "collector.log").open("w+") as log:
                collector = subprocess.Popen([args.collector, "--config", str(config_path)], stdout=log, stderr=log)
                try:
                    deadline = time.monotonic() + 10
                    while True:
                        if collector.poll() is not None:
                            log.seek(0)
                            raise RuntimeError(log.read())
                        try:
                            with socket.create_connection(("127.0.0.1", proxy_port), timeout=0.1):
                                break
                        except OSError:
                            if time.monotonic() > deadline:
                                raise
                            time.sleep(0.05)
                    if spark:
                        environment = {key: value for key, value in os.environ.items() if not key.startswith(("DD_", "_DD_", "OTEL_", "PYSPARK_"))}
                        for key in ("JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS"):
                            environment.pop(key, None)
                        environment.update({"PYTHONPATH": args.spark_runtime, "PYTHONDONTWRITEBYTECODE": "1", "JAVA_HOME": args.java_home,
                            "SPARK_HOME": str(Path(args.spark_runtime) / "pyspark"), "SPARK_LOCAL_IP": "127.0.0.1",
                            "PYSPARK_PYTHON": sys.executable, "DD_DATA_JOBS_ENABLED": str(sdk_enabled).lower(),
                            "DD_DATA_JOBS_OPENLINEAGE_ENABLED": str(bool(args.openlineage_jar)).lower(),
                            "DD_SERVICE": "ddot-djm-spark", "DD_ENV": "ddot-poc", "DD_TRACE_ENABLED": "true", "DD_TRACE_OTEL_ENABLED": "true",
                            "OTEL_TRACES_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/json",
                            "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": otlp + "/v1/traces", "DD_TRACE_AGENT_URL": proxy,
                            "DD_AGENT_HOST": "127.0.0.1", "DD_TRACE_AGENT_PORT": str(proxy_port),
                            "DD_REMOTE_CONFIG_ENABLED": "false", "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false",
                            "DD_RUNTIME_METRICS_ENABLED": "false", "DD_PROFILING_ENABLED": "false", "DD_CODE_ORIGIN_FOR_SPANS_ENABLED": "false"})
                        command = [str(Path(args.spark_runtime) / "pyspark/bin/spark-submit"), "--master", "local[2]",
                            "--driver-memory", "512m", "--conf", "spark.driver.extraJavaOptions=--add-opens=java.base/java.security=ALL-UNNAMED -javaagent:" + args.java_agent]
                        if args.openlineage_jar:
                            command.extend(["--jars", args.openlineage_jar])
                        command.append(str(root / "spark_app.py"))
                        child = subprocess.run(command, env=environment, cwd=temporary, capture_output=True, text=True, timeout=100)
                    else:
                        child = subprocess.run(["docker", "run", "--rm", "--network=host", "-e", "OPENLINEAGE_URL=" + proxy,
                            "-e", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=" + otlp + "/v1/traces",
                            "-e", "DJM_LINEAGE_ENABLED=" + str(sdk_enabled).lower(), "-e", "ALLOW_LINEAGE_FAILURE=true", "ddot-djm-python:poc"],
                            capture_output=True, text=True, timeout=30)
                    assert child.returncode == 0, child.stdout + child.stderr[-12000:]
                finally:
                    collector.terminate()
                    collector.wait(timeout=10)
        lineage, spans = [], []
        for record in records:
            if record["path"].startswith("/api/v1/lineage"):
                assert record["path"] == "/api/v1/lineage?api-version=2"
                assert record["headers"]["authorization"] == "Bearer local-test-key"
                assert record["headers"]["content-encoding"] == "gzip"
                lineage.append(json.loads(record["body"]))
            elif record["path"] == "/v1/traces":
                for resource in json.loads(record["body"])["resourceSpans"]:
                    for scope in resource["scopeSpans"]:
                        spans.extend(scope["spans"])
        spark_spans = []
        for span in spans:
            attributes = {attribute["key"]: attribute["value"] for attribute in span.get("attributes", [])}
            if attributes.get("span.type", {}).get("stringValue") == "spark":
                spark_spans.append({"name": span["name"], "attribute_keys": sorted(attributes),
                    "djm_enabled": attributes.get("_dd.djm.enabled"), "trace_id": span["traceId"]})
        if spark:
            assert bool(spark_spans) == sdk_enabled, (len(spans), child.stderr[-6000:])
            if sdk_enabled:
                assert any(span["djm_enabled"] for span in spark_spans)
                if args.openlineage_jar:
                    assert len(lineage) >= 2, child.stderr[-6000:]
                    assert "_dd.ol_intake.emit_spans" in json.dumps(lineage)
            else:
                assert not lineage
        else:
            assert len(spans) == 1, spans
            expected = sdk_enabled and proxy_enabled
            assert len(lineage) == (2 if expected else 0), lineage
            if expected:
                assert [event["eventType"] for event in lineage] == ["START", "COMPLETE"]
                assert lineage[0]["run"]["runId"] == lineage[1]["run"]["runId"]
                assert lineage[0]["inputs"][0]["name"] == "orders.csv"
            else:
                result = json.loads(child.stdout.strip().splitlines()[-1])
                assert result["lineage_statuses"] == ([404, 404] if sdk_enabled else [])
        result = {"case": name, "lineage_requests": len(lineage), "otlp_spans": len(spans), "spark_spans": spark_spans,
            "lineage_event_types": [event["eventType"] for event in lineage], "stdout": child.stdout.splitlines(),
            "lineage_run_facet_keys": sorted({key for event in lineage for key in event["run"].get("facets", {})}),
            "sdk_enabled": sdk_enabled, "proxy_enabled": proxy_enabled, "native_spark_runtime": spark}
        results.append(result)
        print(json.dumps({key: result[key] for key in ("case", "lineage_requests", "otlp_spans")}), flush=True)

    try:
        if args.spark_runtime:
            run("native-spark", spark=True)
            run("native-spark-disabled", sdk_enabled=False, spark=True)
        else:
            run("openlineage-enabled")
            run("openlineage-sdk-disabled", sdk_enabled=False)
            run("openlineage-proxy-disabled", proxy_enabled=False)
    finally:
        server.shutdown()
        server.server_close()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps({"timestamp": datetime.now(timezone.utc).isoformat(),
        "scope": "actual SDK workloads through actual Collector, local fixture only", "backend_verified": False,
        "checks": results}, indent=2) + "\n")


if __name__ == "__main__":
    main()
