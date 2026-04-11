'use strict';

const CAN_MANAGE_MEMBERS = window.CAN_MANAGE_MEMBERS === true;
const CAN_MANAGE_RECRUITING = window.CAN_MANAGE_RECRUITING === true;
const IS_ADMIN = window.IS_ADMIN === true;
const HAS_FORMER_TAB = window.HAS_FORMER_TAB === true;

let allMembers = [];        // for recruiter dropdown and capacity header
let editingProspectId = null;
let reactivatingMemberId = null;
let editingFormerMemberId = null;
let currentFormerAliasMemberId = null;

// Flatpickr instance — initialised in DOMContentLoaded
let prospectContactedFP = null;

// Choices.js instance — initialised in DOMContentLoaded
let recruiterChoices = null;

// ── Tabs ──────────────────────────────────────────────────────────────────────

function setupTabs() {
    const tabs = document.querySelectorAll('.tab-btn');
    tabs.forEach(btn => {
        btn.addEventListener('click', () => {
            tabs.forEach(t => t.classList.remove('active'));
            btn.classList.add('active');
            document.querySelectorAll('.tab-content').forEach(c => c.style.display = 'none');
            const target = document.getElementById('tab-' + btn.dataset.tab);
            if (target) target.style.display = 'block';
        });
    });
    // Show the initially active tab (CSS hides all .tab-content by default)
    const activeBtn = document.querySelector('.tab-btn.active');
    if (activeBtn) {
        const target = document.getElementById('tab-' + activeBtn.dataset.tab);
        if (target) target.style.display = 'block';
    }
}

// ── Capacity Header ───────────────────────────────────────────────────────────

function renderCapacityHeader(settings) {
    const headerEl = document.getElementById('recruiting-header');
    if (!headerEl) return;

    const activeMemberCount = allMembers.filter(m => m.rank !== 'EX').length;
    const maxMembers = settings.alliance_max_members || 100;
    const openSpots = Math.max(0, maxMembers - activeMemberCount);

    const wrapper = document.createElement('div');
    wrapper.className = 'capacity-bar';

    const capacityLine = document.createElement('p');
    capacityLine.className = 'capacity-line';
    const strong = document.createElement('strong');
    strong.textContent = `Alliance Capacity: ${activeMemberCount} / ${maxMembers}`;
    const spotsSpan = document.createElement('span');
    spotsSpan.className = 'open-spots';
    spotsSpan.textContent = `  Open Spots: ${openSpots}`;
    capacityLine.append(strong, spotsSpan);
    wrapper.appendChild(capacityLine);

    if (settings.join_requirements) {
        const reqLabel = document.createElement('p');
        reqLabel.className = 'req-label';
        reqLabel.textContent = 'Join Requirements:';
        const reqText = document.createElement('p');
        reqText.className = 'req-text';
        reqText.textContent = settings.join_requirements;
        wrapper.append(reqLabel, reqText);
    }

    headerEl.replaceChildren(wrapper);
}

// ── Former Members ────────────────────────────────────────────────────────────

async function loadFormerMembers() {
    const container = document.getElementById('former-members-list');
    if (!container) return;

    try {
        const res = await fetch('/api/former-members');
        if (!res.ok) throw new Error('Failed to load former members');
        const members = await res.json();
        renderFormerMembers(members, container);
    } catch (err) {
        console.error(err);
        const p = document.createElement('p');
        p.className = 'empty';
        p.textContent = 'Failed to load former members.';
        container.replaceChildren(p);
    }
}

