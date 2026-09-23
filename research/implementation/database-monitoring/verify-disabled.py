#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Run the same real SDK/DB workload with DBM SQL propagation disabled."""
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess

KUBE = ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc"]
source = json.loads(subprocess.check_output(KUBE + ["get", "deployment", "dbm-python", "-o", "json"]))
pod = source["spec"]["template"]["spec"]
pod["restartPolicy"] = "Never"
container = pod["containers"][0]
container.pop("readinessProbe", None)
for env in container["env"]:
    if env["name"] == "DD_DBM_PROPAGATION_MODE":
        env["value"] = "disabled"
container["env"].append({"name": "ITERATIONS", "value": "2"})
job = {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"generateName": "dbm-disabled-", "namespace": "ddot-poc"},
       "spec": {"backoffLimit": 0, "ttlSecondsAfterFinished": 3600,
                "template": {"metadata": {"labels": {"app.kubernetes.io/part-of": "ddot-products-poc"}}, "spec": pod}}}
created = json.loads(subprocess.check_output(KUBE + ["create", "-f", "-", "-o", "json"], input=json.dumps(job), text=True))
name = created["metadata"]["name"]
subprocess.run(KUBE + ["wait", "--for=condition=complete", "job/" + name, "--timeout=90s"], check=True)
text = subprocess.check_output(KUBE + ["logs", "job/" + name], text=True)
events = []
for line in text.splitlines():
    try:
        event = json.loads(line)
    except ValueError:
        continue
    if event.get("event") == "query":
        events.append(event)
assert len(events) == 2, events
assert all(event["traceparent"] is None and not event["propagation_expected"] for event in events), events
result = {"timestamp": datetime.now(timezone.utc).isoformat(), "job": name, "queries": len(events),
          "sql_trace_contexts": 0, "ordinary_otlp_export_configured": True, "backend_product_verified": False}
Path(__file__).with_name("disabled-results.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result, indent=2))
