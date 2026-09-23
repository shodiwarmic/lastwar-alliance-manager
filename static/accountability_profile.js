'use strict';

const _cfg       = document.getElementById('page-config').dataset;
const MEMBER_ID  = parseInt(_cfg.memberId, 10) || 0;
const CAN_MANAGE = _cfg.canManage === 'true';

// Flatpickr instance — initialised in DOMContentLoaded
let strikeRefDateFP = null;

function fmtNumber(n) {
    if (n == null) return '—';
    if (n >= 1_000_000_000) return (n / 1_000_000_000).toFixed(1) + 'B';
    if (n >= 1_000_000)     return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000)         return (n / 1_000).toFixed(1) + 'K';
    return String(n);
}

function tagClass(tag) {
    if (tag === 'At Risk')           return 'acc-tag acc-tag--at-risk';
    if (tag === 'Needs Improvement') return 'acc-tag acc-tag--needs-improvement';
    return 'acc-tag acc-tag--reliable';
}

// Labels come from the managed category list. The old switch here rendered every
// custom category as "Manual"; an unknown key now shows as itself.
function strikeTypeLabel(t) {
    return StrikeTypes.label(t);
}

// --- Add Strike modal ---

function openStrikeModal(preType) {
    document.getElementById('strike-member-id').value = MEMBER_ID;
    document.getElementById('strike-member-name').value = window._profileName || '';
    document.getElementById('strike-type').value = preType || '';
    document.getElementById('strike-reason').value = '';
    strikeRefDateFP.clear(false);
    document.getElementById('strike-modal-status').textContent = '';
    const strikeModal = document.getElementById('add-strike-modal');
    strikeModal.style.display = 'flex';
    trapFocus(strikeModal);
}

function closeStrikeModal() {
    const strikeModal = document.getElementById('add-strike-modal');
    releaseFocus(strikeModal);
    strikeModal.style.display = '';
}

async function saveStrike() {
    const strikeType = document.getElementById('strike-type').value;
    const reason     = document.getElementById('strike-reason').value.trim();
    const refDate    = document.getElementById('strike-ref-date').value;
    const status     = document.getElementById('strike-modal-status');
    if (!strikeType) { status.textContent = 'Category is required.'; return; }
    if (!reason) { status.textContent = 'Reason is required.'; return; }

    const res = await fetch('/api/accountability/strikes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ member_id: MEMBER_ID, strike_type: strikeType, reason, ref_date: refDate }),
    });
    if (!res.ok) {
        status.textContent = res.status === 400 ? (await res.text()).trim() : 'Failed to save.';
        return;
    }
    closeStrikeModal();
    boot();
}

// --- Renders ---

function renderHeader(profile) {
    window._profileName = profile.name;
    const header = document.getElementById('profile-header');
    header.replaceChildren();

    const nameDiv = document.createElement('div');
    const nameP = noTranslate(document.createElement('p'));
    nameP.className = 'acc-profile-name';
    nameP.textContent = profile.name;
    const subP = document.createElement('p');
    subP.className = 'acc-profile-sub';
    subP.textContent = profile.rank;
    nameDiv.append(nameP, subP);

    const tagSpan = document.createElement('span');
    tagSpan.className = tagClass(profile.tag);
    tagSpan.textContent = profile.tag;

    const strikeBadge = document.createElement('span');
    strikeBadge.className = 'acc-tag';
    strikeBadge.style.background = 'var(--color-border)';
    strikeBadge.textContent = profile.active_strikes + ' active strike' + (profile.active_strikes !== 1 ? 's' : '');

    header.append(nameDiv, tagSpan, strikeBadge);

    if (CAN_MANAGE) {
        const actions = document.createElement('div');
        actions.className = 'acc-profile-actions';
        const addBtn = document.createElement('button');
        addBtn.className = 'btn btn-danger btn-sm';
        addBtn.append(svgIcon('plus'), document.createTextNode(' Add Strike'));
        addBtn.addEventListener('click', () => openStrikeModal(null));
        actions.appendChild(addBtn);
        header.appendChild(actions);
    }
}

