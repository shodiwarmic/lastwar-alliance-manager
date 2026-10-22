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
        renderOpponents();
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
    function observationForm(target, label) {
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
        return el('details', { className: 'war-obs-form' }, el('summary', null, label),
            el('div', { className: 'land-target-grid' },
                field('Observed (ST)', at), field('Alliance power', inputs.alliance_power), field('Members', inputs.member_count),
                target.own ? null : field('Group rank', inputs.group_rank),
                field('Top 10 THP', inputs.top10_thp), field('Top 5 THP', inputs.top5_thp), field('Top 3 THP', inputs.top3_thp),
                field('Note', note, 'land-field-wide')),
            el('p', { className: 'hub-section-note', textContent: 'Leave a figure blank when it was not seen — a blank is unknown, never zero.' }),
            save);
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
            rowEdit, ro ? null : observationForm({ season_alliance_id: o.id }, 'Record an observation'), buildPairing(o));
    }

    // ── Player pairing: the Scout Report's members, held in the browser only ─
    function buildPairing(o) {
        const box = el('details', { className: 'war-obs-form war-pairing' }, el('summary', null, 'Compare players'));
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
                        .then(r => { players[o.id] = r; draw(); })
                        .catch(err => showToast(err.message, 'error'))
                        .finally(() => { go.disabled = false; });
                } }, theirs ? 'Reload players' : 'Load players');
                parts.push(el('div', { className: 'tab-toolbar' }, field('LastRank alliance', link), go),
                    el('p', { className: 'hub-section-note', textContent: 'One LastRank request. Their players are shown here only and never saved.' }));
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
    async function openAddOpponent() {
        const slot = document.getElementById('war-add-slot');
        if (slot.firstChild) { slot.replaceChildren(); return; }
        if (!registry) {
            try { registry = await api('/api/external-alliances'); } catch (e) { registry = []; }
        }
        const server = el('input', { type: 'number', className: 'form-input', min: '1', placeholder: 'e.g. 1701' });
        const tag = el('input', { type: 'text', className: 'form-input', translate: 'no', autocomplete: 'off' });
        const name = el('input', { type: 'text', className: 'form-input', translate: 'no', autocomplete: 'off' });
        const rel = select(RELATIONSHIPS, 'enemy');
        const faction = select([['', '— none —']].concat((data.factions || []).map(f => [String(f.id), f.label])), '');
        let pickedID = 0;
        const clearPick = () => { pickedID = 0; };
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
                    finder.close();
                },
            }),
        });
        const save = el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
            const body = { season_id: season.id, relationship: rel.value, faction_id: faction.value ? parseInt(faction.value, 10) : null };
            if (pickedID) body.external_alliance_id = pickedID;
            else Object.assign(body, { tag: tag.value.trim(), name: name.value.trim(), server: parseInt(server.value, 10) || 0 });
            send('PUT', '/api/season-hub/alliances', body)
                .then(() => { slot.replaceChildren(); showToast('Opponent added.'); load(true); })
                .catch(err => showToast(err.message, 'error'));
        } }, 'Add');
        const grid = el('div', { className: 'land-target-grid war-add-grid' }, field('Server', server), field('Tag', tag), field('Name', name),
            field('Relationship', rel), field('Faction', faction), finder.dropdown);
        slot.replaceChildren(el('div', { className: 'card land-drop' }, el('strong', { textContent: 'Add an opponent' }),
            el('p', { className: 'hub-section-note', textContent: 'Type a tag or name to search your registry; enter the server to look it up on LastRank. Tag and server together name an alliance.' }),
            grid, el('div', { className: 'tab-toolbar' }, save,
                el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => slot.replaceChildren() }, 'Cancel'))));
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
            el('td', { 'data-export-value': ob.alliance_power ?? '', textContent: compact(ob.alliance_power) }),
            el('td', { textContent: ob.member_count ?? '—' }),
            el('td', { textContent: ob.group_rank ?? '—' }),
            el('td', { 'data-export-value': ob.top10_thp ?? '', textContent: compact(ob.top10_thp) }),
            el('td', { 'data-export-value': ob.top5_thp ?? '', textContent: compact(ob.top5_thp) }),
            el('td', { 'data-export-value': ob.top3_thp ?? '', textContent: compact(ob.top3_thp) }),
            el('td', { textContent: ob.source }),
            el('td', null, ro ? null : el('button', { type: 'button', className: 'btn btn-danger btn-sm', 'aria-label': 'Delete observation', onclick: async () => {
                if (!await showConfirm('Delete this observation?', 'Delete')) return;
                api('/api/season-hub/war/observations/' + ob.id, { method: 'DELETE' }).then(() => load(true))
                    .catch(err => showToast(err.message, 'error'));
            } }, svgIcon('x')))));
        box.replaceChildren(el('div', { className: 'table-scroll land-events-scroll' },
            el('table', { className: 'data-table war-obs-table', translate: 'no' },
                el('thead', null, el('tr', null, ['Observed (ST)', 'Alliance', 'Power', 'Members', 'Rank', 'Top 10', 'Top 5', 'Top 3', 'Source', '']
                    .map(t => el('th', { textContent: t, translate: 'yes' })))),
                el('tbody', null, rows.length ? rows : el('tr', null, el('td', { colspan: '10', className: 'loading-msg', translate: 'yes', textContent: 'No observations yet.' }))))));
    }

    // ── Settings ─────────────────────────────────────────────────────────────
    function renderSettings() {
        const body = document.getElementById('war-settings-body');
        const c = data.config;
        const ro = !(CAN_CONFIGURE && season && !season.archived_at);
        const n = (k, props) => num(c[k], Object.assign({ disabled: ro, 'data-k': k }, props || {}));
        const form = el('div', { className: 'land-settings-grid' },
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
            ['weight_power', 'weight_top10', 'weight_top5', 'weight_top3', 'min_categories', 'loot_step_pct', 'loot_max_pct']
                .forEach(k => { if (out[k] == null) out[k] = 0; });
            send('PUT', '/api/season-hub/war/config', out).then(() => { showToast('War settings saved.'); load(true); })
                .catch(err => showToast(err.message, 'error'));
        } }, 'Save settings');
        body.replaceChildren(...[note, form, save].filter(Boolean));
    }
})();
