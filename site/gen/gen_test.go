// gen_test.go is the published-docs gate.
//
// The gate it replaces compared source bytes to copy bytes and reported
// "site/docs is in sync" while the published bundle carried links that 404 and
// heading anchors that land nowhere. Freshness is necessary but it is not the
// property readers care about, so this file checks both:
//
//   - TestSiteDocsInSync            — site/docs is exactly what the generator makes.
//   - TestSiteDocsHaveNoDeadLinks   — every relative link in every published
//     chapter resolves against what site/ actually serves, fragments included.
//   - TestSiteDocsNavMatchesGenerator — docs.html's nav and the generator agree.
//   - TestViewerContractUnchanged   — the docs.html behaviour the rewriting
//     depends on is still there.
//   - TestSlugifyMatchesViewer      — Go's slugify still equals the viewer's.
//
// Every test fails closed: if it cannot find the repo, read a file, or scan
// anything, it calls t.Fatal naming what was NOT verified rather than passing.
package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// minLinks is a coverage floor. The bundle carries 28 relative links today; if a
// scan ever reports fewer than this the scanner is broken, not the docs, and a
// gate that checks almost nothing must fail rather than report success.
const minLinks = 20

// minPages guards the same way against a truncated page list.
const minPages = 8

// root locates the module root, failing loudly rather than skipping — a docs
// guard that quietly does nothing is worse than no guard at all.
func root(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v — the published-docs gate verified NOTHING", err)
	}
	r, err := findRoot(cwd)
	if err != nil {
		t.Fatalf("%v — the published-docs gate verified NOTHING", err)
	}
	return r
}

// TestSiteDocsInSync is the anti-rot half: site/docs must be exactly what the
// generator produces. Edit docs/API.md and forget to regenerate, and this fails
// naming the file.
func TestSiteDocsInSync(t *testing.T) {
	r := root(t)
	want, _, err := generate(r)
	if err != nil {
		t.Fatalf("generate: %v — the published-docs gate verified NOTHING", err)
	}
	if len(pages) < minPages || len(want) != len(pages) {
		t.Fatalf("generator produced %d of %d pages (floor %d) — too few to be a real check",
			len(want), len(pages), minPages)
	}

	dir := filepath.Join(r, "site", "docs")
	for name, body := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("site/docs/%s is missing: %v — run `make site-docs`", name, err)
			continue
		}
		if string(got) != body {
			t.Errorf("site/docs/%s is STALE (it is not what the generator makes from its source). "+
				"docs/ + ROADMAP.md + CHANGELOG.md are the single source; run `make site-docs`.", name)
		}
	}

	// No orphans: a file in site/docs the generator does not produce is a
	// hand-maintained copy, and hand-maintained copies rot.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read site/docs: %v — the orphan check verified NOTHING", err)
	}
	md := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		md++
		if _, ok := want[e.Name()]; !ok {
			t.Errorf("site/docs/%s is not generated from anything — add it to `pages` in "+
				"site/gen/main.go (and to DOCS in site/docs.html) or delete it", e.Name())
		}
	}
	if md < minPages {
		t.Fatalf("only %d markdown files found in site/docs (floor %d) — the orphan scan verified almost nothing", md, minPages)
	}
}

