#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Verify real SDK probe uploads through both real-backend Collector variants."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess

KUBE = ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc"]
PROBE = "d086e0b0-38f1-4620-80a9-bb3726b39106"


def run(*args):
    return subprocess.check_output(KUBE + list(args), text=True)


def events(text):
    records = []
    for line in text.splitlines():
        try:
            records.append(json.loads(line))
        except ValueError:
            pass
    return records


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=Path(__file__).with_name("kind-results.json"))
    args = parser.parse_args()
    results = []
    for application, collector in [("debugger-python", "collector"), ("debugger-python-http-forwarder", "collector-http-forwarder")]:
        records = events(run("logs", "deployment/" + application, "--since=10m"))
        activity = [entry for entry in records if entry.get("event") == "activity"]
        requests = [entry for entry in records if entry.get("event") == "sdk_http"]
        uploads = [entry for entry in requests if entry.get("path", "").startswith("/debugger/")]
        snapshots = [entry for entry in uploads if entry.get("captured_snapshots", 0) and entry.get("trace_correlated_snapshots", 0)]
        diagnostics = [entry for entry in uploads if "INSTALLED" in entry.get("diagnostics", [])]
        assert activity and snapshots and diagnostics, {"app": application, "observed": requests}
        assert all(PROBE in entry["probe_ids"] for entry in snapshots + diagnostics)
        assert not any(entry.get("observation_error") for entry in uploads), uploads
        # Aggregate counters establish healthy Collector transport, not attribution
        # of an individual debugger span. Full payload logging was not authorized.
        code = """import json,urllib.request,urllib.error
base='http://COLLECTOR.ddot-poc.svc.cluster.local'
metrics=urllib.request.urlopen(base+':8888/metrics').read().decode()
names=('otelcol_receiver_accepted_spans','otelcol_exporter_sent_spans','otelcol_exporter_send_failed_spans')
counts={name:sum(float(line.rsplit(' ',1)[1]) for line in metrics.splitlines() if line.startswith(name) and (name.startswith('otelcol_receiver') or 'exporter="otlp_http/datadog"' in line)) for name in names}
info=json.load(urllib.request.urlopen(base+':8126/info'))
try:
 response=urllib.request.urlopen(urllib.request.Request(base+':8126/v0.7/config',data=b'{}',headers={'Content-Type':'application/json'}))
 status=response.status
except urllib.error.HTTPError as error:status=error.code
print(json.dumps({'aggregate_trace_counters':counts,'discovery_endpoints':info['endpoints'],'remote_config_status':status}))
""".replace('COLLECTOR', collector)
        transport = json.loads(run("exec", "deployment/" + application, "--", "python", "-c", code))
        assert "/debugger/v2/input" in transport["discovery_endpoints"]
        assert "/v0.7/config" not in transport["discovery_endpoints"]
        assert transport["remote_config_status"] == 404
        successes = [entry for entry in uploads if 200 <= entry["status"] < 300]
        results.append({"application": application, "collector": collector,
                        "observed_snapshot_requests": len(snapshots),
                        "captured_snapshots": sum(entry["captured_snapshots"] for entry in snapshots),
                        "trace_correlated_snapshots": sum(entry["trace_correlated_snapshots"] for entry in snapshots),
                        "installed_diagnostic_requests": len(diagnostics),
                        "upload_response_statuses": sorted({entry["status"] for entry in uploads}),
                        "accepted_upload_requests": len(successes), "collector_transport": transport,
                        "sdk_otlp_writer_assertion": "passed at workload startup", "exact_kind_otlp_span_readback": "not performed",
                        "backend_transport_accepted": bool(successes) and len(successes) == len(uploads),
                        "signed_remote_configuration_verified": False, "backend_product_verified": False})
    result = {"timestamp": datetime.now(timezone.utc).isoformat(), "sdk": "ddtrace 4.13.0rc1",
              "probe_configuration": "supported local probe file, not signed backend RC",
              "http_observation": "original SDK HTTP transport called unchanged; original response status retained",
              "checks": results,
              "blockers": ["Standalone Collector has no signed RC client/configuration service.",
                           "No authenticated Datadog UI/API probe registration or snapshot readback was available.",
                           "Detailed Collector payload logging was rejected by automatic approval review; aggregate counts do not identify this product's individual traces."]}
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
