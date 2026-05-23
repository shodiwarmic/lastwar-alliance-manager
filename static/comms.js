// comms.js — Alliance Communications page

const cfg = document.getElementById('page-config').dataset;
const CAN_MANAGE = cfg.canManage === 'true';

// Cache: keyed by type ('mail', 'announcement') or 'resources'
const cache = {};

// Track which tabs have been loaded
const loaded = {};

// ── Tab switching ────────────────────────────────────────────────────────────

const tabBtns = document.querySelectorAll('.tab-btn');
const tabContents = document.querySelectorAll('.tab-content');

function switchTab(tabName) {
    tabBtns.forEach(b => b.classList.toggle('active', b.dataset.tab === tabName));
    tabContents.forEach(c => {
        c.style.display = c.id === 'tab-' + tabName ? 'block' : 'none';
    });
    if (!loaded[tabName]) {
        loaded[tabName] = true;
        if (tabName === 'mail') loadTemplates('mail');
        else if (tabName === 'announcement') loadTemplates('announcement');
        else if (tabName === 'resources') loadResources();
    }
}

tabBtns.forEach(btn => {
    btn.addEventListener('click', () => switchTab(btn.dataset.tab));
});

// ── Initialise first tab ─────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
    switchTab('mail');

    document.getElementById('new-mail-btn')?.addEventListener('click', () => openTemplateModal('mail'));
    document.getElementById('new-announcement-btn')?.addEventListener('click', () => openTemplateModal('announcement'));
    document.getElementById('new-resource-btn')?.addEventListener('click', openResourceModal);

    document.getElementById('mail-search').addEventListener('input', () => renderTemplateList('mail'));
    document.getElementById('announcement-search').addEventListener('input', () => renderTemplateList('announcement'));
    document.getElementById('resource-search').addEventListener('input', renderResourceList);

    document.getElementById('template-save-btn').addEventListener('click', saveTemplate);
    document.getElementById('template-cancel-btn').addEventListener('click', () => {
        document.getElementById('modal-template').style.display = '';
    });
    document.getElementById('resource-save-btn').addEventListener('click', saveResource);
    document.getElementById('resource-cancel-btn').addEventListener('click', () => {
        document.getElementById('modal-resource').style.display = '';
    });

    // Live variable preview in the content textarea
    document.getElementById('template-content-input').addEventListener('input', updateVarsPreview);
    document.getElementById('template-required-vars-input').addEventListener('input', updateVarsPreview);
});

// ── Template loading & rendering ─────────────────────────────────────────────

async function loadTemplates(type) {
    const listEl = document.getElementById(type + '-list');
    listEl.textContent = 'Loading…';
    try {
        const res = await fetch('/api/comms/templates?type=' + type);
        if (!res.ok) throw new Error('fetch failed');
        const data = await res.json();
        cache[type] = data.items || [];
        populateCategorySuggestions();
        renderTemplateList(type);
    } catch {
        listEl.textContent = 'Failed to load. Please refresh.';
    }
}

function renderTemplateList(type) {
    const listEl = document.getElementById(type + '-list');
    const query = document.getElementById(type + '-search').value.trim().toLowerCase();
    const items = cache[type] || [];

    if (query) {
        const filtered = items.filter(t =>
            t.title.toLowerCase().includes(query) ||
            t.content.toLowerCase().includes(query)
        );
        listEl.replaceChildren();
        const count = document.createElement('p');
        count.className = 'comms-results-count';
        count.textContent = filtered.length + ' result' + (filtered.length !== 1 ? 's' : '');
        listEl.appendChild(count);
        if (filtered.length === 0) {
            listEl.appendChild(emptyState('No templates match your search.'));
        } else {
            filtered.forEach(t => listEl.appendChild(renderTemplateCard(t)));
        }
        return;
    }

    listEl.replaceChildren();
    if (items.length === 0) {
        listEl.appendChild(emptyState('No templates yet.' + (CAN_MANAGE ? ' Click "+ New" to create one.' : '')));
        return;
    }

    const groups = groupByCategory(items);
    Object.keys(groups).sort().forEach(cat => {
        listEl.appendChild(renderCategorySection(cat, groups[cat], renderTemplateCard, type));
    });
}

function groupByCategory(items) {
    return items.reduce((acc, item) => {
        const cat = item.category || 'General';
        (acc[cat] = acc[cat] || []).push(item);
        return acc;
    }, {});
}

