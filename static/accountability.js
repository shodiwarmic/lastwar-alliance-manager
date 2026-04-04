'use strict';

const CAN_MANAGE = window.CAN_MANAGE || false;

// --- Helpers ---

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

function attendanceLabel(s) {
    switch (s) {
        case 'attended':     return 'Attended';
        case 'no_show':      return 'No-Show';
        case 'excused':      return 'Excused';
        case 'not_enrolled': return 'Not Enrolled';
        default:             return s;
    }
}

// --- Tabs ---

function setupTabs() {
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-panel').forEach(p => p.classList.add('hidden'));
            btn.classList.add('active');
            const panel = document.getElementById('tab-' + btn.dataset.tab);
            if (panel) panel.classList.remove('hidden');
            onTabActivated(btn.dataset.tab);
        });
    });
}

let strikesLoaded = false;
let reportLoaded  = false;

function onTabActivated(tab) {
    if (tab === 'strikes' && !strikesLoaded) {
        strikesLoaded = true;
        loadStrikes();
    }
    if (tab === 'report' && !reportLoaded) {
        reportLoaded = true;
        loadReport();
    }
}

// --- Add Strike modal ---

let allMembers = [];

function openStrikeModal(memberID, memberName, preType) {
    document.getElementById('strike-member-id').value = memberID;
    document.getElementById('strike-member-name').value = memberName;
    if (preType) document.getElementById('strike-type').value = preType;
    document.getElementById('strike-reason').value = '';
    document.getElementById('strike-ref-date').value = '';
    document.getElementById('strike-modal-status').textContent = '';
    document.getElementById('add-strike-modal').style.display = 'flex';
}

function closeStrikeModal() {
    document.getElementById('add-strike-modal').style.display = '';
}

async function saveStrike() {
    const memberID   = parseInt(document.getElementById('strike-member-id').value, 10);
    const strikeType = document.getElementById('strike-type').value;
    const reason     = document.getElementById('strike-reason').value.trim();
    const refDate    = document.getElementById('strike-ref-date').value;
    const status     = document.getElementById('strike-modal-status');
    if (!reason) { status.textContent = 'Reason is required.'; return; }

    const res = await fetch('/api/accountability/strikes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ member_id: memberID, strike_type: strikeType, reason, ref_date: refDate }),
    });
    if (!res.ok) { status.textContent = 'Failed to save strike.'; return; }
    closeStrikeModal();
    loadMembers();
    strikesLoaded = false; // invalidate cache so next tab-switch refetches
}

// --- Tab: Members ---

async function loadMembers() {
    const tbody = document.getElementById('members-tbody');
    const loadingRow = document.createElement('tr');
    const loadingCell = document.createElement('td');
    loadingCell.colSpan = 6;
    loadingCell.className = 'loading-msg';
    loadingCell.textContent = 'Loading…';
    loadingRow.appendChild(loadingCell);
    tbody.replaceChildren(loadingRow);

    let members;
    try {
        const res = await fetch('/api/accountability/members');
        if (!res.ok) throw new Error();
        members = await res.json();
    } catch {
        loadingCell.textContent = 'Failed to load members.';
        return;
    }

    allMembers = members;

    // VS flag banner
    const below = members.filter(m => m.below_threshold);
    const banner = document.getElementById('vs-flag-banner');
    if (below.length) {
        document.getElementById('vs-flag-names').textContent = below.map(m => m.name).join(', ');
        banner.style.display = 'flex';
    } else {
        banner.style.display = 'none';
    }

    const search = document.getElementById('member-search').value.toLowerCase();
    const filtered = search ? members.filter(m => m.name.toLowerCase().includes(search)) : members;

    tbody.replaceChildren();

    if (!filtered.length) {
        const emptyRow = document.createElement('tr');
        const emptyCell = document.createElement('td');
        emptyCell.colSpan = 6;
        emptyCell.textContent = 'No members found.';
        emptyRow.appendChild(emptyCell);
        tbody.appendChild(emptyRow);
        return;
    }

    filtered.forEach(m => {
        const tr = document.createElement('tr');
        if (m.below_threshold) tr.className = 'acc-row--below-vs';

        const tdName = document.createElement('td');
        tdName.textContent = m.name;

        const tdRank = document.createElement('td');
        tdRank.textContent = m.rank;

        const tdTag = document.createElement('td');
        const tagSpan = document.createElement('span');
        tagSpan.className = tagClass(m.tag);
        tagSpan.textContent = m.tag;
        tdTag.appendChild(tagSpan);

        const tdStrikes = document.createElement('td');
        tdStrikes.textContent = String(m.active_strikes);

        const tdVS = document.createElement('td');
        const vsSpan = document.createElement('span');
        vsSpan.className = m.below_threshold ? 'acc-vs--below' : 'acc-vs--ok';
        vsSpan.textContent = fmtNumber(m.vs_total);
        tdVS.appendChild(vsSpan);

        const tdActions = document.createElement('td');
        const viewBtn = document.createElement('a');
        viewBtn.href = '/accountability/' + m.id;
        viewBtn.className = 'btn btn-secondary btn-sm';
        viewBtn.textContent = 'Profile';
        tdActions.appendChild(viewBtn);

        if (CAN_MANAGE) {
            const strikeBtn = document.createElement('button');
            strikeBtn.className = 'btn btn-danger btn-sm';
            strikeBtn.textContent = '+ Strike';
            strikeBtn.style.marginLeft = '6px';
            strikeBtn.addEventListener('click', () => openStrikeModal(m.id, m.name, null));
            tdActions.appendChild(strikeBtn);
        }

        tr.append(tdName, tdRank, tdTag, tdStrikes, tdVS, tdActions);
        tbody.appendChild(tr);
    });
}

