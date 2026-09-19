# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Build a small Java-only Spark image from a pinned PySpark distribution's Spark JARs."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile

parser = argparse.ArgumentParser()
parser.add_argument("--runtime", type=Path, default=Path("/tmp/ddot-poc-spark-runtime"))
parser.add_argument("--java-home", type=Path, default=Path("/usr/local/sdkman/candidates/java/current"))
parser.add_argument("--agent", type=Path, default=Path("/tmp/ddot-research-java-agent-1.66.0.jar"))
parser.add_argument("--lineage", type=Path, default=Path("/tmp/ddot-openlineage-spark_2.13-1.45.0.jar"))
parser.add_argument("--context", type=Path, default=Path("/tmp/ddot-djm-spark-build"))
parser.add_argument("--archive", type=Path, default=Path("/tmp/ddot-djm-spark.tar"))
args = parser.parse_args()
root = Path(__file__).resolve().parent
assert (args.runtime / "pyspark/jars/spark-sql_2.13-4.0.0.jar").is_file(), "Expected PySpark 4.0.0 runtime"
assert hashlib.sha256(args.agent.read_bytes()).hexdigest() == "5f0eb51160fade367d97404624561b6666f7475fb1453a7a73237eb643e398d8"
assert hashlib.sha256(args.lineage.read_bytes()).hexdigest() == "de887d1acfc0b890915071da4fff1bd2862a0e104b0df46b1e475009d1ea7a3c"
args.context.mkdir(parents=True, exist_ok=True)
for name in ("bin", "jars"):
    shutil.copytree(args.runtime / "pyspark" / name, args.context / "spark" / name, dirs_exist_ok=True)
shutil.copy2(args.agent, args.context / "dd-java-agent.jar")
shutil.copy2(args.lineage, args.context / "openlineage-spark.jar")
shutil.copy2(root / "Dockerfile.spark", args.context / "Dockerfile")
classes = args.context / "classes"
classes.mkdir(exist_ok=True)
subprocess.run([str(args.java_home / "bin/javac"), "--release", "17", "-cp", str(args.runtime / "pyspark/jars/*"),
    "-d", str(classes), str(root / "DjmSparkWorkload.java")], check=True)
subprocess.run([str(args.java_home / "bin/jar"), "--create", "--file", str(args.context / "workload.jar"),
    "-C", str(classes), "."], check=True)
subprocess.run(["docker", "buildx", "build", "-t", "ddot-djm-spark:poc", "--output",
    "type=docker,dest=" + str(args.archive), str(args.context)], check=True)
with tarfile.open(args.archive) as archive:
    manifest = json.load(archive.extractfile("manifest.json"))
    image = "sha256:" + hashlib.sha256(archive.extractfile(manifest[0]["Config"]).read()).hexdigest()
evidence = root / "evidence/spark-build.json"
evidence.parent.mkdir(exist_ok=True)
evidence.write_text(json.dumps({"built": True, "image_config": image, "archive_sha256": hashlib.sha256(args.archive.read_bytes()).hexdigest(),
    "spark_version": "4.0.0", "java_agent": "1.66.0", "openlineage_spark": "1.45.0",
    "spark_jars": {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted((args.runtime / "pyspark/jars").glob("*.jar"))}}, indent=2) + "\n")
print(image)
