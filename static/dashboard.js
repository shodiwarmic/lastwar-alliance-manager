'use strict';

const VS_MINIMUM  = window.VS_MINIMUM  || 2500000;
const USER_RANK   = window.USER_RANK   || '';
const IS_ADMIN    = window.IS_ADMIN    || false;

// --- Card metadata ---

const CARD_META = {
    'health':       { label: 'Alliance Health',  icon: '🛡️' },
    'vs':           { label: 'VS Performance',   icon: '⚔️' },
    'schedule':     { label: 'Schedule',         icon: '📅' },
    'diplomacy':    { label: 'Diplomacy',        icon: '🤝' },
    'leader-flags':    { label: 'Leader Flags',    icon: '⚠️' },
    'accountability':  { label: 'Accountability',  icon: '⚖️' },
};

// --- Formatting helpers ---

function fmtNumber(n) {
    if (n == null) return '—';
    if (n >= 1_000_000_000) return (n / 1_000_000_000).toFixed(1) + 'B';
    if (n >= 1_000_000)     return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000)         return (n / 1_000).toFixed(1) + 'K';
    return String(n);
}

function fmtPct(num, denom) {
    if (!denom) return '—';
    return Math.round((num / denom) * 100) + '%';
}

// VS day columns we sum
const VS_DAYS = ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday'];

function vsTotal(row) {
    return VS_DAYS.reduce((s, d) => s + (row[d] || 0), 0);
}

// --- Schedule: map day_number to a calendar date ---
// day_number is 1-based. Within each 7-day block, position 1=Mon...7=Sun.
// We align by treating Mon of the current week as position 1.
function getWeekStart() {
    const now = new Date();
    const dow = now.getDay(); // 0=Sun,1=Mon...6=Sat
    const offsetToMonday = (dow === 0) ? -6 : 1 - dow;
    const monday = new Date(now);
    monday.setHours(0, 0, 0, 0);
    monday.setDate(now.getDate() + offsetToMonday);
    return monday;
}

function todayGameDateStr() {
    // Game time = UTC-2
    const d = new Date(Date.now() - 2 * 3600 * 1000);
    return d.toISOString().slice(0, 10);
}

function addGameDays(dateStr, n) {
    const d = new Date(dateStr + 'T12:00:00Z');
    d.setUTCDate(d.getUTCDate() + n);
    return d.toISOString().slice(0, 10);
}

function formatEventDate(dateStr) {
    const today = todayGameDateStr();
    const tomorrow = addGameDays(today, 1);
    if (dateStr === today) return 'Today';
    if (dateStr === tomorrow) return 'Tomorrow';
    const d = new Date(dateStr + 'T12:00:00Z');
    const days = ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'];
    const months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
    return days[d.getUTCDay()] + ' ' + months[d.getUTCMonth()] + ' ' + d.getUTCDate();
}

// --- DOM helpers ---

function el(tag, props, ...children) {
    const node = document.createElement(tag);
    if (props) {
        Object.entries(props).forEach(([k, v]) => {
            if (k === 'className') node.className = v;
            else if (k === 'textContent') node.textContent = v;
            else if (k === 'style') Object.assign(node.style, v);
            else node.setAttribute(k, v);
        });
    }
    children.forEach(c => {
        if (c == null) return;
        node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
    });
    return node;
}

function statCell(value, label) {
    const cell = el('div', { className: 'dash-stat' });
    cell.appendChild(el('div', { className: 'dash-stat-value', textContent: value }));
    cell.appendChild(el('div', { className: 'dash-stat-label', textContent: label }));
    return cell;
}

function loadingEl() {
    return el('p', { className: 'dash-card-loading', textContent: 'Loading…' });
}

function errorEl(msg) {
    return el('p', { className: 'dash-card-error', textContent: msg });
}

function buildCard(id) {
    const meta = CARD_META[id] || { label: id, icon: '📌' };
    const card = el('div', { className: 'dash-card', 'data-card-id': id });
    const h3 = el('h3');
    h3.appendChild(document.createTextNode(meta.icon + ' ' + meta.label));
    card.appendChild(h3);
    card.appendChild(loadingEl());
    return card;
}

// --- Card renderers ---

function renderHealth(card, members) {
    const active = members.filter(m => m.rank !== 'EX');
    const totalPower = active.reduce((s, m) => s + (m.power || 0), 0);
    const eligCount = active.filter(m => m.eligible).length;

    const grid = el('div', { className: 'dash-stat-grid' });
    grid.appendChild(statCell(String(active.length), 'Members'));
    grid.appendChild(statCell(fmtNumber(totalPower), 'Total Power'));
    grid.appendChild(statCell(fmtPct(eligCount, active.length), 'Train-Eligible'));

    card.querySelector('.dash-card-loading')?.remove();
    card.appendChild(grid);
}

