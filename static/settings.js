// static/settings.js
const API_BASE = '/api';
const SETTINGS_URL = `${API_BASE}/settings`;
const PERM_ROWS = [
    { key: 'manage_members', label: 'Manage Roster (Home)' },
    { key: 'view_train', label: 'View Train Schedule' }, { key: 'manage_train', label: 'Manage Train Schedule' },
    { key: 'view_awards', label: 'View Awards' }, { key: 'manage_awards', label: 'Manage Awards' },
    { key: 'view_recs', label: 'View Recommendations' }, { key: 'manage_recs', label: 'Manage Recommendations' },
    { key: 'view_dyno', label: 'View Dyno' }, { key: 'manage_dyno', label: 'Manage Dyno' },
    { key: 'view_rankings', label: 'View Rankings' },
    { key: 'view_storm', label: 'View Desert Storm' }, { key: 'manage_storm', label: 'Manage Desert Storm' },
    { key: 'view_vs_points', label: 'View VS Points' }, { key: 'manage_vs_points', label: 'Manage VS Points' },
    { key: 'view_upload', label: 'Access OCR Upload Tool' },
    { key: 'manage_settings', label: 'Access Settings Tab' }
];

let isR5OrAdmin = false;

let permissions = {};

async function fetchPermissions() {
    try {
        const response = await fetch(`${API_BASE}/check-auth`);
        if (response.ok) {
            const data = await response.json();
            permissions = data.permissions || {};
            isR5OrAdmin = permissions.manage_settings || false;
        }
    } catch (error) {
        console.error('Auth check error:', error);
    }
}

// Load settings
async function loadSettings() {
    try {
        const response = await fetch(SETTINGS_URL);
        if (!response.ok) throw new Error('Failed to load settings');
        
        const settings = await response.json();
        
        document.getElementById('award-first').value = settings.award_first_points;
        document.getElementById('award-second').value = settings.award_second_points;
        document.getElementById('award-third').value = settings.award_third_points;
        document.getElementById('recent-conductor-days').value = settings.recent_conductor_penalty_days;
        document.getElementById('above-average-penalty').value = settings.above_average_conductor_penalty;
        document.getElementById('r4r5-rank-boost').value = settings.r4r5_rank_boost;
        document.getElementById('first-time-boost').value = settings.first_time_conductor_boost || 5;
        document.getElementById('schedule-message-template').value = settings.schedule_message_template || 'Train Schedule - Week {WEEK}\n\n{SCHEDULES}\n\nNext in line:\n{NEXT_3}';
        document.getElementById('daily-message-template').value = settings.daily_message_template || 'ALL ABOARD! Daily Train Assignment\n\nDate: {DATE}\n\nToday\'s Conductor: {CONDUCTOR_NAME} ({CONDUCTOR_RANK})\nBackup Engineer: {BACKUP_NAME} ({BACKUP_RANK})\n\nDEPARTURE SCHEDULE:\n- 15:00 ST (17:00 UK) - Conductor {CONDUCTOR_NAME}, please request train assignment in alliance chat\n- 16:30 ST (18:30 UK) - If conductor hasn\'t shown up, Backup {BACKUP_NAME} takes over and assigns train to themselves\n\nRemember: Communication is key! Let the alliance know if you can\'t make it.\n\nAll aboard for another successful run!';
        document.getElementById('max-hq-level').value = settings.max_hq_level || 35;
        document.getElementById('settings-login-message').value = settings.login_message || '';

        // Load Password Policy Settings (Admin Only)
        const minLen = document.getElementById('pwd-min-length');
        if (minLen) {
            minLen.value = settings.pwd_min_length || 12;
            document.getElementById('pwd-history-count').value = settings.pwd_history_count !== undefined ? settings.pwd_history_count : 4;
            document.getElementById('pwd-validity-days').value = settings.pwd_validity_days !== undefined ? settings.pwd_validity_days : 180;
            document.getElementById('pwd-require-special').checked = settings.pwd_require_special;
            document.getElementById('pwd-require-upper').checked = settings.pwd_require_upper;
            document.getElementById('pwd-require-lower').checked = settings.pwd_require_lower;
            document.getElementById('pwd-require-number').checked = settings.pwd_require_number;
        }

        // Load Storm Timezones
        if (settings.storm_timezones) {
            const activeZones = settings.storm_timezones.split(',');
            document.querySelectorAll('.tz-checkbox').forEach(cb => {
                cb.checked = activeZones.includes(cb.value);
            });
        }

        // Load DST Preference
        const dstCheckbox = document.getElementById('storm_respect_dst');
        if (dstCheckbox && settings.storm_respect_dst !== undefined) {
            dstCheckbox.checked = settings.storm_respect_dst;
        }
        
        // Power tracking
        const powerTrackingEnabled = settings.power_tracking_enabled || false;
        const powerTrackingCheckbox = document.getElementById('power-tracking-enabled');
        if (powerTrackingCheckbox) {
            powerTrackingCheckbox.checked = powerTrackingEnabled;
        }
        togglePowerUploadSection(powerTrackingEnabled);

        const squadCheckbox = document.getElementById('squad-tracking-enabled');
        if (squadCheckbox) squadCheckbox.checked = settings.squad_tracking_enabled === true;

        // --- Fetch and Build RBAC Matrix ---
        const matrixRes = await fetch(`${API_BASE}/permissions`);
        if (matrixRes.ok) {
            const matrix = await matrixRes.json();
            const tbody = document.querySelector('#permissions-matrix tbody');
            tbody.innerHTML = '';
            
            PERM_ROWS.forEach((row, index) => {
                const bgClass = index % 2 === 0 ? '' : 'background: var(--bg-secondary);';
                let tr = `<tr style="border-bottom: 1px solid var(--border-color); ${bgClass}">
                            <td style="text-align: left; padding: 10px 12px; font-weight: 500;">${row.label}</td>`;
                
                ['R5', 'R4', 'R3', 'R2', 'R1'].forEach(rank => {
                    const rankData = matrix.find(m => m.rank === rank) || {};
                    const checked = rankData[row.key] ? 'checked' : '';
                    tr += `<td style="padding: 10px;"><input type="checkbox" class="perm-checkbox" data-rank="${rank}" data-key="${row.key}" ${checked} style="width: 18px; height: 18px; cursor: pointer; accent-color: var(--primary-color);"></td>`;
                });
                tr += '</tr>';
                tbody.innerHTML += tr;
            });
        }
    } catch (error) {
        console.error('Error loading settings:', error);
        alert('Failed to load settings');
    }
}

