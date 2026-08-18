/**
 * site/ gate — renders index.html and docs.html in a real browser and fails on
 * the defects that static inspection cannot see.
 *
 * Why a browser: every bug this catches was invisible in the source. A
 * screenshot stretched to the wrong aspect ratio, both theme variants painted
 * on top of each other, a 1× image served to a 2× screen, an anchor pointing
 * at an id that no longer exists — the HTML reads fine in all four cases.
 *
 * Each check states what it measured, not just pass/fail, so a green run is
 * evidence rather than an assertion. Run:
 *
 *   node scripts/check-render.mjs                 # serves site/ itself
 *   node scripts/check-render.mjs --selftest      # prove the checks can fail
 */

import { chromium } from 'playwright';
import { createServer } from 'http';
import { readFile } from 'fs/promises';
import { existsSync } from 'fs';
import { resolve, dirname, extname, join, normalize } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const SITE = resolve(__dirname, '..', 'site');

const VIEWPORTS = [
  { w: 1600, h: 900, label: 'desktop-wide' },
  { w: 1440, h: 900, label: 'desktop' },
  { w: 1280, h: 800, label: 'laptop' },
  { w: 1024, h: 768, label: 'tablet-landscape' },
  { w: 768,  h: 1024, label: 'tablet' },
  { w: 430,  h: 932, label: 'phone-large' },
  { w: 390,  h: 844, label: 'phone' },
  { w: 360,  h: 780, label: 'phone-small' },
];

const MIME = {
  '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.js': 'text/javascript',
  '.mjs': 'text/javascript', '.json': 'application/json', '.md': 'text/markdown; charset=utf-8',
  '.png': 'image/png', '.jpg': 'image/jpeg', '.svg': 'image/svg+xml',
  '.woff2': 'font/woff2', '.txt': 'text/plain; charset=utf-8',
};

function serve(root) {
  return new Promise(ok => {
    const s = createServer(async (req, res) => {
      const rel = normalize(decodeURIComponent(req.url.split('?')[0])).replace(/^(\.\.[/\\])+/, '');
      let file = join(root, rel);
      if (!extname(file)) file = join(file, 'index.html');
      try {
        const body = await readFile(file);
        res.writeHead(200, { 'content-type': MIME[extname(file)] || 'application/octet-stream' });
        res.end(body);
      } catch {
        res.writeHead(404).end('not found');
      }
    });
    s.listen(0, '127.0.0.1', () => ok(s));
  });
}

// ---------------------------------------------------------------------------
// Findings
// ---------------------------------------------------------------------------
const findings = [];
const notes = [];
const fail = (check, where, detail) => findings.push({ check, where, detail });
const note = (s) => notes.push(s);

