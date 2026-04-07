'use strict';

const MEMBER_ID  = window.MEMBER_ID  || 0;
const CAN_MANAGE = window.CAN_MANAGE || false;

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

function strikeTypeLabel(t) {
    switch (t) {
        case 'vs_below_threshold': return 'VS Below Min';
        case 'train_no_show':      return 'Train No-Show';
        case 'storm_no_show':      return 'Storm No-Show';
        default:                   return 'Manual';
    }
}

// --- Add Strike modal ---

function openStrikeModal(preType) {
    document.getElementById('strike-member-id').value = MEMBER_ID;
    document.getElementById('strike-member-name').value = window._profileName || '';
    if (preType) document.getElementById('strike-type').value = preType;
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
    if (!reason) { status.textContent = 'Reason is required.'; return; }

    const res = await fetch('/api/accountability/strikes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ member_id: MEMBER_ID, strike_type: strikeType, reason, ref_date: refDate }),
    });
    if (!res.ok) { status.textContent = 'Failed to save.'; return; }
    closeStrikeModal();
    boot();
}

// --- Renders ---

function renderHeader(profile) {
    window._profileName = profile.name;
    const header = document.getElementById('profile-header');
    header.replaceChildren();

    const nameDiv = document.createElement('div');
    const nameP = document.createElement('p');
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
    strikeBadge.style.background = 'rgba(0,0,0,0.08)';
    strikeBadge.textContent = profile.active_strikes + ' active strike' + (profile.active_strikes !== 1 ? 's' : '');

    header.append(nameDiv, tagSpan, strikeBadge);

    if (CAN_MANAGE) {
        const actions = document.createElement('div');
        actions.className = 'acc-profile-actions';
        const addBtn = document.createElement('button');
        addBtn.className = 'btn btn-danger btn-sm';
        addBtn.textContent = '+ Add Strike';
        addBtn.addEventListener('click', () => openStrikeModal(null));
        actions.appendChild(addBtn);
        header.appendChild(actions);
    }
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

        const tdDate = document.createElement('td');
        tdDate.textContent = s.ref_date || s.created_at.slice(0, 10);

        const tdStatus = document.createElement('td');
        const statusSpan = document.createElement('span');
        statusSpan.className = s.status === 'active' ? 'acc-status--active' : 'acc-status--excused';
        statusSpan.textContent = s.status === 'active' ? 'Active' : 'Excused';
        if (s.excused_by) {
            const bySpan = document.createElement('span');
            bySpan.style.fontSize = '0.8rem';
            bySpan.style.color = 'var(--text-muted)';
            bySpan.textContent = ' by ' + s.excused_by;
            tdStatus.append(statusSpan, bySpan);
        } else {
            tdStatus.appendChild(statusSpan);
        }

        tr.append(tdType, tdReason, tdDate, tdStatus);

        if (CAN_MANAGE) {
            const tdAct = document.createElement('td');
            if (s.status === 'active') {
                const excuseBtn = document.createElement('button');
                excuseBtn.className = 'btn btn-secondary btn-sm';
                excuseBtn.textContent = 'Excuse';
                excuseBtn.addEventListener('click', () => excuseStrike(s.id, excuseBtn, tr));
                tdAct.appendChild(excuseBtn);
            }
            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.textContent = 'Delete';
            delBtn.style.marginLeft = s.status === 'active' ? '6px' : '0';
            delBtn.addEventListener('click', () => deleteStrike(s.id, delBtn, tr));
            tdAct.appendChild(delBtn);
            tr.appendChild(tdAct);
        }

        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    container.appendChild(table);
}

async function excuseStrike(strikeID, btn, tr) {
    btn.style.display = 'none';
    const confirmSpan = document.createElement('span');
    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
    const label = Object.assign(document.createElement('span'), { textContent: 'Excuse?' });
    label.style.fontSize = '0.85rem';
    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-primary btn-sm';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', async () => {
        const res = await fetch('/api/accountability/strikes/' + strikeID, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ status: 'excused', excused_reason: '' }),
        });
        if (res.ok) boot();
    });
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); btn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    tr.querySelector('td:last-child').appendChild(confirmSpan);
}

async function deleteStrike(strikeID, btn, tr) {
    btn.style.display = 'none';
    const confirmSpan = document.createElement('span');
    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
    const label = Object.assign(document.createElement('span'), { textContent: 'Sure?' });
    label.style.fontSize = '0.85rem';
    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-danger btn-sm';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', async () => {
        const res = await fetch('/api/accountability/strikes/' + strikeID, { method: 'DELETE' });
        if (res.ok) boot();
    });
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); btn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    tr.querySelector('td:last-child').appendChild(confirmSpan);
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
    container.appendChild(table);
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
        const tdBy = document.createElement('td');
        tdBy.textContent = s.recorded_by || '—';
        tr.append(tdDate, tdStatus, tdExcuse, tdBy);
        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    container.appendChild(table);
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
        tdShowed.textContent = t.showed_up ? '✅' : '❌';
        tr.append(tdDate, tdType, tdShowed);
        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    container.appendChild(table);
}

// --- Tab switching ---

function initTabs() {
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            document.querySelectorAll('.tab-content').forEach(c => { c.style.display = 'none'; });
            const target = document.getElementById('tab-' + btn.dataset.tab);
            if (target) target.style.display = 'block';
        });
    });
    const activeBtn = document.querySelector('.tab-btn.active');
    if (activeBtn) {
        const target = document.getElementById('tab-' + activeBtn.dataset.tab);
        if (target) target.style.display = 'block';
    }
}

// --- Boot ---

async function boot() {
    if (!MEMBER_ID) return;
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

    initTabs();
    boot();
    if (CAN_MANAGE) {
        document.getElementById('btn-strike-save').addEventListener('click', saveStrike);
        document.getElementById('btn-strike-cancel').addEventListener('click', closeStrikeModal);
    }
});