// Power tracking toggle
function togglePowerUploadSection(enabled) {
    const uploadLink = document.getElementById('power-upload-link');
    if (uploadLink) {
        uploadLink.style.display = enabled ? 'block' : 'none';
    }
}

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    const settingsForm = document.getElementById('settings-form');
    
    // Guard: Only run if we are actually on the Settings page
    if (settingsForm) {
        await fetchPermissions();
        await loadSettings();
        
        // Save settings
        settingsForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            
            if (!isR5OrAdmin) {
                alert('You do not have permission to modify settings. Only R5 members and admins can do this.');
                return;
            }
            
            // Gather checked timezones
            const selectedZones = Array.from(document.querySelectorAll('.tz-checkbox:checked'))
                .map(cb => cb.value)
                .join(',');
    
            const settings = {
                award_first_points: parseInt(document.getElementById('award-first').value),
                award_second_points: parseInt(document.getElementById('award-second').value),
                award_third_points: parseInt(document.getElementById('award-third').value),
                recent_conductor_penalty_days: parseInt(document.getElementById('recent-conductor-days').value),
                above_average_conductor_penalty: parseInt(document.getElementById('above-average-penalty').value),
                r4r5_rank_boost: parseInt(document.getElementById('r4r5-rank-boost').value),
                first_time_conductor_boost: parseInt(document.getElementById('first-time-boost').value),
                schedule_message_template: document.getElementById('schedule-message-template').value,
                daily_message_template: document.getElementById('daily-message-template').value,
                login_message: document.getElementById('settings-login-message').value,
                max_hq_level: parseInt(document.getElementById('max-hq-level').value, 10),
                power_tracking_enabled: document.getElementById('power-tracking-enabled').checked,
                squad_tracking_enabled: document.getElementById('squad-tracking-enabled').checked,
                storm_timezones: selectedZones,
                storm_respect_dst: document.getElementById('storm_respect_dst').checked
            };

            // Gather and Save RBAC Matrix Data
            const newMatrix = ['R5', 'R4', 'R3', 'R2', 'R1'].map(rank => {
                const obj = { rank: rank };
                document.querySelectorAll(`.perm-checkbox[data-rank="${rank}"]`).forEach(cb => {
                    obj[cb.dataset.key] = cb.checked;
                });
                return obj;
            });
            
            try {
                await fetch(`${API_BASE}/permissions`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(newMatrix)
                });
            } catch (matrixErr) {
                console.error("Failed to save permissions matrix:", matrixErr);
            }
            
            // Append Admin-only Password Policy Settings
            const minLen = document.getElementById('pwd-min-length');
            if (minLen) {
                settings.pwd_min_length = parseInt(minLen.value);
                settings.pwd_history_count = parseInt(document.getElementById('pwd-history-count').value);
                settings.pwd_validity_days = parseInt(document.getElementById('pwd-validity-days').value);
                settings.pwd_require_special = document.getElementById('pwd-require-special').checked;
                settings.pwd_require_upper = document.getElementById('pwd-require-upper').checked;
                settings.pwd_require_lower = document.getElementById('pwd-require-lower').checked;
                settings.pwd_require_number = document.getElementById('pwd-require-number').checked;
            }

            try {
                const response = await fetch(SETTINGS_URL, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(settings)
                });
                
                if (!response.ok) {
                    const error = await response.text();
                    throw new Error(error);
                }
                
                alert('✅ Settings saved successfully!');
            } catch (error) {
                console.error('Error saving settings:', error);
                alert('❌ Failed to save settings: ' + error.message);
            }
        });
        
        // Reset to defaults
        const resetBtn = document.getElementById('reset-btn');
        if (resetBtn) {
            resetBtn.addEventListener('click', () => {
                if (confirm('Reset all settings to default values?')) {
                    document.getElementById('award-first').value = 3;
                    document.getElementById('award-second').value = 2;
                    document.getElementById('award-third').value = 1;
                    
                    const recPoints = document.getElementById('recommendation-points');
                    if (recPoints) recPoints.value = 10;
                    
                    document.getElementById('recent-conductor-days').value = 30;
                    document.getElementById('above-average-penalty').value = 10;
                    document.getElementById('r4r5-rank-boost').value = 5;
                    document.getElementById('first-time-boost').value = 5;
                    document.getElementById('schedule-message-template').value = 'Train Schedule - Week {WEEK}\n\n{SCHEDULES}\n\nNext in line:\n{NEXT_3}';
                    document.getElementById('daily-message-template').value = 'ALL ABOARD! Daily Train Assignment\n\nDate: {DATE}\n\nToday\'s Conductor: {CONDUCTOR_NAME} ({CONDUCTOR_RANK})\nBackup Engineer: {BACKUP_NAME} ({BACKUP_RANK})\n\nDEPARTURE SCHEDULE:\n- 15:00 ST (17:00 UK) - Conductor {CONDUCTOR_NAME}, please request train assignment in alliance chat\n- 16:30 ST (18:30 UK) - If conductor hasn\'t shown up, Backup {BACKUP_NAME} takes over and assigns train to themselves\n\nRemember: Communication is key! Let the alliance know if you can\'t make it.\n\nAll aboard for another successful run!';
                    document.getElementById('max-hq-level').value = 35;
                    document.getElementById('settings-login-message').value = `<strong>Default Credentials:</strong>\nUsername: <code>admin</code><br>\nPassword: <code>admin123</code>`;

                    // Reset Password Policy Defaults (Admin Only)
                    const minLen = document.getElementById('pwd-min-length');
                    if (minLen) {
                        minLen.value = 12;
                        document.getElementById('pwd-history-count').value = 4;
                        document.getElementById('pwd-validity-days').value = 180;
                        document.getElementById('pwd-require-special').checked = false;
                        document.getElementById('pwd-require-upper').checked = false;
                        document.getElementById('pwd-require-lower').checked = false;
                        document.getElementById('pwd-require-number').checked = false;
                    }

                    const powerToggle = document.getElementById('power-tracking-enabled');
                    if (powerToggle) powerToggle.checked = false;
                    document.getElementById('squad-tracking-enabled').checked = false;
                    
                    togglePowerUploadSection(false);
                }
            });
        }

        // Power tracking toggle listener
        const powerToggle = document.getElementById('power-tracking-enabled');
        if (powerToggle) {
            powerToggle.addEventListener('change', (e) => {
                togglePowerUploadSection(e.target.checked);
            });
        }
    }
});