function renderCategorySection(category, items, renderCard, storageKey) {
    const key = 'comms-cat-' + storageKey + '-' + category;
    const isCollapsed = sessionStorage.getItem(key) === 'collapsed';

    const section = document.createElement('div');
    section.className = 'category-section';

    const header = document.createElement('div');
    header.className = 'category-header' + (isCollapsed ? ' collapsed' : '');

    const chevron = document.createElement('span');
    chevron.className = 'category-chevron';
    chevron.textContent = '▼';

    const name = document.createElement('span');
    name.className = 'category-name';
    name.textContent = category;

    const count = document.createElement('span');
    count.className = 'category-count';
    count.textContent = items.length;

    header.append(chevron, name, count);

    const body = document.createElement('div');
    body.className = 'category-body';
    body.style.display = isCollapsed ? 'none' : 'flex';
    items.forEach(item => body.appendChild(renderCard(item)));

    header.addEventListener('click', () => {
        const collapsed = body.style.display === 'none';
        body.style.display = collapsed ? 'flex' : 'none';
        header.classList.toggle('collapsed', !collapsed);
        sessionStorage.setItem(key, collapsed ? '' : 'collapsed');
    });

    section.append(header, body);
    return section;
}

function renderTemplateCard(t) {
    const requiredVars = parseRequiredVars(t.required_vars);
    const detectedVars = extractVariables(t.content);

    const card = document.createElement('div');
    card.className = 'template-card';

    // Header row: title + actions
    const headerRow = document.createElement('div');
    headerRow.className = 'template-card-header';

    const title = document.createElement('span');
    title.className = 'template-card-title';
    title.textContent = t.title;

    const actions = document.createElement('div');
    actions.className = 'template-card-actions';

    const copyBtn = document.createElement('button');
    copyBtn.className = 'btn btn-secondary btn-sm';
    copyBtn.textContent = 'Copy';
    copyBtn.addEventListener('click', () => copyWithVariables(t.content));

    actions.appendChild(copyBtn);

    if (CAN_MANAGE) {
        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openTemplateModal(t.type, t));

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-danger btn-sm';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', async () => {
            if (!await showConfirm('Delete this template?', 'Delete')) return;
            const res = await fetch('/api/comms/templates/' + t.id, { method: 'DELETE' });
            if (res.ok) {
                delete cache[t.type];
                loaded[t.type] = false;
                loadTemplates(t.type);
                showToast('Template deleted.');
            } else {
                showToast('Delete failed.', 'error');
            }
        });

        actions.append(editBtn, delBtn);
    }

    headerRow.append(title, actions);
    card.appendChild(headerRow);

    // Variable chips
    if (detectedVars.length > 0) {
        const varsRow = document.createElement('div');
        varsRow.className = 'template-vars-row';
        detectedVars.forEach(v => {
            const chip = document.createElement('span');
            const isSystem = requiredVars.includes(v);
            chip.className = 'var-chip ' + (isSystem ? 'var-chip-system' : 'var-chip-user');
            chip.textContent = (isSystem ? '🔒 ' : '') + '{' + v + '}';
            chip.title = isSystem ? 'System-provided (auto-filled by integrations)' : 'Fill in when copying';
            varsRow.appendChild(chip);
        });
        card.appendChild(varsRow);
    }

    return card;
}


// ── Resource loading & rendering ─────────────────────────────────────────────

async function loadResources() {
    const listEl = document.getElementById('resource-list');
    listEl.textContent = 'Loading…';
    try {
        const res = await fetch('/api/comms/resources');
        if (!res.ok) throw new Error('fetch failed');
        const data = await res.json();
        cache.resources = data.items || [];
        renderResourceList();
    } catch {
        listEl.textContent = 'Failed to load. Please refresh.';
    }
}