// TestSiteDocsHaveNoDeadLinks is the half the old gate did not have. It reads
// what site/docs actually ships and resolves every relative link the way
// site/docs.html will at runtime:
//
//   - "<chapter>.md[#frag]" — rule 5 turns it into in-page navigation, and the
//     fragment must be an id the viewer really assigns (h2/h3 slugify).
//   - "screenshots/<img>"   — rule 4 re-points it at site/screenshots/.
//   - a bare "#frag"        — DEAD: the hash router reads it as a document slug
//     and navigates the reader to a different chapter.
//   - anything else relative — DEAD: the browser resolves it against site/, and
//     site/ ships only these chapters, the screenshots and the two HTML pages.
func TestSiteDocsHaveNoDeadLinks(t *testing.T) {
	r := root(t)
	dir := filepath.Join(r, "site", "docs")

	// Published chapters, and the anchors each will expose, read from what
	// actually ships rather than from the sources.
	published := map[string]bool{}
	anchors := map[string]map[string]bool{}
	bodies := map[string]string{}
	for _, p := range pages {
		raw, err := os.ReadFile(filepath.Join(dir, p.dst))
		if err != nil {
			t.Fatalf("read site/docs/%s: %v — the link scan verified NOTHING", p.dst, err)
		}
		published[p.dst] = true
		bodies[p.dst] = string(raw)
		anchors[p.dst], _ = anchorsFor(string(raw))
	}

	totalAnchors := 0
	for _, a := range anchors {
		totalAnchors += len(a)
	}
	if totalAnchors == 0 {
		t.Fatal("no heading anchors were derived from any chapter — the anchor scan is broken and fragments were NOT verified")
	}

	links, absolute := 0, 0
	for _, p := range pages {
		eachLink(bodies[p.dst], func(line int, target string) {
			where := fmt.Sprintf("site/docs/%s:%d", p.dst, line)
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") {
				if strings.HasPrefix(target, repoURL) {
					absolute++
				}
				return
			}
			links++

			file, frag := target, ""
			if j := strings.IndexByte(target, '#'); j != -1 {
				file, frag = target[:j], target[j+1:]
			}

			if file == "" {
				t.Errorf("DEAD LINK %s: bare fragment %q — site/docs.html's hash router reads "+
					"\"#%s\" as a document slug and navigates away from this chapter; it must be "+
					"written \"%s#%s\"", where, target, frag, p.dst, frag)
				return
			}

			// Rule 5 matches on the basename, case-insensitively.
			base := strings.ToLower(path.Base(file))
			if published[base] {
				if base != file {
					t.Errorf("DEAD-ISH LINK %s: %q reaches the right chapter only through "+
						"site/docs.html's basename fallback; the generator must emit %q", where, target, base)
				}
				if frag != "" && !anchors[base][frag] {
					t.Errorf("DEAD ANCHOR %s: %q — %q is not a heading id site/docs.html assigns in %s "+
						"(it slugifies h2/h3 text, deleting backticks and underscores and collapsing "+
						"runs of hyphens)", where, target, frag, base)
				}
				return
			}

			if strings.HasPrefix(file, "screenshots/") && imageExts[strings.ToLower(path.Ext(file))] {
				if _, err := os.Stat(filepath.Join(r, "site", "screenshots", path.Base(file))); err != nil {
					t.Errorf("DEAD IMAGE %s: %q — site/screenshots/%s does not ship in the bundle",
						where, target, path.Base(file))
				}
				return
			}

			t.Errorf("DEAD LINK %s: %q — the viewer resolves relative URLs against site/, and "+
				"site/%s is not in the published bundle. It must be rewritten to an absolute repo URL "+
				"by site/gen (run `make site-docs`).", where, target, file)
		})
	}

	if links < minLinks {
		t.Fatalf("only %d relative links were checked (floor %d) — the link scan is broken and the "+
			"published bundle was NOT verified", links, minLinks)
	}
	if absolute == 0 {
		t.Fatal("no link was rewritten to an absolute repo URL — the generator's escape-hatch rewriting " +
			"is not running, so out-of-bundle links were NOT verified")
	}
	t.Logf("published bundle: %d relative links and %d anchors checked across %d chapters "+
		"(%d links rewritten to %s)", links, totalAnchors, len(pages), absolute, repoURL)
}

