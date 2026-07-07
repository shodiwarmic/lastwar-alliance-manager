// static/external-alliances.js — External Alliances registry.
// The registry is populated by allies, VS League opponent lookups, and prospect sources.
// Managers (manage_allies or manage_vs_points) can add/edit/delete and bulk-refresh from LastRank.
'use strict';

(function () {
    const cfg = document.getElementById('page-config');
    const CAN_MANAGE = cfg && cfg.dataset.canManage === 'true';
    let all = [];

    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        if (props) Object.entries(props).forEach(([k, v]) => {
            if (v == null) return;
            if (k === 'className') node.className = v;
            else if (k === 'text') node.textContent = v;
            else if (k === 'onclick') node.addEventListener('click', v);
            else node.setAttribute(k, v);
        });
        children.flat().forEach(c => { if (c != null) node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c); });
        return node;
    }
    const fmtBig = n => {
        if (n == null) return '—';
        n = Number(n);
        if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B';
        if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
        if (n >= 1e3) return (n / 1e3).toFixed(0) + 'K';
        return '' + n;
    };
    async function api(method, url, body) {
        const opt = { method, headers: {} };
        if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
        const res = await fetch(url, opt);
        const text = (await res.text()).trim();
        if (!res.ok) throw new Error(text || (res.status + ' error'));
        return text ? JSON.parse(text) : null;
    }
    const numOrNull = v => { const s = String(v).trim(); return s === '' ? null : parseInt(s, 10); };
    const strOrNull = v => { const s = String(v).trim(); return s === '' ? null : s; };

    const tbody = document.getElementById('ext-tbody');
    const search = document.getElementById('ext-search');
    const countEl = document.getElementById('ext-count');
    const actions = document.getElementById('ext-actions');
    const progress = document.getElementById('ext-progress');

    function relBadges(a) {
        const wrap = el('span', { className: 'ext-rel' });
        if (a.is_ally) wrap.appendChild(el('span', { className: 'ext-badge ally', text: 'Ally' }));
        if (a.is_opponent) {
            const decided = a.vs_wins + a.vs_losses + a.vs_ties;
            const rec = decided ? ('VS ' + a.vs_wins + '–' + a.vs_losses + (a.vs_ties ? '–' + a.vs_ties : '')) : 'VS pending';
            wrap.appendChild(el('span', { className: 'ext-badge opponent', text: rec }));
        }
        if (a.prospect_count > 0) wrap.appendChild(el('span', { className: 'ext-badge prospect', text: a.prospect_count + ' prospect' + (a.prospect_count === 1 ? '' : 's') }));
        if (!wrap.childNodes.length) wrap.appendChild(el('span', { className: 'ext-badge none', text: '—' }));
        return wrap;
    }

    function render(list) {
        tbody.replaceChildren();
        if (!list.length) {
            tbody.appendChild(el('tr', {}, el('td', { colspan: CAN_MANAGE ? '9' : '8', style: 'text-align:center;padding:20px;color:var(--color-text-muted);', text: 'No alliances yet.' })));
            countEl.textContent = '';
            return;
        }
        countEl.textContent = list.length + ' of ' + all.length;
        list.forEach(a => {
            const tagCell = el('td', {});
            tagCell.appendChild(el('span', { className: 'ext-tag', text: a.tag ? '[' + a.tag + ']' : '—' }));
            if (a.lastrank_id) tagCell.appendChild(el('a', { className: 'ext-lr', href: 'https://lastrank.fun/a/' + a.lastrank_id, target: '_blank', rel: 'noopener noreferrer', title: 'View on LastRank' }, '↗'));
            const cells = [
                tagCell,
                el('td', { text: a.name || '—' }),
                el('td', { className: 'ext-num', text: a.server != null ? a.server : '—' }),
                el('td', { className: 'ext-num', text: fmtBig(a.power) }),
                el('td', { className: 'ext-num', text: fmtBig(a.kills) }),
                el('td', { className: 'ext-num', text: a.member_count != null ? a.member_count : '—' }),
                el('td', {}, relBadges(a)),
                el('td', { className: 'ext-num', text: (a.updated_at || '').slice(0, 10) || '—' }),
            ];
            if (CAN_MANAGE) {
                cells.push(el('td', { style: 'white-space:nowrap;' },
                    el('button', { className: 'btn btn-secondary btn-sm', onclick: () => openModal(a) }, 'Edit'),
                    el('button', { className: 'btn btn-danger btn-sm', style: 'margin-left:6px;', onclick: () => del(a) }, 'Del')));
            }
            tbody.appendChild(el('tr', {}, cells));
        });
    }

    function applyFilter() {
        const q = search.value.trim().toLowerCase();
        render(q ? all.filter(a => (a.tag || '').toLowerCase().includes(q) || (a.name || '').toLowerCase().includes(q)) : all);
    }

    async function load() {
        try {
            all = await api('GET', '/api/external-alliances') || [];
            applyFilter();
        } catch (e) {
            tbody.replaceChildren(el('tr', {}, el('td', { colspan: CAN_MANAGE ? '9' : '8', style: 'text-align:center;padding:20px;color:var(--color-danger);', text: 'Could not load: ' + e.message })));
        }
    }

    async function del(a) {
        if (!await showConfirm('Remove [' + (a.tag || a.name || '?') + '] from the registry?', 'Delete')) return;
        try { await api('DELETE', '/api/external-alliances/' + a.id); showToast('Removed.'); await load(); }
        catch (e) { showToast(e.message, 'error'); }
    }

    // ---- add/edit modal ----
    function field(label, input) { return el('div', { className: 'vsl-field' }, el('label', { style: 'display:block;font-size:.78rem;color:var(--color-text-mid);font-weight:600;margin-bottom:3px;', text: label }), input); }
    function inp(type, value, ph) { return el('input', { className: 'form-input', type: type || 'text', value: value != null ? value : '', placeholder: ph || '' }); }

    function openModal(a) {
        a = a || {};
        const editing = a.id != null;
        const tag = inp('text', a.tag, 'cROw');
        const name = inp('text', a.name, 'Black Crow Legion');
        const server = inp('number', a.server, 'server #');
        const power = inp('number', a.power, 'power');
        const kills = inp('number', a.kills, 'kills');
        const members = inp('number', a.member_count, 'members');
        let lastrankId = a.lastrank_id || null;
        const lr = inp('text', a.lastrank_id, 'paste lastrank.fun/a/… link');
        lr.style.flex = '1'; lr.style.minWidth = '150px';
        const note = el('span', { className: 'vsl-help' });

        const searchBtn = el('a', { className: 'btn btn-secondary btn-sm', target: '_blank', rel: 'noopener noreferrer' }, 'Search ↗');
        const updateSearch = () => {
            const parts = [];
            if (server.value.trim()) parts.push('#' + server.value.trim());
            if (tag.value.trim()) parts.push('[' + tag.value.trim() + ']');
            if (name.value.trim()) parts.push(name.value.trim());
            const q = parts.join(' ');
            searchBtn.href = 'https://lastrank.fun/search' + (q ? '?q=' + encodeURIComponent(q) : '');
        };
        [tag, name, server].forEach(f => f.addEventListener('input', updateSearch));
        updateSearch();
        const lookupBtn = el('button', { className: 'btn btn-secondary btn-sm', type: 'button' }, 'Look up');
        lookupBtn.addEventListener('click', async () => {
            note.textContent = 'Looking up…';
            try {
                const snap = await api('POST', '/api/external-alliances/lookup', { url: lr.value });
                lastrankId = snap.alliance_id;
                if (snap.tag) tag.value = snap.tag;
                if (snap.name) name.value = snap.name;
                if (snap.server_id) server.value = snap.server_id;
                power.value = snap.power; kills.value = snap.kills; members.value = snap.member_count;
                note.textContent = 'Power ' + fmtBig(snap.power) + ' · kills ' + fmtBig(snap.kills) + ' · ' + snap.member_count + '/100';
                updateSearch();
            } catch (e) { note.textContent = e.message; }
        });

        modal(editing ? 'Edit alliance' : 'Add alliance', [
            el('div', { className: 'vsl-form-grid' }, field('Tag', tag), field('Name', name), field('Server', server)),
            el('div', { className: 'vsl-form-grid' }, field('Power', power), field('Kills', kills), field('Members', members)),
            field('LastRank link', el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap;' }, searchBtn, lr, lookupBtn)), note,
        ], async () => {
            if (!tag.value.trim() && !name.value.trim()) throw new Error('Tag or name is required');
            const payload = {
                tag: strOrNull(tag.value), name: strOrNull(name.value), server: numOrNull(server.value),
                power: numOrNull(power.value), kills: numOrNull(kills.value), member_count: numOrNull(members.value),
                lastrank_id: strOrNull(lr.value) || (lastrankId || null),
            };
            if (editing) await api('PUT', '/api/external-alliances/' + a.id, payload);
            else await api('POST', '/api/external-alliances', payload);
            await load();
        }, editing ? 'Save' : 'Add');
    }

    function modal(title, bodyNodes, onSave, saveLabel) {
        const overlay = el('div', { className: 'modal' });
        const content = el('div', { className: 'modal-content modal-md' });
        content.appendChild(el('h2', { text: title }));
        bodyNodes.forEach(n => content.appendChild(n));
        const status = el('p', { className: 'status-msg' });
        const saveBtn = el('button', { className: 'btn btn-primary', text: saveLabel || 'Save' });
        const cancel = el('button', { className: 'btn btn-secondary', text: 'Cancel' });
        function close() { overlay.style.display = ''; overlay.remove(); }
        saveBtn.addEventListener('click', async () => {
            saveBtn.disabled = true; status.textContent = 'Saving…';
            try { await onSave(); close(); showToast('Saved.'); }
            catch (e) { status.textContent = e.message; saveBtn.disabled = false; }
        });
        cancel.addEventListener('click', close);
        overlay.addEventListener('click', e => { if (e.target === overlay) close(); });
        content.appendChild(status);
        content.appendChild(el('div', { className: 'modal-actions' }, saveBtn, cancel));
        overlay.appendChild(content);
        document.body.appendChild(overlay);
        overlay.style.display = 'flex';
    }

    // ---- bulk refresh from LastRank ----
    async function bulkRefresh(btn) {
        const targets = all.filter(a => a.lastrank_id);
        if (!targets.length) { showToast('No cached alliances have a LastRank link yet. Edit one and add its link first.', 'info'); return; }
        if (!await showConfirm('Refresh ' + targets.length + ' alliance' + (targets.length === 1 ? '' : 's') + ' from LastRank? This is paced at 1/sec.', 'Refresh')) return;
        btn.disabled = true;
        progress.hidden = false;
        let done = 0, failed = 0;
        for (const a of targets) {
            progress.textContent = 'Refreshing ' + (done + 1) + ' of ' + targets.length + '… [' + (a.tag || a.name || '?') + ']';
            try { await api('POST', '/api/external-alliances/' + a.id + '/refresh'); }
            catch { failed++; }
            done++;
        }
        progress.textContent = 'Refreshed ' + (done - failed) + ' of ' + targets.length + (failed ? ' (' + failed + ' failed)' : '') + '.';
        btn.disabled = false;
        await load();
        setTimeout(() => { progress.hidden = true; }, 5000);
    }

    // ---- toolbar (manage only) ----
    if (CAN_MANAGE && actions) {
        actions.appendChild(el('button', { className: 'btn btn-primary btn-sm', onclick: () => openModal(null) }, '+ Add'));
        const refreshBtn = el('button', { className: 'btn btn-secondary btn-sm' }, '↻ Refresh from LastRank');
        refreshBtn.addEventListener('click', () => bulkRefresh(refreshBtn));
        actions.appendChild(refreshBtn);
    }

    search.addEventListener('input', applyFilter);
    load();
})();
