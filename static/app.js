const API_URL = '/api/members';

let editingMemberId = null;
let currentUsername = '';
let canManageRanks = false;
let isR5OrAdmin = false;
let isAdmin = false;
let allMembers = []; 
let isPowerTrackingEnabled = false; 

// Fetch permissions to determine if Edit/Delete buttons should render
async function fetchPermissions() {
    try {
        const response = await fetch('/api/check-auth');
        if (response.ok) {
            const data = await response.json();
            currentUsername = data.username;
            canManageRanks = data.can_manage_ranks || false;
            isR5OrAdmin = data.is_r5_or_admin || false;
            isAdmin = data.is_admin || false;
        }
    } catch (error) {
        console.error('Auth check error:', error);
    }
}

async function fetchSettings() {
    try {
        const response = await fetch('/api/settings');
        if (response.ok) {
            const settings = await response.json();
            isPowerTrackingEnabled = settings.power_tracking_enabled === true;
        }
    } catch (error) {
        console.error('Error fetching settings:', error);
    }
}

function updateDisplayedMembers() {
    // 1. Grab current values from all inputs
    const searchTerm = (document.getElementById('search-input')?.value || '').toLowerCase().trim();
    const eligibleOnly = document.getElementById('filter-eligible')?.checked || false;
    const sortBy = document.getElementById('sort-by')?.value || 'name-asc';

    // NEW: Grab an array of the 'data-rank' values from all active chips
    const activeRankChips = Array.from(document.querySelectorAll('.rank-chip.active')).map(chip => chip.dataset.rank);

    // 2. Apply Filters
    let filtered = allMembers.filter(member => {
        const matchesSearch = member.name.toLowerCase().includes(searchTerm) || member.rank.toLowerCase().includes(searchTerm);
        
        // Match if "all" is active, OR if the member's rank is in our list of active chips
        const matchesRank = activeRankChips.includes('all') || activeRankChips.includes(member.rank);
        
        const matchesEligible = !eligibleOnly || member.eligible !== false; 

        return matchesSearch && matchesRank && matchesEligible;
    });

    // 3. Apply Sorting
    filtered.sort((a, b) => {
        if (sortBy === 'name-asc') {
            return a.name.localeCompare(b.name);
        } else if (sortBy === 'name-desc') {
            return b.name.localeCompare(a.name);
        } else if (sortBy === 'power-desc') {
            return (b.power || 0) - (a.power || 0);
        } else if (sortBy === 'power-asc') {
            return (a.power || 0) - (b.power || 0);
        } else if (sortBy === 'rank-desc') {
            // Map ranks to numeric values for correct Last War sorting
            const rankOrder = { 'R5': 5, 'R4': 4, 'R3': 3, 'R2': 2, 'R1': 1 };
            const rankA = rankOrder[a.rank] || 0;
            const rankB = rankOrder[b.rank] || 0;
            if (rankA !== rankB) return rankB - rankA;
            return a.name.localeCompare(b.name); // Tie-breaker: Name
        }
        return 0;
    });

    // 4. Update the UI
    displayMembers(filtered);
    updateMemberCount(filtered.length);

    // Toggle the clear search 'X' button
    const clearBtn = document.getElementById('clear-search');
    if (clearBtn) clearBtn.style.display = searchTerm ? 'flex' : 'none';
}

document.addEventListener('DOMContentLoaded', async () => {
    await fetchPermissions();
    await fetchSettings();

    // Hide power sorting options if power tracking is disabled
    document.querySelectorAll('.power-sort-option').forEach(el => {
        el.style.display = isPowerTrackingEnabled ? 'block' : 'none';
    });
    
    // Hide action bar if user lacks permissions
    const actionBar = document.querySelector('.action-bar');
    if (!canManageRanks && actionBar) {
        actionBar.style.display = 'none';
        const notice = document.createElement('div');
        notice.className = 'permission-notice';
        notice.innerHTML = '<p>ℹ️ Only R4 and R5 members can add or manage member ranks.</p>';
        document.querySelector('main').insertBefore(notice, document.querySelector('.members-section'));
    }

    setupModalListeners();
    setupCSVImport();
    setupSearch();
    loadMembers();
});