function renderVS(card, vsRows) {
    const totals = vsRows
        .filter(r => r.member_rank !== 'EX')
        .map(r => ({ name: r.member_name, rank: r.member_rank, total: vsTotal(r) }));
    totals.sort((a, b) => b.total - a.total);

    const weekTotal = totals.reduce((s, r) => s + r.total, 0);
    const avg = totals.length ? Math.round(weekTotal / totals.length) : 0;
    const above = totals.filter(r => r.total >= VS_MINIMUM).length;

    const grid = el('div', { className: 'dash-stat-grid' });
    grid.appendChild(statCell(fmtNumber(weekTotal), 'Week Total'));
    grid.appendChild(statCell(fmtNumber(avg), 'Avg/Member'));
    grid.appendChild(statCell(fmtPct(above, totals.length), '≥ Min'));

    const topLabel = el('p', { className: 'dash-section-label', textContent: 'Top 3' });
    const topList = el('ul', { className: 'dash-list' });
    totals.slice(0, 3).forEach(r => {
        const li = el('li');
        li.appendChild(el('span', { className: 'dash-list-name', textContent: r.name }));
        li.appendChild(el('span', { className: 'dash-list-value', textContent: fmtNumber(r.total) }));
        topList.appendChild(li);
    });

    const botLabel = el('p', { className: 'dash-section-label', textContent: 'Bottom 3' });
    const botList = el('ul', { className: 'dash-list' });
    totals.slice(-3).reverse().forEach(r => {
        const li = el('li');
        li.appendChild(el('span', { className: 'dash-list-name', textContent: r.name }));
        li.appendChild(el('span', { className: 'dash-list-value', textContent: fmtNumber(r.total) }));
        botList.appendChild(li);
    });

    card.querySelector('.dash-card-loading')?.remove();
    card.append(grid, topLabel, topList, botLabel, botList);
}

function renderSchedule(card, events) {
    card.querySelector('.dash-card-loading')?.remove();

    if (!Array.isArray(events) || !events.length) {
        card.appendChild(el('p', { textContent: 'No upcoming events.' }));
        return;
    }

    // Show next 3 events sorted by date+time
    const sorted = events
        .slice()
        .sort((a, b) => (a.event_date + a.event_time).localeCompare(b.event_date + b.event_time))
        .slice(0, 3);

    const list = el('ul', { className: 'dash-list' });
    sorted.forEach(ev => {
        const li = el('li');

        const nameSpan = el('span', { className: 'dash-list-name' });
        nameSpan.textContent = ev.type_icon + ' ' + ev.type_name;
        li.appendChild(nameSpan);

        const right = el('span', { className: 'dash-list-value' });
        right.textContent = formatEventDate(ev.event_date) + ' ' + ev.event_time;
        li.appendChild(right);

        list.appendChild(li);
    });
    card.appendChild(list);
}

function renderDiplomacy(card, allies, agreementTypes) {
    const typeMap = {};
    agreementTypes.forEach(t => { typeMap[t.id] = t.name; });

    const active = allies.filter(a => a.active);
    card.querySelector('.dash-card-loading')?.remove();

    if (!active.length) {
        card.appendChild(el('p', { textContent: 'No active allies.' }));
        return;
    }

    const list = el('ul', { className: 'dash-list' });
    active.forEach(ally => {
        const li = el('li', { style: { flexWrap: 'wrap', gap: '0.25rem' } });

        const nameSpan = el('span', { className: 'dash-list-name' });
        nameSpan.textContent = (ally.tag ? '[' + ally.tag + '] ' : '') + ally.name;
        li.appendChild(nameSpan);

        const tags = el('span');
        (ally.agreement_type_ids || []).forEach(id => {
            if (typeMap[id]) {
                tags.appendChild(el('span', { className: 'dash-tag', textContent: typeMap[id] }));
            }
        });
        li.appendChild(tags);
        list.appendChild(li);
    });
    card.appendChild(list);
}

