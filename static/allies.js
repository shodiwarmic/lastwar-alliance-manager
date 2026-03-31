// allies.js — Allies Tracker page

const CAN_MANAGE = window.CAN_MANAGE === true;

let allTypes = [];   // AllyAgreementType[]
let allAllies = [];  // Ally[]
let editingAllyId = null;
let editingTypeId = null;
let includeInactive = false;

// ── Init ─────────────────────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
    // Show initial active tab
    const activeBtn = document.querySelector('.tab-btn.active');
    if (activeBtn) {
        const target = document.getElementById('tab-' + activeBtn.dataset.tab);
        if (target) target.style.display = 'block';
    }

    // Tab switching
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(t => { t.style.display = 'none'; });
            btn.classList.add('active');
            const target = document.getElementById('tab-' + btn.dataset.tab);
            if (target) target.style.display = 'block';
        });
    });

    // Show inactive toggle
    const toggle = document.getElementById('show-inactive-toggle');
    if (toggle) {
        toggle.addEventListener('change', () => {
            includeInactive = toggle.checked;
            loadAllies();
        });
    }

    // Add ally button
    const addAllyBtn = document.getElementById('add-ally-btn');
    if (addAllyBtn) addAllyBtn.addEventListener('click', () => openAllyModal(null));

    // Add type button
    const addTypeBtn = document.getElementById('add-type-btn');
    if (addTypeBtn) addTypeBtn.addEventListener('click', () => openTypeModal(null));

    // Ally modal form
    document.getElementById('ally-form').addEventListener('submit', e => {
        e.preventDefault();
        saveAlly();
    });
    document.getElementById('ally-modal-cancel').addEventListener('click', closeAllyModal);

    // Type modal form
    document.getElementById('type-form').addEventListener('submit', e => {
        e.preventDefault();
        saveType();
    });
    document.getElementById('type-modal-cancel').addEventListener('click', closeTypeModal);

    loadAgreementTypes().then(() => {
        loadAllies();
        if (CAN_MANAGE) renderTypesTab();
    });
});

// ── Data Loading ─────────────────────────────────────────────────────────────

async function loadAgreementTypes() {
    const res = await fetch('/api/ally-agreement-types');
    if (!res.ok) return;
    allTypes = await res.json();
}

async function loadAllies() {
    const url = '/api/allies' + (includeInactive ? '?include_inactive=true' : '');
    const res = await fetch(url);
    if (!res.ok) return;
    allAllies = await res.json();
    renderAllies();
}

// ── Rendering ─────────────────────────────────────────────────────────────────

function renderAllies() {
    const container = document.getElementById('allies-list');
    if (allAllies.length === 0) {
        container.replaceChildren();
        const p = document.createElement('p');
        p.style.color = 'var(--text-secondary)';
        p.textContent = CAN_MANAGE ? 'No allies yet. Add one to get started.' : 'No allies found.';
        container.appendChild(p);
        return;
    }

    const cards = allAllies.map(ally => renderAllyCard(ally));
    container.replaceChildren(...cards);
}

function renderAllyCard(ally) {
    const card = document.createElement('div');
    card.className = 'ally-card' + (ally.active ? '' : ' inactive');

    // Server badge
    const badge = document.createElement('div');
    badge.className = 'ally-server-badge';
    badge.textContent = ally.server;
    card.appendChild(badge);

    // Body
    const body = document.createElement('div');
    body.className = 'ally-body';

    // Header row: tag, name, inactive badge
    const headerRow = document.createElement('div');
    headerRow.className = 'ally-header-row';

    const tag = document.createElement('span');
    tag.className = 'ally-tag';
    tag.textContent = '[' + ally.tag + ']';

    const name = document.createElement('span');
    name.className = 'ally-name';
    name.textContent = ally.name;

    headerRow.appendChild(tag);
    headerRow.appendChild(name);

    if (!ally.active) {
        const inactiveBadge = document.createElement('span');
        inactiveBadge.className = 'ally-inactive-badge';
        inactiveBadge.textContent = 'Inactive';
        headerRow.appendChild(inactiveBadge);
    }
    body.appendChild(headerRow);

    // Agreement type pills
    if (ally.agreement_type_ids && ally.agreement_type_ids.length > 0) {
        const pills = document.createElement('div');
        pills.className = 'ally-agreements';
        ally.agreement_type_ids.forEach(tid => {
            const t = allTypes.find(x => x.id === tid);
            if (!t) return;
            const pill = document.createElement('span');
            pill.className = 'agreement-pill';
            pill.textContent = t.name;
            pills.appendChild(pill);
        });
        body.appendChild(pills);
    }

    // Meta: contact, notes
    if (ally.contact || ally.notes) {
        const meta = document.createElement('div');
        meta.className = 'ally-meta';
        if (ally.contact) {
            const contactLine = document.createElement('span');
            contactLine.textContent = '📞 ' + ally.contact;
            meta.appendChild(contactLine);
        }
        if (ally.notes) {
            const notesLine = document.createElement('span');
            notesLine.textContent = ally.notes;
            meta.appendChild(notesLine);
        }
        body.appendChild(meta);
    }

    card.appendChild(body);

    // Actions (manage only)
    if (CAN_MANAGE) {
        const actions = document.createElement('div');
        actions.className = 'ally-actions';

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openAllyModal(ally));

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
            yesBtn.addEventListener('click', () => doDeleteAlly(ally.id));
            const noBtn = document.createElement('button');
            noBtn.className = 'btn btn-secondary btn-sm';
            noBtn.textContent = 'No';
            noBtn.addEventListener('click', () => { confirmSpan.remove(); delBtn.style.display = ''; });
            confirmSpan.append(label, yesBtn, noBtn);
            actions.appendChild(confirmSpan);
        });

        actions.appendChild(editBtn);
        actions.appendChild(delBtn);
        card.appendChild(actions);
    }

    return card;
}

