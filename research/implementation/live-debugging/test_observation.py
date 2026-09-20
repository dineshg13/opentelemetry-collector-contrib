# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""The observation hook must preserve the real HTTP exchange, including errors."""
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading
import unittest

import app


class ObservationTest(unittest.TestCase):
    def test_original_request_and_error_response_are_preserved(self):
        received = []

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                received.append((self.path, self.rfile.read(int(self.headers["Content-Length"]))))
                self.send_response(429)
                self.send_header("Retry-After", "7")
                self.end_headers()
                self.wfile.write(b"real upstream rejection")

            def log_message(self, *_):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        body = json.dumps([{"debugger": {"snapshot": {"probe": {"id": "fixture"}, "captures": {"entry": {}}}},
                            "dd": {"trace_id": "1", "span_id": "2"}}]).encode()
        connection = http.client.HTTPConnection("127.0.0.1", server.server_port)
        try:
            connection.request("POST", "/debugger/v2/input?ddtags=synthetic", body, headers={"Content-Type": "application/json"})
            response = connection.getresponse()
            self.assertEqual(429, response.status)
            self.assertEqual("7", response.getheader("Retry-After"))
            self.assertEqual(b"real upstream rejection", response.read())
            self.assertEqual([("/debugger/v2/input?ddtags=synthetic", body)], received)
            self.assertEqual(429, app.observed[-1]["status"])
            self.assertEqual(1, app.observed[-1]["captured_snapshots"])
        finally:
            connection.close()
            server.shutdown()
            server.server_close()
            thread.join()

    def test_multipart_diagnostics(self):
        body = (b'--BOUNDARY\r\nContent-Disposition: form-data; name="event"; filename="event.json"\r\n'
                b'Content-Type: application/json\r\n\r\n'
                b'[{"debugger":{"diagnostics":{"probeId":"fixture","status":"INSTALLED"}}}]\r\n--BOUNDARY--\r\n')
        result = app.payload_summary(body, {"Content-Type": "multipart/form-data; boundary=BOUNDARY"})
        self.assertEqual(["INSTALLED"], result["diagnostics"])
        self.assertEqual(["fixture"], result["probe_ids"])
        self.assertEqual(0, result["snapshots"])


if __name__ == "__main__":
    unittest.main()
