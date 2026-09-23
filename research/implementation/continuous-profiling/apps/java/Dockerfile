# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM eclipse-temurin:21.0.8_9-jdk-jammy AS build
WORKDIR /app
ADD https://repo.maven.apache.org/maven2/com/datadoghq/dd-java-agent/1.66.0/dd-java-agent-1.66.0.jar /app/dd-java-agent.jar
RUN echo '5f0eb51160fade367d97404624561b6666f7475fb1453a7a73237eb643e398d8  dd-java-agent.jar' | sha256sum -c - && chmod 644 dd-java-agent.jar
COPY ProfileWorkload.java .
RUN javac ProfileWorkload.java
FROM eclipse-temurin:21.0.8_9-jre-jammy
WORKDIR /app
COPY --from=build /app/dd-java-agent.jar /app/ProfileWorkload.class ./
ENV DD_SERVICE=ddot-profile-java DD_ENV=poc DD_VERSION=1 \
    DD_PROFILING_ENABLED=true DD_PROFILING_START_DELAY=1 DD_PROFILING_UPLOAD_PERIOD=10 \
    DD_TRACE_ENABLED=false DD_TRACE_OTEL_EXPORTER=otlp \
    DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIG_ENABLED=false \
    DD_RUNTIME_METRICS_ENABLED=false DD_TRACE_STARTUP_LOGS=false
USER 65532:65532
ENTRYPOINT ["java", "-javaagent:/app/dd-java-agent.jar", "-cp", "/app", "ProfileWorkload"]
