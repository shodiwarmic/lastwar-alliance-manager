'use strict';

function fmtNumber(n) {
    if (n == null) return '—';
    if (n >= 1_000_000_000) return (n / 1_000_000_000).toFixed(1) + 'B';
    if (n >= 1_000_000)     return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000)         return (n / 1_000).toFixed(1) + 'K';
    return String(n);
}

function statCard(value, label) {
    const card = document.createElement('div');
    card.className = 'acc-stat-card';
    const val = document.createElement('div');
    val.className = 'acc-stat-card-value';
    val.textContent = value;
    const lbl = document.createElement('div');
    lbl.className = 'acc-stat-card-label';
    lbl.textContent = label;
    card.append(val, lbl);
    return card;
}

function memberList(members, valueFormatter) {
    if (!members || !members.length) {
        return Object.assign(document.createElement('p'), { textContent: 'No data.' });
    }
    const list = document.createElement('ul');
    list.className = 'dash-list';
    members.forEach(m => {
        const li = document.createElement('li');
        const nameSpan = document.createElement('span');
        nameSpan.className = 'dash-list-name';
        nameSpan.textContent = m.name + ' (' + m.rank + ')';
        const valSpan = document.createElement('span');
        valSpan.className = 'dash-list-value';
        valSpan.textContent = valueFormatter(m.value);
        li.append(nameSpan, valSpan);
        list.appendChild(li);
    });
    return list;
}

async function boot() {
    let report;
    try {
        const res = await fetch('/api/accountability/report-data');
        if (!res.ok) throw new Error();
        report = await res.json();
    } catch {
        document.getElementById('report-stat-cards').textContent = 'Failed to load report data.';
        return;
    }

    // Stat cards
    const total = (report.tag_counts['At Risk'] || 0) + (report.tag_counts['Needs Improvement'] || 0) + (report.tag_counts['Reliable'] || 0);
    const reliablePct = total ? Math.round(((report.tag_counts['Reliable'] || 0) / total) * 100) + '%' : '—';
    const cards = document.getElementById('report-stat-cards');
    cards.replaceChildren(
        statCard(String(total), 'Total Members'),
        statCard(reliablePct, 'Reliable'),
        statCard(String(report.tag_counts['Needs Improvement'] || 0), 'Needs Improvement'),
        statCard(String(report.tag_counts['At Risk'] || 0), 'At Risk'),
        statCard(String(report.total_strikes), 'Active Strikes'),
    );

    // VS leaders
    const vsLeadersEl = document.getElementById('report-vs-leaders');
    vsLeadersEl.replaceChildren(memberList(report.vs_leaders, fmtNumber));

    // VS underperformers
    const vsUnderEl = document.getElementById('report-vs-under');
    if (!report.vs_underperformers || !report.vs_underperformers.length) {
        vsUnderEl.replaceChildren(Object.assign(document.createElement('p'), {
            textContent: '✅ All members are meeting the VS minimum.',
        }));
    } else {
        vsUnderEl.replaceChildren(memberList(report.vs_underperformers, fmtNumber));
    }

    // Power growth
    const growthEl = document.getElementById('report-power-growth');
    growthEl.replaceChildren(memberList(report.power_growth, v => '+' + fmtNumber(v)));

    // Tag counts
    const tagEl = document.getElementById('report-tag-counts');
    tagEl.replaceChildren();
    [['Reliable', 'acc-tag--reliable'], ['Needs Improvement', 'acc-tag--needs-improvement'], ['At Risk', 'acc-tag--at-risk']].forEach(([tag, cls]) => {
        const row = document.createElement('div');
        row.style.cssText = 'display:flex;justify-content:space-between;align-items:center;padding:6px 0;border-bottom:1px solid var(--border-color);';
        const label = document.createElement('span');
        label.className = 'acc-tag ' + cls;
        label.textContent = tag;
        const count = document.createElement('span');
        count.style.fontWeight = '600';
        count.textContent = String(report.tag_counts[tag] || 0);
        row.append(label, count);
        tagEl.appendChild(row);
    });
}

document.addEventListener('DOMContentLoaded', boot);
