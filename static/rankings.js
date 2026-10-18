const API_BASE = '/api';

// Global Chart instances so we can destroy/recreate them cleanly
let charts = {
    troop: null,
    squad: null,
    vs: null
};

let rawGrowthData = [];
let allVsData = [];
let rawKillData = [];
let rawHQData = [];
let rawProfessionData = [];
let currentVsWeek = null;

// Colours read from CSS tokens at draw time; redraws on themechange (see handler below).
function getChartPalette() {
    const cs = getComputedStyle(document.documentElement);
    const t = n => cs.getPropertyValue(n).trim();
    return {
        text:    t('--color-text-muted'),
        card:    t('--color-card'),
        accent:  t('--color-accent'),
        success: t('--color-success'),
        warning: t('--color-warning'),
        danger:  t('--color-danger'),
        info:    t('--color-info'),
        purple:  t('--color-purple'),
        muted:   t('--color-text-muted'),
        series: [
            t('--color-accent'),
            t('--color-purple'),
            t('--color-info'),
            t('--color-success'),
            t('--color-warning'),
            t('--color-danger'),
            t('--color-text-mid'),
            t('--color-text-muted'),
        ],
    };
}

// --- Global Chart.js Styling ---
// This ensures the charts match your app's typography and adapt to light/dark themes
Chart.defaults.font.family = '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif';
Chart.defaults.color = getComputedStyle(document.documentElement).getPropertyValue('--color-text-muted').trim();
Chart.defaults.plugins.tooltip.backgroundColor = 'rgba(26, 32, 44, 0.9)';
Chart.defaults.plugins.tooltip.padding = 12;
Chart.defaults.plugins.tooltip.cornerRadius = 8;
Chart.defaults.plugins.tooltip.titleFont = { size: 14, weight: 'bold' };
Chart.defaults.plugins.tooltip.bodyFont = { size: 13 };

// --- Tab 3: Troop Kills ---
async function loadKillData() {
    try {
        const response = await fetch(`${API_BASE}/kill-history`);
        if (!response.ok) throw new Error('Failed to load kill data');

        rawKillData = await response.json() || [];
        renderKillTable(rawKillData);
    } catch (error) {
        console.error('Error:', error);
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 6;
        td.className = 'error';
        td.textContent = 'Failed to load data.';
        tr.appendChild(td);
        document.getElementById('kills-tbody').replaceChildren(tr);
    }
}

function formatKillDelta(delta) {
    const span = document.createElement('span');
    if (delta === null || delta === undefined) {
        span.className = 'neutral-growth';
        span.textContent = '—';
    } else if (delta > 0) {
        span.className = 'positive-growth';
        span.textContent = '+' + formatNumber(delta);
    } else if (delta < 0) {
        span.style.color = 'var(--color-danger)';
        span.textContent = formatNumber(delta);
    } else {
        span.className = 'neutral-growth';
        span.textContent = '0';
    }
    return span;
}

function buildKillRow(k) {
    const tr = document.createElement('tr');
    tr.dataset.search = k.member_name + ' ' + k.member_rank;

    const tdName = document.createElement('td');
    const strong = document.createElement('strong');
    strong.textContent = k.member_name;
    tdName.appendChild(strong);

    const tdRank = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = `member-rank rank-${k.member_rank}`;
    badge.textContent = k.member_rank;
    tdRank.appendChild(badge);

    const tdKills = document.createElement('td');
    tdKills.style.fontWeight = '600';
    tdKills.style.color = 'var(--color-danger)';
    tdKills.textContent = formatNumber(k.current_kills);

    const td7d = document.createElement('td');
    td7d.appendChild(formatKillDelta(k.kills_delta_7d));

    const td30d = document.createElement('td');
    td30d.appendChild(formatKillDelta(k.kills_delta_30d));

    const tdDate = document.createElement('td');
    tdDate.style.color = 'var(--color-text-muted)';
    tdDate.style.fontSize = '0.9em';
    if (k.last_recorded_at) {
        const d = new Date(k.last_recorded_at.replace(' ', 'T') + 'Z');
        tdDate.textContent = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
    } else {
        tdDate.textContent = '—';
    }

    tr.append(tdName, tdRank, tdKills, td7d, td30d, tdDate);
    return tr;
}

function renderKillTable(data) {
    const tbody = document.getElementById('kills-tbody');
    if (!data || data.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 6;
        td.className = 'empty-state';
        td.style.cssText = 'text-align: center; padding: 20px;';
        td.textContent = 'No kill data recorded yet.';
        tr.appendChild(td);
        tbody.replaceChildren(tr);
        return;
    }
    tbody.replaceChildren(...data.map(buildKillRow));
    QuickSearch.apply('kills-search');
}

