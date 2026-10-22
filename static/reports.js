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

// A growth-window control: a select of presets (works on every phone, unlike a
    // datalist on a number input) and a number field for any other value, shown only for
    // Custom. The number field holds the value either way; onChange fires when it changes.
    function windowControl(input, value, onChange) {
        const select = document.getElementById(input.id + '-select');
        const isPreset = v => [...select.options].some(o => o.value === String(v));
        const show = (v) => {
            input.value = v;
            select.value = isPreset(v) ? String(v) : 'custom';
            input.style.display = select.value === 'custom' ? '' : 'none';
        };
        show(value);
        select.addEventListener('change', () => {
            if (select.value === 'custom') {
                input.style.display = '';
                input.focus();
                return;
            }
            input.value = select.value;
            input.style.display = 'none';
            onChange();
        });
        input.addEventListener('change', onChange);
        return { show };
    }
    let weeklyWindowCtl = null;

    // The growth window: remembered per browser (UserPrefs), clamped to 1–365.
    function weeklyWindow() {
        const n = parseInt(document.getElementById('weekly-growth-window').value, 10);
        return Number.isFinite(n) ? Math.min(365, Math.max(1, n)) : 30;
    }

    // The weekly donation result: last complete Monday–Sunday week, named, because it is
    // not the VS week the rest of the summary reports (on every day but Monday).
    function donationMinLabel(v, source) {
        const t = v > 0 ? v.toLocaleString('en-US') : 'off';
        return source === 'current_setting' ? t + " (today's setting — no record for this week)" : t;
    }

    function renderWeeklyDonations(d) {
        const heading = document.getElementById('weekly-donations-heading');
        const box = document.getElementById('weekly-donations');
        if (!heading || !box) return;
        if (!d) {
            heading.textContent = 'Donations';
            box.replaceChildren(el('p', { className: 'report-empty', text: 'No donation readings for last week.' }));
            return;
        }
        heading.textContent = 'Donations, week of ' + fmtDate(d.week_date);
        const pts = v => Number(v).toLocaleString('en-US');
        const parts = [
            el('h4', { className: 'report-subhead', text: 'Top donors' }),
            memberList(d.leaders, pts),
            el('h4', { className: 'report-subhead', text: d.minimum.weekly > 0
                ? 'Below the weekly minimum of ' + d.minimum.weekly.toLocaleString('en-US') : 'Below the weekly minimum' }),
        ];
        if (d.minimum.weekly > 0 && d.minimum.source === 'current_setting') {
            parts.push(el('p', { className: 'report-week-note', text: "Today's setting: no minimum was recorded for that week." }));
        }
        if (!(d.minimum.weekly > 0)) {
            parts.push(el('p', { className: 'report-empty', text: 'No weekly minimum was set for that week.' }));
        } else if (!d.below.length) {
            parts.push(el('p', {}, svgIcon('check'), ' Everyone read to the end met it.'));
        } else {
            parts.push(memberList(d.below, pts));
        }
        parts.push(el('p', { className: 'report-week-note',
            text: `${d.members_read} members read, ${d.read_to_end} read to the end (a Sunday reading). Only those read to the end are judged.` }));
        box.replaceChildren(...parts);
    }

    async function runWeekly(panel) {
        setStatus(panel, 'Loading…');
        const input = document.getElementById('weekly-growth-window');
        if (weeklyWindowCtl) weeklyWindowCtl.show(weeklyWindow());
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

        renderWeeklyDonations(rep.donations, rep.generated_at);

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
            numCell(m.no_score_days ? String(m.no_score_days) : '—', m.no_score_days),
            numCell(String(m.misses), m.misses),
            m.worst ? el('td', { dataset: { exportValue: m.worst.date } },
                fmtDate(m.worst.date) + ' · ' + formatCompact(m.worst.points)) : el('td', { text: '—' }),
            m.misses ? numCell(formatCompact(m.avg_shortfall), m.avg_shortfall) : el('td', { text: '—' })));
        if (!rows.length) {
            rows.push(el('tr', {}, el('td', { colspan: '7', className: 'report-empty', text: 'Nobody missed the minimum in this range.' })));
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
        if (cov.no_score_days) {
            summary.push(el('p', { className: 'report-note', text: `No score recorded on ${cov.no_score_days} member-days (the member has no VS points for that week): shown under "No score", not counted as misses.` }));
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

    // --- Participation grid (GET /api/reports/participation) -------------------------

    const GRID_LETTER = { present: 'P', zero: '0', missed: 'M', excused: 'E' };
    const GRID_RULE = {
        absent: 'not on the board = missed',
        zero: 'on the board with 0 = missed; not on it is blameless',
        role: 'a starter not on the board = missed; subs are never penalised',
    };

    function gridCellTitle(c) {
        if (c.status === 'excused') return 'Excused' + (c.reason ? ': ' + c.reason : '');
        if (c.status === 'missed') return 'Missed';
        const bits = [];
        if (c.rank != null) bits.push('rank ' + c.rank);
        if (c.score != null) bits.push('score ' + Number(c.score).toLocaleString());
        return (c.status === 'zero' ? 'Zero score' : 'Present') + (bits.length ? ' · ' + bits.join(', ') : '');
    }

    function renderParticipation(panel, rep) {
        const table = document.getElementById('participation-table');
        const headRow = table.tHead.rows[0];
        headRow.querySelectorAll('th[data-board]').forEach(th => th.remove());
        rep.boards.forEach(b => headRow.appendChild(el('th', {
            className: 'report-grid-board',
            dataset: { board: String(b.event_id), exportKey: 'board-' + b.event_id },
            title: `${b.type_name} · ${GRID_RULE[b.absence_rule] || b.absence_rule} · ${b.entries} on the board, ${b.missed} missed, ${b.excused} excused`,
        }, (b.type_icon ? b.type_icon + ' ' : '') + b.type_short + (b.task_force ? ' ' + b.task_force : ''),
            ' ', el('br'), el('span', { className: 'report-grid-date', text: fmtDate(b.event_date) }))));

        const rows = rep.members.map(m => {
            const vs = m.vs
                ? el('td', { className: 'num', dataset: { exportValue: `${m.vs.misses}/${m.vs.days_evaluated}` },
                    title: m.vs.no_score_days ? `${m.vs.no_score_days} days with no score recorded` : null,
                    text: `${m.vs.misses} / ${m.vs.days_evaluated}` })
                : el('td', { dataset: { exportValue: '' }, title: 'Former member: not in the VS report', text: '—' });
            return el('tr', {},
                el('td', { text: m.name }),
                el('td', {}, el('span', { className: 'member-rank rank-' + m.rank, text: m.rank })),
                numCell(String(m.boards), m.boards),
                numCell(String(m.missed), m.missed),
                numCell(String(m.excused), m.excused),
                vs,
                ...rep.boards.map(b => {
                    const c = m.cells[b.event_id];
                    if (!c) return el('td', { className: 'report-grid-cell', dataset: { exportValue: '' } });
                    return el('td', { className: 'report-grid-cell', title: gridCellTitle(c), dataset: { exportValue: c.status } },
                        el('span', { className: 'pt-badge pt-badge--' + c.status, text: GRID_LETTER[c.status] || c.status }));
                }));
        });
        if (!rows.length) rows.push(el('tr', {}, el('td', { colspan: '6', className: 'report-empty', text: 'No members.' })));
        table.tBodies[0].replaceChildren(...rows);
        exportReady(table);

        const byType = {};
        rep.boards.forEach(b => { byType[b.type_short] = (byType[b.type_short] || 0) + 1; });
        const types = Object.entries(byType).map(([k, n]) => `${k} ${n}`).join(', ');
        document.getElementById('participation-summary').replaceChildren(
            el('p', { className: 'report-summary-lead', text: rep.boards.length
                ? `${rep.boards.length} recorded ${rep.boards.length === 1 ? 'board' : 'boards'} (${types}), ${rep.members.length} members.`
                : 'No board was recorded in this range.' }),
            el('p', { className: 'report-note', text: 'VS misses / days: days below that week\'s VS daily minimum over the evaluated VS days in the same range, as in the VS misses report.' }));
        setMeta(panel, [fmtRange(rep.from, rep.to), 'Generated ' + fmtGenerated(rep.generated_at)]);
        showBody(panel);
    }

    async function runParticipation(panel) {
        setStatus(panel, 'Loading…');
        try {
            renderParticipation(panel, await fetchReport('/api/reports/participation?' + rangeQuery(panel)));
        } catch (e) {
            panel.querySelector('.report-body').style.display = 'none';
            setStatus(panel, 'Could not run the report: ' + e.message, true);
        }
    }
    REPORTS.participation = { run: runParticipation };

    // --- Donations (GET /api/reports/donations) --------------------------------------

    async function loadDonationsMemberPicker() {
        const sel = document.getElementById('donations-member');
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

    function renderDonations(panel, rep) {
        const cov = rep.coverage;
        const weekly = rep.members.filter(m => m.weeks_below_weekly > 0).length;
        const daily = rep.members.filter(m => m.weeks_below_daily > 0).length;
        const summary = [el('p', { className: 'report-summary-lead',
            text: `${weekly} of ${cov.members_evaluated} members below the weekly minimum in at least one week, ${daily} below the daily minimum. Monday–Sunday weeks.` })];
        const plain = v => v > 0 ? v.toLocaleString('en-US') : 'off';
        summary.push(el('ul', { className: 'report-summary-list' }, rep.weeks.map(w =>
            el('li', {}, 'Week of ' + fmtDate(w.week_date) + ': weekly ' + plain(w.minimums.weekly)
                + ', daily ' + plain(w.minimums.daily)
                + (w.minimums.source === 'current_setting' ? " (today's settings — no record for this week)" : '')
                + ` · ${w.members_read} read, ${w.members_read_to_end} to the end` + (w.over ? '' : ' (still running)')))));
        if (cov.weeks_without_sunday_reading.length) {
            summary.push(el('p', { className: 'report-note', text: 'Nobody was read on the Sunday of: '
                + cov.weeks_without_sunday_reading.map(d => fmtDate(d)).join(', ') + ' — those weeks are not judged on the weekly rule.' }));
        }
        summary.push(el('p', { className: 'report-note', text: 'The weekly rule judges a total read on Sunday once the week is over; the daily rule judges days known exactly (readings on two days in a row), from the member\'s join date.'
            + (cov.members_excluded_ex ? ` Former members are not included (${cov.members_excluded_ex} had readings in this range).` : '') }));
        document.getElementById('donations-summary').replaceChildren(...summary);

        const sumTable = document.getElementById('donations-summary-table');
        const sumRows = rep.members.map(m => el('tr', {},
            el('td', { text: m.name }),
            el('td', {}, el('span', { className: 'member-rank rank-' + m.rank, text: m.rank })),
            numCell(String(m.weeks.length), m.weeks.length),
            numCell(String(m.weeks_below_weekly), m.weeks_below_weekly),
            numCell(String(m.weeks_below_daily), m.weeks_below_daily),
            numCell(String(m.weeks_not_read), m.weeks_not_read)));
        if (!sumRows.length) sumRows.push(el('tr', {}, el('td', { colspan: '6', className: 'report-empty', text: 'No members.' })));
        sumTable.tBodies[0].replaceChildren(...sumRows);
        exportReady(sumTable);

        const minsByWeek = Object.fromEntries(rep.weeks.map(w => [w.week_date, w.minimums]));
        const weekTable = document.getElementById('donations-weeks-table');
        const rows = [];
        rep.members.forEach(m => m.weeks.forEach((w, i) => {
            const mins = minsByWeek[w.week_date] || {};
            const below = [w.below_weekly ? 'weekly' : '', w.below_daily ? `daily (${w.days_below_daily} days)` : '', w.not_read_to_end ? 'not read to end' : '']
                .filter(Boolean).join(', ');
            rows.push(el('tr', { className: i === 0 ? 'report-group-start' : null },
                el('td', { text: m.name }),
                el('td', { dataset: { exportValue: w.week_date }, text: fmtDate(w.week_date, true) }),
                ...w.days.map(DonationMarks.dayCell),
                DonationMarks.totalCell(w.total, w),
                numCell(mins.weekly > 0 ? mins.weekly.toLocaleString('en-US') : 'off', mins.weekly || 0),
                numCell(mins.daily > 0 ? mins.daily.toLocaleString('en-US') : 'off', mins.daily || 0),
                el('td', { text: below })));
        }));
        if (!rows.length) rows.push(el('tr', {}, el('td', { colspan: '13', className: 'report-empty', text: 'No weeks in this range.' })));
        weekTable.tBodies[0].replaceChildren(...rows);
        exportReady(weekTable);

        const legend = document.getElementById('donations-legend');
        if (legend && !legend.childElementCount) legend.appendChild(DonationMarks.legend());

        setMeta(panel, [fmtRange(rep.from, rep.to) + ' (Monday–Sunday weeks)', 'Generated ' + fmtGenerated(rep.generated_at)]);
        showBody(panel);
    }

    async function runDonations(panel) {
        loadDonationsMemberPicker();
        setStatus(panel, 'Loading…');
        const params = rangeQuery(panel);
        const member = document.getElementById('donations-member').value;
        if (member) params.set('member_id', member);
        try {
            renderDonations(panel, await fetchReport('/api/reports/donations?' + params));
        } catch (e) {
            panel.querySelector('.report-body').style.display = 'none';
            setStatus(panel, 'Could not run the report: ' + e.message, true);
        }
    }
    REPORTS.donations = { run: runDonations };

    // --- Season participation (GET /api/reports/season-participation) ---------------
    // One on-screen table (sortable) and a detached export with one column per week, built
    // from the report data (TableExport.attach) — the on-screen week dots don't export.

    let seasonReport = null;
    const seasonSort = { col: null, asc: true };
    const STATUS_LABEL = { active: 'Active', at_risk: 'At Risk', below: 'Below Requirement', na: 'Not yet rated' };
    const ELIG_LABEL = { eligible: 'Eligible', on_pace: 'On Pace', can_still_qualify: 'Can Still Qualify', eliminated: 'Eliminated', not_applicable: '—' };
    const pctText = v => v == null ? '—' : Math.round(v) + '%';
    const warsText = m => m.has_wars ? m.wars_attended + ' / ' + m.wars_completed + ' (of ' + m.wars_scheduled + ')' : m.wars_attended + ' (legacy count)';
    const tierLabel = (rep, k) => { const t = (rep.tiers || []).find(x => x.key === k); return t ? t.label : (k || ''); };
    const scoreLabel = (rep, k) => {
        if (k === 'not_scored') return 'Not scored';
        if (k === 'not_counted') return "Doesn't count";
        if (!k) return '';
        const sl = (rep.score_levels || []).find(x => x.key === k);
        return sl ? sl.label : k;
    };

    function seasonExportTable(rows) {
        const rep = seasonReport;
        const t = document.createElement('table');
        const head = t.createTHead().insertRow();
        const cols = ['Member', 'Rank'].concat((rep ? rep.weeks : []).map(w => 'Week ' + w),
            ['Full', 'Partial', 'Absent', 'Excused', 'Not scored', 'Wars attended', 'Wars completed', 'Wars scheduled', 'Attack',
                'Defense', 'Poll attack %', 'Poll defense %', 'Poll %', 'Overall %', 'Status', 'Season strikes', 'Reviews dismissed',
                'Contribution', 'Reward tier', 'Transferred from', 'Transfer reason'],
            (rep ? rep.tiers : []).map(x => x.label + ' eligibility'));
        cols.forEach(c => head.appendChild(Object.assign(document.createElement('th'), { textContent: c })));
        const body = t.createTBody();
        rows.forEach(m => {
            const tr = body.insertRow();
            const cell = (v, raw) => { const td = tr.insertCell(); td.textContent = v == null ? '' : String(v); if (raw != null) td.dataset.exportValue = raw; };
            cell(m.name); cell(m.rank);
            m.weekly.forEach(k => cell(scoreLabel(rep, k)));
            [m.full, m.partial, m.absent, m.excused, m.not_scored, m.wars_attended].forEach(v => cell(v));
            cell(m.wars_completed); cell(m.wars_scheduled);
            cell(m.has_wars ? m.wars_attack : ''); cell(m.has_wars ? m.wars_defense : '');
            [m.poll_attack_pct, m.poll_defense_pct, m.poll_pct].forEach(v => cell(v == null ? '' : v.toFixed(1)));
            const ov = m.standing.overall;
            cell(ov.applicable ? ov.pct.toFixed(1) : '');
            cell(STATUS_LABEL[m.standing.final] || m.standing.final);
            cell(m.season_strikes_applied); cell(m.reviews_dismissed); cell(m.contribution_total);
            cell(tierLabel(rep, m.reward_tier)); cell(m.transferred_from); cell(m.transfer_override_note);
            rep.tiers.forEach(x => { const e = (m.eligibility || []).find(y => y.tier === x.key); cell(e ? ELIG_LABEL[e.status] || e.status : ''); });
        });
        return t;
    }

    function renderSeasonReport(panel, rep) {
        seasonReport = rep;
        const sel = document.getElementById('season-report-season');
        sel.replaceChildren(...rep.seasons.map(s => el('option', { value: s.id, text: 'S' + s.season_number + ' — ' + s.name + (s.archived ? ' (archived)' : '') })));
        sel.value = String(rep.season.id || '');
        const table = document.getElementById('season-report-table');
        const th = (text, key, type) => el('th', { dataset: key ? { sort: key, sortType: type || 'text' } : null, text });
        table.tHead.replaceChildren(el('tr', {}, th('Member', 'name'), th('Rank', 'rank'), th('Weeks'), th('Full', 'full', 'number'),
            th('Partial', 'partial', 'number'), th('Absent', 'absent', 'number'), th('Not scored', 'not_scored', 'number'),
            th('Wars', 'wars', 'number'), th('Polls', 'polls', 'number'), th('Overall', 'overall', 'number'), th('Status', 'status', 'number'),
            th('Strikes', 'strikes', 'number'), th('Contribution', 'contrib', 'number'), th('Reward', 'reward'), th('Eligibility')));
        const sev = { below: 3, at_risk: 2, active: 1, na: 0 };
        const rows = rep.members.map(m => {
            const uncounted = rep.uncounted_weeks || [];
            const dots = el('span', { className: 'week-dots' }, m.weekly.map((k, i) => el('span', {
                className: 'week-dot ' + (uncounted.includes(i + 1) ? 'dot-uncounted' : k === 'not_scored' ? 'dot-notscored' : !k ? 'dot-empty' : k === 'full' ? 'dot-full' : k === 'partial' ? 'dot-partial' : k === 'excused' ? 'dot-empty' : 'dot-absent'),
                title: 'Week ' + (i + 1) + ': ' + (uncounted.includes(i + 1) ? "doesn't count this season" + (k && k !== 'not_counted' ? ' (' + scoreLabel(rep, k) + ')' : '') : (scoreLabel(rep, k) || 'still to come')) })));
            const ov = m.standing.overall;
            const elig = (m.eligibility || []).filter(e => e.status !== 'not_applicable')
                .map(e => { const t = rep.tiers.find(x => x.key === e.tier); return (t ? t.label : e.tier) + ': ' + (ELIG_LABEL[e.status] || e.status); });
            return el('tr', {},
                el('td', { text: m.name }),
                el('td', {}, el('span', { className: 'member-rank rank-' + m.rank, text: m.rank })),
                el('td', {}, dots),
                numCell(String(m.full), m.full), numCell(String(m.partial), m.partial), numCell(String(m.absent), m.absent),
                numCell(String(m.not_scored), m.not_scored),
                el('td', { dataset: { sortValue: m.wars_attended }, text: warsText(m) }),
                el('td', { dataset: { sortValue: m.poll_pct == null ? '' : m.poll_pct }, title: m.has_wars ? 'Attack ' + pctText(m.poll_attack_pct) + ' · Defense ' + pctText(m.poll_defense_pct) : null, text: pctText(m.poll_pct) }),
                el('td', { dataset: { sortValue: ov.applicable ? ov.pct : '' }, title: ov.reason, text: ov.applicable ? ov.pct.toFixed(1) + '%' : '—' }),
                el('td', { dataset: { sortValue: sev[m.standing.final] || 0 } },
                    el('span', { className: 'status-badge status-' + (STATUS_LABEL[m.standing.final] ? m.standing.final : 'na'), text: STATUS_LABEL[m.standing.final] || m.standing.final })),
                el('td', { dataset: { sortValue: m.season_strikes_applied }, title: m.reviews_dismissed ? m.reviews_dismissed + ' poll reviews dismissed' : null,
                    text: String(m.season_strikes_applied) }),
                el('td', { className: 'num', dataset: { sortValue: m.contribution_total, exportValue: m.contribution_total }, text: formatCompact(m.contribution_total) }),
                el('td', {}, tierLabel(rep, m.reward_tier), m.transferred_from ? el('div', { className: 'report-note', text: 'from ' + m.transferred_from + (m.transfer_override_note ? ' — ' + m.transfer_override_note : '') }) : null),
                el('td', { className: 'report-note', text: elig.join(' · ') }));
        });
        if (!rows.length) rows.push(el('tr', {}, el('td', { colspan: '15', className: 'report-empty', text: 'No season, or nobody on its roster.' })));
        table.tBodies[0].replaceChildren(...rows);
        if (window.TableSort) TableSort.attach(table, { state: seasonSort }).apply();
        const bar = document.getElementById('season-report-export');
        if (!bar.dataset.wired && window.TableExport) {
            bar.dataset.wired = '1';
            TableExport.attach({ bar, key: 'season-participation', filename: 'season-participation',
                getRows: () => (seasonReport ? seasonReport.members : []), build: seasonExportTable });
        }
        document.getElementById('season-report-summary').replaceChildren(
            el('p', { className: 'report-summary-lead', text: rep.season.id
                ? `${rep.members.length} members · ${rep.completed_weeks} of ${rep.weeks.length} weeks complete` + (rep.in_progress ? ' · season in progress' : '')
                : 'No season yet.' }),
            el('p', { className: 'report-note', text: "Overall is points over the weeks that have finished, Excused weeks and the season's weeks that don't count left out; a finished week with no score is Not scored and counts as Absent. Wars are attended of the completed wars (a season without war records shows its weekly key-event count)." }));
        setMeta(panel, [rep.season.id ? 'S' + rep.season.season_number + ' — ' + rep.season.name : '', 'Generated ' + fmtGenerated(rep.generated_at)]);
        showBody(panel);
    }

    async function runSeasonReport(panel) {
        setStatus(panel, 'Loading…');
        const sel = document.getElementById('season-report-season');
        const sid = sel && sel.value ? '?season_id=' + encodeURIComponent(sel.value) : '';
        try {
            renderSeasonReport(panel, await fetchReport('/api/reports/season-participation' + sid));
        } catch (e) {
            panel.querySelector('.report-body').style.display = 'none';
            setStatus(panel, 'Could not run the report: ' + e.message, true);
        }
    }
    REPORTS['season-participation'] = { run: runSeasonReport };
    document.addEventListener('DOMContentLoaded', () => {
        const sel = document.getElementById('season-report-season');
        if (sel) sel.addEventListener('change', () => runSeasonReport(document.getElementById('tab-season-participation')));
    });

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
            weeklyWindowCtl = windowControl(weeklyWin, UserPrefs.get('weeklyGrowthWindow'), () => runReport('weekly'));
        }
        // Export buttons stay off until a report has filled their table.
        document.querySelectorAll('table[data-export-csv]').forEach(exportReady);
        document.getElementById('vs-misses-show-clean')?.addEventListener('change', () => {
            if (vsMissesData) renderVSMissesSummaryRows();
        });
        Tabs.init({ hash: true, onActivate: (name) => { if (!ran[name]) runReport(name); } });
    });
})();
