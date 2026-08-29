// static/global.js - Global JavaScript for handling mobile menu, user dropdown, and logout functionality

// ---- Game-time clock (single source of truth) ----
// The game day rolls over at a FIXED 02:00 UTC (10PM EDT / 9PM EST) — a fixed UTC-2
// offset with NO daylight saving. The raw shifted Date is kept private so callers can't
// accidentally read local-zone fields on it; only derived string/number helpers are
// exposed (read via getUTC* internally). Do NOT use a DST zone like America/New_York.
(function () {
    const GAME_UTC_OFFSET_HOURS = -2;
    const gt = () => new Date(Date.now() + GAME_UTC_OFFSET_HOURS * 3600 * 1000); // PRIVATE: read UTC only
    window.gameDateStr = () => gt().toISOString().slice(0, 10);          // YYYY-MM-DD in game time
    window.gameWeekday = () => (gt().getUTCDay() + 6) % 7;               // Mon=0 … Sun=6
    window.currentVSWeekMonday = () => {                                 // Monday of the current game VS week
        const d = gt();
        d.setUTCDate(d.getUTCDate() - window.gameWeekday());
        return d.toISOString().slice(0, 10);
    };
})();

// ---- Identifier surfaces: opt out of browser translation ----
// Player names, aliases, alliance names/tags and usernames are IDENTIFIERS, not
// prose: they are typed back into the game, matched against the roster on import,
// and carried into CSV/XLSX exports. The browser translator rewrites text nodes in
// place, so a translated "Pàcha" is a name that matches nothing any more.
//
// Templates say this with translate="no" on the container. JS-built DOM has no
// such container to mark, so mark the node holding the identifier as it is built:
//
//     noTranslate(nameSpan);                       // stamp an existing element
//     row.appendChild(noTranslate(el('span', …))); // or inline, it returns the node
//
// Mark the SMALLEST element that wraps the identifier — marking a whole card or
// table also freezes its buttons, headers and empty states, which are UI prose a
// non-English reader does need translated. See CLAUDE.md -> `translate="no"` on
// identifier surfaces, not on prose.
function noTranslate(el) {
    if (el) el.translate = false;
    return el;
}
window.noTranslate = noTranslate;

// Render "<label><identifier>" into `el` -- e.g. setLabeledName(h, 'Converting: ',
// 'Pàcha'). The label stays translatable prose; only the name is fenced off. A
// plain `el.textContent = 'Converting: ' + name` would freeze both or neither.
function setLabeledName(el, label, name) {
    if (!el) return el;
    const who = noTranslate(document.createElement('span'));
    who.textContent = name;
    el.replaceChildren(document.createTextNode(label), who);
    return el;
}
window.setLabeledName = setLabeledName;

// Choices.js WRAPS the original <select> in its own markup and renders the visible
// list into that wrapper as a SIBLING of the select, not a descendant -- so
// translate="no" on the <select> never reaches the rendered items. Call this right
// after constructing a Choices control over a list of names.
function noTranslateChoices(instance) {
    if (!instance) return instance;
    const passed = instance.passedElement && instance.passedElement.element;
    noTranslate((instance.containerOuter && instance.containerOuter.element)
        || (passed && passed.closest('.choices')));
    return instance;
}
window.noTranslateChoices = noTranslateChoices;

// ---- VS Themes (single source of truth) ----
// Mon=0 … Sun=6, game-fixed for all servers. MUST keep all SEVEN entries: the schedule page
// renders a full 7-day week and calls getVSTheme on Sunday (index 6). `points` is the VS Duel
// League match-point weight for winning that day (Sunday has no duel → 0); the league only
// ever indexes day_number 1-6 (indices 0-5).
const VS_THEMES = [
    { label: 'Radar Training',     short: 'Radar',     icon: '📡', points: 1 },
    { label: 'Base Expansion',     short: 'Expand',    icon: '🏗️', points: 2 },
    { label: 'Age of Science',     short: 'Science',   icon: '🔬', points: 2 },
    { label: 'Train Heroes',       short: 'Heroes',    icon: '🦸', points: 2 },
    { label: 'Total Mobilization', short: 'Mobilize',  icon: '📦', points: 2 },
    { label: 'Enemy Buster',       short: 'Enemy',     icon: '💥', points: 4 },
    { label: 'Alliance Star',      short: 'Celebrate', icon: '⭐', points: 0 },
];

function getVSTheme(dateStr) {
    // Game time = UTC-2; VS resets on Monday game time.
    // Adding 2h to UTC gives game time; (UTCDay+6)%7 → Mon=0…Sun=6
    const d = new Date(dateStr + 'T02:00:00Z');
    return VS_THEMES[(d.getUTCDay() + 6) % 7];
}

// Fold diacritics + lowercase for accent-insensitive search: "Pàcha" → "pacha".
// Fuse's own `ignoreDiacritics` option is a no-op in fuse.js 7.0.0, so callers pre-fold
// BOTH the indexed text and the query with this before handing them to Fuse.
window.foldSearch = (s) =>
    String(s == null ? '' : s).normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase();

