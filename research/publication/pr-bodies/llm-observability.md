#### Description

Adds Python, Java and Node GenAI workloads over SDK OTLP paths, plus Python native LLM span/evaluation forwarding through both proxy alternatives. Backend LLM conversion, session/evaluation visibility and product correlation remain unverified.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets the combined implementation branch `dinesh.gurumurthy/poc-all-products`.

#### Testing

Actual Python 4.13.0rc1, Java 1.66.0 and Node 6.16.0 SDK semantic tests passed and kind workloads completed. Both real US5 native span/evaluation intakes returned 202. Native proxy gates and explicit OTLP LLM conversion-disable attributes were checked independently.

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-llm-observability/research/implementation/llm-observability/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
