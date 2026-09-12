'use strict';

const API = '/api/officer-command';
const cfg = document.getElementById('page-config').dataset;
const canManage = cfg.canManage === 'true';   // permission — decides whether the toggle exists

// Intent, not permission: the page opens read-only for everyone, and a manager
// asks for the editing chrome. render() is a full rebuild, so flipping this and
// re-rendering is the whole of the mode switch.
let editing = false;
const isEditing = () => canManage && editing;

let categories = [];   // OCCategory[]
let allMembers = [];   // {id, name, rank}[]

// ── modal state ──────────────────────────────────────────────────
// Ids, not indexes: a card's position is a property of the current filter.
let respModalCatId = null;    // the category to create into
let respModalRespId = null;   // null for create, the id for edit

// Which cards have their Tasks open. Kept for the page load so filtering or
// saving does not snap every open card shut; deliberately not persisted.
const openTasks = new Set();
let activePicker = null;   // the single open inline member picker (if any)
let activeAddBtn = null;   // the "+ Add" button hidden while activePicker is open

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

// ── row identity ─────────────────────────────────────────────────
// Every handler closes over an id and resolves it at action time. Holding an
// index instead is what made the filtered page act on the wrong row (#102);
// keying on the id means the class of bug cannot come back.
function findCat(id) {
    const ci = categories.findIndex(c => c.id === id);
    return ci === -1 ? null : { ci, cat: categories[ci] };
}

function findResp(id) {
    for (let ci = 0; ci < categories.length; ci++) {
        const resps = categories[ci].responsibilities || [];
        const ri = resps.findIndex(rp => rp.id === id);
        if (ri !== -1) return { ci, ri, rp: resps[ri] };
    }
    return null;
}

// ── reorder ──────────────────────────────────────────────────────
// The global buildOrderButtons() moves DOM siblings; here the order lives in
// the data array and render() rebuilds the grid from it, so this builds the
// same markup over a data move. Disabled at the ends, and while a filter is
// active — moving past a hidden neighbour would be an invisible change.
function moveButtons({ atStart, atEnd, disabled, onUp, onDown }) {
    const wrap = document.createElement('span');
    wrap.className = 'order-btns';
    const mk = (icon, label, stuck, onMove) => {
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'order-btn';
        b.title = disabled ? 'Clear filters to reorder' : label;
        b.setAttribute('aria-label', label);
        b.disabled = disabled || stuck;
        b.appendChild(svgIcon(icon, 12));
        b.addEventListener('click', onMove);
        return b;
    };
    wrap.append(
        mk('chevron-up', 'Move up', atStart, onUp),
        mk('chevron-down', 'Move down', atEnd, onDown),
    );
    return wrap;
}

// Reorder saves are full-list PUTs, and buttons make rapid clicks the normal
// case. Each save waits for the previous one on that list and then sends the
// CURRENT in-memory order, so intermediate states coalesce and the last order
// wins. Fire-and-forget would let a stale PUT land last.
let catOrderChain = Promise.resolve();
const respOrderChains = new Map();

function queueCategoryOrder() {
    catOrderChain = catOrderChain.then(saveCategoryOrder, saveCategoryOrder);
}

function queueRespOrder(catId) {
    const prev = respOrderChains.get(catId) || Promise.resolve();
    const send = () => saveRespOrder(catId);
    respOrderChains.set(catId, prev.then(send, send));
}

function moveCategory(catId, delta) {
    const found = findCat(catId);
    if (!found) return;
    const to = found.ci + delta;
    if (to < 0 || to >= categories.length) return;
    [categories[found.ci], categories[to]] = [categories[to], categories[found.ci]];
    render();
    queueCategoryOrder();
}

function moveResponsibility(respId, delta) {
    const found = findResp(respId);
    if (!found) return;
    const resps = categories[found.ci].responsibilities;
    const to = found.ri + delta;
    if (to < 0 || to >= resps.length) return;
    [resps[found.ri], resps[to]] = [resps[to], resps[found.ri]];
    render();
    queueRespOrder(categories[found.ci].id);
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
    const prev = sel.value;   // preserve the active filter across the rebuild

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
    if (opts.some(o => o.value === prev)) sel.value = prev;
}

