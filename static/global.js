// static/global.js — loaded on EVERY page (layout.html, and login.html), so every byte here is
// downloaded everywhere and any edit invalidates every user's cached copy. It holds only layout
// chrome and primitives most pages use. Code one page uses lives in that page; a widget a few
// pages use is an opt-in module. See CLAUDE.md -> "What belongs in global.js" before adding.

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
    // gameClock bundles the whole instant for callers doing weekday+time
    // arithmetic — a countdown to "Thursday 00:00 ST", say.
    //
    // `weekday` is gameWeekday()'s own Mon=0…Sun=6, CALLED rather than re-derived,
    // so the two cannot disagree. Raw getUTCDay() is Sunday-first and the app
    // never exposes that numbering; anything reading these fields against it will
    // be off by one in a way that only shows up on one day of the week.
    window.gameClock = () => {
        const d = gt();
        return {
            date: d.toISOString().slice(0, 10),
            weekday: window.gameWeekday(),
            hh: d.getUTCHours(),
            mm: d.getUTCMinutes(),
        };
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

// ---- Compact numbers (single source of truth) ----
// 123456789 → "123.46M", -2500000 → "-2.5M", 452345 → "452.35K", 999995 → "1M", 999 → "999".
// opts.decimals (0–3, default 2) sets the precision; trailing zeros are dropped either way,
// so "2.5M", never "2.50M". Tier from |n|; no "+" — callers that want one add it.
// opts.suffixes: 'short' (K/M/B) or 'metric' (K/M/G, as the game prints billions). Omitted, it
// is the user's numberSuffixes preference (UserPrefs), read once and kept in step by
// setNumberSuffixes, which switches what is already on the page in place (no reload).
// opts.empty (default "—") is what null / non-numeric input returns.
(function () {
    let prefStyle = null;
    const userStyle = () => prefStyle || (prefStyle =
        (window.UserPrefs && window.UserPrefs.get('numberSuffixes')) === 'metric' ? 'metric' : 'short');
    const billions = new Set();   // number parts this page has written at the billions tier

    window.formatCompact = function (n, opts) {
        opts = opts || {};
        const empty = opts.empty !== undefined ? opts.empty : '—';
        if (n === null || n === undefined || n === '') return empty;
        const v = Number(n);
        if (!Number.isFinite(v)) return empty;
        const abs = Math.abs(v);
        if (abs < 1000) return String(v);
        const d = Number.isInteger(opts.decimals) && opts.decimals >= 0 && opts.decimals <= 3
            ? opts.decimals : 2;
        const style = (opts.suffixes === 'metric' || opts.suffixes === 'short') ? opts.suffixes : userStyle();
        const f = Math.pow(10, d);
        const tiers = [[1e3, 'K'], [1e6, 'M'], [1e9, style === 'metric' ? 'G' : 'B']];
        let i = abs >= 1e9 ? 2 : abs >= 1e6 ? 1 : 0;
        let r = Math.round(abs / tiers[i][0] * f) / f;
        if (r >= 1000 && i < 2) { i++; r = Math.round(abs / tiers[i][0] * f) / f; }
        // String(r), not r.toFixed(d): the shortest form, so trailing zeros never print.
        const num = (v < 0 ? '-' : '') + String(r);
        if (i === 2 && !opts.suffixes) billions.add(num);   // only the preference's output switches
        return num + tiers[i][1];
    };

    // Switch the user's style in place: swap the billions letter on what this formatter has
    // already written (text, title, aria-label), then let canvases redraw. No reload.
    window.setNumberSuffixes = function (next) {
        next = next === 'metric' ? 'metric' : 'short';
        const prev = userStyle();
        if (next === prev) return false;
        prefStyle = next;
        if (window.UserPrefs) window.UserPrefs.set('numberSuffixes', next);
        const from = prev === 'metric' ? 'G' : 'B';
        const to = next === 'metric' ? 'G' : 'B';
        const re = new RegExp('(^|[^\\w.])(-?\\d+(?:\\.\\d+)?)' + from + '(?!\\w)', 'g');
        const swap = s => s.replace(re, (m, pre, num) => (billions.has(num) ? pre + num + to : m));
        const skip = 'script, style, textarea, [contenteditable], .tl-text, .number-style-option';
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, {
            acceptNode: t => (t.parentElement && t.parentElement.closest(skip))
                ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT,
        });
        for (let t = walker.nextNode(); t; t = walker.nextNode()) {
            const s = swap(t.nodeValue);
            if (s !== t.nodeValue) t.nodeValue = s;
        }
        document.querySelectorAll('[title], [aria-label]').forEach(el => {
            ['title', 'aria-label'].forEach(a => {
                const v = el.getAttribute(a);
                const s = v && swap(v);
                if (s && s !== v) el.setAttribute(a, s);
            });
        });
        window.dispatchEvent(new CustomEvent('numberstylechange', { detail: { suffixes: next } }));
        return true;
    };
})();

