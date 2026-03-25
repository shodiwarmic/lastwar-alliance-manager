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
let respModalCatIdx = null;   // category index being edited/added to
let respModalRespIdx = null;  // null = new, number = editing existing
let assigneeModalCatIdx = null;
let assigneeModalRespIdx = null;

// ── helpers ──────────────────────────────────────────────────────
function escapeHtml(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function showError(msg) {
    const el = document.getElementById('oc-error');
    el.textContent = msg;
    el.classList.remove('hidden');
    setTimeout(() => el.classList.add('hidden'), 6000);
}

function freqBadge(freq) {
    const cls = { Daily: 'freq-daily', Weekly: 'freq-weekly', Seasonal: 'freq-seasonal' }[freq] || 'freq-weekly';
    return `<span class="freq-badge ${cls}">${escapeHtml(freq)}</span>`;
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
    const opts = ['<option value="">All Leaders</option>'];
    categories.forEach(cat => {
        (cat.responsibilities || []).forEach(rp => {
            (rp.assignees || []).forEach(a => {
                if (!seen.has(a.member_id)) {
                    seen.add(a.member_id);
                    opts.push(`<option value="${a.member_id}">${escapeHtml(a.name)}</option>`);
                }
            });
        });
    });
    document.getElementById('filter-leader').innerHTML = opts.join('');
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
        container.innerHTML = canManage
            ? '<p style="color:var(--text-secondary)">No categories yet. Use <strong>+ Add Category</strong> to get started.</p>'
            : '<p style="color:var(--text-secondary)">No responsibilities have been configured yet.</p>';
        return;
    }

    const html = categories.map((cat, ci) => {
        const visibleResps = (cat.responsibilities || []).filter(rp => {
            if (frequency && rp.frequency !== frequency) return false;
            if (leader && !rp.assignees.some(a => a.member_id === leader)) return false;
            return true;
        });

        // Category is hidden if all its responsibilities are filtered out (and leader/freq filter is active)
        const catHidden = (leader || frequency) && visibleResps.length === 0 && (cat.responsibilities || []).length > 0;
        if (catHidden) return '';

        const rows = visibleResps.map((rp, ri) => {
            const assigneeChips = (rp.assignees || []).map(a =>
                `<span class="oc-chip">
                    ${escapeHtml(a.name)} ${escapeHtml(a.rank)}
                    ${canManage ? `<button class="oc-chip-remove" data-ci="${ci}" data-ri="${ri}" data-mid="${a.member_id}" title="Remove">×</button>` : ''}
                </span>`
            ).join('');

            const addAssigneeBtn = canManage
                ? `<button class="oc-add-assignee-btn" data-ci="${ci}" data-ri="${ri}">+ Add</button>`
                : '';

            const rowActions = canManage ? `
                <div class="oc-row-actions">
                    <button class="btn btn-sm btn-secondary" data-edit-resp data-ci="${ci}" data-ri="${ri}">Edit</button>
                    <button class="btn btn-sm btn-danger" data-del-resp data-ci="${ci}" data-ri="${ri}">Delete</button>
                </div>` : '';

            const dragAttr = canManage ? `draggable="true" data-drag-resp-ci="${ci}" data-drag-resp-ri="${ri}"` : '';

            return `<tr class="oc-row" ${dragAttr}>
                <td>
                    ${canManage ? `<span class="oc-drag-handle" title="Drag to reorder">⠿</span>` : ''}
                    <div class="oc-row-name">${escapeHtml(rp.name)}</div>
                    ${rp.description ? `<div class="oc-row-desc">${escapeHtml(rp.description)}</div>` : ''}
                </td>
                <td style="white-space:nowrap;">${freqBadge(rp.frequency)}</td>
                <td>
                    <div class="oc-assignees">
                        ${assigneeChips}
                        ${addAssigneeBtn}
                    </div>
                </td>
                <td>${rowActions}</td>
            </tr>`;
        }).join('');

        const catDragAttr = canManage ? `draggable="true" data-drag-cat="${ci}"` : '';

        return `<div class="oc-category" ${catDragAttr} data-cat-idx="${ci}">
            <div class="oc-category-header">
                ${canManage ? `<span class="oc-drag-handle" title="Drag to reorder category">⠿</span>` : ''}
                <span class="oc-category-name" data-ci="${ci}">${escapeHtml(cat.name)}</span>
                ${canManage ? `
                <button class="btn btn-sm btn-secondary" data-rename-cat="${ci}" title="Rename">✎</button>
                <button class="btn btn-sm btn-danger" data-del-cat="${ci}" title="Delete category">🗑</button>
                <button class="btn btn-sm btn-primary" data-add-resp="${ci}">+ Add Responsibility</button>
                ` : ''}
            </div>
            ${visibleResps.length ? `
            <table class="oc-table">
                <thead><tr>
                    <th>Responsibility</th>
                    <th>Frequency</th>
                    <th>Assigned To</th>
                    ${canManage ? '<th></th>' : ''}
                </tr></thead>
                <tbody>${rows}</tbody>
            </table>` : `<div class="oc-empty">No responsibilities${frequency || leader ? ' match the current filters' : ''}.</div>`}
        </div>`;
    }).join('');

    container.innerHTML = html;
    wireEvents(container);
}

