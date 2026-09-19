#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Check real kind query/OTLP correlation. Does not claim Datadog product readback."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import subprocess

KUBE = ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc"]


def output(*args):
    return subprocess.check_output(KUBE + list(args), text=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--collector", default="deployment/collector")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    events = []
    for line in output("logs", "deployment/dbm-python", "--since=10m").splitlines():
        try:
            events.append(json.loads(line))
        except ValueError:
            pass
    parents = {event["traceparent"] for event in events if event.get("traceparent")}
    assert parents, "No actual SDK SQL context in workload logs"
    logs = output("logs", args.collector, "--since=10m", "--tail=50000")
    # Detailed debug exporter output separates each emitted span and LogRecord.
    records = re.split(r"(?=\b(?:LogRecord|Span) #\d+)", logs)
    query_pairs = set()
    span_pairs = set()
    for record in records:
        trace_id = re.search(r"Trace ID\s*:\s*([0-9a-f]{32})", record)
        span_id = re.search(r"Span ID\s*:\s*([0-9a-f]{16})", record)
        if not trace_id or not span_id:
            continue
        pair = (trace_id.group(1), span_id.group(1))
        if record.startswith("LogRecord") and "db.server.query_sample" in record:
            query_pairs.add(pair)
        if record.startswith("Span") and ("postgres.query" in record or "db.system" in record):
            span_pairs.add(pair)
    expected = {(parent.split("-")[1], parent.split("-")[2]) for parent in parents}
    matched = expected & query_pairs & span_pairs
    assert matched, {"sql_contexts": len(expected), "query_log_contexts": len(query_pairs), "db_spans": len(span_pairs)}
    assert "postgresql.backends" in logs, "No real PostgreSQL metric in collector output"
    assert "db.server.top_query" in logs, "No real top query log in collector output"
    result = {
        "timestamp": datetime.now(timezone.utc).isoformat(), "context": "kind-otel-dd", "namespace": "ddot-poc",
        "sdk": "ddtrace 4.13.0rc1", "driver": "psycopg2 2.9.11", "database": "PostgreSQL 16.15",
        "sdk_sql_contexts_observed": len(expected), "query_samples_with_ids": len(query_pairs),
        "otlp_db_span_contexts": len(span_pairs), "matched_query_log_and_otlp_span_contexts": len(matched),
        "example_matches": [{"trace_id": trace_id, "span_id": span_id} for trace_id, span_id in sorted(matched)[:3]],
        "postgresql_metrics": True, "top_queries": True,
        "backend_product_verified": False,
        "backend_blocker": "Standard OTLP query logs/metrics are not Datadog DBM event-platform payloads; no supported mapping or DBM UI/API readback is available.",
    }
    rendered = json.dumps(result, indent=2) + "\n"
    if args.output:
        args.output.write_text(rendered)
    print(rendered)


if __name__ == "__main__":
    main()
