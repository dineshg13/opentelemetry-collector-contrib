# Progress checkpoint

Updated: 2026-09-19. Coordinator: `/root`.

Combined branch: `dinesh.gurumurthy/poc-all-products`, worktree `/tmp/ddot-research-all`.
Started from fork default `main` at `16fa3257d56c299e115a558b1a668018a39990d5`.
Original checkout and original untracked research inputs are preserved; only a WORKTREE.md
pointer was added there. Assembled review: `dinesh.gurumurthy/poc-review-snapshot`, worktree
`/tmp/ddot-research-review`. The intended combined branch retains product PR diffs while
publication input is pending. No authenticated Datadog backend/UI validation has occurred.

| Product | Agent | Branch suffix | Status | PR |
| --- | --- | --- | --- | --- |
| Live Debugging | `/root/live_debugging` | live-debugging | complete; reviewed in snapshot | pending human sections |
| LLM Observability | `/root/llm_observability` | llm-observability | complete; reviewed in snapshot | pending human sections |
| Application Security | `/root/application_security` | application-security | complete; reviewed in snapshot | pending human sections |
| CI Visibility | `/root/ci_visibility` | ci-visibility | complete; reviewed in snapshot | pending human sections |
| Data Jobs Monitoring | `/root/data_jobs_monitoring` | data-jobs-monitoring | complete; reviewed in snapshot | pending human sections |
| Continuous Profiling | `/root/continuous_profiling` | continuous-profiling | complete; reviewed in snapshot | pending human sections |
| Database Monitoring | `/root/database_monitoring` | database-monitoring | complete; reviewed in snapshot | pending human sections |
| Data Streams Monitoring | `/root/data_streams_monitoring` | data-streams-monitoring | complete; reviewed in snapshot | pending human sections |

Product branches use `dinesh.gurumurthy/poc-` + suffix. Product worktrees use
`/tmp/ddot-research-` + suffix. Shared worktree: `/tmp/ddot-research-shared`.

## Blockers and constraints

- PR descriptions: user must supply Description, Link to tracking issue, Testing, Documentation
  verbatim for each PR (or explicitly decline sections). Human must check authorship checkbox.
  Prepare deliverables first; do not post generated descriptions/comments or mark ready.
- Backend credentials/access: unavailable in the environment. Do not mistake local HTTP tests for product
  success in Datadog. Never record credential values.
- Sandboxed Git metadata is read-only; authorized worktree creation succeeded with escalation.
  Sandboxed GitHub network unavailable; escalated read-only API access succeeded.

## Completed

- Read principles, instructions, AGENTS.md, CONTRIBUTING.md, existing DBM/DSM research.
- Recorded clean source checkouts and exact commits.
- Identified default branch and created isolated combined branch.

## Next

The eight-product technical review is complete; final packaging and publication verification
are recorded in the closing checkpoint below. The async request for human PR sections is
pending. Once supplied (or explicitly declined), create product draft PRs, obtain
human attestation, apply review/check gates and merge into the intended combined branch;
leave the final combined PR draft/open. Authenticated product acceptance needs the access
and workload dependencies documented per product.

## Shared ownership and early evidence

`/root/shared_components` owns `research/shared/**`, current extension research, reusable
forwarder harness, and proposed configuration model in its own isolated worktree.

- Current contrib Datadog extension is metadata/status; SDK product proxy is proposed.
- Existing HTTP forwarder selects one origin and preserves incoming path/query.
- Datadog receiver mounts a catch-all `/` returning 200; unrecognized product requests can
  appear successful without being handled. `/info` capability evidence matters.
- Live Debugging needs snapshots/diagnostics/symbols and RC; existing logs disable gate is
  insufficient for unequivocal product disablement.
- Pinned Python LLM code can use APM transport by default; native LLM traffic is not
  universally EVP JSON forwarding. Language defaults must be documented separately.
