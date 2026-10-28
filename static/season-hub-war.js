/* season-hub-war.js — the Season Hub's War tab (private-docs 277–280): opponents and their
   observations, the matchup score beside the leadership's override, the live player pairing,
   and the season's War settings. Instants are server time (ST). */

(function () {
    'use strict';
    if (!document.getElementById('tab-war') || !window.SeasonHub) return;

    const cfg = document.getElementById('page-config').dataset;
    const CAN_MANAGE = cfg.canManageWar === 'true';
    const CAN_CONFIGURE = cfg.canConfigureWar === 'true';

    const RATINGS = {
        very_favorable: ['Very Favorable', 'success'], favorable: ['Favorable', 'success'], even: ['Even', 'info'],
        unfavorable: ['Unfavorable', 'warning'], very_unfavorable: ['Very Unfavorable', 'danger'],
        insufficient_data: ['Insufficient data', 'neutral'],
    };
    const RELATIONSHIPS = [['enemy', 'Enemy'], ['allied', 'Allied'], ['neutral', 'Neutral'], ['unknown', 'Unknown']];
    const SORTS = [['rating', 'Matchup'], ['power', 'Power'], ['rank', 'Group rank'], ['tag', 'Tag']];

    let season = null;
    let data = null;
    let loaded = 0;
    let sortBy = 'rating';
    let registry = null;      // the alliance registry, fetched once, for the add finder
    let pendingOpponent = null; // { warID, allianceID }: an opponent just added from a war card
    const players = {};       // season alliance id → the Scout Report's members (browser only)
    const gen = { n: 0 };

    // ── DOM helper (dashboard.js's el(), copied: page-local) ─────────────────
    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        if (props) {
            Object.entries(props).forEach(([k, v]) => {
                if (v == null || v === false) return;
                if (k === 'className') node.className = v;
                else if (k === 'textContent') node.textContent = v;
                else if (k === 'value') node.value = v;
                else if (k === 'checked') node.checked = !!v;
                else if (k.startsWith('on')) node.addEventListener(k.slice(2), v);
                else node.setAttribute(k, v === true ? '' : v);
            });
        }
        children.flat().forEach(c => {
            if (c == null || c === false) return;
            node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
        });
        return node;
    }
    function select(options, value, props) {
        const s = el('select', Object.assign({ className: 'form-input' }, props || {}));
        options.forEach(([v, t]) => s.appendChild(el('option', { value: v, textContent: t })));
        s.value = value == null ? '' : String(value);
        return s;
    }
    function field(label, control, extraClass) {
        return el('label', { className: 'land-field' + (extraClass ? ' ' + extraClass : '') }, el('span', { textContent: label }), control);
    }
    const num = (v, props) => el('input', Object.assign({ type: 'number', className: 'form-input', min: '0', value: v ?? '' }, props || {}));
    const readNum = inp => inp.value === '' ? null : Number(inp.value);
    const nowST = () => { const g = new Date(Date.now() - 2 * 3600e3); return g.toISOString().slice(0, 16); };
    const editable = () => CAN_MANAGE && season && !season.archived_at;

    // Click-to-sort headers (TableSort): [label, key, type] per column; key '' is not sortable.
    // The state outlives a re-render, so a re-drawn table keeps its order.
    const sortStates = {};
    function sortHead(cols) {
        return el('thead', null, el('tr', null, cols.map(([t, key, type]) =>
            el('th', { textContent: t, translate: 'yes', 'data-sort': key || null, 'data-sort-type': key ? (type || 'text') : null }))));
    }
    function sortable(table, name) {
        if (!window.TableSort) return table;
        sortStates[name] = sortStates[name] || { col: null, asc: true };
        TableSort.attach(table, { state: sortStates[name] }).apply();
        return table;
    }

    const compact = v => v == null ? '—' : formatCompact(v);
    const utcToST = s => {
        if (!s) return '';
        const d = new Date(s.replace(' ', 'T') + 'Z');
        if (isNaN(d)) return '';
        return new Date(d.getTime() - 2 * 3600e3).toISOString().slice(0, 16).replace('T', ' ');
    };

    async function api(url, opts) {
        const res = await fetch(url, opts);
        if (!res.ok) throw new Error((await res.text()) || 'Request failed');
        return res.json();
    }
    const send = (method, url, body) => api(url, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });

    // ── Loading ───────────────────────────────────────────────────────────────
    function load(force) {
        if (!season) return;
        if (!force && loaded === season.id && data) return;
        const sid = season.id;
        const mine = ++gen.n;
        api('/api/season-hub/war?season_id=' + sid)
            .then(d => {
                if (mine !== gen.n || !season || season.id !== sid) return;
                data = d;
                loaded = sid;
                render();
            })
            .catch(err => showToast(err.message || 'Failed to load War.', 'error'));
    }
    window.SeasonHub.onSeason(s => {
        if (!season || season.id !== s.id) data = null;
        season = s;
        if (window.SeasonHub.tab === 'war') load();
    });
    window.SeasonHub.onTab(name => { if (name === 'war') load(); });

    function render() {
        renderOwn();
        renderRecord();
        loadPollReviews();
        renderResources();
        renderWars();
        renderOpponents();
        renderPosts();
        renderBattles();
        renderObservations();
        renderSettings();
        document.dispatchEvent(new CustomEvent('seasonwar:render', { detail: { season, data } }));
    }

    function ratingBadge(key, score) {
        const [label, tone] = RATINGS[key] || [key, 'neutral'];
        return el('span', { className: 'land-status tone-' + tone, textContent: label + (score != null ? ' · ' + (score > 0 ? '+' : '') + score.toFixed(2) + '%' : '') });
    }

    function figureLine(label, f) {
        return el('div', { className: 'war-figure' }, el('span', { className: 'war-figure-label', textContent: label }),
            el('strong', { textContent: compact(f && f.value) }),
            f && f.at ? el('span', { className: 'war-figure-asof', textContent: 'as of ' + utcToST(f.at) + ' ST' + (f.source ? ' · ' + f.source : '') }) : null);
    }

    // ── Poll reviews (private-docs 285): voted Yes, did not attend ───────────
    function loadPollReviews() {
        const box = document.getElementById('war-poll-reviews');
        if (!box || !season) return;
        const sid = season.id;
        api('/api/season-hub/poll-reviews?season_id=' + sid).then(d => {
            if (!season || season.id !== sid) return;
            const list = d.reviews || [];
            if (!list.length) { box.replaceChildren(); return; }
            const ro = !!season.archived_at;
            const rows = list.map(p => {
                const row = el('div', { className: 'poll-review-row' },
                    el('span', { className: 'member-rank rank-' + p.member_rank, textContent: p.member_rank }),
                    el('strong', { translate: 'no', textContent: p.member_name }),
                    el('span', { className: 'land-drop-when', textContent: 'voted Yes, did not attend ' + p.war }));
                const decide = decision => send('POST', '/api/season-hub/poll-reviews', { war_id: p.war_id, member_id: p.member_id, decision })
                    .then(() => { showToast(decision === 'applied' ? 'Strike applied.' : 'Dismissed.'); loadPollReviews(); })
                    .catch(err => showToast(err.message, 'error'));
                const acts = el('div', { className: 'poll-review-actions' },
                    !ro && d.can_apply ? el('button', { type: 'button', className: 'btn btn-danger btn-sm', onclick: () => decide('applied') }, 'Apply strike') : null,
                    !ro && d.can_dismiss ? el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => decide('dismissed') }, 'Dismiss') : null);
                row.appendChild(acts);
                return row;
            });
            box.replaceChildren(el('div', { className: 'card land-section poll-reviews' },
                el('h3', { className: 'land-section-title', textContent: 'Poll reviews (' + list.length + ')' }),
                el('p', { className: 'hub-section-note', textContent: 'Members who voted Yes on a completed war\'s poll and did not attend. Applying files a strike; nothing changes a weekly score.' }),
                ...rows));
        }).catch(() => box.replaceChildren());
    }

    // ── Our record ───────────────────────────────────────────────────────────
    const pctText = r => r.pct == null ? '—' : r.pct.toFixed(0) + '%';
    const wlText = r => r.wins + '–' + r.losses + (r.draws ? ' (' + r.draws + ' drawn)' : '') + (r.unknown ? ' (' + r.unknown + ' unknown)' : '');
    function renderRecord() {
        const box = document.getElementById('war-record');
        const r = data.record;
        const line = (label, x) => el('div', { className: 'war-figure' }, el('span', { className: 'war-figure-label', textContent: label }),
            el('strong', { textContent: wlText(x) }), el('span', { className: 'war-figure-asof', textContent: 'win rate ' + pctText(x) }));
        box.replaceChildren(el('h3', { className: 'land-section-title', textContent: 'Our war record' }),
            el('div', { className: 'war-figures' }, line('Overall', r.overall), line('Attacking', r.attack), line('Defending', r.defense)),
            el('p', { className: 'hub-section-note', textContent: 'Completed wars whose battle date has come. Draws and unknown results are listed, not counted.' }));
    }

    // ── Resources, rank goals, loot (private-docs 279) ───────────────────────
    const TIER_STATUS = { on_track: ['On Track', 'success'], at_risk: ['At Risk', 'warning'], behind: ['Behind', 'danger'], unknown: ['Unknown', 'neutral'] };
    const signed = v => v == null ? '—' : (v > 0 ? '' : '−') + formatCompact(Math.abs(v));

    function renderResources() {
        const box = document.getElementById('war-resources');
        const r = data.resources;
        const ro = !editable();
        const [label, tone] = TIER_STATUS[r.status] || [r.status, 'neutral'];
        const fig = (name, value, note) => el('div', { className: 'war-figure' }, el('span', { className: 'war-figure-label', textContent: name }),
            el('strong', { textContent: value }), note ? el('span', { className: 'war-figure-asof', textContent: note }) : null);
        const own = r.own;
        const summary = el('div', { className: 'card land-section' },
            el('div', { className: 'land-readiness-head' }, el('h3', { textContent: 'Reward Tier Status' }),
                el('span', { className: 'land-status tone-' + tone, textContent: label })),
            el('div', { className: 'war-figures' },
                fig('Our total', own ? compact(own.resource_total) : '—', own ? 'as of ' + own.observed_at + ' ST · ' + own.source : 'not recorded yet'),
                fig('Projection now', r.projected_now != null ? compact(r.projected_now) : '—', 'projection · ' + compact(r.per_hour) + '/h from held cities'),
                fig('Projection at reset', r.projected_at_reset != null ? compact(r.projected_at_reset) : '—', 'projection · reset ' + r.next_reset + ' ST' +
                    (r.production_stop ? ' · production stops ' + r.production_stop + ' ST' : '') + (r.stopped ? ' (stopped)' : '')),
                fig('War rank', r.rank != null ? '#' + r.rank : '—', r.group ? r.group.label : (r.rank != null ? 'no reward group' : '')),
                fig('Target', r.target_rank != null ? '#' + r.target_rank : '—', r.target_group ? r.target_group.label : (r.target_rank == null ? 'set in War settings' : '')),
                fig('Gap to target', r.gap == null ? '—' : r.gap <= 0 ? 'past it' : compact(r.gap),
                    r.target_cutoff ? 'cutoff ' + compact(r.target_cutoff.resource_total) + ' as of ' + r.target_cutoff.observed_at + ' ST' : 'no cutoff recorded for the target rank'),
                fig('Gap to the rank above', r.gap_next_above == null ? '—' : r.gap_next_above <= 0 ? 'past it' : compact(r.gap_next_above),
                    r.next_above ? 'rank #' + r.next_above.rank : '')),
            r.unknown_output && r.unknown_output.length ? el('p', { className: 'hub-section-note', textContent: 'No output known for: ' + r.unknown_output.join(', ') + ' — set the level table in Land settings.' }) : null,
            el('p', { className: 'hub-section-note', textContent: 'Projections add what held cities produce since our last recorded total; they are never saved as a balance.' }));
        const parts = [summary];

        if (!ro) {
            const who = select([['own', 'Our alliance']].concat(data.opponents.map(o => [String(o.id), '[' + o.tag + '] S' + o.server])), 'own', { translate: 'no' });
            const total = num(), rank = num(null, { min: '1' });
            const at = el('input', { type: 'datetime-local', className: 'form-input', value: nowST() });
            const save = el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                const body = { season_id: season.id, resource_total: readNum(total), war_rank: readNum(rank), observed_at: at.value.replace('T', ' ') };
                if (who.value === 'own') body.own = true; else body.season_alliance_id = parseInt(who.value, 10);
                send('POST', '/api/season-hub/war/resources', body).then(d => { showToast(d.written ? 'Total recorded.' : 'Already recorded.'); load(true); })
                    .catch(err => showToast(err.message, 'error'));
            } }, 'Record total');
            const cRank = num(null, { min: '1' }), cTotal = num();
            const cAt = el('input', { type: 'datetime-local', className: 'form-input', value: nowST() });
            const cSave = el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                send('POST', '/api/season-hub/war/cutoffs', { season_id: season.id, rank: readNum(cRank) || 0, resource_total: readNum(cTotal), observed_at: cAt.value.replace('T', ' ') })
                    .then(d => { showToast(d.written ? 'Cutoff recorded.' : 'Already recorded.'); load(true); })
                    .catch(err => showToast(err.message, 'error'));
            } }, 'Record cutoff');
            const imp = (kind, title, columns) => el('button', { type: 'button', className: 'btn btn-secondary btn-sm',
                onclick: () => window.SeasonHub.openImport({ kind, title, columns, onDone: () => load(true) }) }, 'Import ' + title);
            parts.push(el('details', { className: 'war-obs-form' }, el('summary', null, 'Record a total or a cutoff'),
                el('div', { className: 'land-target-grid' }, field('Alliance', who), field('Total', total), field('War rank', rank), field('Observed (ST)', at)),
                el('div', { className: 'tab-toolbar' }, save, imp('resource_observations', 'totals',
                    'Columns: Total, required; Alliance and Server (blank Alliance, or our own tag, is ours), Rank, Observed at (YYYY-MM-DD HH:MM, server time), Note.')),
                el('div', { className: 'land-target-grid' }, field('Rank', cRank), field('Total at that rank', cTotal), field('Observed (ST)', cAt)),
                el('div', { className: 'tab-toolbar' }, cSave, imp('rank_cutoffs', 'cutoffs', 'Columns: Rank and Total, required; Observed at, Note.'))));
        }

        // Cutoffs, latest per rank, with their group.
        const groupOf = rank => (r.groups || []).find(g => rank >= g.rank_from && rank <= g.rank_to);
        if ((r.cutoffs || []).length) {
            parts.push(el('div', { className: 'war-cutoffs' }, r.cutoffs.map(c => el('div', { className: 'war-cat' },
                el('span', { textContent: '#' + c.rank + (groupOf(c.rank) ? ' · ' + groupOf(c.rank).label : '') }),
                el('strong', { textContent: compact(c.resource_total) }),
                el('span', { className: 'war-figure-asof', textContent: 'as of ' + c.observed_at + ' ST' })))));
        }

        // Loot scenarios: one card per opponent with a total, each step against our gap.
        const loot = (r.loot || []).map(o => el('div', { className: 'war-cat war-loot' },
            el('div', { className: 'land-target-head' }, el('strong', { translate: 'no', textContent: '[' + o.tag + '] S' + o.server }),
                ratingBadge(o.rating), o.override ? el('span', { className: 'war-override' }, 'Leadership: ', ratingBadge(o.override)) : null),
            el('span', { className: 'war-figure-asof', textContent: 'Total ' + compact(o.total) + (o.war_rank ? ' · rank #' + o.war_rank : '') + ' · as of ' + o.observed_at + ' ST' }),
            el('div', { className: 'war-loot-steps' }, o.scenarios.map(s => el('div', { className: 'war-loot-step' },
                el('span', { textContent: s.pct + '%' }), el('strong', { textContent: compact(s.amount) }),
                el('small', { textContent: r.gap == null ? 'gap unknown' : r.gap <= 0 ? 'no gap' : s.of_gap + '% of gap' }))))));
        parts.push(el('div', { className: 'war-loot-list' }, loot.length ? loot
            : [el('p', { className: 'loading-msg', textContent: 'Loot scenarios appear once an opponent\'s total is recorded.' })]));
        box.replaceChildren(...parts);
    }

    // ── Wars ─────────────────────────────────────────────────────────────────
    const STATUS_LABELS = { considering: 'Considering', planned: 'Planned', declared: 'Declared', incoming: 'Incoming',
        confirmed: 'Confirmed', completed: 'Completed', cancelled: 'Cancelled' };
    const STATUS_TONES = { considering: 'neutral', planned: 'info', declared: 'warning', incoming: 'warning', confirmed: 'warning', completed: 'success', cancelled: 'neutral' };
    const FLOW = { considering: ['planned', 'cancelled'], planned: ['considering', 'declared', 'cancelled'], declared: ['planned', 'completed', 'cancelled'],
        incoming: ['confirmed', 'cancelled'], confirmed: ['incoming', 'completed', 'cancelled'], completed: [], cancelled: ['considering', 'incoming'] };
    const OUTCOMES = [['', '—'], ['win', 'Win'], ['loss', 'Loss'], ['draw', 'Draw'], ['unknown', 'Unknown']];
    const ROLES = [['attack', 'Attack'], ['defense', 'Defense']];
    const opponentOptions = () => [['', '— none —']].concat(data.opponents.map(o => [String(o.id), '[' + o.tag + '] ' + (o.name || '') + ' · S' + o.server]));
    const memberOptions = () => [['', '— none —']].concat((window.SeasonHub.members || []).filter(m => m.rank !== 'EX').map(m => [String(m.member_id), m.name]));
    const linkOptions = kind => [['', '— none —']].concat(((data.links || {})[kind] || []).map(f => [String(f.id), f.title]));
    const intOrNull = v => v === '' ? null : parseInt(v, 10);

    function calendarNote(d) {
        const c = d.calendar || {};
        if (c.action === 'refused') showToast('Saved. Not on the calendar: ' + c.message, 'info', 8000);
        else showToast('Saved.');
    }

    function renderWars() {
        const box = document.getElementById('war-wars');
        const ro = !editable();
        const bar = el('div', { className: 'tab-toolbar war-toolbar' },
            ro ? null : el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                send('POST', '/api/season-hub/war/wars', { season_id: season.id, role: 'attack', direction: 'outgoing' })
                    .then(() => load(true)).catch(err => showToast(err.message, 'error'));
            } }, '+ War we declare'),
            ro ? null : el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => {
                send('POST', '/api/season-hub/war/wars', { season_id: season.id, role: 'defense', direction: 'incoming' })
                    .then(() => load(true)).catch(err => showToast(err.message, 'error'));
            } }, '+ War declared on us'),
            ro ? null : el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => window.SeasonHub.openImport({
                kind: 'war_results', title: 'war results',
                columns: 'Columns: Date (YYYY-MM-DD) and Role (attack or defense) name the war; Outcome (win, loss, draw, unknown), required; Participants, Resources. Completing a war stays a step on its card.',
                onDone: () => load(true) }) }, 'Import results'));
        const cards = data.wars.map(buildWarCard);
        box.replaceChildren(bar, ...(cards.length ? cards : [el('p', { className: 'loading-msg', textContent: 'No wars yet.' })]));
    }

    function buildWarCard(w) {
        const ro = !editable();
        const opp = data.opponents.find(o => o.id === w.opponent_season_alliance_id);
        const head = el('div', { className: 'land-target-head' },
            el('strong', { translate: 'no', textContent: (w.role === 'attack' ? 'Attack' : 'Defense') + (w.opponent_tag ? ' vs [' + w.opponent_tag + '] ' + w.opponent_name : '') }),
            el('span', { className: 'land-status tone-' + (STATUS_TONES[w.status] || 'neutral'), textContent: STATUS_LABELS[w.status] || w.status }),
            opp ? ratingBadge(opp.matchup.rating, opp.matchup.score) : null,
            opp && opp.assessment_override ? el('span', { className: 'war-override' }, 'Leadership: ', ratingBadge(opp.assessment_override)) : null,
            w.outcome ? el('span', { className: 'land-status tone-' + (w.outcome === 'win' ? 'success' : w.outcome === 'loss' ? 'danger' : 'neutral'), textContent: w.outcome }) : null);
        const meta = el('div', { className: 'land-drop-when' }, [w.battle_date ? w.battle_date + (w.battle_time ? ' ' + w.battle_time + ' ST' : '') : 'No date yet',
            w.week_number ? 'Week ' + w.week_number : null, w.direction === 'incoming' ? 'declared on us' : 'we declare',
            w.priority ? w.priority + ' priority' : null, w.participant_count != null ? w.participant_count + ' took part' : null].filter(Boolean).join(' · '));
        const ownFlow = { outgoing: ['considering', 'planned', 'declared', 'completed', 'cancelled'], incoming: ['incoming', 'confirmed', 'completed', 'cancelled'] }[w.direction];
        const next = (FLOW[w.status] || []).filter(s => ownFlow.includes(s));
        const status = select([[w.status, STATUS_LABELS[w.status]]].concat(next.map(s => [s, '→ ' + STATUS_LABELS[s]])), w.status, { disabled: ro });
        const role = select(ROLES, w.role, { disabled: ro });
        // An opponent added from this card arrives picked, for the officer to save.
        const picked = pendingOpponent && pendingOpponent.warID === w.id ? pendingOpponent.allianceID : null;
        if (picked) pendingOpponent = null;
        const opponent = select(opponentOptions().concat(ro ? [] : [['__add', '+ Add an opponent…']]),
            picked || w.opponent_season_alliance_id, { disabled: ro, translate: 'no' });
        let lastOpponent = opponent.value;
        opponent.addEventListener('change', () => {
            if (opponent.value !== '__add') { lastOpponent = opponent.value; return; }
            opponent.value = lastOpponent;
            openAddOpponent(w.id);
        });
        const date = el('input', { type: 'date', className: 'form-input', value: w.battle_date || '', disabled: ro });
        const time = el('input', { type: 'time', className: 'form-input', value: w.battle_time || '', disabled: ro });
        const priority = select([['', '—'], ['high', 'High'], ['medium', 'Medium'], ['low', 'Low']], w.priority || '', { disabled: ro });
        const declaredBy = select(memberOptions(), w.declared_by_member_id, { disabled: ro, translate: 'no' });
        const plannedDecl = el('input', { type: 'datetime-local', className: 'form-input', value: (w.planned_declaration_at || '').replace(' ', 'T'), disabled: ro });
        const outcome = select(OUTCOMES, w.outcome || '', { disabled: ro });
        const participants = num(w.participant_count, { max: '200', disabled: ro });
        const resources = num(w.resource_result, { disabled: ro });
        const pollHeld = el('input', { type: 'checkbox', checked: w.poll_held, disabled: ro });
        const notes = el('input', { type: 'text', className: 'form-input', value: w.notes || '', disabled: ro });
        const save = ro ? null : el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
            const body = { status: status.value, role: role.value, opponent_season_alliance_id: intOrNull(opponent.value),
                battle_date: date.value, battle_time: time.value, priority: priority.value, declared_by_member_id: intOrNull(declaredBy.value),
                planned_declaration_at: plannedDecl.value.replace('T', ' '), outcome: outcome.value,
                participant_count: intOrNull(participants.value), poll_held: pollHeld.checked, notes: notes.value.trim() };
            if (resources.value === '') body.clear_resource_result = true; else body.resource_result = Number(resources.value);
            send('PUT', '/api/season-hub/war/wars/' + w.id, body).then(d => { calendarNote(d); load(true); })
                .catch(err => showToast(err.message, 'error'));
        } }, 'Save');
        const del = ro || (w.status !== 'considering' && w.status !== 'cancelled') ? null : el('button', { type: 'button', className: 'btn btn-danger btn-sm', onclick: async () => {
            if (!await showConfirm('Delete this war?', 'Delete')) return;
            api('/api/season-hub/war/wars/' + w.id, { method: 'DELETE' }).then(() => load(true)).catch(err => showToast(err.message, 'error'));
        } }, 'Delete');
        const details = el('details', { className: 'war-obs-form' }, el('summary', null, ro ? 'Details' : 'Edit'),
            el('div', { className: 'land-target-grid' },
                field('Status', status), field('Role', role), field('Opponent', opponent), field('Battle date', date), field('Time (ST)', time),
                field('Priority', priority), field('Declared by', declaredBy), field('Declare at (ST)', plannedDecl),
                field('Result', outcome), field('Took part', participants), field('Resources won/lost', resources),
                field('Attendance poll held', pollHeld), field('Notes', notes, 'land-field-wide')),
            el('div', { className: 'tab-toolbar' }, save, del));
        return el('div', { className: 'land-drop card war-card' }, head, meta, details, buildStrategy(w, ro), buildReview(w, ro));
    }

    // Prose shown read-only gets the per-block Translate control; editors get the fields.
    function prose(label, text) {
        if (!text) return null;
        const p = el('p', { className: 'war-prose' });
        p.textContent = text;
        if (window.TranslateBlock) TranslateBlock.attach(p, text);
        return el('div', null, el('span', { className: 'war-figure-label', textContent: label }), p);
    }

    function buildStrategy(w, ro) {
        const box = el('details', { className: 'war-obs-form' }, el('summary', null, 'Strategy · ' + w.strategy_status +
            (w.strategy_updated_at ? ' · updated ' + utcToST(w.strategy_updated_at) + ' ST' : '')));
        const links = el('div', { className: 'land-snapshot-links' },
            w.strategy_file_id ? el('a', { href: '/api/files/download/' + w.strategy_file_id, textContent: 'Strategy document' }) : null,
            w.map_file_id ? el('a', { href: '/api/files/download/' + w.map_file_id, textContent: 'Map' }) : null,
            w.strategy_comms_id ? el('span', { textContent: 'Battle mail: ' + ((((data.links || {}).mails || []).find(m => m.id === w.strategy_comms_id) || {}).title || 'linked') }) : null);
        if (ro) {
            box.append(links, prose('Key objective', w.key_objective) || '', prose('Special instructions', w.special_instructions) || '');
            return box;
        }
        const file = select(linkOptions('files'), w.strategy_file_id);
        const map = select(linkOptions('files'), w.map_file_id);
        const mail = select(linkOptions('mails'), w.strategy_comms_id);
        const st = select([['draft', 'Draft'], ['ready', 'Ready'], ['final', 'Final'], ['archived', 'Archived']], w.strategy_status);
        const objective = el('textarea', { className: 'form-input', rows: '2' });
        objective.value = w.key_objective || '';
        const instructions = el('textarea', { className: 'form-input', rows: '3' });
        instructions.value = w.special_instructions || '';
        box.append(links, el('div', { className: 'land-target-grid' },
            field('Strategy document', file), field('Map', map), field('Battle mail', mail), field('Status', st),
            field('Key objective', objective, 'land-field-wide'), field('Special instructions', instructions, 'land-field-wide')),
            el('p', { className: 'hub-section-note', textContent: 'Files and mails tagged to this season.' }),
            el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                send('PUT', '/api/season-hub/war/wars/' + w.id, { strategy_file_id: intOrNull(file.value), map_file_id: intOrNull(map.value),
                    strategy_comms_id: intOrNull(mail.value), strategy_status: st.value, key_objective: objective.value,
                    special_instructions: instructions.value })
                    .then(() => { showToast('Strategy saved.'); load(true); }).catch(err => showToast(err.message, 'error'));
            } }, 'Save strategy'));
        return box;
    }

    function buildReview(w, ro) {
        if (w.status !== 'completed' && !(w.review_worked || w.review_improve || w.review_adjust)) return null;
        const box = el('details', { className: 'war-obs-form' }, el('summary', null, 'Review'));
        if (ro) {
            box.append(prose('What worked', w.review_worked) || '', prose('What needs improvement', w.review_improve) || '',
                prose('Adjustment for next battle', w.review_adjust) || '');
            return box;
        }
        const ta = v => { const t = el('textarea', { className: 'form-input', rows: '2' }); t.value = v || ''; return t; };
        const worked = ta(w.review_worked), improve = ta(w.review_improve), adjust = ta(w.review_adjust);
        box.append(el('div', { className: 'land-target-grid' }, field('What worked', worked, 'land-field-wide'),
            field('What needs improvement', improve, 'land-field-wide'), field('Adjustment for next battle', adjust, 'land-field-wide')),
            el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                send('PUT', '/api/season-hub/war/wars/' + w.id + '/review', { review_worked: worked.value, review_improve: improve.value, review_adjust: adjust.value })
                    .then(() => { showToast('Review saved.'); load(true); }).catch(err => showToast(err.message, 'error'));
            } }, 'Save review'));
        return box;
    }

    // ── Trade Posts (private-docs 280) ───────────────────────────────────────
    const POST_ACTIONS = [['', '—'], ['take', 'Take'], ['contest', 'Contest'], ['defend', 'Defend'], ['hold', 'Hold'], ['ignore', 'Ignore']];
    const PRIORITIES = [['', '—'], ['high', 'High'], ['medium', 'Medium'], ['low', 'Low']];

    function renderPosts() {
        const box = document.getElementById('war-posts');
        const ro = !editable();
        const parts = [];
        if (!ro) {
            const lookup = data.post_lookup || [];
            const used = new Set((data.trade_posts || []).map(p => p.post_key));
            const pick = lookup.length ? select([['', '— pick a post —']].concat(lookup.filter(a => !used.has(a.id)).map(a =>
                [a.id, a.name + ' ' + a.id + (a.position ? ' · ' + [a.position.column_band, a.position.row_band].filter(Boolean).join(' ') : '')])), '') : null;
            const name = el('input', { type: 'text', className: 'form-input', placeholder: lookup.length ? 'or a name' : 'Post name' });
            const add = el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                const body = { season_id: season.id };
                if (pick && pick.value) body.post_key = pick.value; else body.name = name.value.trim();
                send('POST', '/api/season-hub/war/trade-posts', body).then(() => load(true)).catch(err => showToast(err.message, 'error'));
            } }, '+ Post');
            parts.push(el('div', { className: 'tab-toolbar war-toolbar' }, pick ? field('Post', pick) : null, field(pick ? 'Name' : 'Post', name), add,
                el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => window.SeasonHub.openImport({
                    kind: 'trade_post_observations', title: 'Trade Post owners',
                    columns: 'Columns: Post (its key or name) and Owner tag, required; Owner server (tag and server name a season alliance — another is kept as a label), Observed at.',
                    onDone: () => load(true) }) }, 'Import owners')));
        }
        const cards = (data.trade_posts || []).map(p => buildPostCard(p, ro));
        parts.push(el('div', { className: 'war-post-list' }, cards.length ? cards : [el('p', { className: 'loading-msg', textContent: 'No Trade Posts yet.' })]));
        box.replaceChildren(...parts);
    }

    function buildPostCard(p, ro) {
        const factionLabel = id => ((data.factions || []).find(f => f.id === id) || {}).label;
        const owner = p.owner;
        const head = el('div', { className: 'land-target-head' }, el('strong', { textContent: p.name }),
            p.strategic_priority ? el('span', { className: 'land-status tone-' + ({ high: 'danger', medium: 'warning', low: 'neutral' }[p.strategic_priority]), textContent: p.strategic_priority }) : null,
            p.planned_action ? el('span', { className: 'land-status tone-info', textContent: p.planned_action + (p.planned_at ? ' · ' + p.planned_at + ' ST' : '') }) : null);
        const ownerLine = el('div', { className: 'land-drop-when' }, owner
            ? el('span', null, 'Held by ', el('span', { translate: 'no', textContent: owner.label }), owner.faction_id && factionLabel(owner.faction_id) ? ' · ' + factionLabel(owner.faction_id) : '',
                ' · as of ' + owner.observed_at + ' ST · ' + owner.source)
            : 'Owner not recorded');
        const card = el('div', { className: 'land-drop card war-post' }, head, ownerLine);
        if (p.notes) card.appendChild(prose('Notes', p.notes));
        if (ro) return card;
        const priority = select(PRIORITIES, p.strategic_priority || '');
        const action = select(POST_ACTIONS, p.planned_action || '');
        const at = el('input', { type: 'datetime-local', className: 'form-input', value: (p.planned_at || '').replace(' ', 'T') });
        const assigned = select(opponentOptions(), p.assigned_season_alliance_id, { translate: 'no' });
        const war = select([['', '— none —']].concat(data.wars.map(w => [String(w.id), (w.role === 'attack' ? 'Attack' : 'Defense') +
            (w.opponent_tag ? ' vs [' + w.opponent_tag + ']' : '') + (w.battle_date ? ' ' + w.battle_date : '')])), p.related_war_id, { translate: 'no' });
        const mail = select(linkOptions('mails'), p.strategy_comms_id);
        const map = select(linkOptions('files'), p.map_file_id);
        const notes = el('input', { type: 'text', className: 'form-input', value: p.notes || '' });
        const save = el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
            send('PUT', '/api/season-hub/war/trade-posts/' + p.id, { strategic_priority: priority.value, planned_action: action.value,
                planned_at: at.value.replace('T', ' '), assigned_season_alliance_id: intOrNull(assigned.value), related_war_id: intOrNull(war.value),
                strategy_comms_id: intOrNull(mail.value), map_file_id: intOrNull(map.value), notes: notes.value.trim() })
                .then(d => { calendarNote(d); load(true); }).catch(err => showToast(err.message, 'error'));
        } }, 'Save');
        const del = el('button', { type: 'button', className: 'btn btn-danger btn-sm', onclick: async () => {
            if (!await showConfirm('Remove ' + p.name + ' from the board?', 'Remove')) return;
            api('/api/season-hub/war/trade-posts/' + p.id, { method: 'DELETE' }).then(() => load(true)).catch(err => showToast(err.message, 'error'));
        } }, 'Remove');
        card.appendChild(el('details', { className: 'war-obs-form' }, el('summary', null, 'Plan'),
            el('div', { className: 'land-target-grid' }, field('Priority', priority), field('Action', action), field('When (ST)', at),
                field('Assigned to', assigned), field('Related war', war), field('Strategy mail', mail), field('Map', map), field('Notes', notes, 'land-field-wide')),
            el('p', { className: 'hub-section-note', textContent: 'Take, contest or defend with a time goes on the Schedule as a Trade Post Action; hold and ignore never do.' }),
            el('div', { className: 'tab-toolbar' }, save, del)));
        const who = select([['', '— pick —']].concat(data.opponents.map(o => [String(o.id), '[' + o.tag + '] S' + o.server])).concat([['label', 'Someone else (type it)']]), '', { translate: 'no' });
        const label = el('input', { type: 'text', className: 'form-input', placeholder: 'TAG (S123), or none', translate: 'no' });
        const faction = select([['', '— faction —']].concat((data.factions || []).map(f => [String(f.id), f.label])), '');
        const when = el('input', { type: 'datetime-local', className: 'form-input', value: nowST() });
        card.appendChild(el('details', { className: 'war-obs-form' }, el('summary', null, 'Record the owner'),
            el('div', { className: 'land-target-grid' }, field('Owner', who), field('Or', label), field('Faction', faction), field('Observed (ST)', when)),
            el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                const body = { observed_at: when.value.replace('T', ' '), owner_faction_id: intOrNull(faction.value) };
                if (who.value && who.value !== 'label') body.owner_season_alliance_id = parseInt(who.value, 10); else body.owner_label = label.value.trim();
                send('POST', '/api/season-hub/war/trade-posts/' + p.id + '/observations', body)
                    .then(d => { showToast(d.written ? 'Owner recorded.' : 'Already recorded.'); load(true); }).catch(err => showToast(err.message, 'error'));
            } }, 'Record'),
            owner ? el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: async () => {
                if (!await showConfirm('Delete the latest owner observation?', 'Delete')) return;
                api('/api/season-hub/war/trade-posts/observations/' + owner.observation_id, { method: 'DELETE' }).then(() => load(true))
                    .catch(err => showToast(err.message, 'error'));
            } }, 'Delete latest') : null));
        return card;
    }

    // ── Faction battles ──────────────────────────────────────────────────────
    function renderBattles() {
        const box = document.getElementById('war-battles');
        const ro = !editable();
        const factionName = id => id == null ? 'No faction' : ((data.factions || []).find(f => f.id === id) || {}).label || 'Faction';
        const wl = el('div', { className: 'war-figures' }, (data.factions_wl || []).map(f => el('div', { className: 'war-figure' },
            el('span', { className: 'war-figure-label', textContent: factionName(f.faction_id) }),
            el('strong', { textContent: wlText(f.season) + ' · ' + pctText(f.season) }),
            el('span', { className: 'war-figure-asof', textContent: f.weeks.map(x => 'W' + x.week + ' ' + x.record.wins + '–' + x.record.losses).join(' · ') }))));
        const alliances = [['', '—']].concat(data.opponents.map(o => [String(o.id), '[' + o.tag + '] S' + o.server]));
        const tbody = el('div', { className: 'war-battle-list' });
        const addRow = b => {
            b = b || { role: 'attack', outcome: 'unknown' };
            const date = el('input', { type: 'date', className: 'form-input', value: b.battle_date || '', disabled: ro });
            const ally = select(alliances, b.alliance_season_alliance_id, { disabled: ro, translate: 'no' });
            const opp = select(alliances.concat([['label', 'Other (type it)']]), b.opponent_season_alliance_id || (b.opponent_label ? 'label' : ''), { disabled: ro, translate: 'no' });
            const label = el('input', { type: 'text', className: 'form-input', value: b.opponent_label || '', disabled: ro, translate: 'no', placeholder: 'TAG (S123)' });
            const role = select(ROLES, b.role, { disabled: ro });
            const outcome = select(OUTCOMES.slice(1), b.outcome, { disabled: ro });
            const showLabel = () => { label.style.display = opp.value === 'label' ? '' : 'none'; };
            opp.addEventListener('change', showLabel);
            showLabel();
            const tr = el('div', { className: 'war-battle-row' }, field('Date', date), field('Alliance', ally), field('Opponent', el('span', { className: 'war-battle-opp' }, opp, label)),
                field('Role', role), field('Result', outcome),
                el('div', { className: 'war-battle-del' }, ro ? null : el('button', { type: 'button', className: 'btn btn-danger btn-sm', 'aria-label': 'Remove battle', onclick: async () => {
                    if (!b.id) { tr.remove(); return; }
                    if (!await showConfirm('Delete this battle?', 'Delete')) return;
                    api('/api/season-hub/war/faction-battles/' + b.id, { method: 'DELETE' }).then(() => load(true)).catch(err => showToast(err.message, 'error'));
                } }, svgIcon('x'))));
            tr._read = () => ({ id: b.id || 0, battle_date: date.value, alliance_season_alliance_id: intOrNull(ally.value),
                opponent_season_alliance_id: opp.value && opp.value !== 'label' ? parseInt(opp.value, 10) : null,
                opponent_label: opp.value === 'label' ? label.value.trim() : '', role: role.value, outcome: outcome.value, notes: b.notes || '' });
            tbody.appendChild(tr);
        };
        (data.battles || []).forEach(addRow);
        const table = tbody;
        const bar = ro ? null : el('div', { className: 'tab-toolbar war-toolbar' },
            el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => addRow() }, '+ Battle'),
            el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                const battles = Array.from(tbody.children).map(tr => tr._read()).filter(b => b.battle_date || b.alliance_season_alliance_id);
                send('PUT', '/api/season-hub/war/faction-battles', { season_id: season.id, battles })
                    .then(d => { showToast(d.written + ' battle' + (d.written === 1 ? '' : 's') + ' saved.'); load(true); })
                    .catch(err => showToast(err.message, 'error'));
            } }, 'Save battles'),
            el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => window.SeasonHub.openImport({
                kind: 'faction_battles', title: 'faction battles',
                columns: 'Columns: Date (YYYY-MM-DD), Alliance tag, Alliance server, Role (attack or defense), Outcome (win, loss, draw, unknown), required; Opponent tag, Opponent server, Notes. Record each battle once, from the side whose record it is.',
                onDone: () => load(true) }) }, 'Import CSV'));
        box.replaceChildren(el('div', { className: 'card land-section' },
            el('p', { className: 'hub-section-note', textContent: 'Each faction\'s record comes from its alliances\' battles; an alliance with no faction is counted in none.' }),
            wl, table, bar));
    }

    // ── Our side ─────────────────────────────────────────────────────────────
    function renderOwn() {
        const box = document.getElementById('war-own');
        const o = data.own;
        const parts = [el('h3', { className: 'land-section-title', textContent: 'Our alliance' }),
            el('div', { className: 'war-figures' }, figureLine('Power', o.power), figureLine('Members', o.member_count),
                figureLine('Top 10 THP', o.top10_thp), figureLine('Top 5 THP', o.top5_thp), figureLine('Top 3 THP', o.top3_thp))];
        if (editable()) parts.push(observationForm({ own: true }, 'Record our figures'));
        box.replaceChildren(...parts);
    }

    // An inline observation form (details): for our side or one opponent.
    // lastrankID: an opponent's LastRank link, which adds the From LastRank button (one request,
    // recorded at the instant LastRank last saw the alliance).
    function observationForm(target, label, lastrankID) {
        const at = el('input', { type: 'datetime-local', className: 'form-input', value: nowST() });
        const inputs = { alliance_power: num(), member_count: num(null, { max: '200' }), group_rank: num(null, { min: '1' }),
            top10_thp: num(), top5_thp: num(), top3_thp: num() };
        const note = el('input', { type: 'text', className: 'form-input' });
        const save = el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
            const body = Object.assign({ season_id: season.id, observed_at: at.value.replace('T', ' '), note: note.value.trim() }, target);
            Object.entries(inputs).forEach(([k, i]) => { body[k] = readNum(i); });
            send('POST', '/api/season-hub/war/observations', body)
                .then(d => { showToast(d.written ? 'Observation recorded.' : 'Already recorded.'); load(true); })
                .catch(err => showToast(err.message, 'error'));
        } }, 'Record');
        const fromLR = !target.own && lastrankID ? el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => {
            fromLR.disabled = true;
            send('POST', '/api/season-hub/war/observations/lastrank', { season_id: season.id, season_alliance_id: target.season_alliance_id })
                .then(d => {
                    showToast(d.written ? 'Recorded from LastRank: ' + compact(d.alliance_power) + ' power, ' + d.member_count + ' members, as of ' + d.observed_at + ' ST.'
                        : 'LastRank has nothing newer: its reading as of ' + d.observed_at + ' ST is already recorded.', d.written ? 'success' : 'info');
                    load(true);
                })
                .catch(err => showToast(err.message, 'error'))
                .finally(() => { fromLR.disabled = false; });
        } }, 'From LastRank') : null;
        return el('details', { className: 'war-obs-form' }, el('summary', null, label),
            el('div', { className: 'land-target-grid' },
                field('Observed (ST)', at), field('Alliance power', inputs.alliance_power), field('Members', inputs.member_count),
                target.own ? null : field('Group rank', inputs.group_rank),
                field('Top 10 THP', inputs.top10_thp), field('Top 5 THP', inputs.top5_thp), field('Top 3 THP', inputs.top3_thp),
                field('Note', note, 'land-field-wide')),
            el('p', { className: 'hub-section-note', textContent: 'Leave a figure blank when it was not seen — a blank is unknown, never zero.' }),
            el('div', { className: 'tab-toolbar' }, save, fromLR),
            !target.own && !lastrankID ? el('p', { className: 'hub-section-note', textContent: 'No LastRank link yet — load their players under Compare players to link it, then record from LastRank here.' }) : null);
    }

    // ── Opponents ────────────────────────────────────────────────────────────
    function sortedOpponents() {
        const list = data.opponents.slice();
        const key = {
            rating: o => o.matchup.score == null ? -Infinity : o.matchup.score,
            power: o => o.figures.power.value == null ? -Infinity : o.figures.power.value,
            rank: o => o.figures.group_rank.value == null ? Infinity : -o.figures.group_rank.value,
            tag: null,
        }[sortBy];
        if (key) list.sort((a, b) => key(b) - key(a));
        else list.sort((a, b) => a.tag.localeCompare(b.tag) || a.server - b.server);
        return list;
    }

    function renderOpponents() {
        const box = document.getElementById('war-opponents');
        const bar = el('div', { className: 'tab-toolbar war-toolbar' },
            field('Sort by', select(SORTS, sortBy, { onchange: e => { sortBy = e.target.value; renderOpponents(); } })),
            editable() ? el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: openAddOpponent }, '+ Opponent') : null,
            editable() ? el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => window.SeasonHub.openImport({
                kind: 'opponent_observations', title: 'opponent observations',
                columns: 'Columns: Tag and Server, required (our own tag records our side); Power, Members, Rank, Top10 THP, Top5 THP, Top3 THP, Observed at (YYYY-MM-DD HH:MM, server time; blank is now), Note. Each alliance must already be on this tab.',
                onDone: () => load(true) }) }, 'Import CSV') : null);
        const cards = sortedOpponents().map(buildOpponentCard);
        box.replaceChildren(bar, el('div', { id: 'war-add-slot' }),
            ...(cards.length ? cards : [el('p', { className: 'loading-msg', textContent: 'No opponents yet.' })]));
    }

    function buildOpponentCard(o) {
        const ro = !editable();
        const m = o.matchup;
        const head = el('div', { className: 'land-target-head' },
            el('strong', { translate: 'no', textContent: '[' + o.tag + '] ' + (o.name || '') }),
            el('span', { className: 'war-server', textContent: 'S' + o.server }),
            ratingBadge(m.rating, m.score),
            o.assessment_override ? el('span', { className: 'war-override' }, 'Leadership: ', ratingBadge(o.assessment_override)) : null);
        const meta = el('div', { className: 'land-drop-when' },
            [o.faction_label ? 'Faction ' + o.faction_label : null, (RELATIONSHIPS.find(r => r[0] === o.relationship) || [0, o.relationship])[1],
                o.figures.group_rank.value != null ? 'Group rank #' + o.figures.group_rank.value : null,
                o.ally_name ? 'Ally' : null].filter(Boolean).join(' · '));
        const figures = el('div', { className: 'war-figures' }, figureLine('Power', o.figures.power), figureLine('Members', o.figures.member_count),
            figureLine('Top 10 THP', o.figures.top10_thp), figureLine('Top 5 THP', o.figures.top5_thp), figureLine('Top 3 THP', o.figures.top3_thp));
        const breakdown = el('div', { className: 'war-breakdown' }, m.categories.map(c => el('div', { className: 'war-cat' + (c.pct == null ? ' war-cat-dropped' : '') },
            el('span', { textContent: c.label }),
            el('strong', { textContent: c.pct == null ? 'unknown' : (c.pct > 0 ? '+' : '') + c.pct.toFixed(1) + '%' }),
            el('span', { className: 'war-figure-asof', textContent: c.pct == null ? 'not counted' : 'weight ' + c.effective.toFixed(1) + '% → ' + (c.contribution > 0 ? '+' : '') + c.contribution.toFixed(2) }))));
        const note = m.rating === 'insufficient_data'
            ? el('p', { className: 'hub-section-note', textContent: 'Rated once at least ' + data.config.min_categories + ' categories are known on both sides.' }) : null;

        // Leadership's assessment and the season row's fields (the same row the Ally modal edits).
        const override = select([['', '— none —']].concat(Object.entries(RATINGS).filter(([k]) => k !== 'insufficient_data').map(([k, v]) => [k, v[0]])),
            o.assessment_override || '', { disabled: ro });
        const overrideNote = el('input', { type: 'text', className: 'form-input', value: o.assessment_note || '', disabled: ro });
        const rel = select(RELATIONSHIPS, o.relationship, { disabled: ro });
        const faction = select([['', '— none —']].concat((data.factions || []).map(f => [String(f.id), f.label])), o.faction_id, { disabled: ro });
        const terms = el('input', { type: 'text', className: 'form-input', value: o.terms || '', disabled: ro });
        const notes = el('input', { type: 'text', className: 'form-input', value: o.notes || '', disabled: ro });
        const rowEdit = el('details', { className: 'war-obs-form' }, el('summary', null, 'Assessment, faction and notes'),
            el('div', { className: 'land-target-grid' }, field('Leadership assessment', override), field('Why', overrideNote),
                field('Relationship', rel), field('Faction', faction), field('Terms', terms, 'land-field-wide'), field('Notes', notes, 'land-field-wide')),
            ro ? null : el('div', { className: 'tab-toolbar' },
                el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                    send('PUT', '/api/season-hub/alliances', { season_id: season.id, external_alliance_id: o.external_alliance_id,
                        assessment_override: override.value, assessment_note: overrideNote.value.trim(), relationship: rel.value,
                        faction_id: faction.value ? parseInt(faction.value, 10) : null, terms: terms.value.trim(), notes: notes.value.trim() })
                        .then(() => { showToast('Saved.'); load(true); })
                        .catch(err => showToast(err.message, 'error'));
                } }, 'Save'),
                el('button', { type: 'button', className: 'btn btn-danger btn-sm', onclick: async () => {
                    if (!await showConfirm('Remove [' + o.tag + '] from this season? Its registry entry stays.', 'Remove')) return;
                    api('/api/season-hub/alliances/' + o.id, { method: 'DELETE' }).then(() => load(true))
                        .catch(err => showToast(err.message, 'error'));
                } }, 'Remove from season')));
        return el('div', { className: 'land-drop card war-opponent' }, head, meta, figures, breakdown, note,
            rowEdit, ro ? null : observationForm({ season_alliance_id: o.id }, 'Record an observation', o.lastrank_id), buildPairing(o));
    }

    // ── Player pairing: the Scout Report's members, held in the browser only ─
    function buildPairing(o) {
        // Open while players are loaded, so the redraw after the link is saved keeps them in view.
        const box = el('details', { className: 'war-obs-form war-pairing', open: !!players[o.id] }, el('summary', null, 'Compare players'));
        const body = el('div');
        box.appendChild(body);
        const draw = () => {
            const theirs = players[o.id];
            const parts = [];
            if (CAN_MANAGE) {
                const link = el('input', { type: 'text', className: 'form-input', value: o.lastrank_id || '', placeholder: 'lastrank.fun link or id' });
                const go = el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => {
                    go.disabled = true;
                    send('POST', '/api/external-alliances/report', { lastrank_id: link.value.trim() })
                        .then(r => {
                            players[o.id] = r; draw();
                            if (!editable()) return;
                            // Keep the link on the alliance and record LastRank's power and member count.
                            return send('PUT', '/api/season-hub/alliances', { season_id: season.id,
                                external_alliance_id: o.external_alliance_id, lastrank_id: r.lastrank_id })
                                .then(res => {
                                    if (res && res.lastrank_note) showToast(res.lastrank_note, 'warning', 6000);
                                    load(true);
                                });
                        })
                        .catch(err => showToast(err.message, 'error'))
                        .finally(() => { go.disabled = false; });
                } }, theirs ? 'Reload players' : 'Load players');
                parts.push(el('div', { className: 'tab-toolbar' }, field('LastRank alliance', link), go),
                    el('p', { className: 'hub-section-note', textContent: 'Their players are shown here only and never saved. The link stays on the alliance, and its power and member count are recorded.' }));
            }
            if (theirs) {
                const known = (theirs.members || []).filter(p => p.hero_power != null).sort((a, b) => b.hero_power - a.hero_power).slice(0, 10);
                parts.push(el('p', { className: 'hub-section-note', textContent: 'LastRank holds ' + (theirs.members || []).length + ' of their players (the game shows ' +
                    theirs.member_count + ' members); hero power is known for ' + known.length + '. A missing player is unknown, not weaker.' }));
                const ours = data.own_top || [];
                const rows = [];
                for (let i = 0; i < Math.max(ours.length, known.length); i++) {
                    const a = ours[i], b = known[i];
                    const delta = a && b ? (a.hero_power - b.hero_power) / b.hero_power * 100 : null;
                    rows.push(el('div', { className: 'war-pair' },
                        el('span', { className: 'war-pair-n', textContent: '#' + (i + 1) }),
                        el('span', { className: 'war-pair-side', translate: 'no' }, a ? a.name : '—', el('small', { textContent: a ? compact(a.hero_power) : '' })),
                        el('span', { className: 'war-pair-side', translate: 'no' }, b ? b.name : 'unknown', el('small', { textContent: b ? compact(b.hero_power) : '' })),
                        el('span', { className: 'war-pair-delta' + (delta == null ? '' : delta >= 0 ? ' up' : ' down'),
                            textContent: delta == null ? '' : (delta > 0 ? '+' : '') + delta.toFixed(1) + '%' })));
                }
                parts.push(el('div', { className: 'war-pairs' },
                    el('div', { className: 'war-pair war-pair-head' }, el('span'), el('span', { textContent: 'Ours (THP)' }), el('span', { textContent: 'Theirs (THP)' }), el('span')),
                    ...rows));
            } else if (!CAN_MANAGE) {
                parts.push(el('p', { className: 'hub-section-note', textContent: 'Loading their players needs the War tab\'s manage permission.' }));
            }
            body.replaceChildren(...parts);
        };
        draw();
        return box;
    }

    // ── Add an opponent: the registry finder, local matches first ───────────
    // forWar: the war card that asked (the new opponent arrives picked on it); the toolbar's
    // button passes its click event instead, and toggles the form.
    async function openAddOpponent(forWar) {
        const warID = typeof forWar === 'number' ? forWar : 0;
        const slot = document.getElementById('war-add-slot');
        if (slot.firstChild) {
            if (warID) { slot.scrollIntoView({ behavior: 'smooth', block: 'center' }); return; }
            slot.replaceChildren(); return;
        }
        if (!registry) {
            try { registry = await api('/api/external-alliances'); } catch (e) { registry = []; }
        }
        const server = el('input', { type: 'number', className: 'form-input', min: '1', placeholder: 'e.g. 1701' });
        const tag = el('input', { type: 'text', className: 'form-input', translate: 'no', autocomplete: 'off' });
        const name = el('input', { type: 'text', className: 'form-input', translate: 'no', autocomplete: 'off' });
        const rel = select(RELATIONSHIPS, 'enemy');
        const faction = select([['', '— none —']].concat((data.factions || []).map(f => [String(f.id), f.label])), '');
        // pickedLR: the pick's LastRank id — the server links it and records LastRank's figures.
        let pickedID = 0, pickedLR = '';
        const lrNote = el('p', { className: 'hub-section-note' });
        const clearPick = () => { pickedID = 0; pickedLR = ''; lrNote.textContent = ''; };
        [server, tag, name].forEach(i => i.addEventListener('input', clearPick));
        const finder = buildRemoteFinder({
            inputs: [tag, name],
            localSearch: q => QuickSearch.filter(registry || [], q, x => (x.tag || '') + ' ' + (x.name || '')).slice(0, 6),
            context: () => server.value.trim(),
            guard: () => {
                if (server.value.trim()) return null;
                server.focus();
                return 'Enter the server number — LastRank search matches it strictly.';
            },
            remoteSearch: q => api('/api/external-alliances/search?q=' + encodeURIComponent(q) + '&server=' + encodeURIComponent(server.value.trim())).then(l => l || []),
            localHead: 'In your registry',
            remoteHead: 'From LastRank',
            searchingText: 'Searching LastRank…',
            emptyText: () => 'No LastRank matches on server ' + server.value.trim() + '.',
            item: (x, kind) => ({
                label: (x.tag ? '[' + x.tag + '] ' : '') + (x.name || ''),
                meta: [x.server != null ? 'S' + x.server : null, x.power != null ? formatCompact(x.power) + ' pw' : null].filter(Boolean).join(' · '),
                onPick: () => {
                    tag.value = x.tag || ''; name.value = x.name || ''; if (x.server != null) server.value = x.server;
                    pickedID = kind === 'local' ? x.id : 0;
                    pickedLR = x.lastrank_id || '';
                    lrNote.textContent = pickedLR ? 'Linked to LastRank: its power and member count are recorded when you add.' : '';
                    finder.close();
                },
            }),
        });
        const save = el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
            const body = { season_id: season.id, relationship: rel.value, faction_id: faction.value ? parseInt(faction.value, 10) : null };
            if (pickedID) body.external_alliance_id = pickedID;
            else Object.assign(body, { tag: tag.value.trim(), name: name.value.trim(), server: parseInt(server.value, 10) || 0 });
            if (pickedLR) body.lastrank_id = pickedLR;
            save.disabled = true;
            send('PUT', '/api/season-hub/alliances', body)
                .then(res => {
                    slot.replaceChildren();
                    if (warID && res && res.id) pendingOpponent = { warID, allianceID: res.id };
                    showToast((warID ? 'Opponent added — save the war to keep it.' : 'Opponent added.') +
                        (res && res.lastrank_imported ? ' LastRank\'s figures are recorded.' : ''));
                    if (res && res.lastrank_note) showToast(res.lastrank_note, 'warning', 6000);
                    load(true);
                })
                .finally(() => { save.disabled = false; })
                .catch(err => showToast(err.message, 'error'));
        } }, 'Add');
        const grid = el('div', { className: 'land-target-grid war-add-grid' }, field('Server', server), field('Tag', tag), field('Name', name),
            field('Relationship', rel), field('Faction', faction), finder.dropdown);
        slot.replaceChildren(el('div', { className: 'card land-drop' }, el('strong', { textContent: 'Add an opponent' }),
            el('p', { className: 'hub-section-note', textContent: 'Type a tag or name to search your registry; enter the server to look it up on LastRank. Tag and server together name an alliance.' }),
            grid, lrNote, el('div', { className: 'tab-toolbar' }, save,
                el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => slot.replaceChildren() }, 'Cancel'))));
        if (warID) slot.scrollIntoView({ behavior: 'smooth', block: 'center' });
        tag.focus();
    }

    // ── Observation log ──────────────────────────────────────────────────────
    function renderObservations() {
        const box = document.getElementById('war-observations');
        const names = {};
        data.opponents.forEach(o => { names[o.id] = '[' + o.tag + '] S' + o.server; });
        const ro = !editable();
        const rows = data.observations.map(ob => el('tr', null,
            el('td', { textContent: ob.observed_at }),
            el('td', { textContent: ob.is_own ? 'Ours' : (names[ob.season_alliance_id] || '—') }),
            el('td', { 'data-export-value': ob.alliance_power ?? '', 'data-sort-value': ob.alliance_power ?? '', textContent: compact(ob.alliance_power) }),
            el('td', { textContent: ob.member_count ?? '—' }),
            el('td', { textContent: ob.group_rank ?? '—' }),
            el('td', { 'data-export-value': ob.top10_thp ?? '', 'data-sort-value': ob.top10_thp ?? '', textContent: compact(ob.top10_thp) }),
            el('td', { 'data-export-value': ob.top5_thp ?? '', 'data-sort-value': ob.top5_thp ?? '', textContent: compact(ob.top5_thp) }),
            el('td', { 'data-export-value': ob.top3_thp ?? '', 'data-sort-value': ob.top3_thp ?? '', textContent: compact(ob.top3_thp) }),
            el('td', { textContent: ob.source }),
            el('td', null, ro ? null : el('button', { type: 'button', className: 'btn btn-danger btn-sm', 'aria-label': 'Delete observation', onclick: async () => {
                if (!await showConfirm('Delete this observation?', 'Delete')) return;
                api('/api/season-hub/war/observations/' + ob.id, { method: 'DELETE' }).then(() => load(true))
                    .catch(err => showToast(err.message, 'error'));
            } }, svgIcon('x')))));
        box.replaceChildren(el('div', { className: 'table-scroll land-events-scroll' },
            sortable(el('table', { className: 'data-table war-obs-table', translate: 'no' },
                sortHead([['Observed (ST)', 'at', 'date'], ['Alliance', 'alliance'], ['Power', 'power', 'number'], ['Members', 'members', 'number'],
                    ['Rank', 'rank', 'number'], ['Top 10', 'top10', 'number'], ['Top 5', 'top5', 'number'], ['Top 3', 'top3', 'number'], ['Source', 'source'], ['']]),
                el('tbody', null, rows.length ? rows : el('tr', null, el('td', { colspan: '10', className: 'loading-msg', translate: 'yes', textContent: 'No observations yet.' })))), 'observations')));
    }

    // ── Settings ─────────────────────────────────────────────────────────────
    function renderSettings() {
        const body = document.getElementById('war-settings-body');
        const c = data.config;
        const ro = !(CAN_CONFIGURE && season && !season.archived_at);
        const n = (k, props) => num(c[k], Object.assign({ disabled: ro, 'data-k': k }, props || {}));
        const typeSel = select([['', '— not scheduled —']].concat(((data.links || {}).event_types || []).map(t => [String(t.id), t.title])),
            c.war_event_type_id, { disabled: ro, 'data-k': 'war_event_type_id' });
        const form = el('div', { className: 'land-settings-grid' },
            field('Wars go on the calendar as', typeSel),
            field('Weight: alliance power', n('weight_power', { max: '100' })), field('Weight: top 10 THP', n('weight_top10', { max: '100' })),
            field('Weight: top 5 THP', n('weight_top5', { max: '100' })), field('Weight: top 3 THP', n('weight_top3', { max: '100' })),
            field('Very Favorable from (%)', n('band_very_favorable', { min: '-100', step: '0.01' })),
            field('Favorable from (%)', n('band_favorable', { min: '-100', step: '0.01' })),
            field('Unfavorable at or below (%)', n('band_unfavorable', { min: '-100', step: '0.01' })),
            field('Very Unfavorable at or below (%)', n('band_very_unfavorable', { min: '-100', step: '0.01' })),
            field('Categories needed to rate', n('min_categories', { min: '1', max: '4' })),
            field('Loot step (%)', n('loot_step_pct', { max: '100' })), field('Loot cap (%)', n('loot_max_pct', { max: '100' })),
            field('Production stops: week', n('production_stop_week', { min: '1' })),
            field('Production stops: day (1–7)', n('production_stop_day', { min: '1', max: '7' })));
        const note = c.saved ? null : el('p', { className: 'hub-section-note', textContent: 'Not set for this season yet: these are the defaults (Pudgey\'s starting weights and bands).' });
        const save = ro ? null : el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
            const out = { season_id: season.id };
            form.querySelectorAll('[data-k]').forEach(i => { out[i.dataset.k] = readNum(i); });
            out.war_event_type_id = typeSel.value ? parseInt(typeSel.value, 10) : null;
            ['weight_power', 'weight_top10', 'weight_top5', 'weight_top3', 'min_categories', 'loot_step_pct', 'loot_max_pct']
                .forEach(k => { if (out[k] == null) out[k] = 0; });
            send('PUT', '/api/season-hub/war/config', out).then(() => { showToast('War settings saved.'); load(true); })
                .catch(err => showToast(err.message, 'error'));
        } }, 'Save settings');
        const target = num(c.target_war_rank, { min: '1', disabled: ro, 'data-k': 'target_war_rank' });
        form.insertBefore(field('Target war rank', target), form.children[1]);

        // Rank groups (S4: King #1, Queen #2–3, Rook #4–10), in order.
        const list = el('div', { className: 'war-groups' });
        const addGroup = g => {
            g = g || {};
            const name = el('input', { type: 'text', className: 'form-input', value: g.label || '', disabled: ro, 'aria-label': 'Group name' });
            const from = num(g.rank_from, { min: '1', disabled: ro, 'aria-label': 'From rank' });
            const to = num(g.rank_to, { min: '1', disabled: ro, 'aria-label': 'To rank' });
            const row = el('div', { className: 'war-group-row' }, name, el('span', { textContent: '#' }), from,
                el('span', { textContent: 'to' }), to,
                ro ? null : el('button', { type: 'button', className: 'btn btn-danger btn-sm', 'aria-label': 'Remove group', onclick: () => { row.remove(); refreshOrderButtons(list); } }, svgIcon('x')));
            if (!ro) row.prepend(buildOrderButtons(row, null));
            row._read = () => ({ id: g.id || 0, label: name.value.trim(), rank_from: readNum(from) || 0, rank_to: readNum(to) || 0 });
            list.appendChild(row);
            if (!ro) refreshOrderButtons(list);
        };
        ((data.resources || {}).groups || []).forEach(addGroup);
        const groupBar = ro ? null : el('div', { className: 'tab-toolbar' },
            el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => addGroup() }, '+ Group'),
            el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                send('PUT', '/api/season-hub/war/rank-groups', { season_id: season.id, groups: Array.from(list.children).map(x => x._read()) })
                    .then(() => { showToast('Rank groups saved.'); load(true); }).catch(err => showToast(err.message, 'error'));
            } }, 'Save groups'));
        body.replaceChildren(...[note, form, save, el('h4', { textContent: 'Reward groups by war rank' }), list, groupBar].filter(Boolean));
    }
})();