function currentMonday() {
    const d = new Date();
    const day = d.getUTCDay() || 7;
    d.setUTCDate(d.getUTCDate() - (day - 1));
    return d.toISOString().slice(0, 10);
}

async function addVSStrikesForBelow() {
    const below = allMembers.filter(m => m.below_threshold);
    if (!below.length) return;

    const refDate = currentMonday();
    let added = 0;
    let skipped = 0;

    const banner = document.getElementById('vs-flag-banner');
    const btn = document.getElementById('btn-add-vs-strikes');
    if (btn) btn.disabled = true;

    for (const m of below) {
        const res = await fetch('/api/accountability/strikes', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                member_id:   m.id,
                strike_type: 'vs_below_threshold',
                reason:      'VS below minimum this week',
                ref_date:    refDate,
            }),
        });
        if (res.status === 409) {
            skipped++;
        } else if (res.ok) {
            added++;
        }
    }

    if (btn) btn.disabled = false;

    // Show inline result in the banner
    let msg = '';
    if (added > 0 && skipped === 0) {
        msg = `✓ ${added} strike${added !== 1 ? 's' : ''} added.`;
    } else if (added > 0 && skipped > 0) {
        msg = `✓ ${added} added, ${skipped} already had a strike this week.`;
    } else {
        msg = `All ${skipped} member${skipped !== 1 ? 's' : ''} already had a VS strike this week.`;
    }

    let statusEl = banner.querySelector('.vs-add-status');
    if (!statusEl) {
        statusEl = document.createElement('span');
        statusEl.className = 'vs-add-status';
        statusEl.style.cssText = 'margin-left:12px;font-size:0.85em;';
        banner.appendChild(statusEl);
    }
    statusEl.textContent = msg;
    setTimeout(() => { statusEl.textContent = ''; }, 5000);

    if (added > 0) {
        loadMembers();
        strikesLoaded = false;
    }
}

// --- Tab: Strikes ---

async function loadStrikes() {
    const container = document.getElementById('strikes-container');
    container.replaceChildren(Object.assign(document.createElement('p'), { className: 'loading-msg', textContent: 'Loading…' }));

    const status = document.getElementById('strikes-status-filter').value;
    let url = '/api/accountability/strikes';
    if (status) url += '?status=' + status;

    let strikes;
    try {
        const res = await fetch(url);
        if (!res.ok) throw new Error();
        strikes = await res.json();
    } catch {
        container.replaceChildren(Object.assign(document.createElement('p'), { textContent: 'Failed to load strikes.' }));
        return;
    }

    container.replaceChildren();

    if (!strikes.length) {
        container.appendChild(Object.assign(document.createElement('p'), { textContent: 'No strikes found.' }));
        return;
    }

    const table = document.createElement('table');
    table.className = 'data-table';
    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    ['Member', 'Type', 'Reason', 'Date', 'Status', CAN_MANAGE ? 'Actions' : ''].filter(Boolean).forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        headerRow.appendChild(th);
    });
    thead.appendChild(headerRow);
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    strikes.forEach(s => {
        const tr = document.createElement('tr');

        const tdMember = document.createElement('td');
        const link = document.createElement('a');
        link.href = '/accountability/' + s.member_id;
        link.textContent = s.member_name + ' (' + s.member_rank + ')';
        tdMember.appendChild(link);

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
        tdStatus.appendChild(statusSpan);

        tr.append(tdMember, tdType, tdReason, tdDate, tdStatus);

        if (CAN_MANAGE) {
            const tdAct = document.createElement('td');
            if (s.status === 'active') {
                const excuseBtn = document.createElement('button');
                excuseBtn.className = 'btn btn-secondary btn-sm';
                excuseBtn.textContent = 'Excuse';
                excuseBtn.addEventListener('click', () => excuseStrikeInline(s.id, excuseBtn, tr));
                tdAct.appendChild(excuseBtn);
            }
            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.textContent = 'Delete';
            delBtn.style.marginLeft = s.status === 'active' ? '6px' : '0';
            delBtn.addEventListener('click', () => deleteStrikeInline(s.id, delBtn, tr));
            tdAct.appendChild(delBtn);
            tr.appendChild(tdAct);
        }

        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    container.appendChild(table);
}

function excuseStrikeInline(strikeID, btn, tr) {
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
        if (res.ok) { strikesLoaded = false; loadStrikes(); }
    });
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); btn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    tr.querySelector('td:last-child').appendChild(confirmSpan);
}

