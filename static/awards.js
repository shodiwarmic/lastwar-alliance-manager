// static/awards.js
const API_URL = '/api/awards';
const AWARD_TYPES_URL = '/api/award-types';
const MEMBERS_URL = '/api/members';

let AWARD_TYPES = []; // Will be loaded dynamically from database
let currentWeekDate = null;
let allMembers = [];
let currentAwards = {};
let allHistory = [];
let activeAwardTypes = new Set(); // All active awards
let allAwardTypesData = []; // Store full award type objects

// Get most recent Monday
function getMostRecentMonday(date = new Date()) {
    const d = new Date(date);
    const day = d.getDay();
    const diff = day === 0 ? 6 : day - 1; // If Sunday, go back 6 days, else go back to Monday
    d.setDate(d.getDate() - diff);
    return d;
}

// Format date as YYYY-MM-DD
function formatDate(date) {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
}

// Format date for display (European style: dd/mm/yyyy)
function formatDisplayDate(dateStr) {
    const date = new Date(dateStr + 'T00:00:00');
    const day = String(date.getDate()).padStart(2, '0');
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const year = date.getFullYear();
    
    return `Week of ${day}/${month}/${year}`;
}

// Initialize current week
function initializeWeek() {
    currentWeekDate = getMostRecentMonday();
    updateWeekDisplay();
}

// Update week display
function updateWeekDisplay() {
    document.getElementById('week-display').textContent = formatDisplayDate(formatDate(currentWeekDate));
}

// Navigate weeks functions
function navigatePrevWeek() {
    currentWeekDate.setDate(currentWeekDate.getDate() - 7);
    updateWeekDisplay();
    loadAwards();
}

function navigateNextWeek() {
    currentWeekDate.setDate(currentWeekDate.getDate() + 7);
    updateWeekDisplay();
    loadAwards();
}

// Fuzzy search function
function fuzzyMatch(str, pattern) {
    if (!pattern) return true;
    const strLower = str.toLowerCase();
    const patternLower = pattern.toLowerCase();
    
    // Exact match gets priority
    if (strLower.includes(patternLower)) return true;
    
    // Fuzzy match - check if all pattern chars appear in order
    let patternIdx = 0;
    for (let i = 0; i < strLower.length && patternIdx < patternLower.length; i++) {
        if (strLower[i] === patternLower[patternIdx]) {
            patternIdx++;
        }
    }
    return patternIdx === patternLower.length;
}

// Load award types from database
async function loadAwardTypes() {
    try {
        const response = await fetch(AWARD_TYPES_URL);
        const awardTypes = await response.json();
        
        allAwardTypesData = awardTypes;
        
        AWARD_TYPES = awardTypes
            .filter(at => at.active)
            .sort((a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name))
            .map(at => at.name);
        
        // Initialize active award types as empty - will be populated based on assignments
        activeAwardTypes = new Set();
    } catch (error) {
        console.error('Error loading award types:', error);
        // Fallback to default awards if API fails
        AWARD_TYPES = [
            'Alliance Champion',
            'Star of Desert Storm',
            'Soldier Crusher',
            'Divine Healer',
            'Great Destroyer',
            'Grind King'
        ];
        activeAwardTypes = new Set(AWARD_TYPES);
        allAwardTypesData = AWARD_TYPES.map((name, idx) => ({
            id: idx + 1,
            name: name,
            active: true,
            sort_order: idx
        }));
    }
}

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

// Load awards for current week
async function loadAwards() {
    const weekDate = formatDate(currentWeekDate);
    
    try {
        const response = await fetch(`${API_URL}?week=${weekDate}`);
        const awards = await response.json();
        
        // Organize awards by type and rank
        currentAwards = {};
        awards.forEach(award => {
            if (!currentAwards[award.award_type]) {
                currentAwards[award.award_type] = {};
            }
            currentAwards[award.award_type][award.rank] = award.member_id;
        });
        
        // Set active award types to only those with assignments
        activeAwardTypes = new Set(Object.keys(currentAwards));
        
        renderAwardsForm();
    } catch (error) {
        console.error('Error loading awards:', error);
    }
}

