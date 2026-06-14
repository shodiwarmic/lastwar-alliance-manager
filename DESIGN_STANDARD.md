# Alliance Manager — Frontend Design Standard

This document is the canonical reference for all UI/CSS work in this codebase. It complements `CLAUDE.md` (which covers architecture, backend patterns, and Go conventions) by covering the design system: tokens, theming, components, and CSS/JS rules specific to the visual layer.

---

## Table of Contents

1. [Theme System](#theme-system)
2. [Design Tokens](#design-tokens)
3. [CSS File Organisation](#css-file-organisation)
4. [Global Utility Classes](#global-utility-classes)
5. [Component Patterns](#component-patterns)
   - [Buttons](#buttons)
   - [Modals](#modals)
   - [Tabs](#tabs)
   - [Data Tables](#data-tables)
   - [Chips and Badges](#chips-and-badges)
   - [Cards](#cards)
   - [Status and Feedback](#status-and-feedback)
6. [Typography](#typography)
7. [Icon System](#icon-system)
8. [Dark Mode Rules](#dark-mode-rules)
9. [JavaScript DOM Standards](#javascript-dom-standards)
10. [Approved Exceptions](#approved-exceptions)

---

## Theme System

The app uses two theming systems. **Only the attribute system is canonical for new code.**

### Canonical: `html[data-theme]`

`theme.js` sets `data-theme="light"` or `data-theme="dark"` on `<html>` synchronously on page load, before first paint. All CSS written during and after the redesign uses this selector.

```css
/* ✅ Correct — new page CSS */
html[data-theme="dark"] .my-element {
    background: var(--color-surface);
}
```

### Legacy: `html.theme-*` (do not use for new code)

The old class-based system (`html.theme-dark`, `html.theme-light`, `html.theme-auto`) remains in `styles.css` to support page CSS written before the redesign. Do not write new dark-mode overrides targeting these classes. The two systems coexist because `theme.js` sets both simultaneously; the canonical tokens in `:root` and `html[data-theme]` always win due to specificity.

### `color-scheme` property

When setting the native `color-scheme` hint on a specific element (e.g. a date picker), include `html[data-theme="dark"]` alongside the legacy selectors:

```css
/* ✅ Covers both systems */
html.theme-dark .my-input,
html.theme-auto .my-input,
html[data-theme="dark"] .my-input {
    color-scheme: dark;
}
```

---

## Design Tokens

All CSS must use these tokens. Never hardcode hex or rgba colour values (see [Approved Exceptions](#approved-exceptions) for the narrow list of cases where hardcoded values are intentional).

### Surface / Background

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-bg` | `#f4f6f9` | `#0d1117` | Page background, content area fill |
| `--color-surface` | `#ffffff` | `#161b26` | Topbar, section headers, filter bars |
| `--color-card` | `#ffffff` | `#1c2130` | Cards, table rows, list items |

Layer these like an elevation stack: `--color-bg` → `--color-surface` → `--color-card`. Don't skip levels.

### Text

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-text` | `#1c2030` | `#e4e8f4` | Body text, headings, labels |
| `--color-text-mid` | `#4a5270` | `#9aa3bc` | Secondary text, metadata, column headers |
| `--color-text-muted` | `#8b93a8` | `#545d78` | Placeholders, hints, timestamps |

### Border

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-border` | `#e4e8f0` | `rgba(255,255,255,0.08)` | All borders, dividers, separators |

Use one border token for everything. Do not invent per-component border colours.

### Accent

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-accent` | `#7c6df7` | `#9580ff` | Active states, focus rings, links |
| `--color-accent-bg` | `rgba(124,109,247,0.09)` | `rgba(149,128,255,0.12)` | Active nav item background, drag-over states, chip fills |
| `--color-accent-border` | `rgba(124,109,247,0.25)` | `rgba(149,128,255,0.30)` | Chip and tag borders at accent weight |

### Semantic Colours

| Token pair | Light bg / fg | Dark bg / fg | Use |
|---|---|---|---|
| `--color-success-bg` / `--color-success` | `#dcfce7` / `#16a34a` | `rgba(74,222,128,0.12)` / `#4ade80` | Eligible, attended, active-season badges |
| `--color-warning-bg` / `--color-warning` | `#fef3c7` / `#d97706` | `rgba(251,191,36,0.12)` / `#fbbf24` | Pending, substitute, at-capacity states |
| `--color-danger-bg` / `--color-danger` | `#fee2e2` / `#dc2626` | `rgba(248,113,113,0.12)` / `#f87171` | Errors, destructive actions, no-show, below-threshold |
| `--color-info-bg` / `--color-info` | `#dbeafe` / `#1d4ed8` | `rgba(59,130,246,0.15)` / `#93c5fd` | Informational panels, at-risk badges, daily-frequency |
| `--color-purple-bg` / `--color-purple` | `#ede9fe` / `#6d28d9` | `rgba(109,40,217,0.15)` / `#c4b5fd` | Role/privilege indicators, Admin nav, seasonal-frequency |

**Warning token note:** `--color-warning` / `--color-warning-bg` are defined in `html[data-theme]` blocks and in `:root` as fallbacks. Always pass a fallback when using them inline just in case:

```css
color: var(--color-warning, #d97706);
background: var(--color-warning-bg, #fef3c7);
```

### Alert Panel (inline info boxes)

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-alert-bg` | `#ede9ff` | `#1e1830` | Alert/callout panel background |
| `--color-alert-border` | `rgba(124,109,247,0.30)` | `rgba(149,128,255,0.30)` | Alert panel border |
| `--color-alert-text` | `#4c1d95` | `#c4b5fd` | Alert panel text |

Use alert panels sparingly — for contextual warnings that live inline with content, not for transient feedback (use `showToast` for that).

### Sidebar Shell

These tokens are for the sidebar and mobile header only. Do not use in page content.

| Token | Value | Use |
|---|---|---|
| `--color-sidebar` | `#1c2030` / `#111420` | Sidebar background |
| `--color-sidebar-text` | `#c8cfdf` | Sidebar nav link text |
| `--color-sidebar-muted` | `#6b7490` | Section labels, username sub-text |
| `--color-sidebar-border` | `rgba(255,255,255,0.07)` | Sidebar dividers |
| `--color-sidebar-hover` | `rgba(255,255,255,0.05)` | Nav link hover state |

### Legacy tokens (do not use in new page CSS)

These exist in the legacy `html.theme-*` blocks and are still referenced by pre-redesign page CSS. Do not introduce them in new files.

| Do not use | Use instead |
|---|---|
| `--danger-color` | `--color-danger` |
| `--accent-color` | `--color-accent` |
| `--primary-color` | `--color-accent` |
| `--text-color` | `--color-text` |
| `--bg-primary` / `--bg-secondary` | `--color-bg` / `--color-surface` |
| `--text-primary` / `--text-secondary` | `--color-text` / `--color-text-mid` |
| `--border-color` | `--color-border` |

---

## CSS File Organisation

```
styles.css          — Global tokens, shell layout, utility classes. Never touch for page features.
static/feature.css  — Page-specific layout only. Link via {{define "head_tags"}}.
```

**Before writing any page CSS**, check whether `styles.css` already provides what you need:

```
.card / .card-header .data-table          .filter-chip / .filter-chip-label
.tab-toolbar         .status-msg / .loading-msg / .empty-state
.tab-bar / .tab-btn  .badge-hq / .badge-troop / .badge-profession / .badge-squad-type
.btn / .btn-primary / .btn-secondary / .btn-danger / .btn-warning / .btn-ghost / .btn-sm
.button-group        .form-input          .modal / .modal-content
```

Page CSS is for page-specific layout only (grid arrangements, per-page card shapes, per-page overrides). Global pattern CSS goes in `styles.css`.

---

## Responsive Layout

**One structural width breakpoint: `@media (max-width: 768px)` / `(min-width: 769px)`.** It exists solely to swap the navigation chrome (sidebar ↔ mobile header + bottom-tabs + off-canvas). **Do not add other width breakpoints.** Make everything else fluid:

- **Type & spacing** → `clamp(min, vw, max)` — e.g. `padding: clamp(15px, 3vw, 30px)`.
- **Grids** → `columns: <width>` or `repeat(auto-fit, minmax(<min>, 1fr))` (reflow on their own). For a *discrete* set of column counts (e.g. the schedule's 7/4/2/1), use **container queries** (`container-type: inline-size` + `@container`) — component-scoped, keyed to the component's own width, so they're not page breakpoints.
- **Toolbars / headers** → `flex-wrap` + `margin-left:auto` / `justify-content`.
- **Tabs / wide tables** → `overflow-x: auto`.

True mobile-shell behaviours (things that exist only because the bottom-tabs / off-canvas nav does) go *inside* the `≤768px` block, not a new breakpoint. Avoid JS `matchMedia` width checks; if unavoidable, key them to 768px. (Device-feature queries — `prefers-color-scheme`, `prefers-reduced-motion`, `hover/pointer` — are not width breakpoints and are fine.)

---

## Global Utility Classes

### `.data-table`

Standard table with gradient header. Use as the base class on any `<table>`. Add a page-specific subclass for `min-width` overrides:

```html
<div style="overflow-x:auto">
  <table class="data-table my-page-table">...</table>
</div>
```

Always wrap wide tables in a scroll container — don't set `overflow-x` on the table itself.

### `.filter-chip` / `.filter-chip-label`

Pill-shaped toggle button. Toggle the `.active` class in JS.

```html
<button class="filter-chip active" data-filter="r5">R5</button>
```

### `.tab-bar` / `.tab-btn`

Horizontal tab switcher. The active tab button gets `class="tab-btn active"`. Use with `.tab-content` panels.

```html
<div class="tab-bar">
  <button class="tab-btn active" data-tab="overview">Overview</button>
  <button class="tab-btn" data-tab="history">History</button>
</div>
<div id="tab-overview" class="tab-content">...</div>
<div id="tab-history"  class="tab-content">...</div>
```

Always show the initial tab explicitly in `DOMContentLoaded`:

```javascript
const activeBtn = document.querySelector('.tab-btn.active');
if (activeBtn) {
    const target = document.getElementById('tab-' + activeBtn.dataset.tab);
    if (target) target.style.display = 'block'; // NOT style.display = ''
}
```

### `.tab-toolbar`

Flex row for filter controls above a tab panel. Provides the right `gap` and `flex-wrap` without extra CSS.

**Table export buttons** (CSV / XLSX) are auto-wired by `global.js` for any
`<table data-export-csv="…">`: the two buttons are grouped in a right-aligned
`.table-export-actions` wrapper (`margin-left: auto`) and appended to the nearest preceding
`.tab-toolbar`. So put a `.tab-toolbar` above the table — it can also hold that section's
search/filter controls — and the export buttons land at its right end. A page that builds its
own export buttons (card lists with no real table, e.g. Members) right-aligns them the same way.

### `.status-msg`

Inline async status text (e.g. "Saving…" / "Saved"). Always clear it after a timeout.

### `.loading-msg` / `.empty-state`

Canonical pair for asynchronous list/table states:

- `.empty-state` — full-section "no results" placeholder (centered, generous padding). Use
  for an empty list, no search matches, or a not-yet-loaded panel.
- `.loading-msg` — row-level loading indicator inside a `.data-table` `<td>` while data loads.

```html
<tbody id="rows"><tr><td colspan="6" class="loading-msg">Loading…</td></tr></tbody>
<div id="list"><p class="empty-state">No members found.</p></div>
```

Deprecated (still in `styles.css`, do not use in new code): `.loading` / `.empty` — superseded
by `.empty-state`.

---

## Component Patterns

### Buttons

```html
<button class="btn btn-primary">Save</button>
<button class="btn btn-secondary">Cancel</button>
<button class="btn btn-danger btn-sm">Delete</button>
<button class="btn btn-warning btn-sm">Flag</button>
<button class="btn btn-ghost btn-sm">Clear</button>
```

- **All buttons are outlined, one weight.** Every variant renders the same way: a colour-coded
  border + matching text on the surface, filling with its colour on hover. The colour is the
  only difference — `.btn-primary` accent, `.btn-secondary` neutral text colour, `.btn-danger`
  red, `.btn-warning` amber, `.btn-ghost` a lighter muted outline for tertiary actions.
  Rationale: buttons in this app sit together as **peer** actions, and a *filled* button
  optically reads larger/heavier than an outlined one of identical size — which is wrong when
  the buttons are equals, and would over-emphasise the destructive one. **Do not reintroduce
  filled buttons for action emphasis.**
- **Emphasis is by SIZE, not fill.** When one action should stand out, make it a larger button
  (full-size `.btn` among `.btn-sm` siblings) — see the Train log's primary action. Never give
  one peer a filled background to make it "pop".
- **Selection state uses fill** via `.btn-toggle` (a segmented view/option toggle). The selected
  option gets `.active` (filled accent); inactive options are outlined. Here fill signals
  *state*, not emphasis. Toggle `.active` in JS. Reference: Shoutouts (dyno) view switcher,
  Members alias-filter bar. (For pill-shaped filters use `.filter-chip.active` instead.)
- Do not use `.primary-action-btn` / `.secondary-action-btn` — deprecated.
- **Two sizes only.** Default `.btn` and `.btn .btn-sm` — there is no `.btn-lg`. Heuristic: a
  button inside a card, table row, toolbar, or chip cluster takes `.btn-sm`; a standalone
  page-level primary action or a modal-footer button stays full-size `.btn`. Size and weight
  are independent (a small destructive button is `btn btn-danger btn-sm`).
- **Mobile icon collapse.** Where a row of icon+label buttons is tight on mobile, the label may
  collapse to icon-only by hiding a label span under a breakpoint — keep `title` + `aria-label`
  so the action stays clear and accessible. Card actions: `members.js` `memberActionBtn()`
  building `<svg> + <span class="member-action-label">`. Table-row actions: the global
  `rowActionBtn(className, icon, label, onClick)` helper (`global.js`) builds `<svg> +
  <span class="action-label">`; `.action-label` is hidden `@media (max-width: 768px)`.
- **Table-row action clusters.** Wrap a row cell's buttons (Edit / Delete / Excuse …) in a
  `<div class="row-actions">` — `inline-flex; gap; white-space:nowrap` keeps them on one line
  instead of wrapping when the column is squeezed; the table's horizontal scroll handles
  overflow. Reference: Season Hub rewards, Accountability strikes.
- `.btn` is `display:inline-flex` with `gap:6px`, so an SVG icon + text label align
  automatically — `<button class="btn btn-primary"><svg class="svg-icon"…><use href="/icons.svg#icon-device-floppy"/></svg> Save</button>`.
- **`.button-group`** — a `display:flex; gap:10px` container for a set of buttons. Use it
  instead of `margin-right` when laying out multiple buttons in a row.

### Modals

Modals live in `{{define "modals"}}`, **not** inside `{{define "content"}}`.

```html
{{define "modals"}}
<div id="my-modal" class="modal">
  <div class="modal-content">
    <h2>Title</h2>
    <!-- form content -->
    <div class="modal-actions">
      <button class="btn btn-primary" id="my-modal-save">Save</button>
      <button class="btn btn-secondary" id="my-modal-cancel">Cancel</button>
    </div>
  </div>
</div>
{{end}}
```

Open/close in JS:

```javascript
// Open
document.getElementById('my-modal').style.display = 'flex';

// Close
document.getElementById('my-modal').style.display = '';
```

Never add `class="hidden"` to a `.modal` element — it uses `display:none` by default and `display:flex !important` cannot override `!important` from a utility class.

Close on backdrop click:

```javascript
modal.addEventListener('click', e => {
    if (e.target === modal) modal.style.display = '';
});
```

**Close affordance — no corner `×`.** A modal closes via a `Cancel` (or `Close`) button in
the `.modal-actions` footer, plus backdrop click. Do **not** add a corner close glyph.

```html
<!-- ✅ Do -->
<div class="modal-actions">
  <button class="btn btn-primary">Save</button>
  <button class="btn btn-secondary">Cancel</button>
</div>

<!-- ❌ Don't -->
<span class="close" id="my-modal-close">&times;</span>
```

Viewer-style modals with no footer (image/document preview) close on backdrop click alone.

### Cards

`.card` is the canonical surface panel — a `--color-surface` box with border, radius,
padding, and the standard card shadow. Use it for any grouped content block; never hand-roll
a panel with a bespoke background / border / shadow.

```html
<div class="card">
  <div class="card-header">
    <h2 class="icon-heading">Section Title</h2>
    <button class="btn btn-primary">Action</button>
  </div>
  <!-- body -->
</div>
```

- `.card-header` is the title + actions row (flex, space-between, bottom border). The title is
  an `<h2>`; trailing buttons sit on the right.
- Drop the card's default `margin-bottom` (inline `margin-bottom:0`) when cards sit in a grid
  or flex row that already supplies the gap.

**Stat tiles.** A row of small metric cards (a number + a caption) is a grid of `.card`s —
not a bespoke `.stat-card`. Value/caption typography is inline with tokens (there is no
dedicated class):

```html
<div style="display:grid; grid-template-columns:repeat(auto-fit,minmax(200px,1fr)); gap:20px;">
  <div class="card" style="margin-bottom:0; text-align:center;">
    <div style="font-size:2em; font-weight:bold; line-height:1; color:var(--color-text);">42</div>
    <div style="font-size:0.85em; color:var(--color-text-mid); margin-top:5px;">Active</div>
  </div>
</div>
```

Colour the value with a semantic token (`--color-success` / `--color-danger` /
`--color-warning`) when the metric carries a sentiment. Used on Shoutouts, Admin, and
Accountability.

**Deprecated — removed from `styles.css`, do not reintroduce:** `.stats-dashboard`,
`.stat-card`, `.stat-icon`, `.charts-section`, `.view-toggle`, `.filter-section`, `.info-box`,
and the `.search-box` class — all superseded by `.card` plus standard filter/search inputs
(`.form-input`).

### Tabs

See [`.tab-bar` / `.tab-btn`](#tab-bar--tab-btn) above. Key rules:

- Show with `style.display = 'block'` — never `style.display = ''` (the global rule sets `.tab-content { display: none }`, so clearing inline style re-hides it).
- The active tab button gets `.active` class; the active panel gets `style.display = 'block'` (not a class).

**Non-canonical patterns — do not use.** Two divergent tab patterns exist in older pages
and must not be used in new code:

```
❌ Class-toggled:  .tab-content.active { display:block }   (Rankings, Admin)
❌ Hidden-class:   .tab-panel + .hidden                    (Train, Accountability)
```

Both work today but bypass the global `.tab-content { display:none }` rule. Migration of
those pages is deferred; the canonical `.tab-content` + `style.display='block'` pattern is
the only one for new work.

### Data Tables

```html
<div style="overflow-x:auto">
  <table class="data-table">
    <thead>
      <tr>
        <th>Name</th>
        <th>Rank</th>
        <th style="width:1%;white-space:nowrap">Actions</th>
      </tr>
    </thead>
    <tbody id="my-table-body">
      <!-- rows injected by JS -->
    </tbody>
  </table>
</div>
```

- Pin the actions column with `width:1%; white-space:nowrap`.
- Use `overflow-x:auto` on a wrapper div, not on the table.
- For sortable columns, add `data-sort="column_key"` and `cursor:pointer` on `<th>`.

### Chips and Badges

**Rank badge** (`.member-rank`): defined in `styles.css`. Don't recreate.

**Secondary member badges**: `.badge-hq`, `.badge-troop`, `.badge-profession`, `.badge-squad-type` — all in `styles.css`.

**Semantic status badges** — use the bg/fg token pairs:

```css
.my-badge-active {
    background: var(--color-success-bg);
    color: var(--color-success);
    border-radius: 12px;
    padding: 2px 10px;
    font-size: 0.75rem;
    font-weight: 600;
}
```

**Chip borders** — use `--color-accent-border` (or the equivalent `color-mix` approach for semantic colours), not the full-strength `--color-*` text token:

```css
/* ✅ */
border: 1px solid var(--color-accent-border);
border: 1px solid color-mix(in srgb, var(--color-info) 35%, transparent);

/* ❌ Too heavy */
border: 1px solid var(--color-info);
```

### Cards

**Entity cards** (members, awards, prospects, mail items): `border: 2px solid var(--color-border)`.

**Toolbar / filter panel cards** (capacity bars, settings sections, generate panels): `border: 1px solid var(--color-border)`.

```css
/* Entity card */
.my-entity-card {
    background: var(--color-card);
    border: 2px solid var(--color-border);
    border-radius: 8px;
    padding: 16px;
}

/* Filter / info card */
.my-filter-panel {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 14px 18px;
}
```

### Status and Feedback

Never use `alert()`, `confirm()`, or `prompt()`. Use the helpers in `global.js`:

```javascript
// Transient notification
showToast('Saved successfully.');
showToast('Failed to save.', 'error');
showToast('Processing…', 'info', 8000); // custom duration ms

// Destructive confirmation (returns Promise<boolean>)
if (!await showConfirm('Delete this member?', 'Delete')) return;

// With custom title (e.g. showing newly-created credentials)
await showConfirm('Your new password is: ...', 'OK', 'Credentials');

// Inline field validation
setFieldError(document.getElementById('name-input'), 'Name is required.');
clearFieldError(document.getElementById('name-input'));
clearAllFieldErrors(document.getElementById('my-form'));
```

---

## Typography

**Font:** Plus Jakarta Sans, loaded from Google Fonts in `styles.css`. No other font family should be introduced.

**Weights in use:**

| Weight | Use |
|---|---|
| 400 | Body text, table cell values |
| 500 | Secondary labels, nav links (non-active) |
| 600 | Card titles, section headers, active nav links, form labels |
| 700 | Page headings (`<h1>` topbar), stat values |
| 800 | Brand name, avatar initials |

**Type scale (reference):**

```
0.72rem  — micro labels (column sub-labels, timestamp suffixes)
0.75rem  — badge / chip text
0.78rem  — tag text, secondary chip text
0.82rem  — table metadata
0.85rem  — secondary body, notes, hints
0.875rem — form labels, table cell body
0.9rem   — filter chips, secondary UI
0.95rem  — section titles, card titles
1rem     — primary body
1.1rem   — group headings
1.4–1.75rem — page-level headings (topbar h1 uses 18px / 1.125rem fixed)
```

Use `font-size: 0.875rem` as the default for modal form labels and table body text.

**Monospace.** All monospace text (code snippets, CSV-help `<code>`, message-box textareas,
Discord/chat output) must use the `--font-mono` token:

```css
font-family: var(--font-mono);   /* ui-monospace, 'Cascadia Code', 'Courier New', monospace */
```

Never write a bare `monospace` or `'Courier New', monospace` stack in page CSS — the token is
defined once in `:root`.

---

## Icon System

All icons use **Tabler Icons** (outline set): `viewBox="0 0 24 24"`, `stroke-width="2"`,
`stroke-linecap="round"`, `stroke-linejoin="round"`, `fill="none"`, MIT licensed. They are
delivered as a single SVG sprite at `static/icons.svg`, each icon a `<symbol id="icon-{slug}">`.

**In templates** — reference by `<use>`, with `class="svg-icon"` for alignment:

```html
<svg class="svg-icon" width="14" height="14" aria-hidden="true"><use href="/icons.svg#icon-device-floppy"/></svg>
```

The `.svg-icon` class (`vertical-align: middle`) centres the icon with adjacent text in
any container, at any size. The `svgIcon()` JS helper adds it automatically.

**Icons in headings** — add `class="icon-heading"` to the heading element. `vertical-align`
references the font's x-height, which sits a fixed-size icon low next to large heading text;
`.icon-heading` flex-centres it on the heading's optical centre instead:

```html
<h3 class="icon-heading"><svg class="svg-icon" width="16" height="16" aria-hidden="true"><use href="/icons.svg#icon-mail"/></svg> Battle Mail</h3>
```

**Icon-only wrappers** (a badge/pill/button containing *only* an icon, no text) — the wrapper
must centre the icon itself. `vertical-align` on the icon can't help because there is no text
to align against, so the icon falls to the line-box baseline. Make the wrapper a centring
flex box:

```css
.my-icon-badge { display: inline-flex; align-items: center; justify-content: center; }
```

Alignment is decided by the icon's **parent**, not the icon. Icon **+ text** in one inline
element works via `.svg-icon`; an icon **alone** needs its wrapper to flex-centre (or the row
that lays it out to use `align-items: center`).

**In JavaScript** — use the `svgIcon(name, size = 14)` helper from `global.js`:

```javascript
btn.replaceChildren(svgIcon('pencil'), document.createTextNode(' Edit'));   // icon + label
delBtn.setAttribute('aria-label', 'Delete'); delBtn.appendChild(svgIcon('trash')); // icon-only
```

Icons inherit colour via `currentColor`, so they adapt to text colour and theme automatically.
Standalone pages that aren't built on `layout.html` (e.g. `login.html`) must include
`<script src="/global.js"></script>` before their page script to get `svgIcon`.

**Choosing an icon — the whole Tabler set is in scope, not just the sprite.**
`static/icons.svg` contains only the subset of Tabler icons added so far; it is **not** the
palette to choose from. The full Tabler library (~5,900 outline icons, browsable at
<https://tabler.io/icons>) is available to us. When a feature needs an icon, find the
semantically correct Tabler icon and add it — do **not** settle for an approximate icon just
because it already happens to be in the sprite.

**Adding a new icon:** copy the `<path>` elements from
`https://raw.githubusercontent.com/tabler/tabler-icons/main/icons/outline/{slug}.svg`
and add a `<symbol id="icon-{slug}" viewBox="0 0 24 24" fill="none" stroke="currentColor"
stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` to `static/icons.svg`,
matching the existing symbols (strip Tabler's transparent background `<path>`).

**Emoji policy.** No emoji in UI chrome — section headings, tab labels, button faces, stat
cards, badges, page illustrations, and category labels all use SVG icons. See
[Approved Exceptions](#approved-exceptions) for the narrow set of permitted non-icon glyphs.

---

## Dark Mode Rules

1. **Always use tokens.** Every foreground and background colour must come from a `var(--color-*)` token. Hardcoded hex/rgba does not adapt.

2. **Use `html[data-theme="dark"]` for overrides**, not `html.theme-dark`. Both work today, but `data-theme` is canonical.

3. **Include the `data-theme` selector for `color-scheme`:**

   ```css
   html.theme-dark .my-input,
   html[data-theme="dark"] .my-input {
       color-scheme: dark;
   }
   ```

4. **`color-mix()` for semi-transparent borders:**

   When a border needs to be tinted with a semantic colour at reduced opacity, use `color-mix` so it automatically adapts between the light and dark token values:

   ```css
   border: 1px solid color-mix(in srgb, var(--color-danger) 35%, transparent);
   ```

5. **Never test only in light mode.** Check every new component in both `data-theme="dark"` and `data-theme="light"` before committing.

6. **Chart.js colours** — read from the computed token at init, don't hardcode:

   ```javascript
   const style = getComputedStyle(document.documentElement);
   Chart.defaults.color = style.getPropertyValue('--color-text-mid').trim();
   ```

---

## JavaScript DOM Standards

### Safe DOM construction

```javascript
// ✅ Always
const card = document.createElement('div');
card.className = 'my-card';
card.textContent = item.name;
container.appendChild(card);

// Batch replace children cleanly
container.replaceChildren(...items.map(buildCard));

// ❌ Never
container.innerHTML = `<div class="my-card">${item.name}</div>`;
```

### Event binding

```javascript
// ✅
btn.addEventListener('click', () => deleteItem(item.id));

// ❌
btn.setAttribute('onclick', `deleteItem(${item.id})`);
```

### Colours in JS

```javascript
// ✅
span.style.color = 'var(--color-info)';
span.style.cssText = 'color:var(--color-text-mid);font-size:0.85em;';

// ❌
span.style.color = '#1d4ed8';
```

If the same styled element is created in a loop, put the style in a CSS class rather than repeating it in JS.

### `el()` helper pattern

For files that build significant DOM, use a builder helper rather than chained `createElement` calls. See `dashboard.js` for the canonical `el(tag, props, ...children)` implementation — copy it into any new file that constructs complex DOM.

### Destructive confirmation (button-swap pattern)

When there is no async `showConfirm` available (rare), use the inline swap:

```javascript
deleteBtn.addEventListener('click', () => {
    deleteBtn.style.display = 'none';
    const span = document.createElement('span');
    span.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
    const label = Object.assign(document.createElement('span'), { textContent: 'Sure?', style: 'font-size:0.85rem;' });
    const yes = Object.assign(document.createElement('button'), { className: 'btn btn-danger btn-sm', textContent: 'Yes' });
    const no  = Object.assign(document.createElement('button'), { className: 'btn btn-secondary btn-sm', textContent: 'No' });
    yes.addEventListener('click', () => doDelete(item.id));
    no.addEventListener('click', () => { span.remove(); deleteBtn.style.display = ''; });
    span.append(label, yes, no);
    actionsContainer.appendChild(span);
});
```

Prefer `showConfirm` in all new code — the swap pattern exists for legacy compatibility.

---

## Approved Exceptions

### Emoji & non-icon glyphs

UI chrome uses SVG icons (see [Icon System](#icon-system)). The only places a literal glyph
or emoji is permitted:

- **Copy-to-game text output.** Strings the user copies into the game (Desert Storm battle
  mail, Schedule Discord/chat text, the schedule canvas PNG) may contain emoji — SVG cannot
  render in plaintext, and the marker is part of the copied content (`🏜️ ⏰ 💪 ⚡`).
- **User-entered icon fields.** The Schedule event-type "Icon" inputs (`et-icon`, `se-icon`)
  and the season-template icon cell store a user-chosen emoji as data; their placeholders
  (`📅`, `🌐`, `🎯`) hint that an emoji is expected.
- **Plain-text typographic symbols.** `✓` (U+2713), `✗` (U+2717), `★` (U+2605), `✕` (U+2715),
  `—` used inline in compact status text / column headers are typographic glyphs (not
  emoji-presentation) and render consistently across platforms. New code should still prefer
  `svgIcon('check')` / `svgIcon('x')` for prominent status; these are tolerated inline only.
- **`<option>` labels.** SVG cannot live inside `<option>`; squad-type and upload-category
  selects use plain text labels (the icon is shown on the resulting card/badge instead).

No other emoji belong in headings, buttons, badges, tabs, or page illustrations.

### Hardcoded colours

The following hardcoded colour values are intentional and should not be replaced with tokens.

### Seat tier dots (`recruiting.css`)

```css
.seat-gold   { background: #f1c40f; }
.seat-purple { background: #9b59b6; }
.seat-blue   { background: #3498db; }
.seat-grey   { background: #95a5a6; }
```

These map to specific in-game seat tier colours that must be constant across light and dark mode. They are not semantic states — they are game identifiers. Any new in-game colour-coding that requires this treatment should be documented here.

### Sidebar shell colours

`--color-sidebar` is hardcoded to a near-black value in both themes (dark stays dark; light also uses a dark sidebar). This is intentional — the sidebar is always a dark panel regardless of page theme.

---

*Last updated: May 2026. Amend this document whenever a new global pattern is introduced or an existing one is changed.*
