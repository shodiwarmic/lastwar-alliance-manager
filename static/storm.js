// static/storm.js
const API_URL = '/api/storm-assignments';
const MEMBERS_URL = '/api/members';

const BUILDINGS = [
    // Stage 1 - Immediate (Priority: Hospitals > Oil Refineries)
    { id: 'field_hospital_1', name: 'Field Hospital I', stage: 1, points: '30/s', priority: 'CRITICAL', boost: 'Heal 15 troops/10s' },
    { id: 'field_hospital_2', name: 'Field Hospital II', stage: 1, points: '30/s', priority: 'CRITICAL', boost: 'Heal 15 troops/10s' },
    { id: 'field_hospital_3', name: 'Field Hospital III', stage: 1, points: '30/s', priority: 'CRITICAL', boost: 'Heal 15 troops/10s' },
    { id: 'field_hospital_4', name: 'Field Hospital IV', stage: 1, points: '30/s', priority: 'CRITICAL', boost: 'Heal 15 troops/10s' },
    { id: 'oil_refinery_1', name: 'Oil Refinery I', stage: 1, points: '50/s', priority: 'HIGH' },
    { id: 'oil_refinery_2', name: 'Oil Refinery II', stage: 1, points: '50/s', priority: 'HIGH' },
    { id: 'science_hub', name: 'Science Hub', stage: 1, points: '10/s', priority: 'MEDIUM', boost: 'Teleport cooldown -50%' },
    { id: 'info_center', name: 'Info Center', stage: 1, points: '10/s', priority: 'LOW', boost: '+10% all points' },
    
    // Stage 2 - After 10 minutes (Priority: Hospitals & Nuclear Silo)
    { id: 'nuclear_silo', name: 'Nuclear Silo', stage: 2, points: '80/s', priority: 'CRITICAL', boost: 'HIGHEST POINTS!' },
    { id: 'arsenal', name: 'Arsenal', stage: 2, points: '10/s', priority: 'MEDIUM', boost: '+15% ATK/DEF/HP' },
    { id: 'mercenary_factory', name: 'Mercenary Factory', stage: 2, points: '10/s', priority: 'MEDIUM', boost: '-15% enemy stats' }
];

let allMembers = [];
let currentTaskForce = 'A';
let assignments = {};

// Load members
async function loadMembers() {
    try {
        const response = await fetch(MEMBERS_URL);
        allMembers = await response.json();
        allMembers.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
    } catch (error) {
        console.error('Error loading members:', error);
    }
}

// Load assignments
async function loadAssignments() {
    try {
        const response = await fetch(`${API_URL}?task_force=${currentTaskForce}`);
        const data = await response.json();
        
        // Initialize empty assignments for all buildings
        assignments = {};
        BUILDINGS.forEach(building => {
            assignments[building.id] = [];
        });
        
        // Fill in saved assignments
        data.forEach(assignment => {
            if (!assignments[assignment.building_id]) {
                assignments[assignment.building_id] = [];
            }
            assignments[assignment.building_id].push(assignment.member_id);
        });
        
        renderBuildings();
    } catch (error) {
        console.error('Error loading assignments:', error);
        // Initialize empty assignments
        assignments = {};
        BUILDINGS.forEach(building => {
            assignments[building.id] = [];
        });
        renderBuildings();
    }
}

// Get members assigned in a specific stage (excluding current building and slot)
function getAssignedMembersInStage(stage, excludeBuildingId, excludeSlot) {
    const assignedIds = new Set();
    BUILDINGS.filter(b => b.stage === stage).forEach(building => {
        if (building.id !== excludeBuildingId && assignments[building.id]) {
            assignments[building.id].forEach((memberId) => {
                if (memberId) {
                    assignedIds.add(memberId);
                }
            });
        }
        // For same building, exclude other slots
        if (building.id === excludeBuildingId && assignments[building.id]) {
            assignments[building.id].forEach((memberId, slotIndex) => {
                if (memberId && slotIndex !== excludeSlot) {
                    assignedIds.add(memberId);
                }
            });
        }
    });
    return assignedIds;
}

