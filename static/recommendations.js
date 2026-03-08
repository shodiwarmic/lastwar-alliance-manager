// static/recommendations.js
const API_URL = '/api/recommendations';
const MEMBERS_URL = '/api/members';

let allMembers = [];
let allRecommendations = [];
let currentUsername = '';
let currentUserId = 0;
let currentView = 'list'; // 'list' or 'grouped'
let currentFilter = 'all'; // 'all', 'active', 'assigned', 'mine'

// Fetch permissions to get the username for Delete button rendering
async function fetchPermissions() {
    try {
        const response = await fetch('/api/check-auth');
        if (response.ok) {
            const data = await response.json();
            currentUsername = data.username;
            currentUserId = data.user_id || 0;
        }
    } catch (error) {
        console.error('Auth check error:', error);
    }
}

// Load members
async function loadMembers() {
    try {
        const response = await fetch(MEMBERS_URL);
        allMembers = await response.json();
        allMembers.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
        populateMemberSelect();
    } catch (error) {
        console.error('Error loading members:', error);
        alert('Failed to load members.');
    }
}

// Populate member select dropdown
function populateMemberSelect() {
    const select = document.getElementById('member-select');
    if (!select) return;
    
    select.innerHTML = '<option value="">Select a member...</option>';
    
    allMembers.forEach(member => {
        const option = document.createElement('option');
        option.value = member.id;
        option.textContent = `${member.name} (${member.rank})`;
        select.appendChild(option);
    });
}

// Setup search filter for member dropdown
function setupMemberSearch() {
    const searchInput = document.getElementById('member-search');
    const selectElement = document.getElementById('member-select');
    
    if (!searchInput || !selectElement) return;
    
    searchInput.addEventListener('input', () => {
        const searchTerm = searchInput.value.toLowerCase().trim();
        const options = selectElement.options;
        let visibleCount = 0;
        
        for (let i = 1; i < options.length; i++) {
            const text = options[i].textContent.toLowerCase();
            if (text.includes(searchTerm)) {
                options[i].style.display = '';
                visibleCount++;
            } else {
                options[i].style.display = 'none';
            }
        }
        
        // Auto-select if only one match
        if (visibleCount === 1 && searchTerm) {
            for (let i = 1; i < options.length; i++) {
                if (options[i].style.display !== 'none') {
                    selectElement.selectedIndex = i;
                    break;
                }
            }
        }
    });
}

// Load recommendations
async function loadRecommendations() {
    try {
        const response = await fetch(API_URL);
        allRecommendations = await response.json();
        updateStatistics();
        renderRecommendations();
    } catch (error) {
        console.error('Error loading recommendations:', error);
        const list = document.getElementById('recommendations-list');
        if (list) list.innerHTML = '<p class="no-data">Failed to load recommendations.</p>';
    }
}

// Update statistics dashboard
function updateStatistics() {
    const active = allRecommendations.filter(r => !r.expired);
    const assigned = allRecommendations.filter(r => r.expired);
    
    // Count unique members recommended
    const uniqueMembers = new Set(active.map(r => r.member_id));
    
    // Find most recommended member (active only)
    const memberCounts = {};
    active.forEach(rec => {
        memberCounts[rec.member_name] = (memberCounts[rec.member_name] || 0) + 1;
    });
    
    const topMember = Object.entries(memberCounts).sort((a, b) => b[1] - a[1])[0];
    const topMemberText = topMember ? `${topMember[0]} (${topMember[1]})` : '-';
    
    const totalEl = document.getElementById('total-recommendations');
    if (totalEl) totalEl.textContent = active.length;
    
    const membersRecEl = document.getElementById('members-recommended');
    if (membersRecEl) membersRecEl.textContent = uniqueMembers.size;
    
    const topRecEl = document.getElementById('top-recommended');
    if (topRecEl) topRecEl.textContent = topMemberText;
    
    const assignedCountEl = document.getElementById('assigned-count');
    if (assignedCountEl) assignedCountEl.textContent = assigned.length;
}