// --- Shared cell builders for the HQ / Profession leaderboards ---
function buildLastUpdatedCell(iso) {
    const td = document.createElement('td');
    td.style.color = 'var(--color-text-muted)';
    td.style.fontSize = '0.9em';
    if (iso) {
        const d = new Date(iso.replace(' ', 'T') + 'Z');
        td.textContent = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
    } else {
        td.textContent = '—';
    }
    return td;
}

function buildNameRankCells(name, rank) {
    const tdName = document.createElement('td');
    const strong = document.createElement('strong');
    strong.textContent = name;
    tdName.appendChild(strong);

    const tdRank = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = `member-rank rank-${rank}`;
    badge.textContent = rank;
    tdRank.appendChild(badge);
    return [tdName, tdRank];
}

// --- Tab: HQ Level ---
async function loadHQData() {
    try {
        const response = await fetch(`${API_BASE}/hq-level-history`);
        if (!response.ok) throw new Error('Failed to load HQ data');
        rawHQData = await response.json() || [];
        renderHQTable(rawHQData);
    } catch (error) {
        console.error('Error:', error);
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 6;
        td.className = 'error';
        td.textContent = 'Failed to load data.';
        tr.appendChild(td);
        document.getElementById('hq-tbody').replaceChildren(tr);
    }
}

function buildHQRow(s) {
    const tr = document.createElement('tr');
    tr.dataset.search = s.member_name + ' ' + s.member_rank;
    const [tdName, tdRank] = buildNameRankCells(s.member_name, s.member_rank);

    const tdLevel = document.createElement('td');
    tdLevel.style.fontWeight = '600';
    tdLevel.textContent = formatNumber(s.current_hq_level);

    const td7d = document.createElement('td');
    td7d.appendChild(formatKillDelta(s.delta_7d));
    const td30d = document.createElement('td');
    td30d.appendChild(formatKillDelta(s.delta_30d));

    tr.append(tdName, tdRank, tdLevel, td7d, td30d, buildLastUpdatedCell(s.last_recorded_at));
    return tr;
}

function renderHQTable(data) {
    const tbody = document.getElementById('hq-tbody');
    if (!data || data.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 6;
        td.className = 'empty-state';
        td.style.cssText = 'text-align: center; padding: 20px;';
        td.textContent = 'No HQ level data recorded yet.';
        tr.appendChild(td);
        tbody.replaceChildren(tr);
        return;
    }
    tbody.replaceChildren(...data.map(buildHQRow));
    QuickSearch.apply('hq-search');
}

// --- Tab: Profession Level ---
async function loadProfessionData() {
    try {
        const response = await fetch(`${API_BASE}/profession-level-history`);
        if (!response.ok) throw new Error('Failed to load profession data');
        rawProfessionData = await response.json() || [];
        renderProfessionTable(rawProfessionData);
    } catch (error) {
        console.error('Error:', error);
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 7;
        td.className = 'error';
        td.textContent = 'Failed to load data.';
        tr.appendChild(td);
        document.getElementById('profession-tbody').replaceChildren(tr);
    }
}

function buildProfessionRow(s) {
    const tr = document.createElement('tr');
    tr.dataset.search = s.member_name + ' ' + s.member_rank;
    const [tdName, tdRank] = buildNameRankCells(s.member_name, s.member_rank);

    const tdProf = document.createElement('td');
    tdProf.textContent = s.profession || '—';

    const tdLevel = document.createElement('td');
    tdLevel.style.fontWeight = '600';
    tdLevel.textContent = formatNumber(s.current_profession_level);

    const td7d = document.createElement('td');
    td7d.appendChild(formatKillDelta(s.delta_7d));
    const td30d = document.createElement('td');
    td30d.appendChild(formatKillDelta(s.delta_30d));

    tr.append(tdName, tdRank, tdProf, tdLevel, td7d, td30d, buildLastUpdatedCell(s.last_recorded_at));
    return tr;
}

function renderProfessionTable(data) {
    const tbody = document.getElementById('profession-tbody');
    if (!data || data.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 7;
        td.className = 'empty-state';
        td.style.cssText = 'text-align: center; padding: 20px;';
        td.textContent = 'No profession level data recorded yet.';
        tr.appendChild(td);
        tbody.replaceChildren(tr);
        return;
    }
    tbody.replaceChildren(...data.map(buildProfessionRow));
    QuickSearch.apply('profession-search');
}

