'use strict';

// Game server is UTC-2 (same constant as storm.js)
const SERVER_UTC_OFFSET = -2;

const CAN_MANAGE = window.CAN_MANAGE === true;

// ── Game-time helpers ─────────────────────────────────────────────────────────

function gameNow() {
    return new Date(Date.now() + SERVER_UTC_OFFSET * 3600 * 1000);
}

function gameToday() {
    const d = gameNow();
    return d.toISOString().slice(0, 10); // YYYY-MM-DD
}

// ── State ─────────────────────────────────────────────────────────────────────

let allMembers = [];   // [{id, name, rank}]
let allRules = [];     // EligibilityRule[]
let editingLogId = null;
let editingRuleId = null;

// Flatpickr instances — initialised in DOMContentLoaded
let logDateFP = null;
let filterFromFP = null;
let filterToFP = null;

// Choices.js instances — initialised in DOMContentLoaded
let conductorChoices = null;
let vipChoices = null;

// ── Init ──────────────────────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
    logDateFP    = flatpickr('#log-date',    { dateFormat: 'Y-m-d', allowInput: true });
    filterFromFP = flatpickr('#filter-from', { dateFormat: 'Y-m-d', allowInput: true });
    filterToFP   = flatpickr('#filter-to',   { dateFormat: 'Y-m-d', allowInput: true });

    conductorChoices = new Choices('#log-conductor', {
        searchEnabled: true, searchPlaceholderValue: 'Search…',
        itemSelectText: '', shouldSort: false,
    });
    vipChoices = new Choices('#log-vip', {
        searchEnabled: true, searchPlaceholderValue: 'Search…',
        itemSelectText: '', shouldSort: false,
    });

    setupTabs();
    loadMembers().then(() => {
        loadTrainLogs(null, null);
        if (CAN_MANAGE) {
            loadRules();
        }
    });

    if (CAN_MANAGE) {
        document.getElementById('btn-log-train').addEventListener('click', () => openLogModal(null));
        document.getElementById('btn-log-save').addEventListener('click', saveTrainLog);
        document.getElementById('btn-log-cancel').addEventListener('click', closeLogModal);
        document.getElementById('log-modal').addEventListener('click', (e) => {
            if (e.target === document.getElementById('log-modal')) closeLogModal();
        });
        document.getElementById('log-vip').addEventListener('change', onVIPChange);

        document.getElementById('btn-new-rule').addEventListener('click', () => openRuleModal(null));
        document.getElementById('btn-rule-save').addEventListener('click', saveRule);
        document.getElementById('btn-rule-cancel').addEventListener('click', closeRuleModal);
        document.getElementById('rule-modal').addEventListener('click', (e) => {
            if (e.target === document.getElementById('rule-modal')) closeRuleModal();
        });
        document.getElementById('rule-sm-type').addEventListener('change', onSMTypeChange);
        document.getElementById('btn-add-group').addEventListener('click', addGroup);

        document.getElementById('btn-run-rule').addEventListener('click', runSelectedRule);
        document.getElementById('sel-rule-picker').addEventListener('change', () => {
            document.getElementById('btn-run-rule').disabled =
                !document.getElementById('sel-rule-picker').value;
        });
    }

    document.getElementById('btn-apply-filter').addEventListener('click', applyFilter);
    document.getElementById('btn-clear-filter').addEventListener('click', clearFilter);
});

// ── Tabs ──────────────────────────────────────────────────────────────────────

function setupTabs() {
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-panel').forEach(p => p.classList.add('hidden'));
            btn.classList.add('active');
            document.getElementById('tab-' + btn.dataset.tab).classList.remove('hidden');
        });
    });
}

// ── Members ───────────────────────────────────────────────────────────────────

async function loadMembers() {
    const res = await fetch('/api/members');
    if (!res.ok) return;
    allMembers = await res.json();
    populateMemberDropdowns();
}

function populateMemberDropdowns() {
    if (!conductorChoices) return;

    const memberOpts = allMembers.map(m => ({ value: String(m.id), label: `[${m.rank}] ${m.name}` }));

    conductorChoices.setChoices(
        [{ value: '', label: '— select member —', placeholder: true }, ...memberOpts],
        'value', 'label', true
    );
    vipChoices.setChoices(
        [{ value: '', label: '— none —', placeholder: true }, ...memberOpts],
        'value', 'label', true
    );
}

function makeOpt(value, text) {
    const o = document.createElement('option');
    o.value = value;
    o.textContent = text;
    return o;
}

