package internal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goweft/temple/internal"
)

// depContract declares a ceiling, so checkDeps must reach a verdict.
const depContract = `
[scope]
purpose = "deps fixture"
allow   = ["src/**", "*.toml", "*.txt", "go.mod"]
max_deps = 0
`

func countDeps(t *testing.T, files map[string]string) *internal.DepResult {
	t.Helper()
	root, cfg := setup(t, files, depContract)
	c, err := internal.LoadContract(cfg)
	if err != nil {
		t.Fatalf("LoadContract: %v", err)
	}
	return internal.CountDeps(c, root)
}

func TestGoModCountsDirectRequiresOnly(t *testing.T) {
	gomod := "module example.com/x\n\ngo 1.22\n\nrequire (\n\tgithub.com/a/b v1.0.0\n\tgithub.com/c/d v2.0.0 // indirect\n)\n"
	dr := countDeps(t, map[string]string{"go.mod": gomod})
	if dr == nil {
		t.Fatal("expected a DepResult for go.mod")
	}
	if dr.From != "go.mod" {
		t.Errorf("expected From go.mod, got %q", dr.From)
	}
	if dr.Count != 1 {
		t.Errorf("expected 1 direct require (indirect excluded), got %d", dr.Count)
	}
}

func TestGoModSingleLineRequire(t *testing.T) {
	gomod := "module example.com/x\n\ngo 1.22\n\nrequire github.com/a/b v1.0.0\n"
	dr := countDeps(t, map[string]string{"go.mod": gomod})
	if dr == nil || dr.Count != 1 {
		t.Fatalf("expected count 1 from single-line require, got %v", dr)
	}
}

// The regression this whole change exists to prevent: a declared ceiling with
// no parseable manifest must be a finding, not a silent pass.
func TestDepsFailClosedWhenManifestMissing(t *testing.T) {
	root, cfg := setup(t, map[string]string{"src/a.py": "x = 1\n"}, depContract)
	c, err := internal.LoadContract(cfg)
	if err != nil {
		t.Fatalf("LoadContract: %v", err)
	}
	findings, stats, err := internal.Run(root, c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasCheck(findings, "deps") {
		t.Error("expected a deps finding when max_deps is declared and no manifest exists")
	}
	if stats.Deps != nil {
		t.Error("expected stats.Deps to stay nil — unmeasured is not zero")
	}
}

// No ceiling declared means the check has nothing to enforce; stay quiet.
func TestDepsSilentWhenNoCeilingDeclared(t *testing.T) {
	noCeiling := "\n[scope]\npurpose = \"no ceiling\"\nallow = [\"src/**\", \"*.toml\"]\n"
	root, cfg := setup(t, map[string]string{"src/a.py": "x = 1\n"}, noCeiling)
	c, err := internal.LoadContract(cfg)
	if err != nil {
		t.Fatalf("LoadContract: %v", err)
	}
	findings, _, err := internal.Run(root, c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasCheck(findings, "deps") {
		t.Error("expected no deps finding when max_deps is absent")
	}
}

// deps_from pins the manifest; an unparseable pin still fails closed.
func TestDepsFromPinFailsClosedOnUnknownManifest(t *testing.T) {
	contract := depContract + "deps_from = \"Cargo.toml\"\n"
	root, cfg := setup(t, map[string]string{"src/a.py": "x = 1\n"}, contract)
	c, err := internal.LoadContract(cfg)
	if err != nil {
		t.Fatalf("LoadContract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Cargo.toml")); err == nil {
		t.Fatal("fixture should not have a Cargo.toml")
	}
	findings, _, err := internal.Run(root, c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasCheck(findings, "deps") {
		t.Error("expected a deps finding when deps_from names a manifest temple cannot parse")
	}
}
