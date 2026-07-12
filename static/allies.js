// allies.js — Allies Tracker page

const cfg = document.getElementById('page-config').dataset;
const CAN_MANAGE = cfg.canManage === 'true';

let allTypes = [];   // AllyAgreementType[]
let allAllies = [];  // Ally[]
let editingAllyId = null;
let editingTypeId = null;
let includeInactive = false;

// ── Init ─────────────────────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
    // Show initial active tab
    const activeBtn = document.querySelector('.tab-btn.active');
    if (activeBtn) {
        const target = document.getElementById('tab-' + activeBtn.dataset.tab);
        if (target) target.style.display = 'block';
    }

    // Tab switching
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(t => { t.style.display = 'none'; });
            btn.classList.add('active');
            const target = document.getElementById('tab-' + btn.dataset.tab);
            if (target) target.style.display = 'block';

            // Load the NAP the first time it's opened, not on every page load.
            if (btn.dataset.tab === 'nap' && !napLoaded) {
                napLoaded = true;
                loadNap();
            }
        });
    });

    const napRefreshBtn = document.getElementById('nap-refresh-btn');
    if (napRefreshBtn) napRefreshBtn.addEventListener('click', () => refreshNap(napRefreshBtn));

    // Show inactive toggle
    const toggle = document.getElementById('show-inactive-toggle');
    if (toggle) {
        toggle.addEventListener('change', () => {
            includeInactive = toggle.checked;
            loadAllies();
        });
    }

    // Add ally button
    const addAllyBtn = document.getElementById('add-ally-btn');
    if (addAllyBtn) addAllyBtn.addEventListener('click', () => openAllyModal(null));

    // Add type button
    const addTypeBtn = document.getElementById('add-type-btn');
    if (addTypeBtn) addTypeBtn.addEventListener('click', () => openTypeModal(null));

    // Ally modal form
    document.getElementById('ally-form').addEventListener('submit', e => {
        e.preventDefault();
        saveAlly();
    });
    document.getElementById('ally-modal-cancel').addEventListener('click', closeAllyModal);

    // Type modal form
    document.getElementById('type-form').addEventListener('submit', e => {
        e.preventDefault();
        saveType();
    });
    document.getElementById('type-modal-cancel').addEventListener('click', closeTypeModal);

    loadAgreementTypes().then(() => {
        loadAllies();
        if (CAN_MANAGE) renderTypesTab();
    });
});

// ── Data Loading ─────────────────────────────────────────────────────────────

async function loadAgreementTypes() {
    const res = await fetch('/api/ally-agreement-types');
    if (!res.ok) return;
    allTypes = await res.json();
}

async function loadAllies() {
    const url = '/api/allies' + (includeInactive ? '?include_inactive=true' : '');
    const res = await fetch(url);
    if (!res.ok) return;
    allAllies = await res.json();
    renderAllies();
}

// ── Rendering ─────────────────────────────────────────────────────────────────

function renderAllies() {
    const container = document.getElementById('allies-list');
    if (allAllies.length === 0) {
        container.replaceChildren();
        const p = document.createElement('p');
        p.style.color = 'var(--color-text-mid)';
        p.textContent = CAN_MANAGE ? 'No allies yet. Add one to get started.' : 'No allies found.';
        container.appendChild(p);
        return;
    }

    const cards = allAllies.map(ally => renderAllyCard(ally));
    container.replaceChildren(...cards);
}

