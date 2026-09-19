# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Generate scoped lineage jobs for both implemented Collector alternatives."""
import argparse
import json

parser = argparse.ArgumentParser()
parser.add_argument("--sdk-disabled", action="store_true")
args = parser.parse_args()
jobs = []
for alternative in (False, True):
    collector = "collector-http-forwarder" if alternative else "collector"
    name = "djm-python" + ("-alternative" if alternative else "") + ("-disabled" if args.sdk_disabled else "")
    labels = {"app": name, "app.kubernetes.io/part-of": "ddot-products-poc"}
    jobs.append({"apiVersion": "batch/v1", "kind": "Job", "metadata": {
        "name": name, "namespace": "ddot-poc", "labels": labels}, "spec": {
        "backoffLimit": 0, "template": {"metadata": {"labels": labels}, "spec": {
            "nodeSelector": {"kubernetes.io/hostname": "otel-dd-worker"},
            "restartPolicy": "Never", "containers": [{"name": "workload", "image": "ddot-djm-python:poc", "imagePullPolicy": "Never",
                "env": [{"name": "OPENLINEAGE_URL", "value": "http://" + collector + ".ddot-poc.svc.cluster.local:8126"},
                        {"name": "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "value": "http://" + collector + ".ddot-poc.svc.cluster.local:4318/v1/traces"},
                        {"name": "DJM_LINEAGE_ENABLED", "value": str(not args.sdk_disabled).lower()}],
                "resources": {"requests": {"cpu": "50m", "memory": "96Mi"}, "limits": {"cpu": "1", "memory": "256Mi"}}}]}}}})
print(json.dumps({"apiVersion": "v1", "kind": "List", "items": jobs}, indent=2))