// --- Utility Formatters ---
function formatNumber(num) {
    return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

function formatGrowth(num) {
    const span = document.createElement('span');
    if (num > 0) {
        span.className = 'positive-growth';
        span.textContent = '+' + formatCompact(num);
    } else if (num < 0) {
        span.style.color = 'var(--color-danger)';
        span.textContent = formatCompact(num);
    } else {
        span.className = 'neutral-growth';
        span.textContent = '-';
    }
    return span;
}

// --- Tab 1: Growth Analytics ---

// The two growth windows, in days: chosen on the page, remembered per browser (UserPrefs),
// clamped to what the server accepts (1–365).
let growthWindows = { short: 7, long: 30 };

function clampWindow(v, def) {
    const n = parseInt(v, 10);
    if (!Number.isFinite(n)) return def;
    return Math.min(365, Math.max(1, n));
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

function initGrowthWindows() {
    const s = document.getElementById('growth-window-short');
    const l = document.getElementById('growth-window-long');
    if (!s || !l) return;
    growthWindows = {
        short: clampWindow(UserPrefs.get('growthWindowShort'), 7),
        long: clampWindow(UserPrefs.get('growthWindowLong'), 30),
    };
    let sCtl, lCtl;
    const apply = () => {
        growthWindows = { short: clampWindow(s.value, 7), long: clampWindow(l.value, 30) };
        sCtl.show(growthWindows.short);
        lCtl.show(growthWindows.long);
        UserPrefs.set('growthWindowShort', String(growthWindows.short));
        UserPrefs.set('growthWindowLong', String(growthWindows.long));
        loadGrowthData();
    };
    sCtl = windowControl(s, growthWindows.short, apply);
    lCtl = windowControl(l, growthWindows.long, apply);
}

function setGrowthHeaders(shortDays, longDays) {
    const set = (id, text) => { const th = document.getElementById(id); if (th) th.textContent = text; };
    set('growth-short-header', `${shortDays}-Day Growth`);
    set('growth-long-header', `${longDays}-Day Growth`);
    set('hero-growth-short-header', `Hero Power ${shortDays}-Day`);
    set('hero-growth-long-header', `Hero Power ${longDays}-Day`);
}

async function loadGrowthData() {
    try {
        const response = await fetch(`${API_BASE}/rankings?short=${growthWindows.short}&long=${growthWindows.long}`);
        if (!response.ok) throw new Error('Failed to load growth data');

        const data = await response.json();
        rawGrowthData = data.growth_data || [];
        growthWindows = { short: data.short_days || growthWindows.short, long: data.long_days || growthWindows.long };
        setGrowthHeaders(growthWindows.short, growthWindows.long);

        renderCompositionCharts();
        renderGrowthTable(rawGrowthData);
    } catch (error) {
        console.error('Error:', error);
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 8;
        td.className = 'error';
        td.textContent = 'Failed to load data.';
        tr.appendChild(td);
        document.getElementById('growth-tbody').replaceChildren(tr);
    }
}

function renderCompositionCharts() {
    // 1. Troop Distribution (Doughnut Chart)
    const troops = {};
    rawGrowthData.forEach(m => {
        if (m.troop_level > 0) {
            const label = `T${m.troop_level}`;
            troops[label] = (troops[label] || 0) + 1;
        }
    });

    const p = getChartPalette();
    Chart.defaults.color = p.text;

    const troopCanvas = document.getElementById('troopChart').getContext('2d');
    if (charts.troop) charts.troop.destroy();

    charts.troop = new Chart(troopCanvas, {
        type: 'doughnut',
        data: {
            labels: Object.keys(troops),
            datasets: [{
                data: Object.values(troops),
                backgroundColor: p.series,
                borderWidth: 2,
                borderColor: p.card,
                hoverOffset: 4
            }]
        },
        options: {
            responsive: true,
            cutout: '65%',
            plugins: {
                legend: { position: 'bottom', labels: { padding: 20, usePointStyle: true } },
                tooltip: {
                    callbacks: {
                        label: function(context) {
                            let label = context.label || '';
                            if (label) label += ': ';
                            label += context.parsed + ' Commanders';
                            return label;
                        }
                    }
                }
            }
        }
    });

    // 2. Squad Distribution (Pie Chart)
    const squads = { 'Tank': 0, 'Aircraft': 0, 'Missile': 0, 'Unknown': 0 };
    rawGrowthData.forEach(m => {
        const type = m.squad_type || 'Unknown';
        if (squads[type] !== undefined) squads[type]++;
    });

    // Remove 'Unknown' if it's 0 to keep the chart clean
    if (squads['Unknown'] === 0) delete squads['Unknown'];

    const squadCanvas = document.getElementById('squadChart').getContext('2d');
    if (charts.squad) charts.squad.destroy();

    charts.squad = new Chart(squadCanvas, {
        type: 'pie',
        data: {
            labels: Object.keys(squads),
            datasets: [{
                data: Object.values(squads),
                backgroundColor: [p.warning, p.info, p.danger, p.muted],
                borderWidth: 2,
                borderColor: p.card,
                hoverOffset: 4
            }]
        },
        options: {
            responsive: true,
            plugins: {
                legend: { position: 'bottom', labels: { padding: 20, usePointStyle: true } },
                tooltip: {
                    callbacks: {
                        label: function(context) {
                            return ` ${context.label}: ${context.parsed} Commanders`;
                        }
                    }
                }
            }
        }
    });
}

// A growth cell: null means no reading that old, so there is no figure (never 0).
function growthCell(value, days) {
    const td = document.createElement('td');
    if (value == null) {
        td.className = 'neutral-growth';
        td.textContent = '—';
        td.title = `No reading ${days} days old`;
        td.dataset.exportValue = '';
        return td;
    }
    td.appendChild(formatGrowth(value));
    td.dataset.exportValue = String(value);
    return td;
}

function buildGrowthRow(m) {
    const tr = document.createElement('tr');
    tr.dataset.search = m.name + ' ' + m.rank;

    const tdName = document.createElement('td');
    const strong = document.createElement('strong');
    strong.textContent = m.name;
    tdName.appendChild(strong);

    const tdRank = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = `member-rank rank-${m.rank}`;
    badge.textContent = m.rank;
    tdRank.appendChild(badge);

    const tdPower = document.createElement('td');
    tdPower.style.fontWeight = '600';
    tdPower.style.color = 'var(--color-text)';
    tdPower.textContent = m.current_power != null ? formatNumber(m.current_power) : '—';

    const tdShort = growthCell(m.growth_short, growthWindows.short);
    const tdLong = growthCell(m.growth_long, growthWindows.long);

    const tdHero = document.createElement('td');
    tdHero.style.fontWeight = '600';
    tdHero.style.color = 'var(--color-purple)';
    if (m.current_hero_power > 0) {
        tdHero.textContent = formatNumber(m.current_hero_power);
    } else {
        tdHero.style.color = 'var(--color-text-muted)';
        tdHero.textContent = '—';
    }

    const tdHeroShort = growthCell(m.hero_growth_short, growthWindows.short);
    const tdHeroLong = growthCell(m.hero_growth_long, growthWindows.long);

    tr.append(tdName, tdRank, tdPower, tdShort, tdLong, tdHero, tdHeroShort, tdHeroLong);
    return tr;
}

function renderGrowthTable(data) {
    const tbody = document.getElementById('growth-tbody');
    if (data.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 8;
        td.className = 'empty-state';
        td.style.cssText = 'text-align: center; padding: 20px;';
        td.textContent = 'No active commanders found.';
        tr.appendChild(td);
        tbody.replaceChildren(tr);
        return;
    }
    tbody.replaceChildren(...data.map(buildGrowthRow));
    QuickSearch.apply('growth-search');
}

// Hide mode rather than re-render: these are data-export-csv tables, so hiding
// lets the export scope toggle offer "filtered only" vs the full table. It also
// keeps the row set stable for anything else reading the tbody.
// The leaderboard position in column 0 is the member's real standing, so gaps
// under a filter (#1, #7, #12) are correct and must not be renumbered — another
// reason this hides rather than re-renders.
QuickSearch.attach({
    input: 'vs-search', container: 'vs-tbody', rows: 'tr',
    emptyText: 'No commanders match your search.',
});

QuickSearch.attach({
    input: 'growth-search', container: 'growth-tbody', rows: 'tr',
    emptyText: 'No commanders match your search.',
});

// --- Tab 2: VS Duel Activity ---
async function loadVSData() {
    try {
        const response = await fetch(`${API_BASE}/vs-points`);
        if (!response.ok) throw new Error('Failed to load VS data');

        allVsData = await response.json() || [];

        const weeks = [...new Set(allVsData.map(v => v.week_date))].sort((a, b) => b.localeCompare(a));
        const select = document.getElementById('vs-week-select');

        if (weeks.length === 0) {
            const opt = document.createElement('option');
            opt.value = '';
            opt.textContent = 'No VS Data Available';
            select.replaceChildren(opt);

            const tr = document.createElement('tr');
            const td = document.createElement('td');
            td.colSpan = 9;
            td.className = 'empty-state';
            td.style.cssText = 'text-align: center; padding: 20px;';
            td.textContent = 'No VS data recorded yet.';
            tr.appendChild(td);
            document.getElementById('vs-tbody').replaceChildren(tr);
            return;
        }

        select.replaceChildren(...weeks.map(w => {
            const opt = document.createElement('option');
            opt.value = w;
            opt.textContent = `Week of ${w}`;
            return opt;
        }));
        select.addEventListener('change', (e) => renderVSWeek(e.target.value));

        renderVSWeek(weeks[0]);

    } catch (error) {
        console.error('Error:', error);
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 9;
        td.className = 'error';
        td.style.cssText = 'text-align: center; color: var(--color-danger); padding: 20px;';
        td.textContent = 'Failed to load data.';
        tr.appendChild(td);
        document.getElementById('vs-tbody').replaceChildren(tr);
    }
}

function buildVSRow(v, idx) {
    const tr = document.createElement('tr');
    tr.dataset.search = v.member_name;

    const tdIdx = document.createElement('td');
    tdIdx.style.cssText = 'color: var(--color-text-muted); font-size: 0.9em;';
    tdIdx.textContent = `#${idx + 1}`;

    const tdName = document.createElement('td');
    const strong = document.createElement('strong');
    strong.textContent = v.member_name;
    tdName.appendChild(strong);

    const days = [v.monday, v.tuesday, v.wednesday, v.thursday, v.friday, v.saturday];
    const dayTds = days.map(d => {
        const td = document.createElement('td');
        td.textContent = formatCompact(d);
        td.dataset.exportValue = d != null ? String(d) : '';   // raw for export
        return td;
    });

    const tdTotal = document.createElement('td');
    tdTotal.className = 'vs-total-col';
    tdTotal.textContent = formatNumber(v.total);

    tr.append(tdIdx, tdName, ...dayTds, tdTotal);
    return tr;
}

function renderVSWeek(weekDate) {
    currentVsWeek = weekDate;

    let weekData = allVsData.filter(v => v.week_date === weekDate);

    // Calculate totals and sort by Highest Total
    weekData = weekData.map(v => {
        v.total = v.monday + v.tuesday + v.wednesday + v.thursday + v.friday + v.saturday;
        return v;
    }).sort((a, b) => b.total - a.total);

    const vp = getChartPalette();
    Chart.defaults.color = vp.text;

    // 1. Render Massive Stacked Bar Chart
    const vsCanvas = document.getElementById('vsChart').getContext('2d');
    if (charts.vs) charts.vs.destroy();

    // Cap at top 20 performers so the chart labels don't become unreadable
    const chartData = weekData.slice(0, 20);

    charts.vs = new Chart(vsCanvas, {
        type: 'bar',
        data: {
            labels: chartData.map(v => v.member_name),
            datasets: [
                { label: 'Mon (Radar)', data: chartData.map(v => v.monday), backgroundColor: vp.warning, borderRadius: 2 },
                { label: 'Tue (Base)', data: chartData.map(v => v.tuesday), backgroundColor: vp.success, borderRadius: 2 },
                { label: 'Wed (Tech)', data: chartData.map(v => v.wednesday), backgroundColor: vp.info, borderRadius: 2 },
                { label: 'Thu (Hero)', data: chartData.map(v => v.thursday), backgroundColor: vp.accent, borderRadius: 2 },
                { label: 'Fri (Train)', data: chartData.map(v => v.friday), backgroundColor: vp.purple, borderRadius: 2 },
                { label: 'Sat (Kill)', data: chartData.map(v => v.saturday), backgroundColor: vp.danger, borderRadius: { topLeft: 4, topRight: 4 } }
            ]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: {
                mode: 'index', // Hovering over a bar shows all days for that person
                intersect: false,
            },
            scales: {
                x: {
                    stacked: true,
                    grid: { display: false } // Cleaner look on the x-axis
                },
                y: {
                    stacked: true,
                    title: { display: true, text: 'VS Points', font: { weight: 'bold' } },
                    ticks: {
                        callback: function(value) { return formatCompact(value); }
                    }
                }
            },
            plugins: {
                legend: { position: 'top', labels: { usePointStyle: true, boxWidth: 8 } },
                tooltip: {
                    callbacks: {
                        label: function(context) {
                            return ` ${context.dataset.label}: ${formatNumber(context.raw)}`;
                        },
                        footer: function(tooltipItems) {
                            let total = 0;
                            tooltipItems.forEach(function(tooltipItem) {
                                total += tooltipItem.raw;
                            });
                            return `Total: ${formatNumber(total)}`;
                        }
                    }
                }
            }
        }
    });

    // 2. Render Table
    document.getElementById('vs-tbody').replaceChildren(...weekData.map(buildVSRow));
    QuickSearch.apply('vs-search');
}

QuickSearch.attach({
    input: 'kills-search', container: 'kills-tbody', rows: 'tr',
    emptyText: 'No commanders match your search.',
});

QuickSearch.attach({
    input: 'hq-search', container: 'hq-tbody', rows: 'tr',
    emptyText: 'No commanders match your search.',
});

QuickSearch.attach({
    input: 'profession-search', container: 'profession-tbody', rows: 'tr',
    emptyText: 'No commanders match your search.',
});

// --- Donations tab ---

const DAY_STATUS_LABEL = { final: 'final', so_far: 'so far', blank: '—' };

function donationsStatusText(m, weekOver) {
    if (m.below_weekly) return 'below the weekly minimum';
    if (m.not_read_to_end) return 'not read to the end';
    if (m.joined_mid_week) return 'joined this week';
    if (m.total.status === 'so_far' && !weekOver) return 'so far';
    return DAY_STATUS_LABEL[m.total.status] || m.total.status;
}

function donationsMinimumsText(min) {
    const part = (label, v) => v > 0 ? `${label} ${DonationMarks.amount(v)}` : `${label} off`;
    const src = min.source === 'current_setting' ? ' (no record for this week: today\'s settings)' : '';
    return `Minimums for the week: ${part('weekly', min.weekly)}, ${part('daily', min.daily)}` +
        (min.daily > 0 ? `, flag after ${min.flag_days} day${min.flag_days !== 1 ? 's' : ''} below` : '') + src + '.';
}

// Entering by hand (private-docs 290): the day cells become inputs for officers with
// manage_members. A cell holds what was entered (blank when nothing was); the screenshots'
// figure is its placeholder. Save sends only the changed cells, each with the value it was
// loaded with, so an entry changed underneath is reported rather than overwritten.
let donationsData = null;
let donationsEditing = false;

function donationInputCell(m, day, date) {
    const td = document.createElement('td');
    const input = document.createElement('input');
    input.type = 'text';
    input.inputMode = 'numeric';
    input.className = 'form-input donations-input';
    input.setAttribute('aria-label', `${m.name}, ${date}`);
    input.dataset.memberId = m.id;
    input.dataset.date = date;
    input.dataset.was = day.entered ? String(day.amount) : '';
    input.value = input.dataset.was;
    if (!day.entered && day.status !== 'blank') input.placeholder = DonationMarks.amount(day.amount);
    td.appendChild(input);
    return td;
}

function buildDonationsRow(m, idx, weekOver) {
    const tr = document.createElement('tr');
    tr.dataset.search = m.name;
    // The commander is the first column, so it is the one the narrow layout pins; the
    // position is a muted prefix in the same cell, and the export carries the bare name.
    const tdName = document.createElement('td');
    tdName.dataset.exportText = m.name;
    if (m.total.status !== 'blank') {
        tdName.appendChild(Object.assign(document.createElement('span'), { className: 'donations-pos', textContent: `${idx + 1}. ` }));
    }
    const strong = document.createElement('strong');
    strong.textContent = m.name;
    tdName.appendChild(strong);
    const days = donationsEditing
        ? m.days.map((d, i) => donationInputCell(m, d, donationsData.days[i]))
        : m.days.map(DonationMarks.dayCell);
    const tdTotal = DonationMarks.totalCell(m.total, m);
    const tdStatus = document.createElement('td');
    tdStatus.textContent = donationsStatusText(m, weekOver);
    if (m.below_daily) {
        const badge = Object.assign(document.createElement('span'), { className: 'don-badge', textContent: `${m.days_below_daily} days below daily` });
        tdStatus.appendChild(badge);
    }
    tr.append(tdName, ...days, tdTotal, tdStatus);
    return tr;
}

async function loadDonations(week) {
    const tbody = document.getElementById('donations-tbody');
    if (!tbody) return;
    const select = document.getElementById('donations-week-select');
    try {
        const res = await fetch(`${API_BASE}/donations` + (week ? `?week=${encodeURIComponent(week)}` : ''));
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        donationsData = data;
        select.replaceChildren(...data.weeks.map((w, i) => {
            const opt = document.createElement('option');
            opt.value = w;
            opt.textContent = i === 0 ? `Week of ${w} (this week)` : `Week of ${w}`;
            return opt;
        }));
        select.value = data.week_date;
        document.getElementById('donations-minimums').textContent = donationsMinimumsText(data.minimums);
        if (!data.members.length) {
            const tr = document.createElement('tr');
            const td = Object.assign(document.createElement('td'), { colSpan: 10, className: 'empty-state', textContent: 'No active members.' });
            tr.appendChild(td);
            tbody.replaceChildren(tr);
        } else {
            tbody.replaceChildren(...data.members.map((m, i) => buildDonationsRow(m, i, data.week_over)));
        }
        QuickSearch.apply('donations-search');
    } catch (err) {
        console.error('loadDonations:', err);
        const tr = document.createElement('tr');
        const td = Object.assign(document.createElement('td'), { colSpan: 10, className: 'error', textContent: 'Failed to load donations.' });
        td.style.cssText = 'text-align: center; color: var(--color-danger); padding: 20px;';
        tr.appendChild(td);
        tbody.replaceChildren(tr);
    }
}

function setDonationsEditing(on) {
    donationsEditing = on;
    const show = (id, v) => { const e = document.getElementById(id); if (e) e.style.display = v ? '' : 'none'; };
    show('donations-edit-btn', !on);
    show('donations-import-btn', !on);
    show('donations-save-btn', on);
    show('donations-cancel-btn', on);
    show('donations-edit-hint', on);
    document.getElementById('donations-week-select').disabled = on;
    if (donationsData) loadDonations(donationsData.week_date);
}

function parseAmountInput(v) {
    const t = v.replace(/[,\s]/g, '');
    if (t === '') return { ok: true, value: null };
    return /^\d+$/.test(t) ? { ok: true, value: Number(t) } : { ok: false };
}

async function putDonationEntries(source, cells) {
    const res = await fetch(`${API_BASE}/donations/entries`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source, cells }),
    });
    if (!res.ok) throw new Error((await res.text()).trim() || ('HTTP ' + res.status));
    return res.json();
}