// Save current form state
function saveFormState() {
    const formState = {};
    document.querySelectorAll('.member-select').forEach(select => {
        const award = select.dataset.award;
        const rank = select.dataset.rank;
        const key = `${award}|${rank}`;
        formState[key] = select.value;
    });
    return formState;
}

// Restore form state
function restoreFormState(formState) {
    if (!formState) return;
    
    document.querySelectorAll('.member-select').forEach(select => {
        const award = select.dataset.award;
        const rank = select.dataset.rank;
        const key = `${award}|${rank}`;
        if (formState[key]) {
            select.value = formState[key];
        }
    });
}

// Render awards form
function renderAwardsForm(preserveFormState = false) {
    const grid = document.getElementById('awards-grid');
    if (!grid) return;
    
    // Save current form state before re-rendering (only if preserving)
    const formState = preserveFormState ? saveFormState() : null;
    
    let html = '';
    
    // Get active award types as array and sort
    const activeTypes = Array.from(activeAwardTypes).sort();
    const inactiveTypes = AWARD_TYPES.filter(type => !activeAwardTypes.has(type)).sort();
    
    activeTypes.forEach(awardType => {
        html += `<div class="award-card">`;
        html += `<div class="award-header">`;
        html += `<h4 class="award-title">🏆 ${awardType}</h4>`;
        html += `<button class="toggle-award-btn" data-award="${awardType}" title="Hide this award">✕</button>`;
        html += `</div>`;
        
        for (let rank = 1; rank <= 3; rank++) {
            const selectedMemberId = currentAwards[awardType]?.[rank] || '';
            const rankLabel = rank === 1 ? '🥇 1st Place' : rank === 2 ? '🥈 2nd Place' : '🥉 3rd Place';
            
            html += `<div class="award-position">`;
            html += `<label>${rankLabel}</label>`;
            html += `<input type="text" class="member-search" placeholder="🔍 Search member..." data-award="${awardType}" data-rank="${rank}">`;
            html += `<select class="member-select" data-award="${awardType}" data-rank="${rank}">`;
            html += `<option value="">-- Select Member --</option>`;
            
            allMembers.forEach(member => {
                const selected = member.id === selectedMemberId ? 'selected' : '';
                html += `<option value="${member.id}" ${selected} data-name="${member.name.toLowerCase()}">${escapeHtml(member.name)} (${member.rank})</option>`;
            });
            
            html += `</select>`;
            html += `</div>`;
        }
        
        html += `</div>`;
    });
    
    // Show inactive awards section if any
    if (inactiveTypes.length > 0) {
        html += `<div class="inactive-awards-section">`;
        html += `<h4 class="inactive-title">Inactive Awards (Click to activate)</h4>`;
        html += `<div class="inactive-awards-chips">`;
        inactiveTypes.forEach(awardType => {
            html += `<button class="inactive-award-chip" data-award="${awardType}">${awardType}</button>`;
        });
        html += `</div>`;
        html += `</div>`;
    }
    
    grid.innerHTML = html;
    
    // Restore form state after rendering (only if preserving)
    if (preserveFormState) {
        restoreFormState(formState);
    }
    
    // Setup toggle buttons
    document.querySelectorAll('.toggle-award-btn').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            const award = e.target.dataset.award;
            activeAwardTypes.delete(award);
            renderAwardsForm(true); // Preserve form state when hiding
        });
    });
    
    // Setup inactive award chips
    document.querySelectorAll('.inactive-award-chip').forEach(chip => {
        chip.addEventListener('click', (e) => {
            e.preventDefault();
            const award = e.target.dataset.award;
            activeAwardTypes.add(award);
            renderAwardsForm(true); // Preserve form state when showing
        });
    });
    
    // Setup search filters for all dropdowns
    setupSearchFilters();
    
    // Update toggle button text
    updateToggleButton();
}

// Setup search filters
function setupSearchFilters() {
    const searchInputs = document.querySelectorAll('.member-search');
    
    searchInputs.forEach(input => {
        input.addEventListener('input', (e) => {
            const award = e.target.dataset.award;
            const rank = e.target.dataset.rank;
            const searchTerm = e.target.value.toLowerCase().trim();
            const select = document.querySelector(`select[data-award="${award}"][data-rank="${rank}"]`);
            
            filterSelectOptions(select, searchTerm);
        });
    });
}

