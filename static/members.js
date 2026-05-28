'use strict';

const API_URL = '/api/members';

let editingMemberId = null;
let currentUsername = '';
let canManageRanks = false;
let isR5OrAdmin = false;
let isAdmin = false;
let canViewTrain = false;
let canManageTrain = false;
let allMembers = [];
let isPowerTrackingEnabled = false;
let isSquadTrackingEnabled = false;
let currentMaxHQ = 35;
let sortField = 'name';
let sortDir = 'asc';
let fuseInstance = null;
let skillRegistry = []; // [{key, label}] — populated from GET /api/skills
let currentFilteredMembers = [];

const SORT_DEFAULTS = {
    name: 'asc', rank: 'desc', power: 'desc',
    hq: 'desc', hero_power: 'desc', kills: 'desc', squad_power: 'desc',
};

const SKILL_EMOJI = { medical_aid: '🩹' };

// Define the HQ requirements for each Troop Tier
const TROOP_HQ_REQ = { 1: 1, 2: 4, 3: 6, 4: 10, 5: 14, 6: 17, 7: 20, 8: 24, 9: 27, 10: 30, 11: 35 };

function fetchPermissions() {
    const cfg = document.getElementById('page-config').dataset;
    currentUsername = cfg.username || '';
    isAdmin = cfg.isAdmin === 'true';
    canManageRanks = cfg.canManageMembers === 'true';
    isR5OrAdmin = cfg.canManageSettings === 'true';
    canViewTrain = cfg.viewTrain === 'true';
    canManageTrain = cfg.manageTrain === 'true';

    const eligibleRow = document.getElementById('eligible-filter-row');
    if (eligibleRow) eligibleRow.style.display = canViewTrain ? 'flex' : 'none';

    const modalEligibleWrapper = document.getElementById('modal-eligible-wrapper');
    if (modalEligibleWrapper) {
        modalEligibleWrapper.style.display = canManageTrain ? 'flex' : 'none';
    }

    const notesSection = document.getElementById('modal-notes-section');
    if (notesSection) notesSection.style.display = canManageRanks ? 'block' : 'none';
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
                el.style.display = isSquadTrackingEnabled ? '' : 'none';
            });
            const activeSortChip = document.querySelector('.sort-chip.active');
            if (activeSortChip && activeSortChip.style.display === 'none') {
                sortField = 'name';
                sortDir = 'asc';
                updateSortChips();
            }

            // Dynamically hide ANY troop tier that exceeds the server's Max HQ setting
            Object.entries(TROOP_HQ_REQ).forEach(([tier, reqHQ]) => {
                const chip = document.querySelector(`.troop-chip[data-troop="${tier}"]`);
                const option = document.querySelector(`#member-troop-level option[value="${tier}"]`);

                if (currentMaxHQ < reqHQ) {
                    if (chip) chip.style.display = 'none';
                    if (option) option.style.display = 'none';
                } else {
                    if (chip) chip.style.display = '';
                    if (option) option.style.display = '';
                }
            });
        }
    } catch (error) {
        console.error('Error fetching settings:', error);
    }
}

// Function to dynamically disable/enable troop options based on HQ level
window.updateTroopLevelOptions = function () {
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
                troopSelect.value = '';
            }
        }
    });
};

function updateDisplayedMembers() {
    const searchTerm = (document.getElementById('search-input')?.value || '').trim();

    let base;
    if (searchTerm.length > 0 && fuseInstance) {
        base = fuseInstance.search(searchTerm).map(r => r.item);
    } else {
        base = [...allMembers];
    }

    const activeRanks = Array.from(document.querySelectorAll('.rank-chip.active')).map(c => c.dataset.rank);
    const activeProfs = Array.from(document.querySelectorAll('.prof-chip.active')).map(c => c.dataset.prof);
    const activeSquads = Array.from(document.querySelectorAll('.squad-chip.active')).map(c => c.dataset.squad);
    const activeTroops = Array.from(document.querySelectorAll('.troop-chip.active')).map(c => c.dataset.troop);
    const activeEligible = document.querySelector('.eligible-chip.active')?.dataset.eligible || 'all';
    const activeSkills = Array.from(document.querySelectorAll('.skill-chip.active')).map(c => c.dataset.skill);

    let filtered = base.filter(member => {
        const matchesRank = activeRanks.includes('all') || activeRanks.includes(member.rank);

        const memProf = member.profession || 'none';
        const matchesProf = activeProfs.includes('all') || activeProfs.includes(memProf);

        const memSquad = member.squad_type || 'none';
        const matchesSquad = activeSquads.includes('all') || activeSquads.includes(memSquad);

        const memTroop = (member.troop_level || 0).toString();
        const matchesTroop = activeTroops.includes('all') || activeTroops.includes(memTroop);

        const matchesEligible =
            activeEligible === 'all' ||
            (activeEligible === 'eligible' && member.eligible !== false) ||
            (activeEligible === 'not-eligible' && member.eligible === false);

        const memberSkillList = member.skills ? member.skills.split(',') : [];
        const matchesSkill = activeSkills.length === 0 || activeSkills.includes('all') ||
            activeSkills.every(s => memberSkillList.includes(s));

        return matchesRank && matchesProf && matchesSquad && matchesTroop && matchesEligible && matchesSkill;
    });

    const RANK_ORDER = { R5: 5, R4: 4, R3: 3, R2: 2, R1: 1 };
    filtered.sort((a, b) => {
        let diff = 0;
        if (sortField === 'name')             diff = a.name.localeCompare(b.name);
        else if (sortField === 'rank')        diff = (RANK_ORDER[a.rank] || 0) - (RANK_ORDER[b.rank] || 0);
        else if (sortField === 'power')       diff = (a.power || 0) - (b.power || 0);
        else if (sortField === 'hq')          diff = (a.level || 0) - (b.level || 0);
        else if (sortField === 'hero_power')  diff = (a.hero_power || 0) - (b.hero_power || 0);
        else if (sortField === 'kills')       diff = (a.current_kills || 0) - (b.current_kills || 0);
        else if (sortField === 'squad_power') diff = (a.squad_power || 0) - (b.squad_power || 0);
        if (diff === 0) diff = a.name.localeCompare(b.name);
        return sortDir === 'asc' ? diff : -diff;
    });

    currentFilteredMembers = filtered;
    displayMembers(filtered);
    updateMemberCount(filtered.length);

    const clearBtn = document.getElementById('clear-search');
    if (clearBtn) clearBtn.style.display = searchTerm ? 'flex' : 'none';
}