function renderAllyCard(ally) {
    const card = document.createElement('div');
    card.className = 'ally-card' + (ally.active ? '' : ' inactive');

    // Server badge
    const badge = document.createElement('div');
    badge.className = 'ally-server-badge';
    badge.textContent = ally.server;
    card.appendChild(badge);

    // Body
    const body = document.createElement('div');
    body.className = 'ally-body';

    // Header row: tag, name, inactive badge
    const headerRow = document.createElement('div');
    headerRow.className = 'ally-header-row';

    const tag = document.createElement('span');
    tag.className = 'ally-tag';
    tag.textContent = '[' + ally.tag + ']';

    const name = document.createElement('span');
    name.className = 'ally-name';
    name.textContent = ally.name;

    headerRow.appendChild(tag);
    headerRow.appendChild(name);

    if (!ally.active) {
        const inactiveBadge = document.createElement('span');
        inactiveBadge.className = 'ally-inactive-badge';
        inactiveBadge.textContent = 'Inactive';
        headerRow.appendChild(inactiveBadge);
    }
    body.appendChild(headerRow);

    // Agreement type pills
    if (ally.agreement_type_ids && ally.agreement_type_ids.length > 0) {
        const pills = document.createElement('div');
        pills.className = 'ally-agreements';
        ally.agreement_type_ids.forEach(tid => {
            const t = allTypes.find(x => x.id === tid);
            if (!t) return;
            const pill = document.createElement('span');
            pill.className = 'agreement-pill';
            pill.textContent = t.name;
            pills.appendChild(pill);
        });
        body.appendChild(pills);
    }

    // Meta: contact, notes
    if (ally.contact || ally.notes) {
        const meta = document.createElement('div');
        meta.className = 'ally-meta';
        if (ally.contact) {
            const contactLine = document.createElement('span');
            contactLine.append(svgIcon('phone', 13), document.createTextNode(' ' + ally.contact));
            meta.appendChild(contactLine);
        }
        if (ally.notes) {
            const notesLine = document.createElement('span');
            notesLine.textContent = ally.notes;
            meta.appendChild(notesLine);
        }
        body.appendChild(meta);
    }

    card.appendChild(body);

    // Actions (manage only)
    if (CAN_MANAGE) {
        const actions = document.createElement('div');
        actions.className = 'ally-actions';

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-secondary btn-sm';
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => openAllyModal(ally));

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-danger btn-sm';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', async () => {
            if (!await showConfirm('Delete this ally?', 'Delete')) return;
            doDeleteAlly(ally.id);
        });

        actions.appendChild(editBtn);
        actions.appendChild(delBtn);
        card.appendChild(actions);
    }

    return card;
}

function renderTypesTab() {
    const container = document.getElementById('types-list');
    if (!container) return;

    if (allTypes.length === 0) {
        container.replaceChildren();
        const p = document.createElement('p');
        p.style.color = 'var(--color-text-mid)';
        p.textContent = 'No agreement types yet.';
        container.appendChild(p);
        return;
    }

    const rows = allTypes.map(t => renderTypeRow(t));
    container.replaceChildren(...rows);
}

function renderTypeRow(t) {
    const row = document.createElement('div');
    row.className = 'type-row' + (t.active ? '' : ' inactive');

    const nameEl = document.createElement('span');
    nameEl.className = 'type-name';
    nameEl.textContent = t.name;
    row.appendChild(nameEl);

    if (!t.active) {
        const inactiveLabel = document.createElement('span');
        inactiveLabel.className = 'type-inactive-label';
        inactiveLabel.textContent = 'Inactive';
        row.appendChild(inactiveLabel);
    }

    const actions = document.createElement('div');
    actions.className = 'type-actions';

    const editBtn = document.createElement('button');
    editBtn.className = 'btn btn-secondary btn-sm';
    editBtn.textContent = 'Edit';
    editBtn.addEventListener('click', () => openTypeModal(t));

    const toggleBtn = document.createElement('button');
    toggleBtn.className = 'btn btn-secondary btn-sm';
    toggleBtn.textContent = t.active ? 'Deactivate' : 'Activate';
    toggleBtn.addEventListener('click', () => toggleType(t));

    const delBtn = document.createElement('button');
    delBtn.className = 'btn btn-danger btn-sm';
    delBtn.textContent = 'Delete';
    delBtn.addEventListener('click', async () => {
        if (!await showConfirm('Delete this agreement type?', 'Delete')) return;
        doDeleteType(t.id);
    });

    actions.appendChild(editBtn);
    actions.appendChild(toggleBtn);
    actions.appendChild(delBtn);
    row.appendChild(actions);

    return row;
}

// ── Ally Modal ────────────────────────────────────────────────────────────────

