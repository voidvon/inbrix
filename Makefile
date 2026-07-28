.PHONY: build test vet fmt-check notices screenshots demo-screenshots site-docs site-docs-check check clean

# Build the lilmail binary
build:
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

# Fail if site/docs/*.md have drifted from their sources (the same gate CI runs).
site-docs-check:
	node scripts/sync-site-docs.mjs --check

# Everything CI runs, in the same order. Run this before opening a PR.
check: build fmt-check vet test site-docs-check

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

# Copy the repo's markdown into site/docs/, which site/docs.html renders.
# Never hand-edit site/docs/*.md — edit the source document and re-run this.
site-docs:
	node scripts/sync-site-docs.mjs

clean:
	rm -f lilmail
