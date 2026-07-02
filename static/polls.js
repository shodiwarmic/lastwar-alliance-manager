// polls.js — Poll Tracker (runs on the Comms page)

const pollCfg = document.getElementById('page-config')?.dataset ?? {};
const CAN_MANAGE_POLLS = pollCfg.canManagePolls === 'true';

const pollLoaded = {};

// ── Poll detail modal state ─────────────────────────────────────────────────
// The cached detail payload is the single source of truth; the "By option" view's
// optimistic mutations edit it in place, and a JSON snapshot is used for rollback.
let pollDetailView = 'member';       // 'member' | 'option'
let pollDetailData = null;           // cached detail payload
let pollDetailInstance = null;       // current poll instance
let activeOptionPickers = [];        // member-pickers mounted in the by-option view
let pollListDirty = false;           // refresh the instance-list card on close if true
const pollToggleInFlight = new Set();// per-mutation guard keys (defeats rapid re-taps)

function destroyOptionPickers() {
    activeOptionPickers.forEach(p => p.destroy());
    activeOptionPickers = [];
}

// ── Data loaders (exposed on window so comms.js switchTab can call them) ─────

window.loadPollTemplates = async function () {
    if (pollLoaded['poll-templates']) return;
    pollLoaded['poll-templates'] = true;
    await _fetchPollTemplates();
};

window.loadPollInstances = async function () {
    if (pollLoaded['polls']) return;
    pollLoaded['polls'] = true;
    await _fetchPollInstances();
};

async function _fetchPollTemplates() {
    const listEl = document.getElementById('poll-template-list');
    if (!listEl) return;
    listEl.textContent = 'Loading…';
    try {
        const res = await fetch('/api/comms/poll-templates');
        if (!res.ok) throw new Error('fetch failed');
        const data = await res.json();
        renderPollTemplateList(data.items || []);
    } catch {
        listEl.textContent = 'Failed to load. Please refresh.';
    }
}

async function _fetchPollInstances() {
    const listEl = document.getElementById('poll-instance-list');
    if (!listEl) return;
    listEl.textContent = 'Loading…';
    try {
        const res = await fetch('/api/comms/poll-instances');
        if (!res.ok) throw new Error('fetch failed');
        const data = await res.json();
        renderPollInstanceList(data.items || []);
    } catch {
        listEl.textContent = 'Failed to load. Please refresh.';
    }
}

// ── Poll Template List ────────────────────────────────────────────────────────

function renderPollTemplateList(items) {
    const listEl = document.getElementById('poll-template-list');
    if (!listEl) return;
    listEl.replaceChildren();

    if (items.length === 0) {
        const p = document.createElement('p');
        p.className = 'comms-empty';
        p.textContent = 'No poll templates yet.' + (CAN_MANAGE_POLLS ? ' Click "+ New Poll Template" to create one.' : '');
        listEl.appendChild(p);
        return;
    }

    items.forEach(t => listEl.appendChild(renderPollTemplateCard(t)));
}

function renderPollTemplateCard(t) {
    const opts = parsePollOptions(t.options);
    const card = document.createElement('div');
    card.className = 'poll-template-card';

    const header = document.createElement('div');
    header.className = 'poll-template-card-header';

    const info = document.createElement('div');
    const titleEl = document.createElement('span');
    titleEl.className = 'poll-template-card-title';
    titleEl.textContent = t.title;

    const meta = document.createElement('span');
    meta.className = 'poll-template-card-meta';
    meta.textContent = (t.poll_type === 'anonymous' ? 'Anonymous' : 'Named') +
        (t.multi_select ? ' · Multi-select' : '') +
        ' · ' + opts.length + ' option' + (opts.length !== 1 ? 's' : '');

    info.append(titleEl, meta);

    const actions = document.createElement('div');
    actions.className = 'poll-template-card-actions';

    if (CAN_MANAGE_POLLS) {
        const launchBtn = document.createElement('button');
        launchBtn.className = 'btn btn-primary btn-sm';
        launchBtn.textContent = 'Launch Poll';
        launchBtn.addEventListener('click', () => openPollInstanceModal(null, t));

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openPollTemplateModal(t));

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-danger btn-sm';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', async () => {
            if (!await showConfirm('Delete this poll template? All launched polls from this template will keep their data.', 'Delete')) return;
            const res = await fetch('/api/comms/poll-templates/' + t.id, { method: 'DELETE' });
            if (res.ok) {
                pollLoaded['poll-templates'] = false;
                _fetchPollTemplates();
                showToast('Template deleted.');
            } else {
                showToast('Delete failed.', 'error');
            }
        });

        actions.append(launchBtn, editBtn, delBtn);
    }

    header.append(info, actions);
    card.appendChild(header);

    const question = document.createElement('p');
    question.className = 'poll-template-card-question';
    question.textContent = t.question;
    card.appendChild(question);

    const optRow = document.createElement('div');
    optRow.className = 'poll-options-row';
    opts.forEach(o => {
        const chip = document.createElement('span');
        chip.className = 'poll-option-chip';
        chip.textContent = o;
        optRow.appendChild(chip);
    });
    card.appendChild(optRow);

    return card;
}

// ── Poll Instance List ────────────────────────────────────────────────────────

