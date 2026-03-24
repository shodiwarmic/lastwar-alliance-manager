// static/storm.js

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

// The game server is UTC-2 (0:00 Server = 02:00 UTC = 22:00 EDT)
const SERVER_UTC_OFFSET = -2;

// Timezone mapping with strict Standard Time offsets
const TIMEZONE_MAP = {
    "America/New_York": { label: "US Eastern", stdOffset: -5, stdName: "EST" },
    "America/Los_Angeles": { label: "US Pacific", stdOffset: -8, stdName: "PST" },
    "Europe/London": { label: "UK", stdOffset: 0, stdName: "GMT" },
    "Europe/Berlin": { label: "CET", stdOffset: 1, stdName: "CET" },
    "Australia/Perth": { label: "AWST", stdOffset: 8, stdName: "AWST" }
};

// The three server time slots
const STORM_SLOTS = [
    { id: 1, start: "09:00", end: "09:30" },
    { id: 2, start: "18:00", end: "18:30" },
    { id: 3, start: "23:00", end: "23:30" }
];

function formatStormTimes(selectedZonesStr, respectDST, elementId) {
    if (!elementId) elementId = 'storm-time-select-a';
    const selectedZones = selectedZonesStr ? selectedZonesStr.split(',') : ["America/New_York"];
    const dropdown = document.getElementById(elementId);
    if (!dropdown) return;

    dropdown.innerHTML = '';
    const refDate = new Date();

    STORM_SLOTS.forEach(slot => {
        let labelParts = [`${slot.start} Server Time`];

        selectedZones.forEach(zoneKey => {
            const tzInfo = TIMEZONE_MAP[zoneKey];
            if (!tzInfo) return;

            const [hours, minutes] = slot.start.split(':');

            // Convert Server Time (UTC-2) backward to true UTC
            const utcDate = new Date(Date.UTC(
                refDate.getUTCFullYear(),
                refDate.getUTCMonth(),
                refDate.getUTCDate(),
                parseInt(hours) - SERVER_UTC_OFFSET,
                parseInt(minutes)
            ));

            let timeString = '';

            if (respectDST) {
                const formatter = new Intl.DateTimeFormat('en-US', {
                    timeZone: zoneKey,
                    hour: '2-digit',
                    minute: '2-digit',
                    hour12: false,
                    timeZoneName: 'short'
                });
                timeString = formatter.format(utcDate);
            } else {
                const stdDate = new Date(utcDate.getTime() + (tzInfo.stdOffset * 3600000));
                const formattedHour = String(stdDate.getUTCHours()).padStart(2, '0');
                const formattedMin = String(stdDate.getUTCMinutes()).padStart(2, '0');
                timeString = `${formattedHour}:${formattedMin} ${tzInfo.stdName}`;
            }

            labelParts.push(timeString);
        });

        const option = document.createElement('option');
        option.value = slot.id;
        option.textContent = labelParts.join(' | ');
        dropdown.appendChild(option);
    });
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// --- State ---
const API_BASE = '/api/storm';
let allMembers = [];
let currentTF = 'A';
let tfConfig = { A: null, B: null };
let groups = [];
let myRegistration = null;
let registrations = [];
let canManage = false;
let saveDebounceTimer = null;
let myRegState = { slot_1: 0, slot_2: 0, slot_3: 0 };
let dragMemberId = null;
let dragIsSub = false;

// --- Error display ---
function showError(msg) {
    const el = document.getElementById('storm-error');
    el.textContent = msg;
    el.classList.remove('hidden');
    setTimeout(() => el.classList.add('hidden'), 6000);
}

// --- CSRF helper ---
function getCsrfToken() {
    const el = document.querySelector('input[name="gorilla.csrf.Token"]');
    return el ? el.value : '';
}

// --- API helpers ---
async function apiFetch(url, options = {}) {
    if (!options.headers) options.headers = {};
    if (options.method && options.method !== 'GET') {
        options.headers['X-CSRF-Token'] = getCsrfToken();
    }
    const res = await fetch(url, options);
    return res;
}

// --- All assigned IDs for current TF ---
function allAssignedIds() {
    const ids = new Set();
    for (const g of groups) {
        for (const b of g.buildings) {
            for (const m of b.members) ids.add(m.member_id);
        }
        for (const m of g.direct_members) ids.add(m.member_id);
    }
    return ids;
}

// --- Render all ---
function renderAll() {
    renderPool();
    renderGroups();
    updateCapacityBar();
}

// --- Render member pool ---
function renderPool() {
    const pool = document.getElementById('member-pool');
    if (!pool) return;
    const searchVal = (document.getElementById('pool-search') || {}).value || '';
    const assigned = allAssignedIds();
    const activeSlotIdx = tfConfig[currentTF]; // 1,2,3 or null

    const filtered = allMembers.filter(m => {
        if (searchVal) {
            const q = searchVal.toLowerCase();
            if (!m.name.toLowerCase().includes(q) && !m.rank.toLowerCase().includes(q)) return false;
        }
        return !assigned.has(m.id);
    });

    let html = '';
    for (const m of filtered) {
        let regVal = 0;
        if (activeSlotIdx) {
            const key = `slot_${activeSlotIdx}`;
            const reg = registrations.find(r => r.member_id === m.id);
            if (reg) regVal = reg[key] || 0;
            // If this is the current user's own registration
            if (myRegistration && myRegistration.member_id === m.id) {
                regVal = myRegistration[key] || 0;
            }
        }

        let regBadge = '';
        let dimmed = '';
        if (!activeSlotIdx) {
            regBadge = '<span class="reg-none">No slot set</span>';
        } else if (regVal === 1) {
            regBadge = '<span class="reg-avail">✓ Available</span>';
        } else if (regVal === 2) {
            regBadge = '<span class="reg-sub">⚡ Sub Only</span>';
        } else {
            regBadge = '<span class="reg-none">Not registered</span>';
            dimmed = ' dimmed';
        }

        const powerStr = m.power != null ? m.power.toLocaleString() : '—';
        html += `<div class="pool-card${dimmed}" draggable="true" data-member-id="${m.id}" data-is-sub="${regVal === 2 ? 'true' : 'false'}">
            <div class="pool-name">${escapeHtml(m.name)}</div>
            <div class="pool-meta">${escapeHtml(m.rank)} · ⚡${powerStr}</div>
            <div class="pool-reg">${regBadge}</div>
        </div>`;
    }

    pool.innerHTML = html || '<p style="color:var(--text-secondary); font-size:0.85em;">All members assigned</p>';

    // Drag events
    pool.querySelectorAll('.pool-card').forEach(card => {
        card.addEventListener('dragstart', e => {
            dragMemberId = parseInt(card.dataset.memberId);
            dragIsSub = card.dataset.isSub === 'true';
            e.dataTransfer.effectAllowed = 'move';
        });
    });
}

// --- Format power sum ---
function formatPowerSum(memberIds) {
    let total = 0;
    let unknown = 0;
    for (const id of memberIds) {
        const m = allMembers.find(x => x.id === id);
        if (!m) continue;
        if (m.power != null) total += Number(m.power);
        else unknown++;
    }
    let str = '⚡' + total.toLocaleString();
    if (unknown > 0) str += ` (+${unknown} unknown)`;
    return str;
}

// --- Render groups ---
function renderGroups() {
    const container = document.getElementById('groups-container');
    if (!container) return;

    if (groups.length === 0) {
        container.innerHTML = '<p style="color:var(--text-secondary);">No groups yet. ' + (canManage ? 'Click "+ Add Group" to create one.' : '') + '</p>';
        return;
    }

    let html = '';
    for (const g of groups) {
        // Power totals for this group
        const primaryIds = [];
        const subIds = [];
        for (const b of g.buildings) {
            for (const m of b.members) {
                (m.is_sub ? subIds : primaryIds).push(m.member_id);
            }
        }
        for (const m of g.direct_members) {
            (m.is_sub ? subIds : primaryIds).push(m.member_id);
        }
        const primaryPower = formatPowerSum(primaryIds);
        const subPower = formatPowerSum(subIds);

        html += `<div class="group-card" data-group-id="${g.id}">
            <div class="group-header">
                <h4>${escapeHtml(g.name)}</h4>
                <span style="font-size:0.8em; color:var(--text-secondary);">Primary: ${primaryPower} | Sub: ${subPower}</span>
                ${canManage ? `<button class="btn btn-danger" style="padding:2px 8px;" onclick="deleteGroup(${g.id})">✕</button>` : ''}
            </div>
            <div class="group-body">`;

        if (canManage) {
            html += `<textarea class="form-input" placeholder="Instructions..." rows="2" style="width:100%;box-sizing:border-box;margin-bottom:8px;resize:vertical;" data-group-id="${g.id}" onchange="debouncedUpdateGroupMeta(${g.id})">${escapeHtml(g.instructions || '')}</textarea>`;
        } else if (g.instructions) {
            html += `<p style="color:var(--text-secondary); font-size:0.9em; margin-bottom:8px;">${escapeHtml(g.instructions)}</p>`;
        }

        // Buildings
        for (const b of g.buildings) {
            const bInfo = BUILDINGS.find(x => x.id === b.building_id) || { name: b.building_id, priority: '' };
            const priorityBadge = bInfo.priority ? `<span style="font-size:0.75em;margin-left:4px;opacity:0.7;">[${bInfo.priority}]</span>` : '';
            html += `<div class="building-slot" data-group-id="${g.id}" data-building-id="${b.building_id}" data-gb-id="${b.id}">
                <div class="building-slot-header">${escapeHtml(bInfo.name)}${priorityBadge}</div>
                <div>`;
            for (const m of b.members) {
                const member = allMembers.find(x => x.id === m.member_id);
                const mName = member ? member.name : `#${m.member_id}`;
                html += `<span class="member-chip${m.is_sub ? ' is-sub' : ''}">${escapeHtml(mName)}${m.is_sub ? ' (sub)' : ''}
                    ${canManage ? `<span class="chip-remove" data-group-id="${g.id}" data-building-id="${b.building_id}" data-member-id="${m.member_id}">×</span>` : ''}
                </span>`;
            }
            html += '</div>';
            if (canManage) {
                html += renderInlineSearch(g.id, b.building_id, false);
                html += `<button class="btn btn-secondary" style="margin-top:6px;padding:2px 8px;font-size:0.8em;" onclick="removeBuilding(${g.id},'${b.building_id}')">Remove building</button>`;
            }
            html += `</div>`;
        }

        // Direct/flex members slot
        html += `<div class="building-slot" data-group-id="${g.id}" data-direct="true">
            <div class="building-slot-header">Flexible Role</div>
            <div>`;
        for (const m of g.direct_members) {
            const member = allMembers.find(x => x.id === m.member_id);
            const mName = member ? member.name : `#${m.member_id}`;
            html += `<span class="member-chip${m.is_sub ? ' is-sub' : ''}">${escapeHtml(mName)}${m.is_sub ? ' (sub)' : ''}
                ${canManage ? `<span class="chip-remove" data-group-id="${g.id}" data-direct="true" data-member-id="${m.member_id}">×</span>` : ''}
            </span>`;
        }
        html += '</div>';
        if (canManage) {
            html += renderInlineSearch(g.id, null, true);
        }
        html += `</div>`;

        // Add building dropdown
        if (canManage) {
            const usedBuildingIds = g.buildings.map(b => b.building_id);
            const availBuildings = BUILDINGS.filter(b => !usedBuildingIds.includes(b.id));
            if (availBuildings.length > 0) {
                html += `<div style="margin-top:8px;display:flex;gap:8px;align-items:center;">
                    <select class="form-input" id="add-bldg-select-${g.id}" style="flex:1;">`;
                for (const b of availBuildings) {
                    html += `<option value="${b.id}">${escapeHtml(b.name)}</option>`;
                }
                html += `</select>
                    <button class="btn btn-secondary" style="padding:4px 10px;" onclick="addBuilding(${g.id})">+ Add Building</button>
                </div>`;
            }
        }

        html += `</div></div>`;
    }

    container.innerHTML = html;

    // Wire drop zones
    container.querySelectorAll('.building-slot').forEach(slot => {
        slot.addEventListener('dragover', e => {
            e.preventDefault();
            slot.classList.add('drag-over');
        });
        slot.addEventListener('dragleave', () => slot.classList.remove('drag-over'));
        slot.addEventListener('drop', e => {
            e.preventDefault();
            slot.classList.remove('drag-over');
            if (!dragMemberId) return;
            const gid = parseInt(slot.dataset.groupId);
            const bid = slot.dataset.buildingId;
            const isDirect = slot.dataset.direct === 'true';
            handleDrop(gid, bid, isDirect, dragMemberId, dragIsSub);
            dragMemberId = null;
        });
    });

    // Wire chip removes
    container.querySelectorAll('.chip-remove').forEach(btn => {
        btn.addEventListener('click', () => {
            const gid = parseInt(btn.dataset.groupId);
            const mid = parseInt(btn.dataset.memberId);
            const bid = btn.dataset.buildingId;
            const isDirect = btn.dataset.direct === 'true';
            removeMemberChip(gid, bid, isDirect, mid);
        });
    });

    // Wire inline search inputs
    container.querySelectorAll('.inline-search-input').forEach(input => {
        input.addEventListener('input', () => {
            const dropdown = input.nextElementSibling;
            const q = input.value.toLowerCase();
            const gid = parseInt(input.dataset.groupId);
            const bid = input.dataset.buildingId || null;
            const isDirect = input.dataset.direct === 'true';
            showInlineDropdown(dropdown, q, gid, bid, isDirect);
        });
        input.addEventListener('focus', () => {
            const dropdown = input.nextElementSibling;
            const gid = parseInt(input.dataset.groupId);
            const bid = input.dataset.buildingId || null;
            const isDirect = input.dataset.direct === 'true';
            showInlineDropdown(dropdown, '', gid, bid, isDirect);
        });
        // Close on outside click
        document.addEventListener('click', e => {
            if (!input.parentElement.contains(e.target)) {
                const dropdown = input.nextElementSibling;
                if (dropdown) dropdown.innerHTML = '';
            }
        });
    });
}

function renderInlineSearch(groupId, buildingId, isDirect) {
    const bidAttr = buildingId ? `data-building-id="${buildingId}"` : '';
    const directAttr = isDirect ? 'data-direct="true"' : '';
    return `<div class="inline-search" style="margin-top:4px;">
        <input type="text" class="form-input inline-search-input" placeholder="Add member..."
            data-group-id="${groupId}" ${bidAttr} ${directAttr}>
        <div class="inline-dropdown"></div>
    </div>`;
}

function showInlineDropdown(dropdown, q, groupId, buildingId, isDirect) {
    const assigned = allAssignedIds();
    const filtered = allMembers.filter(m => {
        if (assigned.has(m.id)) return false;
        if (q && !m.name.toLowerCase().includes(q) && !m.rank.toLowerCase().includes(q)) return false;
        return true;
    }).slice(0, 20);

    if (filtered.length === 0) {
        dropdown.innerHTML = '<div style="padding:6px 10px;color:var(--text-secondary);font-size:0.85em;">No members available</div>';
        return;
    }

    dropdown.innerHTML = filtered.map(m =>
        `<div data-member-id="${m.id}">${escapeHtml(m.name)} (${m.rank})</div>`
    ).join('');

    dropdown.querySelectorAll('div[data-member-id]').forEach(item => {
        item.addEventListener('click', () => {
            const mid = parseInt(item.dataset.memberId);
            handleDrop(groupId, buildingId, isDirect, mid, false);
            dropdown.innerHTML = '';
            // Clear the input
            const input = dropdown.previousElementSibling;
            if (input) input.value = '';
        });
    });
}

function handleDrop(groupId, buildingId, isDirect, memberId, isSub) {
    const assigned = allAssignedIds();
    if (assigned.has(memberId)) return;

    const g = groups.find(x => x.id === groupId);
    if (!g) return;

    if (isDirect) {
        const pos = g.direct_members.length;
        g.direct_members.push({ id: 0, member_id: memberId, is_sub: isSub, position: pos });
    } else {
        const b = g.buildings.find(x => x.building_id === buildingId);
        if (!b) return;
        const pos = b.members.length;
        b.members.push({ id: 0, member_id: memberId, is_sub: isSub, position: pos });
    }

    debouncedSaveGroups();
    renderAll();
}

function removeMemberChip(groupId, buildingId, isDirect, memberId) {
    const g = groups.find(x => x.id === groupId);
    if (!g) return;

    if (isDirect) {
        g.direct_members = g.direct_members.filter(m => m.member_id !== memberId);
    } else {
        const b = g.buildings.find(x => x.building_id === buildingId);
        if (b) b.members = b.members.filter(m => m.member_id !== memberId);
    }

    debouncedSaveGroups();
    renderAll();
}

function removeBuilding(groupId, buildingId) {
    const g = groups.find(x => x.id === groupId);
    if (!g) return;
    g.buildings = g.buildings.filter(b => b.building_id !== buildingId);
    debouncedSaveGroups();
    renderAll();
}

function addBuilding(groupId) {
    const sel = document.getElementById(`add-bldg-select-${groupId}`);
    if (!sel || !sel.value) return;
    const g = groups.find(x => x.id === groupId);
    if (!g) return;
    const sortOrder = g.buildings.length;
    g.buildings.push({ id: 0, building_id: sel.value, sort_order: sortOrder, members: [] });
    debouncedSaveGroups();
    renderAll();
}

// --- Debounced saves ---
function debouncedSaveGroups() {
    if (saveDebounceTimer) clearTimeout(saveDebounceTimer);
    saveDebounceTimer = setTimeout(saveAllGroups, 500);
}

async function saveAllGroups() {
    for (const g of groups) {
        // Save buildings
        try {
            const res = await apiFetch(`${API_BASE}/groups/${g.id}/buildings`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(g.buildings)
            });
            if (!res.ok) {
                const txt = await res.text();
                console.error('Save buildings error:', txt);
                showError('Failed to save buildings: ' + txt);
            }
        } catch (e) {
            console.error('Save buildings error:', e);
            showError('Failed to save buildings: ' + e.message);
        }

        // Save direct members
        try {
            const res = await apiFetch(`${API_BASE}/groups/${g.id}/members`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(g.direct_members)
            });
            if (!res.ok) {
                const txt = await res.text();
                console.error('Save members error:', txt);
                showError('Failed to save members: ' + txt);
            }
        } catch (e) {
            console.error('Save members error:', e);
            showError('Failed to save members: ' + e.message);
        }
    }
}

