// Captures docs/img/*.png from a seeded local instance, per shots.json. See README.md.
//
//   PASSWORD=<the seed's password> node capture.mjs [--only members,schedule]
//
// Logs in once and reuses the session (the login endpoint is rate-limited), sets the
// theme the way the app stores it, waits for each page's requests to settle, and sizes
// the viewport to the page's scroll container (a full-page screenshot stops at the fold,
// because the layout scrolls .content-scroll, not the document).
import { chromium } from 'playwright';
import { readFileSync, mkdirSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const cfg = JSON.parse(readFileSync(join(here, 'shots.json'), 'utf8'));
const out = join(here, '..', '..', 'docs', 'img');
const base = process.env.BASE || cfg.base;
const password = process.env.PASSWORD;
const onlyArg = process.argv.indexOf('--only');
const only = onlyArg > 0 ? new Set(process.argv[onlyArg + 1].split(',')) : null;

if (!password) {
  console.error('Set PASSWORD to the password cmd/demo-seed set (its --password, or the one it printed).');
  process.exit(2);
}

// The app reads "today" from its own clock, so the seed's anchor must be today's game date
// (UTC−2) or the current-week views show the wrong week. See README.md.
const gameToday = new Date(Date.now() - 2 * 3600 * 1000).toISOString().slice(0, 10);
if (cfg.today !== gameToday && !process.argv.includes('--allow-other-day')) {
  console.error(`shots.json says ${cfg.today} but today's game date is ${gameToday}.`);
  console.error('Seed with today\'s date and move shots.json\'s date (a full retake), or pass --allow-other-day');
  console.error('to retake single shots whose dates will then differ from the rest of the set.');
  process.exit(2);
}

mkdirSync(out, { recursive: true });
const authDir = join(here, '.auth');
mkdirSync(authDir, { recursive: true });
const state = join(authDir, 'state.json');

const browser = await chromium.launch();
{
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await page.goto(base + '/login');
  const status = await page.evaluate(async ([u, p]) => (await fetch('/api/login', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: u, password: p }),
  })).status, [cfg.account, password]);
  if (status !== 200) {
    console.error(`login as ${cfg.account} failed: HTTP ${status}`);
    process.exit(1);
  }
  await ctx.storageState({ path: state });
  await ctx.close();
}

let failed = 0;
for (const shot of cfg.shots) {
  if (only && !only.has(shot.file)) continue;
  const ctx = await browser.newContext({ storageState: state, viewport: { width: shot.width, height: 900 } });
  await ctx.addInitScript(theme => {
    localStorage.setItem('lastwar-theme-preference', theme);
    // The once-per-session LastRank review toast would otherwise land in the picture.
    sessionStorage.setItem('lastrank-review-nudged', '1');
  }, shot.theme);
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('response', r => { if (r.status() >= 500) errors.push(`${r.status()} ${r.url()}`); });
  await page.goto(base + shot.path, { waitUntil: 'networkidle' });
  await page.waitForTimeout(1000); // charts animate in
  const height = await page.evaluate(() => {
    const c = document.querySelector('.content-scroll');
    return c ? c.scrollHeight : document.documentElement.scrollHeight;
  });
  const max = shot.max_height || cfg.max_height;
  await page.setViewportSize({ width: shot.width, height: Math.min(height, max) });
  await page.waitForTimeout(300);
  const file = join(out, shot.file + '.png');
  await page.screenshot({ path: file });
  console.log(`${errors.length ? 'WARN' : 'ok  '} ${shot.file}.png  ${shot.width}×${Math.min(height, max)} ${shot.theme}${errors.length ? '  ' + errors.join('; ') : ''}`);
  if (errors.length) failed++;
  await ctx.close();
}
await browser.close();
process.exit(failed ? 1 : 0);
