// static/train.js
const API_URL = '/api/train-schedules';
const MEMBERS_URL = '/api/members';
const MEMBER_STATS_URL = '/api/members/stats';

let currentWeekStart = null;
let allMembers = [];
let memberStats = {};
let backupMembers = [];
let schedules = {};
let allHistory = [];
let currentUserRank = '';
let isAdmin = false;

// Fetch permissions to determine scheduling access
async function fetchPermissions() {
    try {
        const response = await fetch('/api/check-auth');
        if (response.ok) {
            const data = await response.json();
            currentUserRank = data.rank || '';
            isAdmin = data.is_admin || false;
        }
    } catch (error) {
        console.error('Auth check error:', error);
    }
}

// Check if user can edit train schedules (R4, R5, or admin)
function canEditSchedule() {
    return isAdmin || currentUserRank === 'R4' || currentUserRank === 'R5';
}

// Get Monday of current week
function getMondayOfWeek(date) {
    const d = new Date(date);
    const day = d.getDay();
    const diff = d.getDate() - day + (day === 0 ? -6 : 1); // Adjust when day is Sunday
    return new Date(d.setDate(diff));
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
    const days = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
    const day = String(date.getDate()).padStart(2, '0');
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const year = date.getFullYear();
    
    return `${days[date.getDay()]}, ${day}/${month}/${year}`;
}

// Initialize current week
function initializeWeek() {
    currentWeekStart = getMondayOfWeek(new Date());
    updateWeekDisplay();
}

// Update week display
function updateWeekDisplay() {
    const endDate = new Date(currentWeekStart);
    endDate.setDate(endDate.getDate() + 6);
    
    const startMonth = currentWeekStart.toLocaleString('default', { month: 'long' });
    const endMonth = endDate.toLocaleString('default', { month: 'long' });
    const year = currentWeekStart.getFullYear();
    
    let displayText;
    if (startMonth === endMonth) {
        displayText = `Week of ${startMonth} ${currentWeekStart.getDate()}-${endDate.getDate()}, ${year}`;
    } else {
        displayText = `Week of ${startMonth} ${currentWeekStart.getDate()} - ${endMonth} ${endDate.getDate()}, ${year}`;
    }
    
    document.getElementById('week-display').textContent = displayText;
}

// Hide weekly message section
function hideWeeklyMessage() {
    document.getElementById('weekly-message-section').style.display = 'none';
}

// Generate weekly message
async function generateWeeklyMessage() {
    const startDate = formatDate(currentWeekStart);
    
    try {
        const response = await fetch(`/api/train-schedules/weekly-message?start=${startDate}`);
        if (!response.ok) throw new Error('Failed to generate message');
        
        const data = await response.json();
        document.getElementById('weekly-message').value = data.message;
        document.getElementById('weekly-message-section').style.display = 'block';
        
        // Scroll to message
        document.getElementById('weekly-message-section').scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    } catch (error) {
        console.error('Error generating message:', error);
        alert('Failed to generate weekly message');
    }
}

// Show daily message section
function showDailyMessageSection() {
    document.getElementById('daily-message-section').style.display = 'block';
    
    // Set default date to today
    const today = new Date();
    document.getElementById('daily-message-date').value = formatDate(today);
    
    // Scroll to message section
    document.getElementById('daily-message-section').scrollIntoView({ behavior: 'smooth', block: 'nearest' });
}

