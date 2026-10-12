// table-utils.js — the shared table and list utilities: QuickSearch (the list filter) and
// table export (CSV + XLSX, with per-table export options).
//
// They live together because they depend on each other: the export reads the qsHidden /
// qsFiltered markers QuickSearch writes, so a filtered table exports what the officer sees.
// Not in global.js (see CLAUDE.md -> "What belongs in global.js"): 15 of the layout pages use
// them, and this is the code that changes with export features, which shouldn't invalidate the
// cached global.js for every page.
//
// Load AFTER global.js (svgIcon, showToast, UserPrefs, formatCompact) and BEFORE the page
// script; link table-utils.css first in head_tags. build-check.yml's "Opt-in module dependency
// check" enforces both. XLSX also needs the per-template SheetJS tag ("XLSX export dependency
// check").

'use strict';

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
    // single spanning cell — the same shape the export skips. It is the
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
            // colSpan >= 2 so the export's placeholder rule skips it too.
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

// ---- Table export (CSV + XLSX) and export options --------------------------
//
// One button group per export, built by TableExport.attach:
//   { table }                       a real table — the data-export-csv auto-wire's path
//   { bar, getRows, build }         a container plus a detached-table builder (Members,
//                                   Recruiting), for lists that aren't tables on screen
//
// Numbers: RAW by default — typed numbers, so a spreadsheet can sort and sum. A cell whose
// text isn't the raw value (formatCompact's "4.2B") carries data-export-value="<raw>";
// otherwise a plain or comma-grouped numeral is parsed, the null glyph "—" exports empty, and
// everything else stays text. "As shown" writes the text exactly as on screen.
// Columns: keyed by th[data-export-key], else the header text. A header whose text changes at
// run time MUST carry data-export-key, or an excluded column comes back.
(function () {
    'use strict';

    // Plain or comma-grouped, optional sign and decimals, no leading zeros ("007" stays text).
    const NUMERAL = /^[+-]?(?:0|[1-9]\d{0,2}(?:,\d{3})+|[1-9]\d*)(?:\.\d+)?$/;
    const NULL_GLYPH = '—';

    const colKey = th => (th.dataset.exportKey || th.textContent.trim());

    // Keyed on the marker QuickSearch sets, not getComputedStyle/offsetParent — those would also
    // drop rows in an inactive tab and force layout per row.
    function cellText(td) {
        const input = td.querySelector('input, select, textarea');
        let val;
        if (td.dataset.exportText !== undefined) {
            // Source text stamped by whatever rendered the cell (translate.js, whose control can
            // swap the visible text for a translation; an identifier the B/G swap could touch).
            // The export must carry the original either way.
            val = td.dataset.exportText;
        } else if (input) {
            val = input.type === 'checkbox' ? (input.checked ? 'Yes' : 'No') : input.value;
        } else {
            val = td.textContent;
        }
        return String(val).trim();
    }

    function rawValue(td, text) {
        if (td.dataset.exportValue !== undefined) {
            const v = td.dataset.exportValue.trim();
            if (v === '') return '';
            const n = Number(v);
            return Number.isFinite(n) ? n : v;
        }
        if (text === NULL_GLYPH) return '';
        if (NUMERAL.test(text)) return Number(text.replace(/,/g, ''));
        return text;
    }

    // [headers, ...rows]. scope 'all' includes rows QuickSearch has hidden; numbers 'shown'
    // keeps the on-screen text; exclude is a list of column keys.
    function extract(tableEl, opts) {
        opts = opts || {};
        const raw = opts.numbers !== 'shown';
        const exclude = new Set(opts.exclude || []);
        const skip = new Set();
        const headers = [];
        tableEl.querySelectorAll('thead th').forEach((th, i) => {
            if ('noExport' in th.dataset || exclude.has(colKey(th))) { skip.add(i); return; }
            headers.push(th.textContent.trim());
        });
        const rows = [headers];
        tableEl.querySelectorAll('tbody tr').forEach(tr => {
            if (opts.scope !== 'all' && tr.dataset.qsHidden === '1') return;
            if (tr.hasAttribute('data-qs-empty')) return;
            const tds = tr.querySelectorAll('td');
            if (tds.length === 1 && tds[0].colSpan > 1) return;
            const cells = [];
            tds.forEach((td, i) => {
                if (skip.has(i)) return;
                const text = cellText(td);
                cells.push(raw ? rawValue(td, text) : text);
            });
            if (cells.length) rows.push(cells);
        });
        return rows;
    }

    // One CSV field. A TEXT field that begins with = + - @ TAB or CR gets a leading ' so a
    // spreadsheet opens it as text, not a formula (OWASP CSV injection). A typed number is
    // never prefixed, so raw -2500000 stays a number, and nor is a lone + or - (Rankings'
    // "no change" mark), which can't be a formula. polls.js's writer uses this too.
    function csvField(v) {
        if (typeof v === 'number') return String(v);
        let s = String(v == null ? '' : v);
        if (/^[=@\t\r]|^[+-]./.test(s)) s = "'" + s;
        return (s.includes(',') || s.includes('"') || s.includes('\n') || s.includes('\r'))
            ? '"' + s.replace(/"/g, '""') + '"'
            : s;
    }

    function download(blob, filename) {
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    }

    function csv(tableEl, filename, opts) {
        if (typeof tableEl === 'string') tableEl = document.getElementById(tableEl);
        if (!tableEl) return;
        const rows = extract(tableEl, opts);
        // The BOM is deliberate: it's what makes Excel read a UTF-8 file without mangling
        // accented names (standing-decisions.md, "CSV exports deliberately begin with a BOM").
        const text = '﻿' + rows.map(r => r.map(csvField).join(',')).join('\r\n');
        download(new Blob([text], { type: 'text/csv;charset=utf-8;' }), filename);
    }

    function xlsx(tableEl, filename, opts) {
        if (typeof tableEl === 'string') tableEl = document.getElementById(tableEl);
        if (!tableEl) return;
        // SheetJS is a per-template CDN script. A page that adds an exportable table without it
        // would otherwise throw a bare ReferenceError and look like a dead button.
        if (typeof XLSX === 'undefined') {
            console.error('TableExport.xlsx: SheetJS not loaded. Add the xlsx.mini.min.js script tag to this page template.');
            showToast('Excel export is unavailable on this page.', 'error');
            return;
        }
        const ws = XLSX.utils.aoa_to_sheet(extract(tableEl, opts));   // numbers stay numbers
        const wb = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(wb, ws, 'Sheet1');
        XLSX.writeFile(wb, filename);
    }

    // ---- Settings: the ONLY storage touchpoint ----
    // Per browser, through UserPrefs (localStorage today, built to become a per-account server
    // setting later — that swap changes UserPrefs or this pair, not the export code or its UI).
    const DEFAULTS = { numbers: 'raw', exclude: [] };
    function readAll() {
        try {
            const v = JSON.parse((window.UserPrefs && UserPrefs.get('exportSettings')) || '{}');
            return v && typeof v === 'object' && !Array.isArray(v) ? v : {};
        } catch (_) { return {}; }
    }
    const settings = {
        load(key) {
            const s = readAll()[key] || {};
            return {
                numbers: s.numbers === 'shown' ? 'shown' : 'raw',
                exclude: Array.isArray(s.exclude) ? s.exclude.filter(k => typeof k === 'string') : [],
            };
        },
        // Re-reads at save time, so a save made in another tab since this page loaded survives.
        save(key, value) {
            if (!window.UserPrefs) return;
            const all = readAll();
            const numbers = value && value.numbers === 'shown' ? 'shown' : 'raw';
            const exclude = value && Array.isArray(value.exclude) ? value.exclude : [];
            if (numbers === DEFAULTS.numbers && !exclude.length) delete all[key];
            else all[key] = { numbers, exclude };
            UserPrefs.set('exportSettings', JSON.stringify(all));
        },
    };
    const isCustom = (s, rawOnly) => (!rawOnly && s.numbers !== 'raw') || s.exclude.length > 0;

    // ---- The Options dialog (one per page, built on first use, on <body>) ----
    // On <body> because a .modal inside .container sits in a stacking context that breaks the
    // fixed overlay (CLAUDE.md -> "Modals must go in {{define "modals"}}").
    let dialog = null;
    function ensureDialog() {
        if (dialog) return dialog;
        const overlay = document.createElement('div');
        overlay.className = 'modal export-options-modal';
        overlay.setAttribute('role', 'dialog');
        overlay.setAttribute('aria-modal', 'true');
        overlay.setAttribute('aria-labelledby', 'export-options-title');
        const content = document.createElement('div');
        content.className = 'modal-content modal-sm';
        overlay.appendChild(content);
        document.body.appendChild(overlay);
        dialog = { overlay, content, opener: null };
        // Idempotent: Escape runs modal-focus.js's own close first, then this one.
        const close = () => {
            overlay.style.display = '';
            if (typeof releaseFocus === 'function') releaseFocus(overlay);
            if (dialog.opener && document.contains(dialog.opener)) dialog.opener.focus();
            dialog.opener = null;
        };
        dialog.close = close;
        overlay.addEventListener('click', e => { if (e.target === overlay) close(); });
        // modal-focus.js's trap closes on Escape itself; this runs after it to return focus.
        overlay.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
        return dialog;
    }

    function el(tag, cls, text) {
        const n = document.createElement(tag);
        if (cls) n.className = cls;
        if (text != null) n.textContent = text;
        return n;
    }

    function openOptions(ctx) {
        const d = ensureDialog();
        const s = ctx.current();
        d.content.replaceChildren();
        const title = el('h2', null, 'Export options');
        title.id = 'export-options-title';
        d.content.append(title, el('p', 'export-options-for', ctx.filename + '.csv / .xlsx'));

        let radios = null;
        if (ctx.rawOnly) {
            d.content.appendChild(el('p', 'export-options-note',
                'This export keeps raw numbers, so its numbers re-import cleanly.'));
        } else {
            const fs = el('fieldset', 'export-options-group');
            fs.appendChild(el('legend', null, 'Numbers'));
            const group = el('div', 'radio-group');
            radios = [['raw', 'Raw numbers (sort and sum in a spreadsheet)'],
                      ['shown', 'As shown on screen (' + formatCompact(4200000000) + ', 1,234)']].map(([v, label]) => {
                const l = el('label', 'radio-label');
                const r = document.createElement('input');
                r.type = 'radio'; r.name = 'export-numbers'; r.value = v; r.checked = s.numbers === v;
                l.append(r, document.createTextNode(label));
                group.appendChild(l);
                return r;
            });
            fs.appendChild(group);
            d.content.appendChild(fs);
        }

        const cols = el('fieldset', 'export-options-group');
        cols.appendChild(el('legend', null, 'Columns'));
        const list = el('div', 'export-options-columns');
        const boxes = [];
        const seen = new Set();
        const ht = ctx.headerTable();
        (ht ? Array.from(ht.querySelectorAll('thead th')) : []).forEach(th => {
            const key = colKey(th);
            if ('noExport' in th.dataset || !key || seen.has(key)) return;
            seen.add(key);
            const l = el('label', 'checkbox-label');
            const c = document.createElement('input');
            c.type = 'checkbox'; c.value = key; c.checked = !s.exclude.includes(key);
            const name = el('span', null, th.textContent.trim() || key);
            l.append(c, name);
            list.appendChild(l);
            boxes.push(c);
        });
        cols.appendChild(list);
        const hint = el('p', 'export-options-hint', 'Choose at least one column.');
        cols.appendChild(hint);
        d.content.appendChild(cols);

        const save = el('button', 'btn btn-primary', 'Save');
        const reset = el('button', 'btn btn-secondary', 'Reset');
        const cancel = el('button', 'btn btn-secondary', 'Cancel');
        [save, reset, cancel].forEach(b => { b.type = 'button'; });
        const sync = () => {
            const any = boxes.some(b => b.checked);
            save.disabled = !any;
            hint.style.display = any ? 'none' : '';
        };
        boxes.forEach(b => b.addEventListener('change', sync));
        sync();
        reset.addEventListener('click', () => {
            boxes.forEach(b => { b.checked = true; });
            if (radios) radios.forEach(r => { r.checked = r.value === 'raw'; });
            sync();
        });
        cancel.addEventListener('click', () => d.close());
        save.addEventListener('click', () => {
            // Keep exclusions for columns this view doesn't show (another season's key-event
            // column, a trackable that's gone) — they may come back.
            const known = new Set(boxes.map(b => b.value));
            const exclude = s.exclude.filter(k => !known.has(k))
                .concat(boxes.filter(b => !b.checked).map(b => b.value));
            const numbers = radios ? (radios.find(r => r.checked) || {}).value || 'raw' : 'raw';
            settings.save(ctx.key, { numbers, exclude });
            ctx.onSaved();
            d.close();
            showToast('Export options saved.');
        });
        const actions = el('div', 'modal-actions');
        actions.append(save, reset, cancel);
        d.content.appendChild(actions);

        d.opener = ctx.opener;
        d.overlay.style.display = 'flex';
        if (typeof trapFocus === 'function') trapFocus(d.overlay);
    }

    function button(label, title, icon) {
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'btn btn-secondary btn-sm';
        b.title = title;
        if (icon) {
            b.setAttribute('aria-label', title);
            b.classList.add('export-options-btn');
            b.appendChild(svgIcon(icon, 14));
            b.appendChild(el('span', 'action-label', label));
        } else {
            b.textContent = label;
        }
        return b;
    }

    // attach(opts) -> handle { group, setDownloads(enabled), refresh() }
    //   table       a real table (the auto-wire passes each data-export-csv table)
    //   bar         container for a detached export's buttons
    //   getRows     () => rows for a detached export; empty means "Nothing to export."
    //   build       (rows) => detached <table>; build([]) must give the header (the column list)
    //   key         settings key (default: the table's data-export-csv minus ".csv")
    //   filename    file name without extension (default: key)
    //   numbers     'raw-only' hides the Numbers choice (an export that must re-import)
    function attach(opts) {
        const table = opts.table || null;
        const key = opts.key || (table ? (table.dataset.exportCsv || '').replace(/\.csv$/i, '') : '') || 'export';
        const filename = opts.filename || (table ? (table.dataset.exportCsv || key + '.csv').replace(/\.csv$/i, '') : key);
        const rawOnly = opts.numbers === 'raw-only';
        const headerTable = () => table || (opts.build ? opts.build([]) : null);
        const hasHeader = () => {
            const ht = headerTable();
            return !!ht && Array.from(ht.querySelectorAll('thead th')).some(th => !('noExport' in th.dataset) && colKey(th));
        };
        const current = () => {
            const s = settings.load(key);
            if (rawOnly) s.numbers = 'raw';
            return s;
        };
        const source = () => {
            if (table) return table;
            const rows = opts.getRows ? opts.getRows() : [];
            if (!rows || !rows.length) { showToast('Nothing to export.', 'info'); return null; }
            return opts.build(rows);
        };

        const group = document.createElement('div');
        group.className = 'table-export-actions';

        // Export scope. The checkbox appears only while a QuickSearch filter is actually
        // narrowing this table, so unfiltered tables look exactly as before. Checked = export
        // what you see, which is the pre-existing behaviour of every filtered export.
        let scopeBox = null;
        let scopeLabel = null;
        if (table) {
            scopeLabel = document.createElement('label');
            scopeLabel.className = 'export-scope';
            scopeBox = document.createElement('input');
            scopeBox.type = 'checkbox';
            scopeBox.checked = true;
            scopeLabel.append(scopeBox, document.createTextNode('Filtered only'));
        }
        const scope = () => (scopeBox && !scopeBox.checked ? 'all' : 'visible');

        const optionsBtn = button('Options', 'Export options', 'adjustments-horizontal');
        const csvBtn = button('↓ CSV', 'Download as CSV');
        const xlsxBtn = button('↓ XLSX', 'Download as Excel spreadsheet');
        const markCustom = () => optionsBtn.classList.toggle('is-custom', isCustom(current(), rawOnly));
        markCustom();

        const run = (fn, ext) => () => {
            const t = source();
            if (!t) return;
            const s = current();
            fn(t, filename + ext, { scope: scope(), numbers: s.numbers, exclude: s.exclude });
        };
        csvBtn.addEventListener('click', run(csv, '.csv'));
        xlsxBtn.addEventListener('click', run(xlsx, '.xlsx'));
        optionsBtn.addEventListener('click', () => {
            if (!hasHeader()) return;
            openOptions({ key, filename, rawOnly, headerTable, current, opener: optionsBtn, onSaved: markCustom });
        });

        // Reflect the row count in the labels while filtered, so the choice can't surprise
        // anyone: "↓ CSV 12" vs "↓ CSV 98".
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
        if (table) {
            table.addEventListener('quicksearch:filtered', syncScopeUI);
            scopeBox.addEventListener('change', syncScopeUI);
            group.append(scopeLabel);
        }
        group.append(optionsBtn, csvBtn, xlsxBtn);

        if (opts.bar) {
            opts.bar.appendChild(group);
        } else if (table) {
            // Nearest preceding .tab-toolbar, searching up through ancestors; else a new one.
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
            if (toolbar) {
                toolbar.appendChild(group);
            } else {
                const wrap = document.createElement('div');
                wrap.className = 'tab-toolbar';
                wrap.appendChild(group);
                table.parentNode.insertBefore(wrap, table);
            }
        }

        const handle = {
            group,
            // Downloads follow the caller (rows to export); Options follows the header.
            setDownloads(enabled) {
                csvBtn.disabled = !enabled;
                xlsxBtn.disabled = !enabled;
                optionsBtn.disabled = !hasHeader();
            },
            refresh() { optionsBtn.disabled = !hasHeader(); markCustom(); },
        };
        handle.refresh();
        if (table) table._tableExport = handle;
        return handle;
    }

    // Auto-wire every table[data-export-csv] present at load.
    document.addEventListener('DOMContentLoaded', () => {
        document.querySelectorAll('table[data-export-csv]').forEach(table => attach({ table }));
    });

    window.TableExport = {
        attach,
        csv,
        xlsx,
        csvField,
        settings,
        // The handle the auto-wire made for a table (the Scout Report drives its own buttons).
        handleFor: (table) => (table && table._tableExport) || null,
    };
})();
