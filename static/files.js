// files.js - Handles file management UI and interactions

let canManageFiles = false;
let canUploadFiles = false;
let allFilesData = [];
let currentEditFileId = null;

document.addEventListener('DOMContentLoaded', async () => {
    try {
        const response = await fetch('/api/check-auth');
        if (response.ok) {
            const data = await response.json();
            canManageFiles = data.permissions.manage_files || data.is_admin;
            canUploadFiles = data.permissions.upload_files || data.is_admin;
        }
    } catch (e) {}
    loadFiles();
});

function buildFileCard(file) {
    let icon = '📄';
    if (file.file_type === 'image') icon = '🖼️';
    if (file.file_type === 'spreadsheet') icon = '📊';

    const card = document.createElement('div');
    card.className = 'card';
    card.style.cssText = 'padding: 15px; margin-bottom: 0;';

    const topRow = document.createElement('div');
    topRow.style.cssText = 'display: flex; justify-content: space-between; align-items: start;';

    const infoDiv = document.createElement('div');

    const h3 = document.createElement('h3');
    h3.style.cssText = 'margin: 0; font-size: 1.1em; color: var(--text-primary);';
    h3.textContent = `${icon} ${file.title}`;

    const metaDiv = document.createElement('div');
    metaDiv.style.cssText = 'font-size: 0.85em; color: var(--text-muted); margin-top: 5px;';

    metaDiv.appendChild(document.createTextNode(`By: ${file.owner_name} | View: `));
    const viewRank = document.createElement('span');
    viewRank.className = `member-rank rank-${file.min_rank}`;
    viewRank.textContent = file.min_rank;
    metaDiv.appendChild(viewRank);
    metaDiv.appendChild(document.createTextNode(' | Edit: '));
    const editRank = document.createElement('span');
    editRank.className = `member-rank rank-${file.min_edit_rank}`;
    editRank.textContent = file.min_edit_rank;
    metaDiv.appendChild(editRank);

    infoDiv.append(h3, metaDiv);
    topRow.appendChild(infoDiv);

    const actionsDiv = document.createElement('div');
    actionsDiv.style.cssText = 'margin-top: 15px; display: flex; gap: 10px;';

    const ext = file.file_name.substring(file.file_name.lastIndexOf('.'));
    const openBtn = document.createElement('button');
    openBtn.className = 'btn btn-sm btn-primary';
    openBtn.style.flex = '1';
    openBtn.textContent = 'Open';
    openBtn.addEventListener('click', () => openFile(file.id, file.file_type, file.title, ext));
    actionsDiv.appendChild(openBtn);

    if (canManageFiles || file.is_owner) {
        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-sm btn-secondary';
        editBtn.textContent = '✏️';
        editBtn.addEventListener('click', () => showEditModal(file.id));
        actionsDiv.appendChild(editBtn);

        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'btn btn-sm btn-danger';
        deleteBtn.textContent = '🗑️';
        deleteBtn.addEventListener('click', () => deleteFile(file.id));
        actionsDiv.appendChild(deleteBtn);
    }

    card.append(topRow, actionsDiv);
    return card;
}

async function loadFiles() {
    const grid = document.getElementById('files-grid');
    try {
        const res = await fetch('/api/files');
        allFilesData = await res.json();

        if (!allFilesData || allFilesData.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'empty-state';
            empty.textContent = 'No files uploaded yet.';
            grid.replaceChildren(empty);
            return;
        }

        grid.replaceChildren(...allFilesData.map(buildFileCard));
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
        document.getElementById('image-modal').style.display = 'flex';
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
        document.getElementById('document-modal').style.display = 'flex';

        // Submit the hidden form to securely pass the token into the iframe
        form.submit();
    }
}

function showUploadModal() {
    document.getElementById('upload-form').reset();
    document.getElementById('upload-modal').style.display = 'flex';
}

function closeUploadModal() {
    document.getElementById('upload-modal').style.display = 'none';
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

    try {
        const res = await fetch('/api/files/upload', { method: 'POST', body: formData });
        if (!res.ok) throw new Error("Upload failed");

        closeUploadModal();
        loadFiles();
    } catch (err) {
        alert(err.message);
    } finally {
        btn.disabled = false;
        btn.textContent = "Upload";
    }
}

async function deleteFile(id) {
    if (!confirm("Delete this file permanently?")) return;
    await fetch(`/api/files/${id}`, { method: 'DELETE' });
    loadFiles();
}

// --- File Editing Logic ---
function showEditModal(id) {
    const file = allFilesData.find(f => f.id === id);
    if (!file) return;

    currentEditFileId = id;
    document.getElementById('edit-file-title').value = file.title;
    document.getElementById('edit-file-min-rank').value = file.min_rank;
    document.getElementById('edit-file-min-edit-rank').value = file.min_edit_rank;

    document.getElementById('edit-modal').style.display = 'flex';
}

function closeEditModal() {
    document.getElementById('edit-modal').style.display = 'none';
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
        min_edit_rank: document.getElementById('edit-file-min-edit-rank').value
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
    } catch (err) {
        alert(err.message);
    } finally {
        btn.disabled = false;
        btn.textContent = "Save Changes";
    }
}

// end of files.js
