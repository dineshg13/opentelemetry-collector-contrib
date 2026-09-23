# DBM OTLP intake execution ledger

See [the approved plan](PLAN.md). Started 2026-09-22.

| Milestone | Status | Evidence / commit |
| --- | --- | --- |
| M0 Plan and isolated execution branches | Complete | `09ec9311b59` |
| M1 Contract and fixtures | Complete (`eac00be0874`) | `CONTRACT.md`; two version-1 OTLP fixture records; JSON and expected interval/precision checks passed. Receiver, mapper and consumer fixture tests passed in later milestones. |
| M2 Receiver counters | Complete | `44a0977d299` plus epoch follow-up `8ee39df8ad0`; full receiver unit suite passes, including native identity, exact counters, baseline/reset/eviction and calls-based execution regressions. |
| M3 Receiver snapshots and complete statistics | Complete locally | `9245f5be74f` source; `d0ddd98c774` real PG16 integration. Unit suite/schema/changelog pass; actual calls=3, rows=15, active=1, idle=2 plus restart/failure/limit checks. |
| M4 Backend mapping | Complete | `de9adf83e355` (dd-source); mapper/golden suite passes. `4ed78bb8c94c` (dd-go) tests actual DBM decoders and trace parser against exact mapper output with race detector. |
| M5 Intake publisher and routing | Local implementation complete; durable replay contract deferred | Private client `167ba5eeafe1`, routing/publisher `edf3a3d2d19b` (dd-source), authorization `1efca656593c` (dd-go). Rapid service 11/11 tests and applicable checks pass. |
| M6 Real backend validation | Deferred by user | User requested local working solution first; deployment and DD app verification later. Local source→mapper→consumer tests are complete. |
| M7 Opt-in plans | Complete locally | Receiver `5fdd25439f9`, mapper `c725b73cb958` (dd-source), real decoder/parser regression `44e7a919106b` (dd-go). Source unit/integration tests, mapper/CLI and full consumer race suite pass. |

## Environment

- Current Collector worktree: `/tmp/ddot-dbm-only`, branch `dinesh.gurumurthy/poc-dbm-only`.
- Earlier milestones and captured evidence were produced in `/tmp/ddot-dbm-otlp`
  on `dinesh.gurumurthy/poc-dbm-otlp-intake`; their source hashes remain unchanged.
- Current backend checkout: `/home/bits/dd/dd-source`, branch `dinesh.gurumurthy/dbm-poc`.
- Earlier backend milestones used `/tmp/dd-source-dbm-otlp`, branch `dinesh.gurumurthy/dbm-otlp-intake`.
- Original checkouts preserved; prior temporary worktrees had disappeared.
- Build output and isolated worktrees use `/tmp` because the main filesystem has limited free space.
- Local source, Collector, intake and consumer tests passed. Backend deployment and application verification are deferred.

## Initial environment checks

- Bazel diagnostic target built successfully using `/tmp/dbm-bazel`; diagnostic reported pre-existing hook/include setup warnings and a nested PATH warning. No global configuration changed.
- Explicit `kind-otel-dd` read-only check succeeded; existing PostgreSQL, application and both Collectors are healthy. The user's current context is another kind cluster and was not changed.
- Private intake server currently does not authorize the OTLP logs intake caller. A narrowly scoped service-auth policy change is a deployment prerequisite.
- Private intake does not expose a replay-deduplication key. No exactly-once guarantee claimed.

## Backend milestone evidence

- `dd-source` private client commit `167ba5eeafe1` validates trusted org range,
  track-specific JSON arrays, homogeneous database identity and encoded request
  size. Focused Bazel tests pass (1/1).
- `dd-go` authorization commit `1efca656593c1ee7d38277a01b6892a3e77ee02a`
  permits exactly namespace `rapid-otel`, service account `otlp-intake-logs`.
  Real authorization interceptor tests preserve existing callers and reject
  missing/wrong/prefix identities. Full grpcservice package race tests pass.
- Shared mapper tests pass (1/1), including strict neutral contract validation,
  query signature goldens, 64-bit native identifiers, second-to-millisecond
  conversion, trace context and snapshot preservation. Actual downstream decoder
  tests passed separately with the race detector. Optional local block metrics are not collected;
  existing consumers may show zeros for missing fields. No full Agent parity claim.
