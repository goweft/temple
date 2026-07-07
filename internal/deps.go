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

// CountDeps counts third-party dependencies declared in the repo's manifest.
// v0 supports Python manifests only (pyproject.toml, requirements.txt).
// Returns nil when no supported manifest is found.
func CountDeps(c *Contract, root string) *DepResult {
	candidates := []string{"pyproject.toml", "requirements.txt"}
	if c.DepsFrom != "" {
		candidates = []string{c.DepsFrom}
	}
	for _, name := range candidates {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		switch {
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
