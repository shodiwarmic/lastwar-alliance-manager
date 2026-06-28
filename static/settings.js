const API_BASE = '/api';
const SETTINGS_URL = `${API_BASE}/settings`;


let isR5OrAdmin = false;

function fetchPermissions() {
    isR5OrAdmin = document.getElementById('page-config').dataset.canManage === 'true';
}

async function loadSettings() {
    try {
        const response = await fetch(SETTINGS_URL);
        if (!response.ok) throw new Error('Failed to load settings');
        
        const settings = await response.json();
        
        document.getElementById('alliance-name').value = settings.alliance_name ?? '';
        document.getElementById('alliance-tag').value = settings.alliance_tag ?? '';
        document.getElementById('lastrank-alliance-id').value = settings.lastrank_alliance_id ?? '';
        document.getElementById('max-hq-level').value = settings.max_hq_level || 35;
        document.getElementById('settings-login-message').value = settings.login_message || '';
        document.getElementById('train-free-limit').value = settings.train_free_daily_limit ?? 1;
        document.getElementById('train-purchased-limit').value = settings.train_purchased_daily_limit ?? 2;
        document.getElementById('alliance-max-members').value = settings.alliance_max_members ?? 100;
        document.getElementById('join-requirements').value = settings.join_requirements ?? '';
        document.getElementById('vs-minimum-points').value = settings.vs_minimum_points ?? 2500000;
        document.getElementById('vs-flag-days').value = settings.vs_flag_days_threshold ?? 2;
        document.getElementById('strike-needs-improvement-threshold').value = settings.strike_needs_improvement_threshold ?? 1;
        document.getElementById('strike-at-risk-threshold').value = settings.strike_at_risk_threshold ?? 3;

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

        const [matrixRes, schemaRes] = await Promise.all([
            fetch(`${API_BASE}/permissions`),
            fetch(`${API_BASE}/permissions/schema`)
        ]);
        if (matrixRes.ok && schemaRes.ok) {
            const matrix = await matrixRes.json();
            const permGroups = await schemaRes.json();
            const matrixEl = document.getElementById('permissions-matrix');
            const grid = document.createElement('div');
            grid.className = 'perm-grid';

            // Header row (built in JS so the grid owns the full layout)
            ['Feature', 'Permission', 'R5', 'R4', 'R3', 'R2', 'R1'].forEach(text => {
                const cell = document.createElement('div');
                cell.className = 'perm-grid-header';
                cell.textContent = text;
                grid.appendChild(cell);
            });

            let globalRowIndex = 0;

            for (const [groupIdx, group] of permGroups.entries()) {
                const isLastGroup = groupIdx === permGroups.length - 1;
                const featureGroupClass = groupIdx % 2 !== 0 ? 'perm-feature-group-odd' : 'perm-feature-group-even';

                const featureCell = document.createElement('div');
                featureCell.className = `perm-feature ${featureGroupClass}`;
                featureCell.textContent = group.feature;
                featureCell.style.gridRow = `span ${group.rows.length}`;
                if (!isLastGroup) featureCell.classList.add('perm-feature-group-border');
                grid.appendChild(featureCell);

                group.rows.forEach((perm, i) => {
                    const isLastInGroup = i === group.rows.length - 1;
                    const rowCells = [];

                    const labelCell = document.createElement('div');
                    labelCell.className = 'perm-grid-label';
                    if (globalRowIndex % 2 !== 0) labelCell.classList.add('row-alt');
                    if (isLastInGroup && !isLastGroup) labelCell.classList.add('perm-row-last-in-group');
                    labelCell.textContent = perm.label;
                    rowCells.push(labelCell);
                    grid.appendChild(labelCell);

                    ['R5', 'R4', 'R3', 'R2', 'R1'].forEach(rank => {
                        const rankData = matrix.find(m => m.rank === rank) || {};
                        const cell = document.createElement('div');
                        cell.className = 'perm-grid-check';
                        if (globalRowIndex % 2 !== 0) cell.classList.add('row-alt');
                        if (isLastInGroup && !isLastGroup) cell.classList.add('perm-row-last-in-group');
                        const input = document.createElement('input');
                        input.type = 'checkbox';
                        input.className = 'perm-checkbox';
                        input.dataset.rank = rank;
                        input.dataset.key = perm.key;
                        input.checked = !!rankData[perm.key];
                        cell.appendChild(input);
                        rowCells.push(cell);
                        grid.appendChild(cell);
                    });

                    rowCells.forEach(cell => {
                        cell.addEventListener('mouseenter', () => {
                            rowCells.forEach(c => c.classList.add('row-hovered'));
                            featureCell.classList.add('perm-feature-hovered');
                        });
                        cell.addEventListener('mouseleave', () => {
                            rowCells.forEach(c => c.classList.remove('row-hovered'));
                            featureCell.classList.remove('perm-feature-hovered');
                        });
                    });

                    globalRowIndex++;
                });
            }

            matrixEl.replaceChildren(grid);
        }
    } catch (error) {
        console.error('Error loading settings:', error);
    }
}

