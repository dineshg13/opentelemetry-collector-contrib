# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM node:24.18.0-bookworm-slim
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY app.cjs .
ENV DD_SERVICE=ddot-dsm-node DD_ENV=ddot-poc DD_VERSION=1 \
    DD_DATA_STREAMS_ENABLED=true DD_TRACE_ENABLED=true OTEL_TRACES_EXPORTER=otlp \
    OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/json DD_PROFILING_ENABLED=false \
    DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIGURATION_ENABLED=false \
    DD_RUNTIME_METRICS_ENABLED=false DD_TRACE_STARTUP_LOGS=false
USER 65532:65532
ENTRYPOINT ["node", "/app/app.cjs"]