// ---- Join-date helpers (anchored to today's GAME date) ----
// Pure UTC date math so there's no local-timezone drift. "days ago" >= 0 = past.
window.gameDaysAgoToISO = (n) => {
    const d = new Date(window.gameDateStr() + 'T00:00:00Z');
    d.setUTCDate(d.getUTCDate() - n);
    return d.toISOString().slice(0, 10);
};
window.gameISOToDaysAgo = (iso) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(iso)) return null;
    const then = new Date(iso + 'T00:00:00Z').getTime();
    const today = new Date(window.gameDateStr() + 'T00:00:00Z').getTime();
    return Math.round((today - then) / 86400000);
};
// Format a YYYY-MM-DD date-only string as e.g. "Jun 14 2026". Built from the parts
// (not new Date(str)) so it isn't shifted a day by UTC parsing.
window.formatJoinDate = (iso) => {
    if (!iso || !/^\d{4}-\d{2}-\d{2}$/.test(iso)) return '';
    const [y, mo, d] = iso.split('-');
    return new Date(+y, +mo - 1, +d).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
};

// ---- Per-user UI preferences ----
// Currently localStorage-backed (persists per browser). The storage layer is isolated
// behind get/set so a future server-synced, per-account backend can replace it without
// touching call sites. Register new prefs + their defaults in PREF_DEFAULTS.
(function () {
    const NS = 'amPref:';
    const PREF_DEFAULTS = {
        memberSinceDisplay: 'days', // 'days' (in-alliance days) | 'date' (join date)
    };
    const read = (key) => {
        try {
            const v = localStorage.getItem(NS + key);
            return v === null ? PREF_DEFAULTS[key] : v;
        } catch (_) { return PREF_DEFAULTS[key]; }
    };
    const write = (key, value) => {
        try { localStorage.setItem(NS + key, value); } catch (_) { /* storage unavailable */ }
    };
    window.UserPrefs = { get: read, set: write, defaults: PREF_DEFAULTS };
})();

// ---- Linked "joined [N] days ago · [date]" entry widget ----
// Two-way bound: editing the days count fills the date and vice-versa, anchored to
// today's game date. Used by the member edit modal and the CSV / LastRank import rows.
// Returns { row, getISO, setISO, clear }. onChange(iso) fires on any user change.
window.buildJoinDateField = function (initialISO, onChange) {
    const row = document.createElement('span');
    row.className = 'join-date-row';
    const daysInput = document.createElement('input');
    daysInput.type = 'number';
    daysInput.min = '0';
    daysInput.className = 'form-input join-days-input';
    daysInput.placeholder = 'days';
    const dateInput = document.createElement('input');
    dateInput.type = 'text';
    dateInput.className = 'form-input join-date-input';
    dateInput.placeholder = 'YYYY-MM-DD';
    row.append(
        document.createTextNode('Joined '), daysInput,
        document.createTextNode(' days ago · '), dateInput
    );

    let fp = null;
    const setDate = (iso) => { if (fp) fp.setDate(iso || null, false); else dateInput.value = iso || ''; };
    const fire = () => { if (onChange) onChange((dateInput.value || '').trim()); };

    // User edits the days count → recompute the date (programmatic .value set on the
    // days input below never re-fires 'input', so there's no feedback loop).
    const fromDays = () => {
        const n = parseInt(daysInput.value, 10);
        if (Number.isFinite(n) && n >= 0) setDate(window.gameDaysAgoToISO(n));
        else if (daysInput.value === '') setDate('');
        fire();
    };
    const fromDate = () => {
        const iso = (dateInput.value || '').trim();
        daysInput.value = /^\d{4}-\d{2}-\d{2}$/.test(iso) ? window.gameISOToDaysAgo(iso) : '';
        fire();
    };

    if (window.flatpickr) fp = flatpickr(dateInput, { dateFormat: 'Y-m-d', allowInput: true, onChange: fromDate });
    else dateInput.addEventListener('change', fromDate);
    daysInput.addEventListener('input', fromDays);

    const setISO = (iso) => {
        setDate(iso || '');
        daysInput.value = (iso && /^\d{4}-\d{2}-\d{2}$/.test(iso)) ? window.gameISOToDaysAgo(iso) : '';
    };
    if (initialISO) setISO(initialISO);

    return { row, getISO: () => (dateInput.value || '').trim(), setISO, clear: () => setISO('') };
};

// ---- Table export (CSV + XLSX) ----

// scope: 'all' exports every row; anything else (the default) honours an active
// QuickSearch filter, so the file matches what the officer can actually see.
// Keyed on the marker QuickSearch sets, not getComputedStyle/offsetParent —
// those would also drop rows in an inactive tab and force layout per row.
function _extractTableData(tableEl, scope) {
    const skipCols = new Set();
    const rows = [];

    const ths = tableEl.querySelectorAll('thead th');
    const headers = [];
    ths.forEach((th, i) => {
        if ('noExport' in th.dataset) { skipCols.add(i); return; }
        headers.push(th.textContent.trim());
    });
    rows.push(headers);

    tableEl.querySelectorAll('tbody tr').forEach(tr => {
        if (scope !== 'all' && tr.dataset.qsHidden === '1') return;
        if (tr.hasAttribute('data-qs-empty')) return;
        const tds = tr.querySelectorAll('td');
        if (tds.length === 1 && tds[0].colSpan > 1) return;
        const cells = [];
        tds.forEach((td, i) => {
            if (skipCols.has(i)) return;
            const input = td.querySelector('input, select, textarea');
            let val;
            if (td.dataset.exportText !== undefined) {
                // Source text stamped by whatever rendered the cell -- currently
                // translate.js, whose control can swap the visible text for a
                // translation. The export must carry the original either way.
                val = td.dataset.exportText;
            } else if (input) {
                val = input.type === 'checkbox' ? (input.checked ? 'Yes' : 'No') : input.value;
            } else {
                val = td.textContent;
            }
            cells.push(val.trim());
        });
        if (cells.length) rows.push(cells);
    });

    return rows;
}

