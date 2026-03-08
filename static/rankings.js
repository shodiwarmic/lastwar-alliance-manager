// static/rankings.js
const API_BASE = '/api';
const RANKINGS_URL = `${API_BASE}/rankings`;

let currentData = null;
let filteredRankings = null;
let memberTimelineCharts = [];
let charts = {
    score: null,
    conductor: null,
    pointsBreakdown: null
};

// Format date to dd/mm/yyyy
function formatDate(dateStr) {
    if (!dateStr) return '-';
    const parts = dateStr.split('-');
    return `${parts[2]}/${parts[1]}/${parts[0]}`;
}

// Load rankings
async function loadRankings() {
    try {
        const response = await fetch(RANKINGS_URL);
        if (!response.ok) throw new Error('Failed to load rankings');
        
        currentData = await response.json();
        filteredRankings = currentData.rankings;
        
        displaySystemInfo(currentData.settings, currentData.average_conductor_count);
        displayCharts(currentData);
        displayRankings(filteredRankings);
        
        const avgCountEl = document.getElementById('avg-count');
        if (avgCountEl) {
            avgCountEl.textContent = currentData.average_conductor_count.toFixed(2);
        }
    } catch (error) {
        console.error('Error loading rankings:', error);
        const rankingsList = document.getElementById('rankings-list');
        if (rankingsList) {
            rankingsList.innerHTML = '<p class="error">Failed to load rankings. Please try again.</p>';
        }
    }
}

// Display system info
function displaySystemInfo(settings, avgCount) {
    const systemInfoEl = document.getElementById('system-info');
    if (!systemInfoEl) return;
    
    const html = `
        <div class="system-info-grid">
            <div class="system-info-item">
                <span class="info-label">🥇 1st Place Award:</span>
                <span class="info-value">+${settings.award_first_points} pts</span>
            </div>
            <div class="system-info-item">
                <span class="info-label">🥈 2nd Place Award:</span>
                <span class="info-value">+${settings.award_second_points} pts</span>
            </div>
            <div class="system-info-item">
                <span class="info-label">🥉 3rd Place Award:</span>
                <span class="info-value">+${settings.award_third_points} pts</span>
            </div>
            <div class="system-info-item">
                <span class="info-label">⭐ Recommendations:</span>
                <span class="info-value">5*√n pts (non-linear scaling)</span>
            </div>
            <div class="system-info-item">
                <span class="info-label">🏅 R4/R5 Rank Boost:</span>
                <span class="info-value">${settings.r4r5_rank_boost} × 2^(days/7) pts (exponential)</span>
            </div>
            <div class="system-info-item">
                <span class="info-label">🎯 First Time Conductor Boost:</span>
                <span class="info-value">+${settings.first_time_conductor_boost} pts (if never been conductor)</span>
            </div>
            <div class="system-info-item">
                <span class="info-label">⏱️ Recent Conductor Penalty:</span>
                <span class="info-value">-${settings.recent_conductor_penalty_days} pts max (based on days)</span>
            </div>
            <div class="system-info-item">
                <span class="info-label">📊 Above Average Penalty:</span>
                <span class="info-value">-${settings.above_average_conductor_penalty} pts</span>
            </div>
        </div>
        <p class="system-note">
            <strong>Note:</strong> Awards and recommendations stack across multiple weeks until you're assigned as conductor/backup, then they expire. 
            Average conductor count: <strong>${avgCount.toFixed(2)}</strong> times.
            <br><strong>Recommendation Formula:</strong> 5*√n points per day (1 rec = 5pts, 4 recs = 10pts, 9 recs = 15pts, 16 recs = 20pts)
            <br><strong>R4/R5 Boost:</strong> Base × 2^(days/7) - doubles every week (Day 0: 1×, Day 7: 2×, Day 14: 4×, Day 21: 8×)
        </p>
    `;
    systemInfoEl.innerHTML = html;
}