// openAllyModal(null) creates, openAllyModal(ally) edits.
//
// `prefill` seeds the create form without entering edit mode — used by "Add as ally" on the NAP
// tab. Passing a bare {tag, name, server} object as `ally` instead would half-enter edit mode and
// throw, since edit mode dereferences ally.id and ally.agreement_type_ids.
function openAllyModal(ally, prefill) {
    const seed = ally || prefill || null;
    editingAllyId = ally ? ally.id : null;
    document.getElementById('ally-modal-title').textContent = ally ? 'Edit Ally' : 'Add Ally';
    document.getElementById('ally-server').value = seed && seed.server != null ? seed.server : '';
    document.getElementById('ally-tag').value = seed && seed.tag ? seed.tag : '';
    document.getElementById('ally-name').value = seed && seed.name ? seed.name : '';
    document.getElementById('ally-contact').value = ally ? ally.contact : '';
    document.getElementById('ally-notes').value = ally ? ally.notes : '';
    document.getElementById('ally-modal-status').textContent = '';

    // Active checkbox — only show in edit mode
    const activeGroup = document.getElementById('ally-active-group');
    if (ally) {
        activeGroup.style.display = 'block';
        document.getElementById('ally-active').checked = ally.active;
    } else {
        activeGroup.style.display = 'none';
    }

    // Render agreement type checkboxes (active types only, or types already selected)
    const checkboxContainer = document.getElementById('ally-agreement-checkboxes');
    checkboxContainer.replaceChildren();
    const activeTypes = allTypes.filter(t => t.active || (ally && ally.agreement_type_ids.includes(t.id)));
    activeTypes.forEach(t => {
        const label = document.createElement('label');
        label.className = 'agreement-checkbox-label';
        const cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.value = t.id;
        cb.checked = ally ? ally.agreement_type_ids.includes(t.id) : false;
        const nameSpan = document.createElement('span');
        nameSpan.textContent = t.name;
        label.appendChild(cb);
        label.appendChild(nameSpan);
        checkboxContainer.appendChild(label);
    });

    const allyModal = document.getElementById('ally-modal');
    allyModal.style.display = 'flex';
    trapFocus(allyModal);
}

function closeAllyModal() {
    const allyModal = document.getElementById('ally-modal');
    releaseFocus(allyModal);
    allyModal.style.display = '';
}

async function saveAlly() {
    const statusEl = document.getElementById('ally-modal-status');
    statusEl.textContent = '';

    const server = document.getElementById('ally-server').value.trim();
    const tag = document.getElementById('ally-tag').value.trim();
    const name = document.getElementById('ally-name').value.trim();
    const contact = document.getElementById('ally-contact').value.trim();
    const notes = document.getElementById('ally-notes').value.trim();
    const activeEl = document.getElementById('ally-active');
    const active = editingAllyId ? activeEl.checked : true;

    const checkboxes = document.querySelectorAll('#ally-agreement-checkboxes input[type="checkbox"]:checked');
    const agreementTypeIDs = Array.from(checkboxes).map(cb => parseInt(cb.value));

    const body = { server, tag, name, contact, notes, active, agreement_type_ids: agreementTypeIDs };
    const url = editingAllyId ? '/api/allies/' + editingAllyId : '/api/allies';
    const method = editingAllyId ? 'PUT' : 'POST';

    const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });

    if (!res.ok) {
        const text = await res.text();
        statusEl.textContent = text || 'Save failed.';
        return;
    }

    closeAllyModal();
    await loadAgreementTypes();
    await loadAllies();
    await refreshNapIfLoaded();  // an added/edited ally changes the NAP tab's ✓ Ally flags
}

async function doDeleteAlly(id) {
    const res = await fetch('/api/allies/' + id, { method: 'DELETE' });
    if (!res.ok) return;
    await loadAllies();
    await refreshNapIfLoaded();
}

// Re-render the NAP from cache after an ally changes. Cheap and local — it never calls LastRank.
async function refreshNapIfLoaded() {
    if (napLoaded) await loadNap();
}

// ── Type Modal ────────────────────────────────────────────────────────────────

function openTypeModal(t) {
    editingTypeId = t ? t.id : null;
    document.getElementById('type-modal-title').textContent = t ? 'Edit Agreement Type' : 'Add Agreement Type';
    document.getElementById('type-name').value = t ? t.name : '';
    document.getElementById('type-modal-status').textContent = '';
    const typeModal = document.getElementById('type-modal');
    typeModal.style.display = 'flex';
    trapFocus(typeModal);
}

