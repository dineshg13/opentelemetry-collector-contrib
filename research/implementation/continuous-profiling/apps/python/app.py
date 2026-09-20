# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Long-lived native profiler with a named, recognizable CPU stack."""
import contextlib
import json
import os
import signal
import time
from pathlib import Path

import ddtrace
from ddtrace.profiling import Profiler


def profile_hot_loop():
    return sum(value * value for value in range(40000))


def main():
    running = True

    def stop(*_):
        nonlocal running
        running = False

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    profiling = os.getenv("DD_PROFILING_ENABLED", "true").lower() == "true"
    tracing = os.getenv("DD_TRACE_ENABLED", "false").lower() == "true"
    if tracing and os.getenv("OTEL_TRACES_EXPORTER") != "otlp":
        raise RuntimeError("This application only permits OTLP trace export")
    profiler = Profiler() if profiling else None
    if profiler:
        profiler.start()
    print(json.dumps({"language": "python", "sdk": ddtrace.__version__,
                      "profiling": profiling, "otlp_tracing": tracing}), flush=True)
    Path("/tmp/ready").touch()
    duration = float(os.getenv("WORKLOAD_SECONDS", "0"))
    deadline = time.monotonic() + duration if duration else float("inf")
    checksum = 0
    while running and time.monotonic() < deadline:
        scope = ddtrace.tracer.trace("profile.hot_loop") if tracing else contextlib.nullcontext()
        with scope:
            for _ in range(40):
                checksum += profile_hot_loop()
        time.sleep(0.02)
    if profiler:
        profiler.stop()
    ddtrace.tracer.shutdown()
    print(json.dumps({"complete": True, "checksum_positive": checksum > 0}), flush=True)


if __name__ == "__main__":
    main()
