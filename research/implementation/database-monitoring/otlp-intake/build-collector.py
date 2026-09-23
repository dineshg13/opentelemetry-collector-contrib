#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Build the DBM test Collector with OCB and this checkout's contrib modules."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path("/tmp/dbm-only-collector"))
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    dist = output / "collector"
    manifest = (HERE / "builder-config.yaml").read_text().replace(
        "__BUILD_OUTPUT__", json.dumps(str(dist)))
    modules = subprocess.check_output(
        ["git", "ls-files", "--", "*go.mod"], cwd=ROOT, text=True).splitlines()
    replacements = []
    for filename in modules:
        path = ROOT / filename
        module = next((line.removeprefix("module ").strip()
                       for line in path.read_text().splitlines()
                       if line.startswith("module ")), "")
        if module.startswith("github.com/open-telemetry/opentelemetry-collector-contrib"):
            replacements.append(f"{module} => {path.parent}")
    manifest += "\nreplaces:\n" + "".join("  - " + json.dumps(x) + "\n" for x in replacements)
    rendered = output / "builder-config.yaml"
    rendered.write_text(manifest)
    env = dict(os.environ)
    env.setdefault("GOCACHE", "/tmp/dbm-go-cache")
    env.setdefault("GOMAXPROCS", "2")
    env["CGO_ENABLED"] = "0"
    # OCB output lives outside the checkout; record provenance explicitly.
    env["GOFLAGS"] = (env.get("GOFLAGS", "") + " -buildvcs=false").strip()
    subprocess.run([
        "go", "tool", "-modfile=internal/tools/go.mod",
        "go.opentelemetry.io/collector/cmd/builder", "--config", str(rendered),
    ], cwd=ROOT, env=env, check=True)
    binary = dist / "dbm-otlp-collector"
    assert binary.is_file(), binary
    imports = (dist / "components.go").read_text()
    for forbidden in ("receiver/datadogreceiver", "exporter/datadogexporter",
                      "connector/datadogconnector", "extension/datadogextension",
                      "extension/httpforwarderextension", "processor/transformprocessor"):
        assert forbidden not in imports, forbidden
    dependencies = subprocess.check_output(
        ["go", "list", "-deps", "./..."], cwd=dist, env=env, text=True)
    (output / "dependencies.txt").write_text(dependencies)
    trace_agent_dependencies = [line for line in dependencies.splitlines()
                                if "datadog-agent/pkg/trace" in line]
    assert not trace_agent_dependencies, trace_agent_dependencies
    record = {
        "timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_commit": subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
        "working_tree_dirty": bool(subprocess.check_output(
            ["git", "status", "--porcelain"], cwd=ROOT, text=True).strip()),
        "go": subprocess.check_output(["go", "version"], env=env, text=True).strip(),
        "binary": str(binary),
        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "trace_agent_dependencies": trace_agent_dependencies,
    }
    (output / "build.json").write_text(json.dumps(record, indent=2) + "\n")
    print(json.dumps(record, indent=2))


if __name__ == "__main__":
    main()
