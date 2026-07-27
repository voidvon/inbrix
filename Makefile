.PHONY: build test vet notices screenshots demo-screenshots site-docs clean

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

# Run build + vet + test
check: build vet test

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
