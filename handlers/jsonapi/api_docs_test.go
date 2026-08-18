// handlers/jsonapi/api_docs_test.go — the /v1 surface must match docs/API.md.
//
// WHY THIS EXISTS: docs/API.md is the contract external clients build against;
// inbrix's own README calls /v1 "a stable seam an external UI can build
// against". The failure mode that document has in practice is not being WRONG —
// it is being INCOMPLETE: a route lands, no one updates the reference, and a
// consumer writing a client from the document alone cannot discover it. That gap
// is invisible to every other test in this package, because they all drive routes
// they already know about.
//
// This test closes it from both directions:
//
//	TestEveryV1RouteIsDocumented — every route Register() mounts appears in
//	  docs/API.md. A new endpoint fails CI until it is documented.
//	TestNoDocumentedV1RouteIsMissing — every /v1 path the document advertises is
//	  actually mounted. A removed endpoint fails CI until the doc drops it, so the
//	  reference cannot promise a 404.
//	TestV1RouteCoverageFloor — the enumeration itself found a plausible number of
//	  routes. Without this, a change that made GetRoutes return nothing would make
//	  both tests above pass by checking nothing at all.
package jsonapi

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"inbrix/config"
	"inbrix/handlers/web"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// apiDocPath is the reference this test holds the code to.
const apiDocPath = "../../docs/API.md"

// routesNotInReferenceTable lists routes intentionally absent from the endpoint
// tables, with the reason. Keep this SHORT and justified — it is the escape hatch
// that would let the whole test rot if it grew.
//
// (Currently empty: every mounted route is documented. An entry here must explain
// why a consumer does not need the route to write a client.)
var routesNotInReferenceTable = map[string]string{}

// newFullSurfaceApp mounts the WIDEST possible /v1 surface: CalDAV and CardDAV
// enabled in config AND the broker secret set, so every conditionally-registered
// group is present. A test that mounted only the default surface would silently
// stop checking the calendar and contacts routes — the two groups most likely to
// drift, since they are off in a bare build.
func newFullSurfaceApp(t *testing.T) *fiber.App {
	t.Helper()
	t.Setenv(brokerEnvSecret, "docs-test-secret")

	store := session.New()
	cfg := &config.Config{}
	cfg.Encryption.Key = parityTestKey
	cfg.CalDAV.Enabled = true
	cfg.CardDAV.Enabled = true
	cfg.CardDAV.URL = "https://dav.example.com/carddav/"

	h := New(store, cfg, web.NewAuthHandler(store, cfg))
	app := fiber.New()
	h.Register(app)
	return app
}

// mountedV1Routes returns the deduplicated "METHOD /v1/path" set actually mounted,
// with Fiber's :params preserved (that is how docs/API.md writes them too).
func mountedV1Routes(t *testing.T) []string {
	t.Helper()
	app := newFullSurfaceApp(t)

	seen := map[string]bool{}
	for _, r := range app.GetRoutes(true) {
		method := strings.ToUpper(r.Method)
		// Fiber registers an implicit HEAD for every GET and an OPTIONS handler;
		// neither is a distinct documented endpoint.
		if method == "HEAD" || method == "OPTIONS" {
			continue
		}
		if !strings.HasPrefix(r.Path, "/v1/") && r.Path != "/v1" {
			continue
		}
		seen[method+" "+r.Path] = true
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// readAPIDoc loads docs/API.md. A missing document is a hard failure, never a
// skip: silently passing when the contract is gone is the exact outcome this
// file exists to prevent.
func readAPIDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(apiDocPath)
	if err != nil {
		abs, _ := filepath.Abs(apiDocPath)
		t.Fatalf("cannot read the API reference at %s: %v", abs, err)
	}
	if len(b) < 4096 {
		t.Fatalf("%s is only %d bytes — that cannot be the full /v1 reference", apiDocPath, len(b))
	}
	return string(b)
}

// docPathRE matches a /v1 path as written in docs/API.md: inside backticks, in a
// curl example, or bare in prose. Trailing punctuation and query strings are
// trimmed by the caller.
var docPathRE = regexp.MustCompile(`/v1(?:/[A-Za-z0-9_:.\-]+)*`)

// documentedPaths returns every distinct /v1 path mentioned anywhere in the doc,
// normalised the way normalisePath normalises a mounted route.
func documentedPaths(doc string) map[string]bool {
	out := map[string]bool{}
	for _, m := range docPathRE.FindAllString(doc, -1) {
		out[normalisePath(m)] = true
	}
	return out
}

// normalisePath makes a mounted route and a documented path comparable: it drops
// a query string, strips trailing punctuation the prose may carry, collapses
// every :param to a single placeholder (the doc is free to name a param
// differently from the code), and drops a trailing slash.
func normalisePath(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimRight(p, ".,;:`)>'\"")
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") || (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) {
			segs[i] = ":param"
		}
	}
	p = strings.Join(segs, "/")
	if len(p) > 3 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// TestEveryV1RouteIsDocumented fails when a mounted route's path is nowhere in