function reportEntryResult(r) {
    const parts = [`${r.saved} saved`];
    if (r.cleared) parts.push(`${r.cleared} cleared`);
    if (r.conflicts.length) parts.push(`${r.conflicts.length} changed by someone else meanwhile — not saved, reloaded`);
    if (r.errors.length) parts.push(`${r.errors.length} refused: ${r.errors.slice(0, 3).join('; ')}`);
    showToast('Donations: ' + parts.join(', ') + '.', r.conflicts.length || r.errors.length ? 'error' : 'success', 6000);
}

async function saveDonationEntries() {
    const cells = [];
    let bad = 0;
    document.querySelectorAll('#donations-tbody .donations-input').forEach(input => {
        const p = parseAmountInput(input.value);
        if (!p.ok) { bad++; setFieldError(input, 'A whole number'); return; }
        clearFieldError(input);
        const was = input.dataset.was === '' ? null : Number(input.dataset.was);
        if (p.value !== was) cells.push({ member_id: Number(input.dataset.memberId), game_date: input.dataset.date, amount: p.value, was });
    });
    if (bad) { showToast(`${bad} cell${bad !== 1 ? 's are' : ' is'} not a whole number.`, 'error'); return; }
    if (!cells.length) { setDonationsEditing(false); return; }
    try {
        reportEntryResult(await putDonationEntries('manual', cells));
        setDonationsEditing(false);
    } catch (e) {
        showToast('Could not save donations: ' + e.message, 'error');
    }
}