function renderResourceList() {
    const listEl = document.getElementById('resource-list');
    const query = document.getElementById('resource-search').value.trim().toLowerCase();
    const items = cache.resources || [];

    const filtered = query
        ? items.filter(r => r.title.toLowerCase().includes(query) || r.description.toLowerCase().includes(query) || r.url.toLowerCase().includes(query))
        : items;

    listEl.replaceChildren();

    if (query) {
        const count = document.createElement('p');
        count.className = 'comms-results-count';
        count.textContent = filtered.length + ' result' + (filtered.length !== 1 ? 's' : '');
        listEl.appendChild(count);
    }

    if (filtered.length === 0) {
        listEl.appendChild(emptyState(query ? 'No resources match your search.' : 'No resources yet.' + (CAN_MANAGE ? ' Click "+ New Resource" to add one.' : '')));
        return;
    }

    const grid = document.createElement('div');
    grid.className = 'resource-grid';
    filtered.forEach(r => grid.appendChild(renderResourceCard(r)));
    listEl.appendChild(grid);
}

function renderResourceCard(r) {
    const card = document.createElement('div');
    card.className = 'resource-card';

    const headerRow = document.createElement('div');
    headerRow.className = 'resource-card-header';

    const titleLink = document.createElement('a');
    titleLink.className = 'resource-card-title';
    titleLink.textContent = r.title;
    titleLink.href = r.url;
    titleLink.target = '_blank';
    titleLink.rel = 'noopener noreferrer';

    const actions = document.createElement('div');
    actions.className = 'resource-card-actions';

    if (CAN_MANAGE) {
        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openResourceModal(r));

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-danger btn-sm';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', async () => {
            if (!await showConfirm('Delete this resource?', 'Delete')) return;
            const res = await fetch('/api/comms/resources/' + r.id, { method: 'DELETE' });
            if (res.ok) {
                cache.resources = null;
                loaded.resources = false;
                loadResources();
                showToast('Resource deleted.');
            } else {
                showToast('Delete failed.', 'error');
            }
        });

        actions.append(editBtn, delBtn);
    }

    headerRow.append(titleLink, actions);
    card.appendChild(headerRow);

    if (r.description) {
        const desc = document.createElement('p');
        desc.className = 'resource-card-desc';
        desc.textContent = r.description;
        card.appendChild(desc);
    }

    const meta = document.createElement('p');
    meta.className = 'resource-card-meta';
    meta.textContent = r.url;
    card.appendChild(meta);

    return card;
}


// ── Template modal ────────────────────────────────────────────────────────────

function openTemplateModal(type, template) {
    const modal = document.getElementById('modal-template');
    const titleEl = document.getElementById('modal-template-title');
    const idEl = document.getElementById('template-id');
    const typeEl = document.getElementById('template-type');
    const titleInput = document.getElementById('template-title-input');
    const catInput = document.getElementById('template-category-input');
    const contentInput = document.getElementById('template-content-input');
    const reqVarsInput = document.getElementById('template-required-vars-input');
    const statusEl = document.getElementById('template-status');

    const label = type === 'mail' ? 'Mail Template' : 'Announcement';
    titleEl.textContent = template ? 'Edit ' + label : 'New ' + label;
    typeEl.value = type;
    statusEl.textContent = '';

    if (template) {
        idEl.value = template.id;
        titleInput.value = template.title;
        catInput.value = template.category;
        contentInput.value = template.content;
        reqVarsInput.value = parseRequiredVars(template.required_vars).join(', ');
    } else {
        idEl.value = '';
        titleInput.value = '';
        catInput.value = '';
        contentInput.value = '';
        reqVarsInput.value = '';
    }

    updateVarsPreview();
    modal.style.display = 'flex';
    titleInput.focus();
}

function updateVarsPreview() {
    const content = document.getElementById('template-content-input').value;
    const reqRaw = document.getElementById('template-required-vars-input').value;
    const requiredVars = reqRaw.split(',').map(v => v.trim()).filter(Boolean);
    const detected = extractVariables(content);

    const preview = document.getElementById('template-vars-preview');
    const chipsEl = document.getElementById('template-vars-chips');

    if (detected.length === 0) {
        preview.style.display = 'none';
        return;
    }

    preview.style.display = 'flex';
    chipsEl.replaceChildren();
    detected.forEach(v => {
        const chip = document.createElement('span');
        const isSystem = requiredVars.includes(v);
        chip.className = 'var-chip ' + (isSystem ? 'var-chip-system' : 'var-chip-user');
        chip.textContent = (isSystem ? '🔒 ' : '') + '{' + v + '}';
        chipsEl.appendChild(chip);
    });
}