// docs/API.md.
func TestEveryV1RouteIsDocumented(t *testing.T) {
	doc := readAPIDoc(t)
	documented := documentedPaths(doc)

	var missing []string
	for _, route := range mountedV1Routes(t) {
		method, path, _ := strings.Cut(route, " ")
		if reason, ok := routesNotInReferenceTable[route]; ok {
			t.Logf("route %s is deliberately undocumented: %s", route, reason)
			continue
		}
		if !documented[normalisePath(path)] {
			missing = append(missing, method+" "+path)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d /v1 route(s) are mounted but absent from %s:\n  %s\n\n"+
			"A consumer writing a client from the reference alone cannot discover these. "+
			"Document them (or, with a written reason, add them to routesNotInReferenceTable).",
			len(missing), apiDocPath, strings.Join(missing, "\n  "))
	}
}

// TestEveryV1RouteHasItsMethodDocumented is the stricter half: the path being
// mentioned somewhere is not enough — the METHOD must appear too, because
// "GET /v1/contacts/groups is documented" says nothing about DELETE.
//
// It looks for the method token within a window around each mention of the path,
// which is how both the endpoint tables (`| DELETE | /v1/… |`) and the curl
// examples (`curl -X DELETE … /v1/…`) are laid out.
func TestEveryV1RouteHasItsMethodDocumented(t *testing.T) {
	doc := readAPIDoc(t)

	var missing []string
	for _, route := range mountedV1Routes(t) {
		method, path, _ := strings.Cut(route, " ")
		if _, ok := routesNotInReferenceTable[route]; ok {
			continue
		}
		if !methodDocumentedNear(doc, method, normalisePath(path)) {
			missing = append(missing, route)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d /v1 route(s) whose METHOD is not documented alongside the path in %s:\n  %s\n\n"+
			"The path appears, but not with this verb — a client cannot tell the endpoint accepts it.",
			len(missing), apiDocPath, strings.Join(missing, "\n  "))
	}
}

// methodDocumentedNear reports whether method appears within 220 bytes before any
// mention of path in doc. 220 bytes comfortably spans a markdown table row
// ("| DELETE | `/v1/contacts/groups/:name` | …") and a curl invocation, without
// spilling into an unrelated neighbouring row.
func methodDocumentedNear(doc, method, path string) bool {
	const window = 220
	for _, m := range docPathRE.FindAllStringIndex(doc, -1) {
		if normalisePath(doc[m[0]:m[1]]) != path {
			continue
		}
		start := m[0] - window
		if start < 0 {
			start = 0
		}
		if strings.Contains(doc[start:m[1]], method) {
			return true
		}
	}
	return false
}