function renderFormerMembers(members, container) {
    if (!members || members.length === 0) {
        const p = document.createElement('p');
        p.className = 'empty';
        p.textContent = 'No former members.';
        container.replaceChildren(p);
        return;
    }

    const table = document.createElement('table');
    table.className = 'recruiting-table';

    const thead = table.createTHead();
    const hr = thead.insertRow();
    ['Name', 'Last Power', 'Train Runs', 'Last VS Week', 'Reason', ''].forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        hr.appendChild(th);
    });

    const tbody = table.createTBody();
    members.forEach(m => {
        const tr = tbody.insertRow();

        const nameTd = tr.insertCell();
        nameTd.textContent = m.name;

        const powerTd = tr.insertCell();
        powerTd.textContent = m.last_power ? formatPower(m.last_power) : '—';

        const trainTd = tr.insertCell();
        trainTd.textContent = m.train_count || 0;

        const vsTd = tr.insertCell();
        vsTd.textContent = m.last_vs_week || '—';

        const reasonTd = tr.insertCell();
        reasonTd.textContent = m.leave_reason || '—';

        const actionsTd = tr.insertCell();
        actionsTd.className = 'actions-cell';

        const reactivateBtn = document.createElement('button');
        reactivateBtn.className = 'btn btn-primary btn-sm';
        reactivateBtn.textContent = 'Reactivate';
        reactivateBtn.addEventListener('click', () => openReactivateModal(m.id, m.name));
        actionsTd.appendChild(reactivateBtn);

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.style.marginLeft = '6px';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openFormerEditModal(m));
        actionsTd.appendChild(editBtn);

        const aliasBtn = document.createElement('button');
        aliasBtn.className = 'btn btn-secondary btn-sm';
        aliasBtn.style.marginLeft = '6px';
        aliasBtn.textContent = '🏷️';
        aliasBtn.title = 'Manage Nicknames';
        aliasBtn.addEventListener('click', () => openFormerAliasModal(m.id, m.name));
        actionsTd.appendChild(aliasBtn);

        if (IS_ADMIN) {
            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.style.marginLeft = '6px';
            delBtn.textContent = 'Delete';
            delBtn.addEventListener('click', () => permanentlyDeleteMember(m.id, m.name, actionsTd, delBtn));
            actionsTd.appendChild(delBtn);
        }

        tbody.appendChild(tr);
    });

    const wrap = document.createElement('div');
    wrap.className = 'table-scroll';
    wrap.appendChild(table);
    container.replaceChildren(wrap);
}

function openReactivateModal(id, name) {
    reactivatingMemberId = id;
    const modal = document.getElementById('reactivate-modal');
    const nameEl = document.getElementById('reactivate-member-name');
    const statusEl = document.getElementById('reactivate-status');
    if (nameEl) nameEl.textContent = `Reactivating: ${name}`;
    if (statusEl) statusEl.textContent = '';
    if (modal) { modal.style.display = 'flex'; trapFocus(modal); }
}

async function permanentlyDeleteMember(id, name, actionsCell, delBtn) {
    delBtn.style.display = 'none';
    const confirmSpan = document.createElement('span');
    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
    const label = document.createElement('span');
    label.textContent = 'Delete forever?';
    label.style.fontSize = '0.85rem';
    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-danger btn-sm';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', async () => {
        try {
            const res = await fetch(`/api/members/${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error('Failed to delete member');
            await loadFormerMembers();
        } catch (err) {
            console.error(err);
            const msg = document.createElement('span');
            msg.style.cssText = 'color:var(--danger-color);font-size:0.85rem;';
            msg.textContent = 'Delete failed.';
            confirmSpan.replaceWith(msg);
            delBtn.style.display = '';
        }
    });
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); delBtn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    actionsCell.appendChild(confirmSpan);
}

// ── Edit Former Member ────────────────────────────────────────────────────────

function openFormerEditModal(m) {
    editingFormerMemberId = m.id;
    document.getElementById('edit-former-name').value = m.name;
    document.getElementById('edit-former-reason').value = m.leave_reason || '';
    const statusEl = document.getElementById('edit-former-status');
    if (statusEl) statusEl.textContent = '';
    const modal = document.getElementById('edit-former-modal');
    if (modal) { modal.style.display = 'flex'; trapFocus(modal); }
}

async function handleFormerEditSubmit(e) {
    e.preventDefault();
    if (!editingFormerMemberId) return;

    const name = document.getElementById('edit-former-name').value.trim();
    const leave_reason = document.getElementById('edit-former-reason').value.trim();
    const statusEl = document.getElementById('edit-former-status');

    if (!name) return;

    try {
        const res = await fetch(`/api/former-members/${editingFormerMemberId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, leave_reason }),
        });
        if (!res.ok) throw new Error('Failed to save');
        const modal = document.getElementById('edit-former-modal');
        releaseFocus(modal);
        modal.style.display = 'none';
        editingFormerMemberId = null;
        await loadFormerMembers();
    } catch (err) {
        console.error(err);
        if (statusEl) {
            statusEl.textContent = 'Failed to save changes. Please try again.';
            statusEl.style.color = 'var(--color-danger)';
        }
    }
}