function renderLeaderFlags(card, members, vsRows) {
    const totals = {};
    vsRows.forEach(r => { totals[r.member_id] = vsTotal(r); });

    const below = members
        .filter(m => m.rank !== 'EX')
        .filter(m => {
            const t = totals[m.id] ?? null;
            return t !== null && t < VS_MINIMUM;
        })
        .map(m => ({ name: m.name, rank: m.rank, total: totals[m.id] }))
        .sort((a, b) => a.total - b.total);

    card.querySelector('.dash-card-loading')?.remove();

    if (!below.length) {
        card.appendChild(el('p', { textContent: '✅ All members are meeting the VS minimum this week.' }));
        return;
    }

    const label = el('p', { className: 'dash-section-label' });
    label.textContent = 'Below VS minimum (' + fmtNumber(VS_MINIMUM) + ')';
    card.appendChild(label);

    const list = el('ul', { className: 'dash-list' });
    below.forEach(r => {
        const li = el('li');
        li.appendChild(el('span', { className: 'dash-list-name', textContent: r.name + ' (' + r.rank + ')' }));
        li.appendChild(el('span', { className: 'dash-list-value', textContent: fmtNumber(r.total) }));
        list.appendChild(li);
    });
    card.appendChild(list);
}

function renderAccountability(card, data) {
    const grid = el('div', { className: 'dash-stat-grid' });
    grid.appendChild(statCell(String(data.at_risk),           'At Risk'));
    grid.appendChild(statCell(String(data.needs_improvement), 'Needs Improvement'));
    grid.appendChild(statCell(String(data.reliable),          'Reliable'));

    card.querySelector('.dash-card-loading')?.remove();
    card.appendChild(grid);

    if (data.top_at_risk && data.top_at_risk.length) {
        card.appendChild(el('p', { className: 'dash-section-label', textContent: 'Most Strikes' }));
        const list = el('ul', { className: 'dash-list' });
        data.top_at_risk.forEach(m => {
            const li = el('li');
            li.appendChild(el('span', { className: 'dash-list-name', textContent: m.name + ' (' + m.rank + ')' }));
            li.appendChild(el('span', { className: 'dash-list-value', textContent: m.active_strikes + ' strike' + (m.active_strikes !== 1 ? 's' : '') }));
            list.appendChild(li);
        });
        card.appendChild(list);
    }
}

// --- Preferences ---

let currentPrefs = [];

async function loadPrefs() {
    const res = await fetch('/api/dashboard/prefs');
    if (!res.ok) throw new Error('Failed to load preferences');
    return res.json();
}

async function savePrefs(prefs) {
    const res = await fetch('/api/dashboard/prefs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(prefs),
    });
    if (!res.ok) throw new Error('Failed to save preferences');
}

// --- Grid sorting ---

let gridSortable = null;

function initGridSortable(grid) {
    if (gridSortable) gridSortable.destroy();
    gridSortable = new Sortable(grid, {
        animation: 150,
        ghostClass: 'sortable-ghost',
        dragClass: 'sortable-drag',
        direction: 'vertical',
        onEnd() {
            // Rebuild prefs from current DOM order
            const order = [...grid.querySelectorAll('.dash-card')].map(c => c.dataset.cardId);
            currentPrefs = currentPrefs
                .slice()
                .sort((a, b) => order.indexOf(a.id) - order.indexOf(b.id));
            savePrefs(currentPrefs).catch(() => {});
        },
    });
}

// --- Customize panel ---

let panelSortable = null;

function openCustomizePanel(available) {
    const panel = document.getElementById('customize-panel');
    const list = document.getElementById('customize-list');
    list.replaceChildren();

    // Show available cards in current prefs order (hidden cards last)
    const ordered = [
        ...currentPrefs.filter(p => available.some(a => a.id === p.id)),
        ...available.filter(a => !currentPrefs.some(p => p.id === a.id)),
    ];

    ordered.forEach(card => {
        const meta = CARD_META[card.id] || { label: card.id, icon: '📌' };
        const pref = currentPrefs.find(p => p.id === card.id) || card;

        const li = document.createElement('li');
        li.dataset.cardId = card.id;

        const handle = el('span', { className: 'customize-drag-handle', textContent: '⠿' });
        const cb = el('input', { type: 'checkbox' });
        cb.checked = pref.visible !== false;
        const lbl = el('span', { className: 'customize-label', textContent: meta.icon + ' ' + meta.label });

        li.append(handle, cb, lbl);
        list.appendChild(li);
    });

    if (panelSortable) panelSortable.destroy();
    panelSortable = new Sortable(list, { animation: 150, ghostClass: 'sortable-ghost', handle: '.customize-drag-handle' });

    panel.style.display = '';
}

function collectPanelPrefs() {
    return [...document.querySelectorAll('#customize-list li')].map(li => ({
        id: li.dataset.cardId,
        visible: li.querySelector('input[type=checkbox]').checked,
    }));
}

// --- Main boot ---