function closeTypeModal() {
    const typeModal = document.getElementById('type-modal');
    releaseFocus(typeModal);
    typeModal.style.display = '';
}

async function saveType() {
    const statusEl = document.getElementById('type-modal-status');
    statusEl.textContent = '';

    const name = document.getElementById('type-name').value.trim();
    if (!name) {
        statusEl.textContent = 'Name is required.';
        return;
    }

    const url = editingTypeId ? '/api/ally-agreement-types/' + editingTypeId : '/api/ally-agreement-types';
    const method = editingTypeId ? 'PUT' : 'POST';

    const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
    });

    if (!res.ok) {
        const text = await res.text();
        statusEl.textContent = text || 'Save failed.';
        return;
    }

    closeTypeModal();
    await loadAgreementTypes();
    renderTypesTab();
}

async function toggleType(t) {
    const res = await fetch('/api/ally-agreement-types/' + t.id, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ active: !t.active }),
    });
    if (!res.ok) return;
    await loadAgreementTypes();
    renderTypesTab();
}

async function doDeleteType(id) {
    const res = await fetch('/api/ally-agreement-types/' + id, { method: 'DELETE' });

    if (res.status === 409) {
        const data = await res.json();
        // Show inline force-delete prompt on the type row
        const typesList = document.getElementById('types-list');
        // Re-render with a notice — simplest approach: show a status in the tab
        const notice = document.createElement('p');
        notice.style.color = 'var(--color-danger)';
        notice.style.marginTop = '8px';
        notice.textContent = data.error + ' Click "Force Delete" to remove it anyway.';
        const forceBtn = document.createElement('button');
        forceBtn.className = 'btn btn-danger btn-sm';
        forceBtn.textContent = 'Force Delete';
        forceBtn.style.marginLeft = '8px';
        forceBtn.addEventListener('click', async () => {
            const r2 = await fetch('/api/ally-agreement-types/' + id + '?force=true', { method: 'DELETE' });
            if (r2.ok) {
                notice.remove();
                await loadAgreementTypes();
                await loadAllies();
                renderTypesTab();
            }
        });
        notice.appendChild(forceBtn);
        typesList.insertBefore(notice, typesList.firstChild);
        return;
    }

    if (!res.ok) return;
    await loadAgreementTypes();
    await loadAllies();
    renderTypesTab();
}

// ── NAP tab ──────────────────────────────────────────────────────────────────
//
// The NAP is the top-N alliances on our server, and we are one of them. Everything here renders
// from cached data served by GET /api/allies/nap — the tab never calls LastRank itself, so it keeps
// working when the volunteer service is down. Refreshing is a deliberate, manage-only action.

const OUR_SERVER_ID = parseInt(cfg.ourServerId, 10) || 0;
let napLoaded = false;

async function loadNap() {
    const container = document.getElementById('nap-list');
    const res = await fetch('/api/allies/nap');
    if (!res.ok) {
        container.replaceChildren(napEmptyState('Could not load the NAP.'));
        return;
    }
    renderNap(await res.json());
}

function renderNap(data) {
    const container = document.getElementById('nap-list');
    const summary = document.getElementById('nap-summary');
    const captured = document.getElementById('nap-captured');
    const refreshBtn = document.getElementById('nap-refresh-btn');

    captured.textContent = '';

    if (!data.server_configured) {
        summary.textContent = '';
        if (refreshBtn) refreshBtn.style.display = 'none';
        container.replaceChildren(napEmptyState(
            'No server number configured. Set your server number in Settings to see the alliances on your server.'
        ));
        return;
    }

    if (refreshBtn) refreshBtn.style.display = '';
    summary.textContent = `Top ${data.nap_size} alliances on server ${data.server}, including us. Manage agreements from the Allies tab.`;

    if (!data.alliances.length) {
        container.replaceChildren(napEmptyState(
            `No alliances cached yet. Refresh from LastRank to load the top alliances on server ${data.server}.`
        ));
        return;
    }

    if (data.captured_at) {
        captured.textContent = `Data captured ${data.captured_at} UTC`;
    }

    const table = document.createElement('table');
    table.className = 'data-table nap-table';

    const thead = document.createElement('thead');
    const hrow = document.createElement('tr');
    ['#', 'Alliance', 'Power', 'Kills', 'Members', 'Status'].forEach(h => {
        const th = document.createElement('th');
        th.textContent = h;
        hrow.appendChild(th);
    });
    thead.appendChild(hrow);
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    let dividerDrawn = false;
    data.alliances.forEach((a, i) => {
        // The NAP line: everything above it is the pact, everything below is context. Having two
        // separate numbers (size and import limit) is pointless if the cut isn't legible.
        const next = data.alliances[i + 1];
        tbody.appendChild(renderNapRow(a));
        if (!dividerDrawn && a.in_nap && (!next || !next.in_nap)) {
            tbody.appendChild(napDividerRow(data.nap_size));
            dividerDrawn = true;
        }
    });
    table.appendChild(tbody);

    const scroll = document.createElement('div');
    scroll.className = 'table-scroll';
    scroll.appendChild(table);
    container.replaceChildren(scroll);
}