function exportTableToCSV(tableEl, filename, scope) {
    if (typeof tableEl === 'string') tableEl = document.getElementById(tableEl);
    if (!tableEl) return;

    const rows = _extractTableData(tableEl, scope);
    const csv = '﻿' + rows.map(row =>
        row.map(val => {
            const s = String(val ?? '');
            return (s.includes(',') || s.includes('"') || s.includes('\n') || s.includes('\r'))
                ? '"' + s.replace(/"/g, '""') + '"'
                : s;
        }).join(',')
    ).join('\r\n');

    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
}

function exportTableToXLSX(tableEl, filename, scope) {
    if (typeof tableEl === 'string') tableEl = document.getElementById(tableEl);
    if (!tableEl) return;
    const rows = _extractTableData(tableEl, scope);
    const ws = XLSX.utils.aoa_to_sheet(rows);
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, 'Sheet1');
    XLSX.writeFile(wb, filename);
}

// ---- Toast notifications ----
function showToast(message, type = 'success', duration = 3500) {
    const container = document.getElementById('toast-container');
    if (!container) return;
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    container.appendChild(toast);
    requestAnimationFrame(() => {
        requestAnimationFrame(() => toast.classList.add('toast-show'));
    });
    setTimeout(() => {
        toast.classList.remove('toast-show');
        toast.addEventListener('transitionend', () => toast.remove(), { once: true });
    }, duration);
}

// ---- Confirmation modal ----
function showConfirm(message, confirmLabel = 'Confirm', title = 'Are you sure?') {
    return new Promise(resolve => {
        const modal  = document.getElementById('confirm-modal');
        const msg    = document.getElementById('confirm-modal-message');
        const titleEl = document.getElementById('confirm-modal-title');
        if (titleEl) titleEl.textContent = title;
        msg.textContent = message;

        // Re-query after potential cloneNode replacements
        const freshConfirm = () => document.getElementById('confirm-modal-confirm');
        const freshCancel  = () => document.getElementById('confirm-modal-cancel');

        freshConfirm().textContent = confirmLabel;
        modal.style.display = 'flex';

        const cleanup = (result) => {
            modal.style.display = 'none';
            // Remove listeners by replacing nodes
            const c = freshConfirm();
            const x = freshCancel();
            c.replaceWith(c.cloneNode(true));
            x.replaceWith(x.cloneNode(true));
            resolve(result);
        };

        freshConfirm().addEventListener('click', () => cleanup(true),  { once: true });
        freshCancel().addEventListener('click',  () => cleanup(false), { once: true });
    });
}

// ---- Inline field validation ----
function setFieldError(fieldEl, message) {
    clearFieldError(fieldEl);
    fieldEl.classList.add('field-error');
    const err = document.createElement('span');
    err.className = 'field-error-message';
    err.textContent = message;
    fieldEl.insertAdjacentElement('afterend', err);
}

function clearFieldError(fieldEl) {
    fieldEl.classList.remove('field-error');
    const next = fieldEl.nextElementSibling;
    if (next?.classList.contains('field-error-message')) next.remove();
}

function clearAllFieldErrors(formEl) {
    formEl.querySelectorAll('.field-error').forEach(el => clearFieldError(el));
}

