# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Render isolated app deployments for the HTTP-forwarder Collector alternative."""
from pathlib import Path
import yaml

documents = list(yaml.safe_load_all(Path(__file__).with_name("applications.yaml").read_text()))
for document in documents:
    name = document["metadata"]["name"] + "-forwarder"
    document["metadata"]["name"] = name
    document["metadata"]["labels"]["app"] = name
    document["spec"]["selector"]["matchLabels"]["app"] = name
    template = document["spec"]["template"]
    template["metadata"]["labels"]["app"] = name
    for container in template["spec"]["containers"]:
        for entry in container["env"]:
            entry["value"] = entry["value"].replace("collector.ddot-poc.svc.cluster.local",
                                                    "collector-http-forwarder.ddot-poc.svc.cluster.local")
print(yaml.safe_dump_all(documents, sort_keys=False), end="")