async function saveTemplate() {
    const id = document.getElementById('template-id').value;
    const type = document.getElementById('template-type').value;
    const title = document.getElementById('template-title-input').value.trim();
    const category = document.getElementById('template-category-input').value.trim() || 'General';
    const content = document.getElementById('template-content-input').value;
    const reqRaw = document.getElementById('template-required-vars-input').value;
    const requiredVars = reqRaw.split(',').map(v => v.trim()).filter(Boolean);
    const statusEl = document.getElementById('template-status');

    if (!title) {
        setFieldError(document.getElementById('template-title-input'), 'Title is required.');
        return;
    }

    const body = { type, title, category, content, required_vars: JSON.stringify(requiredVars) };
    const url = id ? '/api/comms/templates/' + id : '/api/comms/templates';
    const method = id ? 'PUT' : 'POST';

    statusEl.textContent = 'Saving…';
    try {
        const res = await fetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const msg = await res.text();
            statusEl.textContent = msg || 'Save failed.';
            return;
        }
        document.getElementById('modal-template').style.display = '';
        delete cache[type];
        loaded[type] = false;
        loadTemplates(type);
        showToast(id ? 'Template updated.' : 'Template created.');
    } catch {
        statusEl.textContent = 'Network error.';
    }
}

// ── Resource modal ────────────────────────────────────────────────────────────

function openResourceModal(resource) {
    const modal = document.getElementById('modal-resource');
    const titleEl = document.getElementById('modal-resource-title');
    const idEl = document.getElementById('resource-id');
    const titleInput = document.getElementById('resource-title-input');
    const urlInput = document.getElementById('resource-url-input');
    const descInput = document.getElementById('resource-desc-input');
    const statusEl = document.getElementById('resource-status');

    statusEl.textContent = '';

    if (resource && resource.id) {
        titleEl.textContent = 'Edit Resource';
        idEl.value = resource.id;
        titleInput.value = resource.title;
        urlInput.value = resource.url;
        descInput.value = resource.description;
    } else {
        titleEl.textContent = 'New Resource';
        idEl.value = '';
        titleInput.value = '';
        urlInput.value = '';
        descInput.value = '';
    }

    modal.style.display = 'flex';
    titleInput.focus();
}

async function saveResource() {
    const id = document.getElementById('resource-id').value;
    const title = document.getElementById('resource-title-input').value.trim();
    const url = document.getElementById('resource-url-input').value.trim();
    const description = document.getElementById('resource-desc-input').value.trim();
    const statusEl = document.getElementById('resource-status');

    if (!title) {
        setFieldError(document.getElementById('resource-title-input'), 'Title is required.');
        return;
    }
    if (!url) {
        setFieldError(document.getElementById('resource-url-input'), 'URL is required.');
        return;
    }

    const endpoint = id ? '/api/comms/resources/' + id : '/api/comms/resources';
    const method = id ? 'PUT' : 'POST';

    statusEl.textContent = 'Saving…';
    try {
        const res = await fetch(endpoint, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ title, url, description }),
        });
        if (!res.ok) {
            const msg = await res.text();
            statusEl.textContent = msg || 'Save failed.';
            return;
        }
        document.getElementById('modal-resource').style.display = '';
        cache.resources = null;
        loaded.resources = false;
        loadResources();
        showToast(id ? 'Resource updated.' : 'Resource created.');
    } catch {
        statusEl.textContent = 'Network error.';
    }
}

// ── Datalist population ───────────────────────────────────────────────────────

function populateCategorySuggestions() {
    const dl = document.getElementById('category-suggestions');
    const seen = new Set();
    ['mail', 'announcement'].forEach(type => {
        (cache[type] || []).forEach(t => seen.add(t.category));
    });
    // Default suggestions always present
    ['General', 'Desert Storm', 'Gold Zombies', 'Policy', 'Event', 'Reminder', 'Important'].forEach(s => seen.add(s));
    dl.replaceChildren();
    [...seen].sort().forEach(cat => {
        const opt = document.createElement('option');
        opt.value = cat;
        dl.appendChild(opt);
    });
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function parseRequiredVars(rv) {
    try {
        const parsed = JSON.parse(rv || '[]');
        return Array.isArray(parsed) ? parsed : [];
    } catch {
        return [];
    }
}

function emptyState(msg) {
    const p = document.createElement('p');
    p.className = 'comms-empty';
    p.textContent = msg;
    return p;
}