// Generate conductor reminder messages
async function generateConductorMessages() {
    const startDate = formatDate(currentWeekStart);
    
    try {
        const response = await fetch(`/api/train-schedules/conductor-messages?start=${startDate}`);
        if (!response.ok) throw new Error('Failed to generate conductor messages');
        
        const data = await response.json();
        
        // Display messages
        const messagesContainer = document.getElementById('conductor-messages-list');
        messagesContainer.innerHTML = '';
        
        data.messages.forEach((msg, index) => {
            const messageCard = document.createElement('div');
            messageCard.className = 'conductor-message-card';
            messageCard.innerHTML = `
                <div class="conductor-message-header">
                    <strong>${msg.day} – ${msg.name}</strong>
                </div>
                <div class="conductor-message-content">
                    <textarea readonly rows="4" id="conductor-msg-${index}">${msg.message}</textarea>
                </div>
                <button class="copy-btn copy-conductor-msg" data-index="${index}">📋 Copy Message</button>
            `;
            messagesContainer.appendChild(messageCard);
        });
        
        document.getElementById('conductor-messages-section').style.display = 'block';
        document.getElementById('conductor-messages-section').scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        
        // Add event listeners to copy buttons
        document.querySelectorAll('.copy-conductor-msg').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const index = e.target.dataset.index;
                const textarea = document.getElementById(`conductor-msg-${index}`);
                textarea.select();
                document.execCommand('copy');
                
                // Visual feedback
                const originalText = e.target.textContent;
                e.target.textContent = '✅ Copied!';
                setTimeout(() => {
                    e.target.textContent = originalText;
                }, 2000);
            });
        });
        
    } catch (error) {
        console.error('Error generating conductor messages:', error);
        alert('Failed to generate conductor reminder messages');
    }
}

// Load members
async function loadMembers() {
    try {
        const response = await fetch(MEMBERS_URL);
        allMembers = await response.json();
        // Sort members case-insensitively by name
        allMembers.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
        backupMembers = allMembers.filter(m => m.rank === 'R4' || m.rank === 'R5');
        
        // Load member statistics
        await loadMemberStats();
    } catch (error) {
        console.error('Error loading members:', error);
    }
}

// Load member statistics
async function loadMemberStats() {
    try {
        const response = await fetch(MEMBER_STATS_URL);
        const stats = await response.json();
        
        // Convert to object keyed by member ID for easy lookup
        memberStats = {};
        stats.forEach(stat => {
            memberStats[stat.id] = stat;
        });
    } catch (error) {
        console.error('Error loading member stats:', error);
    }
}

// Load schedules for current week
async function loadSchedules() {
    const startDate = formatDate(currentWeekStart);
    const endDate = new Date(currentWeekStart);
    endDate.setDate(endDate.getDate() + 6);
    const endDateStr = formatDate(endDate);
    
    try {
        const response = await fetch(`${API_URL}?start=${startDate}&end=${endDateStr}`);
        const data = await response.json();
        
        schedules = {};
        data.forEach(schedule => {
            schedules[schedule.date] = schedule;
        });
        
        renderScheduleGrid();
    } catch (error) {
        console.error('Error loading schedules:', error);
        document.getElementById('schedule-grid').innerHTML = 
            '<p class="empty">Error loading schedules. Please try again.</p>';
    }
}

// Render schedule grid
function renderScheduleGrid() {
    const grid = document.getElementById('schedule-grid');
    const days = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];
    
    let html = '';
    
    for (let i = 0; i < 7; i++) {
        const date = new Date(currentWeekStart);
        date.setDate(date.getDate() + i);
        const dateStr = formatDate(date);
        const schedule = schedules[dateStr];
        const isPast = date < new Date(new Date().setHours(0, 0, 0, 0));
        
        html += `<div class="day-card ${isPast ? 'past' : ''}">`;
        html += `<div class="day-header">`;
        html += `<h4>${days[i]}</h4>`;
        html += `<div class="day-date">${date.getDate()}/${date.getMonth() + 1}</div>`;
        html += `</div>`;
        
        if (schedule) {
            const showedUpClass = schedule.conductor_showed_up === null ? '' : 
                                schedule.conductor_showed_up ? 'success' : 'warning';
            
            html += `<div class="schedule-info ${showedUpClass}">`;
            html += `<div class="conductor">`;
            html += `<strong>Conductor:</strong><br>${escapeHtml(schedule.conductor_name)}`;
            if (schedule.conductor_score !== null && schedule.conductor_score !== undefined) {
                html += ` <span class="score-badge">${schedule.conductor_score} pts</span>`;
            }
            if (schedule.conductor_showed_up !== null) {
                html += schedule.conductor_showed_up ? 
                    ' <span class="status-badge success">✓ Showed up</span>' :
                    ' <span class="status-badge warning">✗ Absent</span>';
            }
            html += `</div>`;
            html += `<div class="backup">`;
            html += `<strong>Backup:</strong><br>${escapeHtml(schedule.backup_name)} (${schedule.backup_rank})`;
            if (schedule.conductor_showed_up === false) {
                html += ' <span class="status-badge active">🚂 Stepped in</span>';
            }
            html += `</div>`;
            if (schedule.notes) {
                html += `<div class="notes"><strong>Notes:</strong> ${escapeHtml(schedule.notes)}</div>`;
            }
            if (canEditSchedule()) {
                html += `<div class="schedule-actions">`;
                html += `<button class="edit-schedule-btn" onclick="editSchedule('${dateStr}')">✏️ Edit</button>`;
                html += `<button class="clear-schedule-btn" onclick="clearSchedule(${schedule.id}, '${dateStr}')">🗑️ Clear</button>`;
                html += `</div>`;
            }
            html += `</div>`;
        } else {
            html += `<div class="no-schedule">`;
            html += `<p>Not scheduled</p>`;
            if (canEditSchedule()) {
                html += `<div class="schedule-actions">`;
                html += `<button class="schedule-btn" onclick="openScheduleModal('${dateStr}')">✏️ Schedule</button>`;
                html += `</div>`;
            }
            html += `</div>`;
        }
        
        html += `</div>`;
    }
    
    grid.innerHTML = html;
}

