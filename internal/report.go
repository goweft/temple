package internal

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// Version is the current release. Embedded at build time via ldflags when
// built with goreleaser; falls back to "dev" for local builds.
var Version = "dev"

// ─────────────────────────────────────────────
// Text
// ─────────────────────────────────────────────

func budget(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf(" / %d", *v)
}

// RenderText returns the human-readable report.
func RenderText(c *Contract, findings []Finding, s Stats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "temple \u2014 scope contract check\n")
	if c.Purpose != "" {
		fmt.Fprintf(&b, "  purpose: %s\n", c.Purpose)
	}
	fmt.Fprintf(&b, "  files:        %d%s\n", s.Files, budget(c.MaxFiles))
	fmt.Fprintf(&b, "  source lines: %d%s\n", s.Lines, budget(c.MaxLines))
	if s.Deps != nil {
		fmt.Fprintf(&b, "  deps:         %d%s\n", *s.Deps, budget(c.MaxDeps))
	}
	fmt.Fprintln(&b)
	if len(findings) == 0 {
		fmt.Fprintf(&b, "  PASS \u2014 repo is within scope.")
	} else {
		fmt.Fprintf(&b, "  FAIL \u2014 %d scope breach(es):\n", len(findings))
		for _, f := range findings {
			loc := ""
			if f.Path != "" {
				loc = " " + f.Path
				if f.Line > 0 {
					loc += fmt.Sprintf(":%d", f.Line)
				}
			}
			fmt.Fprintf(&b, "    [%s]%s \u2014 %s\n", f.Check, loc, f.Message)
		}
	}
	return b.String()
}

// ─────────────────────────────────────────────
// JSON
// ─────────────────────────────────────────────

type jsonFinding struct {
	Check    string `json:"check"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity"`
}

type jsonReport struct {
	Purpose  string        `json:"purpose"`
	Stats    jsonStats     `json:"stats"`
	Budgets  jsonBudgets   `json:"budgets"`
	Pass     bool          `json:"pass"`
	Findings []jsonFinding `json:"findings"`
}

type jsonStats struct {
	Files int  `json:"files"`
	Lines int  `json:"lines"`
	Deps  *int `json:"deps"`
}

type jsonBudgets struct {
	MaxFiles *int `json:"max_files"`
	MaxLines *int `json:"max_lines"`
	MaxDeps  *int `json:"max_deps"`
}

// RenderJSON returns the machine-readable JSON report.
func RenderJSON(c *Contract, findings []Finding, s Stats) string {
	jf := make([]jsonFinding, len(findings))
	for i, f := range findings {
		jf[i] = jsonFinding{f.Check, f.Message, f.Path, f.Line, f.Severity}
	}
	r := jsonReport{
		Purpose:  c.Purpose,
		Stats:    jsonStats{s.Files, s.Lines, s.Deps},
		Budgets:  jsonBudgets{c.MaxFiles, c.MaxLines, c.MaxDeps},
		Pass:     len(findings) == 0,
		Findings: jf,
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

// ─────────────────────────────────────────────
// SARIF 2.1.0
// ─────────────────────────────────────────────

const helpURI = "https://github.com/goweft/temple#what-it-checks"

var sarifRules = []struct {
	id, name, short, full string
}{
	{"structure", "FileOutsideScope",
		"A git-tracked file falls outside the repo's declared scope.",
		"Every git-tracked file must match an `allow` glob in temple.toml; files outside the declared set are scope drift."},
	{"files", "FileCountCeiling",
		"The tracked-file count exceeds the declared ceiling.",
		"The number of git-tracked files exceeds `max_files` in temple.toml."},
	{"lines", "SourceLineBudget",
		"The source-line count exceeds the declared budget.",
		"Total source lines across counted extensions exceed `max_lines`."},
	{"deps", "DependencyCeiling",
		"The dependency count exceeds the declared ceiling.",
		"Declared third-party dependencies exceed `max_deps` in temple.toml."},
	{"forbid", "ForbiddenPattern",
		"A forbidden pattern appears in source.",
		"A pattern listed in `forbid` in temple.toml was found in a source file."},
}

var ruleIndex = func() map[string]int {
	m := make(map[string]int, len(sarifRules))
	for i, r := range sarifRules {
		m[r.id] = i
	}
	return m
}()

func fingerprint(f Finding, uri string) string {
	raw := fmt.Sprintf("%s|%s|%d|%s", f.Check, uri, f.Line, f.Message)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum[:8]) // 16 hex chars
}

func sarifLevel(severity string) string {
	if severity == "warning" {
		return "warning"
	}
	return "error"
}

// RenderSARIF returns a SARIF 2.1.0 document for GitHub code scanning.
// configRel is the repo-relative POSIX path to temple.toml (used to anchor
// repo-level findings that have no specific source file).
func RenderSARIF(c *Contract, findings []Finding, s Stats, configRel string) string {
	// rules array
	rules := make([]any, len(sarifRules))
	for i, r := range sarifRules {
		rules[i] = map[string]any{
			"id":               r.id,
			"name":             r.name,
			"shortDescription": map[string]string{"text": r.short},
			"fullDescription":  map[string]string{"text": r.full},
			"helpUri":          helpURI,
			"defaultConfiguration": map[string]string{
				"level": "error",
			},
		}
	}

	// results array
	results := make([]any, len(findings))
	for i, f := range findings {
		uri := f.Path
		if uri == "" {
			uri = configRel
		}
		startLine := f.Line
		if startLine == 0 {
			startLine = 1
		}
		idx, ok := ruleIndex[f.Check]
		if !ok {
			idx = 0
		}
		results[i] = map[string]any{
			"ruleId":    f.Check,
			"ruleIndex": idx,
			"level":     sarifLevel(f.Severity),
			"message":   map[string]string{"text": f.Message},
			"locations": []any{
				map[string]any{
					"physicalLocation": map[string]any{
						"artifactLocation": map[string]string{"uri": uri},
						"region":           map[string]int{"startLine": startLine},
					},
				},
			},
			"partialFingerprints": map[string]string{
				"templeScope/v1": fingerprint(f, uri),
			},
		}
	}

	doc := map[string]any{
		"$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"version": "2.1.0",
		"runs": []any{
			map[string]any{
				"tool": map[string]any{
					"driver": map[string]any{
						"name":            "temple",
						"informationUri":  "https://github.com/goweft/temple",
						"version":         Version,
						"semanticVersion": Version,
						"rules":           rules,
					},
				},
				"results": results,
				"properties": map[string]any{
					"purpose": c.Purpose,
					"stats": map[string]any{
						"files": s.Files,
						"lines": s.Lines,
						"deps":  s.Deps,
					},
					"budgets": map[string]any{
						"max_files": c.MaxFiles,
						"max_lines": c.MaxLines,
						"max_deps":  c.MaxDeps,
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b)
}
