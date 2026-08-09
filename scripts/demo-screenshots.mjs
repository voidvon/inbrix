/**
 * lilmail demo-mode Playwright screenshotter
 *
 * Captures screenshots using the in-memory demo account (no real IMAP needed).
 * Assumes the server is already running (started by scripts/seed-demo.sh or
 * manually with [demo] enabled = true in config.toml).
 *
 * Every shot is captured TWICE — once in each colour scheme — and at
 * deviceScaleFactor 2, because the site displays these at up to ~1100 CSS px
 * on a retina screen. A 1× capture shown there is upscaled 2× by the browser
 * and the app's 13 px UI text turns to mush; that softness is the single most
 * visible quality problem a screenshot-led landing page can have.
 *
 * Output, for each name:
 *   site/screenshots/<name>.png       1280×800   (1× — the srcset default)
 *   site/screenshots/<name>@2x.png    2560×1600  (2× — what retina actually uses)
 *   site/screenshots/<name>-dark.png      + @2x  (dark colour scheme)
 * and a 1× mirror in docs/screenshots/ for the in-repo markdown.
 *
 * Run via:
 *   scripts/seed-demo.sh --screenshots    (recommended — handles everything)
 *   BASE_URL=http://localhost:3099 node scripts/demo-screenshots.mjs
 *
 * Environment variables:
 *   BASE_URL            Running lilmail instance. Default: http://localhost:3099
 *   LILMAIL_DEMO_EMAIL  Demo account email.    Default: demo@lilmail.dev
 *   LILMAIL_DEMO_PASS   Demo account password. Default: demo
 */

import { chromium } from 'playwright';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import { mkdirSync, copyFileSync } from 'fs';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT      = resolve(__dirname, '..');
const SITE_DIR  = resolve(ROOT, 'site', 'screenshots');
const DOCS_DIR  = resolve(ROOT, 'docs', 'screenshots');
mkdirSync(SITE_DIR, { recursive: true });
mkdirSync(DOCS_DIR, { recursive: true });

const BASE_URL   = process.env.BASE_URL || 'http://localhost:3099';
const DEMO_EMAIL = process.env.LILMAIL_DEMO_EMAIL || process.env.LILMAIL_USERNAME || 'demo@lilmail.dev';
const DEMO_PASS  = process.env.LILMAIL_DEMO_PASS  || process.env.LILMAIL_PASSWORD || 'demo';

const WIDTH  = 1280;
const HEIGHT = 800;
const PHONE_W = 390;
const PHONE_H = 780;

/**
 * One capture pass. `scheme` is 'light' or 'dark'; `scale` is the device pixel
 * ratio. The file suffix encodes both, so a single loop produces the whole
 * 1×/2× × light/dark matrix without any of the four passes knowing about the
 * others.
 */
function fileFor(name, scheme, scale) {
  return `${name}${scheme === 'dark' ? '-dark' : ''}${scale === 2 ? '@2x' : ''}.png`;
}