// Render buildings
function renderBuildings() {
    const grid = document.getElementById('buildings-grid');
    if (!grid) return;

    let html = '';
    
    // Group buildings by stage
    const stages = [
        { num: 1, title: 'Stage 1 - Immediate (0:00)', buildings: BUILDINGS.filter(b => b.stage === 1) },
        { num: 2, title: 'Stage 2 - After 10 Minutes', buildings: BUILDINGS.filter(b => b.stage === 2) }
    ];
    
    stages.forEach(stage => {
        html += `<div class="storm-stage">`;
        html += `<h4 class="storm-stage-title">${stage.title}</h4>`;
        html += `<div class="storm-stage-buildings">`;
        
        stage.buildings.forEach(building => {
            const priorityClass = building.priority.toLowerCase();
            const assignedMembers = assignments[building.id] || [];
            
            html += `<div class="storm-building ${priorityClass}">`;
            html += `<div class="building-header">`;
            html += `<h5>${building.name}</h5>`;
            html += `<span class="priority-badge ${priorityClass}">${building.priority}</span>`;
            html += `</div>`;
            html += `<div class="building-info">`;
            html += `<span class="points">⚡ ${building.points}</span>`;
            if (building.boost) {
                html += `<span class="boost">✨ ${building.boost}</span>`;
            }
            html += `</div>`;
            
            // Member slots
            html += `<div class="member-slots">`;
            for (let i = 0; i < 4; i++) {
                const memberId = assignedMembers[i] || '';
                html += `<div class="slot-container">`;
                html += `<label>Slot ${i + 1}</label>`;
                html += `<div class="searchable-select" data-building="${building.id}" data-slot="${i}" data-stage="${building.stage}">`;
                html += `<input type="text" class="search-input" placeholder="Search member..." autocomplete="off">`;
                html += `<div class="dropdown-list" style="display: none;"></div>`;
                html += `<input type="hidden" class="selected-member-id" value="${memberId}">`;
                html += `</div>`;
                html += `</div>`;
            }
            html += `</div>`; // member-slots
            html += `</div>`; // storm-building
        });
        
        html += `</div>`; // storm-stage-buildings
        html += `</div>`; // storm-stage
    });
    
    grid.innerHTML = html;
    
    // Initialize searchable selects
    document.querySelectorAll('.searchable-select').forEach(container => {
        initSearchableSelect(container);
    });
}

// Initialize a searchable select dropdown
function initSearchableSelect(container) {
    const searchInput = container.querySelector('.search-input');
    const dropdownList = container.querySelector('.dropdown-list');
    const hiddenInput = container.querySelector('.selected-member-id');
    
    // Get currently selected member
    const currentMemberId = parseInt(hiddenInput.value) || null;
    if (currentMemberId) {
        const member = allMembers.find(m => m.id === currentMemberId);
        if (member) {
            searchInput.value = `${member.name} (${member.rank})`;
        }
    }
    
    // Show dropdown on focus
    searchInput.addEventListener('focus', () => {
        updateDropdownList(container, '');
        dropdownList.style.display = 'block';
    });
    
    // Filter on input
    searchInput.addEventListener('input', () => {
        updateDropdownList(container, searchInput.value);
        dropdownList.style.display = 'block';
    });
    
    // Hide dropdown when clicking outside
    document.addEventListener('click', (e) => {
        if (!container.contains(e.target)) {
            dropdownList.style.display = 'none';
        }
    });
}

// Update dropdown list with filtered members
function updateDropdownList(container, searchTerm) {
    const dropdownList = container.querySelector('.dropdown-list');
    const searchInput = container.querySelector('.search-input');
    const hiddenInput = container.querySelector('.selected-member-id');
    const buildingId = container.dataset.building;
    const slot = parseInt(container.dataset.slot);
    const stage = parseInt(container.dataset.stage);
    
    // Get members already assigned in this stage
    const assignedInStage = getAssignedMembersInStage(stage, buildingId, slot);
    
    // Filter available members
    const filteredMembers = allMembers.filter(member => {
        // Check if member matches search term
        const matchesSearch = searchTerm === '' || 
            member.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
            member.rank.toLowerCase().includes(searchTerm.toLowerCase());
        
        // Check if member is available (not assigned in this stage)
        const isAvailable = !assignedInStage.has(member.id);
        
        return matchesSearch && isAvailable;
    });
    
    let html = '<div class="dropdown-item" data-member-id="">-- Not Assigned --</div>';
    
    if (filteredMembers.length === 0 && searchTerm !== '') {
        html += '<div class="dropdown-item disabled">No members found</div>';
    } else {
        filteredMembers.forEach(member => {
            html += `<div class="dropdown-item" data-member-id="${member.id}">${escapeHtml(member.name)} (${member.rank})</div>`;
        });
    }
    
    dropdownList.innerHTML = html;
    
    // Add click handlers to dropdown items
    dropdownList.querySelectorAll('.dropdown-item:not(.disabled)').forEach(item => {
        item.addEventListener('click', () => {
            const memberId = parseInt(item.dataset.memberId) || null;
            
            // Update hidden input
            hiddenInput.value = memberId || '';
            
            // Update search input display
            searchInput.value = item.textContent.trim();
            
            // Update assignments
            if (!assignments[buildingId]) {
                assignments[buildingId] = [];
            }
            assignments[buildingId][slot] = memberId;
            
            // Hide dropdown
            dropdownList.style.display = 'none';
            
            // Re-render to update other dropdowns (remove selected member from options)
            renderBuildings();
        });
    });
}

