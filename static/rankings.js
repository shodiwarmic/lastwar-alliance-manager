const API_BASE = '/api';

let charts = {
    troop: null,
    squad: null,
    vs: null
};

let rawGrowthData = [];
let allVsData = []; // Holds all weeks to populate the dropdown

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
    if (num > 0) return `<span class="positive-growth">+${formatShortPower(num)}</span>`;
    if (num < 0) return `<span style="color: #dc3545;">${formatShortPower(num)}</span>`;
    return `<span class="neutral-growth">-</span>`;
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
        document.getElementById('growth-tbody').innerHTML = '<tr><td colspan="5" class="error">Failed to load data.</td></tr>';
    }
}

function renderCompositionCharts() {
    // 1. Troop Distribution
    const troops = {};
    rawGrowthData.forEach(m => {
        if (m.troop_level > 0) {
            const label = `T${m.troop_level}`;
            troops[label] = (troops[label] || 0) + 1;
        }
    });

    const troopCanvas = document.getElementById('troopChart');
    if (charts.troop) charts.troop.destroy();
    charts.troop = new Chart(troopCanvas, {
        type: 'pie',
        data: {
            labels: Object.keys(troops),
            datasets: [{
                data: Object.values(troops),
                backgroundColor: ['#667eea', '#764ba2', '#4facfe', '#00f2fe', '#f093fb', '#f5576c']
            }]
        },
        options: { responsive: true, plugins: { legend: { position: 'right' } } }
    });

    // 2. Squad Distribution
    const squads = { 'Tank': 0, 'Aircraft': 0, 'Missile': 0, 'Unknown': 0 };
    rawGrowthData.forEach(m => {
        const type = m.squad_type || 'Unknown';
        if (squads[type] !== undefined) squads[type]++;
    });

    const squadCanvas = document.getElementById('squadChart');
    if (charts.squad) charts.squad.destroy();
    charts.squad = new Chart(squadCanvas, {
        type: 'doughnut',
        data: {
            labels: Object.keys(squads),
            datasets: [{
                data: Object.values(squads),
                backgroundColor: ['#f6ad55', '#63b3ed', '#fc8181', '#cbd5e0']
            }]
        },
        options: { responsive: true, plugins: { legend: { position: 'right' } } }
    });
}

function renderGrowthTable(data) {
    const tbody = document.getElementById('growth-tbody');
    if (data.length === 0) {
        tbody.innerHTML = '<tr><td colspan="5" class="empty">No active commanders found.</td></tr>';
        return;
    }

    tbody.innerHTML = data.map(m => `
        <tr>
            <td><strong>${m.name}</strong></td>
            <td><span class="rank-badge rank-${m.rank}">${m.rank}</span></td>
            <td style="font-weight: bold;">${formatNumber(m.current_power)}</td>
            <td>${formatGrowth(m.growth_7d)}</td>
            <td>${formatGrowth(m.growth_30d)}</td>
        </tr>
    `).join('');
}

document.getElementById('growth-search')?.addEventListener('input', (e) => {
    const term = e.target.value.toLowerCase();
    const filtered = rawGrowthData.filter(m => m.name.toLowerCase().includes(term));
    renderGrowthTable(filtered);
});

// --- Tab 2: VS Duel Activity ---
async function loadVSData() {
    try {
        // Fetch all weeks to populate the dropdown
        const response = await fetch(`${API_BASE}/vs-points`);
        if (!response.ok) throw new Error('Failed to load VS data');
        
        allVsData = await response.json() || [];
        
        // Extract unique weeks
        const weeks = [...new Set(allVsData.map(v => v.week_date))].sort((a, b) => b.localeCompare(a));
        
        const select = document.getElementById('vs-week-select');
        if (weeks.length === 0) {
            select.innerHTML = '<option value="">No VS Data Available</option>';
            document.getElementById('vs-tbody').innerHTML = '<tr><td colspan="9" class="empty">No VS data recorded yet.</td></tr>';
            return;
        }

        select.innerHTML = weeks.map(w => `<option value="${w}">Week of ${w}</option>`).join('');
        select.addEventListener('change', (e) => renderVSWeek(e.target.value));
        
        // Render the most recent week initially
        renderVSWeek(weeks[0]);

    } catch (error) {
        console.error('Error:', error);
        document.getElementById('vs-tbody').innerHTML = '<tr><td colspan="9" class="error">Failed to load data.</td></tr>';
    }
}

function renderVSWeek(weekDate) {
    // Filter data for the selected week
    let weekData = allVsData.filter(v => v.week_date === weekDate);
    
    // Calculate totals and sort by Highest Total
    weekData = weekData.map(v => {
        v.total = v.monday + v.tuesday + v.wednesday + v.thursday + v.friday + v.saturday;
        return v;
    }).sort((a, b) => b.total - a.total);

    // 1. Render Stacked Bar Chart
    const vsCanvas = document.getElementById('vsChart');
    if (charts.vs) charts.vs.destroy();
    
    // Take top 15 performers for the chart so it doesn't get squished
    const chartData = weekData.slice(0, 15);
    
    charts.vs = new Chart(vsCanvas, {
        type: 'bar',
        data: {
            labels: chartData.map(v => v.member_name),
            datasets: [
                { label: 'Mon (Radar)', data: chartData.map(v => v.monday), backgroundColor: '#f6ad55' },
                { label: 'Tue (Base)', data: chartData.map(v => v.tuesday), backgroundColor: '#68d391' },
                { label: 'Wed (Tech)', data: chartData.map(v => v.wednesday), backgroundColor: '#4fd1c5' },
                { label: 'Thu (Hero)', data: chartData.map(v => v.thursday), backgroundColor: '#63b3ed' },
                { label: 'Fri (Train)', data: chartData.map(v => v.friday), backgroundColor: '#b794f4' },
                { label: 'Sat (Kill)', data: chartData.map(v => v.saturday), backgroundColor: '#fc8181' }
            ]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            scales: {
                x: { stacked: true },
                y: { stacked: true, title: { display: true, text: 'Points' } }
            },
            plugins: { legend: { position: 'bottom' } }
        }
    });

    // 2. Render Table
    const tbody = document.getElementById('vs-tbody');
    tbody.innerHTML = weekData.map((v, idx) => `
        <tr>
            <td><strong>#${idx + 1}</strong></td>
            <td><strong>${v.member_name}</strong></td>
            <td>${formatShortPower(v.monday)}</td>
            <td>${formatShortPower(v.tuesday)}</td>
            <td>${formatShortPower(v.wednesday)}</td>
            <td>${formatShortPower(v.thursday)}</td>
            <td>${formatShortPower(v.friday)}</td>
            <td>${formatShortPower(v.saturday)}</td>
            <td class="vs-total-col">${formatNumber(v.total)}</td>
        </tr>
    `).join('');
}

// --- Initialization ---
document.addEventListener('DOMContentLoaded', () => {
    if (document.getElementById('tab-growth')) {
        loadGrowthData();
        loadVSData();
    }
});