// ── Train Logs ────────────────────────────────────────────────────────────────

async function loadTrainLogs(from, to) {
    let url = '/api/train-logs';
    const params = [];
    if (from) params.push('from=' + from);
    if (to) params.push('to=' + to);
    if (params.length) url += '?' + params.join('&');

    const res = await fetch(url);
    if (!res.ok) return;
    const logs = await res.json();
    renderLogsTable(logs);
}

function renderLogsTable(logs) {
    const container = document.getElementById('logs-container');
    if (!logs || logs.length === 0) {
        container.replaceChildren(emptyState('No trains logged yet.'));
        return;
    }

    const table = document.createElement('table');
    table.className = 'train-table';

    const thead = table.createTHead();
    const hr = thead.insertRow();
    ['Date', 'Type', 'Conductor', 'VIP', 'Notes', ...(CAN_MANAGE ? [''] : [])].forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        hr.appendChild(th);
    });

    const tbody = table.createTBody();
    logs.forEach(log => {
        const tr = tbody.insertRow();

        // Date
        td(tr, log.date);

        // Type badge
        const typeTd = tr.insertCell();
        const badge = document.createElement('span');
        badge.className = log.train_type === 'FREE' ? 'badge-free' : 'badge-purchased';
        badge.textContent = log.train_type === 'FREE' ? 'Free' : 'Purchased';
        typeTd.appendChild(badge);

        // Conductor
        td(tr, log.conductor_name);

        // VIP
        const vipTd = tr.insertCell();
        if (log.vip_name) {
            vipTd.textContent = log.vip_name;
            if (log.vip_type) {
                const vbadge = document.createElement('span');
                vbadge.className = 'badge-vip-type';
                vbadge.textContent = log.vip_type === 'SPECIAL_GUEST' ? 'Guest' : 'Guardian';
                vipTd.appendChild(vbadge);
            }
        } else {
            vipTd.textContent = '—';
        }

        // Notes
        td(tr, log.notes || '—');

        // Actions
        if (CAN_MANAGE) {
            const actTd = tr.insertCell();
            const row = document.createElement('div');
            row.className = 'row-actions';

            const editBtn = document.createElement('button');
            editBtn.className = 'btn btn-secondary btn-sm';
            editBtn.textContent = 'Edit';
            editBtn.addEventListener('click', () => openLogModal(log));
            row.appendChild(editBtn);

            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.textContent = 'Delete';
            delBtn.addEventListener('click', () => {
                delBtn.style.display = 'none';
                const confirmSpan = document.createElement('span');
                confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
                const label = document.createElement('span');
                label.textContent = 'Sure?';
                label.style.fontSize = '0.85rem';
                const yesBtn = document.createElement('button');
                yesBtn.className = 'btn btn-danger btn-sm';
                yesBtn.textContent = 'Yes';
                yesBtn.addEventListener('click', () => deleteTrainLog(log.id));
                const noBtn = document.createElement('button');
                noBtn.className = 'btn btn-secondary btn-sm';
                noBtn.textContent = 'No';
                noBtn.addEventListener('click', () => {
                    confirmSpan.remove();
                    delBtn.style.display = '';
                });
                confirmSpan.append(label, yesBtn, noBtn);
                row.appendChild(confirmSpan);
            });
            row.appendChild(delBtn);

            actTd.appendChild(row);
        }
    });

    const wrap = document.createElement('div');
    wrap.className = 'table-scroll';
    wrap.appendChild(table);
    container.replaceChildren(wrap);
}

function applyFilter() {
    const from = document.getElementById('filter-from').value;
    const to = document.getElementById('filter-to').value;
    loadTrainLogs(from || null, to || null);
}

function clearFilter() {
    filterFromFP.clear(false);
    filterToFP.clear(false);
    loadTrainLogs(null, null);
}

// ── Log Train Modal ───────────────────────────────────────────────────────────

function openLogModal(log) {
    editingLogId = log ? log.id : null;
    document.getElementById('log-modal-title').textContent = log ? 'Edit Train' : 'Log Train';
    logDateFP.setDate(log ? log.date : gameToday(), false);
    document.getElementById('log-type').value = log ? log.train_type : 'FREE';
    conductorChoices.setChoiceByValue(log ? String(log.conductor_id) : '');
    vipChoices.setChoiceByValue(log && log.vip_id ? String(log.vip_id) : '');
    document.getElementById('log-vip-type').value = (log && log.vip_type) ? log.vip_type : 'SPECIAL_GUEST';
    document.getElementById('log-notes').value = log ? log.notes : '';
    document.getElementById('log-edit-id').value = log ? log.id : '';
    document.getElementById('log-limit-warning').classList.add('hidden');
    document.getElementById('log-modal-status').textContent = '';
    onVIPChange();
    const logModal = document.getElementById('log-modal');
    logModal.style.display = 'flex';
    trapFocus(logModal);
}