function renderPollInstanceList(items) {
    const listEl = document.getElementById('poll-instance-list');
    if (!listEl) return;
    listEl.replaceChildren();

    if (items.length === 0) {
        const p = document.createElement('p');
        p.className = 'comms-empty';
        p.textContent = 'No polls launched yet.' + (CAN_MANAGE_POLLS ? ' Use a poll template to launch one.' : '');
        listEl.appendChild(p);
        return;
    }

    items.forEach(pi => listEl.appendChild(renderPollInstanceCard(pi)));
}

function renderPollInstanceCard(pi) {
    const card = document.createElement('div');
    card.className = 'poll-instance-card';

    const header = document.createElement('div');
    header.className = 'poll-instance-card-header';

    const info = document.createElement('div');
    const label = document.createElement('span');
    label.className = 'poll-instance-card-label';
    label.textContent = pi.label;

    const meta = document.createElement('span');
    meta.className = 'poll-template-card-meta';
    meta.textContent = (pi.poll_type === 'anonymous' ? 'Anonymous' : 'Named') +
        (pi.multi_select ? ' · Multi-select' : '') +
        (pi.rank_filter ? ' · ' + JSON.parse(pi.rank_filter).join(', ') : '') +
        ' · ' + formatDate(pi.created_at);
    info.append(label, meta);

    const actions = document.createElement('div');
    actions.className = 'poll-template-card-actions';

    const viewBtn = document.createElement('button');
    viewBtn.className = 'btn btn-secondary btn-sm';
    viewBtn.textContent = 'View';
    viewBtn.addEventListener('click', () => openPollDetailModal(pi));
    actions.appendChild(viewBtn);

    if (CAN_MANAGE_POLLS) {
        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openPollInstanceModal(pi, null));
        actions.appendChild(editBtn);

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-danger btn-sm';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', async () => {
            if (!await showConfirm('Delete this poll and all its responses?', 'Delete')) return;
            const res = await fetch('/api/comms/poll-instances/' + pi.id, { method: 'DELETE' });
            if (res.ok) {
                pollLoaded['polls'] = false;
                _fetchPollInstances();
                showToast('Poll deleted.');
            } else {
                showToast('Delete failed.', 'error');
            }
        });
        actions.appendChild(delBtn);
    }

    header.append(info, actions);
    card.appendChild(header);

    const question = document.createElement('p');
    question.className = 'poll-template-card-question';
    question.textContent = pi.question;
    card.appendChild(question);

    const opts = parsePollOptions(pi.options);
    if (opts.length > 0) {
        const optRow = document.createElement('div');
        optRow.className = 'poll-options-row';
        opts.forEach(o => {
            const chip = document.createElement('span');
            chip.className = 'poll-option-chip';
            chip.textContent = o;
            optRow.appendChild(chip);
        });
        card.appendChild(optRow);
    }

    // Progress bar
    const progressWrap = document.createElement('div');
    progressWrap.className = 'poll-progress-wrap';

    const progressText = document.createElement('span');
    progressText.className = 'poll-progress-text';
    if (pi.poll_type === 'named') {
        progressText.textContent = pi.responded_count + ' / ' + pi.total_eligible + ' responded';
    } else {
        progressText.textContent = pi.responded_count + ' response' + (pi.responded_count !== 1 ? 's' : '') + ' recorded';
    }

    if (pi.poll_type === 'named' && pi.total_eligible > 0) {
        const bar = document.createElement('div');
        bar.className = 'poll-progress-bar';
        const fill = document.createElement('div');
        fill.className = 'poll-progress-fill';
        fill.style.width = Math.min(100, Math.round(pi.responded_count / pi.total_eligible * 100)) + '%';
        bar.appendChild(fill);
        progressWrap.append(bar, progressText);
    } else {
        progressWrap.appendChild(progressText);
    }
    card.appendChild(progressWrap);

    // Export row — available to anyone who can view the poll
    const exportRow = document.createElement('div');
    exportRow.className = 'poll-export-row';

    const csvBtn = document.createElement('button');
    csvBtn.className = 'btn btn-secondary btn-sm';
    csvBtn.textContent = 'Export CSV';
    csvBtn.addEventListener('click', async () => {
        const data = await fetchPollDetail(pi.id);
        if (!data) return;
        exportPollRowsCSV(buildPollExportRows(pi, data), pollFilenameSlug(pi.label) + '.csv');
    });

    const xlsxBtn = document.createElement('button');
    xlsxBtn.className = 'btn btn-secondary btn-sm';
    xlsxBtn.textContent = 'Export XLSX';
    xlsxBtn.addEventListener('click', async () => {
        const data = await fetchPollDetail(pi.id);
        if (!data) return;
        exportPollRowsXLSX(buildPollExportRows(pi, data), pollFilenameSlug(pi.label) + '.xlsx');
    });

    const copyBtn = document.createElement('button');
    copyBtn.className = 'btn btn-secondary btn-sm';
    copyBtn.textContent = 'Copy Summary';
    copyBtn.addEventListener('click', async () => {
        const data = await fetchPollDetail(pi.id);
        if (!data) return;
        const text = buildPollSummaryText(pi, data);
        try {
            // writeToClipboard (mail.js, loaded before polls.js on this page) falls
            // back to execCommand in non-secure HTTP contexts where navigator.clipboard
            // is unavailable.
            await writeToClipboard(text);
            showToast('Summary copied to clipboard.');
        } catch {
            showToast('Failed to copy to clipboard.', 'error');
        }
    });

    exportRow.append(csvBtn, xlsxBtn, copyBtn);
    card.appendChild(exportRow);

    return card;
}