// ── Former Member Aliases ─────────────────────────────────────────────────────

async function openFormerAliasModal(memberId, memberName) {
    currentFormerAliasMemberId = memberId;
    const titleEl = document.getElementById('former-alias-modal-title');
    if (titleEl) titleEl.textContent = `Nicknames for ${memberName}`;

    const globalWrapper = document.getElementById('former-global-alias-checkbox-wrapper');
    if (globalWrapper) {
        globalWrapper.style.display = CAN_MANAGE_MEMBERS ? 'block' : 'none';
    }

    const modal = document.getElementById('former-alias-modal');
    if (modal) { modal.style.display = 'flex'; trapFocus(modal); }
    await loadFormerAliases();
}

async function loadFormerAliases() {
    const list = document.getElementById('former-aliases-list');
    if (!list) return;

    const loadingP = document.createElement('p');
    loadingP.style.cssText = 'text-align:center;color:var(--text-muted);';
    loadingP.textContent = 'Loading...';
    list.replaceChildren(loadingP);

    try {
        const res = await fetch(`/api/members/${currentFormerAliasMemberId}/aliases`);
        const aliases = await res.json();

        if (!aliases || aliases.length === 0) {
            const p = document.createElement('p');
            p.style.cssText = 'text-align:center;color:var(--text-muted);';
            p.textContent = 'No nicknames set for this commander.';
            list.replaceChildren(p);
            return;
        }

        const rows = aliases.map(a => {
            const row = document.createElement('div');
            row.style.cssText = 'display:flex;justify-content:space-between;align-items:center;padding:10px;border-bottom:1px solid var(--border-color);';

            const left = document.createElement('div');
            const badgeStyles = {
                global:   'background:#e2e8f0;color:#4a5568;',
                personal: 'background:#bee3f8;color:#2b6cb0;',
                ocr:      'background:#fed7d7;color:#c53030;',
            };
            if (badgeStyles[a.category]) {
                const badge = document.createElement('span');
                badge.style.cssText = badgeStyles[a.category] + 'padding:2px 6px;border-radius:4px;font-size:0.8em;margin-right:8px;';
                badge.textContent = a.category.charAt(0).toUpperCase() + a.category.slice(1);
                left.appendChild(badge);
            }
            const strong = document.createElement('strong');
            strong.textContent = a.alias;
            left.appendChild(strong);
            row.appendChild(left);

            const canDelete = a.is_mine || IS_ADMIN || ((a.category === 'global' || a.category === 'ocr') && CAN_MANAGE_MEMBERS);
            if (canDelete) {
                const deleteBtn = document.createElement('button');
                deleteBtn.style.cssText = 'background:none;border:none;color:#e53e3e;cursor:pointer;';
                deleteBtn.title = 'Remove Nickname';
                deleteBtn.textContent = '✖';
                deleteBtn.addEventListener('click', () => deleteFormerAlias(a.id, row, deleteBtn));
                row.appendChild(deleteBtn);
            }

            return row;
        });

        list.replaceChildren(...rows);
    } catch (e) {
        const p = document.createElement('p');
        p.style.cssText = 'color:#e53e3e;text-align:center;';
        p.textContent = 'Error loading aliases.';
        list.replaceChildren(p);
    }
}