function togglePowerUploadSection(enabled) {
    const uploadLink = document.getElementById('power-upload-link');
    if (uploadLink) uploadLink.style.display = enabled ? 'block' : 'none';
}

let _settingsStatusTimer = null;
function showSettingsStatus(message, success) {
    const el = document.getElementById('settings-save-status');
    if (!el) return;
    el.textContent = message;
    el.style.color = success ? 'var(--color-success)' : 'var(--color-danger)';
    clearTimeout(_settingsStatusTimer);
    _settingsStatusTimer = setTimeout(() => { el.textContent = ''; }, 4000);
}

// Show a "View alliance on LastRank" link once the field holds a valid id/URL.
function updateLastRankAllianceLink() {
    const input = document.getElementById('lastrank-alliance-id');
    const view = document.getElementById('lastrank-alliance-view');
    if (!input || !view) return;
    const v = input.value.trim();
    const m = v.match(/\/a\/([0-9a-fA-F]{32})/) || v.match(/^([0-9a-fA-F]{32})$/);
    if (m) {
        view.replaceChildren(document.createTextNode('Linked: '));
        const a = document.createElement('a');
        a.href = 'https://lastrank.fun/a/' + m[1].toLowerCase();
        a.target = '_blank';
        a.rel = 'noopener noreferrer';
        a.textContent = 'View alliance on LastRank ↗';
        view.appendChild(a);
    } else {
        view.replaceChildren();
    }
}

