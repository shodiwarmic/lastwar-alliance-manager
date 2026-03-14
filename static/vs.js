// static/vs.js - Comprehensive VS Points Logic

const API_URL = '/api/vs-points';
const MEMBERS_URL = '/api/members';

let currentWeekDate = null;
let allMembers = [];
let currentVSPoints = {};
let canEditVS = false;
let isTotalMode = false;

// Sorting State
let sortColumn = 'name';
let sortAscending = true;

const daysOfWeek = ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday'];

/**
 * 1. DATE LOGIC & LOCALIZATION
 */

function getMostRecentMonday(date = new Date()) {
    const d = new Date(date);
    const day = d.getDay();
    const diff = day === 0 ? 6 : day - 1; 
    d.setDate(d.getDate() - diff);
    return d;
}

function formatDate(date) {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
}

function formatDisplayDate(dateStr) {
    const date = new Date(dateStr + 'T00:00:00');
    // Localizes the date format automatically based on the user's browser settings
    return `Week of ${date.toLocaleDateString(undefined, { 
        weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' 
    })}`;
}

function updateWeekDisplay() {
    const weekDisplay = document.getElementById('week-display');
    const nextBtn = document.getElementById('next-week');
    
    const formatted = formatDate(currentWeekDate);
    if (weekDisplay) {
        weekDisplay.textContent = formatDisplayDate(formatted);
    }

    // Disable "Next" button if we are already at the current real-world week
    if (nextBtn) {
        const todayMonday = getMostRecentMonday();
        nextBtn.disabled = (currentWeekDate >= todayMonday);
    }
}

/**
 * 2. DATA LOADING & PERMISSIONS
 */

async function checkPermissions() {
    try {
        const response = await fetch('/api/check-auth');
        if (response.ok) {
            const data = await response.json();
            
            canEditVS = data.is_admin || (data.permissions && data.permissions.manage_vs_points);
            
            if (canEditVS) {
                const adminElements = ['save-btn', 'clear-btn', 'csv-import-btn', 'toggle-mode-btn'];
                adminElements.forEach(id => {
                    document.getElementById(id)?.classList.remove('hidden');
                });
            }
        }
    } catch (e) { 
        console.error("Auth check failed:", e); 
    }
}

async function loadMembers() {
    try {
        const response = await fetch(MEMBERS_URL);
        allMembers = await response.json();
    } catch (error) {
        console.error('Error loading members:', error);
    }
}

async function loadVSPoints() {
    const weekDate = formatDate(currentWeekDate);
    try {
        const response = await fetch(`${API_URL}?week=${weekDate}`);
        const vsPoints = await response.json();
        
        currentVSPoints = {};
        if (vsPoints && Array.isArray(vsPoints)) {
            vsPoints.forEach(point => {
                currentVSPoints[point.member_id] = point;
            });
        }
        renderTable();
    } catch (error) {
        console.error('Error loading VS points:', error);
        renderTable();
    }
}

/**
 * 3. TABLE RENDERING & UI LOGIC
 */

function calculateTotal(memberId) {
    const points = currentVSPoints[memberId];
    if (!points) return 0;
    return daysOfWeek.reduce((sum, day) => sum + (parseInt(points[day]) || 0), 0);
}