function getFilters() {
    const activeChip = document.querySelector('#filter-frequency-chips .filter-chip.active');
    return {
        leader: parseInt(document.getElementById('filter-leader').value) || 0,
        frequency: activeChip ? activeChip.dataset.freq : '',
    };
}

// ── render ───────────────────────────────────────────────────────
function render() {
    // Blur does not fire when a focused input is removed from the DOM, so tear the
    // open inline picker down explicitly before the grid is rebuilt.
    closeActivePicker();
    const container = document.getElementById('oc-categories');
    const { leader, frequency } = getFilters();
    const filtering = !!(leader || frequency);

    if (!categories.length) {
        const p = document.createElement('p');
        p.style.color = 'var(--color-text-mid)';
        if (isEditing()) {
            p.appendChild(document.createTextNode('No categories yet. Use '));
            const strong = document.createElement('strong');
            strong.textContent = '+ Add Category';
            p.appendChild(strong);
            p.appendChild(document.createTextNode(' to get started.'));
        } else if (canManage) {
            p.appendChild(document.createTextNode('No responsibilities have been configured yet. Use '));
            const strong = document.createElement('strong');
            strong.textContent = 'Edit Responsibilities';
            p.appendChild(strong);
            p.appendChild(document.createTextNode(' to add one.'));
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

        const catHidden = filtering && visibleResps.length === 0 && (cat.responsibilities || []).length > 0;
        if (catHidden) return;

        const catDiv = document.createElement('div');
        catDiv.className = 'oc-category';

        // ── Category header ──────────────────────────────────────
        const header = document.createElement('div');
        header.className = 'oc-category-header';

        if (isEditing()) {
            header.appendChild(moveButtons({
                atStart: ci === 0,
                atEnd: ci === categories.length - 1,
                disabled: filtering,
                onUp: () => moveCategory(cat.id, -1),
                onDown: () => moveCategory(cat.id, 1),
            }));
        }

        const nameSpan = document.createElement('span');
        nameSpan.className = 'oc-category-name';
        nameSpan.dataset.catId = cat.id;
        nameSpan.textContent = cat.name;
        header.appendChild(nameSpan);

        if (isEditing()) {
            header.appendChild(rowActionBtn('btn btn-sm btn-secondary', 'pencil', 'Rename',
                () => startRenameCategory(cat.id)));
            header.appendChild(rowActionBtn('btn btn-sm btn-danger', 'trash', 'Delete', async () => {
                if (!await showConfirm('Delete this category?', 'Delete')) return;
                deleteCategory(cat.id);
            }));
        }

        catDiv.appendChild(header);

        // ── Responsibility cards or empty state ──────────────────
        if (visibleResps.length) {
            const grid = document.createElement('div');
            grid.className = 'oc-grid';
            visibleResps.forEach(rp => grid.appendChild(buildRespCard(cat, rp, filtering)));
            catDiv.appendChild(grid);

            if (isEditing()) {
                const footer = document.createElement('div');
                footer.className = 'oc-category-footer';
                const addRespBtn = document.createElement('button');
                addRespBtn.className = 'btn btn-sm btn-primary';
                // A page-level action, not a row action: the label never collapses.
                addRespBtn.append(svgIcon('plus'), document.createTextNode(' Add Responsibility'));
                addRespBtn.addEventListener('click', () => openRespModal(cat.id, null));
                footer.appendChild(addRespBtn);
                catDiv.appendChild(footer);
            }
        } else {
            const emptyDiv = document.createElement('div');
            emptyDiv.className = 'oc-empty';
            if (isEditing() && !filtering) {
                emptyDiv.appendChild(document.createTextNode('No responsibilities yet — '));
                const addLink = document.createElement('button');
                addLink.className = 'oc-empty-add-link';
                addLink.textContent = 'Add one';
                addLink.addEventListener('click', () => openRespModal(cat.id, null));
                emptyDiv.appendChild(addLink);
            } else {
                emptyDiv.textContent = `No responsibilities${filtering ? ' match the current filters' : ''}.`;
            }
            catDiv.appendChild(emptyDiv);
        }

        catEls.push(catDiv);
    });

    container.replaceChildren(...catEls);
}

