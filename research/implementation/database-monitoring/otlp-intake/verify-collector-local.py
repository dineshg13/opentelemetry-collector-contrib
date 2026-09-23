#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Verify a built Collector's OTLP wire path and opt-in disablement locally."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import time


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def run_for(command, env, logfile, seconds):
    with logfile.open("w") as output:
        process = subprocess.Popen(command, env=env, stdout=output, stderr=subprocess.STDOUT)
        try:
            try:
                code = process.wait(timeout=seconds)
                raise RuntimeError(f"Collector exited early ({code}); inspect {logfile}")
            except subprocess.TimeoutExpired:
                pass
        finally:
            stop(process)
        if process.returncode != 0:
            raise RuntimeError(f"Collector shutdown failed; inspect {logfile}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--endpoint", default="127.0.0.1:25432")
    parser.add_argument("--context", default="kind-otel-dd")
    parser.add_argument("--namespace", default="dbm-otlp-dev")
    parser.add_argument("--secret", default="postgres")
    parser.add_argument("--secret-key", default="password")
    args = parser.parse_args()
    if not args.endpoint.startswith("127.0.0.1:"):
        parser.error("endpoint must be an explicitly configured loopback port-forward")
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    wire = args.output / "wire"
    if wire.exists() and any(wire.iterdir()):
        parser.error("output wire directory must be empty; choose a new output path")
    source = Path(__file__).resolve().parent
    secret = json.loads(subprocess.check_output([
        "kubectl", "--context", args.context, "-n", args.namespace,
        "get", "secret", args.secret, "-o", "json",
    ]))
    env = dict(os.environ)
    env.update(DBM_POSTGRES_ENDPOINT=args.endpoint, DBM_POSTGRES_USERNAME="dbm_monitor",
               DBM_POSTGRES_PASSWORD=base64.b64decode(secret["data"][args.secret_key]).decode(),
               DBM_DATABASE_INSTANCE=f"postgres.{args.namespace}.svc.cluster.local:5432")
    enabled = source / "collector-local.yaml"
    disabled = args.output / "disabled.yaml"
    disabled.write_text(enabled.read_text().replace("enabled: true", "enabled: false"))
    binary = str(args.binary.resolve())
    for config in (enabled, disabled):
        subprocess.run([binary, "validate", f"--config={config}"], env=env, check=True)
    # Refuse an occupied port so we cannot accidentally send to another service.
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 4319))
    with (args.output / "capture.log").open("w") as output:
        capture = subprocess.Popen(["python3", str(source / "capture-otlp.py"), "--output", str(wire)], stdout=output, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 5
            while True:
                try:
                    with socket.create_connection(("127.0.0.1", 4319), timeout=0.2):
                        break
                except OSError:
                    if capture.poll() is not None or time.monotonic() >= deadline:
                        raise RuntimeError("local capture failed to start")
                    time.sleep(0.1)
            run_for([binary, f"--config={enabled}"], env, args.output / "enabled.log", 12)
            requests = sorted(wire.glob("logs-*.json"))
            events = {}
            for request in requests:
                for resource in json.loads(request.read_text()).get("resourceLogs", []):
                    for scope in resource.get("scopeLogs", []):
                        for record in scope.get("logRecords", []):
                            name = record.get("eventName", "missing")
                            events[name] = events.get(name, 0) + 1
            for name in ("db.server.query_metrics", "db.server.activity"):
                if events.get(name, 0) < 2:
                    raise RuntimeError(f"insufficient captured {name}; inspect enabled.log")
            run_for([binary, f"--config={disabled}"], env, args.output / "disabled.log", 6)
            if len(list(wire.glob("logs-*.json"))) != len(requests):
                raise RuntimeError("disabled receiver emitted OTLP records")
            evidence = {"binary_sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest(),
                        "requests": len(requests), "events": events,
                        "disabled_new_requests": 0, "destination": "local OTLP capture, not Datadog",
                        "verified_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
            (args.output / "evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
            print(json.dumps(evidence, indent=2))
        finally:
            stop(capture)


if __name__ == "__main__":
    main()
