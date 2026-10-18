'use strict';

const cfg = document.getElementById('page-config').dataset;
const CAN_MANAGE = cfg.canManage === 'true';

// --- Helpers ---

function tagClass(tag) {
    if (tag === 'At Risk')           return 'acc-tag acc-tag--at-risk';
    if (tag === 'Needs Improvement') return 'acc-tag acc-tag--needs-improvement';
    return 'acc-tag acc-tag--reliable';
}

function strikeTypeLabel(t) {
    return StrikeTypes.label(t);
}

// Resolves once the category list has loaded (or failed — labels then fall back to
// the raw key). Anything that renders a strike's category awaits it.
let strikeTypesReady = Promise.resolve();

// --- Tabs ---

// Tabs.init (tabs.js) calls this for the initial tab too, so a deep link such as
// /accountability#strikes lazy-loads its panel exactly as a click would. (The weekly
// report that was the Report tab is on the Reports page now.)
let strikesLoaded = false;
let participationLoaded = false;

function onTabActivated(tab) {
    if (tab === 'strikes' && !strikesLoaded) {
        strikesLoaded = true;
        loadStrikes();
    }
    if (tab === 'participation' && !participationLoaded) {
        participationLoaded = true;
        loadParticipationChips();
        loadParticipation();
    }
}

// --- Add Strike modal ---

let allMembers = [];

// Flatpickr instances — initialised in DOMContentLoaded
let strikeRefDateFP = null;

function openStrikeModal(memberID, memberName, preType) {
    document.getElementById('strike-member-id').value = memberID;
    document.getElementById('strike-member-name').value = memberName;
    const typeSelect = document.getElementById('strike-type');
    typeSelect.value = preType || '';
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
    const memberID   = parseInt(document.getElementById('strike-member-id').value, 10);
    const strikeType = document.getElementById('strike-type').value;
    const reason   = document.getElementById('strike-reason').value.trim();
    const refDate  = document.getElementById('strike-ref-date').value;
    const status   = document.getElementById('strike-modal-status');
    if (!strikeType) { status.textContent = 'Category is required.'; return; }
    if (!reason) { status.textContent = 'Reason is required.'; return; }

    const res = await fetch('/api/accountability/strikes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ member_id: memberID, strike_type: strikeType, reason, ref_date: refDate }),
    });
    if (!res.ok) {
        status.textContent = res.status === 400 ? (await res.text()).trim() : 'Failed to save strike.';
        return;
    }
    closeStrikeModal();
    loadMembers();
    strikesLoaded = false; // invalidate cache so next tab-switch refetches
}

// The weeks the verdicts were judged on, from the members API (context): the notes on the
// VS heading, the donation column's week and every strike's ref_date come from here, never
// from the browser's clock (private-docs 266), and they refresh with every load.
let judgedContext = null;

// Evaluated-week notes on the VS-below heading: an override is always named, "(last
// week)" on the Monday fallback, and an import-lag note when not all completed days are
// imported, so officers know which week and how much data the flags reflect.
function applyVSWeekNotes(ctx) {
    const heading = document.getElementById('vs-flag-heading');
    if (!heading) return;
    heading.querySelectorAll('.acc-week-note').forEach(n => n.remove());
    const note = (text, title) => {
        const s = document.createElement('span');
        s.className = 'acc-week-note';
        s.textContent = text;
        if (title) s.title = title;
        heading.appendChild(s);
    };
    if (ctx.vs_minimum_source === 'override') {
        note(` (${formatCompact(ctx.vs_minimum)}, this week's override)`);
    }
    if (ctx.vs_fallback) {
        note(' (last week)', 'Current VS week has no completed days yet — showing last week');
    }
    if (ctx.vs_imported_completed < ctx.vs_completed) {
        note(` (flags based on ${ctx.vs_imported_completed} of ${ctx.vs_completed} completed days imported so far)`);
    }
}

// "2026-10-06" → "6 Oct", for naming a donation week in prose.
function shortWeek(date) {
    const [y, m, d] = date.split('-').map(Number);
    return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString('en-GB', { day: 'numeric', month: 'short', timeZone: 'UTC' });
}

// The donation strikes to file, as the server built them: one per member and judged week
// (decision 23), each with its week and reason.
function donationStrikeTargets(members) {
    const out = [];
    members.forEach(m => (m.donation_strikes || []).forEach(t => out.push({ member: m, week: t.week, rules: t.rules, reason: t.reason })));
    return out;
}