// Filter select options
function filterSelectOptions(selectElement, searchTerm) {
    if (!selectElement) return;
    const options = selectElement.options;
    let visibleCount = 0;
    
    for (let i = 0; i < options.length; i++) {
        const option = options[i];
        if (i === 0) { // Keep first option visible
            option.style.display = '';
            continue;
        }
        
        const name = option.dataset.name || '';
        
        if (name.includes(searchTerm)) {
            option.style.display = '';
            visibleCount++;
        } else {
            option.style.display = 'none';
        }
    }
    
    // Auto-select if only one visible option
    if (visibleCount === 1 && searchTerm) {
        for (let i = 1; i < options.length; i++) {
            if (options[i].style.display !== 'none') {
                selectElement.selectedIndex = i;
                break;
            }
        }
    }
}

// Save awards
async function saveAwards() {
    const weekDate = formatDate(currentWeekDate);
    
    // Check if there are hidden awards with saved data
    const hiddenAwardsWithData = [];
    const inactiveTypes = AWARD_TYPES.filter(type => !activeAwardTypes.has(type));
    inactiveTypes.forEach(awardType => {
        if (currentAwards[awardType]) {
            const ranks = Object.keys(currentAwards[awardType]);
            if (ranks.length > 0) {
                hiddenAwardsWithData.push(awardType);
            }
        }
    });
    
    // Warn user if saving will delete hidden awards
    if (hiddenAwardsWithData.length > 0) {
        const message = `Warning: ${hiddenAwardsWithData.length} hidden award(s) have saved data that will be deleted:\n\n${hiddenAwardsWithData.join(', ')}\n\nOnly visible awards will be saved. Continue?`;
        if (!confirm(message)) {
            return;
        }
    }
    
    const awards = [];
    
    // Collect awards from visible form only
    const selects = document.querySelectorAll('.member-select');
    selects.forEach(select => {
        const memberId = parseInt(select.value);
        if (memberId) {
            awards.push({
                award_type: select.dataset.award,
                rank: parseInt(select.dataset.rank),
                member_id: memberId
            });
        }
    });
    
    try {
        const response = await fetch(API_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ week_date: weekDate, awards })
        });
        
        if (!response.ok) {
            throw new Error('Failed to save awards');
        }
        
        alert('✓ Awards saved successfully!');
        await loadHistory();
    } catch (error) {
        console.error('Error saving awards:', error);
        alert('Failed to save awards: ' + error.message);
    }
}

// Clear awards
async function clearAwards() {
    const weekDate = formatDate(currentWeekDate);
    
    if (!confirm('Clear all awards for this week? This cannot be undone.')) {
        return;
    }
    
    try {
        const response = await fetch(`${API_URL}/${weekDate}`, {
            method: 'DELETE'
        });
        
        if (!response.ok && response.status !== 204) {
            throw new Error('Failed to clear awards');
        }
        
        currentAwards = {};
        renderAwardsForm();
        await loadHistory();
        alert('✓ Awards cleared for this week.');
    } catch (error) {
        console.error('Error clearing awards:', error);
        alert('Failed to clear awards: ' + error.message);
    }
}

// Load history
async function loadHistory() {
    try {
        const response = await fetch(API_URL);
        allHistory = await response.json();
        
        // Get unique weeks
        const weeks = [...new Set(allHistory.map(a => a.week_date))].sort().reverse();
        
        // Populate week filter
        const weekFilter = document.getElementById('week-filter');
        if (weekFilter) {
            weekFilter.innerHTML = '<option value="">All Weeks</option>';
            weeks.forEach(week => {
                const option = document.createElement('option');
                option.value = week;
                option.textContent = formatDisplayDate(week);
                weekFilter.appendChild(option);
            });
        }
        
        renderHistory();
    } catch (error) {
        console.error('Error loading history:', error);
        const historyContent = document.getElementById('history-content');
        if(historyContent) {
            historyContent.innerHTML = '<p class="empty">Error loading history.</p>';
        }
    }
}

