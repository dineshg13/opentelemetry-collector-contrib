# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real OpenLineage SDK job events plus ordinary Datadog SDK OTLP traces."""
from datetime import datetime, timezone
import importlib.metadata
import json
import os
from pathlib import Path
import tempfile
import uuid

import ddtrace
from openlineage.client.event_v2 import InputDataset, Job, OutputDataset, Run, RunEvent, RunState
from openlineage.client.transport.http import HttpConfig, HttpTransport
import requests

if getattr(ddtrace.tracer._span_aggregator.writer, "_otlp_endpoint", None) != os.environ["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]:
    raise RuntimeError("The Datadog SDK must use its actual OTLP writer")
enabled = os.environ.get("DJM_LINEAGE_ENABLED", "true").lower() == "true"
run_id = str(uuid.uuid4())
transport = None
if enabled:
    session = requests.Session()
    session.trust_env = False
    config = HttpConfig.from_dict({"url": os.environ["OPENLINEAGE_URL"],
        "endpoint": "openlineage/api/v1/lineage", "compression": "gzip", "timeout": 10,
        "retry": {"total": 0, "status_forcelist": []}})
    config.session = session
    transport = HttpTransport(config)
statuses = []
errors = []


def emit(state):
    if not transport:
        return
    event = RunEvent(eventTime=datetime.now(timezone.utc).isoformat(), eventType=state,
        producer="https://github.com/dineshg13/opentelemetry-collector-contrib",
        run=Run(runId=run_id), job=Job(namespace="ddot-poc", name="orders-summary"),
        inputs=[InputDataset(namespace="file:ddot-poc", name="orders.csv")],
        outputs=[OutputDataset(namespace="file:ddot-poc", name="orders-summary.json")])
    try:
        response = transport.emit(event)
        statuses.append(response.status_code)
    except requests.RequestException as error:
        status = error.response.status_code if error.response is not None else None
        statuses.append(status)
        errors.append({"type": type(error).__name__, "status": status})


try:
    with ddtrace.tracer.trace("job.orders-summary", resource="orders-summary") as span:
        span.set_tag("job.name", "orders-summary")
        span.set_tag("job.run.id", run_id)
        span.set_tag("poc.instrumentation", "manual-openlineage-and-ddtrace")
        emit(RunState.START)
        with tempfile.TemporaryDirectory(prefix="ddot-orders-") as directory:
            source = Path(directory) / "orders.csv"
            source.write_text("amount\n10\n20\n30\n")
            amounts = [int(value) for value in source.read_text().splitlines()[1:]]
            summary = {"count": len(amounts), "total": sum(amounts)}
            (Path(directory) / "orders-summary.json").write_text(json.dumps(summary))
        span.set_metric("job.records", summary["count"])
        emit(RunState.COMPLETE)
        trace_id = format(span.trace_id, "032x")
finally:
    if transport:
        transport.close()
    ddtrace.tracer.shutdown()
print(json.dumps({"run_id": run_id, "trace_id": trace_id, "summary": summary,
    "lineage_enabled": enabled, "lineage_statuses": statuses, "lineage_errors": errors,
    "ddtrace_version": ddtrace.__version__, "openlineage_version": importlib.metadata.version("openlineage-python"),
    "scope": "manual OpenLineage job plus ordinary SDK trace; not native Spark DJM"}), flush=True)
if errors and os.environ.get("ALLOW_LINEAGE_FAILURE", "false").lower() != "true":
    raise SystemExit(1)