- Conventional Datadog API/application key environment variables are absent. Backend product
  validation remains blocked pending an approved test organization and credentials.

## Integration checkpoint: first products complete

- Shared component report/harness: commit `2496c1cb3be`, integrated into combined branch.
- Live Debugging: `c00317469b436b3b57dc6e558caa0494d34f8df7`. Four source-component
  assertions pass; full Python 4.13.0rc1 local probe emitted 18 snapshots plus diagnostics to
  a mock through actual forwarder and experimental adapter. No signed RC/backend/UI result.
- LLM Observability: `449c9a0cbe8657fdc69fc495db1d384830b19c4c`. Pinned JS writer
  fixtures and full Python 4.13.0rc1 emitted native EVP paths through actual forwarder; strict
  mock rejected preserved prefixes. Rewritten controls passed. No backend result.
- Shared canonical configuration changed to `modes` lists and `pipelines` maps because a
  product can use native traces, native event/evaluation uploads, and OTLP simultaneously.
- Additional Python release environment installed only in /tmp; exact wheel/dependency
  hashes recorded in `python-runtime.json` and `validation/python-requirements.txt`.
- Shared and first two products assembled on `dinesh.gurumurthy/poc-review-snapshot`, at
  `/tmp/ddot-research-review`. This separate snapshot preserves product diffs against the
  intended combined PR target until human-authored PR sections are supplied.
- The async request for all nine PRs' required human sections is pending. No PR created;
  no authorship checkbox checked; no issue/PR comments posted. Unmodified templates are
  under `publication/pr-bodies/`. Human attestation also gates product PR readiness.
- SSH commit signing subsequently failed with signing-agent communication error. Used
  per-command `commit.gpgsign=false` for isolated research commits, retaining Assisted-by
  trailers. Global/repository signing settings were not changed.
- Research integrity check on snapshot: 2/8 product reports, 33 local links, 48 pinned
  source files checked; passing for completed inventory. Full-inventory check awaits queue.

## Third product and publication recovery

AppSec commit `eef3baba88b8e3b260c2afd61d49f37d94ca9ce6` integrated into review snapshot.
Actual Python WAF output (published 4.13.0rc1 plus Flask) passed through current forwarder;
replay of exact payload through current Datadog receiver lost structured trigger details.
Legacy scalar markers remain, so HTTP 200 is not compatibility. WAF/IAST/RASP/API Security
paths researched; SCA breadth remains an explicit product-scope question.

Initial shared/Live/LLM/combined/review branches pushed to fork. A later SSH push failed
because signing-agent communication was unavailable and global Git rewrote HTTPS to SSH.
HTTPS push succeeded with per-command `GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1`
and `gh auth git-credential`; no stored Git or SSH settings changed.

Transient sandbox No-space-left error recovered without file deletion; host df/inodes
showed available space. No user caches were removed.

## Fourth product and additional runtime evidence

Data Jobs commit `50cf528275ca2de651b4debab8f5f13b157426e3` integrated into review snapshot.
Actual OSS OpenLineage Python 1.45.0 client passes backend-path gzip/Bearer/response tests
through current forwarder; Agent-prefixed path is preserved and rejected by strict mock.
Java native Spark requires separate trace processing and remains runtime/backend unverified.
Python/JS lack dedicated DJM instrumentation in inspected SDK source; external OL is separate.

Profiling experiments now demonstrate real Java 1.66.0 JFR uploads through unchanged forwarder
using exact profiling URL override; Python native default and Agent URL-prefix variants fail
strict backend path contract. Final report in progress. Java artifact checksum recorded in
`java-runtime.json`; runtime used by product must be taken from product results, not assumed
from coordinator shell inventory. CI is also exercising manual Java product APIs.

Supplemental integrations-core DBM sources: clean master snapshot
`916e4f4364609494986417f9a5efc4b3b0281e52`; SDK/Agent original checkouts still preserved.

## Fifth product complete; all workstreams assigned

