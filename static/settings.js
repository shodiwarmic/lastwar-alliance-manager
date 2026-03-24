const API_BASE = '/api';
const SETTINGS_URL = `${API_BASE}/settings`;

const PERM_ROWS = [
    { key: 'manage_members', label: 'Manage Roster (Home)' },
    { key: 'view_dyno', label: 'View Shoutouts' }, 
    { key: 'manage_dyno', label: 'Manage Shoutouts' },
    { key: 'view_anonymous_authors', label: 'View Anonymous Authors' },
    { key: 'view_rankings', label: 'View Analytics Dashboard' },
    { key: 'view_storm', label: 'View Desert Storm' }, 
    { key: 'manage_storm', label: 'Manage Desert Storm' },
    { key: 'view_vs_points', label: 'View VS Points' }, 
    { key: 'manage_vs_points', label: 'Manage VS Points' },
    { key: 'view_upload', label: 'Access OCR Upload Tool' },
    { key: 'view_files', label: 'View Alliance Files' },
    { key: 'upload_files', label: 'Upload Alliance Files' },
    { key: 'manage_files', label: 'Manage Alliance Files' },
    { key: 'view_schedule', label: 'View Schedule' },
    { key: 'manage_schedule', label: 'Manage Schedule' },
    { key: 'manage_settings', label: 'Access Settings Tab' }
];

let isR5OrAdmin = false;

async function fetchPermissions() {
    try {
        const response = await fetch(`${API_BASE}/check-auth`);
        if (response.ok) {
            const data = await response.json();
            isR5OrAdmin = data.permissions?.manage_settings || false;
        }
    } catch (error) {
        console.error('Auth check error:', error);
    }
}

async function loadSettings() {
    try {
        const response = await fetch(SETTINGS_URL);
        if (!response.ok) throw new Error('Failed to load settings');
        
        const settings = await response.json();
        
        document.getElementById('award-first').value = settings.award_first_points || 3;
        document.getElementById('award-second').value = settings.award_second_points || 2;
        document.getElementById('award-third').value = settings.award_third_points || 1;
        document.getElementById('max-hq-level').value = settings.max_hq_level || 35;
        document.getElementById('settings-login-message').value = settings.login_message || '';

        const minLen = document.getElementById('pwd-min-length');
        if (minLen) {
            minLen.value = settings.pwd_min_length || 12;
            document.getElementById('pwd-history-count').value = settings.pwd_history_count ?? 4;
            document.getElementById('pwd-validity-days').value = settings.pwd_validity_days ?? 180;
            document.getElementById('pwd-require-special').checked = settings.pwd_require_special;
            document.getElementById('pwd-require-upper').checked = settings.pwd_require_upper;
            document.getElementById('pwd-require-lower').checked = settings.pwd_require_lower;
            document.getElementById('pwd-require-number').checked = settings.pwd_require_number;
        }

        if (settings.storm_timezones) {
            const activeZones = settings.storm_timezones.split(',');
            document.querySelectorAll('.tz-checkbox').forEach(cb => {
                cb.checked = activeZones.includes(cb.value);
            });
        }

        const dstCheckbox = document.getElementById('storm_respect_dst');
        if (dstCheckbox && settings.storm_respect_dst !== undefined) {
            dstCheckbox.checked = settings.storm_respect_dst;
        }
        
        const powerTrackingEnabled = settings.power_tracking_enabled || false;
        const powerTrackingCheckbox = document.getElementById('power-tracking-enabled');
        if (powerTrackingCheckbox) {
            powerTrackingCheckbox.checked = powerTrackingEnabled;
        }
        togglePowerUploadSection(powerTrackingEnabled);

        const squadCheckbox = document.getElementById('squad-tracking-enabled');
        if (squadCheckbox) squadCheckbox.checked = settings.squad_tracking_enabled === true;

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
    }
}

function togglePowerUploadSection(enabled) {
    const uploadLink = document.getElementById('power-upload-link');
    if (uploadLink) uploadLink.style.display = enabled ? 'block' : 'none';
}

document.addEventListener('DOMContentLoaded', async () => {
    const settingsForm = document.getElementById('settings-form');
    
    if (settingsForm) {
        await fetchPermissions();
        await loadSettings();
        
        settingsForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            
            if (!isR5OrAdmin) {
                alert('You do not have permission to modify settings.');
                return;
            }
            
            const selectedZones = Array.from(document.querySelectorAll('.tz-checkbox:checked'))
                .map(cb => cb.value).join(',');
    
            // We pass placeholder 0/empty values for the dead Train settings so the Go Backend API doesn't panic
            const settings = {
                award_first_points: parseInt(document.getElementById('award-first').value) || 0,
                award_second_points: parseInt(document.getElementById('award-second').value) || 0,
                award_third_points: parseInt(document.getElementById('award-third').value) || 0,
                recent_conductor_penalty_days: 0,
                above_average_conductor_penalty: 0,
                r4r5_rank_boost: 0,
                first_time_conductor_boost: 0,
                recommendation_points: 0,
                schedule_message_template: "",
                daily_message_template: "",
                login_message: document.getElementById('settings-login-message').value,
                max_hq_level: parseInt(document.getElementById('max-hq-level').value, 10),
                power_tracking_enabled: document.getElementById('power-tracking-enabled').checked,
                squad_tracking_enabled: document.getElementById('squad-tracking-enabled').checked,
                storm_timezones: selectedZones,
                storm_respect_dst: document.getElementById('storm_respect_dst').checked
            };

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
                
                if (!response.ok) throw new Error(await response.text());
                alert('✅ Settings saved successfully!');
            } catch (error) {
                console.error('Error saving settings:', error);
                alert('❌ Failed to save settings: ' + error.message);
            }
        });

        const powerToggle = document.getElementById('power-tracking-enabled');
        if (powerToggle) {
            powerToggle.addEventListener('change', (e) => togglePowerUploadSection(e.target.checked));
        }
    }
});