function renderDonationBanner(members, ctx) {
    const banner = document.getElementById('donation-flag-banner');
    if (!banner) return;
    const names = document.getElementById('donation-flag-names');
    const note = document.getElementById('donation-flag-note');
    const targets = donationStrikeTargets(members);
    names.replaceChildren();
    targets.forEach((t, i) => {
        if (i) names.appendChild(document.createTextNode(', '));
        names.appendChild(noTranslate(Object.assign(document.createElement('span'), { textContent: t.member.name })));
        const rules = t.rules.map(r => r === 'daily' ? `daily, ${t.member.donation_days_below} days` : r);
        names.appendChild(document.createTextNode(` (${rules.join(' and ')}, week of ${shortWeek(t.week)})`));
    });
    const dailyOn = ctx.donation_daily_minimums.daily > 0;
    note.textContent = dailyOn && ctx.donation_judged_days === 0
        ? 'Daily rule: no exact day yet — readings on two consecutive days make a day exact.'
        : '';
    const addBtn = document.getElementById('btn-add-donation-strikes');
    if (addBtn) addBtn.style.display = targets.length ? '' : 'none';
    banner.style.display = targets.length || note.textContent ? 'flex' : 'none';
}

// The Donations column names the daily rule's week, which is the week its total is for.
function applyDonationColumnHeading(ctx) {
    const th = document.getElementById('donations-col-head');
    if (th) th.textContent = `Donations (week of ${shortWeek(ctx.donation_daily_week)})`;
}

function donationCell(m) {
    const td = document.createElement('td');
    const total = document.createElement('span');
    if (m.donation_total_status === 'blank') {
        total.className = 'don-blank';
        total.textContent = '—';
        total.title = 'No donation reading this week';
    } else {
        total.className = m.donation_total_status === 'so_far' ? 'don-so-far' : '';
        total.textContent = formatCompact(m.donation_total);
        if (m.donation_total_status === 'so_far') total.title = 'So far: the week is still running, or was not read on Sunday';
    }
    td.appendChild(total);
    td.dataset.exportValue = m.donation_total_status === 'blank' ? '' : String(m.donation_total);
    if (m.donation_below_weekly) {
        const b = Object.assign(document.createElement('span'), { className: 'don-badge', textContent: 'weekly' });
        b.title = 'Below last week\'s weekly minimum';
        td.appendChild(b);
    }
    if (m.donation_below_daily) {
        const b = Object.assign(document.createElement('span'), { className: 'don-badge', textContent: `${m.donation_days_below} days` });
        b.title = 'Below the daily minimum on this many days';
        td.appendChild(b);
    }
    return td;
}

// --- Tab: Members ---

async function loadMembers() {
    const tbody = document.getElementById('members-tbody');
    const loadingRow = document.createElement('tr');
    const loadingCell = document.createElement('td');
    loadingCell.colSpan = 7;
    loadingCell.className = 'loading-msg';
    loadingCell.textContent = 'Loading…';
    loadingRow.appendChild(loadingCell);
    tbody.replaceChildren(loadingRow);

    let members, ctx;
    try {
        const res = await fetch('/api/accountability/members');
        if (!res.ok) throw new Error();
        ({ members, context: ctx } = await res.json());
    } catch (err) {
        console.error('loadMembers:', err);
        loadingCell.textContent = 'Failed to load members.';
        return;
    }

    allMembers = members;
    judgedContext = ctx;
    applyVSWeekNotes(ctx);
    applyDonationColumnHeading(ctx);
    renderDonationBanner(members, ctx);

    // VS flag banner
    const below = members.filter(m => m.below_threshold);
    const banner = document.getElementById('vs-flag-banner');
    if (below.length) {
        document.getElementById('vs-flag-names').textContent = below.map(m => m.name).join(', ');
        banner.style.display = 'flex';
    } else {
        banner.style.display = 'none';
    }

    // Search hides rows (QuickSearch), so this renders the whole roster: the
    // table is data-export-csv and hiding lets the export scope toggle offer
    // the full roster as well as the filtered view.
    const filtered = members;

    tbody.replaceChildren();

    if (!filtered.length) {
        const emptyRow = document.createElement('tr');
        const emptyCell = document.createElement('td');
        emptyCell.colSpan = 7;
        emptyCell.textContent = 'No members found.';
        emptyRow.appendChild(emptyCell);
        tbody.appendChild(emptyRow);
        return;
    }

    filtered.forEach(m => {
        const tr = document.createElement('tr');
        if (m.below_threshold) tr.className = 'acc-row--below-vs';
        else if (m.donation_below_weekly || m.donation_below_daily) tr.className = 'acc-row--below-donation';
        tr.dataset.search = m.name + ' ' + m.rank;

        const tdName = document.createElement('td');
        tdName.textContent = m.name;

        const tdRank = document.createElement('td');
        const rankChip = document.createElement('span');
        rankChip.className = `member-rank rank-${m.rank}`;
        rankChip.textContent = m.rank;
        tdRank.appendChild(rankChip);

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
        vsSpan.textContent = formatCompact(m.vs_total);
        tdVS.appendChild(vsSpan);
        tdVS.dataset.exportValue = m.vs_total != null ? String(m.vs_total) : '';

        const tdActions = document.createElement('td');
        const actionWrap = document.createElement('div');
        actionWrap.className = 'row-actions';
        // A link, so not rowActionBtn — but the same icon + collapsing label.
        const viewBtn = document.createElement('a');
        viewBtn.href = '/accountability/' + m.id;
        viewBtn.className = 'btn btn-secondary btn-sm';
        viewBtn.title = 'Profile';
        viewBtn.setAttribute('aria-label', 'Profile');
        const viewLabel = document.createElement('span');
        viewLabel.className = 'action-label';
        viewLabel.textContent = 'Profile';
        viewBtn.append(svgIcon('user'), viewLabel);
        actionWrap.appendChild(viewBtn);

        if (CAN_MANAGE) {
            actionWrap.appendChild(rowActionBtn('btn btn-danger btn-sm', 'plus', 'Strike',
                () => openStrikeModal(m.id, m.name, null)));
        }
        tdActions.appendChild(actionWrap);

        tr.append(tdName, tdRank, tdTag, tdStrikes, tdVS, donationCell(m), tdActions);
        tbody.appendChild(tr);
    });
    QuickSearch.apply('member-search');
}

