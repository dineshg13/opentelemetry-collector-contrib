#!/usr/bin/env python3
"""Real Python pytest SDK + unmodified Collector forwarder; local fake backend only.

The compatibility gateway is an explicitly incomplete research adapter, NOT the
Datadog extension or Agent. Its presence is measured separately from forwarding.
"""

import argparse
from email.parser import BytesParser
from email.policy import default
import gzip
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

import msgpack


SETTINGS = "/api/v2/libraries/tests/services/setting"
SKIPPABLE = "/api/v2/ci/tests/skippable"
KNOWN = "/api/v2/ci/libraries/tests"
MANAGEMENT = "/api/v2/test/libraries/test-management/tests"
ROUTES = {
    "/api/v2/citestcycle": "citestcycle-intake",
    "/api/v2/citestcov": "citestcov-intake",
    SETTINGS: "api",
    SKIPPABLE: "api",
    KNOWN: "api",
    MANAGEMENT: "api",
    "/api/v2/git/repository/search_commits": "api",
    "/api/v2/git/repository/packfile": "api",
}
ATTRIBUTES = {
    "code_coverage": True, "tests_skipping": True, "require_git": False,
    "itr_enabled": True, "flaky_test_retries_enabled": False,
    "known_tests_enabled": True, "early_flake_detection": {"enabled": False},
    "test_management": {"enabled": True}, "coverage_report_upload_enabled": False,
}


def request(port, path, body=None, headers=None, method="POST"):
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=15)
    conn.request(method, path, body, headers or {})
    response = conn.getresponse()
    result = response.status, dict(response.getheaders()), response.read()
    conn.close()
    return result


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def read_body(self):
        if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
            chunks = []
            while True:
                size = int(self.rfile.readline().split(b";", 1)[0], 16)
                if not size:
                    self.rfile.readline()
                    return b"".join(chunks)
                chunks.append(self.rfile.read(size))
                self.rfile.read(2)
        return self.rfile.read(int(self.headers.get("Content-Length", 0)))

    def reply(self, code, body, headers=None):
        self.send_response(code)
        for key, value in (headers or {}).items():
            if key.lower() not in ("content-length", "transfer-encoding", "connection"):
                self.send_header(key, value)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.do_POST()


class Backend(Handler):
    """Strict raw Datadog endpoint fixture: no /info and no EVP prefix."""

    def do_POST(self):
        body = self.read_body()
        self.server.records.append((self.path, dict(self.headers), body))
        path = urlsplit(self.path).path
        if path not in ROUTES:
            return self.reply(404, b'{"error":"not a raw intake route"}')
        if self.headers.get("DD-API-KEY") != "research-server-key":
            return self.reply(403, b'{"error":"wrong key"}')
        if self.headers.get("X-Research-Subdomain") != ROUTES[path]:
            return self.reply(400, b'{"error":"wrong destination"}')
        if "throttle=1" in self.path:
            return self.reply(429, b'{"error":"fixture throttle"}', {"Retry-After": "7"})
        if path == SETTINGS:
            result = {"data": {"attributes": ATTRIBUTES}}
        elif path == SKIPPABLE:
            result = {"data": [], "meta": {"correlation_id": "research-skip-id"}}
        elif path == KNOWN:
            result = {"data": {"attributes": {"tests": {}}}}
        elif path == MANAGEMENT:
            result = {"data": {"attributes": {"modules": {}}}}
        elif path.endswith("search_commits"):
            # Force the real SDK to create and upload a packfile from the fixture repo.
            result = {"data": []}
        else:
            result = {"ok": True}
        self.reply(200, json.dumps(result).encode(), {"Content-Type": "application/json"})