// Generate mail
function generateMail() {
    // Check if there are any assignments
    let hasAssignments = false;
    Object.keys(assignments).forEach(buildingId => {
        if (assignments[buildingId] && assignments[buildingId].some(id => id)) {
            hasAssignments = true;
        }
    });
    
    if (!hasAssignments) {
        alert('No assignments found. Please assign members to buildings first.');
        return;
    }
    
    // Get battle time
    const battleTime = document.getElementById('battleTime')?.value;
    
    let mail = `🏜️ DESERT STORM - TASK FORCE ${currentTaskForce}\n`;
    mail += `═══════════════════════════════════════\n\n`;
    
    if (battleTime) {
        const timeText = battleTime === '18:00' ? '18:00-18:30 ST (20:00 UK / 21:00 CET)' : '09:00-09:30 ST (11:00 UK / 12:00 CET)';
        mail += `⏰ BATTLE TIME: ${timeText}\n\n`;
    }
    
    mail += `BATTLE STRATEGY:\n\n`;
    mail += `STAGE 1 (Start - 10 min):\n`;
    mail += `1. Field Hospitals (all 4) - CRITICAL for troop healing!\n`;
    mail += `2. Oil Refineries I & II (100/s total) - HIGH PRIORITY\n`;
    mail += `3. Science Hub - Faster teleports (1min cooldown)\n`;
    mail += `4. Info Center - +10% all points (low priority)\n\n`;
    
    mail += `STAGE 2 (10-30 min):\n`;
    mail += `1. Nuclear Silo (80/s) - CRITICAL! 3 STRONGEST CAPTURE!\n`;
    mail += `2. Maintain Field Hospitals for continuous healing\n`;
    mail += `3. Hold and defend Nuclear Silo at all costs\n`;
    mail += `4. After 20min: Oil Rigs appear - collect for bonus points\n`;
    mail += `5. Arsenal & Mercenary Factory - Secure these for buffs\n\n`;
    
    mail += `TACTICAL TIPS:\n\n`;
    mail += `STARTING THE BATTLE:\n`;
    mail += `- Enter game IMMEDIATELY when battle starts\n`;
    mail += `- TELEPORT to your assigned location (don't walk!)\n`;
    mail += `- Port cooldown: 2min normally, 1min with Science Hub\n\n`;
    
    mail += `SQUAD MANAGEMENT (CRITICAL!):\n`;
    mail += `- WEAKEST squad = Defend buildings\n`;
    mail += `- STRONGEST squad(s) = Attack enemies\n`;
    mail += `- This protects your main force and maximizes combat power\n\n`;
    
    mail += `HOSPITALS:\n`;
    mail += `- CRITICAL for gathering troops back\n`;
    mail += `- Collect regularly using the House+ icon (left side)\n`;
    mail += `- Your survival depends on healing!\n\n`;
    
    mail += `DEFENSE STRATEGY:\n`;
    mail += `- If attacked by MUCH STRONGER opponent:\n`;
    mail += `  - Remove all troops from wall, OR\n`;
    mail += `  - Teleport to safety immediately\n`;
    mail += `- Don't sacrifice troops unnecessarily!\n\n`;
    
    mail += `COMBAT & POINTS:\n`;
    mail += `- Collect supply drops IMMEDIATELY before opponents\n`;
    mail += `- Buildings generate points after 60 seconds\n`;
    mail += `- After 20min: and if you are low on troops focus on Oil Rigs for extra points\n\n`;
    
    mail += `TEAMWORK:\n`;
    mail += `- Once your building is secure, check map (top-right)\n`;
    mail += `- Relocate to help teammates or capture new buildings\n`;
    mail += `- BACK UP teammates under attack\n`;
    mail += `- Attack together - coordinate on same target\n`;
    mail += `- Watch opponent movements - exploit vulnerabilities!\n\n`;
    
    mail += `═══════════════════════════════════════\n`;
    mail += `ATTENTION SUBSTITUTES:\n`;
    mail += `Hey team! We really need you to be online and ready at battle time.\n`;
    mail += `There's a very high chance someone from the main roster will miss it,\n`;
    mail += `so your participation is crucial for our success!\n\n`;
    mail += `- Be online 2-3 minutes before battle starts\n`;
    mail += `- Watch alliance chat for updates\n`;
    mail += `- Jump in immediately if someone doesn't show\n\n`;
    mail += `Your flexibility and readiness make all the difference! 💪\n\n`;
    mail += `═══════════════════════════════════════\n`;
    mail += `BUILDING ASSIGNMENTS:\n\n`;
    
    // Group by stage
    const stages = [
        { num: 1, title: 'STAGE 1 - IMMEDIATE', buildings: BUILDINGS.filter(b => b.stage === 1) },
        { num: 2, title: 'STAGE 2 - AFTER 10 MIN', buildings: BUILDINGS.filter(b => b.stage === 2) }
    ];
    
    stages.forEach(stage => {
        mail += `\n${stage.title}:\n`;
        mail += `───────────────────────────────\n`;
        
        stage.buildings.forEach(building => {
            const memberIds = assignments[building.id] || [];
            const assignedMembers = memberIds
                .filter(id => id)
                .map(id => {
                    const member = allMembers.find(m => m.id === id);
                    return member ? member.name : 'Unknown';
                });
            
            if (assignedMembers.length > 0) {
                mail += `\n- ${building.name}:\n`;
                mail += `   ${assignedMembers.join(', ')}\n`;
            }
        });
    });
    
    mail += `\n═══════════════════════════════════════\n`;
    mail += `💪 LET'S WIN THIS! GOOD LUCK EVERYONE!\n`;
    mail += `═══════════════════════════════════════\n`;
    
    // Display mail
    const mailContent = document.getElementById('mail-content');
    const mailOutput = document.getElementById('mail-output');
    
    if (mailContent && mailOutput) {
        mailContent.textContent = mail;
        mailOutput.style.display = 'block';
        mailOutput.scrollIntoView({ behavior: 'smooth' });
    }
}

