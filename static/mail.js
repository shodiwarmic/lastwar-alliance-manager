// mail.js — shared mail template variable logic
// Loaded explicitly by pages that use mail templates (comms.html, season-hub.html, storm.html).
// Must appear before the page-specific script in each template's scripts block.

const DAYS_OF_WEEK = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

// Assignment builder state
let _assignmentTarget = null;
let _assignmentSkillKey = null;
let _assignmentSkillLabel = null;
let _assignmentPairings = [];
let _allOthers = [];
const _assignmentDrafts = {};
let _skillRegistryCache = null;

// Cached active roster (name + rank) for {member:} / {members:} variables.
// Lazily fetched the first time such a variable is filled in.
let _rosterCache = null;

// Fetches the active roster once and caches it. /api/members already excludes
// EX (former) members and sorts by name; we filter/sort defensively anyway so
// the behaviour is explicit and independent of the endpoint.
async function fetchRoster() {
    if (_rosterCache) return _rosterCache;
    try {
        const res = await fetch('/api/members');
        const list = res.ok ? await res.json() : [];
        _rosterCache = list
            .filter(m => m.rank !== 'EX')
            .sort((a, b) => (a.name || '').localeCompare(b.name || ''));
    } catch {
        _rosterCache = [];
    }
    return _rosterCache;
}

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

async function fetchSkillRegistry() {
    if (_skillRegistryCache) return _skillRegistryCache;
    const res = await fetch('/api/skills');
    _skillRegistryCache = res.ok ? await res.json() : [];
    return _skillRegistryCache;
}

async function openAssignmentModal(skillKey, targetTextarea) {
    _assignmentPairings = [];
    _assignmentTarget = null;
    _assignmentSkillKey = null;
    _assignmentSkillLabel = null;
    _allOthers = [];

    const statusEl = document.getElementById('assignment-modal-status');
    const bodyEl = document.getElementById('assignment-builder-body');
    const modal = document.getElementById('assignment-modal');
    const titleEl = document.getElementById('assignment-modal-title');

    statusEl.textContent = '';
    statusEl.style.color = '';
    bodyEl.replaceChildren();

    const generateBtn = document.getElementById('assignment-generate-btn');
    const formatRow = document.getElementById('assignment-format-row');
    generateBtn.style.display = 'none';
    formatRow.style.display = 'none';

    document.getElementById('assignment-cancel-btn').onclick = () => {
        modal.style.display = '';
    };

    const registry = await fetchSkillRegistry();
    const skillEntry = registry.find(s => s.key === skillKey);

    if (!skillEntry) {
        titleEl.textContent = 'Assignment Builder';
        statusEl.textContent = `Unknown skill key '${skillKey}'. Valid keys: ${registry.map(s => s.key).join(', ') || '(none registered)'}.`;
        statusEl.style.color = 'var(--color-danger)';
        modal.style.display = 'flex';
        return;
    }

    _assignmentTarget = targetTextarea;
    _assignmentSkillKey = skillKey;
    _assignmentSkillLabel = skillEntry.label;
    titleEl.textContent = `${skillEntry.label} Assignment Builder`;

    const res = await fetch('/api/members/skills');
    if (!res.ok) {
        statusEl.textContent = 'Failed to load members. Please try again.';
        statusEl.style.color = 'var(--color-danger)';
        modal.style.display = 'flex';
        return;
    }
    const members = await res.json();

    const engineers = members.filter(m => m.skills && m.skills.split(',').includes(skillKey));
    _allOthers = members.filter(m => !m.skills || !m.skills.split(',').includes(skillKey));

    if (engineers.length === 0) {
        statusEl.textContent = `No members currently have the ${skillEntry.label} skill. Assign it on the Roster page first.`;
        statusEl.style.color = 'var(--color-warning)';
    }

    if (_assignmentDrafts[skillKey]) {
        _assignmentPairings = _assignmentDrafts[skillKey];
    } else {
        _assignmentPairings = engineers.map(e => ({ engineer: e, member: null }));
    }

    renderAssignmentBuilder();

    generateBtn.style.display = '';
    formatRow.style.display = '';
    generateBtn.onclick = () => {
        _assignmentDrafts[_assignmentSkillKey] = _assignmentPairings.map(p => ({
            engineer: p.engineer,
            member: p.member,
        }));

        const fmt = document.getElementById('assignment-format-input').value || '• {engineer} → {member}';
        const lines = [];
        _assignmentPairings.forEach(p => {
            if (p.member) {
                lines.push(fmt.replace('{engineer}', p.engineer.name).replace('{member}', p.member.name));
            }
        });

        _assignmentTarget.value = lines.join('\n');
        modal.style.display = '';
    };

    modal.style.display = 'flex';
}

