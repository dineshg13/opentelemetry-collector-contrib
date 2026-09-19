# Implementation checkpoint

The active request is [new-instr.md](../new-instr.md). Earlier research and local mock/component
prototypes are reference material, not completed end-to-end implementations.

Coordinator owns the actual Collector distribution, combined configuration/build, kind
resources, integration checks, current index, and final PR from poc-all-products to fork main.
The selected context is kind-otel-dd; resource inventory and credential presence checks are
in progress. No credentials will be recorded. No production resources may be modified.

| Product | Implementation owner | Status |
| --- | --- | --- |
| Live Debugging | queued dedicated agent | not implemented |
| LLM Observability | queued dedicated agent | not implemented |
| Application Security | queued dedicated agent | not implemented |
| CI Visibility | queued dedicated agent | not implemented |
| Data Jobs Monitoring | queued dedicated agent | not implemented |
| Continuous Profiling | queued dedicated agent | not implemented |
| Database Monitoring | queued dedicated agent | not implemented |
| Data Streams Monitoring | queued dedicated agent | not implemented |

Shared forwarding/configuration implementation is separately owned. All ordinary traces,
logs and metrics must use standard OTLP ingestion; Datadog receiver is prohibited. Product
protocol adapters may forward opaque data but must not import substantial Agent logic.
Completed means built, deployed to kind, and product behavior verified against real backend.
Without that evidence, mark the product incomplete or blocked and finish unaffected work.