// Copy mail to clipboard
async function copyMail() {
    const mailContent = document.getElementById('mail-content');
    if (!mailContent) return;
    
    const mailText = mailContent.textContent;
    
    try {
        await navigator.clipboard.writeText(mailText);
        alert('✓ Mail copied to clipboard!');
    } catch (error) {
        // Fallback for older browsers
        const textArea = document.createElement('textarea');
        textArea.value = mailText;
        textArea.style.position = 'fixed';
        textArea.style.left = '-999999px';
        document.body.appendChild(textArea);
        textArea.select();
        
        try {
            document.execCommand('copy');
            alert('✓ Mail copied to clipboard!');
        } catch (err) {
            alert('Failed to copy mail. Please copy manually.');
        }
        
        document.body.removeChild(textArea);
    }
}

// Clear all assignments
async function clearAssignments() {
    if (!confirm(`Clear all assignments for Task Force ${currentTaskForce}? This cannot be undone.`)) {
        return;
    }
    
    try {
        const response = await fetch(`${API_URL}/${currentTaskForce}`, {
            method: 'DELETE'
        });
        
        if (!response.ok && response.status !== 204) {
            throw new Error('Failed to clear assignments');
        }
        
        // Reset local assignments
        assignments = {};
        BUILDINGS.forEach(building => {
            assignments[building.id] = [];
        });
        
        renderBuildings();
        const mailOutput = document.getElementById('mail-output');
        if (mailOutput) mailOutput.style.display = 'none';
        alert('✓ Assignments cleared!');
    } catch (error) {
        console.error('Error clearing assignments:', error);
        alert('Failed to clear assignments: ' + error.message);
    }
}

// Escape HTML
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    // Guard: Only initialize if we are on the Storm Assignments page
    const buildingsGrid = document.getElementById('buildings-grid');
    if (buildingsGrid) {
        await loadMembers();
        await loadAssignments();
        
        // Event listeners
        const generateMailBtn = document.getElementById('generate-mail-btn');
        if (generateMailBtn) generateMailBtn.addEventListener('click', generateMail);
        
        const clearAssignmentsBtn = document.getElementById('clear-assignments-btn');
        if (clearAssignmentsBtn) clearAssignmentsBtn.addEventListener('click', clearAssignments);
        
        const copyMailBtn = document.getElementById('copy-mail-btn');
        if (copyMailBtn) copyMailBtn.addEventListener('click', copyMail);

        // Task force switcher
        document.querySelectorAll('input[name="taskForce"]').forEach(radio => {
            radio.addEventListener('change', (e) => {
                currentTaskForce = e.target.value;
                loadAssignments();
                const mailOutput = document.getElementById('mail-output');
                if (mailOutput) mailOutput.style.display = 'none';
            });
        });
    }
});