// Fold diacritics + lowercase for accent-insensitive search: "Pàcha" → "pacha".
// Fuse's own `ignoreDiacritics` option is a no-op in fuse.js 7.0.0, so callers pre-fold
// BOTH the indexed text and the query with this before handing them to Fuse.
window.foldSearch = (s) =>
    String(s == null ? '' : s).normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase();

// Desert Storm is fought on game-day Fridays only. Returns the rejection the server
// gives for any other day (validateDesertStorm, handlers_schedule.go — worded like
// the schedule's cooldown rejections), or '' for a Friday or an empty date, so a form
// can say why it cannot be saved before the officer tries. The server remains the
// authority; this is the same rule shown early, not a second copy of its judgement.
window.desertStormDayError = (dateStr) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(dateStr || '')) return '';
    const d = new Date(dateStr + 'T12:00:00Z');
    const dow = d.getUTCDay();
    if (dow === 5) return '';
    const days = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
    const next = new Date(d);
    next.setUTCDate(d.getUTCDate() + ((5 - dow + 7) % 7));
    return 'Desert Storm runs on Fridays only — ' + dateStr + ' is a ' + days[dow]
        + ' (next eligible date ' + next.toISOString().slice(0, 10) + ')';
};


// ---- Per-user UI preferences ----
// Currently localStorage-backed (persists per browser). The storage layer is isolated
// behind get/set so a future server-synced, per-account backend can replace it without
// touching call sites. Register new prefs + their defaults in PREF_DEFAULTS.
(function () {
    const NS = 'amPref:';
    const PREF_DEFAULTS = {
        memberSinceDisplay: 'days', // 'days' (in-alliance days) | 'date' (join date)
        numberSuffixes: 'short',    // 'short' (K/M/B) | 'metric' (K/M/G, as the game prints billions)
        exportSettings: '{}',       // JSON per export key: { numbers: 'raw'|'shown', exclude: [colKey] } (table-utils.js)
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


// Player photo <img>, hotlinked from the game CDN, with one retry on the backup CDN host.
// Lazy, so a photo in a hidden container (the sidebar on the mobile shell) is never fetched.
// No inline onerror: the handler is attached here, so it stays CSP-safe once the CDN hosts are
// allowlisted. The sidebar tile, Members and Recruiting all build their photos with it.
//   opts.className  default 'player-avatar'
//   opts.onFail     called when both hosts fail; default removes the image (no broken icon)
function buildPlayerAvatar(primary, failover, opts) {
    opts = opts || {};
    const img = document.createElement('img');
    img.className = opts.className || 'player-avatar';
    img.alt = '';
    img.loading = 'lazy';
    img.src = primary;
    let triedFailover = false;
    img.addEventListener('error', () => {
        if (!triedFailover && failover) { triedFailover = true; img.src = failover; }
        else if (opts.onFail) opts.onFail(img);
        else img.remove();
    });
    return img;
}

// Swap the sidebar user tile's initials for the logged-in user's photo when present. Falls back
// to the initials if the photo (and failover) fail to load or are blocked by CSP.
document.addEventListener('DOMContentLoaded', () => {
    const su = document.querySelector('.sidebar-user-avatar');
    if (!su || !su.dataset.photo) return;
    const initials = su.textContent.trim();
    const img = buildPlayerAvatar(su.dataset.photo, su.dataset.photoFailover, {
        className: 'sidebar-user-photo',
        onFail: () => { su.textContent = initials; },   // restore initials on total failure
    });
    su.textContent = '';
    su.appendChild(img);
});

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

    // Numbers: billions as B or G (the shared user menu, rendered in the gear dropdown AND
    // the more-sheet). Switches in place via setNumberSuffixes — no reload, so unsaved input
    // on the page survives. preventDefault matters: these are <a href="#"> like the theme
    // options, and a navigation to "#" would send every hash-enabled tab page to its default.
    const numberOptions = document.querySelectorAll('.number-style-option');
    const markNumberStyle = () => {
        const current = (window.UserPrefs && UserPrefs.get('numberSuffixes')) === 'metric' ? 'metric' : 'short';
        numberOptions.forEach(o => o.classList.toggle('active', o.dataset.suffixes === current));
    };
    markNumberStyle();
    numberOptions.forEach(option => {
        option.addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            const style = option.dataset.suffixes === 'metric' ? 'metric' : 'short';
            if (window.setNumberSuffixes(style)) {
                showToast('Numbers now show billions as ' + (style === 'metric' ? 'G' : 'B') + '.', 'info');
            }
            markNumberStyle();
        });
    });
    // Other tabs follow a change made here, and a page restored from the back-forward cache
    // re-checks, so nothing needs a reload to catch up.
    window.addEventListener('storage', (e) => {
        if (e.key === 'amPref:numberSuffixes') { window.setNumberSuffixes(e.newValue); markNumberStyle(); }
    });
    window.addEventListener('pageshow', (e) => {
        if (e.persisted && window.UserPrefs) { window.setNumberSuffixes(UserPrefs.get('numberSuffixes')); markNumberStyle(); }
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


// ---- Update nudge (release_check.go) ----
//
// An administrator's pages carry data-update-available when the daily release check has seen
// a newer release than this install runs. One toast per session per release, keyed by the
// tag, so a later release nudges again; the Admin page's "About this install and updates"
// block carries the detail and is where the toast points. No server-side acknowledgement.
(function () {
    const cfg = document.getElementById('layout-config');
    const tag = cfg && cfg.dataset.updateAvailable;
    if (!tag) return;
    const FLAG = 'update-nudged-' + tag;
    try {
        if (sessionStorage.getItem(FLAG)) return;
        sessionStorage.setItem(FLAG, '1');
    } catch (e) {
        return; // storage blocked — better silent than nagging every page
    }
    window.addEventListener('load', () => {
        setTimeout(() => {
            showToast(`Alliance Manager ${tag} is available — see Admin → Security & API.`, 'info', 8000);
        }, 1500);
    });
})();


// ---- Admin preview as rank (175) ----
//
// The banner layout.html renders while an administrator previews a rank: a rank select
// that switches the preview in place, and Exit. Both reload, because every permission the
// page was rendered with belongs to the rank being left.
(function () {
    // Starting one: the rank chips in the user menu (rendered in both the desktop dropdown
    // and the mobile More sheet, hence classes rather than ids). The preview starts on the
    // dashboard, since the page it was started from may be one the rank cannot open.
    document.querySelectorAll('.preview-rank-chip').forEach(chip => {
        chip.addEventListener('click', async (e) => {
            e.stopPropagation();
            document.querySelectorAll('.preview-rank-chip').forEach(c => { c.disabled = true; });
            try {
                const res = await fetch('/api/preview-rank', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ rank: chip.dataset.rank }),
                });
                if (!res.ok) throw new Error(await res.text());
                window.location.href = '/';
            } catch (err) {
                showToast('Could not start the preview.', 'error');
                document.querySelectorAll('.preview-rank-chip').forEach(c => { c.disabled = false; });
            }
        });
    });

    const select = document.getElementById('preview-banner-rank');
    const exit = document.getElementById('preview-banner-exit');
    if (!select && !exit) return;

    async function send(method, body) {
        const res = await fetch('/api/preview-rank', {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: body ? JSON.stringify(body) : undefined,
        });
        if (!res.ok) throw new Error(await res.text());
    }
    if (select) {
        select.addEventListener('change', async () => {
            try {
                await send('POST', { rank: select.value });
                location.reload();
            } catch (e) {
                showToast('Could not switch the preview rank.', 'error');
            }
        });
    }
    if (exit) {
        exit.addEventListener('click', async () => {
            try {
                await send('DELETE');
                location.reload();
            } catch (e) {
                showToast('Could not exit the preview.', 'error');
            }
        });
    }
})();

// ---- Demo mode (176) ----
//
// In the public demo some actions are refused with a 403 whose body is one fixed
// sentence. Page scripts do not surface a 403's body — most toast their own "Failed to
// …" — so this wraps fetch once, toasts the demo's sentence itself, and drops the page's
// own error toast that follows within a moment. Only active when the layout says demo
// mode; otherwise nothing is wrapped.
(function () {
    const cfg = document.getElementById('layout-config');
    if (!cfg || cfg.dataset.demo !== 'true' || !window.fetch) return;
    const DEMO_REFUSAL = 'This action is disabled in the demo.';
    let quietUntil = 0;
    const realFetch = window.fetch;
    window.fetch = async function (...args) {
        const res = await realFetch.apply(this, args);
        if (res.status === 403) {
            try {
                const body = (await res.clone().text()).trim();
                if (body === DEMO_REFUSAL) {
                    showToast(DEMO_REFUSAL, 'info', 5000);
                    quietUntil = Date.now() + 2000;
                }
            } catch (e) { /* an unreadable body is not ours */ }
        }
        return res;
    };
    const realToast = window.showToast;
    window.showToast = function (message, type, duration) {
        if (type === 'error' && Date.now() < quietUntil) return;
        return realToast(message, type, duration);
    };
})();
