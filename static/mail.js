// mail.js — shared mail template variable logic
// Loaded explicitly by pages that use mail templates (comms.html, season-hub.html, storm.html).
// Must appear before the page-specific script in each template's scripts block.

const DAYS_OF_WEEK = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

// Returns unique variable names found in content, skipping {{escaped}} blocks.
// Strips optional type prefix so {time:battle_time} → 'battle_time'.
function extractVariables(content) {
    const stripped = content.replace(/\{\{.*?\}\}/g, '');
    return [...new Set([...stripped.matchAll(/\{(?:\w+:)?(\w+)\}/g)].map(m => m[1]))];
}

// Returns [{name, type}] in order of first appearance, deduplicated by name.
// Type defaults to 'text' when no prefix is present.
function extractVariableSpecs(content) {
    const stripped = content.replace(/\{\{.*?\}\}/g, '');
    const seen = new Set();
    const specs = [];
    for (const m of stripped.matchAll(/\{(?:(\w+):)?(\w+)\}/g)) {
        const type = m[1] || 'text';
        const name = m[2];
        if (!seen.has(name)) {
            seen.add(name);
            specs.push({ name, type });
        }
    }
    return specs;
}

function toVarLabel(name) {
    return name.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

// Single-pass substitution: {{ → {, }} → }, {[type:]var} → value (or original if not in values).
function applyTemplate(content, values) {
    return content.replace(/\{\{|\}\}|\{(?:\w+:)?(\w+)\}/g, (match, varName) => {
        if (match === '{{') return '{';
        if (match === '}}') return '}';
        return Object.prototype.hasOwnProperty.call(values, varName) ? values[varName] : match;
    });
}

function buildVarInput(name, type) {
    if (type === 'multiline') {
        const ta = document.createElement('textarea');
        ta.className = 'form-input';
        ta.rows = 3;
        ta.style.cssText = 'margin-top:4px;resize:vertical;';
        ta.dataset.varName = name;
        ta.placeholder = toVarLabel(name);
        return ta;
    }
    if (type === 'dayofweek') {
        const sel = document.createElement('select');
        sel.className = 'form-input';
        sel.style.marginTop = '4px';
        sel.dataset.varName = name;
        for (const day of DAYS_OF_WEEK) {
            const opt = document.createElement('option');
            opt.value = day;
            opt.textContent = day;
            sel.appendChild(opt);
        }
        return sel;
    }
    if (type === 'time') {
        const input = document.createElement('input');
        input.type = 'text';
        input.className = 'form-input';
        input.dataset.varName = name;
        input.dataset.varType = 'time';
        input.placeholder = 'HH:MM';
        input.maxLength = 5;
        input.style.marginTop = '4px';
        return input;
    }
    const inputTypeMap = { number: 'number', date: 'date' };
    const input = document.createElement('input');
    input.type = inputTypeMap[type] || 'text';
    input.className = 'form-input';
    input.dataset.varName = name;
    input.placeholder = toVarLabel(name);
    input.style.marginTop = '4px';
    return input;
}

// Writes text to clipboard. Uses execCommand fallback when clipboard API is unavailable
// (non-secure HTTP contexts such as local test environments accessed by IP).
async function writeToClipboard(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(text);
        return;
    }
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;left:-9999px;top:-9999px;opacity:0;';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    if (!ok) throw new Error('execCommand copy failed');
}

// Opens the global #vars-modal for any variables not already in prefilledValues.
// If all variables are pre-filled (or there are none), copies directly to clipboard.
// prefilledValues: object of varName → value provided by calling context (e.g. storm.js).
async function copyWithVariables(content, prefilledValues) {
    const prefilled = prefilledValues || {};
    const specs = extractVariableSpecs(content);
    const userSpecs = specs.filter(s => !Object.prototype.hasOwnProperty.call(prefilled, s.name));

    if (userSpecs.length === 0) {
        try {
            await writeToClipboard(applyTemplate(content, prefilled));
            showToast('Copied!');
        } catch {
            showToast('Copy failed — check clipboard permissions.', 'error');
        }
        return;
    }

    const form = document.getElementById('vars-form');
    const modal = document.getElementById('vars-modal');
    form.replaceChildren();

    userSpecs.forEach(({ name, type }) => {
        const label = document.createElement('label');
        label.style.cssText = 'display:block;margin-bottom:14px;color:var(--text-primary);font-size:0.9rem;';
        label.textContent = toVarLabel(name);
        label.appendChild(buildVarInput(name, type));
        form.appendChild(label);
    });

    modal.style.display = 'flex';

    if (typeof flatpickr !== 'undefined') {
        form.querySelectorAll('[data-var-type="time"]').forEach(el => {
            flatpickr(el, { enableTime: true, noCalendar: true, time_24hr: true });
        });
    }

    const firstEl = form.querySelector('input,select,textarea');
    if (firstEl) firstEl.focus();

    document.getElementById('vars-copy-btn').onclick = async () => {
        const values = Object.assign({}, prefilled);
        form.querySelectorAll('[data-var-name]').forEach(el => {
            values[el.dataset.varName] = el.value;
        });
        try {
            await writeToClipboard(applyTemplate(content, values));
            modal.style.display = '';
            showToast('Copied!');
        } catch {
            showToast('Copy failed — check clipboard permissions.', 'error');
        }
    };

    document.getElementById('vars-cancel-btn').onclick = () => {
        modal.style.display = '';
    };
}
