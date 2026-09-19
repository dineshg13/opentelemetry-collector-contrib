#!/usr/bin/env python3
"""Check the research inventory, local links, and pinned source-link targets.

This is a documentation integrity check, not SDK or backend validation.
Source repositories are only read. Use --allow-incomplete during research.
"""

import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import unquote, urlsplit


PRODUCTS = (
    "live-debugging", "llm-observability", "application-security", "ci-visibility",
    "data-jobs-monitoring", "continuous-profiling", "database-monitoring",
    "data-streams-monitoring",
)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--allow-incomplete", action="store_true")
    args = parser.parse_args()
    research = Path(__file__).resolve().parents[1]
    manifest = json.loads((research / "sources.json").read_text())
    sources = {
        item["repository"]: (Path(item["path"]), item["commit"])
        for item in manifest["sources"]
    }
    sources["open-telemetry/opentelemetry-collector-contrib"] = (
        research.parent, manifest["collector"]["commit"]
    )
    errors = []
    missing = []
    for product in PRODUCTS:
        report = research / "products" / product / "README.md"
        if not report.is_file():
            missing.append(str(report.relative_to(research)))
    if missing and not args.allow_incomplete:
        errors.extend("Missing product report: " + item for item in missing)

    checked_sources = set()
    link_count = 0
    # Original input documents are preserved verbatim, including historical links.
    legacy = {"dbm.md", "dsm.md", "instructions.md", "principles.md"}
    for document in research.rglob("*.md"):
        if document.parent == research and document.name in legacy:
            continue
        for match in re.finditer(r"\[[^\]]*\]\(([^\s)]+)\)", document.read_text()):
            link = match.group(1).strip("<>")
            url = urlsplit(link)
            if not url.scheme and not link.startswith(("/", "#")):
                target = document.parent / unquote(url.path)
                if not target.exists():
                    errors.append(f"{document.relative_to(research)}: missing link {link}")
                link_count += 1
            if url.netloc != "github.com":
                continue
            parts = url.path.strip("/").split("/")
            if len(parts) < 5 or parts[2] != "blob":
                continue
            repo = "/".join(parts[:2])
            if repo not in sources:
                continue
            path, expected_commit = sources[repo]
            commit, filename = parts[3], "/".join(parts[4:])
            if not re.fullmatch(r"[0-9a-f]{40}", commit):
                errors.append(f"{document.relative_to(research)}: unpinned source {link}")
                continue
            # Reports may intentionally cite another pinned revision; verify its object too.
            key = (repo, commit, filename)
            if key in checked_sources:
                continue
            checked_sources.add(key)
            result = subprocess.run(
                ["git", "-C", str(path), "cat-file", "-e", f"{commit}:{filename}"],
                capture_output=True, text=True, check=False,
            )
            if result.returncode:
                errors.append(f"{document.relative_to(research)}: missing source {link}")
    for error in errors:
        print(error, file=sys.stderr)
    print(f"Inventory: {len(PRODUCTS) - len(missing)}/{len(PRODUCTS)} product reports; "
          f"{link_count} local links; {len(checked_sources)} pinned source files checked.")
    print("Documentation checks " + ("FAILED" if errors else "passed") +
          "; this does not establish product compatibility or backend success.")
    return bool(errors)


if __name__ == "__main__":
    sys.exit(main())