function closeLogModal() {
    const logModal = document.getElementById('log-modal');
    releaseFocus(logModal);
    logModal.style.display = '';
    editingLogId = null;
}

function onVIPChange() {
    const vipSel = document.getElementById('log-vip');
    const vipTypeSel = document.getElementById('log-vip-type');
    vipTypeSel.style.display = vipSel.value ? '' : 'none';
}

async function saveTrainLog() {
    const date = document.getElementById('log-date').value.trim();
    const trainType = document.getElementById('log-type').value;
    const conductorId = parseInt(document.getElementById('log-conductor').value, 10);
    const vipVal = document.getElementById('log-vip').value;
    const vipId = vipVal ? parseInt(vipVal, 10) : null;
    const vipType = vipId ? document.getElementById('log-vip-type').value : null;
    const notes = document.getElementById('log-notes').value.trim();

    const logStatus = document.getElementById('log-modal-status');
    if (!date) { logStatus.textContent = 'Date is required.'; return; }
    if (!conductorId) { logStatus.textContent = 'Conductor is required.'; return; }
    logStatus.textContent = '';

    const body = { date, train_type: trainType, conductor_id: conductorId, notes };
    if (vipId) { body.vip_id = vipId; body.vip_type = vipType; }

    const url = editingLogId ? `/api/train-logs/${editingLogId}` : '/api/train-logs';
    const method = editingLogId ? 'PUT' : 'POST';

    const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });

    if (!res.ok) {
        const text = await res.text();
        document.getElementById('log-modal-status').textContent = 'Error: ' + text;
        return;
    }

    const data = await res.json();
    if (data.limit_warning) {
        document.getElementById('limit-warning').classList.remove('hidden');
        setTimeout(() => document.getElementById('limit-warning').classList.add('hidden'), 6000);
    }

    closeLogModal();
    loadTrainLogs(
        document.getElementById('filter-from').value || null,
        document.getElementById('filter-to').value || null
    );
}

async function deleteTrainLog(id) {
    const res = await fetch(`/api/train-logs/${id}`, { method: 'DELETE' });
    if (!res.ok) {
        // restore the row to its original state on failure
        loadTrainLogs(
            document.getElementById('filter-from').value || null,
            document.getElementById('filter-to').value || null
        );
        return;
    }
    loadTrainLogs(
        document.getElementById('filter-from').value || null,
        document.getElementById('filter-to').value || null
    );
}

// ── Eligibility Rules ─────────────────────────────────────────────────────────

async function loadRules() {
    const res = await fetch('/api/eligibility-rules');
    if (!res.ok) return;
    allRules = await res.json();
    renderRules();
    populateRulePicker();
}

function renderRules() {
    const container = document.getElementById('rules-container');
    if (!container) return;
    if (!allRules || allRules.length === 0) {
        container.replaceChildren(emptyState('No rules yet. Create one to get started.'));
        return;
    }

    const list = document.createElement('div');
    list.className = 'rules-list';

    allRules.forEach(rule => {
        const card = document.createElement('div');
        card.className = 'rule-card';

        const info = document.createElement('div');
        const nameEl = document.createElement('div');
        nameEl.className = 'rule-card-name';
        nameEl.textContent = rule.name;

        const meta = document.createElement('div');
        meta.className = 'rule-card-meta';
        const sm = parseSM(rule.selection_method);
        meta.textContent = smLabel(sm);
        info.appendChild(nameEl);
        info.appendChild(meta);
        card.appendChild(info);

        const actions = document.createElement('div');
        actions.className = 'row-actions';

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openRuleModal(rule));
        actions.appendChild(editBtn);

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-danger btn-sm';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', () => {
            delBtn.style.display = 'none';
            const confirmSpan = document.createElement('span');
            confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
            const label = document.createElement('span');
            label.textContent = 'Sure?';
            label.style.fontSize = '0.85rem';
            const yesBtn = document.createElement('button');
            yesBtn.className = 'btn btn-danger btn-sm';
            yesBtn.textContent = 'Yes';
            yesBtn.addEventListener('click', () => deleteRule(rule.id));
            const noBtn = document.createElement('button');
            noBtn.className = 'btn btn-secondary btn-sm';
            noBtn.textContent = 'No';
            noBtn.addEventListener('click', () => {
                confirmSpan.remove();
                delBtn.style.display = '';
            });
            confirmSpan.append(label, yesBtn, noBtn);
            actions.appendChild(confirmSpan);
        });
        actions.appendChild(delBtn);

        card.appendChild(actions);
        list.appendChild(card);
    });

    container.replaceChildren(list);
}

