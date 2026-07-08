// static/external-alliances.js — External Alliances registry.
// The registry is populated by allies, VS League opponent lookups, and prospect sources.
// Managers (manage_allies or manage_vs_points) can add/edit/delete and bulk-refresh from LastRank.
// Search / filter / sort / extended-gather UX mirrors the Members page.
'use strict';

(function () {
    const cfg = document.getElementById('page-config');
    const CAN_MANAGE = cfg && cfg.dataset.canManage === 'true';
    let all = [];
    let fuseInstance = null;
    let sortField = 'updated';
    let sortDir = 'desc';
    const SORT_DEFAULTS = { tag: 'asc', name: 'asc', server: 'asc', power: 'desc', kills: 'desc', members: 'desc', vs: 'desc', updated: 'desc' };
    const SORT_LABELS = { tag: 'Tag', name: 'Name', server: 'Server', power: 'Power', kills: 'Kills', members: 'Members', vs: 'VS record', updated: 'Updated' };

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
    const vsDecided = a => a.vs_wins + a.vs_losses + a.vs_ties;

    const tbody = document.getElementById('ext-tbody');
    const search = document.getElementById('ext-search');
    const clearSearchBtn = document.getElementById('ext-clear-search');
    const countEl = document.getElementById('ext-count');

    function relBadges(a) {
        const wrap = el('span', { className: 'ext-rel' });
        if (a.ally_status === 'active') wrap.appendChild(el('span', { className: 'ext-badge ally', text: 'Ally' }));
        else if (a.ally_status === 'former') wrap.appendChild(el('span', { className: 'ext-badge former', text: 'Former ally' }));
        if (a.is_opponent) {
            const decided = vsDecided(a);
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
            tbody.appendChild(el('tr', {}, el('td', { colspan: CAN_MANAGE ? '9' : '8', style: 'text-align:center;padding:20px;color:var(--color-text-muted);', text: 'No alliances match.' })));
            countEl.textContent = all.length ? '0 of ' + all.length : '';
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

    // ---- search + filter + sort ----
    function rebuildFuse() {
        if (typeof Fuse === 'undefined') { fuseInstance = null; return; }
        fuseInstance = new Fuse(all, { keys: ['tag', 'name'], threshold: 0.4, minMatchCharLength: 1 });
    }

    function matchesRel(a, rels) {
        if (!rels.length || rels.includes('all')) return true;
        return rels.some(r =>
            (r === 'ally' && a.ally_status === 'active') ||
            (r === 'former' && a.ally_status === 'former') ||
            (r === 'opponent' && a.is_opponent) ||
            (r === 'prospect' && a.prospect_count > 0) ||
            (r === 'unlinked' && a.ally_status === 'never' && !a.is_opponent && !(a.prospect_count > 0)));
    }
    function matchesLR(a, lrs) {
        if (!lrs.length || lrs.includes('all')) return true;
        return lrs.some(v => (v === 'linked' && a.lastrank_id) || (v === 'none' && !a.lastrank_id));
    }

    function sortCmp(a, b) {
        let d = 0;
        switch (sortField) {
            case 'tag': d = (a.tag || '').localeCompare(b.tag || ''); break;
            case 'name': d = (a.name || '').localeCompare(b.name || ''); break;
            case 'server': d = (a.server || 0) - (b.server || 0); break;
            case 'power': d = (a.power || 0) - (b.power || 0); break;
            case 'kills': d = (a.kills || 0) - (b.kills || 0); break;
            case 'members': d = (a.member_count || 0) - (b.member_count || 0); break;
            case 'vs': d = (a.vs_wins - a.vs_losses) - (b.vs_wins - b.vs_losses) || (vsDecided(a) - vsDecided(b)); break;
            case 'updated': d = (a.updated_at || '').localeCompare(b.updated_at || ''); break;
        }
        if (d === 0) d = (a.tag || a.name || '').localeCompare(b.tag || b.name || '');
        return sortDir === 'asc' ? d : -d;
    }

    function applyFilter() {
        const q = search.value.trim();
        let base = (q && fuseInstance) ? fuseInstance.search(q).map(r => r.item) : all.slice();

        const rels = Array.from(document.querySelectorAll('.ext-rel-chip.active')).map(c => c.dataset.rel);
        const lrs = Array.from(document.querySelectorAll('.ext-lr-chip.active')).map(c => c.dataset.lr);
        base = base.filter(a => matchesRel(a, rels) && matchesLR(a, lrs));
        base.sort(sortCmp);

        render(base);
        updateActiveFilterBadge();
        if (clearSearchBtn) clearSearchBtn.style.display = q ? 'flex' : 'none';
    }

    const FILTER_GROUPS = [['.ext-rel-chip', 'rel'], ['.ext-lr-chip', 'lr']];

    function updateActiveFilterBadge() {
        const count = FILTER_GROUPS.reduce((n, [sel, attr]) => {
            const active = Array.from(document.querySelectorAll(sel + '.active'));
            return n + (active.length > 0 && !active.some(c => c.dataset[attr] === 'all') ? 1 : 0);
        }, 0);
        const badge = document.getElementById('ext-filter-count');
        if (badge) { badge.textContent = String(count); badge.hidden = count === 0; }
        const clearBtn = document.getElementById('ext-clear-filters');
        if (clearBtn) clearBtn.disabled = count === 0;
    }

    function clearAllFilters() {
        FILTER_GROUPS.forEach(([sel, attr]) => {
            document.querySelectorAll(sel).forEach(c => c.classList.toggle('active', c.dataset[attr] === 'all'));
        });
        applyFilter();
    }

    function updateSortChips() {
        document.querySelectorAll('.ext-sort-chip').forEach(btn => {
            const f = btn.dataset.sort;
            const active = f === sortField;
            btn.classList.toggle('active', active);
            btn.textContent = SORT_LABELS[f] + (active ? (sortDir === 'asc' ? ' ↑' : ' ↓') : '');
        });
    }

    // Multi-select chip group (mirrors setupChipGroup in members.js): clicking "All"
    // resets the group; clicking another toggles it, and an empty group snaps back to "All".
    function setupChipGroup(sel, attr) {
        const chips = document.querySelectorAll(sel);
        chips.forEach(chip => chip.addEventListener('click', () => {
            const val = chip.dataset[attr];
            if (val === 'all') {
                chips.forEach(c => c.classList.remove('active'));
                chip.classList.add('active');
            } else {
                document.querySelector(sel + '[data-' + attr + '="all"]').classList.remove('active');
                chip.classList.toggle('active');
                if (!document.querySelectorAll(sel + '.active').length) {
                    document.querySelector(sel + '[data-' + attr + '="all"]').classList.add('active');
                }
            }
            applyFilter();
        }));
    }

    function setupControls() {
        if (search) search.addEventListener('input', applyFilter);
        if (clearSearchBtn) clearSearchBtn.addEventListener('click', () => { search.value = ''; applyFilter(); search.focus(); });

        const toggle = document.getElementById('ext-toggle-filters');
        const panel = document.getElementById('ext-filter-collapse');
        if (toggle && panel) {
            toggle.addEventListener('click', () => {
                const open = !panel.classList.contains('open');
                panel.classList.toggle('open', open);
                toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
            });
        }
        const clearBtn = document.getElementById('ext-clear-filters');
        if (clearBtn) clearBtn.addEventListener('click', clearAllFilters);

        FILTER_GROUPS.forEach(([sel, attr]) => setupChipGroup(sel, attr));

        document.querySelectorAll('.ext-sort-chip').forEach(btn => btn.addEventListener('click', () => {
            const f = btn.dataset.sort;
            if (sortField === f) sortDir = sortDir === 'asc' ? 'desc' : 'asc';
            else { sortField = f; sortDir = SORT_DEFAULTS[f] || 'asc'; }
            updateSortChips();
            applyFilter();
        }));
    }

    async function load() {
        try {
            all = await api('GET', '/api/external-alliances') || [];
            rebuildFuse();
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
        const modalApi = {};
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
        const results = el('div', { className: 'ext-find-results', hidden: 'hidden' });

        // Prefill the form from a chosen LastRank snapshot (by-id lookup or search-then-lookup).
        function applySnapshot(snap) {
            lastrankId = snap.alliance_id;
            if (snap.tag) tag.value = snap.tag;
            if (snap.name) name.value = snap.name;
            if (snap.server_id) server.value = snap.server_id;
            power.value = snap.power; kills.value = snap.kills; members.value = snap.member_count;
            if (snap.alliance_id) lr.value = snap.alliance_id;
            note.textContent = 'Power ' + fmtBig(snap.power) + ' · kills ' + fmtBig(snap.kills) + ' · ' + snap.member_count + '/100';
        }

        // Paste-a-URL/id fallback lookup.
        const lookupBtn = el('button', { className: 'btn btn-secondary btn-sm', type: 'button' }, 'Look up');
        lookupBtn.addEventListener('click', async () => {
            note.textContent = 'Looking up…';
            try { applySnapshot(await api('POST', '/api/external-alliances/lookup', { url: lr.value })); }
            catch (e) { note.textContent = e.message; }
        });

        // --- Find: local registry typeahead (instant) + manual LastRank search (on click) ---
        const localMatches = q => (q && fuseInstance)
            ? fuseInstance.search(q).map(r => r.item).filter(x => x.id !== a.id).slice(0, 6) : [];

        function renderResults(localList, lrList, msg) {
            results.replaceChildren();
            if (localList && localList.length) {
                results.appendChild(el('div', { className: 'ext-find-head', text: 'Already in your registry' }));
                localList.forEach(x => {
                    const badge = x.ally_status === 'active' ? 'Ally' : x.ally_status === 'former' ? 'Former ally'
                        : x.is_opponent ? 'VS opponent' : x.prospect_count > 0 ? 'Prospect source' : '';
                    const meta = [x.server != null ? 'S' + x.server : null, badge || null].filter(Boolean).join(' · ');
                    results.appendChild(el('button', { className: 'ext-find-item', type: 'button', onclick: () => { if (modalApi.close) modalApi.close(); openModal(x); } },
                        el('span', { className: 'ext-find-name', text: (x.tag ? '[' + x.tag + '] ' : '') + (x.name || '') }),
                        meta ? el('span', { className: 'ext-find-meta', text: meta }) : null));
                });
            }
            if (msg) results.appendChild(el('div', { className: 'ext-find-msg', text: msg }));
            if (lrList && lrList.length) {
                results.appendChild(el('div', { className: 'ext-find-head', text: 'From LastRank' }));
                lrList.forEach(r => {
                    const meta = [r.server != null ? 'S' + r.server : null, r.power != null ? fmtBig(r.power) + ' pw' : null,
                        r.kills != null ? fmtBig(r.kills) + ' k' : null].filter(Boolean).join(' · ');
                    results.appendChild(el('button', { className: 'ext-find-item', type: 'button', onclick: async () => {
                        if (r.tag) tag.value = r.tag;
                        if (r.name) name.value = r.name;
                        if (r.server != null) server.value = r.server;
                        if (r.power != null) power.value = r.power;
                        if (r.kills != null) kills.value = r.kills;
                        lastrankId = r.lastrank_id; lr.value = r.lastrank_id;
                        results.hidden = true;
                        note.textContent = 'Confirming…';
                        try { applySnapshot(await api('POST', '/api/external-alliances/lookup', { url: r.lastrank_id })); }
                        catch (e) { note.textContent = 'Selected — power ' + fmtBig(r.power) + ' · kills ' + fmtBig(r.kills); }
                    } },
                        el('span', { className: 'ext-find-name', text: (r.tag ? '[' + r.tag + '] ' : '') + (r.name || r.lastrank_id.slice(0, 8)) }),
                        meta ? el('span', { className: 'ext-find-meta', text: meta }) : null));
                });
            }
            results.hidden = !results.childNodes.length;
        }

        const refreshLocal = () => renderResults(localMatches(tag.value.trim() || name.value.trim()), null, null);
        [tag, name].forEach(f => f.addEventListener('input', refreshLocal));

        const lrSearchBtn = el('button', { className: 'btn btn-secondary btn-sm', type: 'button' }, '🔎 Look up on LastRank');
        lrSearchBtn.addEventListener('click', async () => {
            const q = tag.value.trim() || name.value.trim();
            const srv = server.value.trim();
            if (!q) { note.textContent = 'Type a tag or name first.'; return; }
            if (!srv) { note.textContent = "Enter the alliance's server # — LastRank search matches it strictly."; return; }
            lrSearchBtn.disabled = true;
            note.textContent = '';
            renderResults(localMatches(q).slice(0, 4), null, 'Searching LastRank…');
            try {
                const list = await api('GET', '/api/external-alliances/search?q=' + encodeURIComponent(q) + '&server=' + encodeURIComponent(srv));
                renderResults(localMatches(q).slice(0, 4), list, (list && list.length) ? null : 'No LastRank matches on server ' + srv + '.');
            } catch (e) { renderResults(localMatches(q).slice(0, 4), null, e.message); }
            finally { lrSearchBtn.disabled = false; }
        });

        modalApi.close = modal(editing ? 'Edit alliance' : 'Add alliance', [
            el('div', { className: 'ext-find-bar' }, lrSearchBtn, el('span', { className: 'vsl-help', text: 'Filters your registry as you type; searches LastRank on click.' })),
            results,
            el('div', { className: 'vsl-form-grid' }, field('Tag', tag), field('Name', name), field('Server', server)),
            el('div', { className: 'vsl-form-grid' }, field('Power', power), field('Kills', kills), field('Members', members)),
            field('LastRank link', el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap;' }, lr, lookupBtn)), note,
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
        return close;
    }

    // ---- extended gather: per-alliance LastRank refresh, live progress (mirrors Members) ----
    const gatherSection = document.getElementById('ext-gather-section');
    const gatherStatus = document.getElementById('ext-gather-status');
    const gatherProgress = document.getElementById('ext-gather-progress');
    const setGatherStatus = m => { if (gatherStatus) gatherStatus.textContent = m || ''; };

    async function gatherExtended(btn) {
        // Oldest-updated first so an interrupted run resumes where it left off.
        const pool = all.filter(a => a.lastrank_id)
            .sort((a, b) => (a.updated_at || '').localeCompare(b.updated_at || ''));
        if (!pool.length) { showToast('No alliances have a LastRank link yet. Edit one and add its link first.', 'info'); return; }
        if (!await showConfirm('Re-pull power, kills & members for ' + pool.length + ' alliance' + (pool.length === 1 ? '' : 's') + ' from LastRank? Each is fetched at ~1/second.', 'Start')) return;

        btn.disabled = true;
        gatherProgress.style.display = 'block';
        gatherProgress.replaceChildren();
        const rowEls = new Map();
        pool.forEach(a => {
            const status = el('span', { className: 'ext-prog-status', text: 'queued' });
            const label = a.tag ? '[' + a.tag + ']' + (a.name ? ' ' + a.name : '') : (a.name || '?');
            const row = el('div', { className: 'ext-prog-row' }, el('span', { className: 'ext-prog-name', text: label }), status);
            rowEls.set(a.id, { row, status });
            gatherProgress.appendChild(row);
        });

        let updated = 0, failed = 0, i = 0;
        for (const a of pool) {
            i++;
            setGatherStatus('Gathering ' + i + ' of ' + pool.length + '…');
            const { row, status } = rowEls.get(a.id);
            row.className = 'ext-prog-row active';
            status.textContent = 'fetching…';
            try {
                const snap = await api('POST', '/api/external-alliances/' + a.id + '/refresh');
                updated++;
                row.className = 'ext-prog-row done';
                status.textContent = '✓ ' + fmtBig(snap.power) + ' power · ' + fmtBig(snap.kills) + ' kills · ' + (snap.member_count != null ? snap.member_count : '?') + '/100';
            } catch (e) {
                failed++;
                row.className = 'ext-prog-row err';
                status.textContent = 'error — skipped';
            }
        }
        setGatherStatus('');
        showToast('Refreshed ' + updated + ' of ' + pool.length + (failed ? ' (' + failed + ' failed)' : '') + '.');
        btn.disabled = false;
        await load();
    }

    // ---- action bar (manage only) ----
    if (CAN_MANAGE) {
        const addBtn = document.getElementById('ext-add-btn');
        if (addBtn) addBtn.addEventListener('click', () => openModal(null));

        const trigger = document.getElementById('ext-refresh-trigger-btn');
        if (trigger && gatherSection) {
            trigger.addEventListener('click', () => {
                const showing = gatherSection.style.display !== 'none' && gatherSection.style.display !== '';
                gatherSection.style.display = showing ? 'none' : 'block';
                if (!showing) gatherSection.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
            });
        }
        const gatherBtn = document.getElementById('ext-gather-btn');
        if (gatherBtn) gatherBtn.addEventListener('click', () => gatherExtended(gatherBtn));
    }

    setupControls();
    updateSortChips();
    load();
})();