// Each history table scrolls sideways inside its own wrapper, so on a phone the
// Actions column stays reachable instead of running off the edge of the page.
function appendScrollTable(container, table) {
    const wrap = document.createElement('div');
    wrap.className = 'table-scroll';
    wrap.appendChild(table);
    container.appendChild(wrap);
}

function renderStrikes(strikes) {
    const container = document.getElementById('tab-strikes');
    container.replaceChildren();

    if (!strikes.length) {
        container.appendChild(Object.assign(document.createElement('p'), { textContent: 'No strikes on record.' }));
        return;
    }

    const table = document.createElement('table');
    table.className = 'data-table';
    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    ['Type', 'Reason', 'Date', 'Status', CAN_MANAGE ? 'Actions' : ''].forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        headerRow.appendChild(th);
    });
    thead.appendChild(headerRow);
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    strikes.forEach(s => {
        const tr = document.createElement('tr');

        const tdType = document.createElement('td');
        tdType.textContent = strikeTypeLabel(s.strike_type);

        const tdReason = document.createElement('td');
        tdReason.textContent = s.reason;
        if (s.reason) TranslateBlock.attach(tdReason, s.reason);

        const tdDate = document.createElement('td');
        tdDate.textContent = s.ref_date || s.created_at.slice(0, 10);

        const tdStatus = document.createElement('td');
        const statusSpan = document.createElement('span');
        statusSpan.className = s.status === 'active' ? 'acc-status--active' : 'acc-status--excused';
        statusSpan.textContent = s.status === 'active' ? 'Active' : 'Excused';
        if (s.excused_by) {
            const bySpan = document.createElement('span');
            bySpan.style.fontSize = '0.8rem';
            bySpan.style.color = 'var(--color-text-muted)';
            bySpan.textContent = ' by ' + s.excused_by;
            tdStatus.append(statusSpan, bySpan);
        } else {
            tdStatus.appendChild(statusSpan);
        }

        tr.append(tdType, tdReason, tdDate, tdStatus);

        if (CAN_MANAGE) {
            const tdAct = document.createElement('td');
            const actionWrap = document.createElement('div');
            actionWrap.className = 'row-actions';
            if (s.status === 'active') {
                actionWrap.appendChild(
                    rowActionBtn('btn btn-secondary btn-sm', 'check', 'Excuse', () => excuseStrike(s.id))
                );
            }
            actionWrap.appendChild(
                rowActionBtn('btn btn-danger btn-sm', 'trash', 'Delete', () => deleteStrike(s.id))
            );
            tdAct.appendChild(actionWrap);
            tr.appendChild(tdAct);
        }

        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    appendScrollTable(container, table);
}

async function excuseStrike(strikeID) {
    if (!await showConfirm('Excuse this strike?', 'Excuse')) return;
    const res = await fetch('/api/accountability/strikes/' + strikeID, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: 'excused', excused_reason: '' }),
    });
    if (res.ok) boot();
}

async function deleteStrike(strikeID) {
    if (!await showConfirm('Delete this strike?', 'Delete')) return;
    const res = await fetch('/api/accountability/strikes/' + strikeID, { method: 'DELETE' });
    if (res.ok) boot();
}

