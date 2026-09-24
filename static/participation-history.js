// participation-history.js — one member's participation, rendered the same way on
// the per-member Accountability page and on My Participation (Profile). Both feed
// it the shape /api/participation/members/{id} and /api/participation/me return.
//
// Only boards that were actually recorded appear: an event nobody recorded says
// nothing about anyone, so it is never shown as a gap.
'use strict';

const ParticipationHistory = (() => {
    const LABEL = { present: 'Present', zero: 'Zero', missed: 'Missed', excused: 'Excused' };

    function fmt(v) {
        return v == null ? '—' : Number(v).toLocaleString('en-US');
    }

    function badge(cls, text) {
        const s = document.createElement('span');
        s.className = 'pt-badge pt-badge--' + cls;
        s.textContent = text;
        return s;
    }

    // opts.counts: show the boards / missed / excused summary above the table.
    // opts.emptyText: what to say when nothing has been recorded for this member.
    function render(container, history, opts) {
        opts = opts || {};
        container.replaceChildren();
        if (opts.counts) {
            const counts = document.createElement('div');
            counts.className = 'pt-counts';
            counts.append(
                badge('muted', `${history.boards} board${history.boards === 1 ? '' : 's'}`),
                badge('missed', `${history.missed} missed`),
                badge('excused', `${history.excused} excused`));
            container.appendChild(counts);
        }
        if (!history.rows.length) {
            const p = document.createElement('p');
            p.className = 'empty-state';
            p.textContent = opts.emptyText || 'No participation recorded yet.';
            container.appendChild(p);
            return;
        }
        const table = document.createElement('table');
        table.className = 'data-table';
        const thead = document.createElement('thead');
        const htr = document.createElement('tr');
        ['Date', 'Event', 'Status', 'Rank', 'Score'].forEach(h => {
            const th = document.createElement('th');
            th.textContent = h;
            htr.appendChild(th);
        });
        thead.appendChild(htr);
        const tbody = document.createElement('tbody');
        history.rows.forEach(r => {
            const tr = document.createElement('tr');
            const date = document.createElement('td');
            if (opts.linkBoards) {
                const a = document.createElement('a');
                a.href = '/participation/' + r.event_id;
                a.textContent = r.event_date;
                date.appendChild(a);
            } else {
                date.textContent = r.event_date;
            }
            const event = document.createElement('td');
            event.textContent = (r.type_icon ? r.type_icon + ' ' : '') + r.type_name
                + (r.task_force ? ' · TF ' + r.task_force : '');
            const rank = document.createElement('td');
            rank.textContent = r.rank != null ? String(r.rank) : '—';
            const score = document.createElement('td');
            score.textContent = fmt(r.score);
            if (r.score != null && r.score_label) score.title = r.score_label;
            const status = document.createElement('td');
            status.appendChild(badge(r.status, LABEL[r.status] || r.status));
            if (r.role) {
                status.append(' ', badge('role', r.role));
            }
            if (r.reason) {
                const reason = document.createElement('span');
                reason.className = 'pt-reason';
                reason.textContent = r.reason;
                status.appendChild(reason);
                if (window.TranslateBlock) TranslateBlock.attach(reason, r.reason);
            }
            tr.append(date, event, status, rank, score);
            tbody.appendChild(tr);
        });
        table.append(thead, tbody);
        const wrap = document.createElement('div');
        wrap.className = 'table-scroll';
        wrap.appendChild(table);
        container.appendChild(wrap);
    }

    return { render };
})();
