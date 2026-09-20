# Draft publication status

The user explicitly approved: “Yes—override that rule for these nine draft PRs,” referring
to AGENTS.md's human-written-description rule and basic summaries. That authorization also
covers the updated implementation summaries for the same eight products and final draft.
All descriptions preserve the four template sections and unchecked human-authorship boxes.

The eight open product drafts (#11–#18) target `dinesh.gurumurthy/poc-all-products`.
The [final draft #20](https://github.com/dineshg13/opentelemetry-collector-contrib/pull/20)
uses `dinesh.gurumurthy/poc-all-products` into the fork's `main`, as new-instr.md requires.
The obsolete research-only #19 is closed without merge; its historical branch is retained.
All active drafts target the user's fork `dineshg13/opentelemetry-collector-contrib`.

Authenticated readback now verifies six core product paths; full eight-product coverage
remains incomplete because of the documented AppSec, DBM, SDK/control and feature gaps. Product changes are
staged in the combined branch for joint execution; product PRs retain scoped nonempty
reviewable diffs and remain unmerged drafts. No PR was marked ready or merged, no issue/PR
comments were posted, and fork `main` remains at `16fa3257d56c299e115a558b1a668018a39990d5`.
See [the publication index](README.md) and [verification snapshot](verification.json).
