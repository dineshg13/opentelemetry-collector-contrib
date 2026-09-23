#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Persistent DBM workload with real SQL trace propagation and OTLP traces."""
import json
import os
import re
import signal
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import ddtrace.auto  # noqa: F401
import ddtrace
from ddtrace import tracer
import psycopg2

SERVICE = os.getenv("DD_SERVICE", "dbm-otlp-dev-workload")
DATABASE = os.getenv("DB_NAME", "dbm")
STOP = threading.Event()
LOCK = threading.Lock()
STATUS = {
    "service": SERVICE,
    "ready": False,
    "iterations": 0,
    "rows_returned": 0,
    "trace_contexts": 0,
    "connection_attempts": 0,
    "errors": 0,
    "last_query_unix_seconds": None,
    "last_error_type": None,
}


class Health(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path not in ("/", "/healthz", "/readyz"):
            self.send_error(404)
            return
        with LOCK:
            status = dict(STATUS)
        payload = json.dumps(status).encode()
        self.send_response(200 if status["ready"] or self.path == "/healthz" else 503)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass


def connect(suffix):
    connection = psycopg2.connect(
        host=os.getenv("DB_HOST", "postgres"),
        port=int(os.getenv("DB_PORT", "5432")),
        dbname=DATABASE,
        user=os.getenv("DB_USER", "dbm_app"),
        password=os.environ["DB_PASSWORD"],
        application_name=f"{SERVICE}-{suffix}",
        connect_timeout=5,
    )
    connection.autocommit = True
    return connection


def run_iteration(connection, sleep_seconds):
    with tracer.trace("dbm.workload", service=SERVICE) as span:
        with connection.cursor() as cursor:
            cursor.execute("SELECT pg_sleep(%s), current_database()", (sleep_seconds,))
            assert cursor.fetchone()[1] == DATABASE
            query = cursor.query.decode()
            match = re.search(r"traceparent='([0-9a-f-]+)'", query)
            context = match.group(1) if match else None
            expected = os.environ.get("DD_DBM_PROPAGATION_MODE") == "full"
            assert bool(context) == expected, "SQL trace propagation differs from configuration"
            if context:
                assert context.split("-")[1] == f"{span.trace_id:032x}", "SQL trace ID mismatch"
            cursor.execute("SELECT value FROM generate_series(1, 5) AS value")
            rows = len(cursor.fetchall())
    tracer.flush()
    with LOCK:
        STATUS["iterations"] += 1
        STATUS["rows_returned"] += rows
        STATUS["trace_contexts"] += int(context is not None)
        STATUS["last_query_unix_seconds"] = int(time.time())
        STATUS["last_error_type"] = None
        iteration = STATUS["iterations"]
    print(json.dumps({"event": "query", "iteration": iteration,
                      "traceparent": context, "rows": rows}), flush=True)


def main():
    endpoint = os.environ["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"]
    # Verify that ddtrace selected its actual OTLP writer before querying the DB.
    assert getattr(tracer._span_aggregator.writer, "_otlp_endpoint", None) == endpoint
    sleep_seconds = float(os.getenv("QUERY_SLEEP_SECONDS", "3"))
    pause_seconds = float(os.getenv("QUERY_PAUSE_SECONDS", "1"))
    limit = int(os.getenv("ITERATIONS", "0"))
    assert sleep_seconds >= 0 and pause_seconds >= 0 and limit >= 0
    server = ThreadingHTTPServer(("0.0.0.0", int(os.getenv("HTTP_PORT", "8080"))), Health)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    for signum in (signal.SIGTERM, signal.SIGINT):
        signal.signal(signum, lambda *_: STOP.set())
    try:
        while not STOP.is_set():
            active = idle = None
            try:
                with LOCK:
                    STATUS["connection_attempts"] += 1
                active = connect("active")
                idle = connect("idle")
                # An independent idle session remains visible during every slow query.
                with idle.cursor() as cursor:
                    cursor.execute("SELECT 1")
                    cursor.fetchone()
                with LOCK:
                    STATUS["ready"] = True
                print(json.dumps({"event": "ready", "service": SERVICE,
                                  "ddtrace": ddtrace.__version__, "connections": 2,
                                  "protocol": os.environ.get("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL")}), flush=True)
                while not STOP.is_set():
                    run_iteration(active, sleep_seconds)
                    with LOCK:
                        done = limit and STATUS["iterations"] >= limit
                    if done:
                        STOP.set()
                    STOP.wait(pause_seconds)
            except psycopg2.Error as error:
                # Exception text can contain connection details. Expose only its type.
                with LOCK:
                    STATUS["ready"] = False
                    STATUS["errors"] += 1
                    STATUS["last_error_type"] = type(error).__name__
                print(json.dumps({"event": "database_error", "type": type(error).__name__}), flush=True)
                STOP.wait(2)
            finally:
                with LOCK:
                    STATUS["ready"] = False
                for connection in (active, idle):
                    if connection is not None:
                        connection.close()
    finally:
        server.shutdown()
        server.server_close()
        tracer.shutdown()


if __name__ == "__main__":
    main()
