# Alliance Manager — Claude Code Guide

## Stack
- **Backend**: Go, gorilla/mux, gorilla/csrf, gorilla/sessions, SQLite (mattn/go-sqlite3)
- **Migrations**: Goose (`-- +goose Up` / `-- +goose StatementBegin` headers required)
- **Frontend**: Vanilla JS, no build step. CSS custom properties (`var(--name)`) throughout.
- **Templates**: Go `html/template`, parsed as `layout.html` + page template pairs

## Adding a new feature — checklist

1. **Migration** — name it `NNN_feature_name.sql` where NNN follows the last file in `migrations/`. Check before assuming.
2. **Permissions** — add columns to `rank_permissions` via migration, add fields to `RankPermissions` struct in `models.go`, add to the `getRankPermissions` SELECT in `handlers_admin.go`, and add the all-true entry in the admin shortcut block in `main.go` (`getPageData`).
3. **Routes** — register in `main.go` following the existing pattern. UI page routes go in the `pages` map and `pagePermissions` map.
4. **Handler file** — one file per feature, e.g. `handlers_feature.go`.
5. **Template** — `templates/feature.html`. Define `header_text`, `content`, `scripts`, and `modals` blocks (see below).
6. **CSS** — `static/feature.css`, linked via `{{define "head_tags"}}`. Never embed `<style>` blocks in templates.
7. **JS** — `static/feature.js`, loaded in `{{define "scripts"}}`.

## Known gotchas

### rank is TEXT, not INTEGER
`rank_permissions.rank` is `TEXT PRIMARY KEY` with values `'R1'`–`'R5'`. Integer comparisons silently do nothing.

```sql
-- Wrong
UPDATE rank_permissions SET col = true WHERE rank >= 4;

-- Correct (see 008_schedules.sql as the canonical pattern)
UPDATE rank_permissions SET col = 1 WHERE rank IN ('R4', 'R5');
```

### Modals must go in `{{define "modals"}}`, not `{{define "content"}}`
`layout.html` renders `{{block "modals" .}}` **outside** the `.container` div. Modals placed inside `{{define "content"}}` are trapped inside the container, which creates a stacking context that breaks `position: fixed` overlays — the backdrop won't cover the page correctly.

```html
{{define "content"}}
  <!-- page body only -->
{{end}}

{{define "modals"}}
  <div id="my-modal" class="modal">
    <div class="modal-content">...</div>
  </div>
{{end}}
```

### Use the global `.modal` / `.modal-content` classes
`styles.css` already defines `.modal` (hidden by default, toggled via `style.display='flex'`) and `.modal-content` (styled box). Don't write custom modal CSS — use these.

### CSRF is handled globally
`static/csrf.js` intercepts all `fetch` calls and injects `X-CSRF-Token` on POST/PUT/DELETE automatically. You don't need to manually attach the token in page JS.

### Pass `canManage` to the template, not the permission column name
Handlers resolve the boolean server-side and pass it to the template. The column name never reaches the frontend.

```go
data := getPageData(r, "...", "feature")
data.CanManage = data.Permissions.ManageFeature
renderTemplate(w, r, "feature.html", data)
```

```html
{{define "scripts"}}
<script>window.CAN_MANAGE = {{if .CanManage}}true{{else}}false{{end}};</script>
<script src="/feature.js"></script>
{{end}}
```

### Parameterised queries everywhere
No string-formatted SQL for user input. All DB writes should use `?` placeholders.

### Wrap deletes in a transaction
Even when cascades handle children, wrap category/parent deletes in a transaction — consistent with existing handlers.

## Template blocks

| Block | Purpose |
|-------|---------|
| `header_text` | `<h1>emoji Title</h1><h2>Subtitle</h2>` — matches page header style |
| `head_tags` | Link page CSS: `<link rel="stylesheet" href="/feature.css">` |
| `content` | Main page body |
| `scripts` | JS includes + inline `window.*` config vars |
| `modals` | Modal HTML — rendered outside the container div |

## Permissions matrix

When adding new permission columns, follow `008_schedules.sql` exactly:
- `ALTER TABLE` to add column with a safe default
- `UPDATE` using `WHERE rank IN (...)` — never `WHERE rank >= N`
- Populate all ranks explicitly if the default isn't right for everyone

## Frontend JS hardening

All JS files are being migrated away from `innerHTML` string injection to safe DOM construction. Work is tracked on the `js-hardening` branch, one file per session.

**Target pattern** — use `createElement` + `textContent`, never build HTML strings:
```javascript
// Safe
const card = document.createElement('div');
card.className = 'member-card';
card.textContent = member.name;   // never executes HTML
container.appendChild(card);

// For clearing + replacing children
container.replaceChildren(...items);  // or replaceChildren(singleNode)
```

