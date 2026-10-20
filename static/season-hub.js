/* season-hub.js — Season Hub page logic */

(function () {
    'use strict';

    // ---- Button loading state (moved from global.js — only Season Hub uses it) ----
    function setButtonLoading(btn, loadingText = 'Saving…') {
        btn.disabled = true;
        btn._originalText = btn.textContent;
        btn.textContent = loadingText;
    }

    function clearButtonLoading(btn) {
        btn.disabled = false;
        btn.textContent = btn._originalText ?? btn.textContent;
    }

    function escapeHtml(s) {
        return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    }

    // ── Config ────────────────────────────────────────────────────────────────
    const cfg = document.getElementById('page-config').dataset;
    const CAN_MANAGE = cfg.canManage === 'true';
    const CAN_MANAGE_REWARDS = cfg.canManageRewards === 'true';
    const CURRENT_MEMBER_ID = parseInt(cfg.memberId, 10) || 0;

    // ── State ─────────────────────────────────────────────────────────────────
    let activeSeason = null;       // Season object currently being viewed
    let scoreLevels = [];          // ScoreLevel[] for active season
    let allMembers = [];           // SeasonMember[] — filtered server-side already
    let allRewards = [];           // SeasonReward[]
    let rewardTiers = [];          // SeasonRewardTier[] for active season, in sort order
    let tierByKey = {};            // key → SeasonRewardTier, rebuilt on every data load
    let allMailItems = [];         // SeasonMailItem[]
    let contribPreviewData = null; // pending import data from preview
    let editingRewardId = null;    // id being edited, or null for create
    let rewardMemberChoices = null;

    // ── Rankings sort state ───────────────────────────────────────────────────
    let rankingsSortCol = 'rank_position';
    let rankingsSortAsc = true;

    // ── Request ownership ─────────────────────────────────────────────────────
    // Each load takes a generation; only the newest response for it is used, and rewards /
    // mail must also still belong to the season on screen. So a quick A→B→A switch, or a save
    // overlapping a switch, can't paint one season's data under another.
    const gen = { data: 0, rewards: 0, mail: 0 };
    const inflight = { rewards: null, mail: null };   // season id of a load in flight

    // ── Tabs (tabs.js), with #tab deep links ──────────────────────────────────
    // Declared up here, BEFORE Tabs.init: it calls onActivate for the initial tab inside
    // init(), so a `let` further down would throw (TDZ) and stop the whole page script.
    let currentTab = null;
    // Rewards and Season Mail load when their tab is shown (and again when the season changes
    // while one is open — loadSeasonData). A load already in flight for this season isn't
    // repeated.
    function loadTabExtras() {
        if (!activeSeason) return;
        if (currentTab === 'rewards' && inflight.rewards !== activeSeason.id) loadRewards();
        if (currentTab === 'comms' && inflight.mail !== activeSeason.id) loadSeasonMail();
    }
    // An old #season-mail link opens the tab it became.
    if (location.hash === '#season-mail') history.replaceState(null, '', '#comms');
    const tabsCtl = Tabs.init({ hash: true, defaultTab: 'rankings', onActivate: name => { currentTab = name; loadTabExtras(); } });

    // ── Per-season layout ─────────────────────────────────────────────────────
    // The template renders every tab this viewer may see; the season's layout reorders,
    // renames and hides them. Buttons are moved, not rebuilt, so tabs.js keeps its listeners.
    function applyLayout(layout) {
        const bar = document.getElementById('season-tab-bar');
        if (!bar || !layout || !layout.tabs) return;
        layout.tabs.forEach(t => {
            const btn = bar.querySelector('.tab-btn[data-tab="' + t.key + '"]');
            if (!btn) return;
            btn.textContent = t.label || t.default_label;
            btn.style.display = t.visible ? '' : 'none';
            bar.appendChild(btn);
        });
        const current = bar.querySelector('.tab-btn.active');
        if (!current || current.style.display === 'none') {
            const first = Array.from(bar.querySelectorAll('.tab-btn')).find(b => b.style.display !== 'none');
            if (first) tabsCtl.show(first.dataset.tab);
        }
    }

    // ── Load season list (for selector) ──────────────────────────────────────
    function loadSeasonList() {
        fetch('/api/season-hub/seasons')
            .then(r => r.json())
            .then(data => {
                const sel = document.getElementById('season-select');
                if (!sel) return;
                const seasons = data.seasons || [];
                sel.replaceChildren();
                if (seasons.length === 0) {
                    const opt = document.createElement('option');
                    opt.value = '';
                    opt.textContent = 'No seasons';
                    sel.appendChild(opt);
                    return;
                }
                seasons.forEach(s => {
                    const opt = document.createElement('option');
                    opt.value = s.id;
                    opt.textContent = 'S' + s.season_number + ' — ' + s.name +
                        (s.is_active ? ' · Active' : '');
                    if (s.is_active) opt.selected = true;
                    sel.appendChild(opt);
                });
                // Default to active season, or first if none active
                if (!sel.value && seasons.length > 0) sel.value = seasons[0].id;
                loadSeasonData(parseInt(sel.value, 10) || null);
            })
            .catch(() => showToast('Failed to load seasons.', 'error'));
    }

    const seasonSelect = document.getElementById('season-select');
    if (seasonSelect) {
        seasonSelect.addEventListener('change', () => {
            const id = parseInt(seasonSelect.value, 10) || null;
            loadSeasonData(id);
            if (CAN_MANAGE) { allRewards = []; renderRewardsTable(); }
            allMailItems = []; renderMailList();
        });
    }

    // ── Load season data ──────────────────────────────────────────────────────
    function loadSeasonData(seasonId) {
        const url = seasonId ? '/api/season-hub/data?season_id=' + seasonId : '/api/season-hub/data';
        const mine = ++gen.data;
        fetch(url)
            .then(r => r.json())
            .then(data => {
                if (mine !== gen.data) return;   // a newer season was asked for since
                const prevId = activeSeason ? activeSeason.id : null;
                activeSeason = data.season || null;
                scoreLevels = (data.season && data.season.score_levels) || [];
                allMembers = data.members || [];
                setRewardTiers((data.season && data.season.reward_tiers) || []);
                applyLayout(data.layout);

                updateSeasonHeader();
                renderRankingsTable();

                if (CAN_MANAGE) {
                    populateWeekSelect();
                    loadParticipationWeek();
                    populateContribWeekSelect();
                    renderContribHeaders();
                    renderManualTable();
                    populateOcrCategoryDropdown();
                }
                // A new season (or the first load, e.g. a #rewards deep link) with Rewards or
                // Season Mail open: load it. A reload of the same season doesn't — the saves that
                // trigger one already call loadRewards / loadSeasonMail themselves.
                if ((activeSeason ? activeSeason.id : null) !== prevId) loadTabExtras();
            })
            .catch(() => showToast('Failed to load season data.', 'error'));
    }

    function updateSeasonHeader() {
        const badge = document.getElementById('season-status-badge');
        const archiveBtn = document.getElementById('btn-archive-season');
        const editBtn = document.getElementById('btn-edit-season');
        const deleteBtn = document.getElementById('btn-delete-season');

        if (!activeSeason) {
            if (badge) badge.style.display = 'none';
            if (archiveBtn) archiveBtn.style.display = 'none';
            if (editBtn) editBtn.style.display = 'none';
            if (deleteBtn) deleteBtn.style.display = 'none';
            return;
        }

        if (badge) {
            badge.style.display = '';
            const today = new Date().toISOString().slice(0, 10);
            if (activeSeason.archived_at) {
                badge.textContent = 'Archived';
                badge.className = 'season-badge archived';
            } else if (activeSeason.start_date > today) {
                badge.textContent = 'Upcoming · Starts ' + activeSeason.start_date;
                badge.className = 'season-badge upcoming';
            } else if (activeSeason.is_active) {
                badge.textContent = 'Active';
                badge.className = 'season-badge active';
            } else {
                badge.textContent = 'Upcoming · Starts ' + activeSeason.start_date;
                badge.className = 'season-badge upcoming';
            }
        }
        // Archive button only shown for non-archived seasons that are active
        if (archiveBtn) {
            archiveBtn.style.display = (!activeSeason.archived_at && activeSeason.is_active) ? '' : 'none';
        }
        // Edit always visible when a season is selected
        if (editBtn) editBtn.style.display = '';
        // Delete only for non-active seasons (archived or upcoming)
        if (deleteBtn) deleteBtn.style.display = (!activeSeason.is_active) ? '' : 'none';

        // Update key event column headers
        const keyEventHeader = document.getElementById('key-event-col-header');
        if (keyEventHeader) keyEventHeader.textContent = activeSeason.key_event_name;
        const participationHeader = document.getElementById('key-event-participation-header');
        if (participationHeader) participationHeader.textContent = activeSeason.key_event_name;
    }

    // ── Rankings tab ──────────────────────────────────────────────────────────
    QuickSearch.attach({
        input: 'rankings-search',
        container: 'rankings-tbody',
        rows: 'tr',
        count: 'rankings-count',
        countLabel: (shown, total, q) =>
            q.trim() ? `${shown} of ${total} members` : `${total} member${total !== 1 ? 's' : ''}`,
        emptyText: 'No members match your search.',
    });

    const rankingsTable = document.getElementById('rankings-table');
    if (rankingsTable) {
        rankingsTable.querySelector('thead').addEventListener('click', e => {
            const th = e.target.closest('th[data-sort]');
            if (!th) return;
            const col = th.dataset.sort;
            if (rankingsSortCol === col) {
                rankingsSortAsc = !rankingsSortAsc;
            } else {
                rankingsSortCol = col;
                rankingsSortAsc = col === 'name' || col === 'rank' || col === 'rank_position';
            }
            renderRankingsTable();
        });
    }

    function renderRankingsTable() {
        const noSeasonMsg = document.getElementById('no-season-msg');
        const table = document.getElementById('rankings-table');
        const tbody = document.getElementById('rankings-tbody');

        if (!activeSeason) {
            if (noSeasonMsg) noSeasonMsg.style.display = '';
            if (table) table.style.display = 'none';
            return;
        }
        if (noSeasonMsg) noSeasonMsg.style.display = 'none';
        if (table) table.style.display = '';

        const maxScorePts = scoreLevels.length > 0 ? Math.max(...scoreLevels.map(sl => sl.points)) : 0;
        const maxPts = activeSeason ? activeSeason.week_count * maxScorePts : 0;

        // Search is applied by hiding rows (QuickSearch), NOT by filtering here:
        // rank positions below must be computed over the whole roster, or typing
        // in the search box renumbers the leaderboard and #1 becomes whoever you
        // searched for.
        let filtered = allMembers.slice();

        // Assign stable rank positions based on the default order (participation % desc,
        // then contribution % desc) before any user-chosen sort is applied.
        const defaultSorted = filtered.slice().sort((a, b) => {
            if (b.participation_pct !== a.participation_pct) return b.participation_pct - a.participation_pct;
            return b.contribution_pct - a.contribution_pct;
        });
        const rankPos = {};
        defaultSorted.forEach((m, i) => { rankPos[m.member_id] = i + 1; });

        // Apply user sort
        filtered = filtered.slice().sort((a, b) => {
            let va, vb;
            switch (rankingsSortCol) {
                case 'rank_position':     va = rankPos[a.member_id];   vb = rankPos[b.member_id];      break;
                case 'name':              va = a.name.toLowerCase();   vb = b.name.toLowerCase();      break;
                case 'rank':              va = a.rank;                  vb = b.rank;                    break;
                case 'participation_pct': va = a.participation_pct;    vb = b.participation_pct;       break;
                case 'contribution_pct':  va = a.contribution_pct;     vb = b.contribution_pct;        break;
                case 'key_event_attendance': va = a.key_event_attendance; vb = b.key_event_attendance; break;
                case 'class_tag':         va = a.class_tag;             vb = b.class_tag;               break;
                case 'reward_tier':       va = tierSortIndex(a.reward_tier); vb = tierSortIndex(b.reward_tier); break;
                default:                  va = 0; vb = 0;
            }
            if (va < vb) return rankingsSortAsc ? -1 : 1;
            if (va > vb) return rankingsSortAsc ? 1 : -1;
            return 0;
        });

        // Update sort indicator on headers
        document.querySelectorAll('#rankings-table th[data-sort]').forEach(th => {
            th.classList.remove('sort-asc', 'sort-desc');
            if (th.dataset.sort === rankingsSortCol) {
                th.classList.add(rankingsSortAsc ? 'sort-asc' : 'sort-desc');
            }
        });

        const rows = filtered.map((m, i) => {
            const tr = document.createElement('tr');

            tr.dataset.search = m.name + ' ' + m.rank;

            // Rank position (stable — always reflects default sort order)
            const tdPos = document.createElement('td');
            tdPos.className = 'col-rank';
            tdPos.textContent = rankPos[m.member_id];
            tr.appendChild(tdPos);

            // Name — an archived season shows the name it froze, with the name today and a
            // link to the current profile when the member still exists.
            const tdName = document.createElement('td');
            tdName.appendChild(memberNameNode(m));
            tr.appendChild(tdName);

            // Alliance rank
            const tdRank = document.createElement('td');
            const rankChip1 = document.createElement('span');
            rankChip1.className = `member-rank rank-${m.rank}`;
            rankChip1.textContent = m.rank;
            tdRank.appendChild(rankChip1);
            tr.appendChild(tdRank);

            // Participation
            tr.appendChild(makeParticipationCell(m, maxPts));

            // Contribution
            tr.appendChild(makeContributionCell(m));

            // Key event attendance
            const tdKey = document.createElement('td');
            const eligClass = m.key_event_eligible ? 'key-event-eligible' : 'key-event-ineligible';
            const eligSpan = document.createElement('span');
            eligSpan.className = eligClass;
            eligSpan.textContent = m.key_event_attendance + ' / ' + (activeSeason ? activeSeason.key_event_required : '—');
            tdKey.appendChild(eligSpan);
            tr.appendChild(tdKey);

            // Class tag
            const tdTag = document.createElement('td');
            tdTag.appendChild(makeClassTag(m.class_tag));
            tr.appendChild(tdTag);

            // Reward tier
            const tdTier = document.createElement('td');
            if (m.reward_tier) {
                tdTier.appendChild(makeTierBadge(m.reward_tier));
            }
            tr.appendChild(tdTier);

            return tr;
        });

        tbody.replaceChildren(...rows);
        QuickSearch.apply('rankings-search');
    }

    function memberNameNode(m) {
        const wrap = document.createElement('span');
        if (m.profile_id) {
            const a = document.createElement('a');
            a.href = '/accountability/' + m.profile_id;
            a.className = 'member-name-link';
            a.textContent = m.name;
            wrap.appendChild(a);
        } else {
            wrap.textContent = m.name;
        }
        if (m.current_name) {
            const now = document.createElement('span');
            now.className = 'member-current-name';
            now.textContent = ' (now ' + m.current_name + ')';
            wrap.appendChild(now);
        }
        wrap.title = activeSeason && activeSeason.archived_at
            ? (m.profile_id ? 'Name as frozen at archive' : 'Name as frozen at archive; the member has since been deleted')
            : '';
        return wrap;
    }

    function makeParticipationCell(m, maxPts) {
        const td = document.createElement('td');

        const wrap = document.createElement('div');
        wrap.className = 'pct-bar-wrap';
        const bg = document.createElement('div');
        bg.className = 'pct-bar-bg';
        const fill = document.createElement('div');
        fill.className = 'pct-bar-fill';
        fill.style.width = Math.min(100, m.participation_pct || 0).toFixed(1) + '%';
        bg.appendChild(fill);
        const label = document.createElement('span');
        label.className = 'pct-bar-label';
        label.textContent = maxPts > 0
            ? m.participation_pts + ' / ' + maxPts + ' pts'
            : (m.participation_pct || 0).toFixed(1) + '%';
        wrap.append(bg, label);
        td.appendChild(wrap);

        // Per-week score dots
        if (m.weekly_scores && m.weekly_scores.length > 0) {
            const maxWeekPts = scoreLevels.length > 0 ? Math.max(...scoreLevels.map(sl => sl.points)) : 0;
            const dots = document.createElement('div');
            dots.className = 'week-dots';
            m.weekly_scores.forEach((key, i) => {
                const dot = document.createElement('span');
                dot.className = 'week-dot';
                const sl = scoreLevels.find(s => s.key === key);
                if (!key) {
                    dot.classList.add('dot-empty');
                } else if (sl && sl.points === maxWeekPts) {
                    dot.classList.add('dot-full');
                } else if (sl && sl.points > 0) {
                    dot.classList.add('dot-partial');
                } else {
                    dot.classList.add('dot-absent');
                }
                dot.title = 'Week ' + (i + 1) + ': ' + (sl ? sl.label : 'Not logged');
                dots.appendChild(dot);
            });
            td.appendChild(dots);
        }

        return td;
    }

    function makeContributionCell(m) {
        const td = document.createElement('td');
        const wrap = document.createElement('div');
        wrap.className = 'pct-bar-wrap';
        const bg = document.createElement('div');
        bg.className = 'pct-bar-bg';
        const fill = document.createElement('div');
        fill.className = 'pct-bar-fill';
        fill.style.width = Math.min(100, m.contribution_pct || 0).toFixed(1) + '%';
        fill.style.background = 'var(--color-success)';
        bg.appendChild(fill);
        const label = document.createElement('span');
        label.className = 'pct-bar-label';
        label.textContent = m.contribution_total > 0
            ? m.contribution_total.toLocaleString()
            : '—';
        wrap.append(bg, label);
        td.appendChild(wrap);
        // The label is in the browser's locale; the export wants the number.
        td.dataset.exportValue = m.contribution_total > 0 ? String(m.contribution_total) : '';
        return td;
    }

    function makePctBar(pct, color) {
        const td = document.createElement('td');
        const wrap = document.createElement('div');
        wrap.className = 'pct-bar-wrap';
        const bg = document.createElement('div');
        bg.className = 'pct-bar-bg';
        const fill = document.createElement('div');
        fill.className = 'pct-bar-fill';
        fill.style.width = Math.min(100, pct || 0).toFixed(1) + '%';
        fill.style.background = color;
        bg.appendChild(fill);
        const label = document.createElement('span');
        label.className = 'pct-bar-label';
        label.textContent = (pct || 0).toFixed(1) + '%';
        wrap.appendChild(bg);
        wrap.appendChild(label);
        td.appendChild(wrap);
        return td;
    }

    function makeClassTag(tag) {
        const span = document.createElement('span');
        span.className = 'class-tag';
        if (tag === 'Active Member') span.classList.add('active-member');
        else if (tag === 'At Risk / Inconsistent') span.classList.add('at-risk');
        else span.classList.add('dead-weight');
        span.textContent = tag || '';
        return span;
    }

    // Reward tiers are per-season and configurable, so badges are driven by the
    // season's tier list rather than a fixed set of names. A key with no matching
    // tier (one removed after the reward was assigned) still renders, showing the
    // raw key — visible rather than blank.
    function setRewardTiers(tiers) {
        rewardTiers = tiers || [];
        tierByKey = {};
        rewardTiers.forEach(t => { tierByKey[t.key] = t; });
    }

    function tierSortIndex(key) {
        const t = tierByKey[key];
        return t ? t.sort_order : Number.MAX_SAFE_INTEGER;
    }

    function makeTierBadge(tier) {
        const span = document.createElement('span');
        span.className = 'tier-badge';
        const def = tierByKey[tier];
        span.classList.add('tone-' + ((def && def.color) || 'neutral'));
        span.textContent = def ? def.label : (tier || '');
        return span;
    }

    // ── Participation tab ─────────────────────────────────────────────────────

    // Rows carry a score <select>, key-event count and three note inputs, so the
    // search hides rows rather than re-rendering — a re-render would wipe every
    // unsaved edit. saveParticipation walks all rows, so hidden members still save.
    QuickSearch.attach({
        input: 'participation-search',
        container: 'participation-tbody',
        rows: 'tr',
        emptyText: 'No members match your search.',
    });

    function populateWeekSelect() {
        const sel = document.getElementById('week-select');
        if (!sel || !activeSeason) return;
        const prevWeek = sel.value;
        sel.replaceChildren();
        for (let w = 1; w <= activeSeason.week_count; w++) {
            const opt = document.createElement('option');
            opt.value = w;
            opt.textContent = w;
            sel.appendChild(opt);
        }
        if (prevWeek && parseInt(prevWeek, 10) <= activeSeason.week_count) {
            sel.value = prevWeek;
        }
        sel.addEventListener('change', loadParticipationWeek);
    }

    function populateContribWeekSelect() {
        const sel = document.getElementById('contrib-week');
        if (!sel || !activeSeason) return;
        const prevWeek = sel.value;
        sel.replaceChildren();
        const totalOpt = document.createElement('option');
        totalOpt.value = 0;
        totalOpt.textContent = 'Season Total';
        sel.appendChild(totalOpt);
        for (let w = 1; w <= activeSeason.week_count; w++) {
            const opt = document.createElement('option');
            opt.value = w;
            opt.textContent = w;
            sel.appendChild(opt);
        }
        if (prevWeek !== '' && parseInt(prevWeek, 10) <= activeSeason.week_count) {
            sel.value = prevWeek;
        }
    }

    function loadParticipationWeek() {
        const sel = document.getElementById('week-select');
        if (!sel || !activeSeason) return;
        const week = parseInt(sel.value, 10);
        const tbody = document.getElementById('participation-tbody');
        if (!tbody) return;

        fetch('/api/season-hub/participation?season_id=' + activeSeason.id + '&week=' + week)
            .then(r => r.json())
            .then(data => renderParticipationRows(data.entries, week))
            .catch(() => showToast('Failed to load participation.', 'error'));
    }

    function renderParticipationRows(existingEntries, week) {
        const tbody = document.getElementById('participation-tbody');
        if (!tbody) return;

        // Build a lookup of existing entries for the selected week, keyed by member_id
        const existing = {};
        (existingEntries || []).filter(e => e.week_number === week).forEach(e => { existing[e.member_id] = e; });

        // Use allMembers to enumerate active members
        const activeMembers = allMembers.filter(m => m.rank !== 'EX');
        if (activeMembers.length === 0 && allMembers.length > 0) {
            // Non-managers see only themselves; still show the row
        }

        const members = CAN_MANAGE ? allMembers.filter(m => m.rank !== 'EX') : allMembers;

        const rows = members.map(m => {
            const e = existing[m.member_id] || {};
            const tr = document.createElement('tr');
            tr.dataset.memberId = m.member_id;
            tr.dataset.search = m.name + ' ' + m.rank;

            const tdName = document.createElement('td');
            tdName.textContent = m.name;
            tr.appendChild(tdName);

            const tdRank = document.createElement('td');
            const rankChip2 = document.createElement('span');
            rankChip2.className = `member-rank rank-${m.rank}`;
            rankChip2.textContent = m.rank;
            tdRank.appendChild(rankChip2);
            tr.appendChild(tdRank);

            // Score dropdown
            const tdScore = document.createElement('td');
            const sel = document.createElement('select');
            sel.className = 'score-select';
            scoreLevels.forEach(sl => {
                const opt = document.createElement('option');
                opt.value = sl.key;
                opt.textContent = sl.label;
                if ((e.score || 'absent') === sl.key) opt.selected = true;
                sel.appendChild(opt);
            });
            tdScore.appendChild(sel);
            tr.appendChild(tdScore);

            // Key event count
            const tdKey = document.createElement('td');
            const keyInput = document.createElement('input');
            keyInput.type = 'number';
            keyInput.className = 'key-event-count';
            keyInput.min = '0';
            keyInput.style.width = '60px';
            keyInput.value = e.attended_key_event || 0;
            tdKey.appendChild(keyInput);
            tr.appendChild(tdKey);

            // Note
            const tdNote = document.createElement('td');
            const noteInput = document.createElement('input');
            noteInput.type = 'text';
            noteInput.className = 'general-note-input note-input form-input';
            noteInput.value = e.note || '';
            noteInput.placeholder = 'Optional note';
            tdNote.appendChild(noteInput);
            tr.appendChild(tdNote);

            // Attack / Defense notes and in-game poll confirmations.
            // Each week has one attack action and one defense action, so the
            // role-specific note says which context the officer meant.
            const mkRoleNote = (cls, value, placeholder) => {
                const td = document.createElement('td');
                const input = document.createElement('input');
                input.type = 'text';
                input.className = `${cls} note-input form-input`;
                input.value = value || '';
                input.placeholder = placeholder;
                td.appendChild(input);
                return td;
            };
            const mkPollCheck = (cls, checked, label) => {
                const td = document.createElement('td');
                td.className = 'poll-cell';
                const box = document.createElement('input');
                box.type = 'checkbox';
                box.className = `${cls} poll-check`;
                box.checked = !!checked;
                box.title = label;
                box.setAttribute('aria-label', `${label} — ${m.name}`);
                td.appendChild(box);
                return td;
            };

            tr.appendChild(mkRoleNote('attack-note-input', e.attack_note, 'Attack note'));
            tr.appendChild(mkPollCheck('attack-poll-check', e.attack_poll_voted, 'Voted on attack poll'));
            tr.appendChild(mkRoleNote('defense-note-input', e.defense_note, 'Defense note'));
            tr.appendChild(mkPollCheck('defense-poll-check', e.defense_poll_voted, 'Voted on defense poll'));

            return tr;
        });

        tbody.replaceChildren(...rows);
        QuickSearch.apply('participation-search');
    }

    const btnSaveParticipation = document.getElementById('btn-save-participation');
    if (btnSaveParticipation) {
        btnSaveParticipation.addEventListener('click', saveParticipation);
    }

    function saveParticipation() {
        const sel = document.getElementById('week-select');
        if (!sel || !activeSeason) return;
        const week = parseInt(sel.value, 10);
        const tbody = document.getElementById('participation-tbody');
        if (!tbody) return;

        const entries = [];
        tbody.querySelectorAll('tr').forEach(tr => {
            const memberId = parseInt(tr.dataset.memberId, 10);
            if (!memberId) return;
            const score = tr.querySelector('.score-select').value;
            const attended = parseInt(tr.querySelector('.key-event-count').value, 10) || 0;
            // '.note-input' is shared styling across all three note fields, so
            // select each by its own role class rather than by the shared one.
            const note = tr.querySelector('.general-note-input').value;
            entries.push({
                member_id: memberId,
                score,
                attended_key_event: attended,
                note,
                attack_note:        tr.querySelector('.attack-note-input').value,
                defense_note:       tr.querySelector('.defense-note-input').value,
                attack_poll_voted:  tr.querySelector('.attack-poll-check').checked,
                defense_poll_voted: tr.querySelector('.defense-poll-check').checked
            });
        });

        setButtonLoading(btnSaveParticipation);
        fetch('/api/season-hub/participation', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ season_id: activeSeason.id, week_number: week, entries })
        })
            .then(r => {
                if (!r.ok) return r.text().then(t => { throw new Error(t); });
                showToast('Week ' + week + ' participation saved.');
                loadSeasonData(activeSeason ? activeSeason.id : null);
            })
            .catch(err => showToast(err.message || 'Save failed.', 'error'))
            .finally(() => clearButtonLoading(btnSaveParticipation));
    }

    // ── Contributions tab ─────────────────────────────────────────────────────

    // OCR import — open modal
    const btnContribOcr = document.getElementById('btn-contrib-ocr');
    function populateOcrCategoryDropdown() {
        const sel = document.getElementById('contrib-category');
        if (!sel) return;
        sel.replaceChildren();
        (activeSeason ? activeSeason.trackables || [] : []).forEach(t => {
            ['_daily', '_weekly', '_season'].forEach(suffix => {
                const opt = document.createElement('option');
                opt.value = t.key + suffix;
                opt.textContent = t.label + suffix.replace('_', ' ');
                sel.appendChild(opt);
            });
        });
    }

    if (btnContribOcr) {
        btnContribOcr.addEventListener('click', () => {
            if (!activeSeason) { showToast('No active season.', 'error'); return; }
            populateOcrCategoryDropdown();
            const modal = document.getElementById('modal-ocr-import');
            if (modal) modal.style.display = 'flex';
        });
    }

    // Manual entry table — headers and inputs are driven by activeSeason.trackables
    function renderContribHeaders() {
        const headRow = document.getElementById('contrib-manual-thead-row');
        if (!headRow) return;
        // Keep first two (Member, Rank), replace the rest
        while (headRow.children.length > 2) headRow.removeChild(headRow.lastChild);
        (activeSeason ? activeSeason.trackables || [] : []).forEach(t => {
            const th = document.createElement('th');
            th.textContent = t.label;
            // Labels are editable and needn't be unique; the trackable key is both stable and unique.
            th.dataset.exportKey = 'trackable:' + t.key;
            headRow.appendChild(th);
        });
    }

    function renderManualTable() {
        const tbody = document.getElementById('contrib-manual-tbody');
        if (!tbody) return;

        const trackables = activeSeason ? (activeSeason.trackables || []) : [];
        // Render EVERY member and let QuickSearch hide rows. Filtering here would
        // rebuild the grid on each keystroke, blanking unsaved .contrib-input
        // values and re-fetching them, and would drop hidden members from the
        // save payload (saveManualContributions walks the rows, not the array).
        const members = allMembers.filter(m => m.rank !== 'EX');

        const rows = members.map(m => {
            const tr = document.createElement('tr');
            tr.dataset.memberId = m.member_id;
            tr.dataset.search = m.name + ' ' + m.rank;

            const tdName = document.createElement('td');
            tdName.textContent = m.name;
            tr.appendChild(tdName);

            const tdRank = document.createElement('td');
            const rankChip3 = document.createElement('span');
            rankChip3.className = `member-rank rank-${m.rank}`;
            rankChip3.textContent = m.rank;
            tdRank.appendChild(rankChip3);
            tr.appendChild(tdRank);

            trackables.forEach(t => {
                const td = document.createElement('td');
                const input = document.createElement('input');
                input.type = 'number';
                input.className = 'contrib-input';
                input.dataset.key = t.key;
                input.min = '0';
                input.value = '';
                input.placeholder = '0';
                td.appendChild(input);
                tr.appendChild(td);
            });

            return tr;
        });

        tbody.replaceChildren(...rows);
        QuickSearch.apply('contrib-manual-search');
        loadContribValues();
    }

    function loadContribValues() {
        if (!activeSeason) return;
        const weekEl = document.getElementById('contrib-week');
        const week = weekEl ? (parseInt(weekEl.value, 10) || 0) : 0;
        fetch('/api/season-hub/contributions?season_id=' + activeSeason.id + '&week=' + week)
            .then(r => r.json())
            .then(data => {
                const lookup = {};
                (data.entries || []).forEach(e => { lookup[e.member_id] = e; });
                const tbody = document.getElementById('contrib-manual-tbody');
                if (!tbody) return;
                tbody.querySelectorAll('tr').forEach(tr => {
                    const mid = parseInt(tr.dataset.memberId, 10);
                    const e = lookup[mid];
                    tr.querySelectorAll('.contrib-input').forEach(input => {
                        // Show actual value (including 0) when a DB record exists.
                        // Leave blank when there's no record — blank means "no data yet".
                        input.value = e !== undefined ? ((e.records || {})[input.dataset.key] ?? 0) : '';
                    });
                });
                QuickSearch.apply('contrib-manual-search');
            })
            .catch(() => {}); // silent — table still usable without pre-populated values
    }

    // Reload values when week changes
    const contribWeekEl = document.getElementById('contrib-week');
    if (contribWeekEl) {
        contribWeekEl.addEventListener('change', loadContribValues);
    }

    // Search hides rows in place — see renderManualTable for why re-rendering
    // on keystroke was destructive.
    QuickSearch.attach({
        input: 'contrib-manual-search',
        container: 'contrib-manual-tbody',
        rows: 'tr',
        emptyText: 'No members match your search.',
    });

    // Save manual entries
    const btnContribManualSave = document.getElementById('btn-contrib-manual-save');
    if (btnContribManualSave) {
        btnContribManualSave.addEventListener('click', saveManualContributions);
    }

    function saveManualContributions() {
        if (!activeSeason) return;
        const tbody = document.getElementById('contrib-manual-tbody');
        const weekEl = document.getElementById('contrib-week');
        const statusEl = document.getElementById('contrib-manual-status');
        if (!tbody || !weekEl) return;

        const entries = [];
        tbody.querySelectorAll('tr').forEach(tr => {
            const memberId = parseInt(tr.dataset.memberId, 10);
            if (!memberId) return;
            const records = {};
            let hasExplicit = false;
            tr.querySelectorAll('.contrib-input').forEach(input => {
                if (input.value !== '') hasExplicit = true;
                records[input.dataset.key] = input.value !== '' ? (parseInt(input.value, 10) || 0) : 0;
            });
            if (hasExplicit) entries.push({ member_id: memberId, records });
        });

        if (entries.length === 0) {
            if (statusEl) { statusEl.textContent = 'No non-zero entries to save.'; statusEl.style.color = 'var(--color-danger)'; }
            return;
        }

        setButtonLoading(btnContribManualSave);
        if (statusEl) { statusEl.textContent = ''; statusEl.style.color = ''; }

        fetch('/api/season-hub/contributions/manual', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                season_id: activeSeason.id,
                week_number: parseInt(weekEl.value, 10) || 0,
                entries,
            }),
        })
            .then(r => r.ok ? r.json() : r.text().then(t => { throw new Error(t); }))
            .then(data => {
                showToast(data.saved + ' contribution' + (data.saved !== 1 ? 's' : '') + ' saved.');
                loadContribValues();
                loadSeasonData(activeSeason ? activeSeason.id : null);
            })
            .catch(err => showToast(err.message || 'Save failed.', 'error'))
            .finally(() => clearButtonLoading(btnContribManualSave));
    }

    const btnContribPreview = document.getElementById('btn-contrib-preview');
    const btnContribCommit = document.getElementById('btn-contrib-commit');
    const btnContribCancel = document.getElementById('btn-contrib-cancel');

    if (btnContribPreview) {
        btnContribPreview.addEventListener('click', () => submitContribImport(false));
    }
    if (btnContribCommit) {
        btnContribCommit.addEventListener('click', () => submitContribImport(true));
    }
    if (btnContribCancel) {
        btnContribCancel.addEventListener('click', cancelContribImport);
    }

    function submitContribImport(commit) {
        const category = document.getElementById('contrib-category').value;
        const week = document.getElementById('contrib-week').value;
        const filesInput = document.getElementById('contrib-images');
        const statusEl = document.getElementById('contrib-import-status');

        if (!activeSeason) {
            if (statusEl) { statusEl.textContent = 'No active season.'; statusEl.style.color = 'var(--color-danger)'; }
            return;
        }
        if (!filesInput || filesInput.files.length === 0) {
            if (statusEl) { statusEl.textContent = 'Please select at least one screenshot.'; statusEl.style.color = 'var(--color-danger)'; }
            return;
        }

        const btn = commit ? btnContribCommit : btnContribPreview;
        setButtonLoading(btn);
        if (statusEl) { statusEl.textContent = commit ? 'Committing…' : 'Processing…'; statusEl.style.color = 'var(--color-text-muted)'; }

        const fd = new FormData();
        fd.append('season_id', activeSeason.id);
        fd.append('week_number', week);
        fd.append('category', category);
        fd.append('commit', commit ? 'true' : 'false');
        Array.from(filesInput.files).forEach(f => fd.append('images[]', f));

        if (commit && contribPreviewData) {
            const mappings = [];
            document.querySelectorAll('#contrib-unresolved-list tr').forEach((tr, idx) => {
                const memberSel = tr.querySelector('.unresolved-member-sel');
                const aliasSel  = tr.querySelector('.unresolved-alias-sel');
                const row = (contribPreviewData.unresolved || [])[idx];
                if (memberSel && memberSel.value && row) {
                    mappings.push({
                        original_name: row.original_name,
                        points:        row.points,
                        member_id:     parseInt(memberSel.value, 10),
                        alias_type:    aliasSel ? aliasSel.value : '',
                    });
                }
            });
            if (mappings.length) fd.append('resolved_mappings', JSON.stringify(mappings));
        }

        fetch('/api/season-hub/contributions/import', { method: 'POST', body: fd })
            .then(r => r.ok ? r.json().then(d => ({ ok: true, data: d })) : r.text().then(t => { throw new Error(t || 'Import failed.'); }))
            .then(({ ok, data }) => {
                if (!ok) throw new Error(data.error || 'Import failed.');
                if (commit) {
                    showToast('Contributions committed: ' + (data.committed || 0) + ' records' + (data.resolved || 0 ? ', ' + data.resolved + ' resolved' : '') + '.');
                    cancelContribImport();
                    loadSeasonData(activeSeason ? activeSeason.id : null);
                } else {
                    contribPreviewData = data;
                    renderContribPreview(data);
                    if (btnContribCommit) btnContribCommit.style.display = '';
                    if (btnContribCancel) btnContribCancel.style.display = '';
                    if (btnContribPreview) btnContribPreview.style.display = 'none';
                    if (statusEl) statusEl.textContent = '';
                }
            })
            .catch(err => {
                if (statusEl) { statusEl.textContent = err.message; statusEl.style.color = 'var(--color-danger)'; }
            })
            .finally(() => clearButtonLoading(btn));
    }

    QuickSearch.attach({
        input: 'contrib-preview-search', container: 'contrib-preview-tbody', rows: 'tr',
        emptyText: 'No names match your search.',
    });
    QuickSearch.attach({
        input: 'contrib-unresolved-search', container: 'contrib-unresolved-list', rows: 'tr',
        emptyText: 'No names match your search.',
    });

    function renderContribPreview(data) {
        const card = document.getElementById('contrib-preview-card');
        const summary = document.getElementById('contrib-preview-summary');
        const tbody = document.getElementById('contrib-preview-tbody');
        const unresolvedWrap = document.getElementById('contrib-unresolved-wrap');
        const unresolvedList = document.getElementById('contrib-unresolved-list');

        if (!card) return;
        card.style.display = '';

        const matched = (data.matched || []);
        const unresolved = (data.unresolved || []);
        if (summary) summary.textContent = matched.length + ' matched, ' + unresolved.length + ' unresolved.';

        const rows = matched.map(row => {
            const tr = document.createElement('tr');
            tr.dataset.search = (row.original_name || '') + ' ' + (row.member_name || '');
            const tdOcr = document.createElement('td');
            tdOcr.textContent = row.original_name;
            const tdMember = document.createElement('td');
            tdMember.textContent = row.member_name;
            const tdMatch = document.createElement('td');
            tdMatch.textContent = row.match_type;
            const tdPts = document.createElement('td');
            tdPts.textContent = (row.points || 0).toLocaleString();
            tdPts.dataset.exportValue = String(row.points || 0);
            tr.append(tdOcr, tdMember, tdMatch, tdPts);
            return tr;
        });
        if (tbody) tbody.replaceChildren(...rows);
        QuickSearch.apply('contrib-preview-search');

        if (unresolvedWrap && unresolvedList) {
            if (unresolved.length > 0) {
                unresolvedWrap.style.display = '';
                const rows = unresolved.map((row, idx) => {
                    const tr = document.createElement('tr');
                    tr.dataset.idx = idx;
                    tr.dataset.search = row.original_name || '';

                    const tdName = document.createElement('td');
                    tdName.textContent = row.original_name;

                    const tdPts = document.createElement('td');
                    tdPts.textContent = (row.points || 0).toLocaleString();
                    tdPts.dataset.exportValue = String(row.points || 0);

                    const tdMap = document.createElement('td');
                    const memberSel = document.createElement('select');
                    memberSel.className = 'form-input unresolved-member-sel';
                    memberSel.style.cssText = 'width:180px;font-size:0.85rem;';
                    const ignoreOpt = document.createElement('option');
                    ignoreOpt.value = ''; ignoreOpt.textContent = '— Ignore —';
                    memberSel.appendChild(ignoreOpt);
                    allMembers.filter(m => m.rank !== 'EX').forEach(m => {
                        const opt = document.createElement('option');
                        opt.value = m.id; opt.textContent = m.name + ' (' + m.rank + ')';
                        memberSel.appendChild(opt);
                    });
                    tdMap.appendChild(memberSel);

                    const tdAlias = document.createElement('td');
                    const aliasSel = document.createElement('select');
                    aliasSel.className = 'form-input unresolved-alias-sel';
                    aliasSel.style.cssText = 'width:160px;font-size:0.85rem;';
                    aliasSel.disabled = true;
                    [['', 'Do not save alias'], ['ocr', 'Save as OCR Alias'], ['global', 'Save as Global Alias'], ['personal', 'Save as Personal Alias']].forEach(([v, t]) => {
                        const opt = document.createElement('option');
                        opt.value = v; opt.textContent = t;
                        aliasSel.appendChild(opt);
                    });
                    memberSel.addEventListener('change', () => {
                        aliasSel.disabled = !memberSel.value;
                        if (memberSel.value) aliasSel.value = 'ocr';
                        else aliasSel.value = '';
                    });
                    tdAlias.appendChild(aliasSel);

                    tr.append(tdName, tdPts, tdMap, tdAlias);
                    return tr;
                });
                unresolvedList.replaceChildren(...rows);
                QuickSearch.apply('contrib-unresolved-search');
            } else {
                unresolvedWrap.style.display = 'none';
            }
        }
    }

    function cancelContribImport() {
        contribPreviewData = null;
        const card = document.getElementById('contrib-preview-card');
        if (card) card.style.display = 'none';
        if (btnContribCommit) btnContribCommit.style.display = 'none';
        if (btnContribCancel) btnContribCancel.style.display = 'none';
        if (btnContribPreview) btnContribPreview.style.display = '';
        const statusEl = document.getElementById('contrib-import-status');
        if (statusEl) statusEl.textContent = '';
        const filesInput = document.getElementById('contrib-images');
        if (filesInput) filesInput.value = '';
        const modal = document.getElementById('modal-ocr-import');
        if (modal) modal.style.display = '';
    }

    // ── Rewards tab ───────────────────────────────────────────────────────────

    QuickSearch.attach({
        input: 'rewards-search',
        container: 'rewards-tbody',
        rows: 'tr',
        emptyText: 'No rewards match your search.',
    });

    function loadRewards() {
        if (!activeSeason) return;
        const sid = activeSeason.id;
        const mine = ++gen.rewards;
        inflight.rewards = sid;
        fetch('/api/season-hub/rewards?season_id=' + sid)
            .then(r => r.json())
            .then(data => {
                if (mine !== gen.rewards || !activeSeason || activeSeason.id !== sid) return;
                allRewards = data.rewards || [];
                renderRewardsTable();
            })
            .catch(() => { if (mine === gen.rewards) showToast('Failed to load rewards.', 'error'); })
            .finally(() => { if (mine === gen.rewards) inflight.rewards = null; });
    }

    // One chip per configured tier showing how many of its slots are used.
    // slot_count 0 means "no cap set" and renders as a bare count.
    function renderTierSlots() {
        const wrap = document.getElementById('rewards-tier-slots');
        if (!wrap) return;

        if (!activeSeason || rewardTiers.length === 0) {
            wrap.replaceChildren();
            return;
        }

        const used = {};
        allRewards.forEach(rw => { used[rw.reward_tier] = (used[rw.reward_tier] || 0) + 1; });

        wrap.replaceChildren(...rewardTiers.map(t => {
            const n = used[t.key] || 0;
            const chip = document.createElement('span');
            chip.className = 'tier-slot-chip';
            if (t.slot_count > 0 && n > t.slot_count) chip.classList.add('over-capacity');

            chip.appendChild(makeTierBadge(t.key));

            const count = document.createElement('span');
            count.className = 'tier-slot-count';
            count.textContent = t.slot_count > 0 ? n + '/' + t.slot_count : String(n);
            chip.appendChild(count);

            return chip;
        }));
    }

    function renderRewardsTable() {
        const tbody = document.getElementById('rewards-tbody');
        if (!tbody) return;

        renderTierSlots();

        if (!activeSeason || allRewards.length === 0) {
            const tr = document.createElement('tr');
            const td = document.createElement('td');
            td.colSpan = CAN_MANAGE_REWARDS ? 8 : 7;
            td.className = 'loading-msg';
            td.textContent = activeSeason ? 'No rewards assigned yet.' : 'No active season.';
            tr.appendChild(td);
            tbody.replaceChildren(tr);
            return;
        }

        const rows = allRewards.map(rw => {
            const tr = document.createElement('tr');

            tr.dataset.search = rw.member_name + ' ' + rw.member_rank;

            const tdName = document.createElement('td'); tdName.textContent = rw.member_name; tr.appendChild(tdName);
            const tdRank = document.createElement('td'); const rankChip4 = document.createElement('span'); rankChip4.className = `member-rank rank-${rw.member_rank}`; rankChip4.textContent = rw.member_rank; tdRank.appendChild(rankChip4); tr.appendChild(tdRank);
            const tdTier = document.createElement('td'); tdTier.appendChild(makeTierBadge(rw.reward_tier)); tr.appendChild(tdTier);
            const tdPart = document.createElement('td'); tdPart.textContent = (rw.participation_pct || 0).toFixed(1) + '%'; tr.appendChild(tdPart);
            const tdContrib = document.createElement('td'); tdContrib.textContent = rw.contribution_pct != null ? (rw.contribution_pct).toFixed(1) + '%' : '—'; tr.appendChild(tdContrib);
            // #rewards-table is translate="no" to protect names and the CSV/XLSX export,
            // but this cell is free-form officer prose — opt it back in.
            const tdNote = document.createElement('td'); tdNote.setAttribute('translate', 'yes'); tdNote.textContent = rw.note || ''; tr.appendChild(tdNote);
            const tdBy = document.createElement('td'); tdBy.textContent = rw.logged_by || ''; tr.appendChild(tdBy);

            if (CAN_MANAGE_REWARDS) {
                const tdActions = document.createElement('td');
                const actionWrap = document.createElement('div');
                actionWrap.className = 'row-actions';

                actionWrap.appendChild(
                    rowActionBtn('btn btn-secondary btn-sm', 'pencil', 'Edit', () => openRewardModal(rw))
                );
                actionWrap.appendChild(
                    rowActionBtn('btn btn-danger btn-sm', 'trash', 'Delete', async () => {
                        if (!await showConfirm('Delete this reward?', 'Delete')) return;
                        deleteReward(rw.id);
                    })
                );

                tdActions.appendChild(actionWrap);
                tr.appendChild(tdActions);
            }

            return tr;
        });

        tbody.replaceChildren(...rows);
        QuickSearch.apply('rewards-search');
    }

    function openRewardModal(rw) {
        editingRewardId = rw ? rw.id : null;
        const modal = document.getElementById('modal-assign-reward');
        const title = document.getElementById('reward-modal-title');
        const memberSel = document.getElementById('reward-member');
        const tierSel = document.getElementById('reward-tier');
        const partPct = document.getElementById('reward-participation-pct');
        const contribPct = document.getElementById('reward-contribution-pct');
        const noteEl = document.getElementById('reward-note');
        const errEl = document.getElementById('reward-error');
        if (!modal) return;

        title.textContent = rw ? 'Edit Reward' : 'Assign Reward';
        if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }

        // Populate member select from allMembers
        if (rewardMemberChoices) {
            const opts = allMembers
                .filter(m => m.rank !== 'EX')
                .map(m => ({
                    value: String(m.member_id),
                    label: `<span class="member-rank rank-${m.rank}">${m.rank}</span> ${escapeHtml(m.name)}`,
                    selected: !!(rw && rw.member_id === m.member_id),
                }));
            rewardMemberChoices.setChoices(
                [{ value: '', label: '— select member —', placeholder: true }, ...opts],
                'value', 'label', true
            );
        }

        // The tier list is per-season and configurable, so the options are built
        // here rather than being static markup in the template.
        tierSel.replaceChildren(...rewardTiers.map(t => {
            const opt = document.createElement('option');
            opt.value = t.key;
            opt.textContent = t.label;
            return opt;
        }));

        if (rw) {
            // A reward whose tier was removed keeps its key; surface it rather
            // than silently snapping the dropdown to some other tier.
            if (rw.reward_tier && !tierByKey[rw.reward_tier]) {
                const orphan = document.createElement('option');
                orphan.value = rw.reward_tier;
                orphan.textContent = rw.reward_tier + ' (removed)';
                tierSel.appendChild(orphan);
            }
            tierSel.value = rw.reward_tier;
            partPct.value = rw.participation_pct;
            contribPct.value = rw.contribution_pct != null ? rw.contribution_pct : '';
            noteEl.value = rw.note || '';
        } else {
            // Default to the lowest tier — the broadest, most commonly assigned one.
            tierSel.value = rewardTiers.length ? rewardTiers[rewardTiers.length - 1].key : '';
            partPct.value = '';
            contribPct.value = '';
            noteEl.value = '';
        }

        modal.style.display = 'flex';
    }

    const btnAssignReward = document.getElementById('btn-assign-reward');
    if (btnAssignReward) {
        btnAssignReward.addEventListener('click', () => openRewardModal(null));
    }

    const btnCancelReward = document.getElementById('btn-cancel-reward');
    if (btnCancelReward) {
        btnCancelReward.addEventListener('click', () => {
            const modal = document.getElementById('modal-assign-reward');
            if (modal) modal.style.display = '';
        });
    }

    const rewardMemberEl = document.getElementById('reward-member');
    if (rewardMemberEl) {
        rewardMemberChoices = noTranslateChoices(new Choices(rewardMemberEl, {
            searchEnabled: true, searchPlaceholderValue: 'Search…',
            itemSelectText: '', shouldSort: false, allowHTML: true,
        }));
    }

    const formAssignReward = document.getElementById('form-assign-reward');
    if (formAssignReward) {
        formAssignReward.addEventListener('submit', e => {
            e.preventDefault();
            submitReward();
        });
    }

    function submitReward() {
        if (!activeSeason) return;
        const memberSel = document.getElementById('reward-member');
        const tierSel = document.getElementById('reward-tier');
        const partPct = document.getElementById('reward-participation-pct');
        const contribPct = document.getElementById('reward-contribution-pct');
        const noteEl = document.getElementById('reward-note');
        const errEl = document.getElementById('reward-error');
        const submitBtn = document.getElementById('btn-submit-reward');

        const body = {
            season_id: activeSeason.id,
            member_id: parseInt(memberSel.value, 10),
            reward_tier: tierSel.value,
            participation_pct: parseFloat(partPct.value),
            contribution_pct: contribPct.value !== '' ? parseFloat(contribPct.value) : null,
            note: noteEl.value
        };

        const method = editingRewardId ? 'PUT' : 'POST';
        const url = editingRewardId ? '/api/season-hub/rewards/' + editingRewardId : '/api/season-hub/rewards';

        setButtonLoading(submitBtn);
        fetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        })
            .then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }))
            .then(() => {
                const modal = document.getElementById('modal-assign-reward');
                if (modal) modal.style.display = '';
                showToast(editingRewardId ? 'Reward updated.' : 'Reward assigned.');
                loadRewards();
                loadSeasonData(activeSeason ? activeSeason.id : null);
            })
            .catch(err => {
                if (errEl) { errEl.textContent = err.message || 'Save failed.'; errEl.style.display = ''; }
            })
            .finally(() => clearButtonLoading(submitBtn));
    }

    function deleteReward(id) {
        fetch('/api/season-hub/rewards/' + id, { method: 'DELETE' })
            .then(r => {
                if (!r.ok) return r.text().then(t => { throw new Error(t); });
                showToast('Reward deleted.');
                loadRewards();
                loadSeasonData(activeSeason ? activeSeason.id : null);
            })
            .catch(err => showToast(err.message || 'Delete failed.', 'error'));
    }

    // ── Season Mail tab ───────────────────────────────────────────────────────
    function loadSeasonMail() {
        if (!activeSeason) return;
        const sid = activeSeason.id;
        const mine = ++gen.mail;
        inflight.mail = sid;
        fetch('/api/comms/templates?type=mail&season_id=' + sid)
            .then(r => r.json())
            .then(data => {
                if (mine !== gen.mail || !activeSeason || activeSeason.id !== sid) return;
                allMailItems = data.items || [];
                renderMailList();
            })
            .catch(() => { if (mine === gen.mail) showToast('Failed to load season mail.', 'error'); })
            .finally(() => { if (mine === gen.mail) inflight.mail = null; });
    }

    function renderMailList() {
        const container = document.getElementById('season-mail-list');
        if (!container) return;

        if (!activeSeason || allMailItems.length === 0) {
            const p = document.createElement('p');
            p.className = 'loading-msg';
            p.textContent = activeSeason ? 'No documents uploaded yet.' : 'No active season.';
            container.replaceChildren(p);
            return;
        }

        const items = allMailItems.map(item => {
            const wrapper = document.createElement('div');
            wrapper.className = 'mail-item-wrap';

            // Header row
            const div = document.createElement('div');
            div.className = 'mail-item';

            const info = document.createElement('div');
            info.className = 'mail-item-info';

            const titleEl = document.createElement('div');
            titleEl.className = 'mail-item-title';
            titleEl.textContent = item.title;

            const meta = document.createElement('div');
            meta.className = 'mail-item-meta';
            meta.textContent = 'Posted by ' + item.posted_by + ' · ' + formatDate(item.posted_at);

            info.append(titleEl, meta);

            const actions = document.createElement('div');
            actions.className = 'mail-item-actions';

            // Copy button — uses mail.js copyWithVariables for variable support
            const copyBtn = document.createElement('button');
            copyBtn.className = 'btn btn-secondary btn-sm';
            copyBtn.textContent = 'Copy';
            copyBtn.addEventListener('click', () => copyWithVariables(item.content || ''));
            actions.appendChild(copyBtn);

            // Toggle expand
            if (item.content) {
                const expandBtn = document.createElement('button');
                expandBtn.className = 'btn btn-secondary btn-sm';
                expandBtn.textContent = 'View';
                let expanded = false;
                expandBtn.addEventListener('click', () => {
                    expanded = !expanded;
                    contentBox.style.display = expanded ? 'block' : 'none';
                    expandBtn.textContent = expanded ? 'Hide' : 'View';
                });
                actions.appendChild(expandBtn);
            }

            if (CAN_MANAGE) {
                const editBtn = document.createElement('button');
                editBtn.className = 'btn btn-secondary btn-sm';
                editBtn.textContent = 'Edit';
                editBtn.addEventListener('click', () => openMailModal(item));
                actions.appendChild(editBtn);

                const delBtn = document.createElement('button');
                delBtn.className = 'btn btn-danger btn-sm';
                delBtn.textContent = 'Delete';
                delBtn.addEventListener('click', async () => {
                    if (!await showConfirm('Delete this mail item?', 'Delete')) return;
                    deleteMailItem(item.id);
                });
                actions.appendChild(delBtn);
            }

            div.append(info, actions);

            // Content box (collapsed by default)
            const contentBox = document.createElement('pre');
            contentBox.className = 'mail-item-content';
            contentBox.style.display = 'none';
            contentBox.textContent = item.content || '';
            if (item.content) TranslateBlock.attach(contentBox, item.content);

            wrapper.append(div, contentBox);
            return wrapper;
        });

        container.replaceChildren(...items);
    }

    const btnUploadMail = document.getElementById('btn-upload-mail');
    const btnCancelMail = document.getElementById('btn-cancel-mail');
    const formUploadMail = document.getElementById('form-upload-mail');
    let editingMailId = null;

    function openMailModal(item) {
        editingMailId = item ? item.id : null;
        const modal = document.getElementById('modal-upload-mail');
        const heading = modal ? modal.querySelector('h3') : null;
        const titleEl = document.getElementById('mail-title');
        const contentEl = document.getElementById('mail-content');
        const errEl = document.getElementById('mail-error');
        const submitBtn = formUploadMail ? formUploadMail.querySelector('button[type="submit"]') : null;

        if (heading) heading.textContent = item ? 'Edit Mail' : 'Post Mail';
        if (submitBtn) submitBtn.textContent = item ? 'Save' : 'Post';
        if (titleEl) titleEl.value = item ? item.title : '';
        if (contentEl) contentEl.value = item ? (item.content || '') : '';
        if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }
        if (modal) modal.style.display = 'flex';
    }

    if (btnUploadMail) {
        btnUploadMail.addEventListener('click', () => openMailModal(null));
    }
    if (btnCancelMail) {
        btnCancelMail.addEventListener('click', () => {
            const modal = document.getElementById('modal-upload-mail');
            if (modal) modal.style.display = '';
        });
    }
    if (formUploadMail) {
        formUploadMail.addEventListener('submit', e => {
            e.preventDefault();
            submitMailUpload();
        });
    }

    function submitMailUpload() {
        const titleEl = document.getElementById('mail-title');
        const contentEl = document.getElementById('mail-content');
        const errEl = document.getElementById('mail-error');

        if (!titleEl.value.trim()) {
            if (errEl) { errEl.textContent = 'Title is required.'; errEl.style.display = ''; }
            return;
        }
        if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }

        const isEdit = !!editingMailId;

        if (!isEdit && !activeSeason) {
            if (errEl) { errEl.textContent = 'No active season.'; errEl.style.display = ''; }
            return;
        }

        const url = isEdit
            ? '/api/comms/templates/' + editingMailId
            : '/api/comms/templates';
        const method = isEdit ? 'PUT' : 'POST';
        const seasonCategory = activeSeason ? 'Season ' + activeSeason.season_number : 'Season Mail';
        const body = isEdit
            ? { title: titleEl.value.trim(), content: contentEl ? contentEl.value : '', category: seasonCategory, required_vars: '[]' }
            : { type: 'mail', season_id: activeSeason.id, title: titleEl.value.trim(), content: contentEl ? contentEl.value : '', category: seasonCategory };

        const submitBtn = formUploadMail.querySelector('button[type="submit"]');
        setButtonLoading(submitBtn);
        fetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        })
            .then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }))
            .then(() => {
                const modal = document.getElementById('modal-upload-mail');
                if (modal) modal.style.display = '';
                formUploadMail.reset();
                showToast(isEdit ? 'Mail updated.' : 'Mail posted.');
                loadSeasonMail();
            })
            .catch(err => {
                if (errEl) { errEl.textContent = err.message || 'Save failed.'; errEl.style.display = ''; }
            })
            .finally(() => clearButtonLoading(submitBtn));
    }

    function deleteMailItem(id) {
        fetch('/api/comms/templates/' + id, { method: 'DELETE' })
            .then(r => {
                if (!r.ok) return r.text().then(t => { throw new Error(t); });
                showToast('Document deleted.');
                loadSeasonMail();
            })
            .catch(err => showToast(err.message || 'Delete failed.', 'error'));
    }

    // ── Season create / archive ───────────────────────────────────────────────
    const btnCreateSeason = document.getElementById('btn-create-season');
    const btnCancelCreateSeason = document.getElementById('btn-cancel-create-season');
    const formCreateSeason = document.getElementById('form-create-season');
    const btnArchiveSeason = document.getElementById('btn-archive-season');

    // ── Create Season — trackable + event row helpers ─────────────────────────

    let csEventTypes = []; // cached schedule_event_types for dropdowns

    function fetchEventTypes() {
        if (csEventTypes.length > 0) return Promise.resolve(csEventTypes);
        // GET /api/schedule/event-types returns a BARE ARRAY, not {event_types:[]}.
        // Reading the wrapper left the cache permanently empty, so every type
        // dropdown in the Edit Season modal held only "— none —" and saving a row
        // blanked its type.
        return fetch('/api/schedule/event-types')
            .then(r => r.ok ? r.json() : [])
            .then(d => { csEventTypes = Array.isArray(d) ? d : []; return csEventTypes; })
            .catch(() => []);
    }

    // Create modes: a template, another season's configuration, or blank.
    function createMode() {
        const checked = document.querySelector('input[name="cs-mode"]:checked');
        return checked ? checked.value : 'template';
    }
    function showCreateMode() {
        const mode = createMode();
        document.querySelectorAll('#modal-create-season [class*="cs-mode-"]').forEach(el => {
            el.style.display = el.classList.contains('cs-mode-' + mode) ? '' : 'none';
        });
        // A copy takes the source season's floors; the other two set their own.
        document.querySelectorAll('#modal-create-season .cs-floors').forEach(el => {
            el.style.display = mode === 'copy' ? 'none' : '';
        });
    }
    document.querySelectorAll('input[name="cs-mode"]').forEach(r => r.addEventListener('change', showCreateMode));

    function loadCopySources() {
        const sel = document.getElementById('cs-source');
        if (!sel) return;
        fetch('/api/season-hub/seasons')
            .then(r => r.json())
            .then(d => {
                sel.replaceChildren(...(d.seasons || []).map(se => {
                    const opt = document.createElement('option');
                    opt.value = se.id;
                    opt.textContent = 'S' + se.season_number + ' — ' + se.name + (se.archived_at ? ' (archived)' : '');
                    return opt;
                }));
                noTranslate(sel);
            })
            .catch(() => {});
    }

    function openCreateSeasonModal() {
        const errEl = document.getElementById('cs-error');
        if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }
        updateArchiveWarning();
        showCreateMode();
        loadCopySources();

        // Populate template dropdown
        const sel = document.getElementById('cs-template');
        if (sel && sel.options.length <= 1) {
            fetch('/api/season-hub/templates')
                .then(r => r.json())
                .then(d => {
                    sel.replaceChildren();
                    const blank = document.createElement('option');
                    blank.value = ''; blank.textContent = '— select template —';
                    sel.appendChild(blank);
                    (d.templates || []).forEach(t => {
                        const opt = document.createElement('option');
                        opt.value = t.id;
                        opt.dataset.name = t.template_name;
                        opt.dataset.defaults = t.defaults;
                        opt.textContent = (t.season_number > 0 ? 'S' + t.season_number + ' — ' : '') + t.template_name;
                        sel.appendChild(opt);
                    });
                })
                .catch(() => {});
        }

        const modal = document.getElementById('modal-create-season');
        if (modal) modal.style.display = 'flex';
    }

    if (btnCreateSeason) {
        btnCreateSeason.addEventListener('click', openCreateSeasonModal);
    }

    // Template select — show preview of week count and key event
    const csTplSel = document.getElementById('cs-template');
    if (csTplSel) {
        csTplSel.addEventListener('change', () => {
            const opt = csTplSel.selectedOptions[0];
            const preview = document.getElementById('cs-template-preview');
            if (!opt || !opt.value) { if (preview) preview.style.display = 'none'; return; }
            try {
                const defs = JSON.parse(opt.dataset.defaults || '{}');
                if (preview) {
                    preview.textContent = (defs.week_count || 8) + ' weeks · ' + (defs.key_event_name || '') +
                        (defs.key_event_required ? ' (required: ' + defs.key_event_required + ')' : '');
                    preview.style.display = '';
                }
            } catch (_) {}
            updateArchiveWarning();
        });
    }

    // Show/hide archive warning based on start date vs today
    function updateArchiveWarning() {
        const warnEl = document.getElementById('cs-archive-warning');
        if (!warnEl) return;
        const startDateEl = document.getElementById('cs-start-date');
        const today = new Date().toISOString().slice(0, 10);
        const hasActiveSeason = activeSeason && !activeSeason.archived_at;
        if (!hasActiveSeason || !startDateEl || !startDateEl.value) {
            warnEl.style.display = 'none';
            return;
        }
        if (startDateEl.value <= today) {
            warnEl.textContent = 'The current active season "' + activeSeason.name + '" will be archived when this season is created.';
        } else {
            warnEl.textContent = 'The current active season "' + activeSeason.name + '" will remain active until the new season begins on ' + startDateEl.value + '.';
        }
        warnEl.style.display = '';
    }

    const csStartDateEl = document.getElementById('cs-start-date');
    if (csStartDateEl) {
        csStartDateEl.addEventListener('change', updateArchiveWarning);
    }
    if (btnCancelCreateSeason) {
        btnCancelCreateSeason.addEventListener('click', () => {
            const modal = document.getElementById('modal-create-season');
            if (modal) modal.style.display = '';
        });
    }
    if (formCreateSeason) {
        formCreateSeason.addEventListener('submit', e => {
            e.preventDefault();
            submitCreateSeason();
        });
    }

    function submitCreateSeason() {
        const errEl = document.getElementById('cs-error');
        const submitBtn = formCreateSeason.querySelector('button[type="submit"]');
        const tplSel = document.getElementById('cs-template');

        const mode = createMode();
        const body = {
            mode,
            template_id: parseInt(tplSel?.value, 10) || 0,
            source_season_id: parseInt(document.getElementById('cs-source').value, 10) || 0,
            name: document.getElementById('cs-name').value.trim(),
            season_number: parseInt(document.getElementById('cs-number').value, 10) || 0,
            week_count: parseInt(document.getElementById('cs-weeks').value, 10) || 0,
            key_event_name: document.getElementById('cs-key-event').value.trim(),
            start_date: document.getElementById('cs-start-date').value,
            tier_active_min_pct: parseInt(document.getElementById('cs-tier-active').value, 10) || 70,
            tier_at_risk_min_pct: parseInt(document.getElementById('cs-tier-at-risk').value, 10) || 60,
        };

        const need = msg => { if (errEl) { errEl.textContent = msg; errEl.style.display = ''; } };
        if (mode === 'template' && !body.template_id) { need('Please select a template.'); return; }
        if (mode === 'copy' && !body.source_season_id) { need('Please choose the season to copy.'); return; }
        if (mode !== 'template' && (!body.name || !body.season_number)) { need('Name and season number are required.'); return; }
        if (mode === 'blank' && !body.week_count) { need('Weeks are required.'); return; }
        if (!body.start_date) {
            if (errEl) { errEl.textContent = 'Start date is required.'; errEl.style.display = ''; }
            return;
        }

        setButtonLoading(submitBtn);
        fetch('/api/season-hub/seasons', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        })
            .then(r => r.ok ? r.json() : r.text().then(t => { throw new Error(t); }))
            .then(d => {
                const modal = document.getElementById('modal-create-season');
                if (modal) modal.style.display = '';
                let msg = 'Season created.';
                const p = d && d.pushed;
                if (p && (p.created || p.skipped_no_type || p.skipped_unscheduled)) {
                    msg += ' Pushed ' + (p.created || 0) + ' event' + ((p.created || 0) === 1 ? '' : 's') + ' to schedule';
                    if (p.skipped_unscheduled) msg += ', ' + p.skipped_unscheduled + ' skipped (no day set)';
                    if (p.skipped_no_type) msg += ', ' + p.skipped_no_type + ' skipped (no event type — set it in Edit Season)';
                    if (p.drifted) msg += ', ' + p.drifted + ' drifted from the template date';
                    msg += '.';
                }
                showToast(msg);
                loadSeasonList();
                loadRewards();
                loadSeasonMail();
            })
            .catch(err => {
                if (errEl) { errEl.textContent = err.message || 'Create failed.'; errEl.style.display = ''; }
            })
            .finally(() => clearButtonLoading(submitBtn));
    }

    if (btnArchiveSeason) {
        btnArchiveSeason.addEventListener('click', async () => {
            if (!activeSeason) return;
            if (!await showConfirm('Archive "' + activeSeason.name + '"? All write operations will be locked.', 'Archive Season')) return;
            fetch('/api/season-hub/seasons/' + activeSeason.id + '/archive', { method: 'POST' })
                .then(r => {
                    if (!r.ok) return r.text().then(t => { throw new Error(t); });
                    showToast('Season archived.');
                    loadSeasonList();
                    loadRewards();
                    loadSeasonMail();
                })
                .catch(err => showToast(err.message || 'Archive failed.', 'error'));
        });
    }

    // ── Edit season — per-row trackable/event builders ────────────────────────

    // Per-row REST, same shape as buildEsTrackableRow. The key is an input on a
    // new row and plain text once saved — it is the value stored on every
    // season_rewards row for this tier, so it cannot change afterwards.
    //
    // Order is set by the up/down buttons, not by a sort_order field: the number
    // itself means nothing to an officer, only the resulting ladder does. A move
    // persists immediately for every already-saved row, so the badge order on the
    // page behind the modal matches what is on screen.
    function buildEsRewardTierRow(rt, seasonId) {
        rt = rt || {};
        const tr = document.createElement('tr');
        tr.dataset.id = rt.id || '';
        if (rt.sort_order != null) tr.dataset.sortOrder = rt.sort_order;

        const tdKey = document.createElement('td');
        if (rt.id) {
            tdKey.textContent = rt.key;
        } else {
            const inp = document.createElement('input');
            inp.type = 'text'; inp.className = 'form-input rt-key'; inp.placeholder = 'key (e.g. vanguard)';
            tdKey.appendChild(inp);
        }
        tr.appendChild(tdKey);

        const tdLabel = document.createElement('td');
        const inpLabel = document.createElement('input');
        inpLabel.type = 'text'; inpLabel.className = 'form-input rt-label';
        inpLabel.value = rt.label || ''; inpLabel.placeholder = 'Label';
        tdLabel.appendChild(inpLabel);
        tr.appendChild(tdLabel);

        const tdSlots = document.createElement('td');
        const inpSlots = document.createElement('input');
        inpSlots.type = 'number'; inpSlots.className = 'form-input rt-slots';
        inpSlots.min = '0'; inpSlots.value = rt.slot_count != null ? rt.slot_count : 0;
        tdSlots.appendChild(inpSlots);
        tr.appendChild(tdSlots);

        const tdColor = document.createElement('td');
        const selColor = ColorPicker.buildSelect(rt.color, 'form-input rt-color');
        tdColor.appendChild(selColor);
        tr.appendChild(tdColor);
        // The Choices upgrade happens in refreshTierEditor() once the tbody is
        // populated — Choices wraps the element in place, so it needs a parent.

        const tdAct = document.createElement('td');
        tdAct.style.cssText = 'white-space:nowrap;';
        tdAct.appendChild(buildOrderButtons(tr, persistTierOrder));

        const saveBtn = document.createElement('button');
        saveBtn.type = 'button'; saveBtn.className = 'btn btn-primary btn-sm'; saveBtn.textContent = 'Save';
        saveBtn.style.marginLeft = '4px';
        saveBtn.addEventListener('click', () => {
            const label = inpLabel.value.trim();
            if (!label) { showToast('Label required.', 'error'); return; }
            const payload = {
                label,
                slot_count: parseInt(inpSlots.value, 10) || 0,
                color: selColor.value,
                sort_order: rowPosition(tr),
            };
            if (rt.id) {
                fetch('/api/season-hub/reward-tiers/' + rt.id, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                }).then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }))
                  .then(() => { rt.label = label; showToast('Reward tier saved.'); reloadActiveSeasonTiers(); })
                  .catch(err => showToast(err.message || 'Save failed.', 'error'));
            } else {
                const keyEl = tdKey.querySelector('.rt-key');
                const key = keyEl ? keyEl.value.trim() : '';
                if (!key) { showToast('Key required.', 'error'); return; }
                fetch('/api/season-hub/reward-tiers', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(Object.assign({ season_id: seasonId, key }, payload))
                }).then(r => r.ok ? r.json() : r.text().then(t => { throw new Error(t); }))
                  .then(d => {
                      tr.dataset.id = d.id; rt.id = d.id; rt.key = key;
                      tr.dataset.sortOrder = payload.sort_order;
                      tdKey.replaceChildren(document.createTextNode(key));
                      showToast('Reward tier created.');
                      reloadActiveSeasonTiers();
                  })
                  .catch(err => showToast(err.message || 'Create failed.', 'error'));
            }
        });
        tdAct.appendChild(saveBtn);

        const delBtn = document.createElement('button');
        delBtn.type = 'button'; delBtn.className = 'btn btn-danger btn-sm';
        delBtn.setAttribute('aria-label', 'Delete'); delBtn.appendChild(svgIcon('x'));
        delBtn.style.marginLeft = '4px';
        delBtn.addEventListener('click', () => {
            if (!rt.id) { destroyTierRow(tr); return; }
            delBtn.style.display = 'none';
            const cs = document.createElement('span');
            cs.style.cssText = 'display:inline-flex;gap:4px;align-items:center;margin-left:4px;';
            const yBtn = document.createElement('button');
            yBtn.type = 'button'; yBtn.className = 'btn btn-danger btn-sm'; yBtn.textContent = 'Delete';
            yBtn.addEventListener('click', () => {
                fetch('/api/season-hub/reward-tiers/' + rt.id, { method: 'DELETE' })
                    .then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }))
                    .then(() => { destroyTierRow(tr); showToast('Reward tier deleted.'); reloadActiveSeasonTiers(); })
                    .catch(err => { cs.remove(); delBtn.style.display = ''; showToast(err.message || 'Delete failed.', 'error'); });
            });
            const nBtn = document.createElement('button');
            nBtn.type = 'button'; nBtn.className = 'btn btn-secondary btn-sm'; nBtn.textContent = 'No';
            nBtn.addEventListener('click', () => { cs.remove(); delBtn.style.display = ''; });
            cs.append(yBtn, nBtn);
            tdAct.appendChild(cs);
        });
        tdAct.appendChild(delBtn);
        tr.appendChild(tdAct);
        return tr;
    }

    // Called after any structural change to the tier editor. The two halves are
    // deliberately independent: the colour preview needs Choices, the reorder
    // end-stops do not, so a missing Choices tag must not take the buttons with
    // it. ColorPicker.upgradeAll is itself a no-op without Choices.
    function refreshTierEditor() {
        const tbody = document.getElementById('es-reward-tiers-tbody');
        if (!tbody) return;
        ColorPicker.upgradeAll(tbody, '.rt-color', 'tier-color-choices');
        refreshOrderButtons(tbody);
    }

    // Choices leaves its wrapper behind if the row is removed without this.
    function destroyTierRow(tr) {
        ColorPicker.destroyIn(tr, '.rt-color');
        const tbody = tr.parentNode;
        tr.remove();
        if (tbody) refreshOrderButtons(tbody);
    }

    // A move is only meaningful once every row has an id, so unsaved new rows are
    // skipped — they pick up their position when they are first saved. Sends one
    // PUT per row whose position actually changed.
    function persistTierOrder() {
        const tbody = document.getElementById('es-reward-tiers-tbody');
        if (!tbody) return;
        const rows = Array.from(tbody.children);
        const writes = rows.map((tr, i) => {
            const id = tr.dataset.id;
            if (!id) return null;
            if (String(tr.dataset.sortOrder) === String(i)) return null;
            const label = tr.querySelector('.rt-label').value.trim();
            if (!label) return null;
            tr.dataset.sortOrder = i;
            return fetch('/api/season-hub/reward-tiers/' + id, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    label,
                    slot_count: parseInt(tr.querySelector('.rt-slots').value, 10) || 0,
                    color: tr.querySelector('.rt-color').value,
                    sort_order: i,
                }),
            }).then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }));
        }).filter(Boolean);

        if (writes.length === 0) return;
        Promise.all(writes)
            .then(() => reloadActiveSeasonTiers())
            .catch(err => showToast(err.message || 'Reorder failed.', 'error'));
    }

    // Tier edits are saved per row while the modal is open, so the badges and
    // slot chips already on the page behind it would otherwise show stale
    // labels, colours and caps until the next full reload.
    function reloadActiveSeasonTiers() {
        if (!activeSeason) return;
        fetch('/api/season-hub/reward-tiers?season_id=' + activeSeason.id)
            .then(r => r.ok ? r.json() : null)
            .then(d => {
                if (!d) return;
                setRewardTiers(d.reward_tiers || []);
                if (activeSeason) activeSeason.reward_tiers = rewardTiers;
                renderRankingsTable();
                if (CAN_MANAGE) renderRewardsTable();
            })
            .catch(() => {});
    }

    function buildEsTrackableRow(tk, seasonId) {
        tk = tk || {};
        const tr = document.createElement('tr');
        tr.dataset.id = tk.id || '';

        const tdKey = document.createElement('td');
        if (tk.id) {
            tdKey.textContent = tk.key;
        } else {
            const inp = document.createElement('input');
            inp.type = 'text'; inp.className = 'form-input tk-key'; inp.placeholder = 'key (e.g. war_merit)';
            tdKey.appendChild(inp);
        }
        tr.appendChild(tdKey);

        const tdLabel = document.createElement('td');
        const inpLabel = document.createElement('input');
        inpLabel.type = 'text'; inpLabel.className = 'form-input tk-label'; inpLabel.value = tk.label || ''; inpLabel.placeholder = 'Label';
        tdLabel.appendChild(inpLabel);
        tr.appendChild(tdLabel);

        const tdAct = document.createElement('td');
        tdAct.style.cssText = 'white-space:nowrap;';

        const saveBtn = document.createElement('button');
        saveBtn.type = 'button'; saveBtn.className = 'btn btn-primary btn-sm'; saveBtn.textContent = 'Save';
        saveBtn.addEventListener('click', () => {
            const label = inpLabel.value.trim();
            if (!label) { showToast('Label required.', 'error'); return; }
            if (tk.id) {
                fetch('/api/season-hub/trackables/' + tk.id, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ label })
                }).then(r => r.ok ? showToast('Trackable saved.') : r.text().then(t => { throw new Error(t); }))
                  .catch(err => showToast(err.message || 'Save failed.', 'error'));
            } else {
                const keyEl = tdKey.querySelector('.tk-key');
                const key = keyEl ? keyEl.value.trim() : '';
                if (!key) { showToast('Key required.', 'error'); return; }
                fetch('/api/season-hub/trackables', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ season_id: seasonId, key, label, sort_order: tr.rowIndex || 0 })
                }).then(r => r.ok ? r.json() : r.text().then(t => { throw new Error(t); }))
                  .then(d => {
                      tr.dataset.id = d.id; tk.id = d.id;
                      tdKey.replaceChildren(document.createTextNode(key));
                      showToast('Trackable created.');
                  })
                  .catch(err => showToast(err.message || 'Create failed.', 'error'));
            }
        });
        tdAct.appendChild(saveBtn);

        const delBtn = document.createElement('button');
        delBtn.type = 'button'; delBtn.className = 'btn btn-danger btn-sm'; delBtn.setAttribute('aria-label', 'Delete'); delBtn.appendChild(svgIcon('x'));
        delBtn.style.marginLeft = '4px';
        delBtn.addEventListener('click', () => {
            if (!tk.id) { tr.remove(); return; }
            delBtn.style.display = 'none';
            const cs = document.createElement('span');
            cs.style.cssText = 'display:inline-flex;gap:4px;align-items:center;margin-left:4px;';
            const yBtn = document.createElement('button');
            yBtn.type = 'button'; yBtn.className = 'btn btn-danger btn-sm'; yBtn.textContent = 'Delete';
            yBtn.addEventListener('click', () => {
                fetch('/api/season-hub/trackables/' + tk.id, { method: 'DELETE' })
                    .then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }))
                    .then(() => { tr.remove(); showToast('Trackable deleted.'); })
                    .catch(err => { cs.remove(); delBtn.style.display = ''; showToast(err.message || 'Delete failed.', 'error'); });
            });
            const nBtn = document.createElement('button');
            nBtn.type = 'button'; nBtn.className = 'btn btn-secondary btn-sm'; nBtn.textContent = 'No';
            nBtn.addEventListener('click', () => { cs.remove(); delBtn.style.display = ''; });
            cs.append(yBtn, nBtn);
            tdAct.appendChild(cs);
        });
        tdAct.appendChild(delBtn);
        tr.appendChild(tdAct);
        return tr;
    }

    function buildEsEventRow(ev, seasonId) {
        ev = ev || {};
        const tr = document.createElement('tr');
        tr.dataset.id = ev.id || '';

        function mkTd(child) { const td = document.createElement('td'); td.appendChild(child); return td; }

        const inLabel = document.createElement('input');
        inLabel.type = 'text'; inLabel.className = 'form-input ev-label'; inLabel.value = ev.label || ''; inLabel.placeholder = 'Event name';
        tr.appendChild(mkTd(inLabel));

        const selType = document.createElement('select');
        selType.className = 'form-input ev-type';
        const optNone = document.createElement('option'); optNone.value = ''; optNone.textContent = '— none —';
        selType.appendChild(optNone);
        csEventTypes.forEach(et => {
            const opt = document.createElement('option');
            opt.value = et.id; opt.textContent = et.name;
            if (ev.event_type_id && et.id === ev.event_type_id) opt.selected = true;
            selType.appendChild(opt);
        });
        tr.appendChild(mkTd(selType));

        const inDay = document.createElement('input');
        inDay.type = 'number'; inDay.className = 'form-input ev-day'; inDay.min = '1'; inDay.max = '7';
        inDay.placeholder = 'blank=unsched'; inDay.style.width = '80px';
        if (ev.day_offset != null) inDay.value = ev.day_offset;
        tr.appendChild(mkTd(inDay));

        const inTime = document.createElement('input');
        inTime.type = 'text'; inTime.className = 'form-input ev-time'; inTime.value = ev.event_time || '20:00'; inTime.style.width = '90px';
        flatpickr(inTime, { enableTime: true, noCalendar: true, dateFormat: 'H:i', time_24hr: true, minuteIncrement: 30, allowInput: true });
        tr.appendChild(mkTd(inTime));

        const inWkS = document.createElement('input');
        inWkS.type = 'number'; inWkS.className = 'form-input ev-wk-start';
        // No min: pre-season events use 0 (or negative) for weeks before season start.
        inWkS.value = ev.week_start != null ? ev.week_start : 1; inWkS.style.width = '55px';
        tr.appendChild(mkTd(inWkS));

        const inWkE = document.createElement('input');
        inWkE.type = 'number'; inWkE.className = 'form-input ev-wk-end'; inWkE.min = '0'; inWkE.value = ev.week_end !== undefined ? ev.week_end : 0; inWkE.style.width = '55px';
        tr.appendChild(mkTd(inWkE));

        const inLevel = document.createElement('input');
        inLevel.type = 'number'; inLevel.className = 'form-input ev-level'; inLevel.min = '1';
        inLevel.placeholder = '—'; inLevel.style.width = '50px';
        if (ev.level != null) inLevel.value = ev.level;
        tr.appendChild(mkTd(inLevel));

        const inNotes = document.createElement('input');
        inNotes.type = 'text'; inNotes.className = 'form-input ev-notes'; inNotes.value = ev.notes || '';
        tr.appendChild(mkTd(inNotes));

        const chkServer = document.createElement('input');
        chkServer.type = 'checkbox'; chkServer.className = 'ev-server';
        chkServer.title = 'Server event';
        chkServer.style.cssText = 'width:16px;height:16px;cursor:pointer;';
        chkServer.checked = !!ev.is_server_event;
        tr.appendChild(mkTd(chkServer));

        const inDuration = document.createElement('input');
        inDuration.type = 'number'; inDuration.className = 'form-input ev-duration'; inDuration.min = '1';
        inDuration.value = ev.duration_days || 1; inDuration.style.width = '50px';
        tr.appendChild(mkTd(inDuration));

        // Provenance is read here and edited in Settings → Season Templates; the row
        // carries it on every save so a Save never strips it.
        tr.appendChild(mkTd(makeGradeBadge(ev.grade || 'unverified', ev.source)));

        const tdAct = document.createElement('td');
        tdAct.style.cssText = 'white-space:nowrap;';

        function collectEvData() {
            const dayVal = inDay.value;
            const lvlVal = inLevel.value.trim();
            const wkSParsed = parseInt(inWkS.value, 10);
            const wkEParsed = parseInt(inWkE.value, 10);
            return {
                season_id:       seasonId,
                label:           inLabel.value.trim(),
                event_type_id:   selType.value ? parseInt(selType.value, 10) : null,
                day_offset:      dayVal !== '' ? (parseInt(dayVal, 10) || null) : null,
                event_time:      inTime.value || '20:00',
                // Preserve 0 / negative values for pre-season events.
                week_start:      Number.isFinite(wkSParsed) ? wkSParsed : 1,
                week_end:        Number.isFinite(wkEParsed) ? wkEParsed : 0,
                level:           lvlVal !== '' ? parseInt(lvlVal, 10) : null,
                notes:           inNotes.value || '',
                is_server_event: chkServer.checked,
                duration_days:   parseInt(inDuration.value, 10) || 1,
                template_key:    ev.template_key || '',
                source:          ev.source || '',
                grade:           ev.grade || 'unverified',
            };
        }

        const saveBtn = document.createElement('button');
        saveBtn.type = 'button'; saveBtn.className = 'btn btn-primary btn-sm'; saveBtn.textContent = 'Save';
        saveBtn.addEventListener('click', () => {
            const payload = collectEvData();
            if (!payload.label) { showToast('Label required.', 'error'); return; }
            if (ev.id) {
                fetch('/api/season-hub/season-events/' + ev.id, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                }).then(r => r.ok ? showToast('Event saved.') : r.text().then(t => { throw new Error(t); }))
                  .catch(err => showToast(err.message || 'Save failed.', 'error'));
            } else {
                fetch('/api/season-hub/season-events', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                }).then(r => r.ok ? r.json() : r.text().then(t => { throw new Error(t); }))
                  .then(d => { tr.dataset.id = d.id; ev.id = d.id; showToast('Event created.'); })
                  .catch(err => showToast(err.message || 'Create failed.', 'error'));
            }
        });
        tdAct.appendChild(saveBtn);

        const delBtn = document.createElement('button');
        delBtn.type = 'button'; delBtn.className = 'btn btn-danger btn-sm'; delBtn.setAttribute('aria-label', 'Delete'); delBtn.appendChild(svgIcon('x'));
        delBtn.style.marginLeft = '4px';
        delBtn.addEventListener('click', () => {
            if (!ev.id) { tr.remove(); return; }
            delBtn.style.display = 'none';
            const cs = document.createElement('span');
            cs.style.cssText = 'display:inline-flex;gap:4px;align-items:center;margin-left:4px;';
            const yBtn = document.createElement('button');
            yBtn.type = 'button'; yBtn.className = 'btn btn-danger btn-sm'; yBtn.textContent = 'Delete';
            yBtn.addEventListener('click', () => {
                fetch('/api/season-hub/season-events/' + ev.id, { method: 'DELETE' })
                    .then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }))
                    .then(() => { tr.remove(); showToast('Event deleted.'); })
                    .catch(err => { cs.remove(); delBtn.style.display = ''; showToast(err.message || 'Delete failed.', 'error'); });
            });
            const nBtn = document.createElement('button');
            nBtn.type = 'button'; nBtn.className = 'btn btn-secondary btn-sm'; nBtn.textContent = 'No';
            nBtn.addEventListener('click', () => { cs.remove(); delBtn.style.display = ''; });
            cs.append(yBtn, nBtn);
            tdAct.appendChild(cs);
        });
        tdAct.appendChild(delBtn);
        tr.appendChild(tdAct);
        return tr;
    }

    // ── Provenance grades ─────────────────────────────────────────────────────
    // The grade says where a season fact came from (migration 088); the source text is
    // the badge's title. In-game observation outranks every one of them.
    const GRADE_LABELS = {
        official: 'Official', cpt_hedge: 'Cpt Hedge', guide: 'Guide', owner: 'Owner',
        requester: 'Requester', conflict: 'Conflict', unverified: 'Unverified',
    };
    function makeGradeBadge(grade, source) {
        const g = GRADE_LABELS[grade] ? grade : 'unverified';
        const span = document.createElement('span');
        span.className = 'grade-badge grade-' + g;
        span.textContent = GRADE_LABELS[g];
        if (source) span.title = source;
        return span;
    }

    // ── Template-change notice ───────────────────────────────────────────────
    // A corrected template never rewrites a season made from it (migration 088). The
    // notice lists what differs and applies a row at a time through the ordinary
    // season-event endpoints; pushed calendar rows don't move — the next push reports
    // any drift, as always.
    const DAY_NAMES = ['', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
    let esEvents = [];   // the open modal's season_events rows, as last loaded

    function describeTemplateRow(t) {
        const day = t.day_offset ? DAY_NAMES[t.day_offset] || ('day ' + t.day_offset) : 'unscheduled';
        const weeks = t.week_end > t.week_start ? 'W' + t.week_start + '–' + t.week_end
            : t.week_end < t.week_start && t.week_start >= 1 ? 'W' + t.week_start + '+' : 'W' + t.week_start;
        return t.label + ' · ' + weeks + ' ' + day + (t.day_offset ? ' ' + t.event_time : '');
    }

    function describeField(f) {
        const show = (field, v) => {
            if (v === '') return '—';
            if (field === 'day') return DAY_NAMES[parseInt(v, 10)] || v;
            if (field === 'grade') return GRADE_LABELS[v] || v;
            return v;
        };
        const names = { week_start: 'first week', week_end: 'last week', server_event: 'server event',
            duration_days: 'days', template_key: 'key' };
        return (names[f.field] || f.field) + ': ' + show(f.field, f.season) + ' → ' + show(f.field, f.template);
    }

    function templatePayload(t, base) {
        let typeId = t.event_type_id != null ? t.event_type_id : null;
        if (typeId == null && base && base.type_name === t.type_name) typeId = base.event_type_id;
        return {
            season_id:       activeSeason.id,
            label:           t.label,
            event_type_id:   typeId,
            day_offset:      t.day_offset != null ? t.day_offset : null,
            event_time:      t.event_time || '20:00',
            all_day:         base ? !!base.all_day : false,
            level:           t.level != null ? t.level : (base ? base.level : null),
            week_start:      t.week_start,
            week_end:        t.week_end,
            notes:           t.notes || '',
            is_server_event: !!t.is_server_event,
            duration_days:   t.duration_days || 1,
            template_key:    t.key || '',
            source:          t.source || '',
            grade:           t.grade || 'unverified',
        };
    }

    // An alliance row needs a schedule event type to reach the calendar; a template type
    // no install has created yet would save as untyped and be skipped by every push.
    function missingType(t) {
        return !t.is_server_event && t.type_name && t.event_type_id == null;
    }

    function applyTemplateRow(t, seasonEventId) {
        if (missingType(t)) {
            showToast('The event type "' + t.type_name + '" does not exist yet. Settings → Season Templates → Sync Event Types first.', 'error', 7000);
            return Promise.resolve(false);
        }
        const base = seasonEventId ? esEvents.find(e => e.id === seasonEventId) : null;
        const url = seasonEventId ? '/api/season-hub/season-events/' + seasonEventId : '/api/season-hub/season-events';
        return fetch(url, {
            method: seasonEventId ? 'PUT' : 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(templatePayload(t, base)),
        }).then(r => r.ok ? true : r.text().then(txt => { throw new Error(txt); }));
    }

    function loadTemplateDiff(season) {
        const box = document.getElementById('es-template-diff');
        if (!box) return;
        fetch('/api/season-hub/seasons/' + season.id + '/template-diff')
            .then(r => r.ok ? r.json() : null)
            .then(d => {
                if (!d || !activeSeason || activeSeason.id !== season.id) return;
                renderTemplateDiff(box, season, d);
            })
            .catch(() => {});
    }

    function renderTemplateDiff(box, season, d) {
        box.replaceChildren();
        if (d.error) {
            const p = document.createElement('p');
            p.textContent = d.error;
            box.appendChild(p);
            box.style.display = '';
            return;
        }
        if (!d.diff || d.empty) { box.style.display = 'none'; return; }
        const diff = d.diff;
        const real = diff.changed.filter(c => !c.provenance_only);
        const provOnly = diff.changed.filter(c => c.provenance_only);
        const readOnly = !!d.archived;

        const summary = document.createElement('p');
        summary.className = 'es-diff-summary';
        const parts = [];
        if (real.length) parts.push(real.length + ' row' + (real.length === 1 ? '' : 's') + ' differ');
        if (diff.added.length) parts.push(diff.added.length + ' new row' + (diff.added.length === 1 ? '' : 's'));
        if (diff.removed.length) parts.push(diff.removed.length + ' row' + (diff.removed.length === 1 ? ' is' : 's are') + ' not in the template');
        if (diff.ambiguous.length) parts.push(diff.ambiguous.length + ' to match by hand');
        if (provOnly.length) parts.push(provOnly.length + ' only gain a source and grade');
        setLabeledName(summary, 'This season\'s template has changed since the season was created: ', d.template.name);
        summary.appendChild(document.createTextNode(' — ' + parts.join(', ') + '.'));
        box.appendChild(summary);

        if (readOnly) {
            const note = document.createElement('p');
            note.className = 'es-diff-note';
            note.textContent = 'This season is archived, so its rows stay as they were.';
            box.appendChild(note);
        }

        const toggle = document.createElement('button');
        toggle.type = 'button';
        toggle.className = 'btn btn-secondary btn-sm';
        toggle.textContent = 'Review changes';
        const list = document.createElement('ul');
        list.className = 'es-diff-list';
        list.style.display = 'none';
        toggle.addEventListener('click', () => {
            const open = list.style.display === 'none';
            list.style.display = open ? '' : 'none';
            toggle.textContent = open ? 'Hide changes' : 'Review changes';
        });
        box.appendChild(toggle);

        const after = () => reloadEsEvents(season);
        function item(text, detail, actionLabel, action) {
            const li = document.createElement('li');
            const head = document.createElement('div');
            head.className = 'es-diff-head';
            const label = document.createElement('span');
            label.className = 'es-diff-label';
            label.textContent = text;
            head.appendChild(label);
            if (action && !readOnly) {
                const btn = document.createElement('button');
                btn.type = 'button';
                btn.className = 'btn btn-primary btn-sm';
                btn.textContent = actionLabel;
                btn.addEventListener('click', () => {
                    btn.disabled = true;
                    Promise.resolve(action())
                        .then(ok => { if (ok !== false) { showToast('Applied.'); after(); } else btn.disabled = false; })
                        .catch(err => { btn.disabled = false; showToast(err.message || 'Apply failed.', 'error'); });
                });
                head.appendChild(btn);
            }
            li.appendChild(head);
            if (detail) {
                const det = document.createElement('div');
                det.className = 'es-diff-detail';
                det.textContent = detail;
                li.appendChild(det);
            }
            list.appendChild(li);
            return li;
        }

        real.forEach(c => item(c.label, c.fields.map(describeField).join('; '), 'Apply',
            () => applyTemplateRow(c.template, c.season_event_id)));
        diff.added.forEach(t => item('New: ' + describeTemplateRow(t), t.source ? 'Source: ' + t.source : '',
            'Add', () => applyTemplateRow(t, 0)));
        diff.removed.forEach(rm => item('Not in the template: ' + rm.label,
            'Deleting it keeps anything already on the calendar.', 'Delete', async () => {
                if (!await showConfirm('Delete "' + rm.label + '" from this season?', 'Delete')) return false;
                return fetch('/api/season-hub/season-events/' + rm.season_event_id, { method: 'DELETE' })
                    .then(r => r.ok ? true : r.text().then(txt => { throw new Error(txt); }));
            }));
        diff.ambiguous.forEach(a => {
            const li = item('Match by hand: ' + a.label, 'More than one template row could be this one.', null, null);
            if (readOnly) return;
            const pick = document.createElement('select');
            pick.className = 'form-input es-diff-pick';
            a.candidates.forEach((t, i) => {
                const opt = document.createElement('option');
                opt.value = String(i);
                opt.textContent = describeTemplateRow(t);
                pick.appendChild(opt);
            });
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'btn btn-primary btn-sm';
            btn.textContent = 'Apply';
            btn.addEventListener('click', () => {
                btn.disabled = true;
                applyTemplateRow(a.candidates[parseInt(pick.value, 10)], a.season_event_id)
                    .then(ok => { if (ok !== false) { showToast('Applied.'); after(); } else btn.disabled = false; })
                    .catch(err => { btn.disabled = false; showToast(err.message || 'Apply failed.', 'error'); });
            });
            const row = document.createElement('div');
            row.className = 'es-diff-pickrow';
            row.append(pick, btn);
            li.appendChild(row);
        });
        if (provOnly.length) {
            item(provOnly.length + ' row' + (provOnly.length === 1 ? '' : 's') + ' already match and only gain a source and grade',
                provOnly.map(c => c.label).join(', '), 'Apply all', () => {
                    return provOnly.reduce((p, c) => p.then(() => applyTemplateRow(c.template, c.season_event_id)), Promise.resolve(true));
                });
        }
        box.appendChild(list);
        box.style.display = '';
    }

    function reloadEsEvents(season) {
        return fetch('/api/season-hub/season-events?season_id=' + season.id)
            .then(r => r.json())
            .then(evData => {
                esEvents = evData.events || [];
                const evBody = document.getElementById('es-events-tbody');
                if (evBody) evBody.replaceChildren(...esEvents.map(ev => buildEsEventRow(ev, season.id)));
                loadTemplateDiff(season);
            })
            .catch(() => {});
    }

    // ── Edit season ───────────────────────────────────────────────────────────
    const btnEditSeason = document.getElementById('btn-edit-season');
    const btnCancelEditSeason = document.getElementById('btn-cancel-edit-season');
    const formEditSeason = document.getElementById('form-edit-season');

    if (btnEditSeason) {
        btnEditSeason.addEventListener('click', () => {
            if (!activeSeason) return;
            const s = activeSeason;
            document.getElementById('es-name').value = s.name;
            document.getElementById('es-number').value = s.season_number;
            document.getElementById('es-start-date').value = s.start_date;
            document.getElementById('es-week-count').value = s.week_count;
            document.getElementById('es-key-event-name').value = s.key_event_name;
            document.getElementById('es-key-event-required').value = s.key_event_required;
            document.getElementById('es-tier-active').value = s.tier_active_min_pct;
            document.getElementById('es-tier-at-risk').value = s.tier_at_risk_min_pct;
            document.getElementById('es-key-event-total').value = s.key_event_total || 0;
            document.getElementById('es-resource-name').value = s.resource_name || '';
            const optVal = v => (v === null || v === undefined) ? '' : v;
            document.getElementById('es-poll-active').value = optVal(s.poll_active_min_pct);
            document.getElementById('es-poll-at-risk').value = optVal(s.poll_at_risk_min_pct);
            document.getElementById('es-war-active').value = optVal(s.war_active_min_pct);
            document.getElementById('es-war-at-risk').value = optVal(s.war_at_risk_min_pct);
            document.getElementById('es-schedule-buffer').value = s.schedule_buffer_minutes;
            document.getElementById('es-land-buffer').value = s.land_readiness_buffer_hours;
            loadLayoutEditor(s);
            const errEl = document.getElementById('es-error');
            if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }
            const diffBox = document.getElementById('es-template-diff');
            if (diffBox) { diffBox.replaceChildren(); diffBox.style.display = 'none'; }

            // Load trackables and events, then open modal
            fetchEventTypes().then(() => {
                Promise.all([
                    fetch('/api/season-hub/trackables?season_id=' + s.id).then(r => r.json()),
                    fetch('/api/season-hub/season-events?season_id=' + s.id).then(r => r.json()),
                    fetch('/api/season-hub/reward-tiers?season_id=' + s.id).then(r => r.json()),
                ]).then(([tkData, evData, rtData]) => {
                    const tkBody = document.getElementById('es-trackables-tbody');
                    if (tkBody) tkBody.replaceChildren(...(tkData.trackables || []).map(t => buildEsTrackableRow(t, s.id)));
                    esEvents = evData.events || [];
                    const evBody = document.getElementById('es-events-tbody');
                    if (evBody) evBody.replaceChildren(...esEvents.map(ev => buildEsEventRow(ev, s.id)));
                    loadTemplateDiff(s);
                    const rtBody = document.getElementById('es-reward-tiers-tbody');
                    if (rtBody) {
                        rtBody.replaceChildren(...(rtData.reward_tiers || []).map(t => buildEsRewardTierRow(t, s.id)));
                        refreshTierEditor();
                    }
                }).catch(() => {});
                const modal = document.getElementById('modal-edit-season');
                if (modal) modal.style.display = 'flex';
            });
        });
    }

    if (btnCancelEditSeason) {
        btnCancelEditSeason.addEventListener('click', () => {
            const modal = document.getElementById('modal-edit-season');
            if (modal) modal.style.display = '';
        });
    }

    const btnEsAddRewardTier = document.getElementById('btn-es-add-reward-tier');
    if (btnEsAddRewardTier) {
        btnEsAddRewardTier.addEventListener('click', () => {
            const tb = document.getElementById('es-reward-tiers-tbody');
            if (tb && activeSeason) {
                tb.appendChild(buildEsRewardTierRow({}, activeSeason.id));
                refreshTierEditor();
            }
        });
    }

    const btnEsAddTrackable = document.getElementById('btn-es-add-trackable');
    if (btnEsAddTrackable) {
        btnEsAddTrackable.addEventListener('click', () => {
            const tb = document.getElementById('es-trackables-tbody');
            if (tb && activeSeason) tb.appendChild(buildEsTrackableRow({}, activeSeason.id));
        });
    }

    const btnEsAddEvent = document.getElementById('btn-es-add-event');
    if (btnEsAddEvent) {
        btnEsAddEvent.addEventListener('click', () => {
            const tb = document.getElementById('es-events-tbody');
            if (tb && activeSeason) tb.appendChild(buildEsEventRow({}, activeSeason.id));
        });
    }

    const btnEsPushSchedule = document.getElementById('btn-es-push-schedule');
    if (btnEsPushSchedule) {
        btnEsPushSchedule.addEventListener('click', () => {
            if (!activeSeason) return;
            const statusEl = document.getElementById('es-push-status');
            if (statusEl) { statusEl.textContent = 'Pushing…'; statusEl.style.color = 'var(--color-text-muted)'; }
            setButtonLoading(btnEsPushSchedule);
            fetch('/api/season-hub/season-events/push', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ season_id: activeSeason.id })
            })
                .then(r => r.ok ? r.json() : r.text().then(t => { throw new Error(t); }))
                .then(d => {
                    let msg = d.created + ' event' + (d.created !== 1 ? 's' : '') + ' created';
                    if (d.skipped > 0) msg += ', ' + d.skipped + ' already existed';
                    if (d.skipped_unscheduled > 0) msg += ', ' + d.skipped_unscheduled + ' skipped (no day set)';
                    if (d.skipped_no_type > 0) msg += ', ' + d.skipped_no_type + ' skipped (no event type — set it in Edit Season)';
                    if (d.skipped_invalid > 0) {
                        msg += ', ' + d.skipped_invalid + ' skipped as invalid';
                        const shown = (d.invalid || []).slice(0, 3)
                            .map(iv => iv.date + ' (' + iv.reason + ')').join('; ');
                        if (shown) msg += ': ' + shown;
                    }
                    // Rows sitting away from the date the template now computes —
                    // almost always because the season's start_date moved after
                    // they were pushed. Reported, never moved: the officer may
                    // have placed one deliberately, and the app cannot tell.
                    if (d.drifted > 0) {
                        msg += ', ' + d.drifted + ' drifted from the template date';
                        const shownDrift = (d.drifted_rows || []).slice(0, 3)
                            .map(r => r.label + ' (week ' + r.week + ') expected ' + r.expected + ', is ' + r.actual)
                            .join('; ');
                        if (shownDrift) msg += ': ' + shownDrift;
                    }
                    const incomplete = d.skipped_no_type > 0 || d.skipped_invalid > 0 || d.drifted > 0;
                    if (statusEl) {
                        statusEl.textContent = msg;
                        statusEl.style.color = incomplete ? 'var(--color-warning)' : 'var(--color-success)';
                    }
                    showToast(msg, incomplete ? 'info' : 'success');
                })
                .catch(err => {
                    if (statusEl) { statusEl.textContent = err.message || 'Push failed.'; statusEl.style.color = 'var(--color-danger)'; }
                    showToast(err.message || 'Push failed.', 'error');
                })
                .finally(() => clearButtonLoading(btnEsPushSchedule));
        });
    }

    if (formEditSeason) {
        formEditSeason.addEventListener('submit', e => {
            e.preventDefault();
            if (!activeSeason) return;
            const errEl = document.getElementById('es-error');
            const submitBtn = formEditSeason.querySelector('button[type="submit"]');
            const body = {
                name: document.getElementById('es-name').value.trim(),
                season_number: parseInt(document.getElementById('es-number').value, 10),
                start_date: document.getElementById('es-start-date').value,
                week_count: parseInt(document.getElementById('es-week-count').value, 10),
                key_event_name: document.getElementById('es-key-event-name').value.trim(),
                key_event_required: parseInt(document.getElementById('es-key-event-required').value, 10) || 0,
                tier_active_min_pct: parseInt(document.getElementById('es-tier-active').value, 10) || 70,
                tier_at_risk_min_pct: parseInt(document.getElementById('es-tier-at-risk').value, 10) || 60,
                key_event_total: parseInt(document.getElementById('es-key-event-total').value, 10) || 0,
                resource_name: document.getElementById('es-resource-name').value.trim(),
                schedule_buffer_minutes: parseInt(document.getElementById('es-schedule-buffer').value, 10) || 0,
                land_readiness_buffer_hours: parseInt(document.getElementById('es-land-buffer').value, 10) || 0,
            };
            // A blank category floor is sent as null: "use the overall floor".
            [['poll_active_min_pct', 'es-poll-active'], ['poll_at_risk_min_pct', 'es-poll-at-risk'],
             ['war_active_min_pct', 'es-war-active'], ['war_at_risk_min_pct', 'es-war-at-risk']].forEach(([k, id]) => {
                const v = document.getElementById(id).value.trim();
                body[k] = v === '' ? null : parseInt(v, 10);
            });
            setButtonLoading(submitBtn);
            fetch('/api/season-hub/seasons/' + activeSeason.id, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            })
                .then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }))
                .then(() => {
                    const modal = document.getElementById('modal-edit-season');
                    if (modal) modal.style.display = '';
                    showToast('Season updated.');
                    loadSeasonList();
                })
                .catch(err => {
                    if (errEl) { errEl.textContent = err.message || 'Update failed.'; errEl.style.display = ''; }
                })
                .finally(() => clearButtonLoading(submitBtn));
        });
    }

    // ── Layout editor (Edit Season → Layout) ─────────────────────────────────
    function buildLayoutRow(item, tbody) {
        const tr = document.createElement('tr');
        tr.dataset.key = item.key;
        const tdShow = document.createElement('td');
        const chk = document.createElement('input');
        chk.type = 'checkbox';
        chk.className = 'layout-visible';
        chk.checked = !!item.visible;
        chk.setAttribute('aria-label', 'Show ' + item.default_label);
        tdShow.appendChild(chk);
        const tdLabel = document.createElement('td');
        const inp = document.createElement('input');
        inp.type = 'text';
        inp.className = 'form-input layout-label';
        inp.maxLength = 60;
        inp.placeholder = item.default_label;
        inp.value = item.label || '';
        tdLabel.appendChild(inp);
        const tdOrder = document.createElement('td');
        tdOrder.appendChild(buildOrderButtons(tr, () => refreshOrderButtons(tbody)));
        tr.append(tdShow, tdLabel, tdOrder);
        return tr;
    }

    function loadLayoutEditor(season) {
        const tabsBody = document.querySelector('#es-layout-tabs tbody');
        const cardsBody = document.querySelector('#es-layout-cards tbody');
        if (!tabsBody || !cardsBody) return;
        fetch('/api/season-hub/seasons/' + season.id + '/layout')
            .then(r => r.ok ? r.json() : null)
            .then(d => {
                if (!d) return;
                tabsBody.replaceChildren(...d.tabs.map(t => buildLayoutRow(t, tabsBody)));
                cardsBody.replaceChildren(...d.cards.map(c => buildLayoutRow(c, cardsBody)));
                refreshOrderButtons(tabsBody);
                refreshOrderButtons(cardsBody);
            })
            .catch(() => {});
    }

    const btnSaveLayout = document.getElementById('btn-es-save-layout');
    if (btnSaveLayout) {
        btnSaveLayout.addEventListener('click', () => {
            if (!activeSeason) return;
            const read = sel => Array.from(document.querySelectorAll(sel + ' tbody tr')).map(tr => ({
                key: tr.dataset.key,
                visible: tr.querySelector('.layout-visible').checked,
                label: tr.querySelector('.layout-label').value.trim(),
            }));
            const statusEl = document.getElementById('es-layout-status');
            setButtonLoading(btnSaveLayout);
            fetch('/api/season-hub/seasons/' + activeSeason.id + '/layout', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ tabs: read('#es-layout-tabs'), cards: read('#es-layout-cards') }),
            })
                .then(r => r.ok ? r.json() : r.text().then(t => { throw new Error(t); }))
                .then(() => {
                    if (statusEl) { statusEl.textContent = 'Layout saved.'; statusEl.style.color = 'var(--color-success)'; }
                    loadSeasonData(activeSeason.id);
                })
                .catch(err => {
                    if (statusEl) { statusEl.textContent = err.message || 'Save failed.'; statusEl.style.color = 'var(--color-danger)'; }
                })
                .finally(() => clearButtonLoading(btnSaveLayout));
        });
    }

    // ── Delete season ─────────────────────────────────────────────────────────
    const btnDeleteSeason = document.getElementById('btn-delete-season');

    if (btnDeleteSeason) {
        btnDeleteSeason.addEventListener('click', async () => {
            if (!activeSeason) return;
            if (!await showConfirm(
                'Permanently delete "' + activeSeason.name + '"? This removes all participation, contribution, and reward data for this season.',
                'Delete Season'
            )) return;
            fetch('/api/season-hub/seasons/' + activeSeason.id, { method: 'DELETE' })
                .then(r => {
                    if (!r.ok) return r.text().then(t => { throw new Error(t); });
                    return r.json();
                })
                .then(res => {
                    // Calendar events with a recorded participation board stay on the
                    // schedule (a board is history); say so rather than leave them
                    // looking like a purge that missed.
                    const kept = (res && res.kept_with_boards) || 0;
                    showToast(kept
                        ? `Season deleted. ${kept} schedule event${kept === 1 ? '' : 's'} with a participation board ${kept === 1 ? 'was' : 'were'} kept.`
                        : 'Season deleted.', 'success', kept ? 7000 : undefined);
                    // Clear the deleted season's data; loadSeasonList selects the
                    // next season and reloads its data (rewards/mail lazy-load on
                    // tab focus). Don't fetch rewards/mail here — activeSeason still
                    // points at the just-deleted season until loadSeasonData resolves.
                    if (CAN_MANAGE) { allRewards = []; renderRewardsTable(); }
                    allMailItems = []; renderMailList();
                    loadSeasonList();
                })
                .catch(err => showToast(err.message || 'Delete failed.', 'error'));
        });
    }

    // Close modals on backdrop click
    document.querySelectorAll('.modal').forEach(modal => {
        modal.addEventListener('click', e => {
            if (e.target === modal) modal.style.display = '';
        });
    });

    // ── Helpers ───────────────────────────────────────────────────────────────
    function formatDate(iso) {
        if (!iso) return '';
        try {
            return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
        } catch (_) {
            return iso;
        }
    }

    // ── Init ──────────────────────────────────────────────────────────────────
    loadSeasonList();

})();
