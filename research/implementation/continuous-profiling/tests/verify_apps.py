# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Exercise built SDK images against an explicit local contract fixture.

This test proves payload production and OTLP selection, never backend success.
The fixture is NOT used by deployment manifests.
"""
import argparse
import email.parser
import email.policy
import gzip
import http.server
import json
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path


def read_body(handler):
    if handler.headers.get("Transfer-Encoding", "").lower() != "chunked":
        return handler.rfile.read(int(handler.headers.get("Content-Length", "0")))
    chunks = []
    while True:
        length = int(handler.rfile.readline().split(b";", 1)[0], 16)
        if not length:
            while handler.rfile.readline() != b"\r\n":
                pass
            return b"".join(chunks)
        chunks.append(handler.rfile.read(length))
        handler.rfile.read(2)


class Fixture(http.server.BaseHTTPRequestHandler):
    records = []

    def log_message(self, *_):
        pass

    def do_GET(self):
        body = json.dumps({"version": "7.0.0", "endpoints": ["/profiling/v1/input"]}).encode()
        self.send_response(200 if self.path == "/info" else 404)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        body = read_body(self)
        if self.headers.get("Content-Encoding") == "gzip":
            body = gzip.decompress(body)
        record = {"path": self.path, "bytes": len(body), "content_type": self.headers.get("Content-Type", "")}
        if "multipart/form-data" in record["content_type"]:
            message = email.parser.BytesParser(policy=email.policy.default).parsebytes(
                ("Content-Type: " + record["content_type"] + "\r\n\r\n").encode() + body)
            parts = list(message.iter_parts())
            record["parts"] = [{"filename": part.get_filename(), "bytes": len(part.get_payload(decode=True))}
                               for part in parts]
            for part in parts:
                if part.get_filename() in ("event.json", "event") or part.get_param("name", header="content-disposition") == "event":
                    event = json.loads(part.get_payload(decode=True))
                    record["family"] = event.get("family")
                    record["attachments"] = event.get("attachments")
        elif self.path == "/v1/traces" and "json" in record["content_type"]:
            payload = json.loads(body)
            resources = payload.get("resourceSpans", payload.get("resource_spans", []))
            record["span_count"] = sum(len(scope.get("spans", [])) for resource in resources
                                        for scope in resource.get("scopeSpans", resource.get("scope_spans", [])))
        self.records.append(record)
        status = 202 if self.path in ("/profiling/v1/input", "/api/v2/profile") else 200 if self.path == "/v1/traces" else 404
        response = b"{}"
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)


def run_app(language, url, seconds, profiling=True):
    env = {
        "WORKLOAD_SECONDS": str(seconds), "DD_TRACE_AGENT_URL": url,
        "DD_TRACE_ENABLED": "true", "OTEL_TRACES_EXPORTER": "otlp",
        "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": url + "/v1/traces",
        "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/json",
        "DD_TRACE_OTEL_ENABLED": "true", "DD_TRACE_OTEL_EXPORTER": "otlp",
        "DD_OTLP_TRACES_ENDPOINT": url + "/v1/traces", "DD_OTLP_TRACES_PROTOCOL": "http/json",
        "DD_PROFILING_ENABLED": str(profiling).lower(),
        "DD_PROFILING_UPLOAD_INTERVAL": "4", "DD_PROFILING_UPLOAD_PERIOD": "4",
    }
    command = ["docker", "run", "--rm", "--network=host"]
    for key, value in env.items():
        command.extend(["-e", key + "=" + value])
    command.append("ddot-profile-" + language + ":poc")
    result = subprocess.run(command, capture_output=True, text=True, timeout=seconds + 40)
    return {"language": language, "exit_code": result.returncode,
            "stdout": result.stdout, "stderr": result.stderr}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--seconds", type=int, default=14)
    parser.add_argument("--languages", nargs="+", choices=("python", "java", "node"), default=["python", "java", "node"])
    args = parser.parse_args()
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    url = "http://127.0.0.1:" + str(server.server_port)
    results = []
    try:
        for language in args.languages:
            offset = len(Fixture.records)
            result = run_app(language, url, args.seconds)
            records = Fixture.records[offset:]
            expected = "node" if language == "node" else language
            profiles = [r for r in records if r.get("family") == expected]
            traces = [r for r in records if r.get("span_count", 0) > 0]
            nonempty_profiles = profiles and all(any(part["bytes"] > 0 and
                (str(part["filename"]).endswith(".pprof") or str(part["filename"]).endswith(".jfr"))
                for part in profile["parts"]) for profile in profiles)
            forbidden = [r for r in records if r["path"].startswith(("/v0.", "/v1/traces/"))]
            result.update({"profile_requests": profiles, "otlp_trace_samples": traces[:3],
                           "total_requests": len(records), "profile_count": len(profiles), "otlp_trace_batches": len(traces),
                           "native_trace_requests": len(forbidden),
                           "passed": result["exit_code"] == 0 and bool(nonempty_profiles) and bool(traces) and not forbidden})
            results.append(result)
            print(json.dumps({k: result[k] for k in ("language", "profile_count", "otlp_trace_batches", "native_trace_requests", "passed")}), flush=True)
        # A local SDK disable control must stop collection/upload, while OTLP traces continue.
        disabled = []
        for language in args.languages:
            offset = len(Fixture.records)
            result = run_app(language, url, 6, profiling=False)
            records = Fixture.records[offset:]
            count = sum(1 for r in records if r["path"] == "/profiling/v1/input")
            trace_count = sum(1 for r in records if r.get("span_count", 0) > 0)
            disabled.append({"language": language, "profile_requests": count, "otlp_trace_batches": trace_count,
                             "passed": result["exit_code"] == 0 and count == 0 and trace_count > 0,
                             "exit_code": result["exit_code"]})
        report = {"timestamp": datetime.now(timezone.utc).isoformat(), "kind": "local SDK contract test, not Datadog backend",
                  "enabled": results, "disabled": disabled,
                  "passed": all(r["passed"] for r in results + disabled)}
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + "\n")
        if not report["passed"]:
            raise SystemExit(1)
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