// --- Update capacity bar ---
function updateCapacityBar() {
    let primaries = 0, subs = 0, totalPower = 0, unknownPower = 0;
    const assigned = allAssignedIds();

    for (const g of groups) {
        for (const b of g.buildings) {
            for (const m of b.members) {
                if (m.is_sub) subs++;
                else {
                    primaries++;
                    const mem = allMembers.find(x => x.id === m.member_id);
                    if (mem && mem.power != null) totalPower += Number(mem.power);
                    else unknownPower++;
                }
            }
        }
        for (const m of g.direct_members) {
            if (m.is_sub) subs++;
            else {
                primaries++;
                const mem = allMembers.find(x => x.id === m.member_id);
                if (mem && mem.power != null) totalPower += Number(mem.power);
                else unknownPower++;
            }
        }
    }

    const unassigned = allMembers.length - assigned.size;
    const powerStr = '⚡' + totalPower.toLocaleString() + (unknownPower > 0 ? ` (+${unknownPower} unknown)` : '');

    const setCell = (id, text, warn, danger) => {
        const el = document.getElementById(id);
        if (!el) return;
        el.textContent = text;
        el.classList.remove('warn', 'danger');
        if (danger) el.classList.add('danger');
        else if (warn) el.classList.add('warn');
    };

    setCell('cap-primaries', `Primaries: ${primaries}`, primaries >= 18, primaries >= 20);
    setCell('cap-subs', `Substitutes: ${subs}`, subs >= 8, subs >= 10);
    setCell('cap-power', powerStr, false, false);
    setCell('cap-unassigned', `Unassigned: ${unassigned}`, false, false);
}

