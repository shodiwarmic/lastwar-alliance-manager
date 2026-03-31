'use strict';

const CAN_MANAGE_MEMBERS = window.CAN_MANAGE_MEMBERS === true;
const CAN_MANAGE_RECRUITING = window.CAN_MANAGE_RECRUITING === true;
const IS_ADMIN = window.IS_ADMIN === true;
const HAS_FORMER_TAB = window.HAS_FORMER_TAB === true;

let allMembers = [];        // for recruiter dropdown and capacity header
let editingProspectId = null;
let reactivatingMemberId = null;

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
    if (modal) modal.style.display = 'flex';
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
    document.getElementById('prospect-recruiter').value = (prospect && prospect.recruiter_id) ? prospect.recruiter_id : '';
    document.getElementById('prospect-contacted').value = prospect ? prospect.first_contacted : '';
    document.getElementById('prospect-notes').value = prospect ? prospect.notes : '';
    document.getElementById('prospect-hero-power').value = (prospect && prospect.hero_power != null) ? prospect.hero_power : '';
    document.getElementById('prospect-seat-color').value = prospect ? (prospect.seat_color || '') : '';
    document.getElementById('prospect-interested-r4').checked = prospect ? !!prospect.interested_in_r4 : false;

    if (title) title.textContent = prospect ? 'Edit Prospect' : 'Add Prospect';
    if (submitBtn) submitBtn.textContent = prospect ? 'Save Changes' : 'Add Prospect';
    if (modal) modal.style.display = 'flex';
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
        document.getElementById('prospect-modal').style.display = 'none';
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
        const select = document.getElementById('prospect-recruiter');
        if (select) {
            const activeMembers = allMembers.filter(m => m.rank !== 'EX' && m.rank !== 'PROSPECT');
            activeMembers.forEach(m => {
                const opt = document.createElement('option');
                opt.value = m.id;
                opt.textContent = `${m.name} (${m.rank})`;
                select.appendChild(opt);
            });
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
    document.getElementById('close-reactivate-modal')?.addEventListener('click', () => {
        reactivateModal.style.display = 'none';
    });
    document.getElementById('cancel-reactivate-btn')?.addEventListener('click', () => {
        reactivateModal.style.display = 'none';
    });
    window.addEventListener('click', e => {
        if (e.target === reactivateModal) reactivateModal.style.display = 'none';
    });

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
            reactivateModal.style.display = 'none';
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

    // Prospect modal
    const prospectModal = document.getElementById('prospect-modal');
    document.getElementById('add-prospect-btn')?.addEventListener('click', () => openProspectModal(null));
    document.getElementById('close-prospect-modal')?.addEventListener('click', () => {
        prospectModal.style.display = 'none';
    });
    document.getElementById('cancel-prospect-btn')?.addEventListener('click', () => {
        prospectModal.style.display = 'none';
    });
    window.addEventListener('click', e => {
        if (e.target === prospectModal) prospectModal.style.display = 'none';
    });
    document.getElementById('prospect-form')?.addEventListener('submit', handleProspectSubmit);
});