// --- CSV import: preview, map names, confirm -------------------------------------------

let donationsImportRows = [];

async function previewDonationsImport(file) {
    const fd = new FormData();
    fd.append('csv_file', file);
    fd.append('week', document.getElementById('donations-week-select').value);
    let data;
    try {
        const res = await fetch(`${API_BASE}/donations/import/preview`, { method: 'POST', body: fd });
        if (!res.ok) throw new Error((await res.text()).trim());
        data = await res.json();
    } catch (e) {
        showToast(e.message || 'Could not read the file.', 'error', 6000);
        return;
    }
    donationsImportRows = data.rows;
    const active = (donationsData ? donationsData.members : []).slice().sort((a, b) => a.name.localeCompare(b.name));
    const tbody = document.getElementById('donations-import-tbody');
    tbody.replaceChildren(...data.rows.map((row, i) => {
        const tr = document.createElement('tr');
        const tdFile = Object.assign(document.createElement('td'), { textContent: row.original_name });
        const tdMember = document.createElement('td');
        if (row.matched_member) {
            tdMember.textContent = row.matched_member.name;
        } else {
            const sel = document.createElement('select');
            sel.className = 'form-input';
            sel.dataset.row = i;
            sel.appendChild(Object.assign(document.createElement('option'), { value: '', textContent: '— skip —' }));
            active.forEach(m => sel.appendChild(Object.assign(document.createElement('option'), { value: m.id, textContent: m.name })));
            tdMember.appendChild(sel);
        }
        const dates = row.cells.map(c => c.game_date);
        const tdDays = Object.assign(document.createElement('td'), {
            textContent: dates.length === 1 ? dates[0] : `${dates.length} days, ${dates[0]} – ${dates[dates.length - 1]}`,
        });
        const total = row.cells.reduce((s, c) => s + (c.amount || 0), 0);
        const tdTotal = Object.assign(document.createElement('td'), { textContent: DonationMarks.amount(total) });
        tr.append(tdFile, tdMember, tdDays, tdTotal);
        return tr;
    }));
    const errBox = document.getElementById('donations-import-errors');
    errBox.textContent = data.errors.length ? `Skipped: ${data.errors.join('; ')}` : '';
    errBox.style.display = data.errors.length ? '' : 'none';
    document.getElementById('donations-import-modal').style.display = 'flex';
}