// TestSourceLinksExistInRepo checks the generator's escape hatch. A link that
// cannot resolve inside the bundle is rewritten to an absolute repo URL — which
// makes it invisible to every relative-path check, including the one above. So a
// typo (../config.tml.example, docs/SIGNIN.md) would be laundered into a
// well-formed GitHub 404 that nothing catches. This test demands that each
// escaped path names a file the repository really contains.
func TestSourceLinksExistInRepo(t *testing.T) {
	r := root(t)
	_, b, err := generate(r)
	if err != nil {
		t.Fatalf("generate: %v — the escape-hatch check verified NOTHING", err)
	}
	if len(b.escapes) == 0 {
		t.Fatal("the generator rewrote no link to an absolute repo URL — either the escape hatch is " +
			"not running or the scan is broken; out-of-bundle links were NOT verified")
	}
	for _, e := range b.brokenEscapes() {
		t.Errorf("DEAD LINK %s:%d: %q points at %q, which this repository does not contain — "+
			"rewriting it to %s%s only makes it a 404 on GitHub instead of in the bundle",
			e.src, e.line, e.orig, e.rel, repoURL, e.rel)
	}
	t.Logf("escape hatch: %d out-of-bundle links checked against the repo tree", len(b.escapes))
}

// TestSiteDocsNavMatchesGenerator keeps site/docs.html's DOCS table and the
// generator's page list from drifting: a nav entry for a page nobody generates
// is a 404 in the viewer, and a generated page missing from the nav is
// unreachable — and, because rule 5 only rewrites links to chapters listed in
// DOCS, also silently turns every link to it into a dead one.
func TestSiteDocsNavMatchesGenerator(t *testing.T) {
	r := root(t)
	html, err := os.ReadFile(filepath.Join(r, "site", "docs.html"))
	if err != nil {
		t.Fatalf("read site/docs.html: %v — the nav check verified NOTHING", err)
	}
	navRE := regexp.MustCompile(`path\s*:\s*'\./docs/([^']+)'`)
	var nav []string
	for _, m := range navRE.FindAllStringSubmatch(string(html), -1) {
		nav = append(nav, m[1])
	}
	if len(nav) < minPages {
		t.Fatalf("found %d doc entries in site/docs.html (floor %d) — the nav scan is broken and verified NOTHING", len(nav), minPages)
	}

	var generated []string
	for _, p := range pages {
		generated = append(generated, p.dst)
	}
	sort.Strings(nav)
	sort.Strings(generated)
	if strings.Join(nav, ",") != strings.Join(generated, ",") {
		t.Errorf("site/docs.html nav and the generator disagree:\n  nav:       %v\n  generated: %v", nav, generated)
	}
}

// TestViewerContractUnchanged pins the three site/docs.html behaviours the
// rewriting is built on. If the viewer stops rewriting ".md" hrefs, stops
// re-pointing screenshot images, or stops routing on the hash, then the
// generated links this gate calls "live" become dead and the gate would still
// pass. Fail here instead, naming what has to be re-derived.
func TestViewerContractUnchanged(t *testing.T) {
	r := root(t)
	raw, err := os.ReadFile(filepath.Join(r, "site", "docs.html"))
	if err != nil {
		t.Fatalf("read site/docs.html: %v — the viewer contract verified NOTHING", err)
	}
	html := string(raw)
	for _, c := range []struct{ what, needle string }{
		{"rule 5 — hrefs ending in <base>.md become in-page navigation",
			`const m = h.match(/([^\/]+\.md)(#.*)?$/i);`},
		{"rule 5 — the basename is looked up case-insensitively in byFile",
			`const target = m && byFile[m[1].toLowerCase()];`},
		{"byFile is keyed on the DOCS table's filenames",
			`const byFile = Object.fromEntries(DOCS.map(d => [d.path.split('/').pop().toLowerCase(), d]));`},
		{"rule 4 — screenshot images are re-pointed at ./screenshots/",
			`const m = s.match(/(?:\.\.\/)*(?:docs\/)?screenshots\/(.+)$/);`},
		{"the hash router reads the first hash segment as a document slug",
			`const [slug, ...rest] = raw.split('/');`},
		{"heading ids come from h2/h3 only",
			`root.querySelectorAll('h2, h3').forEach(h => {`},
	} {
		if !strings.Contains(html, c.needle) {
			t.Errorf("site/docs.html no longer contains the behaviour the generator targets (%s).\n"+
				"  expected to find: %s\n"+
				"  Re-derive the rewriting in site/gen/main.go against the new viewer before trusting this gate.",
				c.what, c.needle)
		}
	}
}