// matchesMounted reports whether a path written in the documentation corresponds
// to something Register() mounts. Two allowances, both needed because prose is
// not a routing table:
//
//   - a mounted ":param" segment matches any single documented segment, so the
//     curl example `/v1/messages/42/flags` resolves to `/v1/messages/:uid/flags`;
//   - a documented path that is a segment-wise PREFIX of a mounted route is fine,
//     because the doc legitimately names groups (`/v1/calendar/*`, `/v1/settings`).
//
// It deliberately does NOT allow the reverse (a documented path with extra
// segments beyond any mounted route) — that is the phantom-endpoint case.
func matchesMounted(documented string, mounted [][]string) bool {
	docSegs := strings.Split(documented, "/")
	for _, routeSegs := range mounted {
		if len(docSegs) > len(routeSegs) {
			continue
		}
		ok := true
		for i, ds := range docSegs {
			if routeSegs[i] == ":param" {
				continue // a concrete value in an example stands in for the param
			}
			if routeSegs[i] != ds {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// TestNoDocumentedV1RouteIsMissing catches the opposite drift: the reference
// advertising an endpoint that no longer exists. It compares against the widest
// surface, so a route that is merely config-gated does not trip it.
func TestNoDocumentedV1RouteIsMissing(t *testing.T) {
	doc := readAPIDoc(t)

	var mounted [][]string
	for _, route := range mountedV1Routes(t) {
		_, path, _ := strings.Cut(route, " ")
		mounted = append(mounted, strings.Split(normalisePath(path), "/"))
	}

	var phantom []string
	for p := range documentedPaths(doc) {
		if p == "/v1" {
			continue // the surface as a whole, not an endpoint
		}
		if !matchesMounted(p, mounted) {
			phantom = append(phantom, p)
		}
	}
	sort.Strings(phantom)
	if len(phantom) > 0 {
		t.Fatalf("%d path(s) documented in %s are not mounted by Register():\n  %s\n\n"+
			"The reference is promising endpoints that would 404.",
			len(phantom), apiDocPath, strings.Join(phantom, "\n  "))
	}
}

// TestV1RouteCoverageFloor asserts the enumeration is doing real work. Both
// direction tests above iterate over mountedV1Routes(); if that returned an empty
// or truncated slice they would pass while verifying nothing. It also pins that
// the conditionally-registered calendar and contacts groups really did mount, so
// a config/gating regression cannot quietly shrink what is being checked.
func TestV1RouteCoverageFloor(t *testing.T) {
	routes := mountedV1Routes(t)

	// Raise this when routes are added; it must never be lowered to make a red
	// build green. It is the floor that stops this harness passing by doing less.
	const minRoutes = 45
	if len(routes) < minRoutes {
		t.Fatalf("only %d /v1 routes enumerated, want at least %d — the route walk is under-running "+
			"and the documentation tests above are checking almost nothing:\n  %s",
			len(routes), minRoutes, strings.Join(routes, "\n  "))
	}

	// Representative routes from each conditionally-mounted group.
	for _, want := range []string{
		"GET /v1/me",
		"POST /v1/messages",
		"GET /v1/calendar/events",
		"POST /v1/calendar/rsvp",
		"GET /v1/contacts/cards",
		"GET /v1/contacts/groups",
		"POST /v1/contacts/import",
		"GET /v1/contacts/export",
		"POST /v1/contacts/:uid/photo",
		"GET /v1/contacts/frequent",
		"GET /v1/settings/identities",
		"GET /v1/accounts",
		"GET /v1/scheduled",
	} {
		found := false
		for _, r := range routes {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected route %q was not mounted on the full surface — either it was removed "+
				"(update this list and docs/API.md) or its registration gate regressed", want)
		}
	}
}

// TestSigningDocIsReferenced keeps docs/SIGNING.md discoverable. The broker seam
// is the one part of the auth model a consumer can get catastrophically wrong
// (it is a bearer secret with no replay protection), so the API reference must
// point at the document that says so.
func TestSigningDocIsReferenced(t *testing.T) {
	doc := readAPIDoc(t)
	if !strings.Contains(doc, "SIGNING.md") {
		t.Fatal("docs/API.md no longer links docs/SIGNING.md — the injected-credential " +
			"seam's exact wire format and its lack of replay protection would be undiscoverable")
	}
	if _, err := os.Stat("../../docs/SIGNING.md"); err != nil {
		t.Fatalf("docs/API.md links docs/SIGNING.md but it is not there: %v", err)
	}
}
