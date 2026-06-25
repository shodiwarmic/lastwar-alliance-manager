'use strict';

const cfg = document.getElementById('page-config').dataset;
const CAN_MANAGE_MEMBERS = cfg.canManageMembers === 'true';
const CAN_MANAGE_RECRUITING = cfg.canManageRecruiting === 'true';
const IS_ADMIN = cfg.isAdmin === 'true';
const HAS_FORMER_TAB = cfg.hasFormerTab === 'true';

let allMembers = [];        // for recruiter dropdown and capacity header
let editingProspectId = null;
let currentModalType = 'transfer';  // tracks which type context the modal opened in
let convertingProspect = null;
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
        p.className = 'empty-state';
        p.textContent = 'Failed to load former members.';
        container.replaceChildren(p);
    }
}

function renderFormerMembers(members, container) {
    if (!members || members.length === 0) {
        const p = document.createElement('p');
        p.className = 'empty-state';
        p.textContent = 'No former members.';
        container.replaceChildren(p);
        return;
    }

    const table = document.createElement('table');
    table.className = 'data-table recruiting-ex-table';

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
        aliasBtn.setAttribute('aria-label', 'Manage Nicknames');
        aliasBtn.appendChild(svgIcon('tag'));
        aliasBtn.title = 'Manage Nicknames';
        aliasBtn.addEventListener('click', () => openFormerAliasModal(m.id, m.name));
        actionsTd.appendChild(aliasBtn);

        if (IS_ADMIN) {
            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.style.marginLeft = '6px';
            delBtn.textContent = 'Delete';
            delBtn.addEventListener('click', () => permanentlyDeleteMember(m.id, m.name));
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

async function permanentlyDeleteMember(id, name) {
    if (!await showConfirm(`Permanently delete ${name}? This cannot be undone.`, 'Delete Forever')) return;
    try {
        const res = await fetch(`/api/members/${id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error('Failed to delete member');
        await loadFormerMembers();
    } catch (err) {
        console.error(err);
        showToast('Delete failed.', 'error');
    }
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
    loadingP.style.cssText = 'text-align:center;color:var(--color-text-muted);';
    loadingP.textContent = 'Loading...';
    list.replaceChildren(loadingP);

    try {
        const res = await fetch(`/api/members/${currentFormerAliasMemberId}/aliases`);
        const aliases = await res.json();

        if (!aliases || aliases.length === 0) {
            const p = document.createElement('p');
            p.style.cssText = 'text-align:center;color:var(--color-text-muted);';
            p.textContent = 'No nicknames set for this commander.';
            list.replaceChildren(p);
            return;
        }

        const rows = aliases.map(a => {
            const row = document.createElement('div');
            row.className = 'alias-row';

            const left = document.createElement('div');
            const badgeClass = { global: 'alias-badge-global', personal: 'alias-badge-personal', ocr: 'alias-badge-ocr' }[a.category];
            if (badgeClass) {
                const badge = document.createElement('span');
                badge.className = `alias-badge ${badgeClass}`;
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
                deleteBtn.className = 'alias-delete-btn';
                deleteBtn.title = 'Remove Nickname';
                deleteBtn.setAttribute('aria-label', 'Remove Nickname');
                deleteBtn.appendChild(svgIcon('x'));
                deleteBtn.addEventListener('click', () => deleteFormerAlias(a.id));
                row.appendChild(deleteBtn);
            }

            return row;
        });

        list.replaceChildren(...rows);
    } catch (e) {
        const p = document.createElement('p');
        p.style.cssText = 'color:var(--color-danger);text-align:center;';
        p.textContent = 'Error loading aliases.';
        list.replaceChildren(p);
    }
}

async function deleteFormerAlias(aliasId) {
    if (!await showConfirm('Remove this alias?', 'Remove')) return;
    try {
        const res = await fetch(`/api/aliases/${aliasId}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        await loadFormerAliases();
    } catch (err) {
        showToast('Failed to remove alias.', 'error');
    }
}

// ── Prospects ─────────────────────────────────────────────────────────────────

async function loadAllProspects() {
    const transfersContainer = document.getElementById('transfers-list');
    const prospectsContainer = document.getElementById('prospects-list');

    try {
        const res = await fetch('/api/prospects');
        if (!res.ok) throw new Error('Failed to load prospects');
        const all = await res.json();

        const transfers = all.filter(p => p.prospect_type === 'transfer');
        const prospects = all.filter(p => p.prospect_type === 'prospect');

        if (transfersContainer) renderProspects(transfers, transfersContainer, 'transfer');
        if (prospectsContainer) renderProspects(prospects, prospectsContainer, 'prospect');
    } catch (err) {
        console.error(err);
        const makeErrP = () => {
            const p = document.createElement('p');
            p.className = 'empty-state';
            p.textContent = 'Failed to load.';
            return p;
        };
        if (transfersContainer) transfersContainer.replaceChildren(makeErrP());
        if (prospectsContainer) prospectsContainer.replaceChildren(makeErrP());
    }
}

function renderProspects(items, container, typeContext) {
    if (!items || items.length === 0) {
        const p = document.createElement('p');
        p.className = 'empty-state';
        if (typeContext === 'transfer') {
            p.textContent = CAN_MANAGE_RECRUITING ? 'No transfers yet. Add one to get started.' : 'No transfers.';
        } else {
            p.textContent = CAN_MANAGE_RECRUITING ? 'No prospects yet. Add one to get started.' : 'No prospects.';
        }
        container.replaceChildren(p);
        return;
    }

    const cards = items.map(p => buildProspectCard(p, typeContext));
    container.replaceChildren(...cards);
}

const STATUS_LABELS = {
    interested:            'Interested',
    pending:               'Pending',
    declined:              'Declined',
    qualified_transfer:    'Qualified Transfer',
    unqualified_transfer:  'Unqualified Transfer',
};

function buildProspectCard(p, typeContext) {
    const card = document.createElement('div');
    card.className = 'prospect-card';

    // Header row
    const header = document.createElement('div');
    header.className = 'prospect-header';

    const seatTitle = p.seat_color ? p.seat_color.charAt(0).toUpperCase() + p.seat_color.slice(1) + ' seat' : '';

    // Seat color dot — only when there's no avatar to carry the seat color as a
    // border (see below).
    if (p.seat_color && !p.lastrank_photo_url) {
        const dot = document.createElement('span');
        dot.className = `seat-dot seat-${p.seat_color}`;
        dot.title = seatTitle;
        header.appendChild(dot);
    }

    // Name + LastRank link grouped so the header's space-between doesn't pull
    // the icon away from the name (and the shared class keeps the icon tight to
    // the name on mobile rather than inflating to the 44px touch min-size).
    const nameGroup = document.createElement('span');
    nameGroup.className = 'inline-icon-actions';
    nameGroup.style.minWidth = '0';

    // LastRank avatar (hotlinked; falls over to the backup CDN, then hides). When
    // the prospect has a seat color, it rings the avatar as a colored border
    // instead of a separate dot.
    if (p.lastrank_photo_url) {
        const av = buildLastRankAvatar(p.lastrank_photo_url, p.lastrank_photo_failover);
        if (p.seat_color) {
            av.classList.add('seat-edge', `seat-edge-${p.seat_color}`);
            av.title = seatTitle;
        }
        nameGroup.appendChild(av);
    }

    const nameEl = document.createElement('span');
    nameEl.className = 'prospect-name';
    nameEl.textContent = p.name;
    nameGroup.appendChild(nameEl);

    // LastRank profile link (public data) — only when this prospect is linked.
    if (p.lastrank_public_id) {
        const lrLink = document.createElement('a');
        lrLink.href = 'https://lastrank.fun/p/' + p.lastrank_public_id;
        lrLink.target = '_blank';
        lrLink.rel = 'noopener noreferrer';
        lrLink.title = 'View on LastRank';
        lrLink.setAttribute('aria-label', 'View on LastRank');
        lrLink.style.cssText = 'opacity:0.6;display:inline-flex;align-items:center;text-decoration:none;color:inherit;';
        lrLink.appendChild(svgIcon('external-link'));
        nameGroup.appendChild(lrLink);
    }
    header.appendChild(nameGroup);

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
    if (p.hero_power != null) detailItems.push(['Total Hero Power', formatPower(p.hero_power)]);
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

    if (CAN_MANAGE_RECRUITING || CAN_MANAGE_MEMBERS) {
        const actions = document.createElement('div');
        actions.className = 'prospect-actions';

        if (CAN_MANAGE_RECRUITING) {
            const editBtn = document.createElement('button');
            editBtn.className = 'btn btn-secondary btn-sm';
            editBtn.textContent = 'Edit';
            editBtn.addEventListener('click', () => openProspectModal(p));
            actions.appendChild(editBtn);

            const moveBtn = document.createElement('button');
            moveBtn.className = 'btn btn-secondary btn-sm';
            moveBtn.textContent = typeContext === 'transfer' ? 'Move to Prospects' : 'Move to Transfers';
            moveBtn.addEventListener('click', () => moveProspect(p, typeContext === 'transfer' ? 'prospect' : 'transfer'));
            actions.appendChild(moveBtn);

            const lrBtn = document.createElement('button');
            lrBtn.className = 'btn btn-secondary btn-sm';
            lrBtn.textContent = p.lastrank_public_id ? 'Refresh LastRank' : 'Look up on LastRank';
            lrBtn.addEventListener('click', () => openLastRankProspectModal(p));
            actions.appendChild(lrBtn);
        }

        if (CAN_MANAGE_MEMBERS) {
            const convertBtn = document.createElement('button');
            convertBtn.className = 'btn btn-primary btn-sm';
            convertBtn.textContent = 'Add to Roster';
            convertBtn.addEventListener('click', () => openConvertModal(p));
            actions.appendChild(convertBtn);
        }

        if (CAN_MANAGE_RECRUITING) {
            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.textContent = 'Delete';
            delBtn.addEventListener('click', () => deleteProspect(p.id, p.name));
            actions.appendChild(delBtn);
        }

        card.appendChild(actions);
    }

    return card;
}

async function deleteProspect(id, name) {
    if (!await showConfirm(`Delete prospect ${name}?`, 'Delete')) return;
    try {
        const res = await fetch(`/api/prospects/${id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error('Failed to delete prospect');
        await loadAllProspects();
    } catch (err) {
        console.error(err);
        showToast('Delete failed.', 'error');
    }
}

// ── Convert Prospect to Member ────────────────────────────────────────────────

function openConvertModal(prospect) {
    convertingProspect = prospect;
    const nameEl = document.getElementById('convert-prospect-name');
    if (nameEl) nameEl.textContent = `Converting: ${prospect.name}`;
    document.getElementById('convert-rank').value = 'R1';
    document.getElementById('convert-level').value = '';
    document.getElementById('convert-power').value = prospect.power ? prospect.power : '';
    const statusEl = document.getElementById('convert-status');
    if (statusEl) statusEl.textContent = '';
    const modal = document.getElementById('convert-modal');
    if (modal) { modal.style.display = 'flex'; trapFocus(modal); }
}

async function handleConvertSubmit(e) {
    e.preventDefault();
    if (!convertingProspect) return;

    const rank = document.getElementById('convert-rank').value;
    const levelVal = document.getElementById('convert-level').value;
    const level = levelVal !== '' ? parseInt(levelVal, 10) : 0;
    const powerVal = document.getElementById('convert-power').value;
    const power = powerVal !== '' ? parseInt(powerVal, 10) : null;
    const statusEl = document.getElementById('convert-status');

    try {
        const res = await fetch(`/api/prospects/${convertingProspect.id}/convert`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ rank, level, power }),
        });
        if (!res.ok) {
            const msg = await res.text();
            throw new Error(msg || 'Failed to convert prospect');
        }
        const convertedName = convertingProspect.name;
        const modal = document.getElementById('convert-modal');
        releaseFocus(modal);
        modal.style.display = 'none';
        convertingProspect = null;
        showToast(`${convertedName} added to roster.`);
        await loadAllProspects();
    } catch (err) {
        console.error(err);
        if (statusEl) {
            statusEl.textContent = err.message || 'Failed to add member. Please try again.';
            statusEl.style.color = 'var(--color-danger)';
        }
    }
}

// ── Prospect Modal ────────────────────────────────────────────────────────────

function applyStatusOptionsForType(type) {
    const statusSelect = document.getElementById('prospect-status');
    if (!statusSelect) return;
    const transferOnlyOpts = statusSelect.querySelectorAll(
        'option[value="qualified_transfer"], option[value="unqualified_transfer"]'
    );
    transferOnlyOpts.forEach(opt => {
        opt.hidden = type === 'prospect';
        opt.disabled = type === 'prospect';
    });
    if (type === 'prospect' &&
        (statusSelect.value === 'qualified_transfer' || statusSelect.value === 'unqualified_transfer')) {
        statusSelect.value = 'interested';
    }
}

async function moveProspect(p, newType) {
    let newStatus = p.status;
    let newServer = p.server || '';
    let newSeatColor = p.seat_color || '';

    if (newType === 'prospect') {
        newServer = '';
        newSeatColor = '';
        if (newStatus === 'qualified_transfer' || newStatus === 'unqualified_transfer') {
            newStatus = 'interested';
        }
    }

    const payload = {
        name: p.name,
        server: newServer,
        source_alliance: p.source_alliance,
        power: p.power,
        rank_in_alliance: p.rank_in_alliance,
        recruiter_id: p.recruiter_id,
        status: newStatus,
        notes: p.notes,
        hero_power: p.hero_power,
        seat_color: newSeatColor,
        interested_in_r4: p.interested_in_r4,
        first_contacted: p.first_contacted,
        prospect_type: newType,
    };

    try {
        const res = await fetch(`/api/prospects/${p.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        if (!res.ok) throw new Error('Failed to move');
        const label = newType === 'transfer' ? 'Transfers' : 'Prospects';
        showToast(`${p.name} moved to ${label}.`);
        await loadAllProspects();
    } catch (err) {
        console.error(err);
        showToast('Move failed.', 'error');
    }
}

function openProspectModal(prospect = null, defaultType = 'transfer') {
    editingProspectId = prospect ? prospect.id : null;
    currentModalType = prospect ? prospect.prospect_type : defaultType;

    const modal = document.getElementById('prospect-modal');
    const title = document.getElementById('prospect-modal-title');
    const submitBtn = document.getElementById('prospect-submit-btn');

    document.getElementById('prospect-name').value = prospect ? prospect.name : '';
    document.getElementById('prospect-status').value = prospect ? prospect.status : 'interested';
    document.getElementById('prospect-server').value = prospect ? (prospect.server || '') : '';
    document.getElementById('prospect-alliance').value = prospect ? prospect.source_alliance : '';
    document.getElementById('prospect-power').value = (prospect && prospect.power) ? prospect.power : '';
    document.getElementById('prospect-rank').value = prospect ? prospect.rank_in_alliance : '';
    recruiterChoices.setChoiceByValue(prospect && prospect.recruiter_id ? String(prospect.recruiter_id) : '');
    prospectContactedFP.setDate(prospect && prospect.first_contacted ? prospect.first_contacted : null, false);
    document.getElementById('prospect-notes').value = prospect ? prospect.notes : '';
    document.getElementById('prospect-hero-power').value = (prospect && prospect.hero_power != null) ? prospect.hero_power : '';
    document.getElementById('prospect-seat-color').value = prospect ? (prospect.seat_color || '') : '';
    document.getElementById('prospect-interested-r4').checked = prospect ? !!prospect.interested_in_r4 : false;

    const rowServerSeat = document.getElementById('row-server-seat');
    if (rowServerSeat) rowServerSeat.style.display = currentModalType === 'transfer' ? '' : 'none';
    applyStatusOptionsForType(currentModalType);

    const typeLabel = currentModalType === 'transfer' ? 'Transfer' : 'Prospect';
    if (title) title.textContent = prospect ? `Edit ${typeLabel}` : `Add ${typeLabel}`;
    if (submitBtn) submitBtn.textContent = prospect ? 'Save Changes' : `Add ${typeLabel}`;
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
        prospect_type: currentModalType,
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
        await loadAllProspects();
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

    // Load all prospects (populates both Transfers and Prospects tabs)
    loadAllProspects();
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
                statusEl.style.color = 'var(--color-danger)';
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

    // Convert modal
    const convertModal = document.getElementById('convert-modal');
    if (convertModal) {
        const closeConvertModal = () => { releaseFocus(convertModal); convertModal.style.display = 'none'; convertingProspect = null; };
        document.getElementById('close-convert-modal')?.addEventListener('click', closeConvertModal);
        document.getElementById('cancel-convert-btn')?.addEventListener('click', closeConvertModal);
        window.addEventListener('click', e => { if (e.target === convertModal) closeConvertModal(); });
        document.getElementById('convert-form')?.addEventListener('submit', handleConvertSubmit);
    }

    // Prospect modal
    const prospectModal = document.getElementById('prospect-modal');
    document.getElementById('add-transfer-btn')?.addEventListener('click', () => openProspectModal(null, 'transfer'));
    document.getElementById('add-prospect-btn')?.addEventListener('click', () => openProspectModal(null, 'prospect'));
    const closeProspectModal = () => { releaseFocus(prospectModal); prospectModal.style.display = 'none'; };
    document.getElementById('close-prospect-modal')?.addEventListener('click', closeProspectModal);
    document.getElementById('cancel-prospect-btn')?.addEventListener('click', closeProspectModal);
    window.addEventListener('click', e => { if (e.target === prospectModal) closeProspectModal(); });
    document.getElementById('prospect-form')?.addEventListener('submit', handleProspectSubmit);

    // LastRank prospect lookup modal
    const lrModal = document.getElementById('lastrank-prospect-modal');
    const closeLrModal = () => { if (lrModal) { releaseFocus(lrModal); lrModal.style.display = ''; } };
    document.getElementById('lr-prospect-close-btn')?.addEventListener('click', closeLrModal);
    window.addEventListener('click', e => { if (e.target === lrModal) closeLrModal(); });
    document.getElementById('lr-prospect-fetch-btn')?.addEventListener('click', doLastRankProspectFetch);

    // LastRank bulk refresh (one button per tab)
    document.querySelectorAll('.lastrank-bulk-btn').forEach(btn => {
        btn.addEventListener('click', () => doLastRankProspectBulk(btn.dataset.type));
    });
});

// ── LastRank prospect enrichment ──────────────────────────────────────────────
let lrCurrentProspect = null;

function openLastRankProspectModal(p) {
    lrCurrentProspect = p;
    const modal = document.getElementById('lastrank-prospect-modal');
    document.getElementById('lr-prospect-name').textContent = p.name + (p.lastrank_public_id ? ` (saved ID: ${p.lastrank_public_id})` : '');
    document.getElementById('lr-prospect-input').value = '';
    document.getElementById('lr-prospect-result').textContent = '';
    document.getElementById('lr-prospect-search').href = 'https://lastrank.fun/search?q=' + encodeURIComponent(p.name);
    if (modal) { modal.style.display = 'flex'; if (typeof trapFocus === 'function') trapFocus(modal); }
}

async function doLastRankProspectFetch() {
    if (!lrCurrentProspect) return;
    const input = document.getElementById('lr-prospect-input');
    const result = document.getElementById('lr-prospect-result');
    const btn = document.getElementById('lr-prospect-fetch-btn');
    btn.disabled = true;
    result.textContent = 'Fetching…';
    try {
        const res = await fetch('/api/lastrank/prospect', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ prospect_id: lrCurrentProspect.id, lastrank_input: input.value.trim() })
        });
        if (!res.ok) throw new Error((await res.text()) || 'Lookup failed');
        const d = await res.json();
        result.textContent = `${d.lastrank_name}: ${formatPower(d.power)} power`
            + (d.hero_power != null ? `, ${formatPower(d.hero_power)} hero power` : '')
            + (d.alliance_abbr ? ` · [${d.alliance_abbr}]` : '')
            + (d.server_id ? ` · Server ${d.server_id}` : '');
        showToast('Prospect updated from LastRank.');
        await loadAllProspects();
    } catch (e) {
        result.textContent = '';
        showToast(e.message || 'Lookup failed.', 'error');
    } finally {
        btn.disabled = false;
    }
}

async function doLastRankProspectBulk(type) {
    const statusEl = document.querySelector(`.lastrank-bulk-status[data-type="${type}"]`);
    const setS = m => { if (statusEl) statusEl.textContent = m || ''; };
    let prospects;
    try {
        const res = await fetch('/api/prospects');
        prospects = await res.json();
    } catch (e) { showToast('Could not load prospects.', 'error'); return; }

    const pool = (prospects || []).filter(p => p.prospect_type === type && p.lastrank_public_id);
    const noun = type === 'transfer' ? 'transfer' : 'prospect';
    if (pool.length === 0) {
        showToast(`No ${noun}s have a saved LastRank ID yet. Use "Look up on LastRank" on a card first.`, 'info');
        return;
    }
    if (!await showConfirm(`Refresh ${pool.length} ${noun}(s) from LastRank? This runs at ~1/second.`, 'Start')) return;

    document.querySelectorAll('.lastrank-bulk-btn').forEach(b => b.disabled = true);
    let synced = 0, i = 0;
    for (const p of pool) {
        i++;
        setS(`Refreshing ${i} of ${pool.length}…`);
        try {
            const res = await fetch('/api/lastrank/prospect', {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ prospect_id: p.id, bulk: true })
            });
            if (res.ok) synced++;
        } catch (e) { /* skip individual failures */ }
    }
    setS('');
    try {
        await fetch('/api/lastrank/prospect/finish', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ kind: 'prospects', prospects_synced: synced })
        });
    } catch (e) { /* logging only */ }

    document.querySelectorAll('.lastrank-bulk-btn').forEach(b => b.disabled = false);
    showToast(`Refreshed ${synced} ${noun}(s) from LastRank.`);
    await loadAllProspects();
}
