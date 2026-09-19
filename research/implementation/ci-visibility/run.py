# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Run real pytest instrumentation, native CI products and ordinary OTLP traces."""
import os
from pathlib import Path
import subprocess
import sys


def main():
    defaults = {
        "DD_SERVICE": "ddot-ci-python", "DD_ENV": "ddot-poc",
        "DD_CIVISIBILITY_ENABLED": "true", "DD_CIVISIBILITY_AGENTLESS_ENABLED": "false",
        "DD_TRACE_ENABLED": "true", "DD_PYTEST_USE_NEW_PLUGIN": "true",
        # Existing private SDK switch: otherwise the pytest plugin diverts ordinary
        # application spans into its CI event writer, bypassing OTLP entirely.
        "_DD_CIVISIBILITY_USE_CI_CONTEXT_PROVIDER": "true",
        "DD_TRACE_AGENT_URL": "http://collector.ddot-poc.svc.cluster.local:8126",
        "OTEL_TRACES_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/json",
        "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector.ddot-poc.svc.cluster.local:4318/v1/traces",
        "DD_INSTRUMENTATION_TELEMETRY_ENABLED": "false", "DD_REMOTE_CONFIGURATION_ENABLED": "false",
        "DD_RUNTIME_METRICS_ENABLED": "false", "DD_PROFILING_ENABLED": "false",
        "DD_DYNAMIC_INSTRUMENTATION_ENABLED": "false", "DD_CODE_ORIGIN_FOR_SPANS_ENABLED": "false",
        "DD_LOGS_INJECTION": "false", "DD_AGENTLESS_LOG_SUBMISSION_ENABLED": "false",
        "DD_TRACE_STARTUP_LOGS": "false", "PYTEST_DISABLE_PLUGIN_AUTOLOAD": "1",
        "DD_GIT_REPOSITORY_URL": "https://example.invalid/ddot-ci-fixture.git",
        "DD_GIT_BRANCH": "ddot-poc",
    }
    for key, value in defaults.items():
        os.environ.setdefault(key, value)
    if os.environ["OTEL_TRACES_EXPORTER"] != "otlp" or os.environ.get("DD_TRACE_AGENT_PROTOCOL_VERSION"):
        raise ValueError("Ordinary traces must use the Datadog SDK OTLP exporter")
    root = Path(__file__).resolve().parent
    return subprocess.call([sys.executable, "-m", "pytest", "-p", "ddtrace.testing.internal.pytest.entry_point",
                            "--ddtrace", "-q", str(root / "test_workload.py")], cwd=root)


if __name__ == "__main__":
    raise SystemExit(main())
