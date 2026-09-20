# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Render the native Spark Job for the alternative proxy or SDK-disabled control."""
import argparse
import json
from pathlib import Path

import yaml

parser = argparse.ArgumentParser()
parser.add_argument("--alternative", action="store_true")
parser.add_argument("--sdk-disabled", action="store_true")
args = parser.parse_args()
job = yaml.safe_load(Path(__file__).with_name("spark.yaml").read_text())
name = "djm-spark" + ("-alternative" if args.alternative else "") + ("-disabled" if args.sdk_disabled else "")
job["metadata"]["name"] = name
job["spec"]["template"]["metadata"]["labels"]["app"] = name
container = job["spec"]["template"]["spec"]["containers"][0]
if args.alternative:
    for entry in container["env"]:
        if "value" in entry:
            entry["value"] = entry["value"].replace("collector.ddot-poc", "collector-http-forwarder.ddot-poc")
container["env"].append({"name": "DD_DATA_JOBS_ENABLED", "value": str(not args.sdk_disabled).lower()})
print(json.dumps(job, indent=2))
