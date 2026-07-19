// files.js - Handles file management UI: upload, edit, tags, filtering, Collabora.

let canManageFiles = false;
let canUploadFiles = false;
let userRank = '';
let allFilesData = [];
let allTagsData = [];
let currentEditFileId = null;
let tagEditorId = null;         // null = creating a tag; id = editing that tag
let uploadTagChoices = null;
let editTagChoices = null;
let createTagChoices = null;
let renderSortChips = null;
let newTagReturnChoices = null; // which tag picker to drop the new tag back into (create only)
let newTagReturnModal = null;   // which modal to return focus to on close
let newTagColorChoices = null;  // color dropdown in the tag editor modal

// Turn a color <select> into a Choices dropdown that shows a swatch per option.
// The swatch is pure CSS (files.css) keyed on the option value; we just tag the
// Choices wrapper with .color-choices to scope it. Returns null if Choices is absent.
function makeColorChoices(selector) {
    if (!window.Choices) return null;
    const c = new Choices(selector, { searchEnabled: false, shouldSort: false, itemSelectText: '', allowHTML: false });
    document.querySelector(selector)?.closest('.choices')?.classList.add('color-choices');
    return c;
}

// Set a color dropdown's value through Choices when present, else the native select.
function setColorValue(choices, selectId, value) {
    if (choices) choices.setChoiceByValue(value);
    else document.getElementById(selectId).value = value;
}

const RANK_ORDER = { R1: 1, R2: 2, R3: 3, R4: 4, R5: 5 };
const SORT_LABELS = { name: 'Name', updated: 'Updated', uploaded: 'Uploaded', owner: 'Owner', type: 'Type' };
const SORT_DEFAULTS = { name: 'asc', updated: 'desc', uploaded: 'desc', owner: 'asc', type: 'asc' };
const sortState = { field: 'name', dir: 'asc' };

document.addEventListener('DOMContentLoaded', () => {
    const cfg = document.getElementById('page-config').dataset;
    canManageFiles = cfg.canManage === 'true';
    canUploadFiles = cfg.canUpload === 'true';
    userRank = cfg.userRank || '';

    // Upgrade the tag multi-selects to searchable Choices controls (pills with ×).
    if (window.Choices) {
        const opts = { removeItemButton: true, shouldSort: false, itemSelectText: '', searchPlaceholderValue: 'Search tags…' };
        uploadTagChoices = new Choices('#file-tags', opts);
        editTagChoices = new Choices('#edit-file-tags', opts);
        createTagChoices = new Choices('#create-file-tags', opts);

        // Paint the selected pills / dropdown options in each tag's own color.
        ['file-tags', 'edit-file-tags', 'create-file-tags']
            .forEach(id => colorizeTagPicker(document.getElementById(id)));

        // Color dropdown with visible swatches (tag editor modal).
        newTagColorChoices = makeColorChoices('#new-tag-color');
    }

    document.getElementById('upload-file-btn')?.addEventListener('click', showUploadModal);
    document.getElementById('create-file-btn')?.addEventListener('click', showCreateModal);
    document.getElementById('manage-tags-btn')?.addEventListener('click', showTagModal);
    document.getElementById('edit-cancel-btn').addEventListener('click', closeEditModal);
    document.getElementById('edit-form').addEventListener('submit', handleEdit);
    document.getElementById('upload-cancel-btn').addEventListener('click', closeUploadModal);
    document.getElementById('upload-form').addEventListener('submit', handleUpload);
    document.getElementById('create-cancel-btn')?.addEventListener('click', closeCreateModal);
    document.getElementById('create-form')?.addEventListener('submit', handleCreate);
    document.getElementById('tag-close-btn')?.addEventListener('click', closeTagModal);
    document.getElementById('tag-add-btn')?.addEventListener('click', () => openTagEditor({}));
    document.getElementById('new-tag-cancel-btn')?.addEventListener('click', closeNewTagModal);
    document.getElementById('new-tag-form')?.addEventListener('submit', saveTagEditor);
    // Live "sits between …" hint under the sort-order field (name affects tie-breaking).
    document.getElementById('new-tag-sort-order')?.addEventListener('input', updateSortHint);
    document.getElementById('new-tag-name')?.addEventListener('input', updateSortHint);

    // "+ New tag" buttons beside each tag picker open the tag editor, returning the new
    // tag to that picker. data-target = the picker's <select> id.
    const pickerChoices = () => ({
        'file-tags': uploadTagChoices,
        'edit-file-tags': editTagChoices,
        'create-file-tags': createTagChoices,
    });
    document.querySelectorAll('.new-tag-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const id = btn.dataset.target;
            openTagEditor({ returnChoices: pickerChoices()[id], returnSelectEl: document.getElementById(id) });
        });
    });

    // Close modals on backdrop click (no corner × per design standard)
    [['edit-modal', closeEditModal], ['upload-modal', closeUploadModal],
     ['create-modal', closeCreateModal], ['tag-modal', closeTagModal],
     ['new-tag-modal', closeNewTagModal], ['image-modal', closeImageModal],
     ['document-modal', closeDocumentModal]].forEach(([id, fn]) => {
        const modal = document.getElementById(id);
        if (modal) modal.addEventListener('click', e => { if (e.target === modal) fn(); });
    });

    // Shared filter-panel plumbing (filter-panel.js).
    FilterPanel.setupSearch('file-search', 'clear-file-search', applyFilters);
    FilterPanel.setupToggle({ onClear: clearFilters });
    renderSortChips = FilterPanel.setupSortChips('.sort-chip', sortState, SORT_LABELS, SORT_DEFAULTS, applyFilters);
    // Type chips are static (fixed set), so wire them once here; tag chips are wired
    // in buildTagChips() because they're rebuilt after tag CRUD.
    FilterPanel.setupChipGroup('.type-chip', 'type', applyFilters);

    loadTags().then(loadFiles);
});

