# DBM OTLP intake execution ledger

See [the approved plan](PLAN.md). Started 2026-09-22.

| Milestone | Status | Evidence / commit |
| --- | --- | --- |
| M0 Plan and isolated execution branches | Complete | Initial documentation commit |
| M1 Contract and fixtures | In progress | Contract schema and consumer compatibility |
| M2 Receiver counters | Pending | |
| M3 Receiver snapshots and complete statistics | Pending | |
| M4 Backend mapping | Pending | |
| M5 Intake publisher and routing | Pending | |
| M6 Real backend validation | Pending | |
| M7 Opt-in plans | Pending | |

## Environment

- Collector worktree: `/tmp/ddot-dbm-otlp`, branch `dinesh.gurumurthy/poc-dbm-otlp-intake`.
- Backend worktree: `/tmp/dd-source-dbm-otlp`, branch `dinesh.gurumurthy/dbm-otlp-intake`.
- Original checkouts preserved; prior temporary worktrees had disappeared.
- Main filesystem has about 2.9 GiB free. Use `/tmp` for build output and isolated worktrees.
- No new backend or deployment success claimed. Tests and access will be established during implementation.
