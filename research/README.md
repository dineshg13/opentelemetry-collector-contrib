# Datadog SDK products through the Collector

**Active implementation phase:** follow [the revised instructions](new-instr.md) and
[current implementation checkpoint](implementation/PROGRESS.md). The research below is
background evidence; none of its mock results satisfies the new end-to-end completion bar.

Research follows [instructions](instructions.md) and [principles](principles.md).
The source investigation covers all eight products and Python, Java and JavaScript where
supported. Local prototypes exercise real Collector components and selected SDK workloads.
**No product has authenticated Datadog backend/UI end-to-end validation.** Configuration
proposals and experimental adapters are not shipped Collector features.

## Findings and recommended architecture

Use explicit upstream pipelines for standard signals, an optional Datadog-owned HTTP entry
point for compatible native uploads, and validated native trace processing for products
carried inside APM. Retain Agent database and broker collection as required by the principles.
The current Datadog extension provides metadata/status, so the product entry point, discovery,
control plane and per-product settings require implementation.

The existing HTTP forwarder preserves the incoming path/query and forwards to one origin.
Its configured endpoint path is not a rewrite rule. Two narrow configuration-only paths
passed local transport tests: Java profiling with an exact backend-path URL override, and an
OpenLineage client configured with the backend path. They still require authenticated product
acceptance. Most native SDK Agent paths need routing, metadata/authentication handling and,
for some products, discovery or synchronous control responses. A forwarder to a real Agent
can retain those Agent responsibilities; that transitional architecture was source-inspected,
not exercised against a running Agent here.

Trace conversion also needs work. A real Python AppSec WAF payload loses structured trigger
details through the current Datadog receiver. DBM experiments preserve 128-bit correlation
IDs under the default gate but expose a SQL resource mapping gap at the exporter helper
boundary. The receiver's catch-all HTTP 200 can conceal unsupported product endpoints;
successful requests alone do not establish compatibility.

See the [architecture map](architecture.md), [coverage matrix](coverage.md),
[shared component/options comparison](shared/README.md),
[proposed configuration contract](shared/configuration.md),
[decisions and principles conflicts](decisions.md), and
[prioritized implementation plan](implementation-plan.md).

## Product reports and executed evidence

Each report traces the three SDKs, Agent handlers and backend destinations, compares viable
Collector options, records configurations/results, and assigns follow-up work. Unsupported
language combinations and unexecuted features are explicit.

| Product | Recommended path | Executed local evidence; remaining boundary |
| --- | --- | --- |
| [Live Debugging](products/live-debugging/README.md) | Shared native upload/discovery/RC service, with separate trace/metric paths | Real Python snapshots/diagnostics and JS uploader components through actual forwarder plus adapter; signed RC and UI-managed probes unverified |
| [LLM Observability](products/llm-observability/README.md) | Standard OTLP where supported; native EVP/evaluations and native APM handled separately | Real Python and JS writer components demonstrate preserved-prefix failure against strict mock; rewritten controls pass; no OTLP/backend run |
| [Application Security](products/application-security/README.md) | Preserve native security data and sampling in a validated trace pipeline | Real Python WAF capture/replay through actual receiver proves structured trigger loss; legacy fixture markers survive; full product parity unverified |
| [CI Visibility](products/ci-visibility/README.md) | Shared native data and synchronous control routes with discovery | Real Python pytest and Java manual test APIs through adapter; JS source components; no backend optimization outcome |
| [Data Jobs Monitoring](products/data-jobs-monitoring/README.md) | Java Spark native trace path plus separate OpenLineage option | Real OpenLineage client passes backend-path forwarder checks; Agent path fails strict mock; Spark execution unverified |
| [Continuous Profiling](products/continuous-profiling/README.md) | Opaque profile adapter; narrowly proven direct Java URL option | Real Python and Java uploads; exact Java URL passes strict mock through unchanged forwarder; no profile UI/correlation result |
| [Database Monitoring](products/database-monitoring/README.md) | SDK correlation in explicit trace pipelines; Agent DB checks retained | Five real Python SDK/native receiver cases, including forwarder and 128-bit negative control; resource bridge checked at helper only; no live DB/backend |
| [Data Streams Monitoring](products/data-streams-monitoring/README.md) | Opaque SDK statistics adapter and required trace side paths; Agent broker collection retained | Real Python checkpoint payloads through forwarder/adapter and Java discovery checks; no broker/backend topology validation |