// ---------------------------------------------------------------------------
// Per-viewport, per-theme DOM checks
// ---------------------------------------------------------------------------
async function inspect(page, opts = {}) {
  return page.evaluate(async ({ isRouter, isDark }) => {
    const out = { overflow: null, imgs: [], smallText: [], deadAnchors: [], hiddenPairs: [], themedShots: [] };

    // 1 · page-level horizontal overflow.
    // scrollWidth alone is not enough: html{overflow-x:clip} makes an
    // overflowing child report the clipped width, so measure the children too.
    const de = document.documentElement;
    const bleed = [];
    document.querySelectorAll('body *').forEach(el => {
      const cs = getComputedStyle(el);
      if (cs.position === 'fixed') return;
      const r = el.getBoundingClientRect();
      if (r.width <= 0) return;
      // Deliberate designs that overflow their own scroll container are fine;
      // only flag elements that push the PAGE sideways.
      let p = el.parentElement, contained = false;
      while (p && p !== document.body) {
        const pcs = getComputedStyle(p);
        if (/auto|scroll|hidden|clip/.test(pcs.overflowX)) { contained = true; break; }
        p = p.parentElement;
      }
      if (contained) return;
      // Something parked wholly off-screen left (a skip link at -9999px) adds
      // no horizontal scroll — only content crossing the RIGHT edge does, plus
      // anything straddling the left edge and therefore partly unreachable.
      if (r.right <= 0) return;
      if (r.right > window.innerWidth + 1 || r.left < -1) {
        bleed.push({ tag: el.tagName, cls: String(el.className).slice(0, 50),
                     left: Math.round(r.left), right: Math.round(r.right) });
      }
    });
    out.overflow = { docW: de.scrollWidth, winW: window.innerWidth, bleed: bleed.slice(0, 6) };

    // 2 · every visible raster image at its true aspect ratio, and — on a 2×
    // context — actually resolving to a 2× source when one is offered.
    // naturalWidth is DENSITY-CORRECTED: a candidate picked via a "2x"
    // descriptor reports half its real pixel width, so comparing it against
    // the device pixels needed double-counts the ratio and every retina image
    // looks 2x too soft. Re-load currentSrc bare to get its true pixel size.
    const trueSize = async (url) => await new Promise(res => {
      const probe = new Image();
      probe.onload = () => res([probe.naturalWidth, probe.naturalHeight]);
      probe.onerror = () => res([0, 0]);
      probe.src = url;
    });
    for (const im of document.querySelectorAll('img')) {
      const r = im.getBoundingClientRect();
      if (r.width < 4 || r.height < 4) continue;
      if (!im.naturalWidth || !im.naturalHeight) continue;
      const rendered = r.width / r.height;
      const natural = im.naturalWidth / im.naturalHeight;
      const url = im.currentSrc || im.src;
      const [px, py] = await trueSize(url);
      // Only `fill` (the default) stretches pixels. cover/contain/none crop or
      // letterbox instead, so a box ratio that differs from the source ratio is
      // the intended design, not a defect — flagging it made every deliberately
      // cropped thumbnail look like a bug.
      const fit = getComputedStyle(im).objectFit;
      out.imgs.push({
        file: url.split('/').pop(),
        css: `${Math.round(r.width)}x${Math.round(r.height)}`,
        nat: `${im.naturalWidth}x${im.naturalHeight}`,
        realPx: `${px}x${py}`,
        skewPct: fit === 'fill' ? +(Math.abs(rendered / natural - 1) * 100).toFixed(1) : 0,
        objectFit: fit,
        offersSrcset: !!(im.srcset || im.closest('picture')?.querySelector('source[srcset]')),
        // >1 means the display asks for more pixels than the file has — the
        // exact cause of a soft-looking screenshot.
        upscale: px ? +(r.width * devicePixelRatio / px).toFixed(2) : 0,
      });
    }

    // 3 · legibility floor for real body copy.
    const TEXTY = new Set(['P', 'LI', 'DD', 'DT', 'TD', 'TH', 'SPAN', 'B', 'EM', 'STRONG', 'A', 'CODE']);
    // `body *`, not `main *, footer *`. A landing with no <main> element — two
    // in this suite — made this scan almost nothing, so the floor passed
    // vacuously: a planted 9px paragraph went straight through the self-test.
    // Found by the sirboard agent's mutation testing, not by reading the code.
    document.querySelectorAll('body *').forEach(el => {
      if (!TEXTY.has(el.tagName)) return;
      const own = [...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim().length > 12);
      if (!own) return;
      const r = el.getBoundingClientRect();
      if (r.width < 4 || r.height < 4) return;
      const cs = getComputedStyle(el);
      if (cs.visibility === 'hidden' || cs.textTransform === 'uppercase') return;  // eyebrows/labels
      const size = parseFloat(cs.fontSize);
      if (size < 12) {
        out.smallText.push({ tag: el.tagName, size,
                             text: el.textContent.trim().slice(0, 40) });
      }
    });

    // 4 · every same-page fragment link resolves.
    // Skipped on the docs viewer: it is a hash ROUTER, so "#api" and
    // "#getting-started/first-run" name a chapter and a section within it,
    // not ids in the current document. checkDocsRoutes() validates those
    // against the real route table instead.
    if (!isRouter) {
      document.querySelectorAll('a[href^="#"]').forEach(a => {
        const id = a.getAttribute('href').slice(1);
        if (!id) return;
        if (!document.getElementById(id) && !document.querySelector(`[name="${CSS.escape(id)}"]`)) {
          out.deadAnchors.push({ href: '#' + id, text: a.textContent.trim().slice(0, 30) });
        }
      });
    }

    // 5 · theme-aware screenshots, both mechanisms, decided from the DOM
    // relationship rather than the filename. Filename sniffing does not work:
    // in the ".only-light / .only-dark" model the LIGHT capture is the
    // unmarked one (hero.png beside hero-dark.png), so "no marker in the name"
    // means "the light variant", not "theme-neutral art".
    //
    //   a) paired elements — exactly one of .only-light / .only-dark may paint,
    //      and it must be the one matching the active theme;
    //   b) swapped src — an <img data-light data-dark> must be showing the
    //      attribute for the active theme.
    const painted = e => {
      const r = e.getBoundingClientRect();
      return r.width > 4 && r.height > 4 && getComputedStyle(e).visibility !== 'hidden';
    };
    out.hiddenPairs.push({
      scope: 'document',
      light: [...document.querySelectorAll('.only-light')].filter(painted).length,
      dark:  [...document.querySelectorAll('.only-dark')].filter(painted).length,
    });

    document.querySelectorAll('img[data-light][data-dark]').forEach(im => {
      if (!painted(im)) return;
      const want = new URL(im.getAttribute(isDark ? 'data-dark' : 'data-light'), location.href).href;
      const got  = im.currentSrc || im.src || '';
      if (got && got !== want) {
        out.themedShots.push({ file: got.split('/').pop(), wanted: want.split('/').pop() });
      }
    });

    //   c) <picture> with a prefers-color-scheme <source> — the third mechanism
    //      in this suite, and the one my earlier check was blind to: it reported
    //      "page does not use that mechanism" on a page whose screenshots were
    //      in fact theme-swapped, so the swap went unverified entirely.
    document.querySelectorAll('picture > source[media*="prefers-color-scheme"]').forEach(src => {
      const im = src.parentElement.querySelector('img');
      if (!im || !painted(im)) return;
      const forDark = /dark/i.test(src.getAttribute('media') || '');
      // The source applies in its own scheme; the <img src> is the fallback.
      const wantRaw = (isDark === forDark) ? (src.getAttribute('srcset') || '').split(/\s|,/)[0]
                                           : im.getAttribute('src');
      if (!wantRaw) return;
      const want = new URL(wantRaw, location.href).href;
      const got  = im.currentSrc || im.src || '';
      if (got && got !== want) {
        out.themedShots.push({ file: got.split('/').pop(), wanted: want.split('/').pop() });
      }
    });

    return out;
  }, { isRouter: !!opts.isRouter, isDark: !!opts.isDark });
}