function buildExportTable(members) {
    const table = document.createElement('table');
    const thead = document.createElement('thead');
    const headerRow = document.createElement('tr');
    ['Name', 'Rank', 'HQ Level', 'Power', 'Hero Power', 'Kills'].forEach(label => {
        const th = document.createElement('th');
        th.textContent = label;
        headerRow.appendChild(th);
    });
    thead.appendChild(headerRow);
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    members.forEach(m => {
        const tr = document.createElement('tr');
        [
            m.name || '',
            m.rank || '',
            m.level != null ? m.level : '',
            m.power != null ? m.power : '',
            m.hero_power != null ? m.hero_power : '',
            m.current_kills != null ? m.current_kills : '',
        ].forEach(val => {
            const td = document.createElement('td');
            td.textContent = val;
            tr.appendChild(td);
        });
        tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    return table;
}

document.addEventListener('DOMContentLoaded', async () => {
    await fetchPermissions();
    await fetchSettings();

    document.querySelectorAll('.power-sort-option').forEach(el => {
        el.style.display = isPowerTrackingEnabled ? '' : 'none';
    });
    const activeSortChipAfterPower = document.querySelector('.sort-chip.active');
    if (activeSortChipAfterPower && activeSortChipAfterPower.style.display === 'none') {
        sortField = 'name';
        sortDir = 'asc';
        updateSortChips();
    }

    const actionBar = document.querySelector('.action-bar');
    if (!canManageRanks && actionBar) {
        actionBar.style.display = 'none';
        const notice = document.createElement('div');
        notice.className = 'permission-notice';
        const p = document.createElement('p');
        p.textContent = 'ℹ️ Only R4 and R5 members can add or manage member ranks.';
        notice.appendChild(p);
        document.querySelector('main').insertBefore(notice, document.querySelector('.members-section'));
    }

    const hqInput = document.getElementById('member-level');
    if (hqInput) hqInput.addEventListener('input', updateTroopLevelOptions);

    setupModalListeners();
    setupCSVImport();
    setupSearch();
    await loadSkillRegistry();
    loadMembers();

    const membersList = document.getElementById('members-list');
    if (membersList) {
        const exportBar = document.createElement('div');
        exportBar.className = 'tab-toolbar';
        exportBar.style.justifyContent = 'flex-end';

        const exportCsvBtn = document.createElement('button');
        exportCsvBtn.className = 'btn btn-secondary btn-sm';
        exportCsvBtn.textContent = '↓ CSV';
        exportCsvBtn.addEventListener('click', () =>
            exportTableToCSV(buildExportTable(currentFilteredMembers), 'members.csv'));

        const exportXlsxBtn = document.createElement('button');
        exportXlsxBtn.className = 'btn btn-secondary btn-sm';
        exportXlsxBtn.textContent = '↓ XLSX';
        exportXlsxBtn.addEventListener('click', () =>
            exportTableToXLSX(buildExportTable(currentFilteredMembers), 'members.xlsx'));

        exportBar.append(exportCsvBtn, exportXlsxBtn);
        membersList.parentElement.insertBefore(exportBar, membersList);
    }
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

    window.addEventListener('click', event => {
        if (event.target === memberModal) closeMemberModalFunc();
    });

    if (memberForm) memberForm.addEventListener('submit', handleMemberFormSubmit);
}

function openMemberModal(editing = false) {
    if (!canManageRanks) return;
    const memberModal = document.getElementById('member-modal');
    if (memberModal) {
        memberModal.style.display = 'flex';
        trapFocus(memberModal);
        document.getElementById('member-name').focus();
    }
}

function closeMemberModalFunc() {
    const memberModal = document.getElementById('member-modal');
    if (memberModal) {
        releaseFocus(memberModal);
        memberModal.style.display = 'none';
        resetMemberForm();
    }
}

function resetMemberForm() {
    editingMemberId = null;
    document.getElementById('member-form').reset();
    document.getElementById('member-eligible').checked = true;

    const levelInput = document.getElementById('member-level');
    if (levelInput) levelInput.value = '';
    const profInput = document.getElementById('member-profession');
    if (profInput) profInput.value = '';
    const troopInput = document.getElementById('member-troop-level');
    if (troopInput) troopInput.value = '';

    const squadTypeInput = document.getElementById('member-squad-type');
    if (squadTypeInput) squadTypeInput.value = '';
    const sqPowerInput = document.getElementById('member-squad-power');
    if (sqPowerInput) sqPowerInput.value = '';
    const sqTimestampText = document.getElementById('modal-squad-power-timestamp');
    if (sqTimestampText) sqTimestampText.textContent = '';

    const squadSection = document.getElementById('modal-squad-section');
    if (squadSection) {
        squadSection.style.display = isSquadTrackingEnabled ? 'flex' : 'none';
    }

    const powerInput = document.getElementById('modal-member-power');
    if (powerInput) powerInput.value = '';
    const timestampText = document.getElementById('modal-power-timestamp');
    if (timestampText) timestampText.textContent = '';

    const powerSection = document.getElementById('modal-power-section');
    if (powerSection) {
        powerSection.style.display = isPowerTrackingEnabled ? 'block' : 'none';
    }

    if (typeof updateTroopLevelOptions === 'function') updateTroopLevelOptions();

    const notesInput = document.getElementById('member-notes');
    if (notesInput) notesInput.value = '';

    const heroPowerInput = document.getElementById('member-hero-power');
    if (heroPowerInput) heroPowerInput.value = '';
    const heroTimestampText = document.getElementById('modal-hero-power-timestamp');
    if (heroTimestampText) heroTimestampText.textContent = '';

    // Reset skill checkboxes
    document.querySelectorAll('[data-skill-key]').forEach(cb => { cb.checked = false; });

    document.getElementById('modal-form-title').textContent = 'Add New Member';
    document.getElementById('submit-btn').textContent = 'Add Member';
}

async function loadSkillRegistry() {
    try {
        const res = await fetch('/api/skills');
        if (res.ok) skillRegistry = await res.json();
    } catch (e) {
        console.error('Error loading skill registry:', e);
    }
}

async function loadMembers() {
    try {
        const response = await fetch(API_URL);
        const members = await response.json();
        allMembers = members;
        rebuildFuse();
        updateDisplayedMembers();
    } catch (error) {
        console.error('Error loading members:', error);
        const membersList = document.getElementById('members-list');
        if (membersList) {
            const p = document.createElement('p');
            p.className = 'empty';
            p.textContent = 'Error loading members. Please try again.';
            membersList.replaceChildren(p);
        }
    }
}

function displayMembers(members) {
    const membersList = document.getElementById('members-list');
    if (!membersList) return;

    if (!members || members.length === 0) {
        const p = document.createElement('p');
        p.className = 'empty';
        p.textContent = 'No members yet. Add your first alliance member!';
        membersList.replaceChildren(p);
        return;
    }

    membersList.replaceChildren(...members.map(buildMemberCard));
}

function buildMemberCard(member) {
    const card = document.createElement('div');
    card.className = 'member-card';

    // ── info column ──────────────────────────────────────────────
    const info = document.createElement('div');
    info.className = 'member-info';

    // Name + aliases + alias button
    const nameDiv = document.createElement('div');
    nameDiv.className = 'member-name';
    nameDiv.style.cssText = 'display:flex;align-items:center;gap:8px;';
    nameDiv.appendChild(document.createTextNode(member.name));

    if (member.personal_aliases) {
        const span = document.createElement('span');
        span.style.cssText = 'color:var(--color-info);font-size:0.85em;';
        span.textContent = `(${member.personal_aliases})`;
        nameDiv.appendChild(span);
    }
    if (member.global_aliases) {
        const span = document.createElement('span');
        span.style.cssText = 'color:var(--color-text-muted);font-size:0.85em;';
        span.textContent = `[${member.global_aliases}]`;
        nameDiv.appendChild(span);
    }

    const aliasBtn = document.createElement('button');
    aliasBtn.className = 'icon-btn';
    aliasBtn.title = 'Manage Nicknames';
    aliasBtn.style.cssText = 'background:none;border:none;cursor:pointer;opacity:0.6;padding:0;';
    aliasBtn.textContent = '🏷️';
    aliasBtn.addEventListener('click', () => openAliasModal(member.id, member.name));
    nameDiv.appendChild(aliasBtn);

    info.appendChild(nameDiv);

    // Rank badge
    const rankBadge = document.createElement('span');
    rankBadge.className = `member-rank rank-${member.rank.replace(/\s+/g, '-')}`;
    rankBadge.textContent = member.rank;
    info.appendChild(rankBadge);

    // HQ badge
    if (member.level) {
        const badge = document.createElement('span');
        badge.className = 'member-rank badge-hq';
        badge.textContent = `HQ ${member.level}`;
        info.appendChild(badge);
    }

    // Troop badge
    if (member.troop_level) {
        const badge = document.createElement('span');
        badge.className = 'member-rank badge-troop';
        badge.textContent = `T${member.troop_level}`;
        info.appendChild(badge);
    }

    // Profession badge
    if (member.profession) {
        const badge = document.createElement('span');
        badge.className = 'member-rank badge-profession';
        badge.textContent = member.profession;
        info.appendChild(badge);
    }

    // Skill badges
    if (member.skills) {
        const memberSkills = member.skills.split(',');
        skillRegistry.forEach(({ key, label }) => {
            if (memberSkills.includes(key)) {
                const badge = document.createElement('span');
                badge.className = 'badge-skill';
                badge.textContent = SKILL_EMOJI[key] || '⚕️';
                badge.title = label;
                info.appendChild(badge);
            }
        });
    }

    // Power
    if (isPowerTrackingEnabled && member.power) {
        const span = document.createElement('span');
        span.className = 'member-power';
        span.title = `Overall Power: ${member.power.toLocaleString()}`;
        span.textContent = formatPower(member.power);
        info.appendChild(span);
    }

    // Squad
    if (isSquadTrackingEnabled && (member.squad_type || member.squad_power)) {
        let typeIcon = '';
        if (member.squad_type === 'Tank') typeIcon = '🛡️ ';
        else if (member.squad_type === 'Aircraft') typeIcon = '✈️ ';
        else if (member.squad_type === 'Missile') typeIcon = '🚀 ';
        const span = document.createElement('span');
        span.className = 'member-power';
        span.title = `Squad Power: ${member.squad_power ? member.squad_power.toLocaleString() : 0}`;
        span.textContent = `${typeIcon}${formatPower(member.squad_power)}`;
        info.appendChild(span);
    }

    // Hero Power
    if (member.hero_power > 0) {
        const span = document.createElement('span');
        span.className = 'member-power';
        span.title = `Total Hero Power: ${member.hero_power.toLocaleString()}`;
        span.textContent = `🦸 ${formatPower(member.hero_power)}`;
        info.appendChild(span);
    }

    // Troop Kills
    if (member.current_kills > 0) {
        const span = document.createElement('span');
        span.className = 'member-power';
        span.title = `Troop Kills: ${member.current_kills.toLocaleString()}`;
        span.textContent = `💀 ${formatPower(member.current_kills)}`;
        info.appendChild(span);
    }

    // Eligible status (view only)
    if (canViewTrain) {
        const eligible = member.eligible !== false;
        const span = document.createElement('span');
        span.className = `member-eligible ${eligible ? 'eligible' : 'not-eligible'}`;
        span.textContent = eligible ? '✓ Eligible' : '✗ Not Eligible';
        info.appendChild(span);
    }

    card.appendChild(info);

    // ── actions column ───────────────────────────────────────────
    if (canManageRanks) {
        const actions = document.createElement('div');
        actions.className = 'member-actions';

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-sm btn-secondary';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => editMember(member));
        actions.appendChild(editBtn);

        if (member.rank !== 'EX') {
            const archiveBtn = document.createElement('button');
            archiveBtn.className = 'btn btn-sm btn-danger';
            archiveBtn.textContent = 'Archive';
            archiveBtn.addEventListener('click', () => archiveMember(member.id, member.name, actions, archiveBtn));
            actions.appendChild(archiveBtn);
        }

        if (isR5OrAdmin && !member.has_user) {
            const inviteUserBtn = document.createElement('button');
            inviteUserBtn.className = 'invite-user-btn';
            inviteUserBtn.textContent = 'Invite User';
            inviteUserBtn.addEventListener('click', () => inviteUserForMember(member.id, member.name, actions, inviteUserBtn));
            actions.appendChild(inviteUserBtn);
        }

        if (canManageTrain) {
            const eligible = member.eligible !== false;
            const toggleBtn = document.createElement('button');
            toggleBtn.className = `toggle-eligible-btn ${eligible ? 'eligible' : 'not-eligible'}`;
            toggleBtn.textContent = eligible ? '✓ Eligible' : '✗ Not Eligible';
            toggleBtn.addEventListener('click', () => toggleEligible(member.id, eligible, actions, toggleBtn));
            actions.appendChild(toggleBtn);
        }

        card.appendChild(actions);
    }

    return card;
}

async function handleMemberFormSubmit(e) {
    e.preventDefault();
    if (!canManageRanks) return;

    const name = document.getElementById('member-name').value.trim();
    const rank = document.getElementById('member-rank').value;
    const eligible = document.getElementById('member-eligible').checked;

    const level = parseInt(document.getElementById('member-level').value, 10) || 0;
    const profession = document.getElementById('member-profession').value;
    const squad_type = document.getElementById('member-squad-type').value;
    const troop_level = parseInt(document.getElementById('member-troop-level').value, 10) || 0;

    const sqPowerInput = document.getElementById('member-squad-power');
    const squad_power = (sqPowerInput && sqPowerInput.value !== '') ? parseInt(sqPowerInput.value, 10) : 0;

    if (level > currentMaxHQ) {
        showModalStatus(`HQ Level cannot exceed the server maximum of ${currentMaxHQ}.`);
        return;
    }
    if (level < 0) {
        showModalStatus('HQ Level cannot be negative.');
        return;
    }

    const powerInput = document.getElementById('modal-member-power');
    const power = (powerInput && powerInput.value !== '') ? parseInt(powerInput.value, 10) : 0;

    const heroPowerInput = document.getElementById('member-hero-power');
    const hero_power = (heroPowerInput && heroPowerInput.value !== '') ? parseInt(heroPowerInput.value, 10) : 0;

    const killCountInput = document.getElementById('member-kill-count');
    const current_kills = (killCountInput && killCountInput.value !== '') ? parseInt(killCountInput.value, 10) : 0;

    const notesInput = document.getElementById('member-notes');
    const notes = (notesInput && canManageRanks) ? notesInput.value.trim() : '';

    if (!name || !rank) {
        showModalStatus('Please fill in Name and Rank.');
        return;
    }

    try {
        let targetId;
        if (editingMemberId) {
            const response = await fetch(`${API_URL}/${editingMemberId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, rank, level, eligible, power, profession, squad_type, troop_level, squad_power, hero_power, current_kills, notes }),
            });
            if (!response.ok) throw new Error('Failed to update member');
            targetId = editingMemberId;
            editingMemberId = null;
        } else {
            const response = await fetch(API_URL, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, rank, level, eligible, power, profession, squad_type, troop_level, squad_power, hero_power, current_kills, notes }),
            });
            if (!response.ok) {
                if (response.status === 403) throw new Error('Permission denied');
                throw new Error('Failed to add member');
            }
            const newMember = await response.json();
            targetId = newMember.id;
        }

        const skillsArr = [...document.querySelectorAll('[data-skill-key]:checked')].map(el => el.dataset.skillKey);
        const skillsRes = await fetch(`${API_URL}/${targetId}/skills`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ skills: skillsArr }),
        });
        if (!skillsRes.ok) throw new Error('Failed to save skills');

        closeMemberModalFunc();
        await loadMembers();
    } catch (error) {
        console.error('Error saving member:', error);
        showModalStatus('Failed to save member. Please try again.');
    }
}

function showModalStatus(msg) {
    let el = document.getElementById('member-modal-status');
    if (!el) {
        el = document.createElement('p');
        el.id = 'member-modal-status';
        el.className = 'status-msg';
        el.style.color = 'var(--color-danger)';
        document.getElementById('submit-btn')?.parentElement?.prepend(el);
    }
    el.textContent = msg;
    clearTimeout(el._timer);
    el._timer = setTimeout(() => { el.textContent = ''; }, 5000);
}

window.editMember = function (member) {
    if (!canManageRanks) return;
    editingMemberId = member.id;

    const eligible = member.eligible !== false;
    const power = member.power || 0;
    const powerUpdatedAt = member.power_updated_at || '';
    const level = member.level || 0;
    const squadType = member.squad_type || '';
    const squadPower = member.squad_power || 0;
    const squadPowerUpdatedAt = member.squad_power_updated_at || '';
    const troopLevel = member.troop_level || 0;
    const profession = member.profession || '';
    const notes = member.notes || '';
    const heroPower = member.hero_power || 0;
    const heroPowerUpdatedAt = member.hero_power_updated_at || '';
    const currentKills = member.current_kills || 0;
    const killsUpdatedAt = member.kills_updated_at || '';

    document.getElementById('member-name').value = member.name;
    document.getElementById('member-rank').value = member.rank;
    document.getElementById('member-eligible').checked = eligible;

    const levelInput = document.getElementById('member-level');
    if (levelInput) levelInput.value = (level > 0) ? level : '';

    const powerSection = document.getElementById('modal-power-section');
    if (powerSection) powerSection.style.display = isPowerTrackingEnabled ? 'block' : 'none';

    const powerInput = document.getElementById('modal-member-power');
    if (powerInput) powerInput.value = (power && power > 0) ? power : '';

    const timestampText = document.getElementById('modal-power-timestamp');
    if (timestampText) {
        if (powerUpdatedAt) {
            const updatedDate = new Date(powerUpdatedAt.replace(' ', 'T') + 'Z');
            timestampText.textContent = `Last updated: ${updatedDate.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })}`;
        } else {
            timestampText.textContent = 'Last updated: Never';
        }
    }

    const profInput = document.getElementById('member-profession');
    if (profInput) profInput.value = profession;

    const troopInput = document.getElementById('member-troop-level');
    if (troopInput) troopInput.value = (troopLevel > 0) ? troopLevel : '';

    const squadSection = document.getElementById('modal-squad-section');
    if (squadSection) squadSection.style.display = isSquadTrackingEnabled ? 'flex' : 'none';

    const squadTypeInput = document.getElementById('member-squad-type');
    if (squadTypeInput) squadTypeInput.value = squadType;

    const sqPowerInput = document.getElementById('member-squad-power');
    if (sqPowerInput) sqPowerInput.value = (squadPower && squadPower > 0) ? squadPower : '';

    const sqTimestampText = document.getElementById('modal-squad-power-timestamp');
    if (sqTimestampText) {
        if (squadPowerUpdatedAt) {
            const updatedDate = new Date(squadPowerUpdatedAt.replace(' ', 'T') + 'Z');
            sqTimestampText.textContent = `Last updated: ${updatedDate.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })}`;
        } else {
            sqTimestampText.textContent = 'Last updated: Never';
        }
    }

    if (typeof updateTroopLevelOptions === 'function') updateTroopLevelOptions();

    const notesInput = document.getElementById('member-notes');
    if (notesInput) notesInput.value = notes;

    const heroPowerInput = document.getElementById('member-hero-power');
    if (heroPowerInput) heroPowerInput.value = (heroPower && heroPower > 0) ? heroPower : '';

    const heroTimestampText = document.getElementById('modal-hero-power-timestamp');
    if (heroTimestampText) {
        if (heroPowerUpdatedAt) {
            const updatedDate = new Date(heroPowerUpdatedAt.replace(' ', 'T') + 'Z');
            heroTimestampText.textContent = `Last updated: ${updatedDate.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })}`;
        } else {
            heroTimestampText.textContent = 'Last updated: Never';
        }
    }

    const killCountInput = document.getElementById('member-kill-count');
    if (killCountInput) killCountInput.value = (currentKills && currentKills > 0) ? currentKills : '';

    const killTimestampText = document.getElementById('modal-kill-count-timestamp');
    if (killTimestampText) {
        if (killsUpdatedAt) {
            const updatedDate = new Date(killsUpdatedAt.replace(' ', 'T') + 'Z');
            killTimestampText.textContent = `Last updated: ${updatedDate.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })}`;
        } else {
            killTimestampText.textContent = 'Last updated: Never';
        }
    }

    // Pre-check skill checkboxes
    const memberSkills = (member.skills || '').split(',').filter(Boolean);
    document.querySelectorAll('[data-skill-key]').forEach(cb => {
        cb.checked = memberSkills.includes(cb.dataset.skillKey);
    });

    document.getElementById('modal-form-title').textContent = 'Edit Member';
    document.getElementById('submit-btn').textContent = 'Update Member';
    openMemberModal(true);
};

async function archiveMember(id, name, actionsContainer, archiveBtn) {
    if (!await showConfirm(`Archive ${name}?`, 'Archive')) return;

    archiveBtn.style.display = 'none';
    const reasonSpan = document.createElement('span');
    reasonSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;flex-wrap:wrap;';
    const reasonInput = document.createElement('input');
    reasonInput.type = 'text';
    reasonInput.placeholder = 'Reason for leaving (optional)';
    reasonInput.style.cssText = 'font-size:0.85rem;padding:3px 6px;border:1px solid var(--color-border);border-radius:4px;min-width:180px;';
    const confirmBtn = document.createElement('button');
    confirmBtn.className = 'btn btn-danger btn-sm';
    confirmBtn.textContent = 'Confirm';
    const skipBtn = document.createElement('button');
    skipBtn.className = 'btn btn-secondary btn-sm';
    skipBtn.textContent = 'Skip';

    async function doArchive(leaveReason) {
        try {
            const response = await fetch(`${API_URL}/${id}/archive`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ leave_reason: leaveReason }),
            });
            if (!response.ok) throw new Error('Failed to archive member');
            await loadMembers();
        } catch (error) {
            console.error('Error archiving member:', error);
            reasonSpan.remove();
            showToast('Failed to archive.', 'error');
            archiveBtn.style.display = '';
        }
    }

    confirmBtn.addEventListener('click', () => doArchive(reasonInput.value.trim()));
    skipBtn.addEventListener('click', () => doArchive(''));

    reasonSpan.append(reasonInput, confirmBtn, skipBtn);
    actionsContainer.appendChild(reasonSpan);
}

window.toggleEligible = async function (id, currentStatus, actionsContainer, toggleBtn) {
    if (!canManageTrain) return;

    const newStatus = !currentStatus;
    const statusText = newStatus ? 'eligible' : 'not eligible';

    if (!await showConfirm(`Mark as ${statusText}?`, 'Yes')) return;
    try {
        const response = await fetch(`${API_URL}`);
        if (!response.ok) throw new Error('fetch failed');
        const members = await response.json();
        const member = members.find(m => m.id === id);
        if (!member) throw new Error('Member not found');
        const updateResponse = await fetch(`${API_URL}/${id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ...member, eligible: newStatus }),
        });
        if (!updateResponse.ok) throw new Error('update failed');
        loadMembers();
    } catch (error) {
        console.error('Error toggling eligibility:', error);
        showToast('Failed to update eligibility.', 'error');
    }
};