function deleteStrikeInline(strikeID, btn, tr) {
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
        if (res.ok) { strikesLoaded = false; loadStrikes(); loadMembers(); }
    });
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); btn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    tr.querySelector('td:last-child').appendChild(confirmSpan);
}

// --- Tab: Storm Attendance ---

async function loadStormAttendance() {
    const date = document.getElementById('storm-date-input').value;
    if (!date) return;

    const container = document.getElementById('storm-member-list');
    container.replaceChildren(Object.assign(document.createElement('p'), { className: 'loading-msg', textContent: 'Loading…' }));

    let members;
    try {
        const res = await fetch('/api/accountability/storm-attendance?date=' + encodeURIComponent(date));
        if (!res.ok) throw new Error();
        members = await res.json();
    } catch {
        container.replaceChildren(Object.assign(document.createElement('p'), { textContent: 'Failed to load attendance.' }));
        return;
    }

    container.replaceChildren();

    members.forEach(m => {
        const row = document.createElement('div');
        row.className = 'storm-member-row';
        row.dataset.memberId = m.member_id;

        const nameSpan = document.createElement('span');
        nameSpan.className = 'storm-member-name';
        nameSpan.textContent = m.member_name + ' (' + m.member_rank + ')';

        const statusSel = document.createElement('select');
        statusSel.className = 'form-input';
        [
            ['not_enrolled', 'Not Enrolled'],
            ['attended',     'Attended'],
            ['no_show',      'No-Show'],
            ['excused',      'Excused'],
        ].forEach(([val, lbl]) => {
            const opt = document.createElement('option');
            opt.value = val;
            opt.textContent = lbl;
            if (val === m.status) opt.selected = true;
            statusSel.appendChild(opt);
        });

        const excuseInput = document.createElement('input');
        excuseInput.type = 'text';
        excuseInput.className = 'form-input';
        excuseInput.placeholder = 'Excuse reason';
        excuseInput.value = m.excuse_reason || '';
        excuseInput.style.display = m.status === 'excused' ? '' : 'none';

        statusSel.addEventListener('change', () => {
            excuseInput.style.display = statusSel.value === 'excused' ? '' : 'none';
        });

        row.append(nameSpan, statusSel, excuseInput);
        container.appendChild(row);
    });

    document.getElementById('btn-storm-save').style.display = '';
    document.getElementById('storm-save-status').textContent = '';
}

async function saveStormAttendance() {
    const date = document.getElementById('storm-date-input').value;
    const statusEl = document.getElementById('storm-save-status');
    if (!date) { statusEl.textContent = 'Select a date first.'; return; }

    const records = [];
    document.querySelectorAll('.storm-member-row').forEach(row => {
        const memberID    = parseInt(row.dataset.memberId, 10);
        const statusSel   = row.querySelector('select');
        const excuseInput = row.querySelector('input[type=text]');
        records.push({
            member_id:     memberID,
            status:        statusSel.value,
            excuse_reason: excuseInput.value.trim(),
        });
    });

    const res = await fetch('/api/accountability/storm-attendance', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ storm_date: date, records }),
    });

    if (!res.ok) { statusEl.textContent = 'Save failed.'; return; }
    statusEl.textContent = '✓ Saved';
    setTimeout(() => { statusEl.textContent = ''; }, 3000);
}

// --- Tab: Report ---

