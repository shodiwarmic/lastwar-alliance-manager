// participation.js — the participation recording screen (#13).
//
//   /participation/new        choose a type, then a past occurrence (or create one)
//   /participation/{eventID}  import a CSV or add members → check → save → suggested strikes
//
// The server derives every status and every suggestion (participation.go); this
// page only edits what is stored — the rows the mail listed, the roles for the
// battle, and the officer's judgements — and renders what comes back.
'use strict';

const cfg = document.getElementById('page-config').dataset;
const CAN_MANAGE = cfg.canManage === 'true';
const PP = window.ParticipationParse;

const pathMatch = location.pathname.match(/^\/participation\/(\d+)$/);
const EVENT_ID = pathMatch ? parseInt(pathMatch[1], 10) : 0;

const STATUS_LABEL = { present: 'Present', zero: 'Zero', missed: 'Missed', excused: 'Excused' };

let detail = null;   // GET /api/participation/boards/{id}
let rows = [];       // working entries in board order: {name, member_id, member_name, member_rank, values:{key:number|null}}
let roles = [];      // working roles:   {member_id, member_name, role, task_force}
let pickers = [];    // member pickers to destroy on re-render

// --- Helpers ----------------------------------------------------------------------

function el(tag, props, ...children) {
    const node = document.createElement(tag);
    if (props) {
        Object.entries(props).forEach(([k, v]) => {
            if (v == null) return;
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

function badge(status) {
    return el('span', { className: 'pt-badge pt-badge--' + status }, STATUS_LABEL[status] || status);
}

function nameSpan(name) {
    return noTranslate(el('span', null, name));
}

function trackables() {
    return (detail && detail.type && detail.type.trackables) || [];
}

function primaryKey() {
    const t = trackables();
    return t.length ? t[0].key : '';
}

function isRoleType() {
    return detail && detail.type && detail.type.absence_rule === 'role';
}

function fmtValue(v) {
    return v == null ? '' : Number(v).toLocaleString('en-US');
}

function fmtDate(d) {
    const dt = new Date(d + 'T00:00:00Z');
    return isNaN(dt) ? d : dt.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' });
}

async function errorText(res, fallback) {
    try {
        const t = (await res.text()).trim();
        return t || fallback;
    } catch (err) {
        console.error('errorText:', err);
        return fallback;
    }
}

function setHeader(...nodes) {
    document.getElementById('pt-header').replaceChildren(...nodes);
}

function show(id, on) {
    const node = document.getElementById(id);
    if (node) node.hidden = !on;
}

// --- Occurrence chooser (/participation/new) ----------------------------------------

// Desert Storm is fought on game-day Fridays only: on any other date Create is
// disabled and the reason sits under the date, in the server's own words.
function checkNewDayRule(types) {
    const select = document.getElementById('pt-new-type');
    const dateIn = document.getElementById('pt-new-date');
    const btn = document.getElementById('pt-new-create');
    if (!dateIn || !btn) return true;
    const t = types.find(x => String(x.event_type_id) === select.value);
    const msg = t && t.short_name === 'DS' ? desertStormDayError(dateIn.value) : '';
    if (msg) setFieldError(dateIn, msg);
    else clearFieldError(dateIn);
    btn.disabled = !!msg;
    return !msg;
}

async function initNew() {
    setHeader(el('p', { className: 'pt-header-title' }, 'Record a board'));
    show('pt-new', true);
    let types = [];
    try {
        const res = await fetch('/api/participation/types');
        if (!res.ok) throw new Error('HTTP ' + res.status);
        types = await res.json();
    } catch (err) {
        console.error('initNew types:', err);
        document.getElementById('pt-new-recent').replaceChildren(el('p', { className: 'empty-state' }, 'Could not load the event types. Reload to try again.'));
        return;
    }
    const select = document.getElementById('pt-new-type');
    select.replaceChildren(...types.map(t => el('option', { value: String(t.event_type_id) }, (t.icon ? t.icon + ' ' : '') + t.name)));
    const wanted = new URLSearchParams(location.search).get('type');
    const pre = types.find(t => wanted && (t.short_name === wanted || String(t.event_type_id) === wanted));
    if (pre) select.value = String(pre.event_type_id);
    // A Desert Storm battle belongs to one task force, so creating one asks which.
    const syncTF = () => {
        const t = types.find(x => String(x.event_type_id) === select.value);
        const g = document.getElementById('pt-new-tf-group');
        if (g) g.hidden = !(t && t.absence_rule === 'role');
        checkNewDayRule(types);
    };
    select.addEventListener('change', () => { syncTF(); loadRecent(); });
    syncTF();
    if (!types.length) {
        document.getElementById('pt-new-recent').replaceChildren(el('p', { className: 'empty-state' }, 'No event type tracks participation yet.'));
        return;
    }
    const dateIn = document.getElementById('pt-new-date');
    if (dateIn) {
        dateIn.max = gameDateStr();
        dateIn.value = gameDateStr();
        document.getElementById('pt-new-create').addEventListener('click', createOccurrence);
        ['input', 'change'].forEach(ev => dateIn.addEventListener(ev, () => checkNewDayRule(types)));
        checkNewDayRule(types);
    }
    loadRecent();
}

async function loadRecent() {
    const typeID = document.getElementById('pt-new-type').value;
    const box = document.getElementById('pt-new-recent');
    box.replaceChildren(el('p', { className: 'loading-msg' }, 'Loading…'));
    let recent;
    try {
        const res = await fetch('/api/participation/recent?type=' + encodeURIComponent(typeID));
        if (!res.ok) throw new Error('HTTP ' + res.status);
        recent = await res.json();
    } catch (err) {
        console.error('loadRecent:', err);
        box.replaceChildren(el('p', { className: 'empty-state' }, 'Could not load recent events.'));
        return;
    }
    const typeName = document.getElementById('pt-new-type').selectedOptions[0]?.textContent || 'this type';
    if (!recent.length) {
        box.replaceChildren(el('p', { className: 'empty-state' }, `No past ${typeName} events are on the schedule.`));
        return;
    }
    box.replaceChildren(el('p', { className: 'pt-step-hint' }, 'Recent events on the schedule:'),
        el('ul', { className: 'pt-recent-list' }, ...recent.map(x => {
            const when = fmtDate(x.event_date) + (x.task_force ? ' · TF ' + x.task_force : '')
                + (x.all_day ? '' : ' · ' + x.event_time + ' ST');
            const state = x.has_board
                ? el('span', { className: 'pt-badge pt-badge--present' }, 'Recorded · ' + x.rows)
                : el('span', { className: 'pt-badge pt-badge--muted' }, 'Not recorded');
            const open = el('a', { className: 'btn btn-secondary btn-sm', href: '/participation/' + x.event_id },
                svgIcon(x.has_board ? 'eye' : 'clipboard-list'), x.has_board ? ' Open' : ' Record');
            return el('li', null, el('span', null, when), state, open);
        })));
}

async function createOccurrence() {
    const status = document.getElementById('pt-new-status');
    status.textContent = '';
    if (document.getElementById('pt-new-create').disabled) return;  // reason is under the date
    const body = {
        event_type_id: parseInt(document.getElementById('pt-new-type').value, 10),
        event_date: document.getElementById('pt-new-date').value,
        event_time: document.getElementById('pt-new-time').value || '00:00',
    };
    const tfGroup = document.getElementById('pt-new-tf-group');
    if (tfGroup && !tfGroup.hidden) body.task_force = document.getElementById('pt-new-tf').value;
    if (!body.event_date) { status.textContent = 'Pick the date the event ran.'; return; }
    let res;
    try {
        res = await fetch('/api/participation/occurrences', {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
        });
    } catch (err) {
        console.error('createOccurrence:', err);
        status.textContent = 'Network error — the event was not created.';
        return;
    }
    if (!res.ok) {
        // The Schedule page's own validation message, word for word.
        status.textContent = await errorText(res, 'Could not create that event.');
        return;
    }
    const out = await res.json();
    location.href = '/participation/' + out.id;
}

// --- Board -------------------------------------------------------------------------

async function loadBoard() {
    let res;
    try {
        res = await fetch('/api/participation/boards/' + EVENT_ID);
    } catch (err) {
        console.error('loadBoard:', err);
        setHeader(el('p', { className: 'empty-state' }, 'Network error — reload to try again.'));
        return;
    }
    if (!res.ok) {
        setHeader(el('p', { className: 'empty-state' }, await errorText(res, 'Could not load this event.')));
        return;
    }
    detail = await res.json();
    renderHeader();
    if (!detail.type) {
        hideAllSteps();
        return;
    }
    if (detail.event.event_date > detail.game_date) {
        hideAllSteps();
        return;
    }
    if (CAN_MANAGE) resetWorkingCopy();
    renderAll();
}

function hideAllSteps() {
    ['pt-add', 'pt-check', 'pt-board', 'pt-suggest'].forEach(id => show(id, false));
}

function renderHeader() {
    const ev = detail.event;
    const title = el('p', { className: 'pt-header-title' }, (ev.type_icon ? ev.type_icon + ' ' : '') + ev.type_name);
    const bits = [fmtDate(ev.event_date)];
    if (ev.task_force) bits.push('Task Force ' + ev.task_force);
    if (!ev.all_day) bits.push(ev.event_time + ' ST');
    if (ev.level != null) bits.push('Lv.' + ev.level);
    const meta = el('span', { className: 'pt-header-meta' }, bits.join(' · '));
    const nodes = [title, meta];
    if (!detail.type) {
        nodes.push(el('p', { className: 'empty-state', style: { width: '100%' } }, ev.type_name + ' does not track participation.'));
    } else if (ev.event_date > detail.game_date) {
        nodes.push(el('p', { className: 'empty-state', style: { width: '100%' } }, 'This event has not happened yet. A board can be recorded once it is over.'));
    } else if (detail.board) {
        const b = detail.board;
        const who = b.recorded_by ? 'Recorded by ' + b.recorded_by : 'Recorded';
        const status = el('span', { className: 'pt-badge pt-badge--present' }, who);
        nodes.push(status);
        if (b.source === 'legacy') {
            nodes.push(el('span', { className: 'pt-badge pt-badge--muted', title: 'Carried over from the old Storm Attendance screen, which recorded who came but not ranks, scores or roles.' }, 'From Storm Attendance'));
        }
    } else {
        nodes.push(el('span', { className: 'pt-badge pt-badge--muted' }, 'Not recorded'));
    }
    setHeader(...nodes);
}

function resetWorkingCopy() {
    // Entries come back ranked-first in rank order, so the array order IS the board
    // order and position gives the rank back on save.
    rows = detail.entries.map(e => ({
        name: e.name, member_id: e.member_id, member_name: e.member_name || '',
        member_rank: (detail.roster.find(m => m.id === e.member_id) || {}).rank || '',
        values: Object.fromEntries(trackables().map(t => [t.key, e.values[t.key] ?? null])),
    }));
    const src = detail.board ? detail.roles : (detail.roles_prefill || []);
    roles = src.map(r => ({ member_id: r.member_id, member_name: r.member_name, role: r.role || 'starter', task_force: r.task_force || '' }));
    document.getElementById('pt-notes').value = detail.board ? detail.board.notes : '';
}

function renderAll() {
    if (CAN_MANAGE) {
        show('pt-add', true);
        show('pt-check', rows.length > 0 || !!detail.board || isRoleType());
        document.getElementById('pt-delete').style.display = detail.board ? '' : 'none';
        renderEntries();
        renderRoles();
    }
    renderBoard();
    renderSuggestions();
}

// --- Step 1: add the board ---------------------------------------------------------

// Rows are numbered by position: rank N is the Nth row. A board migrated from the old
// Storm Attendance screen never had ranks, and giving it invented ones would be worse
// than leaving them blank, so its rows stay unranked.
function isRanked() {
    return !(detail.board && detail.board.source === 'legacy');
}

// A blank CSV with the columns this event's board needs, so the leader never has to
// guess the headers the import reads. The BOM keeps accented names intact in Excel.
function downloadTemplate(e) {
    e.preventDefault();
    const header = ['Rank', 'Name', ...trackables().map(t => t.label)];
    const blob = new Blob(['\ufeff' + header.join(',') + '\n'], { type: 'text/csv;charset=utf-8' });
    const a = el('a', { href: URL.createObjectURL(blob), download: (detail.event.type_short || 'board') + '-' + detail.event.event_date + '.csv' });
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

// The server reads the file, maps its headers, and matches names the way every import
// does; it saves nothing. The rows land in the check table to be fixed and saved.
async function importCSV() {
    const input = document.getElementById('pt-csv-input');
    const file = input.files && input.files[0];
    input.value = '';  // the same file can be chosen again after a fix
    if (!file) return;
    if (rows.length && !await showConfirm(
        `Replace the ${rows.length} row${rows.length === 1 ? '' : 's'} in the table with the board from ${file.name}?`, 'Replace')) return;

    const form = new FormData();
    form.append('csv_file', file);
    let res;
    try {
        res = await fetch('/api/participation/boards/' + EVENT_ID + '/csv', { method: 'POST', body: form });
    } catch (err) {
        console.error('importCSV:', err);
        showToast('Network error — the file was not read.', 'error');
        return;
    }
    if (!res.ok) {
        showToast(await errorText(res, 'Could not read that file.'), 'error', 8000);
        return;
    }
    const out = await res.json();
    rows = out.rows.map(r => ({
        name: r.name, member_id: r.member_id, member_name: r.member_name || '', member_rank: r.member_rank || '',
        values: Object.fromEntries(trackables().map(t => [t.key, r.values[t.key] ?? null])),
    }));
    const box = document.getElementById('pt-import-problems');
    if (out.problems.length) {
        box.replaceChildren(
            el('span', null, `${out.problems.length} thing${out.problems.length === 1 ? '' : 's'} in ${file.name} need${out.problems.length === 1 ? 's' : ''} a look:`),
            noTranslate(el('ul', null, ...out.problems.map(p => el('li', null, (p.line ? 'Line ' + p.line + ': ' : '') + p.message)))));
        box.hidden = false;
    } else {
        box.hidden = true;
    }
    show('pt-check', true);
    renderEntries();
    showToast(`Read ${rows.length} row${rows.length === 1 ? '' : 's'} from ${file.name}.`);
}

// Adding by hand: search for the member and they join the end of the board. The
// snapshot is simply their current name — there is nothing to type or to match.
function renderAddMember() {
    const box = document.getElementById('pt-add-member');
    if (!box) return;
    const picker = createMemberPicker({
        placeholder: 'Add a member to the board…',
        maxResults: 20,
        keepOpenOnPick: false,
        getCandidates: () => detail.roster,
        isExcluded: m => rows.some(r => r.member_id === m.id),
        onPick: m => {
            rows.push({
                name: m.name, member_id: m.id, member_name: m.name, member_rank: m.rank,
                values: Object.fromEntries(trackables().map(t => [t.key, null])),
            });
            show('pt-check', true);
            renderEntries();
            // Straight to the score for the row just added.
            const inputs = document.querySelectorAll('#pt-entries-body tr:last-child .pt-col-value input');
            if (inputs.length) inputs[0].focus();
        },
    });
    pickers.push(picker);
    box.replaceChildren(picker.el);
}

// --- Step 2: check -----------------------------------------------------------------

function destroyPickers() {
    pickers.forEach(p => p.destroy());
    pickers = [];
}

function usedMemberIds() {
    return new Set([...rows.map(r => r.member_id), ...roles.map(r => r.member_id)].filter(Boolean));
}

function updateSummary() {
    const matched = rows.filter(r => r.member_id).length;
    const unmatched = rows.length - matched;
    document.getElementById('pt-check-summary').textContent = rows.length
        ? `${rows.length} row${rows.length === 1 ? '' : 's'} · ${matched} matched · ${unmatched} need${unmatched === 1 ? 's' : ''} a member`
        : 'No rows yet — import a CSV above, or add members in board order.';
}

function renderEntries() {
    destroyPickers();
    renderAddMember();
    const head = document.getElementById('pt-entries-head');
    // Rank sits inside the Member cell rather than in its own column: on a phone the
    // first column is the one that stays put while the table scrolls sideways, and it
    // should be the name, not a number.
    head.replaceChildren(el('tr', null,
        el('th', null, '# · Member'),
        ...trackables().map(t => el('th', null, t.label)), el('th', null, '')));

    const body = document.getElementById('pt-entries-body');
    body.replaceChildren(...rows.map((r, i) => buildEntryRow(r, i)));
    if (!rows.length) {
        body.appendChild(el('tr', null, el('td', { colspan: String(2 + trackables().length), className: 'empty-state' },
            'No rows yet — import a CSV above, or add members in board order.')));
    }
    updateSummary();
}

function moveRow(i, to) {
    if (to < 0 || to >= rows.length) return;
    const [r] = rows.splice(i, 1);
    rows.splice(to, 0, r);
    renderEntries();
}

function buildEntryRow(r, i) {
    const tr = el('tr', { className: r.member_id ? null : 'pt-row-unmatched' });

    // The cell holds up to three parts — the row head (rank and member, or rank and
    // what the board said), the board's own name when it differs, and the picker —
    // kept as separate children so the narrow layout can place each on its own line.
    const matchTd = el('td', { className: 'pt-col-member' });
    const rank = el('span', { className: 'pt-rank-cell' }, (isRanked() ? String(i + 1) : '—') + ' · ');
    // What the board called them, shown only when it differs from the roster name
    // (an imported alias, a rename, a tag) — it is kept as the row's snapshot.
    const boardName = r.name && r.name !== r.member_name
        ? noTranslate(el('span', { className: 'pt-board-name' }, 'On the board: ' + r.name)) : null;
    if (r.member_id) {
        // No "match someone else" button: a wrong match is fixed by removing the row
        // and adding the right member, and the button crowded the row on a phone.
        matchTd.appendChild(el('span', { className: 'pt-row-head' }, rank, el('span', { className: 'pt-match' },
            el('span', { className: 'pt-member-name' }, nameSpan(r.member_name)),
            r.member_rank ? el('span', { className: 'member-rank rank-' + r.member_rank }, r.member_rank) : null)));
        if (boardName) matchTd.appendChild(boardName);
    } else {
        const picker = createMemberPicker({
            placeholder: 'Pick member…',
            maxResults: 20,
            getCandidates: () => detail.roster,
            isExcluded: m => rows.some(x => x.member_id === m.id),
            onPick: m => { Object.assign(r, { member_id: m.id, member_name: m.name, member_rank: m.rank }); renderEntries(); },
        });
        pickers.push(picker);
        matchTd.append(el('span', { className: 'pt-row-head' }, rank,
            noTranslate(el('span', { className: 'pt-board-name' }, 'On the board: ' + r.name))),
            el('span', { className: 'pt-match pt-pick' }, picker.el));
    }

    const valueTds = trackables().map(t => {
        // Plain text keyboard, not inputmode=decimal: scores are typed the way the
        // game prints them ("81.2G"), and a numeric keypad has no K/M/G.
        const input = el('input', { type: 'text', autocomplete: 'off', autocapitalize: 'characters', spellcheck: 'false', className: 'form-input', 'aria-label': t.label });
        input.value = fmtValue(r.values[t.key]);
        input.addEventListener('input', () => {
            const v = input.value.trim() === '' ? null : PP.parseAmount(input.value.trim());
            r.values[t.key] = v;
            input.classList.toggle('field-error', input.value.trim() !== '' && v === null);
        });
        // The column header's text, repeated in the cell for the narrow layout,
        // which has no header row.
        return el('td', { className: 'pt-col-value' }, el('span', { className: 'pt-cell-label' }, t.label), input);
    });

    const up = rowActionBtn('btn btn-ghost btn-sm', 'chevron-up', 'Move up', () => moveRow(i, i - 1));
    const down = rowActionBtn('btn btn-ghost btn-sm', 'chevron-down', 'Move down', () => moveRow(i, i + 1));
    up.disabled = i === 0;
    down.disabled = i === rows.length - 1;
    const remove = rowActionBtn('btn btn-danger btn-sm', 'trash', 'Remove', () => { rows.splice(i, 1); renderEntries(); });

    tr.append(matchTd, ...valueTds, el('td', { className: 'pt-col-actions' }, el('div', { className: 'row-actions' }, up, down, remove)));
    return tr;
}

function renderRoles() {
    show('pt-roles', isRoleType());
    if (!isRoleType()) return;
    const body = document.getElementById('pt-roles-body');
    const sorted = roles.slice().sort((a, b) => (a.role === b.role ? 0 : a.role === 'starter' ? -1 : 1)
        || a.member_name.localeCompare(b.member_name));
    body.replaceChildren(...sorted.map(r => {
        const roleSel = el('select', { className: 'form-input', 'aria-label': 'Role' },
            el('option', { value: 'starter' }, 'Starter'), el('option', { value: 'sub' }, 'Sub'));
        roleSel.value = r.role;
        roleSel.addEventListener('change', () => { r.role = roleSel.value; });
        const tfSel = el('select', { className: 'form-input', 'aria-label': 'Task force' },
            el('option', { value: '' }, '—'), el('option', { value: 'A' }, 'A'), el('option', { value: 'B' }, 'B'));
        tfSel.value = r.task_force || '';
        tfSel.addEventListener('change', () => { r.task_force = tfSel.value; });
        const remove = rowActionBtn('btn btn-danger btn-sm', 'x', 'Remove', () => {
            roles = roles.filter(x => x !== r);
            renderRoles();
        });
        return el('tr', null, el('td', null, nameSpan(r.member_name)), el('td', null, roleSel), el('td', null, tfSel), el('td', null, remove));
    }));
    if (!roles.length) {
        body.appendChild(el('tr', null, el('td', { colspan: '4', className: 'empty-state' },
            'No roles yet. Without roles no one can be counted as missing this battle — add the starters below.')));
    }
    const add = document.getElementById('pt-roles-add');
    const picker = createMemberPicker({
        placeholder: 'Add a member to the lineup…',
        maxResults: 20,
        getCandidates: () => detail.roster,
        isExcluded: m => roles.some(r => r.member_id === m.id),
        onPick: m => { roles.push({ member_id: m.id, member_name: m.name, role: 'starter', task_force: '' }); renderRoles(); },
    });
    pickers.push(picker);
    add.replaceChildren(picker.el);
}

async function saveBoard() {
    const status = document.getElementById('pt-save-status');
    status.textContent = '';
    // Client checks mirror the server's so the officer is told before the round trip.
    for (let i = 0; i < rows.length; i++) {
        const r = rows[i];
        if (!(r.name || '').trim()) { status.textContent = `Row ${i + 1} has no name.`; return; }
        for (const t of trackables()) {
            if (r.values[t.key] == null) { status.textContent = `Row ${i + 1} (${r.name}) needs a value for ${t.label}.`; return; }
        }
    }
    const body = {
        entries: rows.map((r, i) => ({ rank: isRanked() ? i + 1 : null, name: r.name.trim(), member_id: r.member_id || null, values: r.values })),
        roles: isRoleType() ? roles.map(r => ({ member_id: r.member_id, role: r.role, task_force: r.task_force })) : [],
        notes: document.getElementById('pt-notes').value,
    };
    const btn = document.getElementById('pt-save');
    btn.disabled = true;
    let res;
    try {
        res = await fetch('/api/participation/boards/' + EVENT_ID, {
            method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
        });
    } catch (err) {
        console.error('saveBoard:', err);
        status.textContent = 'Network error — the board was not saved.';
        btn.disabled = false;
        return;
    }
    btn.disabled = false;
    if (!res.ok) {
        status.textContent = await errorText(res, 'Could not save the board.');
        return;
    }
    detail = await res.json();
    resetWorkingCopy();
    renderHeader();
    renderAll();
    showToast('Board saved.');
    const sugg = document.getElementById('pt-suggest');
    if (sugg && !sugg.hidden) sugg.scrollIntoView({ behavior: 'smooth', block: 'start' });
}

async function deleteBoard() {
    if (!await showConfirm('Delete this board? The rows, roles, excuses and dismissals recorded for it are removed. Strikes already confirmed are kept.', 'Delete board')) return;
    let res;
    try {
        res = await fetch('/api/participation/boards/' + EVENT_ID, { method: 'DELETE' });
    } catch (err) {
        console.error('deleteBoard:', err);
        showToast('Network error — the board was not deleted.', 'error');
        return;
    }
    if (!res.ok) {
        showToast(await errorText(res, 'Could not delete the board.'), 'error');
        return;
    }
    showToast('Board deleted.');
    loadBoard();
}

// --- The board, as derived ---------------------------------------------------------

function renderBoard() {
    const section = document.getElementById('pt-board');
    if (!detail.board) {
        // Managers have the steps above; everyone else is told in words.
        section.hidden = CAN_MANAGE;
        if (!CAN_MANAGE) {
            document.getElementById('pt-board-counts').replaceChildren();
            document.getElementById('pt-board-body').replaceChildren(el('p', { className: 'empty-state' }, 'No board recorded for this event yet.'));
        }
        return;
    }
    section.hidden = false;
    const st = detail.statuses;
    const n = s => st.filter(x => s.includes(x.status)).length;
    const unmatched = detail.entries.filter(e => !e.member_id).length;
    const counts = [
        el('span', { className: 'pt-badge pt-badge--present' }, `${n(['present'])} present`),
        n(['zero']) ? el('span', { className: 'pt-badge pt-badge--zero' }, `${n(['zero'])} at zero`) : null,
        el('span', { className: 'pt-badge pt-badge--missed' }, `${n(['missed'])} missed`),
        el('span', { className: 'pt-badge pt-badge--excused' }, `${n(['excused'])} excused`),
        unmatched ? el('span', { className: 'pt-badge pt-badge--muted', title: 'Rows on the board not matched to a member' }, `${unmatched} unmatched`) : null,
    ].filter(Boolean);
    document.getElementById('pt-board-counts').replaceChildren(...counts);

    if (!st.length) {
        document.getElementById('pt-board-body').replaceChildren(el('p', { className: 'empty-state' },
            'This board lists no roster members.'));
        return;
    }
    const key = primaryKey();
    const label = trackables()[0]?.label || 'Score';
    const cols = ['Member', 'Status', 'Rank', label];
    if (isRoleType()) cols.push('Role');
    if (CAN_MANAGE) cols.push('');
    const table = el('table', { className: 'data-table' },
        el('thead', null, el('tr', null, ...cols.map(c => el('th', null, c)))),
        el('tbody', null, ...st.map(s => {
            const statusTd = el('td', null, badge(s.status));
            if (s.dismissed) statusTd.append(' ', el('span', { className: 'pt-badge pt-badge--muted', title: 'Suggestion dismissed' }, 'dismissed'));
            if (s.struck) statusTd.append(' ', el('span', { className: 'pt-badge pt-badge--muted', title: detail.type.strike_label + ' strike filed for this date' }, 'struck'));
            if (s.reason) {
                const reason = el('span', { className: 'pt-reason' }, s.reason);
                statusTd.appendChild(reason);
                TranslateBlock.attach(reason, s.reason);
            }
            const tds = [
                el('td', null, nameSpan(s.name)), statusTd,
                el('td', null, s.rank != null ? String(s.rank) : '—'),
                el('td', null, s.values && s.values[key] != null ? fmtValue(s.values[key]) : '—'),
            ];
            if (isRoleType()) {
                const role = s.role ? s.role[0].toUpperCase() + s.role.slice(1) + (s.task_force ? ' · TF ' + s.task_force : '') : '—';
                tds.push(el('td', null, role));
            }
            if (CAN_MANAGE) {
                const x = detail.exceptions.find(e => e.member_id === s.member_id);
                const td = el('td');
                if (x && (x.kind === 'excused' || x.kind === 'dismissed')) {
                    td.appendChild(rowActionBtn('btn btn-secondary btn-sm', 'arrow-back-up', x.kind === 'excused' ? 'Undo excuse' : 'Undo dismiss',
                        () => undoException(s)));
                }
                tds.push(td);
            }
            return el('tr', null, ...tds);
        })));
    document.getElementById('pt-board-body').replaceChildren(el('div', { className: 'table-scroll' }, table));
}

// --- Step 3: suggestions -----------------------------------------------------------

function renderSuggestions() {
    const section = document.getElementById('pt-suggest');
    if (!section) return;
    const list = detail.board ? detail.suggestions : [];
    section.hidden = list.length === 0;
    if (!list.length) return;
    const zeroLabel = '0 ' + (trackables()[0]?.label || '').toLowerCase();
    document.getElementById('pt-suggest-list').replaceChildren(...list.map(s => el('li', null,
        noTranslate(el('span', { className: 'pt-suggest-name' }, s.name)),
        s.status === 'zero' ? el('span', { className: 'pt-badge pt-badge--zero' }, zeroLabel) : badge(s.status),
        s.role ? el('span', { className: 'pt-badge pt-badge--role' }, s.role + (s.task_force ? ' · TF ' + s.task_force : '')) : null,
        el('div', { className: 'row-actions' },
            rowActionBtn('btn btn-danger btn-sm', 'flag', 'Strike', () => strike(s)),
            rowActionBtn('btn btn-secondary btn-sm', 'check', 'Excuse', () => openExcuse(s)),
            rowActionBtn('btn btn-ghost btn-sm', 'x', 'Dismiss', () => dismiss(s))),
    )));
    document.getElementById('pt-strike-all-label').textContent = `Strike all (${list.length})`;
}

async function post(url, body, okMsg) {
    let res;
    try {
        res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    } catch (err) {
        console.error('post ' + url + ':', err);
        showToast('Network error — nothing was changed.', 'error');
        return false;
    }
    if (!res.ok) {
        showToast(await errorText(res, 'That did not work.'), 'error', 7000);
        loadBoard();
        return false;
    }
    if (okMsg) showToast(okMsg);
    return true;
}

async function strike(s) {
    if (await post(`/api/participation/boards/${EVENT_ID}/strikes`, { member_id: s.member_id },
        `Strike added for ${s.name} (${detail.type.strike_label}).`)) loadBoard();
}

async function strikeAll() {
    const n = detail.suggestions.length;
    if (!n) return;
    if (!await showConfirm(`Add a ${detail.type.strike_label} strike for all ${n} suggested member${n === 1 ? '' : 's'}?`, `Strike ${n}`)) return;
    if (await post(`/api/participation/boards/${EVENT_ID}/strikes`, { all: true }, `${n} strike${n === 1 ? '' : 's'} added.`)) loadBoard();
}

async function dismiss(s) {
    if (await post(`/api/participation/boards/${EVENT_ID}/exceptions`, { member_id: s.member_id, kind: 'dismissed' },
        `${s.name} will not be suggested for this board again.`)) loadBoard();
}

let excuseTarget = null;

function openExcuse(s) {
    excuseTarget = s;
    const modal = document.getElementById('pt-excuse-modal');
    document.getElementById('pt-excuse-name').textContent = s.name;
    document.getElementById('pt-excuse-reason').value = '';
    document.getElementById('pt-excuse-status').textContent = '';
    modal.style.display = 'flex';
    trapFocus(modal);
    document.getElementById('pt-excuse-reason').focus();
}

function closeExcuse() {
    const modal = document.getElementById('pt-excuse-modal');
    releaseFocus(modal);
    modal.style.display = '';
    excuseTarget = null;
}

async function saveExcuse() {
    const reason = document.getElementById('pt-excuse-reason').value.trim();
    if (!reason) { document.getElementById('pt-excuse-status').textContent = 'An excuse needs a reason.'; return; }
    const s = excuseTarget;
    closeExcuse();
    if (await post(`/api/participation/boards/${EVENT_ID}/exceptions`, { member_id: s.member_id, kind: 'excused', reason },
        `${s.name} excused.`)) loadBoard();
}

async function undoException(s) {
    let res;
    try {
        res = await fetch(`/api/participation/boards/${EVENT_ID}/exceptions?member_id=${s.member_id}`, { method: 'DELETE' });
    } catch (err) {
        console.error('undoException:', err);
        showToast('Network error — nothing was changed.', 'error');
        return;
    }
    if (!res.ok) {
        showToast(await errorText(res, 'Could not undo that.'), 'error', 7000);
        return;
    }
    loadBoard();
}

// --- Boot --------------------------------------------------------------------------

document.addEventListener('DOMContentLoaded', () => {
    if (CAN_MANAGE) {
        document.getElementById('pt-csv-input').addEventListener('change', importCSV);
        document.getElementById('pt-csv-template').addEventListener('click', downloadTemplate);
        document.getElementById('pt-save').addEventListener('click', saveBoard);
        document.getElementById('pt-delete').addEventListener('click', deleteBoard);
        document.getElementById('pt-strike-all').addEventListener('click', strikeAll);
        document.getElementById('pt-excuse-save').addEventListener('click', saveExcuse);
        document.getElementById('pt-excuse-cancel').addEventListener('click', closeExcuse);
        document.getElementById('pt-excuse-reason').addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); saveExcuse(); } });
        const modal = document.getElementById('pt-excuse-modal');
        modal.addEventListener('click', e => { if (e.target === modal) closeExcuse(); });
    }
    if (EVENT_ID) loadBoard();
    else initNew();
});