// Display charts
function displayCharts(data) {
    const rankings = data.rankings;
    
    // Destroy existing charts
    Object.values(charts).forEach(chart => {
        if (chart) chart.destroy();
    });
    
    // 1. Score Distribution Chart
    const scoreLabels = rankings.map((r, i) => `#${i + 1} ${r.member.name}`);
    const scoreData = rankings.map(r => r.total_score);
    
    const scoreCanvas = document.getElementById('scoreChart');
    if (scoreCanvas) {
        const existingScoreChart = Chart.getChart(scoreCanvas);
        if (existingScoreChart) existingScoreChart.destroy();
        
        const scoreCtx = scoreCanvas.getContext('2d');
        charts.score = new Chart(scoreCtx, {
            type: 'bar',
            data: {
                labels: scoreLabels.slice(0, 15),
                datasets: [{
                    label: 'Total Score',
                    data: scoreData.slice(0, 15),
                    backgroundColor: 'rgba(102, 126, 234, 0.8)',
                    borderColor: 'rgba(102, 126, 234, 1)',
                    borderWidth: 1
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: true,
                plugins: {
                    legend: { display: false },
                    title: { display: false }
                },
                scales: {
                    y: {
                        beginAtZero: true,
                        title: { display: true, text: 'Points' }
                    }
                }
            }
        });
    }
    
    // 2. Conductor Frequency Chart
    const conductorCounts = rankings.map(r => ({ name: r.member.name, count: r.conductor_count }));
    conductorCounts.sort((a, b) => b.count - a.count);
    
    const conductorCanvas = document.getElementById('conductorChart');
    if (conductorCanvas) {
        const existingConductorChart = Chart.getChart(conductorCanvas);
        if (existingConductorChart) existingConductorChart.destroy();
        
        const conductorCtx = conductorCanvas.getContext('2d');
        charts.conductor = new Chart(conductorCtx, {
            type: 'bar',
            data: {
                labels: conductorCounts.slice(0, 15).map(c => c.name),
                datasets: [{
                    label: 'Conductor Count',
                    data: conductorCounts.slice(0, 15).map(c => c.count),
                    backgroundColor: 'rgba(255, 159, 64, 0.8)',
                    borderColor: 'rgba(255, 159, 64, 1)',
                    borderWidth: 1
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: true,
                plugins: {
                    legend: { display: false }
                },
                scales: {
                    y: {
                        beginAtZero: true,
                        ticks: { stepSize: 1 },
                        title: { display: true, text: 'Times as Conductor' }
                    }
                }
            }
        });
    }
    
    // 3. Points Breakdown Chart (Top 10)
    const top10 = rankings.slice(0, 10);
    const pointsBreakdownCanvas = document.getElementById('pointsBreakdownChart');
    if (pointsBreakdownCanvas) {
        const existingPointsChart = Chart.getChart(pointsBreakdownCanvas);
        if (existingPointsChart) existingPointsChart.destroy();
        
        const pointsBreakdownCtx = pointsBreakdownCanvas.getContext('2d');
        charts.pointsBreakdown = new Chart(pointsBreakdownCtx, {
            type: 'bar',
            data: {
                labels: top10.map(r => r.member.name),
                datasets: [
                    {
                        label: 'Awards',
                        data: top10.map(r => r.award_points),
                        backgroundColor: 'rgba(255, 205, 86, 0.8)'
                    },
                    {
                        label: 'Recommendations',
                        data: top10.map(r => r.recommendation_points),
                        backgroundColor: 'rgba(75, 192, 192, 0.8)'
                    },
                    {
                        label: 'Rank Boost',
                        data: top10.map(r => r.rank_boost),
                        backgroundColor: 'rgba(153, 102, 255, 0.8)'
                    },
                    {
                        label: 'First Timer',
                        data: top10.map(r => r.first_time_conductor_boost),
                        backgroundColor: 'rgba(54, 162, 235, 0.8)'
                    },
                    {
                        label: 'Recent Penalty',
                        data: top10.map(r => -r.recent_conductor_penalty),
                        backgroundColor: 'rgba(255, 99, 132, 0.8)'
                    },
                    {
                        label: 'Above Avg Penalty',
                        data: top10.map(r => -r.above_average_penalty),
                        backgroundColor: 'rgba(255, 159, 64, 0.8)'
                    }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: true,
                plugins: {
                    legend: { display: true, position: 'bottom' }
                },
                scales: {
                    x: { stacked: true },
                    y: { 
                        stacked: true,
                        title: { display: true, text: 'Points' }
                    }
                }
            }
        });
    }
    
    // Create member timeline charts
    createMemberTimelineCharts(rankings);
}

// Create timeline charts for each member showing point accumulation over last 3 months
async function createMemberTimelineCharts(rankings) {
    // Destroy existing member charts
    memberTimelineCharts.forEach(chart => chart.destroy());
    memberTimelineCharts = [];
    
    // Fetch timeline data and power history for each member (last 3 months)
    try {
        const response = await fetch(`${API_BASE}/member-timelines?months=3`);
        if (!response.ok) throw new Error('Failed to load timeline data');
        
        const timelineData = await response.json();
        
        // Create chart for each member within their ranking card
        for (const ranking of rankings) {
            const memberData = timelineData[ranking.member.id];
            if (!memberData || memberData.dates.length === 0) continue;
            
            const canvas = document.getElementById(`timeline-${ranking.member.id}`);
            if (!canvas) continue;
            
            // Destroy any existing chart on this canvas
            const existingChart = Chart.getChart(canvas);
            if (existingChart) {
                existingChart.destroy();
            }
            
            // Get member-specific settings
            const showReset = document.getElementById(`show-reset-${ranking.member.id}`)?.checked ?? true;
            const showNoReset = document.getElementById(`show-no-reset-${ranking.member.id}`)?.checked ?? true;
            const showBreakdown = document.getElementById(`show-breakdown-${ranking.member.id}`)?.checked ?? false;
            const showPower = document.getElementById(`show-power-${ranking.member.id}`)?.checked ?? true;
            const scaleType = document.querySelector(`input[name="scale-type-${ranking.member.id}"]:checked`)?.value || 'linear';
            
            const ctx = canvas.getContext('2d');
            
            const datasets = [];
            
            // Show breakdown of points (stacked areas)
            if (showBreakdown) {
                if (showReset) {
                    datasets.push({
                        label: 'Awards',
                        data: memberData.awards_with_reset,
                        backgroundColor: 'rgba(255, 205, 86, 0.8)',
                        borderColor: 'rgba(255, 205, 86, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'reset',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'Recommendations',
                        data: memberData.recommendations_with_reset,
                        backgroundColor: 'rgba(75, 192, 192, 0.8)',
                        borderColor: 'rgba(75, 192, 192, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'reset',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'Rank Boost',
                        data: memberData.rank_boost_with_reset,
                        backgroundColor: 'rgba(153, 102, 255, 0.8)',
                        borderColor: 'rgba(153, 102, 255, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'reset',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'First Timer Boost',
                        data: memberData.first_time_boost_with_reset,
                        backgroundColor: 'rgba(54, 162, 235, 0.8)',
                        borderColor: 'rgba(54, 162, 235, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'reset',
                        yAxisID: 'y'
                    });
                    // Penalties are negative
                    datasets.push({
                        label: 'Recent Penalty',
                        data: memberData.recent_penalty_with_reset.map(v => -v),
                        backgroundColor: 'rgba(255, 99, 132, 0.8)',
                        borderColor: 'rgba(255, 99, 132, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'reset',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'Above Avg Penalty',
                        data: memberData.above_avg_penalty_with_reset.map(v => -v),
                        backgroundColor: 'rgba(255, 159, 64, 0.8)',
                        borderColor: 'rgba(255, 159, 64, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'reset',
                        yAxisID: 'y'
                    });
                }
                if (showNoReset) {
                    datasets.push({
                        label: 'Awards (Cumulative)',
                        data: memberData.awards_cumulative,
                        backgroundColor: 'rgba(255, 205, 86, 0.5)',
                        borderColor: 'rgba(255, 205, 86, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'cumulative',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'Recommendations (Cumulative)',
                        data: memberData.recommendations_cumulative,
                        backgroundColor: 'rgba(75, 192, 192, 0.5)',
                        borderColor: 'rgba(75, 192, 192, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'cumulative',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'Rank Boost (Cumulative)',
                        data: memberData.rank_boost_cumulative,
                        backgroundColor: 'rgba(153, 102, 255, 0.5)',
                        borderColor: 'rgba(153, 102, 255, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'cumulative',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'First Timer (Cumulative)',
                        data: memberData.first_time_boost_cumulative,
                        backgroundColor: 'rgba(54, 162, 235, 0.5)',
                        borderColor: 'rgba(54, 162, 235, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'cumulative',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'Recent Penalty (Cumulative)',
                        data: memberData.recent_penalty_cumulative.map(v => -v),
                        backgroundColor: 'rgba(255, 99, 132, 0.5)',
                        borderColor: 'rgba(255, 99, 132, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'cumulative',
                        yAxisID: 'y'
                    });
                    datasets.push({
                        label: 'Above Avg (Cumulative)',
                        data: memberData.above_avg_penalty_cumulative.map(v => -v),
                        backgroundColor: 'rgba(255, 159, 64, 0.5)',
                        borderColor: 'rgba(255, 159, 64, 1)',
                        borderWidth: 1,
                        fill: true,
                        stack: 'cumulative',
                        yAxisID: 'y'
                    });
                }
            } else {
                // Show combined totals (lines)
                if (showReset && memberData.points_with_reset) {
                    datasets.push({
                        label: 'With Train Resets',
                        data: memberData.points_with_reset,
                        borderColor: 'rgba(255, 99, 132, 1)',
                        backgroundColor: 'rgba(255, 99, 132, 0.1)',
                        borderWidth: 2,
                        fill: true,
                        tension: 0.1,
                        yAxisID: 'y'
                    });
                }
                
                if (showNoReset && memberData.points_cumulative) {
                    datasets.push({
                        label: 'Cumulative (No Reset)',
                        data: memberData.points_cumulative,
                        borderColor: 'rgba(75, 192, 192, 1)',
                        backgroundColor: 'rgba(75, 192, 192, 0.1)',
                        borderWidth: 2,
                        yAxisID: 'y',
                        fill: true,
                        tension: 0.1
                    });
                }
            }
            
            if (showPower && memberData.power) {
                datasets.push({
                    label: 'Power',
                    data: memberData.power,
                    borderColor: 'rgba(102, 126, 234, 1)',
                    backgroundColor: 'rgba(102, 126, 234, 0.1)',
                    borderWidth: 2,
                    fill: false,
                    tension: 0.1,
                    yAxisID: 'y1'
                });
            }
            
            if (datasets.length === 0) continue;
            
            // Create annotations for conductor assignments (vertical lines)
            const annotations = {};
            if (memberData.conductor_dates && memberData.conductor_dates.length > 0) {
                memberData.conductor_dates.forEach((conductorWeek, idx) => {
                    const weekIndex = memberData.dates.indexOf(conductorWeek);
                    if (weekIndex !== -1) {
                        annotations[`conductor-${idx}`] = {
                            type: 'line',
                            xMin: weekIndex,
                            xMax: weekIndex,
                            borderColor: 'rgba(255, 159, 64, 0.8)',
                            borderWidth: 2,
                            borderDash: [5, 5],
                            label: {
                                display: true,
                                content: '🚂',
                                position: 'start',
                                yAdjust: -10,
                                font: { size: 14 }
                            }
                        };
                    }
                });
            }
            
            const chart = new Chart(ctx, {
                type: 'line',
                data: {
                    labels: memberData.dates,
                    datasets: datasets
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: true,
                    plugins: {
                        legend: { 
                            display: true, 
                            position: 'top',
                            labels: { font: { size: 11 } }
                        },
                        tooltip: {
                            mode: 'index',
                            intersect: false
                        },
                        annotation: {
                            annotations: annotations
                        }
                    },
                    scales: {
                        x: {
                            title: { display: true, text: 'Week', font: { size: 11 } },
                            ticks: { 
                                maxRotation: 45,
                                minRotation: 45,
                                font: { size: 9 }
                            },
                            stacked: showBreakdown
                        },
                        y: {
                            type: scaleType,
                            beginAtZero: true,
                            position: 'left',
                            title: { display: true, text: 'Points', font: { size: 11 } },
                            ticks: { font: { size: 10 } },
                            stacked: showBreakdown
                        },
                        y1: {
                            type: 'linear',
                            position: 'right',
                            beginAtZero: false,
                            title: { display: true, text: 'Power', font: { size: 11 }, color: 'rgba(102, 126, 234, 1)' },
                            ticks: { 
                                font: { size: 10 },
                                color: 'rgba(102, 126, 234, 1)',
                                callback: function(value) {
                                    return formatPowerShort(value);
                                }
                            },
                            grid: {
                                drawOnChartArea: false
                            }
                        }
                    },
                    interaction: {
                        mode: 'nearest',
                        axis: 'x',
                        intersect: false
                    }
                }
            });
            
            memberTimelineCharts.push(chart);
        }
    } catch (error) {
        console.error('Error creating member timeline charts:', error);
    }
}

// Format power for chart labels (short)
function formatPowerShort(power) {
    if (!power) return '0';
    if (power >= 1000000000) {
        return (power / 1000000000).toFixed(1) + 'B';
    } else if (power >= 1000000) {
        return (power / 1000000).toFixed(1) + 'M';
    } else if (power >= 1000) {
        return (power / 1000).toFixed(0) + 'K';
    }
    return power.toString();
}

// Filter rankings
function filterRankings() {
    if (!currentData || !currentData.rankings) return;
    
    const nameFilter = document.getElementById('filter-name').value.toLowerCase().trim();
    const rankFilter = document.getElementById('filter-rank').value;
    
    filteredRankings = currentData.rankings.filter(ranking => {
        const nameMatch = !nameFilter || ranking.member.name.toLowerCase().includes(nameFilter);
        const rankMatch = !rankFilter || ranking.member.rank === rankFilter;
        return nameMatch && rankMatch;
    });
    
    displayRankings(filteredRankings);
}

// Clear filters
function clearFilters() {
    const filterName = document.getElementById('filter-name');
    const filterRank = document.getElementById('filter-rank');
    if (filterName) filterName.value = '';
    if (filterRank) filterRank.value = '';
    
    filteredRankings = currentData.rankings;
    displayRankings(filteredRankings);
}

// Display rankings
function displayRankings(rankings) {
    const rankingsList = document.getElementById('rankings-list');
    if (!rankingsList) return;

    if (rankings.length === 0) {
        rankingsList.innerHTML = '<p class="empty">No members match the current filters.</p>';
        return;
    }

    let html = '';
    rankings.forEach((ranking, index) => {
        const rankClass = index === 0 ? 'rank-first' : index === 1 ? 'rank-second' : index === 2 ? 'rank-third' : '';
        const medal = index === 0 ? '🥇' : index === 1 ? '🥈' : index === 2 ? '🥉' : '';
        
        html += `
            <div class="ranking-card ${rankClass}">
                <div class="ranking-header">
                    <div class="ranking-position">
                        <span class="position-number">${medal} #${index + 1}</span>
                        <h4>${escapeHtml(ranking.member.name)} <span class="rank-badge">${ranking.member.rank}</span></h4>
                    </div>
                    <div class="total-score">
                        <span class="score-value">${ranking.total_score}</span>
                        <span class="score-label">pts</span>
                    </div>
                </div>
                
                <div class="ranking-details">
                    <div class="detail-section">
                        <h5>📊 Score Breakdown</h5>
                        <div class="detail-grid">
                            <div class="detail-item positive">
                                <span class="detail-label">🏆 Awards:</span>
                                <span class="detail-value">+${ranking.award_points} pts</span>
                            </div>
                            <div class="detail-item positive">
                                <span class="detail-label">⭐ Recommendations:</span>
                                <span class="detail-value">+${ranking.recommendation_points} pts (${ranking.recommendation_count})</span>
                            </div>
                            <div class="detail-item positive">
                                <span class="detail-label">🏅 Rank Boost:</span>
                                <span class="detail-value">+${ranking.rank_boost} pts</span>
                            </div>
                            <div class="detail-item positive">
                                <span class="detail-label">🎯 First Timer:</span>
                                <span class="detail-value">+${ranking.first_time_conductor_boost} pts</span>
                            </div>
                            <div class="detail-item negative">
                                <span class="detail-label">⏱️ Recent Conductor:</span>
                                <span class="detail-value">-${ranking.recent_conductor_penalty} pts</span>
                            </div>
                            <div class="detail-item negative">
                                <span class="detail-label">📈 Above Average:</span>
                                <span class="detail-value">-${ranking.above_average_penalty} pts</span>
                            </div>
                        </div>
                    </div>
                    
                    ${ranking.award_details && ranking.award_details.length > 0 ? `
                        <div class="detail-section">
                            <div class="section-header-with-toggle">
                                <h5>🏆 Award Details (${ranking.award_details.filter(a => !a.expired).length} active${ranking.award_details.some(a => a.expired) ? `, ${ranking.award_details.filter(a => a.expired).length} inactive` : ''})</h5>
                                <label class="checkbox-label">
                                    <input type="checkbox" id="show-inactive-${ranking.member.id}" class="show-inactive-toggle" data-member-id="${ranking.member.id}">
                                    <span>Show Inactive Awards</span>
                                </label>
                            </div>
                            <div class="awards-compact-list" id="awards-list-${ranking.member.id}">
                                ${ranking.award_details.filter(a => !a.expired).map(award => `
                                    <div class="award-compact-item">
                                        <span class="award-icon-compact">${getRankEmoji(award.rank)}</span>
                                        <div class="award-info-compact">
                                            <span class="award-type-compact">${escapeHtml(award.award_type)}</span>
                                            <span class="award-week-compact">${getWeeksAgo(award.week_date)}</span>
                                        </div>
                                        <span class="award-points-compact">+${award.points}</span>
                                    </div>
                                `).join('')}
                            </div>
                        </div>
                    ` : `
                        <div class="detail-section">
                            <h5>🏆 Award Details</h5>
                            <p class="no-awards">No awards yet</p>
                        </div>
                    `}
                    
                    <div class="detail-section">
                        <h5>📈 Conductor Statistics</h5>
                        <div class="stats-grid">
                            <div class="stat-item">
                                <span class="stat-label">Times as Conductor:</span>
                                <span class="stat-value">${ranking.conductor_count}</span>
                            </div>
                            <div class="stat-item">
                                <span class="stat-label">Last Conductor Date:</span>
                                <span class="stat-value">${formatDate(ranking.last_conductor_date)}</span>
                            </div>
                            ${ranking.days_since_last_conductor !== null ? `
                                <div class="stat-item">
                                    <span class="stat-label">Days Since Last:</span>
                                    <span class="stat-value">${ranking.days_since_last_conductor} days</span>
                                </div>
                            ` : ''}
                        </div>
                    </div>
                    
                    <div class="detail-section">
                        <h5>📊 Point Accumulation & Power Timeline (Last 3 Months)</h5>
                        <div class="chart-options">
                            <label>
                                <input type="checkbox" id="show-reset-${ranking.member.id}" class="timeline-option" data-member-id="${ranking.member.id}" checked>
                                <span>Show with Train Resets</span>
                            </label>
                            <label>
                                <input type="checkbox" id="show-no-reset-${ranking.member.id}" class="timeline-option" data-member-id="${ranking.member.id}" checked>
                                <span>Show without Resets (Cumulative)</span>
                            </label>
                            <label>
                                <input type="checkbox" id="show-breakdown-${ranking.member.id}" class="timeline-option" data-member-id="${ranking.member.id}">
                                <span>📈 Show Point Breakdown (All Categories)</span>
                            </label>
                            <label>
                                <input type="checkbox" id="show-power-${ranking.member.id}" class="timeline-option" data-member-id="${ranking.member.id}" checked>
                                <span>⚡ Show Power</span>
                            </label>
                            <label>
                                <input type="radio" name="scale-type-${ranking.member.id}" class="timeline-option" data-member-id="${ranking.member.id}" value="linear" checked>
                                <span>Linear Scale</span>
                            </label>
                            <label>
                                <input type="radio" name="scale-type-${ranking.member.id}" class="timeline-option" data-member-id="${ranking.member.id}" value="logarithmic">
                                <span>Logarithmic Scale</span>
                            </label>
                        </div>
                        <canvas id="timeline-${ranking.member.id}" class="member-timeline-canvas"></canvas>
                    </div>
                </div>
            </div>
        `;
    });
    
    rankingsList.innerHTML = html;
    
    // Add event listeners for timeline options (per member)
    document.querySelectorAll('.timeline-option').forEach(element => {
        element.addEventListener('change', () => {
            if (currentData) displayCharts(currentData);
        });
    });
    
    // Add event listeners for show inactive awards toggle (per member)
    document.querySelectorAll('.show-inactive-toggle').forEach(checkbox => {
        checkbox.addEventListener('change', (e) => {
            const memberId = parseInt(e.target.dataset.memberId);
            toggleInactiveAwards(memberId, e.target.checked);
        });
    });
    
    // Create timeline charts after rendering rankings
    createMemberTimelineCharts(filteredRankings);
}

// Toggle inactive awards for a specific member
function toggleInactiveAwards(memberId, showInactive) {
    const ranking = filteredRankings.find(r => r.member.id === memberId);
    if (!ranking || !ranking.award_details) return;
    
    const awardsList = document.getElementById(`awards-list-${memberId}`);
    if (!awardsList) return;
    
    if (showInactive) {
        // Show all awards including inactive
        awardsList.innerHTML = ranking.award_details.map(award => `
            <div class="award-compact-item ${award.expired ? 'expired-award' : ''}">
                <span class="award-icon-compact">${getRankEmoji(award.rank)}</span>
                <div class="award-info-compact">
                    <span class="award-type-compact">${escapeHtml(award.award_type)}${award.expired ? ' (Expired)' : ''}</span>
                    <span class="award-week-compact">${getWeeksAgo(award.week_date)}</span>
                </div>
                <span class="award-points-compact">+${award.points}</span>
            </div>
        `).join('');
    } else {
        // Show only active awards
        awardsList.innerHTML = ranking.award_details.filter(a => !a.expired).map(award => `
            <div class="award-compact-item">
                <span class="award-icon-compact">${getRankEmoji(award.rank)}</span>
                <div class="award-info-compact">
                    <span class="award-type-compact">${escapeHtml(award.award_type)}</span>
                    <span class="award-week-compact">${getWeeksAgo(award.week_date)}</span>
                </div>
                <span class="award-points-compact">+${award.points}</span>
            </div>
        `).join('');
    }
}

// Get rank emoji
function getRankEmoji(rank) {
    switch(rank) {
        case 1: return '🥇';
        case 2: return '🥈';
        case 3: return '🥉';
        default: return '🏅';
    }
}

// Get place text
function getPlaceText(rank) {
    switch(rank) {
        case 1: return '1st Place';
        case 2: return '2nd Place';
        case 3: return '3rd Place';
        default: return `${rank}th Place`;
    }
}

// Get weeks ago text
function getWeeksAgo(weekDate) {
    const awardDate = new Date(weekDate);
    const today = new Date();
    
    // Calculate Monday of current week
    const currentMonday = new Date(today);
    currentMonday.setDate(today.getDate() - today.getDay() + 1);
    currentMonday.setHours(0, 0, 0, 0);
    
    // Calculate difference in weeks
    const diffTime = currentMonday - awardDate;
    const diffWeeks = Math.floor(diffTime / (1000 * 60 * 60 * 24 * 7));
    
    if (diffWeeks === 0) {
        return 'This week';
    } else if (diffWeeks === 1) {
        return 'Last week';
    } else if (diffWeeks === 2) {
        return '2 weeks ago';
    } else if (diffWeeks === 3) {
        return '3 weeks ago';
    } else if (diffWeeks === 4) {
        return '4 weeks ago';
    } else {
        return `${diffWeeks} weeks ago`;
    }
}

// Escape HTML
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    const rankingsList = document.getElementById('rankings-list');
    
    // Guard: Only initialize if we are on the Rankings page
    if (rankingsList) {
        await loadRankings();
        
        // Add event listeners for this specific page
        const refreshBtn = document.getElementById('refresh-btn');
        if (refreshBtn) refreshBtn.addEventListener('click', loadRankings);
        
        const filterName = document.getElementById('filter-name');
        if (filterName) filterName.addEventListener('input', filterRankings);
        
        const filterRank = document.getElementById('filter-rank');
        if (filterRank) filterRank.addEventListener('change', filterRankings);
        
        const clearFiltersBtn = document.getElementById('clear-filters-btn');
        if (clearFiltersBtn) clearFiltersBtn.addEventListener('click', clearFilters);
    }
});