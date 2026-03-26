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
| `static/admin.js` | ⬜ |
| `static/settings.js` | ⬜ |
| `static/profile.js` | ⬜ |
| `static/schedule.js` | ⬜ |
| `static/upload.js` | ⬜ |
| `static/files.js` | ⬜ |

`static/officer_command.js` — skip, already uses correct patterns.

## Running locally

```bash
go run .
```

Migrations run automatically on startup via `initDB()`.