// ── Tags: chips + Choices ──────────────────────────────────────────
async function loadTags() {
    try {
        const res = await fetch('/api/file-tags');
        allTagsData = (await res.json()) || [];
    } catch (e) {
        allTagsData = [];
    }
    buildTagChips();
    populateTagChoices();
}

function makeTagChip(value, label, color) {
    const b = document.createElement('button');
    b.className = 'filter-chip tag-chip' + (value === 'all' ? ' active' : '');
    if (color) b.classList.add('tag-chip--' + color);
    b.dataset.tag = value;
    b.textContent = label;
    return b;
}

function buildTagChips() {
    const row = document.getElementById('tag-chips-row');
    row.querySelectorAll('.tag-chip').forEach(c => c.remove());
    const frag = document.createDocumentFragment();
    frag.appendChild(makeTagChip('all', 'All', null));
    allTagsData.forEach(t => frag.appendChild(makeTagChip(String(t.id), t.name, t.color)));
    frag.appendChild(makeTagChip('untagged', 'Untagged', null));
    row.appendChild(frag);
    // Rebind chip clicks (fresh nodes; old ones were removed above).
    FilterPanel.setupChipGroup('.tag-chip', 'tag', applyFilters);
}

function populateTagChoices() {
    const opts = allTagsData.map(t => ({ value: String(t.id), label: t.name }));
    // 4th arg (replace=true) is REQUIRED — Choices appends by default, which would
    // duplicate the options every time tags reload after a CRUD action.
    if (uploadTagChoices) uploadTagChoices.setChoices(opts, 'value', 'label', true);
    if (editTagChoices) editTagChoices.setChoices(opts, 'value', 'label', true);
    if (createTagChoices) createTagChoices.setChoices(opts, 'value', 'label', true);
}

// ── Filtering / sorting ────────────────────────────────────────────
function sortFiles(list) {
    const mul = sortState.dir === 'asc' ? 1 : -1;
    return [...list].sort((a, b) => {
        let d = 0;
        switch (sortState.field) {
            case 'name':     d = (a.title || '').localeCompare(b.title || ''); break;
            case 'updated':  d = (a.updated_at || '').localeCompare(b.updated_at || ''); break;
            case 'uploaded': d = (a.created_at || '').localeCompare(b.created_at || ''); break;
            case 'owner':    d = (a.owner_name || '').localeCompare(b.owner_name || ''); break;
            case 'type':     d = (a.file_type || '').localeCompare(b.file_type || ''); break;
        }
        if (d === 0) d = (a.title || '').localeCompare(b.title || '');
        return d * mul;
    });
}

