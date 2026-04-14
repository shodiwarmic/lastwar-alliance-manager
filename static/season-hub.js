/* season-hub.js — Season Hub page logic */

(function () {
    'use strict';

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
                    renderManualTable();
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
            tdRank.textContent = m.rank;
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
            tdRank.textContent = m.rank;
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
    if (btnContribOcr) {
        btnContribOcr.addEventListener('click', () => {
            if (!activeSeason) { showToast('No active season.', 'error'); return; }
            const modal = document.getElementById('modal-ocr-import');
            if (modal) modal.style.display = 'flex';
        });
    }

    const CONTRIB_CATEGORIES = ['mutual_assistance', 'siege', 'rare_soil_war', 'defeat'];

    // Manual entry table
    function renderManualTable() {
        const tbody = document.getElementById('contrib-manual-tbody');
        if (!tbody) return;

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
            tdRank.textContent = m.rank;
            tr.appendChild(tdRank);

            CONTRIB_CATEGORIES.forEach(cat => {
                const td = document.createElement('td');
                const input = document.createElement('input');
                input.type = 'number';
                input.className = 'contrib-input';
                input.dataset.category = cat;
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
                        input.value = e !== undefined ? (e[input.dataset.category] ?? 0) : '';
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
            const entry = { member_id: memberId };
            // Include row if any input is non-empty (including explicit 0).
            // Blank means "no intent to change this member" — skip entirely.
            let hasExplicit = false;
            tr.querySelectorAll('.contrib-input').forEach(input => {
                if (input.value !== '') hasExplicit = true;
                entry[input.dataset.category] = input.value !== '' ? (parseInt(input.value, 10) || 0) : 0;
            });
            if (hasExplicit) entries.push(entry);
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

        fetch('/api/season-hub/contributions/import', { method: 'POST', body: fd })
            .then(r => r.json().then(d => ({ ok: r.ok, data: d })))
            .then(({ ok, data }) => {
                if (!ok) throw new Error(data.error || 'Import failed.');
                if (commit) {
                    showToast('Contributions committed: ' + (data.committed || 0) + ' records.');
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
                const items = unresolved.map(name => {
                    const li = document.createElement('li');
                    li.textContent = name;
                    return li;
                });
                unresolvedList.replaceChildren(...items);
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
            const tdRank = document.createElement('td'); tdRank.textContent = rw.member_rank; tr.appendChild(tdRank);
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
        memberSel.replaceChildren();
        allMembers.filter(m => m.rank !== 'EX').forEach(m => {
            const opt = document.createElement('option');
            opt.value = m.member_id;
            opt.textContent = m.name + ' (' + m.rank + ')';
            if (rw && rw.member_id === m.member_id) opt.selected = true;
            memberSel.appendChild(opt);
        });

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

    if (btnCreateSeason) {
        btnCreateSeason.addEventListener('click', () => {
            const errEl = document.getElementById('cs-error');
            if (errEl) { errEl.textContent = ''; errEl.style.display = 'none'; }
            updateArchiveWarning();
            const modal = document.getElementById('modal-create-season');
            if (modal) modal.style.display = 'flex';
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

        const scoreLevelRows = document.querySelectorAll('#cs-score-levels-table tbody tr');
        const newScoreLevels = Array.from(scoreLevelRows).map((row, i) => ({
            key: row.querySelector('.sl-key').value.trim(),
            label: row.querySelector('.sl-label').value.trim(),
            points: parseInt(row.querySelector('.sl-points').value, 10) || 0,
        }));

        const body = {
            name: document.getElementById('cs-name').value.trim(),
            season_number: parseInt(document.getElementById('cs-number').value, 10),
            start_date: document.getElementById('cs-start-date').value,
            week_count: parseInt(document.getElementById('cs-week-count').value, 10),
            key_event_name: document.getElementById('cs-key-event-name').value.trim(),
            key_event_required: parseInt(document.getElementById('cs-key-event-required').value, 10) || 0,
            tier_active_min_pct: parseInt(document.getElementById('cs-tier-active').value, 10) || 70,
            tier_at_risk_min_pct: parseInt(document.getElementById('cs-tier-at-risk').value, 10) || 60,
            tier_count_leader: parseInt(document.getElementById('cs-tier-leader').value, 10) || 1,
            tier_count_core: parseInt(document.getElementById('cs-tier-core').value, 10) || 10,
            tier_count_elite: parseInt(document.getElementById('cs-tier-elite').value, 10) || 20,
            tier_count_valued: parseInt(document.getElementById('cs-tier-valued').value, 10) || 69,
            score_levels: newScoreLevels,
        };

        if (!body.name || !body.start_date) {
            if (errEl) { errEl.textContent = 'Name and start date are required.'; errEl.style.display = ''; }
            return;
        }

        setButtonLoading(submitBtn);
        fetch('/api/season-hub/seasons', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        })
            .then(r => r.ok ? r : r.text().then(t => { throw new Error(t); }))
            .then(() => {
                const modal = document.getElementById('modal-create-season');
                if (modal) modal.style.display = '';
                showToast('Season created.');
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
            const modal = document.getElementById('modal-edit-season');
            if (modal) modal.style.display = 'flex';
        });
    }

    if (btnCancelEditSeason) {
        btnCancelEditSeason.addEventListener('click', () => {
            const modal = document.getElementById('modal-edit-season');
            if (modal) modal.style.display = '';
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
