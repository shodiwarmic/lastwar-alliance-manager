// static/vs-league.js — VS Duel League tab (lives on /vs alongside Member Points).
// Safe DOM only (own el() helper, textContent, addEventListener); tokens via CSS classes;
// showToast/showConfirm for feedback; CSRF injected globally by csrf.js.
'use strict';

(function () {
    const cfg = document.getElementById('page-config');
    const CAN_MANAGE = cfg && cfg.dataset.canManage === 'true';
    const MY_TAG = (cfg && cfg.dataset.allianceTag || '').trim();
    const MY_NAME = (cfg && cfg.dataset.allianceName || '').trim();

    // ---- tiny DOM builder (el() is not global; each hardened page carries its own — F-R10) ----
    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        if (props) {
            Object.entries(props).forEach(([k, v]) => {
                if (v == null) return;
                if (k === 'className') node.className = v;
                else if (k === 'text') node.textContent = v;
                else if (k === 'html') { /* never used — kept intentionally absent */ }
                else if (k === 'onclick') node.addEventListener('click', v);
                else if (k === 'onchange') node.addEventListener('change', v);
                else node.setAttribute(k, v);
            });
        }
        children.flat().forEach(c => {
            if (c == null) return;
            node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
        });
        return node;
    }
    const clear = n => { while (n.firstChild) n.removeChild(n.firstChild); return n; };

    async function api(method, url, body) {
        const opt = { method, headers: {} };
        if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
        const res = await fetch(url, opt);
        const text = (await res.text()).trim();
        if (!res.ok) throw new Error(text || (res.status + ' error'));
        // Parse the body directly (don't rely on the server's content-type header).
        return text ? JSON.parse(text) : null;
    }

    // ---- state ----
    let state = { seasons: [], seasonId: null, season: null, weeks: [], currentWeekDate: null, view: 'week' };
    let allTimeFilter = 'newest'; // All-Time seasons list sort (newest/oldest/best/worst)
    const root = document.getElementById('vs-league-root');

    // ===================== tab switching (points ↔ league) =====================
    // First-paint visibility comes from the server-rendered `active` class (F-R06); here we
    // only toggle the class (never inline display), and persist via URL hash.
    function showTab(name) {
        const valid = (name === 'league' || name === 'alltime') ? name : 'points';
        document.querySelectorAll('#vs-tab-bar .tab-btn').forEach(b =>
            b.classList.toggle('active', b.dataset.tab === valid));
        ['points', 'league', 'alltime'].forEach(t => {
            const elm = document.getElementById('tab-' + t);
            if (elm) elm.classList.toggle('active', valid === t);
        });
        if (('#' + valid) !== location.hash) history.replaceState(null, '', '#' + valid);
        if (valid === 'league' && !state.loaded) initLeague();
        if (valid === 'alltime') renderAllTime();
    }
    document.querySelectorAll('#vs-tab-bar .tab-btn').forEach(b =>
        b.addEventListener('click', () => showTab(b.dataset.tab)));
    const hashTab = () => location.hash === '#league' ? 'league' : location.hash === '#alltime' ? 'alltime' : 'points';
    document.addEventListener('DOMContentLoaded', () => showTab(hashTab()));
    // hash may already be set before DOMContentLoaded fires late — guard immediately too
    if (document.readyState !== 'loading' && (location.hash === '#league' || location.hash === '#alltime')) showTab(hashTab());

    // ===================== league data =====================
    async function initLeague() {
        state.loaded = true;
        try {
            state.seasons = await api('GET', '/api/vs-league/seasons');
        } catch (e) { root.replaceChildren(el('p', { className: 'vsl-empty', text: 'Could not load Duel League: ' + e.message })); return; }
        if (!state.seasons.length) { renderNoSeasons(); return; }
        const active = state.seasons.find(s => s.is_active) || state.seasons[0];
        await selectSeason(active.id);
    }

    async function selectSeason(id) {
        state.seasonId = id;
        state.viewWeekId = null; // reset the week-view selection when switching seasons
        state.season = state.seasons.find(s => s.id === id) || null;
        try {
            const data = await api('GET', '/api/vs-league/weeks?season_id=' + id);
            state.weeks = data || [];
        } catch (e) { showToast(e.message, 'error'); state.weeks = []; }
        // Seasons are always 4 weeks (auto-created on creation). Backfill any missing weeks for
        // seasons made before that — there's no "+ Week" button anymore.
        if (CAN_MANAGE && state.season && state.season.start_date && state.weeks.length < 4) {
            await ensureFourWeeks();
        }
        if (state.currentWeekDate == null) {
            try { const cur = await api('GET', '/api/vs-league/current'); state.currentWeekDate = cur.current_week_date; }
            catch { state.currentWeekDate = null; }
        }
        render();
    }

    // Create any of the 4 weeks that don't exist yet (derived from the season start Monday).
    async function ensureFourWeeks() {
        const have = new Set(state.weeks.map(w => w.week_number));
        const startISO = state.season.start_date;
        for (let n = 1; n <= 4; n++) {
            if (have.has(n)) continue;
            const d = new Date(startISO + 'T00:00:00Z');
            if (isNaN(d)) break;
            d.setUTCDate(d.getUTCDate() + (n - 1) * 7);
            try { await api('POST', '/api/vs-league/weeks', { season_id: state.seasonId, week_number: n, week_date: d.toISOString().slice(0, 10) }); }
            catch (e) { /* best-effort */ }
        }
        try { const data = await api('GET', '/api/vs-league/weeks?season_id=' + state.seasonId); state.weeks = data || []; }
        catch (e) { /* keep current */ }
    }

    function renderNoSeasons() {
        const box = el('div', { className: 'vsl-empty' },
            el('p', { text: 'No Duel League season yet.' }));
        if (CAN_MANAGE) box.appendChild(el('button', { className: 'btn btn-primary', onclick: openSeasonModal }, 'Create the first season'));
        root.replaceChildren(box);
    }

    // ===================== render =====================
    function render() {
        clear(root);
        root.appendChild(renderToolbar());
        root.appendChild(renderSubtabs());
        const view = el('div', { id: 'vsl-view' });
        root.appendChild(view);
        renderView(view);
    }

    function renderToolbar() {
        const bar = el('div', { className: 'vsl-toolbar' });
        const sel = el('select', { className: 'form-input vsl-season-select', onchange: e => selectSeason(parseInt(e.target.value, 10)) });
        state.seasons.forEach(s => {
            const label = 'S' + s.season_number + (s.is_active ? ' (active)' : '') + (s.league_tier ? ' · ' + s.league_tier : '');
            sel.appendChild(el('option', { value: s.id, selected: s.id === state.seasonId ? 'selected' : null, text: label }));
        });
        bar.appendChild(sel);
        bar.appendChild(el('span', { className: 'sep' }));
        if (CAN_MANAGE) {
            bar.appendChild(el('button', { className: 'btn btn-secondary btn-sm', onclick: openSeasonModal }, 'New Season'));
            if (state.season) bar.appendChild(el('button', { className: 'btn btn-secondary btn-sm', onclick: openSeasonEditModal }, 'Season settings'));
            // The 4 weeks are auto-created with the season — no "+ Week"; edit each from its week view.
        }
        return bar;
    }

    function renderSubtabs() {
        const wrap = el('div', { className: 'vsl-subtabs' });
        const onWeek = state.view !== 'summary' && state.view !== 'bracket';
        const sel = viewedWeek();
        // Each week is its own sub-tab (past + current selectable; future disabled), then the rollups.
        state.weeks.slice().sort((a, b) => (a.week_number || 0) - (b.week_number || 0)).forEach(wk => {
            const status = weekStatusOf(wk);
            const active = onWeek && sel && sel.id === wk.id;
            const btn = el('button', {
                className: 'vsl-subtab' + (active ? ' active' : '') + (status === 'future' ? ' future' : ''),
                text: 'Week ' + (wk.week_number != null ? wk.week_number : wk.week_date) + (status === 'current' ? ' •' : '')
            });
            btn.addEventListener('click', () => {
                if (weekStatusOf(wk) === 'future') { showToast('Week ' + (wk.week_number || '') + ' hasn’t started yet.', 'info'); return; }
                state.view = 'week'; state.viewWeekId = wk.id; render();
            });
            wrap.appendChild(btn);
        });
        [['summary', 'Season Summary'], ['bracket', 'Bracket']].forEach(([k, label]) => {
            wrap.appendChild(el('button', {
                className: 'vsl-subtab' + (state.view === k ? ' active' : ''),
                onclick: () => { state.view = k; render(); }
            }, label));
        });
        return wrap;
    }

    function currentWeek() {
        return state.weeks.find(w => w.week_date === state.currentWeekDate) || state.weeks[state.weeks.length - 1] || null;
    }

    // Week status relative to the live game week: past weeks (played) and the current week are
    // selectable; future weeks haven't started.
    function weekStatusOf(wk) {
        if (!state.currentWeekDate || !wk.week_date) return 'current';
        if (wk.week_date < state.currentWeekDate) return 'past';
        if (wk.week_date > state.currentWeekDate) return 'future';
        return 'current';
    }
    function viewedWeek() {
        if (state.viewWeekId != null) {
            const w = state.weeks.find(x => x.id === state.viewWeekId);
            if (w && weekStatusOf(w) !== 'future') return w;
        }
        return currentWeek();
    }
    function renderView(view) {
        if (state.view === 'summary') return renderSummary(view);
        if (state.view === 'bracket') return renderBracket(view);
        return renderCurrentWeek(view);
    }

    // ---------- Current Week ----------
    function pillFor(st) {
        if (!st.decided) return el('span', { className: 'vsl-pill live', text: 'Live' });
        if (st.outcome === 'win') return el('span', { className: 'vsl-pill win', text: 'Won' });
        if (st.outcome === 'loss') return el('span', { className: 'vsl-pill loss', text: 'Lost' });
        if (st.outcome === 'tie') return el('span', { className: 'vsl-pill tie', text: 'Tie' });
        return el('span', { className: 'vsl-pill pending', text: 'Pending' });
    }

    function renderCurrentWeek(view) {
        if (!state.weeks.length) {
            view.appendChild(el('p', { className: 'vsl-empty', text: 'No weeks in this season.' }));
            return;
        }
        const wk = viewedWeek();
        if (!wk) { view.appendChild(el('p', { className: 'vsl-empty', text: 'No week to show yet.' })); return; }
        const st = wk.standing;
        const oppLabel = (wk.opponent_tag ? '[' + wk.opponent_tag + '] ' : '') + (wk.opponent_name || 'Opponent');

        // header card
        const head = el('div', { className: 'card' });
        const hh = el('div', { className: 'card-header' },
            el('div', {},
                el('h2', { text: 'Week ' + (wk.week_number != null ? wk.week_number : '') + ' · vs ' + oppLabel })
            ));
        head.appendChild(hh);
        if (CAN_MANAGE) {
            const actions = el('div', { style: 'display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px;' },
                el('button', { className: 'btn btn-secondary btn-sm', onclick: () => openWeekModal(wk) }, 'Edit matchup'),
                el('button', { className: 'btn btn-secondary btn-sm', onclick: () => openDaysModal(wk) }, 'Enter daily results'),
                el('button', { className: 'btn btn-secondary btn-sm', onclick: () => openBracketModal(wk) }, 'Capture bracket'));
            head.appendChild(actions);
        }

        // winnable banner
        const bannerCls = !st.decided ? 'live' : (st.outcome === 'win' ? 'win' : st.outcome === 'loss' ? 'loss' : 'live');
        const banner = el('div', { className: 'vsl-banner ' + bannerCls });
        banner.appendChild(el('span', { className: 'score', text: 'Match Points  ' + st.our_points + ' – ' + st.opponent_points }));
        banner.appendChild(bannerStatusText(st, wk));
        head.appendChild(banner);

        // metric tiles
        const tiles = el('div', { className: 'vsl-tiles' });
        tiles.appendChild(tile('Weekly Match Score', st.our_points + ' – ' + st.opponent_points, 'first to 7 of 13'));
        tiles.appendChild(tile('Bracket Rank', wk.league_rank != null ? '#' + wk.league_rank : '—', wk.league_tier || (state.season && state.season.league_tier) || ''));
        if (wk.opponent_power != null) tiles.appendChild(tile('Opp Power', fmtBig(wk.opponent_power), 'kills ' + (wk.opponent_kills != null ? fmtBig(wk.opponent_kills) : '—')));
        if (wk.opponent_member_count != null) tiles.appendChild(tile('Opp Members', wk.opponent_member_count + ' / 100', wk.opponent_server ? 'server ' + wk.opponent_server : ''));
        head.appendChild(tiles);

        // strategy badges
        if (wk.strategy_label || wk.strategy_result || wk.notes) {
            const strat = el('div', { style: 'display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-bottom:6px;' });
            if (wk.strategy_label) strat.appendChild(el('span', { className: 'vsl-strat ' + wk.strategy_label, text: wk.strategy_label + ' week' }));
            if (wk.strategy_result) strat.appendChild(el('span', { className: 'vsl-strat ' + wk.strategy_result, text: wk.strategy_result }));
            head.appendChild(strat);
            if (wk.notes) head.appendChild(el('p', { className: 'vsl-help', text: wk.notes }));
        }

        // day tiles
        head.appendChild(el('h3', { style: 'margin:14px 0 4px;font-size:1rem;', text: 'Days' }));
        head.appendChild(renderDayTiles(wk));
        view.appendChild(head);

        // participation
        const partCard = el('div', { className: 'card' },
            el('div', { className: 'card-header' }, el('div', {}, el('h2', { text: 'Participation' }),)));
        const partBody = el('div', { text: 'Loading…', className: 'vsl-help' });
        partCard.appendChild(partBody);
        view.appendChild(partCard);
        loadParticipation(wk.week_date, partBody);
    }

    function bannerStatusText(st, wk) {
        if (st.decided) {
            let txt = st.outcome === 'win' ? 'Clinched the week' : st.outcome === 'loss' ? 'Eliminated' : 'Weekly tie';
            if (st.clinch_day) {
                const theme = getVSTheme(dayDateStr(wk.week_date, st.clinch_day - 1));
                // "clinched" only fits a win; use a neutral verb for a loss/tie decided early.
                const verb = st.outcome === 'win' ? 'clinched' : 'decided';
                txt += ' — ' + (st.clinch_day === 6 ? 'went to Day 6 (Enemy Buster)' : verb + ' on Day ' + st.clinch_day + (theme ? ' (' + theme.short + ')' : ''));
            }
            return el('span', { text: txt });
        }
        const need = st.opponent_points + 1 - st.our_points;
        const txt = 'Live · ' + st.remaining + ' pts still in play' + (need > 0 && need <= st.remaining ? ' · need ' + need + ' to lead' : '');
        return el('span', { text: txt });
    }

    function renderDayTiles(wk) {
        const grid = el('div', { className: 'vsl-days' });
        const byDay = {};
        (wk.days || []).forEach(d => { byDay[d.day_number] = d; });
        for (let n = 1; n <= 6; n++) {
            const theme = getVSTheme(dayDateStr(wk.week_date, n - 1));
            const d = byDay[n];
            const oc = d ? d.outcome : 'pending';
            const tileEl = el('div', { className: 'vsl-day' + (oc === 'win' ? ' win' : oc === 'loss' ? ' loss' : oc === 'tie' ? ' tie' : '') });
            tileEl.appendChild(el('div', { className: 'thm' }, (theme ? theme.icon + ' ' : '') + (theme ? theme.short : 'Day ' + n)));
            tileEl.appendChild(el('div', { className: 'pts', text: (theme ? theme.points : vsLeagueDayPts(n)) + ' pt' + ((theme ? theme.points : 0) === 1 ? '' : 's') }));
            tileEl.appendChild(el('div', { className: 'res', text: oc === 'pending' ? '—' : oc.toUpperCase() }));
            if (d && d.our_score != null && d.opponent_score != null)
                tileEl.appendChild(el('div', { className: 'raw', text: 'Raw ' + fmtBig(d.our_score) + ' – ' + fmtBig(d.opponent_score) }));
            if (d && d.mvp_name) tileEl.appendChild(el('div', { className: 'raw', text: '★ ' + d.mvp_name + (d.mvp_is_ours ? '' : ' (opp)') }));
            grid.appendChild(tileEl);
        }
        return grid;
    }

    async function loadParticipation(weekDate, container) {
        try {
            const days = await api('GET', '/api/vs-league/participation?week_date=' + encodeURIComponent(weekDate));
            clear(container);
            const grid = el('div', { className: 'vsl-days' });
            days.forEach(d => {
                const theme = getVSTheme(dayDateStr(weekDate, d.day_number - 1));
                const t = el('div', { className: 'vsl-day' });
                t.appendChild(el('div', { className: 'thm' }, (theme ? theme.short : 'Day ' + d.day_number)));
                if (!d.imported) { t.appendChild(el('div', { className: 'raw', text: 'not imported' })); }
                else {
                    t.appendChild(el('div', { className: 'res', text: d.active_scorers + ' active' }));
                    t.appendChild(el('div', { className: 'raw', text: d.zero_score + ' zero · avg ' + Math.round(d.avg_per_active) }));
                    t.appendChild(el('div', { className: 'raw', text: 'top10 ' + d.top10_pct.toFixed(0) + '%' }));
                }
                grid.appendChild(t);
            });
            container.replaceWith(grid);
        } catch (e) {
            container.textContent = 'Participation unavailable: ' + e.message;
        }
    }

    // ---------- Season History ----------
    function renderSummary(view) {
        if (!state.weeks.length) { view.appendChild(el('p', { className: 'vsl-empty', text: 'No weeks yet.' })); return; }
        const weeks = state.weeks.slice().sort((a, b) => (a.week_number || 0) - (b.week_number || 0));

        // Season header: tier, progress, our record so far.
        let ourW = 0, ourL = 0, ourT = 0;
        weeks.forEach(w => { const st = w.standing; if (st && st.decided) { if (st.outcome === 'win') ourW++; else if (st.outcome === 'loss') ourL++; else if (st.outcome === 'tie') ourT++; } });
        const played = weeks.filter(w => weekStatusOf(w) !== 'future').length;
        const tier = (state.season && state.season.league_tier) || '';
        const head = el('div', { className: 'card' });
        head.appendChild(el('div', { className: 'card-header' }, el('div', {},
            el('h2', { text: (state.season ? 'S' + state.season.season_number : 'Season') + (tier ? ' · ' + tier : '') }),
            el('div', { className: 'vsl-help', text: 'Week ' + Math.min(Math.max(played, 1), 4) + ' of 4 · our record ' + ourW + '–' + ourL + (ourT ? '–' + ourT : '') }))));
        view.appendChild(head);

        // Standings grid (all alliances × weeks) — async; populates its own holder.
        const stHolder = el('div');
        view.appendChild(stHolder);
        stHolder.appendChild(el('p', { className: 'vsl-help', text: 'Loading standings…' }));
        renderStandings(weeks, stHolder);

        // Our week-by-week table.
        view.appendChild(renderWeekTable(weeks));
    }

    function renderWeekTable(weeks) {
        const card = el('div', { className: 'card' });
        card.appendChild(el('div', { className: 'card-header' }, el('div', {}, el('h2', { text: 'Our weeks' }))));
        const table = el('table', { className: 'data-table' });
        table.appendChild(el('thead', {}, el('tr', {},
            el('th', { text: 'Week' }), el('th', { text: 'Opponent' }), el('th', { text: 'Match Score' }),
            el('th', { text: 'Result' }), el('th', { text: 'Strategy' }), CAN_MANAGE ? el('th', { text: '' }) : null)));
        const tb = el('tbody');
        weeks.forEach(wk => {
            const st = wk.standing;
            const opp = (wk.opponent_tag ? '[' + wk.opponent_tag + '] ' : '') + (wk.opponent_name || '—');
            tb.appendChild(el('tr', {},
                el('td', { text: 'Week ' + (wk.week_number != null ? wk.week_number : '') }),
                el('td', { text: opp }),
                el('td', { text: st.our_points + ' – ' + st.opponent_points }),
                el('td', {}, pillFor(st)),
                el('td', {}, wk.strategy_label ? el('span', { className: 'vsl-strat ' + wk.strategy_label, text: wk.strategy_label }) : el('span', { className: 'vsl-help', text: '—' })),
                CAN_MANAGE ? el('td', {}, el('button', { className: 'btn btn-secondary btn-sm', onclick: () => openWeekModal(wk) }, 'Edit'),
                    el('button', { className: 'btn btn-danger btn-sm', onclick: () => deleteWeek(wk), style: 'margin-left:6px;' }, 'Del')) : null));
        });
        table.appendChild(tb);
        card.appendChild(el('div', { className: 'table-scroll' }, table));
        return card;
    }

    function summaryResultCell(r, status) {
        if (!r) return el('span', { className: 'vsl-scell up', text: status === 'future' ? '💤' : '—' });
        const m = { W: ['w', 'W'], L: ['l', 'L'], T: ['t', 'T'], live: ['live', '⚔'], upcoming: ['up', '💤'] };
        const [cls, txt] = m[r.result] || ['up', '·'];
        return el('span', { className: 'vsl-scell ' + cls, text: txt });
    }

    // Standings: all alliances in rank order (latest week with data), a result cell per week. Uses
    // saved brackets, computing uncaptured weeks the same way the bracket view does.
    async function renderStandings(weeks, holder) {
        const saved = await Promise.all(weeks.map(w => api('GET', '/api/vs-league/weeks/' + w.id + '/matchups').catch(() => [])));
        const perWeek = [];
        for (let i = 0; i < weeks.length; i++) {
            const wk = weeks[i], ms = saved[i] || [], status = weekStatusOf(wk);
            const byTag = new Map();
            if (ms.length) {
                const setSide = (tag, name, rank, pts, oppPts) => {
                    if (!tag) return;
                    const decided = pts != null && oppPts != null;
                    const result = decided ? (pts > oppPts ? 'W' : pts < oppPts ? 'L' : 'T') : (status === 'future' ? 'upcoming' : 'live');
                    byTag.set(tag.toLowerCase(), { tag, name, rank, result });
                };
                ms.forEach(m => { setSide(m.a_tag, m.a_name, m.a_rank, m.a_points, m.b_points); setSide(m.b_tag, m.b_name, m.b_rank, m.b_points, m.a_points); });
            } else {
                const ranked = await inferRankedForWeek(wk);
                if (ranked) ranked.forEach(r => byTag.set(r.tag.toLowerCase(), { tag: r.tag, name: r.name, rank: r.rank, result: status === 'future' ? 'upcoming' : status === 'current' ? 'live' : 'pending' }));
            }
            perWeek.push({ wk, byTag, status });
        }

        let orderWeek = null;
        for (let i = perWeek.length - 1; i >= 0; i--) { if (perWeek[i].byTag.size) { orderWeek = perWeek[i]; break; } }
        clear(holder);
        if (!orderWeek) { holder.appendChild(el('p', { className: 'vsl-empty', text: 'Capture a bracket (or complete week 1) to see standings.' })); return; }

        const allTags = new Map();
        perWeek.forEach(pw => pw.byTag.forEach((v, k) => { if (!allTags.has(k)) allTags.set(k, { tag: v.tag, name: v.name }); else if (v.name && !allTags.get(k).name) allTags.get(k).name = v.name; }));
        const rows = [...allTags.entries()].map(([k, a]) => ({ k, tag: a.tag, name: a.name, rank: (orderWeek.byTag.get(k) || {}).rank || 99 }));
        rows.sort((a, b) => a.rank - b.rank);

        const card = el('div', { className: 'card' });
        card.appendChild(el('div', { className: 'card-header' }, el('div', {},
            el('h2', { text: 'Standings' }),
            el('div', { className: 'vsl-help', text: 'The 16 alliances in current rank order · W/L per week, ⚔ live, 💤 upcoming.' }))));
        const tbl = el('table', { className: 'data-table vsl-stbl' });
        const heads = [el('th', { text: '#' }), el('th', { text: 'Alliance' })];
        perWeek.forEach(pw => heads.push(el('th', { className: 'vsl-wc', text: 'Wk ' + (pw.wk.week_number || '') })));
        tbl.appendChild(el('thead', {}, el('tr', {}, ...heads)));
        const tb = el('tbody');
        rows.forEach(r => {
            const isUs = !!(MY_TAG && r.tag && r.tag.toLowerCase() === MY_TAG.toLowerCase());
            const cells = [
                el('td', { className: 'vsl-rk' + (r.rank <= 3 ? ' rk' + r.rank : ''), text: r.rank <= 16 ? String(r.rank) : '—' }),
                el('td', { className: 'vsl-al', text: (r.tag ? '[' + r.tag + '] ' : '') + (r.name || '') + (isUs ? ' · us' : '') }),
            ];
            perWeek.forEach(pw => cells.push(el('td', { className: 'vsl-wc' }, summaryResultCell(pw.byTag.get(r.k), pw.status))));
            tb.appendChild(el('tr', { className: isUs ? 'us' : null }, ...cells));
        });
        tbl.appendChild(tb);
        card.appendChild(el('div', { className: 'table-scroll' }, tbl));
        holder.appendChild(card);
    }

    async function deleteWeek(wk) {
        if (!await showConfirm('Delete Week ' + (wk.week_number || '') + ' and all its data?', 'Delete')) return;
        try { await api('DELETE', '/api/vs-league/weeks/' + wk.id); showToast('Week deleted.'); await selectSeason(state.seasonId); }
        catch (e) { showToast(e.message, 'error'); }
    }

    // ---------- Bracket ----------
    // inferRankedForWeek returns this week's rank order [{rank,tag,name,server}] computed from prior
    // captured weeks (most wins → earliest wins → starting rank), or null if not computable. Same
    // rule as the capture modal's computeRanks (kept in sync — see openBracketModal).
    async function inferRankedForWeek(wk) {
        const fetchMs = id => api('GET', '/api/vs-league/weeks/' + id + '/matchups').catch(() => []);
        const priorWeeks = state.weeks
            .filter(w => w.week_number != null && wk.week_number != null && w.week_number < wk.week_number)
            .sort((a, b) => a.week_number - b.week_number);
        if (!priorWeeks.length) return null;
        const [priorMs, known] = await Promise.all([
            Promise.all(priorWeeks.map(w => fetchMs(w.id))),
            api('GET', '/api/external-alliances').catch(() => [])
        ]);
        const byTag = new Map(); (known || []).forEach(a => { if (a.tag) byTag.set(a.tag.toLowerCase(), a); });
        const info = new Map(), startRank = new Map(), weekRes = [];
        const remember = s => {
            if (!s.tag) return null;
            const k = s.tag.toLowerCase();
            if (!info.has(k)) info.set(k, { tag: s.tag, name: s.name || '', server: s.server != null ? s.server : null });
            const rec = info.get(k);
            if (s.name) rec.name = s.name;
            if (s.server != null) rec.server = s.server;
            return k;
        };
        priorWeeks.forEach((w, wi) => {
            const res = new Map();
            (priorMs[wi] || []).forEach(m => {
                const A = { tag: m.a_tag, name: m.a_name, server: m.a_server, rank: m.a_rank, pts: m.a_points };
                const B = { tag: m.b_tag, name: m.b_name, server: m.b_server, rank: m.b_rank, pts: m.b_points };
                const ka = remember(A), kb = remember(B);
                if (w.week_number === 1) { if (ka && A.rank != null) startRank.set(ka, A.rank); if (kb && B.rank != null) startRank.set(kb, B.rank); }
                if (A.pts != null && B.pts != null && ka && kb) { const d = A.pts - B.pts; res.set(ka, d > 0 ? 'W' : d < 0 ? 'L' : 'T'); res.set(kb, d < 0 ? 'W' : d > 0 ? 'L' : 'T'); }
            });
            weekRes.push(res);
        });
        for (const [k, rec] of info) { if (rec.server == null && byTag.get(k) && byTag.get(k).server != null) rec.server = byTag.get(k).server; }
        const keys = [...startRank.keys()];
        if (keys.length < 2) return null;
        for (const res of weekRes) for (const k of keys) { const v = res.get(k); if (v == null || v === 'T') return null; }
        const arr = keys.map(k => ({ k, timeline: weekRes.map(r => r.get(k)), start: startRank.get(k) }));
        arr.forEach(a => a.wins = a.timeline.filter(x => x === 'W').length);
        arr.sort((a, b) => {
            if (b.wins !== a.wins) return b.wins - a.wins;
            for (let i = 0; i < a.timeline.length; i++) if (a.timeline[i] !== b.timeline[i]) return a.timeline[i] === 'W' ? -1 : 1;
            return a.start - b.start;
        });
        return arr.map((x, i) => Object.assign({ rank: i + 1 }, info.get(x.k)));
    }

    function renderBracket(view) {
        if (!state.weeks.length) { view.appendChild(el('p', { className: 'vsl-empty', text: 'No weeks yet.' })); return; }
        const bar = el('div', { className: 'vsl-toolbar' });
        const sel = el('select', { className: 'form-input', style: 'min-width:160px', onchange: e => loadBracket(parseInt(e.target.value, 10), holder) });
        state.weeks.forEach(wk => sel.appendChild(el('option', { value: wk.id, text: 'Week ' + (wk.week_number != null ? wk.week_number : wk.week_date) })));
        bar.appendChild(sel);
        view.appendChild(bar);
        const holder = el('div');
        view.appendChild(holder);
        const first = currentWeek() || state.weeks[state.weeks.length - 1];
        sel.value = first.id;
        loadBracket(first.id, holder);
    }

    async function loadBracket(weekId, holder) {
        clear(holder);
        holder.appendChild(el('p', { className: 'vsl-help', text: 'Loading…' }));
        let ms;
        try { ms = await api('GET', '/api/vs-league/weeks/' + weekId + '/matchups'); }
        catch (e) { holder.replaceChildren(el('p', { className: 'vsl-empty', text: e.message })); return; }
        clear(holder);
        let banner = null;
        if (!ms.length) {
            // Not captured yet — but the ranks/pairings are DETERMINISTIC from prior results (the game
            // re-ranks by wins → earliest wins → starting rank), so render the computed bracket.
            const wk = state.weeks.find(w => w.id === weekId);
            const ranked = wk ? await inferRankedForWeek(wk) : null;
            clear(holder);
            if (!ranked) {
                holder.appendChild(el('p', { className: 'vsl-empty', text: 'No bracket captured for this week.' + (CAN_MANAGE ? ' Use “Capture bracket” on the Current Week tab.' : '') }));
                return;
            }
            ms = [];
            for (let i = 0; i < 8; i++) {
                const a = ranked[2 * i], b = ranked[2 * i + 1];
                if (!a && !b) continue;
                ms.push({
                    a_rank: a ? a.rank : null, a_tag: a ? a.tag : null, a_name: a ? a.name : null, a_server: a ? a.server : null, a_points: null,
                    b_rank: b ? b.rank : null, b_tag: b ? b.tag : null, b_name: b ? b.name : null, b_server: b ? b.server : null, b_points: null,
                    is_ours: !!(MY_TAG && ((a && (a.tag || '').toLowerCase() === MY_TAG.toLowerCase()) || (b && (b.tag || '').toLowerCase() === MY_TAG.toLowerCase())))
                });
            }
            banner = el('p', { className: 'vsl-help', text: 'Computed from prior results — matchups are always 1v2, 3v4 …. ' + (CAN_MANAGE ? 'Enter this week’s points via “Capture bracket”.' : 'Points appear once captured.') });
        }
        if (banner) holder.appendChild(banner);
        const col = el('div', { className: 'vsl-wkcol', style: 'flex-basis:340px;' });
        // order pairs by best (lowest) rank in each pair
        ms.sort((x, y) => Math.min(x.a_rank || 99, x.b_rank || 99) - Math.min(y.a_rank || 99, y.b_rank || 99));
        ms.forEach(m => col.appendChild(renderPair(m)));
        const wrap = el('div', { className: 'vsl-bracket' }, col);
        holder.appendChild(wrap);
        // derived ladder
        holder.appendChild(renderLadder(ms));
    }

    function renderPair(m) {
        // within-pair order by rank (lower # on top); winner = higher points
        let top = { rank: m.a_rank, tag: m.a_tag, name: m.a_name, pts: m.a_points };
        let bot = { rank: m.b_rank, tag: m.b_tag, name: m.b_name, pts: m.b_points };
        if ((top.rank || 99) > (bot.rank || 99)) { const t = top; top = bot; bot = t; }
        const bothScored = top.pts != null && bot.pts != null;
        const topWin = bothScored && top.pts > bot.pts;
        const botWin = bothScored && bot.pts > top.pts;
        const chip = (side, isWin) => {
            const c = el('div', { className: 'vsl-chip' + (isWin ? ' win' : '') });
            if (side.rank != null) c.appendChild(el('span', { className: 'rk', text: '#' + side.rank }));
            c.appendChild(el('span', { className: 'nm', text: (side.tag ? '[' + side.tag + '] ' : '') + (side.name || '—') }));
            if (side.pts != null) c.appendChild(el('span', { className: 's', text: side.pts }));
            return c;
        };
        const winnerTag = topWin ? top.tag : botWin ? bot.tag : null;
        return el('div', { className: 'vsl-pair' },
            el('div', { className: 'vsl-teams' }, chip(top, topWin), chip(bot, botWin)),
            el('div', { className: 'vsl-elbow' }),
            el('div', { className: 'vsl-wn' + (m.is_ours ? ' us' : '') }, m.is_ours ? 'ours' : (winnerTag ? '[' + winnerTag + ']' : '?')));
    }

    function renderLadder(ms) {
        // Build per-alliance rows from both sides, rank-sorted.
        const rows = [];
        ms.forEach(m => {
            if (m.a_tag || m.a_rank != null) rows.push({ rank: m.a_rank, tag: m.a_tag, name: m.a_name, pts: m.a_points });
            if (m.b_tag || m.b_rank != null) rows.push({ rank: m.b_rank, tag: m.b_tag, name: m.b_name, pts: m.b_points });
        });
        rows.sort((a, b) => (a.rank || 99) - (b.rank || 99));
        const card = el('div', { className: 'card', style: 'margin-top:16px;' });
        card.appendChild(el('div', { className: 'card-header' }, el('div', {}, el('h2', { text: 'Standings' }))));
        const table = el('table', { className: 'data-table' });
        table.appendChild(el('thead', {}, el('tr', {}, el('th', { text: '#' }), el('th', { text: 'Alliance' }), el('th', { text: 'Match Pts' }))));
        const tb = el('tbody');
        rows.forEach(r => tb.appendChild(el('tr', {},
            el('td', { text: r.rank != null ? r.rank : '' }),
            el('td', { className: 'vsl-al', text: (r.tag ? '[' + r.tag + '] ' : '') + (r.name || '—') }),
            el('td', { text: r.pts != null ? r.pts : '—' }))));
        table.appendChild(tb);
        card.appendChild(el('div', { className: 'table-scroll' }, table));
        card.appendChild(el('div', { className: 'vsl-zone' },
            el('span', {}, el('b', { className: 'promo', text: '▲ Promotion' }), ' top 2'),
            el('span', {}, el('b', { className: 'demo', text: '▼ Demotion' }), ' bottom (12–16)')));
        return card;
    }

    // ---------- Day Analysis ----------
    // ===================== helpers =====================
    // ---------- All-Time (cross-season, top-level tab) ----------
    async function renderAllTime() {
        const holder = document.getElementById('vsl-alltime-root');
        if (!holder) return;
        clear(holder);
        holder.appendChild(el('p', { className: 'vsl-help', text: 'Loading…' }));
        let a;
        try { a = await api('GET', '/api/vs-league/analytics'); }
        catch (e) { holder.replaceChildren(el('p', { className: 'vsl-empty', text: e.message })); return; }
        clear(holder);
        const hasSeasons = !!(a && a.totals && a.totals.seasons);
        const hasDayAvg = !!(a && a.day_averages && a.day_averages.some(d => d.weeks_n > 0));
        if (!hasSeasons && !hasDayAvg) {
            holder.appendChild(el('p', { className: 'vsl-empty', text: 'Nothing to analyze yet — play Duel League weeks or import member VS points.' }));
            return;
        }
        const rec = (w, l, t) => w + '–' + l + (t ? '–' + t : '');
        const cardWith = (title, ...nodes) => {
            const c = el('div', { className: 'card' });
            c.appendChild(el('div', { className: 'card-header' }, el('div', {}, el('h2', { text: title }))));
            nodes.forEach(n => { if (n) c.appendChild(n); });
            return c;
        };
        const table = (heads, rowsArr) => {
            const tbl = el('table', { className: 'data-table' });
            tbl.appendChild(el('thead', {}, el('tr', {}, ...heads.map(h => el('th', { text: h })))));
            const tb = el('tbody');
            rowsArr.forEach(cells => tb.appendChild(el('tr', {}, ...cells.map(c => el('td', { text: c })))));
            tbl.appendChild(tb);
            return el('div', { className: 'table-scroll' }, tbl);
        };

        if (hasSeasons) {
            const t = a.totals;
            const tiles = el('div', { className: 'vsl-tiles' });
            tiles.appendChild(tile('Seasons', String(t.seasons), ''));
            tiles.appendChild(tile('All-time record', rec(t.wins, t.losses, t.ties), 'decided weeks'));
            tiles.appendChild(tile('Win rate', t.win_rate != null ? Math.round(t.win_rate * 100) + '%' : '—', 'of decided weeks'));
            tiles.appendChild(tile('Best finish', t.best_final_rank != null ? '#' + t.best_final_rank : '—', 'final rank'));
            holder.appendChild(tiles);
            // Seasons — limited to 5 with a sort filter (list can get long over time).
            const seasonsWrap = el('div');
            holder.appendChild(seasonsWrap);
            const dfp = s => s.wins - s.losses;
            const seasonCmp = f =>
                f === 'oldest' ? (x, y) => x.season_number - y.season_number
                    : f === 'best' ? (x, y) => y.wins - x.wins || dfp(y) - dfp(x) || y.season_number - x.season_number
                        : f === 'worst' ? (x, y) => x.wins - y.wins || dfp(x) - dfp(y) || x.season_number - y.season_number
                            : (x, y) => y.season_number - x.season_number;
            const FILTERS = [['newest', 'Newest'], ['oldest', 'Oldest'], ['best', 'Best'], ['worst', 'Worst']];
            const renderSeasons = () => {
                clear(seasonsWrap);
                const many = a.seasons.length > 5;
                let chips = null;
                if (many) {
                    chips = el('div', { className: 'vsl-toolbar', style: 'gap:6px;margin-bottom:10px;flex-wrap:wrap;' });
                    FILTERS.forEach(([k, label]) => chips.appendChild(el('button', { className: 'filter-chip' + (allTimeFilter === k ? ' active' : ''), text: label, onclick: () => { allTimeFilter = k; renderSeasons(); } })));
                }
                const shown = a.seasons.slice().sort(seasonCmp(allTimeFilter)).slice(0, 5);
                seasonsWrap.appendChild(cardWith('Seasons' + (many ? ' · top 5' : ''), chips, table(
                    ['Season', 'Tier', 'Record', 'Final', 'Weeks'],
                    shown.map(s => ['S' + s.season_number, s.tier || '—', rec(s.wins, s.losses, s.ties), s.final_rank != null ? '#' + s.final_rank : '—', String(s.weeks)]))));
            };
            renderSeasons();
        }

        // Average points by theme day from member VS points across EVERY imported week (large sample,
        // zeros excluded): two bars per day — alliance total (accent) + per-player (info), each
        // scaled to its own max.
        const dcard = cardWith('Average points by day (all imported weeks)',
            el('div', { className: 'vsl-help', style: 'margin-bottom:8px;' },
                el('span', { className: 'vsl-legend-sw a' }), ' alliance total    ',
                el('span', { className: 'vsl-legend-sw p' }), ' per player'));
        const maxA = Math.max(1, ...(a.day_averages || []).map(d => d.avg_points || 0));
        const maxP = Math.max(1, ...(a.day_averages || []).map(d => d.avg_per_player || 0));
        let anyDay = false;
        const mkBar = (val, max, cls) => { const b = el('div', { className: 'vsl-wl' }); if (val != null) b.appendChild(el('span', { className: cls, style: 'flex:0 0 ' + Math.round((val / max) * 100) + '%' })); return b; };
        (a.day_averages || []).forEach(d => {
            if (d.weeks_n > 0) anyDay = true;
            const theme = (typeof VS_THEMES !== 'undefined') ? VS_THEMES[d.day_number - 1] : null;
            const bars = el('div', { style: 'flex:1;display:flex;flex-direction:column;gap:3px;min-width:0;' }, mkBar(d.avg_points, maxA, 'a'), mkBar(d.avg_per_player, maxP, 'p'));
            dcard.appendChild(el('div', { className: 'vsl-analysis-row' },
                el('div', { text: theme ? theme.icon + ' ' + theme.short : 'Day ' + d.day_number }),
                bars,
                el('div', { className: 'vsl-help', text: (d.avg_points != null ? fmtBig(Math.round(d.avg_points)) : '—') + ' · ' + (d.avg_per_player != null ? fmtBig(Math.round(d.avg_per_player)) + '/plyr' : '—') })));
        });
        if (!anyDay) dcard.appendChild(el('p', { className: 'vsl-empty', text: 'Import member VS points to see average points by theme day.' }));
        holder.appendChild(dcard);

        if (a.by_strategy && a.by_strategy.length) {
            const LBL = { push: 'Push', save: 'Save', normal: 'Normal', test: 'Test', recovery: 'Recovery' };
            holder.appendChild(cardWith('Strategy outcomes', table(
                ['Strategy', 'Worked', 'Failed', 'Mixed', 'Weeks'],
                a.by_strategy.map(s => [LBL[s.label] || s.label, String(s.worked), String(s.failed), String(s.mixed), String(s.total)]))));
        }
        if (a.opponents && a.opponents.length) {
            holder.appendChild(cardWith('Opponents faced', table(
                ['Alliance', 'Record', 'Meetings'],
                a.opponents.map(o => [(o.tag ? '[' + o.tag + '] ' : '') + (o.name || '—'), rec(o.wins, o.losses, o.ties), String(o.meetings)]))));
        }
    }

    function tile(lbl, val, sub) {
        return el('div', { className: 'vsl-tile' }, el('div', { className: 'lbl', text: lbl }),
            el('div', { className: 'val', text: val }), sub ? el('div', { className: 'sub', text: sub }) : null);
    }
    function fmtBig(n) { n = Number(n); if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'; if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'; if (n >= 1e3) return (n / 1e3).toFixed(0) + 'K'; return '' + n; }
    function vsLeagueDayPts(n) { return [1, 2, 2, 2, 2, 4][n - 1] || 0; }
    function todayISO() { return (window.gameDateStr ? window.gameDateStr() : new Date().toISOString().slice(0, 10)); }

    // Snap an ISO date to the Monday of its week (local, matching flatpickr's disable check).
    function weekMonday(iso) {
        const d = new Date((iso || todayISO()) + 'T00:00:00');
        if (isNaN(d)) return iso || '';
        const back = d.getDay() === 0 ? 6 : d.getDay() - 1; // days since Monday
        d.setDate(d.getDate() - back);
        return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
    }

    // A date field locked to Mondays via flatpickr (season weeks are always Mondays). Falls back to
    // a native date input if flatpickr didn't load.
    function mondayPicker(value) {
        const start = weekMonday(value);
        const input = inp('text', start, 'Monday (YYYY-MM-DD)');
        if (window.flatpickr) {
            window.flatpickr(input, {
                dateFormat: 'Y-m-d',
                allowInput: false,
                defaultDate: start || undefined,
                locale: { firstDayOfWeek: 1 },
                disable: [d => d.getDay() !== 1], // Mondays only
            });
        } else {
            input.type = 'date';
        }
        return input;
    }
    function dayDateStr(weekMonday, addDays) {
        const d = new Date(weekMonday + 'T00:00:00Z'); d.setUTCDate(d.getUTCDate() + addDays); return d.toISOString().slice(0, 10);
    }

    // ===================== modals (built in JS, appended to body) =====================
    function modal(title, bodyNodes, onSave, saveLabel) {
        const overlay = el('div', { className: 'modal' });
        const content = el('div', { className: 'modal-content modal-md' });
        content.appendChild(el('h2', { text: title }));
        bodyNodes.forEach(n => content.appendChild(n));
        const status = el('p', { className: 'status-msg' });
        content.appendChild(status);
        const actions = el('div', { className: 'modal-actions' });
        const saveBtn = el('button', { className: 'btn btn-primary', text: saveLabel || 'Save' });
        saveBtn.addEventListener('click', async () => {
            saveBtn.disabled = true; status.textContent = 'Saving…';
            try { await onSave(); close(); showToast('Saved.'); }
            catch (e) { status.textContent = e.message; saveBtn.disabled = false; }
        });
        const cancel = el('button', { className: 'btn btn-secondary', text: 'Cancel' });
        function close() { overlay.style.display = ''; overlay.remove(); }
        cancel.addEventListener('click', close);
        overlay.addEventListener('click', e => { if (e.target === overlay) close(); });
        actions.appendChild(saveBtn); actions.appendChild(cancel);
        content.appendChild(actions);
        overlay.appendChild(content);
        document.body.appendChild(overlay);
        overlay.style.display = 'flex';
        return { close, status };
    }
    function field(label, input) { return el('div', { className: 'vsl-field' }, el('label', { text: label }), input); }
    function inp(type, value, ph) { return el('input', { className: 'form-input', type: type || 'text', value: value != null ? value : '', placeholder: ph || '' }); }
    function sel(options, value) {
        const s = el('select', { className: 'form-input' });
        options.forEach(([v, l]) => s.appendChild(el('option', { value: v, selected: v === value ? 'selected' : null, text: l })));
        return s;
    }
    const numOrNull = v => { const s = String(v).trim(); return s === '' ? null : parseInt(s, 10); };
    const strOrNull = v => { const s = String(v).trim(); return s === '' ? null : s; };

    function openSeasonModal() {
        const num = inp('number', '', 'e.g. 34');
        const tier = inp('text', '', 'e.g. Gold Tier 29-2');
        const start = mondayPicker(state.currentWeekDate || todayISO());
        modal('New Duel League Season', [
            el('div', { className: 'vsl-form-grid' }, field('Season number', num), field('Tier (display)', tier), field('Start date', start)),
            el('p', { className: 'vsl-help', text: 'This becomes the active season; the previous active one is archived.' })
        ], async () => {
            const n = numOrNull(num.value);
            if (!n) throw new Error('Season number is required');
            await api('POST', '/api/vs-league/seasons', { season_number: n, league_tier: strOrNull(tier.value), start_date: strOrNull(start.value) });
            state.currentWeekDate = state.currentWeekDate; state.loaded = false; await initLeague();
        }, 'Create season');
    }

    function openSeasonEditModal() {
        const s = state.season;
        if (!s) return;
        const tier = inp('text', s.league_tier || '', 'e.g. Gold Tier 29-2');
        const start = mondayPicker(s.start_date || todayISO());
        modal('Season ' + s.season_number + ' settings', [
            el('div', { className: 'vsl-form-grid' }, field('Tier (display)', tier), field('Start Monday', start)),
            el('p', { className: 'vsl-help', text: 'Changing the start date re-aligns every week to consecutive game-time Mondays.' })
        ], async () => {
            const newStart = start.value;
            if (!newStart) throw new Error('Start date is required');
            await api('PUT', '/api/vs-league/seasons/' + s.id, { league_tier: strOrNull(tier.value), start_date: newStart });
            // Re-align weeks: week n → start + (n-1)*7 (backend snaps to the game Monday). Order the
            // updates to avoid a transient UNIQUE(season_id, week_date) collision during the shift.
            const oldStart = s.start_date || newStart;
            const weeks = state.weeks.slice().sort((a, b) => (a.week_number || 0) - (b.week_number || 0));
            const forward = new Date(newStart) > new Date(oldStart);
            const ordered = forward ? weeks.slice().reverse() : weeks;
            for (const wkk of ordered) {
                const n = wkk.week_number || (weeks.indexOf(wkk) + 1);
                const d = new Date(newStart + 'T00:00:00Z'); d.setUTCDate(d.getUTCDate() + (n - 1) * 7);
                const nd = d.toISOString().slice(0, 10);
                if (nd !== wkk.week_date) await api('PUT', '/api/vs-league/weeks/' + wkk.id, { week_date: nd });
            }
            state.currentWeekDate = null; state.loaded = false; await initLeague();
        }, 'Save');
    }

    function openWeekModal(wk) {
        // Week number + date are derived (seasons are 4 consecutive game-time-Monday weeks); tier
        // and our rank aren't asked for — tier is the season's, our rank is set from the bracket.
        const derived = (() => {
            if (wk) return { num: wk.week_number, date: wk.week_date };
            const nums = state.weeks.map(w => w.week_number || 0);
            const num = (nums.length ? Math.max(...nums) : 0) + 1;
            const startISO = (state.season && state.season.start_date) || todayISO();
            const d = new Date(startISO + 'T00:00:00Z');
            if (!isNaN(d)) d.setUTCDate(d.getUTCDate() + (num - 1) * 7);
            return { num, date: isNaN(d) ? startISO : d.toISOString().slice(0, 10) };
        })();
        const stratLabel = sel([['', '—'], ['push', 'Push'], ['save', 'Save'], ['normal', 'Normal'], ['test', 'Test'], ['recovery', 'Recovery']], wk && wk.strategy_label || '');
        const stratResult = sel([['', '—'], ['worked', 'Worked'], ['failed', 'Failed'], ['mixed', 'Mixed']], wk && wk.strategy_result || '');
        const notes = el('textarea', { className: 'form-input', rows: '2', placeholder: 'leadership context…' }); if (wk && wk.notes) notes.value = wk.notes;

        // ---- opponent identity + finder (server → tag → name, matching the in-game display) ----
        const oppServer = inp('number', wk ? wk.opponent_server : '', 'server #');
        const oppTag = inp('text', wk ? wk.opponent_tag : '', 'cROw');
        const oppName = inp('text', wk ? wk.opponent_name : '', 'Black Crow Legion');
        const lrInput = inp('text', wk ? wk.opponent_lastrank_id : '', 'paste lastrank.fun/a/… link');
        lrInput.style.flex = '1'; lrInput.style.minWidth = '150px';
        const snapNote = el('span', { className: 'vsl-help' });
        let snap = null;

        // Local source: the external-alliances registry (populated by past lookups/allies/prospects).
        let knownAlliances = [];
        api('GET', '/api/external-alliances').then(list => { knownAlliances = list || []; }).catch(() => { });
        const localMatches = q => {
            q = q.trim().toLowerCase();
            if (!q) return [];
            return knownAlliances.filter(a =>
                (a.tag || '').toLowerCase().includes(q) || (a.name || '').toLowerCase().includes(q)).slice(0, 6);
        };
        function setSnapFromMatch(m) {
            oppTag.value = m.tag || ''; oppName.value = m.name || ''; if (m.server != null) oppServer.value = m.server;
            if (m.lastrank_id) {
                lrInput.value = m.lastrank_id;
                snap = { alliance_id: m.lastrank_id, tag: m.tag, name: m.name, server_id: m.server, power: m.power, kills: m.kills, member_count: m.member_count, last_seen_at: m.lastrank_seen_at };
                snapNote.textContent = 'From saved lookup · power ' + fmtBig(m.power || 0) + ' · kills ' + fmtBig(m.kills || 0) + ' · ' + (m.member_count != null ? m.member_count : '?') + '/100';
            } else {
                snap = null;
                snapNote.textContent = 'From your registry — no LastRank snapshot saved yet; use Look up to capture one.';
            }
        }

        // Shared dropdown anchored under whichever of Opp tag / Opp name has focus.
        const dropdown = el('div', { className: 'vsl-find-dropdown', hidden: 'hidden' });
        const oppGrid = el('div', { className: 'vsl-form-grid vsl-opp-grid' },
            field('Opp server', oppServer), field('Opp tag', oppTag), field('Opp name', oppName), dropdown);
        let activeField = null;
        const queryVal = () => (activeField ? activeField.value : '').trim();

        function positionDropdown() {
            if (!activeField) return;
            const cell = activeField.closest('.vsl-field') || activeField;
            dropdown.style.left = cell.offsetLeft + 'px';
            dropdown.style.top = (cell.offsetTop + cell.offsetHeight + 4) + 'px';
            dropdown.style.minWidth = cell.offsetWidth + 'px';
            dropdown.style.maxWidth = (oppGrid.clientWidth - cell.offsetLeft) + 'px';
        }
        const onDocDown = e => {
            if (dropdown.contains(e.target)) return;
            if (activeField && (activeField.closest('.vsl-field') || activeField).contains(e.target)) return;
            closeDropdown();
        };
        function openDropdown() { positionDropdown(); dropdown.hidden = false; document.addEventListener('mousedown', onDocDown); }
        function closeDropdown() { dropdown.hidden = true; document.removeEventListener('mousedown', onDocDown); }

        function localItem(m) {
            const meta = [m.server != null ? 'S' + m.server : null, m.lastrank_id ? 'saved snapshot' : null].filter(Boolean).join(' · ');
            return el('button', { className: 'vsl-find-item', type: 'button', onclick: () => { closeDropdown(); setSnapFromMatch(m); } },
                el('span', { className: 'vsl-find-name', text: (m.tag ? '[' + m.tag + '] ' : '') + (m.name || '') }),
                meta ? el('span', { className: 'vsl-find-meta', text: meta }) : null);
        }
        function lrItem(r) {
            const meta = [r.server != null ? 'S' + r.server : null, r.power != null ? fmtBig(r.power) + ' pw' : null,
                r.kills != null ? fmtBig(r.kills) + ' k' : null].filter(Boolean).join(' · ');
            return el('button', { className: 'vsl-find-item', type: 'button', onclick: async () => {
                oppTag.value = r.tag || ''; oppName.value = r.name || ''; if (r.server != null) oppServer.value = r.server;
                lrInput.value = r.lastrank_id;
                closeDropdown();
                snapNote.textContent = 'Confirming…';
                try {
                    snap = await api('POST', '/api/vs-league/opponent-lookup', { url: r.lastrank_id });
                    snapNote.textContent = 'Power ' + fmtBig(snap.power) + ' · kills ' + fmtBig(snap.kills) + ' · ' + snap.member_count + '/100';
                } catch (e) {
                    snap = { alliance_id: r.lastrank_id, tag: r.tag, name: r.name, server_id: r.server, power: r.power, kills: r.kills, member_count: null, last_seen_at: null };
                    snapNote.textContent = 'Selected — power ' + fmtBig(r.power) + ' · kills ' + fmtBig(r.kills);
                }
            } },
                el('span', { className: 'vsl-find-name', text: (r.tag ? '[' + r.tag + '] ' : '') + (r.name || r.lastrank_id.slice(0, 8)) }),
                meta ? el('span', { className: 'vsl-find-meta', text: meta }) : null);
        }
        function renderDropdown(localList, lrList, msg, isError, showAction) {
            clear(dropdown);
            if (localList && localList.length) {
                dropdown.appendChild(el('div', { className: 'vsl-find-head', text: 'In your registry' }));
                localList.forEach(m => dropdown.appendChild(localItem(m)));
            }
            if (lrList && lrList.length) {
                dropdown.appendChild(el('div', { className: 'vsl-find-head', text: 'From LastRank' }));
                lrList.forEach(r => dropdown.appendChild(lrItem(r)));
            }
            if (msg) dropdown.appendChild(el('div', { className: isError ? 'vsl-find-msg vsl-find-err' : 'vsl-find-msg', text: msg }));
            if (showAction) dropdown.appendChild(el('button', { className: 'vsl-find-action', type: 'button', onclick: runLastRankSearch }, '🔎 Look up “' + queryVal() + '” on LastRank'));
            if (dropdown.childNodes.length) openDropdown(); else closeDropdown();
        }
        async function runLastRankSearch() {
            const q = queryVal();
            const srv = oppServer.value.trim();
            if (!q) return;
            if (!srv) { renderDropdown(localMatches(q), null, 'Enter the opponent server number — LastRank search matches it strictly.', true, false); oppServer.focus(); return; }
            renderDropdown(localMatches(q), null, 'Searching LastRank…', false, false);
            try {
                const list = await api('GET', '/api/external-alliances/search?q=' + encodeURIComponent(q) + '&server=' + encodeURIComponent(srv));
                renderDropdown(localMatches(q), list, (list && list.length) ? null : 'No LastRank matches on server ' + srv + '.', false, false);
            } catch (e) { renderDropdown(localMatches(q), null, e.message, true, false); }
        }
        function refreshFind() {
            const q = queryVal();
            if (!q) { closeDropdown(); return; }
            renderDropdown(localMatches(q), null, null, false, true);
        }
        [oppTag, oppName].forEach(f => {
            f.addEventListener('focus', () => { activeField = f; });
            f.addEventListener('input', () => { activeField = f; refreshFind(); });
            f.addEventListener('keydown', e => {
                if (e.key === 'Escape') closeDropdown();
                else if (e.key === 'Enter' && !dropdown.hidden) { e.preventDefault(); runLastRankSearch(); }
            });
        });

        const lookupBtn = el('button', { className: 'btn btn-secondary btn-sm', type: 'button' }, 'Look up');
        lookupBtn.addEventListener('click', async () => {
            snapNote.textContent = 'Looking up…';
            try {
                snap = await api('POST', '/api/vs-league/opponent-lookup', { url: lrInput.value });
                if (!oppTag.value) oppTag.value = snap.tag; if (!oppName.value) oppName.value = snap.name; if (!oppServer.value) oppServer.value = snap.server_id;
                snapNote.textContent = 'Power ' + fmtBig(snap.power) + ' · kills ' + fmtBig(snap.kills) + ' · ' + snap.member_count + '/100';
            } catch (e) { snapNote.textContent = e.message; }
        });

        modal(wk ? 'Edit matchup' : 'New week', [
            el('p', { className: 'vsl-help', text: 'Week ' + derived.num + ' · ' + derived.date + ' (Mon)' + ((state.season && state.season.league_tier) ? ' · ' + state.season.league_tier : '') }),
            oppGrid,
            el('span', { className: 'vsl-help', text: 'Type an opponent tag or name to search your registry; enter the server to look up on LastRank.' }),
            field('Opponent LastRank link', el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap;' }, lrInput, lookupBtn)),
            snapNote,
            el('div', { className: 'vsl-form-grid' }, field('Strategy', stratLabel), field('Result', stratResult)),
            field('Notes', notes)
        ], async () => {
            const payload = {
                season_id: state.seasonId, week_number: derived.num, week_date: derived.date,
                opponent_tag: strOrNull(oppTag.value), opponent_name: strOrNull(oppName.value), opponent_server: numOrNull(oppServer.value),
                strategy_label: strOrNull(stratLabel.value), strategy_result: strOrNull(stratResult.value), notes: strOrNull(notes.value)
            };
            if (snap) { payload.opponent_lastrank_id = snap.alliance_id; payload.opponent_power = snap.power; payload.opponent_kills = snap.kills; payload.opponent_member_count = snap.member_count; payload.opponent_lastrank_seen_at = snap.last_seen_at; payload.snapshot_now = true; }
            if (wk) await api('PUT', '/api/vs-league/weeks/' + wk.id, payload);
            else await api('POST', '/api/vs-league/weeks', payload);
            await selectSeason(state.seasonId);
        }, wk ? 'Save' : 'Create week');
    }

    function openDaysModal(wk) {
        const byDay = {}; (wk.days || []).forEach(d => byDay[d.day_number] = d);
        const rows = [];
        const body = [el('p', { className: 'vsl-help', text: 'Enter both raw Alliance Duel Scores and the winner is set automatically; otherwise pick the winner manually. Empty days stay pending.' })];

        // Daily MVP: search our roster + (if the opponent's LastRank alliance is known) their roster,
        // or type any raw name. We store only the name; the side toggle just sets mvp_is_ours.
        let ourRoster = [];
        api('GET', '/api/members').then(list => { ourRoster = (list || []).filter(m => m.rank !== 'EX'); }).catch(() => { });
        let oppRoster = null; // null = not fetched / unavailable; [] = fetched (possibly empty)
        if (wk.opponent_lastrank_id) {
            api('GET', '/api/vs-league/opponent-roster?lastrank_id=' + encodeURIComponent(wk.opponent_lastrank_id))
                .then(list => { oppRoster = list || []; }).catch(() => { oppRoster = []; });
        }
        const oppLabel = (wk.opponent_tag ? '[' + wk.opponent_tag + '] ' : 'Opponent ') + 'roster';

        function mvpField(d) {
            const side = sel([['ours', 'Ours'], ['opp', 'Opp']], d.mvp_is_ours === false ? 'opp' : 'ours');
            side.className = 'form-input vsl-mvp-side';
            const input = inp('text', d.mvp_name || '', 'MVP name — search or type');
            const dd = el('div', { className: 'vsl-find-dropdown vsl-mvp-dd', hidden: 'hidden' });
            const wrap = el('div', { className: 'vsl-mvp-wrap' }, side, input, dd);
            const onDocDown = e => { if (!wrap.contains(e.target)) close(); };
            function open() { dd.hidden = false; document.addEventListener('mousedown', onDocDown); }
            function close() { dd.hidden = true; document.removeEventListener('mousedown', onDocDown); }
            const pick = (nm, isOurs) => { input.value = nm; side.value = isOurs ? 'ours' : 'opp'; close(); };
            const item = (nm, meta, isOurs) => el('button', { className: 'vsl-find-item', type: 'button', onclick: () => pick(nm, isOurs) },
                el('span', { className: 'vsl-find-name', text: nm }),
                meta ? el('span', { className: 'vsl-find-meta', text: meta }) : null);
            function render() {
                const q = input.value.trim().toLowerCase();
                clear(dd);
                if (!q) { close(); return; }
                ourRoster.filter(m => (m.name || '').toLowerCase().includes(q)).slice(0, 6).forEach((m, i, arr) => {
                    if (i === 0) dd.appendChild(el('div', { className: 'vsl-find-head', text: 'Our roster' }));
                    dd.appendChild(item(m.name, m.rank || '', true));
                });
                (oppRoster || []).filter(m => (m.name || '').toLowerCase().includes(q)).slice(0, 6).forEach((m, i) => {
                    if (i === 0) dd.appendChild(el('div', { className: 'vsl-find-head', text: oppLabel }));
                    dd.appendChild(item(m.name, m.power != null ? fmtBig(m.power) + ' pw' : '', false));
                });
                if (!dd.childNodes.length) { close(); return; }
                open();
            }
            input.addEventListener('input', render);
            input.addEventListener('focus', render);
            input.addEventListener('keydown', e => { if (e.key === 'Escape') close(); });
            return { wrap, input, side };
        }

        // When both raw scores are present the outcome is determined — set it and lock the picker
        // (the server derives the same value on save). Manual selection stays for score-less days.
        const syncOutcome = r => {
            const a = r.our.value.trim(), b = r.opp.value.trim();
            if (a !== '' && b !== '') {
                r.oc.value = Number(a) > Number(b) ? 'win' : Number(a) < Number(b) ? 'loss' : 'tie';
                r.oc.disabled = true;
                r.oc.title = 'Set automatically from the scores';
            } else {
                r.oc.disabled = false;
                r.oc.title = '';
            }
        };
        for (let n = 1; n <= 6; n++) {
            const theme = getVSTheme(dayDateStr(wk.week_date, n - 1));
            const d = byDay[n] || {};
            const oc = sel([['pending', '—'], ['win', 'Win'], ['loss', 'Loss'], ['tie', 'Tie']], d.outcome || 'pending');
            const our = inp('number', d.our_score != null ? d.our_score : '', 'our raw');
            const opp = inp('number', d.opponent_score != null ? d.opponent_score : '', 'opp raw');
            const mvpF = mvpField(d);
            const r = { n, oc, our, opp, mvpInput: mvpF.input, side: mvpF.side };
            rows.push(r);
            our.addEventListener('input', () => syncOutcome(r));
            opp.addEventListener('input', () => syncOutcome(r));
            syncOutcome(r);
            body.push(el('div', { className: 'vsl-day-entry' },
                el('div', { text: n + '. ' + (theme ? theme.short : '') + ' (' + vsLeagueDayPts(n) + 'pt)' }), oc, our, opp));
            body.push(el('div', { style: 'margin:-2px 0 8px 0;' }, field('MVP day ' + n, mvpF.wrap)));
        }
        modal('Daily results — Week ' + (wk.week_number || ''), body, async () => {
            const days = rows.map(r => ({
                day_number: r.n, outcome: r.oc.value,
                our_score: numOrNull(r.our.value), opponent_score: numOrNull(r.opp.value),
                mvp_name: strOrNull(r.mvpInput.value), mvp_is_ours: r.side.value === 'ours'
            }));
            await api('POST', '/api/vs-league/weeks/' + wk.id + '/days', { days });
            await selectSeason(state.seasonId);
        }, 'Save days');
    }

    async function openBracketModal(wk) {
        const MY_RANK = wk.league_rank != null ? Number(wk.league_rank) : null;

        const fetchMs = id => api('GET', '/api/vs-league/weeks/' + id + '/matchups').catch(() => []);
        const priorWeeks = state.weeks
            .filter(w => w.week_number != null && wk.week_number != null && w.week_number < wk.week_number)
            .sort((a, b) => a.week_number - b.week_number);
        const [thisMs, priorMs, known] = await Promise.all([
            fetchMs(wk.id),
            Promise.all(priorWeeks.map(w => fetchMs(w.id))),
            api('GET', '/api/external-alliances').catch(() => [])
        ]);
        const knownAlliances = known || [];

        // Learn each alliance (by tag) → { name, server }, its week-1 starting rank, and its W/L per
        // prior week. Ranking recomputes each week: most wins → earliest wins → starting rank; the
        // matchups are then always rank-adjacent (1v2, 3v4 …).
        const info = new Map();       // tagLower → { tag, name, server }
        const startRank = new Map();  // tagLower → starting (week-1) rank
        const weekRes = [];           // per prior week: Map(tagLower → 'W'|'L'|'T')
        const remember = s => {
            if (!s.tag) return null;
            const k = s.tag.toLowerCase();
            if (!info.has(k)) info.set(k, { tag: s.tag, name: s.name || '', server: s.server != null ? s.server : null });
            const rec = info.get(k);
            if (s.name) rec.name = s.name;
            if (s.server != null) rec.server = s.server;
            return k;
        };
        priorWeeks.forEach((w, wi) => {
            const res = new Map();
            (priorMs[wi] || []).forEach(m => {
                const A = { tag: m.a_tag, name: m.a_name, server: m.a_server, rank: m.a_rank, pts: m.a_points };
                const B = { tag: m.b_tag, name: m.b_name, server: m.b_server, rank: m.b_rank, pts: m.b_points };
                const ka = remember(A), kb = remember(B);
                if (w.week_number === 1) { if (ka && A.rank != null) startRank.set(ka, A.rank); if (kb && B.rank != null) startRank.set(kb, B.rank); }
                if (A.pts != null && B.pts != null && ka && kb) { const d = A.pts - B.pts; res.set(ka, d > 0 ? 'W' : d < 0 ? 'L' : 'T'); res.set(kb, d < 0 ? 'W' : d > 0 ? 'L' : 'T'); }
            });
            weekRes.push(res);
        });
        if (MY_TAG) remember({ tag: MY_TAG, name: MY_NAME, server: null });
        // Prior weeks may not have captured server numbers; backfill them from the registry by tag.
        {
            const byTag = new Map();
            knownAlliances.forEach(a => { if (a.tag) byTag.set(a.tag.toLowerCase(), a); });
            for (const [k, rec] of info) { if (rec.server == null && byTag.get(k) && byTag.get(k).server != null) rec.server = byTag.get(k).server; }
        }

        // Compute this week's rank order from prior results (only if week 1 is captured and every
        // prior pairing is decided). Sort: wins desc, then earliest-win, then starting rank.
        function computeRanks() {
            const keys = [...startRank.keys()];
            if (keys.length < 2) return null;
            for (const res of weekRes) for (const k of keys) { const v = res.get(k); if (v == null || v === 'T') return null; }
            const arr = keys.map(k => ({ k, timeline: weekRes.map(r => r.get(k)), start: startRank.get(k) }));
            arr.forEach(a => a.wins = a.timeline.filter(x => x === 'W').length);
            arr.sort((a, b) => {
                if (b.wins !== a.wins) return b.wins - a.wins;
                for (let i = 0; i < a.timeline.length; i++) if (a.timeline[i] !== b.timeline[i]) return a.timeline[i] === 'W' ? -1 : 1;
                return a.start - b.start;
            });
            return arr.map((x, i) => Object.assign({ rank: i + 1 }, info.get(x.k)));
        }

        // 16 rank slots (rank fixed = slot index + 1). Fill from this week's own capture (edit),
        // else the computed order, else leave blank (week 1 / not computable) with our own prefilled.
        const slots = Array.from({ length: 16 }, (_, i) => ({ rank: i + 1, tag: '', name: '', server: null, pts: null }));
        let note;
        if (thisMs && thisMs.length) {
            thisMs.forEach(m => {
                if (m.a_rank >= 1 && m.a_rank <= 16) slots[m.a_rank - 1] = { rank: m.a_rank, tag: m.a_tag || '', name: m.a_name || '', server: m.a_server, pts: m.a_points };
                if (m.b_rank >= 1 && m.b_rank <= 16) slots[m.b_rank - 1] = { rank: m.b_rank, tag: m.b_tag || '', name: m.b_name || '', server: m.b_server, pts: m.b_points };
            });
            note = 'Editing this week’s captured bracket.';
        } else {
            const ranked = wk.week_number > 1 ? computeRanks() : null;
            if (ranked) {
                ranked.forEach(a => { slots[a.rank - 1] = { rank: a.rank, tag: a.tag, name: a.name, server: a.server, pts: null }; });
                note = 'Ranks computed from prior results (most wins → earliest wins → starting rank). Matchups are 1v2, 3v4 … — just enter this week’s points.';
            } else {
                note = wk.week_number > 1
                    ? 'Prior weeks aren’t fully captured/scored yet, so ranks couldn’t be computed — fill each rank slot’s alliance and points.'
                    : 'Week-1 ranks are the game’s random starting order. Fill each rank slot’s alliance (they play 1v2, 3v4 …) and this week’s points.';
            }
        }

        // We already know our own alliance (rank + tag from settings) and this week's opponent, so
        // lock both of those slots — only their points stay editable — to prevent accidental edits.
        if (MY_RANK != null && MY_RANK >= 1 && MY_RANK <= 16 && MY_TAG) {
            const us = slots[MY_RANK - 1];
            const myServer = (info.get(MY_TAG.toLowerCase()) || {}).server;
            slots[MY_RANK - 1] = { rank: MY_RANK, tag: MY_TAG, name: MY_NAME, server: us.server != null ? us.server : (myServer != null ? myServer : null), pts: us.pts, locked: true };
            const oppRank = MY_RANK % 2 === 1 ? MY_RANK + 1 : MY_RANK - 1;
            if (oppRank >= 1 && oppRank <= 16 && wk.opponent_tag) {
                const op = slots[oppRank - 1];
                slots[oppRank - 1] = { rank: oppRank, tag: wk.opponent_tag, name: wk.opponent_name || op.name || '', server: wk.opponent_server != null ? wk.opponent_server : op.server, pts: op.pts, locked: true };
            }
        }

        // Per-tag alliance finder: registry type-ahead + LastRank lookup (needs the row's server).
        function attachFinder(tagInput, serverInput, row) {
            const dd = el('div', { className: 'vsl-find-dropdown vsl-bracket-dd', hidden: 'hidden' });
            const wrap = el('div', { className: 'vsl-tagwrap' }, tagInput, dd);
            const onDoc = e => { if (!wrap.contains(e.target)) close(); };
            function open() { dd.hidden = false; document.addEventListener('mousedown', onDoc); }
            function close() { dd.hidden = true; document.removeEventListener('mousedown', onDoc); }
            const pick = a => { tagInput.value = a.tag || ''; row.name = a.name || ''; if (a.server != null) serverInput.value = a.server; close(); };
            const localItems = q => knownAlliances.filter(a => (a.tag || '').toLowerCase().includes(q) || (a.name || '').toLowerCase().includes(q)).slice(0, 6);
            function render(locals, lrs, msg, isErr) {
                clear(dd);
                (locals || []).forEach((a, i) => {
                    if (i === 0) dd.appendChild(el('div', { className: 'vsl-find-head', text: 'Registry' }));
                    dd.appendChild(el('button', { className: 'vsl-find-item', type: 'button', onclick: () => pick(a) },
                        el('span', { className: 'vsl-find-name', text: (a.tag ? '[' + a.tag + '] ' : '') + (a.name || '') }),
                        a.server != null ? el('span', { className: 'vsl-find-meta', text: 'S' + a.server }) : null));
                });
                (lrs || []).forEach((a, i) => {
                    if (i === 0) dd.appendChild(el('div', { className: 'vsl-find-head', text: 'LastRank' }));
                    dd.appendChild(el('button', { className: 'vsl-find-item', type: 'button', onclick: () => pick({ tag: a.tag, name: a.name, server: a.server }) },
                        el('span', { className: 'vsl-find-name', text: (a.tag ? '[' + a.tag + '] ' : '') + (a.name || '') }),
                        el('span', { className: 'vsl-find-meta', text: [a.server != null ? 'S' + a.server : null, a.power != null ? fmtBig(a.power) + ' pw' : null].filter(Boolean).join(' · ') })));
                });
                if (msg) dd.appendChild(el('div', { className: isErr ? 'vsl-find-msg vsl-find-err' : 'vsl-find-msg', text: msg }));
                if (tagInput.value.trim()) dd.appendChild(el('button', { className: 'vsl-find-action', type: 'button', onclick: lookupLR }, '🔎 Look up on LastRank'));
                if (dd.childNodes.length) open(); else close();
            }
            async function lookupLR() {
                const q = tagInput.value.trim(), srv = serverInput.value.trim();
                if (!q) return;
                if (!srv) { render(localItems(q.toLowerCase()), null, 'Enter this alliance’s server # to search LastRank.', true); serverInput.focus(); return; }
                render(localItems(q.toLowerCase()), null, 'Searching LastRank…', false);
                try {
                    const list = await api('GET', '/api/external-alliances/search?q=' + encodeURIComponent(q) + '&server=' + encodeURIComponent(srv));
                    render(localItems(q.toLowerCase()), list, (list && list.length) ? null : 'No matches on server ' + srv + '.', false);
                } catch (e) { render(localItems(q.toLowerCase()), null, e.message, true); }
            }
            tagInput.addEventListener('input', () => {
                const q = tagInput.value.trim().toLowerCase();
                const exact = knownAlliances.find(a => (a.tag || '').toLowerCase() === q);
                row.name = exact ? (exact.name || '') : '';
                if (exact && exact.server != null && !serverInput.value) serverInput.value = exact.server;
                if (!q) { close(); return; }
                render(localItems(q), null, null, false);
                markOurs();
            });
            tagInput.addEventListener('focus', () => { const q = tagInput.value.trim().toLowerCase(); if (q) render(localItems(q), null, null, false); });
            tagInput.addEventListener('keydown', e => { if (e.key === 'Escape') close(); else if (e.key === 'Enter' && !dd.hidden) { e.preventDefault(); lookupLR(); } });
            return wrap;
        }

        const rows = [];
        const grid = el('div', { className: 'vsl-bracket-grid' });
        function markOurs() {
            let assigned = false;
            for (let i = 0; i < 8; i++) {
                const a = rows[2 * i], b = rows[2 * i + 1];
                const at = (a.tagInput.value || '').trim().toLowerCase(), bt = (b.tagInput.value || '').trim().toLowerCase();
                const mine = !assigned && ((MY_TAG && (at === MY_TAG.toLowerCase() || bt === MY_TAG.toLowerCase())) || (MY_RANK != null && (a.rank === MY_RANK || b.rank === MY_RANK)));
                if (mine) assigned = true;
                a.card.classList.toggle('ours', mine);
                a.badge.textContent = mine ? '★ ours' : '';
            }
        }
        const allianceRow = slot => {
            const serverInput = inp('number', slot.server != null ? slot.server : '', 'srv'); serverInput.className = 'form-input vsl-srv';
            const tagInput = inp('text', slot.tag || '', 'tag'); tagInput.className = 'form-input vsl-tag';
            const ptsInput = inp('number', slot.pts != null ? slot.pts : '', 'pts'); ptsInput.className = 'form-input vsl-pts2';
            const r = { rank: slot.rank, tagInput, serverInput, ptsInput, name: slot.name || '' };
            let tagNode;
            if (slot.locked) {
                // Known alliance (us / this week's opponent) — lock identity, leave points editable.
                tagInput.readOnly = true; tagInput.classList.add('vsl-locked');
                tagInput.title = slot.rank === MY_RANK ? 'Your alliance (locked)' : 'Your opponent this week (locked)';
                if (slot.server != null) { serverInput.readOnly = true; serverInput.classList.add('vsl-locked'); }
                tagNode = el('div', { className: 'vsl-tagwrap' }, tagInput);
            } else {
                tagNode = attachFinder(tagInput, serverInput, r);
            }
            return { r, node: el('div', { className: 'vsl-al-row' }, el('span', { className: 'vsl-rklbl', text: '#' + slot.rank }), serverInput, tagNode, ptsInput) };
        };
        for (let i = 0; i < 8; i++) {
            const A = allianceRow(slots[2 * i]), B = allianceRow(slots[2 * i + 1]);
            const badge = el('span', { className: 'vsl-pair-badge' });
            const card = el('div', { className: 'vsl-pair-card' },
                el('div', { className: 'vsl-pair-hd' }, el('span', { text: 'Pairing ' + (i + 1) }), badge),
                A.node, el('div', { className: 'vsl-pair-vs', text: 'vs' }), B.node);
            A.r.card = card; A.r.badge = badge; B.r.card = card; B.r.badge = badge;
            rows.push(A.r, B.r);
            grid.appendChild(card);
        }
        markOurs();

        const body = [el('p', { className: 'vsl-help', text: note }), grid];
        modal('Capture bracket — Week ' + (wk.week_number || ''), body, async () => {
            let oursAssigned = false;
            const matchups = [];
            for (let i = 0; i < 8; i++) {
                const a = rows[2 * i], b = rows[2 * i + 1];
                const aTag = strOrNull(a.tagInput.value), bTag = strOrNull(b.tagInput.value);
                if (!aTag && !bTag && a.ptsInput.value === '' && b.ptsInput.value === '') continue;
                const at = (a.tagInput.value || '').trim().toLowerCase(), bt = (b.tagInput.value || '').trim().toLowerCase();
                const mine = !oursAssigned && ((MY_TAG && (at === MY_TAG.toLowerCase() || bt === MY_TAG.toLowerCase())) || (MY_RANK != null && (a.rank === MY_RANK || b.rank === MY_RANK)));
                if (mine) oursAssigned = true;
                matchups.push({
                    match_index: i + 1,
                    a_rank: a.rank, a_tag: aTag, a_name: strOrNull(a.name), a_server: numOrNull(a.serverInput.value), a_points: numOrNull(a.ptsInput.value),
                    b_rank: b.rank, b_tag: bTag, b_name: strOrNull(b.name), b_server: numOrNull(b.serverInput.value), b_points: numOrNull(b.ptsInput.value),
                    is_ours: mine
                });
            }
            await api('POST', '/api/vs-league/weeks/' + wk.id + '/matchups', { matchups });
            // Derive our rank from the captured bracket (our slot's rank) and backfill the week.
            const ourMatch = matchups.find(m => m.is_ours);
            if (ourMatch) {
                const myTagL = (MY_TAG || '').toLowerCase();
                const myRank = (ourMatch.a_tag || '').toLowerCase() === myTagL ? ourMatch.a_rank
                    : (ourMatch.b_tag || '').toLowerCase() === myTagL ? ourMatch.b_rank
                        : ourMatch.a_rank === MY_RANK ? ourMatch.a_rank : ourMatch.b_rank;
                if (myRank != null && myRank !== wk.league_rank) await api('PUT', '/api/vs-league/weeks/' + wk.id, { league_rank: myRank }).catch(() => { });
            }
            await selectSeason(state.seasonId);
        }, 'Save bracket');
    }
})();