// ---- Remote finder (type-ahead combobox) ----
//
// One dropdown, two sources: instant local matches as you type, plus a remote
// lookup behind an explicit click. Shared by the External Alliances / VS League
// alliance pickers and the Recruiting player picker.
//
// The remote lookup is NEVER fired per keystroke. Every remote source here is the
// volunteer-run LastRank API behind a shared 1 req/sec limiter, so searching must
// stay a deliberate act — debouncing would still turn one officer typing a name
// into a burst of upstream calls.
//
// Options:
//   inputs       [HTMLElement]  fields that drive the query (last focused wins)
//   localSearch  q => [item]    synchronous, no network
//   remoteSearch async q => [item]   omit for a local-only finder
//   item         (obj, kind) => { label, meta, onPick }   kind: 'local' | 'remote'
//   guard        q => string|null    non-null blocks the remote lookup with that message
//   actionLabel  q => string
//   position     () => void      optional; called on open for custom anchoring
//   prefix       string          CSS class prefix (default 'finder')
//
// Returns { dropdown, refresh, close, runRemote } — insert `dropdown` yourself so
// the caller controls where it sits in the DOM.
function buildRemoteFinder(opts) {
    const prefix = opts.prefix || 'finder';
    const localSearch = opts.localSearch || (() => []);
    const guard = opts.guard || (() => null);
    const actionLabel = opts.actionLabel || (q => 'Look up “' + q + '” on LastRank');

    const dropdown = document.createElement('div');
    dropdown.className = prefix + '-dropdown';
    dropdown.hidden = true;

    let activeField = opts.inputs[0] || null;
    const queryVal = () => (activeField ? activeField.value : '').trim();

    const onDocDown = e => {
        if (dropdown.contains(e.target)) return;
        if (opts.inputs.some(f => f === e.target || f.contains?.(e.target))) return;
        close();
    };
    function open() {
        if (opts.position) opts.position(activeField);
        dropdown.hidden = false;
        document.addEventListener('mousedown', onDocDown);
    }
    function close() {
        dropdown.hidden = true;
        document.removeEventListener('mousedown', onDocDown);
    }

    function node(cls, text) {
        const n = document.createElement('div');
        n.className = prefix + '-' + cls;
        n.textContent = text;
        return n;
    }

    function entry(obj, kind) {
        const spec = opts.item(obj, kind);
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = prefix + '-item';
        const nameEl = document.createElement('span');
        nameEl.className = prefix + '-name';
        nameEl.textContent = spec.label;
        noTranslate(nameEl);   // a player/alliance name, not prose
        btn.appendChild(nameEl);
        if (spec.meta) {
            const metaEl = document.createElement('span');
            metaEl.className = prefix + '-meta';
            metaEl.textContent = spec.meta;
            btn.appendChild(metaEl);
        }
        btn.addEventListener('click', () => { close(); spec.onPick(obj); });
        return btn;
    }

    function render(locals, remotes, msg, isError, showAction) {
        dropdown.replaceChildren();
        if (locals && locals.length) {
            if (opts.localHead) dropdown.appendChild(node('head', opts.localHead));
            locals.forEach(x => dropdown.appendChild(entry(x, 'local')));
        }
        if (remotes && remotes.length) {
            if (opts.remoteHead) dropdown.appendChild(node('head', opts.remoteHead));
            remotes.forEach(x => dropdown.appendChild(entry(x, 'remote')));
        }
        if (msg) {
            const m = node('msg', msg);
            if (isError) m.classList.add(prefix + '-err');
            dropdown.appendChild(m);
        }
        if (showAction && opts.remoteSearch) {
            const act = document.createElement('button');
            act.type = 'button';
            act.className = prefix + '-action';
            act.textContent = '🔎 ' + actionLabel(queryVal());
            act.addEventListener('click', runRemote);
            dropdown.appendChild(act);
        }
        if (dropdown.childNodes.length) open(); else close();
    }

    async function runRemote() {
        const q = queryVal();
        if (!q || !opts.remoteSearch) return;
        const blocked = guard(q);
        if (blocked) { render(localSearch(q), null, blocked, true, false); return; }
        render(localSearch(q), null, 'Searching…', false, false);
        try {
            const list = await opts.remoteSearch(q);
            render(localSearch(q), list, (list && list.length) ? null : 'No matches found.', false, false);
        } catch (e) {
            render(localSearch(q), null, e.message || 'Search failed.', true, false);
        }
    }

    function refresh() {
        const q = queryVal();
        if (!q) { close(); return; }
        render(localSearch(q), null, null, false, true);
    }

    opts.inputs.forEach(f => {
        f.addEventListener('focus', () => { activeField = f; });
        f.addEventListener('input', () => { activeField = f; refresh(); });
        f.addEventListener('keydown', e => {
            if (e.key === 'Escape') close();
            else if (e.key === 'Enter' && !dropdown.hidden) { e.preventDefault(); runRemote(); }
        });
    });

    return { dropdown, refresh, close, runRemote };
}

// ---- Button loading state ----
function setButtonLoading(btn, loadingText = 'Saving…') {
    btn.disabled = true;
    btn._originalText = btn.textContent;
    btn.textContent = loadingText;
}

function clearButtonLoading(btn) {
    btn.disabled = false;
    btn.textContent = btn._originalText ?? btn.textContent;
}

// ---- Clipboard ----
// navigator.clipboard is unavailable on insecure origins (plain-HTTP LAN access is a
// normal way this app is reached), so fall back to a hidden textarea + execCommand.
function fallbackCopy(text, onSuccess) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;top:-9999px;left:-9999px;opacity:0;';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    try {
        if (document.execCommand('copy')) onSuccess();
    } finally {
        document.body.removeChild(ta);
    }
}

function copyToClipboard(text, onSuccess) {
    if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(onSuccess).catch(() => fallbackCopy(text, onSuccess));
    } else {
        fallbackCopy(text, onSuccess);
    }
}

function svgIcon(name, size = 14) {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('width', size);
    svg.setAttribute('height', size);
    svg.setAttribute('aria-hidden', 'true');
    svg.setAttribute('class', 'svg-icon');
    const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
    use.setAttribute('href', `/icons.svg#icon-${name}`);
    svg.appendChild(use);
    return svg;
}