function applyFilters() {
    const grid = document.getElementById('files-grid');
    const q = (document.getElementById('file-search').value || '').trim().toLowerCase();
    const activeTypes = Array.from(document.querySelectorAll('.type-chip.active')).map(c => c.dataset.type);
    const typeFilterOn = activeTypes.length > 0 && !activeTypes.includes('all');
    const activeTags = Array.from(document.querySelectorAll('.tag-chip.active')).map(c => c.dataset.tag);
    const tagFilterOn = activeTags.length > 0 && !activeTags.includes('all');

    let list = allFilesData.filter(f => {
        if (q) {
            const hay = ((f.title || '') + ' ' + (f.owner_name || '')).toLowerCase();
            if (!hay.includes(q)) return false;
        }
        if (typeFilterOn && !activeTypes.includes(f.file_type)) return false;
        if (tagFilterOn) {
            const ids = (f.tags || []).map(t => String(t.id));
            // OR semantics across selected chips; "Untagged" matches files whose
            // visible tag list is empty.
            const ok = activeTags.some(sel => sel === 'untagged' ? ids.length === 0 : ids.includes(sel));
            if (!ok) return false;
        }
        return true;
    });

    list = sortFiles(list);

    if (list.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'empty-state';
        empty.textContent = allFilesData.length === 0 ? 'No files uploaded yet.' : 'No files match the current filters.';
        grid.replaceChildren(empty);
    } else {
        grid.replaceChildren(...list.map(buildFileCard));
    }

    FilterPanel.updateActiveBadge([['.type-chip', 'type'], ['.tag-chip', 'tag']], { extra: q ? 1 : 0 });
}

function clearFilters() {
    FilterPanel.clearChipGroups([['.type-chip', 'type'], ['.tag-chip', 'tag']]);
    const search = document.getElementById('file-search');
    search.value = '';
    document.getElementById('clear-file-search').style.display = 'none';
    sortState.field = 'name';
    sortState.dir = 'asc';
    if (renderSortChips) renderSortChips();
    applyFilters();
}

// ── File cards ─────────────────────────────────────────────────────
// Parse a server timestamp, tolerating both the space form ("2026-07-19 03:31:38", UTC)
// and the ISO form the driver returns for some columns ("2026-07-19T03:31:38Z"). Only
// appends 'Z' when the string carries no timezone, so it never double-stamps.
function parseFileDate(s) {
    if (!s) return null;
    let iso = String(s).trim().replace(' ', 'T');
    if (!/(z|[+-]\d{2}:?\d{2})$/i.test(iso)) iso += 'Z';
    const d = new Date(iso);
    return isNaN(d) ? null : d;
}

// Short date for the visible "Updated" badge — app-wide style ("Jul 17, 2026"), matching
// window.formatJoinDate. Local time, so it agrees with the tooltip.
function fileDateShort(s) {
    const d = parseFileDate(s);
    return d ? d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
             : (s ? String(s).substring(0, 10) : '');
}

// Full, human-readable timestamp for hover tooltips (same style + time).
function fileTimestampFull(s) {
    const d = parseFileDate(s);
    return d ? d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })
             : (s || '');
}

// File-card action button: SVG icon + a label that collapses to icon-only when the
// card is narrow (via the @container query in files.css). title/aria-label keep the
// action discoverable when the text is hidden. Mirrors memberActionBtn in members.js.
function fileActionBtn(className, icon, label) {
    const btn = document.createElement('button');
    btn.className = className;
    btn.title = label;
    btn.setAttribute('aria-label', label);
    const span = document.createElement('span');
    span.className = 'file-action-label';
    span.textContent = label;
    btn.append(svgIcon(icon, 14), span);
    return btn;
}

// A metadata badge (Members-card style): an icon + value pill. rankClass gives the
// per-rank color for the view/edit badges; omit it for muted (owner/updated) badges.
// title is the hover-expanded description (native tooltip, as the Members card uses).
function fileMetaBadge(icon, value, rankClass, title) {
    const span = document.createElement('span');
    span.className = 'member-rank ' + (rankClass || 'file-meta-muted');
    if (title) span.title = title;
    span.append(svgIcon(icon, 13), document.createTextNode(value));
    return span;
}

