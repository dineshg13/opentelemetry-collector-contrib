# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

"""Real SDK uploads through unmodified current http_forwarder to strict mock intake.

The mock accepts ONLY /api/v2/profile with the research key and returns 202.
This is transport evidence, not authenticated Datadog/backend validation.
"""
import argparse
import email.policy
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
from email.parser import BytesParser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


ROOT = Path(__file__).resolve().parent


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def read_body(handler):
    if handler.headers.get("Transfer-Encoding", "").lower() != "chunked":
        return handler.rfile.read(int(handler.headers.get("Content-Length", "0")))
    chunks = []
    while True:
        size = int(handler.rfile.readline().split(b";", 1)[0].strip(), 16)
        if size == 0:
            while handler.rfile.readline().strip():
                pass
            return b"".join(chunks)
        chunks.append(handler.rfile.read(size))
        assert handler.rfile.read(2) == b"\r\n"


def multipart_details(content_type, body):
    parsed = BytesParser(policy=email.policy.default).parsebytes(
        b"Content-Type: " + content_type.encode() + b"\r\n\r\n" + body
    )
    parts = []
    event = {}
    for part in parsed.iter_parts():
        payload = part.get_payload(decode=True)
        name = part.get_filename()
        parts.append({"filename": name, "bytes": len(payload),
                      "magic_hex": payload[:4].hex()})
        if name == "event.json":
            event = json.loads(payload)
    return parts, {key: event.get(key) for key in
                   ("family", "version", "attachments", "tags_profiler")}