// ---- Quick search (shared list filter) ------------------------------------
//
// ONE searchable-list primitive for every member list in the app. Two modes:
//
//   hide (default) — toggles row visibility in place, leaving the DOM alone.
//        MANDATORY for any list with editable cells or transient DOM state.
//        Re-rendering a table on every keystroke throws away unsaved input; where
//        the values arrive from a fetch it also fires one request per keystroke
//        (season-hub's Contributions grid did both — see renderManualTable).
//   custom — pages that legitimately rebuild from their data array pass onQuery
//        and use QuickSearch.filter/matcher themselves. Safe only when nothing in
//        a row holds state the data array doesn't already have.
//
// Matching folds diacritics through foldSearch and ANDs whitespace-separated
// tokens, so "pàcha r4" matches a row whose data-search is "Pacha R4", and a
// pasted " Pacha " still matches. NOTE: foldSearch deliberately does NOT trim —
// it has to stay byte-for-byte in step with Go's foldName (namematch.go), which
// does. Trimming belongs here, at the call site, not in foldSearch.
//
// Match text comes from row.dataset.search. That is an ATTRIBUTE, so the
// browser's translator never rewrites it, and it holds the SOURCE name rather
// than the row's rendered text — which on most of our rows also contains rank
// chips, point totals and button labels, so matching textContent would make "e"
// match every row carrying an Edit button. opts.text is the escape hatch;
// textContent is the last-resort default.
(function () {
    'use strict';

    const node = (t) => (typeof t === 'string' ? document.getElementById(t) : t);

    const tokens = (q) => window.foldSearch(q).split(/\s+/).filter(Boolean);

    // Pure predicate — for pages that filter their own data array.
    function match(text, query) {
        const toks = tokens(query);
        if (!toks.length) return true;
        const hay = window.foldSearch(text);
        return toks.every(t => hay.includes(t));
    }

    // Same, with the query folded once for a whole pass.
    //   const ok = QuickSearch.matcher(q); list.filter(x => ok(x.name));
    function matcher(query) {
        const toks = tokens(query);
        if (!toks.length) return () => true;
        return (text) => {
            const hay = window.foldSearch(text);
            return toks.every(t => hay.includes(t));
        };
    }

    // Array convenience. `pick` returns the haystack for one item; join several
    // fields with ' ' to search across them.
    function filter(list, query, pick) {
        const ok = matcher(query);
        const get = pick || (x => (x && x.name) || '');
        return (list || []).filter(x => ok(get(x)));
    }

    // Folded key, memoized per element: recomputed only when the source text
    // actually changes, so a 100-row table costs 100 string compares per
    // keystroke instead of 100 NFD normalizations.
    function rowKey(row, text) {
        const raw = text ? text(row)
            : (row.dataset.search != null ? row.dataset.search : row.textContent);
        if (row._qsRaw !== raw) {
            row._qsRaw = raw;
            row._qsKey = window.foldSearch(raw);
        }
        return row._qsKey;
    }

    // A list's own placeholder ("Loading…", "No rewards assigned yet.") is a
    // single spanning cell — the same shape _extractTableData skips. It is the
    // table's state, not data, so it is never filtered and never counted.
    function isPlaceholder(row) {
        if (row.tagName !== 'TR') return false;
        return row.children.length === 1
            && row.children[0].tagName === 'TD'
            && row.children[0].colSpan > 1;
    }

    function buildEmpty(container, opts) {
        const msg = opts.emptyText || 'No matches.';
        const rowGroup = /^(TBODY|THEAD|TFOOT|TABLE)$/.test(container.tagName);
        let el;
        if (rowGroup) {
            const table = container.closest('table');
            el = document.createElement('tr');
            const td = document.createElement('td');
            // colSpan >= 2 so _extractTableData's placeholder rule skips it too.
            td.colSpan = Math.max(2, opts.colspan
                || (table ? table.querySelectorAll('thead th').length : 0));
            td.className = 'empty-state qs-empty-cell';
            td.textContent = msg;
            el.appendChild(td);
        } else {
            el = document.createElement('p');
            el.className = 'empty-state qs-empty';
            el.textContent = msg;
        }
        el.setAttribute('data-qs-empty', '');
        return el;
    }

    // Build the standard widget for lists whose toolbar is constructed in JS.
    // Same markup the templates hand-write.
    function widget(opts) {
        opts = opts || {};
        const wrap = document.createElement('div');
        wrap.className = 'filter-search-wrap quick-search';

        const input = document.createElement('input');
        input.type = 'text';   // not "search" — that adds a second native clear x
        input.className = 'form-input';
        input.autocomplete = 'off';
        input.placeholder = opts.placeholder || 'Search members…';
        input.setAttribute('aria-label', opts.label || input.placeholder);
        if (opts.id) input.id = opts.id;

        const clearBtn = document.createElement('button');
        clearBtn.type = 'button';
        clearBtn.style.display = 'none';
        clearBtn.setAttribute('aria-label', 'Clear search');
        clearBtn.appendChild(svgIcon('x', 14));

        wrap.append(input, clearBtn);
        return { wrap, input, clearBtn };
    }

    // Stamp shown/total on the nearest ancestor table and let the export wire
    // know. Keeps QuickSearch ignorant of the export UI: it publishes counts,
    // the export wire decides what to do with them.
    function publish(container, shown, total, filtered) {
        const table = container.closest ? container.closest('table') : null;
        if (!table) return;
        table.dataset.qsShown = String(shown);
        table.dataset.qsTotal = String(total);
        table.dataset.qsFiltered = filtered ? '1' : '0';
        table.dispatchEvent(new CustomEvent('quicksearch:filtered', {
            bubbles: true,
            detail: { shown, total, filtered },
        }));
    }

    // attach(opts) -> handle
    //
    //   input      element | id                (required)
    //   clearBtn   element | id                (default: the button inside the
    //                                           enclosing .filter-search-wrap)
    //   container  element | id | () => el     the STABLE ancestor of the rows.
    //                                          Never the table — three targets
    //                                          build their table in JS with no id.
    //   rows       selector, relative to container (default 'tbody > tr')
    //   text       (row) => string             override for data-search
    //   groups     selector for section wrappers to collapse when empty
    //   count      element | id                gets "N of M"
    //   countLabel (shown, total, raw) => string
    //   emptyText  string | false              in-list "no matches" node
    //   colspan    number                      override for the empty <tr>
    //   initial    string                      seed the box (re-created inputs)
    //   onFilter   (shown, total, raw)         fires after every hide pass
    //   onQuery    (raw, handle)               CUSTOM MODE: page filters+renders
    //
    // handle: { apply, clear, value, input, destroy }
    function attach(opts) {
        const input = node(opts.input);
        if (!input) return null;

        const wrap = input.closest('.filter-search-wrap');
        const clearBtn = opts.clearBtn ? node(opts.clearBtn)
            : (wrap ? wrap.querySelector('button') : null);
        const countEl = opts.count ? node(opts.count) : null;
        const rowsSel = opts.rows || 'tbody > tr';

        const container = () =>
            (typeof opts.container === 'function' ? opts.container() : node(opts.container));

        function hidePass() {
            const c = container();
            if (!c) return;
            const toks = tokens(input.value);
            let shown = 0, total = 0;

            c.querySelectorAll(rowsSel).forEach(row => {
                if (row.hasAttribute('data-qs-empty') || isPlaceholder(row)) return;
                total++;
                const ok = !toks.length
                    || toks.every(t => rowKey(row, opts.text).includes(t));
                // '' (not 'block'/'table-row') so the stylesheet's own display —
                // grid for .lr-row, flex for .prospect-card — takes back over.
                row.style.display = ok ? '' : 'none';
                if (ok) { delete row.dataset.qsHidden; shown++; }
                else { row.dataset.qsHidden = '1'; }
            });

            if (opts.groups) {
                c.querySelectorAll(opts.groups).forEach(g => {
                    const any = Array.from(g.querySelectorAll(rowsSel)).some(r =>
                        r.dataset.qsHidden !== '1'
                        && !r.hasAttribute('data-qs-empty') && !isPlaceholder(r));
                    g.style.display = (!toks.length || any) ? '' : 'none';
                });
            }

            if (opts.emptyText !== false) {
                const want = toks.length > 0 && shown === 0 && total > 0;
                let e = c.querySelector('[data-qs-empty]');
                if (want && !e) { e = buildEmpty(c, opts); c.appendChild(e); }
                if (e) e.style.display = want ? '' : 'none';
            }

            if (countEl) {
                countEl.textContent = opts.countLabel
                    ? opts.countLabel(shown, total, input.value)
                    : (toks.length ? shown + ' of ' + total : String(total));
            }
            publish(c, shown, total, toks.length > 0 && shown < total);
            if (opts.onFilter) opts.onFilter(shown, total, input.value);
        }

        function run() {
            if (opts.onQuery) opts.onQuery(input.value, handle);
            else hidePass();
        }

        function syncClear() {
            if (clearBtn) clearBtn.style.display = input.value ? 'flex' : 'none';
        }
        const onInput = () => { syncClear(); run(); };
        function onClear() { input.value = ''; syncClear(); run(); input.focus(); }
        // Escape clears the box first. modal-focus.js's trap listens on the modal
        // in the bubble phase, so stopping propagation here means Escape clears a
        // non-empty search and only the SECOND Escape closes the modal.
        const onKey = (e) => {
            if (e.key === 'Escape' && input.value) { e.stopPropagation(); onClear(); }
        };

        input.addEventListener('input', onInput);
        input.addEventListener('keydown', onKey);
        if (clearBtn) clearBtn.addEventListener('click', onClear);

        const handle = {
            apply: run,
            clear: onClear,
            value: () => input.value,
            input,
            destroy() {
                input.removeEventListener('input', onInput);
                input.removeEventListener('keydown', onKey);
                if (clearBtn) clearBtn.removeEventListener('click', onClear);
                delete input._quickSearch;
            },
        };
        input._quickSearch = handle;

        if (opts.initial) input.value = opts.initial;
        syncClear();
        if (opts.autoApply !== false) run();
        return handle;
    }

    // Re-apply the active query for an input whose handle you don't hold. Lets a
    // render fn a thousand lines from the attach site stay one self-documenting
    // line:  QuickSearch.apply('participation-search');
    function apply(target) {
        const el = node(target);
        if (el && el._quickSearch) el._quickSearch.apply();
    }

    window.QuickSearch = { attach, apply, widget, match, matcher, filter };
})();

