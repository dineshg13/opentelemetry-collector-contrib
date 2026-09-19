#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Deploy only the two SDK applications; the coordinator owns both Collectors."""
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent
KUBE = ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc"]


def main():
    assert subprocess.check_output(["kubectl", "config", "current-context"], text=True).strip() == "kind-otel-dd"
    subprocess.run(["docker", "build", "-t", "ddot-debugger-python:poc", str(ROOT)], check=True)
    architecture = subprocess.check_output(KUBE + ["get", "nodes", "-o", "jsonpath={.items[0].status.nodeInfo.architecture}"], text=True)
    with tempfile.TemporaryDirectory(prefix="ddot-debugger-image-") as directory:
        archive = str(Path(directory) / "image.tar")
        subprocess.run(["docker", "image", "save", "--platform", "linux/" + architecture,
                        "-o", archive, "ddot-debugger-python:poc"], check=True)
        subprocess.run(["kind", "load", "image-archive", "--name", "otel-dd", "--nodes", "otel-dd-worker", archive], check=True)
    subprocess.run(KUBE + ["apply", "-f", str(ROOT / "deployment.yaml")], check=True)
    for name in ["debugger-python", "debugger-python-http-forwarder"]:
        subprocess.run(KUBE + ["rollout", "restart", "deployment/" + name], check=True)
        subprocess.run(KUBE + ["rollout", "status", "deployment/" + name, "--timeout=120s"], check=True)


if __name__ == "__main__":
    main()
