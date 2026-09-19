#!/usr/bin/env bash
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
cd "$(dirname "$0")"
for language in python java node; do
  docker build --platform linux/arm64 -t "ddot-profile-${language}:poc" "apps/${language}"
done
