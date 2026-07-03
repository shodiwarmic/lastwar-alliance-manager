// lastrank.js — LastRank.fun sync UI on the Members page.
// Phase 1: one alliance fetch → review modal → commit (stats auto, rank/unmatched
// reviewed). Phase 2: browser-driven per-player loop for troop kills, oldest
// lastrank_synced_at first so an interrupted run resumes cleanly.
// Safe DOM only (no innerHTML); feedback via showToast/showConfirm.

(function () {
    const cfgEl = document.getElementById('page-config');
    const CAN_MANAGE = cfgEl && cfgEl.dataset.canManageMembers === 'true';
    if (!CAN_MANAGE) return; // section + trigger stay hidden for non-managers

    // --- DOM builder (mirrors dashboard.js el() pattern) ---
    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        if (props) {
            Object.entries(props).forEach(([k, v]) => {
                if (k === 'className') node.className = v;
                else if (k === 'textContent') node.textContent = v;
                else if (k === 'style') Object.assign(node.style, v);
                else node.setAttribute(k, v);
            });
        }
        children.forEach(c => {
            if (c == null) return;
            node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
        });
        return node;
    }

    const fmt = n => (n == null ? '—' : Number(n).toLocaleString());

    function relTime(iso) {
        if (!iso) return 'unknown';
        const t = new Date(iso);
        if (isNaN(t)) return iso;
        const mins = Math.round((Date.now() - t.getTime()) / 60000);
        if (mins < 1) return 'just now';
        if (mins < 60) return mins + 'm ago';
        const hrs = Math.round(mins / 60);
        if (hrs < 24) return hrs + 'h ago';
        return Math.round(hrs / 24) + 'd ago';
    }

    // --- elements ---
    const triggerBtn = document.getElementById('lastrank-trigger-btn');
    const section = document.getElementById('lastrank-section');
    const fetchBtn = document.getElementById('lastrank-fetch-btn');
    const extendedBtn = document.getElementById('lastrank-extended-btn');
    const statusEl = document.getElementById('lastrank-status');
    const progressEl = document.getElementById('lastrank-extended-progress');
    const modal = document.getElementById('lastrank-modal');
    const metaEl = document.getElementById('lastrank-alliance-meta');
    const bodyEl = document.getElementById('lastrank-review-body');
    const confirmBtn = document.getElementById('lastrank-confirm-btn');
    const cancelBtn = document.getElementById('lastrank-cancel-btn');

    let previewData = null;

    const setStatus = msg => { if (statusEl) statusEl.textContent = msg || ''; };

    // --- show the section trigger (manager-only) ---
    if (triggerBtn) {
        triggerBtn.style.display = '';
        triggerBtn.addEventListener('click', () => {
            section.style.display = (section.style.display === 'none' || !section.style.display) ? 'block' : 'none';
        });
    }
    if (fetchBtn) fetchBtn.addEventListener('click', doFetch);
    if (extendedBtn) extendedBtn.addEventListener('click', doExtended);
    if (cancelBtn) cancelBtn.addEventListener('click', closeModal);
    if (confirmBtn) confirmBtn.addEventListener('click', doCommit);

    function closeModal() { modal.style.display = ''; }

    // --- Phase 1: fetch + review ---
    async function doFetch() {
        setStatus('Fetching from LastRank…');
        fetchBtn.disabled = true;
        try {
            const res = await fetch('/api/lastrank/preview', {
                method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}'
            });
            if (!res.ok) throw new Error((await res.text()) || 'Fetch failed');
            previewData = await res.json();
            renderReview(previewData);
            modal.style.display = 'flex';
        } catch (e) {
            showToast(e.message || 'Could not fetch from LastRank.', 'error');
        } finally {
            setStatus('');
            fetchBtn.disabled = false;
        }
    }

    function renderReview(data) {
        const a = data.alliance || {};
        const label = (a.abbr ? '[' + a.abbr + '] ' : '') + (a.name || 'Alliance');
        const nameNode = a.alliance_id
            ? el('a', { href: 'https://lastrank.fun/a/' + a.alliance_id, target: '_blank', rel: 'noopener noreferrer', title: 'View alliance on LastRank' }, el('strong', { textContent: label }))
            : el('strong', { textContent: label });
        metaEl.replaceChildren(
            nameNode,
            document.createTextNode(
                ` · Server ${a.server_id || '?'} · ${a.cur_member || '?'}/${a.max_member || '?'} members · LastRank data from ${relTime(a.last_seen_at)}`
            )
        );

        bodyEl.replaceChildren();

        const matched = data.matched || [];
        const withChanges = matched.filter(hasChange);
        const upToDate = matched.length - withChanges.length;

        // Matched changes
        bodyEl.appendChild(el('div', { className: 'lr-group-title', textContent: `Updates (${withChanges.length})` }));
        if (withChanges.length === 0) {
            bodyEl.appendChild(el('p', { className: 'lr-empty', textContent: 'No stat or rank changes to apply.' }));
        }
        withChanges.forEach(m => bodyEl.appendChild(renderMatchedRow(m)));
        if (upToDate > 0) {
            bodyEl.appendChild(el('p', { className: 'lr-summary', textContent: `${upToDate} member(s) already up to date — their LastRank ID is still saved for extended sync.` }));
        }

        // Unmatched
        const unmatched = data.unmatched || [];
        bodyEl.appendChild(el('div', { className: 'lr-group-title', textContent: `Unmatched names (${unmatched.length})` }));
        if (unmatched.length === 0) {
            bodyEl.appendChild(el('p', { className: 'lr-empty', textContent: 'Every LastRank member matched a roster member.' }));
        }
        unmatched.forEach(u => bodyEl.appendChild(renderUnmatchedRow(u, data.all_members || [])));

        // Possibly-departed members (absent from LastRank, or unranked there).
        const candidates = data.archive_candidates || [];
        bodyEl.appendChild(el('div', { className: 'lr-group-title', textContent: `Possibly left the alliance (${candidates.length})` }));
        if (candidates.length === 0) {
            bodyEl.appendChild(el('p', { className: 'lr-empty', textContent: 'Everyone on your roster is active on LastRank.' }));
        } else {
            bodyEl.appendChild(el('p', { className: 'lr-summary', textContent: 'Nothing happens unless you tick Archive. Verify first — a name LastRank couldn’t match (alias drift) can appear here even if the member is still active.' }));
        }
        candidates.forEach(c => {
            const cb = el('input', { type: 'checkbox' }); // default unchecked = no action
            c._archive = cb;
            bodyEl.appendChild(el('div', { className: 'lr-row' },
                el('div', { className: 'lr-row-name', textContent: `${c.name} (${c.rank})` }),
                el('label', { className: 'lr-field' }, cb, el('span', {}, ` Archive — ${c.reason}`))
            ));
        });
    }

    function hasChange(m) {
        return (m.power && m.power.apply) || (m.hero_power && m.hero_power.apply)
            || (m.hq_level && m.hq_level.apply) || !!m.rank_diff || !!m.name_change;
    }

    function statField(label, diff, member, key, klass) {
        if (!diff) return null;
        if (!diff.apply) {
            return el('div', { className: 'lr-field lr-skip' },
                `${label}: ${fmt(diff.new)} (${diff.skip_reason === 'stale' ? 'your data is newer' : diff.skip_reason || 'no change'})`);
        }
        const cb = el('input', { type: 'checkbox' });
        cb.checked = true;
        member._cb[key] = cb;
        return el('div', { className: 'lr-field' + (klass ? ' ' + klass : '') },
            cb,
            el('span', {}, `${label}: ${fmt(diff.current)} → `),
            el('span', { className: 'lr-new', textContent: fmt(diff.new) })
        );
    }

    function renderMatchedRow(m) {
        m._cb = {};
        const name = (m.matched_member && m.matched_member.name) || m.lastrank_name;
        const row = el('div', { className: 'lr-row' },
            el('div', { className: 'lr-row-name', textContent: name })
        );

        // Name change (matched via alias) — ask what to do; default keeps the name.
        if (m.name_change) {
            const sel = el('select', { className: 'form-input' },
                el('option', { value: '', textContent: `Keep "${m.name_change.current}"` }),
                el('option', { value: 'rename', textContent: `Rename to "${m.name_change.new}"` }),
                el('option', { value: 'alias', textContent: `Add "${m.name_change.new}" as global alias` })
            );
            m._nameAction = sel;
            row.appendChild(el('div', { className: 'lr-field lr-rank' },
                el('span', {}, 'Name on LastRank: '),
                el('span', { className: 'lr-new', textContent: m.name_change.new }),
                el('span', { className: 'lr-skip', textContent: `  (roster: ${m.name_change.current})` })));
            row.appendChild(el('div', { className: 'lr-unmatched-controls' }, sel));
        }

        const pf = statField('Power', m.power, m, 'power');
        const hf = statField('Hero Power', m.hero_power, m, 'hero');
        if (pf) row.appendChild(pf);
        if (hf) row.appendChild(hf);
        if (m.hq_level) {
            if (m.hq_level.apply) {
                const cb = el('input', { type: 'checkbox' }); cb.checked = true; m._cb.hq = cb;
                row.appendChild(el('div', { className: 'lr-field' }, cb,
                    el('span', {}, `HQ: ${m.hq_level.current} → `), el('span', { className: 'lr-new', textContent: String(m.hq_level.new) })));
            } else {
                row.appendChild(el('div', { className: 'lr-field lr-skip', textContent: `HQ: ${m.hq_level.new} (not higher than current)` }));
            }
        }
        if (m.rank_diff) {
            const cb = el('input', { type: 'checkbox' }); m._cb.rank = cb; // unchecked: review-only
            row.appendChild(el('div', { className: 'lr-field lr-rank' }, cb,
                el('span', {}, `Rank: ${m.rank_diff.current} → `),
                el('span', { className: 'lr-new', textContent: m.rank_diff.new }),
                el('span', { className: 'lr-skip', textContent: '  (review — leave unchecked to keep current)' })));
        }
        return row;
    }

    function renderUnmatchedRow(u, roster) {
        const detail = [u.rank, u.power != null ? fmt(u.power) + ' power' : null].filter(Boolean).join(' · ');
        const actionSel = el('select', { className: 'form-input' },
            el('option', { value: 'ignore', textContent: 'Ignore' }),
            el('option', { value: 'alias', textContent: 'Map to member (global alias)' }),
            el('option', { value: 'rename', textContent: 'Rename member to this name' }),
            el('option', { value: 'add', textContent: 'Add as new member' })
        );
        const memberSel = el('select', { className: 'form-input' },
            el('option', { value: '', textContent: '— pick member —' }),
            ...roster.map(r => el('option', { value: String(r.id), textContent: `${r.name} (${r.rank})` }))
        );
        memberSel.style.display = 'none';

        // "Accept their changes too" — apply this entry's stats to the paired/new
        // member. Shown for any non-ignore action; the server still gates on
        // staleness so it never overwrites fresher local data.
        const applyCb = el('input', { type: 'checkbox' });
        applyCb.checked = true;
        const applyLabel = el('label', { className: 'lr-field' }, applyCb,
            el('span', {}, " Also apply this player's power / hero / HQ"));
        applyLabel.style.display = 'none';
        u._applyStats = applyCb;

        // Optional join date, shown only for "add" — linked days-ago/date widget.
        // Blank → today's game date (server-side).
        const joinWidget = window.buildJoinDateField('');
        u._joinWidget = joinWidget;
        const joinLabel = el('label', { className: 'lr-field' }, el('span', {}, 'Join date: '), joinWidget.row);
        joinLabel.style.display = 'none';

        actionSel.addEventListener('change', () => {
            const a = actionSel.value;
            memberSel.style.display = (a === 'alias' || a === 'rename') ? '' : 'none';
            applyLabel.style.display = (a === 'ignore') ? 'none' : '';
            joinLabel.style.display = (a === 'add') ? '' : 'none';
        });
        u._action = actionSel;
        u._member = memberSel;
        return el('div', { className: 'lr-row' },
            el('div', { className: 'lr-row-name', textContent: u.lastrank_name }),
            detail ? el('div', { className: 'lr-field lr-skip', textContent: detail }) : null,
            el('div', { className: 'lr-unmatched-controls' }, actionSel, memberSel),
            applyLabel,
            joinLabel
        );
    }

    async function doCommit() {
        if (!previewData) return;
        const members = (previewData.matched || []).map(m => {
            const out = { member_id: m.matched_member.id, lastrank_public_id: m.lastrank_public_id };
            const cb = m._cb || {};
            if (m.power && m.power.apply && cb.power && cb.power.checked) out.power = m.power.new;
            if (m.hero_power && m.hero_power.apply && cb.hero && cb.hero.checked) out.hero_power = m.hero_power.new;
            if (m.hq_level && m.hq_level.apply && cb.hq && cb.hq.checked) out.hq_level = m.hq_level.new;
            if (m.rank_diff && cb.rank && cb.rank.checked) out.new_rank = m.rank_diff.new;
            if (m.name_change && m._nameAction && m._nameAction.value) {
                out.name_action = m._nameAction.value; // 'rename' | 'alias'
                out.name_new = m.name_change.new;
            }
            return out;
        });

        const unmatched = [];
        (previewData.unmatched || []).forEach(u => {
            const action = u._action ? u._action.value : 'ignore';
            if (action === 'ignore') return;
            const entry = { lastrank_name: u.lastrank_name, lastrank_public_id: u.lastrank_public_id, action };
            if (action === 'alias' || action === 'rename') {
                const mid = u._member ? parseInt(u._member.value, 10) : 0;
                if (!mid) return; // skip incomplete selections
                entry.member_id = mid;
            }
            if (action === 'add') {
                entry.new_rank = u.rank || '';
                entry.joined_at = u._joinWidget ? u._joinWidget.getISO() : '';
            }
            entry.apply_stats = u._applyStats ? u._applyStats.checked : false;
            if (entry.apply_stats) {
                if (u.power != null) entry.power = u.power;
                if (u.hero_power != null) entry.hero_power = u.hero_power;
                if (u.base_level != null) entry.base_level = u.base_level;
            }
            unmatched.push(entry);
        });

        const archive = (previewData.archive_candidates || [])
            .filter(c => c._archive && c._archive.checked)
            .map(c => c.member_id);

        confirmBtn.disabled = true;
        try {
            const res = await fetch('/api/lastrank/commit', {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ capture_date: previewData.alliance.last_seen_at, members, unmatched, archive })
            });
            if (!res.ok) throw new Error((await res.text()) || 'Commit failed');
            const r = await res.json();
            closeModal();
            let msg = `Applied: power ${r.power_updated}, hero ${r.hero_updated}, HQ ${r.hq_updated}, rank ${r.rank_updated}.`;
            if (r.members_archived) msg += ` Archived ${r.members_archived}.`;
            showToast(msg);
            if (typeof loadMembers === 'function') loadMembers();
        } catch (e) {
            showToast(e.message || 'Could not apply changes.', 'error');
        } finally {
            confirmBtn.disabled = false;
        }
    }

    // --- Phase 2: extended (per-player kills), browser-driven ---
    async function doExtended() {
        let members;
        try {
            const res = await fetch('/api/members');
            members = await res.json();
        } catch (e) {
            showToast('Could not load roster.', 'error');
            return;
        }
        // Oldest-synced first so an interrupted run resumes where it left off.
        // localeCompare returns 0 on ties (the old a<b?-1:1 returned 1 on ties,
        // which mis-ordered equal timestamps and made re-runs restart at the top).
        const pool = (members || [])
            .filter(m => m.lastrank_public_id)
            .sort((a, b) => (a.lastrank_synced_at || '').localeCompare(b.lastrank_synced_at || ''));

        if (pool.length === 0) {
            showToast('No members have a LastRank ID yet. Run "Fetch Alliance Data" first.', 'info');
            return;
        }
        if (!await showConfirm(`Fetch full stats (kills, power, hero, HQ) + photos for ${pool.length} member(s)? This pulls each from LastRank at ~1/second.`, 'Start')) return;

        extendedBtn.disabled = true;
        fetchBtn.disabled = true;
        progressEl.style.display = 'block';
        progressEl.replaceChildren();

        const rowEls = new Map();
        pool.forEach(m => {
            const status = el('span', { className: 'lr-prog-status', textContent: 'queued' });
            const row = el('div', { className: 'lr-prog-row' }, el('span', { className: 'lr-prog-name', textContent: m.name }), status);
            rowEls.set(m.id, { row, status });
            progressEl.appendChild(row);
        });

        let synced = 0, killRecords = 0, powerRecords = 0, heroRecords = 0, hqRecords = 0, professionRecords = 0, professionChanges = 0, photoRecords = 0, i = 0;
        for (const m of pool) {
            i++;
            setStatus(`Fetching ${i} of ${pool.length}…`);
            const { row, status } = rowEls.get(m.id);
            row.className = 'lr-prog-row active';
            status.textContent = 'fetching…';
            try {
                const r = await fetch('/api/lastrank/player', {
                    method: 'POST', headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ member_id: m.id })
                });
                if (!r.ok) throw new Error(await r.text());
                const data = await r.json();
                synced++;
                if (data.kills_applied) killRecords++;
                if (data.power_applied) powerRecords++;
                if (data.hero_applied) heroRecords++;
                if (data.hq_applied) hqRecords++;
                if (data.profession_level_applied) professionRecords++;
                if (data.profession_changed) professionChanges++;
                if (data.photo_updated) photoRecords++;
                if (data.kills_applied || data.power_applied || data.hero_applied || data.hq_applied || data.profession_level_applied || data.profession_changed || data.photo_updated) {
                    const parts = [];
                    if (data.kills_applied) parts.push('kills');
                    if (data.power_applied) parts.push('power');
                    if (data.hero_applied) parts.push('hero');
                    if (data.hq_applied) parts.push('HQ');
                    if (data.profession_level_applied) parts.push('profession lv');
                    if (data.profession_changed) parts.push('profession');
                    if (data.photo_updated) parts.push('photo');
                    row.className = 'lr-prog-row done';
                    status.textContent = '✓ ' + parts.join(' + ') + ' updated';
                } else {
                    row.className = 'lr-prog-row skip';
                    status.textContent = data.skip_reason === 'no_id' ? 'no LastRank id'
                        : data.skip_reason === 'stale' ? 'your data is newer' : 'no change';
                }
            } catch (e) {
                row.className = 'lr-prog-row err';
                status.textContent = 'error — skipped';
            }
        }

        setStatus('');
        try {
            await fetch('/api/lastrank/finish', {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ kind: 'extended', members_synced: synced, kill_records: killRecords, power_records: powerRecords, hero_records: heroRecords, hq_records: hqRecords, profession_records: professionRecords, profession_changes: professionChanges, photo_records: photoRecords })
            });
        } catch (e) { /* logging only — ignore */ }

        showToast(`Extended sync complete across ${synced} member(s) — ${killRecords} kills, ${powerRecords} power, ${heroRecords} hero, ${hqRecords} HQ, ${professionRecords} profession lv, ${photoRecords} photos.`);
        extendedBtn.disabled = false;
        fetchBtn.disabled = false;
        if (typeof loadMembers === 'function') loadMembers();
    }
})();