function statCard(value, label) {
    const card = document.createElement('div');
    card.className = 'acc-stat-card';
    const val = document.createElement('div');
    val.className = 'acc-stat-card-value';
    val.textContent = value;
    const lbl = document.createElement('div');
    lbl.className = 'acc-stat-card-label';
    lbl.textContent = label;
    card.append(val, lbl);
    return card;
}

function memberListEl(members, valueFormatter) {
    if (!members || !members.length) {
        return Object.assign(document.createElement('p'), { textContent: 'No data.' });
    }
    const list = document.createElement('ul');
    list.className = 'dash-list';
    members.forEach(m => {
        const li = document.createElement('li');
        const nameSpan = document.createElement('span');
        nameSpan.className = 'dash-list-name';
        nameSpan.textContent = m.name + ' (' + m.rank + ')';
        const valSpan = document.createElement('span');
        valSpan.className = 'dash-list-value';
        valSpan.textContent = valueFormatter(m.value);
        li.append(nameSpan, valSpan);
        list.appendChild(li);
    });
    return list;
}

async function loadReport() {
    let report;
    try {
        const res = await fetch('/api/accountability/report-data');
        if (!res.ok) throw new Error();
        report = await res.json();
    } catch {
        document.getElementById('report-stat-cards').replaceChildren(
            Object.assign(document.createElement('p'), { textContent: 'Failed to load report.' })
        );
        return;
    }

    const total = (report.tag_counts['At Risk'] || 0) +
                  (report.tag_counts['Needs Improvement'] || 0) +
                  (report.tag_counts['Reliable'] || 0);
    const reliablePct = total ? Math.round(((report.tag_counts['Reliable'] || 0) / total) * 100) + '%' : '—';

    document.getElementById('report-stat-cards').replaceChildren(
        statCard(String(total), 'Total Members'),
        statCard(reliablePct, 'Reliable'),
        statCard(String(report.tag_counts['Needs Improvement'] || 0), 'Needs Improvement'),
        statCard(String(report.tag_counts['At Risk'] || 0), 'At Risk'),
        statCard(String(report.total_strikes), 'Active Strikes'),
    );

    document.getElementById('report-vs-leaders').replaceChildren(
        memberListEl(report.vs_leaders, fmtNumber)
    );

    const vsUnderEl = document.getElementById('report-vs-under');
    if (!report.vs_underperformers || !report.vs_underperformers.length) {
        vsUnderEl.replaceChildren(Object.assign(document.createElement('p'), {
            textContent: '✅ All members meeting VS minimum.',
        }));
    } else {
        vsUnderEl.replaceChildren(memberListEl(report.vs_underperformers, fmtNumber));
    }

    document.getElementById('report-power-growth').replaceChildren(
        memberListEl(report.power_growth, v => '+' + fmtNumber(v))
    );

    const tagEl = document.getElementById('report-tag-counts');
    tagEl.replaceChildren();
    [
        ['Reliable',          'acc-tag--reliable'],
        ['Needs Improvement', 'acc-tag--needs-improvement'],
        ['At Risk',           'acc-tag--at-risk'],
    ].forEach(([tag, cls]) => {
        const row = document.createElement('div');
        row.style.cssText = 'display:flex;justify-content:space-between;align-items:center;padding:6px 0;border-bottom:1px solid var(--border-color);';
        const label = document.createElement('span');
        label.className = 'acc-tag ' + cls;
        label.textContent = tag;
        const count = document.createElement('span');
        count.style.fontWeight = '600';
        count.textContent = String(report.tag_counts[tag] || 0);
        row.append(label, count);
        tagEl.appendChild(row);
    });
}

// --- Boot ---

document.addEventListener('DOMContentLoaded', () => {
    setupTabs();
    loadMembers();

    document.getElementById('member-search').addEventListener('input', loadMembers);

    if (CAN_MANAGE) {
        document.getElementById('btn-strike-save').addEventListener('click', saveStrike);
        document.getElementById('btn-strike-cancel').addEventListener('click', closeStrikeModal);

        const addVSBtn = document.getElementById('btn-add-vs-strikes');
        if (addVSBtn) addVSBtn.addEventListener('click', addVSStrikesForBelow);

        document.getElementById('btn-storm-load').addEventListener('click', loadStormAttendance);
        document.getElementById('btn-storm-save').addEventListener('click', saveStormAttendance);
    }

    document.getElementById('strikes-status-filter').addEventListener('change', () => {
        strikesLoaded = false;
        loadStrikes();
    });
});