async function addVSStrikesForBelow() {
    const below = allMembers.filter(m => m.below_threshold);
    if (!below.length || !judgedContext) return;

    // The week the flags were judged on, from the server (private-docs 266).
    const refDate = judgedContext.vs_week;
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
                reason:      `VS below minimum (week of ${shortWeek(refDate)})`,
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

async function addDonationStrikesForBelow() {
    if (!judgedContext) return;
    const targets = donationStrikeTargets(allMembers);
    if (!targets.length) return;
    const banner = document.getElementById('donation-flag-banner');
    const btn = document.getElementById('btn-add-donation-strikes');
    if (btn) btn.disabled = true;
    let added = 0, skipped = 0, failed = 0;
    for (const t of targets) {
        const res = await fetch('/api/accountability/strikes', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                member_id:   t.member.id,
                strike_type: 'donation_below_minimum',
                reason:      'Donations below minimum: ' + t.reason,
                ref_date:    t.week,
            }),
        });
        if (res.status === 409) skipped++;
        else if (res.ok) added++;
        else failed++;
    }
    if (btn) btn.disabled = false;

    const parts = [];
    if (added) parts.push(`✓ ${added} strike${added !== 1 ? 's' : ''} added`);
    if (skipped) parts.push(`${skipped} already had a donation strike for that week`);
    if (failed) parts.push(`${failed} failed`);
    let statusEl = banner.querySelector('.vs-add-status');
    if (!statusEl) {
        statusEl = document.createElement('span');
        statusEl.className = 'vs-add-status';
        statusEl.style.cssText = 'margin-left:12px;font-size:0.85em;';
        banner.appendChild(statusEl);
    }
    statusEl.textContent = parts.join(', ') + '.';
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
    await strikeTypesReady;

    const status = document.getElementById('strikes-status-filter').value;
    let url = '/api/accountability/strikes';
    if (status) url += '?status=' + status;

    let strikes;
    try {
        const res = await fetch(url);
        if (!res.ok) throw new Error();
        strikes = await res.json();
    } catch (err) {
        console.error('loadStrikes:', err);
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
        tr.dataset.search = s.member_name + ' ' + s.member_rank + ' ' + strikeTypeLabel(s.strike_type);

        const tdMember = document.createElement('td');
        const link = noTranslate(document.createElement('a'));
        link.href = '/accountability/' + s.member_id;
        link.textContent = s.member_name + ' ';
        const linkRankChip = document.createElement('span');
        linkRankChip.className = `member-rank rank-${s.member_rank}`;
        linkRankChip.textContent = s.member_rank;
        link.appendChild(linkRankChip);
        tdMember.appendChild(link);

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
        tdStatus.appendChild(statusSpan);

        tr.append(tdMember, tdType, tdReason, tdDate, tdStatus);

        if (CAN_MANAGE) {
            const tdAct = document.createElement('td');
            const actionWrap = document.createElement('div');
            actionWrap.className = 'row-actions';
            if (s.status === 'active') {
                actionWrap.appendChild(
                    rowActionBtn('btn btn-secondary btn-sm', 'check', 'Excuse', () => excuseStrikeInline(s.id))
                );
            }
            actionWrap.appendChild(
                rowActionBtn('btn btn-danger btn-sm', 'trash', 'Delete', () => deleteStrikeInline(s.id))
            );
            tdAct.appendChild(actionWrap);
            tr.appendChild(tdAct);
        }

        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    container.appendChild(table);
    QuickSearch.apply('strikes-search');
}

