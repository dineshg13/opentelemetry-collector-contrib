#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Build an actual Collector service with OCB and local contrib module replacements."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[3]


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=Path("/tmp/ddot-poc-build"))
    parser.add_argument("--image", default="ddot-products-collector:poc")
    parser.add_argument("--skip-image", action="store_true")
    parser.add_argument("--generic-only", action="store_true", help="Build without the Datadog extension or its dependencies")
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    dist = output / "collector"
    manifest = (ROOT / "research/implementation/collector/builder-config.yaml").read_text()
    if args.generic_only:
        manifest = "\n".join(line for line in manifest.splitlines()
                             if "extension/datadogextension" not in line) + "\n"
    manifest = manifest.replace("__BUILD_OUTPUT__", json.dumps(str(dist)))
    modules = subprocess.check_output(["git", "ls-files", "--", "*go.mod"], cwd=ROOT, text=True).splitlines()
    replacements = []
    for filename in modules:
        path = ROOT / filename
        line = next((x for x in path.read_text().splitlines() if x.startswith("module ")), "")
        module = line.removeprefix("module ").strip()
        if module.startswith("github.com/open-telemetry/opentelemetry-collector-contrib"):
            replacements.append(f"{module} => {path.parent}")
    manifest += "\nreplaces:\n" + "".join("  - " + json.dumps(x) + "\n" for x in replacements)
    rendered = output / "builder-config.yaml"
    rendered.write_text(manifest)
    env = dict(os.environ)
    env.setdefault("GOCACHE", "/tmp/ddot-research-go-cache")
    env.setdefault("GOMAXPROCS", "2")
    env["CGO_ENABLED"] = "0"
    # Generated output is outside the checkout; record provenance explicitly below.
    env["GOFLAGS"] = (env.get("GOFLAGS", "") + " -buildvcs=false").strip()
    run(["go", "tool", "-modfile=internal/tools/go.mod", "go.opentelemetry.io/collector/cmd/builder",
         "--config", str(rendered)], cwd=ROOT, env=env)
    binary = dist / "ddot-products-collector"
    assert binary.is_file(), binary
    imports = (dist / "components.go").read_text()
    for forbidden in ["receiver/datadogreceiver", "exporter/datadogexporter", "connector/datadogconnector"]:
        assert forbidden not in imports, forbidden
    if args.generic_only:
        assert "extension/datadogextension" not in imports
    dependencies = subprocess.check_output(["go", "list", "-deps", "./..."], cwd=dist, env=env, text=True)
    (output / "dependencies.txt").write_text(dependencies)
    record = {
        "timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
        "working_tree_dirty": bool(subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip()),
        "go": subprocess.check_output(["go", "version"], env=env, text=True).strip(),
        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "image": args.image,
        "generic_only": args.generic_only,
        "trace_agent_dependencies": [x for x in dependencies.splitlines() if "datadog-agent/pkg/trace" in x],
    }
    if args.generic_only:
        assert not record["trace_agent_dependencies"], record["trace_agent_dependencies"]
    if not args.skip_image:
        context = output / "image"
        context.mkdir(exist_ok=True)
        shutil.copy2(binary, context / binary.name)
        shutil.copy2(ROOT / "research/implementation/collector/Dockerfile", context / "Dockerfile")
        run(["docker", "build", "-t", args.image, str(context)])
        record["image_id"] = subprocess.check_output(["docker", "image", "inspect", args.image,
                                                     "--format", "{{.Id}}"], text=True).strip()
    (output / "build.json").write_text(json.dumps(record, indent=2) + "\n")
    print(json.dumps(record, indent=2))


if __name__ == "__main__":
    main()
