const API_URL = '/api/members';

let editingMemberId = null;
let currentUsername = '';
let canManageRanks = false;
let isR5OrAdmin = false;
let isAdmin = false;
let allMembers = []; 
let isPowerTrackingEnabled = false;
let isSquadTrackingEnabled = false; 
let currentMaxHQ = 35; 

// Define the HQ requirements for each Troop Tier
const TROOP_HQ_REQ = { 1: 1, 2: 4, 3: 6, 4: 10, 5: 14, 6: 17, 7: 20, 8: 24, 9: 27, 10: 30, 11: 35 };

// Change let canManageRanks = false; to:
let permissions = {};

async function fetchPermissions() {
    try {
        const response = await fetch('/api/check-auth');
        if (response.ok) {
            const data = await response.json();
            currentUsername = data.username;
            isAdmin = data.is_admin || false;
            permissions = data.permissions || {};
            
            // Backwards compatibility
            canManageRanks = permissions.manage_members || false; 
            isR5OrAdmin = permissions.manage_settings || false;

            // Hide Train Eligibility Filters and Modal Inputs based on matrix
            const filterEligibleWrapper = document.getElementById('filter-eligible-wrapper');
            if (filterEligibleWrapper) {
                filterEligibleWrapper.style.display = permissions.view_train ? 'flex' : 'none';
            }

            const modalEligibleWrapper = document.getElementById('modal-eligible-wrapper');
            if (modalEligibleWrapper) {
                modalEligibleWrapper.style.display = permissions.manage_train ? 'flex' : 'none';
            }
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
            isSquadTrackingEnabled = settings.squad_tracking_enabled === true;
            
            currentMaxHQ = settings.max_hq_level || 35;
            const hqInput = document.getElementById('member-level');
            if (hqInput) {
                hqInput.max = currentMaxHQ;
            }

            // Hide/Show squad sorting and filtering options globally
            document.querySelectorAll('.squad-sort-option').forEach(el => {
                el.style.display = isSquadTrackingEnabled ? 'block' : 'none';
            });

            // NEW: Dynamically hide ANY troop tier that exceeds the server's Max HQ setting
            Object.entries(TROOP_HQ_REQ).forEach(([tier, reqHQ]) => {
                const chip = document.querySelector(`.troop-chip[data-troop="${tier}"]`);
                const option = document.querySelector(`#member-troop-level option[value="${tier}"]`);
                
                if (currentMaxHQ < reqHQ) {
                    if (chip) chip.style.display = 'none';
                    if (option) option.style.display = 'none';
                } else {
                    if (chip) chip.style.display = ''; // Reset to default
                    if (option) option.style.display = ''; 
                }
            });
        }
    } catch (error) {
        console.error('Error fetching settings:', error);
    }
}

// Function to dynamically disable/enable troop options based on HQ level
window.updateTroopLevelOptions = function() {
    const hqLevel = parseInt(document.getElementById('member-level').value, 10) || 0;
    const troopSelect = document.getElementById('member-troop-level');
    
    if (!troopSelect) return;

    Array.from(troopSelect.options).forEach(option => {
        if (!option.value) return; // Skip the "None / Unknown" default option
        
        const tier = parseInt(option.value, 10);
        
        if (hqLevel >= TROOP_HQ_REQ[tier]) {
            option.disabled = false;
        } else {
            option.disabled = true;
            if (troopSelect.value == tier) {
                troopSelect.value = ""; 
            }
        }
    });
};

