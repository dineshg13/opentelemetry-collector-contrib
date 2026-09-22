# DBM OTLP intake execution ledger

See [the approved plan](PLAN.md). Started 2026-09-22.

| Milestone | Status | Evidence / commit |
| --- | --- | --- |
| M0 Plan and isolated execution branches | Complete | `09ec9311b59` |
| M1 Contract and fixtures | Complete (`eac00be0874`) | `CONTRACT.md`; two version-1 OTLP fixture records; JSON and expected interval/precision checks passed. Production mapper/receiver fixture tests follow in M3/M4. |
| M2 Receiver counters | Complete | `44a0977d299`; full receiver unit suite passes, including native identity, exact counters, baseline/reset/eviction and calls-based execution regressions. |
| M3 Receiver snapshots and complete statistics | Complete locally | `9245f5be74f` source; `d0ddd98c774` real PG16 integration. Unit suite/schema/changelog pass; actual calls=3, rows=15, active=1, idle=2 plus reset/failure/limit checks. |
| M4 Backend mapping | Complete | `de9adf83e355` (dd-source); mapper/golden suite passes. `4ed78bb8c94c` (dd-go) tests actual DBM decoders and trace parser against exact mapper output with race detector. |
| M5 Intake publisher and routing | Local implementation complete; durable replay contract deferred | Private client `167ba5eeafe1`, routing/publisher `edf3a3d2d19b` (dd-source), authorization `1efca656593c` (dd-go). Rapid service 11/11 tests and applicable checks pass. |
| M6 Real backend validation | Deferred by user | User requested local working solution first; deployment and DD app verification later. Local source→mapper→consumer tests continue. |
| M7 Opt-in plans | Pending | |

## Environment

- Collector worktree: `/tmp/ddot-dbm-otlp`, branch `dinesh.gurumurthy/poc-dbm-otlp-intake`.
- Backend worktree: `/tmp/dd-source-dbm-otlp`, branch `dinesh.gurumurthy/dbm-otlp-intake`.
- Original checkouts preserved; prior temporary worktrees had disappeared.
- Main filesystem has about 2.9 GiB free. Use `/tmp` for build output and isolated worktrees.
- No new backend or deployment success claimed. Tests and access will be established during implementation.

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
  tests are being added separately. Optional local block metrics are not collected;
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
