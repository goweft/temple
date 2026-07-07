"""temple core.

One job: read a repo's declared scope contract (temple.toml) and fail the
build when the repo has drifted past it. temple checks *scope width* — it is
deliberately not a code-quality linter, a security scanner, or a formatter.

Zero third-party dependencies. Standard library only.
"""

from __future__ import annotations

import hashlib
import json
import re
import subprocess
import sys
import tomllib
from dataclasses import asdict, dataclass, field
from pathlib import Path

try:
    from . import __version__
except ImportError:  # pragma: no cover - fallback for unusual import paths
    __version__ = "0.0.0"

DEFAULT_CONTRACT = "temple.toml"

# Exit codes:
#   0  in scope (pass)
#   1  scope breach (fail)
#   2  could not evaluate (no/invalid contract)
EXIT_OK = 0
EXIT_BREACH = 1
EXIT_NOEVAL = 2


# --------------------------------------------------------------------------- #
# Contract
# --------------------------------------------------------------------------- #

DEFAULT_COUNT_EXT = [".py", ".go", ".rs", ".js", ".ts", ".sh"]


@dataclass
class Contract:
    """The scope contract, loaded from the [scope] table of temple.toml."""

    purpose: str = ""
    allow: list[str] = field(default_factory=list)
    max_files: int | None = None
    max_lines: int | None = None
    max_deps: int | None = None
    forbid: list[str] = field(default_factory=list)
    deps_from: str | None = None
    count_ext: list[str] = field(default_factory=lambda: list(DEFAULT_COUNT_EXT))

    @staticmethod
    def load(path: Path) -> "Contract":
        with open(path, "rb") as fh:
            data = tomllib.load(fh)
        scope = data.get("scope", {})
        return Contract(
            purpose=str(scope.get("purpose", "")),
            allow=[str(x) for x in scope.get("allow", [])],
            max_files=scope.get("max_files"),
            max_lines=scope.get("max_lines"),
            max_deps=scope.get("max_deps"),
            forbid=[str(x) for x in scope.get("forbid", [])],
            deps_from=scope.get("deps_from"),
            count_ext=[str(x) for x in scope.get("count_ext", DEFAULT_COUNT_EXT)],
        )


# --------------------------------------------------------------------------- #
# Findings
# --------------------------------------------------------------------------- #


@dataclass
class Finding:
    check: str  # structure | files | lines | deps | forbid
    message: str
    path: str | None = None
    line: int | None = None
    severity: str = "error"


# --------------------------------------------------------------------------- #
# Glob matching (supports **, *, ? against POSIX paths)
# --------------------------------------------------------------------------- #


def _glob_to_regex(pattern: str) -> re.Pattern[str]:
    """Translate a glob (**, *, ?) into a regex that matches a POSIX path.

    * matches within a path segment; ** matches across segments; ? matches a
    single non-separator character.
    """
    i, n = 0, len(pattern)
    out: list[str] = []
    while i < n:
        c = pattern[i]
        if c == "*":
            if pattern[i : i + 2] == "**":
                j = i + 2
                if pattern[j : j + 1] == "/":  # "**/" -> optional leading dirs
                    out.append("(?:.*/)?")
                    i = j + 1
                else:
                    out.append(".*")
                    i = j
            else:
                out.append("[^/]*")
                i += 1
        elif c == "?":
            out.append("[^/]")
            i += 1
        else:
            out.append(re.escape(c))
            i += 1
    return re.compile("(?s:" + "".join(out) + r")\Z")


def _matches_any(rel: str, patterns: list[str]) -> bool:
    return any(_glob_to_regex(p).match(rel) for p in patterns)


# --------------------------------------------------------------------------- #
# File discovery
# --------------------------------------------------------------------------- #

_WALK_SKIP = {
    ".git", "__pycache__", ".venv", "venv", "node_modules", ".mypy_cache",
    ".pytest_cache", ".ruff_cache", "dist", "build", "target", ".idea", ".tox",
}