function updateDisplayedMembers() {
    // Grab current values from all inputs
    const searchTerm = (document.getElementById('search-input')?.value || '').toLowerCase().trim();
    const eligibleOnly = document.getElementById('filter-eligible')?.checked || false;
    const sortBy = document.getElementById('sort-by')?.value || 'name-asc';

    // Grab arrays of the active attributes
    const activeRanks = Array.from(document.querySelectorAll('.rank-chip.active')).map(c => c.dataset.rank);
    const activeProfs = Array.from(document.querySelectorAll('.prof-chip.active')).map(c => c.dataset.prof);
    const activeSquads = Array.from(document.querySelectorAll('.squad-chip.active')).map(c => c.dataset.squad);
    const activeTroops = Array.from(document.querySelectorAll('.troop-chip.active')).map(c => c.dataset.troop);

    // Apply Filters
    let filtered = allMembers.filter(member => {
        const matchesSearch = member.name.toLowerCase().includes(searchTerm) || member.rank.toLowerCase().includes(searchTerm);
        const matchesEligible = !eligibleOnly || member.eligible !== false; 

        const matchesRank = activeRanks.includes('all') || activeRanks.includes(member.rank);
        
        // Match Professions (Treat empty string or null as 'none')
        const memProf = member.profession || 'none';
        const matchesProf = activeProfs.includes('all') || activeProfs.includes(memProf);

        // Match Squads (Treat empty string as 'none')
        const memSquad = member.squad_type || 'none';
        const matchesSquad = activeSquads.includes('all') || activeSquads.includes(memSquad);

        // Match Troops
        const memTroop = (member.troop_level || 0).toString();
        const matchesTroop = activeTroops.includes('all') || activeTroops.includes(memTroop);

        return matchesSearch && matchesEligible && matchesRank && matchesProf && matchesSquad && matchesTroop;
    });

    // Apply Sorting
    filtered.sort((a, b) => {
        if (sortBy === 'name-asc') {
            return a.name.localeCompare(b.name);
        } else if (sortBy === 'name-desc') {
            return b.name.localeCompare(a.name);
        } else if (sortBy === 'power-desc') {
            return (b.power || 0) - (a.power || 0);
        } else if (sortBy === 'power-asc') {
            return (a.power || 0) - (b.power || 0);
        } else if (sortBy === 'squad-power-desc') {
            return (b.squad_power || 0) - (a.squad_power || 0);
        } else if (sortBy === 'squad-power-asc') {
            return (a.squad_power || 0) - (b.squad_power || 0);
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

    // Update the UI
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

    // Attach HQ Level listener for dynamic Troop Tier validation
    const hqInput = document.getElementById('member-level');
    if (hqInput) hqInput.addEventListener('input', updateTroopLevelOptions);

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
    
    // Core and Advanced fields
    const levelInput = document.getElementById('member-level');
    if (levelInput) levelInput.value = '';
    const profInput = document.getElementById('member-profession');
    if (profInput) profInput.value = '';
    const troopInput = document.getElementById('member-troop-level');
    if (troopInput) troopInput.value = '';
    
    // Squad tracking section
    const squadTypeInput = document.getElementById('member-squad-type');
    if (squadTypeInput) squadTypeInput.value = '';
    const sqPowerInput = document.getElementById('member-squad-power');
    if (sqPowerInput) sqPowerInput.value = '';
    const sqTimestampText = document.getElementById('modal-squad-power-timestamp');
    if (sqTimestampText) sqTimestampText.textContent = '';
    
    const squadSection = document.getElementById('modal-squad-section');
    if (squadSection) {
        squadSection.style.display = isSquadTrackingEnabled ? 'block' : 'none';
    }
    
    // Clear the overall power input and timestamp
    const powerInput = document.getElementById('modal-member-power');
    if (powerInput) powerInput.value = '';
    const timestampText = document.getElementById('modal-power-timestamp');
    if (timestampText) timestampText.textContent = '';

    const powerSection = document.getElementById('modal-power-section');
    if (powerSection) {
        powerSection.style.display = isPowerTrackingEnabled ? 'block' : 'none';
    }
    
    if (typeof updateTroopLevelOptions === 'function') updateTroopLevelOptions();
    
    document.getElementById('modal-form-title').textContent = 'Add New Member';
    document.getElementById('submit-btn').textContent = 'Add Member';
}

async function loadMembers() {
    try {
        const response = await fetch(API_URL);
        const members = await response.json();
        allMembers = members; 
        
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
            powerDisplay = `<span class="member-power" title="Overall Power: ${member.power.toLocaleString()}">${formatPower(member.power)}</span>`;
        }

        let professionBadge = member.profession ? `<span class="member-rank" style="background: #805ad5; margin-left: 5px;">${escapeHtml(member.profession)}</span>` : '';
        let troopBadge = member.troop_level ? `<span class="member-rank" style="background: #dd6b20; margin-left: 5px;">T${member.troop_level}</span>` : '';
        
        let squadDisplay = '';
        if (isSquadTrackingEnabled && (member.squad_type || member.squad_power)) {
            let typeIcon = '';
            if (member.squad_type === 'Tank') typeIcon = '🛡️ ';
            else if (member.squad_type === 'Aircraft') typeIcon = '✈️ ';
            else if (member.squad_type === 'Missile') typeIcon = '🚀 ';
            squadDisplay = `<span class="member-power" style="margin-left: 10px; color: var(--accent-color);" title="Squad Power: ${member.squad_power ? member.squad_power.toLocaleString() : 0}">${typeIcon}${formatPower(member.squad_power)}</span>`;
        }

        let toggleEligibleBtn = '';
        if (permissions.manage_train) {
            toggleEligibleBtn = `<button class="toggle-eligible-btn ${eligibleClass}" onclick="toggleEligible(${member.id}, ${member.eligible !== false})">${eligibleStatus}</button>`;
        }

        let actionsHtml = '';
        if (canManageRanks) {
            actionsHtml = `
                <div class="member-actions">
                    <button class="edit-btn" onclick="editMember(${member.id}, '${escapeHtml(member.name)}', '${escapeHtml(member.rank)}', ${member.eligible !== false}, ${member.power || 0}, '${member.power_updated_at || ''}', ${member.level || 0}, '${escapeHtml(member.squad_type || '')}', ${member.squad_power || 0}, '${member.squad_power_updated_at || ''}', ${member.troop_level || 0}, '${escapeHtml(member.profession || '')}')">Edit</button>
                    <button class="delete-btn" onclick="deleteMember(${member.id}, '${escapeHtml(member.name)}', ${member.has_user})">Delete</button>
                    ${(isR5OrAdmin && !member.has_user) ? `<button class="create-user-btn" onclick="createUserForMember(${member.id}, '${escapeHtml(member.name)}')">Create User</button>` : ''}
                    ${toggleEligibleBtn}
                </div>
            `;
        }
        
        // Format the aliases for display
        let aliasHtml = '';
        if (member.personal_aliases) {
            aliasHtml += ` <span style="color: #63b3ed; font-size: 0.85em;">(${escapeHtml(member.personal_aliases)})</span>`;
        }
        if (member.global_aliases) {
            aliasHtml += ` <span style="color: #a0aec0; font-size: 0.85em;">[${escapeHtml(member.global_aliases)}]</span>`;
        }
        
        return `
            <div class="member-card">
                <div class="member-info">
                    <div class="member-name" style="display: flex; align-items: center; gap: 8px;">
                        ${escapeHtml(member.name)}
                        ${aliasHtml}
                        <button onclick="openAliasModal(${member.id}, '${escapeHtml(member.name.replace(/'/g, "\\'"))}')" class="icon-btn" title="Manage Nicknames" style="background: none; border: none; cursor: pointer; opacity: 0.6; padding: 0;">🏷️</button>
                    </div>
                    <span class="member-rank rank-${member.rank.replace(/\s+/g, '-')}">${escapeHtml(member.rank)}</span>
                    ${member.level ? `<span class="member-rank" style="background: #4a5568; margin-left: 5px;">HQ ${member.level}</span>` : ''}
                    ${troopBadge}
                    ${professionBadge}
                    ${powerDisplay}
                    ${squadDisplay}
                    ${permissions.view_train ? `<span class="member-eligible ${eligibleClass}">${eligibleStatus}</span>` : ''}
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
    
    const level = parseInt(document.getElementById('member-level').value, 10) || 0;
    const profession = document.getElementById('member-profession').value;
    const squad_type = document.getElementById('member-squad-type').value;
    const troop_level = parseInt(document.getElementById('member-troop-level').value, 10) || 0;
    
    const sqPowerInput = document.getElementById('member-squad-power');
    const squad_power = (sqPowerInput && sqPowerInput.value !== '') ? parseInt(sqPowerInput.value, 10) : 0;

    // Enforce HQ level limits
    if (level > currentMaxHQ) {
        alert(`HQ Level cannot exceed the current server maximum of ${currentMaxHQ}.`);
        return;
    }
    if (level < 0) {
        alert('HQ Level cannot be negative.');
        return;
    }
    
    // Grab the overall power value.
    const powerInput = document.getElementById('modal-member-power');
    const power = (powerInput && powerInput.value !== '') ? parseInt(powerInput.value, 10) : 0;

    if (!name || !rank) {
        alert('Please fill in all fields');
        return;
    }

    try {
        if (editingMemberId) {
            const response = await fetch(`${API_URL}/${editingMemberId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, rank, level, eligible, power, profession, squad_type, troop_level, squad_power }), 
            });
            if (!response.ok) throw new Error('Failed to update member');
            editingMemberId = null;
        } else {
            const response = await fetch(API_URL, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, rank, level, eligible, power, profession, squad_type, troop_level, squad_power}),
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

