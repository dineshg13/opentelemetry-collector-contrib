# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""SDK wire-contract tests. The loopback HTTP fixture is never a backend validation."""

import argparse
from datetime import datetime, timezone
import gzip
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--python-runtime", required=True)
    parser.add_argument("--java-agent", required=True)
    parser.add_argument("--java-home", default=os.environ.get("JAVA_HOME", ""))
    parser.add_argument("--output")
    parser.add_argument("--node-image", help="Optional image with dd-trace installed under /app/node_modules")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    requests = []

    class Capture(BaseHTTPRequestHandler):
        def do_GET(self):
            body = json.dumps({"endpoints": ["/evp_proxy/v2/", "/evp_proxy/v4/"]}).encode()
            self.send_response(200 if self.path == "/info" else 404)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            if self.headers.get("Content-Encoding") == "gzip":
                body = gzip.decompress(body)
            requests.append({"path": self.path, "content_type": self.headers.get("Content-Type"), "body": body})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"{}")

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Capture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    endpoint = f"http://127.0.0.1:{server.server_port}"
    environment = {key: value for key, value in os.environ.items() if not key.startswith(("DD_", "_DD_", "OTEL_"))}
    environment.update({
        "PYTHONPATH": str(Path(args.python_runtime).resolve()),
        "PYTHONDONTWRITEBYTECODE": "1",
        "DD_TRACE_AGENT_URL": endpoint,
        "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false",
        "DD_REMOTE_CONFIGURATION_ENABLED": "false",
        "DD_RUNTIME_METRICS_ENABLED": "false",
        "DD_TRACE_STARTUP_LOGS": "false",
        "DD_TRACE_ENABLED": "true",
        "DD_PROFILING_ENABLED": "false",
        "DD_TRACE_OTEL_ENABLED": "true",
        "DD_CODE_ORIGIN_FOR_SPANS_ENABLED": "false",
        "DD_SERVICE": "ddot-llm-wire-test",
        "OTEL_TRACES_EXPORTER": "otlp",
        "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/json",
        "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": endpoint + "/v1/traces",
    })
    results = []

    def run_case(language, command):
        requests.clear()
        child = subprocess.run(command, cwd=root, env=environment, text=True, capture_output=True, timeout=45)
        if child.returncode:
            raise RuntimeError(f"{language}: {child.stdout}\n{child.stderr}")
        native = [request["path"] for request in requests if request["path"].endswith("/traces") and request["path"] != "/v1/traces"]
        assert not native, native
        spans = []
        for request in requests:
            if request["path"] != "/v1/traces":
                continue
            assert "json" in request["content_type"], request["content_type"]
            for resource in json.loads(request["body"])["resourceSpans"]:
                for scope in resource["scopeSpans"]:
                    spans.extend(scope["spans"])
        selected = [span for span in spans if span["name"] == "chat deterministic-poc"]
        assert selected, f"No GenAI span in {requests}"
        for span in selected:
            attributes = {attribute["key"]: attribute["value"] for attribute in span["attributes"]}
            assert attributes["gen_ai.operation.name"]["stringValue"] == "chat", attributes
            assert attributes["gen_ai.provider.name"]["stringValue"] == "custom", attributes
            assert attributes["gen_ai.response.model"]["stringValue"] == "deterministic-poc", attributes
            inputs = json.loads(attributes["gen_ai.input.messages"]["stringValue"])
            outputs = json.loads(attributes["gen_ai.output.messages"]["stringValue"])
            assert inputs[0]["role"] == "user" and outputs[0]["content"] == "ready"
            assert float(next(iter(attributes["gen_ai.usage.input_tokens"].values()))) == 4
            assert float(next(iter(attributes["gen_ai.usage.output_tokens"].values()))) == 1
            assert len(span["traceId"]) == 32 and len(span["spanId"]) == 16
        results.append({"language": language, "otlp_spans": len(selected), "native_trace_requests": len(native),
                        "paths": sorted({request["path"] for request in requests}),
                        "semantic_fields": ["operation", "provider", "model", "input", "output", "token_counts"],
                        "stdout": child.stdout.strip().splitlines()})

    try:
        run_case("python", [sys.executable, str(root / "app.py"), "--iterations", "1", "--health-port", "0"])
        requests.clear()
        child = subprocess.run([sys.executable, str(root / "app.py"), "--mode", "native", "--iterations", "1", "--health-port", "0"],
                               cwd=root, env=environment, text=True, capture_output=True, timeout=45)
        assert child.returncode == 0, child.stderr
        native_paths = {request["path"] for request in requests}
        assert "/evp_proxy/v2/api/v2/llmobs" in native_paths, native_paths
        assert "/evp_proxy/v2/api/intake/llm-obs/v2/eval-metric" in native_paths, native_paths
        assert not any(path.endswith("/traces") for path in native_paths), native_paths
        results.append({"language": "python-native", "paths": sorted(native_paths), "ordinary_trace_requests": 0})
        java = str(Path(args.java_home) / "bin/java") if args.java_home else "java"
        javac = str(Path(args.java_home) / "bin/javac") if args.java_home else "javac"
        with tempfile.TemporaryDirectory(prefix="ddot-llm-java-") as classes:
            subprocess.run([javac, "-cp", args.java_agent, "-d", classes, str(root / "LlmWorkload.java")], check=True)
            run_case("java", [java, f"-javaagent:{args.java_agent}", "-cp", classes + os.pathsep + args.java_agent, "LlmWorkload", "2"])
        results.append({"java_runtime": subprocess.check_output([java, "-version"], stderr=subprocess.STDOUT, text=True).strip()})
        if args.node_image:
            command = ["docker", "run", "--rm", "--network", "host", "--entrypoint", "node",
                       "--mount", f"type=bind,src={root},dst=/work,readonly",
                       "-e", "DDOT_JS_SDK=/app/node_modules/dd-trace"]
            for key, value in environment.items():
                if key.startswith(("DD_", "OTEL_")):
                    command.extend(["-e", key + "=" + value])
            run_case("javascript", command + [args.node_image, "/work/app.cjs"])
    finally:
        server.shutdown()
        server.server_close()
    result = {"timestamp": datetime.now(timezone.utc).isoformat(), "scope": "actual SDK to local HTTP wire-contract fixture", "backend_verified": False, "checks": results}
    rendered = json.dumps(result, indent=2) + "\n"
    if args.output:
        Path(args.output).write_text(rendered)
    print(rendered)


if __name__ == "__main__":
    main()