// Render history
function renderHistory() {
    const searchInput = document.getElementById('history-search');
    const weekFilterSelect = document.getElementById('week-filter');
    const content = document.getElementById('history-content');
    
    if(!content) return;

    const searchTerm = searchInput ? searchInput.value.toLowerCase().trim() : '';
    const weekFilter = weekFilterSelect ? weekFilterSelect.value : '';
    
    let filtered = allHistory;
    
    if (weekFilter) {
        filtered = filtered.filter(a => a.week_date === weekFilter);
    }
    
    if (searchTerm) {
        filtered = filtered.filter(a => a.member_name.toLowerCase().includes(searchTerm));
    }
    
    if (filtered.length === 0) {
        content.innerHTML = '<p class="empty">No awards found.</p>';
        return;
    }
    
    // Group by week
    const groupedByWeek = {};
    filtered.forEach(award => {
        if (!groupedByWeek[award.week_date]) {
            groupedByWeek[award.week_date] = {};
        }
        if (!groupedByWeek[award.week_date][award.award_type]) {
            groupedByWeek[award.week_date][award.award_type] = [];
        }
        groupedByWeek[award.week_date][award.award_type].push(award);
    });
    
    let html = '';
    
    Object.keys(groupedByWeek).sort().reverse().forEach(week => {
        html += `<div class="week-history">`;
        html += `<h4 class="week-history-title">📅 ${formatDisplayDate(week)}</h4>`;
        html += `<div class="awards-history-grid">`;
        
        Object.keys(groupedByWeek[week]).sort().forEach(awardType => {
            const awards = groupedByWeek[week][awardType].sort((a, b) => a.rank - b.rank);
            
            html += `<div class="history-award-card">`;
            html += `<h5 class="history-award-title">${awardType}</h5>`;
            
            awards.forEach(award => {
                const rankEmoji = award.rank === 1 ? '🥇' : award.rank === 2 ? '🥈' : '🥉';
                html += `<div class="history-award-item rank-${award.rank}">`;
                html += `${rankEmoji} ${escapeHtml(award.member_name)}`;
                html += `</div>`;
            });
            
            html += `</div>`;
        });
        
        html += `</div>`;
        html += `</div>`;
    });
    
    content.innerHTML = html;
}

// Escape HTML
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Add new award type
async function addNewAwardType(name) {
    if (!name || !name.trim()) {
        alert('Please enter an award name');
        return false;
    }
    
    // Check if already exists
    if (AWARD_TYPES.includes(name.trim())) {
        alert('This award type already exists');
        return false;
    }
    
    try {
        const response = await fetch(AWARD_TYPES_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: name.trim() })
        });
        
        if (response.ok) {
            // Reload award types
            await loadAwardTypes();
            activeAwardTypes.add(name.trim());
            renderAwardsForm(true);
            document.getElementById('new-award-search').value = '';
            document.getElementById('award-suggestions').style.display = 'none';
            return true;
        } else if (response.status === 409) {
            alert('This award type already exists in the database');
            return false;
        } else {
            const error = await response.text();
            alert('Error adding award type: ' + error);
            return false;
        }
    } catch (error) {
        console.error('Error adding award type:', error);
        alert('Error adding award type. Please try again.');
        return false;
    }
}

