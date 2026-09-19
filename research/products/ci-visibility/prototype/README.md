# Reproduce CI transport evidence

These are local protocol experiments, not authenticated Datadog end-to-end tests. The
Python experiment runs both real pytest plugin generations. The optional Java experiment
loads a real released Java agent and uses its public manual CI API. The JavaScript test
executes real source modules with explicitly stubbed dependencies.

## Prerequisites and commands

Use a temporary directory, Python 3.12, Node.js, Git and the
[shared current-forwarder executable](../../../shared/forwarder/README.md). The executable
hosts the unmodified `http_forwarder` extension; it is not a full Collector with pipelines.
No SDK source checkout is modified. Install packages into isolated temporary paths:

```sh
python3 -m pip install --target /tmp/ddot-research-python-runtime ddtrace==4.13.0rc1
python3 -m pip install --target /tmp/ddot-research-ci-python pytest==8.3.5 msgpack==1.1.1
PYTHONPATH=/tmp/ddot-research-ci-python python3 research/products/ci-visibility/prototype/validate.py
node research/products/ci-visibility/prototype/check-js-components.cjs /home/bits/dd/dd-trace-js
```

Dependency/runtime installs may need network access; tests themselves use loopback only.
This sandbox required approved execution with loopback sockets. There are no live keys:
`research-server-key` is a fake credential checked only by the fixture. The harness removes
Datadog/OpenTelemetry environment settings from child processes and supplies test settings;
all listeners use ephemeral local ports and all child processes are stopped in cleanup.

Optional real Java manual API experiment (JDK available, jar downloaded separately):

```sh
PYTHONPATH=/tmp/ddot-research-ci-python python3 research/products/ci-visibility/prototype/validate.py \
  --java-agent /tmp/ddot-research-java-agent-1.66.0.jar
```

The jar is `com.datadoghq:dd-java-agent:1.66.0`, from
`https://repo.maven.apache.org/maven2/com/datadoghq/dd-java-agent/1.66.0/dd-java-agent-1.66.0.jar`.
SHA-256: `5f0eb51160fade367d97404624561b6666f7475fb1453a7a73237eb643e398d8`.
Release source is `a099fffb31657bb6e8b4d04ee741491f3480829d`; this is supplemental runtime
evidence, **not** an execution of inspected Java master. The Python wheel's release source
is `c280552330bb906df23b2963d6457a8d23fb81de`, likewise separate from Python main.

## Exact topology and configuration

The harness writes this supported forwarder YAML subtree with selected ephemeral ports:

```yaml
ingress:
  endpoint: 127.0.0.1:INGRESS_PORT
  compression_algorithms: []
egress:
  endpoint: http://127.0.0.1:DESTINATION_PORT
  timeout: 15s
```

Empty ingress compression algorithms deliberately preserve the SDK's compressed bytes and
`Content-Encoding`. The first destination is a strict mock Datadog backend: it implements
raw intake/API paths, no `/info` and no EVP prefixes. The second destination is a small
**research compatibility adapter** placed *after* the forwarder, which supplies `/info`,
validates CI path/subdomain pairs, strips EVP prefixes, replaces the API key and adds static
metadata. It then calls the same strict backend. Real code changes in a production
Collector/Datadog extension would be needed to provide these adapter responsibilities.

The local backend uses `X-Research-Subdomain` to assert which real intake hostname a request
would target; no public hostname is contacted. The adapter is intentionally incomplete: no
TLS, actual container discovery, load/backpressure, streaming fan-out, production size limits,
complete timeout/retry policy, logs/media/replay/report support or real Datadog API behavior.
It does not pretend to reproduce the full Agent or ship a new Collector component.

SDK configuration uses `DD_TRACE_AGENT_URL=http://127.0.0.1:INGRESS_PORT`, service/environment
fixture values and agentless disabled. Python runs pytest with `--ddtrace`, explicitly
loads either `ddtrace.contrib.internal.pytest.plugin` (legacy) or
`ddtrace.testing.internal.pytest.entry_point` (default new plugin), and enables ITR. A tiny
temporary Git repository allows the real SDK to search commits and upload an actual
generated packfile; its remote is `example.invalid`, and the fixture is not shallow.
Remote Configuration and telemetry are disabled to isolate CI product traffic.

## Results and evidence boundaries

Executed on 2026-09-19, Linux arm64; exact summary in [results.json](results.json).

| Check | Observation |
| --- | --- |
| Current forwarder direct to backend, Python legacy | `/info` 404 leads to `/v0.5/traces` fallback; no complete CI native path |
| Current forwarder direct to backend, Python default | Only `/info` observed; pytest still exits zero, but CI setup fails and no CI payload is sent |
| Synthetic direct EVP post | Prefix remains unchanged at backend, status 404 returned |
| Real Python legacy, forwarder + adapter | Decoded `test`, `test_module_end`, `test_session_end`, `test_suite_end`; one decoded per-test coverage record with files |
| Real Python default, forwarder + adapter | Test-cycle, coverage, settings, known/skippable/test-management and git search/packfile endpoints reached |
| Real Python legacy control and git | Same API family plus actual SDK-generated packfile; settings fixture enables these requests |
| Real Java 1.66.0 manual API, forwarder + adapter | Decoded all four CI event types; settings/known/skippable/test-management and real git search/packfile observed; automatic coverage not tested |
| Synthetic throttling | 429 status, exact JSON body and `Retry-After: 7` returned through adapter and forwarder |
| Credentials | Backend receives the configured synthetic key, not the client probe's spoofed key |
| Product separation | LLM endpoint rejected; no backend request |
| Disablement | `/info` removes CI capabilities and CI requests rejected; no backend request |
| JS source-component discovery | Four cases pass: no EVP, v2, v3 downgrade, v4 preference/gzip |
| JS source-component writer | Two cases pass: events and coverage select correct path/subdomain, omit SDK API key |

Neither fixture HTTP 200 nor passing application tests proves the Datadog product works.
The empty known/skip/management responses do not test selection, quarantine, retries, history
or backend entitlement. The 429 probe does not execute SDK retry timing. Legacy coverage
bytes are decoded; the default-plugin run asserts routes, not an independently decoded
default encoder schema. Java manual API evidence does not validate JUnit/build instrumentation
or automatic coverage. JS serializers/network classes are stubs and no test-runner plugin
is loaded. Agent Go code is source-inspected; this harness never runs the Agent implementation.

Required next validation: compile/instrument pinned Java and JS test projects, run both
Python plugin matrices against real Agent and production Collector changes, test settings
that actually select/skip/quarantine/retry tests, then use a test Datadog org/key to verify
session/test/coverage/git records and UI. Reports, media, logs, replay, native trace fallback
product parity and all nondefault Agent egress options remain separate unverified features.
