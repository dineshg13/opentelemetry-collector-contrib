# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

"""Exercise the installed SDK's real DBM propagator and native trace writer.

There is deliberately no database driver/service: this validates context creation
and trace transport, not database collection or instrumentation hooks.
"""

import json

import ddtrace
from ddtrace.propagation._database_monitoring import _DBM_Propagator

QUERY = "SELECT * FROM orders WHERE id = 42"
with ddtrace.tracer.trace("postgres.query", service="orders-db", resource=QUERY, span_type="sql") as span:
    span.set_tags({"db.type": "postgresql", "db.name": "orders", "out.host": "database.local", "peer.service": "orders-db", "span.kind": "client"})
    args, _ = _DBM_Propagator(0, "query").inject(span, (QUERY,), {})
    result = {
        "sdk_version": ddtrace.__version__,
        "sql": args[0],
        "trace_id": f"{span.trace_id:032x}",
        "span_id": f"{span.span_id:016x}",
        "traceparent": span.context._traceparent,
        "marker": span.get_tag("_dd.dbm_trace_injected"),
    }
ddtrace.tracer.flush()
ddtrace.tracer.shutdown()
print(json.dumps(result))
