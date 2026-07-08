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
            el('div', { className: 'vsl-form-grid' },
                field('Week #', weekNo), field('Week date (Mon)', weekDate), field('Our rank', rank), field('Tier', tier)),
            oppGrid,
            el('span', { className: 'vsl-help', text: 'Type an opponent tag or name to search your registry; enter the server to look up on LastRank.' }),
            field('Opponent LastRank link', el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap;' }, lrInput, lookupBtn)),
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

        // The 16 alliances + their ranks are constant all season, so learn rank↔tag/name from any
        // captured week (prior weeks, or this week if re-capturing) plus our own known alliance.
        const rankInfo = new Map();  // rank → { tag, name }
        const tagRank = new Map();   // tag(lowercase) → rank
        const learn = (rk, tg, nm) => {
            if (rk == null || !tg) return;
            const r = Number(rk);
            if (!rankInfo.has(r)) rankInfo.set(r, { tag: tg, name: nm || '' });
            tagRank.set(String(tg).toLowerCase(), r);
        };
        if (MY_RANK != null && MY_TAG) learn(MY_RANK, MY_TAG, MY_NAME);

        const priorWeeks = state.weeks
            .filter(w => w.week_number != null && wk.week_number != null && w.week_number < wk.week_number)
            .sort((a, b) => a.week_number - b.week_number);
        const fetchMs = id => api('GET', '/api/vs-league/weeks/' + id + '/matchups').catch(() => []);
        const [thisMs, ...priorMsArr] = await Promise.all([fetchMs(wk.id), ...priorWeeks.map(w => fetchMs(w.id))]);
        const allPrior = priorMsArr.map(x => x || []);
        allPrior.concat([thisMs || []]).forEach(list => list.forEach(m => { learn(m.a_rank, m.a_tag, m.a_name); learn(m.b_rank, m.b_tag, m.b_name); }));

        const tagOf = rk => (rankInfo.get(Number(rk)) || {}).tag || '';
        const nameOf = rk => (rankInfo.get(Number(rk)) || {}).name || '';
        const pad = arr => { while (arr.length < 8) arr.push({}); return arr.slice(0, 8); };

        // Predict this week's pairings from the immediately-prior week (winners vs winners, losers
        // vs losers, paired adjacently). Only when every prior pairing is decided (both points, no tie).
        function predict(prev) {
            if (!prev || !prev.length) return null;
            const winners = [], losers = [];
            for (const m of prev) {
                if (m.a_points == null || m.b_points == null || m.a_points === m.b_points) return null;
                const A = { rank: m.a_rank, tag: m.a_tag, name: m.a_name }, B = { rank: m.b_rank, tag: m.b_tag, name: m.b_name };
                const aWon = m.a_points > m.b_points;
                winners.push(aWon ? A : B); losers.push(aWon ? B : A);
            }
            const out = [];
            const pairUp = arr => { for (let i = 0; i + 1 < arr.length; i += 2) out.push({ aRank: arr[i].rank, aTag: arr[i].tag, aName: arr[i].name, bRank: arr[i + 1].rank, bTag: arr[i + 1].tag, bName: arr[i + 1].name }); };
            pairUp(winners); pairUp(losers);
            return out.length ? pad(out) : null;
        }

        let pairs, note;
        if (thisMs && thisMs.length) {
            pairs = pad(thisMs.slice().sort((a, b) => (a.match_index || 0) - (b.match_index || 0)).map(m => ({
                aRank: m.a_rank, aTag: m.a_tag, aName: m.a_name, aPts: m.a_points,
                bRank: m.b_rank, bTag: m.b_tag, bName: m.b_name, bPts: m.b_points
            })));
            note = 'Editing this week’s captured bracket.';
        } else {
            const predicted = predict(allPrior.length ? allPrior[allPrior.length - 1] : null);
            if (predicted) {
                pairs = predicted;
                note = 'Pairings predicted from Week ' + priorWeeks[priorWeeks.length - 1].week_number + ' (winners vs winners, losers vs losers) — verify against the game and adjust, then enter this week’s points.';
            } else {
                pairs = [];
                for (let i = 0; i < 8; i++) { const ar = 2 * i + 1, br = 2 * i + 2; pairs.push({ aRank: ar, aTag: tagOf(ar), aName: nameOf(ar), bRank: br, bTag: tagOf(br), bName: nameOf(br) }); }
                note = priorWeeks.length
                    ? 'Last week’s results are incomplete, so pairings couldn’t be predicted — assign them manually (tags auto-fill from rank).'
                    : 'Week-1 pairings are rank-adjacent (1v2, 3v4 … 15v16). Ranks are pre-set — fill each alliance’s tag and match points.';
            }
        }

        const rows = [];
        const grid = el('div', { className: 'vsl-bracket-grid' });
        function markOurs() {
            let assigned = false;
            rows.forEach(r => {
                const mine = !assigned && MY_RANK != null && (Number(r.aRank.value) === MY_RANK || Number(r.bRank.value) === MY_RANK);
                if (mine) assigned = true;
                r.card.classList.toggle('ours', mine);
                r.badge.textContent = mine ? '★ ours' : '';
            });
        }
        pairs.forEach((p, idx) => {
            const aRank = inp('number', p.aRank != null ? p.aRank : '', '#'); aRank.className = 'form-input vsl-rk';
            const bRank = inp('number', p.bRank != null ? p.bRank : '', '#'); bRank.className = 'form-input vsl-rk';
            const aTag = inp('text', p.aTag || '', 'tag');
            const bTag = inp('text', p.bTag || '', 'tag');
            const aPts = inp('number', p.aPts != null ? p.aPts : '', 'pts'); aPts.className = 'form-input vsl-pts';
            const bPts = inp('number', p.bPts != null ? p.bPts : '', 'pts'); bPts.className = 'form-input vsl-pts';
            const badge = el('span', { className: 'vsl-pair-badge' });
            const r = { aRank, aTag, aPts, bRank, bTag, bPts, aName: p.aName || '', bName: p.bName || '', badge };
            rows.push(r);
            const link = (rankEl, tagEl, which) => {
                rankEl.addEventListener('input', () => {
                    const info = rankInfo.get(Number(rankEl.value));
                    if (info) { tagEl.value = info.tag; r[which + 'Name'] = info.name; }
                    markOurs();
                });
                tagEl.addEventListener('input', () => {
                    const rk = tagRank.get(tagEl.value.trim().toLowerCase());
                    if (rk != null && !rankEl.value) rankEl.value = rk;
                    r[which + 'Name'] = (rankInfo.get(Number(rankEl.value)) || {}).name || '';
                    markOurs();
                });
            };
            link(aRank, aTag, 'a'); link(bRank, bTag, 'b');
            const side = (rankEl, tagEl, ptsEl) => el('div', { className: 'vsl-pair-side' },
                el('span', { className: 'vsl-rkhash', text: '#' }), rankEl, tagEl, ptsEl);
            const card = el('div', { className: 'vsl-pair-card' },
                el('div', { className: 'vsl-pair-hd' }, el('span', { text: 'Pairing ' + (idx + 1) }), badge),
                side(aRank, aTag, aPts),
                el('div', { className: 'vsl-pair-vs', text: 'vs' }),
                side(bRank, bTag, bPts));
            r.card = card;
            grid.appendChild(card);
        });
        markOurs();

        const body = [el('p', { className: 'vsl-help', text: note }), grid];
        modal('Capture bracket — Week ' + (wk.week_number || ''), body, async () => {
            let oursAssigned = false;
            const matchups = rows
                .map((r, i) => ({ i, r }))
                .filter(({ r }) => r.aTag.value.trim() || r.bTag.value.trim() || r.aRank.value || r.bRank.value)
                .map(({ i, r }) => {
                    const aRankN = numOrNull(r.aRank.value), bRankN = numOrNull(r.bRank.value);
                    const mine = !oursAssigned && MY_RANK != null && (aRankN === MY_RANK || bRankN === MY_RANK);
                    if (mine) oursAssigned = true;
                    return {
                        match_index: i + 1,
                        a_rank: aRankN, a_tag: strOrNull(r.aTag.value), a_name: strOrNull(r.aName), a_points: numOrNull(r.aPts.value),
                        b_rank: bRankN, b_tag: strOrNull(r.bTag.value), b_name: strOrNull(r.bName), b_points: numOrNull(r.bPts.value),
                        is_ours: mine
                    };
                });
            await api('POST', '/api/vs-league/weeks/' + wk.id + '/matchups', { matchups });
            await selectSeason(state.seasonId);
        }, 'Save bracket');
    }
})();
