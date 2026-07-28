package main

// assets_embed_test.go — every file under assets/ is compiled into the binary by
// `//go:embed all:assets` and served at /assets. An asset nothing references is
// therefore not merely untidy: it is dead weight inside a binary whose headline
// claim is "single binary, ~30 MB, no build step".
//
// Five such files had accumulated (apple-touch-icon.png, icon-48.png,
// lilmail-logo.svg, lilmail.png, lilmail.svg) — none reachable from any template,
// stylesheet, service worker, manifest or Go source. This test stops that
// happening again, in the direction that actually matters: it fails on an asset
// that is embedded but unreachable.
//
// It deliberately does NOT check the reverse (a reference to a missing asset),
// because `//go:embed all:assets` already makes that a compile error.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assetsAllowedUnreferenced lists files that are shipped ON PURPOSE despite no
// file naming them. Every entry needs a reason; this is the only escape hatch, so
// keep it short or the test stops meaning anything.
var assetsAllowedUnreferenced = map[string]string{
	"assets/vendor/htmx.min.js.LICENSE":   "the vendored bundle's upstream licence — redistribution requires it to travel with the copy (see README → Third-party notices)",
	"assets/vendor/alpine.min.js.LICENSE": "the vendored bundle's upstream licence — redistribution requires it to travel with the copy (see README → Third-party notices)",
}

// assetSearchRoots are the places a reference to an asset can legitimately live:
// Go source (route handlers), the templates, and asset files that reference other
// assets (CSS url(), the service worker's precache list, the web manifest).
var assetSearchRoots = []string{"templates", "assets", "main.go", "handlers", "config"}

// TestEveryEmbeddedAssetIsReferenced walks assets/ and asserts each file's name
// appears somewhere that could actually reach it.
func TestEveryEmbeddedAssetIsReferenced(t *testing.T) {
	haystack := readSearchCorpus(t)

	var assets []string
	err := filepath.WalkDir("assets", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			assets = append(assets, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk assets/: %v", err)
	}

	// Guard against the walk silently finding nothing, which would make the loop
	// below pass while verifying zero assets.
	const minAssets = 10
	if len(assets) < minAssets {
		t.Fatalf("only %d files found under assets/, expected at least %d — the walk is under-running "+
			"and this test would pass without checking anything", len(assets), minAssets)
	}

	var orphans []string
	for _, a := range assets {
		if reason, ok := assetsAllowedUnreferenced[a]; ok {
			t.Logf("%s is deliberately unreferenced: %s", a, reason)
			continue
		}
		if !strings.Contains(haystack, filepath.Base(a)) {
			orphans = append(orphans, a)
		}
	}
	if len(orphans) > 0 {
		t.Fatalf("%d asset(s) are embedded in the binary but referenced by nothing:\n  %s\n\n"+
			"Delete them, or add them to assetsAllowedUnreferenced with a reason. Every file under "+
			"assets/ is compiled in by //go:embed all:assets, so an unreachable one is dead weight "+
			"shipped to every user.", len(orphans), strings.Join(orphans, "\n  "))
	}
}

// readSearchCorpus concatenates every file that could reference an asset. It
// excludes the asset's own path by construction (we match on basename against
// OTHER files' contents), so a file naming itself cannot count as a reference.
func readSearchCorpus(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, root := range assetSearchRoots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".woff2", ".ttf", ".ico":
				return nil // binary; cannot contain a textual reference
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			text := string(data)
			// An asset naming itself is not a reference TO itself — strip self-mentions
			// so e.g. a stylesheet with a `/*! mail.css */` banner cannot vouch for its
			// own inclusion.
			if strings.HasPrefix(filepath.ToSlash(path), "assets/") {
				text = strings.ReplaceAll(text, filepath.Base(path), "")
			}
			b.WriteString(text)
			b.WriteByte('\n')
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if b.Len() < 1024 {
		t.Fatalf("search corpus is only %d bytes — the roots %v did not load, so every asset would "+
			"look unreferenced", b.Len(), assetSearchRoots)
	}
	return b.String()
}