async function excuseStrikeInline(strikeID) {
    if (!await showConfirm('Excuse this strike?', 'Excuse')) return;
    const res = await fetch('/api/accountability/strikes/' + strikeID, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: 'excused', excused_reason: '' }),
    });
    if (res.ok) { strikesLoaded = false; loadStrikes(); }
}

async function deleteStrikeInline(strikeID) {
    if (!await showConfirm('Delete this strike?', 'Delete')) return;
    const res = await fetch('/api/accountability/strikes/' + strikeID, { method: 'DELETE' });
    if (res.ok) { strikesLoaded = false; loadStrikes(); loadMembers(); }
}

// --- Tab: Participation ---
//
// Recorded boards only, newest first. An event nobody recorded is not listed:
// participation is optional, so a missing board says nothing about anyone.

let participationType = 0;   // 0 = every tracked type

async function loadParticipationChips() {
    const box = document.getElementById('participation-chips');
    let types = [];
    try {
        const res = await fetch('/api/participation/types');
        if (!res.ok) throw new Error('HTTP ' + res.status);
        types = await res.json();
    } catch (err) {
        console.error('loadParticipationChips:', err);
        return;
    }
    const chip = (id, label) => {
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'filter-chip' + (id === participationType ? ' active' : '');
        b.textContent = label;
        b.addEventListener('click', () => {
            participationType = id;
            box.querySelectorAll('.filter-chip').forEach(c => c.classList.toggle('active', c === b));
            loadParticipation();
        });
        return b;
    };
    box.replaceChildren(chip(0, 'All'), ...types.map(t => chip(t.event_type_id, (t.icon ? t.icon + ' ' : '') + t.name)));
}

async function loadParticipation() {
    const tbody = document.getElementById('participation-tbody');
    const loading = document.createElement('tr');
    loading.appendChild(Object.assign(document.createElement('td'), { colSpan: 9, className: 'loading-msg', textContent: 'Loading…' }));
    tbody.replaceChildren(loading);
    let boards;
    try {
        const res = await fetch('/api/participation/boards' + (participationType ? '?type=' + participationType : ''));
        if (!res.ok) throw new Error('HTTP ' + res.status);
        boards = await res.json();
    } catch (err) {
        console.error('loadParticipation:', err);
        const tr = document.createElement('tr');
        tr.appendChild(Object.assign(document.createElement('td'), { colSpan: 9, className: 'empty-state', textContent: 'Failed to load participation boards.' }));
        tbody.replaceChildren(tr);
        return;
    }
    if (!boards.length) {
        const tr = document.createElement('tr');
        tr.appendChild(Object.assign(document.createElement('td'), {
            colSpan: 9, className: 'empty-state',
            textContent: participationType ? 'No boards recorded for this event yet.' : 'No participation boards recorded yet.',
        }));
        tbody.replaceChildren(tr);
        return;
    }
    tbody.replaceChildren(...boards.map(b => {
        const tr = document.createElement('tr');
        const td = text => Object.assign(document.createElement('td'), { textContent: text });
        const open = document.createElement('a');
        open.href = '/participation/' + b.event_id;
        open.className = 'btn btn-secondary btn-sm';
        open.title = 'Open';
        open.setAttribute('aria-label', 'Open');
        const label = Object.assign(document.createElement('span'), { className: 'action-label', textContent: 'Open' });
        open.append(svgIcon('eye'), label);
        const openTd = document.createElement('td');
        openTd.appendChild(open);
        const pending = td(String(b.pending));
        if (b.pending) pending.className = 'acc-status--active';
        tr.append(
            td(b.event_date),
            td((b.type_icon ? b.type_icon + ' ' : '') + b.type_name),
            td(b.task_force || '—'),
            td(b.matched === b.rows ? String(b.rows) : `${b.rows} (${b.matched} matched)`),
            td(String(b.missed)),
            td(String(b.excused)),
            pending,
            td(b.recorded_by || '—'),
            openTd);
        return tr;
    }));
}

