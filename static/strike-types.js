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
