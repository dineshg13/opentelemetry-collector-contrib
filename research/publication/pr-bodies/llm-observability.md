#### Description

Adds Python, Java and Node GenAI workloads over SDK OTLP paths, plus Python native LLM span/evaluation forwarding through both proxy alternatives. Authenticated product queries verify core LLM conversion, session tags and native evaluations; broader feature coverage remains limited.

This is an **draft PoC with verified core backend paths** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets `dinesh.gurumurthy/poc-all-products`; [combined integration draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20).

#### Testing

Actual Python 4.13.0rc1, Java 1.66.0 and Node 6.16.0 SDK semantic tests passed and kind workloads completed. Both real US5 native span/evaluation intakes returned 202. Native proxy gates and explicit OTLP LLM conversion-disable attributes were checked independently.

**Authenticated readback, 2026-09-20:** All eight language/mode/Collector workloads have matching LLM product records, correct token counts and expected input/output; both Python native evaluations have score 1. This verifies the deterministic core PoC, not every LLM feature. [Evidence and limits](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/backend-readback/README.md).

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-llm-observability/research/implementation/llm-observability/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