function fallbackCopy(text, onSuccess) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;top:-9999px;left:-9999px;opacity:0;';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    try {
        if (document.execCommand('copy')) onSuccess();
    } finally {
        document.body.removeChild(ta);
    }
}

window.inviteUserForMember = async function (memberId, memberName, actionsContainer, inviteBtn) {
    if (!await showConfirm(`Send invite to ${memberName}?`, 'Send')) return;
    inviteBtn.style.display = 'none';
    try {
        const response = await fetch(`${API_URL}/${memberId}/invite`, { method: 'POST' });
        if (!response.ok) {
            const errText = await response.text();
            throw new Error(errText);
        }
        const result = await response.json();
        const inviteBox = document.createElement('div');
        inviteBox.style.cssText = 'display:flex;flex-direction:column;gap:6px;font-size:0.85rem;padding:10px;background:var(--color-surface);border:1px solid var(--color-border);border-radius:6px;max-width:320px;';
        const heading = document.createElement('span');
        heading.style.fontWeight = 'bold';
        heading.textContent = 'Invite link (valid 48h):';
        const linkRow = document.createElement('div');
        linkRow.style.cssText = 'display:flex;gap:6px;align-items:center;';
        const linkAnchor = document.createElement('a');
        linkAnchor.href = result.invite_url;
        linkAnchor.textContent = window.location.origin + result.invite_url;
        linkAnchor.style.cssText = 'font-size:0.8rem;word-break:break-all;';
        const copyBtn = document.createElement('button');
        copyBtn.className = 'btn btn-secondary btn-sm';
        copyBtn.textContent = 'Copy';
        copyBtn.style.flexShrink = '0';
        copyBtn.addEventListener('click', () => {
            const fullURL = window.location.origin + result.invite_url;
            const onSuccess = () => {
                copyBtn.textContent = 'Copied!';
                setTimeout(() => { copyBtn.textContent = 'Copy'; }, 2000);
            };
            if (navigator.clipboard && window.isSecureContext) {
                navigator.clipboard.writeText(fullURL).then(onSuccess).catch(() => fallbackCopy(fullURL, onSuccess));
            } else {
                fallbackCopy(fullURL, onSuccess);
            }
        });
        linkRow.append(linkAnchor, copyBtn);
        const dismissBtn = document.createElement('button');
        dismissBtn.className = 'btn btn-secondary btn-sm';
        dismissBtn.textContent = 'Dismiss';
        dismissBtn.addEventListener('click', async () => {
            inviteBox.remove();
            await loadMembers();
        });
        inviteBox.append(heading, linkRow, dismissBtn);
        actionsContainer.appendChild(inviteBox);
    } catch (error) {
        console.error('Error generating invite:', error);
        showToast(error.message || 'Failed to generate invite.', 'error');
        inviteBtn.style.display = '';
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

function setupSearch() {
    const searchInput = document.getElementById('search-input');
    const clearBtn = document.getElementById('clear-search');

    if (searchInput) searchInput.addEventListener('input', updateDisplayedMembers);

    if (clearBtn) {
        clearBtn.addEventListener('click', () => {
            searchInput.value = '';
            updateDisplayedMembers();
            searchInput.focus();
        });
    }

    function setupChipGroup(chipSelector, dataAttribute) {
        const chips = document.querySelectorAll(chipSelector);
        chips.forEach(chip => {
            chip.addEventListener('click', e => {
                const clickedValue = e.target.getAttribute(`data-${dataAttribute}`);

                if (clickedValue === 'all') {
                    chips.forEach(c => c.classList.remove('active'));
                    e.target.classList.add('active');
                } else {
                    document.querySelector(`${chipSelector}[data-${dataAttribute}="all"]`).classList.remove('active');
                    e.target.classList.toggle('active');

                    const activeChips = document.querySelectorAll(`${chipSelector}.active`);
                    if (activeChips.length === 0) {
                        document.querySelector(`${chipSelector}[data-${dataAttribute}="all"]`).classList.add('active');
                    }
                }
                updateDisplayedMembers();
            });
        });
    }

    setupChipGroup('.rank-chip', 'rank');
    setupChipGroup('.prof-chip', 'prof');
    setupChipGroup('.squad-chip', 'squad');
    setupChipGroup('.troop-chip', 'troop');
    setupChipGroup('.skill-chip', 'skill');

    document.querySelectorAll('.eligible-chip').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.eligible-chip').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            updateDisplayedMembers();
        });
    });

    document.querySelectorAll('.sort-chip').forEach(btn => {
        btn.addEventListener('click', () => {
            const field = btn.dataset.sort;
            if (sortField === field) {
                sortDir = sortDir === 'asc' ? 'desc' : 'asc';
            } else {
                sortField = field;
                sortDir = SORT_DEFAULTS[field] || 'asc';
            }
            updateSortChips();
            updateDisplayedMembers();
        });
    });
}

