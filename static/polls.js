// polls.js — Poll Tracker (runs on the Comms page)

const pollCfg = document.getElementById('page-config')?.dataset ?? {};
const CAN_MANAGE_POLLS = pollCfg.canManagePolls === 'true';

const pollLoaded = {};

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

    return card;
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
    }

    if (pending.length === 0 && responded.length === 0) {
        const p = document.createElement('p');
        p.className = 'comms-empty';
        p.textContent = 'No members in scope for this poll.';
        bodyEl.appendChild(p);
    }
}

function renderMemberRow(m, pi, hasResponded) {
    const row = document.createElement('div');
    row.className = 'poll-member-row';

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
                lbl.append(cb, ' ', opt);
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
    document.getElementById('poll-detail-close-btn')?.addEventListener('click', () => {
        document.getElementById('modal-poll-detail').style.display = '';
    });

    // Auto-load the active poll tab if it happens to be the initial tab
    const activeTab = document.querySelector('.tab-btn.active')?.dataset.tab;
    if (activeTab === 'poll-templates') window.loadPollTemplates();
    else if (activeTab === 'polls') window.loadPollInstances();
});
