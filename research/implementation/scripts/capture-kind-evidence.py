#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Save bounded status/count evidence without payloads, credentials or raw logs."""
from collections import Counter
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
KUBE = ['kubectl', '--context', 'kind-otel-dd', '-n', 'ddot-poc']


def capture():
    result = {'timestamp': datetime.now(timezone.utc).isoformat(), 'context': 'kind-otel-dd',
              'backend_product_readback': 'blocked: no authenticated account or read-scoped application key available', 'collectors': {}, 'pods': []}
    for name in ['collector', 'collector-http-forwarder']:
        logs = subprocess.check_output(KUBE + ['logs', 'deploy/' + name, '--since=20m'], text=True)
        counts = Counter()
        failures = []
        malformed = 0
        for line in logs.splitlines():
            if 'Forwarded HTTP product request' in line:
                try:
                    value = json.loads(line[line.index('{'):])
                except (ValueError, json.JSONDecodeError):
                    # Disk exhaustion or log rotation can truncate a record.
                    # Surface the count rather than pretending evidence is complete.
                    malformed += 1
                    continue
                key = tuple(value.get(k) for k in ['route', 'upstream_host', 'upstream_status_code', 'status_code'])
                counts[key] += 1
            if '\terror\t' in line or '\twarn\t' in line:
                # Only logger message, without structured fields or payloads.
                fields = line.split('\t')
                failures.append(fields[3][:240] if len(fields) > 3 else 'warning/error')
        metrics = subprocess.check_output(KUBE + ['exec', 'deploy/' + name, '--', 'wget', '-qO-', 'http://127.0.0.1:8888/metrics'], text=True)
        selected = [line for line in metrics.splitlines() if line.startswith(('otelcol_exporter_sent_', 'otelcol_exporter_send_failed_', 'otelcol_receiver_accepted_', 'otelcol_receiver_refused_'))]
        result['collectors'][name] = {
            'upstream_outcomes': [dict(zip(['route','upstream_host','upstream_status','sdk_status','requests'], list(k)+[v])) for k,v in sorted(counts.items())],
            'telemetry_metrics': selected, 'warning_error_messages': dict(Counter(failures)),
            'malformed_forwarding_log_records': malformed,
            'interpretation': 'Intake responses and Collector counters are transport evidence, not product UI or backend correlation proof.'}
    pods = json.loads(subprocess.check_output(KUBE + ['get', 'pods', '-o', 'json']))['items']
    for pod in pods:
        statuses = pod['status'].get('containerStatuses', [])
        result['pods'].append({'name':pod['metadata']['name'],'phase':pod['status'].get('phase'),
                              'containers':[{'name':s['name'],'ready':s['ready'],'restarts':s['restartCount'], 'image':s['image'], 'image_id':s.get('imageID')} for s in statuses]})
    return result


if __name__ == '__main__':
    result = capture()
    path = ROOT / 'evidence/kind-integration.json'
    path.write_text(json.dumps(result, indent=2) + '\n')
    for name, value in result['collectors'].items():
        print(name, json.dumps(value['upstream_outcomes']), json.dumps(value['warning_error_messages']))
    print('Wrote',path)