function setupModalListeners() {
    const memberModal = document.getElementById('member-modal');
    const closeMemberModal = document.getElementById('close-member-modal');
    const addMemberBtn = document.getElementById('add-member-btn');
    const cancelBtn = document.getElementById('cancel-btn');
    const importCsvTriggerBtn = document.getElementById('import-csv-trigger-btn');
    const csvImportSection = document.getElementById('csv-import-section');
    const cancelImportBtn = document.getElementById('cancel-import-btn');
    const memberForm = document.getElementById('member-form');

    if (addMemberBtn) addMemberBtn.addEventListener('click', () => openMemberModal(false));
    if (closeMemberModal) closeMemberModal.addEventListener('click', closeMemberModalFunc);
    if (cancelBtn) cancelBtn.addEventListener('click', closeMemberModalFunc);

    if (importCsvTriggerBtn && csvImportSection) {
        importCsvTriggerBtn.addEventListener('click', () => {
            csvImportSection.style.display = 'block';
            csvImportSection.scrollIntoView({ behavior: 'smooth' });
        });
    }

    if (cancelImportBtn && csvImportSection) {
        cancelImportBtn.addEventListener('click', () => {
            csvImportSection.style.display = 'none';
            document.getElementById('csv-file').value = '';
            document.getElementById('import-result').style.display = 'none';
        });
    }

    window.addEventListener('click', (event) => {
        if (event.target === memberModal) closeMemberModalFunc();
    });

    if (memberForm) memberForm.addEventListener('submit', handleMemberFormSubmit);
}

function openMemberModal(editing = false) {
    if (!canManageRanks) {
        alert('You do not have permission to manage members. Only R4 and R5 can do this.');
        return;
    }
    const memberModal = document.getElementById('member-modal');
    if (memberModal) {
        memberModal.style.display = 'flex';
        document.getElementById('member-name').focus();
    }
}

function closeMemberModalFunc() {
    const memberModal = document.getElementById('member-modal');
    if (memberModal) {
        memberModal.style.display = 'none';
        resetMemberForm();
    }
}

function resetMemberForm() {
    editingMemberId = null;
    document.getElementById('member-form').reset();
    document.getElementById('member-eligible').checked = true;
    
    // Clear the power input and timestamp
    const powerInput = document.getElementById('modal-member-power');
    if (powerInput) powerInput.value = '';
    const timestampText = document.getElementById('modal-power-timestamp');
    if (timestampText) timestampText.textContent = '';

    const powerSection = document.getElementById('modal-power-section');
    if (powerSection) {
        powerSection.style.display = isPowerTrackingEnabled ? 'block' : 'none';
    }
    
    document.getElementById('modal-form-title').textContent = 'Add New Member';
    document.getElementById('submit-btn').textContent = 'Add Member';
}

async function loadMembers() {
    try {
        const response = await fetch(API_URL);
        const members = await response.json();
        allMembers = members; 
        
        // REPLACED: displayMembers(members) and updateMemberCount(members.length)
        // Calling our new master function ensures filters/sorts stay active after saving!
        updateDisplayedMembers();
    } catch (error) {
        console.error('Error loading members:', error);
        const membersList = document.getElementById('members-list');
        if (membersList) membersList.innerHTML = '<p class="empty">Error loading members. Please try again.</p>';
    }
}

