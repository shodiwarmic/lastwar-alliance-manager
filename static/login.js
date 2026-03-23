let currentPolicy = null;

// Reactive UI logic for real-time validation on the Login Modal
function checkForcePasswordRequirements() {
    if (!currentPolicy) return;

    const newPwd = document.getElementById('force-new-password').value;
    const confirmPwd = document.getElementById('force-confirm-password').value;
    const submitBtn = document.getElementById('force-submit-btn');
    const matchStatus = document.getElementById('force-match-status');

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

// Attach the real-time input listeners
document.getElementById('force-new-password').addEventListener('input', checkForcePasswordRequirements);
document.getElementById('force-confirm-password').addEventListener('input', checkForcePasswordRequirements);

document.getElementById('login-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const username = document.getElementById('username').value.trim();
    const password = document.getElementById('password').value;
    const errorDiv = document.getElementById('error-message');
    const successDiv = document.getElementById('success-message');
    const loginBtn = document.getElementById('login-btn');

    errorDiv.style.display = 'none';
    successDiv.style.display = 'none';

    if (!username || !password) {
        errorDiv.textContent = 'Please enter both username and password';
        errorDiv.style.display = 'block';
        return;
    }

    loginBtn.disabled = true;
    loginBtn.textContent = 'Logging in...';

    try {
        const response = await fetch('/api/login', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ username, password }),
        });

        if (response.ok) {
            const data = await response.json();

            if (data.requires_password_change) {
                currentPolicy = data.policy;

                // Dynamically build the reactive data-text list
                const rulesList = document.getElementById('password-rules');
                rulesList.innerHTML = `<li id="rule-length" data-text="Minimum ${currentPolicy.min_length} characters" style="color: #666; transition: color 0.3s;">⚪ Minimum ${currentPolicy.min_length} characters</li>`;
                if (currentPolicy.require_upper) rulesList.innerHTML += `<li id="rule-upper" data-text="At least one uppercase letter" style="color: #666; transition: color 0.3s;">⚪ At least one uppercase letter</li>`;
                if (currentPolicy.require_lower) rulesList.innerHTML += `<li id="rule-lower" data-text="At least one lowercase letter" style="color: #666; transition: color 0.3s;">⚪ At least one lowercase letter</li>`;
                if (currentPolicy.require_number) rulesList.innerHTML += `<li id="rule-number" data-text="At least one number" style="color: #666; transition: color 0.3s;">⚪ At least one number</li>`;
                if (currentPolicy.require_special) rulesList.innerHTML += `<li id="rule-special" data-text="At least one special character" style="color: #666; transition: color 0.3s;">⚪ At least one special character</li>`;

                // Evaluate immediately in case browser auto-filled it
                checkForcePasswordRequirements();

                document.getElementById('force-password-modal').style.display = 'flex';

                loginBtn.disabled = false;
                loginBtn.textContent = 'Login';
            } else {
                successDiv.textContent = 'Login successful! Redirecting...';
                successDiv.style.display = 'block';
                setTimeout(() => { window.location.href = '/'; }, 1000);
            }
        } else {
            errorDiv.textContent = 'Invalid username or password';
            errorDiv.style.display = 'block';
            loginBtn.disabled = false;
            loginBtn.textContent = 'Login';
        }
    } catch (error) {
        console.error('Login error:', error);
        errorDiv.textContent = 'An error occurred. Please try again.';
        errorDiv.style.display = 'block';
        loginBtn.disabled = false;
        loginBtn.textContent = 'Login';
    }
});

// Handle the Force Password Change Form Submission
document.getElementById('force-password-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const newPassword = document.getElementById('force-new-password').value;
    const errorDiv = document.getElementById('force-password-error');
    const submitBtn = document.getElementById('force-submit-btn');

    errorDiv.style.display = 'none';
    submitBtn.disabled = true;
    submitBtn.textContent = 'Updating...';

    try {
        const response = await fetch('/api/force-change-password', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ new_password: newPassword })
        });

        if (response.ok) {
            submitBtn.textContent = 'Success!';
            setTimeout(() => {
                window.location.href = '/';
            }, 500);
        } else {
            const errorData = await response.json();
            errorDiv.textContent = errorData.message || 'Failed to change password.';
            errorDiv.style.display = 'block';
            submitBtn.disabled = false;
            submitBtn.textContent = 'Update & Login';
        }
    } catch (error) {
        errorDiv.textContent = 'A network error occurred. Please try again.';
        errorDiv.style.display = 'block';
        submitBtn.disabled = false;
        submitBtn.textContent = 'Update & Login';
    }
});
