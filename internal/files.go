package internal

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// walkSkip is the set of directory names ignored during the fallback walk.
var walkSkip = map[string]bool{
	".git": true, "__pycache__": true, ".venv": true, "venv": true,
	"node_modules": true, ".mypy_cache": true, ".pytest_cache": true,
	".ruff_cache": true, "dist": true, "build": true, "target": true,
	".idea": true, ".tox": true,
}

// TrackedFiles returns the list of files to check. It prefers git ls-files
// (which honours .gitignore) and falls back to a filtered directory walk
// when git is unavailable or the directory is not a repository.
func TrackedFiles(root string) ([]string, error) {
	if files, err := gitLsFiles(root); err == nil && len(files) > 0 {
		return files, nil
	}
	return walkFiles(root)
}

func gitLsFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			files = append(files, filepath.Join(root, filepath.FromSlash(line)))
		}
	}
	return files, sc.Err()
}

func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if walkSkip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// CountLines returns the number of newline-terminated lines in a file.
func CountLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	return n
}

// HasExt reports whether path has one of the given extensions.
func HasExt(path string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// RelPosix returns the POSIX-style relative path of abs with respect to root.
func RelPosix(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}