function displayMembers(members) {
    const membersList = document.getElementById('members-list');
    if (!membersList) return;
    
    if (!members || members.length === 0) {
        membersList.innerHTML = '<p class="empty">No members yet. Add your first alliance member!</p>';
        return;
    }

    membersList.innerHTML = members.map(member => {
        const eligibleStatus = member.eligible !== false ? '✓ Eligible' : '✗ Not Eligible';
        const eligibleClass = member.eligible !== false ? 'eligible' : 'not-eligible';
        
        let powerDisplay = '';
        if (isPowerTrackingEnabled && member.power) {
            powerDisplay = `<span class="member-power" title="${member.power.toLocaleString()}">${formatPower(member.power)}</span>`;
        }

        let actionsHtml = '';
        if (canManageRanks) {
            actionsHtml = `
                <div class="member-actions">
                    <button class="edit-btn" onclick="editMember(${member.id}, '${escapeHtml(member.name)}', '${escapeHtml(member.rank)}', ${member.eligible !== false}, ${member.power || 0}, '${member.power_updated_at || ''}')">Edit</button>
                    <button class="delete-btn" onclick="deleteMember(${member.id}, '${escapeHtml(member.name)}', ${member.has_user})">Delete</button>
                    ${(isR5OrAdmin && !member.has_user) ? `<button class="create-user-btn" onclick="createUserForMember(${member.id}, '${escapeHtml(member.name)}')">Create User</button>` : ''}
                    <button class="toggle-eligible-btn ${eligibleClass}" onclick="toggleEligible(${member.id}, ${member.eligible !== false})">${eligibleStatus}</button>
                </div>
            `;
        }
        
        return `
            <div class="member-card">
                <div class="member-info">
                    <div class="member-name">${escapeHtml(member.name)}</div>
                    <span class="member-rank rank-${member.rank.replace(/\s+/g, '-')}">${escapeHtml(member.rank)}</span>
                    ${powerDisplay}
                    <span class="member-eligible ${eligibleClass}">${eligibleStatus}</span>
                </div>
                ${actionsHtml}
            </div>
        `;
    }).join('');
}

async function handleMemberFormSubmit(e) {
    e.preventDefault();
    if (!canManageRanks) {
        alert('You do not have permission to manage members. Only R4 and R5 can do this.');
        return;
    }
    
    const name = document.getElementById('member-name').value.trim();
    const rank = document.getElementById('member-rank').value;
    const eligible = document.getElementById('member-eligible').checked;
    
    // Grab the power value. If the box is empty, explicitly set it to 0 to clear it out.
    const powerInput = document.getElementById('modal-member-power');
    const power = (powerInput && powerInput.value !== '') ? parseInt(powerInput.value, 10) : 0;

    if (!name || !rank) {
        alert('Please fill in all fields');
        return;
    }

    try {
        // NEW: Include power in both the PUT and POST body payloads
        if (editingMemberId) {
            const response = await fetch(`${API_URL}/${editingMemberId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, rank, eligible, power }),
            });
            if (!response.ok) throw new Error('Failed to update member');
            editingMemberId = null;
        } else {
            const response = await fetch(API_URL, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, rank, eligible, power }),
            });
            if (!response.ok) {
                if (response.status === 403) throw new Error('Permission denied: Only R4/R5 members can manage ranks');
                throw new Error('Failed to add member');
            }
        }
        closeMemberModalFunc();
        await loadMembers();
    } catch (error) {
        console.error('Error saving member:', error);
        alert('Failed to save member. Please try again.');
    }
}

// NEW: Added power and powerUpdatedAt to the function parameters
window.editMember = function(id, name, rank, eligible, power = 0, powerUpdatedAt = '') {
    if (!canManageRanks) {
        alert('You do not have permission to edit members.');
        return;
    }
    editingMemberId = id;
    document.getElementById('member-name').value = name;
    document.getElementById('member-rank').value = rank;
    document.getElementById('member-eligible').checked = eligible;
    
    const powerSection = document.getElementById('modal-power-section');
    if (powerSection) {
        powerSection.style.display = isPowerTrackingEnabled ? 'block' : 'none';
    }
    
    // Populate Power Input
    const powerInput = document.getElementById('modal-member-power');
    if (powerInput) {
        // If power is 0, leave the box blank for a cleaner UI
        powerInput.value = (power && power > 0) ? power : '';
    }

    // Format and display the timestamp
    const timestampText = document.getElementById('modal-power-timestamp');
    if (timestampText) {
        if (powerUpdatedAt) {
            // Replace space with T and append Z to force JS to read SQLite's CURRENT_TIMESTAMP as UTC
            const formattedDateStr = powerUpdatedAt.replace(' ', 'T') + "Z";
            const updatedDate = new Date(formattedDateStr);
            const options = { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' };
            timestampText.textContent = `Last updated: ${updatedDate.toLocaleDateString(undefined, options)}`;
        } else {
            timestampText.textContent = "Last updated: Never";
        }
    }

    document.getElementById('modal-form-title').textContent = 'Edit Member';
    document.getElementById('submit-btn').textContent = 'Update Member';
    openMemberModal(true);
};

window.deleteMember = async function(id, name, hasUser = false) {
    // Build the confirmation message dynamically
    let confirmMessage = `Are you sure you want to delete ${name} from the roster?`;
    if (hasUser) {
        confirmMessage += `\n\n⚠️ WARNING: This will also permanently delete their login account!`;
    }

    if (!confirm(confirmMessage)) return;

    try {
        const response = await fetch(`${API_URL}/${id}`, {
            method: 'DELETE'
        });
        if (!response.ok) throw new Error('Failed to delete member');
        await loadMembers();
    } catch (error) {
        console.error('Error:', error);
        alert('Failed to delete member. Please try again.');
    }
};

window.toggleEligible = async function(id, currentStatus) {
    if (!canManageRanks) {
        alert('You do not have permission to manage members.');
        return;
    }
    const newStatus = !currentStatus;
    const statusText = newStatus ? 'eligible' : 'not eligible';
    if (!confirm(`Mark this member as ${statusText} for train scheduling?`)) return;
    
    try {
        const response = await fetch(`${API_URL}`);
        if (!response.ok) throw new Error('Failed to fetch members');
        const members = await response.json();
        const member = members.find(m => m.id === id);
        if (!member) throw new Error('Member not found');
        
        const updateResponse = await fetch(`${API_URL}/${id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: member.name, rank: member.rank, eligible: newStatus }),
        });
        
        if (!updateResponse.ok) throw new Error('Failed to update member');
        loadMembers();
    } catch (error) {
        console.error('Error toggling eligibility:', error);
        alert('Failed to update member eligibility: ' + error.message);
    }
};