// --- Strike categories (manage_accountability) ---
//
// One row per category. Every change persists as it is made — a label on blur, the
// Active box on change, a move as one PUT per row whose position changed — so the
// strike form and the Strikes tab behind the modal never disagree with what is on
// screen. Keys are shown, never edited: a key is the value stored on every strike.

function strikeTypePayload(tr) {
    return {
        label: tr.querySelector('.acc-type-label').value.trim(),
        active: tr.querySelector('.acc-type-active').checked,
        sort_order: rowPosition(tr),
    };
}

async function putStrikeType(tr) {
    const payload = strikeTypePayload(tr);
    if (!payload.label) { showToast('A category needs a label.', 'error'); return false; }
    const res = await fetch('/api/accountability/strike-types/' + tr.dataset.id, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
    });
    if (!res.ok) {
        showToast((await res.text()).trim() || 'Could not save that category.', 'error', 6000);
        return false;
    }
    tr.dataset.sortOrder = payload.sort_order;
    return true;
}

async function afterStrikeTypesChange() {
    strikeTypesReady = StrikeTypes.load()
        .then(() => StrikeTypes.fillSelect(document.getElementById('strike-type')))
        .catch(err => console.error('StrikeTypes.load:', err));
    await strikeTypesReady;
    if (strikesLoaded) loadStrikes();
}

function buildStrikeTypeRow(t) {
    const tr = document.createElement('tr');
    tr.dataset.id = t.id;
    tr.dataset.sortOrder = t.sort_order;

    const tdOrder = document.createElement('td');
    tdOrder.appendChild(buildOrderButtons(tr, persistStrikeTypeOrder));

    const tdLabel = document.createElement('td');
    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'form-input acc-type-label';
    input.value = t.label;
    input.maxLength = 60;
    input.setAttribute('aria-label', 'Label');
    let saved = t.label;
    input.addEventListener('change', async () => {
        if (await putStrikeType(tr)) {
            saved = input.value.trim();
            afterStrikeTypesChange();
        } else {
            input.value = saved;
        }
    });
    // The key sits under the label as a caption: it is what strikes store, so it is
    // worth seeing, but it is never edited.
    const meta = document.createElement('div');
    meta.className = 'acc-types-meta';
    const key = document.createElement('code');
    key.className = 'acc-types-key';
    key.textContent = t.key;
    meta.appendChild(noTranslate(key));
    if (t.is_system) {
        const badge = document.createElement('span');
        badge.className = 'acc-type-system';
        badge.textContent = 'System';
        meta.appendChild(badge);
    }
    if (t.in_use) {
        const used = document.createElement('span');
        used.className = 'acc-types-used';
        used.textContent = t.in_use + (t.in_use === 1 ? ' strike' : ' strikes');
        meta.appendChild(used);
    }
    tdLabel.append(input, meta);

    const tdActive = document.createElement('td');
    const box = document.createElement('input');
    box.type = 'checkbox';
    box.className = 'acc-type-active';
    box.checked = t.active;
    box.disabled = t.is_system;
    box.setAttribute('aria-label', 'Active');
    if (t.is_system) box.title = 'System categories are always active';
    box.addEventListener('change', async () => {
        if (await putStrikeType(tr)) afterStrikeTypesChange();
        else box.checked = !box.checked;
    });
    tdActive.appendChild(box);

    const tdAct = document.createElement('td');
    if (!t.is_system && !t.in_use) {
        tdAct.appendChild(rowActionBtn('btn btn-danger btn-sm', 'trash', 'Delete', async () => {
            if (!await showConfirm(`Delete the category "${t.label}"?`, 'Delete')) return;
            const res = await fetch('/api/accountability/strike-types/' + t.id, { method: 'DELETE' });
            if (!res.ok) {
                showToast((await res.text()).trim() || 'Could not delete that category.', 'error', 6000);
                return;
            }
            const tbody = tr.parentNode;
            tr.remove();
            if (tbody) refreshOrderButtons(tbody);
            showToast('Category deleted.');
            afterStrikeTypesChange();
        }));
    }

    tr.append(tdOrder, tdLabel, tdActive, tdAct);
    return tr;
}

