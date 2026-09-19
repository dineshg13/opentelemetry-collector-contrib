# Reproduce the LLM native forwarding contract experiment

Prerequisites: Python 3 stdlib, Node.js (tested 24.18.0), the exact local JS SDK commit from
the product report, permission for loopback sockets, and the shared real forwarder harness.
The default Node contract test installs no SDK packages and makes no network request outside
loopback. Test key is synthetic. An optional full Python test uses a separately installed wheel.

From repository root after shared work is integrated, build the harness using the commands
in [shared forwarder instructions](../../../shared/forwarder/README.md). Then run:

```sh
python3 research/products/llm-observability/prototype/run.py \
  --forwarder /tmp/ddot-research-forwarder \
  --js-sdk "$HOME/dd/dd-trace-js"
```

Both paths are explicit arguments and can point at another isolated build/checkout. The
runner verifies the SDK commit, starts a strict mock HTTP intake on an ephemeral port,
writes a temporary **currently supported extension subtree**, launches the unmodified
`http_forwarder` through its actual factory, asserts all results, and stops both services.
The runtime configuration is:

```yaml
ingress:
  endpoint: 127.0.0.1:<ephemeral-forwarder-port>
  compression_algorithms: []
egress:
  endpoint: http://127.0.0.1:<ephemeral-intake-port>/ignored-path
  headers:
    DD-API-KEY: research-only-key
  timeout: 5s
```

The egress path intentionally demonstrates that configuring an endpoint path cannot remove
the inbound EVP prefix. Compression is explicitly disabled at ingress so opaque payloads
are not decompressed by default. This experiment sends JSON; Java gzip behavior is a
separate required test, covered generically by shared transport tests, not by Java runtime.

`writer_contract.cjs` loads the pinned SDK's actual `base.js`, `spans.js` and `evaluations.js`
classes using a small CommonJS wrapper. `_getOptions`, `makePayload` and `_encode` execute
unchanged. Synthetic ASCII events are provided, with explicit ancillary stubs. The SDK's
network adapter is deliberately not invoked; Python sends those constructed bytes through
the real forwarder. This is a writer contract test, **not a full SDK integration test**.

The full Python import was also attempted with the repository's `.venv-lint/bin/python`
and failed with `ModuleNotFoundError: ddtrace.internal.native._native`. The Node checkout
has no `node_modules`. Built dependencies are required for full pinned-HEAD runtime tests.

The coordinator subsequently installed the official `ddtrace==4.13.0rc1` wheel and
dependencies in `/tmp/ddot-research-python-runtime`; it is an additional runtime version,
not the researched HEAD 569 commits later. To install/reproduce in another isolated location:

```sh
python3 -m pip install --target /tmp/ddot-research-python-runtime 'ddtrace==4.13.0rc1'
python3 research/products/llm-observability/prototype/run.py \
  --forwarder /tmp/ddot-research-forwarder \
  --js-sdk "$HOME/dd/dd-trace-js" \
  --python-runtime /tmp/ddot-research-python-runtime
```

`python_sdk.py` executes the complete installed SDK without stubs, enables manual LLM
instrumentation, creates a span with synthetic input/output and token counts, exports its
context for an evaluation, flushes and shuts down. The runner supplies
`DD_TRACE_AGENT_URL=http://127.0.0.1:<forwarder-port>`, `DD_APM_TRACING_ENABLED=false`,
`DD_TRACE_ENABLED=false`, `DD_LLMOBS_AGENTLESS_ENABLED=false`, and disables SDK telemetry
and remote configuration. It removes inherited `DD_*`, `_DD_*`, and `OTEL_*` settings and
uses `PYTHONPATH` pointing only at that isolated install. Explicit transport choice means this
full-runtime case does not test automatic discovery. No LLM provider or API credentials are
needed. All backend errors here are expected and asserted against captured requests.

Observed 2026-09-19:

| Case | Expected and observed | Meaning |
| --- | --- | --- |
| GET `/info` | 404 | Generic forwarder passes request to mock intake; cannot synthesize Agent capabilities |
| Node span writer native path | 404, `/evp_proxy/v2/api/v2/llmobs` unchanged | Prefix rewrite is missing |
| Node evaluation writer native path | 404, `/evp_proxy/v2/api/intake/llm-obs/v2/eval-metric` unchanged | Prefix rewrite is missing for second intake as well |
| Manually rewritten span control | 202 | Correct-path body can traverse current extension |
| Manually rewritten evaluation control | 202 | Same for evaluation body; this does not test host routing |
| Full Python 4.13.0rc1 span + evaluation | 404 for both real requests | Complete SDK emits both EVP paths; the unchanged prefixes fail the mock intake contract |

All body hashes matched and `X-Research-Response` reached the caller. Agent-native responses
are not fabricated. Span body SHA-256 was
`aeb37bb7815ad8476e70ae831babbb8b8eca4a28395ed1ab0f7bfbb18cc15c87`;
evaluation body SHA-256 was
`ed36bb82505d4a79817d1d6bdd71802575d6f58a0cfcf7c96837e8eb48e9c9d2`.

Mock 202 means only that the strict local endpoint accepted the path. No Datadog authentication,
schema acceptance, backend conversion, queryability, UI rendering, automatic instrumentation,
retry, RC or real Java delivery was validated. Full Python manual instrumentation and local
HTTP delivery were validated only for the separately labeled wheel. Source inspection establishes the
additional host-routing and metadata requirements; these assertions do not test them.
