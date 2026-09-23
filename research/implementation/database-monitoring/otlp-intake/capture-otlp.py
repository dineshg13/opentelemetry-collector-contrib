#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Capture standard OTLP/HTTP JSON locally; this is not a Datadog backend."""
import argparse
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--port", type=int, default=4319)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    sequence = 0

    class Capture(BaseHTTPRequestHandler):
        def do_POST(self):
            nonlocal sequence
            if self.path != "/v1/logs":
                self.send_error(404)
                return
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > 8 * 1024 * 1024:
                self.send_error(413)
                return
            try:
                payload = json.loads(self.rfile.read(length))
                if not isinstance(payload, dict) or "resourceLogs" not in payload:
                    raise ValueError("expected an OTLP logs export request")
            except (ValueError, UnicodeDecodeError):
                self.send_error(400)
                return
            sequence += 1
            filename = args.output / f"logs-{sequence:04d}.json"
            filename.write_text(json.dumps(payload, indent=2) + "\n")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"{}")
            print(f"captured {filename.name}", flush=True)

        def log_message(self, *_args):
            pass

    print(f"Local capture listening on 127.0.0.1:{args.port}", flush=True)
    HTTPServer(("127.0.0.1", args.port), Capture).serve_forever()


if __name__ == "__main__":
    main()
