# Architectural decisions and unresolved principles

Research decisions dated 2026-09-19. These are planning recommendations, not shipping support.
The [principles](principles.md) remain authoritative; conflicts are not resolved by silently
broadening Collector responsibilities.

| Decision | Evidence and rationale | Status / owner |
| --- | --- | --- |
| Cover exactly the eight listed products | DBM/DSM remain in inventory even when collection belongs in Agent | Applied to research; coordinator |
| Keep snapshot and runtime evidence separate | Python main and installed 4.13.0rc1 differ by 569 commits; Collector-linked Agent modules differ from Agent main | Applied; all owners |
| Reject universal configuration-only direct forwarding | Actual forwarder retains SDK paths, one origin, no local discovery/RC; backend paths often differ | Executed shared/product evidence |
| Allow narrowly proven configuration-only paths | Same-path Agent relay and SDK clients explicitly producing backend paths can use current forwarder, subject to auth/metadata semantics | Product-specific transport only; backend acceptance pending |
| Centralize Datadog product routing/control policy | Eight separate opaque receivers would duplicate auth/discovery/identity; current Datadog extension lacks SDK proxy | Proposed optional extension capability; OTel + OTel Agent |
| Reuse upstream generic mechanisms | Explicit path/header handling and response fidelity are reusable; Datadog RC and product allowlists are vendor policy | Upstream discussion needed before production changes |
| Keep standard signals in explicit pipelines | LLM OTLP contract is distinct from native LLM events; OpenLineage is a standard event source | Source/docs supported; backend validation pending |
| Reject unqualified native receiver parity | Actual AppSec payload loses structured trigger detail through current datadogreceiver; generic200 fallback hides missing product endpoints | Executed counterexample; APM + AppSec + OTel |
| Support multiple transport modes per product | LLM spans/evaluations can simultaneously use native traces, EVP and OTLP; Live Debugging includes trace/metric probe side paths | Canonical proposed modes/pipelines model |
| Retain Agent checks | SDK DBM correlation/DSM data is distinct from database/broker collection in the principles | Applied scope; DBM/DSM owners |
| Separate Collector and SDK disablement | Blocking a route cannot remove installed probes, undo local WAF, stop agentless SDK traffic or suppress all product fields in other pipelines | Explicit limitation; cross-team contract required |
| Preserve PR workflow while authorship input is pending | AGENTS.md requires human sections/attestation; consuming product branches into target would eliminate PR diffs | Separate review snapshot; no generated PR bodies/comments |

## Conflicts needing decisions

**“All products enabled through pkg/trace” versus collection ownership.** `pkg/trace` is the
starting point for SDK ingress and related processing, not the universal owner of DBM database
checks or message-system collection. Product reports follow outgoing paths into Event Platform
and other Agent packages where necessary. The instructions' blanket assertion is not applied
as a source fact.

**Proxy plus enrichment versus stateful control.** Live Debugging managed probes and several
SDK features need discovery, remote configuration or synchronous product API responses.
Full signed RC service behavior is more than stateless forwarding. A relay to an existing Agent
retains its dependency; embedding a supported RC provider requires OTel Agent/RC ownership
and explicit agreement under the principles. UI-created probes are unverified without it.

**Proxy plus enrichment versus native trace processing.** AppSec and some LLM/Data Jobs paths
require structured payload preservation, sampling/retention and native trace processing.
Principle 4 points to Agent pipeline parity, but does not identify the concrete component API.
Avoid placing trace transformation into an opaque HTTP extension by convenience. Reuse a
validated pipeline or retain Agent processing until a lossless mapping is demonstrated.

**Every product flag versus Agent-owned collection.** `database_monitoring.enabled` in the
proposal means SDK correlation only; `data_streams_monitoring.enabled` covers explicitly
supported SDK paths. These flags must not imply turning on Agent DB/broker integrations or
silently installing new Collector pipelines. Unsupported modes should fail validation.

**Unequivocal disablement versus shared native traffic.** Managed data/control routes can be
unambiguously closed. A stronger promise over shared trace attributes or backend automatic
conversion needs explicit pipeline enforcement and SDK controls. Do not present the proposed
extension switch as an already implemented global product kill switch.

## Deferred work

No Agent/SDK/Collector production component was changed, deployed or merged into the default
branch. Research prototypes live under research, use loopback/synthetic workloads, and label
any SDK stubs or model adapters. New implementation PRs need component-owner agreement and
product acceptance evidence; the research is intended to make those decisions reviewable.