function renderTypesTab() {
    const container = document.getElementById('types-list');
    if (!container) return;

    if (allTypes.length === 0) {
        container.replaceChildren();
        const p = document.createElement('p');
        p.style.color = 'var(--text-secondary)';
        p.textContent = 'No agreement types yet.';
        container.appendChild(p);
        return;
    }

    const rows = allTypes.map(t => renderTypeRow(t));
    container.replaceChildren(...rows);
}

function renderTypeRow(t) {
    const row = document.createElement('div');
    row.className = 'type-row' + (t.active ? '' : ' inactive');

    const nameEl = document.createElement('span');
    nameEl.className = 'type-name';
    nameEl.textContent = t.name;
    row.appendChild(nameEl);

    if (!t.active) {
        const inactiveLabel = document.createElement('span');
        inactiveLabel.className = 'type-inactive-label';
        inactiveLabel.textContent = 'Inactive';
        row.appendChild(inactiveLabel);
    }

    const actions = document.createElement('div');
    actions.className = 'type-actions';

    const editBtn = document.createElement('button');
    editBtn.className = 'btn btn-secondary btn-sm';
    editBtn.textContent = 'Edit';
    editBtn.addEventListener('click', () => openTypeModal(t));

    const toggleBtn = document.createElement('button');
    toggleBtn.className = 'btn btn-secondary btn-sm';
    toggleBtn.textContent = t.active ? 'Deactivate' : 'Activate';
    toggleBtn.addEventListener('click', () => toggleType(t));

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
        yesBtn.addEventListener('click', () => doDeleteType(t.id));
        const noBtn = document.createElement('button');
        noBtn.className = 'btn btn-secondary btn-sm';
        noBtn.textContent = 'No';
        noBtn.addEventListener('click', () => { confirmSpan.remove(); delBtn.style.display = ''; });
        confirmSpan.append(label, yesBtn, noBtn);
        actions.appendChild(confirmSpan);
    });

    actions.appendChild(editBtn);
    actions.appendChild(toggleBtn);
    actions.appendChild(delBtn);
    row.appendChild(actions);

    return row;
}

// ── Ally Modal ────────────────────────────────────────────────────────────────

function openAllyModal(ally) {
    editingAllyId = ally ? ally.id : null;
    document.getElementById('ally-modal-title').textContent = ally ? 'Edit Ally' : 'Add Ally';
    document.getElementById('ally-server').value = ally ? ally.server : '';
    document.getElementById('ally-tag').value = ally ? ally.tag : '';
    document.getElementById('ally-name').value = ally ? ally.name : '';
    document.getElementById('ally-contact').value = ally ? ally.contact : '';
    document.getElementById('ally-notes').value = ally ? ally.notes : '';
    document.getElementById('ally-modal-status').textContent = '';

    // Active checkbox — only show in edit mode
    const activeGroup = document.getElementById('ally-active-group');
    if (ally) {
        activeGroup.style.display = 'block';
        document.getElementById('ally-active').checked = ally.active;
    } else {
        activeGroup.style.display = 'none';
    }

    // Render agreement type checkboxes (active types only, or types already selected)
    const checkboxContainer = document.getElementById('ally-agreement-checkboxes');
    checkboxContainer.replaceChildren();
    const activeTypes = allTypes.filter(t => t.active || (ally && ally.agreement_type_ids.includes(t.id)));
    activeTypes.forEach(t => {
        const label = document.createElement('label');
        label.className = 'agreement-checkbox-label';
        const cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.value = t.id;
        cb.checked = ally ? ally.agreement_type_ids.includes(t.id) : false;
        const nameSpan = document.createElement('span');
        nameSpan.textContent = t.name;
        label.appendChild(cb);
        label.appendChild(nameSpan);
        checkboxContainer.appendChild(label);
    });

    document.getElementById('ally-modal').style.display = 'flex';
    document.getElementById('ally-server').focus();
}

