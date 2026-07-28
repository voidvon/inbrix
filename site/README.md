# site/ — the lilmail website

Two static pages, no build step, nothing fetched from a CDN — the same rules the
binary follows. Open `index.html` over any static server and it works offline.

```
site/
├── index.html          landing page (self-contained CSS + JS)
├── docs.html           documentation viewer (renders the markdown in docs/)
├── docs/*.md           the repo's markdown, GENERATED with links rewritten — see below
├── screenshots/*.png   app screenshots, light + `-dark` variants
├── assets/fonts/       vendored woff2 (SIL OFL 1.1 — see LICENSES.md)
└── assets/vendor/      marked (markdown), highlight.js (syntax), mermaid (diagrams)
```

## Editing the docs

`site/docs/*.md` are **generated**. Never edit them — edit the source document
(`docs/*.md`, `ROADMAP.md`, `CHANGELOG.md`) and regenerate:

```bash
make site-docs              # go run ./site/gen — regenerate site/docs/
make site-docs-check        # go test ./site/gen — CI gate
```

They are generated rather than copied because a copy is only ever checked for
freshness, and freshness is not the property a reader notices. This bundle ships
eight markdown files, the screenshots and two HTML pages — nothing else — so a
link that is perfectly valid in the repo (`../config.toml.example`, `TASKS.md`,
`README.md#contributing`, `docs/screenshots/README.md`) is a 404 here. The
generator re-points each one:

| Source link | Published as | Why |
|-------------|--------------|-----|
| `docs/API.md`, `API.md`, `../docs/API.md` | `api.md` | `docs.html` rule 5 turns a `<chapter>.md` href into in-page navigation |
| `#attachments` | `api.md#attachments` | the hash router reads a bare `#foo` as a *document slug* and navigates away |
| `CONFIGURATION.md#shared-object-storage-vulos_storage_broker_secret` | `configuration.md#shared-object-storage-vulosstoragebrokersecret` | `docs.html`'s `slugify()` deletes underscores and backticks and collapses hyphen runs; GitHub's does not |
| `TASKS.md`, `../config.toml.example` | `https://github.com/vul-os/lilmail/blob/main/…` | not in the bundle, so it leaves the bundle honestly |
| `docs/screenshots/hero.png` | `screenshots/hero.png` | `docs.html` rule 4 re-points it at `site/screenshots/` |

`site/gen/gen_test.go` fails on drift **and** on any link or heading anchor that
would not resolve, including links the generator sent to an absolute repo URL
that name a path this repo does not contain. It also pins the `docs.html`
behaviour the rewriting depends on, so changing the viewer cannot silently
invalidate the gate.

To add a document, add it to `pages` in `site/gen/main.go` **and** to the `DOCS`
table at the top of the script block in `docs.html`; the gate fails if the two
disagree.

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

Typography is Fraunces (variable — the landing drives its `opsz` and `SOFT` axes),
Hanken Grotesk for body copy, and IBM Plex Mono for anything the machine says.
The Fraunces files are subset to latin plus typographic punctuation with
`python3 -m fontTools.subset`; re-subset from the upstream variable font rather
than shipping the full 120 KB original.
