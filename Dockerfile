FROM golang:1.21 AS build
WORKDIR /src
COPY . /src
RUN make otelcontribcol

FROM debian:bookworm-slim
USER root

RUN apt-get update && \
apt-get -y install default-jre-headless --no-install-recommends && \
apt-get clean && \ 
rm -rf /var/lib/apt/lists/*

ADD https://github.com/open-telemetry/opentelemetry-java-contrib/releases/download/v1.27.0/opentelemetry-jmx-metrics.jar /opt/opentelemetry-jmx-metrics.jar

COPY --from=build /src/bin/otelcontribcol_* /otelcol-contrib
USER 1001
ENTRYPOINT ["/otelcol-contrib"]
EXPOSE 4317 4318 55680 55679