function populateRulePicker() {
    const picker = document.getElementById('sel-rule-picker');
    if (!picker) return;
    picker.replaceChildren(makeOpt('', '— select a rule —'));
    allRules.forEach(rule => picker.appendChild(makeOpt(rule.id, rule.name)));
}

// ── Run Eligibility Rule ──────────────────────────────────────────────────────

async function runSelectedRule() {
    const ruleId = document.getElementById('sel-rule-picker').value;
    if (!ruleId) return;

    document.getElementById('eligible-container').replaceChildren(emptyState('Running…'));

    const res = await fetch(`/api/eligibility-rules/${ruleId}/run`, { method: 'POST' });
    if (!res.ok) {
        const text = await res.text();
        document.getElementById('eligible-container').replaceChildren(emptyState('Error: ' + text));
        return;
    }
    const members = await res.json();
    renderEligibleList(members);
}

function renderEligibleList(members) {
    const container = document.getElementById('eligible-container');
    if (!members || members.length === 0) {
        container.replaceChildren(emptyState('No eligible members for this rule.'));
        return;
    }

    const list = document.createElement('div');
    list.className = 'eligible-list';

    members.forEach((m, index) => {
        const card = document.createElement('div');
        card.className = 'eligible-card';

        const left = document.createElement('div');
        left.style.display = 'flex';
        left.style.alignItems = 'center';
        left.style.gap = '12px';

        const pos = document.createElement('span');
        pos.style.cssText = 'font-size:1.1rem;font-weight:700;color:var(--text-secondary);min-width:24px;text-align:center;';
        pos.textContent = index + 1;
        left.appendChild(pos);

        const nameRank = document.createElement('div');
        const nameEl = document.createElement('div');
        nameEl.className = 'eligible-card-name';
        nameEl.textContent = m.name;
        const rankEl = document.createElement('div');
        rankEl.style.fontSize = '0.8rem';
        rankEl.style.color = 'var(--text-secondary)';
        rankEl.textContent = m.rank;
        nameRank.appendChild(nameEl);
        nameRank.appendChild(rankEl);
        left.appendChild(nameRank);

        card.appendChild(left);

        const stats = document.createElement('div');
        stats.className = 'eligible-card-stats';

        stats.appendChild(makeStat('VS Week', m.vs_total_week));
        stats.appendChild(makeStat('VS Yesterday', m.vs_yesterday));
        stats.appendChild(makeStat('Free idle (d)', fmtDays(m.days_since_free_conducted)));
        stats.appendChild(makeStat('Any idle (d)', fmtDays(m.days_since_any_conducted)));

        card.appendChild(stats);

        const logBtn = document.createElement('button');
        logBtn.className = 'btn btn-primary btn-sm';
        logBtn.textContent = 'Log Train';
        logBtn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-panel').forEach(p => p.classList.add('hidden'));
            document.querySelector('[data-tab="logs"]').classList.add('active');
            document.getElementById('tab-logs').classList.remove('hidden');
            openLogModal({ conductor_id: m.member_id, conductor_name: m.name, train_type: 'FREE', date: gameToday(), notes: '', vip_id: null, vip_type: null });
        });
        card.appendChild(logBtn);

        list.appendChild(card);
    });

    container.replaceChildren(list);
}

function makeStat(label, value) {
    const div = document.createElement('div');
    div.className = 'eligible-card-stat';
    const lbl = document.createElement('span');
    lbl.textContent = label;
    const val = document.createElement('span');
    val.textContent = value;
    div.appendChild(lbl);
    div.appendChild(val);
    return div;
}

function fmtDays(d) {
    if (d >= 9999) return 'never';
    return Math.round(d);
}

// ── Rule Editor Modal ─────────────────────────────────────────────────────────

