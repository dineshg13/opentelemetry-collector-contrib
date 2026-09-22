# DBM OTLP intake execution ledger

See [the approved plan](PLAN.md). Started 2026-09-22.

| Milestone | Status | Evidence / commit |
| --- | --- | --- |
| M0 Plan and isolated execution branches | Complete | `09ec9311b59` |
| M1 Contract and fixtures | Complete | `CONTRACT.md`; two version-1 OTLP fixture records; JSON and expected interval/precision checks passed. Production mapper/receiver fixture tests follow in M3/M4. |
| M2 Receiver counters | In progress | Isolated receiver agent; identity/baseline/reset regressions |
| M3 Receiver snapshots and complete statistics | Pending | |
| M4 Backend mapping | In progress | Shared mapper and strict contract validation |
| M5 Intake publisher and routing | In progress | Typed private-intake request construction; explicit tracks |
| M6 Real backend validation | Pending | |
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