// Make globally accessible since they are called via inline onclick
window.openScheduleModal = function(dateStr) {
    if (!canEditSchedule()) {
        alert('Only R4, R5 ranks and admins can edit the train schedule.');
        return;
    }
    
    const modal = document.getElementById('schedule-modal');
    const form = document.getElementById('schedule-form');
    const schedule = schedules[dateStr];
    
    // Reset form
    form.reset();
    document.getElementById('schedule-id').value = schedule ? schedule.id : '';
    document.getElementById('schedule-date').value = dateStr;
    document.getElementById('display-date').textContent = formatDisplayDate(dateStr);
    
    // Reset search inputs
    document.getElementById('conductor-search').value = '';
    document.getElementById('backup-search').value = '';
    
    // Populate conductor select
    populateConductorSelect(allMembers, schedule);
    
    // Populate backup select (R4 and R5 only)
    populateBackupSelect(backupMembers, schedule);
    
    // Setup search filters
    setupDropdownSearch();
    
    // Show/hide attendance group
    const attendanceGroup = document.getElementById('attendance-group');
    const dateObj = new Date(dateStr + 'T00:00:00');
    const today = new Date(new Date().setHours(0, 0, 0, 0));
    const isNotFuture = dateObj <= today;
    
    if (isNotFuture && schedule) {
        attendanceGroup.style.display = 'block';
        if (schedule.conductor_showed_up !== null) {
            document.querySelector(`input[name="attendance"][value="${schedule.conductor_showed_up ? 'yes' : 'no'}"]`).checked = true;
        }
    } else {
        attendanceGroup.style.display = 'none';
    }
    
    // Set notes
    if (schedule && schedule.notes) {
        document.getElementById('notes').value = schedule.notes;
    }
    
    document.getElementById('modal-title').textContent = schedule ? 'Edit Schedule' : 'Schedule Train';
    modal.style.display = 'flex';
};

window.editSchedule = function(dateStr) {
    if (!canEditSchedule()) {
        alert('Only R4, R5 ranks and admins can edit the train schedule.');
        return;
    }
    openScheduleModal(dateStr);
};

window.clearSchedule = async function(scheduleId, dateStr) {
    if (!canEditSchedule()) {
        alert('Only R4, R5 ranks and admins can edit the train schedule.');
        return;
    }
    
    if (!confirm('Are you sure you want to clear this schedule?')) {
        return;
    }
    
    try {
        const response = await fetch(`${API_URL}/${scheduleId}`, {
            method: 'DELETE'
        });
        
        if (!response.ok && response.status !== 204) {
            throw new Error('Failed to clear schedule');
        }
        
        await loadSchedules();
        await loadHistory();
    } catch (error) {
        console.error('Error clearing schedule:', error);
        alert('Failed to clear schedule: ' + error.message);
    }
};

