/* season-hub.js — Season Hub page logic */

(function () {
    'use strict';

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
    let allMailItems = [];         // SeasonMailItem[]
    let contribPreviewData = null; // pending import data from preview
    let editingRewardId = null;    // id being edited, or null for create
    let rewardMemberChoices = null;

    // ── Rankings sort state ───────────────────────────────────────────────────
    let rankingsSortCol = 'rank_position';
    let rankingsSortAsc = true;

    // ── Tab switching ─────────────────────────────────────────────────────────
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(t => { t.style.display = 'none'; });
            btn.classList.add('active');
            const target = document.getElementById('tab-' + btn.dataset.tab);
            if (target) target.style.display = 'block';
        });
    });

    // Show initial active tab
    const activeTabBtn = document.querySelector('.tab-btn.active');
    if (activeTabBtn) {
        const initial = document.getElementById('tab-' + activeTabBtn.dataset.tab);
        if (initial) initial.style.display = 'block';
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
        fetch(url)
            .then(r => r.json())
            .then(data => {
                activeSeason = data.season || null;
                scoreLevels = (data.season && data.season.score_levels) || [];
                allMembers = data.members || [];

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
    const rankingsSearch = document.getElementById('rankings-search');
    if (rankingsSearch) {
        rankingsSearch.addEventListener('input', renderRankingsTable);
    }

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
        const countEl = document.getElementById('rankings-count');

        if (!activeSeason) {
            if (noSeasonMsg) noSeasonMsg.style.display = '';
            if (table) table.style.display = 'none';
            return;
        }
        if (noSeasonMsg) noSeasonMsg.style.display = 'none';
        if (table) table.style.display = '';

        const maxScorePts = scoreLevels.length > 0 ? Math.max(...scoreLevels.map(sl => sl.points)) : 0;
        const maxPts = activeSeason ? activeSeason.week_count * maxScorePts : 0;

        const query = (rankingsSearch ? rankingsSearch.value : '').toLowerCase();
        let filtered = allMembers.filter(m =>
            !query || m.name.toLowerCase().includes(query)
        );

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
                case 'reward_tier':       va = a.reward_tier || '';     vb = b.reward_tier || '';       break;
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

        if (countEl) countEl.textContent = filtered.length + ' member' + (filtered.length !== 1 ? 's' : '');

        const rows = filtered.map((m, i) => {
            const tr = document.createElement('tr');

            // Rank position (stable — always reflects default sort order)
            const tdPos = document.createElement('td');
            tdPos.className = 'col-rank';
            tdPos.textContent = rankPos[m.member_id];
            tr.appendChild(tdPos);

            // Name
            const tdName = document.createElement('td');
            tdName.textContent = m.name;
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

    function makeTierBadge(tier) {
        const span = document.createElement('span');
        span.className = 'tier-badge';
        if (tier === 'alliance_leader') { span.classList.add('alliance-leader'); span.textContent = 'Alliance Leader'; }
        else if (tier === 'core') { span.classList.add('core'); span.textContent = 'Core'; }
        else if (tier === 'elite') { span.classList.add('elite'); span.textContent = 'Elite'; }
        else if (tier === 'valued') { span.classList.add('valued'); span.textContent = 'Valued'; }
        else span.textContent = tier;
        return span;
    }

    // ── Participation tab ─────────────────────────────────────────────────────
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
            noteInput.className = 'note-input form-input';
            noteInput.value = e.note || '';
            noteInput.placeholder = 'Optional note';
            tdNote.appendChild(noteInput);
            tr.appendChild(tdNote);

            return tr;
        });

        tbody.replaceChildren(...rows);
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
            const note = tr.querySelector('.note-input').value;
            entries.push({ member_id: memberId, score, attended_key_event: attended, note });
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
            headRow.appendChild(th);
        });
    }

    function renderManualTable() {
        const tbody = document.getElementById('contrib-manual-tbody');
        if (!tbody) return;

        const trackables = activeSeason ? (activeSeason.trackables || []) : [];
        const query = (document.getElementById('contrib-manual-search') || {}).value || '';
        const members = allMembers.filter(m => m.rank !== 'EX' &&
            (!query || m.name.toLowerCase().includes(query.toLowerCase())));

        const rows = members.map(m => {
            const tr = document.createElement('tr');
            tr.dataset.memberId = m.member_id;

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
            })
            .catch(() => {}); // silent — table still usable without pre-populated values
    }

    // Reload values when week changes
    const contribWeekEl = document.getElementById('contrib-week');
    if (contribWeekEl) {
        contribWeekEl.addEventListener('change', loadContribValues);
    }

    // Search filter
    const contribManualSearch = document.getElementById('contrib-manual-search');
    if (contribManualSearch) {
        contribManualSearch.addEventListener('input', renderManualTable);
    }

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
        if (statusEl) { statusEl.textContent = commit ? 'Committing…' : 'Processing…'; statusEl.style.color = 'var(--text-muted)'; }

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
            const tdOcr = document.createElement('td');
            tdOcr.textContent = row.original_name;
            const tdMember = document.createElement('td');
            tdMember.textContent = row.member_name;
            const tdMatch = document.createElement('td');
            tdMatch.textContent = row.match_type;
            const tdPts = document.createElement('td');
            tdPts.textContent = (row.points || 0).toLocaleString();
            tr.append(tdOcr, tdMember, tdMatch, tdPts);
            return tr;
        });
        if (tbody) tbody.replaceChildren(...rows);

        if (unresolvedWrap && unresolvedList) {
            if (unresolved.length > 0) {
                unresolvedWrap.style.display = '';
                const rows = unresolved.map((row, idx) => {
                    const tr = document.createElement('tr');
                    tr.dataset.idx = idx;

                    const tdName = document.createElement('td');
                    tdName.textContent = row.original_name;

                    const tdPts = document.createElement('td');
                    tdPts.textContent = (row.points || 0).toLocaleString();

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
    function loadRewards() {
        if (!activeSeason) return;
        fetch('/api/season-hub/rewards?season_id=' + activeSeason.id)
            .then(r => r.json())
            .then(data => {
                allRewards = data.rewards || [];
                renderRewardsTable();
            })
            .catch(() => showToast('Failed to load rewards.', 'error'));
    }

    function renderRewardsTable() {
        const tbody = document.getElementById('rewards-tbody');
        if (!tbody) return;

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

            const tdName = document.createElement('td'); tdName.textContent = rw.member_name; tr.appendChild(tdName);
            const tdRank = document.createElement('td'); const rankChip4 = document.createElement('span'); rankChip4.className = `member-rank rank-${rw.member_rank}`; rankChip4.textContent = rw.member_rank; tdRank.appendChild(rankChip4); tr.appendChild(tdRank);
            const tdTier = document.createElement('td'); tdTier.appendChild(makeTierBadge(rw.reward_tier)); tr.appendChild(tdTier);
            const tdPart = document.createElement('td'); tdPart.textContent = (rw.participation_pct || 0).toFixed(1) + '%'; tr.appendChild(tdPart);
            const tdContrib = document.createElement('td'); tdContrib.textContent = rw.contribution_pct != null ? (rw.contribution_pct).toFixed(1) + '%' : '—'; tr.appendChild(tdContrib);
            const tdNote = document.createElement('td'); tdNote.textContent = rw.note || ''; tr.appendChild(tdNote);
            const tdBy = document.createElement('td'); tdBy.textContent = rw.logged_by || ''; tr.appendChild(tdBy);

            if (CAN_MANAGE_REWARDS) {
                const tdActions = document.createElement('td');
                const editBtn = document.createElement('button');
                editBtn.className = 'btn btn-secondary btn-sm';
                editBtn.textContent = 'Edit';
                editBtn.addEventListener('click', () => openRewardModal(rw));
                tdActions.appendChild(editBtn);

                // Inline delete confirm
                const delBtn = document.createElement('button');
                delBtn.className = 'btn btn-danger btn-sm';
                delBtn.textContent = 'Delete';
                delBtn.style.marginLeft = '4px';
                delBtn.addEventListener('click', () => {
                    delBtn.style.display = 'none';
                    const confirmSpan = document.createElement('span');
                    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;margin-left:4px;';
                    const label = document.createElement('span');
                    label.textContent = 'Sure?';
                    label.style.fontSize = '0.85rem';
                    const yesBtn = document.createElement('button');
                    yesBtn.className = 'btn btn-danger btn-sm';
                    yesBtn.textContent = 'Yes';
                    yesBtn.addEventListener('click', () => deleteReward(rw.id));
                    const noBtn = document.createElement('button');
                    noBtn.className = 'btn btn-secondary btn-sm';
                    noBtn.textContent = 'No';
                    noBtn.addEventListener('click', () => { confirmSpan.remove(); delBtn.style.display = ''; });
                    confirmSpan.append(label, yesBtn, noBtn);
                    tdActions.appendChild(confirmSpan);
                });
                tdActions.appendChild(delBtn);
                tr.appendChild(tdActions);
            }

            return tr;
        });

        tbody.replaceChildren(...rows);
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

        if (rw) {
            tierSel.value = rw.reward_tier;
            partPct.value = rw.participation_pct;
            contribPct.value = rw.contribution_pct != null ? rw.contribution_pct : '';
            noteEl.value = rw.note || '';
        } else {
            tierSel.value = 'valued';
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
        rewardMemberChoices = new Choices(rewardMemberEl, {
            searchEnabled: true, searchPlaceholderValue: 'Search…',
            itemSelectText: '', shouldSort: false, allowHTML: true,
        });
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
        fetch('/api/season-hub/season-mail?season_id=' + activeSeason.id)
            .then(r => r.json())
            .then(data => {
                allMailItems = data.items || [];
                renderMailList();
            })
            .catch(() => showToast('Failed to load season mail.', 'error'));
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

            // Copy button
            const copyBtn = document.createElement('button');
            copyBtn.className = 'btn btn-secondary btn-sm';
            copyBtn.textContent = 'Copy';
            copyBtn.addEventListener('click', () => {
                const text = item.content || '';
                if (navigator.clipboard) {
                    navigator.clipboard.writeText(text).then(() => {
                        copyBtn.textContent = 'Copied!';
                        setTimeout(() => { copyBtn.textContent = 'Copy'; }, 1800);
                    });
                } else {
                    const ta = document.createElement('textarea');
                    ta.value = text;
                    ta.style.cssText = 'position:fixed;opacity:0;pointer-events:none;';
                    document.body.appendChild(ta);
                    ta.select();
                    document.execCommand('copy');
                    ta.remove();
                    copyBtn.textContent = 'Copied!';
                    setTimeout(() => { copyBtn.textContent = 'Copy'; }, 1800);
                }
            });
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
                delBtn.addEventListener('click', () => {
                    delBtn.style.display = 'none';
                    const confirmSpan = document.createElement('span');
                    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
                    const label = document.createElement('span');
                    label.textContent = 'Sure?';
                    label.style.fontSize = '0.85rem';
                    const yesBtn = document.createElement('button');
                    yesBtn.className = 'btn btn-danger btn-sm';
                    yesBtn.textContent = 'Yes';
                    yesBtn.addEventListener('click', () => deleteMailItem(item.id));
                    const noBtn = document.createElement('button');
                    noBtn.className = 'btn btn-secondary btn-sm';
                    noBtn.textContent = 'No';
                    noBtn.addEventListener('click', () => { confirmSpan.remove(); delBtn.style.display = ''; });
                    confirmSpan.append(label, yesBtn, noBtn);
                    actions.appendChild(confirmSpan);
                });
                actions.appendChild(delBtn);
            }

            div.append(info, actions);

            // Content box (collapsed by default)
            const contentBox = document.createElement('pre');
            contentBox.className = 'mail-item-content';
            contentBox.style.display = 'none';
            contentBox.textContent = item.content || '';

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
            ? '/api/season-hub/season-mail/' + editingMailId
            : '/api/season-hub/season-mail/upload';
        const method = isEdit ? 'PUT' : 'POST';
        const body = isEdit
            ? { title: titleEl.value.trim(), content: contentEl ? contentEl.value : '' }
            : { season_id: activeSeason.id, title: titleEl.value.trim(), content: contentEl ? contentEl.value : '' };

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
        fetch('/api/season-hub/season-mail/' + id, { method: 'DELETE' })
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
        return fetch('/api/schedule/event-types')
            .then(r => r.ok ? r.json() : { event_types: [] })
            .then(d => { csEventTypes = d.event_types || []; return csEventTypes; })
            .catch(() => []);
    }

    function openCreateSeasonModal() {
        const errEl = document.getElementById('cs-error');
        if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }
        updateArchiveWarning();

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

        const body = {
            template_id: parseInt(tplSel?.value, 10) || 0,
            start_date: document.getElementById('cs-start-date').value,
            tier_active_min_pct: parseInt(document.getElementById('cs-tier-active').value, 10) || 70,
            tier_at_risk_min_pct: parseInt(document.getElementById('cs-tier-at-risk').value, 10) || 60,
            tier_count_leader: parseInt(document.getElementById('cs-tier-leader').value, 10) || 1,
            tier_count_core: parseInt(document.getElementById('cs-tier-core').value, 10) || 10,
            tier_count_elite: parseInt(document.getElementById('cs-tier-elite').value, 10) || 20,
            tier_count_valued: parseInt(document.getElementById('cs-tier-valued').value, 10) || 69,
        };

        if (!body.template_id) {
            if (errEl) { errEl.textContent = 'Please select a template.'; errEl.style.display = ''; }
            return;
        }
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
                    if (p.skipped_no_type) msg += ', ' + p.skipped_no_type + ' skipped (no event type — run Sync Event Types in Settings)';
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
        delBtn.type = 'button'; delBtn.className = 'btn btn-danger btn-sm'; delBtn.textContent = '✕';
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
        inWkS.type = 'number'; inWkS.className = 'form-input ev-wk-start'; inWkS.min = '1'; inWkS.value = ev.week_start || 1; inWkS.style.width = '55px';
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

        const tdAct = document.createElement('td');
        tdAct.style.cssText = 'white-space:nowrap;';

        function collectEvData() {
            const dayVal = inDay.value;
            const lvlVal = inLevel.value.trim();
            return {
                season_id:       seasonId,
                label:           inLabel.value.trim(),
                event_type_id:   selType.value ? parseInt(selType.value, 10) : null,
                day_offset:      dayVal !== '' ? (parseInt(dayVal, 10) || null) : null,
                event_time:      inTime.value || '20:00',
                week_start:      parseInt(inWkS.value, 10) || 1,
                week_end:        parseInt(inWkE.value, 10) || 0,
                level:           lvlVal !== '' ? parseInt(lvlVal, 10) : null,
                notes:           inNotes.value || '',
                is_server_event: chkServer.checked,
                duration_days:   parseInt(inDuration.value, 10) || 1,
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
        delBtn.type = 'button'; delBtn.className = 'btn btn-danger btn-sm'; delBtn.textContent = '✕';
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
            document.getElementById('es-tier-leader').value = s.tier_count_leader;
            document.getElementById('es-tier-core').value = s.tier_count_core;
            document.getElementById('es-tier-elite').value = s.tier_count_elite;
            document.getElementById('es-tier-valued').value = s.tier_count_valued;
            const errEl = document.getElementById('es-error');
            if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }

            // Load trackables and events, then open modal
            fetchEventTypes().then(() => {
                Promise.all([
                    fetch('/api/season-hub/trackables?season_id=' + s.id).then(r => r.json()),
                    fetch('/api/season-hub/season-events?season_id=' + s.id).then(r => r.json()),
                ]).then(([tkData, evData]) => {
                    const tkBody = document.getElementById('es-trackables-tbody');
                    if (tkBody) tkBody.replaceChildren(...(tkData.trackables || []).map(t => buildEsTrackableRow(t, s.id)));
                    const evBody = document.getElementById('es-events-tbody');
                    if (evBody) evBody.replaceChildren(...(evData.events || []).map(ev => buildEsEventRow(ev, s.id)));
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
            if (statusEl) { statusEl.textContent = 'Pushing…'; statusEl.style.color = 'var(--text-muted)'; }
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
                    if (d.skipped_no_type > 0) msg += ', ' + d.skipped_no_type + ' skipped (no event type — run Sync Event Types in Settings first)';
                    if (statusEl) { statusEl.textContent = msg; statusEl.style.color = 'var(--color-success)'; }
                    showToast(msg, d.skipped_no_type > 0 ? 'info' : 'success');
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
                tier_count_leader: parseInt(document.getElementById('es-tier-leader').value, 10) || 1,
                tier_count_core: parseInt(document.getElementById('es-tier-core').value, 10) || 10,
                tier_count_elite: parseInt(document.getElementById('es-tier-elite').value, 10) || 20,
                tier_count_valued: parseInt(document.getElementById('es-tier-valued').value, 10) || 69,
            };
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
                    showToast('Season deleted.');
                    loadSeasonList();
                    loadRewards();
                    loadSeasonMail();
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

    // ── Load on tab focus ─────────────────────────────────────────────────────
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const tab = btn.dataset.tab;
            if (tab === 'rewards' && activeSeason) loadRewards();
            if (tab === 'season-mail' && activeSeason) loadSeasonMail();
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