function renderTable() {
    const tbody = document.getElementById('vs-tbody');
    if (!tbody) return;
    
    // Update Header Labels
    const day6Header = document.getElementById('day6-header');
    const totalHeader = document.getElementById('total-header');
    if (day6Header && totalHeader) {
        day6Header.textContent = isTotalMode ? "Day 6 (Calc)" : "Day 6";
        totalHeader.textContent = isTotalMode ? "Enter Weekly Total" : "Total";
    }

    const searchTerm = document.getElementById('search-box')?.value.toLowerCase().trim() || '';
    let filteredMembers = sortData(allMembers.filter(member => member.name.toLowerCase().includes(searchTerm)));
    
    let html = '';
    const readOnlyAttr = canEditVS ? '' : 'readonly';
    const totalModeClass = isTotalMode ? 'total-mode-input' : '';
    
    filteredMembers.forEach(member => {
        const points = currentVSPoints[member.id] || { monday: 0, tuesday: 0, wednesday: 0, thursday: 0, friday: 0, saturday: 0 };
        const total = calculateTotal(member.id);

        html += `
            <tr data-member-id="${member.id}">
                <td>
                    <span class="member-name">${escapeHtml(member.name)}</span>
                    <span class="member-rank">(${escapeHtml(member.rank)})</span>
                </td>
                <td><input type="number" class="vs-input" data-member="${member.id}" data-day="monday" value="${points.monday}" min="0" ${readOnlyAttr}></td>
                <td><input type="number" class="vs-input" data-member="${member.id}" data-day="tuesday" value="${points.tuesday}" min="0" ${readOnlyAttr}></td>
                <td><input type="number" class="vs-input" data-member="${member.id}" data-day="wednesday" value="${points.wednesday}" min="0" ${readOnlyAttr}></td>
                <td><input type="number" class="vs-input" data-member="${member.id}" data-day="thursday" value="${points.thursday}" min="0" ${readOnlyAttr}></td>
                <td><input type="number" class="vs-input" data-member="${member.id}" data-day="friday" value="${points.friday}" min="0" ${readOnlyAttr}></td>
                
                <td class="day6-column">
                    ${isTotalMode 
                        ? `<span class="day6-val" style="font-weight:bold; color:var(--text-secondary);">${points.saturday}</span>` 
                        : `<input type="number" class="vs-input" data-member="${member.id}" data-day="saturday" value="${points.saturday}" min="0" ${readOnlyAttr}>`
                    }
                </td>

                <td class="total-column">
                    ${isTotalMode && canEditVS
                        ? `<input type="number" class="vs-input ${totalModeClass}" data-member="${member.id}" data-day="total-calc" value="${total}" min="0">`
                        : `<span class="total-val">${total}</span>`
                    }
                </td>
            </tr>
        `;
    });
    
    tbody.innerHTML = html;
    attachInputListeners();
}

function attachInputListeners() {
    if (!canEditVS) return;

    document.querySelectorAll('.vs-input').forEach(input => {
        input.addEventListener('input', (e) => {
            const memberId = parseInt(e.target.dataset.member);
            const day = e.target.dataset.day;
            let val = parseInt(e.target.value) || 0;

            if (day === 'total-calc') {
                // Calculation Logic: Final Total entered, deduce Day 6
                const p = currentVSPoints[memberId] || {};
                const sum1to5 = (parseInt(p.monday)||0) + (parseInt(p.tuesday)||0) + (parseInt(p.wednesday)||0) + (parseInt(p.thursday)||0) + (parseInt(p.friday)||0);
                
                // Enforce no negatives
                const deducedDay6 = Math.max(0, val - sum1to5);
                updateCurrentVSPoints(memberId, 'saturday', deducedDay6);

                // Update the Day 6 Span visually
                const row = e.target.closest('tr');
                if (row) row.querySelector('.day6-val').textContent = deducedDay6;
            } else {
                updateCurrentVSPoints(memberId, day, val);
                
                // If we edit Day 1-5 while in Total Mode, Day 6 must update to keep the Total constant
                if (isTotalMode) {
                    const row = e.target.closest('tr');
                    const targetTotal = parseInt(row.querySelector('input[data-day="total-calc"]').value) || 0;
                    const p = currentVSPoints[memberId] || {};
                    const newSum1to5 = (parseInt(p.monday)||0) + (parseInt(p.tuesday)||0) + (parseInt(p.wednesday)||0) + (parseInt(p.thursday)||0) + (parseInt(p.friday)||0);
                    
                    const newDeducedDay6 = Math.max(0, targetTotal - newSum1to5);
                    updateCurrentVSPoints(memberId, 'saturday', newDeducedDay6);
                    row.querySelector('.day6-val').textContent = newDeducedDay6;
                } else {
                    // Standard Mode: Just update the Total span
                    const row = e.target.closest('tr');
                    if (row) row.querySelector('.total-val').textContent = calculateTotal(memberId);
                }
            }
        });
    });
}

