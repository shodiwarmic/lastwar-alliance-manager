// alias-audit.js — Alias Audit & Management Page

function escapeHtml(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

let memberChoices = null;
let allMembers = [];
let currentMemberID = null;
let currentAliases = [];
let activeFilter = 'all';

const EMPTY_MESSAGES = {
    all:      'No aliases for this member yet.',
    global:   'No global aliases. These are shared across all officers.',
    personal: 'No personal aliases for this member.',
    ocr:      'No OCR aliases yet. These are created automatically when you map an unrecognized name during an import. Prune incorrect ones here.',
};

const CATEGORY_STYLES = {
    global:   { bg: 'var(--accent-primary, #2980b9)', color: '#fff' },
    personal: { bg: 'var(--accent-secondary, #27ae60)', color: '#fff' },
    ocr:      { bg: 'var(--border-color)', color: 'var(--text-secondary)', italic: true },
};

// ── Helpers ──────────────────────────────────────────────────────────────────

function csrfToken() {
    const el = document.querySelector('input[name="gorilla.csrf.Token"]');
    return el ? el.value : '';
}

function showError(msg) {
    const el = document.getElementById('alias-error');
    el.textContent = msg;
    el.classList.remove('hidden');
}

function clearError() {
    const el = document.getElementById('alias-error');
    el.textContent = '';
    el.classList.add('hidden');
}

// ── Render ────────────────────────────────────────────────────────────────────

function renderTable() {
    clearError();
    const filtered = activeFilter === 'all'
        ? currentAliases
        : currentAliases.filter(a => a.category === activeFilter);

    const tbody = document.getElementById('alias-tbody');
    const empty = document.getElementById('alias-empty-state');
    tbody.replaceChildren();

    if (filtered.length === 0) {
        empty.textContent = EMPTY_MESSAGES[activeFilter] || EMPTY_MESSAGES.all;
        empty.classList.remove('hidden');
        return;
    }

    empty.classList.add('hidden');

    filtered.forEach(alias => {
        const style = CATEGORY_STYLES[alias.category] || CATEGORY_STYLES.ocr;
        const tr = document.createElement('tr');
        tr.style.borderBottom = '1px solid var(--border-color)';

        // Alias text
        const tdAlias = document.createElement('td');
        tdAlias.style.padding = '10px 8px';
        if (style.italic) tdAlias.style.fontStyle = 'italic';
        tdAlias.textContent = alias.alias;

        // Category badge
        const tdCat = document.createElement('td');
        tdCat.style.padding = '10px 8px';
        const badge = document.createElement('span');
        badge.textContent = alias.category;
        badge.style.cssText = `
            display: inline-block; padding: 2px 10px; border-radius: 12px; font-size: 0.8em;
            background: ${style.bg}; color: ${style.color};
        `;
        tdCat.appendChild(badge);

        // Delete button
        const tdAction = document.createElement('td');
        tdAction.style.cssText = 'padding: 10px 8px; text-align: right;';
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'btn btn-danger';
        deleteBtn.style.padding = '3px 10px';
        deleteBtn.textContent = 'Delete';
        deleteBtn.addEventListener('click', () => deleteAlias(alias.id));
        tdAction.appendChild(deleteBtn);

        tr.appendChild(tdAlias);
        tr.appendChild(tdCat);
        tr.appendChild(tdAction);
        tbody.appendChild(tr);
    });
}

function setActiveFilter(filter) {
    activeFilter = filter;
    document.querySelectorAll('[data-filter]').forEach(btn => {
        btn.classList.toggle('active-filter', btn.dataset.filter === filter);
    });
    renderTable();
}

// ── API ───────────────────────────────────────────────────────────────────────

async function loadMembers() {
    const res = await fetch('/api/members');
    if (!res.ok) return;
    allMembers = await res.json();
    if (!Array.isArray(allMembers)) allMembers = [];

    const opts = allMembers
        .sort((a, b) => a.name.localeCompare(b.name))
        .map(m => ({
            value: String(m.id),
            label: `<span class="member-rank rank-${m.rank}">${m.rank}</span> ${escapeHtml(m.name)}`,
        }));

    memberChoices.setChoices(
        [{ value: '', label: '— select member —', placeholder: true }, ...opts],
        'value', 'label', true
    );
}

async function loadAliases(memberID) {
    const res = await fetch(`/api/members/${memberID}/aliases`);
    if (!res.ok) return;
    currentAliases = await res.json();
    if (!Array.isArray(currentAliases)) currentAliases = [];

    const member = allMembers.find(m => m.id === Number(memberID));
    document.getElementById('alias-table-title').textContent =
        member ? `Aliases — ${member.name}` : 'Aliases';

    document.getElementById('alias-table-section').classList.remove('hidden');
    setActiveFilter(activeFilter);
}

async function deleteAlias(aliasID) {
    if (!await showConfirm('Delete this alias?', 'Delete')) return;
    const res = await fetch(`/api/aliases/${aliasID}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken() },
    });
    if (res.status === 403) {
        showError('Insufficient permissions to delete this alias.');
        return;
    }
    if (!res.ok) {
        showError('Failed to delete alias. Please try again.');
        return;
    }
    currentAliases = currentAliases.filter(a => a.id !== aliasID);
    renderTable();
}

async function addAlias(aliasText, isGlobal) {
    const res = await fetch(`/api/members/${currentMemberID}/aliases`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken(),
        },
        body: JSON.stringify({
            alias: aliasText,
            is_global: isGlobal,
            category: isGlobal ? 'global' : 'personal',
        }),
    });
    if (res.status === 403) {
        showError('Insufficient permissions to add a global alias.');
        return false;
    }
    if (!res.ok) {
        showError('Failed to add alias: ' + await res.text());
        return false;
    }
    return true;
}

// ── Modals ────────────────────────────────────────────────────────────────────

function closeAddAliasModal() {
    const m = document.getElementById('add-alias-modal');
    releaseFocus(m);
    m.style.display = 'none';
}

document.getElementById('add-alias-modal').addEventListener('click', e => {
    if (e.target === e.currentTarget) closeAddAliasModal();
});

document.getElementById('add-alias-form').addEventListener('submit', async e => {
    e.preventDefault();
    const text = document.getElementById('new-alias-text').value.trim();
    const isGlobal = document.querySelector('input[name="alias-scope"]:checked').value === 'true';
    const ok = await addAlias(text, isGlobal);
    if (ok) {
        closeAddAliasModal();
        await loadAliases(currentMemberID);
    }
});

// ── Event Listeners ───────────────────────────────────────────────────────────

document.getElementById('member-select').addEventListener('change', e => {
    currentMemberID = e.target.value || null;
    currentAliases = [];
    activeFilter = 'all';
    document.querySelectorAll('[data-filter]').forEach(btn => {
        btn.classList.toggle('active-filter', btn.dataset.filter === 'all');
    });
    if (currentMemberID) {
        loadAliases(currentMemberID);
    } else {
        document.getElementById('alias-table-section').classList.add('hidden');
    }
});

document.querySelectorAll('[data-filter]').forEach(btn => {
    btn.addEventListener('click', () => setActiveFilter(btn.dataset.filter));
});

document.getElementById('btn-add-alias').addEventListener('click', () => {
    document.getElementById('new-alias-text').value = '';
    document.querySelector('input[name="alias-scope"][value="false"]').checked = true;
    const aliasModal = document.getElementById('add-alias-modal');
    aliasModal.style.display = 'flex';
    trapFocus(aliasModal);
});

// ── Init ──────────────────────────────────────────────────────────────────────

memberChoices = new Choices('#member-select', {
    searchEnabled: true, searchPlaceholderValue: 'Search…',
    itemSelectText: '', shouldSort: false, allowHTML: true,
});
loadMembers();
