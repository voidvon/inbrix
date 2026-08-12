.PHONY: build test vet fmt-check notices screenshots demo-screenshots site-docs site-docs-check site-render verify-selftest check clean

# Build the lilmail binary
build:
	npm run build
	go build -o lilmail .

# Regenerate THIRD-PARTY-NOTICES.txt from the real dependency graph (Go modules
# + vendored browser assets). The file is embedded in the binary and served at
# /licenses.txt; re-run after changing go.mod or assets/vendor/.
notices:
	./scripts/gen-notices.sh

# Run all tests
test:
	go test ./...

# Vet
vet:
	go vet ./...

# Fail if anything is unformatted (the same gate CI runs).
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt found unformatted files:"; echo "$$unformatted"; exit 1; fi; \
	echo "gofmt: clean"

# Fail if the published docs have drifted from their sources OR carry a link
# that the site cannot serve (the same gate CI runs). `go test ./...` runs it
# too — this target exists so it can be invoked on its own.
site-docs-check:
	go test ./site/gen -count=1

# Render site/index.html and site/docs.html in Chromium at eight viewports in
# both colour schemes and fail on what only shows up after layout: a stretched
# screenshot, both theme variants painted at once, a 1x image on a 2x screen,
# body copy under 12px, horizontal overflow, a dead anchor. --selftest breaks
# each invariant on purpose and demands the check notice.
site-render:
	cd scripts && npm install --silent && npx --yes playwright install chromium 2>/dev/null || true
	node scripts/check-render.mjs --selftest
	node scripts/check-render.mjs

# Prove the release verifier still REFUSES. scripts/verify.sh is what a user
# runs before executing a downloaded release; this drives 24 synthetic-origin
# cases (missing manifest, HTML error page, no entry, truncated download,
# digest mismatch, ...) and asserts both the exit code and that a diagnostic
# was printed. A checksum guard that has quietly stopped failing looks exactly
# like one that works until the day it matters.
verify-selftest:
	bash scripts/verify.sh --selftest

# Everything CI runs, in the same order. Run this before opening a PR.
check: build fmt-check vet test site-docs-check site-render verify-selftest

# Regenerate docs/screenshots/*.png using Playwright.
# Boots lilmail with a minimal demo config (login page captured without credentials).
# Set LILMAIL_* env vars for inbox/message/compose/settings screenshots (see docs/SCREENSHOTS.md).
screenshots: build
	@echo "==> Installing Playwright dependencies (first run only)..."
	cd scripts && npm install --silent && npx --yes playwright install chromium 2>/dev/null || true
	@echo "==> Running screenshotter..."
	cd scripts && node screenshots.mjs
	@echo "==> Screenshots written to docs/screenshots/"

# Regenerate screenshots using the in-memory demo mode (no IMAP/SMTP required).
# Seeds the inbox with realistic test messages via [demo] enabled = true.
demo-screenshots: build
	@echo "==> Installing Playwright dependencies (first run only)..."
	cd scripts && npm install --silent && npx --yes playwright install chromium 2>/dev/null || true
	@echo "==> Starting demo server and capturing screenshots..."
	scripts/seed-demo.sh --screenshots
	@echo "==> Demo screenshots written to docs/screenshots/"

# Generate site/docs/*.md, which site/docs.html renders, from the repo's
# markdown — rewriting every link so it resolves against what the site actually
# serves. Never hand-edit site/docs/*.md; edit the source document and re-run.
site-docs:
	go run ./site/gen

clean:
	rm -f lilmail