function wireEvents(container) {
    // Category rename
    container.querySelectorAll('[data-rename-cat]').forEach(btn => {
        btn.addEventListener('click', () => {
            const ci = parseInt(btn.dataset.renameCat);
            startRenameCategory(ci);
        });
    });

    // Category delete
    container.querySelectorAll('[data-del-cat]').forEach(btn => {
        btn.addEventListener('click', () => {
            const ci = parseInt(btn.dataset.delCat);
            deleteCategory(ci);
        });
    });

    // Add responsibility
    container.querySelectorAll('[data-add-resp]').forEach(btn => {
        btn.addEventListener('click', () => {
            openRespModal(parseInt(btn.dataset.addResp), null);
        });
    });

    // Edit responsibility
    container.querySelectorAll('[data-edit-resp]').forEach(btn => {
        btn.addEventListener('click', () => {
            openRespModal(parseInt(btn.dataset.ci), parseInt(btn.dataset.ri));
        });
    });

    // Delete responsibility
    container.querySelectorAll('[data-del-resp]').forEach(btn => {
        btn.addEventListener('click', () => {
            deleteResponsibility(parseInt(btn.dataset.ci), parseInt(btn.dataset.ri));
        });
    });

    // Remove assignee
    container.querySelectorAll('.oc-chip-remove').forEach(btn => {
        btn.addEventListener('click', () => {
            removeAssignee(parseInt(btn.dataset.ci), parseInt(btn.dataset.ri), parseInt(btn.dataset.mid));
        });
    });

    // Add assignee
    container.querySelectorAll('.oc-add-assignee-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            openAssigneeModal(parseInt(btn.dataset.ci), parseInt(btn.dataset.ri));
        });
    });

    if (!canManage) return;

    // Drag-and-drop: categories
    container.querySelectorAll('[data-drag-cat]').forEach(el => {
        el.addEventListener('dragstart', e => {
            dragCatIdx = parseInt(el.dataset.dragCat);
            e.dataTransfer.effectAllowed = 'move';
        });
        el.addEventListener('dragover', e => {
            e.preventDefault();
            el.classList.add('drag-over-cat');
        });
        el.addEventListener('dragleave', () => el.classList.remove('drag-over-cat'));
        el.addEventListener('drop', e => {
            e.preventDefault();
            el.classList.remove('drag-over-cat');
            const targetIdx = parseInt(el.dataset.catIdx);
            if (dragCatIdx === null || dragCatIdx === targetIdx) return;
            const moved = categories.splice(dragCatIdx, 1)[0];
            categories.splice(targetIdx, 0, moved);
            dragCatIdx = null;
            render();
            saveCategoryOrder();
        });
        el.addEventListener('dragend', () => {
            dragCatIdx = null;
            el.classList.remove('drag-over-cat');
        });
    });

    // Drag-and-drop: responsibilities
    container.querySelectorAll('[data-drag-resp-ci]').forEach(row => {
        row.addEventListener('dragstart', e => {
            dragRespCatIdx = parseInt(row.dataset.dragRespCi);
            dragRespIdx = parseInt(row.dataset.dragRespRi);
            e.dataTransfer.effectAllowed = 'move';
            e.stopPropagation();
        });
        row.addEventListener('dragover', e => {
            e.preventDefault();
            e.stopPropagation();
            row.classList.add('drag-over-row');
        });
        row.addEventListener('dragleave', () => row.classList.remove('drag-over-row'));
        row.addEventListener('drop', e => {
            e.preventDefault();
            e.stopPropagation();
            row.classList.remove('drag-over-row');
            const targetCi = parseInt(row.dataset.dragRespCi);
            const targetRi = parseInt(row.dataset.dragRespRi);
            if (dragRespCatIdx === null || (dragRespCatIdx === targetCi && dragRespIdx === targetRi)) return;
            if (dragRespCatIdx !== targetCi) {
                dragRespCatIdx = null;
                return; // cross-category reorder not supported
            }
            const resps = categories[dragRespCatIdx].responsibilities;
            const moved = resps.splice(dragRespIdx, 1)[0];
            resps.splice(targetRi, 0, moved);
            dragRespCatIdx = null;
            dragRespIdx = null;
            render();
            saveRespOrder(targetCi);
        });
        row.addEventListener('dragend', () => {
            dragRespCatIdx = null;
            dragRespIdx = null;
            row.classList.remove('drag-over-row');
        });
    });
}

