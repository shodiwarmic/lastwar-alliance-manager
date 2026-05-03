'use strict';

const API = '/api/officer-command';
const canManage = window.CAN_MANAGE === true;

let categories = [];   // OCCategory[]
let allMembers = [];   // {id, name, rank}[]

// ── drag state ──────────────────────────────────────────────────
let dragCatIdx = null;
let dragRespCatIdx = null;
let dragRespIdx = null;

// ── modal state ──────────────────────────────────────────────────
let respModalCatIdx = null;
let respModalRespIdx = null;
let assigneeModalCatIdx = null;
let assigneeModalRespIdx = null;

// ── helpers ──────────────────────────────────────────────────────
function showError(msg) {
    const el = document.getElementById('oc-error');
    el.textContent = msg;
    el.classList.remove('hidden');
    setTimeout(() => el.classList.add('hidden'), 6000);
}

function freqBadgeEl(freq) {
    const cls = { Daily: 'freq-daily', Weekly: 'freq-weekly', Seasonal: 'freq-seasonal' }[freq] || 'freq-weekly';
    const span = document.createElement('span');
    span.className = `freq-badge ${cls}`;
    span.textContent = freq;
    return span;
}

// ── data loading ─────────────────────────────────────────────────
async function loadData() {
    try {
        const [catRes, memRes] = await Promise.all([
            fetch(`${API}/data`),
            fetch('/api/members'),
        ]);
        if (!catRes.ok) throw new Error(await catRes.text());
        if (!memRes.ok) throw new Error(await memRes.text());
        categories = await catRes.json();
        const members = await memRes.json();
        allMembers = members.map(m => ({ id: m.id, name: m.name, rank: m.rank }));
        allMembers.sort((a, b) => b.rank.localeCompare(a.rank) || a.name.localeCompare(b.name));
        buildLeaderFilter();
        render();
    } catch (e) {
        showError('Failed to load data: ' + e.message);
    }
}

// ── filter bar ───────────────────────────────────────────────────
function buildLeaderFilter() {
    const seen = new Set();
    const sel = document.getElementById('filter-leader');

    const defaultOpt = document.createElement('option');
    defaultOpt.value = '';
    defaultOpt.textContent = 'All Leaders';
    const opts = [defaultOpt];

    categories.forEach(cat => {
        (cat.responsibilities || []).forEach(rp => {
            (rp.assignees || []).forEach(a => {
                if (!seen.has(a.member_id)) {
                    seen.add(a.member_id);
                    const opt = document.createElement('option');
                    opt.value = a.member_id;
                    opt.textContent = a.name;
                    opts.push(opt);
                }
            });
        });
    });

    sel.replaceChildren(...opts);
}

function getFilters() {
    return {
        leader: parseInt(document.getElementById('filter-leader').value) || 0,
        frequency: document.getElementById('filter-frequency').value,
    };
}