async function persistStrikeTypeOrder() {
    const tbody = document.getElementById('strike-types-tbody');
    const moved = Array.from(tbody.children).filter((tr, i) => String(tr.dataset.sortOrder) !== String(i));
    let ok = true;
    for (const tr of moved) ok = (await putStrikeType(tr)) && ok;
    if (moved.length) afterStrikeTypesChange();
    if (!ok) renderStrikeTypesTable();
}

async function renderStrikeTypesTable() {
    const tbody = document.getElementById('strike-types-tbody');
    await strikeTypesReady;
    const types = StrikeTypes.all();
    if (!types.length) {
        const td = Object.assign(document.createElement('td'), { colSpan: 4, className: 'empty-state', textContent: 'No categories could be loaded.' });
        const tr = document.createElement('tr');
        tr.appendChild(td);
        tbody.replaceChildren(tr);
        return;
    }
    tbody.replaceChildren(...types.map(buildStrikeTypeRow));
    refreshOrderButtons(tbody);
}

// Mirrors slugStrikeTypeKey (handlers_strike_types.go) so the preview shows the key
// the server will mint; the server's answer is what is stored either way.
function previewStrikeTypeKey(label) {
    return foldSearch(label).replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 40).replace(/_+$/, '');
}

function initStrikeTypesModal() {
    const modal = document.getElementById('strike-types-modal');
    const open = async () => {
        modal.style.display = 'flex';
        trapFocus(modal);
        await afterStrikeTypesChange();
        renderStrikeTypesTable();
    };
    const close = () => { releaseFocus(modal); modal.style.display = ''; };
    document.getElementById('btn-strike-types').addEventListener('click', open);
    document.getElementById('btn-strike-types-close').addEventListener('click', close);
    modal.addEventListener('click', e => { if (e.target === modal) close(); });

    const labelIn = document.getElementById('strike-type-new-label');
    const keyOut = document.getElementById('strike-type-new-key');
    labelIn.addEventListener('input', () => {
        const k = previewStrikeTypeKey(labelIn.value);
        keyOut.textContent = k ? 'key: ' + k : '';
    });
    const add = async () => {
        const label = labelIn.value.trim();
        if (!label) { setFieldError(labelIn, 'Enter a label.'); return; }
        clearFieldError(labelIn);
        const res = await fetch('/api/accountability/strike-types', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ label }),
        });
        if (!res.ok) {
            setFieldError(labelIn, (await res.text()).trim() || 'Could not add that category.');
            return;
        }
        labelIn.value = '';
        keyOut.textContent = '';
        showToast('Category added.');
        await afterStrikeTypesChange();
        renderStrikeTypesTable();
    };
    document.getElementById('btn-strike-type-add').addEventListener('click', add);
    labelIn.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); add(); } });
}

// --- Boot ---

document.addEventListener('DOMContentLoaded', () => {
    strikeRefDateFP = flatpickr('#strike-ref-date', { dateFormat: 'Y-m-d', allowInput: true });

    // Before Tabs.init: a deep link to #strikes renders straight away and needs labels.
    strikeTypesReady = StrikeTypes.load()
        .then(() => { if (CAN_MANAGE) StrikeTypes.fillSelect(document.getElementById('strike-type')); })
        .catch(err => console.error('StrikeTypes.load:', err));

    Tabs.init({ hash: true, defaultTab: 'members', onActivate: onTabActivated });
    loadMembers();

    QuickSearch.attach({
        input: 'member-search', container: 'members-tbody', rows: 'tr',
        emptyText: 'No members match your search.',
    });

    // The strikes table is built in JS with no id, so anchor on its container.
    QuickSearch.attach({
        input: 'strikes-search', container: 'strikes-container', rows: 'tbody > tr',
        emptyText: 'No strikes match your search.',
    });

    if (CAN_MANAGE) {
        initStrikeTypesModal();

        document.getElementById('btn-strike-save').addEventListener('click', saveStrike);
        document.getElementById('btn-strike-cancel').addEventListener('click', closeStrikeModal);

        const addVSBtn = document.getElementById('btn-add-vs-strikes');
        if (addVSBtn) addVSBtn.addEventListener('click', addVSStrikesForBelow);
        const addDonationBtn = document.getElementById('btn-add-donation-strikes');
        if (addDonationBtn) addDonationBtn.addEventListener('click', addDonationStrikesForBelow);
    }

    document.getElementById('strikes-status-filter').addEventListener('change', () => {
        strikesLoaded = false;
        loadStrikes();
    });
});
