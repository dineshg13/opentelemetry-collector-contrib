# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
import ddtrace.auto  # noqa: F401 -- Datadog configures the providers/exporters.
import json
from ddtrace import tracer
from opentelemetry import metrics
from opentelemetry import _logs
from opentelemetry._logs import SeverityNumber

counter = metrics.get_meter("ddot-signals").create_counter("poc.sdk.counter")
with tracer.trace("poc.sdk.operation") as span:
    span.set_tag("poc.language", "python")
    counter.add(3, {"poc.language": "python"})
    _logs.get_logger("ddot-signals").emit(body="ddot-signals-python", severity_number=SeverityNumber.INFO,
                                         attributes={"poc.language": "python"})
    print(json.dumps({"language": "python", "trace_id": f"{span.trace_id:032x}", "span_id": f"{span.span_id:016x}"}))
tracer.shutdown()
for provider in (metrics.get_meter_provider(), _logs.get_logger_provider()):
    flush = getattr(provider, "force_flush", None)
    if flush:
        flush()
print("python: executed trace, log and counter API calls")