class Gateway(Handler):
    """Minimal CI-only capability/rewrite/auth adapter, not production code."""

    def do_POST(self):
        body = self.read_body()
        self.server.records.append((self.path, dict(self.headers), body))
        if self.path == "/info":
            info = {"endpoints": ["/evp_proxy/v2/", "/evp_proxy/v4/"] if self.server.enabled else [],
                    "version": "7.99.0", "config": {"default_env": "research"}}
            return self.reply(200, json.dumps(info).encode(), {"Content-Type": "application/json"})
        if not self.server.enabled:
            return self.reply(404, b'{"error":"CI disabled"}')
        parts = self.path.split("/", 3)
        if len(parts) != 4 or parts[1] != "evp_proxy" or parts[2] not in ("v2", "v4"):
            return self.reply(404, b'{"error":"unsupported route"}')
        path = "/" + parts[3]
        subdomain = ROUTES.get(urlsplit(path).path)
        if not subdomain or self.headers.get("X-Datadog-EVP-Subdomain") != subdomain:
            return self.reply(404, b'{"error":"disabled product or wrong route"}')
        headers = {key: value for key, value in self.headers.items() if key.lower() in (
            "content-type", "content-encoding", "accept-encoding", "user-agent",
            "dd-ci-provider-name", "dd-evp-origin", "dd-evp-origin-version")}
        headers.update({"DD-API-KEY": "research-server-key", "X-Datadog-Hostname": "research-host",
                        "X-Datadog-AgentDefaultEnv": "research", "X-Research-Subdomain": subdomain})
        code, response_headers, response_body = request(self.server.backend, path, body, headers, self.command)
        self.reply(code, response_body, response_headers)


def serve(handler, **attrs):
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    server.records = []
    for key, value in attrs.items():
        setattr(server, key, value)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