// Render recommendations
function renderRecommendations() {
    const container = document.getElementById('recommendations-list');
    if (!container) return;
    
    const searchInput = document.getElementById('filter-search');
    const filterSearch = searchInput ? searchInput.value.toLowerCase() : '';
    
    // Apply filter
    let filtered = allRecommendations.filter(rec => {
        const memberMatch = rec.member_name.toLowerCase().includes(filterSearch);
        const recommenderMatch = rec.recommended_by.toLowerCase().includes(filterSearch);
        const textMatch = memberMatch || recommenderMatch;
        
        if (!textMatch) return false;
        
        switch (currentFilter) {
            case 'active':
                return !rec.expired;
            case 'assigned':
                return rec.expired;
            case 'mine':
                return rec.recommended_by === currentUsername;
            default:
                return true;
        }
    });
    
    if (filtered.length === 0) {
        container.innerHTML = '<p class="no-data">No recommendations found.</p>';
        return;
    }
    
    if (currentView === 'grouped') {
        renderGroupedView(filtered, container);
    } else {
        renderListView(filtered, container);
    }
}

// Render list view
function renderListView(recommendations, container) {
    let html = '';
    recommendations.forEach(rec => {
        const canDelete = rec.recommended_by === currentUsername;
        const date = new Date(rec.created_at);
        const formattedDate = `${String(date.getDate()).padStart(2, '0')}/${String(date.getMonth() + 1).padStart(2, '0')}/${date.getFullYear()} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
        const expiredClass = rec.expired ? ' expired' : '';
        const expiredBadge = rec.expired ? '<span class="expired-badge">✓ Assigned</span>' : '<span class="active-badge">⭐ Active</span>';
        
        html += `
            <div class="recommendation-card${expiredClass}">
                <div class="recommendation-header">
                    <div class="recommendation-member">
                        <span class="member-name">${escapeHtml(rec.member_name)}</span>
                        <span class="member-rank rank-${rec.member_rank}">${rec.member_rank}</span>
                        ${expiredBadge}
                    </div>
                    ${canDelete ? `<button class="delete-btn" onclick="deleteRecommendation(${rec.id})">🗑️ Delete</button>` : ''}
                </div>
                <div class="recommendation-body">
                    <div class="recommendation-meta">
                        <span class="recommended-by">👤 Recommended by: <strong>${escapeHtml(rec.recommended_by)}</strong></span>
                        <span class="recommendation-date">📅 ${formattedDate}</span>
                    </div>
                    ${rec.notes ? `<div class="recommendation-notes">${escapeHtml(rec.notes)}</div>` : ''}
                </div>
            </div>
        `;
    });
    
    container.innerHTML = html;
}

// Render grouped view (by member)
function renderGroupedView(recommendations, container) {
    // Group by member
    const grouped = {};
    recommendations.forEach(rec => {
        if (!grouped[rec.member_id]) {
            grouped[rec.member_id] = {
                member_name: rec.member_name,
                member_rank: rec.member_rank,
                recommendations: []
            };
        }
        grouped[rec.member_id].recommendations.push(rec);
    });
    
    // Sort by count (descending)
    const sortedGroups = Object.values(grouped).sort((a, b) => 
        b.recommendations.length - a.recommendations.length
    );
    
    let html = '';
    sortedGroups.forEach(group => {
        const activeCount = group.recommendations.filter(r => !r.expired).length;
        const assignedCount = group.recommendations.filter(r => r.expired).length;
        
        html += `
            <div class="grouped-card">
                <div class="grouped-header">
                    <div class="grouped-member-info">
                        <span class="member-name-large">${escapeHtml(group.member_name)}</span>
                        <span class="member-rank rank-${group.member_rank}">${group.member_rank}</span>
                    </div>
                    <div class="grouped-badges">
                        ${activeCount > 0 ? `<span class="count-badge active-count">${activeCount} Active</span>` : ''}
                        ${assignedCount > 0 ? `<span class="count-badge assigned-count">${assignedCount} Assigned</span>` : ''}
                    </div>
                </div>
                <div class="grouped-recommendations">
                    ${group.recommendations.map(rec => {
                        const canDelete = rec.recommended_by === currentUsername;
                        const date = new Date(rec.created_at);
                        const formattedDate = `${String(date.getDate()).padStart(2, '0')}/${String(date.getMonth() + 1).padStart(2, '0')}/${date.getFullYear()} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
                        const expiredClass = rec.expired ? 'expired-rec' : 'active-rec';
                        
                        return `
                            <div class="recommendation-item ${expiredClass}">
                                <div class="rec-item-header">
                                    <span class="rec-by">👤 ${escapeHtml(rec.recommended_by)}</span>
                                    <span class="rec-date">📅 ${formattedDate}</span>
                                    ${canDelete ? `<button class="delete-btn-small" onclick="deleteRecommendation(${rec.id})">🗑️</button>` : ''}
                                </div>
                                ${rec.notes ? `<div class="rec-notes-small">${escapeHtml(rec.notes)}</div>` : ''}
                            </div>
                        `;
                    }).join('')}
                </div>
            </div>
        `;
    });
    
    container.innerHTML = html;
}

// Submit recommendation
async function submitRecommendation() {
    const memberSelect = document.getElementById('member-select');
    const notesInput = document.getElementById('notes-input');
    
    const memberId = parseInt(memberSelect.value);
    const notes = notesInput.value.trim();
    
    if (!memberId) {
        alert('Please select a member to recommend.');
        return;
    }
    
    try {
        const response = await fetch(API_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ member_id: memberId, notes })
        });
        
        if (!response.ok) {
            throw new Error('Failed to submit recommendation');
        }
        
        // Clear form
        memberSelect.value = '';
        notesInput.value = '';
        document.getElementById('member-search').value = '';
        
        // Reload recommendations
        await loadRecommendations();
        
        alert('✓ Recommendation submitted successfully!');
    } catch (error) {
        console.error('Error submitting recommendation:', error);
        alert('Failed to submit recommendation: ' + error.message);
    }
}