document.addEventListener('DOMContentLoaded', async () => {
    const settingsForm = document.getElementById('settings-form');
    
    if (settingsForm) {
        await fetchPermissions();
        await loadSettings();
        updateLastRankAllianceLink();
        document.getElementById('lastrank-alliance-id')?.addEventListener('input', updateLastRankAllianceLink);

        settingsForm.addEventListener('submit', async (e) => {
            e.preventDefault();

            if (!isR5OrAdmin) {
                showSettingsStatus('You do not have permission to modify settings.', false);
                return;
            }

            // Fetch current settings first so fields not owned by this form
            // (e.g. schedule defaults) are preserved and not zeroed out.
            let currentSettings = {};
            try {
                const r = await fetch(SETTINGS_URL);
                if (r.ok) currentSettings = await r.json();
            } catch {}

            const selectedZones = Array.from(document.querySelectorAll('.tz-checkbox:checked'))
                .map(cb => cb.value).join(',');

            const settings = Object.assign({}, currentSettings, {
                alliance_name: document.getElementById('alliance-name').value.trim(),
                alliance_tag: document.getElementById('alliance-tag').value.trim(),
                lastrank_alliance_id: document.getElementById('lastrank-alliance-id').value.trim(),
                login_message: document.getElementById('settings-login-message').value,
                max_hq_level: parseInt(document.getElementById('max-hq-level').value, 10),
                power_tracking_enabled: document.getElementById('power-tracking-enabled').checked,
                squad_tracking_enabled: document.getElementById('squad-tracking-enabled').checked,
                storm_timezones: selectedZones,
                storm_respect_dst: document.getElementById('storm_respect_dst').checked,
                train_free_daily_limit: parseInt(document.getElementById('train-free-limit').value, 10) || 1,
                train_purchased_daily_limit: parseInt(document.getElementById('train-purchased-limit').value, 10) || 2,
                alliance_max_members: parseInt(document.getElementById('alliance-max-members').value, 10) || 100,
                join_requirements: document.getElementById('join-requirements').value.trim(),
                vs_minimum_points: parseInt(document.getElementById('vs-minimum-points').value, 10) || 2500000,
                vs_flag_days_threshold: parseInt(document.getElementById('vs-flag-days').value, 10) || 2,
                strike_needs_improvement_threshold: parseInt(document.getElementById('strike-needs-improvement-threshold').value, 10) || 1,
                strike_at_risk_threshold: parseInt(document.getElementById('strike-at-risk-threshold').value, 10) || 3,
            });

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
                showSettingsStatus('Settings saved successfully.', true);
            } catch (error) {
                console.error('Error saving settings:', error);
                showSettingsStatus('Failed to save settings.', false);
            }
        });

        const powerToggle = document.getElementById('power-tracking-enabled');
        if (powerToggle) {
            powerToggle.addEventListener('change', (e) => togglePowerUploadSection(e.target.checked));
        }
    }
});

// ── Season Hub — Default Score Levels ──────────────────────────────────────

(function () {
    const tbody = document.getElementById('score-levels-tbody');
    const btnSave = document.getElementById('btn-save-score-levels');
    const btnAdd = document.getElementById('btn-add-score-level');
    const statusEl = document.getElementById('score-levels-status');
    if (!tbody) return;

    function buildScoreLevelRow(key, label, points) {
        const tr = document.createElement('tr');

        const tdKey = document.createElement('td');
        const inKey = document.createElement('input');
        inKey.type = 'text'; inKey.className = 'form-input sl-key'; inKey.value = key || '';
        inKey.placeholder = 'e.g. full';
        tdKey.appendChild(inKey); tr.appendChild(tdKey);

        const tdLabel = document.createElement('td');
        const inLabel = document.createElement('input');
        inLabel.type = 'text'; inLabel.className = 'form-input sl-label'; inLabel.value = label || '';
        inLabel.placeholder = 'e.g. FULL';
        tdLabel.appendChild(inLabel); tr.appendChild(tdLabel);

        const tdPoints = document.createElement('td');
        const inPoints = document.createElement('input');
        inPoints.type = 'number'; inPoints.className = 'form-input sl-points'; inPoints.value = points != null ? points : 0;
        inPoints.min = '0';
        tdPoints.appendChild(inPoints); tr.appendChild(tdPoints);

        const tdDel = document.createElement('td');
        const btnDel = document.createElement('button');
        btnDel.type = 'button'; btnDel.className = 'btn btn-danger btn-sm'; btnDel.setAttribute('aria-label', 'Delete'); btnDel.appendChild(svgIcon('x'));
        btnDel.addEventListener('click', () => tr.remove());
        tdDel.appendChild(btnDel); tr.appendChild(tdDel);

        return tr;
    }

    function loadScoreLevels() {
        fetch('/api/season-hub/score-levels-default')
            .then(r => r.ok ? r.json() : null)
            .then(d => {
                if (!d || !Array.isArray(d.score_levels)) return;
                tbody.replaceChildren(...d.score_levels.map(sl => buildScoreLevelRow(sl.key, sl.label, sl.points)));
            })
            .catch(() => {});
    }

    function saveScoreLevels() {
        const rows = Array.from(tbody.querySelectorAll('tr'));
        const levels = rows.map((row, i) => ({
            key: row.querySelector('.sl-key').value.trim(),
            label: row.querySelector('.sl-label').value.trim(),
            points: parseInt(row.querySelector('.sl-points').value, 10) || 0,
        })).filter(sl => sl.key && sl.label);

        fetch('/api/season-hub/score-levels-default', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ score_levels: levels }),
        })
            .then(r => r.ok ? r.json() : r.text().then(t => { throw new Error(t); }))
            .then(() => showToast('Score levels saved.'))
            .catch(err => showToast(err.message || 'Save failed.', 'error'));
    }

    if (btnSave) btnSave.addEventListener('click', saveScoreLevels);
    if (btnAdd) btnAdd.addEventListener('click', () => tbody.appendChild(buildScoreLevelRow('', '', 0)));

    loadScoreLevels();
})();