// ── Poll export (CSV / XLSX / text summary) ───────────────────────────────────

async function fetchPollDetail(id) {
    try {
        const res = await fetch('/api/comms/poll-instances/' + id + '/detail');
        if (!res.ok) throw new Error('fetch failed');
        return await res.json();
    } catch {
        showToast('Failed to load poll data.', 'error');
        return null;
    }
}

// Build the tabular export rows (array-of-arrays, first row = header).
// Column scope is identical for CSV and XLSX — no action columns.
function buildPollExportRows(pi, data) {
    if (pi.poll_type === 'anonymous') {
        const counts = data.counts || [];
        const total = counts.reduce((s, c) => s + (c.response_count || 0), 0);
        const rows = [['Option', 'Response Count', '% of Total']];
        counts.forEach(c => {
            const pct = total > 0 ? (c.response_count / total * 100).toFixed(1) : '0.0';
            rows.push([c.option_key, c.response_count, pct + '%']);
        });
        return rows;
    }

    // Named: one row per member-option pair; non-responders get one blank row.
    const rows = [['Member Name', 'Rank', 'Option', 'Responded At']];
    (data.responded || []).forEach(m => {
        const opts = (m.options && m.options.length) ? m.options : [''];
        opts.forEach(opt => {
            rows.push([m.member_name, m.rank, opt, formatDateTime(m.responded_at)]);
        });
    });
    (data.pending || []).forEach(m => {
        rows.push([m.member_name, m.rank, '', '']);
    });
    return rows;
}

function buildPollSummaryText(pi, data) {
    const lines = [pi.label, pi.question, ''];

    if (pi.poll_type === 'anonymous') {
        const counts = data.counts || [];
        const total = counts.reduce((s, c) => s + (c.response_count || 0), 0);
        lines.push('Total responses: ' + total);
        counts.forEach(c => {
            const pct = total > 0 ? (c.response_count / total * 100).toFixed(1) : '0.0';
            lines.push('  ' + c.option_key + ': ' + c.response_count + ' (' + pct + '%)');
        });
        return lines.join('\n');
    }

    const responded = data.responded || [];
    const pending = data.pending || [];

    lines.push('Responded (' + responded.length + '):');
    responded.forEach(m => {
        const opts = (m.options && m.options.length) ? ' — ' + m.options.join(', ') : '';
        lines.push('  ' + m.member_name + opts);
    });
    lines.push('');
    lines.push('Not Responded (' + pending.length + '):');
    pending.forEach(m => lines.push('  ' + m.member_name));

    return lines.join('\n');
}

function pollRowsToCSV(rows) {
    return '﻿' + rows.map(row =>
        row.map(val => {
            const s = String(val ?? '');
            return (s.includes(',') || s.includes('"') || s.includes('\n') || s.includes('\r'))
                ? '"' + s.replace(/"/g, '""') + '"'
                : s;
        }).join(',')
    ).join('\r\n');
}

function exportPollRowsCSV(rows, filename) {
    const blob = new Blob([pollRowsToCSV(rows)], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
}

function exportPollRowsXLSX(rows, filename) {
    const ws = XLSX.utils.aoa_to_sheet(rows);
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, 'Sheet1');
    XLSX.writeFile(wb, filename);
}

function pollFilenameSlug(label) {
    return (label || 'poll').replace(/[^\w-]+/g, '_').replace(/^_+|_+$/g, '').toLowerCase() || 'poll';
}

// ── Poll Template Modal ───────────────────────────────────────────────────────

function openPollTemplateModal(template) {
    const modal = document.getElementById('modal-poll-template');
    document.getElementById('modal-poll-template-title').textContent = template ? 'Edit Poll Template' : 'New Poll Template';
    document.getElementById('poll-template-id').value = template?.id ?? '';
    document.getElementById('poll-template-title-input').value = template?.title ?? '';
    document.getElementById('poll-template-question-input').value = template?.question ?? '';
    document.getElementById('poll-template-multiselect').checked = template?.multi_select ?? false;
    document.getElementById('poll-template-status').textContent = '';

    const pollType = template?.poll_type ?? 'named';
    document.querySelectorAll('input[name="poll-type"]').forEach(r => {
        r.checked = r.value === pollType;
    });

    const opts = template ? parsePollOptions(template.options) : ['Yes', 'No'];
    renderOptionsBuilder(opts);

    modal.style.display = 'flex';
    document.getElementById('poll-template-title-input').focus();
}

function renderOptionsBuilder(opts) {
    const builder = document.getElementById('poll-options-builder');
    builder.replaceChildren();
    opts.forEach((o, i) => builder.appendChild(buildOptionRow(o, i)));
}

function buildOptionRow(value, index) {
    const row = document.createElement('div');
    row.className = 'poll-option-row';

    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'form-input poll-option-input';
    input.value = value;
    input.placeholder = 'Option ' + (index + 1);
    input.autocomplete = 'off';

    const removeBtn = document.createElement('button');
    removeBtn.type = 'button';
    removeBtn.className = 'btn btn-danger btn-sm';
    removeBtn.textContent = '×';
    removeBtn.addEventListener('click', () => {
        row.remove();
    });

    row.append(input, removeBtn);
    return row;
}

async function savePollTemplate() {
    const id = document.getElementById('poll-template-id').value;
    const title = document.getElementById('poll-template-title-input').value.trim();
    const question = document.getElementById('poll-template-question-input').value.trim();
    const pollType = document.querySelector('input[name="poll-type"]:checked')?.value ?? 'named';
    const multiSelect = document.getElementById('poll-template-multiselect').checked;
    const statusEl = document.getElementById('poll-template-status');

    const optInputs = document.querySelectorAll('#poll-options-builder .poll-option-input');
    const options = Array.from(optInputs).map(i => i.value.trim()).filter(Boolean);

    if (!title) {
        setFieldError(document.getElementById('poll-template-title-input'), 'Title is required.');
        return;
    }
    if (!question) {
        setFieldError(document.getElementById('poll-template-question-input'), 'Question is required.');
        return;
    }

    const url = id ? '/api/comms/poll-templates/' + id : '/api/comms/poll-templates';
    const method = id ? 'PUT' : 'POST';

    statusEl.textContent = 'Saving…';
    try {
        const res = await fetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ title, question, options, poll_type: pollType, multi_select: multiSelect }),
        });
        if (!res.ok) {
            statusEl.textContent = await res.text() || 'Save failed.';
            return;
        }
        document.getElementById('modal-poll-template').style.display = '';
        pollLoaded['poll-templates'] = false;
        _fetchPollTemplates();
        showToast(id ? 'Template updated.' : 'Template created.');
    } catch {
        statusEl.textContent = 'Network error.';
    }
}

