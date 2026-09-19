# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Harmless local scanner fixture instrumented by the real Datadog Python WAF."""

import argparse
import json
import os
import threading
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--iterations", type=int, default=0)
    parser.add_argument("--interval", type=float, default=30)
    parser.add_argument("--port", type=int, default=8080)
    args = parser.parse_args()
    defaults = {
        "DD_SERVICE": "ddot-appsec-python",
        "DD_ENV": "ddot-poc",
        "DD_VERSION": "otlp-poc-1",
        "DD_APPSEC_ENABLED": "false",
        "DD_IAST_ENABLED": "false",
        "DD_REMOTE_CONFIGURATION_ENABLED": "false",
        "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false",
        "DD_RUNTIME_METRICS_ENABLED": "false",
        "DD_PROFILING_ENABLED": "false",
        "DD_DATA_STREAMS_ENABLED": "false",
        "DD_LLMOBS_ENABLED": "false",
        "DD_DYNAMIC_INSTRUMENTATION_ENABLED": "false",
        "DD_TRACE_STARTUP_LOGS": "false",
        "OTEL_TRACES_EXPORTER": "otlp",
        "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/json",
        "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector.ddot-poc.svc.cluster.local:4318/v1/traces",
    }
    for key, value in defaults.items():
        os.environ.setdefault(key, value)
    if os.environ["OTEL_TRACES_EXPORTER"] != "otlp":
        raise ValueError("This workload requires the Datadog SDK OTLP trace exporter")
    if os.environ.get("DD_TRACE_API_VERSION") or os.environ.get("DD_TRACE_AGENT_PROTOCOL_VERSION"):
        raise ValueError("A native trace protocol override would disable OTLP export")

    import ddtrace.auto  # noqa: F401 -- bootstrap before importing Flask
    from ddtrace import tracer
    from flask import Flask

    app = Flask(__name__)

    @app.get("/health")
    def health():
        return {"status": "ready"}

    @app.get("/fixture")
    def fixture():
        return {"message": "Harmless local WAF fixture"}

    def exercise():
        with app.test_client() as client:
            for user_agent in ("ddot-benign-fixture", "dd-test-scanner-log"):
                response = client.get("/fixture", headers={"User-Agent": user_agent}, buffered=True)
                print(json.dumps({"fixture": user_agent, "status": response.status_code,
                                  "appsec_enabled": os.environ["DD_APPSEC_ENABLED"]}), flush=True)
                response.close()

    if args.iterations:
        for _ in range(args.iterations):
            exercise()
        tracer.shutdown()
        return

    def generate():
        while True:
            exercise()
            time.sleep(args.interval)

    threading.Thread(target=generate, daemon=True).start()
    app.run(host="0.0.0.0", port=args.port, use_reloader=False)


if __name__ == "__main__":
    main()