- Runtime publishing requires both `OTLP_INTAKE_LOGS_DBM_PUBLISH_ENABLED=true`
  and org feature flag `enable-otlp-intake-dbm-routing`. Defaults remain disabled.

## Reliability and deployment gates

Private intake can publish some envelopes before returning failure. Its protocol
has no deduplication key, and stable mapper keys alone do not prevent replay.
Producer idempotence does not cover repeated application RPC requests. The new
publisher does not add an automatic retry layer; the existing streaming processor
can still replay messages after a timeout. Accurate totals under retries are NOT
verified and are a rollout blocker. Do not enable this for production until a
durable ingestion/consumer idempotency contract is implemented and exercised.

Read-only deployment inspection succeeded. The DBM staging target is a shared
service; overriding its source branch would still replace shared staging. No
isolated backend test deployment has yet been established. Service-account
provisioning and the authorization commit must be deployed together in an
isolated environment before claiming real DBM acceptance. No backend deployment,
real DBM product readback, or trace-link success is claimed by these tests.

## User scope clarification

Backend deployment and real Datadog product verification are deferred by the user.
Finish and commit the local implementation and tests now. The environment question
is closed; no environment reference is required for this local handoff.

## Local implementation validation

- `rapid test -s otlp-intake-logs --repo-root /tmp/dd-source-dbm-otlp`
  passed all 11 service tests after the final timeout and batching fixes.
  Schema, Rapid definitions, Terraform generation and placement checks succeeded;
  Rapid marked the separate Terraform-linter check skipped. Its service tflint
  target passed. The real service binary was built successfully.
- Shared mapper exact-output golden passed. Actual `dd-go` query metrics,
  activity and FQT decoders and SQL comment/trace extraction passed with `-race`.
  Timestamp/interval and milliseconds-to-decoder-nanoseconds conversions are
  asserted. The legacy activity query-ID float64 representation can round native
  64-bit identifiers; canonical string query signatures remain authoritative.
- The generic Collector distribution built offline with no Datadog exporter,
  receiver, connector, Datadog extension or `pkg/trace` dependency. It exported
  six real OTLP/HTTP JSON requests, containing six query statistics collections
  and six activity snapshots, to a loopback capture. Disabling `query_monitoring`
  produced zero new requests. This is local wire-path evidence, not backend
  acceptance. Reproducible scripts and configuration are beside this ledger.

- Mapper CLI milestone: `3d39497ed1c7` (dd-source); JSON/protobuf conversion
  tests pass. Actual controlled PostgreSQL capture maps without rejection; all
  six real Collector HTTP exports also map without rejection (12 collections).
- Live consumer milestone: `52f84c8f57b1` (dd-go); actual source capture → mapper
  native output → real DBM decoders passed with `-race`, both fresh captures and
  pinned reproducible fixtures. Exact 3/15 counts, 1 active/2 idle connections,
  units, intervals, identity, both FQT links and trace extraction are asserted.

## Final local verification and independent review

- Local Collector harness and reproduction commit: `7635118d5c7`.
- Optional plan receiver commit: `5fdd25439f9`; default-disabled, bounded prepared
  EXPLAIN with generic parameter plans. Full receiver suite, generated schema,
  changelog and real PostgreSQL tests pass. The live UPDATE-plan test verified
  unchanged table contents; plan failures retain metric rows.
- Plan mapper compatibility commit: `c725b73cb958` (dd-source). A real consumer
  regression exposed PostgreSQL's outer EXPLAIN array where DBM required its
  single root object. The mapper now unwraps before obfuscation and signature
  calculation. Mapper and CLI suites pass (2/2), and the real plan parser accepts
  the regenerated native payload.
- Final Rapid service run after the plan fix passed all 11 tests and applicable
  checks. Final Collector build including code commit `8ee39df8ad0` succeeded;
  enabled/disabled wire verification passed again. Exact binary hash and times
  are in `evidence/collector-build.json` and `evidence/collector-wire.json`.
- Independent reviews checked source/intake contract correspondence, tenant
  identity, gating, complete snapshot batching, privacy, limits and evidence
  claims. The plan format defect was fixed. Documentation now states the DBM
  mapper's stricter 10,000-row/1 MiB limits, warm-cache assumption, and separate
  core wire versus optional-plan evidence. The counter-reset edge case was fixed in `8ee39df8ad0`; the documented
  older-extension detection limit and replay limitation remain.
