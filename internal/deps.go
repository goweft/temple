package internal

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// DepResult holds the dependency count and the manifest it was read from.
type DepResult struct {
	Count int
	From  string
}

// DepManifests are the manifests CountDeps understands, in probe order.
var DepManifests = []string{"go.mod", "pyproject.toml", "requirements.txt"}

// CountDeps counts third-party dependencies declared in the repo's manifest.
// Returns nil when no supported manifest is found. Callers must treat nil as
// "cannot enforce" — never as "zero dependencies".
func CountDeps(c *Contract, root string) *DepResult {
	candidates := DepManifests
	if c.DepsFrom != "" {
		candidates = []string{c.DepsFrom}
	}
	for _, name := range candidates {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		switch {
		case strings.HasSuffix(name, "go.mod"):
			if r := parseGoMod(path, name); r != nil {
				return r
			}
		case strings.HasSuffix(name, "pyproject.toml"):
			if r := parsePyproject(path, name); r != nil {
				return r
			}
		case strings.HasSuffix(name, "requirements.txt"):
			if r := parseRequirements(path, name); r != nil {
				return r
			}
		}
	}
	return nil
}

// parseGoMod counts direct requires in a go.mod. Requires marked "// indirect"
// are transitive — pulled in by a direct dep, not chosen — and do not count
// against the ceiling. Handles both the single-line and parenthesised forms.
func parseGoMod(path, name string) *DepResult {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	n := 0
	inBlock := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "//"); i >= 0 {
			if strings.Contains(line[i:], "indirect") {
				continue
			}
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		switch {
		case inBlock:
			if line == ")" {
				inBlock = false
				continue
			}
			n++
		case line == "require (":
			inBlock = true
		case strings.HasPrefix(line, "require "):
			n++
		}
	}
	if sc.Err() != nil {
		return nil
	}
	return &DepResult{Count: n, From: name}
}

type pyproject struct {
	Project struct {
		Dependencies []string `toml:"dependencies"`
	} `toml:"project"`
}

func parsePyproject(path, name string) *DepResult {
	var p pyproject
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil
	}
	return &DepResult{Count: len(p.Project.Dependencies), From: name}
}

func parseRequirements(path, name string) *DepResult {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			n++
		}
	}
	return &DepResult{Count: n, From: name}
}