def launch_forwarder(binary, target, directory):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    config = directory / ("forwarder-%s.yaml" % port)
    config.write_text("ingress:\n  endpoint: 127.0.0.1:%s\n  compression_algorithms: []\n"
                      "egress:\n  endpoint: http://127.0.0.1:%s\n  timeout: 15s\n" % (port, target))
    process = subprocess.Popen([binary, "-config", str(config)], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    ready = process.stdout.readline()
    assert ready.startswith(b"READY"), ready
    return process, port


def run_sdk(port, directory, pythonpath, plugin="legacy", allow_failure=False):
    fixture = directory / "test_ci.py"
    fixture.write_text("def test_research_addition():\n    assert 1 + 1 == 2\n")
    if not (directory / ".git").exists():
        for command in (["git", "init", "-q"], ["git", "add", fixture.name],
                        ["git", "-c", "commit.gpgsign=false", "-c", "user.name=Research",
                         "-c", "user.email=research@example.invalid", "commit", "-qm", "CI fixture"],
                        ["git", "remote", "add", "origin", "https://example.invalid/research.git"]):
            subprocess.run(command, cwd=directory, check=True, capture_output=True)
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=directory, text=True).strip()
    env = {key: value for key, value in os.environ.items() if not key.startswith(("DD_", "_DD_", "OTEL_"))}
    env.update({"PYTHONPATH": pythonpath, "PYTEST_DISABLE_PLUGIN_AUTOLOAD": "1",
                "DD_TRACE_AGENT_URL": "http://127.0.0.1:%s" % port,
                "DD_SERVICE": "research-ci", "DD_ENV": "research", "DD_VERSION": "fixture",
                "DD_CIVISIBILITY_ITR_ENABLED": "true", "DD_CIVISIBILITY_AGENTLESS_ENABLED": "false",
                "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false", "DD_REMOTE_CONFIGURATION_ENABLED": "false",
                "DD_GIT_REPOSITORY_URL": "https://example.invalid/research.git",
                "DD_GIT_COMMIT_SHA": commit, "DD_GIT_BRANCH": "research"})
    module = "ddtrace.contrib.internal.pytest.plugin" if plugin == "legacy" else "ddtrace.testing.internal.pytest.entry_point"
    result = subprocess.run([sys.executable, "-m", "pytest", "-p", module,
                             "--ddtrace", "-q", str(fixture)], cwd=directory, env=env,
                            capture_output=True, text=True, timeout=90)
    if not allow_failure:
        assert result.returncode == 0, result.stdout + result.stderr
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--forwarder", default="/tmp/ddot-research-forwarder")
    parser.add_argument("--sdk-path", default="/tmp/ddot-research-python-runtime")
    parser.add_argument("--pytest-path", default="/tmp/ddot-research-ci-python")
    parser.add_argument("--java-agent", help="Optional released SDK jar for the real manual CI API fixture")
    args = parser.parse_args()
    pythonpath = os.pathsep.join((args.sdk_path, args.pytest_path))
    backend = serve(Backend)
    gateway = serve(Gateway, backend=backend.server_port, enabled=True)
    processes = []
    try:
        with tempfile.TemporaryDirectory(prefix="ci-forwarder-") as temp:
            directory = Path(temp)
            direct, direct_port = launch_forwarder(args.forwarder, backend.server_port, directory)
            processes.append(direct)
            run_sdk(direct_port, directory, pythonpath)
            raw_paths = sorted({record[0] for record in backend.records})
            assert "/info" in raw_paths and any(path.startswith("/v0.") for path in raw_paths), raw_paths
            backend.records.clear()
            direct_new = run_sdk(direct_port, directory, pythonpath, plugin="default", allow_failure=True)
            raw_new_paths = sorted({record[0] for record in backend.records})
            assert "/info" in raw_new_paths and not any("citest" in path for path in raw_new_paths), raw_new_paths
            direct_status = request(direct_port, "/evp_proxy/v2/api/v2/citestcycle", b"fixture")[0]
            assert direct_status == 404
            backend.records.clear()
            adapted, adapted_port = launch_forwarder(args.forwarder, gateway.server_port, directory)
            processes.append(adapted)
            sdk_result = run_sdk(adapted_port, directory, pythonpath)
            records = list(backend.records)
            paths = sorted({record[0] for record in records})
            assert SETTINGS in paths, (paths, sdk_result.stderr)
            events = []
            coverages = []
            for path, headers, body in records:
                if path == "/api/v2/citestcycle":
                    if headers.get("Content-Encoding") == "gzip":
                        body = gzip.decompress(body)
                    events.extend(msgpack.unpackb(body, raw=False)["events"])
                elif path == "/api/v2/citestcov":
                    if headers.get("Content-Encoding") == "gzip":
                        body = gzip.decompress(body)
                    message = BytesParser(policy=default).parsebytes(
                        ("Content-Type: " + headers["Content-Type"] + "\r\n\r\n").encode() + body)
                    for part in message.iter_parts():
                        if part.get_content_type() == "application/msgpack":
                            coverages.extend(msgpack.unpackb(part.get_payload(decode=True), raw=False)["coverages"])
            event_types = sorted({event["type"] for event in events})
            assert "test" in event_types, (event_types, sdk_result.stderr)
            assert "/api/v2/citestcov" in paths, (paths, sdk_result.stderr)
            assert coverages and coverages[0]["files"], coverages
            assert "/api/v2/git/repository/search_commits" in paths, (paths, sdk_result.stderr)
            assert "/api/v2/git/repository/packfile" in paths, (paths, sdk_result.stderr)
            assert SKIPPABLE in paths and KNOWN in paths and MANAGEMENT in paths, (paths, sdk_result.stderr)
            backend.records.clear()
            new_result = run_sdk(adapted_port, directory, pythonpath, plugin="default")
            new_paths = sorted({record[0] for record in backend.records})
            assert "/api/v2/citestcycle" in new_paths and SETTINGS in new_paths, (new_paths, new_result.stderr)
            assert all(not path.startswith("/evp_proxy/") for path in new_paths)
            java_result = {"executed": False}
            if args.java_agent:
                backend.records.clear()
                java_source = Path(__file__).resolve().with_name("CiManual.java")
                subprocess.run(["javac", "-cp", args.java_agent, "-d", str(directory), str(java_source)],
                               cwd=directory, check=True, capture_output=True)
                java_env = {key: value for key, value in os.environ.items()
                            if not key.startswith(("DD_", "_DD_", "OTEL_")) and key not in (
                                "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "_JAVA_OPTIONS")}
                java_env.update({"DD_CIVISIBILITY_ENABLED": "true", "DD_CIVISIBILITY_AGENTLESS_ENABLED": "false",
                                 "DD_TRACE_AGENT_URL": "http://127.0.0.1:%s" % adapted_port,
                                 "DD_SERVICE": "research-ci-java", "DD_ENV": "research",
                                 "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false",
                                 "DD_REMOTE_CONFIGURATION_ENABLED": "false", "DD_TRACE_STARTUP_LOGS": "false"})
                java_run = subprocess.run(["java", "-javaagent:" + args.java_agent, "-cp",
                                           str(directory) + os.pathsep + args.java_agent, "CiManual"],
                                          cwd=directory, env=java_env, capture_output=True, text=True, timeout=90)
                assert java_run.returncode == 0, java_run.stdout + java_run.stderr
                java_events = []
                for path, headers, body in backend.records:
                    if path == "/api/v2/citestcycle":
                        if headers.get("Content-Encoding") == "gzip":
                            body = gzip.decompress(body)
                        java_events.extend(msgpack.unpackb(body, raw=False)["events"])
                java_event_types = sorted({event["type"] for event in java_events})
                assert "test" in java_event_types, (java_event_types, java_run.stderr)
                java_result = {"executed": True, "api": "manual CIVisibility session/module/suite/test",
                               "event_types": java_event_types,
                               "backend_paths": sorted({record[0] for record in backend.records})}
            for path, headers, body in records:
                assert headers.get("DD-API-KEY") == "research-server-key"
                assert "X-Datadog-EVP-Subdomain" not in headers
            probe_headers = {"X-Datadog-EVP-Subdomain": "api", "DD-API-KEY": "spoofed-key"}
            status, headers, body = request(adapted_port, "/evp_proxy/v4" + SETTINGS + "?throttle=1", b"{}", probe_headers)
            assert (status, headers.get("Retry-After"), body) == (429, "7", b'{"error":"fixture throttle"}')
            before = len(backend.records)
            blocked = request(adapted_port, "/evp_proxy/v2/api/v2/llmobs", b"{}",
                              {"X-Datadog-EVP-Subdomain": "llmobs-intake"})[0]
            assert blocked == 404 and len(backend.records) == before
            gateway.enabled = False
            status, _, body = request(adapted_port, "/info", method="GET")
            assert json.loads(body)["endpoints"] == []
            assert request(adapted_port, "/evp_proxy/v2/api/v2/citestcycle", b"{}",
                           {"X-Datadog-EVP-Subdomain": "citestcycle-intake"})[0] == 404
            assert len(backend.records) == before
            print(json.dumps({"sdk": "ddtrace 4.13.0rc1", "pytest": "8.3.5",
                "direct_forwarder_observed_paths": raw_paths, "direct_evp_status": direct_status,
                "direct_default_plugin_paths": raw_new_paths,
                "direct_default_plugin_pytest_exit": direct_new.returncode,
                "adapter_default_plugin_backend_paths": new_paths,
                "java_released_sdk": java_result,
                "adapter_backend_paths": paths, "event_types": event_types,
                "decoded_test_coverage_records": len(coverages),
                "python_legacy_backend_request_count": len(records),
                "response_429_body_retry_after_preserved": True,
                "unrelated_product_and_disabled_ci_rejected_without_egress": True,
                "authenticated_datadog_backend": False}, indent=2))
    finally:
        for process in processes:
            process.terminate()
            process.wait(timeout=10)
        gateway.shutdown()
        backend.shutdown()


if __name__ == "__main__":
    main()
