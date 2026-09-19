# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Synthetic application function instrumented by the SDK's real local probe."""
def calculate(value):
    doubled = value * 2
    return {"input": value, "doubled": doubled}