- Only temporary local test processes were started. The owned OTLP capture
  servers and PostgreSQL port-forward were stopped. Disposable test databases
  were removed; existing cluster configuration and deployments were unchanged.

- Native optional-plan consumer milestone: `44e7a919106b` (dd-go). Actual sample
  decoder and PostgreSQL plan parser accept the mapped live plan; query/FQT
  linkage, canonical plan signature, identity, node/cost/rows and identifier
  obfuscation are asserted. Full gRPC package race tests passed. This live plan
  contains no predicate literals, so literal redaction is covered by separate
  unit fixtures rather than claimed from this capture.

## Reset-epoch follow-up

`8ee39df8ad0` adds before/after global reset-epoch reads for PostgreSQL 14+
monitoring and optional per-entry `stats_since` comparison. A reset followed by
counters growing from a prior 100 to 150 now establishes a new baseline instead
of emitting 50. Epoch read failures or a reset during collection purge baselines
and omit that metrics collection; activity can still succeed. Epoch metadata is
internal and does not alter the OTLP contract. PostgreSQL 13 legacy SQL remains
compatible.

Full receiver tests passed, including reset-overrun, unchanged epochs, missing
view/read failures, mid-fetch reset and independent activity cases. Real
PostgreSQL 16.15 integration passed with the new epoch reads, both with and
without optional plans (3 calls, 15 rows, 1 active/2 idle connections). These live
tests did not reset shared statistics; reset transitions are exercised in unit
fixtures. Older extension APIs without per-entry timestamps still cannot reveal
a targeted reset or eviction/re-creation entirely between scrapes when counters
recover past the prior sample. See `CONTRACT.md` for exact semantics.

## Final handoff

The final reset-aware Collector was built and ran the wire/disablement harness
successfully. Fresh core and optional-plan captures were separately mapped and
passed the complete real DBM gRPC/consumer race suite (1.481s). Exact final source
captures, native mapper output, PostgreSQL evidence and SHA-256 hashes are pinned
under `evidence/`, indexed by `final-validation.json`. Documentation edits were
pending during the last Collector build; receiver code was committed at
`8ee39df8ad0`.

The three local implementation branches are ready for review. Deployment,
authenticated service-to-service ingestion, Kafka/index processing, DBM product
readback and UI trace linking remain deferred. Durable replay deduplication is
not implemented and remains a rollout prerequisite. No success at those later
stages is inferred from local test results.

## DBM endpoint reference follow-up

Added all five requested Agent HTTP routes to the plan's endpoint reference,
including their matching private gRPC `track_type` values and current PoC coverage.
Metadata and health are supported by the existing backend service but are not
collected or mapped by this PoC. Kept the user-approved DBM processor path; the
EVP `databasequery` track receives processed events. Documentation only; no
runtime behavior changed or backend verification claimed.

## Development OTLP stack (2026-09-23)

The user authorized a running PostgreSQL/workload/Collector stack using the
workspace `.envrc_dev`. Created isolated namespace `dbm-otlp-dev` on
`kind-otel-dd`, targeting real development OTLP intake `otlp.datad0g.com`.
The supplied API key is valid and matches the Kubernetes Secret; the application
key remains local for readback. PostgreSQL 16.15 uses persistent storage. The
workload uses ddtrace 4.13.0rc1 / psycopg2 2.9.11 and actual SQL trace propagation.
The previously verified generic Collector binary includes receiver commit
`8ee39df8ad0` and has matching SHA-256 recorded in `dev-stack/evidence/`.

All three deployments are ready. Datadog readback at 2026-09-23T14:35:30Z returned
50 query-metrics records, 50 activity records, 96 PostgreSQL metric points and
5 indexed workload spans (queries are bounded, not total counts). Matched
38 SDK SQL trace contexts to stored activity rows; optional EXPLAIN data was
present. Collector queues were empty and no export-failure counters were found.
Seven initial workload database-connection retries occurred during PostgreSQL
startup; subsequent iterations succeeded without additional connection errors.

Independent deployment review found missing fresh-machine image acquisition,
credential rotation not restarting the Collector, and null traceparent handling
in verification. Fixed all three, redeployed and reran verification successfully.
Runnable assets, inspection/stop commands and sanitized evidence are in
`dev-stack/README.md`. The stack is left running. The backend DBM service changes
were not deployed; DBM feature routing, private intake and DBM UI verification
remain deferred. Ordinary log/metric/span visibility is not DBM product proof.