def tracked_files(root: Path) -> list[Path]:
    """Prefer git-tracked files (honors .gitignore). Fall back to a filtered walk."""
    try:
        result = subprocess.run(
            ["git", "-C", str(root), "ls-files"],
            capture_output=True, text=True, check=True,
        )
        files = [root / line for line in result.stdout.splitlines() if line.strip()]
        if files:
            return files
    except (subprocess.CalledProcessError, FileNotFoundError):
        pass

    walked: list[Path] = []
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        parts = path.relative_to(root).parts
        if any(part in _WALK_SKIP for part in parts):
            continue
        walked.append(path)
    return walked


def _count_lines(path: Path) -> int:
    try:
        with open(path, "rb") as fh:
            return sum(1 for _ in fh)
    except OSError:
        return 0


# --------------------------------------------------------------------------- #
# Dependency counting (v0: Python manifests only — other ecosystems are a
# deliberate follow-up, per temple's own scope discipline)
# --------------------------------------------------------------------------- #


def count_deps(contract: Contract, root: Path) -> tuple[int, str] | None:
    candidates = [contract.deps_from] if contract.deps_from else ["pyproject.toml", "requirements.txt"]
    for name in candidates:
        if not name:
            continue
        path = root / name
        if not path.exists():
            continue
        try:
            if name.endswith("pyproject.toml"):
                with open(path, "rb") as fh:
                    data = tomllib.load(fh)
                deps = data.get("project", {}).get("dependencies", []) or []
                return len(deps), name
            if name.endswith("requirements.txt"):
                lines = [ln.strip() for ln in path.read_text().splitlines()]
                deps = [ln for ln in lines if ln and not ln.startswith("#")]
                return len(deps), name
        except Exception:
            return None
    return None


# --------------------------------------------------------------------------- #
# Checks
# --------------------------------------------------------------------------- #


def check_structure(contract: Contract, root: Path, files: list[Path]) -> list[Finding]:
    if not contract.allow:
        return []
    findings: list[Finding] = []
    for path in files:
        rel = path.relative_to(root).as_posix()
        if not _matches_any(rel, contract.allow):
            findings.append(Finding("structure", "file outside declared scope", rel))
    return findings


def check_counts(contract: Contract, root: Path, files: list[Path]) -> list[Finding]:
    findings: list[Finding] = []
    if contract.max_files is not None and len(files) > contract.max_files:
        findings.append(
            Finding("files", f"tracked file count {len(files)} exceeds ceiling {contract.max_files}")
        )
    if contract.max_lines is not None:
        total = sum(_count_lines(p) for p in files if p.suffix in contract.count_ext)
        if total > contract.max_lines:
            findings.append(
                Finding("lines", f"source line count {total} exceeds budget {contract.max_lines}")
            )
    return findings


def check_deps(contract: Contract, root: Path) -> list[Finding]:
    if contract.max_deps is None:
        return []
    result = count_deps(contract, root)
    if result is None:
        return []
    n, src = result
    if n > contract.max_deps:
        return [Finding("deps", f"{n} third-party dependencies in {src} exceeds ceiling {contract.max_deps}")]
    return []


def check_forbid(contract: Contract, root: Path, files: list[Path]) -> list[Finding]:
    if not contract.forbid:
        return []
    compiled = [(raw, re.compile(raw)) for raw in contract.forbid]
    findings: list[Finding] = []
    for path in files:
        if path.suffix not in contract.count_ext:
            continue
        rel = path.relative_to(root).as_posix()
        try:
            text = path.read_text(errors="replace")
        except OSError:
            continue
        for lineno, line in enumerate(text.splitlines(), 1):
            for raw, rx in compiled:
                if rx.search(line):
                    findings.append(Finding("forbid", f"forbidden pattern /{raw}/", rel, lineno))
    return findings


