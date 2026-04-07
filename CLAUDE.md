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
8. **Activity log** — call `logActivity` for every write operation (see section below).

## Activity logging

Every handler that creates, updates, or deletes data must call `logActivity`. The signature is:

```go
logActivity(userID int, username, action, entityType, entityName string, isSensitive bool, details ...string)
```

**Actions**: `"created"`, `"updated"`, `"deleted"`, `"archived"`, `"unarchived"`, `"imported"`, `"accepted"`, `"reset_password"`

**`isSensitive`**: `true` for user accounts, permissions matrix, settings, credentials, and invite events. These are hidden from non-admin users on the activity page.

**`details`**: optional human-readable change summary. For updates, build a field-level diff and pass it as a single joined string:

```go
var changes []string
if old.Name != new.Name {
    changes = append(changes, "name: "+old.Name+" → "+new.Name)
}
// ... other fields ...
logActivity(userID, username, "updated", "entity_type", new.Name, false, strings.Join(changes, "; "))
```

For updates, fetch the old values **before** the UPDATE/Exec call, then compare after.

**Batching**: consecutive `"created"` calls for the same `entity_type` by the same user within 15 minutes are automatically merged (count increments). All other actions always create a new row.

**`entity_type` values** (use these exact strings — they map to human labels in `activity.js`):
`member`, `alias`, `user`, `prospect`, `ally`, `agreement_type`, `train_log`, `eligibility_rule`, `oc_category`, `oc_responsibility`, `oc_assignee`, `award_type`, `awards`, `file`, `schedule`, `storm_assignments`, `storm_config`, `storm_group`, `invite`, `vs_points`, `power_records`, `permissions`, `settings`, `credentials`, `accountability_strike`, `storm_attendance`

When adding a new entity type, also add it to the `ENTITY_LABELS` (and `ENTITY_LABELS_PLURAL` if applicable) maps in `static/activity.js`.

## Known gotchas

### CSP `'unsafe-inline'` must be removed as inline config blocks are migrated
The deployed `Content-Security-Policy` header currently includes `'unsafe-inline'` in `script-src` because 13+ templates use inline `window.VAR = ...` config blocks in `{{define "scripts"}}`. This weakens XSS protection.

**When touching a template's `{{define "scripts"}}` block**, migrate any inline `window.VAR = ...` assignments to `data-*` attributes on a container element and read them from JS via `dataset`. Example:

```html
<!-- Before (in template) -->
{{define "scripts"}}
<script>window.CAN_MANAGE = {{if .CanManage}}true{{else}}false{{end}};</script>
<script src="/feature.js"></script>
{{end}}

<!-- After (in template) -->
{{define "scripts"}}
<div id="page-config" data-can-manage="{{if .CanManage}}true{{else}}false{{end}}" hidden></div>
<script src="/feature.js"></script>
{{end}}
```

```javascript
// After (in JS)
const cfg = document.getElementById('page-config').dataset;
const CAN_MANAGE = cfg.canManage === 'true';
```

Once **all** templates are migrated, remove `'unsafe-inline'` from `script-src` in `install.sh`, `update.sh`, and `Caddyfile`.

### Nav hamburger breakpoint must account for body padding
The hamburger media query threshold is **not** simply `(nav items × min item width) / 0.95`. The base `body` has `padding: 20px`, and the `@media (max-width: 1024px)` rule overrides this to `padding: 10px`. In the icon-only viewport range (≤1024px) the correct formula is:

```
container_needed = items × min_item_width          (e.g. 19 × 44px = 836px)
container = 0.95 × (viewport − 20px)              (body padding 10px each side)
viewport  = (container_needed + 19) / 0.95        (19 = 0.95 × 20)
         = 855 / 0.95 ≈ 900px
```

The current hamburger breakpoint is `@media (max-width: 900px)`. If the item count or min-width changes, recalculate using this formula. Note: users may report the wrapping threshold as ~18px higher than the CSS viewport because browser window width includes the scrollbar.

### Former/archived members have rank `'EX'`, not `'Former'`
Members removed from the alliance are stored with `rank = 'EX'`. Any query or JS filter that needs only active members must exclude this rank explicitly. Filtering by `'Former'` silently does nothing — that string does not exist in the data.

```sql
-- Wrong — 'Former' never matches anything
WHERE m.rank != 'Former'

-- Correct
WHERE m.rank != 'EX'
```

```javascript
// Wrong
members.filter(m => m.rank !== 'Former')

// Correct
members.filter(m => m.rank !== 'EX')
```

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

### Never add `class="hidden"` to a `.modal` element
`.modal` is already `display: none` by default. Adding `.hidden` (which is `display: none !important`) is redundant, but more importantly it **breaks the open logic**: `element.style.display = 'flex'` cannot override `!important` from a stylesheet rule, so the modal will silently stay hidden.

