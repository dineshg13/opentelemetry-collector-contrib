# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Private in-memory client for bounded PoC readback; never prints credentials."""
import base64
import json
import subprocess
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


class Client:
    def __init__(self):
        self.opener = urllib.request.build_opener(NoRedirect())
        self.headers = {"Accept": "application/json", "Content-Type": "application/json"}
        for header, secret, key in [
            ("DD-API-KEY", "datadog-api-key", "api-key"),
            ("DD-APPLICATION-KEY", "datadog-application-key", "application-key"),
        ]:
            result = subprocess.run(
                ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc",
                 "get", "secret", secret, "-o", "json"],
                capture_output=True, text=True, timeout=20,
            )
            if result.returncode:
                raise RuntimeError("Cannot load named credential Secret; output withheld")
            self.headers[header] = base64.b64decode(
                json.loads(result.stdout)["data"][key], validate=True
            ).decode()

    def request(self, method, path, body=None, *, host="api.us5.datadoghq.com"):
        if method not in ("GET", "POST") or not path.startswith(("/api/v1/", "/api/v2/")):
            raise ValueError("Only Datadog read/search API paths are allowed")
        parsed = urllib.parse.urlsplit(path)
        if parsed.netloc or parsed.scheme or parsed.fragment:
            raise ValueError("Only local API paths are allowed")
        dbm_read = (
            host == "app.us5.datadoghq.com" and method == "POST"
            and path == "/api/v1/logs-analytics/list?type=databasequery"
        )
        if host != "api.us5.datadoghq.com" and not dbm_read:
            raise ValueError("Only US5 API and the documented DBM read endpoint are allowed")
        if method == "POST" and not (
            parsed.path.endswith(("/search", "/aggregate"))
            or parsed.path in ("/api/v2/query/timeseries", "/api/v2/query/scalar")
            or dbm_read
        ):
            raise ValueError("POST is restricted to read-only search/aggregate/query")
        req = urllib.request.Request(
            "https://" + host + path,
            data=json.dumps(body).encode() if body is not None else None,
            headers=self.headers, method=method,
        )
        try:
            response = self.opener.open(req, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        except Exception:
            return 0, {"error_class": "transport_error", "detail": "details withheld"}
        with response:
            raw = response.read(5 * 1024 * 1024 + 1)
            if len(raw) > 5 * 1024 * 1024:
                return response.code, {"error_class": "response_limit"}
            try:
                return response.code, json.loads(raw)
            except (ValueError, UnicodeError):
                return response.code, {"error_class": "non_json_response", "bytes": len(raw)}