function renderAssignmentBuilder() {
    const bodyEl = document.getElementById('assignment-builder-body');

    if (_assignmentPairings.length === 0) {
        bodyEl.replaceChildren();
        return;
    }

    const rows = _assignmentPairings.map(pairing => {
        const row = document.createElement('div');
        row.className = 'assignment-engineer-row';

        const nameEl = document.createElement('div');
        nameEl.className = 'assignment-engineer-name';
        nameEl.textContent = pairing.engineer.name;
        row.appendChild(nameEl);

        const takenByOthers = new Set(
            _assignmentPairings
                .filter(p => p !== pairing && p.member !== null)
                .map(p => p.member.id)
        );

        const sel = document.createElement('select');
        sel.className = 'form-input';
        const defaultOpt = document.createElement('option');
        defaultOpt.value = '';
        defaultOpt.textContent = '— unassigned —';
        sel.appendChild(defaultOpt);
        _allOthers.filter(m => !takenByOthers.has(m.id)).forEach(m => {
            const opt = document.createElement('option');
            opt.value = String(m.id);
            opt.textContent = m.name;
            sel.appendChild(opt);
        });
        sel.value = pairing.member ? String(pairing.member.id) : '';
        sel.addEventListener('change', () => {
            const memberId = parseInt(sel.value);
            pairing.member = memberId ? (_allOthers.find(m => m.id === memberId) || null) : null;
            renderAssignmentBuilder();
        });

        row.appendChild(sel);
        return row;
    });

    bodyEl.replaceChildren(...rows);
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
    if (type === 'assignment') {
        const wrapper = document.createElement('div');
        const ta = document.createElement('textarea');
        ta.className = 'form-input';
        ta.rows = 4;
        ta.dataset.varName = name;
        ta.placeholder = 'Click "Build…" to generate assignments';
        ta.style.cssText = 'margin-top:4px;resize:vertical;font-family:monospace;font-size:0.85rem;';
        const buildBtn = document.createElement('button');
        buildBtn.type = 'button';
        buildBtn.className = 'btn btn-secondary btn-sm';
        buildBtn.textContent = 'Build…';
        buildBtn.style.marginTop = '6px';
        buildBtn.addEventListener('click', () => openAssignmentModal(name, ta));
        wrapper.append(ta, buildBtn);
        return wrapper;
    }
    if (type === 'member' || type === 'members') {
        // Roster is populated by fetchRoster() before the modal is built.
        const roster = _rosterCache || [];
        const sel = document.createElement('select');
        sel.className = 'form-input member-var-select';
        sel.dataset.varName = name;
        sel.style.marginTop = '4px';
        if (type === 'members') {
            sel.multiple = true;
            // Native fallback (no Choices.js) needs a visible size; Choices ignores it.
            if (typeof Choices === 'undefined') {
                sel.size = Math.min(8, Math.max(2, roster.length));
            }
        } else {
            const placeholder = document.createElement('option');
            placeholder.value = '';
            placeholder.textContent = '— Select a member —';
            sel.appendChild(placeholder);
        }
        roster.forEach(m => {
            const opt = document.createElement('option');
            opt.value = m.name;
            opt.textContent = `${m.name} (${m.rank})`;
            sel.appendChild(opt);
        });
        return sel;
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

    // Member selects need the roster loaded before the fields are built.
    if (userSpecs.some(s => s.type === 'member' || s.type === 'members')) {
        await fetchRoster();
    }

    const form = document.getElementById('vars-form');
    const modal = document.getElementById('vars-modal');
    form.replaceChildren();

    userSpecs.forEach(({ name, type }) => {
        const label = document.createElement('label');
        label.style.cssText = 'display:block;margin-bottom:14px;color:var(--color-text);font-size:0.9rem;';
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

    // Upgrade member selects to a searchable Choices.js control where available
    // (comms.html, season-hub.html); native <select> is the fallback elsewhere.
    if (typeof Choices !== 'undefined') {
        form.querySelectorAll('select.member-var-select').forEach(el => {
            new Choices(el, {
                removeItemButton: el.multiple,
                shouldSort: false,
                itemSelectText: '',
                searchPlaceholderValue: 'Search members…',
            });
        });
    }

    const firstEl = form.querySelector('input,select,textarea');
    if (firstEl) firstEl.focus();

    document.getElementById('vars-copy-btn').onclick = async () => {
        const values = Object.assign({}, prefilled);
        form.querySelectorAll('[data-var-name]').forEach(el => {
            values[el.dataset.varName] = el.multiple
                ? Array.from(el.selectedOptions).map(o => o.value).join(', ')
                : el.value;
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
