// Password reset claim page. Mirrors invite.js minus the username field — the username
// is fixed on a reset and shown read-only by the template.
// Derive the token from the URL path: /reset-password/{token}
const RESET_TOKEN = window.location.pathname.split('/')[2];
const cfg = document.getElementById('page-config').dataset;

const passwordInput = document.getElementById('password');
const confirmInput = document.getElementById('confirm-password');
const submitBtn = document.getElementById('submit-btn');
const matchStatus = document.getElementById('match-status');
const serverError = document.getElementById('server-error');
const ruleItems = document.querySelectorAll('#password-rules li[data-rule]');

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
    const pwd = passwordInput ? passwordInput.value : '';
    let allMet = true;
    ruleItems.forEach(li => {
        const rule = li.dataset.rule;
        const met = checkRule(rule, pwd);
        if (!met) allMet = false;
        li.textContent = (met ? '✅ ' : '❌ ') + li.textContent.replace(/^[✅❌⚪] /, '');
        li.style.color = met ? 'var(--color-success)' : 'var(--color-danger)';
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
    matchStatus.textContent = match ? '✅ Passwords match' : '❌ Passwords do not match';
    matchStatus.style.color = match ? 'var(--color-success)' : 'var(--color-danger)';
    return match;
}

function updateSubmitButton() {
    const rulesOk = updateRules();
    const matchOk = updateMatchStatus();
    const enabled = rulesOk && matchOk;
    submitBtn.disabled = !enabled;
    submitBtn.style.opacity = enabled ? '1' : '0.6';
    submitBtn.style.cursor = enabled ? 'pointer' : 'not-allowed';
}

if (passwordInput) passwordInput.addEventListener('input', updateSubmitButton);
if (confirmInput)  confirmInput.addEventListener('input', updateSubmitButton);

document.addEventListener('DOMContentLoaded', () => {
    if (passwordInput) passwordInput.focus();
    if (submitBtn) updateSubmitButton();
});

const form = document.getElementById('reset-form');
if (form) {
    form.addEventListener('submit', async (e) => {
        e.preventDefault();

        submitBtn.disabled = true;
        submitBtn.style.opacity = '0.6';
        submitBtn.textContent = 'Resetting password…';
        serverError.style.display = 'none';

        try {
            const resp = await fetch(`/reset-password/${RESET_TOKEN}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
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
            submitBtn.textContent = 'Reset Password & Log In';
            updateSubmitButton();
        }
    });
}
