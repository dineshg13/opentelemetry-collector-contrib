"""Actual Python SDK -> actual http_forwarder -> strict local intake.

The optional path adapter is deliberately separate from the unchanged extension.
It models only path rewriting, not an Agent, discovery, RC or backend semantics.
"""

import argparse
import base64
import gzip
import hashlib
import http.client
import json
import os
from pathlib import Path
import select
import socket
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import msgpack


def clean_fixture_env():
    """Keep runtime paths while excluding caller telemetry/product configuration."""
    return {key: value for key, value in os.environ.items()
            if not key.startswith(("DD_", "_DD_", "OTEL_"))}


class CaptureServer(ThreadingHTTPServer):
    def __init__(self, status=202, upstream=None):
        super().__init__(("127.0.0.1", 0), CaptureHandler)
        self.status = status
        self.upstream = upstream
        self.records = []
        self.thread = threading.Thread(target=self.serve_forever, daemon=True)
        self.thread.start()

    @property
    def url(self):
        return f"http://127.0.0.1:{self.server_port}"

    def close(self):
        self.shutdown()
        self.server_close()
        self.thread.join()


class CaptureHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
            chunks = []
            while True:
                size = int(self.rfile.readline().split(b";", 1)[0], 16)
                if size == 0:
                    while self.rfile.readline() not in (b"\r\n", b"\n", b""):
                        pass
                    break
                chunks.append(self.rfile.read(size))
                assert self.rfile.read(2) == b"\r\n"
            body = b"".join(chunks)
        else:
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        record = {"path": self.path, "headers": dict(self.headers), "body": body}
        self.server.records.append(record)
        if self.server.upstream:
            assert self.path == "/v0.1/pipeline_stats", self.path
            conn = http.client.HTTPConnection("127.0.0.1", self.server.upstream.server_port)
            # Forwarder-provided static authentication/metadata survive the adapter.
            headers = {k: v for k, v in self.headers.items()
                       if k.lower() not in ("host", "transfer-encoding", "content-length")}
            conn.request("POST", "/api" + self.path, body, headers)
            response = conn.getresponse()
            status, response_body = response.status, response.read()
            conn.close()
        else:
            status = self.server.status if self.path == "/api/v0.1/pipeline_stats" else 404
            response_body = json.dumps({"mock_status": status}).encode()
        record["response_status"] = status
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response_body)))
        self.end_headers()
        self.wfile.write(response_body)

    def log_message(self, *_):
        pass