async function confirmDonationsImport() {
    const cells = [];
    donationsImportRows.forEach((row, i) => {
        let id = row.matched_member ? row.matched_member.id : null;
        if (!id) {
            const sel = document.querySelector(`#donations-import-tbody select[data-row="${i}"]`);
            id = sel && sel.value ? Number(sel.value) : null;
        }
        if (!id) return;
        row.cells.forEach(c => cells.push({ member_id: id, game_date: c.game_date, amount: c.amount }));
    });
    document.getElementById('donations-import-modal').style.display = '';
    if (!cells.length) { showToast('Nothing to import.', 'info'); return; }
    try {
        reportEntryResult(await putDonationEntries('csv', cells));
        if (donationsData) loadDonations(donationsData.week_date);
    } catch (e) {
        showToast('Could not import: ' + e.message, 'error');
    }
}

function wireDonationsEditing() {
    const edit = document.getElementById('donations-edit-btn');
    if (!edit) return; // read-only: no manage_members
    edit.addEventListener('click', () => setDonationsEditing(true));
    document.getElementById('donations-cancel-btn').addEventListener('click', () => setDonationsEditing(false));
    document.getElementById('donations-save-btn').addEventListener('click', saveDonationEntries);
    const file = document.getElementById('donations-import-file');
    document.getElementById('donations-import-btn').addEventListener('click', () => file.click());
    file.addEventListener('change', () => { if (file.files[0]) previewDonationsImport(file.files[0]); file.value = ''; });
    document.getElementById('donations-import-cancel').addEventListener('click', () => {
        document.getElementById('donations-import-modal').style.display = '';
    });
    document.getElementById('donations-import-confirm').addEventListener('click', confirmDonationsImport);
}

