const API_BASE = '/api';

// Global Chart instances so we can destroy/recreate them cleanly
let charts = {
    troop: null,
    squad: null,
    vs: null
};

let rawGrowthData = [];
let allVsData = [];

// --- Global Chart.js Styling ---
// This ensures the charts match your app's typography and adapt to light/dark themes
Chart.defaults.font.family = '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif';
Chart.defaults.color = getComputedStyle(document.documentElement).getPropertyValue('--text-muted').trim();
Chart.defaults.plugins.tooltip.backgroundColor = 'rgba(26, 32, 44, 0.9)';
Chart.defaults.plugins.tooltip.padding = 12;
Chart.defaults.plugins.tooltip.cornerRadius = 8;
Chart.defaults.plugins.tooltip.titleFont = { size: 14, weight: 'bold' };
Chart.defaults.plugins.tooltip.bodyFont = { size: 13 };

// --- Tab Management ---
function switchTab(tabId) {
    document.querySelectorAll('.tab-button').forEach(btn => btn.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(content => content.classList.remove('active'));

    document.getElementById(`tab-btn-${tabId}`).classList.add('active');
    document.getElementById(`tab-${tabId}`).classList.add('active');
}

// --- Utility Formatters ---
function formatNumber(num) {
    return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

function formatShortPower(power) {
    if (power >= 1000000) return (power / 1000000).toFixed(1) + 'M';
    if (power >= 1000) return (power / 1000).toFixed(0) + 'K';
    return power.toString();
}

function formatGrowth(num) {
    const span = document.createElement('span');
    if (num > 0) {
        span.className = 'positive-growth';
        span.textContent = '+' + formatShortPower(num);
    } else if (num < 0) {
        span.style.color = 'var(--color-danger)';
        span.textContent = formatShortPower(num);
    } else {
        span.className = 'neutral-growth';
        span.textContent = '-';
    }
    return span;
}

// --- Tab 1: Growth Analytics ---
async function loadGrowthData() {
    try {
        const response = await fetch(`${API_BASE}/rankings`);
        if (!response.ok) throw new Error('Failed to load growth data');

        const data = await response.json();
        rawGrowthData = data.growth_data || [];

        renderCompositionCharts();
        renderGrowthTable(rawGrowthData);
    } catch (error) {
        console.error('Error:', error);
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 5;
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

    const troopCanvas = document.getElementById('troopChart').getContext('2d');
    if (charts.troop) charts.troop.destroy();

    charts.troop = new Chart(troopCanvas, {
        type: 'doughnut',
        data: {
            labels: Object.keys(troops),
            datasets: [{
                data: Object.values(troops),
                backgroundColor: ['#667eea', '#764ba2', '#4facfe', '#00f2fe', '#f093fb', '#f5576c', '#ed8936', '#48bb78'],
                borderWidth: 2,
                borderColor: '#ffffff',
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
                backgroundColor: [
                    '#f6ad55', // Tank (Orange)
                    '#63b3ed', // Aircraft (Blue)
                    '#fc8181', // Missile (Red)
                    '#cbd5e0'  // Unknown (Gray)
                ],
                borderWidth: 2,
                borderColor: '#ffffff',
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

function buildGrowthRow(m) {
    const tr = document.createElement('tr');

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
    tdPower.style.color = 'var(--text-primary)';
    tdPower.textContent = formatNumber(m.current_power);

    const td7d = document.createElement('td');
    td7d.appendChild(formatGrowth(m.growth_7d));

    const td30d = document.createElement('td');
    td30d.appendChild(formatGrowth(m.growth_30d));

    tr.append(tdName, tdRank, tdPower, td7d, td30d);
    return tr;
}

function renderGrowthTable(data) {
    const tbody = document.getElementById('growth-tbody');
    if (data.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 5;
        td.className = 'empty';
        td.style.cssText = 'text-align: center; padding: 20px;';
        td.textContent = 'No active commanders found.';
        tr.appendChild(td);
        tbody.replaceChildren(tr);
        return;
    }
    tbody.replaceChildren(...data.map(buildGrowthRow));
}

document.getElementById('growth-search')?.addEventListener('input', (e) => {
    const term = e.target.value.toLowerCase();
    const filtered = rawGrowthData.filter(m => m.name.toLowerCase().includes(term));
    renderGrowthTable(filtered);
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
            td.className = 'empty';
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

    const tdIdx = document.createElement('td');
    tdIdx.style.cssText = 'color: var(--text-muted); font-size: 0.9em;';
    tdIdx.textContent = `#${idx + 1}`;

    const tdName = document.createElement('td');
    const strong = document.createElement('strong');
    strong.textContent = v.member_name;
    tdName.appendChild(strong);

    const days = [v.monday, v.tuesday, v.wednesday, v.thursday, v.friday, v.saturday];
    const dayTds = days.map(d => {
        const td = document.createElement('td');
        td.textContent = formatShortPower(d);
        return td;
    });

    const tdTotal = document.createElement('td');
    tdTotal.className = 'vs-total-col';
    tdTotal.textContent = formatNumber(v.total);

    tr.append(tdIdx, tdName, ...dayTds, tdTotal);
    return tr;
}

function renderVSWeek(weekDate) {
    let weekData = allVsData.filter(v => v.week_date === weekDate);

    // Calculate totals and sort by Highest Total
    weekData = weekData.map(v => {
        v.total = v.monday + v.tuesday + v.wednesday + v.thursday + v.friday + v.saturday;
        return v;
    }).sort((a, b) => b.total - a.total);

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
                { label: 'Mon (Radar)', data: chartData.map(v => v.monday), backgroundColor: '#f6ad55', borderRadius: 2 },
                { label: 'Tue (Base)', data: chartData.map(v => v.tuesday), backgroundColor: '#68d391', borderRadius: 2 },
                { label: 'Wed (Tech)', data: chartData.map(v => v.wednesday), backgroundColor: '#4fd1c5', borderRadius: 2 },
                { label: 'Thu (Hero)', data: chartData.map(v => v.thursday), backgroundColor: '#63b3ed', borderRadius: 2 },
                { label: 'Fri (Train)', data: chartData.map(v => v.friday), backgroundColor: '#b794f4', borderRadius: 2 },
                { label: 'Sat (Kill)', data: chartData.map(v => v.saturday), backgroundColor: '#fc8181', borderRadius: { topLeft: 4, topRight: 4 } }
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
                        callback: function(value) { return formatShortPower(value); }
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
}

// --- Initialization ---
document.addEventListener('DOMContentLoaded', () => {
    if (document.getElementById('tab-growth')) {
        loadGrowthData();
        loadVSData();
    }
});