async function run(browser, scheme, scale) {
  const context = await browser.newContext({
    viewport: { width: WIDTH, height: HEIGHT },
    deviceScaleFactor: scale,
    colorScheme: scheme,
    reducedMotion: 'reduce',   // no half-finished transitions baked into a still
  });
  const page = await context.newPage();
  const taken = [];

  const shot = async (target, name, description) => {
    const file = fileFor(name, scheme, scale);
    await target.screenshot({ path: resolve(SITE_DIR, file), fullPage: false });
    taken.push(name);
    console.log(`  [ok] ${file} — ${description}`);
  };

  // Alpine hides pre-init markup with [x-cloak]; shooting before it clears
  // bakes an invisible half-page into the PNG.
  async function waitForAlpine(p) {
    try {
      await p.waitForFunction(() => document.querySelectorAll('[x-cloak]').length === 0, { timeout: 4000 });
    } catch (_) {
      await p.waitForTimeout(600);
    }
    // Web fonts settle after Alpine; a shot taken mid-swap shows fallback metrics.
    try { await p.evaluate(() => document.fonts.ready); } catch (_) {}
    await p.waitForTimeout(150);
  }

  try {
    // ---------------------------------------------------------------- login
    console.log(`\n[${scheme} ${scale}×] login page`);
    await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle' });
    await waitForAlpine(page);
    await shot(page, 'login', 'Login page');

    // ------------------------------------------------- authenticate (demo)
    await page.goto(`${BASE_URL}/demo-login`, { waitUntil: 'networkidle' });
    if (page.url().includes('/login')) {
      throw new Error('Demo login failed — check that [demo] enabled = true in config.toml');
    }

    // ---------------------------------------------------------------- inbox
    console.log(`[${scheme} ${scale}×] inbox`);
    await page.goto(`${BASE_URL}/inbox`, { waitUntil: 'networkidle' });
    await waitForAlpine(page);
    await shot(page, 'inbox', 'Inbox with seeded demo messages');

    // ----------------------------------------------------------------- hero
    // The landing page leads with this one, so it has to be the densest view
    // the app has: three panes, all populated. Prefer the message with real
    // body copy and an attachment over the short notification emails — an
    // open message whose body is four lines leaves the reading pane looking
    // like a bug rather than a feature.
    console.log(`[${scheme} ${scale}×] hero (inbox + open message)`);
    const openBestMessage = async (p) => {
      const rows = await p.$$('.email-row');
      let target = null;
      for (const row of rows) {
        const isThread = await row.evaluate(el => !!el.querySelector('.email-row__chevron'));
        const subject  = await row.evaluate(el => el.querySelector('.email-row__subject')?.textContent || '');
        if (!isThread && /moodboard/i.test(subject)) { target = row; break; }
      }
      if (!target) {
        for (const row of rows) {
          const isThread = await row.evaluate(el => !!el.querySelector('.email-row__chevron'));
          if (!isThread) { target = row; break; }
        }
      }
      if (!target) return false;
      await target.click();
      // The placeholder's own text is long enough to satisfy a naive length
      // check, so assert the real view element and the placeholder's absence.
      try {
        await p.waitForFunction(() => {
          const pane = document.querySelector('#email-viewer-pane');
          return pane && pane.querySelector('.email-view') && !pane.querySelector('.viewer-placeholder');
        }, { timeout: 5000 });
      } catch (_) {
        await p.waitForTimeout(1500);
      }
      await p.waitForTimeout(900);   // the mail iframe auto-sizes to its content
      return true;
    };

    if (await openBestMessage(page)) {
      await shot(page, 'hero', 'Inbox with a message open in the reading pane');
    } else {
      console.warn('  [skip] hero — no single-message .email-row found');
    }

    // -------------------------------------------------------------- message
    // Deliberately NOT the same message as the hero: this one expands a JWZ
    // thread first and reads a reply inside it, so the "Reading" screen on the
    // landing page shows conversation threading rather than repeating the
    // hero's picture with a different caption.
    console.log(`[${scheme} ${scale}×] message view`);
    const msgPage = await context.newPage();
    await msgPage.goto(`${BASE_URL}/inbox`, { waitUntil: 'networkidle' });
    await waitForAlpine(msgPage);
    await msgPage.evaluate(() => {
      try {
        const root = document.querySelector('[x-data]');
        const data = root?._x_dataStack?.[0];
        if (data) { data.showComposeModal = false; data.showEmailViewer = false; }
      } catch (_) {}
    });
    await msgPage.waitForTimeout(300);

    const waitForViewer = async (p) => {
      try {
        await p.waitForFunction(() => {
          const pane = document.querySelector('#email-viewer-pane');
          return pane && pane.querySelector('.email-view') && !pane.querySelector('.viewer-placeholder');
        }, { timeout: 5000 });
      } catch (_) {
        await p.waitForTimeout(1500);
      }
      await p.waitForTimeout(900);
    };

    let gotMessage = false;
    const threadRow = await msgPage.$('.email-row:has(.email-row__chevron)');
    if (threadRow) {
      await threadRow.click();
      // Expansion injects .thread-msg children in place; it does not open the
      // viewer, so a shot taken here would show an empty reading pane.
      try {
        await msgPage.waitForSelector('.thread-msg', { timeout: 3000 });
        // Every collapsed thread in the list contributes hidden .thread-msg
        // nodes, so an unfiltered $$ returns children of threads that are
        // still shut — clicking one waits 30 s for a never-visible element.
        // Take only the children the expansion actually revealed.
        const children = [];
        for (const el of await msgPage.$$('.thread-msg')) {
          if (await el.isVisible()) children.push(el);
        }
        if (children.length) {
          await children[children.length - 1].click({ timeout: 4000 });
          await waitForViewer(msgPage);
          gotMessage = await msgPage.evaluate(
            () => !!document.querySelector('#email-viewer-pane .email-view'));
        }
      } catch (_) {}
    }
    if (!gotMessage) gotMessage = await openBestMessage(msgPage);   // fall back to any single message
    if (gotMessage) {
      await shot(msgPage, 'message', 'Message viewer — a reply open inside an expanded thread');
    } else {
      console.warn('  [skip] message — could not open a message in the viewer');
    }
    await msgPage.close();

    // -------------------------------------------------------------- compose
    console.log(`[${scheme} ${scale}×] compose modal`);
    await page.goto(`${BASE_URL}/inbox`, { waitUntil: 'networkidle' });
    await waitForAlpine(page);
    const composeBtn = await page.$([
      'button[data-compose]', '[data-action="compose"]',
      '.compose-btn', '.btn-compose', 'button:has-text("Compose")',
    ].join(', '));
    if (composeBtn) {
      await composeBtn.click();
      await page.waitForTimeout(700);
      const toField = await page.$('input[name="to"], input[placeholder*="To"], input[aria-label*="To"]');
      if (toField) await toField.fill('alice@example.com');
      const subjectField = await page.$('input[name="subject"], input[placeholder*="Subject"]');
      if (subjectField) await subjectField.fill('Re: Product roadmap Q3');
      await shot(page, 'compose', 'Compose modal with CC/BCC and attachment UI');
    } else {
      console.warn('  [skip] compose — compose button not found');
    }

    // --------------------------------------------------------------- search
    console.log(`[${scheme} ${scale}×] search results`);
    await page.goto(`${BASE_URL}/inbox`, { waitUntil: 'networkidle' });
    await waitForAlpine(page);
    const searchInput = await page.$('input[type="search"], input[name="q"]');
    if (searchInput) {
      await searchInput.click();
      // type() so HTMX sees real keydown/keyup events; fill() fires neither.
      await page.type('input[type="search"], input[name="q"]', 'roadmap', { delay: 80 });
      await page.waitForTimeout(1400);   // 500 ms hx-trigger debounce + network
      // Open the hit before capturing. Searching and then photographing an
      // empty reading pane spent three quarters of the frame on the words
      // "Select a message to read" — the shot showed the search box working
      // and the product not. Reuses openBestMessage so the capture waits on
      // .email-view actually existing rather than on a timeout.
      // openBestMessage alone is not enough here: it deliberately skips
      // thread rows, and the "roadmap" hit collapses into a thread, so the
      // first attempt found nothing to open and photographed an empty pane
      // anyway. Expand the thread and open its newest reply first, exactly
      // as the message shot does, and only then fall back.
      let opened = false;
      const hit = await page.$('.email-row:has(.email-row__chevron)');
      if (hit) {
        await hit.click();
        try {
          await page.waitForSelector('.thread-msg', { timeout: 3000 });
          const kids = [];
          for (const el of await page.$$('.thread-msg')) {
            if (await el.isVisible()) kids.push(el);
          }
          if (kids.length) {
            await kids[kids.length - 1].click({ timeout: 4000 });
            await page.waitForFunction(() => {
              const pane = document.querySelector('#email-viewer-pane');
              return pane && pane.querySelector('.email-view') && !pane.querySelector('.viewer-placeholder');
            }, { timeout: 5000 }).catch(() => {});
            await page.waitForTimeout(900);
            opened = await page.evaluate(
              () => !!document.querySelector('#email-viewer-pane .email-view'));
          }
        } catch (_) {}
      }
      if (!opened) opened = await openBestMessage(page);
      if (!opened) {
        console.warn('  [warn] search — no result row to open; capturing the list alone');
      }
      await shot(page, 'search', 'Search results for "roadmap", with the match open');
    } else {
      console.warn('  [skip] search — no search input found');
    }

    // ------------------------------------------------------------- calendar
    console.log(`[${scheme} ${scale}×] calendar`);
    const calResp = await page.goto(`${BASE_URL}/calendar`, { waitUntil: 'networkidle' });
    if (calResp && calResp.status() < 400) {
      await waitForAlpine(page);
      await shot(page, 'calendar', 'Calendar month view');
    } else {
      console.warn(`  [skip] calendar — status ${calResp?.status()}`);
    }

    // ------------------------------------------------------------- settings
    console.log(`[${scheme} ${scale}×] settings`);
    await page.goto(`${BASE_URL}/settings`, { waitUntil: 'networkidle' });
    await waitForAlpine(page);
    await shot(page, 'settings', 'Settings page');
  } finally {
    await context.close();
  }
  return taken;
}

