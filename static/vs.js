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

function buildVSRow(member) {
    const points = currentVSPoints[member.id] || { monday: 0, tuesday: 0, wednesday: 0, thursday: 0, friday: 0, saturday: 0 };
    const total = calculateTotal(member.id);

    const tr = document.createElement('tr');
    tr.dataset.memberId = member.id;

    // Name cell
    const tdName = document.createElement('td');
    const nameSpan = document.createElement('span');
    nameSpan.className = 'member-name';
    nameSpan.textContent = member.name;
    const rankSpan = document.createElement('span');
    rankSpan.className = 'member-rank';
    rankSpan.textContent = `(${member.rank})`;
    tdName.append(nameSpan, ' ', rankSpan);
    tr.appendChild(tdName);

    // Day 1–5 input cells
    ['monday', 'tuesday', 'wednesday', 'thursday', 'friday'].forEach(day => {
        const td = document.createElement('td');
        const input = document.createElement('input');
        input.type = 'number';
        input.className = 'vs-input';
        input.dataset.member = member.id;
        input.dataset.day = day;
        input.value = points[day];
        input.min = '0';
        if (!canEditVS) input.readOnly = true;
        td.appendChild(input);
        tr.appendChild(td);
    });

    // Day 6 cell
    const tdDay6 = document.createElement('td');
    tdDay6.className = 'day6-column';
    if (isTotalMode) {
        const span = document.createElement('span');
        span.className = 'day6-val';
        span.style.cssText = 'font-weight:bold; color:var(--text-secondary);';
        span.textContent = points.saturday;
        tdDay6.appendChild(span);
    } else {
        const input = document.createElement('input');
        input.type = 'number';
        input.className = 'vs-input';
        input.dataset.member = member.id;
        input.dataset.day = 'saturday';
        input.value = points.saturday;
        input.min = '0';
        if (!canEditVS) input.readOnly = true;
        tdDay6.appendChild(input);
    }
    tr.appendChild(tdDay6);

    // Total cell
    const tdTotal = document.createElement('td');
    tdTotal.className = 'total-column';
    if (isTotalMode && canEditVS) {
        const input = document.createElement('input');
        input.type = 'number';
        input.className = 'vs-input total-mode-input';
        input.dataset.member = member.id;
        input.dataset.day = 'total-calc';
        input.value = total;
        input.min = '0';
        tdTotal.appendChild(input);
    } else {
        const span = document.createElement('span');
        span.className = 'total-val';
        span.textContent = total;
        tdTotal.appendChild(span);
    }
    tr.appendChild(tdTotal);

    return tr;
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
    const filteredMembers = sortData(allMembers.filter(member => member.name.toLowerCase().includes(searchTerm)));

    tbody.replaceChildren(...filteredMembers.map(buildVSRow));
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
        showToast('VS Points saved.');
        await loadVSPoints();
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function clearVSPoints() {
    const weekDate = formatDate(currentWeekDate);
    if (!await showConfirm('Clear all points for this week?', 'Clear Week')) return;
    try {
        await fetch(`${API_URL}/${weekDate}`, { method: 'DELETE' });
        currentVSPoints = {};
        renderTable();
        showToast('Points cleared.');
    } catch (error) { showToast('Error: ' + error.message, 'error'); }
}

/**
 * 6. INITIALIZATION & HELPERS
 */

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
                showToast('Cannot navigate to a future week.', 'error');
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
                fileInput.addEventListener('change', handleCSVUpload);
            }
        }
    }
});

let currentImportPayload = null;

async function handleCSVUpload(event) {
    const file = event.target.files[0];
    if (!file) return;

    const formData = new FormData();
    formData.append('csv_file', file);
    formData.append('week_date', formatDate(currentWeekDate));

    try {
        const response = await fetch('/api/vs-points/import/preview', {
            method: 'POST',
            body: formData
        });

        if (!response.ok) throw new Error(await response.text());

        currentImportPayload = await response.json();
        renderPreviewModal(currentImportPayload);
    } catch (error) {
        showToast('Error parsing CSV: ' + error.message, 'error');
    }

    event.target.value = ''; // Reset input
}

