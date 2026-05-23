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
7. [Dark Mode Rules](#dark-mode-rules)
8. [JavaScript DOM Standards](#javascript-dom-standards)
9. [Approved Exceptions](#approved-exceptions)

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
.data-table          .filter-chip / .filter-chip-label
.tab-toolbar         .status-msg
.tab-bar / .tab-btn  .badge-hq / .badge-troop / .badge-profession / .badge-squad-type
.btn / .btn-primary / .btn-secondary / .btn-danger / .btn-sm / .btn-warning
.form-input          .modal / .modal-content
```

Page CSS is for page-specific layout only (grid arrangements, per-page card shapes, per-page overrides). Global pattern CSS goes in `styles.css`.

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

### `.status-msg`

Inline async status text (e.g. "Saving…" / "Saved"). Always clear it after a timeout.

---

## Component Patterns

### Buttons

```html
<button class="btn btn-primary">Save</button>
<button class="btn btn-secondary">Cancel</button>
<button class="btn btn-danger btn-sm">Delete</button>
<button class="btn btn-warning btn-sm">Flag</button>
```

- Do not use `.primary-action-btn` / `.secondary-action-btn` — deprecated.
- `btn-sm` reduces padding for inline/table contexts.
- Buttons in flex rows: wrap them in a `display:flex; gap:6px` container, never rely on `margin-right`.

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

### Tabs

See [`.tab-bar` / `.tab-btn`](#tab-bar--tab-btn) above. Key rules:

- Show with `style.display = 'block'` — never `style.display = ''` (the global rule sets `.tab-content { display: none }`, so clearing inline style re-hides it).
- The active tab button gets `.active` class; the active panel gets `style.display = 'block'` (not a class).

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
