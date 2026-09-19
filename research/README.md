# Datadog SDK products through the Collector

Research follows [instructions](instructions.md) and [principles](principles.md).
See [progress](PROGRESS.md) for ownership, branches, validation, blockers, and resumption.

## Inventory

The complete product inventory is Live Debugging, LLM Observability, Application Security,
CI Visibility, Data Jobs Monitoring, Continuous Profiling, Database Monitoring, and
Data Streams Monitoring. No product may be dropped because its collection belongs in the Agent.

## Baseline sources

| Repository | Reference | Commit |
| --- | --- | --- |
| Collector contrib | main | `16fa3257d56c299e115a558b1a668018a39990d5` |
| Python SDK | main | `d6ab482ea3fb3c8666e75ac1905a3325b8f80098` |
| Java SDK | master | `7b903a53644abc39f55b2fb21283546ae9801f35` |
| JavaScript SDK | master | `d62655e12494634bb53f8c0cd440f087b004ca63` |
| Datadog Agent | main | `761050392a732097645b76d9845d36629f1ef3dc` |

Recorded 2026-09-19. All four source repositories were clean and on the requested reference
branches. Preserve their state; do not update or install packages into them.
Collector `origin/main`, `upstream/main`, and the user's checkout initially matched.
GitHub API confirmed the fork default branch is `main`.

## Evidence conventions

Source-inspected behavior, executed local protocol tests, real SDK tests, and authenticated
backend/UI validation are separate levels. Mock HTTP acceptance proves transport only.
All recommendations and configuration extensions are proposals unless explicitly implemented.

## Existing research

[DBM notes](dbm.md) and [DSM notes](dsm.md) are preserved as supplied. They refer to an older
source location and incomplete language verification; new product reports supersede any
conflicting claims. Missing `01-mental-map` and other parts are not available in this checkout.

## Initial architectural conflicts

- The principles retain Agent database/message-system collection for DBM/DSM, while the task
  states all products are enabled through `pkg/trace`. Trace SDK paths first, but distinguish
  collection/check paths outside `pkg/trace`; do not invent a universal enable flag.
- A product configuration entry point cannot silently instantiate Collector pipelines.
- Proprietary controls must support unequivocal disablement, including control/return paths.
- Local product integration can proceed, but AGENTS.md requires human-written PR sections and
  a human-checked authorship checkbox before readiness. No AI-generated issue/PR comments.