function rebuildFuse() {
    if (typeof Fuse === 'undefined') return;
    fuseInstance = new Fuse(allMembers, {
        keys: ['name', 'global_aliases', 'personal_aliases'],
        threshold: 0.4,
        includeScore: false,
        minMatchCharLength: 1,
    });
}

function updateSortChips() {
    const SORT_LABELS = {
        name: 'Name', rank: 'Rank', power: 'Power',
        hq: 'HQ', hero_power: 'Hero Power', kills: 'Kills', squad_power: 'Squad Power',
    };
    document.querySelectorAll('.sort-chip').forEach(btn => {
        const field = btn.dataset.sort;
        const isActive = field === sortField;
        btn.classList.toggle('active', isActive);
        btn.textContent = SORT_LABELS[field] + (isActive ? (sortDir === 'asc' ? ' ↑' : ' ↓') : '');
    });
}

// ── CSV Import ────────────────────────────────────────────────────
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
        if (!canManageRanks) return;
        const file = fileInput.files[0];
        if (!file || !file.name.endsWith('.csv')) {
            displayImportError('Please select a valid CSV file.');
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
                modal.style.display = 'flex';
                trapFocus(modal);
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

    const closeCSVModal = () => { releaseFocus(modal); modal.style.display = 'none'; };
    closeModal.addEventListener('click', closeCSVModal);
    cancelBtn.addEventListener('click', closeCSVModal);

    confirmBtn.addEventListener('click', async () => {
        const selectedMembers = detectedCSVMembers.filter((_, i) => selectedCSVMembers.has(i));
        if (selectedMembers.length === 0) {
            displayCSVModalStatus('Please select at least one member to import.');
            return;
        }

        const renames = [];
        document.querySelectorAll('.rename-select').forEach(select => {
            if (select.value) renames.push({ old_name: select.value, new_name: select.dataset.newName });
        });

        const removeMemberIDs = Array.from(selectedRemoveMembers);
        if (removeMemberIDs.length > 0) {
            if (!await showConfirm(`Delete ${removeMemberIDs.length} member(s)? This cannot be undone.`, 'Delete')) return;
        }

        doImport(selectedMembers, removeMemberIDs, renames, confirmBtn, modal, fileInput);
    });
}

