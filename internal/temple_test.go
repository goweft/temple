package internal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goweft/temple/internal"
)

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

const testContract = `
[scope]
purpose = "test fixture"
allow   = ["src/**", "*.toml"]
max_files = 3
max_lines = 20
forbid    = ["NETCALL_FORBIDDEN"]
`

func setup(t *testing.T, files map[string]string, contractTOML string) (root, cfgPath string) {
	t.Helper()
	root = t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath = filepath.Join(root, "temple.toml")
	if err := os.WriteFile(cfgPath, []byte(contractTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, cfgPath
}

func runTest(t *testing.T, files map[string]string) ([]internal.Finding, internal.Stats) {
	t.Helper()
	return runTestWith(t, files, testContract)
}

func runTestWith(t *testing.T, files map[string]string, contractTOML string) ([]internal.Finding, internal.Stats) {
	t.Helper()
	root, cfg := setup(t, files, contractTOML)
	c, err := internal.LoadContract(cfg)
	if err != nil {
		t.Fatalf("LoadContract: %v", err)
	}
	findings, stats, err := internal.Run(root, c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings, stats
}

func hasCheck(findings []internal.Finding, check string) bool {
	for _, f := range findings {
		if f.Check == check {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// Core checks
// ─────────────────────────────────────────────

func TestPassWithinScope(t *testing.T) {
	findings, _ := runTest(t, map[string]string{"src/a.py": "x = 1\n"})
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %v", findings)
	}
}

func TestStructureViolation(t *testing.T) {
	findings, _ := runTest(t, map[string]string{
		"src/a.py":  "x = 1\n",
		"stray.txt": "nope\n",
	})
	if !hasCheck(findings, "structure") {
		t.Error("expected structure finding")
	}
}

func TestForbiddenPatternReportsLine(t *testing.T) {
	findings, _ := runTest(t, map[string]string{
		"src/a.py": "y = 2\nNETCALL_FORBIDDEN\n",
	})
	var forb []internal.Finding
	for _, f := range findings {
		if f.Check == "forbid" {
			forb = append(forb, f)
		}
	}
	if len(forb) == 0 {
		t.Fatal("expected forbid finding")
	}
	if forb[0].Line != 2 {
		t.Errorf("expected line 2, got %d", forb[0].Line)
	}
	if forb[0].Path != "src/a.py" {
		t.Errorf("expected path src/a.py, got %q", forb[0].Path)
	}
}

func TestFileCeiling(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 4; i++ {
		files[strings.ReplaceAll(strings.ToLower(strings.Replace(
			"src/f{i}.py", "{i}", string(rune('0'+i)), 1)), " ", "")] = "x = 1\n"
	}
	findings, _ := runTest(t, files)
	if !hasCheck(findings, "files") {
		t.Error("expected files finding")
	}
}

func TestLineBudget(t *testing.T) {
	body := strings.Repeat("x = 1\n", 25)
	findings, _ := runTest(t, map[string]string{"src/a.py": body})
	if !hasCheck(findings, "lines") {
		t.Error("expected lines finding")
	}
}

func TestDepCeiling(t *testing.T) {
	pyproject := "[project]\nname = \"x\"\nversion = \"0\"\ndependencies = [\"requests\", \"click\"]\n"
	findings, _ := runTestWith(t, map[string]string{
		"src/a.py":       "x = 1\n",
		"pyproject.toml": pyproject,
	}, testContract+"max_deps = 0\n")
	if !hasCheck(findings, "deps") {
		t.Error("expected deps finding")
	}
}

// ─────────────────────────────────────────────
// Glob matching
// ─────────────────────────────────────────────

func TestGlobDoubleStarCrossesDirs(t *testing.T) {
	findings, _ := runTest(t, map[string]string{"src/deep/a.py": "x=1\n"})
	if hasCheck(findings, "structure") {
		t.Error("src/deep/a.py should match src/**")
	}
}

func TestGlobSingleStarStaysInSegment(t *testing.T) {
	// docs/x.md does NOT match *.toml or src/**, so it should be flagged
	findings, _ := runTest(t, map[string]string{
		"src/a.py":  "x=1\n",
		"docs/x.md": "text\n",
	})
	found := false
	for _, f := range findings {
		if f.Check == "structure" && f.Path == "docs/x.md" {
			found = true
		}
	}
	if !found {
		t.Error("docs/x.md should be flagged as outside scope")
	}
}

// ─────────────────────────────────────────────
// SARIF output
// ─────────────────────────────────────────────

func runSARIF(t *testing.T, files map[string]string) ([]internal.Finding, map[string]any) {
	t.Helper()
	root, cfg := setup(t, files, testContract)
	c, err := internal.LoadContract(cfg)
	if err != nil {
		t.Fatalf("LoadContract: %v", err)
	}
	findings, stats, err := internal.Run(root, c)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw := internal.RenderSARIF(c, findings, stats, "temple.toml")
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("SARIF unmarshal: %v", err)
	}
	return findings, doc
}

func sarifResults(doc map[string]any) []any {
	runs := doc["runs"].([]any)
	return runs[0].(map[string]any)["results"].([]any)
}

func sarifRules(doc map[string]any) []any {
	runs := doc["runs"].([]any)
	driver := runs[0].(map[string]any)["tool"].(map[string]any)["driver"].(map[string]any)
	return driver["rules"].([]any)
}

func TestSARIFValidEnvelopeWhenClean(t *testing.T) {
	_, doc := runSARIF(t, map[string]string{"src/a.py": "x = 1\n"})
	if doc["version"] != "2.1.0" {
		t.Errorf("expected version 2.1.0, got %v", doc["version"])
	}
	runs := doc["runs"].([]any)
	driver := runs[0].(map[string]any)["tool"].(map[string]any)["driver"].(map[string]any)
	if driver["name"] != "temple" {
		t.Errorf("expected driver name 'temple', got %v", driver["name"])
	}
	if len(sarifResults(doc)) != 0 {
		t.Error("expected no results for clean repo")
	}
}

func TestSARIFForbidCarriesFileAndLine(t *testing.T) {
	_, doc := runSARIF(t, map[string]string{"src/a.py": "y = 2\nNETCALL_FORBIDDEN\n"})
	var forb map[string]any
	for _, r := range sarifResults(doc) {
		rm := r.(map[string]any)
		if rm["ruleId"] == "forbid" {
			forb = rm
			break
		}
	}
	if forb == nil {
		t.Fatal("expected forbid result in SARIF")
	}
	locs := forb["locations"].([]any)
	phys := locs[0].(map[string]any)["physicalLocation"].(map[string]any)
	uri := phys["artifactLocation"].(map[string]any)["uri"].(string)
	line := phys["region"].(map[string]any)["startLine"].(float64)
	if uri != "src/a.py" {
		t.Errorf("expected uri src/a.py, got %q", uri)
	}
	if int(line) != 2 {
		t.Errorf("expected line 2, got %v", line)
	}
}

func TestSARIFRepoLevelFindingAnchorsToContract(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 4; i++ {
		files[strings.ReplaceAll(strings.ToLower(strings.Replace(
			"src/f{i}.py", "{i}", string(rune('0'+i)), 1)), " ", "")] = "x = 1\n"
	}
	_, doc := runSARIF(t, files)
	for _, r := range sarifResults(doc) {
		rm := r.(map[string]any)
		if rm["ruleId"] == "files" {
			locs := rm["locations"].([]any)
			phys := locs[0].(map[string]any)["physicalLocation"].(map[string]any)
			uri := phys["artifactLocation"].(map[string]any)["uri"].(string)
			if uri != "temple.toml" {
				t.Errorf("expected uri temple.toml, got %q", uri)
			}
			return
		}
	}
	t.Error("expected files result in SARIF")
}

func TestSARIFRuleIndexMatchesDeclaredRule(t *testing.T) {
	_, doc := runSARIF(t, map[string]string{"src/a.py": "y = 2\nNETCALL_FORBIDDEN\n"})
	rules := sarifRules(doc)
	for _, r := range sarifResults(doc) {
		rm := r.(map[string]any)
		idx := int(rm["ruleIndex"].(float64))
		ruleID := rm["ruleId"].(string)
		declaredID := rules[idx].(map[string]any)["id"].(string)
		if ruleID != declaredID {
			t.Errorf("ruleId %q at index %d does not match declared rule %q", ruleID, idx, declaredID)
		}
	}
}