// ── Poll Instance Modal ───────────────────────────────────────────────────────

function openPollInstanceModal(instance, template) {
    const modal = document.getElementById('modal-poll-instance');
    const isEdit = !!instance;

    document.getElementById('modal-poll-instance-title').textContent = isEdit ? 'Edit Poll' : 'Launch Poll';
    document.getElementById('poll-instance-id').value = instance?.id ?? '';
    document.getElementById('poll-instance-template-id').value = template?.id ?? '';
    document.getElementById('poll-instance-label-input').value = instance?.label ?? '';
    document.getElementById('poll-instance-status').textContent = '';

    const preview = document.getElementById('poll-instance-question-preview');
    const infoGroup = document.getElementById('poll-instance-info-group');
    const questionGroup = document.getElementById('poll-instance-question-group');
    const questionInput = document.getElementById('poll-instance-question-input');
    const optionsGroup = document.getElementById('poll-instance-options-group');
    const optionsBuilder = document.getElementById('poll-instance-options-builder');

    if (isEdit) {
        preview.style.display = 'none';
        questionInput.value = instance.question ?? '';
        questionGroup.style.display = 'block';

        // Metadata badge row
        const typeBadge = instance.poll_type === 'anonymous' ? 'Anonymous' : 'Named';
        const multiBadge = instance.multi_select ? ' · Multi-select' : '';
        const rankBadge = instance.rank_filter ? ' · ' + JSON.parse(instance.rank_filter).join(', ') : ' · All ranks';
        infoGroup.textContent = typeBadge + multiBadge + rankBadge;
        infoGroup.style.display = 'block';

        // Editable option inputs (count locked)
        const opts = parsePollOptions(instance.options);
        optionsBuilder.replaceChildren();
        opts.forEach(opt => {
            const input = document.createElement('input');
            input.type = 'text';
            input.className = 'form-input poll-option-input';
            input.value = opt;
            input.autocomplete = 'off';
            input.style.marginBottom = '6px';
            optionsBuilder.appendChild(input);
        });
        optionsGroup.style.display = 'block';
    } else {
        preview.textContent = template?.question ?? '';
        preview.style.display = preview.textContent ? 'block' : 'none';
        questionInput.value = '';
        questionGroup.style.display = 'none';
        infoGroup.style.display = 'none';
        optionsBuilder.replaceChildren();
        optionsGroup.style.display = 'none';
    }

    // Rank filter only relevant on launch
    const rankGroup = document.getElementById('poll-rank-filter-group');
    rankGroup.style.display = isEdit ? 'none' : 'block';
    document.querySelectorAll('input[name="poll-rank"]').forEach(cb => { cb.checked = false; });

    document.getElementById('poll-instance-save-btn').textContent = isEdit ? 'Save' : 'Launch';
    modal.style.display = 'flex';
    document.getElementById('poll-instance-label-input').focus();
}

async function savePollInstance() {
    const id = document.getElementById('poll-instance-id').value;
    const templateId = document.getElementById('poll-instance-template-id').value;
    const label = document.getElementById('poll-instance-label-input').value.trim();
    const statusEl = document.getElementById('poll-instance-status');

    if (!label) {
        setFieldError(document.getElementById('poll-instance-label-input'), 'Label is required.');
        return;
    }

    statusEl.textContent = 'Saving…';
    try {
        let res;
        if (id) {
            const question = document.getElementById('poll-instance-question-input').value.trim();
            const options = Array.from(document.querySelectorAll('#poll-instance-options-builder .poll-option-input'))
                .map(i => i.value.trim());
            res = await fetch('/api/comms/poll-instances/' + id, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ label, question, options }),
            });
        } else {
            // Launch
            const rankFilter = Array.from(document.querySelectorAll('input[name="poll-rank"]:checked')).map(cb => cb.value);
            res = await fetch('/api/comms/poll-instances', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ template_id: parseInt(templateId), label, rank_filter: rankFilter }),
            });
        }
        if (!res.ok) {
            statusEl.textContent = await res.text() || 'Save failed.';
            return;
        }
        document.getElementById('modal-poll-instance').style.display = '';
        pollLoaded['polls'] = false;
        _fetchPollInstances();
        showToast(id ? 'Poll updated.' : 'Poll launched.');
    } catch {
        statusEl.textContent = 'Network error.';
    }
}

