# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Representative product workload, including ordinary trace export independently."""
import pytest
from ddtrace import tracer


def add(left, right):
    return left + right


def test_addition():
    with tracer.trace("poc.ci.ordinary-operation") as span:
        span.set_tag("poc.workload", "ci-ordinary-trace")
        assert add(2, 3) == 5


@pytest.mark.skip(reason="Representative skipped test")
def test_skipped():
    raise AssertionError("pytest must not run the skipped test")
