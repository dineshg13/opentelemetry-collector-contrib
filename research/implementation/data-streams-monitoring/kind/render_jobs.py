# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Print scoped DSM jobs for either Collector implementation; no cluster mutation."""
import argparse
import json

parser = argparse.ArgumentParser()
parser.add_argument("--alternative", action="store_true")
parser.add_argument("--sdk-disabled", action="store_true")
args = parser.parse_args()
collector = "collector-http-forwarder" if args.alternative else "collector"
suffix = "-alternative" if args.alternative else ""
suffix += "-disabled" if args.sdk_disabled else ""
jobs = []
for language in ("python", "java", "node"):
    name = "dsm-" + language + suffix
    labels = {"app": name, "app.kubernetes.io/part-of": "ddot-products-poc"}
    jobs.append({"apiVersion": "batch/v1", "kind": "Job", "metadata": {
        "name": name, "namespace": "ddot-poc", "labels": labels}, "spec": {
        "backoffLimit": 0, "template": {"metadata": {"labels": labels}, "spec": {
            "restartPolicy": "Never", "containers": [{"name": "workload",
                "image": "ddot-dsm-" + language + ":poc", "imagePullPolicy": "Never",
                "env": [{"name": "DD_TRACE_AGENT_URL", "value": "http://" + collector + ".ddot-poc.svc.cluster.local:8126"},
                        {"name": "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "value": "http://" + collector + ".ddot-poc.svc.cluster.local:4318/v1/traces"},
                        {"name": "DD_DATA_STREAMS_ENABLED", "value": str(not args.sdk_disabled).lower()}],
                "resources": {"requests": {"cpu": "50m", "memory": "128Mi"},
                              "limits": {"cpu": "1", "memory": "512Mi"}}}]}}}})
print(json.dumps({"apiVersion": "v1", "kind": "List", "items": jobs}, indent=2))
