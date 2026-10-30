/* season-hub-dashboard.js — the Season Hub's Dashboard tab (private-docs 287): the season's
   cards in the layout's order, each from GET /api/season-hub/dashboard (one request, filtered
   by permission server-side). A card the viewer may not see is absent from the data and skipped. */

(function () {
    'use strict';
    if (!document.getElementById('tab-dashboard') || !window.SeasonHub) return;

    let season = null;
    let loadedFor = 0;
    const gen = { n: 0 };

    // ── DOM helper (dashboard.js's el(), copied: page-local) ─────────────────
    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        if (props) {
            Object.entries(props).forEach(([k, v]) => {
                if (v == null || v === false) return;
                if (k === 'className') node.className = v;
                else if (k === 'textContent') node.textContent = v;
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
    const compact = v => v == null ? '—' : formatCompact(v);
    const STATUS = { active: ['Active', 'active'], at_risk: ['At Risk', 'at_risk'], below: ['Below Requirement', 'below'],
        on_track: ['On Track', 'active'], behind: ['Behind', 'below'], unknown: ['Unknown', 'na'], na: ['Not yet rated', 'na'],
        none: ['No route', 'na'], completed: ['Completed', 'active'] };
    const badge = (code, title) => {
        const [label, tone] = STATUS[code] || [code, 'na'];
        return el('span', { className: 'status-badge status-' + tone, textContent: label, title: title || null });
    };
    const empty = text => el('p', { className: 'sd-empty', textContent: text });
    const goTo = tab => el('button', { type: 'button', className: 'btn btn-secondary btn-sm sd-goto',
        textContent: 'Open the ' + tab.charAt(0).toUpperCase() + tab.slice(1) + ' tab', onclick: () => window.SeasonHub.showTab(tab) });

    function load(force) {
        if (!season) return;
        if (!force && loadedFor === season.id) return;
        const sid = season.id;
        const mine = ++gen.n;
        fetch('/api/season-hub/dashboard?season_id=' + sid)
            .then(r => r.ok ? r.json() : Promise.reject(new Error('Failed to load the dashboard.')))
            .then(d => {
                if (mine !== gen.n || !season || season.id !== sid) return;
                loadedFor = sid;
                render(d);
            })
            .catch(err => showToast(err.message, 'error'));
    }
    window.SeasonHub.onSeason(s => {
        if (!season || season.id !== s.id) loadedFor = 0;
        season = s;
        if (window.SeasonHub.tab === 'dashboard') load();
    });
    window.SeasonHub.onTab(name => { if (name === 'dashboard') load(true); });

    const builders = {
        faction_allies: (d) => {
            const groups = (d || []).filter(g => g.allies.length);
            if (!groups.length) return [empty('No allied alliances this season. Mark an alliance Allied on the War tab, or in its Ally record.'), goTo('war')];
            return groups.map(g => el('div', { className: 'sd-section' },
                el('div', { className: 'sd-section-title', textContent: g.label + (g.is_ours ? ' (ours)' : '') }),
                el('ul', { className: 'sd-list' }, g.allies.map(a => el('li', null,
                    el('strong', { translate: 'no', textContent: '[' + a.tag + '] ' + (a.name || '') }),
                    el('span', { className: 'sd-sub', textContent: 'S' + a.server + (a.agreements.length ? ' · ' + a.agreements.join(', ') : '') }),
                    a.terms ? el('span', { className: 'sd-sub', textContent: a.terms }) : null)))));
        },
        leadership_flags: (d) => {
            if (!d) return [];
            const parts = [];
            if (!d.completed_weeks) parts.push(empty('No week of the season has finished yet, so no one is rated.'));
            // A long list (a whole roster tied at 0 % early in a season) shows its first eight and
            // says how many more; Rankings has them all.
            const SHOW = 8;
            const list = (rows, title) => rows.length ? el('div', { className: 'sd-section' },
                el('div', { className: 'sd-section-title', textContent: title + (rows.length > SHOW ? ' (' + rows.length + ')' : '') }),
                el('ul', { className: 'sd-list' }, rows.slice(0, SHOW).map(m => el('li', null,
                    el('span', { className: 'member-rank rank-' + m.rank, textContent: m.rank }),
                    el('strong', { translate: 'no', textContent: m.name }),
                    el('span', { className: 'sd-sub', textContent: m.pct.toFixed(1) + '%' }),
                    m.status ? badge(m.status, m.reason) : null))),
                rows.length > SHOW ? el('div', { className: 'sd-sub' }, '…and ' + (rows.length - SHOW) + ' more. ', goTo('rankings')) : null) : null;
            parts.push(list(d.bottom || [], 'Lowest overall'));
            parts.push(list(d.flagged || [], 'At Risk or Below Requirement'));
            const reviews = d.poll_reviews || [];
            if (reviews.length) {
                parts.push(el('div', { className: 'sd-section' },
                    el('div', { className: 'sd-section-title', textContent: 'Poll reviews waiting (' + reviews.length + ')' }),
                    el('ul', { className: 'sd-list' }, reviews.map(r => el('li', null,
                        el('strong', { translate: 'no', textContent: r.member_name }), el('span', { className: 'sd-sub', textContent: r.war })))),
                    goTo('war')));
            }
            if (!(d.bottom || []).length && !(d.flagged || []).length && !reviews.length && d.completed_weeks) parts.push(empty('Nothing to flag.'));
            return parts;
        },
        season_schedule: (d) => {
            if (!d) return [];
            const line = e => el('li', null, el('span', { className: 'sd-sub', textContent: e.date + (e.all_day ? '' : ' ' + e.time + ' ST') }),
                el('span', { textContent: (e.icon ? e.icon + ' ' : '') + e.type_name }),
                e.status && e.status !== 'confirmed' ? el('span', { className: 'sd-sub', textContent: e.status }) : null);
            const parts = [];
            parts.push(el('div', { className: 'sd-figures' },
                el('div', null, el('div', { className: 'sd-sub', textContent: 'Next' }),
                    el('strong', { textContent: d.next ? (d.next.icon ? d.next.icon + ' ' : '') + d.next.type_name : 'nothing scheduled' }),
                    d.next ? el('div', { className: 'sd-sub', textContent: d.next.date + (d.next.all_day ? '' : ' ' + d.next.time + ' ST') }) : null),
                el('div', null, el('div', { className: 'sd-sub', textContent: 'Conflicts' }),
                    el('strong', { textContent: String(d.conflicts) }), d.conflicts ? goTo('schedule') : null)));
            parts.push(el('div', { className: 'sd-section' }, el('div', { className: 'sd-section-title', textContent: 'Today' }),
                d.today.length ? el('ul', { className: 'sd-list' }, d.today.map(line)) : empty('Nothing today.')));
            parts.push(el('div', { className: 'sd-section' }, el('div', { className: 'sd-section-title', textContent: 'Next 7 days' }),
                d.next_7_days.length ? el('ul', { className: 'sd-list' }, d.next_7_days.map(line)) : empty('Nothing in the next seven days. Push the season\'s events from Edit Season.')));
            return parts;
        },
        land_status: (r) => {
            if (!r) return [];
            if (!r.route_id) return [empty('No active Land route. Plan one on the Land tab.'), goTo('land')];
            return [el('div', null, badge(r.status, (r.reasons || []).join('\n'))),
                el('div', { className: 'sd-figures sd-section' },
                    el('div', null, el('div', { className: 'sd-sub', textContent: r.route_name }), el('strong', { textContent: r.captured + ' of ' + r.total + ' captured' })),
                    r.next ? el('div', null, el('div', { className: 'sd-sub', textContent: 'Next' }),
                        el('strong', { translate: 'no', textContent: r.next.name + (r.next.level ? ' L' + r.next.level : '') }),
                        el('div', { className: 'sd-sub', textContent: r.next.unlock_at ? 'unlocks ' + r.next.unlock_at + ' ST' + (r.next.countdown ? ' (in ' + r.next.countdown + ')' : '') : 'unlock unknown' })) : null),
                el('ul', { className: 'sd-list sd-section' }, (r.reasons || []).map(t => el('li', { className: 'sd-sub', textContent: t })))];
        },
        war_record: (d) => {
            if (!d) return [];
            const wl = x => x.wins + '–' + x.losses + (x.pct != null ? ' · ' + Math.round(x.pct) + '%' : '');
            const r = d.record;
            if (!r.overall.wins && !r.overall.losses && !r.overall.draws && !r.overall.unknown && !(d.factions || []).length) {
                return [empty('No completed wars yet. Record wars on the War tab.'), goTo('war')];
            }
            return [el('div', { className: 'sd-figures' },
                el('div', null, el('div', { className: 'sd-sub', textContent: 'Overall' }), el('strong', { textContent: wl(r.overall) })),
                el('div', null, el('div', { className: 'sd-sub', textContent: 'Attacking' }), el('strong', { textContent: wl(r.attack) })),
                el('div', null, el('div', { className: 'sd-sub', textContent: 'Defending' }), el('strong', { textContent: wl(r.defense) })),
                d.war_rank != null ? el('div', null, el('div', { className: 'sd-sub', textContent: 'War rank' }), el('strong', { textContent: '#' + d.war_rank })) : null),
                (d.factions || []).length ? el('div', { className: 'sd-section' }, el('div', { className: 'sd-section-title', textContent: 'Factions' }),
                    el('ul', { className: 'sd-list' }, d.factions.map(f => el('li', null, el('span', { textContent: f.label + (f.is_ours ? ' (ours)' : '') }),
                        el('strong', { textContent: wl(f.record) }))))) : null];
        },
        reward_tier_status: (d) => {
            if (!d) return [];
            if (!d.own) return [empty('Record our resource total and rank on the War tab.'), goTo('war')];
            return [el('div', null, badge(d.status)),
                el('div', { className: 'sd-figures sd-section' },
                    el('div', null, el('div', { className: 'sd-sub', textContent: 'Rank' }), el('strong', { textContent: d.rank != null ? '#' + d.rank : '—' }),
                        d.group ? el('div', { className: 'sd-sub', textContent: d.group.label }) : null),
                    el('div', null, el('div', { className: 'sd-sub', textContent: 'Target' }), el('strong', { textContent: d.target_rank != null ? '#' + d.target_rank : '—' }),
                        d.target_group ? el('div', { className: 'sd-sub', textContent: d.target_group.label }) : null),
                    el('div', null, el('div', { className: 'sd-sub', textContent: 'Gap' }), el('strong', { textContent: d.gap == null ? '—' : d.gap <= 0 ? 'past it' : compact(d.gap) })),
                    el('div', null, el('div', { className: 'sd-sub', textContent: 'Projection at reset' }), el('strong', { textContent: compact(d.projected_at_reset) })))];
        },
    };

    function render(d) {
        const grid = document.getElementById('season-dashboard-grid');
        const cards = (d.cards || []).filter(c => c.visible && Object.prototype.hasOwnProperty.call(d.data, c.key) && builders[c.key]);
        if (!cards.length) {
            grid.replaceChildren(empty('No cards to show. Edit Season → Layout chooses the cards.'));
            return;
        }
        grid.replaceChildren(...cards.map(c => el('section', { className: 'card sd-card', 'data-card': c.key },
            el('h3', null, c.label || c.default_label), ...builders[c.key](d.data[c.key]))));
    }
})();