function buildFileCard(file) {
    let iconName = 'file-text';
    if (file.file_type === 'image') iconName = 'photo';
    if (file.file_type === 'spreadsheet') iconName = 'chart-line';

    const card = document.createElement('div');
    card.className = 'card file-card';

    // Top row: ONLY the title shares this row with the action buttons. Everything
    // else (meta, updated, tags) is a full-width row below, so it flows under the
    // buttons rather than being trapped in a narrow left column.
    const h3 = document.createElement('h3');
    h3.className = 'file-title';
    h3.append(svgIcon(iconName, 16), document.createTextNode(file.title));

    const actionsDiv = document.createElement('div');
    actionsDiv.className = 'file-actions';

    const ext = file.file_name.substring(file.file_name.lastIndexOf('.'));
    const openBtn = fileActionBtn('btn btn-sm btn-primary', 'external-link', 'Open');
    openBtn.addEventListener('click', () => openFile(file.id, file.file_type, file.title, ext));
    actionsDiv.appendChild(openBtn);

    if (canManageFiles || file.is_owner) {
        const editBtn = fileActionBtn('btn btn-sm btn-secondary', 'pencil', 'Edit');
        editBtn.addEventListener('click', () => showEditModal(file.id));
        actionsDiv.appendChild(editBtn);

        const deleteBtn = fileActionBtn('btn btn-sm btn-danger', 'trash', 'Delete');
        deleteBtn.addEventListener('click', () => deleteFile(file.id));
        actionsDiv.appendChild(deleteBtn);
    }

    const top = document.createElement('div');
    top.className = 'file-card-top';
    top.append(h3, actionsDiv);
    card.appendChild(top);

    // Metadata badges (Members-card style) — a full-width row beneath the title+buttons.
    // Owner/updated are muted; view/edit carry their per-rank color. Icons: owner=user,
    // view=eye, edit=pencil, updated=clock.
    const metaDiv = document.createElement('div');
    metaDiv.className = 'file-meta';
    metaDiv.append(
        fileMetaBadge('user', file.owner_name, null,
            `Uploaded by ${file.owner_name} · ${fileTimestampFull(file.created_at)}`),
        fileMetaBadge('eye', file.min_rank, `rank-${file.min_rank}`,
            `Minimum rank to view: ${file.min_rank}`),
        fileMetaBadge('pencil', file.min_edit_rank, `rank-${file.min_edit_rank}`,
            `Minimum rank to edit: ${file.min_edit_rank}`),
    );
    // Add an "updated" badge only when the file was modified after upload.
    if (file.updated_at && file.updated_at !== file.created_at) {
        const when = fileTimestampFull(file.updated_at);
        const updTitle = file.updated_by_name
            ? `Last updated by ${file.updated_by_name} · ${when}`
            : `Last updated: ${when}`;
        metaDiv.append(fileMetaBadge('clock', fileDateShort(file.updated_at), null, updTitle));
    }
    card.appendChild(metaDiv);

    // Tag badges (already rank-filtered server-side) — full-width row.
    if (file.tags && file.tags.length) {
        const tagRow = document.createElement('div');
        tagRow.className = 'file-tag-row';
        file.tags.forEach(t => {
            const badge = document.createElement('span');
            badge.className = 'file-tag file-tag--' + (t.color || 'neutral');
            badge.textContent = t.name;
            tagRow.appendChild(badge);
        });
        card.appendChild(tagRow);
    }

    return card;
}

async function loadFiles() {
    const grid = document.getElementById('files-grid');
    try {
        const res = await fetch('/api/files');
        allFilesData = (await res.json()) || [];
        applyFilters();
    } catch (error) {
        const errDiv = document.createElement('div');
        errDiv.className = 'error-message';
        errDiv.textContent = 'Error loading files';
        grid.replaceChildren(errDiv);
    }
}

// Extensions Collabora Online can open via WOPI
const COLLABORA_SUPPORTED = new Set(['.docx','.doc','.odt','.xlsx','.xls','.ods','.pptx','.ppt','.odp']);

