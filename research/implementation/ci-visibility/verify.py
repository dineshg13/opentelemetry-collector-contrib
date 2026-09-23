# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Actual SDK and Collector tests against a strict, test-only loopback endpoint."""
import argparse
from datetime import datetime, timezone
from email.parser import BytesParser
from email.policy import default
import gzip
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import http.client
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
from urllib.parse import urlsplit

import msgpack
import yaml
from configure import ROUTES, configuration


def free_port():
    with socket.socket() as port:
        port.bind(("127.0.0.1", 0))
        return port.getsockname()[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--collector", required=True)
    parser.add_argument("--python-runtime", required=True)
    parser.add_argument("--java-agent")
    parser.add_argument("--java-home", default=os.environ.get("JAVA_HOME", ""))
    parser.add_argument("--output")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    captures = []

    class Backend(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
                chunks = []
                while True:
                    size = int(self.rfile.readline().split(b";", 1)[0], 16)
                    if not size:
                        self.rfile.readline()
                        break
                    chunks.append(self.rfile.read(size))
                    self.rfile.read(2)
                raw = b"".join(chunks)
            else:
                raw = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            body = gzip.decompress(raw) if self.headers.get("Content-Encoding") == "gzip" else raw
            captures.append({"path": self.path, "headers": dict(self.headers), "body": body})
            path = urlsplit(self.path).path
            if path not in ROUTES and path != "/v1/traces":
                return self.reply(404, {"error": "Not a raw backend route"})
            if self.headers.get("DD-API-KEY") != "local-test-key":
                return self.reply(403, {"error": "Wrong server credential"})
            if "force_status=429" in self.path:
                return self.reply(429, {"error": "fixture throttle"}, {"Retry-After": "7"})
            result = {}
            if path.endswith("/setting"):
                result = {"data": {"attributes": {
                    "code_coverage": True, "tests_skipping": True, "require_git": False,
                    "itr_enabled": True, "flaky_test_retries_enabled": False,
                    "known_tests_enabled": True, "early_flake_detection": {"enabled": False},
                    "test_management": {"enabled": True}, "coverage_report_upload_enabled": False}}}
            elif path.endswith("/skippable"):
                result = {"data": [], "meta": {"correlation_id": "local-skip-id"}}
            elif path.endswith("/ci/libraries/tests"):
                result = {"data": {"attributes": {"tests": {}}}}
            elif path.endswith("/test-management/tests"):
                result = {"data": {"attributes": {"modules": {}}}}
            elif path.endswith("search_commits"):
                result = {"data": []}
            self.reply(200, result)

        do_PUT = do_POST

        def reply(self, code, data, headers=None):
            body = json.dumps(data).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            for key, value in (headers or {}).items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(body)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Backend)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    results = []

    def run_case(proxy_enabled, sdk_enabled):
        captures.clear()
        product_port, otlp_port = free_port(), free_port()
        with tempfile.TemporaryDirectory(prefix="ddot-ci-sdk-") as temporary:
            directory = Path(temporary)
            for filename in ("run.py", "test_workload.py"):
                shutil.copy2(root / filename, directory / filename)
            for command in (["git", "init", "-q"], ["git", "add", "run.py", "test_workload.py"],
                            ["git", "-c", "commit.gpgsign=false", "-c", "user.name=DDOT test",
                             "-c", "user.email=ddot@example.invalid", "commit", "-qm", "CI test fixture"],
                            ["git", "remote", "add", "origin", "https://example.invalid/ddot-ci-fixture.git"]):
                subprocess.run(command, cwd=directory, check=True, capture_output=True)
            config = configuration(proxy_enabled)
            forwarder = config["extensions"]["http_forwarder"]
            forwarder["ingress"]["endpoint"] = f"127.0.0.1:{product_port}"
            forwarder["egress"]["endpoint"] = f"http://127.0.0.1:{server.server_port}"
            for route in forwarder["routes"]:
                if "endpoint" in route:
                    route["endpoint"] = f"http://127.0.0.1:{server.server_port}" + urlsplit(route["endpoint"]).path
                    route["headers"]["DD-API-KEY"] = "local-test-key"
                    route["headers"]["X-Datadog-Hostname"] = "local-collector"
            config["receivers"]["otlp"]["protocols"]["http"]["endpoint"] = f"127.0.0.1:{otlp_port}"
            config["processors"]["batch"] = {"timeout": "100ms"}
            config["exporters"]["otlp_http"] = {
                "endpoint": f"http://127.0.0.1:{server.server_port}", "encoding": "json",
                "headers": {"dd-api-key": "local-test-key"}, "sending_queue": {"enabled": False}}
            config_path = directory / "collector.yaml"
            config_path.write_text(yaml.safe_dump(config))
            with (directory / "collector.log").open("w+") as log:
                collector = subprocess.Popen([args.collector, "--config", str(config_path)], stdout=log, stderr=log)
                try:
                    deadline = time.monotonic() + 15
                    while time.monotonic() < deadline:
                        try:
                            with socket.create_connection(("127.0.0.1", product_port), timeout=0.2):
                                break
                        except OSError:
                            if collector.poll() is not None:
                                log.seek(0)
                                raise RuntimeError(log.read())
                            time.sleep(0.05)
                    else:
                        raise RuntimeError("Collector failed to start")
                    env = {key: value for key, value in os.environ.items() if not key.startswith(("DD_", "_DD_", "OTEL_"))}
                    env.update(PYTHONPATH=args.python_runtime, PYTHONDONTWRITEBYTECODE="1",
                               DD_TRACE_AGENT_URL=f"http://127.0.0.1:{product_port}",
                               OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=f"http://127.0.0.1:{otlp_port}/v1/traces",
                               DD_CIVISIBILITY_ENABLED=str(sdk_enabled).lower(), DD_CIVISIBILITY_ITR_ENABLED="true")
                    child = subprocess.run([sys.executable, str(directory / "run.py")], env=env, cwd=directory,
                                           text=True, capture_output=True, timeout=90)
                    assert child.returncode == 0, child.stdout + child.stderr
                    time.sleep(0.3)
                    events, ordinary = [], []
                    coverages = 0
                    for capture in captures:
                        path = urlsplit(capture["path"]).path
                        assert not path.startswith("/v0."), "Ordinary native trace fallback"
                        if path == "/api/v2/citestcycle":
                            events.extend(msgpack.unpackb(capture["body"], raw=False, strict_map_key=False)["events"])
                        elif path == "/api/v2/citestcov":
                            header = next(value for key, value in capture["headers"].items() if key.lower() == "content-type")
                            message = BytesParser(policy=default).parsebytes(b"Content-Type: " + header.encode() + b"\r\n\r\n" + capture["body"])
                            for part in message.iter_parts():
                                if (part.get_filename() or "").endswith(".msgpack"):
                                    coverages += len(msgpack.unpackb(part.get_payload(decode=True), raw=False, strict_map_key=False)["coverages"])
                        elif path == "/v1/traces":
                            for resource in json.loads(capture["body"])["resourceSpans"]:
                                for scope in resource["scopeSpans"]:
                                    ordinary.extend(span for span in scope["spans"] if span["name"] == "poc.ci.ordinary-operation")
                    assert ordinary, ("Ordinary SDK span did not reach the standard OTLP pipeline",
                                      [capture["path"] for capture in captures], child.stdout, child.stderr)
                    paths = sorted({urlsplit(capture["path"]).path for capture in captures})
                    if proxy_enabled and sdk_enabled:
                        types = {event["type"] for event in events}
                        assert {"test", "test_session_end", "test_module_end", "test_suite_end"} <= types, types
                        session_id = next(event["content"]["test_session_id"] for event in events if event["type"] == "test_session_end")
                        module_id = next(event["content"]["test_module_id"] for event in events if event["type"] == "test_module_end")
                        suite_id = next(event["content"]["test_suite_id"] for event in events if event["type"] == "test_suite_end")
                        for event in events:
                            assert event["content"]["test_session_id"] == session_id
                            if event["type"] == "test":
                                assert event["content"]["test_module_id"] == module_id
                                assert event["content"]["test_suite_id"] == suite_id
                        statuses = {event["content"]["meta"].get("test.status") for event in events if event["type"] == "test"}
                        assert {"pass", "skip"} <= statuses, statuses
                        assert coverages > 0, "No real SDK coverage"
                        assert "/api/v2/libraries/tests/services/setting" in paths, paths
                        for capture in captures:
                            if capture["path"] != "/v1/traces":
                                headers = {key.lower(): value for key, value in capture["headers"].items()}
                                assert headers["x-datadog-hostname"] == "local-collector"
                                assert "x-datadog-evp-subdomain" not in headers
                    else:
                        assert not events and not coverages, paths
                    result = {"proxy_enabled": proxy_enabled, "sdk_enabled": sdk_enabled,
                              "test_events": len(events), "coverage_entries": coverages,
                              "ordinary_otlp_spans": len(ordinary), "backend_paths": paths,
                              "hierarchy_validated": bool(events),
                              "pytest_summary": child.stdout.strip().splitlines()[-1]}
                    if proxy_enabled:
                        connection = http.client.HTTPConnection("127.0.0.1", product_port, timeout=10)
                        connection.request("POST", "/evp_proxy/v4/api/v2/libraries/tests/services/setting?force_status=429", b"{}",
                                           {"X-Datadog-EVP-Subdomain": "api", "Content-Type": "application/json"})
                        response = connection.getresponse()
                        assert response.status == 429 and response.getheader("Retry-After") == "7"
                        response.read(); connection.close()
                        result["control_error_response_preserved"] = True
                    results.append(result)
                    if proxy_enabled and sdk_enabled and args.java_agent:
                        captures.clear()
                        java = str(Path(args.java_home) / "bin/java") if args.java_home else "java"
                        javac = str(Path(args.java_home) / "bin/javac") if args.java_home else "javac"
                        subprocess.run([javac, "-cp", args.java_agent, "-d", temporary, str(root / "CiManual.java")], check=True)
                        java_env = dict(env, DD_CIVISIBILITY_ENABLED="true", DD_TRACE_OTEL_ENABLED="true",
                                        OTEL_TRACES_EXPORTER="otlp", OTEL_EXPORTER_OTLP_TRACES_PROTOCOL="http/json",
                                        DD_REMOTE_CONFIGURATION_ENABLED="false", DD_INSTRUMENTATION_TELEMETRY_ENABLED="false",
                                        DD_DYNAMIC_INSTRUMENTATION_ENABLED="false", DD_CODE_ORIGIN_FOR_SPANS_ENABLED="false",
                                        DD_PROFILING_ENABLED="false", DD_RUNTIME_METRICS_ENABLED="false")
                        child = subprocess.run([java, "-javaagent:" + args.java_agent,
                                                "-cp", temporary + os.pathsep + args.java_agent, "CiManual"],
                                               cwd=directory, env=java_env, text=True, capture_output=True, timeout=60)
                        assert child.returncode == 0, child.stderr
                        time.sleep(0.3)
                        java_paths = sorted({capture["path"] for capture in captures})
                        java_spans = [span for capture in captures if capture["path"] == "/v1/traces"
                                      for resource in json.loads(capture["body"])["resourceSpans"]
                                      for scope in resource["scopeSpans"] for span in scope["spans"]]
                        assert java_spans, (java_paths, child.stdout, child.stderr)
                        assert "/api/v2/citestcycle" not in java_paths, "CI writer precedence is fixed; update SDK blocker status"
                        results.append({"language": "java", "scope": "Real manual CI SDK API with OTLP configured",
                                        "status": "SDK CI events bypassed by OtlpWriter selection",
                                        "otlp_spans": len(java_spans), "native_test_event_envelopes": 0,
                                        "backend_paths": java_paths,
                                        "java_runtime": subprocess.check_output([java, "-version"], stderr=subprocess.STDOUT, text=True).strip()})
                finally:
                    collector.terminate()
                    collector.wait(timeout=15)

    try:
        run_case(True, True)
        run_case(False, True)
        run_case(True, False)
    finally:
        server.shutdown(); server.server_close()
    result = {"timestamp": datetime.now(timezone.utc).isoformat(), "backend_verified": False,
              "scope": "Actual Python SDK + actual generic HTTP forwarder and OTLP Collector pipelines; test-only fixture responses",
              "checks": results}
    rendered = json.dumps(result, indent=2) + "\n"
    if args.output:
        Path(args.output).write_text(rendered)
    print(rendered)


if __name__ == "__main__":
    main()
