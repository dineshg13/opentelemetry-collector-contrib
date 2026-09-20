# Executable current-forwarder experiment

This module imports the **unmodified** local `extension/httpforwarderextension` factory.
`main.go` reads a real YAML extension subtree using Collector confmap and starts the real
component with a no-op Collector host. It is a minimal host, not a full Collector binary:
no service pipelines, authenticators, config providers or backend credentials are installed.
The YAML egress header test exercises API-key injection; auth-extension lifecycle is untested.
No Datadog product proxy has been simulated inside the extension.

## Reproduce

From the repository root (Go 1.26+ and dependencies from go.mod/go.sum are required):

```sh
cd research/shared/forwarder
GOCACHE=/tmp/ddot-research-go-cache go test -mod=readonly -v ./...
GOCACHE=/tmp/ddot-research-go-cache go build -buildvcs=false -o /tmp/ddot-research-forwarder .
/tmp/ddot-research-forwarder --config config.yaml
```

The executable prints `READY 127.0.0.1:18126` and runs until SIGINT/SIGTERM. Supply a local
capture server on port 18127, or change egress to a test endpoint. The shipped key is synthetic.
`config.yaml` is the exact subtree placed under `extensions.http_forwarder` in a Collector;
the full Collector also needs that extension listed in `service.extensions`. No `${env:...}`
expansion is implemented by this small host; use actual synthetic test values in its YAML.

To relay unchanged SDK paths to an existing local Agent, use egress `http://127.0.0.1:8126`
and a different ingress port. This retains all Agent processing and is not Agentless product
support. To test a direct backend-shaped upload, the **SDK must generate the correct backend
path**; configuring `/api/v2/...` in egress endpoint does not rewrite the path.

## Executed results (2026-09-19)

Runtime Go 1.26.4/linux-arm64. Initial sandbox run failed because TCP listeners were prohibited
(`socket: operation not permitted`). The same local-loopback command succeeded after approved
execution outside that sandbox. No backend network requests or live API keys were used.

```text
go test -mod=mod -v ./...
PASS TestCurrentForwarderWireContract
  PASS /profiling/v1/input
  PASS /debugger/v1/input
  PASS /evp_proxy/v2/api/v2/llmobs
  PASS /v0.1/pipeline_stats
  PASS /info
  PASS /v0.7/config
PASS TestCompressionConfiguration/default_decompresses
PASS TestCompressionConfiguration/explicit_empty_preserves
PASS TestUnsupportedRoutingConfigurationIgnored
ok example.com/ddot-research/forwarder 0.014s
```

The initial routing test expected unknown nested keys to error, but observed success. The
final test records the actual behavior: invented `egress.routes` and `egress.endpoints` have
no effect and produce exactly the baseline config. This follows the pinned confighttp client
custom Unmarshal using WithIgnoreUnused. It is not an endorsement of these invented options.

| Assertion | Observed |
| --- | --- |
| Request method and opaque binary body | Identical POST and bytes at backend |
| Incoming escaped query / repeated query keys | Preserved unchanged |
| Egress endpoint path and query | Ignored; incoming path/query used |
| Host header | Backend host replaces ingress host |
| Configured DD-API-KEY versus incoming key | Exactly one configured key reaches backend |
| Container ID, EVP subdomain, unconfigured Authorization | Passed through unchanged; no lookup, routing or sanitization |
| Static metadata header | Added as configured; no runtime metadata enrichment |
| Via request/response | Added by real forwarder |
| Response HTTP429, Retry-After and JSON body | Returned to caller |
| Two Set-Cookie response values | First only; current copy uses Header.Get/Set |
| Gzip with default ingress config | Decompressed bytes, Content-Encoding removed |
| Gzip with `compression_algorithms: []` | Original compressed bytes and gzip header |
| /info and /v0.7/config | Sent to the same backend unchanged, no local control handling |
| Multi-destination or route config | No such fields; nested invented keys ignored |

Paths in the synthetic test are **protocol probes**, not actual product SDK payloads. The
binary body is intentionally arbitrary and no SDK runs in this test. It proves transport
behavior, not validity of profiling/LLM/debugger/DSM payloads, capabilities, RC or product
experience. Real SDK tests in product-owned directories may reuse this executable and must
report their separate evidence. No authenticated backend, Datadog UI, retry/fan-out, TLS,
container enrichment, RC, load, multipart-specific or native trace translation was validated
by this shared test. The tests acquire ephemeral local ports; between reserving and starting
the real extension there is a small ordinary port-allocation race.
