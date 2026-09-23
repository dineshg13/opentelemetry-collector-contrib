#!/usr/bin/env python3
"""Real OSS OpenLineage HTTP client through the unmodified Collector forwarder.

Uses a local backend-shaped capture server. This is not ddtrace instrumentation,
an Agent implementation, a complete Collector, or a Datadog backend validation.
"""

import argparse
import gzip
import hashlib
import importlib.metadata
import json
from pathlib import Path
import queue
import selectors
import socket
import subprocess
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from openlineage.client.event_v2 import Job, Run, RunEvent, RunState
from openlineage.client.transport.http import HttpConfig, HttpTransport
import requests


EVENT = RunEvent(
    eventType=RunState.COMPLETE,
    eventTime="2026-09-19T00:00:00Z",
    run=Run(runId="153248bb-04ad-456d-bc93-51d48d3b389a"),
    job=Job(namespace="research", name="collector-forwarder-transport"),
    producer="https://github.com/open-telemetry/opentelemetry-collector-contrib",
    inputs=[],
    outputs=[],
)


class RecordingSession(requests.Session):
    def __init__(self):
        super().__init__()
        self.trust_env = False
        self.sent_bodies = []

    def send(self, request, **kwargs):
        self.sent_bodies.append(request.body)
        return super().send(request, **kwargs)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--forwarder", default="/tmp/ddot-research-forwarder")
    args = parser.parse_args()
    captures = queue.Queue()

    class Capture(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def do_POST(self):
            body = self.rfile.read(int(self.headers["Content-Length"]))
            captures.put((self.path, self.headers, body))
            if self.path.startswith("/openlineage/"):
                status, response = 404, b'{"error":"Agent path is not an intake path"}'
            elif "reject=1" in self.path:
                status, response = 429, b'{"error":"local rate limit probe"}'
            else:
                status, response = 202, b'{"accepted":"local transport only"}'
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(response)))
            self.send_header("Retry-After", "7")
            self.end_headers()
            self.wfile.write(response)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Capture)
    server_thread = threading.Thread(target=server.serve_forever, daemon=True)
    server_thread.start()
    ingress_port = free_port()
    backend_port = server.server_address[1]
    results = []
    try:
        with tempfile.TemporaryDirectory(prefix="ddot-openlineage-") as tmp:
            config = Path(tmp, "forwarder.yaml")
            config.write_text(
                f"ingress:\n  endpoint: 127.0.0.1:{ingress_port}\n"
                "  compression_algorithms: []\n"
                "egress:\n"
                f"  endpoint: http://127.0.0.1:{backend_port}/ignored?api-version=99\n"
                "  timeout: 5s\n  headers:\n"
                "    Authorization: Bearer collector-research-key\n"
                "    X-Datadog-Additional-Tags: host:research-host,default_env:research\n"
            )
            with tempfile.TemporaryFile(mode="w+") as logs:
                process = subprocess.Popen(
                    [args.forwarder, "--config", str(config)],
                    stdout=subprocess.PIPE,
                    stderr=logs,
                    text=True,
                )
                try:
                    with selectors.DefaultSelector() as selector:
                        selector.register(process.stdout, selectors.EVENT_READ)
                        if not selector.select(timeout=15):
                            logs.seek(0)
                            raise RuntimeError(f"forwarder failed to become ready: {logs.read()}")
                        assert process.stdout.readline().startswith("READY ")
                    for endpoint, expected_status in (
                        ("openlineage/api/v1/lineage", 404),
                        ("api/v1/lineage", 202),
                        ("api/v1/lineage?api-version=2", 202),
                        ("api/v1/lineage?api-version=2&reject=1", 429),
                    ):
                        with RecordingSession() as session:
                            config = HttpConfig.from_dict(
                                {
                                    "url": f"http://127.0.0.1:{ingress_port}",
                                    "endpoint": endpoint,
                                    "auth": {"type": "api_key", "apiKey": "client-research-key"},
                                    "compression": "gzip",
                                    "timeout": 5,
                                    "retry": {"total": 0, "status_forcelist": []},
                                }
                            )
                            config.session = session
                            transport = HttpTransport(config)
                            try:
                                response = transport.emit(EVENT)
                            except requests.HTTPError as error:
                                response = error.response
                            assert response.status_code == expected_status
                            assert response.headers["Retry-After"] == "7"
                            assert "Via" in response.headers
                            assert response.json()
                            path, headers, body = captures.get(timeout=5)
                            assert path == "/" + endpoint
                            assert body == session.sent_bodies[-1]
                            assert headers["Content-Encoding"] == "gzip"
                            assert headers["Content-Type"] == "application/json"
                            assert headers.get_all("Authorization") == ["Bearer collector-research-key"]
                            assert headers["X-Datadog-Additional-Tags"] == "host:research-host,default_env:research"
                            assert "X-Datadog-Container-Tags" not in headers
                            payload = json.loads(gzip.decompress(body))
                            assert payload["run"]["runId"] == EVENT.run.runId
                            assert payload["job"]["name"] == EVENT.job.name
                            assert payload["eventType"] == "COMPLETE"
                            results.append(
                                {
                                    "endpoint": endpoint,
                                    "status": response.status_code,
                                    "gzip_bytes_unchanged": True,
                                    "collector_bearer_overrides_client": True,
                                    "body_sha256": hashlib.sha256(body).hexdigest(),
                                }
                            )
                            transport.close()
                finally:
                    process.terminate()
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)
                    process.stdout.close()
    finally:
        server.shutdown()
        server.server_close()
        server_thread.join(timeout=5)

    print(json.dumps({"openlineage_python": importlib.metadata.version("openlineage-python"), "results": results}, indent=2))


if __name__ == "__main__":
    main()