// Populate conductor select
function populateConductorSelect(members, schedule) {
    const conductorSelect = document.getElementById('conductor-select');
    conductorSelect.innerHTML = '';
    
    members.forEach(member => {
        const option = document.createElement('option');
        option.value = member.id;
        
        // Build option text with stats
        let optionText = `${member.name} (${member.rank})`;
        const stats = memberStats[member.id];
        if (stats) {
            const statsInfo = [];
            if (stats.conductor_count > 0) {
                statsInfo.push(`${stats.conductor_count}x conductor`);
            }
            if (stats.conductor_no_show_count > 0) {
                statsInfo.push(`⚠️ ${stats.conductor_no_show_count}x unreliable`);
            }
            if (stats.backup_used_count > 0) {
                statsInfo.push(`${stats.backup_used_count}x backup used`);
            }
            if (stats.last_conductor_date) {
                const lastDate = new Date(stats.last_conductor_date + 'T00:00:00');
                const day = String(lastDate.getDate()).padStart(2, '0');
                const month = String(lastDate.getMonth() + 1).padStart(2, '0');
                const year = lastDate.getFullYear();
                statsInfo.push(`last: ${day}/${month}/${year}`);
            }
            if (statsInfo.length > 0) {
                optionText += ` - ${statsInfo.join(', ')}`;
            }
        }
        
        option.textContent = optionText;
        option.dataset.name = member.name.toLowerCase();
        option.dataset.rank = member.rank.toLowerCase();
        if (schedule && member.id === schedule.conductor_id) {
            option.selected = true;
        }
        conductorSelect.appendChild(option);
    });
}

// Populate backup select
function populateBackupSelect(members, schedule) {
    const backupSelect = document.getElementById('backup-select');
    backupSelect.innerHTML = '';
    
    members.forEach(member => {
        const option = document.createElement('option');
        option.value = member.id;
        
        // Build option text with stats
        let optionText = member.name;
        const stats = memberStats[member.id];
        if (stats && stats.backup_used_count > 0) {
            optionText += ` (used as backup ${stats.backup_used_count}x)`;
        }
        
        option.textContent = optionText;
        option.dataset.name = member.name.toLowerCase();
        if (schedule && member.id === schedule.backup_id) {
            option.selected = true;
        }
        backupSelect.appendChild(option);
    });
}

// Setup dropdown search functionality
function setupDropdownSearch() {
    const conductorSearch = document.getElementById('conductor-search');
    const conductorSelect = document.getElementById('conductor-select');
    const backupSearch = document.getElementById('backup-search');
    const backupSelect = document.getElementById('backup-select');
    
    // Filter conductor dropdown
    conductorSearch.addEventListener('input', (e) => {
        const searchTerm = e.target.value.toLowerCase().trim();
        filterSelectOptions(conductorSelect, searchTerm);
    });
    
    // Filter backup dropdown
    backupSearch.addEventListener('input', (e) => {
        const searchTerm = e.target.value.toLowerCase().trim();
        filterSelectOptions(backupSelect, searchTerm);
    });
}

// Filter select options
function filterSelectOptions(selectElement, searchTerm) {
    const options = selectElement.options;
    let visibleCount = 0;
    
    for (let i = 0; i < options.length; i++) {
        const option = options[i];
        const name = option.dataset.name || '';
        const rank = option.dataset.rank || '';
        
        if (name.includes(searchTerm) || rank.includes(searchTerm)) {
            option.style.display = '';
            visibleCount++;
        } else {
            option.style.display = 'none';
        }
    }
    
    // Auto-select if only one visible option
    if (visibleCount === 1 && searchTerm) {
        for (let i = 0; i < options.length; i++) {
            if (options[i].style.display !== 'none') {
                selectElement.selectedIndex = i;
                break;
            }
        }
    }
}