function renderNapRow(a) {
    const tr = document.createElement('tr');
    if (a.is_us) tr.classList.add('nap-us');
    if (!a.in_nap) tr.classList.add('nap-below-line');

    const rank = document.createElement('td');
    rank.className = 'nap-num';
    rank.textContent = a.rank == null ? '\u2014' : String(a.rank);
    tr.appendChild(rank);

    // An inline-flex wrapper INSIDE the cell. Setting display on the <td> itself would take it out
    // of the table layout and the columns would stop lining up.
    const alliance = document.createElement('td');
    const wrap = document.createElement('span');
    wrap.className = 'nap-cell';
    if (a.tag) {
        const tag = document.createElement('span');
        tag.className = 'ally-tag';
        tag.textContent = `[${a.tag}]`;
        wrap.appendChild(tag);
    }
    const name = document.createElement('span');
    name.className = 'ally-name';
    name.textContent = a.name || '(unnamed)';
    wrap.appendChild(name);
    alliance.appendChild(wrap);
    tr.appendChild(alliance);

    const power = document.createElement('td');
    power.className = 'nap-num';
    power.textContent = formatCompact(a.power);
    tr.appendChild(power);

    const kills = document.createElement('td');
    kills.className = 'nap-num';
    kills.textContent = formatCompact(a.kills);
    tr.appendChild(kills);

    // Members is not on the ladder endpoint — it comes from the per-alliance detail call the
    // refresh makes. An alliance we have never enriched shows an em dash rather than a fake 0.
    const members = document.createElement('td');
    members.className = 'nap-num';
    members.textContent = a.member_count == null ? '\u2014' : String(a.member_count);
    tr.appendChild(members);

    tr.appendChild(renderNapStatus(a));
    return tr;
}

function renderNapStatus(a) {
    const td = document.createElement('td');
    const wrap = document.createElement('span');
    wrap.className = 'nap-cell';
    td.appendChild(wrap);

    if (a.is_us) {
        const badge = document.createElement('span');
        badge.className = 'nap-badge nap-badge-us';
        badge.textContent = 'Us';
        wrap.appendChild(badge);
        return td;
    }

    if (a.ally_id) {
        const badge = document.createElement('span');
        badge.className = 'nap-badge nap-badge-ally';
        badge.textContent = a.ally_active ? '✓ Ally' : 'Former ally';
        wrap.appendChild(badge);

        (a.agreement_type_ids || []).forEach(id => {
            const type = allTypes.find(t => t.id === id);
            if (!type) return;
            const pill = document.createElement('span');
            pill.className = 'agreement-pill';
            pill.textContent = type.name;
            wrap.appendChild(pill);
        });
        return td;
    }

    if (CAN_MANAGE) {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'btn btn-secondary btn-sm';
        btn.textContent = '+ Add as ally';
        // Reuse the existing create flow, seeded — never a duplicate implementation.
        btn.addEventListener('click', () => openAllyModal(null, {
            tag: a.tag || '',
            name: a.name || '',
            server: OUR_SERVER_ID || '',
        }));
        wrap.appendChild(btn);
    }
    return td;
}

