const API_BASE = '/api';

let currentPolicy = {
    min_length: 12,
    require_upper: false,
    require_lower: false,
    require_number: false,
    require_special: false
};

// Fetch user data to populate the profile card
async function loadProfileInfo() {
    try {
        const response = await fetch(`${API_BASE}/check-auth`);
        if (response.ok) {
            const data = await response.json();
            
            // Display profile info
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

// Fetch password policy from settings to dynamically update the UI
async function loadPasswordPolicy() {
    try {
        const response = await fetch(`${API_BASE}/settings`);
        if (response.ok) {
            const settings = await response.json();
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
            
            // Re-evaluate current input just in case the browser auto-filled it
            checkPasswordRequirements();
        }
    } catch (error) {
        console.error('Failed to load password policy:', error);
    }
}

// Reactive UI logic for real-time validation
function checkPasswordRequirements() {
    const newPwd = document.getElementById('new-password').value;
    const confirmPwd = document.getElementById('confirm-password').value;
    const submitBtn = document.getElementById('submit-pwd-btn');
    const matchStatus = document.getElementById('match-status');
    
    let allRulesMet = true;

    // Helper to toggle UI state
    function toggleRule(id, isMet) {
        const el = document.getElementById(id);
        if (!el) return;
        const baseText = el.getAttribute('data-text');
        if (isMet) {
            el.innerHTML = `✅ ${baseText}`;
            el.style.color = '#28a745'; // Green
        } else {
            el.innerHTML = `❌ ${baseText}`;
            el.style.color = '#dc3545'; // Red
            allRulesMet = false;
        }
    }

    // Evaluate rules only if the user has typed something
    if (newPwd.length > 0) {
        toggleRule('rule-length', newPwd.length >= currentPolicy.min_length);
        if (currentPolicy.require_upper) toggleRule('rule-upper', /[A-Z]/.test(newPwd));
        if (currentPolicy.require_lower) toggleRule('rule-lower', /[a-z]/.test(newPwd));
        if (currentPolicy.require_number) toggleRule('rule-number', /[0-9]/.test(newPwd));
        if (currentPolicy.require_special) toggleRule('rule-special', /[^a-zA-Z0-9]/.test(newPwd));
    } else {
        // Reset to neutral if empty
        allRulesMet = false;
        const rulesList = document.getElementById('password-rules');
        const listItems = rulesList.getElementsByTagName('li');
        for (let li of listItems) {
            li.innerHTML = `⚪ ${li.getAttribute('data-text')}`;
            li.style.color = '#666';
        }
    }

    // Evaluate matching status
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

    // Toggle submit button
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

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
    const passwordForm = document.getElementById('password-form');
    const newPwdInput = document.getElementById('new-password');
    const confirmPwdInput = document.getElementById('confirm-password');
    
    // Guard: Only run if we are actually on the Profile page
    if (passwordForm) {
        await loadProfileInfo();
        await loadPasswordPolicy();
        
        // Attach real-time listeners
        newPwdInput.addEventListener('input', checkPasswordRequirements);
        confirmPwdInput.addEventListener('input', checkPasswordRequirements);
        
        // Change password form submission
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
                checkPasswordRequirements(); // Reset the UI indicators
                
            } catch (error) {
                console.error('Error changing password:', error);
                alert('❌ Failed to change password: ' + error.message);
            } finally {
                submitBtn.disabled = false;
                submitBtn.textContent = '🔒 Change Password';
            }
        });
    }
});