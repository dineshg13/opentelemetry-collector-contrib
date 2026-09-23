Investigate and validate how to make Datadog SDK products work end to end through the OpenTelemetry Collector. Produce code-backed research, working prototypes, and recommendations that engineering teams can use to plan implementation.

Work autonomously using parallel agents. I may be away from my desk, so continue without waiting for routine confirmation. Keep findings and progress documented so the work can resume after an interruption.

1. **Start with the principles and existing research.**

   Read the principles document in `research/` first. Use it to understand the intended architecture, constraints, and product scope. Then review the existing research to identify established findings and gaps.

   Use the products listed in the principles document as the complete research inventory. Flag ambiguities or conflicts with the principles explicitly.

   Locate the relevant Collector and extension implementations using the existing research and repository contents. Record which implementations and versions you investigate.

2. **Use the SDK and Agent implementations as primary sources.**

   The local repositories are:

   | Implementation | Repository           | Reference branch |
   | -------------- | -------------------- | ---------------- |
   | Python SDK     | `~/dd/dd-trace-py`   | `main`           |
   | Java SDK       | `~/dd/dd-trace-java` | `master`         |
   | JavaScript SDK | `~/dd/dd-trace-js`   | `master`         |
   | Datadog Agent  | `~/dd/datadog-agent` | main             |

   Record the commit identifiers used for research. Preserve existing local work when inspecting repositories.

   All products in scope are enabled through the Agent’s `pkg/trace` package. Start the Agent investigation there and follow relevant code paths into other packages as needed. Examine how products are enabled, endpoints are registered, requests and responses are handled, and backend destinations are selected.

3. **Assign dedicated agents with clear ownership.**

   The primary agent acts as coordinator. After reviewing the principles and creating the product inventory:

   - Assign at least one dedicated agent to each product. Each product agent owns the complete investigation across Python, JavaScript, Java, and the Agent implementation.
   - Assign a shared-components agent to investigate the forwarder extension, Datadog extension, common configuration, and functionality needed by multiple products.
   - Run independent investigations concurrently within the available agent limit. Queue remaining products and start them as slots become available.
   - The coordinator maintains the research index, resolves architectural inconsistencies, manages integration, and produces the final synthesis.

   Give each agent explicit deliverables and ownership of its files. Keep product-specific research and prototypes in separate directories. Coordinate shared-component changes through the shared-components agent, and shared index updates through the coordinator.

4. **Trace each product from the SDK through the Agent to the backend.**

   For each product, investigate Python, Java, and JavaScript where supported. Document:

   - How the SDK enables the product, collects data, and constructs requests.
   - Payload formats, endpoints, protocols, headers, authentication, and configuration.
   - How the Agent enables and handles the product through `pkg/trace`.
   - Every relevant Agent responsibility, including forwarding, enrichment, transformation, aggregation, discovery, and other processing.
   - Response handling, capability negotiation, remote configuration, or other return paths required for the product to function.
   - The backend destination and the requirements for the product experience to work.
   - Differences between languages and unsupported combinations.

   Trace actual implementation paths rather than relying only on documentation. Cite repository paths, relevant symbols, and researched commits. Distinguish verified behavior from assumptions and open questions. Do not assume behavior is identical across SDKs.

5. **Evaluate where required Agent functionality belongs in the Collector.**

   For every Agent responsibility, determine whether it is necessary in the Collector path. Evaluate placement in an existing Collector component, a receiver, the Datadog extension’s HTTP proxy, the forwarder extension, or another component justified by the principles.

   Explain which behavior must be preserved and why the proposed placement is appropriate. Identify opportunities to reuse existing components or contribute functionality upstream.

   For products that appear to require only HTTP forwarding, verify that assumption against the SDK and Agent code. Compare three or four concrete approaches where viable, including:

   - Configuring the existing forwarder extension without code changes.
   - Extending the forwarder extension where configuration alone is insufficient.
   - Adding forwarding support to the Datadog extension.
   - Using a dedicated receiver or another existing component where justified.

   Compare correctness, SDK compatibility, configuration complexity, maintenance cost, and alignment with the principles. Explain why a candidate is unsuitable rather than inventing alternatives to meet a count.

6. **Investigate product configuration in parallel.**

   Explore what it would take to expose support for every product through the Datadog extension, with explicit per-product enable and disable settings in Collector configuration.

   Propose example configuration and explain defaults, dependencies, validation, and runtime behavior. Distinguish settings owned by the extension from receivers, processors, exporters, or pipelines that must be configured elsewhere in the Collector.

   Identify what is supported today, what requires implementation changes, and any limitations of using the Datadog extension as the configuration entry point.