function napDividerRow(napSize) {
    const tr = document.createElement('tr');
    tr.className = 'nap-divider';
    const td = document.createElement('td');
    td.colSpan = 6;
    td.textContent = `NAP line — top ${napSize}`;
    tr.appendChild(td);
    return tr;
}

function napEmptyState(message) {
    const p = document.createElement('p');
    p.className = 'empty-state';
    p.textContent = message;
    return p;
}

function formatCompact(n) {
    if (n == null) return '—';
    if (n >= 1e9) return (n / 1e9).toFixed(1) + 'B';
    if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
    if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K';
    return String(n);
}

async function refreshNap(btn) {
    // Re-entry guard. A disabled button won't dispatch a click in a browser, but if this were ever
    // invoked twice the second call would capture "Refreshing…" as the original label and never
    // restore it — and it would fire a second upstream run against the rate limiter.
    if (btn.disabled) return;

    const original = btn.textContent;
    const status = document.getElementById('nap-refresh-status');
    btn.disabled = true;
    btn.textContent = 'Refreshing…';
    setNapProgress(status, 'Contacting LastRank…', 0, 0);

    try {
        const res = await fetch('/api/allies/nap/refresh', { method: 'POST' });

        // Failures that happen before the work starts (not configured, LastRank unreachable, empty
        // ladder) still come back as a normal error status with a plain-text body.
        if (!res.ok) {
            const msg = (await res.text().catch(() => '')).trim();
            showToast(msg || 'Could not refresh from LastRank.', 'error');
            return;
        }

        // Past that point the server streams NDJSON progress, because enriching member counts costs
        // ~1s per alliance and a silent 15-second wait is indistinguishable from a hang.
        let done = null;
        let failed = null;
        await readNDJSON(res, ev => {
            if (ev.stage === 'ladder') {
                setNapProgress(status, `Reading member counts for ${ev.total} alliances…`, 0, ev.total);
            } else if (ev.stage === 'members') {
                const who = ev.tag ? ` (${ev.tag})` : '';
                setNapProgress(status, `Reading member counts… ${ev.done} of ${ev.total}${who}`, ev.done, ev.total);
            } else if (ev.stage === 'saving') {
                setNapProgress(status, 'Saving…', 1, 1);
            } else if (ev.stage === 'done') {
                done = ev;
            } else if (ev.stage === 'error') {
                failed = ev.message || 'Refresh failed.';
            }
        });

        if (failed) { showToast(failed, 'error'); return; }
        if (!done) { showToast('The refresh was interrupted before it finished.', 'error'); return; }

        // Distinguish "we fetched and nothing had changed" from "we updated things" — reporting an
        // update that didn't happen is worse than saying nothing.
        showToast(done.recorded > 0
            ? `Updated ${done.alliances} alliances (${done.recorded} new datapoints).`
            : 'Already up to date.');
        await loadNap();
    } catch {
        showToast('Could not refresh from LastRank.', 'error');
    } finally {
        btn.disabled = false;
        btn.textContent = original;
        setNapProgress(status, '', 0, 0);
    }
}

// Read a newline-delimited JSON stream, dispatching each complete line as it arrives. Buffers the
// tail because a chunk boundary can land mid-line.
async function readNDJSON(res, onEvent) {
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split('\n');
        buf = lines.pop();                     // keep the partial line for the next chunk
        for (const line of lines) {
            const t = line.trim();
            if (!t) continue;
            try { onEvent(JSON.parse(t)); } catch { /* ignore a malformed line */ }
        }
    }
    const t = buf.trim();
    if (t) { try { onEvent(JSON.parse(t)); } catch { /* ignore */ } }
}

// Progress is real, not a guess: the server reports which alliance it is on out of how many.
function setNapProgress(el, text, done, total) {
    if (!el) return;
    if (!text) { el.replaceChildren(); return; }

    const label = document.createElement('span');
    label.textContent = text;

    const bar = document.createElement('span');
    bar.className = 'nap-progress';
    const fill = document.createElement('span');
    fill.className = 'nap-progress-fill';
    fill.style.width = total > 0 ? Math.round((done / total) * 100) + '%' : '0%';
    bar.appendChild(fill);

    el.replaceChildren(label, bar);
}