async function doImport(selectedMembers, removeMemberIDs, renames, confirmBtn, modal, fileInput) {
    confirmBtn.disabled = true;
    confirmBtn.textContent = 'Importing...';
    try {
        const response = await fetch('/api/members/import/confirm', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ members: selectedMembers, remove_member_ids: removeMemberIDs, renames }),
        });
        if (!response.ok) throw new Error('Failed to import members');
        const result = await response.json();
        closeCSVModal();

        const resultDiv = document.getElementById('import-result');
        resultDiv.style.display = 'block';
        resultDiv.className = 'import-result success';
        const strong = document.createElement('strong');
        strong.textContent = `✓ Successfully imported ${result.added + result.updated} member(s)`;
        resultDiv.replaceChildren(strong);

        await loadMembers();
        fileInput.value = '';
    } catch (error) {
        console.error('Confirm error:', error);
        displayCSVModalStatus('Error importing members. Please try again.');
    } finally {
        confirmBtn.disabled = false;
        confirmBtn.textContent = '✔ Confirm & Import Selected';
    }
}

function displayCSVModalStatus(msg) {
    let el = document.getElementById('csv-modal-status');
    if (!el) {
        el = document.createElement('p');
        el.id = 'csv-modal-status';
        el.className = 'status-msg';
        el.style.color = 'var(--color-danger)';
        document.getElementById('confirm-csv-btn')?.parentElement?.prepend(el);
    }
    el.textContent = msg;
    clearTimeout(el._timer);
    el._timer = setTimeout(() => { el.textContent = ''; }, 5000);
}