// ── Poll Detail Modal ─────────────────────────────────────────────────────────

async function openPollDetailModal(pi) {
    const modal = document.getElementById('modal-poll-detail');
    pollDetailView = 'member';   // each open starts on the member view
    pollListDirty = false;
    document.getElementById('poll-detail-label').textContent = pi.label;
    document.getElementById('poll-detail-question').textContent = pi.question;
    document.getElementById('poll-detail-progress').textContent = '';
    document.getElementById('poll-detail-body').textContent = 'Loading…';
    modal.style.display = 'flex';

    try {
        const res = await fetch('/api/comms/poll-instances/' + pi.id + '/detail');
        if (!res.ok) throw new Error('fetch failed');
        const data = await res.json();
        renderPollDetail(pi, data);
    } catch {
        document.getElementById('poll-detail-body').textContent = 'Failed to load.';
    }
}

function renderPollDetail(pi, data) {
    // Tear down any pickers from the previous render BEFORE the body is wiped —
    // removing a focused input from the DOM does not fire blur, so cleanup must be
    // explicit here (covers view switch, refresh, and rollback re-render).
    destroyOptionPickers();
    pollDetailInstance = pi;
    pollDetailData = data;

    const progressEl = document.getElementById('poll-detail-progress');
    const bodyEl = document.getElementById('poll-detail-body');

    if (pi.poll_type === 'anonymous') {
        const total = data.responded_count || 0;
        progressEl.textContent = total + ' response' + (total !== 1 ? 's' : '') + ' recorded';

        bodyEl.replaceChildren();
        const table = document.createElement('table');
        table.className = 'data-table';
        const thead = document.createElement('thead');
        const hr = document.createElement('tr');
        ['Option', 'Count'].forEach(h => {
            const th = document.createElement('th');
            th.textContent = h;
            hr.appendChild(th);
        });
        thead.appendChild(hr);
        table.appendChild(thead);

        const tbody = document.createElement('tbody');
        (data.counts || []).forEach(c => {
            const tr = document.createElement('tr');
            const tdOpt = document.createElement('td');
            tdOpt.textContent = c.option_key;
            const tdCount = document.createElement('td');

            if (CAN_MANAGE_POLLS) {
                const input = document.createElement('input');
                input.type = 'number';
                input.min = '0';
                input.value = c.response_count;
                input.className = 'form-input poll-anon-count-input';
                input.dataset.option = c.option_key;
                tdCount.appendChild(input);
            } else {
                tdCount.textContent = c.response_count;
            }

            tr.append(tdOpt, tdCount);
            tbody.appendChild(tr);
        });
        table.appendChild(tbody);
        bodyEl.appendChild(table);

        if (CAN_MANAGE_POLLS) {
            const saveBtn = document.createElement('button');
            saveBtn.className = 'btn btn-primary btn-sm';
            saveBtn.textContent = 'Save Counts';
            saveBtn.style.marginTop = '12px';
            saveBtn.addEventListener('click', () => saveAnonCounts(pi.id));
            bodyEl.appendChild(saveBtn);
        }
        return;
    }

    // Named poll
    const pending = data.pending || [];
    const responded = data.responded || [];
    progressEl.textContent = responded.length + ' / ' + (pi.total_eligible || pending.length + responded.length) + ' responded';

    bodyEl.replaceChildren();
    bodyEl.appendChild(renderPollViewToggle(pi));

    if (pollDetailView === 'option') {
        renderPollByOption(pi, data, bodyEl);
    } else {
        renderPollByMember(pi, data, bodyEl);
    }
}

function renderPollViewToggle(pi) {
    const bar = document.createElement('div');
    bar.className = 'poll-view-toggle';
    [['member', 'By member'], ['option', 'By option']].forEach(([val, label]) => {
        const chip = document.createElement('button');
        chip.type = 'button';
        chip.className = 'filter-chip' + (pollDetailView === val ? ' active' : '');
        chip.textContent = label;
        chip.addEventListener('click', () => {
            if (pollDetailView === val) return;
            pollDetailView = val;
            renderPollDetail(pollDetailInstance, pollDetailData);
        });
        bar.appendChild(chip);
    });
    return bar;
}

