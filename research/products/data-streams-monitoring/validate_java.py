"""Real released Java agent + unchanged forwarder, bounded /info gating probe."""

import argparse
import gzip
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import select
import subprocess
import tempfile
import threading

import msgpack

from validate_python import reserve_port


class AgentShapedHandler(BaseHTTPRequestHandler):
    def read_body(self):
        if self.headers.get("Transfer-Encoding", "").lower() != "chunked":
            return self.rfile.read(int(self.headers.get("Content-Length", 0)))
        chunks = []
        while True:
            size = int(self.rfile.readline().split(b";", 1)[0], 16)
            if size == 0:
                while self.rfile.readline() not in (b"\r\n", b"\n", b""):
                    pass
                return b"".join(chunks)
            chunks.append(self.rfile.read(size))
            assert self.rfile.read(2) == b"\r\n"

    def do_GET(self):
        self.server.paths.append(self.path)
        endpoints = ["/v0.4/traces"]
        if self.server.advertise:
            endpoints.append("/v0.1/pipeline_stats")
        body = json.dumps({"version": "7.70.0", "endpoints": endpoints}).encode()
        self.send_response(200 if self.path == "/info" else 404)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        self.server.paths.append(self.path)
        body = self.read_body()
        if self.path == "/v0.1/pipeline_stats":
            self.server.stats.append((dict(self.headers), body))
        self.send_response(200)
        self.send_header("Content-Length", "2")
        self.end_headers()
        self.wfile.write(b"{}")

    do_PUT = do_POST

    def log_message(self, *_):
        pass


def run_case(jar, binary, directory, advertise):
    server = ThreadingHTTPServer(("127.0.0.1", 0), AgentShapedHandler)
    server.advertise, server.paths, server.stats = advertise, [], []
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    port = reserve_port()
    config = Path(directory) / "forwarder.yaml"
    config.write_text(
        f"ingress:\n  endpoint: 127.0.0.1:{port}\n  compression_algorithms: []\n"
        f"egress:\n  endpoint: http://127.0.0.1:{server.server_port}\n  timeout: 5s\n"
    )
    forwarder = subprocess.Popen([binary, "--config", str(config)], stdout=subprocess.PIPE,
                                 stderr=subprocess.STDOUT, text=True)
    try:
        assert select.select([forwarder.stdout], [], [], 10)[0]
        assert forwarder.stdout.readline().startswith("READY")
        result = subprocess.run([
            "java", f"-javaagent:{jar}", "-Ddd.data.streams.enabled=true",
            f"-Ddd.trace.agent.url=http://127.0.0.1:{port}", "-Ddd.service=dsm-research-java",
            "-Ddd.env=test", "-Ddd.instrumentation.telemetry.enabled=false",
            "-Ddd.remote_config.enabled=false", "-Ddd.trace.startup.logs=false",
            "-Ddd.profiling.enabled=false", "-Ddd.appsec.enabled=false",
            "-cp", directory, "DsmEmit",
        ], capture_output=True, text=True, timeout=30, check=True)
        assert "/info" in server.paths
        assert bool(server.stats) == advertise, (server.paths, result.stderr)
        summaries = []
        all_points = []
        has_transactions = False
        for headers, body in server.stats:
            headers = {k.lower(): v for k, v in headers.items()}
            assert headers["content-encoding"] == "gzip"
            assert headers["datadog-meta-lang"] == "java"
            payload = msgpack.unpackb(gzip.decompress(body), raw=False, strict_map_key=False)
            points = [point for bucket in payload["Stats"] for point in bucket["Stats"]]
            assert payload["Service"] == "dsm-research-java"
            all_points.extend(points)
            transactions = any(bucket.get("Transactions") for bucket in payload["Stats"])
            has_transactions = has_transactions or transactions
            summaries.append({"keys": sorted(payload), "point_count": len(points),
                              "transaction_bytes_present": transactions})
        if advertise:
            assert len(all_points) >= 2, (summaries, result.stderr)
            hashes = {point["Hash"] for point in all_points}
            assert any(point["ParentHash"] in hashes for point in all_points)
            assert has_transactions
        return {"advertise_pipeline_stats": advertise, "sdk": "dd-java-agent 1.66.0",
                "paths": server.paths, "stats_requests": len(server.stats), "payloads": summaries}
    finally:
        forwarder.terminate()
        forwarder.communicate(timeout=10)
        server.shutdown()
        server.server_close()
        thread.join()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--forwarder", default="/tmp/ddot-research-forwarder")
    parser.add_argument("--jar", default="/tmp/ddot-research-java-agent-1.66.0.jar")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="ddot-dsm-java-") as directory:
        subprocess.run(["javac", "-cp", args.jar, "-d", directory,
                        str(Path(__file__).with_name("DsmEmit.java"))], check=True)
        cases = [run_case(args.jar, args.forwarder, directory, advertise)
                 for advertise in (False, True)]
    result = json.dumps({"validation": "local Agent-shaped mock only; no Datadog Agent process or backend",
                         "cases": cases}, indent=2)
    if args.output:
        args.output.write_text(result + "\n")
    print(result)


if __name__ == "__main__":
    main()