**Event handling** — wire via `addEventListener`, never inline `onclick` in JS-generated markup:
```javascript
btn.addEventListener('click', () => editMember(member.id));
```

**`escapeHtml()`** — remove at the injection point when converting to `textContent`. Do not leave orphaned calls.

**Modal open/close check** — the global `.modal` class uses `display: flex` for centering. Always open modals with `modal.style.display = 'flex'`, never `'block'`. Verify this on every file during hardening.

**Progress** (branch: `js-hardening`):
| File | Status |
|------|--------|
| `static/members.js` | ✅ Done |
| `static/storm.js` | ✅ Done |
| `static/rankings.js` | ✅ Done |
| `static/vs.js` | ✅ Done |
| `static/alias-audit.js` | ✅ Done |
| `static/dyno.js` | ✅ Done |
| `static/admin.js` | ✅ Done |
| `static/settings.js` | ✅ Done |
| `static/profile.js` | ✅ Done |
| `static/schedule.js` | ✅ Done |
| `static/upload.js` | ✅ Done |
| `static/files.js` | ✅ Done |

`static/officer_command.js` — skip, already uses correct patterns.
`static/login.js` — ✅ Done (same password-rules pattern as profile.js; fixed during final sweep).

## UI feedback — never use browser dialogs

All user-facing feedback must go through the helpers in `static/global.js`. Browser-native `alert()`, `confirm()`, and `prompt()` are banned — they block the thread, ignore theming, and break automated tests.

| Need | Use |
|------|-----|
| Success/error notification | `showToast(message, type, duration)` — `type` is `'success'` (default), `'error'`, or `'info'` |
| Destructive confirmation | `await showConfirm(message, confirmLabel, title)` — returns `true`/`false` |
| Inline field validation | `setFieldError(fieldEl, message)` / `clearFieldError(fieldEl)` / `clearAllFieldErrors(formEl)` |
| Input from user | Add a dedicated `<div class="modal">` in `{{define "modals"}}` with its own form |

```javascript
// Confirmation before delete
if (!await showConfirm('Delete this entry?', 'Delete')) return;

// Toast on success
showToast('Entry deleted.');
showToast('Something went wrong.', 'error');

// Inline field error
setFieldError(document.getElementById('name-input'), 'Name is required.');
```

`showConfirm` supports a `title` parameter (third arg) for cases where the heading should differ from the body, e.g. displaying newly-created credentials.

## CSS variables — always use tokens, never hardcode colors

`styles.css` defines a semantic token layer in all four theme blocks (`:root`, `html.theme-light`, `html.theme-dark`, `@media (prefers-color-scheme: dark) html.theme-auto`). New feature CSS files must use these tokens exclusively — hardcoded hex/rgb values do not adapt to dark mode.

### Available tokens

| Token | Light value | Dark value |
|-------|-------------|------------|
| `--bg-primary` | `white` | `#1a1f2e` |
| `--bg-secondary` | `#f8f9fa` | `rgba(255,255,255,0.05)` |
| `--bg-tertiary` | `#edf2f7` | `rgba(255,255,255,0.08)` |
| `--text-primary` | `#1a202c` | `#e2e8f0` |
| `--text-secondary` | `#4a5568` | `#a0aec0` |
| `--text-muted` | `#718096` | `#718096` |
| `--border-color` | `#e2e8f0` | `rgba(255,255,255,0.1)` |
| `--color-primary` | `#667eea` | `#667eea` |
| `--color-info-bg` / `--color-info` | `#dbeafe` / `#1d4ed8` | `rgba(59,130,246,0.15)` / `#93c5fd` |
| `--color-success-bg` / `--color-success` | `#dcfce7` / `#15803d` | `rgba(34,197,94,0.15)` / `#86efac` |
| `--color-danger-bg` / `--color-danger` | `#fee2e2` / `#dc2626` | `rgba(220,53,69,0.15)` / `#f87171` |
| `--color-purple-bg` / `--color-purple` | `#ede9fe` / `#6d28d9` | `rgba(109,40,217,0.15)` / `#c4b5fd` |

### Rules

- **Backgrounds**: use `var(--bg-primary/secondary/tertiary)`. Never write `background: #fff` or `background: white`.
- **Text**: use `var(--text-primary/secondary/muted)`. Never write `color: #666` or `color: #333`.
- **Tinted boxes** (info/success/warning/error panels): use the `-bg`/text token pair, e.g. `background: var(--color-info-bg); color: var(--color-info)`.
- **Borders**: use `var(--border-color)`.
- **JS-injected styles**: same rules apply — use `var(--token)` in `element.style.cssText` strings. Never write hardcoded colors in JS.

## Running locally

```bash
go run .
```

Migrations run automatically on startup via `initDB()`.
