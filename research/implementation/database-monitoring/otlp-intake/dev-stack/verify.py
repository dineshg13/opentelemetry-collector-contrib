#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Check the running dev stack and read back only its synthetic telemetry."""
import argparse
import base64
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

from deploy import NAMESPACE, env_values


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", type=Path, required=True)
    parser.add_argument("--context", default="kind-otel-dd")
    parser.add_argument("--output", type=Path, default=Path("/tmp/dbm-otlp-dev-verification.json"))
    args = parser.parse_args()
    values = env_values(args.env_file)
    kube = ["kubectl", "--context", args.context, "-n", NAMESPACE]

    def output(*argv):
        return subprocess.check_output(kube + list(argv), text=True)

    secret = json.loads(output("get", "secret", "datadog", "-o", "json"))
    matches = all(base64.b64decode(secret["data"][key]).decode() == values[key] for key in ["DD_API_KEY", "DD_SITE"])
    assert matches, "Kubernetes intake credentials differ from the requested env file"
    pods = json.loads(output("get", "pods", "-o", "json"))["items"]
    collector_versions = [p["metadata"].get("annotations", {}).get("credentials-resource-version") for p in pods if p["metadata"]["labels"].get("app") == "collector" and not p["metadata"].get("deletionTimestamp")]
    credentials_current = bool(collector_versions) and all(version == secret["metadata"]["resourceVersion"] for version in collector_versions)
    assert credentials_current, "Collector must roll out after the intake Secret changes"
    ready = {p["metadata"]["labels"]["app"]: all(c["ready"] for c in p["status"].get("containerStatuses", [])) and bool(p["status"].get("containerStatuses")) for p in pods if not p["metadata"].get("deletionTimestamp")}
    assert ready == {"postgres": True, "collector": True, "workload": True}, ready
    health, metrics = json.loads(output("exec", "deployment/workload", "--", "python", "-c", 'import json,urllib.request; print(json.dumps([json.load(urllib.request.urlopen("http://127.0.0.1:8080/readyz")),urllib.request.urlopen("http://collector:8888/metrics").read().decode()]))'))
    counters = {}
    for line in metrics.splitlines():
        if 'exporter="otlp_http/intake"' not in line:
            continue
        if line.startswith(("otelcol_exporter_sent_", "otelcol_exporter_send_failed_", "otelcol_exporter_enqueue_failed_", "otelcol_exporter_queue_size")):
            name = line.split("{", 1)[0]
            counters[name] = counters.get(name, 0) + float(line.rsplit(" ", 1)[1])
    for signal in ["log_records", "metric_points", "spans"]:
        assert counters.get("otelcol_exporter_sent_" + signal, 0) > 0, signal
    for name, value in counters.items():
        if "failed" in name:
            assert value == 0, (name, value)
    assert health["ready"] and health["trace_contexts"] > 0, health

    headers = {"DD-API-KEY": values["DD_API_KEY"], "DD-APPLICATION-KEY": values["DD_APP_KEY"], "Content-Type": "application/json"}

    def api(path, body=None):
        request = urllib.request.Request("https://api." + values["DD_SITE"] + path, data=json.dumps(body).encode() if body else None, headers=headers)
        try:
            with urllib.request.urlopen(request, timeout=20) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as error:
            return error.code, {}

    logs_status, logs = api("/api/v2/logs/events/search", {"filter": {"from": "now-15m", "to": "now", "query": "service:dbm-otlp-dev-postgres"}, "page": {"limit": 100}})
    counts = {}
    plans = 0
    session_contexts = set()
    for event in logs.get("data", []):
        attrs = event["attributes"].get("attributes", {})
        name = attrs.get("otel", {}).get("event_name", "unknown")
        counts[name] = counts.get(name, 0) + 1
        plans += sum(bool(q.get("postgresql", {}).get("query_plan")) for q in attrs.get("queries", []))
        for session in attrs.get("sessions", []):
            if session.get("trace_id") and session.get("span_id"):
                session_contexts.add((session["trace_id"], session["span_id"]))
    assert logs_status == 200 and all(counts.get(name, 0) for name in ["db.server.activity", "db.server.query_metrics"]), (logs_status, counts)
    source_contexts = set()
    for line in output("logs", "deployment/workload", "--since=15m").splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        parent = event.get("traceparent", "")
        if isinstance(parent, str) and re.fullmatch(r"00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}", parent):
            source_contexts.add(tuple(parent.split("-")[1:3]))

    query = urllib.parse.urlencode({"from": int(time.time()) - 900, "to": int(time.time()), "query": "avg:postgresql.backends{service:dbm-otlp-dev-postgres}"})
    metrics_status, metric_data = api("/api/v1/query?" + query)
    points = sum(sum(point[1] is not None for point in series.get("pointlist", [])) for series in metric_data.get("series", []))
    assert metrics_status == 200 and points > 0, (metrics_status, points)
    spans_status, spans = api("/api/v2/spans/events/search", {"data": {"type": "search_request", "attributes": {"filter": {"from": "now-15m", "to": "now", "query": "service:dbm-otlp-dev-workload"}, "page": {"limit": 5}}}})
    result = {
        "timestamp": datetime.now(timezone.utc).isoformat(), "context": args.context, "namespace": NAMESPACE,
        "ready": ready, "api_key_and_site_match_env_file": matches,
        "collector_uses_current_secret_version": credentials_current,
        "workload": health, "collector_export_counters": counters,
        "datadog_logs": {"http_status": logs_status, "records_by_event": counts, "query_rows_with_plans": plans, "sdk_context_matches_in_activity_logs": len(source_contexts & session_contexts)},
        "datadog_metrics": {"http_status": metrics_status, "series": len(metric_data.get("series", [])), "points": points},
        "datadog_indexed_spans": {"http_status": spans_status, "records": len(spans.get("data", []))},
        "dbm_product_verified": False, "backend_deployment_performed": False,
        "scope": "Real OTLP export and ordinary Datadog telemetry readback; DBM routing, private intake and UI remain unverified.",
    }
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