function updateCurrentVSPoints(memberId, day, value) {
    if (!currentVSPoints[memberId]) {
        currentVSPoints[memberId] = {
            member_id: memberId,
            monday: 0, tuesday: 0, wednesday: 0, thursday: 0, friday: 0, saturday: 0
        };
    }
    currentVSPoints[memberId][day] = value;
}

/**
 * 4. SORTING LOGIC
 */

function sortData(members) {
    return members.sort((a, b) => {
        let valA, valB;
        if (sortColumn === 'name') {
            valA = a.name.toLowerCase();
            valB = b.name.toLowerCase();
        } else if (sortColumn === 'total') {
            valA = calculateTotal(a.id);
            valB = calculateTotal(b.id);
        } else {
            valA = currentVSPoints[a.id] ? (parseInt(currentVSPoints[a.id][sortColumn]) || 0) : 0;
            valB = currentVSPoints[b.id] ? (parseInt(currentVSPoints[b.id][sortColumn]) || 0) : 0;
        }
        if (valA < valB) return sortAscending ? -1 : 1;
        if (valA > valB) return sortAscending ? 1 : -1;
        return 0;
    });
}

function handleSort(e) {
    const th = e.target.closest('th');
    if (!th || !th.dataset.sort) return;
    const column = th.dataset.sort;
    if (sortColumn === column) {
        sortAscending = !sortAscending;
    } else {
        sortColumn = column;
        sortAscending = true;
    }
    document.querySelectorAll('th').forEach(header => {
        header.classList.remove('sort-asc', 'sort-desc');
    });
    th.classList.add(sortAscending ? 'sort-asc' : 'sort-desc');
    renderTable();
}

/**
 * 5. CSV IMPORT & SAVE LOGIC
 */

function handleCSVImport(event) {
    const file = event.target.files[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = function(e) {
        const text = e.target.result;
        const lines = text.split('\n').map(l => l.trim()).filter(l => l);
        if (lines.length < 2) return;

        const headers = lines[0].toLowerCase().split(',').map(h => h.trim());
        const columnMap = {
            name: headers.findIndex(h => h.includes('name') || h.includes('member')),
            monday: headers.findIndex(h => h === 'monday' || h === 'day 1' || h === 'day1'),
            tuesday: headers.findIndex(h => h === 'tuesday' || h === 'day 2' || h === 'day2'),
            wednesday: headers.findIndex(h => h === 'wednesday' || h === 'day 3' || h === 'day3'),
            thursday: headers.findIndex(h => h === 'thursday' || h === 'day 4' || h === 'day4'),
            friday: headers.findIndex(h => h === 'friday' || h === 'day 5' || h === 'day5'),
            saturday: headers.findIndex(h => h === 'saturday' || h === 'day 6' || h === 'day6'),
            total: headers.findIndex(h => h === 'total' || h === 'weekly total')
        };

        if (columnMap.name === -1) {
            alert("CSV Error: 'Name' column missing.");
            return;
        }

        let importCount = 0;
        for (let i = 1; i < lines.length; i++) {
            const cols = lines[i].split(',').map(c => c.trim());
            const memberName = cols[columnMap.name].toLowerCase();
            const member = allMembers.find(m => m.name.toLowerCase() === memberName);
            if (!member) continue;

            let extracted = { monday: 0, tuesday: 0, wednesday: 0, thursday: 0, friday: 0, saturday: 0 };
            let sum1to5 = 0;
            let csvTotal = 0;

            daysOfWeek.forEach(day => {
                const idx = columnMap[day];
                if (idx !== -1 && cols[idx]) {
                    const val = parseInt(cols[idx].replace(/[^0-9]/g, '')) || 0;
                    extracted[day] = val;
                    if (day !== 'saturday') sum1to5 += val;
                }
            });

            // Smart Deduction: If Saturday is missing but Total is present
            if (columnMap.total !== -1 && cols[columnMap.total]) {
                csvTotal = parseInt(cols[columnMap.total].replace(/[^0-9]/g, '')) || 0;
                if (extracted.saturday === 0 && csvTotal > sum1to5) {
                    extracted.saturday = csvTotal - sum1to5;
                }
            }

            // Update local memory
            if (!currentVSPoints[member.id]) {
                currentVSPoints[member.id] = { member_id: member.id, ...extracted };
            } else {
                Object.keys(extracted).forEach(d => {
                    if (extracted[d] > 0) currentVSPoints[member.id][d] = extracted[d];
                });
            }
            importCount++;
        }
        renderTable();
        alert(`Import complete: Updated ${importCount} members.`);
        event.target.value = '';
    };
    reader.readAsText(file);
}

async function saveVSPoints() {
    const weekDate = formatDate(currentWeekDate);
    const points = allMembers.map(member => {
        const vsPoint = currentVSPoints[member.id] || {};
        return {
            member_id: member.id,
            monday: parseInt(vsPoint.monday) || 0,
            tuesday: parseInt(vsPoint.tuesday) || 0,
            wednesday: parseInt(vsPoint.wednesday) || 0,
            thursday: parseInt(vsPoint.thursday) || 0,
            friday: parseInt(vsPoint.friday) || 0,
            saturday: parseInt(vsPoint.saturday) || 0
        };
    });
    
    try {
        const response = await fetch(API_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ week_date: weekDate, points })
        });
        if (!response.ok) throw new Error('Failed to save');
        alert('✓ Saved successfully!');
        await loadVSPoints();
    } catch (error) {
        alert('Error: ' + error.message);
    }
}

