# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM eclipse-temurin:21.0.8_9-jdk-jammy
WORKDIR /app
ADD https://repo.maven.apache.org/maven2/com/datadoghq/dd-java-agent/1.66.0/dd-java-agent-1.66.0.jar /app/dd-java-agent.jar
RUN echo "5f0eb51160fade367d97404624561b6666f7475fb1453a7a73237eb643e398d8  /app/dd-java-agent.jar" | sha256sum -c -
COPY LlmWorkload.java .
RUN javac -cp dd-java-agent.jar LlmWorkload.java && chmod -R a+rX /app
ENV DD_SERVICE=ddot-llm-java DD_ENV=ddot-poc \
    DD_TRACE_OTEL_ENABLED=true OTEL_TRACES_EXPORTER=otlp \
    OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/json \
    DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIGURATION_ENABLED=false \
    DD_RUNTIME_METRICS_ENABLED=false DD_CODE_ORIGIN_FOR_SPANS_ENABLED=false
USER 65532:65532
ENTRYPOINT ["java", "-javaagent:/app/dd-java-agent.jar", "-cp", "/app:/app/dd-java-agent.jar", "LlmWorkload"]