async function openFile(id, type, title, ext) {
    if (type === 'image') {
        document.getElementById('image-modal-title').textContent = title;
        document.getElementById('image-modal-img').src = `/api/files/download/${id}`;
        const imageModal = document.getElementById('image-modal');
        imageModal.style.display = 'flex';
        trapFocus(imageModal);
    } else if (ext === '.pdf') {
        window.open(`/api/files/download/${id}`, '_blank', 'noopener noreferrer');
    } else if (!COLLABORA_SUPPORTED.has(ext)) {
        // CSV and other plain-text formats can't be opened in Collabora Online — download instead
        window.location.href = `/api/files/download/${id}`;
    } else {
        // 1. Fetch the token and internal routing data from Go
        const res = await fetch(`/api/files/${id}/wopi-token`);
        const { token, collabora_domain, wopi_src } = await res.json();

        const protocol = window.location.protocol;

        // 2. Match Collabora theme to your app's theme.js state
        let themePref = localStorage.getItem('lastwar-theme-preference') || 'auto';
        let isDark = false;
        if (themePref === 'dark') {
            isDark = true;
        } else if (themePref === 'auto') {
            isDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        }
        const themeParam = isDark ? '&theme=dark' : '&theme=light';

        // 3. Build the external URL for the browser, but inject the internal route for the server
        const collaboraUrl = `${protocol}//${collabora_domain}/browser/dist/cool.html?WOPISrc=${encodeURIComponent(wopi_src)}${themeParam}`;

        // Build form + iframe via DOM
        const form = document.createElement('form');
        form.id = 'wopi-form';
        form.action = collaboraUrl;
        form.method = 'post';
        form.target = 'collabora-iframe';
        form.style.display = 'none';
        const tokenInput = document.createElement('input');
        tokenInput.name = 'access_token';
        tokenInput.value = token;
        tokenInput.type = 'hidden';
        form.appendChild(tokenInput);

        const iframe = document.createElement('iframe');
        iframe.id = 'collabora-iframe';
        iframe.name = 'collabora-iframe';
        iframe.allowFullscreen = true;
        iframe.style.cssText = 'width:100%; height:100%; border:none; border-radius: 8px;';

        document.getElementById('document-modal-body').replaceChildren(form, iframe);
        const documentModal = document.getElementById('document-modal');
        documentModal.style.display = 'flex';
        trapFocus(documentModal);

        // Submit the hidden form to securely pass the token into the iframe
        form.submit();
    }
}

function closeImageModal() {
    const m = document.getElementById('image-modal');
    releaseFocus(m);
    m.style.display = 'none';
}

function closeDocumentModal() {
    const m = document.getElementById('document-modal');
    releaseFocus(m);
    m.style.display = 'none';
}

// ── Upload ─────────────────────────────────────────────────────────
function showUploadModal() {
    // NOTE: never call upload-form.reset() — Choices restores its at-init (empty)
    // option list on form reset, wiping the tags we loaded via setChoices. Clear
    // each field explicitly instead.
    document.getElementById('file-title').value = '';
    document.getElementById('file-input').value = '';
    document.getElementById('file-min-rank').value = 'R1';
    document.getElementById('file-min-edit-rank').value = 'R4';
    if (uploadTagChoices) uploadTagChoices.removeActiveItems();

    const uploadModal = document.getElementById('upload-modal');
    uploadModal.style.display = 'flex';
    trapFocus(uploadModal);
}

function closeUploadModal() {
    const uploadModal = document.getElementById('upload-modal');
    releaseFocus(uploadModal);
    uploadModal.style.display = 'none';
}

async function handleUpload(e) {
    e.preventDefault();
    const btn = document.getElementById('upload-submit-btn');
    btn.disabled = true;
    btn.textContent = "Uploading...";

    const formData = new FormData();
    formData.append('title', document.getElementById('file-title').value);
    formData.append('min_rank', document.getElementById('file-min-rank').value);
    formData.append('min_edit_rank', document.getElementById('file-min-edit-rank').value);
    formData.append('file', document.getElementById('file-input').files[0]);
    const tagVals = uploadTagChoices ? uploadTagChoices.getValue(true) : [];
    tagVals.forEach(v => formData.append('tag_ids', v));

    try {
        const res = await fetch('/api/files/upload', { method: 'POST', body: formData });
        if (!res.ok) throw new Error("Upload failed");

        closeUploadModal();
        loadFiles();
        showToast('File uploaded.');
    } catch (err) {
        showToast(err.message, 'error');
    } finally {
        btn.disabled = false;
        btn.textContent = "Upload";
    }
}

