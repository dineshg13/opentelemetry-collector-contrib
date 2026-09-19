#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Deploy scoped synthetic product workloads to both Collector variants."""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
sys.dont_write_bytecode = True
import yaml

ROOT = Path(__file__).resolve().parents[1]
KUBE = ['kubectl', '--context', 'kind-otel-dd', '-n', 'ddot-poc']
spec = importlib.util.spec_from_file_location('deploy_collectors', Path(__file__).with_name('deploy-collectors.py'))
loader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(loader)


def alternative(document):
    value = json.loads(json.dumps(document).replace('collector.ddot-poc.svc.cluster.local', 'collector-http-forwarder.ddot-poc.svc.cluster.local'))
    name = value['metadata']['name'] + '-forwarder'
    value['metadata']['name'] = name
    if 'app' in value['metadata'].get('labels', {}):
        value['metadata']['labels']['app'] = name
    value['spec']['template']['metadata']['labels']['app'] = name
    if value['kind'] == 'Deployment':
        value['spec']['selector']['matchLabels']['app'] = name
    return value


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--skip-load', action='store_true')
    args = parser.parse_args()
    assert subprocess.check_output(['kubectl', 'config', 'current-context'], text=True).strip() == 'kind-otel-dd'
    files = ['continuous-profiling/kind/applications.yaml', 'llm-observability/deployment.yaml',
             'llm-observability/jobs.yaml', 'application-security/deployment.yaml', 'application-security/job-node.yaml']
    documents = []
    for filename in files:
        for doc in yaml.safe_load_all((ROOT / filename).read_text()):
            documents.append(doc)
            if not filename.startswith('application-security/'):
                documents.append(alternative(doc))
    for flags in [[], ['--alternative'], ['--sdk-disabled']]:
        value = subprocess.check_output(['python3', str(ROOT / 'data-streams-monitoring/kind/render_jobs.py')] + flags, text=True)
        documents.extend(json.loads(value)['items'])
    # A separate disabled AppSec Job leaves the enabled deployment running.
    disabled = yaml.safe_load((ROOT / 'application-security/job-node.yaml').read_text())
    disabled['metadata']['name'] += '-disabled'
    disabled['spec']['template']['metadata']['labels']['app'] += '-disabled'
    for entry in disabled['spec']['template']['spec']['containers'][0]['env']:
        if entry['name'] == 'DD_APPSEC_ENABLED':
            entry['value'] = 'false'
    documents.append(disabled)
    images = sorted({c['image'] for d in documents for c in d['spec']['template']['spec']['containers']})
    if not args.skip_load:
        for image in images:
            print('Loading', image, flush=True)
            loader.load_image(image)
    for doc in documents:
        doc['spec']['template']['spec']['nodeSelector'] = {'kubernetes.io/hostname': 'otel-dd-worker'}
        # Existing immutable Jobs are retained; use a fresh namespace or delete
        # these exact PoC Jobs explicitly to rerun completed one-shot workloads.
        if doc['kind'] == 'Job':
            found = subprocess.run(KUBE + ['get', 'job', doc['metadata']['name']], capture_output=True)
            if found.returncode == 0:
                continue
        subprocess.run(KUBE + ['apply', '-f', '-'], input=json.dumps(doc), text=True, check=True)
    for doc in documents:
        if doc['kind'] == 'Deployment':
            subprocess.run(KUBE + ['rollout', 'status', 'deployment/' + doc['metadata']['name'], '--timeout=180s'], check=True)


if __name__ == '__main__':
    main()
