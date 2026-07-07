// static/external-alliances.js — External Alliances registry (read-only view).
// Safe DOM only; reuses the app's global helpers. The registry is populated by allies,
// VS League opponent lookups, and prospect sources.
'use strict';

(function () {
    let all = [];

    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        if (props) Object.entries(props).forEach(([k, v]) => {
            if (v == null) return;
            if (k === 'className') node.className = v;
            else if (k === 'text') node.textContent = v;
            else if (k === 'onclick') node.addEventListener('click', v);
            else node.setAttribute(k, v);
        });
        children.flat().forEach(c => { if (c != null) node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c); });
        return node;
    }
    const fmtBig = n => {
        if (n == null) return '—';
        n = Number(n);
        if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B';
        if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
        if (n >= 1e3) return (n / 1e3).toFixed(0) + 'K';
        return '' + n;
    };

    const tbody = document.getElementById('ext-tbody');
    const search = document.getElementById('ext-search');
    const countEl = document.getElementById('ext-count');

    function relBadges(a) {
        const wrap = el('span', { className: 'ext-rel' });
        if (a.is_ally) wrap.appendChild(el('span', { className: 'ext-badge ally', text: 'Ally' }));
        if (a.is_opponent) wrap.appendChild(el('span', { className: 'ext-badge opponent', text: 'VS opponent' }));
        if (a.prospect_count > 0) wrap.appendChild(el('span', { className: 'ext-badge prospect', text: a.prospect_count + ' prospect' + (a.prospect_count === 1 ? '' : 's') }));
        if (!wrap.childNodes.length) wrap.appendChild(el('span', { className: 'ext-badge none', text: '—' }));
        return wrap;
    }

    function render(list) {
        tbody.replaceChildren();
        if (!list.length) {
            tbody.appendChild(el('tr', {}, el('td', { colspan: '8', style: 'text-align:center;padding:20px;color:var(--color-text-muted);', text: 'No alliances yet.' })));
            countEl.textContent = '';
            return;
        }
        countEl.textContent = list.length + ' of ' + all.length;
        list.forEach(a => {
            const tagCell = el('td', {});
            tagCell.appendChild(el('span', { className: 'ext-tag', text: a.tag ? '[' + a.tag + ']' : '—' }));
            if (a.lastrank_id) tagCell.appendChild(el('a', { className: 'ext-lr', href: 'https://lastrank.fun/a/' + a.lastrank_id, target: '_blank', rel: 'noopener noreferrer', title: 'View on LastRank' }, '↗'));
            tbody.appendChild(el('tr', {},
                tagCell,
                el('td', { text: a.name || '—' }),
                el('td', { className: 'ext-num', text: a.server != null ? a.server : '—' }),
                el('td', { className: 'ext-num', text: fmtBig(a.power) }),
                el('td', { className: 'ext-num', text: fmtBig(a.kills) }),
                el('td', { className: 'ext-num', text: a.member_count != null ? a.member_count : '—' }),
                el('td', {}, relBadges(a)),
                el('td', { className: 'ext-num', text: (a.updated_at || '').slice(0, 10) || '—' })));
        });
    }

    function applyFilter() {
        const q = search.value.trim().toLowerCase();
        if (!q) return render(all);
        render(all.filter(a => (a.tag || '').toLowerCase().includes(q) || (a.name || '').toLowerCase().includes(q)));
    }

    async function load() {
        try {
            const res = await fetch('/api/external-alliances');
            if (!res.ok) throw new Error((await res.text()).trim() || res.status);
            const txt = (await res.text());
            all = txt ? JSON.parse(txt) : [];
            render(all);
        } catch (e) {
            tbody.replaceChildren(el('tr', {}, el('td', { colspan: '8', style: 'text-align:center;padding:20px;color:var(--color-danger);', text: 'Could not load: ' + e.message })));
        }
    }

    search.addEventListener('input', applyFilter);
    load();
})();
