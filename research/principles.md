### Problem

We are in continuous debate on what product we want to include in our DDOT Collector (the object formerly known as DDOT Standalone). The discussion so far has been centered around what we can do for re:Invent 2026 rather than taking a step back and what are our principles in designing DDOT Collector, and how shall we approach the problem of products from the customer perspective and drive the discussion based on principles. This document is an attempt to rationalize some principles that we can apply to simplify the discussion and make clear which is responsible for which part of the setup.

### Principles

1. DDOT collector (including custom builds) is a superset of Upstream Collector and can be a drop-in replacement for upstream Collector.
2. Any Datadog telemetry/products that originate outside of Datadog Agent can use DDOT collector provided they only require HTTP proxy to Datadog backend and metadata enrichment.
   1. For many such products, Datadog Agent is just acting as an intermediary (reverse proxy \+ metadata enrichment), potentially all of these products can be Agentless.
   2. The OpenTelemetry team will work with the OTel Agent team in adding these products.
3. Any Datadog product that can be powered with an upstream SDKs & Collector, should work with an upstream version.
   1. For these products, the OpenTelemetry Team would work with the product teams to add their support to Upstream Collector where possible.
   2. Examples include Kubernetes dashboard that gets powered by Kubernetes receivers.
4. We will provide the DD Agent pipeline functionality for OTel customers via DDOT Collector.
   1. Examples include dual shipping , metrics aggregation, metrics V3 etc. The OTel Agent team would be responsible to ensure this parity.
5. Any proprietary feature made available on DDOT should allow for unequivocal disablement (via relevant opt-in/out mechanisms).
   1. A vendor-neutral offering of DDOT should always remain available to our customers.

Practical terms

| Product                          | Telemetry source                     | Placement (with DDOT collector & Agent on same host) | Compatibility ?                                                           | Primary owner               |
| :------------------------------- | :----------------------------------- | :--------------------------------------------------- | :------------------------------------------------------------------------ | :-------------------------- |
| **Live Debugging**               | DDOT SDK                             | DDOT Collector                                       | DD Native signal                                                          | DDOT SDK Team               |
| **LLM Observability**            | DDOT SDK                             | DDOT Collector                                       | Supports OTLP                                                             | DDOT SDK Team               |
| **Application Security**         | DDOT SDK                             | DDOT Collector                                       | DD Native signal                                                          | DDOT SDK Team               |
| **CI Visibility**                | DDOT SDK                             | DDOT Collector                                       | DD Native signal                                                          | DDOT SDK Team               |
| **Data Jobs Monitoring**         | DDOT SDK                             | DDOT Collector                                       | Evaluate upstream support where applicable                                | DDOT SDK Team               |
| **Continuous Profiling**         | DDOT SDK                             | DDOT Collector                                       | Evaluate upstream profiling support                                       | OTel Team \+ Profiling Team |
| **Database Monitoring (DBM)**    | Agent database checks                | Datadog Agent                                        | Add equivalent collection capability to Upstream Collector where possible | OTel Team \+ DBM            |
| **Data Streams Monitoring(DSM)** | Agent integrations / message systems | Datadog Agent                                        | Add equivalent collection capability to Upstream Collector where possible | OTel Team \+ DSM            |
