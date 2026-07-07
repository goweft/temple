# temple

**Fail the build when a repo drifts past its declared single-purpose scope.**

A _temple_ is the stretcher on a loom that holds woven cloth to a constant width so it can't "draw in" and distort. This tool does the same for a repository: you declare the shape, and temple keeps it from drawing in.

temple checks **scope width** — not code quality, not security, not style. You write a short contract that says what this repo is *allowed* to be, and temple fails CI the moment a change breaches it. It turns "one small addition" — the way single-purpose tools quietly become frameworks — into an explicit decision you have to make on purpose.

Zero third-party dependencies. Standard-library Python, one TOML file.

---

## The contract

Drop a `temple.toml` at the repo root:

```toml
[scope]
purpose = "One sentence. What this repo is, and nothing else."

# Every git-tracked file must match at least one of these globs.
allow = ["src/**", "tests/**", "*.md", "*.toml", "LICENSE"]

# Hard budgets (all optional).
max_files = 40      # tracked-file ceiling
max_lines = 2000    # source-line budget (counted extensions only)
max_deps  = 3       # third-party dependency ceiling

# The "does NOT do" list — enforced, not just documented.
# Entries are regexes, matched line-by-line against source files.
forbid = ["import requests", "TODO: framework"]

# Optional knobs:
# deps_from = "pyproject.toml"          # where to read deps (default: auto)
# count_ext = [".py", ".go", ".rs"]     # which files count toward lines/forbid
```

## Run it

```bash
python -m temple check          # or: temple check   (once installed)
temple check --format json      # machine-readable output
temple check --format sarif     # SARIF 2.1.0 for GitHub code scanning
temple check --root path/to/repo --config path/to/temple.toml
```

Example output:

```
temple — scope contract check
  purpose: Fail the build when a repo drifts past its declared single-purpose scope. Nothing more.
  files:        11 / 20
  source lines: 638 / 700
  deps:         0 / 0

  PASS — repo is within scope.
```

## What it checks

1. **structure** — every git-tracked file matches an `allow` glob (`**`, `*`, `?` supported). Anything outside is drift.
2. **files** — tracked-file count stays under `max_files`.
3. **lines** — total source lines (counted extensions) stay under `max_lines`.
4. **deps** — declared third-party dependencies stay under `max_deps`.
5. **forbid** — no forbidden pattern appears in source, reported as `file:line`.

File discovery uses `git ls-files` (so it honors `.gitignore`), falling back to a filtered walk outside a git repo.

## Exit codes

| code | meaning |
|-----:|---------|
| `0`  | in scope — pass |
| `1`  | scope breach — fail |
| `2`  | could not evaluate (missing or invalid `temple.toml`) |

## GitHub Action

```yaml
# .github/workflows/scope.yml
name: scope
on: [push, pull_request]
jobs:
  temple:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: goweft/temple@v0   # composite action; needs Python 3.11+ on the runner
```

## GitHub code scanning (SARIF)

`--format sarif` emits SARIF 2.1.0, which GitHub renders as code-scanning alerts and inline PR annotations. Findings that name a file (`structure`, `forbid`) point at `file:line`; repo-level findings (`files`, `lines`, `deps`) anchor to `temple.toml` — the place you'd change the budget. Each result carries a stable fingerprint so alerts track across commits instead of re-firing.

```yaml
# .github/workflows/scope.yml
name: scope
on: [push, pull_request]
permissions:
  contents: read
  security-events: write        # required to upload SARIF
jobs:
  temple:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with:
          python-version: "3.12"
      - run: pipx install temple-scope
      - name: temple scope check (SARIF)
        id: temple
        run: temple check --format sarif > temple.sarif
        continue-on-error: true          # upload the report even on breach
      - name: upload to code scanning
        if: always()
        uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: temple.sarif
      - name: enforce scope
        if: steps.temple.outcome == 'failure'
        run: exit 1                        # fail the check after the report is filed
```

Run temple at the repo root (the default) so SARIF paths resolve against your files. Requires a `temple.toml`: with no contract, temple exits `2` and writes nothing to upload.

## Install

```bash
pipx install temple-scope        # isolated CLI
# or
pip install temple-scope
```

Requires Python 3.11+ (uses the standard-library `tomllib`).

## Scope (temple's own "does NOT do")

temple deliberately does **not**:

- lint code quality, format, or type-check — that's your existing tools' job;
- scan for security issues — that's the rest of the [goweft](https://github.com/goweft) suite;
- count dependencies outside Python manifests **in v0** (pyproject.toml / requirements.txt only);
- rewrite anything — temple only reports and sets an exit code.

temple ships with its own `temple.toml` and checks itself in CI. It practices what it enforces.

## Roadmap

Shipped in **v0.2.0**: `--format sarif` for GitHub code-scanning annotations — the security-positioned distribution move. (It cost ~180 source lines, so temple's own `max_lines` budget was raised on purpose in the same change; the bump is a documented line in `temple.toml`, not silent drift.)

Deliberately still not in temple:

- `exclude` globs, so the `forbid` scan can skip test fixtures and vendored code (temple's own first dogfood flagged a forbidden literal in a test fixture — this is the clean fix, made on purpose rather than mid-build);
- dependency counting for Go / Rust / npm manifests;
- a `temple init` that writes a starter contract.

Each of these is a real addition, so each gets made on purpose — not smuggled in.

## License

Apache-2.0.