function deleteFormerAlias(aliasId, rowEl, deleteBtn) {
    deleteBtn.style.display = 'none';
    const confirmSpan = document.createElement('span');
    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
    const label = document.createElement('span');
    label.textContent = 'Remove?';
    label.style.fontSize = '0.85rem';
    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-danger btn-sm';
    yesBtn.style.cssText = 'padding:1px 6px;font-size:0.8rem;';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', async () => {
        try {
            const res = await fetch(`/api/aliases/${aliasId}`, { method: 'DELETE' });
            if (!res.ok) throw new Error(await res.text());
            await loadFormerAliases();
        } catch (err) {
            confirmSpan.remove();
            deleteBtn.style.display = '';
        }
    });
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.style.cssText = 'padding:1px 6px;font-size:0.8rem;';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); deleteBtn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    rowEl.appendChild(confirmSpan);
}

// ── Prospects ─────────────────────────────────────────────────────────────────

async function loadProspects() {
    const container = document.getElementById('prospects-list');
    if (!container) return;

    try {
        const res = await fetch('/api/prospects');
        if (!res.ok) throw new Error('Failed to load prospects');
        const prospects = await res.json();
        renderProspects(prospects, container);
    } catch (err) {
        console.error(err);
        const p = document.createElement('p');
        p.className = 'empty';
        p.textContent = 'Failed to load prospects.';
        container.replaceChildren(p);
    }
}

function renderProspects(prospects, container) {
    if (!prospects || prospects.length === 0) {
        const p = document.createElement('p');
        p.className = 'empty';
        p.textContent = CAN_MANAGE_RECRUITING ? 'No prospects yet. Add one to get started.' : 'No prospects.';
        container.replaceChildren(p);
        return;
    }

    const cards = prospects.map(p => buildProspectCard(p));
    container.replaceChildren(...cards);
}

const STATUS_LABELS = {
    interested:            'Interested',
    pending:               'Pending',
    declined:              'Declined',
    qualified_transfer:    'Qualified Transfer',
    unqualified_transfer:  'Unqualified Transfer',
};

function buildProspectCard(p) {
    const card = document.createElement('div');
    card.className = 'prospect-card';

    // Header row
    const header = document.createElement('div');
    header.className = 'prospect-header';

    // Seat color dot (before name)
    if (p.seat_color) {
        const dot = document.createElement('span');
        dot.className = `seat-dot seat-${p.seat_color}`;
        dot.title = p.seat_color.charAt(0).toUpperCase() + p.seat_color.slice(1) + ' seat';
        header.appendChild(dot);
    }

    const nameEl = document.createElement('span');
    nameEl.className = 'prospect-name';
    nameEl.textContent = p.name;
    header.appendChild(nameEl);

    // R4 interest badge
    if (p.interested_in_r4) {
        const r4Badge = document.createElement('span');
        r4Badge.className = 'r4-badge';
        r4Badge.textContent = 'R4 ✓';
        header.appendChild(r4Badge);
    }

    const statusLabel = STATUS_LABELS[p.status] || (p.status.charAt(0).toUpperCase() + p.status.slice(1));
    const badge = document.createElement('span');
    badge.className = `status-badge status-${p.status}`;
    badge.textContent = statusLabel;
    header.appendChild(badge);

    card.appendChild(header);

    // Details
    const details = document.createElement('div');
    details.className = 'prospect-details';

    const detailItems = [];
    if (p.server) detailItems.push(['Server', p.server]);
    if (p.source_alliance) detailItems.push(['Alliance', p.source_alliance]);
    if (p.power) detailItems.push(['Power', formatPower(p.power)]);
    if (p.hero_power != null) detailItems.push(['Hero Power', formatPower(p.hero_power)]);
    if (p.rank_in_alliance) detailItems.push(['Rank', p.rank_in_alliance]);
    if (p.recruiter_name) detailItems.push(['Recruiter', p.recruiter_name]);
    if (p.first_contacted) detailItems.push(['Contacted', p.first_contacted]);

    detailItems.forEach(([label, value]) => {
        const span = document.createElement('span');
        span.className = 'prospect-detail';
        const lbl = document.createElement('strong');
        lbl.textContent = label + ': ';
        span.appendChild(lbl);
        span.appendChild(document.createTextNode(value));
        details.appendChild(span);
    });

    card.appendChild(details);

    if (p.notes) {
        const notesEl = document.createElement('p');
        notesEl.className = 'prospect-notes';
        notesEl.textContent = p.notes;
        card.appendChild(notesEl);
    }

    if (CAN_MANAGE_RECRUITING) {
        const actions = document.createElement('div');
        actions.className = 'prospect-actions';

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openProspectModal(p));
        actions.appendChild(editBtn);

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-danger btn-sm';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', () => deleteProspect(p.id, p.name, actions, delBtn));
        actions.appendChild(delBtn);

        card.appendChild(actions);
    }

    return card;
}