# --------------------------------------------------------------------------- #
# Orchestration
# --------------------------------------------------------------------------- #


def run(root: Path, config: Path) -> tuple[list[Finding], Contract, dict]:
    contract = Contract.load(config)
    files = tracked_files(root)

    findings: list[Finding] = []
    findings += check_structure(contract, root, files)
    findings += check_counts(contract, root, files)
    findings += check_deps(contract, root)
    findings += check_forbid(contract, root, files)

    deps = count_deps(contract, root)
    stats = {
        "files": len(files),
        "lines": sum(_count_lines(p) for p in files if p.suffix in contract.count_ext),
        "deps": deps[0] if deps else None,
    }
    return findings, contract, stats


# --------------------------------------------------------------------------- #
# Reporting
# --------------------------------------------------------------------------- #


def _budget(value: int | None) -> str:
    return f" / {value}" if value is not None else ""


def render_text(contract: Contract, findings: list[Finding], stats: dict) -> str:
    lines = ["temple \u2014 scope contract check"]
    if contract.purpose:
        lines.append(f"  purpose: {contract.purpose}")
    lines.append(f"  files:        {stats['files']}{_budget(contract.max_files)}")
    lines.append(f"  source lines: {stats['lines']}{_budget(contract.max_lines)}")
    if stats["deps"] is not None:
        lines.append(f"  deps:         {stats['deps']}{_budget(contract.max_deps)}")
    lines.append("")
    if not findings:
        lines.append("  PASS \u2014 repo is within scope.")
    else:
        lines.append(f"  FAIL \u2014 {len(findings)} scope breach(es):")
        for f in findings:
            loc = ""
            if f.path:
                loc = f" {f.path}" + (f":{f.line}" if f.line else "")
            lines.append(f"    [{f.check}]{loc} \u2014 {f.message}")
    return "\n".join(lines)


def render_json(contract: Contract, findings: list[Finding], stats: dict) -> str:
    return json.dumps(
        {
            "purpose": contract.purpose,
            "stats": stats,
            "budgets": {
                "max_files": contract.max_files,
                "max_lines": contract.max_lines,
                "max_deps": contract.max_deps,
            },
            "pass": not findings,
            "findings": [asdict(f) for f in findings],
        },
        indent=2,
    )


# --------------------------------------------------------------------------- #
# SARIF (GitHub code scanning)
# --------------------------------------------------------------------------- #

# One reporting rule per check, in a fixed order so ruleIndex stays stable.
SARIF_RULES = [
    {
        "id": "structure",
        "name": "FileOutsideScope",
        "short": "A git-tracked file falls outside the repo's declared scope.",
        "full": "Every git-tracked file must match an `allow` glob in temple.toml; "
                "files outside the declared set are scope drift.",
    },
    {
        "id": "files",
        "name": "FileCountCeiling",
        "short": "The tracked-file count exceeds the declared ceiling.",
        "full": "The number of git-tracked files exceeds `max_files` in temple.toml.",
    },
    {
        "id": "lines",
        "name": "SourceLineBudget",
        "short": "The source-line count exceeds the declared budget.",
        "full": "Total source lines across counted extensions exceed `max_lines`.",
    },
    {
        "id": "deps",
        "name": "DependencyCeiling",
        "short": "The dependency count exceeds the declared ceiling.",
        "full": "Declared third-party dependencies exceed `max_deps` in temple.toml.",
    },
    {
        "id": "forbid",
        "name": "ForbiddenPattern",
        "short": "A forbidden pattern appears in source.",
        "full": "A pattern listed in `forbid` in temple.toml was found in a source file.",
    },
]
_RULE_INDEX = {r["id"]: i for i, r in enumerate(SARIF_RULES)}
_HELP_URI = "https://github.com/goweft/temple#what-it-checks"


def _sarif_level(severity: str) -> str:
    return {"error": "error", "warning": "warning"}.get(severity, "note")


