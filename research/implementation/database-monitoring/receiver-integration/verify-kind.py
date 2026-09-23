#!/usr/bin/env python3
"""Exercise the receiver using a loopback forward to the existing PoC database."""

import argparse
import base64
import json
import os
from pathlib import Path
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", default="127.0.0.1:25432")
    parser.add_argument("--context", default="kind-otel-dd")
    parser.add_argument("--namespace", default="ddot-poc")
    parser.add_argument("--secret", default="dbm-postgres-password")
    parser.add_argument("--secret-key", default="password")
    parser.add_argument("--query-plans", action="store_true")
    parser.add_argument("--output-directory", type=Path, default=Path("/tmp"))
    args = parser.parse_args()
    if not args.endpoint.startswith("127.0.0.1:"):
        parser.error("endpoint must be an explicitly configured loopback port-forward")
    secret = json.loads(subprocess.check_output([
        "kubectl", "--context", args.context, "-n", args.namespace,
        "get", "secret", args.secret, "-o", "json",
    ]))
    args.output_directory.mkdir(parents=True, exist_ok=True)
    capture = args.output_directory / "dbm-live-collections.json"
    evidence = args.output_directory / "dbm-live-evidence.json"
    env = os.environ.copy()
    env.update({
        "POSTGRESQL_MONITORING_TEST_ENDPOINT": args.endpoint,
        "POSTGRESQL_MONITORING_TEST_PASSWORD": base64.b64decode(secret["data"][args.secret_key]).decode(),
        "POSTGRESQL_MONITORING_TEST_CAPTURE": str(capture),
        "POSTGRESQL_MONITORING_TEST_EVIDENCE": str(evidence),
        "POSTGRESQL_MONITORING_TEST_QUERY_PLANS": str(args.query_plans).lower(),
    })
    repo = Path(__file__).resolve().parents[4]
    result = subprocess.run([
        "go", "test", "-p", "2", "-tags", "integration", "-run",
        "^TestQueryMonitoringLivePostgres$", "-count=1", "-v", ".",
    ], cwd=repo / "receiver/postgresqlreceiver", env=env, check=False)
    if result.returncode:
        return result.returncode
    # The Go test reports failure if its uniquely named database cannot be
    # dropped. Only mark cleanup verified after the complete test has passed.
    result_evidence = json.loads(evidence.read_text())
    result_evidence["database_cleanup_verified"] = True
    evidence.write_text(json.dumps(result_evidence, indent=2, sort_keys=True) + "\n")
    print(f"Actual OTLP capture: {capture}\nSource evidence: {evidence}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