7. **Validate proposed paths with working prototypes.**

   Build targeted, minimal prototypes to resolve the important architectural questions. Prioritize testing whether existing forwarder configuration can make forwarding-only products work with Datadog SDKs.

   Record reproducible SDK and Collector configurations, commands, prerequisites, and observed results. Explicitly state which products and SDK languages each prototype validates.

   Where backend access is available, verify that the product actually functions in Datadog. Successful HTTP forwarding alone is insufficient evidence of end-to-end success.

   If full validation is blocked, document exactly what was tested, what remains unverified, and what is needed to complete it.

   When shared implementations change, update affected product research and repeat the relevant validation.

8. **Use product integration branches and one combined integration branch.**

   In the repository containing `research/`, identify the default branch and use this branch naming scheme:

   | Purpose                           | Branch                                 |
   | --------------------------------- | -------------------------------------- |
   | Each product’s integration branch | `dinesh.gurumurthy/poc-{product-name}` |
   | Combined integration branch       | `dinesh.gurumurthy/poc-all-products`   |

   Replace `{product-name}` with a consistent lowercase, hyphenated product name. Reserve `all-products` for the combined branch.

   Create the combined integration branch from the repository’s default branch. Create each product branch from the combined integration branch and give each product agent an isolated Git worktree.

   Open one draft PR per product targeting `dinesh.gurumurthy/poc-all-products`. Each product PR must include:

   - Research findings and source references.
   - SDK and Agent behavior, including the relevant `pkg/trace` implementation.
   - Architecture options and the recommended Collector path.
   - Prototype code and configuration where applicable.
   - Validation results, limitations, and unresolved questions.
   - Follow-up implementation work and cross-team dependencies.

   The coordinator reviews each product’s evidence, checks alignment with the principles, and resolves conflicts. Once a product PR passes the applicable checks, mark it ready and merge it into `dinesh.gurumurthy/poc-all-products`. Leave blocked or incomplete product PRs as drafts with their status documented.

   Integrate shared-component changes through the coordinator and shared-components agent.

   Open one final draft PR from `dinesh.gurumurthy/poc-all-products` to the repository’s default branch. It must contain the combined work, link every product PR, and explain the overall architecture, coverage, validation, remaining gaps, and recommended implementation sequence.

   If implementation changes are needed in other repositories, keep the necessary code PRs separate and link them from the corresponding product PR and final integration PR. Follow the same branch naming convention in those repositories.

9. **Work unattended and document progress continuously.**

   Continue through research, prototyping, documentation, validation, commits, branch pushes, product PR creation, and integration without waiting for routine confirmation.

   You are authorized to merge completed product PRs into the combined integration branch after review and applicable checks. Leave the final integration PR open for my review.

   Make reasonable, reversible decisions using the principles and available evidence. Record assumptions and their implications.

   If a workstream encounters missing credentials, unavailable services, restricted access, or a decision that cannot be resolved from the principles:

   - Record the blocker and the exact action needed to resolve it.
   - Complete everything possible without that dependency.
   - Continue other workstreams.
   - Label affected conclusions and validation as incomplete.

   Keep experiments isolated. Leave deployment, production configuration changes, and merging into the default branch for my review.

   Update `research/` after meaningful milestones with findings, source references, experiments, configurations, decisions, rejected approaches, blockers, and next steps.

   Maintain a durable research index and progress checkpoint containing product status, agent ownership, branches, PR links, completed validation, and next actions. Build on existing research documents and link to authoritative findings.

10. **Produce a synthesis suitable for cross-team planning.**

Deliver:

- An end-to-end architecture map showing SDK, Agent, Collector, and backend responsibilities, including the current path and proposed Collector paths.
- A product coverage matrix covering every product in the principles document and all three SDK languages.
- Per-product research covering SDK behavior, Agent implementation, Collector requirements, and validation status.
- An options comparison and recommended implementation path for each product.
- A proposed Datadog extension configuration model.
- Reproducible prototypes and their results.
- A prioritized implementation plan with dependencies, open decisions, and teams that need to be involved.

Clearly distinguish working paths, researched proposals, and blocked or unverified paths. The result should provide a clear mental model of how the pieces fit together and concrete engineering work that the relevant teams can act on.

1. **Finish with an independent integration review and handoff.**

Have an agent review the combined result for missing products, unsupported claims, inconsistent configuration, duplicated shared functionality, and gaps between prototype results and stated conclusions.

Resolve actionable findings and run the checks needed for the combined changes.

Finish when every product has either completed deliverables or an explicitly documented blocker, all completed work is integrated, and the final draft PR accurately represents the result.

Provide a concise handoff with the final PR link, product PR links, what works end to end, what remains unverified, and the decisions or access needed from me.