window.createUserForMember = async function(memberId, memberName) {
    if (!confirm(`Create a user account for ${memberName}? A random password will be generated.`)) return;
    try {
        const response = await fetch(`${API_URL}/${memberId}/create-user`, { method: 'POST' });
        if (!response.ok) {
            const error = await response.text();
            throw new Error(error);
        }
        const result = await response.json();
        alert(`User created successfully!\n\nUsername: ${result.username}\nPassword: ${result.password}\n\n⚠️ IMPORTANT: Save this password now! It won't be shown again.`);
        
        await loadMembers(); 

    } catch (error) {
        console.error('Error creating user:', error);
        alert('Failed to create user: ' + error.message);
    }
};

function formatPower(power) {
    if (!power) return '';
    if (power >= 1000000000) return '⚡ ' + (power / 1000000000).toFixed(2) + 'B';
    if (power >= 1000000) return '⚡ ' + (power / 1000000).toFixed(2) + 'M';
    if (power >= 1000) return '⚡ ' + (power / 1000).toFixed(1) + 'K';
    return '⚡ ' + power.toString();
}

function updateMemberCount(count) {
    const heading = document.querySelector('.members-section h3');
    if (heading) heading.textContent = `Alliance Members (${count})`;
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function setupSearch() {
    const searchInput = document.getElementById('search-input');
    const clearBtn = document.getElementById('clear-search');
    const eligibleFilter = document.getElementById('filter-eligible');
    const sortDropdown = document.getElementById('sort-by');
    const rankChips = document.querySelectorAll('.rank-chip');

    // Search Input Listener
    if (searchInput) {
        searchInput.addEventListener('input', updateDisplayedMembers);
    }
    
    // Clear Search Listener
    if (clearBtn) {
        clearBtn.addEventListener('click', () => {
            searchInput.value = '';
            updateDisplayedMembers();
            searchInput.focus();
        });
    }

    // NEW: Rank Chip Listeners for multi-select
    rankChips.forEach(chip => {
        chip.addEventListener('click', (e) => {
            const clickedRank = e.target.dataset.rank;
            
            if (clickedRank === 'all') {
                // If "All" is clicked, turn everything else off and turn "All" on
                rankChips.forEach(c => c.classList.remove('active'));
                e.target.classList.add('active');
            } else {
                // If a specific rank is clicked, turn "All" off
                document.querySelector('.rank-chip[data-rank="all"]').classList.remove('active');
                
                // Toggle the clicked chip on/off
                e.target.classList.toggle('active');
                
                // Safety net: If they unclick everything, automatically turn "All" back on
                const activeRanks = document.querySelectorAll('.rank-chip.active');
                if (activeRanks.length === 0) {
                    document.querySelector('.rank-chip[data-rank="all"]').classList.add('active');
                }
            }
            // Trigger the UI update
            updateDisplayedMembers();
        });
    });

    // Dropdown and Checkbox Listeners
    if (eligibleFilter) eligibleFilter.addEventListener('change', updateDisplayedMembers);
    if (sortDropdown) sortDropdown.addEventListener('change', updateDisplayedMembers);
}

// --- CSV Import ---
let detectedCSVMembers = [];
let selectedCSVMembers = new Set();
let membersToRemove = [];
let selectedRemoveMembers = new Set();

function setupCSVImport() {
    const importBtn = document.getElementById('import-btn');
    const fileInput = document.getElementById('csv-file');
    const modal = document.getElementById('csv-preview-modal');
    const closeModal = document.getElementById('close-csv-modal');
    const confirmBtn = document.getElementById('confirm-csv-btn');
    const cancelBtn = document.getElementById('cancel-csv-btn');
    
    if (!importBtn || !fileInput) return;
    
    importBtn.addEventListener('click', async () => {
        if (!canManageRanks) {
            alert('You do not have permission to import members. Only R4 and R5 can do this.');
            return;
        }
        const file = fileInput.files[0];
        if (!file || !file.name.endsWith('.csv')) {
            alert('Please select a valid CSV file');
            return;
        }
        
        const formData = new FormData();
        formData.append('file', file);
        importBtn.disabled = true;
        importBtn.textContent = 'Loading...';
        
        try {
            const response = await fetch('/api/members/import', { method: 'POST', body: formData });
            if (!response.ok) {
                if (response.status === 403) throw new Error('Permission denied: Only R4/R5 members can import members');
                const errorText = await response.text();
                throw new Error(errorText || 'Failed to read CSV');
            }
            
            const result = await response.json();
            if (result.errors && result.errors.length > 0) displayImportError('CSV contains errors:\n' + result.errors.join('\n'));
            
            if (result.detected_members && result.detected_members.length > 0) {
                detectedCSVMembers = result.detected_members;
                selectedCSVMembers = new Set(result.detected_members.map((m, i) => i));
                membersToRemove = result.members_to_remove || [];
                selectedRemoveMembers = new Set();
                showCSVPreview(result);
                modal.style.display = 'block';
            } else {
                displayImportError('No valid members found in CSV file');
            }
        } catch (error) {
            console.error('Import error:', error);
            displayImportError(error.message);
        } finally {
            importBtn.disabled = false;
            importBtn.textContent = 'Preview CSV';
        }
    });
    
    closeModal.addEventListener('click', () => modal.style.display = 'none');
    cancelBtn.addEventListener('click', () => modal.style.display = 'none');
    
    confirmBtn.addEventListener('click', async () => {
        const selectedMembers = detectedCSVMembers.filter((_, i) => selectedCSVMembers.has(i));
        if (selectedMembers.length === 0) {
            alert('Please select at least one member to import');
            return;
        }
        
        const renames = [];
        document.querySelectorAll('.rename-select').forEach(select => {
            if (select.value) renames.push({ old_name: select.value, new_name: select.dataset.newName });
        });
        
        const removeMemberIDs = Array.from(selectedRemoveMembers);
        if (removeMemberIDs.length > 0) {
            if (!confirm(`⚠️ WARNING: You are about to delete ${removeMemberIDs.length} member(s)!\n\nAre you sure you want to continue?`)) return;
        }
        
        confirmBtn.disabled = true;
        confirmBtn.textContent = 'Importing...';
        
        try {
            const response = await fetch('/api/members/import/confirm', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ members: selectedMembers, remove_member_ids: removeMemberIDs, renames: renames })
            });
            
            if (!response.ok) throw new Error('Failed to import members');
            const result = await response.json();
            modal.style.display = 'none';
            
            const resultDiv = document.getElementById('import-result');
            resultDiv.style.display = 'block';
            resultDiv.className = 'import-result success';
            resultDiv.innerHTML = `<strong>✓ Successfully imported ${result.added + result.updated} member(s)</strong>`;
            
            await loadMembers();
            fileInput.value = '';
        } catch (error) {
            console.error('Confirm error:', error);
            alert('Error importing members: ' + error.message);
        } finally {
            confirmBtn.disabled = false;
            confirmBtn.textContent = '✔ Confirm & Import Selected';
        }
    });
}