def run(args):
    received = []

    class Intake(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *_):
            pass

        def do_POST(self):
            body = read_body(self)
            content_type = self.headers.get("Content-Type", "")
            parts, event = multipart_details(content_type, body) if "multipart/" in content_type else ([], {})
            status = 202 if self.path == "/api/v2/profile" else 404
            if self.headers.get("DD-API-KEY") != "research-only-key":
                status = 403
            record = {"path": self.path, "body_bytes": len(body),
                      "body_sha256": hashlib.sha256(body).hexdigest(),
                      "content_type": content_type.split(";", 1)[0],
                      "origin": self.headers.get("DD-EVP-ORIGIN"),
                      "origin_version": self.headers.get("DD-EVP-ORIGIN-VERSION"),
                      "key_matches": self.headers.get("DD-API-KEY") == "research-only-key",
                      "additional_tags": self.headers.get("X-Datadog-Additional-Tags"),
                      "parts": parts, "event": event, "status": status}
            received.append(record)
            self.send_response(status)
            self.send_header("Content-Length", "0")
            self.end_headers()

    server = ThreadingHTTPServer(("127.0.0.1", 0), Intake)
    server.daemon_threads = True
    threading.Thread(target=server.serve_forever, daemon=True).start()
    ingress = free_port()
    results = {"scope": "real SDK + current forwarder + strict mock; backend/UI unverified",
               "runtime": {"python_executable": sys.executable,
                           "python_version": platform.python_version(),
                           "platform": platform.platform(),
                           "java_executable": shutil.which("java"),
                           "java_version": subprocess.run(["java", "-version"], text=True,
                                                          capture_output=True, check=True).stderr.strip(),
                           "java_agent_sha256": hashlib.sha256(Path(args.java_agent).read_bytes()).hexdigest()
                           if args.java_agent else None}, "cases": []}
    with tempfile.TemporaryDirectory(prefix="ddot-profile-") as tmp:
        config = Path(tmp) / "forwarder.yaml"
        config.write_text((ROOT / "forwarder.yaml").read_text()
                          .replace("18426", str(ingress)).replace("18427", str(server.server_port)))
        log = open(Path(tmp) / "forwarder.log", "w+")
        forwarder = subprocess.Popen([args.forwarder, "--config", str(config)], stdout=log, stderr=log)
        try:
            for _ in range(100):
                try:
                    with socket.create_connection(("127.0.0.1", ingress), timeout=0.1):
                        break
                except OSError:
                    if forwarder.poll() is not None:
                        raise RuntimeError("Forwarder exited: " + (Path(tmp) / "forwarder.log").read_text())
                    time.sleep(0.05)
            else:
                raise RuntimeError("Forwarder did not bind")
            base_env = {key: value for key, value in os.environ.items() if not key.startswith("DD_")}
            base_env.update({"DD_TRACE_ENABLED": "false", "DD_REMOTE_CONFIGURATION_ENABLED": "false",
                             "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false", "DD_SERVICE": "research-profiles",
                             "DD_ENV": "research", "DD_VERSION": "1", "DD_PROFILING_ENABLED": "true",
                             "DD_TRACE_AGENT_URL": f"http://127.0.0.1:{ingress}"})

            def execute(name, command, extra_env, expected_path, expected_family, expected_status):
                start = len(received)
                env = base_env | extra_env
                completed = subprocess.run(command, env=env, cwd=tmp, text=True,
                                           capture_output=True, timeout=45)
                uploads = received[start:]
                profiles = [record for record in uploads if record["content_type"] == "multipart/form-data"]
                assert completed.returncode == 0, (name, completed.stderr)
                assert profiles, (name, completed.stdout, completed.stderr)
                for profile in profiles:
                    assert profile["path"] == expected_path, profile
                    assert profile["status"] == expected_status, profile
                    assert profile["event"]["family"] == expected_family, profile
                    assert profile["key_matches"], profile
                    assert any(part["bytes"] > 100 and part["filename"] != "event.json"
                               for part in profile["parts"]), profile
                results["cases"].append({"name": name, "exit_code": completed.returncode,
                                          "command": command,
                                          "sdk_environment": {key: value for key, value in env.items()
                                                              if key.startswith("DD_") or key == "PYTHONPATH"},
                                          "profiles": profiles,
                                          "stdout": completed.stdout.strip(),
                                          "upload_error_logged": "Error uploading" in completed.stderr
                                          or "Failed to upload profile" in completed.stderr})

            py_env = {"PYTHONPATH": args.python_runtime}
            execute("python_default_path", [sys.executable, str(ROOT / "python_workload.py")], py_env,
                    "/profiling/v1/input", "python", 404)
            execute("python_agent_url_with_backend_path", [sys.executable, str(ROOT / "python_workload.py")],
                    py_env | {"DD_TRACE_AGENT_URL": f"http://127.0.0.1:{ingress}/api/v2/profile"},
                    "/api/v2/profile/profiling/v1/input", "python", 404)
            if args.java_agent:
                subprocess.run(["javac", "-d", tmp, str(ROOT / "ProfileWorkload.java")], check=True)
                java = ["java", "-javaagent:" + args.java_agent, "-cp", tmp, "ProfileWorkload"]
                java_env = {"DD_PROFILING_START_DELAY": "0", "DD_PROFILING_UPLOAD_PERIOD": "1",
                            "DD_PROFILING_AGENTLESS": "false"}
                execute("java_default_path", java, java_env, "/profiling/v1/input", "java", 404)
                execute("java_exact_url_override", java,
                        java_env | {"DD_PROFILING_URL": f"http://127.0.0.1:{ingress}/api/v2/profile"},
                        "/api/v2/profile", "java", 202)
            # Exposes the forwarder's response behavior independently of SDK logging.
            conn = http.client.HTTPConnection("127.0.0.1", ingress, timeout=3)
            conn.request("POST", "/api/v2/profile", body=b"response-only-probe")
            response = conn.getresponse()
            assert response.status == 202
            response.read()
            conn.close()
            results["backend_202_returned_unchanged"] = True
        finally:
            forwarder.terminate()
            forwarder.wait(timeout=10)
            log.close()
            server.shutdown()
            server.server_close()
    print(json.dumps(results, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--forwarder", default="/tmp/ddot-research-forwarder")
    parser.add_argument("--python-runtime", default="/tmp/ddot-research-python-runtime")
    parser.add_argument("--java-agent", default="/tmp/ddot-research-java-agent-1.66.0.jar")
    run(parser.parse_args())
