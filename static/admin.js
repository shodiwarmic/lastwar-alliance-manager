// static/admin.js - JavaScript for Admin Dashboard

let allUsers = [];
let allMembers = [];
let allLogins = [];
let currentEditUserId = null;
let currentResetUserId = null;

// Choices.js instances — initialised in DOMContentLoaded
let memberIdChoices = null;
let newOwnerChoices = null;
let loginFilterChoices = null;

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    // Check if we are actually on the admin page
    const usersList = document.getElementById('users-list');
    if (usersList) {
        memberIdChoices = new Choices('#member-id', {
            searchEnabled: true, searchPlaceholderValue: 'Search…',
            itemSelectText: '', shouldSort: false,
        });
        newOwnerChoices = new Choices('#new-owner-select', {
            searchEnabled: true, searchPlaceholderValue: 'Search…',
            itemSelectText: '', shouldSort: false,
        });
        loginFilterChoices = new Choices('#login-filter', {
            searchEnabled: true, searchPlaceholderValue: 'Search…',
            itemSelectText: '', shouldSort: false,
        });

        // Load initial data
        await loadUsers();
        await loadMembers();
        await loadStormSlots();
    }

    // Wire storm slots save button (only present on admin page)
    const slotSaveBtn = document.getElementById('btn-save-storm-slots');
    if (slotSaveBtn) {
        slotSaveBtn.addEventListener('click', saveStormSlots);
    }
});

// Tab Switching
function switchTab(tabName) {
    // Update tab buttons
    document.querySelectorAll('.tab-button').forEach(btn => btn.classList.remove('active'));
    event.target.classList.add('active');

    // Update tab content
    document.querySelectorAll('.tab-content').forEach(content => content.classList.remove('active'));
    document.getElementById(tabName + '-tab').classList.add('active');

    // Load data for the active tab
    if (tabName === 'logins') {
        loadLoginHistory();
    }
}

// Load Users
async function loadUsers() {
    try {
        const response = await fetch('/api/admin/users');
        if (!response.ok) throw new Error('Failed to fetch users');

        allUsers = await response.json();
        displayUsers(allUsers);
    } catch (error) {
        console.error('Error loading users:', error);
        const div = document.createElement('div');
        div.className = 'error-message';
        div.textContent = `Failed to load users: ${error.message}`;
        document.getElementById('users-list').replaceChildren(div);
    }
}

function buildRecentLoginsSection(user) {
    if (!user.recent_logins || user.recent_logins.length === 0) return null;

    const section = document.createElement('div');
    section.className = 'recent-logins';

    const label = document.createElement('strong');
    label.textContent = 'Recent Logins:';
    section.appendChild(label);

    user.recent_logins.slice(0, 3).forEach(login => {
        const location = [login.city, login.country].filter(v => v).join(', ') || 'Unknown';
        const time = new Date(login.login_time).toLocaleString();

        const entry = document.createElement('div');
        entry.className = 'login-entry';

        const locSpan = document.createElement('span');
        locSpan.className = 'login-location';
        locSpan.textContent = `📍 ${location}`;

        const ipSpan = document.createElement('span');
        ipSpan.className = 'login-ip';
        ipSpan.textContent = login.ip_address || 'N/A';

        const timeSpan = document.createElement('span');
        timeSpan.className = 'login-time';
        timeSpan.textContent = time;

        entry.append(locSpan, ipSpan, timeSpan);
        section.appendChild(entry);
    });

    return section;
}