function showCSVPreview(result) {
    const summaryDiv = document.getElementById('csv-summary');
    const previewDiv = document.getElementById('csv-members-preview');

    const newCount = result.detected_members.filter(m => m.is_new).length;
    const changedCount = result.detected_members.filter(m => m.rank_changed).length;
    const unchangedCount = result.detected_members.length - newCount - changedCount;

    // Summary stats
    const stats = document.createElement('div');
    stats.className = 'summary-stats';
    for (const { label, value, cls } of [
        { label: 'Total Members:', value: result.detected_members.length, cls: '' },
        { label: 'New Members:', value: newCount, cls: 'new' },
        { label: 'Rank Changes:', value: changedCount, cls: 'change' },
        { label: 'No Changes:', value: unchangedCount, cls: '' },
    ]) {
        const item = document.createElement('div');
        item.className = 'stat-item';
        const lbl = document.createElement('span');
        lbl.className = cls ? `stat-label ${cls}` : 'stat-label';
        lbl.textContent = label;
        const val = document.createElement('span');
        val.className = cls ? `stat-value ${cls}` : 'stat-value';
        val.textContent = value;
        item.append(lbl, val);
        stats.appendChild(item);
    }
    summaryDiv.replaceChildren(stats);

    // Member list
    const listDiv = document.createElement('div');
    listDiv.className = 'csv-members-list';

    result.detected_members.forEach((member, index) => {
        const statusClass = member.is_new ? 'new' : (member.rank_changed ? 'changed' : 'unchanged');
        const statusText = member.is_new ? 'NEW' : (member.rank_changed ? `${member.old_rank} → ${member.rank}` : 'No Change');

        const item = document.createElement('div');
        item.className = `csv-member-item ${statusClass}`;

        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.className = 'member-checkbox';
        checkbox.dataset.index = index;
        checkbox.checked = selectedCSVMembers.has(index);
        checkbox.addEventListener('change', e => {
            const idx = parseInt(e.target.dataset.index);
            e.target.checked ? selectedCSVMembers.add(idx) : selectedCSVMembers.delete(idx);
        });
        item.appendChild(checkbox);

        const memberInfo = document.createElement('div');
        memberInfo.className = 'member-info';

        const nameSpan = document.createElement('span');
        nameSpan.className = 'member-name';
        nameSpan.textContent = member.name;
        memberInfo.appendChild(nameSpan);

        const rankSpan = document.createElement('span');
        rankSpan.className = `member-rank rank-${member.rank}`;
        rankSpan.textContent = member.rank;
        memberInfo.appendChild(rankSpan);

        if (member.level) {
            const s = document.createElement('span');
            s.className = 'member-rank badge-hq';
            s.textContent = `HQ ${member.level}`;
            memberInfo.appendChild(s);
        }
        if (member.troop_level) {
            const s = document.createElement('span');
            s.className = 'member-rank badge-troop';
            s.textContent = `T${member.troop_level}`;
            memberInfo.appendChild(s);
        }
        if (member.squad_type) {
            const s = document.createElement('span');
            s.className = 'member-rank badge-squad-type';
            s.textContent = member.squad_type;
            memberInfo.appendChild(s);
        }
        if (member.profession) {
            const s = document.createElement('span');
            s.className = 'member-rank badge-profession';
            s.textContent = member.profession;
            memberInfo.appendChild(s);
        }
        if (member.power) {
            const s = document.createElement('span');
            s.className = 'member-power';
            s.style.cssText = 'font-size:0.85em;';
            s.textContent = `⚡ ${(member.power / 1000000).toFixed(1)}M`;
            memberInfo.appendChild(s);
        }
        if (member.squad_power) {
            const s = document.createElement('span');
            s.className = 'member-power';
            s.style.cssText = 'font-size:0.85em;color:var(--color-accent);';
            s.textContent = `🛡️ ${(member.squad_power / 1000000).toFixed(1)}M`;
            memberInfo.appendChild(s);
        }

        const statusSpan = document.createElement('span');
        statusSpan.className = 'member-status';
        statusSpan.textContent = statusText;
        memberInfo.appendChild(statusSpan);

        item.appendChild(memberInfo);

        // Similar match dropdown
        if (member.similar_match && member.similar_match.length > 0) {
            const notice = document.createElement('div');
            notice.className = 'similar-match-notice';

            const icon = document.createElement('span');
            icon.className = 'warning-icon';
            icon.textContent = '⚠️';

            const text = document.createElement('span');
            text.textContent = 'Similar name(s) found.';

            const select = document.createElement('select');
            select.className = 'rename-select';
            select.dataset.index = index;
            select.dataset.newName = member.name;

            const defaultOpt = document.createElement('option');
            defaultOpt.value = '';
            defaultOpt.textContent = 'Add as new member';
            select.appendChild(defaultOpt);

            for (const oldName of member.similar_match) {
                const opt = document.createElement('option');
                opt.value = oldName;
                opt.textContent = `Rename "${oldName}"`;
                select.appendChild(opt);
            }

            notice.append(icon, text, select);
            item.appendChild(notice);
        }

        listDiv.appendChild(item);
    });

    previewDiv.replaceChildren(listDiv);

    // Remove members section
    const removeSection = document.getElementById('remove-members-section');
    const removeList = document.getElementById('members-to-remove-list');

    if (membersToRemove && membersToRemove.length > 0) {
        removeSection.style.display = 'block';
        const grid = document.createElement('div');
        grid.className = 'members-to-remove-grid';

        for (const member of membersToRemove) {
            const item = document.createElement('div');
            item.className = 'remove-member-item';

            const checkbox = document.createElement('input');
            checkbox.type = 'checkbox';
            checkbox.className = 'remove-checkbox';
            checkbox.dataset.memberId = member.id;
            checkbox.addEventListener('change', e => {
                const memberId = parseInt(e.target.dataset.memberId);
                e.target.checked ? selectedRemoveMembers.add(memberId) : selectedRemoveMembers.delete(memberId);
            });

            const memberInfo = document.createElement('div');
            memberInfo.className = 'remove-member-info';

            const nameSpan = document.createElement('span');
            nameSpan.className = 'remove-member-name';
            nameSpan.textContent = member.name;

            const rankSpan = document.createElement('span');
            rankSpan.className = `member-rank rank-${member.rank}`;
            rankSpan.textContent = member.rank;

            memberInfo.append(nameSpan, rankSpan);
            item.append(checkbox, memberInfo);
            grid.appendChild(item);
        }

        removeList.replaceChildren(grid);
    } else {
        removeSection.style.display = 'none';
    }
}

