# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM node:24.18.0-bookworm-slim
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY app.cjs .
ENV DD_SERVICE=ddot-profile-node DD_ENV=poc DD_VERSION=1 \
    DD_PROFILING_ENABLED=true DD_PROFILING_UPLOAD_PERIOD=10 \
    DD_TRACE_ENABLED=false OTEL_TRACES_EXPORTER=otlp \
    DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIGURATION_ENABLED=false \
    DD_RUNTIME_METRICS_ENABLED=false DD_TRACE_STARTUP_LOGS=false
USER 65532:65532
ENTRYPOINT ["node", "/app/app.cjs"]
