#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Deploy only isolated DBM PoC database/application resources to kind-otel-dd."""
import json
from pathlib import Path
import secrets
import subprocess

ROOT = Path(__file__).resolve().parent
KUBE = ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc"]


def main():
    context = subprocess.check_output(["kubectl", "config", "current-context"], text=True).strip()
    assert context == "kind-otel-dd", context
    subprocess.run(["docker", "build", "-t", "ddot-dbm-python:poc", str(ROOT)], check=True)
    subprocess.run(["kind", "load", "docker-image", "--name", "otel-dd", "ddot-dbm-python:poc", "postgres:16-alpine"], check=True)
    found = subprocess.run(KUBE + ["get", "secret", "dbm-postgres-password"], capture_output=True)
    if found.returncode:
        # Credentials are only passed to kubectl on stdin, never printed or committed.
        secret = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "dbm-postgres-password", "namespace": "ddot-poc"},
                  "stringData": {"password": secrets.token_urlsafe(24)}}
        subprocess.run(KUBE + ["apply", "-f", "-"], input=json.dumps(secret), text=True, check=True)
    subprocess.run(KUBE + ["apply", "-f", str(ROOT / "deployment.yaml")], check=True)
    subprocess.run(KUBE + ["rollout", "status", "deployment/dbm-postgres", "--timeout=180s"], check=True)
    subprocess.run(KUBE + ["rollout", "status", "deployment/dbm-python", "--timeout=180s"], check=True)


if __name__ == "__main__":
    main()
