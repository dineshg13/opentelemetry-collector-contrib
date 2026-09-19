#### Description

Adds real Python and Node WAF workloads using standard SDK OTLP export. Structured Python security bytes survive the Collector unchanged. Java structured metadata export has a reproduced SDK gap; full IAST/RASP/SCA, remote control and backend security visibility remain incomplete.

This is an **incomplete draft PoC** in the user's fork. No Datadog receiver or embedded Trace Agent is used. Shared implementation is staged on `dinesh.gurumurthy/poc-all-products`; product completion is not claimed from HTTP success.

#### Link to tracking issue

No tracking issue was provided. Targets the combined implementation branch `dinesh.gurumurthy/poc-all-products`.

#### Testing

Actual Python 4.13.0rc1 and Node 6.16.0 WAF enabled/disabled tests passed through the built Collector; kind workloads ran. A real Java 1.66.0 encoder probe reproduced the structured metadata omission. Local semantics and intake transport do not establish backend AppSec behavior.

#### Documentation

[Runnable implementation, configuration, SDK coverage and evidence](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-application-security/research/implementation/application-security/README.md). [Combined build, deployment and independent review](https://github.com/dineshg13/opentelemetry-collector-contrib/blob/dinesh.gurumurthy/poc-all-products/research/implementation/README.md).

#### Authorship

- [ ] I, a human, wrote this pull request description myself.
