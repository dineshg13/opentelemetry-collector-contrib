#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Replace the identified kind PoC's mock routing, preserving databases and other workloads."""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[3]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--context", default="kind-otel-dd")
    parser.add_argument("--apply", action="store_true", help="perform the authorized PoC migration")
    parser.add_argument("--backup", type=Path, default=Path("/tmp/ddot-poc-migration"))
    args = parser.parse_args()
    current = subprocess.check_output(["kubectl", "config", "current-context"], text=True).strip()
    if args.context != "kind-otel-dd" or current != args.context:
        raise SystemExit("Refusing migration outside the selected kind-otel-dd context")
    kube = ["kubectl", "--context", args.context]

    def get(*parts):
        return json.loads(subprocess.check_output(kube + ["get", *parts, "-o", "json"], text=True))

    nodes = get("nodes")
    assert all(x["metadata"]["name"].startswith("otel-dd-") for x in nodes["items"])
    instrumentation = get("instrumentation", "datadog-otel", "-n", "apps")
    deployments = get("deployments", "-n", "apps")["items"]
    selected = []
    for deployment in deployments:
        annotations = deployment["spec"]["template"]["metadata"].get("annotations", {})
        if "datadog-otel" in annotations.values():
            selected.append(deployment["metadata"]["name"])
    allowed = {"inventory-service", "payment-service", "store-frontend"}
    if not set(selected).issubset(allowed):
        raise SystemExit("Unexpected workload uses the shared Instrumentation; inspect before migrating")
    print(json.dumps({"context": args.context, "apps_to_restart": selected,
                      "remove": ["observability/deployment/fake-datadog", "observability/service/fake-datadog"],
                      "preserve": ["existing PostgreSQL data", "config-recommender", "operator", "other workloads"],
                      "apply": args.apply}))
    if not args.apply:
        return
    subprocess.run(kube + ["rollout", "status", "deployment/otel-collector", "-n", "observability",
                          "--timeout=60s"], check=True)
    args.backup.mkdir(parents=True, exist_ok=True, mode=0o700)
    backup = args.backup / "instrumentation-before.json"
    if not backup.exists():
        fd = os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as f:
            json.dump(instrumentation, f, indent=2)
    subprocess.run(kube + ["apply", "-f", str(ROOT / "research/implementation/kind/namespace.yaml")], check=True)
    source = get("secret", "datadog-api-key", "-n", "observability")
    secret = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "datadog-api-key", "namespace": "ddot-poc"},
              "type": "Opaque", "data": {k: v for k, v in source["data"].items() if k in {"api-key", "application-key"}}}
    subprocess.run(kube + ["apply", "-f", "-"], input=json.dumps(secret), text=True, check=True)
    endpoint = "http://otel-collector.observability.svc.cluster.local:4318"
    proxy_host = "collector.ddot-poc.svc.cluster.local"
    values = {
        "OTEL_TRACES_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_ENDPOINT": endpoint,
        "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": endpoint + "/v1/traces",
        "OTEL_EXPORTER_OTLP_PROTOCOL": "http/json", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/json",
        "DD_TRACE_OTEL_ENABLED": "true", "DD_TRACE_AGENT_URL": "http://" + proxy_host + ":8126",
        "DD_AGENT_HOST": proxy_host, "DD_REMOTE_CONFIGURATION_ENABLED": "false",
        "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false", "DD_RUNTIME_METRICS_ENABLED": "false",
    }
    env = [x for x in instrumentation["spec"].get("env", []) if x["name"] not in values]
    env.extend({"name": k, "value": v} for k, v in values.items())
    patch = {"spec": {"exporter": {"endpoint": endpoint}, "env": env}}
    subprocess.run(kube + ["patch", "instrumentation", "datadog-otel", "-n", "apps", "--type=merge",
                          "-p", json.dumps(patch)], check=True)
    for name in selected:
        subprocess.run(kube + ["rollout", "restart", "deployment/" + name, "-n", "apps"], check=True)
    for name in selected:
        subprocess.run(kube + ["rollout", "status", "deployment/" + name, "-n", "apps", "--timeout=180s"], check=True)
    subprocess.run(kube + ["delete", "deployment/fake-datadog", "service/fake-datadog", "-n", "observability",
                          "--ignore-not-found"], check=True)
    pods = get("pods", "-n", "observability")["items"]
    mounted = {v["configMap"]["name"] for p in pods for v in p["spec"].get("volumes", []) if "configMap" in v}
    removed = []
    for cm in get("configmaps", "-n", "observability")["items"]:
        name = cm["metadata"]["name"]
        if name.startswith("otel-collector-") and name not in mounted and any("fake-datadog" in x for x in cm.get("data", {}).values()):
            subprocess.run(kube + ["delete", "configmap", name, "-n", "observability"], check=True)
            removed.append(name)
    active = get("instrumentation", "datadog-otel", "-n", "apps")
    assert "fake-datadog" not in json.dumps(active["spec"])
    record = {"timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat(), "context": args.context,
              "sdk_otlp_target": endpoint, "product_proxy_target": proxy_host, "apps_restarted": selected,
              "fake_deployment_service_removed": True, "old_mock_configmaps_removed": removed,
              "backend_site": "us5.datadoghq.com", "credential_reference": "ddot-poc/datadog-api-key:api-key",
              "preserved": "PostgreSQL and recommender are existing independent experiment state, unused by this PoC"}
    (args.backup / "result.json").write_text(json.dumps(record, indent=2) + "\n")
    print(json.dumps(record, indent=2))


if __name__ == "__main__":
    main()
