# site/ — the lilmail website

Two static pages, no build step, nothing fetched from a CDN — the same rules the
binary follows. Open `index.html` over any static server and it works offline.

```
site/
├── index.html          landing page (self-contained CSS + JS)
├── docs.html           documentation viewer (renders the markdown in docs/)
├── docs/*.md           copies of the repo's markdown — GENERATED, see below
├── screenshots/*.png   app screenshots, light + `-dark` variants
├── assets/fonts/       vendored woff2 (SIL OFL 1.1 — see LICENSES.md)
└── assets/vendor/      marked (markdown), highlight.js (syntax), mermaid (diagrams)
```

## Editing the docs

`site/docs/*.md` are copies. **Never edit them** — edit the source document
(`docs/*.md`, `ROADMAP.md`, `CHANGELOG.md`) and re-sync:

```bash
make site-docs                          # copy sources into site/docs/
node scripts/sync-site-docs.mjs --check # CI: fail if a copy is stale
```

To add a document, add it to `MAP` in `scripts/sync-site-docs.mjs` **and** to the
`DOCS` table at the top of the script block in `docs.html`.

## Screenshots

`screenshots/*.png` are the light-mode shots from `docs/screenshots/`;
`*-dark.png` are the same views captured with `colorScheme: 'dark'`, and both
pages swap between them with the theme. Regenerate with the demo server:

```bash
scripts/seed-demo.sh          # starts lilmail on :3099 with the in-memory inbox
# then drive Playwright against http://localhost:3099 in the colour scheme you want
```

## Vendored JavaScript

`assets/vendor/highlight.min.js` is highlight.js 11.x bundled with esbuild
(core + bash, ini/toml, json, go, yaml, xml, javascript, http, dockerfile, diff,
sql, css, plaintext) as an IIFE that exposes `window.hljs`. mermaid is large, so
`docs.html` loads it lazily — only for a document that actually contains a
```mermaid fence. Every bundle keeps its upstream licence next to it.

## Theme

The pages use the app's own design tokens (Paper `#FBF7F4`, Coral `#F2674E`,
Teal `#14B8A6`, Ink `#1F2A37`) from `assets/css/mail.css`. If those change in the
app, change them here too — the `:root` block at the top of each page is the only
place they appear.