function renderPollByMember(pi, data, bodyEl) {
    const pending = data.pending || [];
    const responded = data.responded || [];

    // Member search — filters rows in place (no re-render) so focus is preserved.
    const search = document.createElement('input');
    search.type = 'text';
    search.className = 'form-input poll-member-search';
    search.placeholder = 'Search member…';
    search.autocomplete = 'off';
    bodyEl.appendChild(search);

    const sections = [];

    if (pending.length > 0) {
        const section = document.createElement('div');
        section.className = 'poll-pending-section';
        const heading = document.createElement('h3');
        heading.className = 'poll-section-heading poll-pending-heading';
        heading.textContent = 'Pending (' + pending.length + ')';
        section.appendChild(heading);
        const list = document.createElement('div');
        list.className = 'poll-member-list';
        pending.forEach(m => list.appendChild(renderMemberRow(m, pi, false)));
        section.appendChild(list);
        bodyEl.appendChild(section);
        sections.push(section);
    }

    if (responded.length > 0) {
        const section = document.createElement('div');
        section.className = 'poll-responded-section';
        const heading = document.createElement('h3');
        heading.className = 'poll-section-heading';
        heading.textContent = 'Responded (' + responded.length + ')';
        section.appendChild(heading);
        const list = document.createElement('div');
        list.className = 'poll-member-list';
        responded.forEach(m => list.appendChild(renderMemberRow(m, pi, true)));
        section.appendChild(list);
        bodyEl.appendChild(section);
        sections.push(section);
    }

    if (pending.length === 0 && responded.length === 0) {
        search.style.display = 'none';
        const p = document.createElement('p');
        p.className = 'comms-empty';
        p.textContent = 'No members in scope for this poll.';
        bodyEl.appendChild(p);
        return;
    }

    search.addEventListener('input', () => {
        const q = search.value.trim().toLowerCase();
        sections.forEach(sec => {
            let anyVisible = false;
            sec.querySelectorAll('.poll-member-row').forEach(row => {
                const show = !q || (row.dataset.name || '').includes(q);
                row.style.display = show ? '' : 'none';
                if (show) anyVisible = true;
            });
            sec.style.display = anyVisible ? '' : 'none';
        });
    });
}

// ── By-option view ───────────────────────────────────────────────────────────

function renderPollByOption(pi, data, bodyEl) {
    const byOption = data.by_option || [];
    if (byOption.length === 0) {
        const p = document.createElement('p');
        p.className = 'comms-empty';
        p.textContent = 'This poll has no options.';
        bodyEl.appendChild(p);
        return;
    }

    // Candidate pool = every eligible member (pending ∪ responded), normalized to
    // the picker's {id,name,rank} shape. No roster fetch needed.
    const pool = [];
    (data.pending || []).forEach(m => pool.push({ id: m.member_id, name: m.member_name, rank: m.rank }));
    (data.responded || []).forEach(m => pool.push({ id: m.member_id, name: m.member_name, rank: m.rank }));

    byOption.forEach(bucket => {
        const option = bucket.option;
        const section = document.createElement('div');
        section.className = 'poll-option-section';

        const heading = document.createElement('h3');
        heading.className = 'poll-section-heading';
        heading.appendChild(document.createTextNode(option + ' '));
        const count = document.createElement('span');
        count.className = 'poll-option-count';
        count.textContent = '(' + bucket.members.length + ')';
        heading.appendChild(count);
        section.appendChild(heading);

        const list = document.createElement('div');
        list.className = 'poll-option-members';
        bucket.members.forEach(m => list.appendChild(renderOptionChip(pi, option, m)));
        if (bucket.members.length === 0) {
            const empty = document.createElement('span');
            empty.className = 'poll-option-empty';
            empty.textContent = 'No one yet';
            list.appendChild(empty);
        }
        section.appendChild(list);

        if (CAN_MANAGE_POLLS) {
            const picker = createMemberPicker({
                placeholder: 'Add name…',
                getCandidates: () => pool,
                isExcluded: (cand) => memberInOption(pollDetailData, option, cand.id),
                onPick: (cand) => addToOption(pi, option, cand),
            });
            activeOptionPickers.push(picker);
            section.appendChild(picker.el);
        }

        bodyEl.appendChild(section);
    });
}

function renderOptionChip(pi, option, m) {
    const chip = document.createElement('span');
    chip.className = 'poll-option-chip';
    const rank = document.createElement('span');
    rank.className = 'member-rank rank-' + m.rank;
    rank.textContent = m.rank;
    chip.append(rank, ' ' + m.member_name);
    if (CAN_MANAGE_POLLS) {
        const rm = document.createElement('button');
        rm.type = 'button';
        rm.className = 'poll-option-remove';
        rm.textContent = '×';
        rm.setAttribute('aria-label', 'Remove ' + m.member_name + ' from ' + option);
        // option_key stays in this closure — never interpolated into a selector.
        rm.addEventListener('click', () => removeFromOption(pi, option, m.member_id));
        chip.appendChild(rm);
    }
    return chip;
}

function memberInOption(data, option, memberId) {
    const bucket = (data.by_option || []).find(b => b.option === option);
    return !!(bucket && bucket.members.some(m => m.member_id === memberId));
}

function pollGuardKey(pi, memberId, option) {
    // Single-select adds delete the member's other option, so different-option taps
    // for one member must serialize → guard on memberId alone. Multi-select options
    // are independent → guard per (member, option).
    return pi.multi_select ? memberId + ':' + option : String(memberId);
}

// Optimistically move a member INTO an option across all three payload arrays.
function applyAddOptimistic(data, pi, option, cand) {
    const nowIso = new Date().toISOString().slice(0, 19).replace('T', ' ');
    const bucket = (data.by_option || []).find(b => b.option === option);
    if (bucket && !bucket.members.some(m => m.member_id === cand.id)) {
        bucket.members.push({ member_id: cand.id, member_name: cand.name, rank: cand.rank, recorded_at: nowIso });
    }
    if (!pi.multi_select) {
        (data.by_option || []).forEach(b => {
            if (b.option !== option) b.members = b.members.filter(m => m.member_id !== cand.id);
        });
    }
    let ms = (data.responded || []).find(m => m.member_id === cand.id);
    if (!ms) {
        const idx = (data.pending || []).findIndex(m => m.member_id === cand.id);
        if (idx >= 0) {
            ms = data.pending.splice(idx, 1)[0];
        } else {
            ms = { member_id: cand.id, member_name: cand.name, rank: cand.rank };
        }
        ms.responded = true;
        ms.options = [];
        ms.responded_at = nowIso;
        (data.responded = data.responded || []).push(ms);
    }
    if (!pi.multi_select) ms.options = [option];
    else if (!ms.options.includes(option)) ms.options.push(option);
}