// --- Group CRUD ---
async function createGroup(name) {
    try {
        const res = await apiFetch(`${API_BASE}/groups`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ task_force: currentTF, name: name, instructions: '', sort_order: groups.length })
        });
        if (!res.ok) {
            const txt = await res.text();
            showError('Failed to create group: ' + txt);
            return;
        }
        const g = await res.json();
        groups.push(g);
        renderAll();
    } catch (e) {
        console.error('Create group error:', e);
        showError('Failed to create group: ' + e.message);
    }
}

async function deleteGroup(id) {
    if (!confirm('Delete this group? This cannot be undone.')) return;
    try {
        const res = await apiFetch(`${API_BASE}/groups/${id}`, { method: 'DELETE' });
        if (!res.ok) {
            const txt = await res.text();
            showError('Failed to delete group: ' + txt);
            return;
        }
        groups = groups.filter(g => g.id !== id);
        renderAll();
    } catch (e) {
        console.error('Delete group error:', e);
        showError('Failed to delete group: ' + e.message);
    }
}

const groupMetaTimers = {};
function debouncedUpdateGroupMeta(groupId) {
    if (groupMetaTimers[groupId]) clearTimeout(groupMetaTimers[groupId]);
    groupMetaTimers[groupId] = setTimeout(() => updateGroupMeta(groupId), 1000);
}

