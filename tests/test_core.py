import json
import shutil
import tempfile
import unittest
from pathlib import Path

from temple import core

CONTRACT = """
[scope]
purpose = "test fixture"
allow = ["src/**", "*.toml"]
max_files = 3
max_lines = 20
max_deps = 0
forbid = ["NETCALL_FORBIDDEN"]
"""


def _write(root: Path, rel: str, text: str = "") -> Path:
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    return path


class TempleChecks(unittest.TestCase):
    def _run(self, files: dict[str, str], contract: str = CONTRACT):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        for rel, text in files.items():
            _write(root, rel, text)
        config = _write(root, "temple.toml", contract)
        return core.run(root, config)

    def test_pass_within_scope(self):
        findings, _, _ = self._run({"src/a.py": "x = 1\n"})
        self.assertEqual(findings, [])

    def test_structure_violation(self):
        findings, _, _ = self._run({"src/a.py": "x = 1\n", "stray.txt": "nope\n"})
        self.assertIn("structure", {f.check for f in findings})

    def test_forbidden_pattern_reports_line(self):
        findings, _, _ = self._run({"src/a.py": "y = 2\nNETCALL_FORBIDDEN\n"})
        forb = [f for f in findings if f.check == "forbid"]
        self.assertTrue(forb)
        self.assertEqual(forb[0].line, 2)
        self.assertEqual(forb[0].path, "src/a.py")

    def test_file_ceiling(self):
        files = {f"src/f{i}.py": "x = 1\n" for i in range(4)}  # 4 + toml = 5 > 3
        findings, _, _ = self._run(files)
        self.assertIn("files", {f.check for f in findings})

    def test_line_budget(self):
        body = "\n".join("x = 1" for _ in range(25)) + "\n"
        findings, _, _ = self._run({"src/a.py": body})
        self.assertIn("lines", {f.check for f in findings})

    def test_dep_ceiling(self):
        pyproject = (
            '[project]\nname = "x"\nversion = "0"\n'
            'dependencies = ["requests", "click"]\n'
        )
        findings, _, _ = self._run({"src/a.py": "x = 1\n", "pyproject.toml": pyproject})
        self.assertIn("deps", {f.check for f in findings})


class GlobMatching(unittest.TestCase):
    def test_double_star_crosses_dirs(self):
        self.assertTrue(core._glob_to_regex("src/**").match("src/deep/a.py"))

    def test_single_star_stays_in_segment(self):
        self.assertTrue(core._glob_to_regex("*.md").match("README.md"))
        self.assertFalse(core._glob_to_regex("*.md").match("docs/x.md"))

    def test_leading_double_star(self):
        self.assertTrue(core._glob_to_regex("**/*.py").match("a/b/c.py"))
        self.assertTrue(core._glob_to_regex("**/*.py").match("top.py"))


class SarifOutput(unittest.TestCase):
    def _run(self, files: dict[str, str], contract: str = CONTRACT):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        for rel, text in files.items():
            _write(root, rel, text)
        config = _write(root, "temple.toml", contract)
        findings, contract_obj, stats = core.run(root, config)
        doc = json.loads(core.render_sarif(contract_obj, findings, stats, "temple.toml"))
        return findings, doc

    def test_valid_envelope_when_clean(self):
        _, doc = self._run({"src/a.py": "x = 1\n"})
        self.assertEqual(doc["version"], "2.1.0")
        self.assertEqual(doc["runs"][0]["tool"]["driver"]["name"], "temple")
        self.assertEqual(doc["runs"][0]["results"], [])

    def test_forbid_result_carries_file_and_line(self):
        _, doc = self._run({"src/a.py": "y = 2\nNETCALL_FORBIDDEN\n"})
        forb = [r for r in doc["runs"][0]["results"] if r["ruleId"] == "forbid"]
        self.assertTrue(forb)
        phys = forb[0]["locations"][0]["physicalLocation"]
        self.assertEqual(phys["artifactLocation"]["uri"], "src/a.py")
        self.assertEqual(phys["region"]["startLine"], 2)

    def test_repo_level_finding_anchors_to_contract(self):
        files = {f"src/f{i}.py": "x = 1\n" for i in range(4)}  # trips the file ceiling
        _, doc = self._run(files)
        files_r = [r for r in doc["runs"][0]["results"] if r["ruleId"] == "files"]
        self.assertTrue(files_r)
        uri = files_r[0]["locations"][0]["physicalLocation"]["artifactLocation"]["uri"]
        self.assertEqual(uri, "temple.toml")

    def test_ruleindex_matches_declared_rule(self):
        _, doc = self._run({"src/a.py": "y = 2\nNETCALL_FORBIDDEN\n"})
        rules = doc["runs"][0]["tool"]["driver"]["rules"]
        for r in doc["runs"][0]["results"]:
            self.assertEqual(rules[r["ruleIndex"]]["id"], r["ruleId"])


if __name__ == "__main__":
    unittest.main()
