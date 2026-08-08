// lastrank.js — LastRank.fun sync UI on the Members page.
// Phase 1: one alliance fetch → review modal → commit (stats auto, rank/unmatched
// reviewed). Phase 2 (extended per-player sync) runs SERVER-SIDE as a background
// job — see jobs.go; this file only starts it and watches via JobProgress.
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
    if (cancelBtn) cancelBtn.addEventListener('click', closeModal);
    if (confirmBtn) confirmBtn.addEventListener('click', doCommit);

    // Phase 2 (extended sync) runs server-side — see jobs.go. This page only
    // watches it, so closing the tab no longer stops the run.
    if (extendedBtn) {
        window.JobProgress.attach({
            kind: 'lastrank_extended',
            startBtn: extendedBtn,
            container: progressEl,
            statusEl: statusEl,
            cancelBtn: document.getElementById('lastrank-extended-cancel'),
            busyEls: [fetchBtn],
            confirm: 'Fetch full stats (kills, power, hero, HQ) + photos for every member with a LastRank ID? '
                + 'This runs on the server at ~1/second — you can close this page and come back.',
            summarize: c => `Extended sync complete across ${c.members || 0} member(s) — `
                + `${c.kills || 0} kills, ${c.power || 0} power, ${c.hero || 0} hero, `
                + `${c.HQ || 0} HQ, ${c['profession lv'] || 0} profession lv, ${c.photo || 0} photos.`,
            onDone: () => { if (typeof loadMembers === 'function') loadMembers(); },
        });
    }

    function closeModal() { modal.style.display = ''; }

    // ── Durable review queue ────────────────────────────────────────────────
    // Decisions outlive the modal now, so the panel advertises the backlog and can
    // open it WITHOUT a LastRank call — an officer working through it shouldn't
    // have to spend a request against the volunteer service just to look.
    const badgeEl = document.getElementById('lastrank-pending-badge');
    const queueBtn = document.getElementById('lastrank-queue-btn');

    async function refreshPendingBadge() {
        if (!badgeEl) return;
        try {
            const res = await fetch('/api/lastrank/review/summary');
            if (!res.ok) return;
            const d = await res.json();
            const n = d.open_count || 0;
            badgeEl.textContent = String(n);
            badgeEl.hidden = n === 0;
            if (queueBtn) queueBtn.style.display = n === 0 ? 'none' : '';
        } catch (e) { /* badge is decoration; never block the page on it */ }
    }

    async function openQueueOnly() {
        setStatus('Loading review queue…');
        try {
            const res = await fetch('/api/lastrank/review');
            if (!res.ok) throw new Error((await res.text()) || 'Could not load the queue');
            const d = await res.json();
            renderQueueOnly(d.items || []);
            modal.style.display = 'flex';
        } catch (e) {
            showToast(e.message || 'Could not load the review queue.', 'error');
        } finally {
            setStatus('');
        }
    }

    // Queue-only view: no alliance meta, no stat checkboxes — just the decisions,
    // grouped the same way as the post-fetch review so the two read alike.
    function renderQueueOnly(items) {
        previewData = null; // Confirm has nothing to commit in this mode
        indexPending(items);
        metaEl.replaceChildren(el('strong', { textContent: `${items.length} change(s) waiting for review` }));
        bodyEl.replaceChildren();
        if (confirmBtn) confirmBtn.style.display = 'none';

        if (!items.length) {
            bodyEl.appendChild(el('p', { className: 'lr-empty', textContent: 'Nothing waiting. Run Fetch Alliance Data to check for new changes.' }));
            return;
        }
        const groups = [
            ['rank', 'Rank changes'],
            ['name', 'Name changes'],
            ['unmatched', 'Unmatched names'],
            ['archive', 'Possibly left the alliance'],
        ];
        groups.forEach(([kind, title]) => {
            const rows = items.filter(i => i.kind === kind);
            if (!rows.length) return;
            bodyEl.appendChild(el('div', { className: 'lr-group-title', textContent: `${title} (${rows.length})` }));
            rows.forEach(p => bodyEl.appendChild(renderQueueRow(p)));
        });
    }

    function renderQueueRow(p) {
        const detail = p.kind === 'archive' ? p.reason
            : p.kind === 'unmatched' ? p.reason
            : `${p.current_value} → ${p.proposed_value}`;
        const applyBtn = el('button', { className: 'btn btn-primary btn-sm', type: 'button' });
        applyBtn.textContent = 'Apply';
        const row = el('div', { className: 'lr-row' },
            el('div', { className: 'lr-row-name', textContent: p.lastrank_name || p.current_value }),
            el('div', { className: 'lr-field lr-skip', textContent: detail }));

        if (p.kind === 'unmatched') {
            // Resolving one needs a target member, which this compact view can't ask
            // for — send the officer to the full review instead of guessing.
            row.appendChild(el('div', { className: 'lr-field lr-skip', textContent: 'Run Fetch Alliance Data to resolve this one.' }));
            row.appendChild(el('div', { className: 'lr-unmatched-controls' }, deferControls(p)));
            return row;
        }

        applyBtn.addEventListener('click', async () => {
            applyBtn.disabled = true;
            try {
                const res = await fetch('/api/lastrank/review/action', {
                    method: 'POST', headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ ids: [p.id], action: 'apply' })
                });
                if (!res.ok) throw new Error((await res.text()) || 'Could not apply');
                const d = await res.json();
                row.replaceChildren(el('div', { className: 'lr-field lr-skip', textContent:
                    d.applied ? `Applied: ${p.lastrank_name || p.current_value}`
                              : `Already up to date — ${p.lastrank_name || p.current_value}` }));
                refreshPendingBadge();
                if (typeof loadMembers === 'function') loadMembers();
            } catch (e) {
                applyBtn.disabled = false;
                showToast(e.message || 'Could not apply.', 'error');
            }
        });
        row.appendChild(el('div', { className: 'lr-unmatched-controls' }, applyBtn, deferControls(p)));
        return row;
    }

    if (queueBtn) queueBtn.addEventListener('click', openQueueOnly);
    refreshPendingBadge();

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
        if (confirmBtn) confirmBtn.style.display = '';
        indexPending(data.pending);
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
            bodyEl.appendChild(el('p', { className: 'lr-summary', textContent: 'Nothing happens unless you tick Archive. Verify first — a member who changed their in-game name to something we have no alias for can appear here even though they are still active. (Accent-only differences are matched automatically and no longer land here.)' }));
        }
        candidates.forEach(c => {
            const cb = el('input', { type: 'checkbox' }); // default unchecked = no action
            c._archive = cb;
            bodyEl.appendChild(el('div', { className: 'lr-row' },
                el('div', { className: 'lr-row-name', textContent: `${c.name} (${c.rank})` }),
                el('label', { className: 'lr-field' }, cb, el('span', {}, ` Archive — ${c.reason}`)),
                deferControls(pendingFor('archive:m:' + c.member_id))
            ));
        });
    }

    // Maps each preview row to its durable queue row, so a defer has something to
    // act on. Keys mirror lastRankSubjectKey in Go.
    let pendingIndex = {};
    function indexPending(list) {
        pendingIndex = {};
        (list || []).forEach(p => {
            const key = p.kind === 'unmatched'
                ? (p.lastrank_public_id ? 'unmatched:p:' + p.lastrank_public_id
                                        : 'unmatched:n:' + foldSearch(p.lastrank_name))
                : p.kind + ':m:' + p.member_id;
            pendingIndex[key] = p;
        });
    }
    const pendingFor = key => pendingIndex[key];

    // "Not now" hides an item until a genuinely newer pull; "Not until it changes"
    // hides it until LastRank proposes something different. Both are reversible —
    // nothing is applied and nothing is lost.
    function deferControls(pending) {
        if (!pending) return null;
        const wrap = el('span', { className: 'lr-defer' });
        const mk = (label, action, title) => {
            const b = el('button', { className: 'btn btn-secondary btn-sm', type: 'button', title });
            b.textContent = label;
            b.addEventListener('click', async () => {
                b.disabled = true;
                try {
                    const res = await fetch('/api/lastrank/review/action', {
                        method: 'POST', headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ ids: [pending.id], action })
                    });
                    if (!res.ok) throw new Error((await res.text()) || 'Could not defer');
                    wrap.replaceChildren(el('span', { className: 'lr-skip', textContent: 'deferred' }));
                    refreshPendingBadge();
                } catch (e) {
                    b.disabled = false;
                    showToast(e.message || 'Could not defer.', 'error');
                }
            });
            return b;
        };
        wrap.append(
            mk('Not now', 'defer_once', 'Hide until a newer LastRank pull'),
            mk('Not until it changes', 'defer_until_changed', 'Hide until LastRank proposes something different')
        );
        return wrap;
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
            const nameMid = m.matched_member && m.matched_member.id;
            row.appendChild(el('div', { className: 'lr-unmatched-controls' }, sel,
                deferControls(pendingFor('name:m:' + nameMid))));
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
            const mid = m.matched_member && m.matched_member.id;
            row.appendChild(el('div', { className: 'lr-field lr-rank' }, cb,
                el('span', {}, `Rank: ${m.rank_diff.current} → `),
                el('span', { className: 'lr-new', textContent: m.rank_diff.new }),
                el('span', { className: 'lr-skip', textContent: '  (review — leave unchecked to keep current)' }),
                deferControls(pendingFor('rank:m:' + mid))));
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
            el('div', { className: 'lr-unmatched-controls' }, actionSel, memberSel,
                deferControls(pendingFor(u.lastrank_public_id
                    ? 'unmatched:p:' + u.lastrank_public_id
                    : 'unmatched:n:' + foldSearch(u.lastrank_name)))),
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
        const incomplete = [];
        (previewData.unmatched || []).forEach(u => {
            if (u._member) clearFieldError(u._member);
            const action = u._action ? u._action.value : 'ignore';
            if (action === 'ignore') return;
            const entry = { lastrank_name: u.lastrank_name, lastrank_public_id: u.lastrank_public_id, action };
            if (action === 'alias' || action === 'rename') {
                const mid = u._member ? parseInt(u._member.value, 10) : 0;
                if (!mid) {
                    // Used to be skipped silently: the officer picked an action, saw a
                    // success toast, and the row was never applied. Block instead.
                    if (u._member) setFieldError(u._member, 'Pick a member, or set this row to Ignore.');
                    incomplete.push(u);
                    return;
                }
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

        if (incomplete.length) {
            showToast(`Pick a member for ${incomplete.length} unmatched name(s), or set them to Ignore.`, 'error');
            if (incomplete[0]._member) incomplete[0]._member.scrollIntoView({ block: 'center' });
            return;
        }

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
            refreshPendingBadge();
            if (typeof loadMembers === 'function') loadMembers();
        } catch (e) {
            showToast(e.message || 'Could not apply changes.', 'error');
        } finally {
            confirmBtn.disabled = false;
        }
    }

})();