function buildRespCard(cat, rp, filtering) {
    const card = document.createElement('article');
    card.className = 'oc-card';

    const top = document.createElement('div');
    top.className = 'oc-card-top';
    const nameDiv = document.createElement('div');
    nameDiv.className = 'oc-card-name';
    nameDiv.textContent = rp.name;
    top.append(nameDiv, freqBadgeEl(rp.frequency));
    card.appendChild(top);

    if (rp.description) {
        const descDiv = document.createElement('div');
        descDiv.className = 'oc-card-desc';
        descDiv.textContent = rp.description;
        card.appendChild(descDiv);
    }

    if ((rp.tasks || []).length) {
        card.appendChild(buildTasksBlock(rp));
    }

    const people = document.createElement('div');
    people.className = 'oc-assignees oc-card-people';
    (rp.assignees || []).forEach(a => people.appendChild(buildAssigneeChip(rp.id, a)));
    if (isEditing()) {
        const addAssigneeBtn = document.createElement('button');
        addAssigneeBtn.className = 'oc-add-assignee-btn';
        addAssigneeBtn.append(svgIcon('user-plus'), document.createTextNode(' Add'));
        addAssigneeBtn.addEventListener('click', () => openInlinePicker(rp.id, addAssigneeBtn));
        people.appendChild(addAssigneeBtn);
    }
    card.appendChild(people);

    if (isEditing()) {
        const resps = cat.responsibilities || [];
        const ri = resps.indexOf(rp);
        const actions = document.createElement('div');
        actions.className = 'oc-card-actions';
        actions.appendChild(moveButtons({
            atStart: ri === 0,
            atEnd: ri === resps.length - 1,
            disabled: filtering,
            onUp: () => moveResponsibility(rp.id, -1),
            onDown: () => moveResponsibility(rp.id, 1),
        }));
        actions.appendChild(rowActionBtn('btn btn-sm btn-secondary', 'pencil', 'Edit',
            () => openRespModal(cat.id, rp.id)));
        actions.appendChild(rowActionBtn('btn btn-sm btn-danger', 'trash', 'Delete', async () => {
            if (!await showConfirm('Delete this responsibility?', 'Delete')) return;
            deleteResponsibility(rp.id);
        }));
        card.appendChild(actions);
    }

    return card;
}

