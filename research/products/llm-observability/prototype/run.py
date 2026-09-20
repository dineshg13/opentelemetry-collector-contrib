# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real http_forwarder + pinned JS writer contracts + strict local mock intake."""
import argparse
import hashlib
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--forwarder', required=True)
    parser.add_argument('--js-sdk', required=True)
    parser.add_argument('--python-runtime', help='Optional isolated ddtrace==4.13.0rc1 wheel directory')
    args = parser.parse_args()
    commit = subprocess.check_output(['git', '-C', args.js_sdk, 'rev-parse', 'HEAD'], text=True).strip()
    assert commit == 'd62655e12494634bb53f8c0cd440f087b004ca63', commit
    fixtures = json.loads(subprocess.check_output([
        'node', str(Path(__file__).with_name('writer_contract.cjs')), args.js_sdk]))
    accepted_paths = {'/api/v2/llmobs', '/api/intake/llm-obs/v2/eval-metric'}
    captured = []

    class Intake(BaseHTTPRequestHandler):
        def handle_request(self):
            body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
            status = 202 if self.path in accepted_paths else 404
            captured.append({'path': self.path, 'body_sha256': hashlib.sha256(body).hexdigest(),
                             'headers': dict(self.headers), 'status': status, 'body': body})
            response = json.dumps({'mock': True, 'accepted': status == 202}).encode()
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('X-Research-Response', 'preserved')
            self.send_header('Content-Length', str(len(response)))
            self.end_headers()
            self.wfile.write(response)

        do_GET = do_POST = handle_request

        def log_message(self, *_):
            pass

    intake = ThreadingHTTPServer(('127.0.0.1', 0), Intake)
    threading.Thread(target=intake.serve_forever, daemon=True).start()
    with socket.socket() as reservation:
        reservation.bind(('127.0.0.1', 0))
        port = reservation.getsockname()[1]
    observations = []
    try:
        with tempfile.TemporaryDirectory(prefix='llm-forwarder-') as directory:
            config = Path(directory) / 'config.yaml'
            config.write_text(f'''ingress:
  endpoint: 127.0.0.1:{port}
  compression_algorithms: []
egress:
  endpoint: http://127.0.0.1:{intake.server_port}/ignored-path
  headers:
    DD-API-KEY: research-only-key
  timeout: 5s
''')
            with (Path(directory) / 'forwarder.log').open('w+') as log:
                process = subprocess.Popen([args.forwarder, '--config', str(config)], stdout=log, stderr=log)
                try:
                    deadline = time.monotonic() + 10
                    while True:
                        if process.poll() is not None:
                            log.seek(0)
                            raise RuntimeError(log.read())
                        try:
                            with socket.create_connection(('127.0.0.1', port), timeout=.1):
                                break
                        except OSError:
                            if time.monotonic() > deadline:
                                raise
                            time.sleep(.05)

                    def send(method, path, body=b'', headers=None):
                        connection = http.client.HTTPConnection('127.0.0.1', port, timeout=5)
                        connection.request(method, path, body, headers or {})
                        response = connection.getresponse()
                        data = response.read()
                        assert response.getheader('X-Research-Response') == 'preserved'
                        connection.close()
                        return response.status, data

                    status, _ = send('GET', '/info')
                    assert status == 404
                    observations.append({'case': 'SDK capability discovery', 'status': status,
                                         'result': 'forwarded to intake; no Agent capability response'})
                    for fixture in fixtures['requests']:
                        body = fixture['body'].encode()
                        digest = hashlib.sha256(body).hexdigest()
                        status, response = send('POST', fixture['path'], body, fixture['headers'])
                        actual = captured[-1]
                        assert status == 404
                        assert actual['path'] == fixture['path']
                        assert actual['body_sha256'] == digest
                        assert actual['headers']['Dd-Api-Key'] == 'research-only-key'
                        observations.append({'case': fixture['kind'] + ' native EVP path', 'status': status,
                                             'path': actual['path'], 'body_sha256': digest,
                                             'response': json.loads(response), 'body_preserved': True})
                        # Control case is manually rewritten; it is NOT SDK proxy compatibility.
                        direct_path = fixture['path'].removeprefix('/evp_proxy/v2')
                        status, _ = send('POST', direct_path, body, fixture['headers'])
                        assert status == 202
                        assert captured[-1]['body_sha256'] == digest
                        observations.append({'case': fixture['kind'] + ' manually rewritten control',
                                             'status': status, 'path': direct_path,
                                             'body_preserved': True})
                    if args.python_runtime:
                        before = len(captured)
                        child_env = {key: value for key, value in os.environ.items()
                                     if not key.startswith(('DD_', '_DD_', 'OTEL_'))}
                        child_env.update({
                            'PYTHONPATH': str(Path(args.python_runtime).resolve()),
                            'PYTHONDONTWRITEBYTECODE': '1',
                            'DD_TRACE_AGENT_URL': f'http://127.0.0.1:{port}',
                            'DD_TRACE_ENABLED': 'false',
                            'DD_APM_TRACING_ENABLED': 'false',
                            'DD_LLMOBS_AGENTLESS_ENABLED': 'false',
                            'DD_INSTRUMENTATION_TELEMETRY_ENABLED': 'false',
                            'DD_REMOTE_CONFIGURATION_ENABLED': 'false',
                            'DD_INSTRUMENTATION_CONFIG_ID': 'research',
                        })
                        child = subprocess.run(
                            [sys.executable, str(Path(__file__).with_name('python_sdk.py').resolve())],
                            cwd=directory, env=child_env, text=True, capture_output=True, timeout=30)
                        assert child.returncode == 0, child.stderr
                        runtime = json.loads(child.stdout)
                        requests = [request for request in captured[before:]
                                    if request['path'].startswith('/evp_proxy/')]
                        span_requests = [request for request in requests
                                         if request['path'].endswith('/api/v2/llmobs')]
                        eval_requests = [request for request in requests
                                         if request['path'].endswith('/v2/eval-metric')]
                        assert span_requests and eval_requests, requests
                        assert b'research.runtime' in span_requests[0]['body']
                        assert b'hello' in span_requests[0]['body']
                        assert b'quality' in eval_requests[0]['body']
                        assert all(request['status'] == 404 for request in requests)
                        observations.append({'case': 'full Python SDK standalone',
                                             'sdk_version': runtime['sdk_version'],
                                             'paths': [request['path'] for request in requests],
                                             'statuses': [request['status'] for request in requests],
                                             'result': 'real span and evaluation emitted; unchanged EVP paths rejected'})
                finally:
                    process.terminate()
                    process.wait(timeout=10)
    finally:
        intake.shutdown()
        intake.server_close()
    print(json.dumps({'sdk_commit': fixtures['sdk_commit'], 'checks': observations,
                      'backend_validation': False,
                      'scope': 'pinned JS writer contract with stubs; optional full Python wheel; real extension; mock backend'}, indent=2))


if __name__ == '__main__':
    main()