async function boot() {
    const grid = document.getElementById('dashboard-grid');

    let prefsData;
    try {
        prefsData = await loadPrefs();
    } catch {
        grid.appendChild(errorEl('Could not load dashboard preferences.'));
        return;
    }

    currentPrefs = prefsData.prefs;
    const available = prefsData.available;

    // Determine which cards we need data for
    const visibleIds = new Set(currentPrefs.filter(p => p.visible !== false).map(p => p.id));
    const needMembers       = visibleIds.has('health') || visibleIds.has('leader-flags');
    const needVS            = visibleIds.has('vs')     || visibleIds.has('leader-flags');
    const needSchedule      = visibleIds.has('schedule');
    const needAllies        = visibleIds.has('diplomacy');
    const needAccountability = visibleIds.has('accountability');

    // Render card shells in prefs order (visible only)
    const cardEls = {};
    currentPrefs.forEach(pref => {
        if (!pref.visible) return;
        if (!available.some(a => a.id === pref.id)) return;
        const card = buildCard(pref.id);
        cardEls[pref.id] = card;
        grid.appendChild(card);
    });

    // Fetch data in parallel
    const fetches = {};
    if (needMembers)  fetches.members  = fetch('/api/members').then(r => r.ok ? r.json() : Promise.reject());
    if (needVS)       fetches.vs       = fetch('/api/vs-points').then(r => r.ok ? r.json() : Promise.reject());
    if (needSchedule) {
        const today = todayGameDateStr();
        const to13  = addGameDays(today, 13);
        fetches.schedule = fetch('/api/schedule/events?from=' + today + '&to=' + to13)
            .then(r => r.ok ? r.json() : Promise.reject());
    }
    if (needAllies) {
        fetches.allies = fetch('/api/allies').then(r => r.ok ? r.json() : Promise.reject());
        fetches.agreementTypes = fetch('/api/ally-agreement-types').then(r => r.ok ? r.json() : Promise.reject());
    }
    if (needAccountability) fetches.accountability = fetch('/api/accountability/summary').then(r => r.ok ? r.json() : Promise.reject());

    const results = {};
    await Promise.allSettled(
        Object.entries(fetches).map(([key, p]) => p.then(v => { results[key] = v; }).catch(() => { results[key] = null; }))
    );

    // Render each visible card
    if (cardEls['health']) {
        if (results.members) renderHealth(cardEls['health'], results.members);
        else { cardEls['health'].querySelector('.dash-card-loading')?.remove(); cardEls['health'].appendChild(errorEl('Failed to load members.')); }
    }
    if (cardEls['vs']) {
        if (results.vs) renderVS(cardEls['vs'], results.vs);
        else { cardEls['vs'].querySelector('.dash-card-loading')?.remove(); cardEls['vs'].appendChild(errorEl('Failed to load VS data.')); }
    }
    if (cardEls['schedule']) {
        if (results.schedule) renderSchedule(cardEls['schedule'], results.schedule);
        else { cardEls['schedule'].querySelector('.dash-card-loading')?.remove(); cardEls['schedule'].appendChild(errorEl('Failed to load schedule.')); }
    }
    if (cardEls['diplomacy']) {
        if (results.allies && results.agreementTypes) renderDiplomacy(cardEls['diplomacy'], results.allies, results.agreementTypes);
        else { cardEls['diplomacy'].querySelector('.dash-card-loading')?.remove(); cardEls['diplomacy'].appendChild(errorEl('Failed to load allies.')); }
    }
    if (cardEls['leader-flags']) {
        if (results.members && results.vs) renderLeaderFlags(cardEls['leader-flags'], results.members, results.vs);
        else { cardEls['leader-flags'].querySelector('.dash-card-loading')?.remove(); cardEls['leader-flags'].appendChild(errorEl('Failed to load leader flag data.')); }
    }
    if (cardEls['accountability']) {
        if (results.accountability) renderAccountability(cardEls['accountability'], results.accountability);
        else { cardEls['accountability'].querySelector('.dash-card-loading')?.remove(); cardEls['accountability'].appendChild(errorEl('Failed to load accountability data.')); }
    }

    initGridSortable(grid);

    // Customize panel
    document.getElementById('btn-customize').addEventListener('click', () => {
        openCustomizePanel(available);
    });

    document.getElementById('btn-cancel-prefs').addEventListener('click', () => {
        document.getElementById('customize-panel').style.display = 'none';
    });

    document.getElementById('btn-save-prefs').addEventListener('click', async () => {
        const status = document.getElementById('prefs-save-status');
        status.textContent = '';

        const newPrefs = collectPanelPrefs();

        try {
            await savePrefs(newPrefs);
            status.textContent = '✓ Saved';
            setTimeout(() => { status.textContent = ''; }, 2000);
            document.getElementById('customize-panel').style.display = 'none';
            // Reload to apply new order/visibility
            window.location.reload();
        } catch {
            status.textContent = 'Save failed.';
        }
    });
}

document.addEventListener('DOMContentLoaded', boot);
