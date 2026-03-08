// static/vs.js
const API_URL = '/api/vs-points';
const MEMBERS_URL = '/api/members';

let currentWeekDate = null;
let allMembers = [];
let currentVSPoints = {};

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
    const weekDisplay = document.getElementById('week-display');
    if (weekDisplay) {
        weekDisplay.textContent = formatDisplayDate(formatDate(currentWeekDate));
    }
}

// Navigate weeks functions
function navigatePrevWeek() {
    currentWeekDate.setDate(currentWeekDate.getDate() - 7);
    updateWeekDisplay();
    loadVSPoints();
}

function navigateNextWeek() {
    currentWeekDate.setDate(currentWeekDate.getDate() + 7);
    updateWeekDisplay();
    loadVSPoints();
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

// Load VS points for current week
async function loadVSPoints() {
    const weekDate = formatDate(currentWeekDate);
    
    try {
        const response = await fetch(`${API_URL}?week=${weekDate}`);
        const vsPoints = await response.json();
        
        // Organize VS points by member_id
        currentVSPoints = {};
        vsPoints.forEach(point => {
            currentVSPoints[point.member_id] = point;
        });
        
        renderTable();
    } catch (error) {
        console.error('Error loading VS points:', error);
        renderTable();
    }
}

// Calculate total for a member
function calculateTotal(memberId) {
    const points = currentVSPoints[memberId];
    if (!points) return 0;
    
    return (points.monday || 0) + 
           (points.tuesday || 0) + 
           (points.wednesday || 0) + 
           (points.thursday || 0) + 
           (points.friday || 0) + 
           (points.saturday || 0);
}

// Render table
function renderTable() {
    const tbody = document.getElementById('vs-tbody');
    if (!tbody) return;
    
    const searchBox = document.getElementById('search-box');
    const searchTerm = searchBox ? searchBox.value.toLowerCase().trim() : '';
    
    let html = '';
    
    // Filter members based on search
    const filteredMembers = allMembers.filter(member => 
        member.name.toLowerCase().includes(searchTerm)
    );
    
    if (filteredMembers.length === 0) {
        html = '<tr><td colspan="8" style="text-align: center; padding: 20px;">No members found</td></tr>';
    } else {
        filteredMembers.forEach(member => {
            const points = currentVSPoints[member.id] || {
                monday: 0, tuesday: 0, wednesday: 0, thursday: 0, friday: 0, saturday: 0
            };
            
            const total = calculateTotal(member.id);
            
            html += `
                <tr data-member-id="${member.id}">
                    <td>
                        <span class="member-name">${escapeHtml(member.name)}</span>
                        <span class="member-rank">(${escapeHtml(member.rank)})</span>
                    </td>
                    <td><input type="number" class="vs-input" data-member="${member.id}" data-day="monday" value="${points.monday}" min="0"></td>
                    <td><input type="number" class="vs-input" data-member="${member.id}" data-day="tuesday" value="${points.tuesday}" min="0"></td>
                    <td><input type="number" class="vs-input" data-member="${member.id}" data-day="wednesday" value="${points.wednesday}" min="0"></td>
                    <td><input type="number" class="vs-input" data-member="${member.id}" data-day="thursday" value="${points.thursday}" min="0"></td>
                    <td><input type="number" class="vs-input" data-member="${member.id}" data-day="friday" value="${points.friday}" min="0"></td>
                    <td><input type="number" class="vs-input" data-member="${member.id}" data-day="saturday" value="${points.saturday}" min="0"></td>
                    <td class="total-column">${total}</td>
                </tr>
            `;
        });
    }
    
    tbody.innerHTML = html;
    
    // Add input event listeners to update totals
    document.querySelectorAll('.vs-input').forEach(input => {
        input.addEventListener('input', (e) => {
            const memberId = parseInt(e.target.dataset.member);
            updateCurrentVSPoints(memberId, e.target.dataset.day, parseInt(e.target.value) || 0);
            updateTotal(memberId);
        });
    });
}

// Update current VS points in memory
function updateCurrentVSPoints(memberId, day, value) {
    if (!currentVSPoints[memberId]) {
        currentVSPoints[memberId] = {
            member_id: memberId,
            monday: 0, tuesday: 0, wednesday: 0, thursday: 0, friday: 0, saturday: 0
        };
    }
    currentVSPoints[memberId][day] = value;
}

// Update total for a member
function updateTotal(memberId) {
    const row = document.querySelector(`tr[data-member-id="${memberId}"]`);
    if (!row) return;
    
    const total = calculateTotal(memberId);
    const totalCell = row.querySelector('.total-column');
    if (totalCell) {
        totalCell.textContent = total;
    }
}

// Save VS points
async function saveVSPoints() {
    const weekDate = formatDate(currentWeekDate);
    
    // Collect all points from the form
    const points = [];
    allMembers.forEach(member => {
        const vsPoint = currentVSPoints[member.id] || {};
        
        points.push({
            member_id: member.id,
            monday: parseInt(vsPoint.monday) || 0,
            tuesday: parseInt(vsPoint.tuesday) || 0,
            wednesday: parseInt(vsPoint.wednesday) || 0,
            thursday: parseInt(vsPoint.thursday) || 0,
            friday: parseInt(vsPoint.friday) || 0,
            saturday: parseInt(vsPoint.saturday) || 0
        });
    });
    
    try {
        const response = await fetch(API_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ week_date: weekDate, points })
        });
        
        if (!response.ok) {
            throw new Error('Failed to save VS points');
        }
        
        alert('✓ VS points saved successfully!');
        await loadVSPoints();
    } catch (error) {
        console.error('Error saving VS points:', error);
        alert('Failed to save VS points: ' + error.message);
    }
}

// Clear VS points
async function clearVSPoints() {
    const weekDate = formatDate(currentWeekDate);
    
    if (!confirm('Clear all VS points for this week? This cannot be undone.')) {
        return;
    }
    
    try {
        const response = await fetch(`${API_URL}/${weekDate}`, {
            method: 'DELETE'
        });
        
        if (!response.ok && response.status !== 204) {
            throw new Error('Failed to clear VS points');
        }
        
        currentVSPoints = {};
        renderTable();
        alert('✓ VS points cleared for this week.');
    } catch (error) {
        console.error('Error clearing VS points:', error);
        alert('Failed to clear VS points: ' + error.message);
    }
}

// Escape HTML
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    // Guard: Only initialize if we are on the VS Points page
    const vsTbody = document.getElementById('vs-tbody');
    if (vsTbody) {
        await loadMembers();
        initializeWeek();
        await loadVSPoints();
        
        // Set up event listeners
        const prevWeekBtn = document.getElementById('prev-week');
        if (prevWeekBtn) prevWeekBtn.addEventListener('click', navigatePrevWeek);
        
        const nextWeekBtn = document.getElementById('next-week');
        if (nextWeekBtn) nextWeekBtn.addEventListener('click', navigateNextWeek);
        
        const saveBtn = document.getElementById('save-btn');
        if (saveBtn) saveBtn.addEventListener('click', saveVSPoints);
        
        const clearBtn = document.getElementById('clear-btn');
        if (clearBtn) clearBtn.addEventListener('click', clearVSPoints);
        
        const searchBox = document.getElementById('search-box');
        if (searchBox) searchBox.addEventListener('input', renderTable);
    }
});