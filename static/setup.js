// First-run setup page (/setup): redeem the setup key and create the first administrator.
// Same validation shape as invite.js; the server re-checks everything.
const cfg = document.getElementById('page-config').dataset;

const keyInput = document.getElementById('setup-key');
const nameInput = document.getElementById('alliance-name');
const tagInput = document.getElementById('alliance-tag');
const serverInput = document.getElementById('server-number');
const usernameInput = document.getElementById('username');
const passwordInput = document.getElementById('password');
const confirmInput = document.getElementById('confirm-password');
const submitBtn = document.getElementById('submit-btn');
const matchStatus = document.getElementById('match-status');
const usernameError = document.getElementById('username-error');
const serverError = document.getElementById('server-error');
const ruleItems = document.querySelectorAll('#password-rules li[data-rule]');

const usernameRe = /^[a-zA-Z0-9._\-]{3,30}$/;
const SUBMIT_LABEL = 'Create Administrator & Log In';

function validateUsername() {
    const val = usernameInput.value.trim();
    if (!usernameRe.test(val)) {
        usernameError.textContent = 'Username must be 3–30 characters: letters, numbers, . _ - only';
        usernameError.style.display = 'block';
        return false;
    }
    usernameError.style.display = 'none';
    return true;
}

function checkRule(rule, password) {
    switch (rule) {
        case 'length':  return password.length >= (Number(cfg.pwdMinLength) || 8);
        case 'upper':   return /[A-Z]/.test(password);
        case 'lower':   return /[a-z]/.test(password);
        case 'number':  return /[0-9]/.test(password);
        case 'special': return /[^a-zA-Z0-9]/.test(password);
        default:        return true;
    }
}

function updateRules() {
    const pwd = passwordInput.value;
    let allMet = true;
    ruleItems.forEach(li => {
        const met = checkRule(li.dataset.rule, pwd);
        if (!met) allMet = false;
        li.textContent = (met ? '✅ ' : '❌ ') + li.textContent.replace(/^[✅❌⚪] /, '');
        li.style.color = met ? 'var(--color-success)' : 'var(--color-danger)';
    });
    return allMet;
}

function updateMatchStatus() {
    if (confirmInput.value.length === 0) {
        matchStatus.style.display = 'none';
        return false;
    }
    matchStatus.style.display = 'block';
    const match = passwordInput.value === confirmInput.value;
    matchStatus.textContent = match ? '✅ Passwords match' : '❌ Passwords do not match';
    matchStatus.style.color = match ? 'var(--color-success)' : 'var(--color-danger)';
    return match;
}

function updateSubmitButton() {
    const keyOk = keyInput.value.trim().length > 0;
    const identityOk = nameInput.value.trim().length > 0 && tagInput.value.trim().length > 0;
    const usernameOk = usernameRe.test(usernameInput.value.trim());
    const rulesOk = updateRules();
    const matchOk = updateMatchStatus();
    const enabled = keyOk && identityOk && usernameOk && rulesOk && matchOk;
    submitBtn.disabled = !enabled;
    submitBtn.style.opacity = enabled ? '1' : '0.6';
    submitBtn.style.cursor = enabled ? 'pointer' : 'not-allowed';
}

keyInput.addEventListener('input', updateSubmitButton);
nameInput.addEventListener('input', updateSubmitButton);
tagInput.addEventListener('input', updateSubmitButton);
usernameInput.addEventListener('blur', validateUsername);
usernameInput.addEventListener('input', updateSubmitButton);
passwordInput.addEventListener('input', updateSubmitButton);
confirmInput.addEventListener('input', updateSubmitButton);

document.addEventListener('DOMContentLoaded', () => {
    keyInput.focus();
    updateSubmitButton();
});

document.getElementById('setup-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!validateUsername()) return;

    submitBtn.disabled = true;
    submitBtn.style.opacity = '0.6';
    submitBtn.textContent = 'Setting up…';
    serverError.style.display = 'none';

    try {
        const resp = await fetch('/api/setup', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                setup_key: keyInput.value.trim(),
                username: usernameInput.value.trim(),
                password: passwordInput.value,
                confirm_password: confirmInput.value,
                alliance_name: nameInput.value.trim(),
                alliance_tag: tagInput.value.trim(),
                server_number: Number(serverInput.value) || 0,
            }),
        });

        if (resp.ok) {
            const body = await resp.json().catch(() => ({}));
            window.location.href = body.redirect || '/';
            return;
        }

        const msg = await resp.text();
        serverError.textContent = msg || 'An error occurred. Please try again.';
        serverError.style.display = 'block';
    } catch (err) {
        serverError.textContent = 'Network error. Please try again.';
        serverError.style.display = 'block';
    }
    submitBtn.textContent = SUBMIT_LABEL;
    updateSubmitButton();
});