/**
 * Phone pass. The landing page cannot use the 1280-wide desktop captures on a
 * phone: scaled to ~350 CSS px the app's 13 px UI text lands at ~4 px and the
 * shot reads as a grey smudge. lilmail has a real single-column layout below
 * 1024 px, so capture that instead and show it at close to 1:1.
 */
async function runPhone(browser, scheme, scale) {
  const context = await browser.newContext({
    viewport: { width: PHONE_W, height: PHONE_H },
    deviceScaleFactor: scale,
    colorScheme: scheme,
    isMobile: true,
    hasTouch: true,
    reducedMotion: 'reduce',
  });
  const page = await context.newPage();
  const taken = [];
  try {
    await page.goto(`${BASE_URL}/demo-login`, { waitUntil: 'networkidle' });
    if (page.url().includes('/login')) throw new Error('Demo login failed');
    await page.goto(`${BASE_URL}/inbox`, { waitUntil: 'networkidle' });
    await page.waitForSelector('.list-col', { timeout: 8000 });
    try { await page.evaluate(() => document.fonts.ready); } catch (_) {}
    await page.waitForTimeout(500);

    // Guard the reason this pass exists. Below 1024 the list column must own
    // the full width; when it does not, the capture shows a dead strip where
    // the hidden reading pane used to be and the landing page ships that.
    const listW = await page.evaluate(
      () => Math.round(document.querySelector('.list-col').getBoundingClientRect().width));
    if (listW < PHONE_W - 1) {
      throw new Error(
        `phone layout is broken: .list-col is ${listW}px inside a ${PHONE_W}px viewport ` +
        `(expected full width below 1024px)`);
    }

    const file = `phone-inbox${scheme === 'dark' ? '-dark' : ''}${scale === 2 ? '@2x' : ''}.png`;
    await page.screenshot({ path: resolve(SITE_DIR, file), fullPage: false });
    taken.push('phone-inbox');
    console.log(`  [ok] ${file} — Inbox, single-column phone layout`);
  } finally {
    await context.close();
  }
  return taken;
}

async function main() {
  const browser = await chromium.launch({ headless: true });
  let names = [];
  try {
    for (const scale of [1, 2]) {
      for (const scheme of ['light', 'dark']) {
        names = await run(browser, scheme, scale);
        console.log(`[${scheme} ${scale}×] phone`);
        await runPhone(browser, scheme, scale);
      }
    }
  } finally {
    await browser.close();
  }

  // The repo's own docs/ markdown embeds the light 1× shots; keep that mirror
  // in step so docs/SCREENSHOTS.md never points at a stale image.
  for (const name of names) {
    try {
      copyFileSync(resolve(SITE_DIR, `${name}.png`), resolve(DOCS_DIR, `${name}.png`));
    } catch (_) {}
  }

  console.log(`\nDone. ${names.length} views × light/dark × 1×/2× written to ${SITE_DIR}`);
  console.log(`Light 1× mirrored to ${DOCS_DIR}`);
}

main().catch((err) => {
  console.error('\nDemo screenshotter failed:', err.message);
  process.exit(1);
});