- **Open**: `modal.style.display = 'flex'`
- **Close**: `modal.style.display = ''` (clears the inline style; `.modal`'s own `display: none` takes back over)

```html
<!-- Correct — no extra hidden class needed -->
<div id="my-modal" class="modal">
```

### Tab switching: use `style.display = 'block'`, never `style.display = ''`
`styles.css` has a global rule `.tab-content { display: none }`. If you clear a tab's inline style with `style.display = ''`, the element reverts to the CSS rule and stays hidden — tabs appear broken (cursor changes, nothing happens).

Always show the active tab with an explicit value:
```javascript
// Wrong — reverts to CSS display:none
target.style.display = '';

// Correct
target.style.display = 'block';
```

Also show the **initial** active tab explicitly in `DOMContentLoaded` — CSS hides all `.tab-content` by default, so nothing is visible on load until JS sets it:
```javascript
const activeBtn = document.querySelector('.tab-btn.active');
if (activeBtn) {
    const target = document.getElementById('tab-' + activeBtn.dataset.tab);
    if (target) target.style.display = 'block';
}
```

`.tab-bar` / `.tab-btn` styles are defined in `styles.css` (globally available). No need to add them to page CSS — they work automatically on any page that uses `.tab-bar` / `.tab-btn` markup.
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

### Never use browser `alert()` or `confirm()`
`alert()` and `confirm()` block the main thread, look out of place, and vary wildly across browsers/OS. Do not add new calls to either.

**For success/error feedback** — show an inline status message near the triggering action (e.g. a `<p class="status-msg">` that you set `textContent` on and clear after a few seconds), or a non-blocking toast element.

**For destructive confirmations** — use the inline button-swap pattern: hide the Delete button, append a `"Sure?" [Yes] [No]` span in its place, and restore the original button if the user picks No. Never use `confirm()`. Example from `train.js`:

```javascript
delBtn.addEventListener('click', () => {
    delBtn.style.display = 'none';
    const confirmSpan = document.createElement('span');
    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
    const label = document.createElement('span');
    label.textContent = 'Sure?';
    label.style.fontSize = '0.85rem';
    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-danger btn-sm';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', () => doDelete(item.id));
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); delBtn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    actionsContainer.appendChild(confirmSpan);
});

Note: many existing files still use `alert()` — do not add more, and replace them when touching those files.

### Validate required CSV columns before the row loop — never silently skip
After building a `colMap` from CSV headers, check that all required columns are present **before** entering the row loop. A missing column causes every row to hit a `continue`, returning an empty result with no error — a silent failure that's very hard to debug.

```go
// Wrong — silently skips all rows if "name" column is missing
for _, row := range records[1:] {
    nameIdx, ok := colMap["name"]
    if !ok { continue }
    ...
}

// Correct — fail fast with a clear error
nameIdx, ok := colMap["name"]
if !ok {
    http.Error(w, "CSV missing required column: Member (or Name)", http.StatusBadRequest)
    return
}
for _, row := range records[1:] { ... }
```

Also map expected header aliases up front (e.g. `"member"` → `"name"`, `"day 1"` → `"monday"`) so the column check is reliable regardless of export format.

### Never leak raw errors to the client
Do not pass `err.Error()` (or any internal error string) directly to `http.Error`. Log the detail server-side with `slog.Error` and return a generic message to the client.

```go
// Wrong
http.Error(w, err.Error(), http.StatusInternalServerError)

// Correct
slog.Error("short description of what failed", "error", err)
http.Error(w, "Database error", http.StatusInternalServerError)
```

For bad-request (400) errors from JSON decode failures, use `"Invalid request body"` — no logging needed since it's a client error. Validation messages (missing fields, bad enum values) are safe to return as-is since they are written by us, not sourced from the DB or runtime.

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

**Modal open/close check** — the global `.modal` class uses `display: flex` for centering. Always open modals with `modal.style.display = 'flex'` (never `'block'`) and close with `modal.style.display = ''`. Never add `class="hidden"` to a `.modal` element — see gotcha above. Verify open/close on every file during hardening.

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
| `static/recruiting.js` | ✅ Done (written with safe patterns from the start) |

`static/officer_command.js` — skip, already uses correct patterns.
`static/login.js` — ✅ Done (same password-rules pattern as profile.js; fixed during final sweep).

## Documentation

Keep `README.md` up to date whenever a user-facing feature is added, changed, or removed. Each feature should have an entry under the appropriate `###` section in the Features block, written in the same style as existing entries (bullet points, bolded lead phrase, plain-English description of what it does and its permission model). Do not document internal implementation details — README is for end users and operators.

## Running locally

```bash
go run .
```

Migrations run automatically on startup via `initDB()`.