// ── Season Hub — Templates ──────────────────────────────────────────────────

(function () {
    const listEl = document.getElementById('season-templates-list');
    if (!listEl) return;

    function loadSeasonTemplates() {
        fetch('/api/season-hub/templates')
            .then(r => r.ok ? r.json() : null)
            .then(d => {
                if (!d) return;
                const templates = d.templates || [];
                listEl.replaceChildren(...templates.map(buildTemplateCard));
            })
            .catch(() => {});
    }

    // ── Structured sub-editors ─────────────────────────────────────────────────

    function buildTrackablesEditor(json) {
        let items = [];
        try { items = JSON.parse(json) || []; } catch (e) {}

        const wrap = document.createElement('div');
        wrap.style.marginBottom = '10px';

        const hdr = document.createElement('div');
        hdr.style.cssText = 'font-size:0.85rem;font-weight:600;margin-bottom:6px;';
        hdr.textContent = 'Trackables';
        wrap.appendChild(hdr);

        const tableWrap = document.createElement('div');
        tableWrap.style.cssText = 'overflow-x:auto;border:1px solid var(--color-border);border-radius:6px;';

        const tbl = document.createElement('table');
        tbl.className = 'data-table';
        tbl.style.cssText = 'width:100%;font-size:0.85rem;';

        const thead = document.createElement('thead');
        const hrow = document.createElement('tr');
        ['Key', 'Label', ''].forEach(h => {
            const th = document.createElement('th');
            th.textContent = h;
            th.style.padding = '6px 8px';
            hrow.appendChild(th);
        });
        thead.appendChild(hrow);
        tbl.appendChild(thead);

        const tbody = document.createElement('tbody');
        tbl.appendChild(tbody);
        tableWrap.appendChild(tbl);
        wrap.appendChild(tableWrap);

        function addRow(key, label) {
            const tr = document.createElement('tr');

            function inp(placeholder, val, width) {
                const el = document.createElement('input');
                el.type = 'text';
                el.className = 'form-input';
                el.placeholder = placeholder;
                el.style.cssText = `width:${width};font-size:0.8rem;padding:4px 6px;`;
                el.value = val || '';
                return el;
            }
            const keyInp = inp('snake_case_key', key, '130px');
            const lblInp = inp('Display label', label, '160px');

            const delBtn = document.createElement('button');
            delBtn.type = 'button';
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.setAttribute('aria-label', 'Delete');
            delBtn.appendChild(svgIcon('x'));
            delBtn.style.padding = '2px 6px';
            delBtn.addEventListener('click', () => tr.remove());

            [keyInp, lblInp, delBtn].forEach(child => {
                const td = document.createElement('td');
                td.style.padding = '4px 6px';
                td.appendChild(child);
                tr.appendChild(td);
            });
            tbody.appendChild(tr);
        }

        items.forEach(item => addRow(item.key, item.label));

        const addBtn = document.createElement('button');
        addBtn.type = 'button';
        addBtn.className = 'btn btn-secondary btn-sm';
        addBtn.textContent = '+ Add Trackable';
        addBtn.style.marginTop = '6px';
        addBtn.addEventListener('click', () => addRow('', ''));
        wrap.appendChild(addBtn);

        function getJSON() {
            const result = [];
            tbody.querySelectorAll('tr').forEach((tr, i) => {
                const inputs = tr.querySelectorAll('input');
                const key = inputs[0].value.trim();
                if (key) result.push({ key, label: inputs[1].value.trim(), sort_order: i + 1 });
            });
            return JSON.stringify(result);
        }

        return { el: wrap, getJSON };
    }

    function buildDefaultsEditor(json) {
        let defs = {};
        try { defs = JSON.parse(json) || {}; } catch (e) {}

        const wrap = document.createElement('div');
        wrap.style.marginBottom = '10px';

        const hdr = document.createElement('div');
        hdr.style.cssText = 'font-size:0.85rem;font-weight:600;margin-bottom:6px;';
        hdr.textContent = 'Defaults';
        wrap.appendChild(hdr);

        const grid = document.createElement('div');
        grid.style.cssText = 'display:grid;grid-template-columns:repeat(3,1fr);gap:10px;';

        function addField(label, type, placeholder, val, min, max) {
            const fg = document.createElement('div');
            const lbl = document.createElement('label');
            lbl.style.cssText = 'font-size:0.8rem;display:block;margin-bottom:3px;';
            lbl.textContent = label;
            const inp = document.createElement('input');
            inp.type = type;
            inp.className = 'form-input';
            inp.placeholder = placeholder;
            inp.style.cssText = 'font-size:0.85rem;padding:4px 6px;width:100%;box-sizing:border-box;';
            if (val !== undefined && val !== null) inp.value = val;
            if (min !== undefined) inp.min = min;
            if (max !== undefined) inp.max = max;
            fg.appendChild(lbl);
            fg.appendChild(inp);
            grid.appendChild(fg);
            return inp;
        }

        const wcInp  = addField('Week Count', 'number', '8', defs.week_count ?? 8, 1, 52);
        const kenInp = addField('Key Event Name', 'text', 'e.g. Rare Soil War', defs.key_event_name || '');
        const kerInp = addField('Key Event Required (weeks)', 'number', '4', defs.key_event_required ?? 0, 0, 52);

        wrap.appendChild(grid);

        function getJSON() {
            return JSON.stringify({
                week_count: parseInt(wcInp.value, 10) || 8,
                key_event_name: kenInp.value.trim(),
                key_event_required: parseInt(kerInp.value, 10) || 0,
            });
        }

        return { el: wrap, getJSON };
    }

    const DAY_OPTS = [
        { value: '', label: '— unscheduled' },
        { value: '1', label: 'Mon' }, { value: '2', label: 'Tue' },
        { value: '3', label: 'Wed' }, { value: '4', label: 'Thu' },
        { value: '5', label: 'Fri' }, { value: '6', label: 'Sat' },
        { value: '7', label: 'Sun' },
    ];

    function buildEventsEditor(json) {
        let items = [];
        try { items = JSON.parse(json) || []; } catch (e) {}

        const wrap = document.createElement('div');
        wrap.style.marginBottom = '10px';

        const hdr = document.createElement('div');
        hdr.style.cssText = 'font-size:0.85rem;font-weight:600;margin-bottom:6px;';
        hdr.textContent = 'Events';
        wrap.appendChild(hdr);

        const tableWrap = document.createElement('div');
        tableWrap.style.cssText = 'overflow-x:auto;border:1px solid var(--color-border);border-radius:6px;';

        const tbl = document.createElement('table');
        tbl.className = 'data-table';
        tbl.style.cssText = 'width:100%;font-size:0.8rem;white-space:nowrap;';

        const thead = document.createElement('thead');
        const hrow = document.createElement('tr');
        const COLS = [
            { text: 'Label',   title: 'Event label shown in schedule' },
            { text: 'Type',    title: 'Matches schedule_event_types.name; used for push-to-schedule' },
            { text: 'Short',   title: 'Short code, e.g. CC, RSW' },
            { text: 'Icon',    title: 'Emoji icon' },
            { text: 'Day',     title: '1=Mon…7=Sun; blank = unscheduled (skipped during push)' },
            { text: 'Time',    title: 'Default start time (HH:MM)' },
            { text: 'Wk★',    title: 'First week this event runs (1-based; use 0 or negative for pre-season events)' },
            { text: 'Wk✕',    title: 'Last week this event runs. If less than Wk★ (e.g. 0 when Wk★≥1), treated as "open-ended through end of season"' },
            { text: 'Level',   title: 'Optional numeric level tag (e.g. 1 for L1 city clash)' },
            { text: 'Notes',   title: 'Optional notes pushed to schedule' },
            { text: 'Svr', icon: 'world', title: 'Server event — pushed to Server Events section of schedule instead of alliance schedule' },
            { text: 'Days',    title: 'Duration in days (server events only)' },
            { text: '',        title: 'Remove row' },
        ];
        COLS.forEach(c => {
            const th = document.createElement('th');
            if (c.icon) {
                th.append(svgIcon(c.icon, 13), document.createTextNode(' ' + c.text));
            } else {
                th.textContent = c.text;
            }
            th.title = c.title;
            th.style.padding = '6px 6px';
            hrow.appendChild(th);
        });
        thead.appendChild(hrow);
        tbl.appendChild(thead);

        const tbody = document.createElement('tbody');
        tbl.appendChild(tbody);
        tableWrap.appendChild(tbl);
        wrap.appendChild(tableWrap);

        function mkInp(type, placeholder, val, width) {
            const el = document.createElement('input');
            el.type = type;
            el.className = 'form-input';
            el.placeholder = placeholder;
            el.style.cssText = `width:${width};font-size:0.78rem;padding:3px 5px;box-sizing:border-box;`;
            if (val !== undefined && val !== null && val !== '') el.value = val;
            return el;
        }

        function mkSel(selected) {
            const sel = document.createElement('select');
            sel.className = 'form-input';
            sel.style.cssText = 'width:80px;font-size:0.78rem;padding:3px 4px;';
            DAY_OPTS.forEach(o => {
                const opt = document.createElement('option');
                opt.value = o.value;
                opt.textContent = o.label;
                if (String(o.value) === String(selected ?? '')) opt.selected = true;
                sel.appendChild(opt);
            });
            return sel;
        }

        function addRow(ev) {
            ev = ev || {};
            const tr = document.createElement('tr');

            function cell(child) {
                const td = document.createElement('td');
                td.style.padding = '3px 5px';
                td.appendChild(child);
                tr.appendChild(td);
            }

            cell(mkInp('text', 'Event label',  ev.label      || '', '140px'));
            cell(mkInp('text', 'Type name',    ev.type_name  || '', '105px'));
            cell(mkInp('text', 'Short',        ev.type_short || '', '55px'));
            cell(mkInp('text', '🎯',           ev.type_icon  || '', '45px'));
            cell(mkSel(ev.day_offset ?? ''));
            const timeInp = mkInp('text', '20:00', ev.event_time || '20:00', '80px');
            flatpickr(timeInp, { enableTime: true, noCalendar: true, dateFormat: 'H:i', time_24hr: true, minuteIncrement: 30, allowInput: true });
            cell(timeInp);

            const wkS = mkInp('number', '1', ev.week_start ?? 1, '50px');
            const wkE = mkInp('number', '0', ev.week_end   ?? 0, '50px');
            const lvl = mkInp('number', '—', ev.level != null ? ev.level : '', '45px');
            // No min on wkS — pre-season events use 0 (or negative).
            lvl.min = 1;
            cell(wkS); cell(wkE); cell(lvl);

            cell(mkInp('text', 'Notes', ev.notes || '', '110px'));

            const svrChk = document.createElement('input');
            svrChk.type = 'checkbox';
            svrChk.title = 'Server event';
            svrChk.style.cssText = 'width:16px;height:16px;cursor:pointer;';
            svrChk.checked = !!ev.is_server_event;
            cell(svrChk);

            const durInp = mkInp('number', '1', ev.duration_days ?? 1, '50px');
            durInp.min = 1;
            cell(durInp);

            const delBtn = document.createElement('button');
            delBtn.type = 'button';
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.setAttribute('aria-label', 'Remove row');
            delBtn.appendChild(svgIcon('x'));
            delBtn.style.padding = '2px 5px';
            delBtn.addEventListener('click', () => tr.remove());
            cell(delBtn);

            tbody.appendChild(tr);
        }

        items.forEach(ev => addRow(ev));

        const addBtn = document.createElement('button');
        addBtn.type = 'button';
        addBtn.className = 'btn btn-secondary btn-sm';
        addBtn.textContent = '+ Add Event';
        addBtn.style.marginTop = '6px';
        addBtn.addEventListener('click', () => addRow({}));
        wrap.appendChild(addBtn);

        function getJSON() {
            const result = [];
            tbody.querySelectorAll('tr').forEach(tr => {
                const tds = tr.querySelectorAll('td');
                const label = tds[0].querySelector('input').value.trim();
                if (!label) return;
                const daySel = tds[4].querySelector('select');
                const wkSVal = parseInt(tds[6].querySelector('input').value, 10);
                const wkEVal = parseInt(tds[7].querySelector('input').value, 10);
                const lvlVal = tds[8].querySelector('input').value.trim();
                const ev = {
                    label,
                    type_name:       tds[1].querySelector('input').value.trim(),
                    type_short:      tds[2].querySelector('input').value.trim(),
                    type_icon:       tds[3].querySelector('input').value.trim(),
                    day_offset:      daySel.value !== '' ? parseInt(daySel.value, 10) : null,
                    event_time:      tds[5].querySelector('input').value || '20:00',
                    // Preserve 0 / negative values for pre-season events.
                    week_start:      Number.isFinite(wkSVal) ? wkSVal : 1,
                    week_end:        Number.isFinite(wkEVal) ? wkEVal : 0,
                    notes:           tds[9].querySelector('input').value.trim(),
                    is_server_event: tds[10].querySelector('input[type=checkbox]').checked,
                    duration_days:   parseInt(tds[11].querySelector('input').value, 10) || 1,
                };
                if (lvlVal !== '') ev.level = parseInt(lvlVal, 10);
                result.push(ev);
            });
            return JSON.stringify(result);
        }

        return { el: wrap, getJSON };
    }

    // ── Template card (list row only) ───────────────────────────────────────────

    function buildTemplateCard(t) {
        const card = document.createElement('div');
        card.style.cssText = 'border:1px solid var(--color-border);border-radius:8px;padding:10px 14px;margin-bottom:8px;background:var(--color-surface);display:flex;align-items:center;gap:10px;';

        const nameLbl = document.createElement('span');
        nameLbl.textContent = (t.season_number > 0 ? 'S' + t.season_number + ' — ' : '') + t.template_name;
        nameLbl.style.flex = '1';

        const btnEdit = document.createElement('button');
        btnEdit.type = 'button'; btnEdit.className = 'btn btn-secondary btn-sm';
        btnEdit.textContent = 'Edit';
        btnEdit.addEventListener('click', () => openTemplateModal(t));

        const btnSync = document.createElement('button');
        btnSync.type = 'button'; btnSync.className = 'btn btn-secondary btn-sm';
        btnSync.textContent = 'Sync Event Types';
        btnSync.addEventListener('click', () => {
            fetch('/api/season-hub/templates/' + t.id + '/sync-event-types', { method: 'POST' })
                .then(r => r.ok ? r.json() : r.text().then(txt => { throw new Error(txt); }))
                .then(d => showToast(d.created > 0 ? d.created + ' event type(s) synced.' : 'All event types already exist.', d.created > 0 ? 'success' : 'info'))
                .catch(err => showToast(err.message || 'Sync failed.', 'error'));
        });

        const btnDel = document.createElement('button');
        btnDel.type = 'button'; btnDel.className = 'btn btn-danger btn-sm';
        btnDel.textContent = 'Delete';
        btnDel.addEventListener('click', async () => {
            if (!await showConfirm('Permanently delete template "' + t.template_name + '"? This cannot be undone.', 'Delete')) return;
            fetch('/api/season-hub/templates/' + t.id, { method: 'DELETE' })
                .then(r => r.ok ? r.json() : r.text().then(txt => { throw new Error(txt); }))
                .then(() => { showToast('Template deleted.'); card.remove(); })
                .catch(err => showToast(err.message || 'Delete failed.', 'error'));
        });

        card.appendChild(nameLbl);
        card.appendChild(btnEdit);
        card.appendChild(btnSync);
        card.appendChild(btnDel);
        return card;
    }

    // ── Template modal ──────────────────────────────────────────────────────────

    const modal       = document.getElementById('modal-season-template');
    const modalTitle  = document.getElementById('season-template-modal-title');
    const stmName     = document.getElementById('stm-name');
    const stmNum      = document.getElementById('stm-season-number');
    const defaultsWrap    = document.getElementById('stm-defaults-wrap');
    const trackablesWrap  = document.getElementById('stm-trackables-wrap');
    const eventsWrap      = document.getElementById('stm-events-wrap');
    const btnStmSave  = document.getElementById('btn-stm-save');
    const btnStmCancel = document.getElementById('btn-stm-cancel');

    let activeTemplate = null;
    let defaultsEd, trackablesEd, eventsEd;

    function openTemplateModal(t) {
        activeTemplate = t || {};
        const isNew = !activeTemplate.id;

        modalTitle.textContent = isNew ? 'New Season Template' : 'Edit — ' + activeTemplate.template_name;
        stmName.value = activeTemplate.template_name || '';
        stmNum.value  = activeTemplate.season_number ?? '';

        defaultsEd   = buildDefaultsEditor(activeTemplate.defaults    || '{}');
        trackablesEd = buildTrackablesEditor(activeTemplate.trackables || '[]');
        eventsEd     = buildEventsEditor(activeTemplate.events         || '[]');

        defaultsWrap.replaceChildren(defaultsEd.el);
        trackablesWrap.replaceChildren(trackablesEd.el);
        eventsWrap.replaceChildren(eventsEd.el);

        modal.style.display = 'flex';
        stmName.focus();
    }

    function closeTemplateModal() {
        modal.style.display = '';
        activeTemplate = null;
    }

    if (btnStmCancel) btnStmCancel.addEventListener('click', closeTemplateModal);
    if (modal) modal.addEventListener('click', e => { if (e.target === modal) closeTemplateModal(); });

    if (btnStmSave) {
        btnStmSave.addEventListener('click', () => {
            const body = {
                template_name: stmName.value.trim(),
                season_number: parseInt(stmNum.value, 10) || 0,
                defaults:      defaultsEd.getJSON(),
                trackables:    trackablesEd.getJSON(),
                events:        eventsEd.getJSON(),
            };
            if (!body.template_name) { showToast('Template name is required.', 'error'); return; }

            const isNew = !activeTemplate.id;
            const url    = isNew ? '/api/season-hub/templates' : '/api/season-hub/templates/' + activeTemplate.id;
            const method = isNew ? 'POST' : 'PUT';

            fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
                .then(r => r.ok ? r.json() : r.text().then(txt => { throw new Error(txt); }))
                .then(d => {
                    if (isNew) activeTemplate.id = d.id;
                    Object.assign(activeTemplate, body);
                    showToast(isNew ? 'Template created.' : 'Template saved.');
                    closeTemplateModal();
                    loadSeasonTemplates();
                })
                .catch(err => showToast(err.message || 'Save failed.', 'error'));
        });
    }

    const btnAddTemplate = document.getElementById('btn-add-season-template');
    if (btnAddTemplate) btnAddTemplate.addEventListener('click', () => openTemplateModal(null));

    loadSeasonTemplates();
})();