function showCSVPreview(result) {
    const summaryDiv = document.getElementById('csv-summary');
    const previewDiv = document.getElementById('csv-members-preview');
    
    const newCount = result.detected_members.filter(m => m.is_new).length;
    const changedCount = result.detected_members.filter(m => m.rank_changed).length;
    const unchangedCount = result.detected_members.length - newCount - changedCount;
    
    summaryDiv.innerHTML = `
        <div class="summary-stats">
            <div class="stat-item"><span class="stat-label">Total Members:</span><span class="stat-value">${result.detected_members.length}</span></div>
            <div class="stat-item"><span class="stat-label new">New Members:</span><span class="stat-value new">${newCount}</span></div>
            <div class="stat-item"><span class="stat-label change">Rank Changes:</span><span class="stat-value change">${changedCount}</span></div>
            <div class="stat-item"><span class="stat-label">No Changes:</span><span class="stat-value">${unchangedCount}</span></div>
        </div>
    `;
    
    let html = '<div class="csv-members-list">';
    result.detected_members.forEach((member, index) => {
        const statusClass = member.is_new ? 'new' : (member.rank_changed ? 'changed' : 'unchanged');
        const statusText = member.is_new ? 'NEW' : (member.rank_changed ? `${member.old_rank} → ${member.rank}` : 'No Change');
        const checked = selectedCSVMembers.has(index) ? 'checked' : '';
        
        html += `
            <div class="csv-member-item ${statusClass}">
                <input type="checkbox" class="member-checkbox" data-index="${index}" ${checked}>
                <div class="member-info">
                    <span class="member-name">${escapeHtml(member.name)}</span>
                    <span class="member-rank rank-${member.rank}">${member.rank}</span>
                    <span class="member-status">${statusText}</span>
                </div>
                ${member.similar_match && member.similar_match.length > 0 ? `
                    <div class="similar-match-notice">
                        <span class="warning-icon">⚠️</span>
                        <span>Similar name(s) found.</span>
                        <select class="rename-select" data-index="${index}" data-new-name="${escapeHtml(member.name)}">
                            <option value="">Add as new member</option>
                            ${member.similar_match.map(oldName => `<option value="${escapeHtml(oldName)}">Rename "${escapeHtml(oldName)}"</option>`).join('')}
                        </select>
                    </div>
                ` : ''}
            </div>
        `;
    });
    html += '</div>';
    previewDiv.innerHTML = html;
    
    previewDiv.querySelectorAll('.member-checkbox').forEach(checkbox => {
        checkbox.addEventListener('change', (e) => {
            const index = parseInt(e.target.dataset.index);
            e.target.checked ? selectedCSVMembers.add(index) : selectedCSVMembers.delete(index);
        });
    });
    
    const removeSection = document.getElementById('remove-members-section');
    const removeList = document.getElementById('members-to-remove-list');
    
    if (membersToRemove && membersToRemove.length > 0) {
        removeSection.style.display = 'block';
        let removeHtml = '<div class="members-to-remove-grid">';
        membersToRemove.forEach(member => {
            removeHtml += `
                <div class="remove-member-item">
                    <input type="checkbox" class="remove-checkbox" data-member-id="${member.id}">
                    <div class="remove-member-info">
                        <span class="remove-member-name">${escapeHtml(member.name)}</span>
                        <span class="member-rank rank-${member.rank}">${member.rank}</span>
                    </div>
                </div>
            `;
        });
        removeHtml += '</div>';
        removeList.innerHTML = removeHtml;
        
        removeList.querySelectorAll('.remove-checkbox').forEach(checkbox => {
            checkbox.addEventListener('change', (e) => {
                const memberId = parseInt(e.target.dataset.memberId);
                e.target.checked ? selectedRemoveMembers.add(memberId) : selectedRemoveMembers.delete(memberId);
            });
        });
    } else {
        removeSection.style.display = 'none';
    }
}

function displayImportError(message) {
    const resultDiv = document.getElementById('import-result');
    resultDiv.style.display = 'block';
    resultDiv.className = 'import-result error';
    resultDiv.innerHTML = `<strong>✗ Import failed:</strong> ${escapeHtml(message)}`;
}