async function deleteProspect(id, name, actionsContainer, delBtn) {
    delBtn.style.display = 'none';
    const confirmSpan = document.createElement('span');
    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
    const label = document.createElement('span');
    label.textContent = 'Sure?';
    label.style.fontSize = '0.85rem';
    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-danger btn-sm';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', async () => {
        try {
            const res = await fetch(`/api/prospects/${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error('Failed to delete prospect');
            await loadProspects();
        } catch (err) {
            console.error(err);
            confirmSpan.remove();
            delBtn.style.display = '';
        }
    });
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); delBtn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    actionsContainer.appendChild(confirmSpan);
}

// ── Prospect Modal ────────────────────────────────────────────────────────────

function openProspectModal(prospect = null) {
    editingProspectId = prospect ? prospect.id : null;
    const modal = document.getElementById('prospect-modal');
    const title = document.getElementById('prospect-modal-title');
    const submitBtn = document.getElementById('prospect-submit-btn');

    document.getElementById('prospect-name').value = prospect ? prospect.name : '';
    document.getElementById('prospect-status').value = prospect ? prospect.status : 'interested';
    document.getElementById('prospect-server').value = prospect ? prospect.server : '';
    document.getElementById('prospect-alliance').value = prospect ? prospect.source_alliance : '';
    document.getElementById('prospect-power').value = (prospect && prospect.power) ? prospect.power : '';
    document.getElementById('prospect-rank').value = prospect ? prospect.rank_in_alliance : '';
    recruiterChoices.setChoiceByValue(prospect && prospect.recruiter_id ? String(prospect.recruiter_id) : '');
    prospectContactedFP.setDate(prospect && prospect.first_contacted ? prospect.first_contacted : null, false);
    document.getElementById('prospect-notes').value = prospect ? prospect.notes : '';
    document.getElementById('prospect-hero-power').value = (prospect && prospect.hero_power != null) ? prospect.hero_power : '';
    document.getElementById('prospect-seat-color').value = prospect ? (prospect.seat_color || '') : '';
    document.getElementById('prospect-interested-r4').checked = prospect ? !!prospect.interested_in_r4 : false;

    if (title) title.textContent = prospect ? 'Edit Prospect' : 'Add Prospect';
    if (submitBtn) submitBtn.textContent = prospect ? 'Save Changes' : 'Add Prospect';
    if (modal) { modal.style.display = 'flex'; trapFocus(modal); }
}

async function handleProspectSubmit(e) {
    e.preventDefault();

    const name = document.getElementById('prospect-name').value.trim();
    const status = document.getElementById('prospect-status').value;
    const server = document.getElementById('prospect-server').value.trim();
    const source_alliance = document.getElementById('prospect-alliance').value.trim();
    const powerVal = document.getElementById('prospect-power').value;
    const power = powerVal !== '' ? parseInt(powerVal, 10) : null;
    const rank_in_alliance = document.getElementById('prospect-rank').value.trim();
    const recruiterVal = document.getElementById('prospect-recruiter').value;
    const recruiter_id = recruiterVal !== '' ? parseInt(recruiterVal, 10) : null;
    const first_contacted = document.getElementById('prospect-contacted').value;
    const notes = document.getElementById('prospect-notes').value.trim();
    const heroPowerVal = document.getElementById('prospect-hero-power').value;
    const hero_power = heroPowerVal !== '' ? parseInt(heroPowerVal, 10) : null;
    const seat_color = document.getElementById('prospect-seat-color').value;
    const interested_in_r4 = document.getElementById('prospect-interested-r4').checked;

    if (!name) return;

    const payload = {
        name, status, server, source_alliance, power, rank_in_alliance,
        recruiter_id, first_contacted, notes,
        hero_power, seat_color, interested_in_r4,
    };

    try {
        let res;
        if (editingProspectId) {
            res = await fetch(`/api/prospects/${editingProspectId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            });
        } else {
            res = await fetch('/api/prospects', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            });
        }
        if (!res.ok) throw new Error('Failed to save prospect');
        const pm = document.getElementById('prospect-modal');
        releaseFocus(pm);
        pm.style.display = 'none';
        await loadProspects();
    } catch (err) {
        console.error(err);
    }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatPower(power) {
    if (!power) return '';
    if (power >= 1000000000) return (power / 1000000000).toFixed(2) + 'B';
    if (power >= 1000000) return (power / 1000000).toFixed(2) + 'M';
    if (power >= 1000) return (power / 1000).toFixed(1) + 'K';
    return power.toString();
}

