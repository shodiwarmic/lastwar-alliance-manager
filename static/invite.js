// Derive the token from the URL path: /invite/{token}
const INVITE_TOKEN = window.location.pathname.split('/')[2];

const usernameInput = document.getElementById('username');
const passwordInput = document.getElementById('password');
const confirmInput = document.getElementById('confirm-password');
const submitBtn = document.getElementById('submit-btn');
const matchStatus = document.getElementById('match-status');
const usernameError = document.getElementById('username-error');
const serverError = document.getElementById('server-error');
const ruleItems = document.querySelectorAll('#password-rules li[data-rule]');

const usernameRe = /^[a-zA-Z0-9._\-]{3,30}$/;

function validateUsername() {
    if (!usernameInput) return true;
    const val = usernameInput.value.trim();
    if (!usernameRe.test(val)) {
        usernameError.textContent = 'Username must be 3\u201330 characters: letters, numbers, . _ - only';
        usernameError.style.display = 'block';
        return false;
    }
    usernameError.style.display = 'none';
    return true;
}

function checkRule(rule, password) {
    switch (rule) {
        case 'length':  return password.length >= (window.PWD_MIN_LENGTH || 8);
        case 'upper':   return /[A-Z]/.test(password);
        case 'lower':   return /[a-z]/.test(password);
        case 'number':  return /[0-9]/.test(password);
        case 'special': return /[^a-zA-Z0-9]/.test(password);
        default:        return true;
    }
}

function updateRules() {
    const pwd = passwordInput ? passwordInput.value : '';
    let allMet = true;
    ruleItems.forEach(li => {
        const rule = li.dataset.rule;
        const met = checkRule(rule, pwd);
        if (!met) allMet = false;
        li.textContent = (met ? '\u2705 ' : '\u274c ') + li.textContent.replace(/^[\u2705\u274c\u26aa] /, '');
        li.style.color = met ? '#28a745' : '#dc3545';
    });
    return allMet;
}

function updateMatchStatus() {
    if (!confirmInput || confirmInput.value.length === 0) {
        matchStatus.style.display = 'none';
        return false;
    }
    matchStatus.style.display = 'block';
    const match = passwordInput.value === confirmInput.value;
    matchStatus.textContent = match ? '\u2705 Passwords match' : '\u274c Passwords do not match';
    matchStatus.style.color = match ? '#28a745' : '#dc3545';
    return match;
}

function updateSubmitButton() {
    const usernameOk = usernameRe.test((usernameInput ? usernameInput.value.trim() : ''));
    const rulesOk = updateRules();
    const matchOk = updateMatchStatus();
    const enabled = usernameOk && rulesOk && matchOk;
    submitBtn.disabled = !enabled;
    submitBtn.style.opacity = enabled ? '1' : '0.6';
    submitBtn.style.cursor = enabled ? 'pointer' : 'not-allowed';
}

if (usernameInput) {
    usernameInput.addEventListener('blur', validateUsername);
    usernameInput.addEventListener('input', updateSubmitButton);
}
if (passwordInput) passwordInput.addEventListener('input', updateSubmitButton);
if (confirmInput)  confirmInput.addEventListener('input', updateSubmitButton);

document.addEventListener('DOMContentLoaded', () => {
    if (usernameInput) usernameInput.focus();
    updateSubmitButton();
});

const form = document.getElementById('invite-form');
if (form) {
    form.addEventListener('submit', async (e) => {
        e.preventDefault();
        if (!validateUsername()) return;

        submitBtn.disabled = true;
        submitBtn.style.opacity = '0.6';
        submitBtn.textContent = 'Creating account\u2026';
        serverError.style.display = 'none';

        try {
            const resp = await fetch(`/invite/${INVITE_TOKEN}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    username: usernameInput.value.trim(),
                    password: passwordInput.value,
                    confirm_password: confirmInput.value,
                }),
            });

            if (resp.redirected) {
                window.location.href = resp.url;
                return;
            }

            if (resp.ok || resp.status === 303) {
                window.location.href = '/';
                return;
            }

            const msg = await resp.text();
            serverError.textContent = msg || 'An error occurred. Please try again.';
            serverError.style.display = 'block';
        } catch (err) {
            serverError.textContent = 'Network error. Please try again.';
            serverError.style.display = 'block';
        } finally {
            submitBtn.disabled = false;
            submitBtn.style.opacity = '1';
            submitBtn.textContent = 'Create Account \u0026 Log In';
            updateSubmitButton();
        }
    });
}