// Setup award search and suggestions
function setupAwardSearch() {
    const searchInput = document.getElementById('new-award-search');
    const addBtn = document.getElementById('add-award-btn');
    const suggestionsDiv = document.getElementById('award-suggestions');
    
    if (!searchInput || !addBtn || !suggestionsDiv) return;
    
    // Instant search with fuzzy matching
    searchInput.addEventListener('input', (e) => {
        const query = e.target.value.trim();
        
        if (!query) {
            suggestionsDiv.style.display = 'none';
            addBtn.textContent = '➕ Add New Award';
            addBtn.disabled = false;
            return;
        }
        
        // Find matching awards (both active and inactive)
        const matches = allAwardTypesData.filter(at => fuzzyMatch(at.name, query));
        
        // Separate into active and inactive
        const activeMatches = matches.filter(at => at.active && activeAwardTypes.has(at.name));
        const inactiveMatches = matches.filter(at => at.active && !activeAwardTypes.has(at.name));
        const allInactiveMatches = matches.filter(at => !at.active);
        
        // Check if exact match exists
        const exactMatch = allAwardTypesData.find(at => at.name.toLowerCase() === query.toLowerCase());
        
        if (exactMatch) {
            if (exactMatch.active && activeAwardTypes.has(exactMatch.name)) {
                addBtn.textContent = '✓ Already Active';
                addBtn.disabled = true;
            } else if (exactMatch.active && !activeAwardTypes.has(exactMatch.name)) {
                addBtn.textContent = '↻ Activate';
                addBtn.disabled = false;
            } else {
                addBtn.textContent = '↻ Reactivate';
                addBtn.disabled = false;
            }
        } else {
            addBtn.textContent = '➕ Add New Award';
            addBtn.disabled = false;
        }
        
        // Show suggestions
        if (inactiveMatches.length > 0 || allInactiveMatches.length > 0) {
            let html = '<div style="padding: 10px; background: white; border-radius: 6px; border: 1px solid #dee2e6;">';
            
            if (inactiveMatches.length > 0) {
                html += '<strong style="display: block; color: #667eea; font-size: 0.9em; margin-bottom: 5px;">Hidden Awards:</strong>';
                html += '<div style="display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 10px;">';
                inactiveMatches.forEach(at => {
                    html += `<button class="inactive-award-chip" data-award="${escapeHtml(at.name)}" data-action="activate" style="background: #e7f3ff; border-color: #667eea; cursor: pointer;">${escapeHtml(at.name)} <span style="color: #667eea;">↻</span></button>`;
                });
                html += '</div>';
            }
            
            if (allInactiveMatches.length > 0) {
                html += '<strong style="display: block; color: #dc3545; font-size: 0.9em; margin-bottom: 5px;">Inactive Awards:</strong>';
                html += '<div style="display: flex; flex-wrap: wrap; gap: 8px;">';
                allInactiveMatches.forEach(at => {
                    html += `<button class="inactive-award-chip" data-award="${escapeHtml(at.name)}" data-id="${at.id}" data-action="reactivate" style="background: #fff3cd; border-color: #ffc107; cursor: pointer; position: relative;">${escapeHtml(at.name)} <span style="color: #28a745;">↻</span> <span style="color: #dc3545; margin-left: 5px; font-weight: bold;" data-delete="${at.id}">✕</span></button>`;
                });
                html += '</div>';
            }
            
            html += '</div>';
            suggestionsDiv.innerHTML = html;
            suggestionsDiv.style.display = 'block';
            
            // Add click handlers for activate
            suggestionsDiv.querySelectorAll('button[data-action="activate"]').forEach(chip => {
                chip.addEventListener('click', async (e) => {
                    e.preventDefault();
                    const award = e.currentTarget.dataset.award;
                    activeAwardTypes.add(award);
                    renderAwardsForm(true);
                    searchInput.value = '';
                    suggestionsDiv.style.display = 'none';
                });
            });
            
            // Add click handlers for reactivate
            suggestionsDiv.querySelectorAll('button[data-action="reactivate"]').forEach(chip => {
                chip.addEventListener('click', async (e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    
                    // Check if clicking delete icon
                    if (e.target.dataset.delete) {
                        return;
                    }
                    
                    const id = parseInt(e.currentTarget.dataset.id);
                    const award = e.currentTarget.dataset.award;
                    
                    try {
                        const response = await fetch(`${AWARD_TYPES_URL}/${id}`, {
                            method: 'PUT',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ name: award, active: true })
                        });
                        
                        if (response.ok) {
                            await loadAwardTypes();
                            activeAwardTypes.add(award);
                            renderAwardsForm(true);
                            searchInput.value = '';
                            suggestionsDiv.style.display = 'none';
                        } else {
                            alert('Failed to reactivate award type');
                        }
                    } catch (error) {
                        console.error('Error reactivating award:', error);
                        alert('Error reactivating award type');
                    }
                });
            });
            
            // Add delete handlers
            suggestionsDiv.querySelectorAll('span[data-delete]').forEach(span => {
                span.addEventListener('click', async (e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    
                    const id = parseInt(e.target.dataset.delete);
                    const button = e.target.closest('button');
                    const awardName = button.dataset.award;
                    
                    if (!confirm(`Are you sure you want to completely delete "${awardName}"?\n\nThis will remove it and all its history permanently!`)) {
                        return;
                    }
                    
                    try {
                        const response = await fetch(`${AWARD_TYPES_URL}/${id}?force=true`, {
                            method: 'DELETE'
                        });
                        
                        if (response.ok) {
                            await loadAwardTypes();
                            renderAwardsForm(true);
                            searchInput.value = '';
                            suggestionsDiv.style.display = 'none';
                            alert(`Award type "${awardName}" deleted successfully!`);
                        } else {
                            const error = await response.text();
                            alert(`Failed to delete: ${error}`);
                        }
                    } catch (error) {
                        console.error('Error deleting award:', error);
                        alert('Error deleting award type');
                    }
                });
            });
        } else if (activeMatches.length > 0 && !exactMatch) {
            let html = '<div style="padding: 10px; background: #d4edda; border-radius: 6px; border: 1px solid #c3e6cb; color: #155724; font-size: 0.9em;">';
            html += '💡 Similar active awards: ';
            html += activeMatches.map(at => `<strong>${escapeHtml(at.name)}</strong>`).join(', ');
            html += '</div>';
            suggestionsDiv.innerHTML = html;
            suggestionsDiv.style.display = 'block';
        } else {
            suggestionsDiv.style.display = 'none';
        }
    });
    
    // Add/Activate button handler
    addBtn.addEventListener('click', async () => {
        const name = searchInput.value.trim();
        if (!name || addBtn.disabled) return;
        
        const exactMatch = allAwardTypesData.find(at => at.name.toLowerCase() === name.toLowerCase());
        
        if (exactMatch) {
            if (exactMatch.active && !activeAwardTypes.has(exactMatch.name)) {
                // Just activate in UI
                activeAwardTypes.add(exactMatch.name);
                renderAwardsForm(true);
                searchInput.value = '';
                suggestionsDiv.style.display = 'none';
            } else if (!exactMatch.active) {
                // Reactivate in database
                try {
                    const response = await fetch(`${AWARD_TYPES_URL}/${exactMatch.id}`, {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ name: exactMatch.name, active: true })
                    });
                    
                    if (response.ok) {
                        await loadAwardTypes();
                        activeAwardTypes.add(exactMatch.name);
                        renderAwardsForm(true);
                        searchInput.value = '';
                        suggestionsDiv.style.display = 'none';
                    }
                } catch (error) {
                    console.error('Error reactivating:', error);
                }
            }
        } else {
            // Add new
            await addNewAwardType(name);
        }
    });
    
    // Enter key handler
    searchInput.addEventListener('keypress', async (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            addBtn.click();
        }
    });
}

