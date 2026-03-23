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

async function loadFiles() {
    try {
        const res = await fetch('/api/files');
        allFilesData = await res.json();
        
        const grid = document.getElementById('files-grid');
        if (!allFilesData || allFilesData.length === 0) {
            grid.innerHTML = '<div class="empty-state">No files uploaded yet.</div>';
            return;
        }

        grid.innerHTML = allFilesData.map(file => {
            let icon = '📄';
            if (file.file_type === 'image') icon = '🖼️';
            if (file.file_type === 'spreadsheet') icon = '📊';

            let editBtn = (canManageFiles || file.is_owner) ? `<button class="btn btn-sm btn-secondary" onclick="showEditModal(${file.id})">✏️</button>` : '';
            let deleteBtn = (canManageFiles || file.is_owner) ? `<button class="btn btn-sm btn-danger" onclick="deleteFile(${file.id})">🗑️</button>` : '';

            return `
                <div class="card" style="padding: 15px; margin-bottom: 0;">
                    <div style="display: flex; justify-content: space-between; align-items: start;">
                        <div>
                            <h3 style="margin: 0; font-size: 1.1em; color: var(--text-primary);">${icon} ${file.title}</h3>
                            <div style="font-size: 0.85em; color: var(--text-muted); margin-top: 5px;">
                                By: ${file.owner_name} | 
                                View: <span class="member-rank rank-${file.min_rank}">${file.min_rank}</span> | 
                                Edit: <span class="member-rank rank-${file.min_edit_rank}">${file.min_edit_rank}</span>
                            </div>
                        </div>
                    </div>
                    <div style="margin-top: 15px; display: flex; gap: 10px;">
                        <button class="btn btn-sm btn-primary" style="flex: 1;" onclick="openFile(${file.id}, '${file.file_type}', '${file.title}', '${file.file_name.substring(file.file_name.lastIndexOf('.'))}')">Open</button>
                        ${editBtn}
                        ${deleteBtn}
                    </div>
                </div>
            `;
        }).join('');
    } catch (error) {
        document.getElementById('files-grid').innerHTML = `<div class="error-message">Error loading files</div>`;
    }
}

// Extensions Collabora Online can open via WOPI
const COLLABORA_SUPPORTED = new Set(['.docx','.doc','.odt','.xlsx','.xls','.ods','.pptx','.ppt','.odp']);

async function openFile(id, type, title, ext) {
    if (type === 'image') {
        document.getElementById('image-modal-title').textContent = title;
        document.getElementById('image-modal-img').src = `/api/files/download/${id}`;
        document.getElementById('image-modal').style.display = 'block';
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
        
        const iframeHtml = `
            <form id="wopi-form" action="${collaboraUrl}" method="post" target="collabora-iframe" style="display:none;">
                <input name="access_token" value="${token}" type="hidden" />
            </form>
            <iframe id="collabora-iframe" name="collabora-iframe" allowfullscreen style="width:100%; height:100%; border:none; border-radius: 8px;"></iframe>
        `;
        
        document.getElementById('document-modal-body').innerHTML = iframeHtml;
        document.getElementById('document-modal').style.display = 'flex';
        
        // Submit the hidden form to securely pass the token into the iframe
        document.getElementById('wopi-form').submit();
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