function displayImportError(message) {
    const resultDiv = document.getElementById('import-result');
    resultDiv.style.display = 'block';
    resultDiv.className = 'import-result error';
    const strong = document.createElement('strong');
    strong.textContent = '✗ Import failed:';
    resultDiv.replaceChildren(strong, document.createTextNode(' ' + message));
}

// ── Alias Management ──────────────────────────────────────────────
let currentAliasMemberId = null;
let loadedAliases = [];
let activeAliasFilter = 'all';

const ALIAS_EMPTY_MESSAGES = {
    all:      'No nicknames set for this commander.',
    global:   'No global aliases.',
    personal: 'No personal aliases.',
    ocr:      'No OCR aliases. These are created automatically during imports — prune incorrect ones here.',
};

function setAliasFilter(filter) {
    activeAliasFilter = filter;
    document.querySelectorAll('[data-alias-filter]').forEach(btn => {
        btn.classList.toggle('active-filter', btn.dataset.aliasFilter === filter);
    });
    renderAliases();
}

function renderAliases() {
    const list = document.getElementById('aliases-list');
    const filtered = activeAliasFilter === 'all'
        ? loadedAliases
        : loadedAliases.filter(a => a.category === activeAliasFilter);

    if (filtered.length === 0) {
        const p = document.createElement('p');
        p.style.cssText = 'text-align:center;color:var(--color-text-muted);margin-top:12px;';
        p.textContent = ALIAS_EMPTY_MESSAGES[activeAliasFilter] || ALIAS_EMPTY_MESSAGES.all;
        list.replaceChildren(p);
        return;
    }

    const rows = filtered.map(a => {
        const row = document.createElement('div');
        row.style.cssText = 'display:flex;justify-content:space-between;align-items:center;padding:10px;border-bottom:1px solid var(--color-border);';

        const left = document.createElement('div');

        const badgeClass = {
            global:   'badge-alias-global',
            personal: 'badge-alias-personal',
            ocr:      'badge-alias-ocr',
        };
        if (badgeClass[a.category]) {
            const badge = document.createElement('span');
            badge.className = badgeClass[a.category];
            badge.style.cssText = 'padding:2px 6px;border-radius:4px;font-size:0.8em;margin-right:8px;';
            badge.textContent = a.category.charAt(0).toUpperCase() + a.category.slice(1);
            left.appendChild(badge);
        }

        const strong = document.createElement('strong');
        strong.textContent = a.alias;
        left.appendChild(strong);
        row.appendChild(left);

        const canDelete = a.is_mine || window.isAdmin || ((a.category === 'global' || a.category === 'ocr') && canManageRanks);
        if (canDelete) {
            const deleteBtn = document.createElement('button');
            deleteBtn.style.cssText = 'background:none;border:none;color:var(--color-danger);cursor:pointer;';
            deleteBtn.title = 'Remove Nickname';
            deleteBtn.textContent = '✖';
            deleteBtn.addEventListener('click', () => deleteAlias(a.id, row, deleteBtn));
            row.appendChild(deleteBtn);
        }

        return row;
    });

    list.replaceChildren(...rows);
}

