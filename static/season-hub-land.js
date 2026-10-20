/* season-hub-land.js — the Season Hub's Land tab (private-docs 274–276): the active route's
   readiness, daily capacity, holdings, drops and events, the three route slots and their
   targets, and the season's Land settings.
   Instants are server time (ST) throughout; the server stores them in UTC. */

(function () {
    'use strict';
    if (!document.getElementById('tab-land') || !window.SeasonHub) return;

    const cfg = document.getElementById('page-config').dataset;
    const CAN_MANAGE = cfg.canManageLand === 'true';
    const CAN_CONFIGURE = cfg.canConfigureLand === 'true';

    const SLOTS = [['primary', 'Primary'], ['backup_a', 'Backup A'], ['backup_b', 'Backup B']];
    const KINDS = [['city', 'City'], ['stronghold', 'Stronghold'], ['capitol', 'Capitol'], ['trade_post', 'Trade Post'], ['landmark', 'Landmark']];
    const STATUSES = [['planned', 'Planned'], ['declared', 'Declared'], ['captured', 'Captured'], ['skipped', 'Skipped'], ['failed', 'Failed']];
    const CONNECTED = [['unknown', 'Unknown'], ['touching', 'Touching'], ['not_touching', 'Not Touching']];
    const READINESS = {
        none: ['No route', 'neutral'], completed: ['Completed', 'success'], unknown: ['Unknown', 'neutral'],
        on_track: ['On Track', 'success'], at_risk: ['At Risk', 'warning'], behind: ['Behind', 'danger'],
    };

    let season = null;
    let data = null;
    let areas = null;          // the season's map lookup, fetched once per season
    let slot = 'primary';
    let loaded = 0;            // season id the tab last loaded
    const gen = { n: 0 };

    // ── DOM helper (dashboard.js's el(), copied: page-local) ─────────────────
    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        if (props) {
            Object.entries(props).forEach(([k, v]) => {
                if (v == null || v === false) return;
                if (k === 'className') node.className = v;
                else if (k === 'textContent') node.textContent = v;
                else if (k === 'style') Object.assign(node.style, v);
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
    const stInput = v => el('input', { type: 'datetime-local', className: 'form-input', value: v ? v.replace(' ', 'T') : '' });
    const stValue = inp => inp.value ? inp.value.replace('T', ' ').slice(0, 16) : '';
    const editable = () => CAN_MANAGE && season && !season.archived_at;

    async function api(url, opts) {
        const res = await fetch(url, opts);
        if (!res.ok) throw new Error((await res.text()) || 'Request failed');
        return res.json();
    }
    const put = (url, body) => api(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });

    // ── Loading ───────────────────────────────────────────────────────────────
    function load(force) {
        if (!season) return;
        if (!force && loaded === season.id && data) return;
        const sid = season.id;
        const mine = ++gen.n;
        api('/api/season-hub/land?season_id=' + sid)
            .then(d => {
                if (mine !== gen.n || !season || season.id !== sid) return;
                data = d;
                loaded = sid;
                if (d.has_lookup && !areas) {
                    api('/api/season-hub/land/lookup?season_id=' + sid).then(l => { areas = l.areas || []; render(); }).catch(() => {});
                }
                render();
            })
            .catch(err => showToast(err.message || 'Failed to load Land.', 'error'));
    }
    window.SeasonHub.onSeason(s => {
        if (!season || season.id !== s.id) { areas = null; data = null; }
        season = s;
        if (window.SeasonHub.tab === 'land') load();
    });
    window.SeasonHub.onTab(name => { if (name === 'land') load(); });

    function render() {
        renderReadiness();
        renderExtras();
        renderRouteTabs();
        renderRoute();
        renderSettings();
        document.dispatchEvent(new CustomEvent('seasonland:render', { detail: { season, data } }));
    }

    // ── Path status ──────────────────────────────────────────────────────────
    function renderReadiness() {
        const box = document.getElementById('land-readiness');
        const r = data.readiness;
        const [label, tone] = READINESS[r.status] || [r.status, 'neutral'];
        const head = el('div', { className: 'land-readiness-head' },
            el('h3', { textContent: 'Land Path Status' }),
            el('span', { className: 'land-status tone-' + tone, textContent: label }));
        const lines = [];
        if (r.route_id) {
            lines.push(el('div', null, el('strong', { textContent: r.route_name }),
                ' · ' + r.captured + ' of ' + r.total + ' captured'));
        }
        if (r.next) {
            const nm = noTranslate(el('span', { textContent: r.next.name + (r.next.level ? ' L' + r.next.level : '') }));
            lines.push(el('div', null, 'Next: ', nm,
                r.next.unlock_at ? ' · unlocks ' + r.next.unlock_at + ' ST' : '',
                r.next.countdown ? ' (in ' + r.next.countdown + ')' : ''));
        }
        const reasons = el('ul', { className: 'land-reasons' }, (r.reasons || []).map(t => el('li', { textContent: t })));
        box.replaceChildren(head, ...lines, reasons);
    }

    // ── Routes ───────────────────────────────────────────────────────────────
    function routeFor(s) { return data.routes.find(r => r.slot === s); }

    function renderRouteTabs() {
        const bar = document.getElementById('land-route-tabs');
        bar.replaceChildren(...SLOTS.map(([key, label]) => {
            const r = routeFor(key);
            const b = el('button', { type: 'button', className: 'filter-chip' + (key === slot ? ' active' : ''),
                role: 'tab', 'aria-selected': key === slot ? 'true' : 'false',
                onclick: () => { slot = key; renderRouteTabs(); renderRoute(); } },
                label + (r && r.is_active ? ' ★' : '') + (r && r.targets.length ? ' (' + r.targets.length + ')' : ''));
            if (r && r.is_active) b.title = 'The active route';
            return b;
        }));
    }

    function levelOptions(kind) {
        return [['', '—']].concat((data.levels || []).filter(l => l.kind === kind)
            .map(l => [String(l.level), 'L' + l.level + (l.name ? ' ' + l.name : '')]));
    }

    function unlockFor(level) {
        if (!level || level.unlock_week == null || !season) return '';
        const off = data.config.unlock_offset_hours || 0;
        const d = new Date(season.start_date + 'T00:00:00Z');
        d.setUTCDate(d.getUTCDate() + (level.unlock_week - 1) * 7 + (level.unlock_day - 1));
        d.setUTCHours(d.getUTCHours() + off);
        return d.toISOString().slice(0, 16);
    }

    function memberOptions() {
        return [['', '— none —']].concat((window.SeasonHub.members || []).filter(m => m.rank !== 'EX')
            .map(m => [String(m.member_id), m.name]));
    }

    function buildTargetCard(t, route, list) {
        const ro = !editable();
        const card = el('div', { className: 'land-target card' });
        card._target = t;
        const areaInp = el('input', { type: 'text', className: 'form-input', value: t.area_id || '', placeholder: 'e.g. 86',
            list: areas && areas.length ? 'land-area-list' : null, disabled: ro });
        const nameInp = el('input', { type: 'text', className: 'form-input', value: t.name || '', disabled: ro });
        noTranslate(nameInp);
        const kindSel = select(KINDS, t.kind || 'city', { disabled: ro });
        const levelSel = select(levelOptions(kindSel.value), t.level, { disabled: ro });
        if (t.level != null && !Array.from(levelSel.options).some(o => o.value === String(t.level))) {
            levelSel.appendChild(el('option', { value: String(t.level), textContent: 'L' + t.level }));
            levelSel.value = String(t.level);
        }
        const unlockInp = stInput(t.unlock_at);
        const plannedInp = stInput(t.planned_capture_at);
        const statusSel = select(STATUSES, t.status || 'planned', { disabled: ro });
        const connSel = select(CONNECTED, t.connected || 'unknown', { disabled: ro });
        const preSel = select([['', '— none —']].concat(route.targets.filter(o => o.id && o.id !== t.id)
            .map(o => [String(o.id), o.name])), t.prerequisite_target_id, { disabled: ro });
        const buffInp = el('input', { type: 'text', className: 'form-input', value: t.buff || '', disabled: ro });
        const resInp = el('input', { type: 'number', className: 'form-input', min: '0', value: t.resource_per_hour ?? '', disabled: ro });
        const coordSel = select(memberOptions(), t.coordinator_member_id, { disabled: ro });
        noTranslate(coordSel);
        const notesInp = el('input', { type: 'text', className: 'form-input', value: t.notes || '', disabled: ro });
        [unlockInp, plannedInp].forEach(i => { i.disabled = ro; });

        // Choosing a map ID names the target; choosing a level fills its unlock and output.
        areaInp.addEventListener('change', () => {
            const a = (areas || []).find(x => x.id === areaInp.value.trim());
            if (!a) return;
            nameInp.value = a.name;
            kindSel.value = KINDS.some(k => k[0] === a.kind) ? a.kind : 'city';
            kindSel.dispatchEvent(new Event('change'));
            levelSel.value = String(a.level);
            levelSel.dispatchEvent(new Event('change'));
        });
        kindSel.addEventListener('change', () => {
            const keep = levelSel.value;
            levelSel.replaceChildren(...levelOptions(kindSel.value).map(([v, txt]) => el('option', { value: v, textContent: txt })));
            levelSel.value = keep;
        });
        levelSel.addEventListener('change', () => {
            const lv = (data.levels || []).find(l => l.kind === kindSel.value && String(l.level) === levelSel.value);
            if (!lv) return;
            const u = unlockFor(lv);
            if (u) unlockInp.value = u;
            if (lv.resource_per_hour != null) resInp.value = lv.resource_per_hour;
        });

        card._read = () => ({
            id: t.id || 0, area_id: areaInp.value.trim(), name: nameInp.value.trim(), kind: kindSel.value,
            level: levelSel.value ? parseInt(levelSel.value, 10) : null,
            unlock_at: stValue(unlockInp), planned_capture_at: stValue(plannedInp),
            actual_capture_at: t.actual_capture_at || '',
            prerequisite_target_id: preSel.value ? parseInt(preSel.value, 10) : null,
            buff: buffInp.value.trim(), resource_per_hour: resInp.value === '' ? null : parseInt(resInp.value, 10),
            status: statusSel.value, connected: connSel.value,
            coordinator_member_id: coordSel.value ? parseInt(coordSel.value, 10) : null,
            notes: notesInp.value.trim(),
        });

        const head = el('div', { className: 'land-target-head' },
            el('span', { className: 'land-step', textContent: '' }),
            noTranslate(el('strong', { textContent: t.name || 'New target' })),
            el('span', { className: 'land-status tone-' + ({ captured: 'success', failed: 'danger', declared: 'info' }[t.status] || 'neutral'),
                textContent: (STATUSES.find(s => s[0] === (t.status || 'planned')) || [, ''])[1] }));
        if (!ro) {
            const actions = el('div', { className: 'land-target-actions' });
            actions.appendChild(buildOrderButtons(card, () => renumber(list)));
            actions.appendChild(el('button', { type: 'button', className: 'btn btn-danger btn-sm', 'aria-label': 'Remove target',
                onclick: () => { card.remove(); renumber(list); } }, svgIcon('x')));
            head.appendChild(actions);
        }
        card.append(head, el('div', { className: 'land-target-grid' },
            field('Map ID', areaInp), field('Name', nameInp), field('Kind', kindSel), field('Level', levelSel),
            field('Unlocks (ST)', unlockInp), field('Planned capture (ST)', plannedInp),
            field('Status', statusSel), field('Connected', connSel), field('After', preSel),
            field('Buff', buffInp), field('Output / h', resInp), field('Coordinator', coordSel),
            field('Notes', notesInp, 'land-field-wide')));
        return card;
    }

    function renumber(list) {
        Array.from(list.children).forEach((c, i) => {
            const step = c.querySelector('.land-step');
            if (step) step.textContent = (i + 1) + '.';
        });
        refreshOrderButtons(list);
    }

    function renderRoute() {
        const box = document.getElementById('land-route');
        const route = routeFor(slot) || { slot, targets: [] };
        const ro = !editable();
        const nameInp = el('input', { type: 'text', className: 'form-input land-route-name', value: route.name || '',
            placeholder: SLOTS.find(s => s[0] === slot)[1] + ' route', disabled: ro, 'aria-label': 'Route name' });
        const list = el('div', { className: 'land-targets' });
        route.targets.forEach(t => list.appendChild(buildTargetCard(t, route, list)));
        renumber(list);

        const bar = el('div', { className: 'tab-toolbar land-route-bar' }, nameInp);
        if (route.is_active) bar.appendChild(el('span', { className: 'land-status tone-success', textContent: 'Active route' }));
        if (!ro) {
            bar.appendChild(el('button', { type: 'button', className: 'btn btn-secondary btn-sm',
                onclick: () => { list.appendChild(buildTargetCard({}, route, list)); renumber(list); } }, '+ Target'));
            bar.appendChild(el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => saveRoute(nameInp, list) }, 'Save route'));
            if (route.id && !route.is_active) {
                bar.appendChild(el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => activate(route.id) }, 'Make active'));
            }
        }
        if (route.id) bar.appendChild(el('button', { type: 'button', className: 'btn btn-ghost btn-sm', onclick: () => showHistory(route.id) }, 'History'));
        const empty = route.targets.length ? null : el('p', { className: 'loading-msg',
            textContent: ro ? 'No targets on this route.' : 'No targets yet — add one, by map ID where the season has a map.' });
        const history = el('div', { className: 'land-history', id: 'land-history' });
        const datalist = areas && areas.length ? el('datalist', { id: 'land-area-list' },
            areas.filter(a => a.kind !== 'landmark').map(a => el('option', { value: a.id, label: a.name + ' L' + a.level }))) : null;
        box.replaceChildren(bar, list, ...(empty ? [empty] : []), history, ...(datalist ? [datalist] : []));
    }

    function saveRoute(nameInp, list) {
        const targets = Array.from(list.children).map(c => c._read());
        put('/api/season-hub/land/routes', { season_id: season.id, slot, name: nameInp.value.trim(), targets })
            .then(d => {
                const refused = (d.calendar || []).filter(c => c.action === 'refused');
                showToast(refused.length ? 'Route saved. Not on the calendar: ' + refused.map(c => c.message).join('; ')
                    : 'Route saved.', refused.length ? 'info' : 'success', refused.length ? 8000 : undefined);
                load(true);
            })
            .catch(err => showToast(err.message, 'error'));
    }

    function activate(id) {
        put('/api/season-hub/land/routes/' + id + '/activate', {})
            .then(() => { showToast('Route made active.'); load(true); })
            .catch(err => showToast(err.message, 'error'));
    }

    function showHistory(routeId) {
        const box = document.getElementById('land-history');
        api('/api/season-hub/land/routes/' + routeId + '/revisions').then(d => {
            const revs = d.revisions || [];
            box.replaceChildren(el('h4', { textContent: 'Saved versions' }),
                ...(revs.length ? revs.map(v => el('details', { className: 'land-revision' },
                    el('summary', null, v.saved_at + ' UTC' + (v.saved_by ? ' · ' + v.saved_by : '') + ' · ' + (v.targets || []).length + ' targets'),
                    el('ol', null, (v.targets || []).map(t => el('li', null, noTranslate(el('span', { textContent: t.name })),
                        (t.level ? ' L' + t.level : '') + (t.planned_capture_at ? ' · planned ' + t.planned_capture_at + ' ST' : '') + ' · ' + t.status)))))
                    : [el('p', { className: 'loading-msg', textContent: 'No saved versions.' })]));
        }).catch(err => showToast(err.message, 'error'));
    }

    // ── Capacity, holdings, drops and events (private-docs 275) ─────────────
    const kindLabel = k => (KINDS.find(x => x[0] === k) || [k, k])[1];
    const DROP_STATUSES = [['drop_required', 'Drop required'], ['assigned', 'Assigned'], ['abandoning', 'Abandoning'],
        ['complete', 'Complete'], ['cancelled', 'Cancelled']];
    const post = (url, body) => api(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    let showCancelled = false;

    function calendarToast(prefix, d) {
        const refused = (d.calendar || []).filter(c => c.action === 'refused');
        showToast(refused.length ? prefix + ' Not on the calendar: ' + refused.map(c => c.message).join('; ') : prefix,
            refused.length ? 'info' : 'success', refused.length ? 8000 : undefined);
    }

    function renderExtras() {
        const box = document.getElementById('land-extras');
        if (!data.daily_cards) { box.replaceChildren(); return; }
        box.replaceChildren(renderCards(), renderHoldings(), renderDrops(), renderEvents());
    }

    function section(title, ...children) {
        return el('div', { className: 'card land-section' }, el('h3', { className: 'land-section-title', textContent: title }), ...children);
    }

    function renderCards() {
        const grid = el('div', { className: 'land-day-cards' });
        data.daily_cards.forEach(c => {
            const kindRow = (label, k, kind) => {
                const tone = k.drops_required > 0 ? 'warning' : 'success';
                return el('div', { className: 'land-day-kind' },
                    el('div', { className: 'land-day-kind-head' }, el('strong', { textContent: label }),
                        k.drops_required > 0 ? el('span', { className: 'land-status tone-' + tone, textContent: k.drops_required + ' drop' + (k.drops_required === 1 ? '' : 's') + ' required' }) : null),
                    el('div', { className: 'land-day-figures' },
                        'Held ' + k.held + ' · capturing ' + k.planned + (k.drops_planned ? ' · dropping ' + k.drops_planned : '') + ' · cap ' + k.cap),
                    k.over_daily ? el('div', { className: 'land-day-warn', textContent: 'More captures than the ' + k.daily_limit + ' a day the game allows' }) : null,
                    k.drops_required > 0 && editable() ? el('button', { type: 'button', className: 'btn btn-secondary btn-sm',
                        onclick: () => openDropModal({ kind, date: c.date, count: k.drops_required }) }, 'Plan drops') : null);
            };
            grid.appendChild(el('div', { className: 'land-day-card' },
                el('div', { className: 'land-day-date', textContent: c.date === (data.now || '').slice(0, 10) ? c.date + ' (today)' : c.date }),
                kindRow('Cities', c.cities, 'city'), kindRow('Strongholds', c.strongholds, 'stronghold')));
        });
        return section('Capacity by day', el('p', { className: 'hub-section-note',
            textContent: 'From what is held now, the active route\'s planned captures and the open drops. Server time.' }), grid);
    }

    function renderHoldings() {
        const rows = data.holdings.map(h => el('tr', null,
            el('td', { translate: 'no' }, h.name + (h.area_id ? ' (#' + h.area_id + ')' : '')),
            el('td', { textContent: kindLabel(h.kind) }),
            el('td', { textContent: h.level ? 'L' + h.level : '' }),
            el('td', { textContent: h.captured_at }),
            el('td', null, editable() && (h.kind === 'city' || h.kind === 'stronghold' || h.kind === 'capitol')
                ? el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => openDropModal({ holding: h }) }, 'Drop') : null)));
        const table = el('div', { className: 'table-scroll' }, el('table', { className: 'data-table land-holdings', translate: 'no' },
            el('thead', null, el('tr', null, ['Area', 'Kind', 'Level', 'Captured (ST)', ''].map(t => el('th', { textContent: t, translate: 'yes' })))),
            el('tbody', null, rows.length ? rows : el('tr', null, el('td', { colspan: '5', className: 'loading-msg', translate: 'yes', textContent: 'Nothing held — record a capture below.' })))));
        return section('Holdings', table);
    }

    function activeTargets() {
        const r = (data.routes || []).find(x => x.is_active);
        return r ? r.targets.filter(t => t.status === 'planned' || t.status === 'declared') : [];
    }

    function targetOptions(selected) {
        return [['', '— none —']].concat(activeTargets().map(t => [String(t.id),
            t.name + (t.level ? ' L' + t.level : '') + (t.planned_capture_at ? ' · ' + t.planned_capture_at + ' ST' : '')]));
    }

    function renderDrops() {
        const list = data.drops.filter(d => showCancelled || d.status !== 'cancelled');
        const ro = !editable();
        const rows = list.map(d => {
            const status = select(DROP_STATUSES, d.status, { disabled: ro, 'aria-label': 'Status' });
            const member = select(memberOptions(), d.assigned_member_id, { disabled: ro, translate: 'no', 'aria-label': 'Assigned to' });
            const repl = select(targetOptions(), d.replacement_target_id, { disabled: ro, translate: 'no', 'aria-label': 'Makes room for' });
            if (d.replacement_target_id && !repl.value) {
                repl.appendChild(el('option', { value: String(d.replacement_target_id), textContent: d.replacement_name || 'target ' + d.replacement_target_id }));
                repl.value = String(d.replacement_target_id);
            }
            const notes = el('input', { type: 'text', className: 'form-input', value: d.notes || '', disabled: ro, 'aria-label': 'Notes' });
            const save = ro ? null : el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                put('/api/season-hub/land/drops/' + d.id, { status: status.value, notes: notes.value.trim(),
                    assigned_member_id: member.value ? parseInt(member.value, 10) : null,
                    replacement_target_id: repl.value ? parseInt(repl.value, 10) : null })
                    .then(r => { calendarToast('Drop saved.', r); load(true); })
                    .catch(err => showToast(err.message, 'error'));
            } }, 'Save');
            return el('div', { className: 'land-drop card' + (d.status === 'cancelled' ? ' land-drop-cancelled' : '') },
                el('div', { className: 'land-target-head' },
                    el('strong', { translate: 'no', textContent: d.holding_name + (d.level ? ' L' + d.level : '') }),
                    el('span', { className: 'land-drop-when', textContent: d.must_start_by ? 'Start abandoning by ' + d.must_start_by + ' ST' : 'No time yet — pick a planned target' })),
                el('div', { className: 'land-target-grid' },
                    field('Makes room for', repl), field('Status', status), field('Assigned to', member), field('Notes', notes, 'land-field-wide')),
                save);
        });
        const toggle = el('label', { className: 'land-toggle' },
            el('input', { type: 'checkbox', checked: showCancelled, onchange: e => { showCancelled = e.target.checked; renderExtras(); } }), ' Show cancelled');
        return section('Drops', toggle, ...(rows.length ? rows : [el('p', { className: 'loading-msg', textContent: 'No drops planned.' })]));
    }

    function renderEvents() {
        const ro = !editable();
        const parts = [];
        if (!ro) {
            const areaInp = el('input', { type: 'text', className: 'form-input', placeholder: areas ? 'e.g. 86' : '', 'aria-label': 'Map ID' });
            const nameInp = el('input', { type: 'text', className: 'form-input', translate: 'no' });
            const kindSel = select(KINDS, 'city');
            const levelInp = el('input', { type: 'number', className: 'form-input', min: '1', max: '20' });
            const actionSel = select([['captured', 'Captured'], ['abandoned', 'Abandoned'], ['lost', 'Lost']], 'captured');
            const atInp = stInput(data.now);
            const noteInp = el('input', { type: 'text', className: 'form-input' });
            areaInp.addEventListener('change', () => {
                const a = (areas || []).find(x => String(x.id) === areaInp.value.trim());
                if (!a) return;
                nameInp.value = a.name;
                kindSel.value = a.kind;
                levelInp.value = a.level || '';
            });
            const record = el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                post('/api/season-hub/land/events', { season_id: season.id, area_id: areaInp.value.trim(), name: nameInp.value.trim(),
                    kind: kindSel.value, level: levelInp.value ? parseInt(levelInp.value, 10) : null, action: actionSel.value,
                    at: stValue(atInp), note: noteInp.value.trim() })
                    .then(d => { showToast(d.written ? 'Event recorded.' : 'Already recorded.'); load(true); })
                    .catch(err => showToast(err.message, 'error'));
            } }, 'Record');
            const imp = el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => window.SeasonHub.openImport({
                kind: 'land_events', title: 'land events',
                columns: 'Columns: Action (captured, abandoned or lost) and At (YYYY-MM-DD HH:MM, server time), required; Area (map ID) or Name; Kind, Level, Note. A row already recorded is left unchanged.',
                onDone: () => load(true) }) }, 'Import CSV');
            parts.push(el('div', { className: 'land-target-grid land-event-form' },
                areas ? field('Map ID', areaInp) : null, field('Area', nameInp), field('Kind', kindSel), field('Level', levelInp),
                field('What happened', actionSel), field('When (ST)', atInp), field('Note', noteInp, 'land-field-wide')),
            el('div', { className: 'tab-toolbar' }, record, imp));
        }
        const rows = data.events.map(e => el('tr', null,
            el('td', { textContent: e.at }),
            el('td', { translate: 'no', textContent: e.name + (e.area_id ? ' (#' + e.area_id + ')' : '') }),
            el('td', { textContent: kindLabel(e.kind) + (e.level ? ' L' + e.level : '') }),
            el('td', { textContent: e.action }),
            el('td', { textContent: e.source }),
            el('td', null, ro ? null : el('button', { type: 'button', className: 'btn btn-danger btn-sm', 'aria-label': 'Delete event', onclick: async () => {
                if (!await showConfirm('Delete this ' + e.action + ' event for ' + e.name + '?', 'Delete')) return;
                api('/api/season-hub/land/events/' + e.id, { method: 'DELETE' }).then(() => load(true))
                    .catch(err => showToast(err.message, 'error'));
            } }, svgIcon('x')))));
        parts.push(el('div', { className: 'table-scroll land-events-scroll' }, el('table', { className: 'data-table land-events', translate: 'no' },
            el('thead', null, el('tr', null, ['When (ST)', 'Area', 'Kind', 'What', 'Source', ''].map(t => el('th', { textContent: t, translate: 'yes' })))),
            el('tbody', null, rows.length ? rows : el('tr', null, el('td', { colspan: '6', className: 'loading-msg', translate: 'yes', textContent: 'No events recorded.' }))))));
        return section('Events', ...parts);
    }

    // The drop modal: from a holding (that one ticked) or from a day's card (that kind listed,
    // the officer picks which).
    function openDropModal(o) {
        const modal = document.getElementById('modal-land-drop');
        const box = document.getElementById('land-drop-holdings');
        const hint = document.getElementById('land-drop-hint');
        const repl = document.getElementById('land-drop-replacement');
        const notes = document.getElementById('land-drop-notes');
        const kind = o.holding ? o.holding.kind : o.kind;
        const held = data.holdings.filter(h => o.holding ? h === o.holding : (h.kind === kind || (kind === 'city' && h.kind === 'capitol' && data.config.capitol_counts_toward_cap)));
        hint.textContent = o.holding ? 'Abandon this holding to make room for a planned capture.'
            : o.count + ' ' + (kind === 'city' ? 'cit' + (o.count === 1 ? 'y' : 'ies') : 'stronghold' + (o.count === 1 ? '' : 's')) + ' to drop on ' + o.date + ': pick which.';
        box.replaceChildren(...held.map(h => el('label', { className: 'land-drop-pick' },
            el('input', { type: 'checkbox', checked: !!o.holding, value: '' }),
            ' ' + h.name + (h.level ? ' L' + h.level : '') + ' · held since ' + h.captured_at)));
        Array.from(box.querySelectorAll('input')).forEach((c, i) => { c._h = held[i]; });
        repl.replaceChildren(...targetOptions().map(([v, t]) => el('option', { value: v, textContent: t })));
        const sameDay = activeTargets().find(t => (t.planned_capture_at || '').slice(0, 10) === o.date && (!kind || t.kind === kind));
        repl.value = sameDay ? String(sameDay.id) : (activeTargets()[0] ? String(activeTargets()[0].id) : '');
        notes.value = '';
        document.getElementById('land-drop-cancel').onclick = () => { modal.style.display = ''; };
        document.getElementById('land-drop-save').onclick = () => {
            const picked = Array.from(box.querySelectorAll('input:checked')).map(c => c._h);
            if (!picked.length) { showToast('Pick at least one holding.', 'error'); return; }
            post('/api/season-hub/land/drops', { season_id: season.id, notes: notes.value.trim(),
                replacement_target_id: repl.value ? parseInt(repl.value, 10) : null,
                holdings: picked.map(h => ({ area_id: h.area_id, name: h.name, kind: h.kind, level: h.level })) })
                .then(d => { modal.style.display = ''; calendarToast('Drops planned.', d); load(true); })
                .catch(err => showToast(err.message, 'error'));
        };
        modal.style.display = 'flex';
    }

    // ── Settings: the rules and the level table ─────────────────────────────
    function renderSettings() {
        const body = document.getElementById('land-settings-body');
        const c = data.config;
        const ro = !(CAN_CONFIGURE && season && !season.archived_at);
        const num = (k, ph) => el('input', { type: 'number', className: 'form-input', min: '0', value: c[k] ?? '', placeholder: ph, disabled: ro, 'data-k': k });
        const form = el('div', { className: 'land-settings-grid' },
            field('City captures a day', num('city_daily_captures', '2')),
            field('Stronghold captures a day', num('stronghold_daily_captures', '2')),
            field('Cities held at most', num('city_cap', '6')),
            field('Strongholds held at most', num('stronghold_cap', '6')),
            field('Abandoning takes (minutes)', num('abandon_minutes', '60')),
            field('Unlocks after the reset (hours)', num('unlock_offset_hours', '12')),
            field('Re-declare cooldown (days)', num('redeclare_cooldown_days', '5')),
            field('Capitol counts toward the city cap', el('input', { type: 'checkbox', checked: c.capitol_counts_toward_cap, disabled: ro, 'data-k': 'capitol_counts_toward_cap' })),
            field('Source', el('input', { type: 'text', className: 'form-input', value: c.source || '', disabled: ro, 'data-k': 'source', placeholder: 'e.g. Cpt Hedge City Clash guide' })),
            field('Grade', select([['official', 'Official'], ['cpt_hedge', 'Cpt Hedge'], ['guide', 'Guide'], ['owner', 'Owner'],
                ['requester', 'Requester'], ['conflict', 'Conflict'], ['unverified', 'Unverified']], c.grade || 'unverified', { disabled: ro, 'data-k': 'grade' })));
        const note = c.saved ? null : el('p', { className: 'hub-section-note', textContent: 'Not set for this season yet: these are the defaults (Season 4\'s values).' });
        const saveCfg = ro ? null : el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
            const out = { season_id: season.id };
            form.querySelectorAll('[data-k]').forEach(i => {
                const k = i.dataset.k;
                if (i.type === 'checkbox') out[k] = i.checked;
                else if (i.type === 'number') out[k] = i.value === '' ? (k === 'redeclare_cooldown_days' ? null : 0) : parseInt(i.value, 10);
                else out[k] = i.value;
            });
            put('/api/season-hub/land/config', out).then(() => { showToast('Land settings saved.'); load(true); })
                .catch(err => showToast(err.message, 'error'));
        } }, 'Save settings');

        // The level table: one row per city or stronghold level.
        const tbody = el('tbody');
        const addRow = l => {
            l = l || {};
            const cell = (inp) => el('td', null, inp);
            const row = el('tr', null,
                cell(select([['city', 'City'], ['stronghold', 'Stronghold']], l.kind || 'city', { disabled: ro })),
                cell(el('input', { type: 'number', className: 'form-input', min: '1', max: '20', value: l.level ?? '', disabled: ro })),
                cell(el('input', { type: 'text', className: 'form-input', value: l.name || '', disabled: ro })),
                cell(el('input', { type: 'number', className: 'form-input', min: '0', value: l.resource_per_hour ?? '', disabled: ro })),
                cell(el('input', { type: 'number', className: 'form-input', min: '1', value: l.unlock_week ?? '', disabled: ro })),
                cell(el('input', { type: 'number', className: 'form-input', min: '1', max: '7', value: l.unlock_day ?? '', disabled: ro })),
                cell(el('input', { type: 'text', className: 'form-input', value: l.source || '', disabled: ro })),
                el('td', null, ro ? null : el('button', { type: 'button', className: 'btn btn-danger btn-sm', 'aria-label': 'Remove level',
                    onclick: () => row.remove() }, svgIcon('x'))));
            row._extra = l.extra;
            row._grade = l.grade;
            tbody.appendChild(row);
        };
        (data.levels || []).forEach(addRow);
        const table = el('div', { className: 'table-scroll' }, el('table', { className: 'data-table land-levels' },
            el('thead', null, el('tr', null, ['Kind', 'Level', 'Name', 'Output / h', 'Unlock week', 'Unlock day', 'Source', ''].map(h => el('th', { textContent: h })))),
            tbody));
        const levelBar = ro ? null : el('div', { className: 'tab-toolbar' },
            el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => addRow({}) }, '+ Level'),
            el('button', { type: 'button', className: 'btn btn-secondary btn-sm', onclick: () => window.SeasonHub.openImport({
                kind: 'land_levels', title: 'land levels',
                columns: 'Columns: Kind (city or stronghold) and Level, required; Name, Output per hour, Unlock week, Unlock day, Boss level, Resistance, Suggested power, Source, Grade. A level already in the table is replaced.',
                onDone: () => load(true) }) }, 'Import CSV'),
            el('button', { type: 'button', className: 'btn btn-primary btn-sm', onclick: () => {
                const read = v => v === '' ? null : parseInt(v, 10);
                const levels = Array.from(tbody.children).map(r => {
                    const inp = r.querySelectorAll('select, input');
                    return { kind: inp[0].value, level: read(inp[1].value) || 0, name: inp[2].value.trim(),
                        resource_per_hour: read(inp[3].value), unlock_week: read(inp[4].value), unlock_day: read(inp[5].value),
                        source: inp[6].value.trim(), extra: r._extra || {}, grade: r._grade || 'unverified' };
                });
                put('/api/season-hub/land/levels', { season_id: season.id, levels })
                    .then(() => { showToast('Level table saved.'); load(true); })
                    .catch(err => showToast(err.message, 'error'));
            } }, 'Save level table'));
        body.replaceChildren(...[note, form, saveCfg, el('h4', { textContent: 'Levels' }), table, levelBar].filter(Boolean));
    }
})();
