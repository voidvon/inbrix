// storage/repogrep_test.go — a tiny source scanner shared by the signing tests.
//
// Some invariants in this repo are about what the SOURCE does NOT contain (see
// TestNoOutboundWebhookSigner). Expressing those as a test keeps them running in
// CI on every push instead of living in a review checklist nobody re-reads.
//
// It walks the module root (found by climbing to go.mod) rather than shelling out
// to git or grep, so it behaves identically on a developer machine, in CI, and in
// a source tarball with no VCS metadata.
package storage

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoHit is one matching source line, with a module-root-relative path.
type repoHit struct {
	file string
	line int
	text string
}

// repoRoot climbs from the current directory to the directory holding go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above " + dir)
		}
		dir = parent
	}
}

// grepRepo returns every non-test .go line in the module matching pattern.
// Vendored/third-party trees and test files are skipped: the invariants this
// backs are about lilmail's own shipped code.
func grepRepo(pattern string) ([]repoHit, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}

	var hits []repoHit
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "site", "assets", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for n := 1; sc.Scan(); n++ {
			if line := sc.Text(); re.MatchString(line) {
				hits = append(hits, repoHit{file: rel, line: n, text: strings.TrimSpace(line)})
			}
		}
		return sc.Err()
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// TestGrepRepoFindsKnownContent is the scanner's own smoke test. Without it, a
// broken walker would make every invariant that uses grepRepo pass vacuously.
func TestGrepRepoFindsKnownContent(t *testing.T) {
	hits, err := grepRepo(`func ObjectStoreFromHeaders\(`)
	if err != nil {
		t.Fatalf("grepRepo: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly 1 hit for ObjectStoreFromHeaders, got %d (%v) — the scanner is not reading the repo", len(hits), hits)
	}
	if hits[0].file != "storage/object.go" {
		t.Fatalf("hit path = %q, want storage/object.go (paths must be module-root-relative)", hits[0].file)
	}

	// A pattern that must NOT match, so the regex is proven to be applied at all.
	none, err := grepRepo(`zzz-this-string-does-not-exist-in-lilmail`)
	if err != nil {
		t.Fatalf("grepRepo: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("scanner matched a nonexistent pattern %d times", len(none))
	}

	// Test files must be excluded — this very file contains the pattern above.
	for _, h := range hits {
		if strings.HasSuffix(h.file, "_test.go") {
			t.Fatalf("scanner included a test file: %s", h.file)
		}
	}
}
