/**
 * lilmail — sync the repo's markdown into site/docs/
 *
 * site/docs.html renders plain markdown at runtime, so the site needs its own
 * copy of the files it serves (the repo root and docs/ are not published as-is).
 * This script is the only thing that should ever write site/docs/*.md — edit the
 * source documents, then run:
 *
 *   node scripts/sync-site-docs.mjs        # write the copies
 *   node scripts/sync-site-docs.mjs --check # fail if anything is stale (CI)
 *
 * Run via `make site-docs`.
 */

import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const OUT = resolve(ROOT, 'site', 'docs');

// source → published name. The published name must match the `path` entries in
// site/docs.html's DOCS table.
const MAP = [
  ['docs/GETTING-STARTED.md', 'getting-started.md'],
  ['docs/ARCHITECTURE.md',    'architecture.md'],
  ['docs/CONFIGURATION.md',   'configuration.md'],
  ['docs/API.md',             'api.md'],
  ['docs/SIGNING.md',         'signing.md'],
  ['docs/SCREENSHOTS.md',     'screenshots.md'],
  ['ROADMAP.md',              'roadmap.md'],
  ['CHANGELOG.md',            'changelog.md'],
];

const check = process.argv.includes('--check');
mkdirSync(OUT, { recursive: true });

let stale = 0;
for (const [src, name] of MAP) {
  const from = resolve(ROOT, src);
  const to = resolve(OUT, name);
  if (!existsSync(from)) {
    console.error(`  [!!] missing source: ${src}`);
    process.exitCode = 1;
    continue;
  }
  const body = readFileSync(from, 'utf8');
  const current = existsSync(to) ? readFileSync(to, 'utf8') : null;
  if (current === body) {
    console.log(`  [ok] ${name} — up to date`);
    continue;
  }
  stale++;
  if (check) {
    console.error(`  [stale] site/docs/${name} differs from ${src}`);
  } else {
    writeFileSync(to, body);
    console.log(`  [sync] ${name} ← ${src}`);
  }
}

if (check && stale) {
  console.error(`\n${stale} file(s) out of date — run: make site-docs`);
  process.exit(1);
}
console.log(check ? '\nsite/docs is in sync.' : `\nDone (${stale} updated).`);