function openRuleModal(rule) {
    editingRuleId = rule ? rule.id : null;
    document.getElementById('rule-modal-title').textContent = rule ? 'Edit Rule' : 'New Rule';
    document.getElementById('rule-name').value = rule ? rule.name : '';
    document.getElementById('rule-edit-id').value = rule ? rule.id : '';

    // Selection method
    const sm = rule ? parseSM(rule.selection_method) : { type: 'RANDOM', field: '' };
    document.getElementById('rule-sm-type').value = sm.type;
    document.getElementById('rule-sm-field').value = sm.field || 'days_since_free_conducted';
    onSMTypeChange();

    // Conditions
    const groupsContainer = document.getElementById('rule-groups-container');
    groupsContainer.replaceChildren();

    const cond = rule ? parseCond(rule.conditions) : { groups: [] };
    if (cond.groups && cond.groups.length > 0) {
        cond.groups.forEach(g => addGroup(g.conditions || []));
    }

    document.getElementById('rule-modal-status').textContent = '';
    const ruleModal = document.getElementById('rule-modal');
    ruleModal.style.display = 'flex';
    trapFocus(ruleModal);
}

function closeRuleModal() {
    const ruleModal = document.getElementById('rule-modal');
    releaseFocus(ruleModal);
    ruleModal.style.display = '';
    editingRuleId = null;
}

function onSMTypeChange() {
    const type = document.getElementById('rule-sm-type').value;
    const fieldSel = document.getElementById('rule-sm-field');
    fieldSel.style.display = (type === 'RANDOM') ? 'none' : '';
}

function addGroup(existingConditions) {
    const container = document.getElementById('rule-groups-container');

    // Add OR divider between groups
    if (container.children.length > 0) {
        const div = document.createElement('div');
        div.className = 'or-divider';
        div.textContent = '— OR —';
        container.appendChild(div);
    }

    const group = document.createElement('div');
    group.className = 'condition-group';

    const header = document.createElement('div');
    header.className = 'condition-group-header';

    const label = document.createElement('span');
    label.className = 'group-label';
    label.textContent = 'AND Group';
    header.appendChild(label);

    const removeGrpBtn = document.createElement('button');
    removeGrpBtn.className = 'btn btn-ghost btn-sm';
    removeGrpBtn.textContent = '✕ Remove Group';
    removeGrpBtn.addEventListener('click', () => {
        // Remove the group and any adjacent OR divider
        const prev = group.previousElementSibling;
        const next = group.nextElementSibling;
        if (prev && prev.classList.contains('or-divider')) prev.remove();
        else if (next && next.classList.contains('or-divider')) next.remove();
        group.remove();
    });
    header.appendChild(removeGrpBtn);
    group.appendChild(header);

    const condsContainer = document.createElement('div');
    condsContainer.className = 'conditions-list';
    group.appendChild(condsContainer);

    const addCondBtn = document.createElement('button');
    addCondBtn.className = 'btn btn-ghost btn-sm';
    addCondBtn.textContent = '+ Add Condition';
    addCondBtn.addEventListener('click', () => addCondition(condsContainer, null));
    group.appendChild(addCondBtn);

    container.appendChild(group);

    // Populate existing conditions
    if (Array.isArray(existingConditions) && existingConditions.length > 0) {
        existingConditions.forEach(c => addCondition(condsContainer, c));
    } else {
        addCondition(condsContainer, null);
    }
}

function addCondition(container, existing) {
    const row = document.createElement('div');
    row.className = 'condition-row';

    // Variable selector
    const varSel = document.createElement('select');
    varSel.className = 'cond-var';
    const VARIABLES = [
        ['rank', 'Rank'],
        ['vs_total_week', 'VS total (this week)'],
        ['vs_yesterday', 'VS yesterday'],
        ['vs_total_prev_week', 'VS total (prev week)'],
        ['vs_day_monday', 'VS Monday'],
        ['vs_day_tuesday', 'VS Tuesday'],
        ['vs_day_wednesday', 'VS Wednesday'],
        ['vs_day_thursday', 'VS Thursday'],
        ['vs_day_friday', 'VS Friday'],
        ['vs_day_saturday', 'VS Saturday'],
        ['days_since_free_conducted', 'Days since free train'],
        ['days_since_any_conducted', 'Days since any train'],
    ];
    VARIABLES.forEach(([val, lbl]) => varSel.appendChild(makeOpt(val, lbl)));
    if (existing) varSel.value = existing.variable;
    row.appendChild(varSel);

    // Operator selector
    const opSel = document.createElement('select');
    opSel.className = 'cond-op';
    ['>=', '<=', '>', '<', '==', 'in'].forEach(op => opSel.appendChild(makeOpt(op, op)));
    if (existing) opSel.value = existing.op;
    row.appendChild(opSel);

    // Value input
    const valInput = document.createElement('input');
    valInput.className = 'cond-val';
    valInput.type = 'text';
    valInput.placeholder = 'value';
    if (existing) {
        valInput.value = Array.isArray(existing.value)
            ? existing.value.join(', ')
            : String(existing.value ?? '');
    }
    row.appendChild(valInput);

    // Remove button
    const rmBtn = document.createElement('button');
    rmBtn.className = 'btn btn-ghost btn-sm';
    rmBtn.textContent = '✕';
    rmBtn.addEventListener('click', () => row.remove());
    row.appendChild(rmBtn);

    container.appendChild(row);
}

