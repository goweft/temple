// Package internal contains all temple logic.
// One job: load a scope contract and fail the build when the repo drifts past it.
package internal

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// DefaultContract is the filename temple looks for by default.
const DefaultContract = "temple.toml"

// DefaultCountExt is the set of file extensions counted toward source lines
// and scanned by forbid checks when the contract does not specify count_ext.
var DefaultCountExt = []string{".py", ".go", ".rs", ".js", ".ts", ".sh"}

// Contract is the decoded scope contract from temple.toml.
type Contract struct {
	Purpose  string   `toml:"purpose"`
	Allow    []string `toml:"allow"`
	MaxFiles *int     `toml:"max_files"`
	MaxLines *int     `toml:"max_lines"`
	MaxDeps  *int     `toml:"max_deps"`
	Forbid   []string `toml:"forbid"`
	DepsFrom string   `toml:"deps_from"`
	CountExt []string `toml:"count_ext"`
}

// rawTOML mirrors the on-disk layout: contract lives under [scope].
type rawTOML struct {
	Scope Contract `toml:"scope"`
}

// LoadContract reads and parses a temple.toml file.
func LoadContract(path string) (*Contract, error) {
	var raw rawTOML
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, fmt.Errorf("temple: could not parse %s: %w", path, err)
	}
	c := raw.Scope
	if len(c.CountExt) == 0 {
		c.CountExt = append([]string(nil), DefaultCountExt...)
	}
	return &c, nil
}
