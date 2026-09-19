#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real Python SDK/WAF -> unmodified http_forwarder -> local native capture.

Requires ddtrace, Flask, msgpack and the shared forwarder executable.
No authenticated backend is contacted. Output contains only this local test app.
"""
import argparse
import importlib.metadata
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import msgpack


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--forwarder", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    captures = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            body = json.dumps({"version": "research", "endpoints": ["/v0.4/traces"],
                               "span_meta_structs": False}).encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            if self.path == "/v0.4/traces":
                captures.append((dict(self.headers), body))
            self.send_response(200)
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"{}")

        do_PUT = do_POST

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    with socket.socket() as port:
        port.bind(("127.0.0.1", 0))
        ingress = port.getsockname()[1]
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="appsec-forwarder-") as tmp:
        config = Path(tmp) / "config.yaml"
        config.write_text(f"ingress:\n  endpoint: 127.0.0.1:{ingress}\n  compression_algorithms: []\negress:\n  endpoint: http://127.0.0.1:{server.server_port}\n  timeout: 5s\n")
        forwarder = subprocess.Popen([args.forwarder, "--config", str(config)], stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE, text=True)
        try:
            assert forwarder.stdout.readline().startswith("READY"), "forwarder startup failed"
            env = dict(os.environ, DD_TRACE_AGENT_URL=f"http://127.0.0.1:{ingress}",
                       DD_APPSEC_ENABLED="true", DD_REMOTE_CONFIGURATION_ENABLED="false",
                       DD_INSTRUMENTATION_TELEMETRY_ENABLED="false", DD_SERVICE="appsec-local-fixture",
                       DD_ENV="research", DD_VERSION="fixture", DD_TRACE_API_VERSION="v0.4")
            app = """
import ddtrace.auto
from flask import Flask
from ddtrace import tracer
app = Flask(__name__)
@app.route('/')
def index(): return 'local fixture'
with app.test_client() as client:
    response = client.get('/', headers={'User-Agent': 'dd-test-scanner-log'}, buffered=True)
    print('HTTP_STATUS', response.status_code)
    response.close()
tracer.shutdown()
"""
            run = subprocess.run([sys.executable, "-c", app], env=env, cwd=tmp, text=True,
                                 capture_output=True, timeout=45, check=True)
            assert captures, f"no trace captured: {run.stderr}"
            spans = []
            for index, (headers, body) in enumerate(captures):
                (output / f"payload-{index}.msgpack").write_bytes(body)
                spans.extend(span for trace in msgpack.unpackb(body, raw=False, strict_map_key=False)
                             for span in trace)
            security = []
            for span in spans:
                meta = span.get("meta", {})
                structs = span.get("meta_struct", {})
                if meta.get("appsec.event") == "true" or "appsec" in structs:
                    decoded = {key: msgpack.unpackb(value, raw=False, strict_map_key=False)
                               for key, value in structs.items()}
                    security.append({"name": span["name"], "meta": meta,
                                     "metrics": span.get("metrics", {}), "meta_struct": decoded})
            assert security, f"WAF did not emit security event: {run.stderr}"
            result = {"runtime_versions": {name: importlib.metadata.version(name)
                                            for name in ("ddtrace", "flask", "msgpack")},
                      "capture_count": len(captures), "span_count": len(spans),
                      "sdk_stdout": run.stdout.strip(), "backend_validated": False,
                      "receiver_translation_validated": False, "security_spans": security,
                      "request_headers": captures[0][0]}
            (output / "result.json").write_text(json.dumps(result, indent=2)+"\n")
            print(json.dumps({k: v for k, v in result.items() if k != "security_spans"}, indent=2))
        finally:
            forwarder.terminate()
            forwarder.communicate(timeout=10)
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    main()
