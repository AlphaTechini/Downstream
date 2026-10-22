#!/usr/bin/env python3
"""Check that documentation points at evidence that exists, and that evidence is clean.

1. Every relative markdown link in README.md, docs/ and evidence/ resolves to a file.
2. Every row of a README table with Status and Proof columns whose status starts with
   "Verified" links to at least one file under evidence/. docs/CLAIMS.md must exist.
3. Evidence files contain no email address other than the platform's noreply address
   and the <redacted-email> placeholder.
4. Every job log under evidence/day0/ shows the CI job succeeded.
5. Every file listed in evidence/SHA256SUMS exists and has the recorded SHA-256, so a
   copy of the evidence that differs from what was committed first is detected.

Usage: python3 scripts/check_evidence.py [repo_root]
"""
import hashlib
import re
import sys
from pathlib import Path

LINK = re.compile(r"\]\(([^)\s]+)\)")
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
ALLOWED_EMAIL_SUFFIX = "@noreply.gitlab.com"


def md_files(root):
    out = [root / "README.md"]
    out += sorted((root / "docs").glob("*.md"))
    out += sorted((root / "evidence").rglob("*.md"))
    return [p for p in out if p.exists()]


def check_links(root, errors):
    for md in md_files(root):
        text = md.read_text(encoding="utf-8")
        in_fence = False
        for n, line in enumerate(text.splitlines(), 1):
            if line.lstrip().startswith("```"):
                in_fence = not in_fence
            if in_fence:
                continue
            for m in LINK.finditer(line):
                target = m.group(1)
                if target.startswith(("http://", "https://", "mailto:", "#")):
                    continue
                target = target.split("#")[0]
                if not target:
                    continue
                if not (md.parent / target).resolve().exists():
                    errors.append(f"{md.relative_to(root)}:{n}: broken link {m.group(1)}")


def check_claims(root, errors):
    """Every README table that has Status and Proof columns is a claims table.
    A row whose status starts with "Verified" must link to a file under evidence/."""
    readme = root / "README.md"
    if not readme.exists():
        errors.append("README.md is missing")
        return
    status_i = proof_i = None
    verified_rows = 0
    for n, line in enumerate(readme.read_text(encoding="utf-8").splitlines(), 1):
        if not line.startswith("|"):
            status_i = proof_i = None
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        lowered = [c.lower() for c in cells]
        if "status" in lowered and "proof" in lowered:
            status_i, proof_i = lowered.index("status"), lowered.index("proof")
            continue
        if status_i is None or len(cells) <= max(status_i, proof_i):
            continue
        if cells[status_i].lower().startswith("verified"):
            verified_rows += 1
            if "](evidence/" not in cells[proof_i]:
                errors.append(f"README.md:{n}: 'Verified' claim without a link to evidence/")
    if verified_rows == 0:
        errors.append("README.md: no 'Verified' rows found in a Status/Proof table")
    if not (root / "docs" / "CLAIMS.md").exists():
        errors.append("docs/CLAIMS.md is missing")


def check_evidence_files(root, errors):
    ev = root / "evidence"
    for p in sorted(ev.rglob("*")):
        if not p.is_file():
            continue
        text = p.read_text(encoding="utf-8", errors="replace")
        for m in EMAIL.finditer(text):
            if not m.group(0).endswith(ALLOWED_EMAIL_SUFFIX):
                errors.append(f"{p.relative_to(root)}: unredacted email address")
                break
    for p in sorted((ev / "day0").glob("*job-log.txt")):
        if "Job succeeded" not in p.read_text(encoding="utf-8", errors="replace"):
            errors.append(f"{p.relative_to(root)}: no 'Job succeeded' line")


def check_manifest(root, errors):
    manifest = root / "evidence" / "SHA256SUMS"
    if not manifest.exists():
        errors.append("evidence/SHA256SUMS is missing")
        return
    for n, line in enumerate(manifest.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        digest, _, name = line.partition("  ")
        target = root / name
        if not target.exists():
            errors.append(f"evidence/SHA256SUMS:{n}: {name} does not exist")
        elif hashlib.sha256(target.read_bytes()).hexdigest() != digest:
            errors.append(f"evidence/SHA256SUMS:{n}: {name} does not match its recorded SHA-256")


def main():
    root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
    errors = []
    check_links(root, errors)
    check_claims(root, errors)
    check_evidence_files(root, errors)
    check_manifest(root, errors)
    if errors:
        print("FAIL")
        for e in errors:
            print(" -", e)
        return 1
    print("ok: links resolve, verified claims cite evidence, evidence is clean, checksums match")
    return 0


if __name__ == "__main__":
    sys.exit(main())
