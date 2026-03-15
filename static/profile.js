const API_BASE = '/api';

let currentPolicy = {
    min_length: 12,
    require_upper: false,
    require_lower: false,
    require_number: false,
    require_special: false
};

async function loadProfileInfo() {
    try {
        const response = await fetch(`${API_BASE}/check-auth`);
        if (response.ok) {
            const data = await response.json();
            
            const profileUsername = document.getElementById('profile-username');
            if (profileUsername) profileUsername.textContent = data.username;
            
            const profileRole = document.getElementById('profile-role');
            if (profileRole) profileRole.textContent = data.is_admin ? 'Administrator' : 'Member';
            
            const profileMember = document.getElementById('profile-member');
            const profileMemberInfo = document.getElementById('profile-member-info');
            
            if (data.rank && profileMember && profileMemberInfo) {
                profileMember.textContent = data.rank;
                profileMemberInfo.style.display = 'block';
            }
        }
    } catch (error) {
        console.error('Failed to load profile info:', error);
    }
}

// Renamed from loadPasswordPolicy to handle both passwords and game limits
async function loadSystemSettings() {
    try {
        const response = await fetch(`${API_BASE}/settings`);
        if (response.ok) {
            const settings = await response.json();
            
            // 1. Password Policy
            currentPolicy = {
                min_length: settings.pwd_min_length || 12,
                require_upper: settings.pwd_require_upper,
                require_lower: settings.pwd_require_lower,
                require_number: settings.pwd_require_number,
                require_special: settings.pwd_require_special
            };
            
            const rulesList = document.getElementById('password-rules');
            if (rulesList) {
                rulesList.innerHTML = `<li id="rule-length" data-text="Minimum ${currentPolicy.min_length} characters" style="color: #666; transition: color 0.3s;">⚪ Minimum ${currentPolicy.min_length} characters</li>`;
                if (currentPolicy.require_upper) rulesList.innerHTML += `<li id="rule-upper" data-text="At least one uppercase letter" style="color: #666; transition: color 0.3s;">⚪ At least one uppercase letter</li>`;
                if (currentPolicy.require_lower) rulesList.innerHTML += `<li id="rule-lower" data-text="At least one lowercase letter" style="color: #666; transition: color 0.3s;">⚪ At least one lowercase letter</li>`;
                if (currentPolicy.require_number) rulesList.innerHTML += `<li id="rule-number" data-text="At least one number" style="color: #666; transition: color 0.3s;">⚪ At least one number</li>`;
                if (currentPolicy.require_special) rulesList.innerHTML += `<li id="rule-special" data-text="At least one special character" style="color: #666; transition: color 0.3s;">⚪ At least one special character</li>`;
            }
            checkPasswordRequirements();

            // 2. Game Limits (Set dynamic HQ Max)
            const hqInput = document.getElementById('stat-level');
            if (hqInput && settings.max_hq_level) {
                hqInput.max = settings.max_hq_level;
            }
        }
    } catch (error) {
        console.error('Failed to load system settings:', error);
    }
}

// Dynamically enforces troop restrictions based on current HQ Level input
function enforceTroopLevel() {
    const hqLevel = parseInt(document.getElementById('stat-level').value) || 0;
    const troopSelect = document.getElementById('stat-troop-level');
    if (!troopSelect) return;

    const currentSelected = parseInt(troopSelect.value) || 0;
    const troopRequirements = {
        1: 1, 2: 4, 3: 6, 4: 10, 5: 14, 6: 17,
        7: 20, 8: 24, 9: 27, 10: 30, 11: 35
    };

    let maxAllowedTroop = 0;
    
    // Loop through options and disable illegal tiers
    Array.from(troopSelect.options).forEach(option => {
        const tLevel = parseInt(option.value);
        if (tLevel === 0 || isNaN(tLevel)) {
            option.disabled = false; // "Unknown" is always valid
            return;
        }
        
        const reqHQ = troopRequirements[tLevel];
        if (hqLevel >= reqHQ) {
            option.disabled = false;
            maxAllowedTroop = Math.max(maxAllowedTroop, tLevel);
        } else {
            option.disabled = true;
        }
    });

    // If current selection became illegal, downgrade it gracefully
    if (currentSelected > maxAllowedTroop && currentSelected !== 0) {
        troopSelect.value = maxAllowedTroop;
    }
}

async function loadGameStats() {
    try {
        const response = await fetch(`${API_BASE}/profile/me`);
        if (response.ok) {
            const data = await response.json();
            
            document.getElementById('game-stats-section').style.display = 'block';
            document.getElementById('no-member-warning').style.display = 'none';

            document.getElementById('stat-name').value = data.name || '';
            document.getElementById('stat-rank').value = data.rank || '';
            document.getElementById('stat-eligible').checked = data.eligible || false;
            document.getElementById('stat-level').value = data.level || '';
            document.getElementById('stat-power').value = data.power || '';
            document.getElementById('stat-troop-level').value = data.troop_level || 0;
            document.getElementById('stat-squad-type').value = data.squad_type || '';
            document.getElementById('stat-squad-power').value = data.squad_power || '';
            document.getElementById('stat-profession').value = data.profession || '';

            // Run enforcement immediately on load
            enforceTroopLevel();
        } else if (response.status === 404 || response.status === 403) {
            document.getElementById('game-stats-section').style.display = 'none';
            document.getElementById('no-member-warning').style.display = 'block';
        }
    } catch (error) {
        console.error('Failed to load game stats:', error);
    }
}