// Load history
async function loadHistory() {
    try {
        const response = await fetch(API_URL);
        allHistory = await response.json();
        allHistory.sort((a, b) => new Date(b.date) - new Date(a.date));
        renderHistory('all');
    } catch (error) {
        console.error('Error loading history:', error);
        document.getElementById('history-list').innerHTML = 
            '<p class="empty">Error loading history.</p>';
    }
}

// Render history
function renderHistory(filter) {
    let filtered = allHistory;
    
    if (filter === 'completed') {
        filtered = allHistory.filter(s => s.conductor_showed_up !== null);
    } else if (filter === 'backup') {
        filtered = allHistory.filter(s => s.conductor_showed_up === false);
    }
    
    const list = document.getElementById('history-list');
    
    if (filtered.length === 0) {
        list.innerHTML = '<p class="empty">No records found.</p>';
        return;
    }
    
    let html = '<div class="history-grid">';
    
    filtered.slice(0, 50).forEach(schedule => {
        const showedUpClass = schedule.conductor_showed_up === null ? '' : 
                            schedule.conductor_showed_up ? 'success' : 'warning';
        
        html += `<div class="history-card ${showedUpClass}">`;
        html += `<div class="history-date">${formatDisplayDate(schedule.date)}</div>`;
        html += `<div class="history-details">`;
        html += `<div><strong>Conductor:</strong> ${escapeHtml(schedule.conductor_name)}`;
        if (schedule.conductor_showed_up !== null) {
            html += schedule.conductor_showed_up ? 
                ' <span class="status-badge success">✓</span>' :
                ' <span class="status-badge warning">✗</span>';
        }
        html += `</div>`;
        html += `<div><strong>Backup:</strong> ${escapeHtml(schedule.backup_name)}`;
        if (schedule.conductor_showed_up === false) {
            html += ' <span class="status-badge active">🚂 Stepped in</span>';
        }
        html += `</div>`;
        if (schedule.notes) {
            html += `<div class="history-notes">${escapeHtml(schedule.notes)}</div>`;
        }
        html += `</div>`;
        html += `</div>`;
    });
    
    html += '</div>';
    list.innerHTML = html;
}