function closeAllyModal() {
    document.getElementById('ally-modal').style.display = '';
}

async function saveAlly() {
    const statusEl = document.getElementById('ally-modal-status');
    statusEl.textContent = '';

    const server = document.getElementById('ally-server').value.trim();
    const tag = document.getElementById('ally-tag').value.trim();
    const name = document.getElementById('ally-name').value.trim();
    const contact = document.getElementById('ally-contact').value.trim();
    const notes = document.getElementById('ally-notes').value.trim();
    const activeEl = document.getElementById('ally-active');
    const active = editingAllyId ? activeEl.checked : true;

    const checkboxes = document.querySelectorAll('#ally-agreement-checkboxes input[type="checkbox"]:checked');
    const agreementTypeIDs = Array.from(checkboxes).map(cb => parseInt(cb.value));

    const body = { server, tag, name, contact, notes, active, agreement_type_ids: agreementTypeIDs };
    const url = editingAllyId ? '/api/allies/' + editingAllyId : '/api/allies';
    const method = editingAllyId ? 'PUT' : 'POST';

    const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });

    if (!res.ok) {
        const text = await res.text();
        statusEl.textContent = text || 'Save failed.';
        return;
    }

    closeAllyModal();
    await loadAgreementTypes();
    await loadAllies();
}

async function doDeleteAlly(id) {
    const res = await fetch('/api/allies/' + id, { method: 'DELETE' });
    if (!res.ok) return;
    await loadAllies();
}

// ── Type Modal ────────────────────────────────────────────────────────────────

function openTypeModal(t) {
    editingTypeId = t ? t.id : null;
    document.getElementById('type-modal-title').textContent = t ? 'Edit Agreement Type' : 'Add Agreement Type';
    document.getElementById('type-name').value = t ? t.name : '';
    document.getElementById('type-modal-status').textContent = '';
    document.getElementById('type-modal').style.display = 'flex';
    document.getElementById('type-name').focus();
}

function closeTypeModal() {
    document.getElementById('type-modal').style.display = '';
}

async function saveType() {
    const statusEl = document.getElementById('type-modal-status');
    statusEl.textContent = '';

    const name = document.getElementById('type-name').value.trim();
    if (!name) {
        statusEl.textContent = 'Name is required.';
        return;
    }

    const url = editingTypeId ? '/api/ally-agreement-types/' + editingTypeId : '/api/ally-agreement-types';
    const method = editingTypeId ? 'PUT' : 'POST';

    const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
    });

    if (!res.ok) {
        const text = await res.text();
        statusEl.textContent = text || 'Save failed.';
        return;
    }

    closeTypeModal();
    await loadAgreementTypes();
    renderTypesTab();
}

async function toggleType(t) {
    const res = await fetch('/api/ally-agreement-types/' + t.id, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ active: !t.active }),
    });
    if (!res.ok) return;
    await loadAgreementTypes();
    renderTypesTab();
}

async function doDeleteType(id) {
    const res = await fetch('/api/ally-agreement-types/' + id, { method: 'DELETE' });

    if (res.status === 409) {
        const data = await res.json();
        // Show inline force-delete prompt on the type row
        const typesList = document.getElementById('types-list');
        // Re-render with a notice — simplest approach: show a status in the tab
        const notice = document.createElement('p');
        notice.style.color = 'var(--danger-color, #e74c3c)';
        notice.style.marginTop = '8px';
        notice.textContent = data.error + ' Click "Force Delete" to remove it anyway.';
        const forceBtn = document.createElement('button');
        forceBtn.className = 'btn btn-danger btn-sm';
        forceBtn.textContent = 'Force Delete';
        forceBtn.style.marginLeft = '8px';
        forceBtn.addEventListener('click', async () => {
            const r2 = await fetch('/api/ally-agreement-types/' + id + '?force=true', { method: 'DELETE' });
            if (r2.ok) {
                notice.remove();
                await loadAgreementTypes();
                await loadAllies();
                renderTypesTab();
            }
        });
        notice.appendChild(forceBtn);
        typesList.insertBefore(notice, typesList.firstChild);
        return;
    }

    if (!res.ok) return;
    await loadAgreementTypes();
    await loadAllies();
    renderTypesTab();
}
