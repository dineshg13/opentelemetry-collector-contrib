#!/usr/bin/env bash
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
fixture_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
runtime_dir="${1:-/tmp/ddot-sdk-signals-runtime}"
mkdir -p "$runtime_dir/python" "$runtime_dir/node" "$runtime_dir/java"
uv pip install --python python3.12 --target "$runtime_dir/python" -r "$fixture_dir/requirements.txt"
cp "$fixture_dir/package.json" "$fixture_dir/package-lock.json" "$runtime_dir/node/"
npm ci --prefix "$runtime_dir/node" --no-audit --no-fund
curl --fail --location --output "$runtime_dir/java/dd-java-agent-1.66.0.jar" \
  https://repo.maven.apache.org/maven2/com/datadoghq/dd-java-agent/1.66.0/dd-java-agent-1.66.0.jar
for artifact in api context; do
  curl --fail --location --output "$runtime_dir/java/opentelemetry-$artifact-1.47.0.jar" \
    "https://repo.maven.apache.org/maven2/io/opentelemetry/opentelemetry-$artifact/1.47.0/opentelemetry-$artifact-1.47.0.jar"
done
(cd "$runtime_dir/java" && sha256sum --check "$fixture_dir/java-sha256.txt")