// ── render ───────────────────────────────────────────────────────
function render() {
    const container = document.getElementById('oc-categories');
    const { leader, frequency } = getFilters();

    if (!categories.length) {
        const p = document.createElement('p');
        p.style.color = 'var(--text-secondary)';
        if (canManage) {
            p.appendChild(document.createTextNode('No categories yet. Use '));
            const strong = document.createElement('strong');
            strong.textContent = '+ Add Category';
            p.appendChild(strong);
            p.appendChild(document.createTextNode(' to get started.'));
        } else {
            p.textContent = 'No responsibilities have been configured yet.';
        }
        container.replaceChildren(p);
        return;
    }

    const catEls = [];
    categories.forEach((cat, ci) => {
        const visibleResps = (cat.responsibilities || []).filter(rp => {
            if (frequency && rp.frequency !== frequency) return false;
            if (leader && !rp.assignees.some(a => a.member_id === leader)) return false;
            return true;
        });

        const catHidden = (leader || frequency) && visibleResps.length === 0 && (cat.responsibilities || []).length > 0;
        if (catHidden) return;

        const catDiv = document.createElement('div');
        catDiv.className = 'oc-category';
        catDiv.dataset.catIdx = ci;
        if (canManage) {
            catDiv.draggable = true;
            catDiv.dataset.dragCat = ci;
            catDiv.addEventListener('dragstart', e => {
                dragCatIdx = ci;
                e.dataTransfer.effectAllowed = 'move';
            });
            catDiv.addEventListener('dragover', e => {
                e.preventDefault();
                catDiv.classList.add('drag-over-cat');
            });
            catDiv.addEventListener('dragleave', () => catDiv.classList.remove('drag-over-cat'));
            catDiv.addEventListener('drop', e => {
                e.preventDefault();
                catDiv.classList.remove('drag-over-cat');
                const targetIdx = parseInt(catDiv.dataset.catIdx);
                if (dragCatIdx === null || dragCatIdx === targetIdx) return;
                const moved = categories.splice(dragCatIdx, 1)[0];
                categories.splice(targetIdx, 0, moved);
                dragCatIdx = null;
                render();
                saveCategoryOrder();
            });
            catDiv.addEventListener('dragend', () => {
                dragCatIdx = null;
                catDiv.classList.remove('drag-over-cat');
            });
        }

        // ── Category header ──────────────────────────────────────
        const header = document.createElement('div');
        header.className = 'oc-category-header';

        if (canManage) {
            const handle = document.createElement('span');
            handle.className = 'oc-drag-handle';
            handle.title = 'Drag to reorder category';
            handle.textContent = '⠿';
            header.appendChild(handle);
        }

        const nameSpan = document.createElement('span');
        nameSpan.className = 'oc-category-name';
        nameSpan.dataset.ci = ci;
        nameSpan.textContent = cat.name;
        header.appendChild(nameSpan);

        if (canManage) {
            const renameBtn = document.createElement('button');
            renameBtn.className = 'btn btn-sm btn-secondary';
            renameBtn.title = 'Rename';
            renameBtn.textContent = '✎';
            renameBtn.addEventListener('click', () => startRenameCategory(ci));
            header.appendChild(renameBtn);

            const delCatBtn = document.createElement('button');
            delCatBtn.className = 'btn btn-sm btn-danger';
            delCatBtn.title = 'Delete category';
            delCatBtn.textContent = '🗑';
            delCatBtn.addEventListener('click', () => {
                delCatBtn.style.display = 'none';
                const confirmSpan = document.createElement('span');
                confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
                const label = document.createElement('span');
                label.textContent = 'Sure?';
                label.style.fontSize = '0.85rem';
                const yesBtn = document.createElement('button');
                yesBtn.className = 'btn btn-danger btn-sm';
                yesBtn.textContent = 'Yes';
                yesBtn.addEventListener('click', () => deleteCategory(ci));
                const noBtn = document.createElement('button');
                noBtn.className = 'btn btn-secondary btn-sm';
                noBtn.textContent = 'No';
                noBtn.addEventListener('click', () => { confirmSpan.remove(); delCatBtn.style.display = ''; });
                confirmSpan.append(label, yesBtn, noBtn);
                delCatBtn.insertAdjacentElement('afterend', confirmSpan);
            });
            header.appendChild(delCatBtn);

            const addRespBtn = document.createElement('button');
            addRespBtn.className = 'btn btn-sm btn-primary';
            addRespBtn.textContent = '+ Add Responsibility';
            addRespBtn.addEventListener('click', () => openRespModal(ci, null));
            header.appendChild(addRespBtn);
        }

        catDiv.appendChild(header);

        // ── Responsibilities table or empty state ─────────────────
        if (visibleResps.length) {
            const tableScroll = document.createElement('div');
            tableScroll.className = 'table-scroll';

            const table = document.createElement('table');
            table.className = 'data-table oc-table';

            const thead = table.createTHead();
            const hr = thead.insertRow();
            ['Responsibility', 'Frequency', 'Assigned To', ...(canManage ? [''] : [])].forEach(h => {
                const th = document.createElement('th');
                th.textContent = h;
                hr.appendChild(th);
            });

            const tbody = table.createTBody();
            visibleResps.forEach((rp, ri) => {
                const tr = tbody.insertRow();
                tr.className = 'oc-row';
                if (canManage) {
                    tr.draggable = true;
                    tr.dataset.dragRespCi = ci;
                    tr.dataset.dragRespRi = ri;
                    tr.addEventListener('dragstart', e => {
                        dragRespCatIdx = ci;
                        dragRespIdx = ri;
                        e.dataTransfer.effectAllowed = 'move';
                        e.stopPropagation();
                    });
                    tr.addEventListener('dragover', e => {
                        e.preventDefault();
                        e.stopPropagation();
                        tr.classList.add('drag-over-row');
                    });
                    tr.addEventListener('dragleave', () => tr.classList.remove('drag-over-row'));
                    tr.addEventListener('drop', e => {
                        e.preventDefault();
                        e.stopPropagation();
                        tr.classList.remove('drag-over-row');
                        if (dragRespCatIdx === null || (dragRespCatIdx === ci && dragRespIdx === ri)) return;
                        if (dragRespCatIdx !== ci) { dragRespCatIdx = null; return; }
                        const resps = categories[dragRespCatIdx].responsibilities;
                        const moved = resps.splice(dragRespIdx, 1)[0];
                        resps.splice(ri, 0, moved);
                        dragRespCatIdx = null;
                        dragRespIdx = null;
                        render();
                        saveRespOrder(ci);
                    });
                    tr.addEventListener('dragend', () => {
                        dragRespCatIdx = null;
                        dragRespIdx = null;
                        tr.classList.remove('drag-over-row');
                    });
                }

                // Cell 1: drag handle + name + optional description
                const nameTd = tr.insertCell();
                if (canManage) {
                    const handle = document.createElement('span');
                    handle.className = 'oc-drag-handle';
                    handle.title = 'Drag to reorder';
                    handle.textContent = '⠿';
                    nameTd.appendChild(handle);
                }
                const nameDiv = document.createElement('div');
                nameDiv.className = 'oc-row-name';
                nameDiv.textContent = rp.name;
                nameTd.appendChild(nameDiv);
                if (rp.description) {
                    const descDiv = document.createElement('div');
                    descDiv.className = 'oc-row-desc';
                    descDiv.textContent = rp.description;
                    nameTd.appendChild(descDiv);
                }

                // Cell 2: frequency badge
                const freqTd = tr.insertCell();
                freqTd.style.whiteSpace = 'nowrap';
                freqTd.appendChild(freqBadgeEl(rp.frequency));

                // Cell 3: assignee chips + add button
                const assigneesTd = tr.insertCell();
                const assigneesDiv = document.createElement('div');
                assigneesDiv.className = 'oc-assignees';
                (rp.assignees || []).forEach(a => {
                    const chip = document.createElement('span');
                    chip.className = 'oc-chip';
                    chip.appendChild(document.createTextNode(`${a.name} ${a.rank}`));
                    if (canManage) {
                        const removeBtn = document.createElement('button');
                        removeBtn.className = 'oc-chip-remove';
                        removeBtn.title = 'Remove';
                        removeBtn.textContent = '×';
                        removeBtn.addEventListener('click', () => removeAssignee(ci, ri, a.member_id));
                        chip.appendChild(removeBtn);
                    }
                    assigneesDiv.appendChild(chip);
                });
                if (canManage) {
                    const addAssigneeBtn = document.createElement('button');
                    addAssigneeBtn.className = 'oc-add-assignee-btn';
                    addAssigneeBtn.textContent = '+ Add';
                    addAssigneeBtn.addEventListener('click', () => openAssigneeModal(ci, ri));
                    assigneesDiv.appendChild(addAssigneeBtn);
                }
                assigneesTd.appendChild(assigneesDiv);

                // Cell 4: row actions (canManage only)
                if (canManage) {
                    const actionsTd = tr.insertCell();
                    const actionsDiv = document.createElement('div');
                    actionsDiv.className = 'oc-row-actions';

                    const editBtn = document.createElement('button');
                    editBtn.className = 'btn btn-sm btn-secondary';
                    editBtn.textContent = 'Edit';
                    editBtn.addEventListener('click', () => openRespModal(ci, ri));
                    actionsDiv.appendChild(editBtn);

                    const delRespBtn = document.createElement('button');
                    delRespBtn.className = 'btn btn-sm btn-danger';
                    delRespBtn.textContent = 'Delete';
                    delRespBtn.addEventListener('click', () => {
                        delRespBtn.style.display = 'none';
                        const confirmSpan = document.createElement('span');
                        confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
                        const label = document.createElement('span');
                        label.textContent = 'Sure?';
                        label.style.fontSize = '0.85rem';
                        const yesBtn = document.createElement('button');
                        yesBtn.className = 'btn btn-danger btn-sm';
                        yesBtn.textContent = 'Yes';
                        yesBtn.addEventListener('click', () => deleteResponsibility(ci, ri));
                        const noBtn = document.createElement('button');
                        noBtn.className = 'btn btn-secondary btn-sm';
                        noBtn.textContent = 'No';
                        noBtn.addEventListener('click', () => { confirmSpan.remove(); delRespBtn.style.display = ''; });
                        confirmSpan.append(label, yesBtn, noBtn);
                        delRespBtn.insertAdjacentElement('afterend', confirmSpan);
                    });
                    actionsDiv.appendChild(delRespBtn);
                    actionsTd.appendChild(actionsDiv);
                }
            });

            tableScroll.appendChild(table);
            catDiv.appendChild(tableScroll);
        } else {
            const emptyDiv = document.createElement('div');
            emptyDiv.className = 'oc-empty';
            emptyDiv.textContent = `No responsibilities${frequency || leader ? ' match the current filters' : ''}.`;
            catDiv.appendChild(emptyDiv);
        }

        catEls.push(catDiv);
    });

    container.replaceChildren(...catEls);
}

