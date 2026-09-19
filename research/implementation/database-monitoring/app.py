#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real Datadog psycopg instrumentation: SQL propagation plus OTLP trace export."""
import json
import os
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

import ddtrace.auto  # noqa: F401
import ddtrace
from ddtrace import tracer
import psycopg2

ready = False


class Health(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200 if ready else 503)
        self.end_headers()
        self.wfile.write(b"ready" if ready else b"connecting")

    def log_message(self, *args):
        pass


def main():
    global ready
    threading.Thread(target=HTTPServer(("0.0.0.0", 8080), Health).serve_forever, daemon=True).start()
    connection = psycopg2.connect(
        host=os.getenv("DB_HOST", "dbm-postgres"), port=5432, dbname="dbm",
        user="dbm_app", password=os.environ["DB_PASSWORD"], application_name="ddot-dbm-python",
    )
    connection.autocommit = True
    ready = True
    print(json.dumps({"event": "ready", "ddtrace": ddtrace.__version__,
                      "protocol": os.environ.get("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"),
                      "propagation_mode": os.environ.get("DD_DBM_PROPAGATION_MODE")}), flush=True)
    count = int(os.getenv("ITERATIONS", "0"))
    iteration = 0
    while not count or iteration < count:
        with tracer.trace("dbm.workload", service="ddot-dbm-python"):
            with connection.cursor() as cursor:
                # Slow enough for the receiver's 2-second query sampling interval.
                cursor.execute("SELECT pg_sleep(3), current_database()")
                assert cursor.fetchone()[1] == "dbm"
                query = cursor.query.decode()
                match = re.search(r"traceparent='([0-9a-f-]+)'", query)
                actual = match.group(1) if match else None
                expected = os.environ.get("DD_DBM_PROPAGATION_MODE") == "full"
                assert bool(actual) == expected, (expected, query)
                print(json.dumps({"event": "query", "iteration": iteration,
                                  "traceparent": actual, "propagation_expected": expected}), flush=True)
        tracer.flush()
        iteration += 1
        time.sleep(1)
    connection.close()
    tracer.shutdown()


if __name__ == "__main__":
    main()