// ── category actions ─────────────────────────────────────────────
async function addCategory() {
    const name = prompt('Category name:');
    if (!name || !name.trim()) return;
    try {
        const res = await fetch(`${API}/categories`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: name.trim() }),
        });
        if (!res.ok) throw new Error(await res.text());
        const cat = await res.json();
        categories.push(cat);
        buildLeaderFilter();
        render();
    } catch (e) {
        showError('Failed to add category: ' + e.message);
    }
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
    if (!confirm(`Delete category "${cat.name}" and all its responsibilities?`)) return;
    try {
        const res = await fetch(`${API}/categories/${cat.id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        categories.splice(ci, 1);
        buildLeaderFilter();
        render();
    } catch (e) {
        showError('Failed to delete category: ' + e.message);
    }
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

    document.getElementById('resp-modal').style.display = 'flex';
    document.getElementById('resp-name').focus();
}

function closeRespModal() {
    document.getElementById('resp-modal').style.display = 'none';
    respModalCatIdx = null;
    respModalRespIdx = null;
}

async function saveRespModal() {
    const name = document.getElementById('resp-name').value.trim();
    const description = document.getElementById('resp-desc').value.trim();
    const frequency = document.getElementById('resp-freq').value;

    if (!name) { alert('Name is required.'); return; }

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
    if (!confirm(`Delete responsibility "${rp.name}"?`)) return;
    try {
        const res = await fetch(`${API}/responsibilities/${rp.id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        categories[ci].responsibilities.splice(ri, 1);
        buildLeaderFilter();
        render();
    } catch (e) {
        showError('Failed to delete responsibility: ' + e.message);
    }
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
    document.getElementById('assignee-modal').style.display = 'flex';
    document.getElementById('assignee-search').value = '';
    document.getElementById('assignee-search').focus();
}

function closeAssigneeModal() {
    document.getElementById('assignee-modal').style.display = 'none';
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
        container.innerHTML = '<p style="color:var(--text-secondary);padding:0.5rem;font-size:0.9rem;">No members to add.</p>';
        return;
    }
    container.innerHTML = members.map(m =>
        `<div class="assignee-row">
            <span>${escapeHtml(m.name)} <small style="color:var(--text-secondary)">${escapeHtml(m.rank)}</small></span>
            <button class="btn btn-sm btn-primary" data-pick-member="${m.id}">Add</button>
        </div>`
    ).join('');

    container.querySelectorAll('[data-pick-member]').forEach(btn => {
        btn.addEventListener('click', () => pickAssignee(parseInt(btn.dataset.pickMember)));
    });
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
