let allUsers = [];
let allMembers = [];
let allLogins = [];
let currentEditUserId = null;
let currentResetUserId = null;

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    // Check if we are actually on the admin page
    const usersList = document.getElementById('users-list');
    if (usersList) {
        // Load initial data
        await loadUsers();
        await loadMembers();
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
        document.getElementById('users-list').innerHTML = `
            <div class="error-message">Failed to load users: ${error.message}</div>
        `;
    }
}

// Display Users
function displayUsers(users) {
    const usersList = document.getElementById('users-list');
    
    if (users.length === 0) {
        usersList.innerHTML = '<div class="empty-state">No users found</div>';
        return;
    }
    
    usersList.innerHTML = users.map(user => {
        const lastLogin = user.last_login ? new Date(user.last_login).toLocaleString() : 'Never';
        const memberInfo = user.member_name ? `<span class="member-badge">${user.member_name}</span>` : '<span class="no-member">No member linked</span>';
        
        let recentLoginsHTML = '';
        if (user.recent_logins && user.recent_logins.length > 0) {
            recentLoginsHTML = `
                <div class="recent-logins">
                    <strong>Recent Logins:</strong>
                    ${user.recent_logins.slice(0, 3).map(login => {
                        const location = [login.city, login.country].filter(v => v).join(', ') || 'Unknown';
                        const time = new Date(login.login_time).toLocaleString();
                        return `
                            <div class="login-entry">
                                <span class="login-location">📍 ${location}</span>
                                <span class="login-ip">${login.ip_address || 'N/A'}</span>
                                <span class="login-time">${time}</span>
                            </div>
                        `;
                    }).join('')}
                </div>
            `;
        }
        
        return `
            <div class="user-card" data-username="${user.username.toLowerCase()}">
                <div class="user-header">
                    <div class="user-info">
                        <h3>
                            ${user.username}
                            ${user.is_admin ? '<span class="admin-badge">Admin</span>' : ''}
                        </h3>
                        ${memberInfo}
                    </div>
                    <div class="user-actions">
                        <button class="btn btn-sm btn-secondary" onclick="editUser(${user.id})">✏️ Edit</button>
                        <button class="btn btn-sm btn-warning" onclick="showResetPasswordModal(${user.id}, '${user.username}')">🔑 Reset Password</button>
                        <button class="btn btn-sm btn-danger" onclick="deleteUser(${user.id}, '${user.username}')">🗑️ Delete</button>
                    </div>
                </div>
                <div class="user-stats">
                    <div class="stat">
                        <span class="stat-label">Last Login:</span>
                        <span class="stat-value">${lastLogin}</span>
                    </div>
                    <div class="stat">
                        <span class="stat-label">Total Logins:</span>
                        <span class="stat-value">${user.login_count}</span>
                    </div>
                </div>
                ${recentLoginsHTML}
            </div>
        `;
    }).join('');
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
    const select = document.getElementById('member-id');
    if(select) {
        select.innerHTML = '<option value="">No member linked</option>' + 
            allMembers.map(m => `<option value="${m.id}">${m.name} (${m.rank})</option>`).join('');
    }
}

// Show Create User Modal
function showCreateUserModal() {
    currentEditUserId = null;
    document.getElementById('modal-title').textContent = 'Create New User';
    document.getElementById('user-form').reset();
    document.getElementById('user-id').value = '';
    document.getElementById('password-group').style.display = 'block';
    document.getElementById('password').required = true;
    document.getElementById('user-modal').style.display = 'block';
}

// Edit User
function editUser(userId) {
    currentEditUserId = userId;
    const user = allUsers.find(u => u.id === userId);
    if (!user) return;
    
    document.getElementById('modal-title').textContent = 'Edit User';
    document.getElementById('user-id').value = user.id;
    document.getElementById('username').value = user.username;
    document.getElementById('member-id').value = user.member_id || '';
    document.getElementById('is-admin').checked = user.is_admin;
    document.getElementById('password-group').style.display = 'none';
    document.getElementById('password').required = false;
    document.getElementById('user-modal').style.display = 'block';
}