// Swap the sidebar user tile's initials for the logged-in user's LastRank photo
// when present. Falls back to initials if the photo (and failover) fail to load
// or are blocked by CSP, so production stays safe before the CDN is allowlisted.
document.addEventListener('DOMContentLoaded', () => {
    const su = document.querySelector('.sidebar-user-avatar');
    if (!su || !su.dataset.lrPhoto) return;
    const failover = su.dataset.lrPhotoFailover || '';
    const initials = su.textContent.trim();
    const img = document.createElement('img');
    img.className = 'sidebar-user-photo';
    img.alt = '';
    let triedFailover = false;
    img.addEventListener('error', () => {
        if (!triedFailover && failover) {
            triedFailover = true;
            img.src = failover;
        } else {
            su.textContent = initials; // restore initials on total failure
        }
    });
    img.src = su.dataset.lrPhoto;
    su.textContent = '';
    su.appendChild(img);
});

// LastRank avatar <img>, hotlinked from the game CDN. Falls over to the backup
// CDN host on the first load error, then removes itself if that also fails (so a
// blocked/dead image leaves no broken-icon artifact). No inline onerror — the
// handler is attached here so it stays CSP-safe once the CDN hosts are allowlisted.
function buildLastRankAvatar(primary, failover) {
    const img = document.createElement('img');
    img.className = 'lr-avatar';
    img.alt = '';
    img.loading = 'lazy';
    img.src = primary;
    let triedFailover = false;
    img.addEventListener('error', () => {
        if (!triedFailover && failover) {
            triedFailover = true;
            img.src = failover;
        } else {
            img.remove();
        }
    });
    return img;
}

