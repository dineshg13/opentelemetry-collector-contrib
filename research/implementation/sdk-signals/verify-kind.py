# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Send real SDK signals through a scoped port-forward to the actual kind Collector.

Only aggregate Collector counters and workload IDs are recorded. No credentials or
full telemetry payloads are read from the cluster; counters are not product UI proof.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import tempfile
import time
from urllib.request import urlopen

from verify import environment, free_port


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--python-runtime", required=True)
    parser.add_argument("--node-runtime", required=True)
    parser.add_argument("--java-agent", required=True)
    parser.add_argument("--java-api-dir", required=True)
    parser.add_argument("--java-home", default=os.environ.get("JAVA_HOME", ""))
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    kubectl = ["kubectl", "--context", "kind-otel-dd", "-n", "ddot-poc"]
    deployment = json.loads(subprocess.check_output(kubectl + ["get", "deployment", "collector", "-o", "json"], text=True))
    assert deployment.get("status", {}).get("readyReplicas", 0) > 0, "Collector is not ready"
    ingress, telemetry = free_port(), free_port()
    java = str(Path(args.java_home) / "bin/java") if args.java_home else "java"
    javac = str(Path(args.java_home) / "bin/javac") if args.java_home else "javac"
    java_classpath = os.pathsep.join(str(path) for path in sorted(Path(args.java_api_dir).glob("*.jar")))
    pattern = re.compile(r"^(otelcol_(?:receiver_accepted|receiver_refused|exporter_sent|exporter_send_failed)_(?:spans|log_records|metric_points)(?:_total)?)(?:\{([^}]*)\})?\s+([0-9.eE+\-]+)$")

    def counters():
        text = urlopen(f"http://127.0.0.1:{telemetry}/metrics", timeout=10).read().decode()
        result = {}
        for line in text.splitlines():
            match = pattern.match(line)
            if match:
                name, labels, value = match.groups()
                # Exclude the unrelated database receiver and duplicate debug exporter.
                if 'receiver="otlp"' in (labels or "") or 'exporter="otlp_http/datadog"' in (labels or ""):
                    result[name + "{" + (labels or "") + "}"] = float(value)
        return result

    with tempfile.TemporaryDirectory(prefix="ddot-signals-kind-") as temporary:
        subprocess.run([javac, "-cp", java_classpath, "-d", temporary, str(root / "Signals.java")], check=True)
        with (Path(temporary) / "port-forward.log").open("w+") as log:
            forward = subprocess.Popen(kubectl + ["port-forward", "--address", "127.0.0.1", "service/collector",
                                                  f"{ingress}:4318", f"{telemetry}:8888"], stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 20
                while time.monotonic() < deadline:
                    try:
                        with socket.create_connection(("127.0.0.1", telemetry), timeout=0.2):
                            break
                    except OSError:
                        if forward.poll() is not None:
                            log.seek(0)
                            raise RuntimeError(log.read())
                        time.sleep(0.1)
                else:
                    raise TimeoutError("Collector port-forward did not become ready")
                baseline = counters()
                assert baseline, "No bounded Collector counters found"
                runs = []
                for language, command in (
                    ("python", [sys.executable, str(root / "app.py")]),
                    ("javascript", ["node", str(root / "app.cjs")]),
                    ("java", [java, "-javaagent:" + args.java_agent, "-cp", temporary + os.pathsep + java_classpath, "Signals"]),
                ):
                    before = counters()
                    env = environment(language, f"http://127.0.0.1:{ingress}")
                    env.update(PYTHONPATH=args.python_runtime, PYTHONDONTWRITEBYTECODE="1",
                               NODE_PATH=str(Path(args.node_runtime) / "node_modules"))
                    child = subprocess.run(command, env=env, text=True, capture_output=True, timeout=45)
                    time.sleep(3)
                    after = counters()
                    runs.append({"language": language, "exit_code": child.returncode,
                                 "stdout": child.stdout.strip(),
                                 "stderr_without_startup_inventory": "\n".join(line for line in child.stderr.splitlines()
                                                                               if "DATADOG TRACER CONFIGURATION" not in line),
                                 "counter_deltas": {key: value - before.get(key, 0) for key, value in after.items()
                                                    if value != before.get(key, 0)}})
                final = counters()
            finally:
                forward.terminate(); forward.wait(timeout=10)
    result = {"timestamp": datetime.now(timezone.utc).isoformat(), "context": "kind-otel-dd",
              "namespace": "ddot-poc", "collector_ready_replicas": deployment["status"]["readyReplicas"],
              "scope": "Host SDK processes through port-forward to actual kind Collector using real Datadog export destinations",
              "backend_product_readback_verified": False,
              "counter_attribution": "Aggregate counters include concurrent PoC traffic; workload IDs support later readback, not exact per-workload backend attribution",
              "baseline_counters": baseline, "final_counters": final, "runs": runs}
    Path(args.output).write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"timestamp": result["timestamp"], "runs": runs}, indent=2))
    assert all(run["exit_code"] == 0 for run in runs), "A workload failed"


if __name__ == "__main__":
    main()
