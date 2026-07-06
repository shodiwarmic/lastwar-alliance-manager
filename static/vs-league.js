// static/vs-league.js — VS Duel League tab (lives on /vs alongside Member Points).
// Safe DOM only (own el() helper, textContent, addEventListener); tokens via CSS classes;
// showToast/showConfirm for feedback; CSRF injected globally by csrf.js.
'use strict';

(function () {
    const cfg = document.getElementById('page-config');
    const CAN_MANAGE = cfg && cfg.dataset.canManage === 'true';

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
    let state = { seasons: [], seasonId: null, season: null, weeks: [], currentWeekDate: null, view: 'current' };
    const root = document.getElementById('vs-league-root');

    // ===================== tab switching (points ↔ league) =====================
    // First-paint visibility comes from the server-rendered `active` class (F-R06); here we
    // only toggle the class (never inline display), and persist via URL hash.
    function showTab(name) {
        const valid = name === 'league' ? 'league' : 'points';
        document.querySelectorAll('#vs-tab-bar .tab-btn').forEach(b =>
            b.classList.toggle('active', b.dataset.tab === valid));
        const pts = document.getElementById('tab-points');
        const lg = document.getElementById('tab-league');
        if (pts) pts.classList.toggle('active', valid === 'points');
        if (lg) lg.classList.toggle('active', valid === 'league');
        if (('#' + valid) !== location.hash) history.replaceState(null, '', '#' + valid);
        if (valid === 'league' && !state.loaded) initLeague();
    }
    document.querySelectorAll('#vs-tab-bar .tab-btn').forEach(b =>
        b.addEventListener('click', () => showTab(b.dataset.tab)));
    document.addEventListener('DOMContentLoaded', () => {
        showTab(location.hash === '#league' ? 'league' : 'points');
    });
    // hash may already be set before DOMContentLoaded fires late — guard immediately too
    if (document.readyState !== 'loading' && location.hash === '#league') showTab('league');

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
        state.season = state.seasons.find(s => s.id === id) || null;
        try {
            const data = await api('GET', '/api/vs-league/weeks?season_id=' + id);
            state.weeks = data || [];
        } catch (e) { showToast(e.message, 'error'); state.weeks = []; }
        if (state.currentWeekDate == null) {
            try { const cur = await api('GET', '/api/vs-league/current'); state.currentWeekDate = cur.current_week_date; }
            catch { state.currentWeekDate = null; }
        }
        render();
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
            bar.appendChild(el('button', { className: 'btn btn-primary btn-sm', onclick: () => openWeekModal(null) }, '+ Week'));
        }
        return bar;
    }

    function renderSubtabs() {
        const wrap = el('div', { className: 'vsl-subtabs' });
        [['current', 'Current Week'], ['history', 'Season History'], ['bracket', 'Bracket'], ['analysis', 'Day Analysis']]
            .forEach(([k, label]) => {
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

    function renderView(view) {
        if (state.view === 'current') return renderCurrentWeek(view);
        if (state.view === 'history') return renderHistory(view);
        if (state.view === 'bracket') return renderBracket(view);
        if (state.view === 'analysis') return renderAnalysis(view);
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
        const wk = currentWeek();
        if (!wk) {
            view.appendChild(el('p', { className: 'vsl-empty', text: 'No weeks recorded yet.' + (CAN_MANAGE ? ' Add one with “+ Week”.' : '') }));
            return;
        }
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
                txt += ' — ' + (st.clinch_day === 6 ? 'went to Day 6 (Enemy Buster)' : 'clinched Day ' + st.clinch_day + (theme ? ' (' + theme.short + ')' : ''));
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
    function renderHistory(view) {
        if (!state.weeks.length) { view.appendChild(el('p', { className: 'vsl-empty', text: 'No weeks yet.' })); return; }
        const card = el('div', { className: 'card' });
        const table = el('table', { className: 'data-table' });
        const thead = el('thead', {}, el('tr', {},
            el('th', { text: 'Week' }), el('th', { text: 'Opponent' }), el('th', { text: 'Match Score' }),
            el('th', { text: 'Result' }), el('th', { text: 'Strategy' }), CAN_MANAGE ? el('th', { text: '' }) : null));
        table.appendChild(thead);
        const tb = el('tbody');
        state.weeks.forEach(wk => {
            const st = wk.standing;
            const opp = (wk.opponent_tag ? '[' + wk.opponent_tag + '] ' : '') + (wk.opponent_name || '—');
            const row = el('tr', {},
                el('td', { text: 'Week ' + (wk.week_number != null ? wk.week_number : '') }),
                el('td', { text: opp }),
                el('td', { text: st.our_points + ' – ' + st.opponent_points }),
                el('td', {}, pillFor(st)),
                el('td', {}, wk.strategy_label ? el('span', { className: 'vsl-strat ' + wk.strategy_label, text: wk.strategy_label }) : el('span', { className: 'vsl-help', text: '—' })),
                CAN_MANAGE ? el('td', {}, el('button', { className: 'btn btn-secondary btn-sm', onclick: () => openWeekModal(wk) }, 'Edit'),
                    el('button', { className: 'btn btn-danger btn-sm', onclick: () => deleteWeek(wk), style: 'margin-left:6px;' }, 'Del')) : null);
            tb.appendChild(row);
        });
        table.appendChild(tb);
        card.appendChild(el('div', { className: 'table-scroll' }, table));
        view.appendChild(card);
    }

    async function deleteWeek(wk) {
        if (!await showConfirm('Delete Week ' + (wk.week_number || '') + ' and all its data?', 'Delete')) return;
        try { await api('DELETE', '/api/vs-league/weeks/' + wk.id); showToast('Week deleted.'); await selectSeason(state.seasonId); }
        catch (e) { showToast(e.message, 'error'); }
    }

    // ---------- Bracket ----------
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
        if (!ms.length) {
            holder.appendChild(el('p', { className: 'vsl-empty', text: 'No bracket captured for this week.' + (CAN_MANAGE ? ' Use “Capture bracket” on the Current Week tab.' : '') }));
            return;
        }
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
    function renderAnalysis(view) {
        const themed = [0, 1, 2, 3, 4, 5].map(i => ({ w: 0, l: 0, t: 0, marginSum: 0, marginN: 0 }));
        state.weeks.forEach(wk => (wk.days || []).forEach(d => {
            const i = d.day_number - 1;
            if (i < 0 || i > 5) return;
            if (d.outcome === 'win') themed[i].w++;
            else if (d.outcome === 'loss') themed[i].l++;
            else if (d.outcome === 'tie') themed[i].t++;
            if (d.our_score != null && d.opponent_score != null) { themed[i].marginSum += (d.our_score - d.opponent_score); themed[i].marginN++; }
        }));
        const card = el('div', { className: 'card' });
        card.appendChild(el('div', { className: 'card-header' }, el('div', {}, el('h2', { text: 'Day-of-week analysis' }),)));
        let any = false;
        for (let i = 0; i < 6; i++) {
            const s = themed[i];
            const total = s.w + s.l + s.t;
            if (total > 0) any = true;
            const theme = getVSTheme(dayDateStr(state.weeks[0] ? state.weeks[0].week_date : todayISO(), i));
            const bar = el('div', { className: 'vsl-wl' });
            if (total > 0) {
                bar.appendChild(el('span', { className: 'w', style: 'flex:' + s.w }));
                bar.appendChild(el('span', { className: 'l', style: 'flex:' + s.l }));
                bar.appendChild(el('span', { className: 't', style: 'flex:' + s.t }));
            }
            const marginTxt = s.marginN ? (s.marginSum / s.marginN >= 0 ? '+' : '') + Math.round(s.marginSum / s.marginN / 1000) + 'k avg' : '—';
            card.appendChild(el('div', { className: 'vsl-analysis-row' },
                el('div', { text: (theme ? theme.icon + ' ' + theme.short : 'Day ' + (i + 1)) }),
                bar,
                el('div', { className: 'vsl-help', text: total ? (s.w + 'W ' + s.l + 'L' + (s.t ? ' ' + s.t + 'T' : '')) : 'no data' })));
        }
        if (!any) card.appendChild(el('p', { className: 'vsl-empty', text: 'Enter daily results to see which theme days you win and lose most.' }));
        view.appendChild(card);
    }

    // ===================== helpers =====================
    function tile(lbl, val, sub) {
        return el('div', { className: 'vsl-tile' }, el('div', { className: 'lbl', text: lbl }),
            el('div', { className: 'val', text: val }), sub ? el('div', { className: 'sub', text: sub }) : null);
    }
    function fmtBig(n) { n = Number(n); if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'; if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'; if (n >= 1e3) return (n / 1e3).toFixed(0) + 'K'; return '' + n; }
    function vsLeagueDayPts(n) { return [1, 2, 2, 2, 2, 4][n - 1] || 0; }
    function todayISO() { return (window.gameDateStr ? window.gameDateStr() : new Date().toISOString().slice(0, 10)); }
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
        const start = inp('date', todayISO());
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

    function openWeekModal(wk) {
        const weekNo = inp('number', wk ? wk.week_number : '', '1–4');
        const weekDate = inp('date', wk ? wk.week_date : (state.currentWeekDate || todayISO()));
        const rank = inp('number', wk ? wk.league_rank : '', '1–16');
        const tier = inp('text', wk ? wk.league_tier : '', 'tier this week');
        const oppTag = inp('text', wk ? wk.opponent_tag : '', 'PoWr');
        const oppName = inp('text', wk ? wk.opponent_name : '', 'Pantheon of Wrath');
        const oppServer = inp('number', wk ? wk.opponent_server : '', 'server #');
        const stratLabel = sel([['', '—'], ['push', 'Push'], ['save', 'Save'], ['normal', 'Normal'], ['test', 'Test'], ['recovery', 'Recovery']], wk && wk.strategy_label || '');
        const stratResult = sel([['', '—'], ['worked', 'Worked'], ['failed', 'Failed'], ['mixed', 'Mixed']], wk && wk.strategy_result || '');
        const notes = el('textarea', { className: 'form-input', rows: '2', placeholder: 'leadership context…' }); if (wk && wk.notes) notes.value = wk.notes;
        // opponent lookup
        const lrInput = inp('text', wk ? wk.opponent_lastrank_id : '', 'paste lastrank.fun/a/… link');
        lrInput.style.flex = '1'; lrInput.style.minWidth = '150px';
        const snapNote = el('span', { className: 'vsl-help' });
        let snap = null;
        // LastRank has a unified search (?q=); prefill it from the opponent tag + server.
        const searchBtn = el('a', { className: 'btn btn-secondary btn-sm', target: '_blank', rel: 'noopener noreferrer' }, 'Search ↗');
        const updateSearchHref = () => {
            // Match LastRank's alliance display format: "#1713 [cROw] Black Crow Legion".
            const parts = [];
            if (oppServer.value.trim()) parts.push('#' + oppServer.value.trim());
            if (oppTag.value.trim()) parts.push('[' + oppTag.value.trim() + ']');
            if (oppName.value.trim()) parts.push(oppName.value.trim());
            const q = parts.join(' ');
            searchBtn.href = 'https://lastrank.fun/search' + (q ? '?q=' + encodeURIComponent(q) : '');
        };
        [oppTag, oppName, oppServer].forEach(f => f.addEventListener('input', updateSearchHref));
        updateSearchHref();
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
            el('div', { className: 'vsl-form-grid' },
                field('Week #', weekNo), field('Week date (Mon)', weekDate), field('Our rank', rank), field('Tier', tier)),
            el('div', { className: 'vsl-form-grid' }, field('Opp tag', oppTag), field('Opp name', oppName), field('Opp server', oppServer)),
            field('Opponent LastRank link', el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap;' }, searchBtn, lrInput, lookupBtn)),
            el('span', { className: 'vsl-help', text: 'Search LastRank → open the opponent’s alliance → copy the /a/… link → paste → Look up.' }),
            snapNote,
            el('div', { className: 'vsl-form-grid' }, field('Strategy', stratLabel), field('Result', stratResult)),
            field('Notes', notes)
        ], async () => {
            const payload = {
                season_id: state.seasonId, week_number: numOrNull(weekNo.value), week_date: weekDate.value,
                league_rank: numOrNull(rank.value), league_tier: strOrNull(tier.value),
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
        const body = [el('p', { className: 'vsl-help', text: 'Enter who won each day (and optional raw Alliance Duel Scores). Empty days are left pending.' })];
        for (let n = 1; n <= 6; n++) {
            const theme = getVSTheme(dayDateStr(wk.week_date, n - 1));
            const d = byDay[n] || {};
            const oc = sel([['pending', '—'], ['win', 'Win'], ['loss', 'Loss'], ['tie', 'Tie']], d.outcome || 'pending');
            const our = inp('number', d.our_score != null ? d.our_score : '', 'our raw');
            const opp = inp('number', d.opponent_score != null ? d.opponent_score : '', 'opp raw');
            const mvp = inp('text', d.mvp_name || '', 'MVP (ours)');
            rows.push({ n, oc, our, opp, mvp });
            body.push(el('div', { className: 'vsl-day-entry' },
                el('div', { text: n + '. ' + (theme ? theme.short : '') + ' (' + vsLeagueDayPts(n) + 'pt)' }), oc, our, opp));
            body.push(el('div', { style: 'margin:-2px 0 8px 0;' }, field('MVP day ' + n, mvp)));
        }
        modal('Daily results — Week ' + (wk.week_number || ''), body, async () => {
            const days = rows.map(r => ({
                day_number: r.n, outcome: r.oc.value,
                our_score: numOrNull(r.our.value), opponent_score: numOrNull(r.opp.value),
                mvp_name: strOrNull(r.mvp.value), mvp_is_ours: true
            }));
            await api('POST', '/api/vs-league/weeks/' + wk.id + '/days', { days });
            await selectSeason(state.seasonId);
        }, 'Save days');
    }

    function openBracketModal(wk) {
        const rows = [];
        const body = [el('p', { className: 'vsl-help', text: 'Enter the weekly Match Record (up to 8 pairings). Check “ours” on your matchup.' })];
        for (let i = 1; i <= 8; i++) {
            const aR = inp('number', '', '#'); const aT = inp('text', '', 'tag'); const aP = inp('number', '', 'pts');
            const bR = inp('number', '', '#'); const bT = inp('text', '', 'tag'); const bP = inp('number', '', 'pts');
            const ours = el('input', { type: 'checkbox' });
            rows.push({ i, aR, aT, aP, bR, bT, bP, ours });
            body.push(el('div', { style: 'display:grid;grid-template-columns:20px repeat(6,1fr) 44px;gap:5px;align-items:center;margin-bottom:4px;' },
                el('span', { className: 'vsl-help', text: i }), aR, aT, aP, bR, bT, bP,
                el('label', { className: 'vsl-help', style: 'display:flex;gap:3px;align-items:center;' }, ours, 'ours')));
        }
        modal('Capture bracket — Week ' + (wk.week_number || ''), body, async () => {
            const matchups = rows.filter(r => r.aT.value.trim() || r.bT.value.trim()).map(r => ({
                match_index: r.i, a_rank: numOrNull(r.aR.value), a_tag: strOrNull(r.aT.value), a_points: numOrNull(r.aP.value),
                b_rank: numOrNull(r.bR.value), b_tag: strOrNull(r.bT.value), b_points: numOrNull(r.bP.value), is_ours: r.ours.checked
            }));
            await api('POST', '/api/vs-league/weeks/' + wk.id + '/matchups', { matchups });
            await selectSeason(state.seasonId);
        }, 'Save bracket');
    }
})();
