# Validation environments

The [Python runtime manifest](../python-runtime.json) records a separately installed official
`ddtrace==4.13.0rc1` wheel and every dependency hash. Its source tag resolves to
`c280552330bb906df23b2963d6457a8d23fb81de`. This is 569 commits before the researched Python
main snapshot; execution of this release **does not validate later main behavior**.
The source reports and each experiment label the distinction.

Reproduce on Python 3.12 / Linux arm64 (other architectures need their own verified wheel hashes):

```sh
python3 -m pip install --target /tmp/ddot-research-python-runtime --no-cache-dir \
  --only-binary=:all: --require-hashes -r research/validation/python-requirements.txt
cd /tmp
PYTHONPATH=/tmp/ddot-research-python-runtime PYTHONDONTWRITEBYTECODE=1 \
  DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIGURATION_ENABLED=false \
  python3 -c 'import ddtrace; print(ddtrace.__version__, ddtrace.__file__)'
```

Only `/tmp` was used for the installation, preserving global/site and source environments.
Product tests configure loopback endpoints explicitly. No backend secrets were supplied.
The [forwarder experiment](../shared/forwarder/README.md) uses the actual pinned local
Collector component; it is a component host, not a full Collector service binary.

Each product report includes commands, observed assertions, and missing acceptance steps.
No HTTP/mock fixture result is counted as authenticated product success.

The [Java runtime manifest](../java-runtime.json) pins a supplemental `dd-java-agent 1.66.0`
artifact from Maven Central, source tag `a099fffb31657bb6e8b4d04ee741491f3480829d`, and its
SHA-256. It is 81 commits before researched Java master. The coordinator shell inventory reported Temurin 25.0.4 Linux arm64, but the profiling
experiment actually executed Microsoft OpenJDK 21.0.11 at the path recorded in its results.
Those are distinct environments. Product execution records take precedence; the CI runner
uses the Java executable on its invocation PATH, so record `java -version` when reproducing
it. Merely downloading the artifact is not a validation result.

Documentation integrity checks read the source checkouts recorded in `sources.json` by
default. On another machine, map the DataDog source root explicitly (each repository must
contain the recorded Git objects):

```sh
python3 research/checks/validate.py --source-root "$HOME/dd"
```

This verifies the eight reports, relative links and pinned GitHub source-file links. It does
not verify every prose citation, run SDKs, or establish product/backend compatibility.
