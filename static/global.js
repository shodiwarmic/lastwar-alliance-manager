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

function _extractTableData(tableEl) {
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
        const tds = tr.querySelectorAll('td');
        if (tds.length === 1 && tds[0].colSpan > 1) return;
        const cells = [];
        tds.forEach((td, i) => {
            if (skipCols.has(i)) return;
            const input = td.querySelector('input, select, textarea');
            let val;
            if (input) {
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

function exportTableToCSV(tableEl, filename) {
    if (typeof tableEl === 'string') tableEl = document.getElementById(tableEl);
    if (!tableEl) return;

    const rows = _extractTableData(tableEl);
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

function exportTableToXLSX(tableEl, filename) {
    if (typeof tableEl === 'string') tableEl = document.getElementById(tableEl);
    if (!tableEl) return;
    const rows = _extractTableData(tableEl);
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

        const csvBtn = document.createElement('button');
        csvBtn.className = 'btn btn-secondary btn-sm';
        csvBtn.textContent = '↓ CSV';
        csvBtn.title = 'Download as CSV';
        csvBtn.addEventListener('click', () => exportTableToCSV(table, csvFilename));

        const xlsxBtn = document.createElement('button');
        xlsxBtn.className = 'btn btn-secondary btn-sm';
        xlsxBtn.textContent = '↓ XLSX';
        xlsxBtn.title = 'Download as Excel spreadsheet';
        xlsxBtn.addEventListener('click', () => exportTableToXLSX(table, xlsxFilename));

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
        exportActions.append(csvBtn, xlsxBtn);

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