// Table-row action button: SVG icon + a label that collapses to icon-only on
// narrow screens (.action-label is hidden ≤768px). title/aria-label keep the
// action discoverable when the text is hidden. Mirrors members.js
// memberActionBtn() for card actions. Wrap a row's buttons in a .row-actions
// container so they stay on one line.
function rowActionBtn(className, icon, label, onClick) {
    const btn = document.createElement('button');
    btn.className = className;
    btn.title = label;
    btn.setAttribute('aria-label', label);
    const span = document.createElement('span');
    span.className = 'action-label';
    span.textContent = label;
    btn.append(svgIcon(icon, 14), span);
    if (onClick) btn.addEventListener('click', onClick);
    return btn;
}

// Scroll affordance for horizontally-scrollable tab bars (mobile). Wraps each
// .tab-bar in a positioned container and shows a fading chevron at whichever
// edge has more tabs to scroll to. Self-disabling: when the bar isn't
// overflowing (e.g. desktop) neither cue shows. No template changes needed; no
// JS reads .tab-bar directly, so reparenting it is safe.
function setupTabScrollCues() {
    document.querySelectorAll('.tab-bar').forEach(bar => {
        if (bar.closest('.tab-bar-scroll')) return; // already wrapped

        const wrap = document.createElement('div');
        wrap.className = 'tab-bar-scroll';
        bar.parentNode.insertBefore(wrap, bar);
        wrap.appendChild(bar);

        const left = document.createElement('span');
        left.className = 'tab-scroll-cue tab-scroll-cue-left';
        left.setAttribute('aria-hidden', 'true');
        left.appendChild(svgIcon('chevron-left', 18));

        const right = document.createElement('span');
        right.className = 'tab-scroll-cue tab-scroll-cue-right';
        right.setAttribute('aria-hidden', 'true');
        right.appendChild(svgIcon('chevron-right', 18));

        wrap.append(left, right);

        const update = () => {
            const max = bar.scrollWidth - bar.clientWidth;
            wrap.classList.toggle('can-scroll-left', bar.scrollLeft > 1);
            wrap.classList.toggle('can-scroll-right', bar.scrollLeft < max - 1);
        };
        bar.addEventListener('scroll', update, { passive: true });
        window.addEventListener('resize', update);
        update();
    });
}

