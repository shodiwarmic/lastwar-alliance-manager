// reports.js — the Reports page: one tab per report the user may run.
//
// The server renders a tab and a panel only for the reports the user holds the key
// for (reports.go → reportDefs). Each report has a renderer in REPORTS below: run(panel)
// fetches its data route and fills the panel. A report runs the first time its tab is
// shown; Run re-runs it with the panel's controls.
//
// Export tables are template skeletons (data-export-csv, translate="no"), so
// table-utils.js wires their buttons at load; a renderer fills <tbody> and calls
// exportReady(table) to enable the downloads once there are rows.

(function () {
    'use strict';

    // --- DOM helpers ---------------------------------------------------------------

    // el('div', {className: 'x', translate: 'no'}, child, 'text', …) — never HTML strings.
    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        Object.entries(props || {}).forEach(([k, v]) => {
            if (v === undefined || v === null || v === false) return;
            if (k === 'className') node.className = v;
            else if (k === 'text') node.textContent = v;
            else if (k === 'dataset') Object.assign(node.dataset, v);
            else node.setAttribute(k, v === true ? '' : v);
        });
        children.flat().forEach(c => {
            if (c === null || c === undefined || c === false) return;
            node.appendChild(typeof c === 'string' || typeof c === 'number' ? document.createTextNode(String(c)) : c);
        });
        return node;
    }

    // Game dates are calendar dates: format them in UTC so no local offset moves the day.
    const DATE_FMT = { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' };
    const DATE_FMT_Y = { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' };
    function fmtDate(d, withYear) {
        if (!d) return '';
        return new Date(d + 'T00:00:00Z').toLocaleDateString(undefined, withYear ? DATE_FMT_Y : DATE_FMT);
    }
    function fmtRange(from, to) {
        return fmtDate(from, from.slice(0, 4) !== to.slice(0, 4)) + ' – ' + fmtDate(to, true);
    }
    function fmtGenerated(iso) {
        return iso ? new Date(iso).toLocaleString() : '';
    }

    function setStatus(panel, text, isError) {
        const s = panel.querySelector('.report-status');
        if (!s) return;
        s.textContent = text || '';
        s.style.display = text ? '' : 'none';
        s.classList.toggle('status-error', !!isError);
    }

    async function fetchReport(url) {
        const res = await fetch(url);
        if (!res.ok) throw new Error((await res.text()).trim() || ('HTTP ' + res.status));
        return res.json();
    }

    // The download buttons follow whether the table has rows.
    function exportReady(table) {
        const h = window.TableExport && TableExport.handleFor(table);
        if (!h) return;
        h.setDownloads(!!table.tBodies[0] && table.tBodies[0].rows.length > 0);
        h.refresh();
    }

    // --- Range controls (report_range_controls in the template) --------------------

    function rangeQuery(panel) {
        const params = new URLSearchParams();
        const sel = panel.querySelector('.report-range');
        if (!sel) return params;
        if (sel.value === 'custom') {
            params.set('from', panel.querySelector('.report-from').value);
            params.set('to', panel.querySelector('.report-to').value);
        } else {
            params.set('weeks', sel.value);
            if (panel.querySelector('.report-current').checked) params.set('current', '1');
        }
        return params;
    }

    function wireRangeControls(panel, run) {
        const sel = panel.querySelector('.report-range');
        if (!sel) return;
        const custom = panel.querySelector('.report-custom');
        const current = panel.querySelector('.report-current').closest('label');
        const sync = () => {
            const isCustom = sel.value === 'custom';
            custom.style.display = isCustom ? 'inline-flex' : 'none';
            current.style.display = isCustom ? 'none' : '';
        };
        sel.addEventListener('change', sync);
        sync();
        panel.querySelector('.report-run').addEventListener('click', () => run(panel));
    }

    // The resolved range and the rule, on screen and on paper.
    function setMeta(panel, lines) {
        const rangeLine = panel.querySelector('.report-range-line');
        if (rangeLine) rangeLine.textContent = lines.filter(Boolean).join(' · ');
        const printMeta = panel.querySelector('.report-print-meta');
        if (printMeta) printMeta.textContent = lines.filter(Boolean).join(' · ');
    }

    // --- Reports -------------------------------------------------------------------

    // name → { run(panel) }. Filled per report below.
    const REPORTS = {};

    // --- Page ----------------------------------------------------------------------

    const ran = {};
    function runReport(name) {
        const r = REPORTS[name];
        const panel = document.getElementById('tab-' + name);
        if (!r || !panel) return;
        ran[name] = true;
        r.run(panel);
    }

    document.addEventListener('DOMContentLoaded', () => {
        Object.keys(REPORTS).forEach(name => {
            const panel = document.getElementById('tab-' + name);
            if (!panel) return;
            wireRangeControls(panel, () => runReport(name));
        });
        document.querySelectorAll('.report-print').forEach(b => b.addEventListener('click', () => window.print()));
        Tabs.init({ hash: true, onActivate: (name) => { if (!ran[name]) runReport(name); } });
    });
})();
