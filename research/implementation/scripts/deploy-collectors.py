#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Refresh checked-in Collector ConfigMaps and deploy both variants to the PoC kind cluster."""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile
import yaml

ROOT = Path(__file__).resolve().parents[1]
KUBE = ['kubectl', '--context', 'kind-otel-dd', '-n', 'ddot-poc']


def load_image(image):
    # kind's containerd importer needs only the node platform, including when a
    # pulled image has a multi-platform manifest with unavailable remote layers.
    with tempfile.TemporaryDirectory(prefix='ddot-kind-image-') as directory:
        archive = str(Path(directory) / 'image.tar')
        subprocess.run(['docker', 'image', 'save', '--platform=linux/arm64', '-o', archive, image], check=True)
        subprocess.run(['kind', 'load', 'image-archive', '--name', 'otel-dd', archive], check=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--render-only', action='store_true')
    parser.add_argument('--skip-load', action='store_true')
    args = parser.parse_args()
    target = ROOT / 'kind/collectors.yaml'
    documents = list(yaml.safe_load_all(target.read_text()))
    for document in documents:
        if document['kind'] == 'ConfigMap':
            filename = 'combined.yaml' if document['metadata']['name'] == 'collector' else 'combined-http-forwarder.yaml'
            document['data']['collector.yaml'] = (ROOT / 'collector' / filename).read_text()
    target.write_text('# ConfigMaps refreshed from collector/combined*.yaml by scripts/deploy-collectors.py.\n' + yaml.safe_dump_all(documents, sort_keys=False))
    if args.render_only:
        return
    context = subprocess.check_output(['kubectl', 'config', 'current-context'], text=True).strip()
    assert context == 'kind-otel-dd', context
    nodes = json.loads(subprocess.check_output(KUBE + ['get', 'nodes', '-o', 'json']))['items']
    assert nodes and all(n['metadata']['name'].startswith('otel-dd-') for n in nodes)
    if not args.skip_load:
        load_image('ddot-products-collector:poc')
    subprocess.run(KUBE + ['apply', '-f', str(ROOT / 'kind/namespace.yaml')], check=True)
    # Secret values must be supplied separately; manifests contain references only.
    subprocess.run(KUBE + ['get', 'secret', 'datadog-api-key', 'dbm-postgres-password', '-o', 'name'], check=True)
    subprocess.run(KUBE + ['apply', '-f', str(target)], check=True)
    for name in ['collector', 'collector-http-forwarder']:
        subprocess.run(KUBE + ['rollout', 'restart', 'deployment/' + name], check=True)
        subprocess.run(KUBE + ['rollout', 'status', 'deployment/' + name, '--timeout=180s'], check=True)


if __name__ == '__main__':
    main()
