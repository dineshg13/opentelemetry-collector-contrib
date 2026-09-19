# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Datadog SDK manual GenAI workload, with OTLP or separate native LLM transport."""

import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
import signal
import threading
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mode", choices=("otlp", "native"), default="otlp")
    parser.add_argument("--iterations", type=int, default=0, help="0 runs until SIGTERM")
    parser.add_argument("--interval", type=float, default=10)
    parser.add_argument("--health-port", type=int, default=8080)
    args = parser.parse_args()
    os.environ.setdefault("DD_SERVICE", "ddot-llm-python")
    os.environ.setdefault("DD_ENV", "ddot-poc")
    os.environ.setdefault("DD_REMOTE_CONFIGURATION_ENABLED", "false")
    os.environ.setdefault("DD_INSTRUMENTATION_TELEMETRY_ENABLED", "false")
    os.environ.setdefault("DD_RUNTIME_METRICS_ENABLED", "false")
    os.environ.setdefault("DD_TRACE_STARTUP_LOGS", "false")
    if args.mode == "otlp":
        os.environ["DD_LLMOBS_ENABLED"] = "false"
        os.environ["OTEL_TRACES_EXPORTER"] = "otlp"
        os.environ.setdefault("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/json")
        if not os.environ.get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"):
            parser.error("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT must name the Collector /v1/traces endpoint")
        if os.environ.get("DD_TRACE_AGENT_PROTOCOL_VERSION"):
            parser.error("DD_TRACE_AGENT_PROTOCOL_VERSION disables the SDK OTLP exporter")
    else:
        # Native LLM events are a separate product protocol; ordinary APM is disabled.
        os.environ["DD_TRACE_ENABLED"] = "false"
        os.environ["DD_APM_TRACING_ENABLED"] = "false"
        os.environ["DD_LLMOBS_ENABLED"] = "true"
        os.environ["DD_LLMOBS_AGENTLESS_ENABLED"] = "false"
        os.environ.setdefault("DD_LLMOBS_ML_APP", "ddot-llm-python-native")
        if not os.environ.get("DD_TRACE_AGENT_URL"):
            parser.error("DD_TRACE_AGENT_URL must name the Collector product proxy")

    import ddtrace

    if args.mode == "otlp":
        # Fail visibly on an older SDK instead of silently emitting native APM traces.
        writer = ddtrace.tracer._span_aggregator.writer
        if getattr(writer, "_otlp_endpoint", None) != os.environ["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]:
            raise RuntimeError("This ddtrace runtime did not select its native OTLP writer")
    else:
        from ddtrace.llmobs import LLMObs

        LLMObs.enable(agentless_enabled=False, integrations_enabled=False)

    stopped = threading.Event()
    signal.signal(signal.SIGTERM, lambda *_: stopped.set())
    signal.signal(signal.SIGINT, lambda *_: stopped.set())
    state = {"mode": args.mode, "sdk": "ddtrace", "version": ddtrace.__version__, "iterations": 0}

    class Health(BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200 if self.path == "/health" else 404)
            self.end_headers()
            self.wfile.write(json.dumps(state).encode())

        def log_message(self, *_):
            pass

    health = None
    if args.health_port:
        health = ThreadingHTTPServer(("0.0.0.0", args.health_port), Health)
        threading.Thread(target=health.serve_forever, daemon=True).start()
    try:
        while not stopped.is_set():
            if args.mode == "otlp":
                with ddtrace.tracer.trace("chat deterministic-poc", resource="chat deterministic-poc") as span:
                    for key, value in {
                        "gen_ai.operation.name": "chat",
                        "gen_ai.provider.name": "custom",
                        "gen_ai.request.model": "deterministic-poc",
                        "gen_ai.response.model": "deterministic-poc",
                        "gen_ai.conversation.id": "ddot-llm-python",
                        "gen_ai.input.messages": json.dumps([{"role": "user", "content": "Return the word ready"}]),
                        "gen_ai.output.messages": json.dumps([{"role": "assistant", "content": "ready"}]),
                        "poc.workload": "deterministic-manual-instrumentation",
                    }.items():
                        span.set_tag(key, value)
                    span.set_metric("gen_ai.usage.input_tokens", 4)
                    span.set_metric("gen_ai.usage.output_tokens", 1)
                    trace_id = format(span.trace_id, "032x")
                    span_id = format(span.span_id, "016x")
                ddtrace.tracer.flush()
            else:
                with LLMObs.llm(name="chat deterministic-poc", model_name="deterministic-poc", model_provider="custom") as span:
                    LLMObs.annotate(
                        input_data=[{"role": "user", "content": "Return the word ready"}],
                        output_data=[{"role": "assistant", "content": "ready"}],
                        metrics={"input_tokens": 4, "output_tokens": 1},
                    )
                    context = LLMObs.export_span(span)
                    trace_id, span_id = context["trace_id"], context["span_id"]
                LLMObs.submit_evaluation(label="deterministic-match", metric_type="score", value=1, span=context)
                LLMObs.flush()
            state["iterations"] += 1
            print(json.dumps({**state, "trace_id": trace_id, "span_id": span_id, "generated_at_unix": time.time()}), flush=True)
            if args.iterations and state["iterations"] >= args.iterations:
                break
            stopped.wait(args.interval)
    finally:
        if health:
            health.shutdown()
        if args.mode == "native":
            LLMObs.disable()
        ddtrace.tracer.shutdown()


if __name__ == "__main__":
    main()
