#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Execute both actual SDK alternatives with Dynamic Instrumentation disabled."""
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess

KUBE = ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc"]
checks = []
for application in ["debugger-python", "debugger-python-http-forwarder"]:
    source = json.loads(subprocess.check_output(KUBE + ["get", "deployment", application, "-o", "json"]))
    pod = source["spec"]["template"]["spec"]
    pod["restartPolicy"] = "Never"
    container = pod["containers"][0]
    container.pop("readinessProbe", None)
    container["env"].extend([{"name": "DD_DYNAMIC_INSTRUMENTATION_ENABLED", "value": "false"}, {"name": "ITERATIONS", "value": "5"}])
    job = {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"generateName": "debugger-disabled-", "namespace": "ddot-poc"},
           "spec": {"backoffLimit": 0, "ttlSecondsAfterFinished": 3600,
                    "template": {"metadata": {"labels": {"app.kubernetes.io/part-of": "ddot-products-poc"}}, "spec": pod}}}
    result = json.loads(subprocess.check_output(KUBE + ["create", "-f", "-", "-o", "json"], input=json.dumps(job), text=True))
    name = result["metadata"]["name"]
    subprocess.run(KUBE + ["wait", "--for=condition=complete", "job/" + name, "--timeout=90s"], check=True)
    logs = subprocess.check_output(KUBE + ["logs", "job/" + name], text=True)
    records = []
    for line in logs.splitlines():
        try:
            records.append(json.loads(line))
        except ValueError:
            pass
    assert sum(entry.get("event") == "activity" for entry in records) == 5, records
    assert not any(entry.get("event") == "sdk_http" and entry.get("path", "").startswith("/debugger/") for entry in records), records
    checks.append({"application": application, "job": name, "activity_iterations": 5, "debugger_uploads": 0, "ordinary_otlp_configured": True})
result = {"timestamp": datetime.now(timezone.utc).isoformat(), "sdk_dynamic_instrumentation_enabled": False, "checks": checks,
          "scope": "SDK opt-out; route-gate tests belong to shared forwarding component validation", "backend_product_verified": False}
Path(__file__).with_name("disabled-results.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result, indent=2))