async function loadMembersForRecruiter() {
    try {
        const res = await fetch('/api/members');
        if (!res.ok) return;
        allMembers = await res.json();
        if (recruiterChoices) {
            const activeMembers = allMembers.filter(m => m.rank !== 'EX' && m.rank !== 'PROSPECT');
            recruiterChoices.setChoices(
                [
                    { value: '', label: 'None', placeholder: true },
                    ...activeMembers.map(m => ({ value: String(m.id), label: `${m.name} (${m.rank})` })),
                ],
                'value', 'label', true
            );
        }
    } catch (err) {
        console.error('Failed to load members for recruiter dropdown:', err);
    }
}

async function loadSettingsForHeader() {
    try {
        const res = await fetch('/api/settings');
        if (!res.ok) return;
        const data = await res.json();
        renderCapacityHeader(data);
    } catch (err) {
        console.error('Failed to load settings for header:', err);
    }
}

// ── Init ──────────────────────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', async () => {
    prospectContactedFP = flatpickr('#prospect-contacted', { dateFormat: 'Y-m-d', allowInput: true });

    recruiterChoices = new Choices('#prospect-recruiter', {
        searchEnabled: true, searchPlaceholderValue: 'Search…',
        itemSelectText: '', shouldSort: false,
    });

    setupTabs();

    // Prospects is default tab — load it first
    loadProspects();
    if (HAS_FORMER_TAB) {
        loadFormerMembers();
    }

    // Members needed for both recruiter dropdown and capacity header
    if (CAN_MANAGE_RECRUITING) {
        await loadMembersForRecruiter();
    } else {
        // Still need allMembers for the capacity header even without manage permission
        try {
            const res = await fetch('/api/members');
            if (res.ok) allMembers = await res.json();
        } catch (_) { /* non-fatal */ }
    }

    // Capacity header requires allMembers to be populated first
    loadSettingsForHeader();

    // Reactivate modal
    const reactivateModal = document.getElementById('reactivate-modal');
    const closeReactivateModal = () => { releaseFocus(reactivateModal); reactivateModal.style.display = 'none'; };
    document.getElementById('close-reactivate-modal')?.addEventListener('click', closeReactivateModal);
    document.getElementById('cancel-reactivate-btn')?.addEventListener('click', closeReactivateModal);
    window.addEventListener('click', e => { if (e.target === reactivateModal) closeReactivateModal(); });

    document.getElementById('confirm-reactivate-btn')?.addEventListener('click', async () => {
        if (!reactivatingMemberId) return;
        const rank = document.getElementById('reactivate-rank').value;
        const statusEl = document.getElementById('reactivate-status');
        try {
            const res = await fetch(`/api/members/${reactivatingMemberId}/reactivate`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ rank }),
            });
            if (!res.ok) throw new Error('Failed to reactivate member');
            closeReactivateModal();
            reactivatingMemberId = null;
            await loadFormerMembers();
        } catch (err) {
            console.error(err);
            if (statusEl) {
                statusEl.textContent = 'Failed to reactivate. Please try again.';
                statusEl.style.color = 'var(--danger-color)';
            }
        }
    });

    // Edit former member modal
    const editFormerModal = document.getElementById('edit-former-modal');
    const closeEditFormerModal = () => { releaseFocus(editFormerModal); editFormerModal.style.display = 'none'; editingFormerMemberId = null; };
    document.getElementById('close-edit-former-modal')?.addEventListener('click', closeEditFormerModal);
    document.getElementById('cancel-edit-former-btn')?.addEventListener('click', closeEditFormerModal);
    window.addEventListener('click', e => { if (e.target === editFormerModal) closeEditFormerModal(); });
    document.getElementById('edit-former-form')?.addEventListener('submit', handleFormerEditSubmit);

    // Former alias modal
    const formerAliasModal = document.getElementById('former-alias-modal');
    const closeFormerAliasModal = () => { releaseFocus(formerAliasModal); formerAliasModal.style.display = 'none'; };
    document.getElementById('close-former-alias-modal')?.addEventListener('click', closeFormerAliasModal);
    window.addEventListener('click', e => { if (e.target === formerAliasModal) closeFormerAliasModal(); });
    document.getElementById('former-add-alias-form')?.addEventListener('submit', async e => {
        e.preventDefault();
        const input = document.getElementById('former-new-alias-input');
        const isGlobal = document.getElementById('former-new-alias-global')?.checked || false;
        try {
            const res = await fetch(`/api/members/${currentFormerAliasMemberId}/aliases`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ alias: input.value.trim(), is_global: isGlobal }),
            });
            if (!res.ok) throw new Error(await res.text());
            input.value = '';
            const globalCheckbox = document.getElementById('former-new-alias-global');
            if (globalCheckbox) globalCheckbox.checked = false;
            await loadFormerAliases();
        } catch (err) {
            const statusEl = document.getElementById('former-alias-add-status');
            if (statusEl) {
                statusEl.textContent = 'Failed to add nickname.';
                clearTimeout(statusEl._timer);
                statusEl._timer = setTimeout(() => { statusEl.textContent = ''; }, 4000);
            }
        }
    });

    // Prospect modal
    const prospectModal = document.getElementById('prospect-modal');
    document.getElementById('add-prospect-btn')?.addEventListener('click', () => openProspectModal(null));
    const closeProspectModal = () => { releaseFocus(prospectModal); prospectModal.style.display = 'none'; };
    document.getElementById('close-prospect-modal')?.addEventListener('click', closeProspectModal);
    document.getElementById('cancel-prospect-btn')?.addEventListener('click', closeProspectModal);
    window.addEventListener('click', e => { if (e.target === prospectModal) closeProspectModal(); });
    document.getElementById('prospect-form')?.addEventListener('submit', handleProspectSubmit);
});
