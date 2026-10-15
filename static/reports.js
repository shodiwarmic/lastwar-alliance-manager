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

    // A week's VS minimum and where it came from (vsMinimumForWeek's source).
    function minimumLabel(points, source) {
        const v = formatCompact(points);
        if (source === 'override') return v + ", this week's override";
        if (source === 'current_setting') return v + " (today's setting — no record for this week)";
        return v;
    }

    function showBody(panel) {
        setStatus(panel, '');
        panel.querySelector('.report-body').style.display = 'block';
    }

    // --- Weekly summary (GET /api/reports/weekly) ------------------------------------

    function statCard(value, label) {
        return el('div', { className: 'card report-stat' },
            el('div', { className: 'report-stat-value', text: value }),
            el('div', { className: 'report-stat-label', text: label }));
    }

    function memberList(members, fmt) {
        if (!members || !members.length) return el('p', { className: 'report-empty', text: 'No data.' });
        return el('ul', { className: 'report-list' }, members.map(m =>
            el('li', {},
                el('span', { className: 'report-list-name', translate: 'no' }, m.name + ' ',
                    el('span', { className: 'member-rank rank-' + m.rank, text: m.rank })),
                el('span', { className: 'report-list-value', text: fmt(m.value) }))));
    }

    // The growth window: remembered per browser (UserPrefs), clamped to 1–365.
    function weeklyWindow() {
        const n = parseInt(document.getElementById('weekly-growth-window').value, 10);
        return Number.isFinite(n) ? Math.min(365, Math.max(1, n)) : 30;
    }

    async function runWeekly(panel) {
        setStatus(panel, 'Loading…');
        const input = document.getElementById('weekly-growth-window');
        input.value = weeklyWindow();
        UserPrefs.set('weeklyGrowthWindow', input.value);
        let rep;
        try {
            rep = await fetchReport('/api/reports/weekly?window=' + input.value);
        } catch (e) {
            setStatus(panel, 'Failed to load the report: ' + e.message, true);
            return;
        }
        const tags = rep.tag_counts || {};
        const total = (tags['At Risk'] || 0) + (tags['Needs Improvement'] || 0) + (tags['Reliable'] || 0);
        const reliablePct = total ? Math.round(((tags['Reliable'] || 0) / total) * 100) + '%' : '—';
        document.getElementById('weekly-stat-cards').replaceChildren(
            statCard(String(total), 'Total members'),
            statCard(reliablePct, 'Reliable'),
            statCard(String(tags['Needs Improvement'] || 0), 'Needs improvement'),
            statCard(String(tags['At Risk'] || 0), 'At risk'),
            statCard(String(rep.total_strikes), 'Active strikes'));

        document.getElementById('weekly-vs-leaders').replaceChildren(memberList(rep.vs_leaders, formatCompact));

        const under = [];
        if (!rep.vs_underperformers || !rep.vs_underperformers.length) {
            under.push(el('p', {}, svgIcon('check'), ' All members are meeting the VS daily minimum.'));
        } else {
            under.push(memberList(rep.vs_underperformers, formatCompact));
        }
        const bits = [];
        if (rep.fallback_active) bits.push('showing last week');
        if (rep.imported_completed < rep.completed) {
            bits.push(`based on ${rep.imported_completed} of ${rep.completed} completed days imported so far`);
        }
        if (bits.length) under.push(el('p', { className: 'report-week-note', text: 'Flags ' + bits.join('; ') + '.' }));
        document.getElementById('weekly-vs-under').replaceChildren(...under);

        const days = rep.growth_window_days;
        document.getElementById('weekly-growth-heading').textContent = days + '-day power growth';
        const growth = [memberList(rep.power_growth, v => (v > 0 ? '+' : '') + formatCompact(v))];
        if (rep.power_growth_excluded) {
            growth.push(el('p', { className: 'report-week-note',
                text: `${rep.power_growth_excluded} ${rep.power_growth_excluded === 1 ? 'member has' : 'members have'} no reading ${days} days old, so no growth to rank.` }));
        }
        document.getElementById('weekly-power-growth').replaceChildren(...growth);

        document.getElementById('weekly-tag-counts').replaceChildren(...[
            ['Reliable', 'report-tag--reliable'], ['Needs Improvement', 'report-tag--needs-improvement'], ['At Risk', 'report-tag--at-risk'],
        ].map(([tag, cls]) => el('div', { className: 'report-tag-row' },
            el('span', { className: 'report-tag ' + cls, text: tag }),
            el('span', { className: 'report-tag-count', text: String(tags[tag] || 0) }))));

        setMeta(panel, [
            'Week of ' + fmtDate(rep.week_date, true),
            'VS daily minimum ' + minimumLabel(rep.vs_min, rep.vs_min_source),
            'Generated ' + fmtGenerated(rep.generated_at),
        ]);
        showBody(panel);
    }
    REPORTS.weekly = { run: runWeekly };

    // --- VS misses (GET /api/reports/vs-misses) --------------------------------------

    let vsMissesData = null;

    // A cell showing `text` that exports `raw` (table-utils reads data-export-value).
    function numCell(text, raw) {
        return el('td', { className: 'num', dataset: { exportValue: String(raw) }, text });
    }

    async function loadMemberPicker() {
        const sel = document.getElementById('vs-misses-member');
        if (!sel || sel.dataset.loaded) return;
        sel.dataset.loaded = '1';
        try {
            const res = await fetch('/api/members');
            if (!res.ok) return;
            const members = (await res.json()).filter(m => m.rank !== 'EX')
                .sort((a, b) => a.name.localeCompare(b.name));
            members.forEach(m => sel.appendChild(el('option', { value: m.id, text: m.name })));
        } catch { /* the picker stays at Everyone */ }
    }

    function renderVSMissesSummaryRows() {
        const table = document.getElementById('vs-misses-summary-table');
        const showClean = document.getElementById('vs-misses-show-clean').checked;
        const rows = vsMissesData.members.filter(m => showClean || m.misses > 0).map(m => el('tr', {},
            el('td', { text: m.name }),
            el('td', {}, el('span', { className: 'member-rank rank-' + m.rank, text: m.rank })),
            numCell(String(m.days_evaluated), m.days_evaluated),
            numCell(String(m.misses), m.misses),
            m.worst ? el('td', { dataset: { exportValue: m.worst.date } },
                fmtDate(m.worst.date) + ' · ' + formatCompact(m.worst.points)) : el('td', { text: '—' }),
            m.misses ? numCell(formatCompact(m.avg_shortfall), m.avg_shortfall) : el('td', { text: '—' })));
        if (!rows.length) {
            rows.push(el('tr', {}, el('td', { colspan: '6', className: 'report-empty', text: 'Nobody missed the minimum in this range.' })));
        }
        table.tBodies[0].replaceChildren(...rows);
        exportReady(table);
    }

    function renderVSMisses(panel, rep) {
        vsMissesData = rep;
        const cov = rep.coverage;
        const missers = rep.members.filter(m => m.misses > 0);
        const totalMisses = missers.reduce((n, m) => n + m.misses, 0);

        // The rules the figures rest on, in words: each week's minimum, the days not
        // evaluated, and who is left out.
        const summary = [el('p', { className: 'report-summary-lead',
            text: `${totalMisses} ${totalMisses === 1 ? 'miss' : 'misses'} by ${missers.length} of ${cov.members_evaluated} members, over ${cov.days_evaluated} evaluated VS days.` })];
        const weekList = el('ul', { className: 'report-summary-list' }, rep.weeks.map(w =>
            el('li', {}, 'Week of ' + fmtDate(w.week_date) + ': daily minimum ' + minimumLabel(w.minimum.points, w.minimum.source))));
        summary.push(weekList);
        if (cov.days_not_imported.length) {
            summary.push(el('p', { className: 'report-note', text: 'Not imported, so not evaluated: '
                + cov.days_not_imported.map(d => fmtDate(d)).join(', ') + '.' }));
        }
        summary.push(el('p', { className: 'report-note', text: 'A day counts once it is over and imported, from the member\'s join date.'
            + (cov.members_excluded_ex ? ` Former members are not included (${cov.members_excluded_ex} had points in this range).` : '') }));
        document.getElementById('vs-misses-summary').replaceChildren(...summary);

        renderVSMissesSummaryRows();

        const table = document.getElementById('vs-misses-table');
        const rows = [];
        missers.forEach(m => m.days.forEach((d, i) => rows.push(el('tr', { className: i === 0 ? 'report-group-start' : null },
            el('td', { text: m.name }),
            el('td', {}, el('span', { className: 'member-rank rank-' + m.rank, text: m.rank })),
            el('td', { dataset: { exportValue: d.date }, text: fmtDate(d.date, true) }),
            el('td', { text: d.weekday }),
            numCell(formatCompact(d.minimum), d.minimum),
            numCell(formatCompact(d.points), d.points),
            numCell(formatCompact(d.shortfall), d.shortfall)))));
        if (!rows.length) {
            rows.push(el('tr', {}, el('td', { colspan: '7', className: 'report-empty', text: 'No misses in this range.' })));
        }
        table.tBodies[0].replaceChildren(...rows);
        exportReady(table);

        setMeta(panel, [fmtRange(rep.from, rep.to), 'Generated ' + fmtGenerated(rep.generated_at)]);
        showBody(panel);
    }

    async function runVSMisses(panel) {
        loadMemberPicker();
        setStatus(panel, 'Loading…');
        const params = rangeQuery(panel);
        const member = document.getElementById('vs-misses-member').value;
        if (member) params.set('member_id', member);
        try {
            renderVSMisses(panel, await fetchReport('/api/reports/vs-misses?' + params));
        } catch (e) {
            panel.querySelector('.report-body').style.display = 'none';
            setStatus(panel, 'Could not run the report: ' + e.message, true);
        }
    }
    REPORTS['vs-misses'] = { run: runVSMisses };

    // --- Page ----------------------------------------------------------------------

    const ran = {};
    function runReport(name) {
        const r = REPORTS[name];
        const panel = document.getElementById('tab-' + name);
        if (!r || !panel) return;
        ran[name] = true;
        r.run(panel);
    }

    // Paper is white: print in the light theme whatever the screen uses (the dark
    // tokens hang off html.theme-dark / html.theme-auto), then put it back.
    let printThemes = [];
    window.addEventListener('beforeprint', () => {
        const c = document.documentElement.classList;
        printThemes = [...c].filter(x => x === 'theme-dark' || x === 'theme-auto');
        printThemes.forEach(x => c.remove(x));
    });
    window.addEventListener('afterprint', () => {
        printThemes.forEach(x => document.documentElement.classList.add(x));
        printThemes = [];
    });

    document.addEventListener('DOMContentLoaded', () => {
        Object.keys(REPORTS).forEach(name => {
            const panel = document.getElementById('tab-' + name);
            if (!panel) return;
            wireRangeControls(panel, () => runReport(name));
        });
        document.querySelectorAll('.report-print').forEach(b => b.addEventListener('click', () => window.print()));
        const weeklyWin = document.getElementById('weekly-growth-window');
        if (weeklyWin) {
            weeklyWin.value = UserPrefs.get('weeklyGrowthWindow');
            weeklyWin.addEventListener('change', () => runReport('weekly'));
        }
        // Export buttons stay off until a report has filled their table.
        document.querySelectorAll('table[data-export-csv]').forEach(exportReady);
        document.getElementById('vs-misses-show-clean')?.addEventListener('change', () => {
            if (vsMissesData) renderVSMissesSummaryRows();
        });
        Tabs.init({ hash: true, onActivate: (name) => { if (!ran[name]) runReport(name); } });
    });
})();