function checkPasswordRequirements() {
    const newPwd = document.getElementById('new-password').value;
    const confirmPwd = document.getElementById('confirm-password').value;
    const submitBtn = document.getElementById('submit-pwd-btn');
    const matchStatus = document.getElementById('match-status');
    
    let allRulesMet = true;

    function toggleRule(id, isMet) {
        const el = document.getElementById(id);
        if (!el) return;
        const baseText = el.getAttribute('data-text');
        if (isMet) {
            el.innerHTML = `✅ ${baseText}`;
            el.style.color = '#28a745';
        } else {
            el.innerHTML = `❌ ${baseText}`;
            el.style.color = '#dc3545';
            allRulesMet = false;
        }
    }

    if (newPwd.length > 0) {
        toggleRule('rule-length', newPwd.length >= currentPolicy.min_length);
        if (currentPolicy.require_upper) toggleRule('rule-upper', /[A-Z]/.test(newPwd));
        if (currentPolicy.require_lower) toggleRule('rule-lower', /[a-z]/.test(newPwd));
        if (currentPolicy.require_number) toggleRule('rule-number', /[0-9]/.test(newPwd));
        if (currentPolicy.require_special) toggleRule('rule-special', /[^a-zA-Z0-9]/.test(newPwd));
    } else {
        allRulesMet = false;
        const rulesList = document.getElementById('password-rules');
        const listItems = rulesList.getElementsByTagName('li');
        for (let li of listItems) {
            li.innerHTML = `⚪ ${li.getAttribute('data-text')}`;
            li.style.color = '#666';
        }
    }

    let passwordsMatch = false;
    if (confirmPwd.length > 0) {
        matchStatus.style.display = 'block';
        if (newPwd === confirmPwd) {
            matchStatus.innerHTML = '✅ Passwords match';
            matchStatus.style.color = '#28a745';
            passwordsMatch = true;
        } else {
            matchStatus.innerHTML = '❌ Passwords do not match';
            matchStatus.style.color = '#dc3545';
        }
    } else {
        matchStatus.style.display = 'none';
    }

    if (allRulesMet && passwordsMatch && newPwd.length > 0) {
        submitBtn.disabled = false;
        submitBtn.style.opacity = '1';
        submitBtn.style.cursor = 'pointer';
    } else {
        submitBtn.disabled = true;
        submitBtn.style.opacity = '0.6';
        submitBtn.style.cursor = 'not-allowed';
    }
}

document.addEventListener('DOMContentLoaded', async () => {
    const passwordForm = document.getElementById('password-form');
    const statsForm = document.getElementById('game-stats-form');
    const newPwdInput = document.getElementById('new-password');
    const confirmPwdInput = document.getElementById('confirm-password');
    const hqInput = document.getElementById('stat-level');
    
    if (passwordForm) {
        await loadProfileInfo();
        await loadSystemSettings();
        await loadGameStats();
        
        newPwdInput.addEventListener('input', checkPasswordRequirements);
        confirmPwdInput.addEventListener('input', checkPasswordRequirements);
        
        // Listen for HQ input changes to cascade troop restrictions
        if (hqInput) {
            hqInput.addEventListener('input', enforceTroopLevel);
        }
        
        passwordForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            
            const currentPassword = document.getElementById('current-password').value;
            const newPassword = document.getElementById('new-password').value;
            const submitBtn = document.getElementById('submit-pwd-btn');
            
            submitBtn.disabled = true;
            submitBtn.textContent = 'Updating...';
            
            try {
                const response = await fetch(`${API_BASE}/change-password`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        current_password: currentPassword,
                        new_password: newPassword
                    })
                });
                
                if (!response.ok) {
                    const errorText = await response.text();
                    let errorMessage = errorText;
                    try {
                        const errObj = JSON.parse(errorText);
                        errorMessage = errObj.message || errorText;
                    } catch(e) {}
                    throw new Error(errorMessage);
                }
                
                alert('✅ Password changed successfully!');
                passwordForm.reset();
                checkPasswordRequirements();
                
            } catch (error) {
                console.error('Error changing password:', error);
                alert('❌ Failed to change password: ' + error.message);
            } finally {
                submitBtn.disabled = false;
                submitBtn.textContent = '🔒 Change Password';
            }
        });
        
        if (statsForm) {
            statsForm.addEventListener('submit', async (e) => {
                e.preventDefault();
                
                const submitBtn = document.getElementById('submit-stats-btn');
                submitBtn.disabled = true;
                submitBtn.textContent = 'Saving...';
                
                try {
                    const response = await fetch(`${API_BASE}/profile/me`, {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({
                            name: document.getElementById('stat-name').value,
                            level: parseInt(document.getElementById('stat-level').value) || 0,
                            power: parseInt(document.getElementById('stat-power').value) || 0,
                            troop_level: parseInt(document.getElementById('stat-troop-level').value) || 0,
                            squad_type: document.getElementById('stat-squad-type').value,
                            squad_power: parseInt(document.getElementById('stat-squad-power').value) || 0,
                            profession: document.getElementById('stat-profession').value
                        })
                    });
                    
                    if (!response.ok) throw new Error(await response.text());
                    
                    alert('✅ Game stats updated successfully!');
                } catch (error) {
                    console.error('Error updating stats:', error);
                    alert('❌ Failed to update stats.');
                } finally {
                    submitBtn.disabled = false;
                    submitBtn.textContent = '💾 Save Stats';
                }
            });
        }
    }
});