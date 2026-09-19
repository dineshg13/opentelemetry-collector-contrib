# DBM correlation prototype

This is a temporary executable research module, not a Collector distribution. It
imports the unmodified local `datadogreceiver` through relative replacements and
the pinned Agent mapping helper. No Go commands run in the Agent repository.

Prerequisites: Go 1.26, Python 3.12 (tested), installed `ddtrace==4.13.0rc1` plus its
dependencies, access to bind loopback sockets, and the shared HTTP forwarder executable.
The recorded environment installed dependencies in `/tmp/ddot-research-python-runtime`
without changing SDK source checkouts. Its wheel source/version is recorded in the
product report. The optional extra PYTHONPATH includes `msgpack` from another isolated
experiment; the production SDK writer uses its own encoder.

From this directory:

```sh
# Build the unchanged extension harness if the shared binary is unavailable:
(cd ../../../shared/forwarder && go build -o /tmp/ddot-research-forwarder .)

DBM_FORWARDER_BINARY=/tmp/ddot-research-forwarder \
PYTHONPATH=/tmp/ddot-research-python-runtime:/tmp/ddot-research-appsec-python \
GOCACHE=/tmp/ddot-research-gocache \
go run . > result.json
```

For a new machine, create a temporary venv/install target with the stated ddtrace
version and dependencies and point `PYTHONPATH` there (or invoke the appropriate
Python via PATH). The recorded private package mirror URL is not required by code.
No API key or database password is read. The harness removes inherited `DD_*`
environment options from its child before setting its explicit test configuration.

`emit.py` creates one SQL client span manually, calls `_DBM_Propagator.inject`, prints
the SQL/context, flushes the actual native writer and exits. Go starts an ephemeral
receiver, runs that SDK process, and asserts the received pdata. The optional final
case creates a temporary forwarder config with `ingress.endpoint` on loopback and
`egress.endpoint` pointing to the receiver. No production Collector source is modified.

Cases and assertions:

1. Disabled: SQL unchanged, no injection marker.
2. Service: identity comment, no traceparent/marker.
3. Full: matching SQL traceparent, DB span ID, 128-bit trace ID and marker.
4. Full with 128-bit receiver gate disabled: mismatch is expected and asserted.
5. Full through existing forwarder: same identity/context preservation.

Every case also calls the actual imported Agent `GetOTelResourceV2` helper before and
after copying `dd.span.Resource` into `resource.name`: the fallback operation name
becomes the SQL resource. Result labels explicitly distinguish this from running
the exporter. IDs/runtime IDs/process IDs vary across runs; the structural assertions
are deterministic.

Observed final run: all five cases passed. Initial sandbox socket denial was resolved
by approved loopback execution. The first assertion incorrectly expected old `db.type`;
the receiver intentionally maps that to `db.system.name`, now correctly asserted.

Not tested: automatic DB driver instrumentation, dynamic-service mode, Java/JS runtime,
any SQL actually executing, Agent DB checks, exporter/connector final payloads, DBM
intake acceptance or the Datadog UI. Successful transport is not end-to-end DBM.