async function updateGroupMeta(groupId) {
    const g = groups.find(x => x.id === groupId);
    if (!g) return;
    const textarea = document.querySelector(`textarea[data-group-id="${groupId}"]`);
    if (textarea) g.instructions = textarea.value;
    try {
        const res = await apiFetch(`${API_BASE}/groups/${groupId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: g.name, instructions: g.instructions, sort_order: g.sort_order })
        });
        if (!res.ok) {
            const txt = await res.text();
            console.error('Update group meta error:', txt);
        }
    } catch (e) {
        console.error('Update group meta error:', e);
    }
}

// --- My Registration ---
function renderMyRegistration() {
    const container = document.getElementById('my-reg-slots');
    if (!container) return;

    let html = '';
    STORM_SLOTS.forEach((slot, i) => {
        const slotKey = `slot_${slot.id}`;
        const val = myRegState[slotKey] || 0;
        const selectA = document.getElementById('storm-time-select-a');
        let timeLabel = `Slot ${slot.id}: ${slot.start} Server Time`;
        if (selectA && selectA.options[slot.id - 1]) {
            timeLabel = `Slot ${slot.id}: ${selectA.options[slot.id - 1].text}`;
        }

        html += `<div class="my-slot-row">
            <div class="slot-label">${escapeHtml(timeLabel)}</div>
            <div class="three-way">
                <button onclick="setMySlot('${slotKey}', 0)" class="${val === 0 ? 'active-unavail' : ''}">Not Available</button>
                <button onclick="setMySlot('${slotKey}', 1)" class="${val === 1 ? 'active-avail' : ''}">✓ Available</button>
                <button onclick="setMySlot('${slotKey}', 2)" class="${val === 2 ? 'active-sub' : ''}">⚡ Sub Only</button>
            </div>
        </div>`;
    });

    container.innerHTML = html;
}

function setMySlot(key, val) {
    myRegState[key] = val;
    renderMyRegistration();
}

async function saveMyRegistration() {
    try {
        const res = await apiFetch(`${API_BASE}/registrations/me`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(myRegState)
        });
        if (!res.ok) {
            const txt = await res.text();
            showError('Failed to save availability: ' + txt);
            return;
        }
        const btn = document.getElementById('btn-save-my-reg');
        if (btn) {
            const orig = btn.textContent;
            btn.textContent = 'Saved!';
            setTimeout(() => { btn.textContent = orig; }, 2000);
        }
    } catch (e) {
        console.error('Save my registration error:', e);
        showError('Failed to save availability: ' + e.message);
    }
}

// --- Registration View ---
function renderRegistrationView() {
    const container = document.getElementById('reg-view-container');
    if (!container) return;

    const rankOrder = ['R5', 'R4', 'R3', 'R2', 'R1'];
    const slotLabels = [];
    const selectA = document.getElementById('storm-time-select-a');
    STORM_SLOTS.forEach(slot => {
        if (selectA && selectA.options[slot.id - 1]) {
            slotLabels.push(selectA.options[slot.id - 1].text);
        } else {
            slotLabels.push(`Slot ${slot.id}`);
        }
    });

    let html = `<table class="reg-table">
        <thead><tr>
            <th>Member</th><th>Rank</th><th>Power</th>
            <th>${escapeHtml(slotLabels[0])}</th>
            <th>${escapeHtml(slotLabels[1])}</th>
            <th>${escapeHtml(slotLabels[2])}</th>
        </tr></thead><tbody>`;

    // Sort by rank then power
    const sorted = [...registrations].sort((a, b) => {
        const ra = rankOrder.indexOf(a.member_rank);
        const rb = rankOrder.indexOf(b.member_rank);
        if (ra !== rb) return (ra === -1 ? 99 : ra) - (rb === -1 ? 99 : rb);
        const pa = a.member_power || 0;
        const pb = b.member_power || 0;
        return pb - pa;
    });

    for (const reg of sorted) {
        const powerStr = reg.member_power != null ? Number(reg.member_power).toLocaleString() : '—';
        html += `<tr>
            <td>${escapeHtml(reg.member_name)}</td>
            <td>${escapeHtml(reg.member_rank)}</td>
            <td>${powerStr}</td>
            ${[1, 2, 3].map(s => {
                const val = reg[`slot_${s}`] || 0;
                return `<td><span class="reg-pill s${val}" data-member-id="${reg.member_id}" data-slot="${s}" data-val="${val}" onclick="cycleRegPill(this)">${val === 0 ? '—' : val === 1 ? '✓' : '⚡'}</span></td>`;
            }).join('')}
        </tr>`;
    }

    html += '</tbody></table>';
    container.innerHTML = html;
}

async function cycleRegPill(el) {
    const memberId = el.dataset.memberId;
    const slotNum = el.dataset.slot;
    const slotKey = `slot_${slotNum}`;
    let val = parseInt(el.dataset.val);
    val = (val + 1) % 3;

    // Update local state
    const reg = registrations.find(r => r.member_id === parseInt(memberId));
    if (reg) reg[slotKey] = val;

    // Save to server
    try {
        const body = {};
        if (reg) {
            body.slot_1 = reg.slot_1 || 0;
            body.slot_2 = reg.slot_2 || 0;
            body.slot_3 = reg.slot_3 || 0;
        }
        body[slotKey] = val;

        const res = await apiFetch(`${API_BASE}/registrations/${memberId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const txt = await res.text();
            showError('Failed to save registration: ' + txt);
        }
    } catch (e) {
        console.error('Cycle reg pill error:', e);
        showError('Failed to save registration: ' + e.message);
    }

    renderRegistrationView();
}

// --- Save config ---
async function saveConfig() {
    const selA = document.getElementById('storm-time-select-a');
    const selB = document.getElementById('storm-time-select-b');
    const slotA = selA && selA.value ? parseInt(selA.value) : null;
    const slotB = selB && selB.value ? parseInt(selB.value) : null;

    const payload = [
        { task_force: 'A', time_slot: slotA },
        { task_force: 'B', time_slot: slotB }
    ];

    try {
        const res = await apiFetch(`${API_BASE}/config`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const txt = await res.text();
            showError('Failed to save config: ' + txt);
            return;
        }
        tfConfig.A = slotA;
        tfConfig.B = slotB;
        renderAll();
    } catch (e) {
        console.error('Save config error:', e);
        showError('Failed to save config: ' + e.message);
    }
}

// --- Generate mail ---
function generateMail() {
    const isA = currentTF === 'A';
    const selId = isA ? 'storm-time-select-a' : 'storm-time-select-b';
    const timeSelect = document.getElementById(selId);
    let timeText = '';
    if (timeSelect && timeSelect.selectedIndex !== -1) {
        timeText = timeSelect.options[timeSelect.selectedIndex].text;
    }

    let mail = `🏜️ DESERT STORM - TASK FORCE ${currentTF}\n`;
    mail += `═══════════════════════════════════════\n\n`;

    if (timeText) {
        mail += `⏰ BATTLE TIME:\n${timeText}\n\n`;
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
    mail += `GROUP ASSIGNMENTS:\n\n`;

    for (const g of groups) {
        mail += `\nGROUP: ${g.name}\n`;
        mail += `────────────────────────────────\n`;
        if (g.instructions) {
            mail += `Instructions: ${g.instructions}\n`;
        }

        for (const b of g.buildings) {
            const bInfo = BUILDINGS.find(x => x.id === b.building_id) || { name: b.building_id };
            const primaries = b.members.filter(m => !m.is_sub).map(m => {
                const mem = allMembers.find(x => x.id === m.member_id);
                return mem ? mem.name : `#${m.member_id}`;
            });
            const subs = b.members.filter(m => m.is_sub).map(m => {
                const mem = allMembers.find(x => x.id === m.member_id);
                return mem ? mem.name : `#${m.member_id}`;
            });
            mail += `\n  ${bInfo.name}:\n`;
            if (primaries.length > 0) mail += `    Primary: ${primaries.join(', ')}\n`;
            if (subs.length > 0) mail += `    Sub: ${subs.join(', ')}\n`;
        }

        const directPrimaries = g.direct_members.filter(m => !m.is_sub).map(m => {
            const mem = allMembers.find(x => x.id === m.member_id);
            return mem ? mem.name : `#${m.member_id}`;
        });
        const directSubs = g.direct_members.filter(m => m.is_sub).map(m => {
            const mem = allMembers.find(x => x.id === m.member_id);
            return mem ? mem.name : `#${m.member_id}`;
        });
        if (directPrimaries.length > 0 || directSubs.length > 0) {
            mail += `\n  Flexible Role:\n`;
            if (directPrimaries.length > 0) mail += `    ${directPrimaries.join(', ')}\n`;
            if (directSubs.length > 0) mail += `    ${directSubs.join(', ')} (sub)\n`;
        }
    }

    mail += `\n═══════════════════════════════════════\n`;
    mail += `💪 LET'S WIN THIS!\n`;
    mail += `═══════════════════════════════════════\n`;

    const mailContent = document.getElementById('mail-content');
    const mailOutput = document.getElementById('mail-output');
    if (mailContent && mailOutput) {
        mailContent.textContent = mail;
        mailOutput.classList.remove('hidden');
        mailOutput.scrollIntoView({ behavior: 'smooth' });
    }
}

// --- Copy mail ---
async function copyMail() {
    const mailContent = document.getElementById('mail-content');
    if (!mailContent) return;
    const mailText = mailContent.textContent;

    const btn = document.getElementById('btn-copy-mail');
    try {
        await navigator.clipboard.writeText(mailText);
        if (btn) {
            const orig = btn.textContent;
            btn.textContent = '✓ Copied!';
            setTimeout(() => { btn.textContent = orig; }, 2000);
        }
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
            if (btn) {
                const orig = btn.textContent;
                btn.textContent = '✓ Copied!';
                setTimeout(() => { btn.textContent = orig; }, 2000);
            }
        } catch (err) {
            showError('Failed to copy mail. Please copy manually.');
        }
        document.body.removeChild(textArea);
    }
}

// --- Init ---
document.addEventListener('DOMContentLoaded', async () => {
    const root = document.getElementById('storm-root');
    if (!root) return;

    canManage = root.dataset.canManage === 'true';

    // 1. Fetch settings for timezone selects
    try {
        const settingsRes = await fetch('/api/settings');
        if (settingsRes.ok) {
            const settings = await settingsRes.json();
            formatStormTimes(settings.storm_timezones, settings.storm_respect_dst, 'storm-time-select-a');
            formatStormTimes(settings.storm_timezones, settings.storm_respect_dst, 'storm-time-select-b');
        }
    } catch (e) {
        console.error('Error loading settings:', e);
        formatStormTimes('America/New_York', true, 'storm-time-select-a');
        formatStormTimes('America/New_York', true, 'storm-time-select-b');
    }

    // 2. Fetch members
    try {
        const res = await fetch('/api/members');
        if (res.ok) {
            allMembers = await res.json();
            allMembers.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
        }
    } catch (e) {
        console.error('Error loading members:', e);
    }

    // 3. Fetch TF config
    try {
        const res = await fetch(`${API_BASE}/config`);
        if (res.ok) {
            const configs = await res.json();
            for (const c of configs) {
                tfConfig[c.task_force] = c.time_slot;
            }
            // Set selects
            const selA = document.getElementById('storm-time-select-a');
            const selB = document.getElementById('storm-time-select-b');
            if (selA && tfConfig.A) selA.value = tfConfig.A;
            if (selB && tfConfig.B) selB.value = tfConfig.B;
        }
    } catch (e) {
        console.error('Error loading TF config:', e);
    }

    // 4. Fetch groups for current TF
    try {
        const res = await fetch(`${API_BASE}/groups?task_force=${currentTF}`);
        if (res.ok) {
            groups = await res.json();
        }
    } catch (e) {
        console.error('Error loading groups:', e);
    }

    // 5. Fetch registrations (managers only)
    if (canManage) {
        try {
            const res = await apiFetch(`${API_BASE}/registrations`);
            if (res.ok) {
                registrations = await res.json();
            }
        } catch (e) {
            console.error('Error loading registrations:', e);
        }
    }

    // 6. Fetch my registration
    try {
        const res = await fetch(`${API_BASE}/registrations/me`);
        if (res.ok) {
            myRegistration = await res.json();
            myRegState.slot_1 = myRegistration.slot_1 || 0;
            myRegState.slot_2 = myRegistration.slot_2 || 0;
            myRegState.slot_3 = myRegistration.slot_3 || 0;
        }
    } catch (e) {
        // 403 if no linked member — that's OK
        myRegistration = null;
    }

    // 7. Render
    renderAll();

    // --- Event listeners ---

    // Tab switching
    document.querySelectorAll('.section-tab').forEach(tab => {
        tab.addEventListener('click', () => {
            document.querySelectorAll('.section-tab').forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(c => c.classList.add('hidden'));
            tab.classList.add('active');
            const target = document.getElementById('tab-' + tab.dataset.tab);
            if (target) {
                target.classList.remove('hidden');
                if (tab.dataset.tab === 'reg-view') renderRegistrationView();
                if (tab.dataset.tab === 'my-reg') renderMyRegistration();
            }
        });
    });

    // TF chips
    document.querySelectorAll('.filter-chip[data-tf]').forEach(chip => {
        chip.addEventListener('click', async () => {
            document.querySelectorAll('.filter-chip[data-tf]').forEach(c => c.classList.remove('active'));
            chip.classList.add('active');
            currentTF = chip.dataset.tf;

            try {
                const res = await fetch(`${API_BASE}/groups?task_force=${currentTF}`);
                if (res.ok) groups = await res.json();
            } catch (e) {
                console.error('Error loading groups:', e);
                showError('Failed to load groups: ' + e.message);
            }
            renderAll();
        });
    });

    // Pool search
    const poolSearch = document.getElementById('pool-search');
    if (poolSearch) {
        poolSearch.addEventListener('input', renderPool);
    }

    // Add group button
    const btnAddGroup = document.getElementById('btn-add-group');
    if (btnAddGroup) {
        btnAddGroup.addEventListener('click', () => {
            const name = prompt('Group name:');
            if (name && name.trim()) createGroup(name.trim());
        });
    }

    // Save config button
    const btnSaveConfig = document.getElementById('btn-save-config');
    if (btnSaveConfig) {
        btnSaveConfig.addEventListener('click', saveConfig);
    }

    // Save my registration
    const btnSaveMyReg = document.getElementById('btn-save-my-reg');
    if (btnSaveMyReg) {
        btnSaveMyReg.addEventListener('click', saveMyRegistration);
    }

    // Generate mail
    const btnGenerateMail = document.getElementById('btn-generate-mail');
    if (btnGenerateMail) {
        btnGenerateMail.addEventListener('click', generateMail);
    }

    // Copy mail
    const btnCopyMail = document.getElementById('btn-copy-mail');
    if (btnCopyMail) {
        btnCopyMail.addEventListener('click', copyMail);
    }
});
