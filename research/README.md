# Datadog products through standard OpenTelemetry

The active deliverable is the [runnable implementation](implementation/README.md), following
[new-instr.md](new-instr.md) and [principles.md](principles.md). Two real Collector builds,
SDK applications, component changes and kind deployment replace the earlier research-only
proposal. Ordinary signals use standard OTLP components. No Datadog receiver or embedded
Trace Agent is in the implemented distribution.

**Full eight-product verification remains incomplete.** Authenticated September 20 reads
now establish six core product paths: LLM, CI, DSM, debugging, profiling and Spark job
health/catalog. AppSec has partial marker/schema evidence; DBM has no positive product
record. The [current backend matrix](implementation/backend-readback/README.md) records
the exact proof and remaining attribution, SDK, mapping and control gaps.

- [Authenticated backend evidence and limits](implementation/backend-readback/README.md)
- [Implementation and reproducible commands](implementation/README.md)
- [Current ownership, status and next actions](implementation/PROGRESS.md)
- [Combined configuration](implementation/collector/combined.yaml)
- [Independently built generic alternative](implementation/collector/combined-http-forwarder.yaml)
- [Real kind outcomes](implementation/evidence/kind-integration.json)
- [Product disable checks](implementation/evidence/kind-disabled.json)
- [Final integration draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20)
- [Eight product drafts and publication verification](publication/README.md)
- [Source pins](sources.json) and per-product implementation evidence

The [earlier research index](legacy-research-index.md), [old architecture](architecture.md),
[old coverage](coverage.md), and old native-receiver harnesses are preserved as historical
references. Their proposed architecture and negative native-conversion findings do not
describe this implementation. In particular, all three exercised SDKs export real OTLP
traces; Python AppSec structured WAF bytes survive the standard OTLP pipeline; PostgreSQL
query correlation and real Spark SDK execution now have runnable implementations.

The combined branch is `dinesh.gurumurthy/poc-all-products`. All publication targets the
user's `dineshg13/opentelemetry-collector-contrib` fork. Product drafts target that branch;
the final draft targets fork `main`. No PR targets the OpenTelemetry project, no human
identity checkbox is checked, and no default-branch merge is authorized.