// Make globally accessible since it's called via inline onclick attribute
window.deleteRecommendation = async function(id) {
    if (!confirm('Delete this recommendation?')) {
        return;
    }
    
    try {
        const response = await fetch(`${API_URL}/${id}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || 'Failed to delete recommendation');
        }
        
        await loadRecommendations();
        alert('✓ Recommendation deleted.');
    } catch (error) {
        console.error('Error deleting recommendation:', error);
        alert('Failed to delete recommendation: ' + error.message);
    }
};

// Escape HTML
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    // Guard: Only initialize if we are on the Recommendations page
    const recommendationsList = document.getElementById('recommendations-list');
    
    if (recommendationsList) {
        await fetchPermissions();
        await loadMembers();
        await loadRecommendations();
        setupMemberSearch();
        
        // Set up event listeners
        const submitBtn = document.getElementById('submit-recommendation-btn');
        if (submitBtn) submitBtn.addEventListener('click', submitRecommendation);
        
        const filterSearch = document.getElementById('filter-search');
        if (filterSearch) filterSearch.addEventListener('input', renderRecommendations);
        
        // View toggle buttons
        document.querySelectorAll('.toggle-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                document.querySelectorAll('.toggle-btn').forEach(b => b.classList.remove('active'));
                btn.classList.add('active');
                currentView = btn.dataset.view;
                renderRecommendations();
            });
        });
        
        // Filter chips
        document.querySelectorAll('.filter-chip').forEach(chip => {
            chip.addEventListener('click', () => {
                document.querySelectorAll('.filter-chip').forEach(c => c.classList.remove('active'));
                chip.classList.add('active');
                currentFilter = chip.dataset.filter;
                renderRecommendations();
            });
        });
    }
});