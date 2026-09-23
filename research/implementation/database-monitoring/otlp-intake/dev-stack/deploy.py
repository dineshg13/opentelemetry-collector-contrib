#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Deploy an isolated PostgreSQL/workload/Collector stack without logging secrets."""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import secrets
import shlex
import shutil
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
NAMESPACE = "dbm-otlp-dev"
LABELS = {"app.kubernetes.io/part-of": "dbm-otlp-dev"}
POSTGRES_IMAGE = "postgres:16-alpine@sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685"


def env_values(path):
    values = {}
    for line in path.read_text().splitlines():
        words = shlex.split(line, comments=True)
        if words and words[0] == "export":
            words = words[1:]
        for word in words:
            key, sep, value = word.partition("=")
            if not sep or key not in {"DD_API_KEY", "DD_APP_KEY", "DD_SITE"}:
                raise ValueError("env file must contain only literal DD key/site assignments")
            values[key] = value
    if not values.get("DD_API_KEY") or values.get("DD_SITE") != "datad0g.com":
        raise ValueError("this development deployment requires DD_API_KEY and DD_SITE=datad0g.com")
    return values


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", type=Path, required=True)
    parser.add_argument("--collector-binary", type=Path, required=True)
    parser.add_argument("--context", default="kind-otel-dd")
    parser.add_argument("--kind-name", default="otel-dd")
    parser.add_argument("--evidence", type=Path, default=Path("/tmp/dbm-otlp-dev-deployment.json"))
    args = parser.parse_args()
    values = env_values(args.env_file)
    if args.context != "kind-" + args.kind_name:
        raise ValueError("context must match the target kind cluster")
    kube = ["kubectl", "--context", args.context]

    def run(argv, **kwargs):
        return subprocess.run(argv, check=True, **kwargs)

    def get(kind, name, namespaced=True):
        command = kube + (["-n", NAMESPACE] if namespaced else [])
        result = run(command + ["get", kind, name, "-o", "json", "--ignore-not-found"], capture_output=True, text=True)
        return json.loads(result.stdout) if result.stdout.strip() else None

    def apply(obj):
        # Server-side apply avoids copying secrets into last-applied annotations.
        result = subprocess.run(kube + ["apply", "--server-side", "--field-manager=dbm-otlp-dev", "-f", "-"], input=json.dumps(obj), capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError(f"kubectl apply failed for {obj['kind']}/{obj.get('metadata', {}).get('name', 'stack')}; output suppressed to protect secret inputs")
        print(result.stdout.strip(), flush=True)

    existing = get("namespace", NAMESPACE, False)
    if existing and existing["metadata"].get("labels", {}).get("app.kubernetes.io/part-of") != "dbm-otlp-dev":
        raise ValueError("refusing to modify a namespace not owned by this stack")
    run(kube + ["get", "nodes", "-o", "name"])
    digest = hashlib.sha256(args.collector_binary.read_bytes()).hexdigest()
    collector_image = "dbm-otlp-dev-collector:" + digest[:12]
    workload_digest = hashlib.sha256(b"".join((HERE / p).read_bytes() for p in ["app.py", "requirements.txt", "Dockerfile.workload"])).hexdigest()
    workload_image = "dbm-otlp-dev-workload:" + workload_digest[:12]
    with tempfile.TemporaryDirectory(prefix="dbm-dev-image-", dir="/tmp") as tmp:
        build = Path(tmp)
        shutil.copyfile(args.collector_binary, build / "ddot-products-collector")
        (build / "ddot-products-collector").chmod(0o755)
        shutil.copyfile("/etc/ssl/certs/ca-certificates.crt", build / "ca-certificates.crt")
        shutil.copyfile(HERE / "Dockerfile.collector", build / "Dockerfile")
        run(["docker", "build", "-t", collector_image, str(build)])
    run(["docker", "build", "-f", str(HERE / "Dockerfile.workload"), "-t", workload_image, str(HERE)])
    platform = subprocess.check_output(["docker", "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", collector_image], text=True).strip()
    run(["docker", "pull", "--platform", platform, POSTGRES_IMAGE])
    run(["docker", "tag", POSTGRES_IMAGE, "ddot-postgres:16.15"])
    # Docker's image store can retain multi-platform indexes without every
    # platform's blobs; export only the built platform before importing to kind.
    with tempfile.TemporaryDirectory(prefix="dbm-dev-archive-", dir="/tmp") as tmp:
        archive = str(Path(tmp) / "images.tar")
        run(["docker", "image", "save", "--platform", platform, "-o", archive, collector_image, workload_image, "ddot-postgres:16.15"])
        run(["kind", "load", "image-archive", "--name", args.kind_name, archive])

    def obj(kind, name, **rest):
        return {"apiVersion": "v1", "kind": kind, "metadata": {"name": name, "namespace": NAMESPACE, "labels": LABELS}, **rest}

    apply({"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": NAMESPACE, "labels": LABELS}})
    apply(obj("Secret", "datadog", type="Opaque", data={k: base64.b64encode(values[k].encode()).decode() for k in ["DD_API_KEY", "DD_SITE"]}))
    if not get("secret", "postgres"):
        apply(obj("Secret", "postgres", type="Opaque", data={"password": base64.b64encode(secrets.token_urlsafe(32).encode()).decode()}))
    config = (HERE / "collector.yaml").read_text()
    configs = [obj("ConfigMap", "collector", data={"collector.yaml": config}), obj("ConfigMap", "postgres-init", data={"01-init.sh": (HERE / "postgres-init.sh").read_text()}), obj("PersistentVolumeClaim", "postgres", spec={"accessModes": ["ReadWriteOnce"], "resources": {"requests": {"storage": "1Gi"}}})]
    for item in configs:
        apply(item)

    def env(name, value):
        return {"name": name, "value": value}

    def secret_env(name, secret, key):
        return {"name": name, "valueFrom": {"secretKeyRef": {"name": secret, "key": key}}}

    def deployment(name, container, volumes=None, config_hash=None):
        container.setdefault("imagePullPolicy", "Never")
        container.setdefault("resources", {"requests": {"cpu": "50m", "memory": "64Mi"}, "limits": {"cpu": "1", "memory": "512Mi"}})
        template = {"metadata": {"labels": {"app": name, **LABELS}}, "spec": {"containers": [container], "terminationGracePeriodSeconds": 30}}
        if volumes:
            template["spec"]["volumes"] = volumes
        if config_hash:
            template["metadata"]["annotations"] = {"config-sha256": config_hash}
        return {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": name, "namespace": NAMESPACE, "labels": LABELS}, "spec": {"replicas": 1, "strategy": {"type": "Recreate"}, "selector": {"matchLabels": {"app": name}}, "template": template}}

    def service(name, ports):
        return obj("Service", name, spec={"selector": {"app": name}, "ports": [{"name": key, "port": port, "targetPort": port} for key, port in ports]})

    postgres = deployment("postgres", {"name": "postgres", "image": "ddot-postgres:16.15", "args": ["postgres", "-c", "shared_preload_libraries=pg_stat_statements", "-c", "track_activity_query_size=4096", "-c", "track_io_timing=on"], "env": [env("POSTGRES_USER", "postgres"), env("POSTGRES_DB", "dbm"), env("PGDATA", "/var/lib/postgresql/data/pgdata"), secret_env("POSTGRES_PASSWORD", "postgres", "password")], "ports": [{"containerPort": 5432}], "readinessProbe": {"exec": {"command": ["pg_isready", "-U", "postgres", "-d", "dbm"]}, "initialDelaySeconds": 5}, "volumeMounts": [{"name": "data", "mountPath": "/var/lib/postgresql/data"}, {"name": "init", "mountPath": "/docker-entrypoint-initdb.d"}]}, [{"name": "data", "persistentVolumeClaim": {"claimName": "postgres"}}, {"name": "init", "configMap": {"name": "postgres-init"}}])
    collector = deployment("collector", {"name": "collector", "image": collector_image, "env": [secret_env("DD_API_KEY", "datadog", "DD_API_KEY"), secret_env("DD_SITE", "datadog", "DD_SITE"), secret_env("DBM_POSTGRES_PASSWORD", "postgres", "password")], "ports": [{"containerPort": p} for p in [4318, 8888, 13133]], "readinessProbe": {"httpGet": {"path": "/", "port": 13133}}, "volumeMounts": [{"name": "config", "mountPath": "/conf", "readOnly": True}]}, [{"name": "config", "configMap": {"name": "collector"}}], hashlib.sha256(config.encode()).hexdigest())
    # Environment variables are read only at pod startup. Secret rotation must
    # replace the Collector pod even when its config and image are unchanged.
    collector["spec"]["template"]["metadata"]["annotations"]["credentials-resource-version"] = get("secret", "datadog")["metadata"]["resourceVersion"]
    workload = deployment("workload", {"name": "workload", "image": workload_image, "env": [env("DB_HOST", "postgres"), env("DB_NAME", "dbm"), env("DB_USER", "dbm_app"), secret_env("DB_PASSWORD", "postgres", "password"), env("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://collector:4318/v1/traces"), env("DD_VERSION", workload_digest[:12])], "ports": [{"containerPort": 8080}], "readinessProbe": {"httpGet": {"path": "/readyz", "port": 8080}, "initialDelaySeconds": 3}, "livenessProbe": {"httpGet": {"path": "/healthz", "port": 8080}, "initialDelaySeconds": 30}})
    items = [service("postgres", [("postgres", 5432)]), postgres, service("collector", [("otlp-http", 4318), ("metrics", 8888), ("health", 13133)]), collector, service("workload", [("http", 8080)]), workload]
    for item in items:
        apply(item)
    for name in ["postgres", "collector", "workload"]:
        run(kube + ["-n", NAMESPACE, "rollout", "status", "deployment/" + name, "--timeout=180s"])
    evidence = {"context": args.context, "namespace": NAMESPACE, "site": values["DD_SITE"], "otlp_endpoint": "https://otlp." + values["DD_SITE"], "collector_image": collector_image, "collector_binary_sha256": digest, "workload_image": workload_image, "postgres_image": "ddot-postgres:16.15", "database_service": "dbm-otlp-dev-postgres", "workload_service": "dbm-otlp-dev-workload", "backend_deployment_performed": False}
    args.evidence.write_text(json.dumps(evidence, indent=2) + "\n")
    print(json.dumps(evidence, indent=2))


if __name__ == "__main__":
    main()