// Optimistically remove a member FROM an option across all three payload arrays.
function applyRemoveOptimistic(data, option, memberId) {
    const bucket = (data.by_option || []).find(b => b.option === option);
    if (bucket) bucket.members = bucket.members.filter(m => m.member_id !== memberId);
    const ms = (data.responded || []).find(m => m.member_id === memberId);
    if (ms) {
        ms.options = (ms.options || []).filter(o => o !== option);
        if (ms.options.length === 0) {
            data.responded = data.responded.filter(m => m.member_id !== memberId);
            ms.responded = false;
            ms.responded_at = '';
            (data.pending = data.pending || []).push(ms);
        }
    }
}

function addToOption(pi, option, cand) {
    const key = pollGuardKey(pi, cand.id, option);
    if (pollToggleInFlight.has(key)) return;
    pollToggleInFlight.add(key);
    const snapshot = JSON.parse(JSON.stringify(pollDetailData));
    applyAddOptimistic(pollDetailData, pi, option, cand);
    renderPollDetail(pi, pollDetailData);
    sendToggle(pi, cand.id, option, true, key, snapshot);
}

function removeFromOption(pi, option, memberId) {
    const key = pollGuardKey(pi, memberId, option);
    if (pollToggleInFlight.has(key)) return;
    pollToggleInFlight.add(key);
    const snapshot = JSON.parse(JSON.stringify(pollDetailData));
    applyRemoveOptimistic(pollDetailData, option, memberId);
    renderPollDetail(pi, pollDetailData);
    sendToggle(pi, memberId, option, false, key, snapshot);
}

async function sendToggle(pi, memberId, option, selected, key, snapshot) {
    try {
        const res = await fetch('/api/comms/poll-instances/' + pi.id + '/responses/' + memberId + '/toggle', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ option, selected }),
        });
        pollToggleInFlight.delete(key);
        if (res.ok) {
            pollListDirty = true;
            return;
        }
        if (res.status === 400) {
            // Options were likely edited by another officer — resync from the server
            // rather than restoring an equally-stale snapshot.
            showToast('Poll changed — refreshed.', 'info');
            await reloadPollDetail(pi);
            return;
        }
        pollDetailData = snapshot;
        renderPollDetail(pi, snapshot);
        showToast('Failed to save.', 'error');
    } catch {
        pollToggleInFlight.delete(key);
        pollDetailData = snapshot;
        renderPollDetail(pi, snapshot);
        showToast('Failed to save.', 'error');
    }
}

async function reloadPollDetail(pi) {
    try {
        const res = await fetch('/api/comms/poll-instances/' + pi.id + '/detail');
        if (!res.ok) throw new Error('fetch failed');
        const data = await res.json();
        renderPollDetail(pi, data);
    } catch {
        showToast('Failed to refresh poll.', 'error');
    }
}

function closePollDetailModal() {
    // poll-detail-close-btn is the modal's ONLY close path (no backdrop/Esc). Both
    // the picker teardown and the deferred instance-list refresh hang off it — if a
    // backdrop/Esc close is added later, route it through here too.
    destroyOptionPickers();
    document.getElementById('modal-poll-detail').style.display = '';
    if (pollListDirty) {
        pollListDirty = false;
        pollLoaded['polls'] = false;
        _fetchPollInstances();
    }
    pollDetailData = null;
    pollDetailInstance = null;
    pollDetailView = 'member';
}