function buildUserCard(user) {
    const lastLogin = user.last_login ? new Date(user.last_login).toLocaleString() : 'Never';

    const card = document.createElement('div');
    card.className = 'user-card';
    card.dataset.username = user.username.toLowerCase();

    // Header
    const header = document.createElement('div');
    header.className = 'user-header';

    const userInfo = document.createElement('div');
    userInfo.className = 'user-info';

    const h3 = document.createElement('h3');
    h3.textContent = user.username;
    if (user.is_admin) {
        const adminBadge = document.createElement('span');
        adminBadge.className = 'admin-badge';
        adminBadge.textContent = 'Admin';
        h3.appendChild(adminBadge);
    }

    const memberSpan = document.createElement('span');
    if (user.member_name) {
        memberSpan.className = 'member-badge';
        memberSpan.textContent = user.member_name;
    } else {
        memberSpan.className = 'no-member';
        memberSpan.textContent = 'No member linked';
    }

    userInfo.append(h3, memberSpan);

    const actions = document.createElement('div');
    actions.className = 'user-actions';

    const editBtn = document.createElement('button');
    editBtn.className = 'btn btn-sm btn-secondary';
    editBtn.textContent = '✏️ Edit';
    editBtn.addEventListener('click', () => editUser(user.id));

    const resetBtn = document.createElement('button');
    resetBtn.className = 'btn btn-sm btn-warning';
    resetBtn.textContent = '🔑 Reset Password';
    resetBtn.addEventListener('click', () => showResetPasswordModal(user.id, user.username));

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'btn btn-sm btn-danger';
    deleteBtn.textContent = '🗑️ Delete';
    deleteBtn.addEventListener('click', () => deleteUser(user.id, user.username));

    actions.append(editBtn, resetBtn, deleteBtn);
    header.append(userInfo, actions);

    // Stats
    const stats = document.createElement('div');
    stats.className = 'user-stats';

    const statLast = document.createElement('div');
    statLast.className = 'stat';
    const lastLabel = document.createElement('span');
    lastLabel.className = 'stat-label';
    lastLabel.textContent = 'Last Login:';
    const lastVal = document.createElement('span');
    lastVal.className = 'stat-value';
    lastVal.textContent = lastLogin;
    statLast.append(lastLabel, lastVal);

    const statCount = document.createElement('div');
    statCount.className = 'stat';
    const countLabel = document.createElement('span');
    countLabel.className = 'stat-label';
    countLabel.textContent = 'Total Logins:';
    const countVal = document.createElement('span');
    countVal.className = 'stat-value';
    countVal.textContent = user.login_count;
    statCount.append(countLabel, countVal);

    stats.append(statLast, statCount);

    card.append(header, stats);

    const recentLogins = buildRecentLoginsSection(user);
    if (recentLogins) card.appendChild(recentLogins);

    return card;
}

// Display Users
function displayUsers(users) {
    const usersList = document.getElementById('users-list');

    if (users.length === 0) {
        const div = document.createElement('div');
        div.className = 'empty-state';
        div.textContent = 'No users found';
        usersList.replaceChildren(div);
        return;
    }

    usersList.replaceChildren(...users.map(buildUserCard));
}

// Filter Users
function filterUsers() {
    const searchTerm = document.getElementById('user-search').value.toLowerCase();
    const filtered = allUsers.filter(user =>
        user.username.toLowerCase().includes(searchTerm) ||
        (user.member_name && user.member_name.toLowerCase().includes(searchTerm))
    );
    displayUsers(filtered);
}

// Load Members for dropdown
async function loadMembers() {
    try {
        const response = await fetch('/api/members');
        if (!response.ok) throw new Error('Failed to fetch members');

        allMembers = await response.json();
        populateMemberDropdown();
    } catch (error) {
        console.error('Error loading members:', error);
    }
}

// Populate member dropdown
function populateMemberDropdown() {
    if (!memberIdChoices) return;
    memberIdChoices.setChoices(
        [
            { value: '', label: 'No member linked', placeholder: true },
            ...allMembers.map(m => ({ value: String(m.id), label: `${m.name} (${m.rank})` })),
        ],
        'value', 'label', true
    );
}

// Show Create User Modal
function showCreateUserModal() {
    currentEditUserId = null;
    document.getElementById('modal-title').textContent = 'Create New User';
    document.getElementById('user-form').reset();
    document.getElementById('user-id').value = '';
    memberIdChoices.setChoiceByValue('');

    // Default to true for new users
    document.getElementById('force-password-change').checked = true;

    document.getElementById('password-group').style.display = 'block';
    document.getElementById('password').required = true;
    const userModal = document.getElementById('user-modal');
    userModal.style.display = 'flex';
    trapFocus(userModal);
}