// Native <details> gives the toggle, the keyboard handling and the disclosure
// semantics in the accessibility tree. There is no literal aria-expanded
// attribute on a <summary>, so anything testing this keys on details.open.
function buildTasksBlock(rp) {
    const details = document.createElement('details');
    details.className = 'oc-tasks';
    details.open = openTasks.has(rp.id);

    const summary = document.createElement('summary');
    summary.append(svgIcon('chevron-right', 12), document.createTextNode('Tasks'));
    const count = document.createElement('span');
    count.className = 'oc-task-count';
    count.textContent = rp.tasks.length;
    summary.appendChild(count);
    details.appendChild(summary);

    const list = document.createElement('ol');
    list.className = 'oc-task-list';
    rp.tasks.forEach(t => {
        const li = document.createElement('li');
        li.textContent = t;
        list.appendChild(li);
    });
    details.appendChild(list);

    details.addEventListener('toggle', () => {
        if (details.open) openTasks.add(rp.id);
        else openTasks.delete(rp.id);
    });
    return details;
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

function startRenameCategory(catId) {
    const found = findCat(catId);
    if (!found) return;
    const cat = found.cat;
    const nameEl = document.querySelector(`.oc-category-name[data-cat-id="${catId}"]`);
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
            cat.name = newName;
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

async function deleteCategory(catId) {
    const found = findCat(catId);
    if (!found) return;
    try {
        const res = await fetch(`${API}/categories/${catId}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        categories.splice(found.ci, 1);
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
function openRespModal(catId, respId) {
    respModalCatId = catId;
    respModalRespId = respId;

    const isEdit = respId !== null;
    document.getElementById('resp-modal-title').textContent = isEdit ? 'Edit Responsibility' : 'Add Responsibility';
    document.getElementById('resp-name-error').style.display = 'none';
    document.getElementById('resp-tasks-error').style.display = 'none';

    if (isEdit) {
        const found = findResp(respId);
        if (!found) return;
        const rp = found.rp;
        document.getElementById('resp-name').value = rp.name;
        document.getElementById('resp-desc').value = rp.description;
        document.getElementById('resp-freq').value = rp.frequency;
        document.getElementById('resp-tasks').value = (rp.tasks || []).join('\n');
    } else {
        document.getElementById('resp-name').value = '';
        document.getElementById('resp-desc').value = '';
        document.getElementById('resp-freq').value = 'Weekly';
        document.getElementById('resp-tasks').value = '';
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
    document.getElementById('resp-tasks-error').style.display = 'none';
    respModalCatId = null;
    respModalRespId = null;
}

async function saveRespModal() {
    const name = document.getElementById('resp-name').value.trim();
    const description = document.getElementById('resp-desc').value.trim();
    const frequency = document.getElementById('resp-freq').value;
    // One per line; blank lines are how a user spaces the box out, and the
    // server drops them too.
    const tasks = document.getElementById('resp-tasks').value
        .split('\n').map(t => t.trim()).filter(Boolean);
    const nameErrorEl = document.getElementById('resp-name-error');
    const tasksErrorEl = document.getElementById('resp-tasks-error');

    nameErrorEl.style.display = 'none';
    tasksErrorEl.style.display = 'none';

    if (!name) {
        nameErrorEl.textContent = 'Name is required.';
        nameErrorEl.style.display = '';
        document.getElementById('resp-name').focus();
        return;
    }

    const isEdit = respModalRespId !== null;

    try {
        if (isEdit) {
            const found = findResp(respModalRespId);
            if (!found) throw new Error('That responsibility no longer exists.');
            const rp = found.rp;
            const res = await fetch(`${API}/responsibilities/${rp.id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, description, frequency, tasks }),
            });
            if (!res.ok) throw new Error((await res.text()).trim(), { cause: res.status });
            rp.name = name;
            rp.description = description;
            rp.frequency = frequency;
            rp.tasks = tasks;
        } else {
            const found = findCat(respModalCatId);
            if (!found) throw new Error('That category no longer exists.');
            const cat = found.cat;
            const res = await fetch(`${API}/responsibilities`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ category_id: cat.id, name, description, frequency, tasks }),
            });
            if (!res.ok) throw new Error((await res.text()).trim(), { cause: res.status });
            const rp = await res.json();
            cat.responsibilities.push(rp);
        }
        buildLeaderFilter();
        render();
        closeRespModal();
    } catch (e) {
        if (e.cause === 400) {
            tasksErrorEl.textContent = e.message;
            tasksErrorEl.style.display = '';
            document.getElementById('resp-tasks').focus();
            return;
        }
        showError('Failed to save responsibility: ' + e.message);
    }
}