async function openAliasModal(memberId, memberName) {
    currentAliasMemberId = memberId;
    loadedAliases = [];
    activeAliasFilter = 'all';
    document.getElementById('alias-modal-title').textContent = `Nicknames for ${memberName}`;

    document.querySelectorAll('[data-alias-filter]').forEach(btn => {
        btn.classList.toggle('active-filter', btn.dataset.aliasFilter === 'all');
    });

    const globalWrapper = document.getElementById('global-alias-checkbox-wrapper');
    if (globalWrapper) {
        globalWrapper.style.display = (isAdmin || canManageRanks) ? 'block' : 'none';
    }

    const aliasModal = document.getElementById('alias-modal');
    aliasModal.style.display = 'flex';
    trapFocus(aliasModal);
    await loadAliases();
}

async function loadAliases() {
    const list = document.getElementById('aliases-list');

    const loadingP = document.createElement('p');
    loadingP.style.cssText = 'text-align:center;color:var(--color-text-muted);';
    loadingP.textContent = 'Loading...';
    list.replaceChildren(loadingP);

    try {
        const res = await fetch(`${API_URL}/${currentAliasMemberId}/aliases`);
        loadedAliases = await res.json();
        if (!Array.isArray(loadedAliases)) loadedAliases = [];
        renderAliases();
    } catch (e) {
        const p = document.createElement('p');
        p.style.cssText = 'color:var(--color-danger);text-align:center;';
        p.textContent = 'Error loading aliases.';
        list.replaceChildren(p);
    }
}

document.getElementById('add-alias-form')?.addEventListener('submit', async e => {
    e.preventDefault();
    const input = document.getElementById('new-alias-input');
    const isGlobal = document.getElementById('new-alias-global')?.checked || false;

    try {
        const res = await fetch(`${API_URL}/${currentAliasMemberId}/aliases`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alias: input.value.trim(), is_global: isGlobal }),
        });

        if (!res.ok) throw new Error(await res.text());

        input.value = '';
        if (document.getElementById('new-alias-global')) {
            document.getElementById('new-alias-global').checked = false;
        }
        await loadAliases();
        loadMembers();
    } catch (err) {
        const statusEl = document.getElementById('alias-add-status');
        if (statusEl) {
            statusEl.textContent = 'Failed to add nickname.';
            clearTimeout(statusEl._timer);
            statusEl._timer = setTimeout(() => { statusEl.textContent = ''; }, 4000);
        }
    }
});

window.deleteAlias = async function (aliasId, rowEl, deleteBtn) {
    if (!await showConfirm('Remove this alias?', 'Remove')) return;
    try {
        const res = await fetch(`/api/aliases/${aliasId}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        await loadAliases();
        loadMembers();
    } catch (err) {
        showToast('Failed to remove alias.', 'error');
    }
};

document.querySelectorAll('[data-alias-filter]').forEach(btn => {
    btn.addEventListener('click', () => setAliasFilter(btn.dataset.aliasFilter));
});

document.getElementById('close-alias-modal')?.addEventListener('click', () => {
    const aliasModal = document.getElementById('alias-modal');
    releaseFocus(aliasModal);
    aliasModal.style.display = 'none';
});
