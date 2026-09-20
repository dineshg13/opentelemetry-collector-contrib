# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM python:3.12.13-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py .
ENV DD_SERVICE=ddot-djm-python DD_ENV=ddot-poc DD_VERSION=1 \
    DD_TRACE_ENABLED=true OTEL_TRACES_EXPORTER=otlp OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/json \
    DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIGURATION_ENABLED=false \
    DD_RUNTIME_METRICS_ENABLED=false DD_TRACE_STARTUP_LOGS=false
USER 65532:65532
ENTRYPOINT ["python", "-u", "/app/app.py"]