CI commit `37ff6cac1f271277ef8db44bd474bb740fe424dc` assembled in snapshot. Real Python
legacy/default pytest and real Java 1.66.0 manual APIs through actual forwarder plus bounded
research adapter validate test hierarchy events, Python coverage, settings/known/skippable/
test-management requests and git search/packfile. Pinned JS source components have explicit
stubs. No authenticated backend, full Java/JS test framework or optimization behavior result.
Current Python default plugin can disable CI while pytest succeeds if discovery is missing;
legacy fallback behavior must not be generalized to the new plugin.

DSM is now assigned; every product has its own dedicated owner and isolated worktree.
Independent integration review follows completed profiling; DBM/DSM results will be added
to its review scope as they land.

## Independent review in progress

Profiling commit `36ab96ae274f3ea9d57c16a5ba3cc0c55ff34357` integrated; six reports
assembled. Reviewer `/root/integration_review` independently verified critical source claims
and reran JS CI component checks and all shared forwarder tests successfully.

Resolved findings: Live Debugging singular mode corrected to modes list; CI primary example
now disabled; historical CI JDK runtime explicitly unrecorded; profiling actual Microsoft
JDK 21 distinguished from coordinator shell Temurin 25; CI source path portable; documentation
checker gained source-root override and scope clarification. No product runtime behavior
changed by these documentation corrections. Full review waits for DBM and DSM reports.

## Closing checkpoint: eight products reviewed

All eight deliverables are assembled on the review snapshot. Independent reviewer
`/root/integration_review` found no remaining actionable technical research issue after
the recorded corrections. [Review findings and exact scope](INTEGRATION_REVIEW.md) remain
separate from product acceptance and PR readiness. Historical checkpoints above retain
their original in-progress state; this section and the top table are current.

| Product | Research commit | Review correction commit |
| --- | --- | --- |
| Live Debugging | `c00317469b436b3b57dc6e558caa0494d34f8df7` | `625ccdcece9` |
| LLM Observability | `449c9a0cbe8657fdc69fc495db1d384830b19c4c` | none required |
| Application Security | `eef3baba88b8e3b260c2afd61d49f37d94ca9ce6` | none required |
| CI Visibility | `37ff6cac1f271277ef8db44bd474bb740fe424dc` | `4bfb449d7ec`, `e00a24d4931` |
| Data Jobs Monitoring | `50cf528275ca2de651b4debab8f5f13b157426e3` | none required |
| Continuous Profiling | `36ab96ae274f3ea9d57c16a5ba3cc0c55ff34357` | common runtime-manifest clarification |
| Database Monitoring | `5ad1da5a693239096379003b459b713a4db3488f` | none required |
| Data Streams Monitoring | `83b096b8f677136d502290ade3d40684554ec4af` | `7914650dd0ade3c94f4d08e176b6ce876d78c975` |

DBM's five cases validate actual Python propagation/native writer and receiver fields;
the SQL-resource repair is tested at an imported mapping helper, not the full exporter.
DSM's five Python and two Java cases were rerun after fixture isolation fixes. Java records
Microsoft JDK 21.0.11+10-LTS executable/compiler paths and the exact agent-jar hash from the
same invocation. DSM backend/brokers, signed RC and schema/correlation experiences remain
unverified, as do each other report's stated runtime/backend gaps.

Artifact checks passed across all eight products: 12 Python AST parses, 12 JSON parses,
3 Node syntax checks and 4 Go formatting checks. DSM changed artifacts were checked again
after its rerun. Baseline `git diff --check` passed. All original four research inputs and
all nine unmodified PR templates were compared byte-for-byte successfully. The four SDK/Agent
source repositories and supplemental integrations repository remain clean at their pins.

No PR exists yet, and no product PR is marked ready or merged. Product/common branches may
be synchronized with shared documentation while retaining product-only diffs against the
intended combined target. Final branch push and complete link-check results follow below.
