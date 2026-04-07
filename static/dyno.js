// static/dyno.js
const API_URL = '/api/dyno-recommendations';
const MEMBERS_URL = '/api/members';

let allMembers = [];
let allDynoRecs = [];
let currentUsername = '';
let currentUserId = 0;
let currentView = 'list'; // 'list' or 'grouped'
let currentFilter = 'all'; // 'all', 'active', 'positive', 'negative', 'mine'
let canManageDyno = false;


// Chart instances
let pointsChart = null;
let membersChart = null;
let timelineChart = null;

// Fetch permissions to get the user ID for Delete button rendering
async function fetchPermissions() {
    try {
        const response = await fetch('/api/check-auth');
        if (response.ok) {
            const data = await response.json();
            currentUsername = data.username;
            // Removed reliance on user_id to prevent the 0 === 0 scrubbing bug

            // Extract the manage_dyno permission
            canManageDyno = data.permissions?.manage_dyno || data.is_admin || false;
        }
    } catch (error) {
        console.error('Auth check error:', error);
    }
}

// Load members
async function loadMembers() {
    try {
        const response = await fetch(MEMBERS_URL);
        if (!response.ok) throw new Error('Failed to fetch members');

        allMembers = await response.json();
        allMembers.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));

        // populateMemberSelect() was removed here because the reactive modal handles the UI now
    } catch (error) {
        console.error('Error loading members:', error);
        alert('Failed to load members.');
    }
}

// Setup Modal Event Listeners
function setupModal() {
    const modal = document.getElementById('shoutout-modal');
    const openBtn = document.getElementById('open-shoutout-modal-btn');
    const closeBtn = document.getElementById('close-shoutout-modal');
    const cancelBtn = document.getElementById('cancel-shoutout-btn');

    const openModal = () => {
        modal.style.display = 'flex';
        trapFocus(modal);

        document.getElementById('modal-title').textContent = '➕ Add a Shoutout';
        document.getElementById('edit-shoutout-id').value = '';

        // Dynamically parse ranks for visibility dropdown to prevent hardcoding
        const ranks = [...new Set(allMembers.map(m => m.rank).filter(r => r))].sort();
        const rankSelect = document.getElementById('min-rank-select');
        if (rankSelect && rankSelect.options.length <= 1) { // Only populate if empty
            ranks.forEach(rank => {
                const opt = document.createElement('option');
                opt.value = rank;
                opt.textContent = rank + ' and above';
                rankSelect.appendChild(opt);
            });
        }
    };

    const closeModal = () => {
        releaseFocus(modal);
        modal.style.display = 'none';
        document.getElementById('shoutout-form').reset();
        document.getElementById('selected-member-display').style.display = 'none';
        document.getElementById('member-search-results').style.display = 'none';
        document.getElementById('member-select').value = '';
    };

    if (openBtn) openBtn.addEventListener('click', openModal);
    if (closeBtn) closeBtn.addEventListener('click', closeModal);
    if (cancelBtn) cancelBtn.addEventListener('click', closeModal);

    // Close on outside click
    window.addEventListener('click', (e) => {
        if (e.target === modal) closeModal();
    });
}

// Setup Reactive Member Search within the Modal
function setupReactiveSearch() {
    const searchInput = document.getElementById('member-search');
    const resultsContainer = document.getElementById('member-search-results');
    const hiddenSelect = document.getElementById('member-select');
    const displayElement = document.getElementById('selected-member-display');

    if (!searchInput || !resultsContainer) return;

    searchInput.addEventListener('input', (e) => {
        const term = e.target.value.toLowerCase().trim();
        resultsContainer.replaceChildren();

        if (term.length < 1) {
            resultsContainer.style.display = 'none';
            return;
        }

        const matches = allMembers.filter(m => m.name.toLowerCase().includes(term));

        if (matches.length > 0) {
            resultsContainer.style.display = 'block';
            matches.forEach(member => {
                const div = document.createElement('div');
                div.className = 'member-search-item';

                const strong = document.createElement('strong');
                strong.textContent = member.name;
                const badge = document.createElement('span');
                badge.className = `rank-badge rank-${member.rank.toLowerCase()}`;
                badge.style.fontSize = '0.75em';
                badge.textContent = member.rank;
                div.append(strong, ' ', badge);

                div.addEventListener('click', () => {
                    hiddenSelect.value = member.id;
                    searchInput.value = ''; // Clear search bar
                    displayElement.textContent = `Target: ${member.name} (${member.rank})`;
                    displayElement.style.display = 'block';
                    resultsContainer.style.display = 'none';
                });

                resultsContainer.appendChild(div);
            });
        } else {
            resultsContainer.style.display = 'block';
            const noResult = document.createElement('div');
            noResult.className = 'member-search-item';
            noResult.style.cssText = 'color: #999; cursor: default;';
            noResult.textContent = 'No members found';
            resultsContainer.appendChild(noResult);
        }
    });

    // Hide results if clicking outside the search box
    document.addEventListener('click', (e) => {
        if (e.target !== searchInput && e.target !== resultsContainer) {
            resultsContainer.style.display = 'none';
        }
    });
}