// Auto-schedule entire week
async function autoScheduleWeek() {
    // Check if any schedules exist for the current week
    const hasExistingSchedules = Object.keys(schedules).some(dateStr => schedules[dateStr]);
    
    if (hasExistingSchedules) {
        if (!confirm('This will automatically schedule the entire week (7 days) with the top 7 performers. Continue?')) {
            return;
        }
    }
    
    try {
        const response = await fetch(`${API_URL}/auto-schedule`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ start_date: formatDate(currentWeekStart) })
        });
        
        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || 'Failed to auto-schedule week');
        }
        
        await loadSchedules();
        await loadHistory();
    } catch (error) {
        console.error('Error auto-scheduling week:', error);
        alert('Failed to auto-schedule week: ' + error.message);
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
    // Guard: Only initialize if we are on the Train page
    const scheduleGrid = document.getElementById('schedule-grid');
    if (scheduleGrid) {
        await fetchPermissions();
        await loadMembers();
        initializeWeek();
        await loadSchedules();
        await loadHistory();
        
        // --- Static Event Listeners ---
        document.getElementById('prev-week').addEventListener('click', () => {
            currentWeekStart.setDate(currentWeekStart.getDate() - 7);
            updateWeekDisplay();
            loadSchedules();
            hideWeeklyMessage();
        });

        document.getElementById('next-week').addEventListener('click', () => {
            currentWeekStart.setDate(currentWeekStart.getDate() + 7);
            updateWeekDisplay();
            loadSchedules();
            hideWeeklyMessage();
        });

        document.getElementById('copy-message-btn').addEventListener('click', () => {
            const messageText = document.getElementById('weekly-message');
            messageText.select();
            document.execCommand('copy');
            
            const btn = document.getElementById('copy-message-btn');
            const originalText = btn.textContent;
            btn.textContent = '✅ Copied!';
            setTimeout(() => { btn.textContent = originalText; }, 2000);
        });

        document.getElementById('load-daily-message-btn').addEventListener('click', async () => {
            const dateInput = document.getElementById('daily-message-date').value;
            if (!dateInput) {
                alert('Please select a date');
                return;
            }
            
            try {
                const response = await fetch(`/api/train-schedules/daily-message?date=${dateInput}`);
                if (!response.ok) {
                    if (response.status === 404) {
                        alert('No schedule found for this date. Please create a schedule first.');
                    } else {
                        throw new Error('Failed to generate message');
                    }
                    return;
                }
                
                const data = await response.json();
                document.getElementById('daily-message').value = data.message;
            } catch (error) {
                console.error('Error generating daily message:', error);
                alert('Failed to generate daily message');
            }
        });

        document.getElementById('copy-daily-message-btn').addEventListener('click', () => {
            const messageText = document.getElementById('daily-message');
            messageText.select();
            document.execCommand('copy');
            
            const btn = document.getElementById('copy-daily-message-btn');
            const originalText = btn.textContent;
            btn.textContent = '✅ Copied!';
            setTimeout(() => { btn.textContent = originalText; }, 2000);
        });
        
        document.querySelector('.close').addEventListener('click', () => {
            document.getElementById('schedule-modal').style.display = 'none';
        });

        document.getElementById('modal-cancel').addEventListener('click', () => {
            document.getElementById('schedule-modal').style.display = 'none';
        });

        // Form submission
        document.getElementById('schedule-form').addEventListener('submit', async (e) => {
            e.preventDefault();
            
            const id = document.getElementById('schedule-id').value;
            const date = document.getElementById('schedule-date').value;
            const conductorId = parseInt(document.getElementById('conductor-select').value);
            const backupId = parseInt(document.getElementById('backup-select').value);
            const notes = document.getElementById('notes').value.trim() || null;
            
            const attendanceRadio = document.querySelector('input[name="attendance"]:checked');
            let conductorShowedUp = null;
            if (attendanceRadio) {
                conductorShowedUp = attendanceRadio.value === 'yes';
            }
            
            const data = { date, conductor_id: conductorId, backup_id: backupId, conductor_showed_up: conductorShowedUp, notes };
            
            try {
                let response;
                if (id) {
                    response = await fetch(`${API_URL}/${id}`, {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(data)
                    });
                } else {
                    response = await fetch(API_URL, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(data)
                    });
                }
                
                if (!response.ok) {
                    const error = await response.text();
                    throw new Error(error);
                }
                
                document.getElementById('schedule-modal').style.display = 'none';
                await loadSchedules();
                await loadHistory();
            } catch (error) {
                console.error('Error saving schedule:', error);
                alert('Failed to save schedule: ' + error.message);
            }
        });

        // History Filters
        document.getElementById('show-all-history').addEventListener('click', function() {
            document.querySelectorAll('.filter-btn').forEach(btn => btn.classList.remove('active'));
            this.classList.add('active');
            renderHistory('all');
        });

        document.getElementById('show-completed').addEventListener('click', function() {
            document.querySelectorAll('.filter-btn').forEach(btn => btn.classList.remove('active'));
            this.classList.add('active');
            renderHistory('completed');
        });

        document.getElementById('show-backup-used').addEventListener('click', function() {
            document.querySelectorAll('.filter-btn').forEach(btn => btn.classList.remove('active'));
            this.classList.add('active');
            renderHistory('backup');
        });

        // Setup editing controls based on permissions
        if (canEditSchedule()) {
            document.getElementById('auto-schedule-week-btn').addEventListener('click', autoScheduleWeek);
            document.getElementById('generate-message-btn').addEventListener('click', generateWeeklyMessage);
            document.getElementById('generate-daily-message-btn').addEventListener('click', showDailyMessageSection);
            document.getElementById('generate-conductor-messages-btn').addEventListener('click', generateConductorMessages);
        } else {
            // Show read-only notice
            document.getElementById('read-only-notice').style.display = 'block';
            
            // Hide action buttons for read-only users
            document.querySelectorAll('.schedule-controls .action-buttons button').forEach(btn => {
                btn.style.display = 'none';
            });
        }
    }
});