// Edit User
function editUser(userId) {
    currentEditUserId = userId;
    const user = allUsers.find(u => u.id === userId);
    if (!user) return;

    document.getElementById('modal-title').textContent = 'Edit User';
    document.getElementById('user-id').value = user.id;
    document.getElementById('username').value = user.username;
    memberIdChoices.setChoiceByValue(user.member_id ? String(user.member_id) : '');
    document.getElementById('is-admin').checked = user.is_admin;

    // Check the box if the user is currently flagged in the DB
    document.getElementById('force-password-change').checked = user.force_password_change;

    document.getElementById('password-group').style.display = 'none';
    document.getElementById('password').required = false;
    const userModal = document.getElementById('user-modal');
    userModal.style.display = 'flex';
    trapFocus(userModal);
}

// Close User Modal
function closeUserModal() {
    const userModal = document.getElementById('user-modal');
    releaseFocus(userModal);
    userModal.style.display = 'none';
    currentEditUserId = null;
}

// Save User (Create or Update)
async function saveUser(event) {
    event.preventDefault();

    const userId = document.getElementById('user-id').value;
    const username = document.getElementById('username').value;
    const password = document.getElementById('password').value;
    const memberIdValue = document.getElementById('member-id').value;
    const isAdmin = document.getElementById('is-admin').checked;
    const forcePasswordChange = document.getElementById('force-password-change').checked;

    const userData = {
        username,
        member_id: memberIdValue ? parseInt(memberIdValue) : null,
        is_admin: isAdmin,
        force_password_change: forcePasswordChange
    };

    // Add password for new users
    if (!userId) {
        userData.password = password;
    }

    try {
        let response;
        if (userId) {
            // Update existing user
            response = await fetch(`/api/admin/users/${userId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(userData)
            });
        } else {
            // Create new user
            response = await fetch('/api/admin/users', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(userData)
            });
        }

        if (!response.ok) {
            const error = await response.text();
            throw new Error(error);
        }

        const result = await response.json();
        alert(result.message);
        closeUserModal();
        loadUsers();
    } catch (error) {
        alert('Error: ' + error.message);
    }
}

// Delete User (With Safeguards)
let pendingDeleteUserId = null;
let pendingDeleteUsername = null;

async function deleteUser(userId, username) {
    try {
        const checkRes = await fetch(`/api/admin/users/${userId}/file-count`);
        if (!checkRes.ok) throw new Error('Failed to check user files');
        const data = await checkRes.json();

        if (data.count > 0) {
            pendingDeleteUserId = userId;
            pendingDeleteUsername = username;
            document.getElementById('transfer-username').textContent = username;
            document.getElementById('transfer-file-count').textContent = data.count;

            newOwnerChoices.setChoices(
                allUsers
                    .filter(u => u.id !== userId)
                    .map(u => ({ value: String(u.id), label: u.username })),
                'value', 'label', true
            );

            const transferModal = document.getElementById('transfer-files-modal');
            transferModal.style.display = 'flex';
            trapFocus(transferModal);
            return;
        }
    } catch (error) {
        console.error('File check error:', error);
        return;
    }

    if (!confirm(`Are you sure you want to delete user "${username}"?\n\nThis action cannot be undone.`)) return;
    executeUserDelete(userId);
}

function closeTransferFilesModal() {
    const transferModal = document.getElementById('transfer-files-modal');
    releaseFocus(transferModal);
    transferModal.style.display = 'none';
    pendingDeleteUserId = null;
}

async function confirmTransferFiles() {
    const newOwnerId = document.getElementById('new-owner-select').value;
    if (!newOwnerId) return alert('Select a new owner');

    try {
        const res = await fetch(`/api/admin/users/${pendingDeleteUserId}/transfer-files`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ new_owner_id: parseInt(newOwnerId) })
        });
        if (!res.ok) throw new Error('Failed to transfer files');

        closeTransferFilesModal();
        executeUserDelete(pendingDeleteUserId);
    } catch (error) {
        alert('Error: ' + error.message);
    }
}

async function executeUserDelete(userId) {
    try {
        const response = await fetch(`/api/admin/users/${userId}`, { method: 'DELETE' });
        if (!response.ok) throw new Error(await response.text());

        const result = await response.json();
        alert(result.message);
        loadUsers();
    } catch (error) {
        alert('Error: ' + error.message);
    }
}

// Show Reset Password Modal
function showResetPasswordModal(userId, username) {
    currentResetUserId = userId;
    document.getElementById('reset-username').textContent = username;
    document.getElementById('reset-password-info').style.display = 'block';
    document.getElementById('reset-password-result').style.display = 'none';
    document.getElementById('confirm-reset-btn').style.display = 'inline-block';
    const resetModal = document.getElementById('reset-password-modal');
    resetModal.style.display = 'flex';
    trapFocus(resetModal);
}

// Close Reset Password Modal
function closeResetPasswordModal() {
    const resetModal = document.getElementById('reset-password-modal');
    releaseFocus(resetModal);
    resetModal.style.display = 'none';
    currentResetUserId = null;
}

// Confirm Reset Password
async function confirmResetPassword() {
    try {
        const response = await fetch(`/api/admin/users/${currentResetUserId}/reset-password`, {
            method: 'POST'
        });

        if (!response.ok) {
            const error = await response.text();
            throw new Error(error);
        }

        const result = await response.json();

        // Show the result
        document.getElementById('result-username').textContent = result.username;
        document.getElementById('result-password').textContent = result.password;
        document.getElementById('reset-password-info').style.display = 'none';
        document.getElementById('confirm-reset-btn').style.display = 'none';
        document.getElementById('reset-password-result').style.display = 'block';

        loadUsers();
    } catch (error) {
        alert('Error: ' + error.message);
    }
}

// Copy Password
function copyPassword() {
    const password = document.getElementById('result-password').textContent;
    navigator.clipboard.writeText(password).then(() => {
        alert('Password copied to clipboard!');
    });
}

// Load Login History
async function loadLoginHistory() {
    const userId = document.getElementById('login-filter').value;
    const limit = document.getElementById('login-limit').value;

    let url = `/api/admin/login-history?limit=${limit}`;
    if (userId) {
        url += `&user_id=${userId}`;
    }

    try {
        const response = await fetch(url);
        if (!response.ok) throw new Error('Failed to fetch login history');

        allLogins = await response.json();
        displayLoginHistory(allLogins);
        displayLoginStats(allLogins);

        // Populate user filter if not already done
        if (document.getElementById('login-filter').options.length === 1) {
            populateLoginFilter();
        }
    } catch (error) {
        console.error('Error loading login history:', error);
        const div = document.createElement('div');
        div.className = 'error-message';
        div.textContent = `Failed to load login history: ${error.message}`;
        document.getElementById('logins-list').replaceChildren(div);
    }
}

// Populate login filter
function populateLoginFilter() {
    if (!loginFilterChoices) return;
    const uniqueUsers = [...new Map(allLogins.map(l => [l.user_id, l])).values()];
    loginFilterChoices.setChoices(
        [
            { value: '', label: 'All Users', placeholder: true },
            ...uniqueUsers.map(login => ({ value: String(login.user_id), label: login.username })),
        ],
        'value', 'label', true
    );
}

function buildStatCard(icon, value, label) {
    const card = document.createElement('div');
    card.className = 'stat-card';

    const iconDiv = document.createElement('div');
    iconDiv.className = 'stat-icon';
    iconDiv.textContent = icon;

    const info = document.createElement('div');
    info.className = 'stat-info';

    const valDiv = document.createElement('div');
    valDiv.className = 'stat-value';
    valDiv.textContent = value;

    const labelDiv = document.createElement('div');
    labelDiv.className = 'stat-label';
    labelDiv.textContent = label;

    info.append(valDiv, labelDiv);
    card.append(iconDiv, info);
    return card;
}

// Display Login Stats
function displayLoginStats(logins) {
    const statsDiv = document.getElementById('login-stats');
    if (!statsDiv) return;

    const successLogins = logins.filter(l => l.success).length;
    const failedLogins = logins.filter(l => !l.success).length;
    const uniqueUsers = new Set(logins.map(l => l.user_id)).size;
    const uniqueIPs = new Set(logins.map(l => l.ip_address).filter(Boolean)).size;

    statsDiv.replaceChildren(
        buildStatCard('✅', successLogins, 'Successful Logins'),
        buildStatCard('❌', failedLogins, 'Failed Attempts'),
        buildStatCard('👥', uniqueUsers, 'Unique Users'),
        buildStatCard('🌐', uniqueIPs, 'Unique IPs')
    );
}

function buildLoginRow(login) {
    const status = login.success ? '✅' : '❌';
    const statusClass = login.success ? 'success' : 'failed';
    const time = new Date(login.login_time).toLocaleString();
    const location = [login.city, login.country].filter(v => v).join(', ') || 'Unknown';
    const ip = login.ip_address || 'N/A';
    const isp = login.isp || 'Unknown';
    const device = extractDeviceInfo(login.user_agent);

    const tr = document.createElement('tr');
    tr.className = `login-row ${statusClass}`;

    const tdStatus = document.createElement('td');
    const statusBadge = document.createElement('span');
    statusBadge.className = `status-badge ${statusClass}`;
    statusBadge.textContent = status;
    tdStatus.appendChild(statusBadge);

    const tdUser = document.createElement('td');
    const strong = document.createElement('strong');
    strong.textContent = login.username;
    tdUser.appendChild(strong);

    const tdTime = document.createElement('td');
    tdTime.textContent = time;

    const tdLocation = document.createElement('td');
    tdLocation.textContent = `📍 ${location}`;

    const tdIP = document.createElement('td');
    const code = document.createElement('code');
    code.textContent = ip;
    tdIP.appendChild(code);

    const tdISP = document.createElement('td');
    tdISP.textContent = isp;

    const tdDevice = document.createElement('td');
    tdDevice.className = 'device-info';
    tdDevice.textContent = device;

    tr.append(tdStatus, tdUser, tdTime, tdLocation, tdIP, tdISP, tdDevice);
    return tr;
}

// Display Login History
function displayLoginHistory(logins) {
    const loginsList = document.getElementById('logins-list');
    if (!loginsList) return;

    if (logins.length === 0) {
        const div = document.createElement('div');
        div.className = 'empty-state';
        div.textContent = 'No login history found';
        loginsList.replaceChildren(div);
        return;
    }

    const table = document.createElement('table');
    table.className = 'login-table';

    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    ['Status', 'Username', 'Date & Time', 'Location', 'IP Address', 'ISP', 'Device'].forEach(text => {
        const th = document.createElement('th');
        th.textContent = text;
        headerRow.appendChild(th);
    });
    thead.appendChild(headerRow);

    const tbody = document.createElement('tbody');
    logins.forEach(login => tbody.appendChild(buildLoginRow(login)));

    table.append(thead, tbody);
    loginsList.replaceChildren(table);
}

// Extract device info from user agent
function extractDeviceInfo(userAgent) {
    if (!userAgent) return 'Unknown';

    const ua = userAgent.toLowerCase();
    let device = '';
    let browser = '';

    // Device detection
    if (ua.includes('mobile') || ua.includes('android') || ua.includes('iphone')) {
        device = '📱 Mobile';
    } else if (ua.includes('tablet') || ua.includes('ipad')) {
        device = '📱 Tablet';
    } else {
        device = '💻 Desktop';
    }

    // Browser detection
    if (ua.includes('edge')) browser = 'Edge';
    else if (ua.includes('chrome')) browser = 'Chrome';
    else if (ua.includes('firefox')) browser = 'Firefox';
    else if (ua.includes('safari')) browser = 'Safari';
    else browser = 'Other';

    return `${device} • ${browser}`;
}

// Close modals when clicking outside
window.onclick = function(event) {
    if (event.target.classList.contains('modal')) {
        event.target.style.display = 'none';
    }
};

// --- SECURITY & API TAB LOGIC ---

// Hook into the switchTab function
const originalSwitchTab = switchTab;
switchTab = function(tabName) {
    originalSwitchTab(tabName);
    if (tabName === 'security') {
        loadSecuritySettings();
    }
};

async function loadSecuritySettings() {
    try {
        const response = await fetch('/api/settings');
        if (!response.ok) return;

        const settings = await response.json();

        // Explicitly map all integer fields (fallback to safe defaults if db is empty)
        document.getElementById('pwd-min-length').value = settings.pwd_min_length || 12;
        document.getElementById('pwd-history-count').value = settings.pwd_history_count !== undefined ? settings.pwd_history_count : 4;
        document.getElementById('pwd-validity-days').value = settings.pwd_validity_days !== undefined ? settings.pwd_validity_days : 180;

        // Explicitly map all boolean checkboxes
        document.getElementById('pwd-require-special').checked = !!settings.pwd_require_special;
        document.getElementById('pwd-require-upper').checked = !!settings.pwd_require_upper;
        document.getElementById('pwd-require-lower').checked = !!settings.pwd_require_lower;
        document.getElementById('pwd-require-number').checked = !!settings.pwd_require_number;

        // Map the new CV Worker URL
        const cvInput = document.getElementById('cv-worker-url');
        if (cvInput) cvInput.value = settings.cv_worker_url || '';

        // Disable the delete button if no GCP key exists
        const deleteBtn = document.getElementById('delete-gcp-btn');
        if (deleteBtn) {
            if (settings.has_gcp_credentials) {
                deleteBtn.disabled = false;
                deleteBtn.textContent = '🗑️ Delete Active Key';
            } else {
                deleteBtn.disabled = true;
                deleteBtn.textContent = '❌ No Key Configured';
            }
        }
    } catch (error) {
        console.error('Error loading security settings:', error);
    }
}

// Save CV Worker URL
async function saveCVWorkerUrl(event) {
    event.preventDefault();
    const url = document.getElementById('cv-worker-url').value;

    try {
        const response = await fetch('/api/admin/security/cv-worker', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ cv_worker_url: url })
        });

        if (!response.ok) throw new Error(await response.text());
        alert("Microservice routing updated successfully.");
    } catch (error) {
        alert("Error: " + error.message);
    }
}

async function savePasswordPolicy(event) {
    event.preventDefault();
    const payload = {
        pwd_min_length: parseInt(document.getElementById('pwd-min-length').value),
        pwd_history_count: parseInt(document.getElementById('pwd-history-count').value),
        pwd_validity_days: parseInt(document.getElementById('pwd-validity-days').value),
        pwd_require_special: document.getElementById('pwd-require-special').checked,
        pwd_require_upper: document.getElementById('pwd-require-upper').checked,
        pwd_require_lower: document.getElementById('pwd-require-lower').checked,
        pwd_require_number: document.getElementById('pwd-require-number').checked
    };

    try {
        const response = await fetch('/api/admin/security/password-policy', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!response.ok) throw new Error(await response.text());
        alert("Password Policy updated successfully.");
    } catch (error) {
        alert("Error: " + error.message);
    }
}

// GCP File Upload Reader
async function uploadGCPCredentials(event) {
    event.preventDefault();
    const fileInput = document.getElementById('gcp-json-file');

    if (fileInput.files.length === 0) {
        alert("Please select a JSON file.");
        return;
    }

    const file = fileInput.files[0];
    const reader = new FileReader();

    reader.onload = async function(e) {
        const jsonContent = e.target.result;

        try {
            // Verify it's actually valid JSON before sending
            JSON.parse(jsonContent);

            const response = await fetch('/api/admin/credentials', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    service_name: 'gcp_vision',
                    secret: jsonContent
                })
            });

            if (!response.ok) throw new Error(await response.text());

            alert("✅ GCP Credentials securely encrypted and stored.");
            document.getElementById('gcp-upload-form').reset();

        } catch (error) {
            alert("Invalid JSON or Upload Error: " + error.message);
        }
    };

    reader.readAsText(file);
}

// GCP Deletion Modal Logic
function showDeleteGCPModal() {
    const gcpModal = document.getElementById('delete-gcp-modal');
    gcpModal.style.display = 'flex';
    trapFocus(gcpModal);
}

function closeDeleteGCPModal() {
    const gcpModal = document.getElementById('delete-gcp-modal');
    releaseFocus(gcpModal);
    gcpModal.style.display = 'none';
}

async function confirmDeleteGCP() {
    try {
        const response = await fetch('/api/admin/credentials/gcp_vision', {
            method: 'DELETE'
        });

        if (!response.ok) throw new Error(await response.text());

        alert("🗑️ Credentials deleted. OCR Pipelines disabled.");
        closeDeleteGCPModal();
    } catch (error) {
        alert("Error: " + error.message);
    }
}

// ── Advanced Settings: Storm Slot Times ───────────────────────────────────────

async function loadStormSlots() {
    const container = document.getElementById('storm-slots-rows');
    if (!container) return;

    let slots = [];
    try {
        const res = await fetch('/api/storm/slot-times');
        if (res.ok) slots = await res.json();
    } catch { /* ignore */ }

    container.replaceChildren();

    if (!slots.length) {
        const p = document.createElement('p');
        p.className = 'help-text';
        p.textContent = 'No slot data.';
        container.appendChild(p);
        return;
    }

    slots.forEach(slot => {
        const row = document.createElement('div');
        row.style.cssText = 'display:grid;grid-template-columns:60px 1fr 100px;gap:8px;align-items:center;margin-bottom:6px;';

        const numLabel = document.createElement('span');
        numLabel.textContent = 'Slot ' + slot.slot;
        numLabel.style.fontWeight = '600';
        row.appendChild(numLabel);

        const labelInput = document.createElement('input');
        labelInput.type = 'text';
        labelInput.id = 'storm-slot-label-' + slot.slot;
        labelInput.className = 'form-input';
        labelInput.value = slot.label || '';
        labelInput.maxLength = 40;
        labelInput.placeholder = 'e.g. Early';
        row.appendChild(labelInput);

        const timeInput = document.createElement('input');
        timeInput.type = 'text';
        timeInput.id = 'storm-slot-time-' + slot.slot;
        timeInput.className = 'form-input';
        timeInput.value = slot.time_st || '00:00';
        timeInput.maxLength = 5;
        timeInput.placeholder = 'HH:MM';
        row.appendChild(timeInput);

        container.appendChild(row);
    });
}

async function saveStormSlots() {
    const statusEl = document.getElementById('storm-slots-status');
    if (!statusEl) return;

    const slots = [1, 2, 3].map(n => ({
        slot:    n,
        label:   (document.getElementById('storm-slot-label-' + n)?.value || '').trim(),
        time_st: (document.getElementById('storm-slot-time-' + n)?.value || '00:00').trim(),
    }));

    statusEl.textContent = '';

    try {
        const res = await fetch('/api/admin/advanced/storm-slots', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ slots }),
        });
        if (!res.ok) {
            const msg = await res.text();
            statusEl.textContent = msg || 'Save failed';
            statusEl.style.color = 'var(--danger-color, #e74c3c)';
            return;
        }
        statusEl.textContent = 'Saved';
        statusEl.style.color = 'var(--success-color, #27ae60)';
        setTimeout(() => { statusEl.textContent = ''; }, 3000);
    } catch {
        statusEl.textContent = 'Network error';
        statusEl.style.color = 'var(--danger-color, #e74c3c)';
    }
}
