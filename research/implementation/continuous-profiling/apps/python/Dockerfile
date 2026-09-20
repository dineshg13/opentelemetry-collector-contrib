# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM python:3.12.13-slim-bookworm
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir --require-hashes -r requirements.txt
COPY app.py .
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    DD_SERVICE=ddot-profile-python DD_ENV=poc DD_VERSION=1 \
    DD_PROFILING_ENABLED=true DD_TRACE_ENABLED=false OTEL_TRACES_EXPORTER=otlp \
    DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIGURATION_ENABLED=false \
    DD_RUNTIME_METRICS_ENABLED=false DD_TRACE_STARTUP_LOGS=false
USER 65532:65532
ENTRYPOINT ["python", "/app/app.py"]
