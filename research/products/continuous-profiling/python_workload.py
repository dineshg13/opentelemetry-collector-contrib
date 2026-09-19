# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

"""Collect real native Python profiles; uploads happen on profiler stop."""
import json
import platform
import time

import ddtrace
from ddtrace.profiling import Profiler


profiler = Profiler()
profiler.start()
deadline = time.monotonic() + 2
checksum = 0
while time.monotonic() < deadline:
    checksum += sum(n * n for n in range(10000))
profiler.stop()
print(json.dumps({"sdk": ddtrace.__version__, "python": platform.python_version(),
                  "machine": platform.machine(), "checksum_positive": checksum > 0}))