document.addEventListener('DOMContentLoaded', () => {
    setupTabScrollCues();
    const usernameDisplay = document.getElementById('username-display');
    // Toggle user dropdown menu
    if (usernameDisplay) {
        usernameDisplay.addEventListener('click', (event) => {
            event.stopPropagation();
            const dropdown = document.getElementById('user-dropdown-menu');
            if (dropdown) dropdown.classList.toggle('show');
        });
    }
    
    // Close dropdown when clicking outside
    document.addEventListener('click', (event) => {
        const dropdown = document.getElementById('user-dropdown-menu');
        if (dropdown && usernameDisplay && !usernameDisplay.contains(event.target) && !dropdown.contains(event.target)) {
            dropdown.classList.remove('show');
        }
    });

    // Handle Logout — class-based so both sidebar dropdown and more-sheet share the same handler
    document.querySelectorAll('.logout-btn').forEach(btn => {
        btn.addEventListener('click', async (event) => {
            event.preventDefault();
            if (!await showConfirm('Are you sure you want to logout?', 'Logout')) return;

            try {
                const response = await fetch('/api/logout', { method: 'POST' });
                if (!response.ok) {
                    throw new Error(`Server rejected logout: ${response.status} ${response.statusText}`);
                }
                window.location.href = '/login';
            } catch (error) {
                console.error('Logout failed:', error);
                showToast('Logout failed. Check the browser console for details.', 'error');
            }
        });
    });

    // More sheet open/close
    const moreSheet = document.getElementById('more-sheet');
    if (moreSheet) {
        document.getElementById('more-tab-btn')?.addEventListener('click', () => {
            moreSheet.style.display = 'block';
        });
        document.getElementById('mobile-menu-btn')?.addEventListener('click', () => {
            moreSheet.style.display = 'block';
        });
        document.getElementById('more-sheet-close-btn')?.addEventListener('click', () => {
            moreSheet.style.display = '';
        });
    }

    // Auto-wire CSV + XLSX export buttons for tables with data-export-csv attribute
    document.querySelectorAll('table[data-export-csv]').forEach(table => {
        const csvFilename  = table.dataset.exportCsv;
        const xlsxFilename = csvFilename.replace(/\.csv$/i, '.xlsx');

        // Export scope. The checkbox appears only while a QuickSearch filter is
        // actually narrowing this table, so unfiltered tables — and tables with no
        // search at all — look exactly as they did before. Checked = export what
        // you see, which is the pre-existing behaviour of every filtered export.
        const scopeLabel = document.createElement('label');
        scopeLabel.className = 'export-scope';
        const scopeBox = document.createElement('input');
        scopeBox.type = 'checkbox';
        scopeBox.checked = true;
        scopeLabel.append(scopeBox, document.createTextNode('Filtered only'));
        const scope = () => (scopeBox.checked ? 'visible' : 'all');

        const csvBtn = document.createElement('button');
        csvBtn.className = 'btn btn-secondary btn-sm';
        csvBtn.textContent = '↓ CSV';
        csvBtn.title = 'Download as CSV';
        csvBtn.addEventListener('click', () => exportTableToCSV(table, csvFilename, scope()));

        const xlsxBtn = document.createElement('button');
        xlsxBtn.className = 'btn btn-secondary btn-sm';
        xlsxBtn.textContent = '↓ XLSX';
        xlsxBtn.title = 'Download as Excel spreadsheet';
        xlsxBtn.addEventListener('click', () => exportTableToXLSX(table, xlsxFilename, scope()));

        // Reflect the row count in the labels while filtered, so the choice can't
        // surprise anyone: "↓ CSV 12" vs "↓ CSV 98".
        function syncScopeUI() {
            const filtered = table.dataset.qsFiltered === '1';
            scopeLabel.classList.toggle('is-visible', filtered);
            if (!filtered) {
                csvBtn.textContent = '↓ CSV';
                xlsxBtn.textContent = '↓ XLSX';
                return;
            }
            const n = scopeBox.checked ? table.dataset.qsShown : table.dataset.qsTotal;
            csvBtn.textContent = '↓ CSV ' + n;
            xlsxBtn.textContent = '↓ XLSX ' + n;
        }
        table.addEventListener('quicksearch:filtered', syncScopeUI);
        scopeBox.addEventListener('change', syncScopeUI);

        // Find nearest preceding .tab-toolbar, searching up through ancestors
        let toolbar = null;
        let cur = table;
        outer: while (cur && cur !== document.body) {
            let prev = cur.previousElementSibling;
            while (prev) {
                if (prev.classList.contains('tab-toolbar')) { toolbar = prev; break outer; }
                prev = prev.previousElementSibling;
            }
            cur = cur.parentElement;
        }

        // Group the export buttons in a right-aligned wrapper so they sit at
        // the end of the toolbar regardless of the toolbar's other content.
        const exportActions = document.createElement('div');
        exportActions.className = 'table-export-actions';
        exportActions.append(scopeLabel, csvBtn, xlsxBtn);

        if (toolbar) {
            toolbar.appendChild(exportActions);
        } else {
            const wrap = document.createElement('div');
            wrap.className = 'tab-toolbar';
            wrap.appendChild(exportActions);
            table.parentNode.insertBefore(wrap, table);
        }
    });
});



// ---- LastRank review nudge ----
//
// The review queue is filled by the manual sync AND, once the scheduler is on, by
// runs nobody watched. A queue nobody is told about is a queue nobody works, so
// permissioned officers get one nudge per session.
//
// Once per SESSION, not per page load: this is a reminder, not an alarm. It is also
// deliberately cheap — one request, and only for users who could act on the answer.
(function () {
    const cfg = document.getElementById('layout-config');
    if (!cfg || cfg.dataset.canManageMembers !== 'true') return;

    const FLAG = 'lastrank-review-nudged';
    try {
        if (sessionStorage.getItem(FLAG)) return;
    } catch (e) {
        return; // storage blocked — better silent than nagging every page
    }

    // After first paint: the nudge must never compete with the page loading.
    window.addEventListener('load', () => {
        setTimeout(async () => {
            try {
                const res = await fetch('/api/lastrank/review/summary');
                if (!res.ok) return;
                const d = await res.json();
                // Mark as shown even at zero, so a session gets at most one request.
                sessionStorage.setItem(FLAG, '1');
                const n = d.open_count || 0;
                if (n === 0) return;
                showToast(
                    n === 1
                        ? '1 LastRank change is waiting for review.'
                        : `${n} LastRank changes are waiting for review.`,
                    'info', 8000);
            } catch (e) { /* never let a nudge break the page */ }
        }, 1200);
    });
})();