def _fingerprint(finding: Finding, uri: str) -> str:
    raw = f"{finding.check}|{uri}|{finding.line or 0}|{finding.message}"
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()[:16]


def render_sarif(
    contract: Contract, findings: list[Finding], stats: dict, config_rel: str = DEFAULT_CONTRACT
) -> str:
    """Render findings as SARIF 2.1.0 for GitHub code scanning.

    Findings that name a file (structure, forbid) anchor to that file:line.
    Repo-level findings (files, lines, deps) have no source location, so they
    anchor to the contract itself — the place you'd change the budget.
    """
    rules = [
        {
            "id": r["id"],
            "name": r["name"],
            "shortDescription": {"text": r["short"]},
            "fullDescription": {"text": r["full"]},
            "helpUri": _HELP_URI,
            "defaultConfiguration": {"level": "error"},
        }
        for r in SARIF_RULES
    ]

    results = []
    for f in findings:
        uri = f.path or config_rel
        results.append(
            {
                "ruleId": f.check,
                "ruleIndex": _RULE_INDEX.get(f.check, 0),
                "level": _sarif_level(f.severity),
                "message": {"text": f.message},
                "locations": [
                    {
                        "physicalLocation": {
                            "artifactLocation": {"uri": uri},
                            "region": {"startLine": f.line if f.line else 1},
                        }
                    }
                ],
                "partialFingerprints": {"templeScope/v1": _fingerprint(f, uri)},
            }
        )

    doc = {
        "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
        "version": "2.1.0",
        "runs": [
            {
                "tool": {
                    "driver": {
                        "name": "temple",
                        "informationUri": "https://github.com/goweft/temple",
                        "version": __version__,
                        "semanticVersion": __version__,
                        "rules": rules,
                    }
                },
                "results": results,
                "properties": {
                    "purpose": contract.purpose,
                    "stats": stats,
                    "budgets": {
                        "max_files": contract.max_files,
                        "max_lines": contract.max_lines,
                        "max_deps": contract.max_deps,
                    },
                },
            }
        ],
    }
    return json.dumps(doc, indent=2)


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #


def main(argv: list[str] | None = None) -> int:
    import argparse

    parser = argparse.ArgumentParser(
        prog="temple",
        description="Fail the build when a repo drifts past its declared single-purpose scope.",
    )
    parser.add_argument("command", nargs="?", default="check", choices=["check"],
                        help="what to run (default: check)")
    parser.add_argument("--config", "-c", default=DEFAULT_CONTRACT,
                        help=f"path to the scope contract (default: {DEFAULT_CONTRACT})")
    parser.add_argument("--root", "-r", default=".",
                        help="repo root to inspect (default: .)")
    parser.add_argument("--format", "-f", default="text", choices=["text", "json", "sarif"],
                        help="output format: text, json, or sarif (default: text)")
    args = parser.parse_args(argv)

    root = Path(args.root).resolve()
    config = Path(args.config)
    if not config.is_absolute():
        config = root / config

    if not config.exists():
        print(f"temple: no scope contract found at {config}", file=sys.stderr)
        print("temple: declare this repo's scope in a temple.toml. See the README.", file=sys.stderr)
        return EXIT_NOEVAL

    try:
        findings, contract, stats = run(root, config)
    except tomllib.TOMLDecodeError as exc:
        print(f"temple: could not parse {config}: {exc}", file=sys.stderr)
        return EXIT_NOEVAL

    if args.format == "sarif":
        try:
            config_rel = config.resolve().relative_to(root).as_posix()
        except ValueError:
            config_rel = config.name
        output = render_sarif(contract, findings, stats, config_rel)
    elif args.format == "json":
        output = render_json(contract, findings, stats)
    else:
        output = render_text(contract, findings, stats)
    print(output)
    return EXIT_BREACH if findings else EXIT_OK