// Load dyno recommendations
async function loadDynoRecommendations() {
    try {
        const response = await fetch(API_URL);
        allDynoRecs = await response.json();
        updateStatistics();
        updateCharts();
        renderDynoRecommendations();
    } catch (error) {
        console.error('Error loading dyno recommendations:', error);
        const list = document.getElementById('dyno-list');
        if (list) {
            const p = document.createElement('p');
            p.className = 'error-message';
            p.textContent = 'Failed to load dyno recommendations.';
            list.replaceChildren(p);
        }
    }
}

// Update statistics dashboard
function updateStatistics() {
    const active = allDynoRecs.filter(r => !r.expired);
    const expired = allDynoRecs.filter(r => r.expired);
    const positive = active.filter(r => r.points > 0);
    const negative = active.filter(r => r.points < 0);

    const totalEl = document.getElementById('total-dyno');
    if (totalEl) {
        totalEl.textContent = active.length;
        document.getElementById('positive-dyno').textContent = positive.length;
        document.getElementById('negative-dyno').textContent = negative.length;
        document.getElementById('expired-dyno').textContent = expired.length;
    }
}

// Update all charts
function updateCharts() {
    updatePointsChart();
    updateMembersChart();
    updateTimelineChart();
}

// Create/Update Points Distribution Chart
function updatePointsChart() {
    const ctx = document.getElementById('points-chart');
    if (!ctx) return;

    const active = allDynoRecs.filter(r => !r.expired);
    const positiveCount = active.filter(r => r.points > 0).length;
    const negativeCount = active.filter(r => r.points < 0).length;
    const neutralCount = active.filter(r => r.points === 0).length;

    if (pointsChart) {
        pointsChart.destroy();
    }

    pointsChart = new Chart(ctx, {
        type: 'doughnut',
        data: {
            labels: ['Positive', 'Negative', 'Neutral'],
            datasets: [{
                data: [positiveCount, negativeCount, neutralCount],
                backgroundColor: [
                    'rgba(67, 233, 123, 0.8)',
                    'rgba(245, 87, 108, 0.8)',
                    'rgba(156, 163, 175, 0.8)'
                ],
                borderColor: [
                    'rgba(67, 233, 123, 1)',
                    'rgba(245, 87, 108, 1)',
                    'rgba(156, 163, 175, 1)'
                ],
                borderWidth: 2
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: true,
            plugins: {
                legend: {
                    position: 'bottom',
                    labels: {
                        padding: 15,
                        font: { size: 12 }
                    }
                },
                tooltip: {
                    callbacks: {
                        label: function(context) {
                            const label = context.label || '';
                            const value = context.parsed || 0;
                            const total = context.dataset.data.reduce((a, b) => a + b, 0);
                            const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : 0;
                            return `${label}: ${value} (${percentage}%)`;
                        }
                    }
                }
            }
        }
    });
}

// Create/Update Top Members Chart
function updateMembersChart() {
    const ctx = document.getElementById('members-chart');
    if (!ctx) return;

    const active = allDynoRecs.filter(r => !r.expired);

    // Calculate net points per member
    const memberPoints = {};
    active.forEach(rec => {
        if (!memberPoints[rec.member_name]) {
            memberPoints[rec.member_name] = 0;
        }
        memberPoints[rec.member_name] += rec.points;
    });

    // Sort and get top 10 by absolute value
    const sorted = Object.entries(memberPoints)
        .sort((a, b) => Math.abs(b[1]) - Math.abs(a[1]))
        .slice(0, 10);

    const labels = sorted.map(([name]) => name);
    const data = sorted.map(([, points]) => points);
    const colors = data.map(points => points >= 0 ? 'rgba(67, 233, 123, 0.8)' : 'rgba(245, 87, 108, 0.8)');
    const borderColors = data.map(points => points >= 0 ? 'rgba(67, 233, 123, 1)' : 'rgba(245, 87, 108, 1)');

    if (membersChart) {
        membersChart.destroy();
    }

    membersChart = new Chart(ctx, {
        type: 'bar',
        data: {
            labels: labels,
            datasets: [{
                label: 'Net Points',
                data: data,
                backgroundColor: colors,
                borderColor: borderColors,
                borderWidth: 2
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: true,
            indexAxis: 'y',
            plugins: {
                legend: {
                    display: false
                },
                tooltip: {
                    callbacks: {
                        label: function(context) {
                            const value = context.parsed.x;
                            return `Net Points: ${value > 0 ? '+' : ''}${value}`;
                        }
                    }
                }
            },
            scales: {
                x: {
                    beginAtZero: true,
                    grid: {
                        color: 'rgba(0, 0, 0, 0.05)'
                    }
                },
                y: {
                    grid: {
                        display: false
                    }
                }
            }
        }
    });
}

// Create/Update Timeline Chart
function updateTimelineChart() {
    const ctx = document.getElementById('timeline-chart');
    if (!ctx) return;

    // Get last 7 days
    const today = new Date();
    const days = [];
    for (let i = 6; i >= 0; i--) {
        const date = new Date(today);
        date.setDate(date.getDate() - i);
        days.push(date);
    }

    const labels = days.map(d => {
        const month = (d.getMonth() + 1).toString().padStart(2, '0');
        const day = d.getDate().toString().padStart(2, '0');
        return `${month}/${day}`;
    });

    // Count recommendations per day
    const positiveData = days.map(date => {
        const dateStr = formatDateOnly(date);
        return allDynoRecs.filter(rec => {
            const recDate = formatDateOnly(new Date(rec.created_at));
            return recDate === dateStr && rec.points > 0;
        }).length;
    });

    const negativeData = days.map(date => {
        const dateStr = formatDateOnly(date);
        return allDynoRecs.filter(rec => {
            const recDate = formatDateOnly(new Date(rec.created_at));
            return recDate === dateStr && rec.points < 0;
        }).length;
    });

    if (timelineChart) {
        timelineChart.destroy();
    }

    timelineChart = new Chart(ctx, {
        type: 'line',
        data: {
            labels: labels,
            datasets: [{
                label: 'Positive',
                data: positiveData,
                borderColor: 'rgba(67, 233, 123, 1)',
                backgroundColor: 'rgba(67, 233, 123, 0.1)',
                tension: 0.3,
                fill: true,
                pointRadius: 4,
                pointHoverRadius: 6
            }, {
                label: 'Negative',
                data: negativeData,
                borderColor: 'rgba(245, 87, 108, 1)',
                backgroundColor: 'rgba(245, 87, 108, 0.1)',
                tension: 0.3,
                fill: true,
                pointRadius: 4,
                pointHoverRadius: 6
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: true,
            plugins: {
                legend: {
                    position: 'bottom',
                    labels: {
                        padding: 15,
                        usePointStyle: true
                    }
                },
                tooltip: {
                    callbacks: {
                        label: function(context) {
                            return `${context.dataset.label}: ${context.parsed.y} dynos`;
                        }
                    }
                }
            },
            scales: {
                y: {
                    beginAtZero: true,
                    ticks: {
                        stepSize: 1
                    },
                    grid: {
                        color: 'rgba(0, 0, 0, 0.05)'
                    }
                },
                x: {
                    grid: {
                        display: false
                    }
                }
            }
        }
    });
}

// Helper function to format date as YYYY-MM-DD
function formatDateOnly(date) {
    const year = date.getFullYear();
    const month = (date.getMonth() + 1).toString().padStart(2, '0');
    const day = date.getDate().toString().padStart(2, '0');
    return `${year}-${month}-${day}`;
}

// Render dyno recommendations
function renderDynoRecommendations() {
    const container = document.getElementById('dyno-list');
    if (!container) return;

    const searchInput = document.getElementById('filter-search');
    const filterSearch = searchInput ? searchInput.value.toLowerCase() : '';

    // Apply filter
    let filtered = allDynoRecs.filter(rec => {
        const memberMatch = rec.member_name.toLowerCase().includes(filterSearch);
        const creatorMatch = rec.created_by.toLowerCase().includes(filterSearch);
        const searchMatch = !filterSearch || memberMatch || creatorMatch;

        let statusMatch = true;
        if (currentFilter === 'active') {
            statusMatch = !rec.expired;
        } else if (currentFilter === 'positive') {
            statusMatch = !rec.expired && rec.points > 0;
        } else if (currentFilter === 'negative') {
            statusMatch = !rec.expired && rec.points < 0;
        } else if (currentFilter === 'mine') {
            statusMatch = rec.created_by_id === currentUserId;
        }

        return searchMatch && statusMatch;
    });

    if (filtered.length === 0) {
        const p = document.createElement('p');
        p.className = 'no-data';
        p.textContent = 'No dyno recommendations found.';
        container.replaceChildren(p);
        return;
    }

    if (currentView === 'grouped') {
        renderGroupedView(filtered, container);
    } else {
        renderListView(filtered, container);
    }
}

// Render list view
function renderListView(recs, container) {
    container.replaceChildren(...recs.map(rec => createDynoCard(rec)));
}

// Render grouped view (by member)
function renderGroupedView(recs, container) {
    // Group by member
    const grouped = {};
    recs.forEach(rec => {
        if (!grouped[rec.member_id]) {
            grouped[rec.member_id] = {
                member: { id: rec.member_id, name: rec.member_name, rank: rec.member_rank },
                recommendations: []
            };
        }
        grouped[rec.member_id].recommendations.push(rec);
    });

    const groupCards = Object.values(grouped).map(group => {
        const totalPoints = group.recommendations.reduce((sum, r) => sum + r.points, 0);
        const activeRecs = group.recommendations.filter(r => !r.expired);

        const groupCard = document.createElement('div');
        groupCard.className = 'member-group-card';

        const header = document.createElement('div');
        header.className = 'member-group-header';

        const memberInfo = document.createElement('div');
        memberInfo.className = 'member-info';
        const nameSpan = document.createElement('span');
        nameSpan.className = 'member-name';
        nameSpan.textContent = group.member.name;
        const rankBadge = document.createElement('span');
        rankBadge.className = `rank-badge rank-${group.member.rank.toLowerCase()}`;
        rankBadge.textContent = group.member.rank;
        const countSpan = document.createElement('span');
        countSpan.className = 'dyno-count';
        countSpan.textContent = `${activeRecs.length} active`;
        memberInfo.append(nameSpan, rankBadge, countSpan);

        const totalSpan = document.createElement('div');
        totalSpan.className = `member-total-points ${totalPoints >= 0 ? 'positive' : 'negative'}`;
        totalSpan.textContent = `${totalPoints > 0 ? '+' : ''}${totalPoints} pts`;

        header.append(memberInfo, totalSpan);

        const recsContainer = document.createElement('div');
        recsContainer.className = 'grouped-recommendations';
        group.recommendations.forEach(rec => recsContainer.appendChild(createDynoCard(rec, true)));

        groupCard.append(header, recsContainer);
        return groupCard;
    });

    container.replaceChildren(...groupCards);
}

// Create dyno recommendation card
function createDynoCard(rec, compact = false) {
    const card = document.createElement('div');
    card.className = `recommendation-card ${rec.expired ? 'expired' : ''}`;

    const pointsClass = rec.points > 0 ? 'positive' : (rec.points < 0 ? 'negative' : 'neutral');
    const pointsIcon = rec.points > 0 ? '✅' : (rec.points < 0 ? '❌' : '➖');

    const createdDate = new Date(rec.created_at);
    const now = new Date();
    const expiryDate = new Date(createdDate.getTime() + 7 * 24 * 60 * 60 * 1000);
    const daysLeft = Math.ceil((expiryDate - now) / (1000 * 60 * 60 * 24));

    const expiryText = rec.expired
        ? '⏱️ Expired'
        : `⏱️ ${daysLeft} day${daysLeft !== 1 ? 's' : ''} left`;

    // Header
    const recHeader = document.createElement('div');
    recHeader.className = 'rec-header';

    if (!compact) {
        const memberInfo = document.createElement('div');
        memberInfo.className = 'member-info';
        const nameSpan = document.createElement('span');
        nameSpan.className = 'member-name';
        nameSpan.textContent = rec.member_name;
        const rankBadge = document.createElement('span');
        rankBadge.className = `rank-badge rank-${rec.member_rank.toLowerCase()}`;
        rankBadge.textContent = rec.member_rank;
        memberInfo.append(nameSpan, rankBadge);
        recHeader.appendChild(memberInfo);
    }

    const pointsDiv = document.createElement('div');
    pointsDiv.className = `rec-points ${pointsClass}`;
    pointsDiv.textContent = `${pointsIcon} ${rec.points > 0 ? '+' : ''}${rec.points}`;
    recHeader.appendChild(pointsDiv);

    // Notes
    const notesDiv = document.createElement('div');
    notesDiv.className = 'rec-notes';
    notesDiv.textContent = rec.notes || 'No notes provided';

    // Footer
    const footer = document.createElement('div');
    footer.className = 'rec-footer';
    footer.style.cssText = 'display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 15px; margin-top: 15px;';

    const meta = document.createElement('div');
    meta.className = 'rec-meta';
    meta.style.cssText = 'display: flex; gap: 10px; flex-wrap: wrap; align-items: center;';

    const bySpan = document.createElement('span');
    bySpan.className = 'rec-by';
    bySpan.textContent = `by ${rec.created_by}`;
    meta.appendChild(bySpan);

    if (!rec.is_author_public && currentUsername && rec.created_by === currentUsername) {
        const anonBadge = document.createElement('span');
        anonBadge.className = 'expiry-badge';
        anonBadge.style.cssText = 'background: #6c757d; color: white;';
        anonBadge.title = 'Your name is hidden from members without the view permission';
        anonBadge.textContent = '🕵️ Anonymous to Alliance';
        meta.appendChild(anonBadge);
    }

    const dateSpan = document.createElement('span');
    dateSpan.className = 'rec-date';
    dateSpan.textContent = formatDate(rec.created_at);
    meta.appendChild(dateSpan);

    const expiryBadge = document.createElement('span');
    expiryBadge.className = `expiry-badge ${rec.expired ? 'expired' : ''}`;
    expiryBadge.textContent = expiryText;
    meta.appendChild(expiryBadge);

    const actions = document.createElement('div');
    actions.className = 'member-actions';

    if (!rec.expired && currentUsername && rec.created_by === currentUsername) {
        const editBtn = document.createElement('button');
        editBtn.className = 'edit-btn';
        editBtn.textContent = '✏️ Edit';
        editBtn.addEventListener('click', () => editDynoRecommendation(rec.id));
        actions.appendChild(editBtn);
    }

    if (canManageDyno || (currentUsername && rec.created_by === currentUsername)) {
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'delete-btn';
        deleteBtn.textContent = '🗑️ Delete';
        deleteBtn.addEventListener('click', () => deleteDynoRecommendation(rec.id));
        actions.appendChild(deleteBtn);
    }

    footer.append(meta, actions);
    card.append(recHeader, notesDiv, footer);
    return card;
}

// Submit dyno recommendation
async function submitDynoRecommendation(e) {
    if (e) e.preventDefault(); // Prevent standard form submission

    const memberId = parseInt(document.getElementById('member-select').value);
    const points = parseInt(document.getElementById('points-input').value);
    const notes = document.getElementById('notes-input').value.trim();
    const isPublic = document.getElementById('is-public-input').checked;
    const minRank = document.getElementById('min-rank-select').value;

    if (!memberId) {
        alert('Please search and select a target member.');
        return;
    }

    if (isNaN(points)) {
        alert('Please enter valid points.');
        return;
    }

    if (!notes) {
        alert('Please provide notes for this recommendation.');
        return;
    }

    const editId = document.getElementById('edit-shoutout-id').value;
    const method = editId ? 'PUT' : 'POST';
    const endpoint = editId ? `${API_URL}/${editId}` : API_URL;

    try {
        const response = await fetch(endpoint, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                member_id: memberId,
                points: points,
                notes: notes,
                is_author_public: isPublic,
                min_view_rank: minRank
            })
        });

        if (!response.ok) throw new Error(await response.text());

        const shoutoutModal = document.getElementById('shoutout-modal');
        releaseFocus(shoutoutModal);
        shoutoutModal.style.display = 'none';
        document.getElementById('shoutout-form').reset();
        document.getElementById('selected-member-display').style.display = 'none';
        document.getElementById('edit-shoutout-id').value = '';

        await loadDynoRecommendations();
    } catch (error) {
        console.error('Error saving shoutout:', error);
        alert('Failed to save shoutout: ' + error.message);
    }
}

async function deleteDynoRecommendation(id) {
    if (!confirm('Are you sure you want to delete this dyno recommendation?')) {
        return;
    }

    try {
        const response = await fetch(`${API_URL}/${id}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            throw new Error('Failed to delete dyno recommendation');
        }

        await loadDynoRecommendations();
    } catch (error) {
        console.error('Error deleting dyno recommendation:', error);
        alert('Failed to delete dyno recommendation.');
    }
}

function editDynoRecommendation(id) {
    const rec = allDynoRecs.find(r => r.id === id);
    if (!rec) return;

    // Set hidden ID and Title
    document.getElementById('edit-shoutout-id').value = rec.id;
    document.getElementById('modal-title').textContent = '✏️ Edit Shoutout';

    // Populate form fields
    document.getElementById('member-select').value = rec.member_id;
    const displayElement = document.getElementById('selected-member-display');
    displayElement.textContent = `Target: ${rec.member_name} (${rec.member_rank})`;
    displayElement.style.display = 'block';

    document.getElementById('points-input').value = rec.points;
    document.getElementById('notes-input').value = rec.notes;
    document.getElementById('is-public-input').checked = rec.is_author_public;

    // Open modal and set ranks
    const modal = document.getElementById('shoutout-modal');
    modal.style.display = 'flex';
    trapFocus(modal);

    // Ensure visibility ranks are populated, then select the value
    const ranks = [...new Set(allMembers.map(m => m.rank).filter(r => r))].sort();
    const rankSelect = document.getElementById('min-rank-select');
    if (rankSelect && rankSelect.options.length <= 1) {
        ranks.forEach(rank => {
            const opt = document.createElement('option');
            opt.value = rank;
            opt.textContent = rank + ' and above';
            rankSelect.appendChild(opt);
        });
    }
    rankSelect.value = rec.min_view_rank || '';
}

// Format date
function formatDate(dateStr) {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now - date;
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 1) return 'Just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;

    return date.toLocaleDateString();
}

// Setup view toggle
function setupViewToggle() {
    const listBtn = document.getElementById('list-view-btn');
    const groupedBtn = document.getElementById('grouped-view-btn');

    if (!listBtn || !groupedBtn) return;

    listBtn.addEventListener('click', () => {
        currentView = 'list';
        listBtn.classList.add('active');
        groupedBtn.classList.remove('active');
        renderDynoRecommendations();
    });

    groupedBtn.addEventListener('click', () => {
        currentView = 'grouped';
        groupedBtn.classList.add('active');
        listBtn.classList.remove('active');
        renderDynoRecommendations();
    });
}

// Setup filter chips
function setupFilters() {
    const filterChips = document.querySelectorAll('.filter-chip');

    filterChips.forEach(chip => {
        chip.addEventListener('click', () => {
            filterChips.forEach(c => c.classList.remove('active'));
            chip.classList.add('active');
            currentFilter = chip.dataset.filter;
            renderDynoRecommendations();
        });
    });

    // Search filter
    const searchInput = document.getElementById('filter-search');
    if (searchInput) {
        searchInput.addEventListener('input', () => {
            renderDynoRecommendations();
        });
    }
}

// Run on page load
document.addEventListener('DOMContentLoaded', async () => {
    // Guard: Only run if we are actually on the Dyno page
    const dynoList = document.getElementById('dyno-list');
    if (!dynoList) return;

    await fetchPermissions();
    await loadMembers();
    await loadDynoRecommendations();

    setupModal();
    setupReactiveSearch();
    setupViewToggle();
    setupFilters();

    // Bind to the form submit event instead of the button click
    const form = document.getElementById('shoutout-form');
    if (form) {
        form.addEventListener('submit', submitDynoRecommendation);
    }
});
