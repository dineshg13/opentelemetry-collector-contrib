#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Deploy only isolated DBM PoC database/application resources to kind-otel-dd."""
import json
from pathlib import Path
import secrets
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent
KUBE = ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc"]


def main():
    context = subprocess.check_output(["kubectl", "config", "current-context"], text=True).strip()
    assert context == "kind-otel-dd", context
    subprocess.run(["docker", "build", "-t", "ddot-dbm-python:poc", str(ROOT)], check=True)
    # Restrict the archive to the kind nodes' platform. Cached multi-platform
    # indexes can otherwise reference manifests absent from the Docker store.
    architecture = subprocess.check_output(KUBE + ["get", "nodes", "-o", "jsonpath={.items[0].status.nodeInfo.architecture}"], text=True)
    with tempfile.TemporaryDirectory(prefix="ddot-dbm-image-") as directory:
        archive = str(Path(directory) / "images.tar")
        subprocess.run(["docker", "image", "save", "--platform", "linux/" + architecture,
                        "-o", archive, "ddot-dbm-python:poc", "postgres:16-alpine"], check=True)
        subprocess.run(["kind", "load", "image-archive", "--name", "otel-dd", archive], check=True)
    found = subprocess.check_output(KUBE + ["get", "secret", "dbm-postgres-password", "--ignore-not-found", "-o", "name"], text=True)
    if not found.strip():
        # Credentials are only passed to kubectl on stdin, never printed or committed.
        secret = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "dbm-postgres-password", "namespace": "ddot-poc"},
                  "stringData": {"password": secrets.token_urlsafe(24)}}
        subprocess.run(KUBE + ["apply", "-f", "-"], input=json.dumps(secret), text=True, check=True)
    subprocess.run(KUBE + ["apply", "-f", str(ROOT / "deployment.yaml")], check=True)
    # The local development image tag is reused; recreate pods to run the image
    # just imported, even when no Kubernetes manifest field changed.
    subprocess.run(KUBE + ["rollout", "restart", "deployment/dbm-python"], check=True)
    subprocess.run(KUBE + ["rollout", "status", "deployment/dbm-postgres", "--timeout=180s"], check=True)
    subprocess.run(KUBE + ["rollout", "status", "deployment/dbm-python", "--timeout=180s"], check=True)


if __name__ == "__main__":
    main()
