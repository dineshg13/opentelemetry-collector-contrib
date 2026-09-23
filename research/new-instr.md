Build a working end-to-end proof of concept for every product listed in the principles document. Research supports implementation; research documents alone are not a completed deliverable.

The goal is an OpenTelemetry-native solution that makes Datadog products work with Datadog SDKs through the OpenTelemetry Collector, with minimal vendor-specific coupling. Build on existing OpenTelemetry Collector contrib components wherever possible.

Work autonomously using parallel agents. I may be away from my desk. Continue through implementation, builds, deployment to the kind cluster, verification, documentation, and PR creation without waiting for routine confirmation.

1. **Read the principles and inspect the existing implementations.**

   Start with the principles document and existing material in `research/`. Use the products listed there as the complete implementation inventory.

   Use these local repositories as primary sources:

   | Implementation | Repository           | Reference branch                |
   | -------------- | -------------------- | ------------------------------- |
   | Python SDK     | `~/dd/dd-trace-py`   | `main`                          |
   | Java SDK       | `~/dd/dd-trace-java` | `master`                        |
   | JavaScript SDK | `~/dd/dd-trace-js`   | `master`                        |
   | Datadog Agent  | `~/dd/datadog-agent` | Inspect and record the checkout |

   Locate the relevant Collector, contrib, and extension repositories. Record the commits used, and preserve existing local work.

   All products in scope are enabled through the Agent’s `pkg/trace` package. Use it to understand existing behavior, following calls into other packages where necessary. Treat this implementation as a behavioral reference, not the default architecture for the Collector solution.

2. **Follow these implementation constraints.**

   - Do not use the Datadog receiver.
   - Use Datadog SDKs’ OTLP export paths for traces, logs, and metrics, preserving the OpenTelemetry data model. Receive these signals through standard OpenTelemetry components.
   - Verify the required OTLP support and configuration for each SDK language and version. If a capability is missing, identify the necessary SDK change or record the blocker; do not introduce the Datadog receiver as a fallback.
   - Prefer existing contrib receivers, processors, connectors, exporters, and extensions. Extend an appropriate existing component when functionality is missing.
   - Prefer standard protocols, semantic conventions, and configuration. Keep Datadog-specific requirements isolated and explicit.
   - Minimize dependence on `pkg/trace`. Do not embed the Trace Agent or copy substantial Agent product logic into the Collector.
   - Use a minimal `pkg/trace`-based proxy path only when no suitable OpenTelemetry component can be reused or reasonably extended. Document the alternatives investigated and why this dependency is necessary.

   Distinguish standard telemetry ingestion from product-specific protocols and control paths. A product-specific forwarding requirement is not a reason to route ordinary traces, logs, or metrics through a Datadog receiver.

3. **Implement concrete solutions for each product.**

   Trace the product from SDK configuration through Collector processing or forwarding to the real Datadog backend. Identify the Agent behavior that must be preserved, including required headers, metadata, payload handling, responses, discovery, and capability negotiation.

   Then implement the smallest complete path using the architectural constraints above.

   For profiling and other products that primarily require HTTP forwarding:

   - Implement forwarding through the Datadog extension.
   - Implement an alternative using an existing HTTP forwarding/proxy extension where technically viable.
   - Build and test both alternatives independently, then recommend a default based on correctness, configuration, maintainability, and reuse.
   - Keep the extension’s role limited to forwarding and the minimum protocol handling or metadata required for compatibility. Place broader telemetry processing in appropriate OpenTelemetry components.
   - If an alternative cannot work, document the concrete limitation and continue with the viable implementation.

   For DBM and other products needing additional collection or processing, investigate whether an existing contrib receiver can be extended with the required functionality. Implement that extension where appropriate rather than importing Agent behavior wholesale.

   Every product must end with runnable code and configuration, or a precise blocker explaining why a working implementation could not be completed.

4. **Provide explicit product configuration.**

   Investigate and implement per-product enable and disable settings through the Datadog extension where appropriate.

   Keep configuration ownership accurate: receivers, processors, exporters, and pipelines must remain explicitly configured where the Collector requires them. Do not assume an extension can dynamically create pipeline components.

   Provide working configuration examples for each product and a combined configuration. Explain defaults, dependencies, required credentials, and behavior when a product is disabled.

5. **Use dedicated agents and coordinated ownership.**

   The primary agent acts as coordinator.

   - Assign at least one dedicated agent per product. Each owns implementation, relevant SDK coverage, tests, deployment requirements, documentation, and its product PR.
   - Assign a shared-components agent to common forwarding, Datadog extension configuration, and reusable Collector functionality.
   - Run independent work concurrently within the available agent limit; queue remaining products.
   - Give agents isolated Git worktrees and clearly assigned files.
   - Coordinate shared files and components through the coordinator.
   - Coordinate kind deployments so agents do not overwrite one another’s resources. Use separate namespaces or releases for independent experiments where useful.

   The coordinator owns the combined build, shared deployment, integration testing, research index, and final PR.