// Update toggle button text
function updateToggleButton() {
    const btn = document.getElementById('toggle-all-awards-btn');
    if (btn) {
        if (activeAwardTypes.size === AWARD_TYPES.length) {
            btn.textContent = '⚙️ Hide All Awards';
        } else if (activeAwardTypes.size === 0) {
            btn.textContent = '⚙️ Show All Awards';
        } else {
            btn.textContent = `⚙️ Show All (${activeAwardTypes.size}/${AWARD_TYPES.length})`;
        }
    }
}

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    // Guard: Only initialize if we are on the Awards page
    const saveAwardsBtn = document.getElementById('save-awards-btn');
    if (saveAwardsBtn) {
        await loadAwardTypes(); // Load award types first
        await loadMembers();
        initializeWeek();
        await loadAwards();
        await loadHistory();
        
        // Set up award search
        setupAwardSearch();
        
        // Set up event listeners
        document.getElementById('prev-week').addEventListener('click', navigatePrevWeek);
        document.getElementById('next-week').addEventListener('click', navigateNextWeek);
        saveAwardsBtn.addEventListener('click', saveAwards);
        document.getElementById('clear-awards-btn').addEventListener('click', clearAwards);
        document.getElementById('history-search').addEventListener('input', renderHistory);
        document.getElementById('week-filter').addEventListener('change', renderHistory);
        
        // Toggle all awards button
        document.getElementById('toggle-all-awards-btn').addEventListener('click', () => {
            if (activeAwardTypes.size === AWARD_TYPES.length) {
                // Hide all except those with assignments
                activeAwardTypes = new Set(Object.keys(currentAwards));
            } else {
                // Show all
                activeAwardTypes = new Set(AWARD_TYPES);
            }
            renderAwardsForm(true); // Preserve form state when toggling all
            updateToggleButton();
        });
    }
});