window.editMember = function(id, name, rank, eligible, power = 0, powerUpdatedAt = '', level = 0, squadType = '', squadPower = 0, squadPowerUpdatedAt = '', troopLevel = 0, profession = '') {
    if (!canManageRanks) {
        alert('You do not have permission to edit members.');
        return;
    }
    editingMemberId = id;
    
    // Core fields
    document.getElementById('member-name').value = name;
    document.getElementById('member-rank').value = rank;
    document.getElementById('member-eligible').checked = eligible;
    
    const levelInput = document.getElementById('member-level');
    if (levelInput) levelInput.value = (level > 0) ? level : '';
    
    // Power fields
    const powerSection = document.getElementById('modal-power-section');
    if (powerSection) powerSection.style.display = isPowerTrackingEnabled ? 'block' : 'none';
    
    const powerInput = document.getElementById('modal-member-power');
    if (powerInput) powerInput.value = (power && power > 0) ? power : '';

    const timestampText = document.getElementById('modal-power-timestamp');
    if (timestampText) {
        if (powerUpdatedAt) {
            const formattedDateStr = powerUpdatedAt.replace(' ', 'T') + "Z";
            const updatedDate = new Date(formattedDateStr);
            timestampText.textContent = `Last updated: ${updatedDate.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })}`;
        } else {
            timestampText.textContent = "Last updated: Never";
        }
    }

    // Safely populate Squad and Advanced fields
    const profInput = document.getElementById('member-profession');
    if (profInput) profInput.value = profession;

    const troopInput = document.getElementById('member-troop-level');
    if (troopInput) troopInput.value = (troopLevel > 0) ? troopLevel : '';

    // Handle Squad Tracking Section visibility
    const squadSection = document.getElementById('modal-squad-section');
    if (squadSection) squadSection.style.display = isSquadTrackingEnabled ? 'block' : 'none';

    const squadTypeInput = document.getElementById('member-squad-type');
    if (squadTypeInput) squadTypeInput.value = squadType;
    
    const sqPowerInput = document.getElementById('member-squad-power');
    if (sqPowerInput) sqPowerInput.value = (squadPower && squadPower > 0) ? squadPower : '';

    const sqTimestampText = document.getElementById('modal-squad-power-timestamp');
    if (sqTimestampText) {
        if (squadPowerUpdatedAt) {
            const formattedDateStr = squadPowerUpdatedAt.replace(' ', 'T') + "Z";
            const updatedDate = new Date(formattedDateStr);
            sqTimestampText.textContent = `Last updated: ${updatedDate.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}`;
        } else {
            sqTimestampText.textContent = "Last updated: Never";
        }
    }

    // Safely call the Troop Level updater so options lock correctly
    if (typeof updateTroopLevelOptions === 'function') {
        updateTroopLevelOptions();
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
    if (!permissions.manage_train) {
        alert('You do not have permission to manage the train schedule.');
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
        
        // Copy all existing member data (including level and power), then overwrite 'eligible'
        const updatedMemberData = {
            ...member,
            eligible: newStatus
        };

        const updateResponse = await fetch(`${API_URL}/${id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(updatedMemberData),
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
    if (power >= 1000000000) return (power / 1000000000).toFixed(2) + 'B';
    if (power >= 1000000) return (power / 1000000).toFixed(2) + 'M';
    if (power >= 1000) return (power / 1000).toFixed(1) + 'K';
    return power.toString();
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

    if (searchInput) searchInput.addEventListener('input', updateDisplayedMembers);
    
    if (clearBtn) {
        clearBtn.addEventListener('click', () => {
            searchInput.value = '';
            updateDisplayedMembers();
            searchInput.focus();
        });
    }

    // Helper function to manage chip toggle states
    function setupChipGroup(chipSelector, dataAttribute) {
        const chips = document.querySelectorAll(chipSelector);
        chips.forEach(chip => {
            chip.addEventListener('click', (e) => {
                const clickedValue = e.target.getAttribute(`data-${dataAttribute}`);
                
                if (clickedValue === 'all') {
                    // Turn off everything else, turn on 'All'
                    chips.forEach(c => c.classList.remove('active'));
                    e.target.classList.add('active');
                } else {
                    // Turn off 'All'
                    document.querySelector(`${chipSelector}[data-${dataAttribute}="all"]`).classList.remove('active');
                    e.target.classList.toggle('active');
                    
                    // If they unclicked everything, turn 'All' back on automatically
                    const activeChips = document.querySelectorAll(`${chipSelector}.active`);
                    if (activeChips.length === 0) {
                        document.querySelector(`${chipSelector}[data-${dataAttribute}="all"]`).classList.add('active');
                    }
                }
                updateDisplayedMembers();
            });
        });
    }

    // Wire up all 4 rows of chips instantly
    setupChipGroup('.rank-chip', 'rank');
    setupChipGroup('.prof-chip', 'prof');
    setupChipGroup('.squad-chip', 'squad');
    setupChipGroup('.troop-chip', 'troop');

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
                    ${member.level ? `<span class="member-rank" style="background: #4a5568; margin-left: 5px;">HQ ${member.level}</span>` : ''}
                    ${member.troop_level ? `<span class="member-rank" style="background: #dd6b20; margin-left: 5px;">T${member.troop_level}</span>` : ''}
                    ${member.squad_type ? `<span class="member-rank" style="background: #2b6cb0; margin-left: 5px;">${escapeHtml(member.squad_type)}</span>` : ''}
                    ${member.profession ? `<span class="member-rank" style="background: #805ad5; margin-left: 5px;">${escapeHtml(member.profession)}</span>` : ''}
                    ${member.power ? `<span class="member-power" style="margin-left: 10px; font-size: 0.85em;">⚡ ${(member.power / 1000000).toFixed(1)}M</span>` : ''}
                    ${member.squad_power ? `<span class="member-power" style="margin-left: 10px; font-size: 0.85em; color: var(--accent-color);">🛡️ ${(member.squad_power / 1000000).toFixed(1)}M</span>` : ''}
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

// --- Alias Management Logic ---
let currentAliasMemberId = null;

async function openAliasModal(memberId, memberName) {
    currentAliasMemberId = memberId;
    document.getElementById('alias-modal-title').textContent = `Nicknames for ${memberName}`;
    
    // Only show the "Global" checkbox if the user is an Admin
    const globalWrapper = document.getElementById('global-alias-checkbox-wrapper');
    if (globalWrapper) {
        globalWrapper.style.display = isAdmin ? 'block' : 'none';
    }

    document.getElementById('alias-modal').style.display = 'flex';
    await loadAliases();
}

async function loadAliases() {
    const list = document.getElementById('aliases-list');
    list.innerHTML = '<p style="text-align: center; color: var(--text-muted);">Loading...</p>';

    try {
        const res = await fetch(`${API_URL}/${currentAliasMemberId}/aliases`);
        const aliases = await res.json();

        if (!aliases || aliases.length === 0) {
            list.innerHTML = '<p style="text-align: center; color: var(--text-muted);">No nicknames set for this commander.</p>';
            return;
        }

        list.innerHTML = aliases.map(a => {
            const badge = a.is_global ? 
                `<span style="background: #e2e8f0; color: #4a5568; padding: 2px 6px; border-radius: 4px; font-size: 0.8em; margin-right: 8px;">Global</span>` : 
                `<span style="background: #bee3f8; color: #2b6cb0; padding: 2px 6px; border-radius: 4px; font-size: 0.8em; margin-right: 8px;">Personal</span>`;
            
            // Only allow deletion if they own the alias OR they are an admin
            const canDelete = a.is_mine || isAdmin;
            const deleteBtn = canDelete ? `<button onclick="deleteAlias(${a.id})" style="background: none; border: none; color: #e53e3e; cursor: pointer;" title="Remove Nickname">✖</button>` : '';

            return `
                <div style="display: flex; justify-content: space-between; align-items: center; padding: 10px; border-bottom: 1px solid var(--border-color);">
                    <div>${badge} <strong>${escapeHtml(a.alias)}</strong></div>
                    ${deleteBtn}
                </div>
            `;
        }).join('');
    } catch (e) {
        list.innerHTML = '<p style="color: #e53e3e; text-align: center;">Error loading aliases.</p>';
    }
}

document.getElementById('add-alias-form')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const input = document.getElementById('new-alias-input');
    const isGlobal = document.getElementById('new-alias-global')?.checked || false;

    try {
        const res = await fetch(`${API_URL}/${currentAliasMemberId}/aliases`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alias: input.value.trim(), is_global: isGlobal })
        });

        if (!res.ok) throw new Error(await res.text());
        
        input.value = ''; // Clear input
        if (document.getElementById('new-alias-global')) {
            document.getElementById('new-alias-global').checked = false;
        }
        await loadAliases(); // Refresh list
        loadMembers(); // Refresh main table to show updated tags
    } catch (err) {
        alert("Failed to add nickname: " + err.message);
    }
});

window.deleteAlias = async function(aliasId) {
    if (!confirm("Remove this nickname?")) return;
    
    try {
        const res = await fetch(`/api/aliases/${aliasId}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        
        await loadAliases();
        loadMembers(); 
    } catch (err) {
        alert("Failed to delete nickname: " + err.message);
    }
};

document.getElementById('close-alias-modal')?.addEventListener('click', () => {
    document.getElementById('alias-modal').style.display = 'none';
});