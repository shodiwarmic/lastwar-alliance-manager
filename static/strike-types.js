// strike-types.js — the managed strike-category list (migration 079), shared by the
// Accountability page and the per-member Accountability page.
//
// One fetch serves both jobs. Labels need EVERY category, active or not: a
// deactivated category still labels the strikes already filed under it. The strike
// form's <select> offers only the active ones. A key the list does not know
// renders as itself rather than as a blank.
'use strict';

const StrikeTypes = (() => {
    let byKey = new Map();
    let list = [];

    async function load() {
        const res = await fetch('/api/accountability/strike-types?all=1');
        if (!res.ok) throw new Error('strike types: HTTP ' + res.status);
        list = await res.json();
        byKey = new Map(list.map(t => [t.key, t]));
        return list;
    }

    function label(key) {
        const t = byKey.get(key);
        return t ? t.label : key;
    }

    // Rebuilds a <select> with a placeholder plus the active categories in order.
    // Keeps the current value when it is still offered.
    function fillSelect(select) {
        const prev = select.value;
        const placeholder = document.createElement('option');
        placeholder.value = '';
        placeholder.textContent = '— select —';
        const opts = list.filter(t => t.active).map(t => {
            const o = document.createElement('option');
            o.value = t.key;
            o.textContent = t.label;
            return o;
        });
        select.replaceChildren(placeholder, ...opts);
        if (opts.some(o => o.value === prev)) select.value = prev;
    }

    return { load, label, fillSelect, all: () => list };
})();

// A strike's optional season context (private-docs 285): the strike form's Season / Week / War
// selects and the badge the strike lists show. Shared by both Accountability pages.
const StrikeSeason = (() => {
    let seasons = null;

    async function load() {
        if (seasons) return seasons;
        const res = await fetch('/api/accountability/season-options');
        seasons = res.ok ? await res.json() : [];
        return seasons;
    }

    const opt = (v, t) => Object.assign(document.createElement('option'), { value: v, textContent: t });

    // Fills #strike-season and follows it with the week and war selects; blank is no season.
    async function fill() {
        const sel = document.getElementById('strike-season');
        const week = document.getElementById('strike-season-week');
        const war = document.getElementById('strike-season-war');
        if (!sel) return;
        const list = await load();
        sel.replaceChildren(opt('', '— none —'), ...list.map(s => opt(String(s.id), 'S' + s.season_number + ' — ' + s.name)));
        const follow = () => {
            const s = list.find(x => String(x.id) === sel.value);
            week.replaceChildren(opt('', '— week —'));
            war.replaceChildren(opt('', '— war —'));
            if (s) {
                for (let w = 1; w <= s.week_count; w++) week.appendChild(opt(String(w), 'Week ' + w));
                s.wars.forEach(w => war.appendChild(opt(String(w.id), w.label)));
            }
            week.disabled = !s;
            war.disabled = !s || !s.wars.length;
            war.closest('label, .strike-season-war').style.display = s && s.wars.length ? '' : 'none';
        };
        sel.onchange = follow;
        war.onchange = () => {
            const s = list.find(x => String(x.id) === sel.value);
            const w = s && s.wars.find(x => String(x.id) === war.value);
            if (w && w.week && !week.value) week.value = String(w.week);
        };
        sel.value = '';
        follow();
    }

    function read() {
        const v = id => { const el = document.getElementById(id); return el && el.value ? parseInt(el.value, 10) : null; };
        return { season_id: v('strike-season'), season_week: v('strike-season-week'), season_war_id: v('strike-season-war') };
    }

    // "S4 · W5 · Attack W5 (2026-11-19)" for a strike with a season, else null.
    function badge(s) {
        if (!s || s.season_id == null) return null;
        const span = document.createElement('span');
        span.className = 'strike-season-badge';
        span.textContent = ['S' + (s.season_number != null ? s.season_number : '?'), s.season_week ? 'W' + s.season_week : null,
            s.season_war || null].filter(Boolean).join(' · ');
        return span;
    }

    return { fill, read, badge };
})();