function serializeConditions() {
    const groups = [];
    document.querySelectorAll('#rule-groups-container .condition-group').forEach(grp => {
        const conditions = [];
        grp.querySelectorAll('.condition-row').forEach(row => {
            const variable = row.querySelector('.cond-var').value;
            const op = row.querySelector('.cond-op').value;
            const rawVal = row.querySelector('.cond-val').value.trim();
            let value;
            if (op === 'in') {
                value = rawVal.split(',').map(s => s.trim()).filter(Boolean);
            } else if (variable === 'rank') {
                value = rawVal;
            } else {
                value = parseFloat(rawVal) || 0;
            }
            conditions.push({ variable, op, value });
        });
        if (conditions.length > 0) groups.push({ conditions });
    });
    return { groups };
}

function serializeSelectionMethod() {
    const type = document.getElementById('rule-sm-type').value;
    if (type === 'RANDOM') return { type: 'RANDOM' };
    return { type, field: document.getElementById('rule-sm-field').value };
}

async function saveRule() {
    const name = document.getElementById('rule-name').value.trim();
    const ruleStatus = document.getElementById('rule-modal-status');
    if (!name) { ruleStatus.textContent = 'Rule name is required.'; return; }
    ruleStatus.textContent = '';

    const body = {
        name,
        selection_method: serializeSelectionMethod(),
        conditions: serializeConditions(),
    };

    const url = editingRuleId ? `/api/eligibility-rules/${editingRuleId}` : '/api/eligibility-rules';
    const method = editingRuleId ? 'PUT' : 'POST';

    const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });

    if (!res.ok) {
        const text = await res.text();
        document.getElementById('rule-modal-status').textContent = 'Error: ' + text;
        return;
    }

    closeRuleModal();
    loadRules();
}

async function deleteRule(id) {
    const res = await fetch(`/api/eligibility-rules/${id}`, { method: 'DELETE' });
    if (!res.ok) { loadRules(); return; }
    loadRules();
}

// ── Utility ───────────────────────────────────────────────────────────────────

function td(tr, text) {
    const cell = tr.insertCell();
    cell.textContent = text;
    return cell;
}

function emptyState(msg) {
    const p = document.createElement('p');
    p.className = 'empty-state';
    p.textContent = msg;
    return p;
}

function parseSM(raw) {
    try { return JSON.parse(typeof raw === 'string' ? raw : JSON.stringify(raw)); }
    catch { return { type: 'RANDOM' }; }
}

function parseCond(raw) {
    try { return JSON.parse(typeof raw === 'string' ? raw : JSON.stringify(raw)); }
    catch { return { groups: [] }; }
}

function smLabel(sm) {
    if (!sm || sm.type === 'RANDOM') return 'Selection: Random';
    const FIELD_LABELS = {
        rank: 'Rank',
        vs_total_week: 'VS total (week)',
        vs_yesterday: 'VS yesterday',
        vs_total_prev_week: 'VS total (prev week)',
        vs_day_monday: 'VS Mon', vs_day_tuesday: 'VS Tue',
        vs_day_wednesday: 'VS Wed', vs_day_thursday: 'VS Thu',
        vs_day_friday: 'VS Fri', vs_day_saturday: 'VS Sat',
        days_since_free_conducted: 'Days since free train',
        days_since_any_conducted: 'Days since any train',
    };
    return `Selection: ${sm.type === 'GREATEST' ? 'Greatest' : 'Least'} by ${FIELD_LABELS[sm.field] || sm.field}`;
}