// ── Create New (blank Collabora document) ──────────────────────────
function showCreateModal() {
    document.getElementById('create-file-title').value = '';
    document.getElementById('create-file-kind').value = 'document';
    document.getElementById('create-file-min-rank').value = 'R1';
    document.getElementById('create-file-min-edit-rank').value = 'R4';
    if (createTagChoices) createTagChoices.removeActiveItems();

    const m = document.getElementById('create-modal');
    m.style.display = 'flex';
    trapFocus(m);
}

function closeCreateModal() {
    const m = document.getElementById('create-modal');
    releaseFocus(m);
    m.style.display = 'none';
}

async function handleCreate(e) {
    e.preventDefault();
    const btn = document.getElementById('create-submit-btn');
    btn.disabled = true;
    btn.textContent = 'Creating...';

    const title = document.getElementById('create-file-title').value;
    const payload = {
        title,
        kind: document.getElementById('create-file-kind').value,
        min_rank: document.getElementById('create-file-min-rank').value,
        min_edit_rank: document.getElementById('create-file-min-edit-rank').value,
        tag_ids: createTagChoices ? createTagChoices.getValue(true).map(Number) : [],
    };

    try {
        const res = await fetch('/api/files/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        if (!res.ok) throw new Error('Create failed');
        const { id, ext, file_type } = await res.json();

        closeCreateModal();
        loadFiles();
        showToast('Document created.');
        // Open the fresh blank file straight into Collabora for editing.
        openFile(id, file_type, title, ext);
    } catch (err) {
        showToast(err.message, 'error');
    } finally {
        btn.disabled = false;
        btn.textContent = 'Create & Open';
    }
}

async function deleteFile(id) {
    if (!await showConfirm('Delete this file permanently?', 'Delete File')) return;
    await fetch(`/api/files/${id}`, { method: 'DELETE' });
    loadFiles();
    showToast('File deleted.');
}

// ── Edit ───────────────────────────────────────────────────────────
function showEditModal(id) {
    const file = allFilesData.find(f => f.id === id);
    if (!file) return;

    currentEditFileId = id;
    document.getElementById('edit-file-title').value = file.title;
    document.getElementById('edit-file-min-rank').value = file.min_rank;
    document.getElementById('edit-file-min-edit-rank').value = file.min_edit_rank;

    if (editTagChoices) {
        editTagChoices.removeActiveItems();
        const ids = (file.tags || []).map(t => String(t.id));
        if (ids.length) editTagChoices.setChoiceByValue(ids);
    }

    const editModal = document.getElementById('edit-modal');
    editModal.style.display = 'flex';
    trapFocus(editModal);
}

function closeEditModal() {
    const editModal = document.getElementById('edit-modal');
    releaseFocus(editModal);
    editModal.style.display = 'none';
    currentEditFileId = null;
}

async function handleEdit(e) {
    e.preventDefault();
    const btn = document.getElementById('edit-submit-btn');
    btn.disabled = true;
    btn.textContent = "Saving...";

    const payload = {
        title: document.getElementById('edit-file-title').value,
        min_rank: document.getElementById('edit-file-min-rank').value,
        min_edit_rank: document.getElementById('edit-file-min-edit-rank').value,
        tag_ids: editTagChoices ? editTagChoices.getValue(true).map(Number) : [],
    };

    try {
        const res = await fetch(`/api/files/${currentEditFileId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!res.ok) throw new Error("Update failed");

        closeEditModal();
        loadFiles();
        showToast('File updated.');
    } catch (err) {
        showToast(err.message, 'error');
    } finally {
        btn.disabled = false;
        btn.textContent = "Save Changes";
    }
}

// ── Manage Tags ────────────────────────────────────────────────────
async function showTagModal() {
    const m = document.getElementById('tag-modal');
    renderTagList();          // show the cached list immediately (no blank flash)
    m.style.display = 'flex';
    trapFocus(m);
    // Refresh from the server so file counts — and any tags added since page load (by
    // another user or tab) — are current. Targeted rather than full loadTags() so it
    // doesn't rebuild the filter chips and clear an active tag filter.
    try {
        const res = await fetch('/api/file-tags');
        if (res.ok) {
            const fresh = await res.json();
            if (Array.isArray(fresh)) { allTagsData = fresh; renderTagList(); }
        }
    } catch (e) { /* keep the cached list on fetch failure */ }
}

function closeTagModal() {
    const m = document.getElementById('tag-modal');
    releaseFocus(m);
    m.style.display = 'none';
}

// Disable min-rank options above the current user's rank (an empty rank = admin,
// no cap). The server enforces this too; this just prevents a confusing rejection.
function capMinRankOptions(selectId) {
    const sel = document.getElementById(selectId);
    if (!sel) return;
    const cap = userRank ? (RANK_ORDER[userRank] || 0) : 99;
    Array.from(sel.options).forEach(o => { o.disabled = (RANK_ORDER[o.value] || 0) > cap; });
}

function renderTagList() {
    const list = document.getElementById('tag-list');
    if (!allTagsData.length) {
        const empty = document.createElement('p');
        empty.className = 'status-msg';
        empty.textContent = 'No tags yet.';
        list.replaceChildren(empty);
        return;
    }
    const rows = allTagsData.map(tag => {
        const row = document.createElement('div');
        row.className = 'tag-manage-row';

        const badge = document.createElement('span');
        badge.className = 'file-tag file-tag--' + (tag.color || 'neutral');
        badge.textContent = tag.name;

        const meta = document.createElement('span');
        meta.className = 'tag-manage-meta';
        meta.textContent = `${tag.min_rank} · ${tag.file_count} file${tag.file_count === 1 ? '' : 's'}`;

        const actions = document.createElement('span');
        actions.className = 'tag-manage-actions';

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-sm btn-secondary';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openTagEditor({ tag }));

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-sm btn-danger';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', () => deleteTag(tag));

        actions.append(editBtn, delBtn);
        row.append(badge, meta, actions);
        return row;
    });
    list.replaceChildren(...rows);
}

// Give each tag pill / dropdown option its tag's color class, so the multiselect
// matches the card badges. Choices re-renders these items on every interaction, so a
// MutationObserver re-applies after each render. Non-destructive — only toggles
// tag-pill--* classes; the "+ Create tag" option (non-numeric value) stays default.
function colorizeTagPicker(selectEl) {
    const wrap = selectEl && selectEl.closest('.choices');
    if (!wrap) return;
    const apply = () => {
        wrap.querySelectorAll('.choices__item[data-value]').forEach(el => {
            Array.from(el.classList).forEach(c => { if (c.startsWith('tag-pill--')) el.classList.remove(c); });
            const tag = allTagsData.find(t => String(t.id) === el.getAttribute('data-value'));
            if (tag && tag.color) el.classList.add('tag-pill--' + tag.color);
        });
    };
    // childList/subtree only (not attributes), so our own class edits don't re-trigger it.
    new MutationObserver(apply).observe(wrap, { childList: true, subtree: true });
    apply();
}

// The tag editor modal handles both create and edit, opened from three places: a file
// picker's "+ New tag" button (create → select back into that picker), the Manage Tags
// "+ Add Tag" button (create), and a Manage Tags row's Edit button (edit).
//   opts.tag           — the tag to edit (omit to create)
//   opts.returnChoices — a picker Choices to select the new tag into (create only)
//   opts.returnSelectEl— that picker's <select> (used to find the modal to return to)
// Order matches the server's `ORDER BY sort_order, name COLLATE NOCASE`.
function tagSortCmp(a, b) {
    if (a.sort_order !== b.sort_order) return a.sort_order - b.sort_order;
    const an = (a.name || '').toLowerCase(), bn = (b.name || '').toLowerCase();
    return an < bn ? -1 : an > bn ? 1 : 0;
}

// Show which tags the one being edited would land between at the current sort order.
function updateSortHint() {
    const hint = document.getElementById('new-tag-sort-hint');
    if (!hint) return;
    const current = {
        __current: true,
        name: document.getElementById('new-tag-name').value.trim(),
        sort_order: parseInt(document.getElementById('new-tag-sort-order').value, 10) || 0,
    };
    const list = [...allTagsData.filter(t => t.id !== tagEditorId), current].sort(tagSortCmp);
    const idx = list.findIndex(t => t.__current);
    const before = idx > 0 ? list[idx - 1] : null;
    const after = idx < list.length - 1 ? list[idx + 1] : null;

    if (!before && !after) hint.textContent = 'Only tag.';
    else if (before && after) hint.textContent = `Between "${before.name}" and "${after.name}".`;
    else if (before) hint.textContent = `After "${before.name}" (last).`;
    else hint.textContent = `Before "${after.name}" (first).`;
}

function openTagEditor({ tag = null, returnChoices = null, returnSelectEl = null } = {}) {
    tagEditorId = tag ? tag.id : null;
    newTagReturnChoices = returnChoices;
    // Return focus to the file modal (from a picker) or the Manage Tags modal.
    newTagReturnModal = returnSelectEl ? returnSelectEl.closest('.modal') : document.getElementById('tag-modal');

    document.getElementById('new-tag-edit-id').value = tag ? String(tag.id) : '';
    document.getElementById('new-tag-name').value = tag ? tag.name : '';
    document.getElementById('new-tag-min-rank').value = tag ? tag.min_rank : 'R1';
    setColorValue(newTagColorChoices, 'new-tag-color', tag ? tag.color : 'neutral');
    document.getElementById('new-tag-sort-order').value = tag ? String(tag.sort_order) : '0';
    document.getElementById('new-tag-status').textContent = '';
    document.getElementById('new-tag-title').textContent = tag ? 'Edit Tag' : 'New Tag';
    document.getElementById('new-tag-submit-btn').textContent = tag ? 'Save Tag' : 'Create Tag';
    capMinRankOptions('new-tag-min-rank');
    updateSortHint();

    const m = document.getElementById('new-tag-modal');
    m.style.display = 'flex';
    trapFocus(m);
}

function closeNewTagModal() {
    const m = document.getElementById('new-tag-modal');
    releaseFocus(m);
    m.style.display = 'none';
    // Hand keyboard focus back to the modal underneath (file modal or Manage Tags).
    if (newTagReturnModal) trapFocus(newTagReturnModal);
}

async function saveTagEditor(e) {
    e.preventDefault();
    const status = document.getElementById('new-tag-status');
    status.textContent = '';

    const payload = {
        name: document.getElementById('new-tag-name').value.trim(),
        min_rank: document.getElementById('new-tag-min-rank').value,
        color: document.getElementById('new-tag-color').value,
        sort_order: parseInt(document.getElementById('new-tag-sort-order').value, 10) || 0,
    };
    if (!payload.name) { status.textContent = 'Name is required.'; return; }

    const editing = tagEditorId;
    const returnTo = newTagReturnChoices;
    const url = editing ? `/api/file-tags/${editing}` : '/api/file-tags';
    const method = editing ? 'PUT' : 'POST';

    try {
        const res = await fetch(url, {
            method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
        });
        if (!res.ok) {
            status.textContent = (await res.text()).trim() || 'Failed to save tag.';
            return;
        }
        const tag = await res.json();
        closeNewTagModal();
        await loadTags(); // refresh chips + all pickers
        // If the Manage Tags modal is open underneath, refresh its list.
        if (document.getElementById('tag-modal').style.display === 'flex') renderTagList();
        loadFiles();
        // On create from a file picker, select the new tag into that picker.
        if (!editing && returnTo) returnTo.setChoiceByValue(String(tag.id));
        showToast(editing ? 'Tag updated.' : 'Tag created.');
    } catch (err) {
        status.textContent = 'Failed to save tag.';
    }
}

async function deleteTag(tag) {
    if (!await showConfirm(`Delete tag "${tag.name}"?`, 'Delete Tag')) return;

    let res = await fetch(`/api/file-tags/${tag.id}`, { method: 'DELETE' });
    if (res.status === 409) {
        const data = await res.json();
        const msg = `"${tag.name}" is used by ${data.count} file${data.count === 1 ? '' : 's'}. Remove it from them and delete the tag?`;
        if (!await showConfirm(msg, 'Force Delete')) return;
        res = await fetch(`/api/file-tags/${tag.id}?force=true`, { method: 'DELETE' });
    }
    if (!res.ok) { showToast('Failed to delete tag.', 'error'); return; }

    showToast('Tag deleted.');
    await loadTags();
    renderTagList();
    loadFiles();
}