// TestEachLinkSkipsOnlyCode pins the scanner itself. eachLink is the single
// definition of "a link in this document" that both the generator and the gate
// use, and it deliberately ignores fenced blocks and inline code spans. A bug
// that widened that exclusion would make every test above pass by scanning
// nothing, so the exclusion is asserted in both directions.
func TestEachLinkSkipsOnlyCode(t *testing.T) {
	const doc = "See [api](docs/API.md) and [cfg](docs/CONFIGURATION.md#storage).\n" +
		"Prose about `[Attachments](#attachments)` written as code.\n" +
		"```\n" +
		"[fenced](docs/NOPE.md)\n" +
		"```\n" +
		"Trailing [roadmap](ROADMAP.md).\n"

	var got []string
	eachLink(doc, func(_ int, target string) { got = append(got, target) })

	want := []string{"docs/API.md", "docs/CONFIGURATION.md#storage", "ROADMAP.md"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("eachLink scanned %v, want %v — the scanner every other test in this file "+
			"depends on is wrong, so those tests verified the wrong thing", got, want)
	}

	// And the rewriter must leave both kinds of code byte-identical.
	b := &bundle{
		byPath:  map[string]string{"docs/API.md": "api.md"},
		anchors: map[string]map[string]bool{"api.md": {}},
		fuzzy:   map[string]map[string]string{"api.md": {}},
	}
	out := b.rewriteLinks("X.md", "x.md", doc)
	if !strings.Contains(out, "`[Attachments](#attachments)`") {
		t.Error("rewriteLinks rewrote inside an inline code span — prose about a link is not a link")
	}
	if !strings.Contains(out, "[fenced](docs/NOPE.md)") {
		t.Error("rewriteLinks rewrote inside a fenced code block — sample code must survive verbatim")
	}
	if !strings.Contains(out, "[api](api.md)") {
		t.Error("rewriteLinks did not rewrite a real link — the rewriting is not running")
	}
}

// TestSlugifyMatchesViewer pins the Go copy of the viewer's slugify(). It is the
// single most load-bearing assumption in the anchor check: if the viewer's rule
// changes and this copy does not, every fragment this gate passes is a lie.
func TestSlugifyMatchesViewer(t *testing.T) {
	r := root(t)
	raw, err := os.ReadFile(filepath.Join(r, "site", "docs.html"))
	if err != nil {
		t.Fatalf("read site/docs.html: %v — slugify was NOT verified against the viewer", err)
	}
	const js = "const slugify = s => s.toLowerCase().trim()\n" +
		"  .replace(/[`*_~]/g, '').replace(/[^\\w\\s-]/g, '').replace(/\\s+/g, '-').replace(/-+/g, '-');"
	if !strings.Contains(string(raw), js) {
		t.Errorf("site/docs.html's slugify() has changed. site/gen/main.go's slugify must be updated to "+
			"match it byte for byte, or every heading anchor this gate approves may be dead.\n"+
			"  expected:\n%s", js)
	}

	// Cases that actually bite in this repo: underscores and backticks are
	// deleted (not turned into hyphens), and runs of hyphens collapse — which is
	// exactly where GitHub's slugger and this one part company.
	for _, c := range []struct{ in, want string }{
		{"Shared object storage (`VULOS_STORAGE_BROKER_SECRET`)", "shared-object-storage-vulosstoragebrokersecret"},
		{"[1.10.0] - 2026-06-22", "1100-2026-06-22"},
		{"0. lilmail emits no webhooks", "0-lilmail-emits-no-webhooks"},
		{"`[storage]`", "storage"},
		{"Injected-credential mode (embedding hosts)", "injected-credential-mode-embedding-hosts"},
	} {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