def reserve_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def run_case(binary, directory, name, *, adapter=False, preserve=True, status=202, enabled=True):
    backend = CaptureServer(status=status)
    rewrite = CaptureServer(upstream=backend) if adapter else None
    target = rewrite or backend
    port = reserve_port()
    config = Path(directory) / f"{name}.yaml"
    compression = "  compression_algorithms: []\n" if preserve else ""
    config.write_text(
        f"ingress:\n  endpoint: 127.0.0.1:{port}\n{compression}"
        f"egress:\n  endpoint: {target.url}/api\n  timeout: 5s\n"
        "  headers:\n    DD-API-KEY: synthetic-dsm-key\n"
        "    X-Datadog-Additional-Tags: host:research,default_env:test,agent_version:prototype\n"
    )
    forwarder = subprocess.Popen(
        [binary, "--config", str(config)], stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT, text=True,
    )
    try:
        ready, _, _ = select.select([forwarder.stdout], [], [], 10)
        assert ready, "forwarder startup timed out"
        first_line = forwarder.stdout.readline()
        assert first_line.startswith("READY"), first_line
        env = clean_fixture_env()
        env.update({
            "DD_TRACE_AGENT_URL": f"http://127.0.0.1:{port}",
            "DD_SERVICE": "dsm-research-python", "DD_ENV": "test", "DD_VERSION": "research",
            "DD_DATA_STREAMS_ENABLED": str(enabled).lower(),
            "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false",
            "DD_REMOTE_CONFIGURATION_ENABLED": "false",
            "DD_TRACE_STARTUP_LOGS": "false",
        })
        sdk = subprocess.run(
            [sys.executable, str(Path(__file__).with_name("sdk_emit.py"))],
            env=env, text=True, capture_output=True, timeout=20, check=True,
        )
        emitted = json.loads(sdk.stdout.strip().splitlines()[-1])
        assert emitted["enabled"] == enabled
        if not enabled:
            assert backend.records == []
            return {"case": name, "sdk_version": emitted["sdk_version"], "requests": 0}
        assert len(backend.records) == 1, backend.records
        record = backend.records[0]
        headers = {k.lower(): v for k, v in record["headers"].items()}
        assert headers["dd-api-key"] == "synthetic-dsm-key"
        assert headers["datadog-meta-lang"] == "python"
        assert headers["content-type"] == "application/msgpack"
        assert headers["x-datadog-additional-tags"].startswith("host:research,")
        assert "via" in headers
        body = record["body"]
        if preserve:
            assert headers["content-encoding"] == "gzip"
            raw = gzip.decompress(body)
        else:
            assert "content-encoding" not in headers
            raw = body
        payload = msgpack.unpackb(raw, raw=False, strict_map_key=False)
        assert payload["Lang"] == "python"
        assert payload["Service"] == "dsm-research-python"
        assert payload["Env"] == "test"
        assert payload["Version"] == "research"
        points = [point for bucket in payload["Stats"] for point in bucket["Stats"]]
        assert len(points) == 2
        by_hash = {point["Hash"]: point for point in points}
        assert by_hash[emitted["producer_hash"]]["ParentHash"] == 0
        assert by_hash[emitted["consumer_hash"]]["ParentHash"] == emitted["producer_hash"]
        for point in points:
            for key in ("PathwayLatency", "EdgeLatency", "PayloadSize"):
                assert isinstance(point[key], bytes) and len(point[key]) > 0
        backlogs = [backlog for bucket in payload["Stats"] for backlog in bucket["Backlogs"]]
        assert sorted(backlog["Value"] for backlog in backlogs) == [8, 12]
        propagation = base64.b64decode(emitted["propagation"]["dd-pathway-ctx-base64"])
        assert int.from_bytes(propagation[:8], "little") == emitted["producer_hash"]
        expected_status = status if adapter else 404
        assert record["response_status"] == expected_status
        if expected_status == 404:
            assert "does not support data streams monitoring" in sdk.stderr
        if expected_status == 503:
            assert "503" in sdk.stderr
        identical = None
        if rewrite:
            identical = rewrite.records[0]["body"] == body
            assert identical
            assert rewrite.records[0]["response_status"] == status
        return {
            "case": name, "sdk_version": emitted["sdk_version"], "requests": len(backend.records),
            "backend_path": record["path"], "response_status": expected_status,
            "content_encoding": headers.get("content-encoding"),
            "payload_keys": sorted(payload), "point_count": len(points),
            "sketches_are_nonempty_bytes": True, "backlog_values": [8, 12],
            "propagation_parent_link": True, "adapter_body_unchanged": identical,
            "compressed_body_sha256": hashlib.sha256(body).hexdigest() if preserve else None,
        }
    finally:
        forwarder.terminate()
        forwarder.communicate(timeout=10)
        if rewrite:
            rewrite.close()
        backend.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--forwarder", default="/tmp/ddot-research-forwarder")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="ddot-dsm-") as directory:
        cases = [
            run_case(args.forwarder, directory, "configured_api_prefix_is_ignored"),
            run_case(args.forwarder, directory, "default_ingress_decompresses", preserve=False),
            run_case(args.forwarder, directory, "separate_path_adapter", adapter=True),
            run_case(args.forwarder, directory, "backend_503_returns_to_sdk", adapter=True, status=503),
            run_case(args.forwarder, directory, "sdk_disabled", enabled=False),
        ]
    result = json.dumps({"validation": "local HTTP transport only; no authenticated backend", "cases": cases}, indent=2)
    if args.output:
        args.output.write_text(result + "\n")
    print(result)


if __name__ == "__main__":
    main()