QuickSearch.attach({
    input: 'donations-search', container: 'donations-tbody', rows: 'tr',
    emptyText: 'No commanders match your search.',
});

// --- Theme adaptation ---
window.addEventListener('themechange', () => {
    const p = getChartPalette();
    Chart.defaults.color = p.text;
    if (rawGrowthData.length) renderCompositionCharts();
    if (currentVsWeek) renderVSWeek(currentVsWeek);
});

// The VS chart's y-axis ticks are drawn on a canvas, which setNumberSuffixes' text swap can't
// reach — redraw them when the user switches between B and G.
window.addEventListener('numberstylechange', () => {
    if (currentVsWeek) renderVSWeek(currentVsWeek);
});

// --- Initialization ---
document.addEventListener('DOMContentLoaded', () => {
    if (document.getElementById('tab-growth')) {
        // tabs.js, with #tab deep links (#vs, #kills, …). Every tab loads eagerly below.
        Tabs.init({ hash: true, defaultTab: 'growth' });
        initGrowthWindows();
        loadGrowthData();
        loadVSData();
        loadKillData();
        loadHQData();
        loadProfessionData();
        loadDonations();
        document.getElementById('donations-legend')?.appendChild(DonationMarks.legend());
        document.getElementById('donations-week-select')?.addEventListener('change', e => loadDonations(e.target.value));
        wireDonationsEditing();
    }
});