6. **Build and deploy to the kind cluster.**

   Inspect the existing kind cluster and deployment configuration. Confirm the Kubernetes context points to the intended kind cluster before making changes.

   Build the actual Collector distribution and any required application images. Load or publish them as appropriate for the cluster, and deploy the SDK applications and Collector configurations.

   Identify and remove the existing fake Datadog deployment and its associated mock routing from the kind environment. Replace mock destinations with real Datadog backend configuration using available credentials. Keep changes scoped to the PoC resources and preserve unrelated workloads.

   Ensure the final configurations, manifests, and scripts no longer depend on fake Datadog.

   If real backend credentials or access are unavailable, complete the build and local deployment work, document the exact missing prerequisite, and mark backend verification as blocked. A mock response must never be reported as successful end-to-end verification.

7. **Verify product behavior, not just transport.**

   For each product:

   - Build the changed components and Collector distribution successfully.
   - Run meaningful tests for new or modified behavior.
   - Deploy the implementation to kind and verify workload readiness.
   - Generate representative activity using the relevant SDK application.
   - Verify the expected telemetry or product requests pass through the intended Collector components.
   - Verify backend acceptance and the resulting product behavior in Datadog.
   - Verify any required response or control path.
   - Check product enable and disable behavior.
   - Record exactly which SDK languages and versions were exercised.

   Successful HTTP responses, healthy pods, and mock acknowledgements are not sufficient proof that the product works.

   Record reproducible commands, configurations, timestamps, and observable evidence. Separate code inspection, local tests, and real backend verification in the results.

   After integrating shared changes, repeat the relevant checks and verify the combined deployment with all implemented products enabled together.

8. **Create one PR per product and one final integration PR.**

   Use this branch naming scheme:

   | Purpose                           | Branch                                 |
   | --------------------------------- | -------------------------------------- |
   | Each product’s integration branch | `dinesh.gurumurthy/poc-{product-name}` |
   | Combined integration branch       | `dinesh.gurumurthy/poc-all-products`   |

   Replace `{product-name}` with a lowercase, hyphenated product name. Reserve `all-products` for the combined branch.

   In the repository containing the PoC work and `research/`, create the combined branch from the default branch. Create product branches from the combined branch.

   Open one draft PR per product targeting `dinesh.gurumurthy/poc-all-products`. Include implementation, configuration, deployment instructions, tests, verification evidence, source references, and remaining limitations.

   The coordinator reviews each product and merges completed product PRs into the combined branch after applicable checks pass. Keep incomplete or blocked product PRs as drafts with clear status.

   If changes span other repositories, create the necessary code PRs there using the same naming convention and link them from the product PR. Record exact build dependencies so the combined PoC is reproducible.

   Open one final draft PR from `dinesh.gurumurthy/poc-all-products` to the default branch, linking every product PR and describing the combined working solution.

9. **Continue unattended and keep durable progress records.**

   You are authorized to make implementation changes, run builds and tests, deploy and update PoC resources in the kind cluster, remove the fake Datadog resources, commit and push branches, open PRs, and merge completed product PRs into the combined integration branch.

   Leave the final PR open for my review. Do not merge into the default branch or modify production deployments.

   Make reasonable, reversible decisions based on the principles and implementation evidence. Document assumptions.

   When blocked, record the missing dependency or decision, complete all unaffected work, and continue other product workstreams. Do not repeatedly retry an unavailable dependency or wait for routine input.

   Update `research/` after meaningful milestones. Maintain an index and checkpoint containing product status, agent ownership, branches, PR links, implementation decisions, deployment status, validation results, blockers, and next actions.

10. **Finish with working deliverables and an independent review.**

Deliver:

- Working product implementations and a reproducible Collector build.
- Per-product and combined configurations.
- Application examples and kind deployment manifests or scripts.
- A kind deployment using real Datadog destinations, with fake Datadog removed.
- A product and SDK coverage matrix showing what was implemented and verified.
- Architecture diagrams explaining the actual implemented paths.
- A comparison of the forwarding alternatives that were implemented and tested.
- Documentation of remaining vendor-specific dependencies and why they are necessary.
- Product PRs and the final integration PR.

Have an independent agent review the combined implementation for architectural violations, unnecessary `pkg/trace` dependencies, hidden mock usage, integration problems, and unsupported claims of success. Resolve actionable findings.

A product is complete only when its implementation builds, runs in kind, and has evidence of end-to-end behavior against the real backend. Otherwise, label it incomplete or blocked.

Finish with a concise handoff containing PR links, what works, how to reproduce it, what remains unverified, and any decisions or access needed from me.
