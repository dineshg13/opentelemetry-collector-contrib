# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real Datadog checkpoints with OTLP application traces; no broker emulation."""
import json
import os
import time

import ddtrace
from ddtrace.data_streams import set_consume_checkpoint, set_produce_checkpoint
from ddtrace.internal.datastreams import data_streams_processor

if getattr(ddtrace.tracer._span_aggregator.writer, "_otlp_endpoint", None) != os.environ["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]:
    raise RuntimeError("The Datadog SDK must use its OTLP trace writer")

for sequence in range(int(os.environ.get("WORKLOAD_ITERATIONS", "3"))):
    carrier = {}
    with ddtrace.tracer.trace("dsm.produce", resource="poc-orders") as span:
        span.set_tag("messaging.system", "kafka")
        span.set_tag("messaging.destination.name", "poc-orders")
        span.set_tag("messaging.operation.name", "send")
        produced = set_produce_checkpoint("kafka", "poc-orders", carrier.__setitem__)
    time.sleep(0.05)
    with ddtrace.tracer.trace("dsm.consume", resource="poc-orders") as span:
        span.set_tag("messaging.system", "kafka")
        span.set_tag("messaging.destination.name", "poc-orders")
        span.set_tag("messaging.operation.name", "process")
        consumed = set_consume_checkpoint("kafka", "poc-orders", carrier.get)
    print(json.dumps({"sdk": "ddtrace", "version": ddtrace.__version__, "sequence": sequence,
                      "producer_hash": produced.hash if produced else None,
                      "consumer_hash": consumed.hash if consumed else None,
                      "propagation": carrier, "workload": "manual-in-process-checkpoints"}), flush=True)
    time.sleep(0.1)

processor = data_streams_processor()
if processor:
    # These are explicitly manual offset samples, not real broker observations.
    now = time.time()
    processor.track_kafka_produce("poc-orders", 0, 12, now)
    processor.track_kafka_produce("poc-orders", 0, 10, now)
    processor.track_kafka_commit("poc-consumer", "poc-orders", 0, 8, now)
    # The SDK owns serialization/compression/HTTP; deterministic test flush only.
    processor.periodic()
ddtrace.tracer.shutdown()
