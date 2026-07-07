package internal

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
)

// Finding is a single scope violation.
type Finding struct {
	Check    string // structure | files | lines | deps | forbid
	Message  string
	Path     string // relative POSIX path, empty for repo-level findings
	Line     int    // 0 = no specific line
	Severity string // "error" | "warning"
}

// Stats holds the measured values for the three budgets.
type Stats struct {
	Files int
	Lines int
	Deps  *int // nil when no manifest found
}

// Run executes all five checks and returns findings + stats.
func Run(root string, c *Contract) ([]Finding, Stats, error) {
	files, err := TrackedFiles(root)
	if err != nil {
		return nil, Stats{}, fmt.Errorf("temple: file discovery failed: %w", err)
	}

	var findings []Finding
	findings = append(findings, checkStructure(c, root, files)...)
	findings = append(findings, checkCounts(c, root, files)...)
	findings = append(findings, checkDeps(c, root)...)
	findings = append(findings, checkForbid(c, root, files)...)

	// build stats
	lines := 0
	for _, f := range files {
		if HasExt(f, c.CountExt) {
			lines += CountLines(f)
		}
	}
	stats := Stats{Files: len(files), Lines: lines}
	if dr := CountDeps(c, root); dr != nil {
		n := dr.Count
		stats.Deps = &n
	}

	return findings, stats, nil
}

func newFinding(check, msg, path string, line int) Finding {
	return Finding{Check: check, Message: msg, Path: path, Line: line, Severity: "error"}
}

// checkStructure — every tracked file must match an allow glob.
func checkStructure(c *Contract, root string, files []string) []Finding {
	if len(c.Allow) == 0 {
		return nil
	}
	var out []Finding
	for _, f := range files {
		rel := RelPosix(root, f)
		if !matchesAny(rel, c.Allow) {
			out = append(out, newFinding("structure", "file outside declared scope", rel, 0))
		}
	}
	return out
}

// checkCounts — file count and source line budget.
func checkCounts(c *Contract, root string, files []string) []Finding {
	var out []Finding
	if c.MaxFiles != nil && len(files) > *c.MaxFiles {
		out = append(out, newFinding("files",
			fmt.Sprintf("tracked file count %d exceeds ceiling %d", len(files), *c.MaxFiles),
			"", 0))
	}
	if c.MaxLines != nil {
		total := 0
		for _, f := range files {
			if HasExt(f, c.CountExt) {
				total += CountLines(f)
			}
		}
		if total > *c.MaxLines {
			out = append(out, newFinding("lines",
				fmt.Sprintf("source line count %d exceeds budget %d", total, *c.MaxLines),
				"", 0))
		}
	}
	return out
}

// checkDeps — third-party dependency ceiling.
func checkDeps(c *Contract, root string) []Finding {
	if c.MaxDeps == nil {
		return nil
	}
	dr := CountDeps(c, root)
	if dr == nil {
		return nil
	}
	if dr.Count > *c.MaxDeps {
		return []Finding{newFinding("deps",
			fmt.Sprintf("%d third-party dependencies in %s exceeds ceiling %d",
				dr.Count, dr.From, *c.MaxDeps),
			"", 0)}
	}
	return nil
}

// checkForbid — no forbidden pattern may appear in source files.
func checkForbid(c *Contract, root string, files []string) []Finding {
	if len(c.Forbid) == 0 {
		return nil
	}
	compiled := make([]*regexp.Regexp, len(c.Forbid))
	for i, raw := range c.Forbid {
		compiled[i] = regexp.MustCompile(raw)
	}

	var out []Finding
	for _, f := range files {
		if !HasExt(f, c.CountExt) {
			continue
		}
		rel := RelPosix(root, f)
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		lineno := 0
		for sc.Scan() {
			lineno++
			line := sc.Text()
			for i, rx := range compiled {
				if rx.MatchString(line) {
					out = append(out, newFinding("forbid",
						fmt.Sprintf("forbidden pattern /%s/", c.Forbid[i]),
						rel, lineno))
				}
			}
		}
		fh.Close()
	}
	return out
}