async function deleteResponsibility(respId) {
    const found = findResp(respId);
    if (!found) return;
    try {
        const res = await fetch(`${API}/responsibilities/${respId}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        categories[found.ci].responsibilities.splice(found.ri, 1);
        buildLeaderFilter();
    } catch (e) {
        showError('Failed to delete responsibility: ' + e.message);
    }
    render();
}

async function saveRespOrder(catId) {
    const found = findCat(catId);
    if (!found) return;
    const items = (found.cat.responsibilities || []).map((rp, i) => ({ id: rp.id, display_order: i }));
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

// ── inline member picker ─────────────────────────────────────────
function buildAssigneeChip(respId, a) {
    const chip = noTranslate(document.createElement('span'));
    chip.className = 'oc-chip';
    chip.appendChild(document.createTextNode(a.name + ' '));
    const rankBadge = document.createElement('span');
    rankBadge.className = `member-rank rank-${a.rank}`;
    rankBadge.textContent = a.rank;
    chip.appendChild(rankBadge);
    if (isEditing()) {
        const removeBtn = document.createElement('button');
        removeBtn.className = 'oc-chip-remove';
        removeBtn.title = 'Remove';
        removeBtn.textContent = '×';
        removeBtn.addEventListener('click', () => removeAssignee(respId, a.member_id));
        chip.appendChild(removeBtn);
    }
    return chip;
}

// Only one inline picker is open at a time (singleton). Tearing the current one
// down restores its "+ Add" button. Called on every new open and at render() top.
function closeActivePicker() {
    if (activePicker) {
        activePicker.destroy();
        activePicker = null;
    }
    if (activeAddBtn) {
        activeAddBtn.style.display = '';
        activeAddBtn = null;
    }
}

function openInlinePicker(respId, addBtn) {
    closeActivePicker();
    const found = findResp(respId);
    if (!found) return;
    const rp = found.rp;
    addBtn.style.display = 'none';
    const picker = createMemberPicker({
        placeholder: 'Add member…',
        keepOpenOnPick: true,   // rapid multi-add, like the old modal's Add/Add/Add
        maxResults: 500,
        getCandidates: () => allMembers,
        isExcluded: (m) => (rp.assignees || []).some(a => a.member_id === m.id),
        onPick: (m) => pickAssignee(respId, m),
    });
    picker.el.style.minWidth = '160px';
    activePicker = picker;
    activeAddBtn = addBtn;
    addBtn.parentElement.insertBefore(picker.el, addBtn);
    picker.input.focus();
}

// Optimistic: push + chip BEFORE the POST (the candidate already carries
// {id,name,rank}), so the member drops out of the still-open picker immediately
// and a fast second tap can't double-add. Roll back on failure.
async function pickAssignee(respId, m) {
    const found = findResp(respId);
    if (!found) return;
    const rp = found.rp;
    if (!rp.assignees) rp.assignees = [];
    if (rp.assignees.some(a => a.member_id === m.id)) return;   // dedup

    const assignee = { member_id: m.id, name: m.name, rank: m.rank };
    rp.assignees.push(assignee);
    buildLeaderFilter();

    const chip = buildAssigneeChip(rp.id, assignee);
    if (activePicker && activePicker.el.parentElement) {
        activePicker.el.parentElement.insertBefore(chip, activePicker.el);
    }

    try {
        const res = await fetch(`${API}/responsibilities/${rp.id}/assignees`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ member_id: m.id }),
        });
        if (!res.ok) throw new Error(await res.text());
    } catch (e) {
        rp.assignees = rp.assignees.filter(a => a.member_id !== m.id);
        buildLeaderFilter();
        chip.remove();
        showError('Failed to add assignee: ' + e.message);
    }
}

async function removeAssignee(respId, memberID) {
    const found = findResp(respId);
    if (!found) return;
    const rp = found.rp;
    try {
        const res = await fetch(`${API}/responsibilities/${respId}/assignees/${memberID}`, { method: 'DELETE' });
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

    // Add category button — edit chrome, so it is hidden until edit mode is on.
    const btnAddCat = document.getElementById('btn-add-category');
    if (btnAddCat) btnAddCat.addEventListener('click', addCategory);

    // Edit-mode toggle. Only rendered for managers; fill and aria-pressed carry
    // the state, and the chrome appearing is the cue, so the label never changes.
    const btnEditMode = document.getElementById('btn-edit-mode');
    if (btnEditMode) {
        btnEditMode.addEventListener('click', () => {
            editing = !editing;
            btnEditMode.classList.toggle('active', editing);
            btnEditMode.setAttribute('aria-pressed', editing ? 'true' : 'false');
            // The global .hidden class, not the `hidden` attribute: .btn sets
            // display:inline-flex, which beats the UA stylesheet's [hidden] rule.
            if (btnAddCat) btnAddCat.classList.toggle('hidden', !editing);
            render();
        });
    }

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

    // Close resp modal on backdrop click
    document.getElementById('resp-modal').addEventListener('click', e => {
        if (e.target.id === 'resp-modal') closeRespModal();
    });

    // Filters
    document.getElementById('filter-leader').addEventListener('change', render);

    // Frequency filter chips
    document.querySelectorAll('#filter-frequency-chips .filter-chip').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('#filter-frequency-chips .filter-chip').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            render();
        });
    });
});
