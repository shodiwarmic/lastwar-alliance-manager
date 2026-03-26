'use strict';

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

const TIMEZONE_MAP = {
    "America/New_York": { label: "US Eastern", stdOffset: -5, stdName: "EST" },
    "America/Los_Angeles": { label: "US Pacific", stdOffset: -8, stdName: "PST" },
    "Europe/London": { label: "UK", stdOffset: 0, stdName: "GMT" },
    "Europe/Berlin": { label: "CET", stdOffset: 1, stdName: "CET" },
    "Australia/Perth": { label: "AWST", stdOffset: 8, stdName: "AWST" }
};

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

    dropdown.replaceChildren();
    const refDate = new Date();

    STORM_SLOTS.forEach(slot => {
        let labelParts = [`${slot.start} Server Time`];

        selectedZones.forEach(zoneKey => {
            const tzInfo = TIMEZONE_MAP[zoneKey];
            if (!tzInfo) return;

            const [hours, minutes] = slot.start.split(':');
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

// ── State ─────────────────────────────────────────────────────────
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
let dragSourceGroupId = null;
let dragSourceBuildingId = null;
let dragSourceIsDirect = false;

// ── Error display ─────────────────────────────────────────────────
function showError(msg) {
    const el = document.getElementById('storm-error');
    el.textContent = msg;
    el.classList.remove('hidden');
    setTimeout(() => el.classList.add('hidden'), 6000);
}

// ── CSRF helper ───────────────────────────────────────────────────
function getCsrfToken() {
    const el = document.querySelector('input[name="gorilla.csrf.Token"]');
    return el ? el.value : '';
}

// ── API helpers ───────────────────────────────────────────────────
async function apiFetch(url, options = {}) {
    if (!options.headers) options.headers = {};
    if (options.method && options.method !== 'GET') {
        options.headers['X-CSRF-Token'] = getCsrfToken();
    }
    const res = await fetch(url, options);
    return res;
}

// ── All assigned IDs for current TF ──────────────────────────────
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

// ── Render all ────────────────────────────────────────────────────
function renderAll() {
    renderPool();
    renderGroups();
    updateCapacityBar();
}

// ── Render member pool ────────────────────────────────────────────
function getRegVal(memberId, slotIdx) {
    if (!slotIdx) return 0;
    const key = `slot_${slotIdx}`;
    const reg = registrations.find(r => r.member_id === memberId);
    let val = reg ? (reg[key] || 0) : 0;
    if (myRegistration && myRegistration.member_id === memberId) val = myRegistration[key] || 0;
    return val;
}

function renderPool() {
    const pool = document.getElementById('member-pool');
    if (!pool) return;

    const searchVal = (document.getElementById('pool-search') || {}).value || '';
    const assigned = allAssignedIds();
    const activeSlotIdx = tfConfig[currentTF];
    const otherTF = currentTF === 'A' ? 'B' : 'A';
    const otherSlotIdx = tfConfig[otherTF];

    const unassigned = allMembers.filter(m => {
        if (assigned.has(m.id)) return false;
        if (searchVal) {
            const q = searchVal.toLowerCase();
            if (!m.name.toLowerCase().includes(q) &&
                !m.rank.toLowerCase().includes(q) &&
                !(m.squad_type || '').toLowerCase().includes(q)) return false;
        }
        return true;
    });

    const regPriority = v => v === 1 ? 0 : v === 2 ? 1 : 2;
    unassigned.sort((a, b) => {
        const ra = regPriority(getRegVal(a.id, activeSlotIdx));
        const rb = regPriority(getRegVal(b.id, activeSlotIdx));
        if (ra !== rb) return ra - rb;
        return (b.power || 0) - (a.power || 0);
    });

    if (unassigned.length === 0) {
        const p = document.createElement('p');
        p.style.cssText = 'color:var(--text-secondary);font-size:0.85em;';
        p.textContent = 'All members assigned';
        pool.replaceChildren(p);
        return;
    }

    const cards = unassigned.map(m => {
        const regVal = getRegVal(m.id, activeSlotIdx);
        const otherRegVal = otherSlotIdx && otherSlotIdx !== activeSlotIdx ? getRegVal(m.id, otherSlotIdx) : 0;

        const card = document.createElement('div');
        card.className = 'pool-card' + (activeSlotIdx && regVal === 0 ? ' dimmed' : '');
        if (canManage) card.draggable = true;
        card.dataset.memberId = m.id;
        card.dataset.isSub = (regVal === 2) ? 'true' : 'false';

        const nameDiv = document.createElement('div');
        nameDiv.className = 'pool-name';
        nameDiv.textContent = m.name;
        card.appendChild(nameDiv);

        const metaDiv = document.createElement('div');
        metaDiv.className = 'pool-meta';
        const powerStr = m.power != null ? Number(m.power).toLocaleString() : '—';
        metaDiv.textContent = `${m.rank} · ⚡${powerStr}`;
        if (m.squad_power != null && m.squad_power > 0) {
            const SQUAD_ICON = { Tank: '🛡️', Aircraft: '✈️', Missile: '🚀' };
            const sqSpan = document.createElement('span');
            sqSpan.className = 'pool-squad-power';
            sqSpan.textContent = ` · ${SQUAD_ICON[m.squad_type] || ''}${Number(m.squad_power).toLocaleString()}`;
            metaDiv.appendChild(sqSpan);
        }
        card.appendChild(metaDiv);

        const regDiv = document.createElement('div');
        regDiv.className = 'pool-reg';

        const badge = document.createElement('span');
        if (!activeSlotIdx) {
            badge.className = 'reg-none';
            badge.textContent = 'No slot set';
        } else if (regVal === 1) {
            badge.className = 'reg-avail';
            badge.textContent = '✓ Available';
        } else if (regVal === 2) {
            badge.className = 'reg-sub';
            badge.textContent = '⚡ Sub Only';
        } else {
            badge.className = 'reg-none';
            badge.textContent = 'Not registered';
        }
        regDiv.appendChild(badge);

        if (otherRegVal === 1) {
            const ob = document.createElement('span');
            ob.className = 'reg-other reg-avail';
            ob.title = `Also available for TF-${otherTF}`;
            ob.textContent = `TF-${otherTF} ✓`;
            regDiv.appendChild(ob);
        } else if (otherRegVal === 2) {
            const ob = document.createElement('span');
            ob.className = 'reg-other reg-sub';
            ob.title = `Sub only for TF-${otherTF}`;
            ob.textContent = `TF-${otherTF} ⚡`;
            regDiv.appendChild(ob);
        }

        card.appendChild(regDiv);
        return card;
    });

    pool.replaceChildren(...cards);

    if (canManage) {
        pool.querySelectorAll('.pool-card').forEach(card => {
            card.addEventListener('dragstart', e => {
                dragMemberId = parseInt(card.dataset.memberId);
                dragIsSub = card.dataset.isSub === 'true';
                dragSourceGroupId = null;
                dragSourceBuildingId = null;
                dragSourceIsDirect = false;
                e.dataTransfer.effectAllowed = 'move';
            });
        });
    }
}

// ── Format power sum ──────────────────────────────────────────────
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

const SQUAD_ICON_MAP = { Tank: '🛡️', Aircraft: '✈️', Missile: '🚀' };

function memberChipBadges(member) {
    if (!member) return [];
    const badges = [];
    if (member.power != null) {
        const span = document.createElement('span');
        span.className = 'chip-power';
        span.textContent = `⚡${Number(member.power).toLocaleString()}`;
        badges.push(span);
    }
    const icon = SQUAD_ICON_MAP[member.squad_type];
    if (icon && member.squad_power != null && member.squad_power > 0) {
        const span = document.createElement('span');
        span.className = 'chip-power';
        span.textContent = `${icon}${Number(member.squad_power).toLocaleString()}`;
        badges.push(span);
    }
    return badges;
}

// ── DOM builders for group cards ──────────────────────────────────
function buildMemberChip(m, gid, bid, isDirect) {
    const member = allMembers.find(x => x.id === m.member_id);
    const mName = member ? member.name : `#${m.member_id}`;

    const chip = document.createElement('span');
    chip.className = 'member-chip' + (m.is_sub ? ' is-sub' : '');
    if (canManage) chip.draggable = true;
    chip.dataset.chipMemberId = m.member_id;
    chip.dataset.chipGroupId = gid;
    if (bid) chip.dataset.chipBuildingId = bid;
    chip.dataset.chipDirect = isDirect ? 'true' : 'false';

    chip.appendChild(document.createTextNode(mName));
    for (const badge of memberChipBadges(member)) {
        chip.appendChild(badge);
    }

    if (canManage) {
        const subToggle = document.createElement('span');
        subToggle.className = 'chip-sub-toggle';
        subToggle.title = m.is_sub ? 'Mark as Primary' : 'Mark as Sub';
        subToggle.textContent = m.is_sub ? 'SUB' : 'PRI';
        subToggle.dataset.groupId = gid;
        if (bid) subToggle.dataset.buildingId = bid;
        if (isDirect) subToggle.dataset.direct = 'true';
        subToggle.dataset.memberId = m.member_id;
        chip.appendChild(subToggle);

        const removeBtn = document.createElement('span');
        removeBtn.className = 'chip-remove';
        removeBtn.textContent = '×';
        removeBtn.dataset.groupId = gid;
        if (bid) removeBtn.dataset.buildingId = bid;
        if (isDirect) removeBtn.dataset.direct = 'true';
        removeBtn.dataset.memberId = m.member_id;
        chip.appendChild(removeBtn);
    }

    return chip;
}

function renderInlineSearch(groupId, buildingId, isDirect) {
    const wrapper = document.createElement('div');
    wrapper.className = 'inline-search';
    wrapper.style.marginTop = '4px';

    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'form-input inline-search-input';
    input.placeholder = 'Add member...';
    input.dataset.groupId = groupId;
    if (buildingId) input.dataset.buildingId = buildingId;
    if (isDirect) input.dataset.direct = 'true';

    const dropdown = document.createElement('div');
    dropdown.className = 'inline-dropdown';

    wrapper.append(input, dropdown);
    return wrapper;
}

function buildBuildingSlot(g, b) {
    const bInfo = BUILDINGS.find(x => x.id === b.building_id) || { name: b.building_id, priority: '' };

    const slot = document.createElement('div');
    slot.className = 'building-slot';
    slot.dataset.groupId = g.id;
    slot.dataset.buildingId = b.building_id;
    slot.dataset.gbId = b.id;

    const header = document.createElement('div');
    header.className = 'building-slot-header';
    header.appendChild(document.createTextNode(bInfo.name));

    if (bInfo.priority) {
        const prioritySpan = document.createElement('span');
        prioritySpan.style.cssText = 'font-size:0.75em;margin-left:4px;opacity:0.7;';
        prioritySpan.textContent = `[${bInfo.priority}]`;
        header.appendChild(prioritySpan);
    }

    const powerSpan = document.createElement('span');
    powerSpan.style.cssText = 'font-weight:400;font-size:0.85em;color:var(--text-secondary);';
    powerSpan.textContent = ` ${formatPowerSum(b.members.map(m => m.member_id))}`;
    header.appendChild(powerSpan);
    slot.appendChild(header);

    const membersDiv = document.createElement('div');
    for (const m of b.members) {
        membersDiv.appendChild(buildMemberChip(m, g.id, b.building_id, false));
    }
    slot.appendChild(membersDiv);

    if (canManage) {
        slot.appendChild(renderInlineSearch(g.id, b.building_id, false));

        const removeBtn = document.createElement('button');
        removeBtn.className = 'btn btn-secondary';
        removeBtn.style.cssText = 'margin-top:6px;padding:2px 8px;font-size:0.8em;';
        removeBtn.textContent = 'Remove building';
        removeBtn.addEventListener('click', () => removeBuilding(g.id, b.building_id));
        slot.appendChild(removeBtn);
    }

    return slot;
}

function buildDirectSlot(g) {
    const slot = document.createElement('div');
    slot.className = 'building-slot';
    slot.dataset.groupId = g.id;
    slot.dataset.direct = 'true';

    const header = document.createElement('div');
    header.className = 'building-slot-header';
    header.appendChild(document.createTextNode('Flexible Role'));

    const powerSpan = document.createElement('span');
    powerSpan.style.cssText = 'font-weight:400;font-size:0.85em;color:var(--text-secondary);';
    powerSpan.textContent = ` ${formatPowerSum(g.direct_members.map(m => m.member_id))}`;
    header.appendChild(powerSpan);
    slot.appendChild(header);

    const membersDiv = document.createElement('div');
    for (const m of g.direct_members) {
        membersDiv.appendChild(buildMemberChip(m, g.id, null, true));
    }
    slot.appendChild(membersDiv);

    if (canManage) {
        slot.appendChild(renderInlineSearch(g.id, null, true));
    }

    return slot;
}

function buildGroupCard(g) {
    const primaryIds = [];
    const subIds = [];
    for (const b of g.buildings) {
        for (const m of b.members) (m.is_sub ? subIds : primaryIds).push(m.member_id);
    }
    for (const m of g.direct_members) {
        (m.is_sub ? subIds : primaryIds).push(m.member_id);
    }

    const card = document.createElement('div');
    card.className = 'group-card';
    card.dataset.groupId = g.id;

    // Header
    const header = document.createElement('div');
    header.className = 'group-header';

    if (canManage) {
        const nameInput = document.createElement('input');
        nameInput.className = 'group-name-input';
        nameInput.type = 'text';
        nameInput.value = g.name;
        nameInput.dataset.groupId = g.id;
        nameInput.placeholder = 'Group name';
        header.appendChild(nameInput);
    } else {
        const h4 = document.createElement('h4');
        h4.style.margin = '0';
        h4.textContent = g.name;
        header.appendChild(h4);
    }

    const powerInfo = document.createElement('span');
    powerInfo.style.cssText = 'font-size:0.8em;color:var(--text-secondary);';
    powerInfo.textContent = `Primary: ${formatPowerSum(primaryIds)} | Sub: ${formatPowerSum(subIds)}`;
    header.appendChild(powerInfo);

    if (canManage) {
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'btn btn-danger';
        deleteBtn.style.cssText = 'padding:2px 8px;';
        deleteBtn.textContent = '✕';
        deleteBtn.addEventListener('click', () => deleteGroup(g.id));
        header.appendChild(deleteBtn);
    }

    card.appendChild(header);

    // Body
    const body = document.createElement('div');
    body.className = 'group-body';

    if (canManage) {
        const textarea = document.createElement('textarea');
        textarea.className = 'form-input';
        textarea.placeholder = 'Instructions...';
        textarea.rows = 2;
        textarea.style.cssText = 'width:100%;box-sizing:border-box;margin-bottom:8px;resize:vertical;';
        textarea.dataset.groupId = g.id;
        textarea.textContent = g.instructions || '';
        textarea.addEventListener('change', () => debouncedUpdateGroupMeta(g.id));
        body.appendChild(textarea);
    } else if (g.instructions) {
        const p = document.createElement('p');
        p.style.cssText = 'color:var(--text-secondary);font-size:0.9em;margin-bottom:8px;';
        p.textContent = g.instructions;
        body.appendChild(p);
    }

    for (const b of g.buildings) {
        body.appendChild(buildBuildingSlot(g, b));
    }

    body.appendChild(buildDirectSlot(g));

    if (canManage) {
        const usedBuildingIds = g.buildings.map(b => b.building_id);
        const availBuildings = BUILDINGS.filter(b => !usedBuildingIds.includes(b.id));
        if (availBuildings.length > 0) {
            const addDiv = document.createElement('div');
            addDiv.style.cssText = 'margin-top:8px;display:flex;gap:8px;align-items:center;';

            const select = document.createElement('select');
            select.className = 'form-input';
            select.id = `add-bldg-select-${g.id}`;
            select.style.flex = '1';
            for (const b of availBuildings) {
                const opt = document.createElement('option');
                opt.value = b.id;
                opt.textContent = b.name;
                select.appendChild(opt);
            }

            const addBtn = document.createElement('button');
            addBtn.className = 'btn btn-secondary';
            addBtn.style.cssText = 'padding:4px 10px;';
            addBtn.textContent = '+ Add Building';
            addBtn.addEventListener('click', () => addBuilding(g.id));

            addDiv.append(select, addBtn);
            body.appendChild(addDiv);
        }
    }

    card.appendChild(body);
    return card;
}

// ── Render groups ─────────────────────────────────────────────────
function renderGroups() {
    const container = document.getElementById('groups-container');
    if (!container) return;

    if (groups.length === 0) {
        const p = document.createElement('p');
        p.style.cssText = 'color:var(--text-secondary);';
        p.textContent = 'No groups yet.' + (canManage ? ' Click "+ Add Group" to create one.' : '');
        container.replaceChildren(p);
        return;
    }

    container.replaceChildren(...groups.map(buildGroupCard));

    // Wire drop zones
    if (canManage) {
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
    }

    // Wire chip dragstart
    container.querySelectorAll('.member-chip[draggable]').forEach(chip => {
        chip.addEventListener('dragstart', e => {
            dragMemberId = parseInt(chip.dataset.chipMemberId);
            dragSourceGroupId = parseInt(chip.dataset.chipGroupId);
            dragSourceBuildingId = chip.dataset.chipBuildingId || null;
            dragSourceIsDirect = chip.dataset.chipDirect === 'true';
            const srcG = groups.find(x => x.id === dragSourceGroupId);
            if (srcG) {
                let srcM;
                if (dragSourceIsDirect) {
                    srcM = srcG.direct_members.find(x => x.member_id === dragMemberId);
                } else {
                    const srcB = srcG.buildings.find(x => x.building_id === dragSourceBuildingId);
                    srcM = srcB && srcB.members.find(x => x.member_id === dragMemberId);
                }
                dragIsSub = srcM ? srcM.is_sub : false;
            }
            e.dataTransfer.effectAllowed = 'move';
            e.stopPropagation();
        });
    });

    // Wire sub toggles
    container.querySelectorAll('.chip-sub-toggle').forEach(btn => {
        btn.addEventListener('click', () => {
            const gid = parseInt(btn.dataset.groupId);
            const mid = parseInt(btn.dataset.memberId);
            const bid = btn.dataset.buildingId || null;
            const isDirect = btn.dataset.direct === 'true';
            toggleMemberSub(gid, bid, isDirect, mid);
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

    // Wire group name inputs
    container.querySelectorAll('.group-name-input').forEach(input => {
        input.addEventListener('input', () => {
            debouncedUpdateGroupMeta(parseInt(input.dataset.groupId));
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
        document.addEventListener('click', e => {
            if (!input.parentElement.contains(e.target)) {
                const dropdown = input.nextElementSibling;
                if (dropdown) dropdown.replaceChildren();
            }
        });
    });
}

function showInlineDropdown(dropdown, q, groupId, buildingId, isDirect) {
    const assigned = allAssignedIds();
    const filtered = allMembers.filter(m => {
        if (assigned.has(m.id)) return false;
        if (q && !m.name.toLowerCase().includes(q) && !m.rank.toLowerCase().includes(q)) return false;
        return true;
    }).slice(0, 20);

    if (filtered.length === 0) {
        const div = document.createElement('div');
        div.style.cssText = 'padding:6px 10px;color:var(--text-secondary);font-size:0.85em;';
        div.textContent = 'No members available';
        dropdown.replaceChildren(div);
        return;
    }

    const items = filtered.map(m => {
        const div = document.createElement('div');
        div.dataset.memberId = m.id;
        div.textContent = `${m.name} (${m.rank})`;
        div.addEventListener('click', () => {
            handleDrop(groupId, buildingId, isDirect, m.id, false);
            dropdown.replaceChildren();
            const input = dropdown.previousElementSibling;
            if (input) input.value = '';
        });
        return div;
    });

    dropdown.replaceChildren(...items);
}

function handleDrop(groupId, buildingId, isDirect, memberId, isSub) {
    if (dragSourceGroupId !== null) {
        const srcG = groups.find(x => x.id === dragSourceGroupId);
        if (srcG) {
            if (dragSourceIsDirect) {
                srcG.direct_members = srcG.direct_members.filter(m => m.member_id !== memberId);
            } else {
                const srcB = srcG.buildings.find(x => x.building_id === dragSourceBuildingId);
                if (srcB) srcB.members = srcB.members.filter(m => m.member_id !== memberId);
            }
        }
    } else {
        if (allAssignedIds().has(memberId)) return;
    }

    const g = groups.find(x => x.id === groupId);
    if (!g) return;

    if (isDirect) {
        g.direct_members.push({ id: 0, member_id: memberId, is_sub: isSub, position: g.direct_members.length });
    } else {
        const b = g.buildings.find(x => x.building_id === buildingId);
        if (!b) return;
        b.members.push({ id: 0, member_id: memberId, is_sub: isSub, position: b.members.length });
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

function toggleMemberSub(groupId, buildingId, isDirect, memberId) {
    const g = groups.find(x => x.id === groupId);
    if (!g) return;

    if (isDirect) {
        const m = g.direct_members.find(x => x.member_id === memberId);
        if (m) m.is_sub = !m.is_sub;
    } else {
        const b = g.buildings.find(x => x.building_id === buildingId);
        if (b) {
            const m = b.members.find(x => x.member_id === memberId);
            if (m) m.is_sub = !m.is_sub;
        }
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
    g.buildings.push({ id: 0, building_id: sel.value, sort_order: g.buildings.length, members: [] });
    debouncedSaveGroups();
    renderAll();
}

// ── Debounced saves ───────────────────────────────────────────────
function debouncedSaveGroups() {
    if (saveDebounceTimer) clearTimeout(saveDebounceTimer);
    saveDebounceTimer = setTimeout(saveAllGroups, 500);
}

async function saveAllGroups() {
    for (const g of groups) {
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

// ── Capacity bar ──────────────────────────────────────────────────
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

// ── Group CRUD ────────────────────────────────────────────────────
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
    const nameInput = document.querySelector(`.group-name-input[data-group-id="${groupId}"]`);
    if (nameInput) g.name = nameInput.value.trim() || g.name;
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

// ── My Registration ───────────────────────────────────────────────
function renderMyRegistration() {
    const container = document.getElementById('my-reg-slots');
    if (!container) return;

    const rows = STORM_SLOTS.map(slot => {
        const slotKey = `slot_${slot.id}`;
        const val = myRegState[slotKey] || 0;
        const selectA = document.getElementById('storm-time-select-a');
        let timeLabel = `Slot ${slot.id}: ${slot.start} Server Time`;
        if (selectA && selectA.options[slot.id - 1]) {
            timeLabel = `Slot ${slot.id}: ${selectA.options[slot.id - 1].text}`;
        }

        const row = document.createElement('div');
        row.className = 'my-slot-row';

        const label = document.createElement('div');
        label.className = 'slot-label';
        label.textContent = timeLabel;
        row.appendChild(label);

        const threeWay = document.createElement('div');
        threeWay.className = 'three-way';

        for (const [btnVal, btnClass, btnText] of [
            [0, val === 0 ? 'active-unavail' : '', 'Not Available'],
            [1, val === 1 ? 'active-avail' : '', '✓ Available'],
            [2, val === 2 ? 'active-sub' : '', '⚡ Sub Only'],
        ]) {
            const btn = document.createElement('button');
            if (btnClass) btn.className = btnClass;
            btn.textContent = btnText;
            btn.addEventListener('click', () => setMySlot(slotKey, btnVal));
            threeWay.appendChild(btn);
        }

        row.appendChild(threeWay);
        return row;
    });

    container.replaceChildren(...rows);
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

// ── Registration View ─────────────────────────────────────────────
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

    const table = document.createElement('table');
    table.className = 'reg-table';

    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    for (const text of ['Member', 'Rank', 'Power', slotLabels[0], slotLabels[1], slotLabels[2]]) {
        const th = document.createElement('th');
        th.textContent = text;
        headerRow.appendChild(th);
    }
    thead.appendChild(headerRow);
    table.appendChild(thead);

    const tbody = document.createElement('tbody');

    const sorted = [...registrations].sort((a, b) => {
        const ra = rankOrder.indexOf(a.member_rank);
        const rb = rankOrder.indexOf(b.member_rank);
        if (ra !== rb) return (ra === -1 ? 99 : ra) - (rb === -1 ? 99 : rb);
        return (b.member_power || 0) - (a.member_power || 0);
    });

    for (const reg of sorted) {
        const tr = document.createElement('tr');

        const nameCell = document.createElement('td');
        nameCell.textContent = reg.member_name;
        tr.appendChild(nameCell);

        const rankCell = document.createElement('td');
        rankCell.textContent = reg.member_rank;
        tr.appendChild(rankCell);

        const powerCell = document.createElement('td');
        powerCell.textContent = reg.member_power != null ? Number(reg.member_power).toLocaleString() : '—';
        tr.appendChild(powerCell);

        for (const s of [1, 2, 3]) {
            const td = document.createElement('td');
            const val = reg[`slot_${s}`] || 0;
            const pill = document.createElement('span');
            pill.className = `reg-pill s${val}`;
            pill.dataset.memberId = reg.member_id;
            pill.dataset.slot = s;
            pill.dataset.val = val;
            pill.textContent = val === 0 ? '—' : val === 1 ? '✓' : '⚡';
            pill.addEventListener('click', () => cycleRegPill(pill));
            td.appendChild(pill);
            tr.appendChild(td);
        }

        tbody.appendChild(tr);
    }

    table.appendChild(tbody);
    container.replaceChildren(table);
}

async function cycleRegPill(el) {
    const memberId = el.dataset.memberId;
    const slotNum = el.dataset.slot;
    const slotKey = `slot_${slotNum}`;
    let val = parseInt(el.dataset.val);
    val = (val + 1) % 3;

    const reg = registrations.find(r => r.member_id === parseInt(memberId));
    if (reg) reg[slotKey] = val;

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

// ── Save config ───────────────────────────────────────────────────
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

// ── Generate mail ─────────────────────────────────────────────────
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

async function copyMail() {
    const mailContent = document.getElementById('mail-content');
    if (!mailContent) return;
    const btn = document.getElementById('btn-copy-mail');
    try {
        await navigator.clipboard.writeText(mailContent.textContent);
        if (btn) {
            const orig = btn.textContent;
            btn.textContent = '✓ Copied!';
            setTimeout(() => { btn.textContent = orig; }, 2000);
        }
    } catch {
        showError('Copy failed — please select the text and copy manually.');
    }
}

// ── Init ──────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', async () => {
    const root = document.getElementById('storm-root');
    if (!root) return;

    canManage = root.dataset.canManage === 'true';

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

    try {
        const res = await fetch('/api/members');
        if (res.ok) {
            allMembers = await res.json();
            allMembers.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
        }
    } catch (e) {
        console.error('Error loading members:', e);
    }

    try {
        const res = await fetch(`${API_BASE}/config`);
        if (res.ok) {
            const configs = await res.json();
            for (const c of configs) {
                tfConfig[c.task_force] = c.time_slot;
            }
            const selA = document.getElementById('storm-time-select-a');
            const selB = document.getElementById('storm-time-select-b');
            if (selA && tfConfig.A) selA.value = tfConfig.A;
            if (selB && tfConfig.B) selB.value = tfConfig.B;
        }
    } catch (e) {
        console.error('Error loading TF config:', e);
    }

    try {
        const res = await fetch(`${API_BASE}/groups?task_force=${currentTF}`);
        if (res.ok) {
            groups = await res.json();
        }
    } catch (e) {
        console.error('Error loading groups:', e);
    }

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

    try {
        const res = await fetch(`${API_BASE}/registrations/me`);
        if (res.ok) {
            myRegistration = await res.json();
            myRegState.slot_1 = myRegistration.slot_1 || 0;
            myRegState.slot_2 = myRegistration.slot_2 || 0;
            myRegState.slot_3 = myRegistration.slot_3 || 0;
        }
    } catch (e) {
        myRegistration = null;
    }

    renderAll();
    renderMyRegistration();

    document.querySelectorAll('.storm-tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
    const defaultTabBtn = document.querySelector('.storm-tab[data-tab="my-reg"]');
    const defaultContent = document.getElementById('tab-my-reg');
    if (defaultTabBtn) defaultTabBtn.classList.add('active');
    if (defaultContent) defaultContent.classList.add('active');

    document.querySelectorAll('.storm-tab').forEach(tab => {
        tab.addEventListener('click', () => {
            document.querySelectorAll('.storm-tab').forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
            tab.classList.add('active');
            const target = document.getElementById('tab-' + tab.dataset.tab);
            if (target) {
                target.classList.add('active');
                if (tab.dataset.tab === 'planning') renderAll();
                else if (tab.dataset.tab === 'reg-view') renderRegistrationView();
                else if (tab.dataset.tab === 'my-reg') renderMyRegistration();
            }
        });
    });

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

    const poolSearch = document.getElementById('pool-search');
    if (poolSearch) poolSearch.addEventListener('input', renderPool);

    const btnAddGroup = document.getElementById('btn-add-group');
    if (btnAddGroup) {
        btnAddGroup.addEventListener('click', () => {
            const name = prompt('Group name:');
            if (name && name.trim()) createGroup(name.trim());
        });
    }

    const btnSaveConfig = document.getElementById('btn-save-config');
    if (btnSaveConfig) btnSaveConfig.addEventListener('click', saveConfig);

    const btnSaveMyReg = document.getElementById('btn-save-my-reg');
    if (btnSaveMyReg) btnSaveMyReg.addEventListener('click', saveMyRegistration);

    const btnGenerateMail = document.getElementById('btn-generate-mail');
    if (btnGenerateMail) btnGenerateMail.addEventListener('click', generateMail);

    const btnCopyMail = document.getElementById('btn-copy-mail');
    if (btnCopyMail) btnCopyMail.addEventListener('click', copyMail);
});