function renderVSHistory(vsHistory) {
    const container = document.getElementById('tab-vs');
    container.replaceChildren();
    if (!vsHistory.length) {
        container.appendChild(Object.assign(document.createElement('p'), { textContent: 'No VS data on record.' }));
        return;
    }
    const table = document.createElement('table');
    table.className = 'data-table';
    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    ['Week', 'Total'].forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        headerRow.appendChild(th);
    });
    thead.appendChild(headerRow);
    table.appendChild(thead);
    const tbody = document.createElement('tbody');
    vsHistory.forEach(v => {
        const tr = document.createElement('tr');
        const tdWeek = document.createElement('td');
        tdWeek.textContent = v.week_date;
        const tdTotal = document.createElement('td');
        tdTotal.textContent = fmtNumber(v.total);
        tr.append(tdWeek, tdTotal);
        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    appendScrollTable(container, table);
}

function renderStormHistory(stormHistory) {
    const container = document.getElementById('tab-storm');
    container.replaceChildren();
    if (!stormHistory.length) {
        container.appendChild(Object.assign(document.createElement('p'), { textContent: 'No storm attendance logged.' }));
        return;
    }
    const table = document.createElement('table');
    table.className = 'data-table';
    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    ['Date', 'Status', 'Excuse', 'Logged By'].forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        headerRow.appendChild(th);
    });
    thead.appendChild(headerRow);
    table.appendChild(thead);
    const tbody = document.createElement('tbody');
    stormHistory.forEach(s => {
        const tr = document.createElement('tr');
        const tdDate = document.createElement('td');
        tdDate.textContent = s.storm_date;
        const tdStatus = document.createElement('td');
        const statusSpan = document.createElement('span');
        statusSpan.className = 'acc-attend--' + s.status.replace('_', '-');
        statusSpan.textContent = s.status === 'no_show' ? 'No-Show' : s.status.charAt(0).toUpperCase() + s.status.slice(1);
        tdStatus.appendChild(statusSpan);
        const tdExcuse = document.createElement('td');
        tdExcuse.textContent = s.excuse_reason || '—';
        if (s.excuse_reason) TranslateBlock.attach(tdExcuse, s.excuse_reason);
        const tdBy = document.createElement('td');
        tdBy.textContent = s.recorded_by || '—';
        tr.append(tdDate, tdStatus, tdExcuse, tdBy);
        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    appendScrollTable(container, table);
}

function renderTrainHistory(trainHistory) {
    const container = document.getElementById('tab-train');
    container.replaceChildren();
    if (!trainHistory.length) {
        container.appendChild(Object.assign(document.createElement('p'), { textContent: 'No train logs on record.' }));
        return;
    }
    const table = document.createElement('table');
    table.className = 'data-table';
    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    ['Date', 'Type', 'Showed Up'].forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        headerRow.appendChild(th);
    });
    thead.appendChild(headerRow);
    table.appendChild(thead);
    const tbody = document.createElement('tbody');
    trainHistory.forEach(t => {
        const tr = document.createElement('tr');
        const tdDate = document.createElement('td');
        tdDate.textContent = t.date;
        const tdType = document.createElement('td');
        tdType.textContent = t.train_type === 'FREE' ? 'Free' : 'Purchased';
        const tdShowed = document.createElement('td');
        tdShowed.setAttribute('aria-label', t.showed_up ? 'Showed up' : 'No show');
        tdShowed.appendChild(svgIcon(t.showed_up ? 'check' : 'x', 16));
        tr.append(tdDate, tdType, tdShowed);
        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    appendScrollTable(container, table);
}

// --- Boot ---

async function boot() {
    if (!MEMBER_ID) return;
    try {
        await StrikeTypes.load();
        if (CAN_MANAGE) StrikeTypes.fillSelect(document.getElementById('strike-type'));
    } catch (err) {
        // Labels fall back to the raw key; the page is still usable.
        console.error('StrikeTypes.load:', err);
    }
    let profile;
    try {
        const res = await fetch('/api/accountability/members/' + MEMBER_ID);
        if (!res.ok) throw new Error();
        profile = await res.json();
    } catch {
        document.getElementById('profile-header').textContent = 'Failed to load profile.';
        return;
    }
    renderHeader(profile);
    renderStrikes(profile.strikes);
    renderVSHistory(profile.vs_history);
    renderStormHistory(profile.storm_history);
    renderTrainHistory(profile.train_history);
}

document.addEventListener('DOMContentLoaded', () => {
    strikeRefDateFP = flatpickr('#strike-ref-date', { dateFormat: 'Y-m-d', allowInput: true });

    Tabs.init({ hash: true, defaultTab: 'strikes' });
    boot();
    if (CAN_MANAGE) {
        document.getElementById('btn-strike-save').addEventListener('click', saveStrike);
        document.getElementById('btn-strike-cancel').addEventListener('click', closeStrikeModal);
    }
});