function renderMemberRow(m, pi, hasResponded) {
    const row = document.createElement('div');
    row.className = 'poll-member-row' + (pi.multi_select ? ' poll-member-row--multi' : '');
    row.dataset.name = (m.member_name || '').toLowerCase();

    const nameSpan = document.createElement('span');
    nameSpan.className = 'poll-member-name';
    const rankBadge = document.createElement('span');
    rankBadge.className = 'member-rank';
    rankBadge.textContent = m.rank;
    nameSpan.append(rankBadge, ' ', m.member_name);
    row.appendChild(nameSpan);

    if (!pi.multi_select && hasResponded && m.options?.length > 0) {
        const optSpan = document.createElement('span');
        optSpan.className = 'poll-member-options';
        optSpan.textContent = m.options.join(', ');
        row.appendChild(optSpan);
    }

    if (CAN_MANAGE_POLLS) {
        const actionsSpan = document.createElement('span');
        actionsSpan.className = 'poll-member-actions';

        if (pi.multi_select) {
            // Multi-select: checkboxes pre-checked with current selections + Save button.
            // Same UI whether pending or responded — officer can update at any time.
            const opts = parsePollOptions(pi.options);
            const currentSelections = new Set(m.options || []);
            const checkboxes = opts.map(opt => {
                const lbl = document.createElement('label');
                lbl.className = 'poll-multi-option';
                const cb = document.createElement('input');
                cb.type = 'checkbox';
                cb.value = opt;
                cb.checked = currentSelections.has(opt);
                lbl.append(cb, opt);
                return lbl;
            });
            checkboxes.forEach(lbl => actionsSpan.appendChild(lbl));

            const saveBtn = document.createElement('button');
            saveBtn.className = 'btn btn-primary btn-sm';
            saveBtn.textContent = 'Save';
            saveBtn.addEventListener('click', async () => {
                const selected = checkboxes
                    .map(lbl => lbl.querySelector('input'))
                    .filter(cb => cb.checked)
                    .map(cb => cb.value);
                let res;
                if (selected.length === 0) {
                    res = await fetch('/api/comms/poll-instances/' + pi.id + '/responses/' + m.member_id, { method: 'DELETE' });
                } else {
                    res = await fetch('/api/comms/poll-instances/' + pi.id + '/responses', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ member_id: m.member_id, options: selected }),
                    });
                }
                if (res.ok) {
                    await refreshPollDetail(pi);
                } else {
                    showToast('Failed to save.', 'error');
                }
            });
            actionsSpan.appendChild(saveBtn);
        } else if (hasResponded) {
            const clearBtn = document.createElement('button');
            clearBtn.className = 'btn btn-secondary btn-sm';
            clearBtn.textContent = 'Clear';
            clearBtn.addEventListener('click', async () => {
                const res = await fetch('/api/comms/poll-instances/' + pi.id + '/responses/' + m.member_id, { method: 'DELETE' });
                if (res.ok) {
                    await refreshPollDetail(pi);
                } else {
                    showToast('Failed to clear response.', 'error');
                }
            });
            actionsSpan.appendChild(clearBtn);
        } else {
            // Single-select: one button per option, immediate save
            const opts = parsePollOptions(pi.options);
            opts.forEach(opt => {
                const btn = document.createElement('button');
                btn.className = 'btn btn-primary btn-sm';
                btn.textContent = opt;
                btn.addEventListener('click', async () => {
                    const res = await fetch('/api/comms/poll-instances/' + pi.id + '/responses', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ member_id: m.member_id, options: [opt] }),
                    });
                    if (res.ok) {
                        await refreshPollDetail(pi);
                    } else {
                        showToast('Failed to record response.', 'error');
                    }
                });
                actionsSpan.appendChild(btn);
            });
        }
        row.appendChild(actionsSpan);
    }

    return row;
}

async function refreshPollDetail(pi) {
    try {
        const res = await fetch('/api/comms/poll-instances/' + pi.id + '/detail');
        if (!res.ok) throw new Error('fetch failed');
        const data = await res.json();
        renderPollDetail(pi, data);
        // Also refresh the instance list card in the background
        pollLoaded['polls'] = false;
        _fetchPollInstances();
    } catch {
        showToast('Failed to refresh poll.', 'error');
    }
}

async function saveAnonCounts(instanceId) {
    const inputs = document.querySelectorAll('.poll-anon-count-input');
    const counts = {};
    inputs.forEach(inp => {
        counts[inp.dataset.option] = parseInt(inp.value) || 0;
    });
    const res = await fetch('/api/comms/poll-instances/' + instanceId + '/anonymous-counts', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ counts }),
    });
    if (res.ok) {
        pollLoaded['polls'] = false;
        _fetchPollInstances();
        showToast('Counts saved.');
    } else {
        showToast('Failed to save counts.', 'error');
    }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function parsePollOptions(raw) {
    try {
        const parsed = JSON.parse(raw || '[]');
        return Array.isArray(parsed) ? parsed : [];
    } catch {
        return [];
    }
}

function formatDate(isoStr) {
    if (!isoStr) return '';
    const d = new Date(isoStr.replace(' ', 'T') + 'Z');
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

function formatDateTime(isoStr) {
    if (!isoStr) return '';
    const d = new Date(isoStr.replace(' ', 'T') + 'Z');
    return d.toLocaleString(undefined, {
        month: 'short', day: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit',
    });
}

// ── DOMContentLoaded wiring ───────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
    // New poll template button
    document.getElementById('new-poll-template-btn')?.addEventListener('click', () => openPollTemplateModal(null));

    // New poll instance button (opens template picker — for now just shows a toast to guide to template list)
    document.getElementById('new-poll-instance-btn')?.addEventListener('click', () => {
        showToast('Select a template and click "Launch Poll".', 'info');
        // Switch to poll-templates tab
        if (typeof switchTab === 'function') switchTab('poll-templates');
    });

    // Poll template modal buttons
    document.getElementById('poll-template-save-btn')?.addEventListener('click', savePollTemplate);
    document.getElementById('poll-template-cancel-btn')?.addEventListener('click', () => {
        document.getElementById('modal-poll-template').style.display = '';
    });
    document.getElementById('poll-add-option-btn')?.addEventListener('click', () => {
        const builder = document.getElementById('poll-options-builder');
        const count = builder.querySelectorAll('.poll-option-row').length;
        builder.appendChild(buildOptionRow('', count));
    });

    // Poll instance modal buttons
    document.getElementById('poll-instance-save-btn')?.addEventListener('click', savePollInstance);
    document.getElementById('poll-instance-cancel-btn')?.addEventListener('click', () => {
        document.getElementById('modal-poll-instance').style.display = '';
    });

    // Poll detail close
    document.getElementById('poll-detail-close-btn')?.addEventListener('click', closePollDetailModal);

    // Auto-load the active poll tab if it happens to be the initial tab
    const activeTab = document.querySelector('.tab-btn.active')?.dataset.tab;
    if (activeTab === 'poll-templates') window.loadPollTemplates();
    else if (activeTab === 'polls') window.loadPollInstances();
});