function buildMatchedRow(row) {
    const updates = Object.entries(row.updated_fields)
        .map(([day, val]) => `${day}: ${val}`)
        .join(', ');

    const tr = document.createElement('tr');

    const tdName = document.createElement('td');
    tdName.textContent = row.matched_member.name;

    const tdType = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = 'badge';
    badge.textContent = row.match_type;
    tdType.appendChild(badge);

    const tdUpdates = document.createElement('td');
    tdUpdates.textContent = updates;
    if (row.calculated_sat) {
        tdUpdates.appendChild(document.createTextNode(' '));
        const calcBadge = document.createElement('span');
        calcBadge.className = 'badge info';
        calcBadge.textContent = 'Calculated Sat';
        tdUpdates.appendChild(calcBadge);
    }

    tr.append(tdName, tdType, tdUpdates);
    return tr;
}

function buildUnresolvedRow(row, idx, availableMembers) {
    const updates = Object.entries(row.updated_fields)
        .map(([day, val]) => `${day}: ${val}`)
        .join(', ');

    const tr = document.createElement('tr');
    tr.dataset.index = idx;

    const tdName = document.createElement('td');
    tdName.textContent = row.original_name;

    const tdMap = document.createElement('td');
    const wrapper = document.createElement('div');
    wrapper.style.cssText = 'display: flex; flex-direction: column; gap: 5px;';

    const memberSelect = document.createElement('select');
    memberSelect.className = 'member-mapper';
    const defaultOpt = document.createElement('option');
    defaultOpt.value = '';
    defaultOpt.textContent = '-- Ignore / Do Not Import --';
    memberSelect.appendChild(defaultOpt);
    availableMembers.forEach(m => {
        const opt = document.createElement('option');
        opt.value = m.id;
        opt.textContent = m.name;
        memberSelect.appendChild(opt);
    });
    memberSelect.addEventListener('change', (e) => mapUnresolved(idx, e.target.value));

    const aliasSelect = document.createElement('select');
    aliasSelect.className = 'alias-saver';
    aliasSelect.id = `alias-save-${idx}`;
    aliasSelect.disabled = true;
    aliasSelect.style.fontSize = '0.85em';
    [['', 'Do not save alias'], ['global', 'Save as Global Alias'], ['personal', 'Save as Personal Alias']].forEach(([val, text]) => {
        const opt = document.createElement('option');
        opt.value = val;
        opt.textContent = text;
        aliasSelect.appendChild(opt);
    });

    wrapper.append(memberSelect, aliasSelect);
    tdMap.appendChild(wrapper);

    const tdUpdates = document.createElement('td');
    tdUpdates.textContent = updates;

    tr.append(tdName, tdMap, tdUpdates);
    return tr;
}

function renderPreviewModal(data) {
    const matchedBody = document.getElementById('matched-body');
    const unresolvedBody = document.getElementById('unresolved-body');

    document.getElementById('matched-count').textContent = data.matched?.length || 0;
    document.getElementById('unresolved-count').textContent = data.unresolved?.length || 0;

    // Render Matched
    const matchedRows = (data.matched || []).map(buildMatchedRow);
    matchedBody.replaceChildren(...matchedRows);

    // Render Unresolved
    const matchedIds = (data.matched || []).map(r => r.matched_member.id);
    const availableMembers = allMembers.filter(m => !matchedIds.includes(m.id));
    const unresolvedRows = (data.unresolved || []).map((row, idx) => buildUnresolvedRow(row, idx, availableMembers));
    unresolvedBody.replaceChildren(...unresolvedRows);

    document.getElementById('import-preview-modal').style.display = 'flex';
}