// Close User Modal
function closeUserModal() {
    document.getElementById('user-modal').style.display = 'none';
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
    
    const userData = {
        username,
        member_id: memberIdValue ? parseInt(memberIdValue) : null,
        is_admin: isAdmin
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

// Delete User
async function deleteUser(userId, username) {
    if (!confirm(`Are you sure you want to delete user "${username}"?\n\nThis action cannot be undone.`)) {
        return;
    }
    
    try {
        const response = await fetch(`/api/admin/users/${userId}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const error = await response.text();
            throw new Error(error);
        }
        
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
    document.getElementById('reset-password-modal').style.display = 'block';
}

// Close Reset Password Modal
function closeResetPasswordModal() {
    document.getElementById('reset-password-modal').style.display = 'none';
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
        document.getElementById('logins-list').innerHTML = `
            <div class="error-message">Failed to load login history: ${error.message}</div>
        `;
    }
}

// Populate login filter
function populateLoginFilter() {
    const select = document.getElementById('login-filter');
    const uniqueUsers = [...new Map(allLogins.map(l => [l.user_id, l])).values()];
    
    uniqueUsers.forEach(login => {
        const option = document.createElement('option');
        option.value = login.user_id;
        option.textContent = login.username;
        select.appendChild(option);
    });
}

// Display Login Stats
function displayLoginStats(logins) {
    const statsDiv = document.getElementById('login-stats');
    if(!statsDiv) return;

    const successLogins = logins.filter(l => l.success).length;
    const failedLogins = logins.filter(l => !l.success).length;
    const uniqueUsers = new Set(logins.map(l => l.user_id)).size;
    const uniqueIPs = new Set(logins.map(l => l.ip_address).filter(Boolean)).size;
    
    statsDiv.innerHTML = `
        <div class="stat-card">
            <div class="stat-icon">✅</div>
            <div class="stat-info">
                <div class="stat-value">${successLogins}</div>
                <div class="stat-label">Successful Logins</div>
            </div>
        </div>
        <div class="stat-card">
            <div class="stat-icon">❌</div>
            <div class="stat-info">
                <div class="stat-value">${failedLogins}</div>
                <div class="stat-label">Failed Attempts</div>
            </div>
        </div>
        <div class="stat-card">
            <div class="stat-icon">👥</div>
            <div class="stat-info">
                <div class="stat-value">${uniqueUsers}</div>
                <div class="stat-label">Unique Users</div>
            </div>
        </div>
        <div class="stat-card">
            <div class="stat-icon">🌐</div>
            <div class="stat-info">
                <div class="stat-value">${uniqueIPs}</div>
                <div class="stat-label">Unique IPs</div>
            </div>
        </div>
    `;
}

// Display Login History
function displayLoginHistory(logins) {
    const loginsList = document.getElementById('logins-list');
    if(!loginsList) return;
    
    if (logins.length === 0) {
        loginsList.innerHTML = '<div class="empty-state">No login history found</div>';
        return;
    }
    
    loginsList.innerHTML = `
        <table class="login-table">
            <thead>
                <tr>
                    <th>Status</th>
                    <th>Username</th>
                    <th>Date & Time</th>
                    <th>Location</th>
                    <th>IP Address</th>
                    <th>ISP</th>
                    <th>Device</th>
                </tr>
            </thead>
            <tbody>
                ${logins.map(login => {
                    const status = login.success ? '✅' : '❌';
                    const statusClass = login.success ? 'success' : 'failed';
                    const time = new Date(login.login_time).toLocaleString();
                    const location = [login.city, login.country].filter(v => v).join(', ') || 'Unknown';
                    const ip = login.ip_address || 'N/A';
                    const isp = login.isp || 'Unknown';
                    const device = extractDeviceInfo(login.user_agent);
                    
                    return `
                        <tr class="login-row ${statusClass}">
                            <td><span class="status-badge ${statusClass}">${status}</span></td>
                            <td><strong>${login.username}</strong></td>
                            <td>${time}</td>
                            <td>📍 ${location}</td>
                            <td><code>${ip}</code></td>
                            <td>${isp}</td>
                            <td class="device-info">${device}</td>
                        </tr>
                    `;
                }).join('')}
            </tbody>
        </table>
    `;
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
}