The [validation guide](validation/README.md) distinguishes source commits, published SDK
artifacts, actual execution environments and mocks. The [independent review](INTEGRATION_REVIEW.md)
records the review scope, findings and checks. Original research inputs remain preserved.

## Integration and publication

All completed product work is assembled for review on
[`dinesh.gurumurthy/poc-review-snapshot`](https://github.com/dineshg13/opentelemetry-collector-contrib/tree/dinesh.gurumurthy/poc-review-snapshot/research).
The intended PR target, `dinesh.gurumurthy/poc-all-products`, contains the common research and
retains each product's PR diff. Product branches were created from that combined branch.
Use the assembled review snapshot for the complete index: relative links to other products
will remain incomplete on the common-only target and individual product branches until merges.

The eight product drafts [#11–#18](publication/README.md) and
[integration draft #19](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/19)
are open in the user's fork. The integration draft uses `poc-review-snapshot` into the fork's
`main` so individual product drafts retain their own diffs against `poc-all-products`.
The user explicitly authorized basic assistant-written summaries for these nine drafts.
Authorship checkboxes remain unchecked. No PR was marked ready or merged, no issue/PR comments
were posted, and no production component or default branch was changed.

See the [publication index and descriptions](publication/README.md) and
[progress/branch ownership](PROGRESS.md). Backend acceptance separately needs an approved test
organization/site, product access, appropriate credentials and workloads/services listed in
each report. Credentials must stay out of Git.

## Baseline sources

| Repository | Reference | Commit |
| --- | --- | --- |
| Collector contrib | main | `16fa3257d56c299e115a558b1a668018a39990d5` |
| Python SDK | main | `d6ab482ea3fb3c8666e75ac1905a3325b8f80098` |
| Java SDK | master | `7b903a53644abc39f55b2fb21283546ae9801f35` |
| JavaScript SDK | master | `d62655e12494634bb53f8c0cd440f087b004ca63` |
| Datadog Agent | main | `761050392a732097645b76d9845d36629f1ef3dc` |
| Agent integrations (supplemental) | master | `916e4f4364609494986417f9a5efc4b3b0281e52` |

Recorded 2026-09-19. All four source repositories were clean and on the requested reference
branches. Preserve their state; do not update or install packages into them.
Collector `origin/main`, `upstream/main`, and the user's checkout initially matched.
GitHub API confirmed the fork default branch is `main`.
See [sources.json](sources.json) for local paths and supplemental-source details. Executed
Python 4.13.0rc1 and Java 1.66.0 releases predate the investigated source snapshots; their
exact provenance is in [python-runtime.json](python-runtime.json) and
[java-runtime.json](java-runtime.json).

## Evidence conventions

Source-inspected behavior, executed local protocol tests, real SDK tests, and authenticated
backend/UI validation are separate levels. Mock HTTP acceptance proves transport only.
All recommendations and configuration extensions are proposals unless explicitly implemented.

## Existing research

[DBM notes](dbm.md) and [DSM notes](dsm.md) are preserved as supplied. They refer to an older
source location and incomplete language verification; new product reports supersede any
conflicting claims. Missing `01-mental-map` and other parts are not available in this checkout.

## Architectural conflicts requiring owners

- The principles retain Agent database/message-system collection for DBM/DSM, while the task
  states all products are enabled through `pkg/trace`. Trace SDK paths first, but distinguish
  collection/check paths outside `pkg/trace`; do not invent a universal enable flag.
- A product configuration entry point cannot silently instantiate Collector pipelines.
- Proprietary controls must support unequivocal disablement, including control/return paths.
- Local product integration can proceed, but AGENTS.md requires human-written PR sections and
  a human-checked authorship checkbox before readiness. No AI-generated issue/PR comments.