// ── add category modal ───────────────────────────────────────────
function openAddCatModal() {
    document.getElementById('add-cat-name').value = '';
    document.getElementById('add-cat-error').style.display = 'none';
    const addCatModal = document.getElementById('add-cat-modal');
    addCatModal.style.display = 'flex';
    trapFocus(addCatModal);
}

function closeAddCatModal() {
    const addCatModal = document.getElementById('add-cat-modal');
    releaseFocus(addCatModal);
    addCatModal.style.display = '';
}

async function saveAddCatModal() {
    const name = document.getElementById('add-cat-name').value.trim();
    const errorEl = document.getElementById('add-cat-error');
    errorEl.style.display = 'none';

    if (!name) {
        errorEl.textContent = 'Name is required.';
        errorEl.style.display = '';
        document.getElementById('add-cat-name').focus();
        return;
    }
    try {
        const res = await fetch(`${API}/categories`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name }),
        });
        if (!res.ok) throw new Error(await res.text());
        const cat = await res.json();
        categories.push(cat);
        buildLeaderFilter();
        render();
        closeAddCatModal();
    } catch (e) {
        showError('Failed to add category: ' + e.message);
    }
}

// ── category actions ─────────────────────────────────────────────
function addCategory() {
    openAddCatModal();
}

function startRenameCategory(ci) {
    const cat = categories[ci];
    const nameEl = document.querySelector(`.oc-category-name[data-ci="${ci}"]`);
    if (!nameEl) return;

    const input = document.createElement('input');
    input.className = 'oc-category-name-input';
    input.value = cat.name;
    nameEl.replaceWith(input);
    input.focus();
    input.select();

    const commit = async () => {
        const newName = input.value.trim();
        if (!newName || newName === cat.name) { render(); return; }
        try {
            const res = await fetch(`${API}/categories/${cat.id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: newName }),
            });
            if (!res.ok) throw new Error(await res.text());
            categories[ci].name = newName;
        } catch (e) {
            showError('Failed to rename: ' + e.message);
        }
        render();
    };

    input.addEventListener('blur', commit);
    input.addEventListener('keydown', e => {
        if (e.key === 'Enter') input.blur();
        if (e.key === 'Escape') { render(); }
    });
}

async function deleteCategory(ci) {
    const cat = categories[ci];
    try {
        const res = await fetch(`${API}/categories/${cat.id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        categories.splice(ci, 1);
        buildLeaderFilter();
    } catch (e) {
        showError('Failed to delete category: ' + e.message);
    }
    render();
}

async function saveCategoryOrder() {
    const items = categories.map((c, i) => ({ id: c.id, display_order: i }));
    try {
        const res = await fetch(`${API}/categories/reorder`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(items),
        });
        if (!res.ok) showError('Failed to save category order: ' + await res.text());
    } catch (e) {
        showError('Failed to save category order: ' + e.message);
    }
}

// ── responsibility modal ─────────────────────────────────────────
function openRespModal(ci, ri) {
    respModalCatIdx = ci;
    respModalRespIdx = ri;

    const isEdit = ri !== null;
    document.getElementById('resp-modal-title').textContent = isEdit ? 'Edit Responsibility' : 'Add Responsibility';
    document.getElementById('resp-name-error').style.display = 'none';

    if (isEdit) {
        const rp = categories[ci].responsibilities[ri];
        document.getElementById('resp-name').value = rp.name;
        document.getElementById('resp-desc').value = rp.description;
        document.getElementById('resp-freq').value = rp.frequency;
    } else {
        document.getElementById('resp-name').value = '';
        document.getElementById('resp-desc').value = '';
        document.getElementById('resp-freq').value = 'Weekly';
    }

    const respModal = document.getElementById('resp-modal');
    respModal.style.display = 'flex';
    trapFocus(respModal);
}

function closeRespModal() {
    const respModal = document.getElementById('resp-modal');
    releaseFocus(respModal);
    respModal.style.display = '';
    document.getElementById('resp-name-error').style.display = 'none';
    respModalCatIdx = null;
    respModalRespIdx = null;
}

async function saveRespModal() {
    const name = document.getElementById('resp-name').value.trim();
    const description = document.getElementById('resp-desc').value.trim();
    const frequency = document.getElementById('resp-freq').value;
    const nameErrorEl = document.getElementById('resp-name-error');

    nameErrorEl.style.display = 'none';

    if (!name) {
        nameErrorEl.textContent = 'Name is required.';
        nameErrorEl.style.display = '';
        document.getElementById('resp-name').focus();
        return;
    }

    const ci = respModalCatIdx;
    const ri = respModalRespIdx;
    const isEdit = ri !== null;

    try {
        if (isEdit) {
            const rp = categories[ci].responsibilities[ri];
            const res = await fetch(`${API}/responsibilities/${rp.id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, description, frequency }),
            });
            if (!res.ok) throw new Error(await res.text());
            rp.name = name;
            rp.description = description;
            rp.frequency = frequency;
        } else {
            const cat = categories[ci];
            const res = await fetch(`${API}/responsibilities`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ category_id: cat.id, name, description, frequency }),
            });
            if (!res.ok) throw new Error(await res.text());
            const rp = await res.json();
            cat.responsibilities.push(rp);
        }
        buildLeaderFilter();
        render();
        closeRespModal();
    } catch (e) {
        showError('Failed to save responsibility: ' + e.message);
    }
}

async function deleteResponsibility(ci, ri) {
    const rp = categories[ci].responsibilities[ri];
    try {
        const res = await fetch(`${API}/responsibilities/${rp.id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        categories[ci].responsibilities.splice(ri, 1);
        buildLeaderFilter();
    } catch (e) {
        showError('Failed to delete responsibility: ' + e.message);
    }
    render();
}

async function saveRespOrder(ci) {
    const resps = categories[ci].responsibilities;
    const items = resps.map((rp, i) => ({ id: rp.id, display_order: i }));
    try {
        const res = await fetch(`${API}/responsibilities/reorder`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(items),
        });
        if (!res.ok) showError('Failed to save responsibility order: ' + await res.text());
    } catch (e) {
        showError('Failed to save responsibility order: ' + e.message);
    }
}

// ── assignee modal ───────────────────────────────────────────────
function openAssigneeModal(ci, ri) {
    assigneeModalCatIdx = ci;
    assigneeModalRespIdx = ri;
    const rp = categories[ci].responsibilities[ri];
    const assigned = new Set(rp.assignees.map(a => a.member_id));

    renderAssigneeList('', assigned);
    const assigneeModal = document.getElementById('assignee-modal');
    assigneeModal.style.display = 'flex';
    trapFocus(assigneeModal);
}

function closeAssigneeModal() {
    const assigneeModal = document.getElementById('assignee-modal');
    releaseFocus(assigneeModal);
    assigneeModal.style.display = '';
    assigneeModalCatIdx = null;
    assigneeModalRespIdx = null;
}

function renderAssigneeList(filter, assigned) {
    const container = document.getElementById('assignee-list');
    const lower = filter.toLowerCase();
    const members = allMembers.filter(m =>
        !assigned.has(m.id) && m.name.toLowerCase().includes(lower)
    );

    if (!members.length) {
        const p = document.createElement('p');
        p.style.cssText = 'color:var(--text-secondary);padding:0.5rem;font-size:0.9rem;';
        p.textContent = 'No members to add.';
        container.replaceChildren(p);
        return;
    }

    const rows = members.map(m => {
        const row = document.createElement('div');
        row.className = 'assignee-row';

        const nameSpan = document.createElement('span');
        nameSpan.appendChild(document.createTextNode(m.name + ' '));
        const rankChip = document.createElement('span');
        rankChip.className = `member-rank rank-${m.rank}`;
        rankChip.textContent = m.rank;
        nameSpan.appendChild(rankChip);

        const addBtn = document.createElement('button');
        addBtn.className = 'btn btn-sm btn-primary';
        addBtn.textContent = 'Add';
        addBtn.addEventListener('click', () => pickAssignee(m.id));

        row.appendChild(nameSpan);
        row.appendChild(addBtn);
        return row;
    });

    container.replaceChildren(...rows);
}

async function pickAssignee(memberID) {
    const ci = assigneeModalCatIdx;
    const ri = assigneeModalRespIdx;
    const rp = categories[ci].responsibilities[ri];

    try {
        const res = await fetch(`${API}/responsibilities/${rp.id}/assignees`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ member_id: memberID }),
        });
        if (!res.ok) throw new Error(await res.text());
        const assignee = await res.json();
        rp.assignees.push(assignee);
        closeAssigneeModal();
        buildLeaderFilter();
        render();
    } catch (e) {
        showError('Failed to add assignee: ' + e.message);
    }
}

async function removeAssignee(ci, ri, memberID) {
    const rp = categories[ci].responsibilities[ri];
    try {
        const res = await fetch(`${API}/responsibilities/${rp.id}/assignees/${memberID}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        rp.assignees = rp.assignees.filter(a => a.member_id !== memberID);
        buildLeaderFilter();
        render();
    } catch (e) {
        showError('Failed to remove assignee: ' + e.message);
    }
}

// ── init ─────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
    loadData();

    // Add category button
    const btnAddCat = document.getElementById('btn-add-category');
    if (btnAddCat) btnAddCat.addEventListener('click', addCategory);

    // Add category modal
    document.getElementById('add-cat-save').addEventListener('click', saveAddCatModal);
    document.getElementById('add-cat-cancel').addEventListener('click', closeAddCatModal);
    document.getElementById('add-cat-modal').addEventListener('click', e => {
        if (e.target.id === 'add-cat-modal') closeAddCatModal();
    });
    document.getElementById('add-cat-name').addEventListener('keydown', e => {
        if (e.key === 'Enter') saveAddCatModal();
        if (e.key === 'Escape') closeAddCatModal();
    });

    // Resp modal buttons
    document.getElementById('resp-modal-cancel').addEventListener('click', closeRespModal);
    document.getElementById('resp-modal-save').addEventListener('click', saveRespModal);

    // Assignee modal
    document.getElementById('assignee-modal-cancel').addEventListener('click', closeAssigneeModal);
    document.getElementById('assignee-search').addEventListener('input', e => {
        if (assigneeModalCatIdx === null) return;
        const rp = categories[assigneeModalCatIdx].responsibilities[assigneeModalRespIdx];
        const assigned = new Set(rp.assignees.map(a => a.member_id));
        renderAssigneeList(e.target.value, assigned);
    });

    // Close modals on backdrop click
    document.getElementById('resp-modal').addEventListener('click', e => {
        if (e.target.id === 'resp-modal') closeRespModal();
    });
    document.getElementById('assignee-modal').addEventListener('click', e => {
        if (e.target.id === 'assignee-modal') closeAssigneeModal();
    });

    // Filters
    document.getElementById('filter-leader').addEventListener('change', render);
    document.getElementById('filter-frequency').addEventListener('change', render);
});