function refreshUpdatesCell(unresolvedIndex) {
    const row = currentImportPayload.unresolved[unresolvedIndex];
    const updatesCell = document.querySelector(`#unresolved-body tr[data-index="${unresolvedIndex}"] td:last-child`);
    if (!updatesCell) return;

    const text = Object.entries(row.updated_fields).map(([day, val]) => `${day}: ${val}`).join(', ');
    updatesCell.textContent = text;
    if (row.calculated_sat) {
        updatesCell.appendChild(document.createTextNode(' '));
        const badge = document.createElement('span');
        badge.className = 'badge info';
        badge.textContent = 'Calculated Sat';
        updatesCell.appendChild(badge);
    }
}

function mapUnresolved(unresolvedIndex, memberId) {
    const row = currentImportPayload.unresolved[unresolvedIndex];
    const aliasSelect = document.getElementById(`alias-save-${unresolvedIndex}`);

    // Handle Un-selecting a member
    if (!memberId) {
        row.matched_member = null;
        aliasSelect.disabled = true;
        aliasSelect.value = "";

        // Revert calculation if we previously added it
        if (row.calculated_sat) {
            delete row.updated_fields.saturday;
            row.calculated_sat = false;
        }
    } else {
        // Handle Selecting a member
        const member = allMembers.find(m => m.id == memberId);
        row.matched_member = member;
        aliasSelect.disabled = false;

        // Dynamically calculate Saturday if Total is provided but Saturday is missing
        if (row.total !== undefined && row.total !== null && row.updated_fields.saturday === undefined) {
            const p = currentVSPoints[memberId] || {}; // Existing DB points from frontend state

            // Get the value from the CSV upload, or fallback to their existing DB value
            const getVal = (day) => row.updated_fields[day] !== undefined ? row.updated_fields[day] : (parseInt(p[day]) || 0);

            const sum1to5 = getVal('monday') + getVal('tuesday') + getVal('wednesday') + getVal('thursday') + getVal('friday');
            const calcSat = row.total - sum1to5;

            if (calcSat >= 0) {
                row.updated_fields.saturday = calcSat;
                row.calculated_sat = true;
            }
        }
    }

    // Refresh the UI cell to show the newly calculated Saturday
    refreshUpdatesCell(unresolvedIndex);
}

async function commitImport() {
    const finalRecords = [...(currentImportPayload.matched || [])];
    const saveAliases = [];

    if (currentImportPayload.unresolved) {
        currentImportPayload.unresolved.forEach((row, idx) => {
            if (row.matched_member && row.matched_member.id) {
                finalRecords.push(row);

                const aliasSaveType = document.getElementById(`alias-save-${idx}`).value;
                if (aliasSaveType) {
                    saveAliases.push({
                        failed_alias: row.original_name,
                        member_id: row.matched_member.id,
                        is_global: aliasSaveType === 'global'
                    });
                }
            }
        });
    }

    const payload = {
        week_date: formatDate(currentWeekDate),
        records: finalRecords,
        save_aliases: saveAliases
    };

    // DEBUG: Log the payload so we can check it in the browser's developer console
    console.log("Sending Payload:", payload);

    try {
        const response = await fetch('/api/vs-points/import/commit', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!response.ok) throw new Error(await response.text());

        const result = await response.json();

        // Display errors if the backend caught any SQL issues
        if (result.errors && result.errors.length > 0) {
            console.error("Backend SQL Errors:", result.errors);
            showToast(result.message + ` (${result.errors.length} error(s) — see console)`, 'error');
        } else {
            showToast(result.message || 'Import complete.');
        }

        closePreviewModal();
        loadVSPoints();
    } catch (error) {
        showToast('Error saving data: ' + error.message, 'error');
    }
}

function closePreviewModal() {
    document.getElementById('import-preview-modal').style.display = 'none';
    currentImportPayload = null;
}