async function checkPage(browser, base, path, theme, vp) {
  const ctx = await browser.newContext({
    viewport: { width: vp.w, height: vp.h }, deviceScaleFactor: 2, colorScheme: theme,
  });
  const page = await ctx.newPage();
  const where = `${path} ${vp.label}(${vp.w}) ${theme}`;

  const console404 = [];
  page.on('response', r => { if (r.status() >= 400) console404.push(`${r.status()} ${r.url()}`); });
  page.on('pageerror', e => fail('js-error', where, e.message));

  // Every subresource must come from the same origin as the page.
  //
  // The site says, in several places, that it uses no CDN and that an
  // air-gapped box is a supported deployment. That was true and unenforced:
  // nothing here would have noticed a <script src="https://cdn…"> or a
  // Google Fonts @import, and both are the kind of thing that arrives in a
  // hurry and stays. Fonts, highlight.js and marked are all vendored under
  // site/assets/vendor/ precisely so this holds — so assert it.
  //
  // Scoped to real subresource loads: the page's own document, data: and
  // blob: URLs, and about:blank are not fetches off the box.
  const offOrigin = new Set();
  page.on('request', r => {
    const url = r.url();
    if (r.resourceType() === 'document') return;
    if (/^(data|blob|about|javascript):/.test(url)) return;
    if (url.startsWith(base)) return;
    offOrigin.add(`${r.resourceType()} ${url}`);
  });

  await page.goto(`${base}/${path}`, { waitUntil: 'networkidle' });
  // reveal-on-scroll gates most of the page; force it so nothing is measured
  // while still at opacity 0 and translated.
  // Reveal-on-scroll gates most of these pages and the class name differs per
  // repo (.rv here, .reveal there). Force every variant: a fast programmatic
  // scroll does not reliably fire an IntersectionObserver, so anything still
  // hidden would be measured at opacity 0 and mid-transform.
  await page.evaluate(() =>
    document.querySelectorAll('.rv, .reveal, [data-reveal]').forEach(e => e.classList.add('in', 'is-in')));
  await page.evaluate(async () => {
    const H = document.body.scrollHeight;
    for (let y = 0; y < H; y += 400) { window.scrollTo(0, y); await new Promise(r => setTimeout(r, 30)); }
    window.scrollTo(0, 0);
  });
  await page.waitForTimeout(500);

  const r = await inspect(page, { isRouter: path === 'docs.html', isDark: theme === 'dark' });

  if (r.overflow.docW > r.overflow.winW + 1) {
    fail('h-overflow', where, `document is ${r.overflow.docW}px wide in a ${r.overflow.winW}px viewport`);
  }
  if (r.overflow.bleed.length) {
    fail('h-overflow', where,
      'elements pushing past the viewport: ' + r.overflow.bleed
        .map(b => `${b.tag}.${b.cls} [${b.left}→${b.right}]`).join('; '));
  }
  r.imgs.forEach(i => {
    if (i.skewPct > 1.5) {
      fail('img-distorted', where,
        `${i.file} drawn ${i.css} from a ${i.nat} source — ${i.skewPct}% off its true aspect ratio`);
    }
    // Vector art has no resolution to be short of — an SVG drawn at any size is
    // exactly as sharp. Only raster sources can be upscaled into softness.
    const isVector = /\.svgx?(\?|#|$)/i.test(i.file);
    if (!isVector && i.offersSrcset && i.upscale > 1.15) {
      fail('img-soft', where,
        `${i.file} drawn ${i.css} at dpr2 from a ${i.realPx} file — upscaled ${i.upscale}×`);
    }
  });
  r.smallText.forEach(t =>
    fail('text-too-small', where, `<${t.tag.toLowerCase()}> at ${t.size}px: “${t.text}”`));
  r.deadAnchors.forEach(a =>
    fail('dead-anchor', where, `${a.href} (“${a.text}”) matches no element on the page`));
  r.themedShots.forEach(s =>
    fail('screenshot-wrong-theme', where,
      `showing ${s.file} in ${theme} mode; the ${theme} variant is ${s.wanted}`));
  r.hiddenPairs.forEach(p => {
    const live = theme === 'dark' ? p.dark : p.light;
    const dead = theme === 'dark' ? p.light : p.dark;
    if (dead > 0) fail('both-themes-visible', where,
      `${p.scope}: ${dead} off-theme image(s) painted alongside ${live} on-theme`);
    if (live === 0 && dead === 0) return;   // page does not use the paired model
    if (live === 0) fail('no-image-visible', where, `${p.scope}: nothing painted for the ${theme} theme`);
  });
  console404.forEach(u => fail('http-error', where, u));
  offOrigin.forEach(u => fail('off-origin', where,
    `${u} — the site must be self-contained; vendor it under site/assets/vendor/`));

  await ctx.close();
  return r;
}

// ---------------------------------------------------------------------------
// Cross-page anchors: index.html ↔ docs.html
// ---------------------------------------------------------------------------
async function checkCrossPageAnchors(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();

  const idsOf = async (p) => {
    await page.goto(`${base}/${p}`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(400);
    return new Set(await page.evaluate(() => [...document.querySelectorAll('[id]')].map(e => e.id)));
  };
  const linksOf = async (p) => {
    await page.goto(`${base}/${p}`, { waitUntil: 'networkidle' });
    return page.evaluate(() => [...document.querySelectorAll('a[href*=".html#"]')]
      .map(a => ({ href: a.getAttribute('href'), text: a.textContent.trim().slice(0, 30) })));
  };

  const indexIds = await idsOf('index.html');
  const docsIds  = await idsOf('docs.html');
  const ids = { 'index.html': indexIds, 'docs.html': docsIds };

  for (const from of ['index.html', 'docs.html']) {
    for (const l of await linksOf(from)) {
      const [file, frag] = l.href.replace(/^\.\//, '').split('#');
      if (!ids[file]) continue;                       // external or unknown target
      if (file === 'docs.html') continue;             // hash-routed: "#api" is a doc slug, not an id
      if (!ids[file].has(frag)) {
        fail('dead-anchor', `${from} → ${l.href}`,
          `“${l.text}” points at #${frag}, which ${file} does not define`);
      }
    }
  }
  await ctx.close();
}

// docs.html routes on the hash, so its own nav links must name a known slug.
async function checkDocsRoutes(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  await page.goto(`${base}/docs.html`, { waitUntil: 'networkidle' });
  const slugs = new Set(await page.evaluate(() => DOCS.map(d => d.slug)));
  const targets = await page.evaluate(() =>
    [...document.querySelectorAll('a[href*="docs.html#"]')].map(a => a.getAttribute('href')));
  const idxCtx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const idx = await idxCtx.newPage();
  await idx.goto(`${base}/index.html`, { waitUntil: 'networkidle' });
  const fromIndex = await idx.evaluate(() =>
    [...document.querySelectorAll('a[href*="docs.html#"]')].map(a => a.getAttribute('href')));

  for (const href of [...targets, ...fromIndex]) {
    const slug = href.split('#')[1].split('/')[0];
    if (!slugs.has(slug)) {
      fail('dead-doc-route', href, `“${slug}” is not one of the published doc slugs (${[...slugs].join(', ')})`);
    }
  }
  // Every chapter must actually render.
  for (const slug of slugs) {
    await page.goto(`${base}/docs.html#${slug}`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(600);
    const r = await page.evaluate(() => {
      const c = document.getElementById('content');
      return { err: !!c.querySelector('.err'), skeleton: !!c.querySelector('.skeleton'),
               blocks: c.querySelectorAll('pre > code').length, chars: c.textContent.trim().length,
               highlighted: c.querySelectorAll('code.hljs').length,
               // Blocks whose language chip reads "text". NOT a defect and not
               // gated — some content genuinely is plain text: SIGNING.md's
               // SigV4 pseudocode and canonical-request byte layouts, and
               // ARCHITECTURE.md's directory tree. Reported because a sudden
               // jump means a real fence lost its language, not because the
               // number should be zero. It was previously called "unlabelled",
               // which read like a defect count and sent someone hunting for a
               // highlighting bug that did not exist.
               plainText: [...c.querySelectorAll('.code .lang')].filter(l => l.textContent === 'text').length };
    });
    if (r.err || r.skeleton || r.chars < 400) {
      fail('doc-not-rendered', `docs.html#${slug}`,
        `err=${r.err} skeleton=${r.skeleton} textLength=${r.chars}`);
    } else {
      note(`docs#${slug}: ${r.chars} chars, ${r.blocks} code blocks, ${r.highlighted} highlighted, ${r.plainText} plain-text`);
      if (r.blocks && r.highlighted < r.blocks) {
        fail('code-not-highlighted', `docs.html#${slug}`,
          `${r.blocks - r.highlighted} of ${r.blocks} code block(s) carry no hljs markup`);
      }
    }
  }
  await ctx.close();
  await idxCtx.close();
}

// ---------------------------------------------------------------------------
// Per-chapter docs checks.
//
// Everything else here measures docs.html on whichever chapter the router
// happens to open first, which means seven of the eight are never looked at.
// A wide table in API.md, a heading the outline builder drops, an off-origin
// image pasted into one chapter — none of it is reachable from the default
// route, so none of it was being checked. This visits every chapter and
// asserts the three things that are per-chapter rather than per-page:
//
//   - the on-page outline is complete and live: every h2/h3 carries an id and
//     is reachable from #rail, and no outline link points at nothing. An
//     outline that silently omits half a chapter still looks fine;
//   - nothing overflows sideways at the narrow end, where the shell collapses
//     to one column and a wide <pre> or table has nowhere to go;
//   - no chapter pulls a subresource off the box.
// ---------------------------------------------------------------------------
async function checkDocsOutline(browser, base, mutate) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  const offOrigin = new Set();
  page.on('request', r => {
    const url = r.url();
    if (r.resourceType() === 'document') return;
    if (/^(data|blob|about|javascript):/.test(url)) return;
    if (url.startsWith(base)) return;
    offOrigin.add(`${r.resourceType()} ${url}`);
  });

  await page.goto(`${base}/docs.html`, { waitUntil: 'networkidle' });
  const slugs = await page.evaluate(() => DOCS.map(d => d.slug));
  const problems = [];

  for (const slug of slugs) {
    await page.goto(`${base}/docs.html#${slug}`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(600);
    if (mutate) { await page.evaluate(mutate); await page.waitForTimeout(120); }

    const r = await page.evaluate(() => {
      const c = document.getElementById('content');
      const heads = [...c.querySelectorAll('h2,h3')];
      const outline = [...document.querySelectorAll('#rail a')].map(a => a.getAttribute('href') || '');
      const seen = {};
      document.querySelectorAll('[id]').forEach(e => { seen[e.id] = (seen[e.id] || 0) + 1; });
      return {
        noId: heads.filter(h => !h.id).map(h => h.textContent.trim().slice(0, 50)),
        missing: heads.filter(h => h.id && !outline.includes('#' + h.id))
                      .map(h => `${h.tagName} “${h.textContent.trim().slice(0, 50)}”`),
        dead: outline.filter(h => h.startsWith('#') && !document.getElementById(h.slice(1))),
        dupIds: Object.keys(seen).filter(k => seen[k] > 1),
        heads: heads.length, outline: outline.length,
      };
    });
    if (r.noId.length) problems.push(`docs#${slug}: ${r.noId.length} heading(s) carry no id: ${r.noId.join(' | ')}`);
    if (r.missing.length) problems.push(`docs#${slug}: ${r.missing.length} of ${r.heads} heading(s) missing from the on-page outline: ${r.missing.join(' | ')}`);
    if (r.dead.length) problems.push(`docs#${slug}: outline link(s) pointing at no element: ${r.dead.join(', ')}`);
    if (r.dupIds.length) problems.push(`docs#${slug}: duplicate id(s): ${r.dupIds.join(', ')}`);

    // Narrow end: the shell is one column and nothing may push the page sideways.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.waitForTimeout(160);
    if (mutate) await page.evaluate(mutate);
    // Do NOT gate this on `documentElement.scrollWidth - clientWidth` first.
    // That number is blind whenever <body> carries a non-visible overflow-x:
    // measured on basin's docs page, injecting a 2400px child left
    // documentElement.scrollWidth at 390 while body.scrollWidth read 2400, so
    // an early return there would have skipped the scan entirely and printed a
    // clean pass over content genuinely cut off at the viewport edge. (clip on
    // <html> alone does not do this — inbrix's own pages report 2400 — but the
    // check must not depend on which element happens to carry the property.)
    // The geometric scan below needs no such gate: it reads the boxes directly.
    const over = await page.evaluate(() => {
      const de = document.documentElement;
      const n = Math.max(de.scrollWidth, document.body.scrollWidth) - de.clientWidth;
      let worst = null;
      for (const el of document.querySelectorAll('body *')) {
        const cs = getComputedStyle(el);
        if (cs.position === 'fixed') continue;
        const b = el.getBoundingClientRect();
        if (b.width <= 0 || b.right <= 0) continue;
        let p = el.parentElement, inside = false;
        while (p && p !== document.body) {
          if (/auto|scroll|hidden|clip/.test(getComputedStyle(p).overflowX)) { inside = true; break; }
          p = p.parentElement;
        }
        if (inside) continue;
        if (b.right > window.innerWidth + 1 && (!worst || b.right > worst.right))
          worst = { tag: el.tagName, cls: String(el.className).slice(0, 50), right: Math.round(b.right) };
      }
      return worst ? { n, worst } : null;
    });
    if (over) problems.push(`docs#${slug} at 390px: ${over.n}px of horizontal overflow` +
      (over.worst ? ` — widest offender ${over.worst.tag}.${over.worst.cls} reaching ${over.worst.right}px` : ''));
    await page.setViewportSize({ width: 1440, height: 900 });
  }

  offOrigin.forEach(u => problems.push(`docs chapters: ${u} — the site must be self-contained`));
  await ctx.close();
  return { problems, chapters: slugs.length };
}

// ---------------------------------------------------------------------------
// App-shell stickiness — the landing is laid out AS the mail client, so its
// folder rail and message list are position:sticky panes beside a long reader.
// Two ways that silently breaks, both of which shipped and neither of which is
// visible in the source:
//
//   a) an ancestor becomes a scroll container and captures the sticky. A
//      non-`visible` overflow-x forces overflow-y to `auto`, so a single
//      `body{overflow-x:hidden}` — added to stop sideways bleed — made <body>
//      a scroller and both panes scrolled away with the page. Measured
//      rail.top: 68 → -1132 → -3932. The cure is `overflow-x:clip`.
//
//   b) the panes release too late and paint over what follows. Chrome clamps a
//      sticky GRID ITEM to the bottom of the grid CONTAINER rather than to its
//      own grid area, so putting the status bar in as a second grid row moved
//      the release point 30px down and the rail covered the bar at the foot of
//      the page (rail 60..900 against a bar at 870..900).
//
// Both are measured rather than asserted structurally: any future layout that
// keeps the panes pinned and releases them cleanly passes, however it is built.
// ---------------------------------------------------------------------------
const shellGeometry = (page) => page.evaluate(() => {
  const box = s => { const e = document.querySelector(s); if (!e) return null;
    const r = e.getBoundingClientRect(); return { top: r.top, bottom: r.bottom }; };
  const stick = s => { const e = document.querySelector(s); if (!e) return null;
    const cs = getComputedStyle(e);
    return cs.position === 'sticky' ? parseFloat(cs.top) : null; };
  return {
    y: window.scrollY,
    maxScroll: document.documentElement.scrollHeight - window.innerHeight,
    rail: box('.rail'), mlist: box('.mlist'), app: box('.app'), status: box('footer.statusbar'),
    stickTop: { rail: stick('.rail'), mlist: stick('.mlist') },
  };
});

async function checkAppShell(browser, base, mutate) {
  const ctx = await browser.newContext({ viewport: { width: 1500, height: 900 } });
  const page = await ctx.newPage();
  await page.goto(`${base}/index.html`, { waitUntil: 'networkidle' });
  await page.evaluate(() => document.querySelectorAll('.rv,.reveal').forEach(e => e.classList.add('in')));
  if (mutate) await page.evaluate(mutate);
  await page.waitForTimeout(350);

  const scrollTo = async (y) => {
    await page.evaluate(yy => window.scrollTo({
      top: yy === 'max' ? document.documentElement.scrollHeight - window.innerHeight : yy,
      behavior: 'instant',
    }), y);
    await page.waitForTimeout(220);
    return shellGeometry(page);
  };

  const problems = [];
  const probe = await shellGeometry(page);
  // A page too short to scroll would pass every assertion below without
  // testing anything. Say so rather than banking a hollow pass.
  if (probe.maxScroll < 3500 || !probe.rail || probe.stickTop.rail === null) {
    await ctx.close();
    return { problems, applicable: false };
  }

  const mid = await scrollTo(3000);
  for (const pane of ['rail', 'mlist']) {
    const m = mid[pane], want = mid.stickTop[pane];
    if (!m || want === null) continue;
    if (Math.abs(m.top - want) > 2) {
      problems.push(`.${pane} is not pinned at scrollY=${Math.round(mid.y)}: it declares ` +
        `top:${want}px but sits at ${Math.round(m.top)}px — an ancestor is a scroll ` +
        `container and has captured the sticky (body{overflow-x:hidden} does exactly this; use clip)`);
    }
  }

  const end = await scrollTo('max');
  for (const pane of ['rail', 'mlist']) {
    const m = end[pane];
    if (!m) continue;
    if (end.app && m.bottom > end.app.bottom + 2) {
      problems.push(`.${pane} does not release with its container: at the bottom of the page it ` +
        `ends at ${Math.round(m.bottom)}px, past .app's ${Math.round(end.app.bottom)}px`);
    }
    if (end.status && m.bottom > end.status.top + 1) {
      problems.push(`.${pane} paints over the status bar: pane ends at ${Math.round(m.bottom)}px, ` +
        `the bar starts at ${Math.round(end.status.top)}px (a sticky grid item clamps to the grid ` +
        `CONTAINER, so the bar must not be a row inside .app)`);
    }
  }
  await ctx.close();
  return { problems, applicable: true };
}

// ---------------------------------------------------------------------------
// Self-test: break each invariant on purpose and demand the check notices.
// A gate that has quietly stopped failing looks exactly like one that works.
//
// The mutations pick their targets from the page rather than naming selectors,
// so this file is portable across the suite's landing pages, which share no
// class names. A case whose mechanism the page does not use reports "n/a"
// rather than passing silently — an inapplicable check must not read as a
// working one.
// ---------------------------------------------------------------------------
async function selftest(browser, base) {
  const cases = ['img-distorted', 'both-themes-visible', 'text-too-small',
                 'h-overflow', 'screenshot-wrong-theme', 'off-origin'];
  let allCaught = true;
  for (const name of cases) {
    const ctx = await browser.newContext({
      viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, colorScheme: 'dark',
    });
    const page = await ctx.newPage();

    // The off-origin check is a request listener rather than a DOM
    // measurement, so this case cannot go through inspect() like the others.
    // Watch the same way the real run does, then assert on what was seen.
    const sawOffOrigin = new Set();
    if (name === 'off-origin') {
      page.on('request', r => {
        const url = r.url();
        if (r.resourceType() === 'document') return;
        if (/^(data|blob|about|javascript):/.test(url)) return;
        if (url.startsWith(base)) return;
        sawOffOrigin.add(url);
      });
    }

    await page.goto(`${base}/index.html`, { waitUntil: 'networkidle' });
    await page.evaluate(() => document.querySelectorAll('.rv,.reveal').forEach(e => e.classList.add('in', 'is-in')));
    await page.waitForTimeout(500);

    const applicable = await page.evaluate((which) => {
      const vis = e => { const r = e.getBoundingClientRect(); return r.width > 40 && r.height > 40; };
      if (which === 'img-distorted') {
        // Must be a DECODED image: the check skips anything with no intrinsic
        // size, so mutating a lazy image that has not loaded yet produces a
        // false MISSED rather than a real one.
        const im = [...document.querySelectorAll('img')]
          .filter(e => vis(e) && e.naturalWidth > 0 && e.naturalHeight > 0)
          .sort((a, b) => b.getBoundingClientRect().width - a.getBoundingClientRect().width)[0];
        if (!im) return false;
        // Squash it to a wrong ratio without changing its width. object-fit
        // must be forced to `fill` too: the check deliberately ignores skew
        // under cover/contain (those crop rather than stretch), so on a page
        // whose shots are cropped the mutation would be undetectable BY DESIGN
        // and this case would report a false MISSED.
        im.style.setProperty('object-fit', 'fill', 'important');
        im.style.setProperty('height', Math.round(im.getBoundingClientRect().width * 2) + 'px', 'important');
        im.style.setProperty('aspect-ratio', 'auto', 'important');
        return true;
      }
      if (which === 'text-too-small') {
        const p = [...document.querySelectorAll('main p, .pane p, section p, body p')]
          .find(e => e.textContent.trim().length > 40 && vis(e));
        if (!p) return false;
        p.style.setProperty('font-size', '9px', 'important');
        p.style.setProperty('text-transform', 'none', 'important');
        return true;
      }
      if (which === 'h-overflow') {
        // Straight onto <body>. The scan covers `body *`, and putting the
        // oversized element inside <main> is wrong wherever main is itself a
        // scroll container (envoir's reading pane): the check correctly treats
        // anything inside an overflow container as contained, so the mutation
        // could never be seen and the case reported a false MISSED.
        const host = document.body;
        const d = document.createElement('div');
        d.style.cssText = 'width:3000px;height:20px;background:red';
        host.appendChild(d);
        return true;
      }
      if (which === 'both-themes-visible') {
        if (!document.querySelector('.only-light') || !document.querySelector('.only-dark')) return false;
        document.querySelectorAll('.only-light,.only-dark')
          .forEach(e => e.style.setProperty('display', 'block', 'important'));
        return true;
      }
      if (which === 'screenshot-wrong-theme') {
        const swap = document.querySelectorAll('img[data-light][data-dark]');
        const pair = document.querySelector('.only-light') && document.querySelector('.only-dark');
        const pics = document.querySelectorAll('picture > source[media*="prefers-color-scheme"]');
        if (!swap.length && !pair && !pics.length) return false;
        // <picture>: make the dark <source> never match, so the img falls back
        // to its light src while the page is dark. The <source> must STAY in the
        // DOM — removing it deletes the very element the check looks for, which
        // is why an earlier version of this mutation reported a false MISSED.
        pics.forEach(sc => {
          const im = sc.parentElement.querySelector('img');
          if (!im) return;
          // Must still CONTAIN "prefers-color-scheme" or the check's own
          // selector stops matching it and the case reports a false MISSED —
          // the mutation would have deleted its own subject.
          sc.setAttribute('media', '(prefers-color-scheme: dark) and (min-width: 99999px)');
          // force re-selection
          const clone = im.cloneNode(true);
          im.replaceWith(clone);
        });
        swap.forEach(im => { im.removeAttribute('srcset'); im.src = im.getAttribute('data-light'); });
        if (pair) {
          document.querySelectorAll('.only-dark').forEach(e => e.style.setProperty('display', 'none', 'important'));
          document.querySelectorAll('.only-light').forEach(e => e.style.setProperty('display', 'block', 'important'));
        }
        return true;
      }
      if (which === 'off-origin') {
        // A pixel to a host that certainly is not this test server. It never
        // has to load — the request leaving is the defect.
        const im = document.createElement('img');
        im.src = 'https://cdn.example.invalid/pixel.png';
        im.alt = '';
        document.body.appendChild(im);
        return true;
      }
      return false;
    }, name);

    if (!applicable) {
      console.log(`  n/a      ${name} — this page does not use that mechanism`);
      await ctx.close();
      continue;
    }

    await page.waitForTimeout(400);
    const r = await inspect(page, { isDark: true });
    const caught =
      (name === 'img-distorted'       && r.imgs.some(i => i.skewPct > 1.5)) ||
      (name === 'both-themes-visible' && r.hiddenPairs.some(p => p.light > 0 && p.dark > 0)) ||
      (name === 'text-too-small'      && r.smallText.length > 0) ||
      (name === 'h-overflow'          && (r.overflow.docW > r.overflow.winW + 1 || r.overflow.bleed.length > 0)) ||
      (name === 'screenshot-wrong-theme' &&
         (r.themedShots.length > 0 || r.hiddenPairs.some(p => p.light > 0 && p.dark === 0))) ||
      (name === 'off-origin'          && sawOffOrigin.size > 0);
    console.log(`  ${caught ? 'caught  ' : 'MISSED  '} ${name}`);
    if (!caught) allCaught = false;
    await ctx.close();
  }

  // The two app-shell mutations reproduce the exact defects that shipped, so
  // this pair is a regression test as much as a proof the check discriminates.
  const shellCases = [
    ['sticky-captured', () => {
      // The original bug, verbatim: a non-visible overflow-x forces overflow-y
      // to auto, <body> becomes a scroll container, and the panes stick to it.
      document.body.style.setProperty('overflow-x', 'hidden', 'important');
    }],
    ['sticky-overlaps-statusbar', () => {
      // Fold the status bar back in as a grid row of .app — the release point
      // then follows the grid CONTAINER and the rail covers the bar.
      const app = document.querySelector('.app'), bar = document.querySelector('footer.statusbar');
      if (!app || !bar) return;
      bar.style.gridColumn = '1/-1';
      app.appendChild(bar);
    }],
  ];
  for (const [name, mutate] of shellCases) {
    const r = await checkAppShell(browser, base, mutate);
    if (!r.applicable) { console.log(`  n/a      ${name} — no sticky shell on this page`); continue; }
    const caught = r.problems.length > 0;
    console.log(`  ${caught ? 'caught  ' : 'MISSED  '} ${name}`);
    if (!caught) allCaught = false;
  }

  // The per-chapter docs checks, mutated the same way.
  const docsCases = [
    ['docs-outline-gap', () => {
      // Drop one outline entry. A heading you cannot reach from the outline is
      // the failure this exists for, and it is invisible unless counted.
      const a = document.querySelector('#rail a');
      if (a) a.remove();
    }],
    ['docs-chapter-overflow', () => {
      // A wide block in the article, which is where a real one arrives — an
      // un-wrapped table or a long <pre>.
      if (document.getElementById('mutant-wide')) return;
      const d = document.createElement('div');
      d.id = 'mutant-wide';
      d.style.cssText = 'width:3000px;height:12px;background:red';
      document.getElementById('content').appendChild(d);
    }],
  ];
  for (const [name, mutate] of docsCases) {
    const r = await checkDocsOutline(browser, base, mutate);
    const caught = r.problems.length > 0;
    console.log(`  ${caught ? 'caught  ' : 'MISSED  '} ${name}`);
    if (!caught) allCaught = false;
  }
  return allCaught;
}

// ---------------------------------------------------------------------------
async function main() {
  if (!existsSync(join(SITE, 'index.html'))) {
    console.error(`check-render: no site/index.html under ${SITE}`);
    process.exit(2);
  }
  const server = await serve(SITE);
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch({ headless: true });

  try {
    if (process.argv.includes('--selftest')) {
      console.log('check-render self-test — each invariant is broken on purpose:\n');
      const ok = await selftest(browser, base);
      console.log(ok ? '\nSELF-TEST PASS — every check discriminates.'
                     : '\nSELF-TEST FAIL — a check did not notice its own defect.');
      process.exitCode = ok ? 0 : 1;
      return;
    }

    let sampled = 0;
    for (const vp of VIEWPORTS) {
      for (const theme of ['light', 'dark']) {
        for (const path of ['index.html', 'docs.html']) {
          const r = await checkPage(browser, base, path, theme, vp);
          sampled += r.imgs.length;
        }
      }
    }
    await checkCrossPageAnchors(browser, base);
    await checkDocsRoutes(browser, base);

    const outline = await checkDocsOutline(browser, base);
    outline.problems.forEach(p => fail('docs-chapter', 'docs.html', p));
    if (!outline.problems.length)
      note(`docs outline: ${outline.chapters} chapters, every heading in the on-page outline, ` +
           `no dead outline link, nothing overflowing at 390px, no off-origin request`);

    const shell = await checkAppShell(browser, base);
    if (!shell.applicable) {
      note('app-shell: index.html has no sticky rail to test at 1500×900');
    } else {
      shell.problems.forEach(p => fail('sticky-shell', 'index.html 1500×900', p));
      if (!shell.problems.length) note('app-shell: rail and message list pin under the top bar and release cleanly at the end of .app');
    }

    console.log(`\nchecked ${VIEWPORTS.length} viewports × 2 themes × 2 pages; ` +
                `${sampled} rendered images measured\n`);
    notes.forEach(n => console.log('  · ' + n));

    if (findings.length) {
      console.error(`\ncheck-render: ${findings.length} finding(s)\n`);
      const byCheck = {};
      findings.forEach(f => (byCheck[f.check] ||= []).push(f));
      for (const [check, list] of Object.entries(byCheck)) {
        console.error(`  ${check} (${list.length})`);
        // Collapse the viewport dimension: the same defect at eight widths is
        // one defect, and printing it eight times buries the others.
        const seen = new Set();
        list.forEach(f => {
          const key = f.detail;
          if (seen.has(key)) return;
          seen.add(key);
          console.error(`    ${f.where}\n      ${f.detail}`);
        });
      }
      process.exitCode = 1;
    } else {
      console.log('\ncheck-render: clean');
    }
  } finally {
    await browser.close();
    server.close();
  }
}

main().catch(e => { console.error(e); process.exit(2); });