async function clearVSPoints() {
    const weekDate = formatDate(currentWeekDate);
    if (!confirm('Clear all points for this week?')) return;
    try {
        const response = await fetch(`${API_URL}/${weekDate}`, { method: 'DELETE' });
        currentVSPoints = {};
        renderTable();
    } catch (error) { alert('Error: ' + error.message); }
}

/**
 * 6. INITIALIZATION & HELPERS
 */

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

document.addEventListener('DOMContentLoaded', async () => {
    const vsTbody = document.getElementById('vs-tbody');
    if (vsTbody) {
        await checkPermissions(); 
        await loadMembers();
        currentWeekDate = getMostRecentMonday();
        updateWeekDisplay();
        await loadVSPoints();
        
        // Navigation
        document.getElementById('prev-week')?.addEventListener('click', () => {
            currentWeekDate.setDate(currentWeekDate.getDate() - 7);
            updateWeekDisplay();
            loadVSPoints();
        });

        document.getElementById('next-week')?.addEventListener('click', () => {
            const nextWeek = new Date(currentWeekDate);
            nextWeek.setDate(nextWeek.getDate() + 7);
            
            // Prevent going past the actual current week
            const todayMonday = getMostRecentMonday();
            if (nextWeek > todayMonday) {
                alert("Cannot navigate to a future week.");
                return;
            }

            currentWeekDate.setDate(currentWeekDate.getDate() + 7);
            updateWeekDisplay();
            loadVSPoints();
        });

        // Search
        document.getElementById('search-box')?.addEventListener('input', renderTable);
        
        // Sorting
        document.querySelectorAll('th[data-sort]').forEach(th => {
            th.addEventListener('click', handleSort);
        });
        
        // Admin Features
        if (canEditVS) {
            document.getElementById('save-btn')?.addEventListener('click', saveVSPoints);
            document.getElementById('clear-btn')?.addEventListener('click', clearVSPoints);
            
            const toggleBtn = document.getElementById('toggle-mode-btn');
            if (toggleBtn) {
                toggleBtn.addEventListener('click', () => {
                    isTotalMode = !isTotalMode;
                    toggleBtn.textContent = isTotalMode ? '🔙 Return to Day 6 Entry' : '🧮 Enter Weekly Totals';
                    toggleBtn.style.background = isTotalMode ? '#fd7e14' : '#17a2b8';
                    renderTable();
                });
            }

            const csvBtn = document.getElementById('csv-import-btn');
            const fileInput = document.getElementById('csv-file-input');
            if (csvBtn && fileInput) {
                csvBtn.addEventListener('click', () => fileInput.click());
                fileInput.addEventListener('change', handleCSVImport);
            }
        }
    }
});