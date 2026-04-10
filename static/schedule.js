// schedule.js — Alliance Schedule (calendar-based revamp)

'use strict';

// ── VS Themes (Mon=0 … Sun=6, game-fixed for all servers) ────────────────────
const VS_THEMES = [
    { label: 'Radar Training',     short: 'Radar',     icon: '📡' },
    { label: 'Base Expansion',     short: 'Expand',    icon: '🏗️' },
    { label: 'Age of Science',     short: 'Science',   icon: '🔬' },
    { label: 'Train Heroes',       short: 'Heroes',    icon: '🦸' },
    { label: 'Total Mobilization', short: 'Mobilize',  icon: '📦' },
    { label: 'Enemy Buster',       short: 'Enemy',     icon: '💥' },
    { label: 'Alliance Star',      short: 'Celebrate', icon: '⭐' },
];

function getVSTheme(dateStr) {
    // Game time = UTC-2; VS resets on Monday game time.
    // Adding 2h to UTC gives game time; UTCDay+6)%7 → Mon=0…Sun=6
    const d = new Date(dateStr + 'T02:00:00Z');
    return VS_THEMES[(d.getUTCDay() + 6) % 7];
}

// ── State ─────────────────────────────────────────────────────────────────────
const cfg = document.getElementById('page-config') ? document.getElementById('page-config').dataset : {};
const CAN_MANAGE = cfg.canManage === 'true';

let currentWeekStart = '';   // "YYYY-MM-DD" of the Monday being shown
let mobileActiveDayIndex = 0; // 0–6 index within the current week

let eventTypes   = [];
let serverEvents = [];
let weekEvents   = [];
let settings     = {};
let stormSlotTimes = [];
let stormTFConfig  = {};

// ── Date helpers ──────────────────────────────────────────────────────────────

function todayGameDate() {
    // UTC-2: subtract 2h from now
    const d = new Date(Date.now() - 2 * 3600 * 1000);
    return d.toISOString().slice(0, 10);
}

function currentGameMonday() {
    const today = todayGameDate();
    return weekMonday(today);
}

function weekMonday(dateStr) {
    const d = new Date(dateStr + 'T12:00:00Z');
    const dow = (d.getUTCDay() + 6) % 7; // Mon=0…Sun=6
    d.setUTCDate(d.getUTCDate() - dow);
    return d.toISOString().slice(0, 10);
}

function addDays(dateStr, n) {
    const d = new Date(dateStr + 'T12:00:00Z');
    d.setUTCDate(d.getUTCDate() + n);
    return d.toISOString().slice(0, 10);
}

function weekDates(mon) {
    return Array.from({ length: 7 }, (_, i) => addDays(mon, i));
}

function formatDateShort(dateStr) {
    const d = new Date(dateStr + 'T12:00:00Z');
    const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    const months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
    return days[d.getUTCDay()] + ' ' + months[d.getUTCMonth()] + ' ' + d.getUTCDate();
}

function formatDateRange(mon) {
    const sun = addDays(mon, 6);
    const d0 = new Date(mon  + 'T12:00:00Z');
    const d1 = new Date(sun  + 'T12:00:00Z');
    const months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
    const m0 = months[d0.getUTCMonth()];
    const m1 = months[d1.getUTCMonth()];
    const prefix = m0 === m1 ? m0 + ' ' + d0.getUTCDate() : m0 + ' ' + d0.getUTCDate();
    return prefix + '–' + (m0 !== m1 ? m1 + ' ' : '') + d1.getUTCDate() + ', ' + d1.getUTCFullYear();
}

// Format "HH:MM" → "H:MM" (no leading zero on hour)
function formatTime(hhmm) {
    if (!hhmm) return '';
    const [h, m] = hhmm.split(':');
    return String(parseInt(h, 10)) + ':' + m;
}

function dayOfSeason(dateStr) {
    if (!settings.season_start_date) return null;
    const start = new Date(settings.season_start_date + 'T12:00:00Z');
    const d     = new Date(dateStr + 'T12:00:00Z');
    const days  = Math.floor((d - start) / 86400000) + 1;
    return days >= 1 ? days : null;
}

// ── Status message helper ─────────────────────────────────────────────────────

function showStatus(el, msg, isError, durationMs) {
    el.textContent = msg;
    el.style.color = isError ? 'var(--danger-color, #e74c3c)' : 'var(--success-color, #27ae60)';
    if (durationMs !== 0) {
        setTimeout(() => { el.textContent = ''; }, durationMs ?? 3000);
    }
}

// ── Tab switching ─────────────────────────────────────────────────────────────

function initTabs() {
    // Show initial active tab
    const activeBtn = document.querySelector('.tab-btn.active');
    if (activeBtn) {
        const target = document.getElementById('tab-' + activeBtn.dataset.tab);
        if (target) target.style.display = 'block';
    }

    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(t => { t.style.display = 'none'; });
            btn.classList.add('active');
            const target = document.getElementById('tab-' + btn.dataset.tab);
            if (target) target.style.display = 'block';
        });
    });
}

// ── Server event recurrence ───────────────────────────────────────────────────

function getServerEventOccurrencesInWeek(evt, dates) {
    // Returns a Set of dateStr values in dates[] where this event is active
    const active = new Set();
    if (!evt.active) return active;

    const duration = evt.duration_days || 1;

    function covers(startStr) {
        for (let i = 0; i < duration; i++) {
            const d = addDays(startStr, i);
            if (dates.includes(d)) active.add(d);
        }
    }

    if (evt.repeat_type === 'none') {
        if (evt.anchor_date) covers(evt.anchor_date);
        return active;
    }

    if (!evt.anchor_date) return active;
    const anchor = new Date(evt.anchor_date + 'T12:00:00Z');
    const weekStart = new Date(dates[0] + 'T12:00:00Z');
    const weekEnd   = new Date(dates[6] + 'T12:00:00Z');

    if (evt.repeat_type === 'every_n_days') {
        const n = evt.repeat_interval || 14;
        // Find first occurrence on or before weekEnd
        const diffMs = weekStart - anchor;
        const diffDays = Math.floor(diffMs / 86400000);
        let step = Math.floor(diffDays / n);
        if (step < 0) step = 0;
        // Walk from step-1 to cover events that start before the week but cover into it
        for (let s = Math.max(0, step - 1); ; s++) {
            const occDate = addDays(evt.anchor_date, s * n);
            const occ = new Date(occDate + 'T12:00:00Z');
            if (occ > weekEnd) break;
            covers(occDate);
        }
        return active;
    }

    // weekly or biweekly
    const step = evt.repeat_type === 'biweekly' ? 14 : 7;
    const targetDow = evt.repeat_weekday ?? 0; // 0=Mon…6=Sun
    // Find first occurrence from anchor matching repeat_weekday
    const anchorDow = (anchor.getUTCDay() + 6) % 7;
    let daysToFirst = (targetDow - anchorDow + 7) % 7;
    const firstStr = addDays(evt.anchor_date, daysToFirst);
    const firstD   = new Date(firstStr + 'T12:00:00Z');

    const diffFromFirst = weekStart - firstD;
    const diffDays2 = Math.floor(diffFromFirst / 86400000);
    let s = Math.floor(diffDays2 / step);
    if (s < 0) s = 0;
    for (let ss = Math.max(0, s - 1); ; ss++) {
        const occDate = addDays(firstStr, ss * step);
        const occ = new Date(occDate + 'T12:00:00Z');
        if (occ > weekEnd) break;
        covers(occDate);
    }
    return active;
}

// ── Week loading ──────────────────────────────────────────────────────────────

async function loadWeek() {
    const dates = weekDates(currentWeekStart);
    const from  = dates[0];
    const to    = dates[6];

    // Update navigator labels
    document.getElementById('week-range-label').textContent = formatDateRange(currentWeekStart);

    // Update day-card picker
    const picker = document.getElementById('day-card-picker');
    picker.replaceChildren();
    dates.forEach(d => {
        const opt = document.createElement('option');
        opt.value = d;
        opt.textContent = formatDateShort(d);
        picker.appendChild(opt);
    });

    try {
        const res = await fetch('/api/schedule/events?from=' + from + '&to=' + to);
        if (!res.ok) throw new Error('fetch failed');
        weekEvents = await res.json();
    } catch {
        weekEvents = [];
    }

    renderWeek(dates);
}

// ── Week rendering ────────────────────────────────────────────────────────────

function renderWeek(dates) {
    renderGrid(dates);
    // Mobile: show current mobile day
    syncMobileDay(dates);
}


function renderGrid(dates) {
    const grid = document.getElementById('week-grid');
    grid.replaceChildren();

    dates.forEach((d, i) => {
        const col = buildDayCol(d, i);
        grid.appendChild(col);
    });
}

function buildDayCol(dateStr, idx) {
    const today = todayGameDate();
    const vs    = getVSTheme(dateStr);
    const seDay = dayOfSeason(dateStr);
    const dow   = (new Date(dateStr + 'T12:00:00Z').getUTCDay() + 6) % 7; // Mon=0
    const isFriday = dow === 4;

    const col = document.createElement('div');
    col.className = 'day-col';
    col.dataset.date = dateStr;
    col.id = 'day-col-' + dateStr;
    if (idx === mobileActiveDayIndex) col.classList.add('mobile-active');

    // Server event strips (above date in header)
    const seBanners = serverEvents.filter(e =>
        getServerEventOccurrencesInWeek(e, weekDates(currentWeekStart)).has(dateStr));
    seBanners.forEach(evt => {
        const strip = document.createElement('div');
        strip.className = 'server-banner-strip';
        strip.textContent = evt.icon + ' ' + evt.name;
        strip.title = evt.name;
        col.appendChild(strip);
    });

    // Header
    const header = document.createElement('div');
    header.className = 'day-col-header';

    const dateLabel = document.createElement('div');
    dateLabel.className = 'day-col-date';
    dateLabel.textContent = formatDateShort(dateStr);
    header.appendChild(dateLabel);

    if (seDay !== null && settings.current_season) {
        const seLabel = document.createElement('div');
        seLabel.className = 'day-col-season';
        seLabel.textContent = 'S' + settings.current_season + ' D' + seDay;
        header.appendChild(seLabel);
    }

    const vsLabel = document.createElement('div');
    vsLabel.className = 'day-col-vs';
    vsLabel.textContent = vs.icon + ' ' + vs.label;
    header.appendChild(vsLabel);

    col.appendChild(header);

    // Events for this day, sorted by time
    const dayEvents = weekEvents
        .filter(e => e.event_date === dateStr)
        .sort((a, b) => a.event_time.localeCompare(b.event_time));

    dayEvents.forEach(evt => {
        col.appendChild(buildEventCard(evt, dateStr));
    });

    // Storm pills on Fridays
    if (isFriday) {
        buildStormPills().forEach(pill => col.appendChild(pill));
    }

    // Add Event button
    if (CAN_MANAGE) {
        const addBtn = document.createElement('button');
        addBtn.className = 'btn btn-ghost btn-sm btn-add-event-day';
        addBtn.textContent = '+ Add Event';
        addBtn.addEventListener('click', () => openAddEventModal(dateStr));
        col.appendChild(addBtn);
    }

    return col;
}

function buildEventCard(evt, dateStr) {
    const card = document.createElement('div');
    card.className = 'event-card';
    card.dataset.id = evt.id;

    const row = document.createElement('div');
    row.className = 'event-card-row';

    const nameSpan = document.createElement('span');
    nameSpan.className = 'event-card-name';
    nameSpan.textContent = evt.type_icon + ' ' + evt.type_short;
    row.appendChild(nameSpan);

    const timeSpan = document.createElement('span');
    timeSpan.className = 'event-card-time';
    timeSpan.textContent = evt.all_day ? 'All Day' : formatTime(evt.event_time) + ' ST';
    row.appendChild(timeSpan);

    if (evt.level != null) {
        const lvl = document.createElement('span');
        lvl.className = 'event-card-level';
        lvl.textContent = 'Lv.' + evt.level;
        row.appendChild(lvl);
    }

    card.appendChild(row);

    if (evt.notes) {
        const notes = document.createElement('div');
        notes.className = 'event-card-notes';
        notes.textContent = evt.notes;
        card.appendChild(notes);
    }

    if (CAN_MANAGE) {
        const actions = document.createElement('div');
        actions.className = 'event-card-actions';

        const editBtn = document.createElement('button');
        editBtn.className = 'btn btn-ghost btn-sm event-card-action-btn';
        const editIcon = document.createElement('span');
        editIcon.textContent = '✏️';
        const editLabel = document.createElement('span');
        editLabel.className = 'btn-label';
        editLabel.textContent = 'Edit';
        editBtn.appendChild(editIcon);
        editBtn.appendChild(editLabel);
        editBtn.addEventListener('click', () => openEditEventModal(evt));
        actions.appendChild(editBtn);

        const delBtn = document.createElement('button');
        delBtn.className = 'btn btn-danger btn-sm event-card-action-btn';
        const delIcon = document.createElement('span');
        delIcon.textContent = '🗑';
        const delLabel = document.createElement('span');
        delLabel.className = 'btn-label';
        delLabel.textContent = 'Delete';
        delBtn.appendChild(delIcon);
        delBtn.appendChild(delLabel);
        delBtn.addEventListener('click', () => confirmDeleteEvent(evt.id, actions, delBtn));
        actions.appendChild(delBtn);

        card.appendChild(actions);
    }

    return card;
}

function buildStormPills() {
    const pills = [];
    try {
        // TF A and TF B
        ['A', 'B'].forEach(tf => {
            const slotNum = stormTFConfig['tf_' + tf.toLowerCase() + '_slot'];
            if (!slotNum) return;
            const slotInfo = stormSlotTimes.find(s => s.slot === slotNum);
            if (!slotInfo) return;

            const pill = document.createElement('div');
            pill.className = 'storm-pill';
            pill.textContent = '⚡ TF ' + tf + ' – ' + formatTime(slotInfo.time_st) + ' ST';
            if (slotInfo.label) pill.title = slotInfo.label;
            pills.push(pill);
        });
    } catch {
        // graceful fallback: no storm pills
    }
    return pills;
}

// ── Mobile navigation ─────────────────────────────────────────────────────────

function syncMobileDay(dates) {
    const label = document.getElementById('mobile-day-label');
    const d = dates[mobileActiveDayIndex];
    label.textContent = formatDateShort(d);

    // Show only active day column
    document.querySelectorAll('.day-col').forEach((col, i) => {
        if (i === mobileActiveDayIndex) {
            col.classList.add('mobile-active');
        } else {
            col.classList.remove('mobile-active');
        }
    });
}

// ── Event CRUD ────────────────────────────────────────────────────────────────

function setAllDayUI(allDay) {
    const cb = document.getElementById('event-allday-input');
    const timeInput = document.getElementById('event-time-input');
    cb.checked = allDay;
    timeInput.disabled = allDay;
    if (allDay) timeInput.value = '';
}

function openAddEventModal(defaultDate) {
    document.getElementById('event-modal-title').textContent = 'Add Event';
    document.getElementById('event-modal-id').value = '';
    document.getElementById('event-date-input').value = defaultDate || todayGameDate();
    document.getElementById('event-time-input').value = '';
    document.getElementById('event-level-input').value = '';
    document.getElementById('event-notes-input').value = '';
    document.getElementById('event-form-error').textContent = '';
    setAllDayUI(false);
    populateEventTypeSelect(null);
    document.getElementById('event-modal').style.display = 'flex';
}

function openEditEventModal(evt) {
    document.getElementById('event-modal-title').textContent = 'Edit Event';
    document.getElementById('event-modal-id').value = evt.id;
    document.getElementById('event-date-input').value = evt.event_date;
    document.getElementById('event-time-input').value = evt.event_time;
    document.getElementById('event-level-input').value = evt.level ?? '';
    document.getElementById('event-notes-input').value = evt.notes || '';
    document.getElementById('event-form-error').textContent = '';
    setAllDayUI(evt.all_day === true);
    populateEventTypeSelect(evt.event_type_id);
    document.getElementById('event-modal').style.display = 'flex';
}

function populateEventTypeSelect(selectedId) {
    const sel = document.getElementById('event-type-select');
    sel.replaceChildren();
    eventTypes.forEach(et => {
        const opt = document.createElement('option');
        opt.value = et.id;
        opt.textContent = et.icon + ' ' + et.name;
        if (et.id === selectedId) opt.selected = true;
        sel.appendChild(opt);
    });
    updateEventModalForType();
}

function updateEventModalForType() {
    const sel = document.getElementById('event-type-select');
    const typeId = parseInt(sel.value, 10);
    const et = eventTypes.find(e => e.id === typeId);
    const isSystem = et && et.is_system;

    const lvlGroup = document.getElementById('event-level-group');
    const hint = document.getElementById('event-time-hint');

    lvlGroup.style.display = isSystem ? '' : 'none';

    if (!et) { hint.textContent = ''; return; }

    if (et.short_name === 'MG') {
        hint.textContent = 'Must start by 21:59 ST. Every-other-day rule applies.';
        // Update level placeholder with baseline
        document.getElementById('event-level-input').placeholder = settings.mg_baseline ?? '';
    } else if (et.short_name === 'ZS') {
        hint.textContent = 'Cooldown: 71.5h from last ZS start time.';
        document.getElementById('event-level-input').placeholder = settings.zs_baseline ?? '';
    } else {
        hint.textContent = '';
    }
}

async function saveEvent(e) {
    e.preventDefault();
    const errEl = document.getElementById('event-form-error');
    errEl.textContent = '';

    const id     = document.getElementById('event-modal-id').value;
    const allDay = document.getElementById('event-allday-input').checked;
    const body = {
        event_type_id: parseInt(document.getElementById('event-type-select').value, 10),
        event_date:    document.getElementById('event-date-input').value,
        event_time:    allDay ? '00:00' : document.getElementById('event-time-input').value,
        all_day:       allDay,
        notes:         document.getElementById('event-notes-input').value,
    };

    const lvlVal = document.getElementById('event-level-input').value;
    if (lvlVal !== '') body.level = parseInt(lvlVal, 10);

    const url    = id ? '/api/schedule/events/' + id : '/api/schedule/events';
    const method = id ? 'PUT' : 'POST';

    try {
        const res = await fetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const msg = await res.text();
            errEl.textContent = msg || 'Save failed';
            return;
        }
    } catch {
        errEl.textContent = 'Network error';
        return;
    }

    document.getElementById('event-modal').style.display = '';
    await loadWeek();
}

function setActionBtnContent(btn, icon, label) {
    btn.replaceChildren();
    const iconSpan = document.createElement('span');
    iconSpan.textContent = icon;
    const labelSpan = document.createElement('span');
    labelSpan.className = 'btn-label';
    labelSpan.textContent = label;
    btn.appendChild(iconSpan);
    btn.appendChild(labelSpan);
}

function confirmDeleteEvent(id, container, delBtn) {
    const editBtn = container.querySelector('.btn-ghost');
    if (editBtn) editBtn.style.display = 'none';

    // Swap delete button for Sure? + Cancel in-place
    const newDel = delBtn.cloneNode(false);
    setActionBtnContent(newDel, '🗑', 'Sure?');
    delBtn.replaceWith(newDel);

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-secondary btn-sm event-card-action-btn';
    setActionBtnContent(cancelBtn, '✕', 'Cancel');
    newDel.after(cancelBtn);

    newDel.addEventListener('click', async () => {
        await deleteEvent(id);
    });

    cancelBtn.addEventListener('click', () => {
        cancelBtn.remove();
        // Clone to strip all accumulated listeners before rewiring
        const freshDel = newDel.cloneNode(false);
        setActionBtnContent(freshDel, '🗑', 'Delete');
        newDel.replaceWith(freshDel);
        if (editBtn) editBtn.style.display = '';
        freshDel.addEventListener('click', () => confirmDeleteEvent(id, container, freshDel));
    });
}

async function deleteEvent(id) {
    try {
        await fetch('/api/schedule/events/' + id, { method: 'DELETE' });
    } catch { /* ignore */ }
    await loadWeek();
}

// ── Event Types tab ───────────────────────────────────────────────────────────

async function loadEventTypes() {
    try {
        const res = await fetch('/api/schedule/event-types');
        if (!res.ok) throw new Error();
        eventTypes = await res.json();
    } catch {
        eventTypes = [];
    }
    renderEventTypes();
}

function renderEventTypes() {
    const container = document.getElementById('event-types-list');
    container.replaceChildren();

    if (!eventTypes.length) {
        const p = document.createElement('p');
        p.className = 'empty-state';
        p.textContent = 'No event types.';
        container.appendChild(p);
        return;
    }

    eventTypes.forEach(et => {
        const row = document.createElement('div');
        row.className = 'event-type-row';

        const info = document.createElement('div');
        info.className = 'event-type-row-info';

        const name = document.createElement('div');
        name.className = 'event-type-row-name';
        name.textContent = et.icon + ' ' + et.name + ' (' + et.short_name + ')';
        info.appendChild(name);

        const meta = document.createElement('div');
        meta.className = 'event-type-row-meta';
        const tags = [];
        if (et.is_system) tags.push('System');
        if (!et.active) tags.push('Inactive');
        meta.textContent = tags.join(' · ') || 'Custom';
        info.appendChild(meta);

        row.appendChild(info);

        if (CAN_MANAGE) {
            const actions = document.createElement('div');
            actions.className = 'event-type-row-actions';

            if (!et.is_system) {
                const editBtn = document.createElement('button');
                editBtn.className = 'btn btn-ghost btn-sm';
                editBtn.textContent = '✏️ Edit';
                editBtn.addEventListener('click', () => openEditEventTypeModal(et));
                actions.appendChild(editBtn);

                const delBtn = document.createElement('button');
                delBtn.className = 'btn btn-danger btn-sm';
                delBtn.textContent = 'Delete';
                delBtn.addEventListener('click', () => confirmDeleteEventType(et.id, actions, delBtn));
                actions.appendChild(delBtn);
            }

            row.appendChild(actions);
        }

        container.appendChild(row);
    });
}

function openAddEventTypeModal() {
    document.getElementById('event-type-modal-title').textContent = 'Add Event Type';
    document.getElementById('event-type-modal-id').value = '';
    document.getElementById('et-name').value = '';
    document.getElementById('et-short').value = '';
    document.getElementById('et-icon').value = '📅';
    document.getElementById('et-active').checked = true;
    document.getElementById('event-type-form-error').textContent = '';
    document.getElementById('event-type-modal').style.display = 'flex';
}

function openEditEventTypeModal(et) {
    document.getElementById('event-type-modal-title').textContent = 'Edit Event Type';
    document.getElementById('event-type-modal-id').value = et.id;
    document.getElementById('et-name').value = et.name;
    document.getElementById('et-short').value = et.short_name;
    document.getElementById('et-icon').value = et.icon;
    document.getElementById('et-active').checked = et.active;
    document.getElementById('event-type-form-error').textContent = '';
    document.getElementById('event-type-modal').style.display = 'flex';
}

async function saveEventType(e) {
    e.preventDefault();
    const errEl = document.getElementById('event-type-form-error');
    errEl.textContent = '';

    const id   = document.getElementById('event-type-modal-id').value;
    const body = {
        name:       document.getElementById('et-name').value,
        short_name: document.getElementById('et-short').value,
        icon:       document.getElementById('et-icon').value || '📅',
        active:     document.getElementById('et-active').checked,
    };

    const url    = id ? '/api/schedule/event-types/' + id : '/api/schedule/event-types';
    const method = id ? 'PUT' : 'POST';

    try {
        const res = await fetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            errEl.textContent = await res.text() || 'Save failed';
            return;
        }
    } catch {
        errEl.textContent = 'Network error';
        return;
    }

    document.getElementById('event-type-modal').style.display = '';
    await loadEventTypes();
}

function confirmDeleteEventType(id, container, delBtn) {
    delBtn.style.display = 'none';

    const span = document.createElement('span');
    span.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';

    const label = document.createElement('span');
    label.textContent = 'Sure?';
    label.style.fontSize = '0.82rem';

    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-danger btn-sm';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', async () => {
        const res = await fetch('/api/schedule/event-types/' + id, { method: 'DELETE' });
        if (!res.ok) {
            const errEl = document.createElement('span');
            errEl.style.color = 'var(--danger-color,#e74c3c)';
            errEl.style.fontSize = '0.82rem';
            errEl.textContent = await res.text() || 'Delete failed';
            span.replaceWith(errEl);
            delBtn.style.display = '';
            setTimeout(() => errEl.remove(), 4000);
            return;
        }
        span.remove();
        await loadEventTypes();
    });

    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => {
        span.remove();
        delBtn.style.display = '';
    });

    span.append(label, yesBtn, noBtn);
    container.appendChild(span);
}

// ── Server Events tab ─────────────────────────────────────────────────────────

async function loadServerEvents() {
    try {
        const res = await fetch('/api/schedule/server-events');
        if (!res.ok) throw new Error();
        serverEvents = await res.json();
    } catch {
        serverEvents = [];
    }
    renderServerEvents();
}

function repeatLabel(evt) {
    switch (evt.repeat_type) {
        case 'none':         return 'One-time';
        case 'weekly':       return 'Weekly';
        case 'biweekly':     return 'Biweekly';
        case 'every_n_days': return 'Every ' + evt.repeat_interval + ' days';
        default:             return evt.repeat_type;
    }
}

function renderServerEvents() {
    const container = document.getElementById('server-events-list');
    container.replaceChildren();

    if (!serverEvents.length) {
        const p = document.createElement('p');
        p.className = 'empty-state';
        p.textContent = 'No server events configured.';
        container.appendChild(p);
        return;
    }

    serverEvents.forEach(evt => {
        const row = document.createElement('div');
        row.className = 'server-event-row';

        const info = document.createElement('div');
        info.className = 'server-event-row-info';

        const name = document.createElement('div');
        name.className = 'server-event-row-name';
        name.textContent = evt.icon + ' ' + evt.name + ' (' + evt.short_name + ')';
        info.appendChild(name);

        const meta = document.createElement('div');
        meta.className = 'server-event-row-meta';
        const parts = [repeatLabel(evt), evt.duration_days + 'd'];
        if (!evt.active) parts.push('Inactive');
        if (evt.anchor_date) parts.push('Anchor: ' + evt.anchor_date);
        meta.textContent = parts.join(' · ');
        info.appendChild(meta);

        row.appendChild(info);

        if (CAN_MANAGE) {
            const actions = document.createElement('div');
            actions.className = 'server-event-row-actions';

            const editBtn = document.createElement('button');
            editBtn.className = 'btn btn-ghost btn-sm';
            editBtn.textContent = '✏️ Edit';
            editBtn.addEventListener('click', () => openEditServerEventModal(evt));
            actions.appendChild(editBtn);

            const delBtn = document.createElement('button');
            delBtn.className = 'btn btn-danger btn-sm';
            delBtn.textContent = 'Delete';
            delBtn.addEventListener('click', () => confirmDeleteServerEvent(evt.id, actions, delBtn));
            actions.appendChild(delBtn);

            row.appendChild(actions);
        }

        container.appendChild(row);
    });
}

function openAddServerEventModal() {
    document.getElementById('server-event-modal-title').textContent = 'Add Server Event';
    document.getElementById('se-modal-id').value = '';
    document.getElementById('se-name').value = '';
    document.getElementById('se-short').value = '';
    document.getElementById('se-icon').value = '🌐';
    document.getElementById('se-duration').value = '1';
    document.getElementById('se-repeat').value = 'none';
    document.getElementById('se-weekday').value = '0';
    document.getElementById('se-interval').value = '';
    document.getElementById('se-anchor').value = '';
    document.getElementById('se-active').checked = true;
    document.getElementById('server-event-form-error').textContent = '';
    updateServerEventRepeatUI();
    document.getElementById('server-event-modal').style.display = 'flex';
}

function openEditServerEventModal(evt) {
    document.getElementById('server-event-modal-title').textContent = 'Edit Server Event';
    document.getElementById('se-modal-id').value = evt.id;
    document.getElementById('se-name').value = evt.name;
    document.getElementById('se-short').value = evt.short_name;
    document.getElementById('se-icon').value = evt.icon;
    document.getElementById('se-duration').value = evt.duration_days;
    document.getElementById('se-repeat').value = evt.repeat_type;
    document.getElementById('se-weekday').value = evt.repeat_weekday ?? 0;
    document.getElementById('se-interval').value = evt.repeat_interval ?? '';
    document.getElementById('se-anchor').value = evt.anchor_date || '';
    document.getElementById('se-active').checked = evt.active;
    document.getElementById('server-event-form-error').textContent = '';
    updateServerEventRepeatUI();
    document.getElementById('server-event-modal').style.display = 'flex';
}

function updateServerEventRepeatUI() {
    const rt = document.getElementById('se-repeat').value;
    document.getElementById('se-weekday-group').style.display  = (rt === 'weekly' || rt === 'biweekly') ? '' : 'none';
    document.getElementById('se-interval-group').style.display = (rt === 'every_n_days') ? '' : 'none';
    document.getElementById('se-anchor-group').style.display   = (rt !== 'none') ? '' : 'none';
}

async function saveServerEvent(e) {
    e.preventDefault();
    const errEl = document.getElementById('server-event-form-error');
    errEl.textContent = '';

    const id  = document.getElementById('se-modal-id').value;
    const rt  = document.getElementById('se-repeat').value;
    const body = {
        name:          document.getElementById('se-name').value,
        short_name:    document.getElementById('se-short').value,
        icon:          document.getElementById('se-icon').value || '🌐',
        duration_days: parseInt(document.getElementById('se-duration').value, 10),
        repeat_type:   rt,
        active:        document.getElementById('se-active').checked,
        anchor_date:   document.getElementById('se-anchor').value || null,
    };

    if (rt === 'weekly' || rt === 'biweekly') {
        body.repeat_weekday = parseInt(document.getElementById('se-weekday').value, 10);
    }
    if (rt === 'every_n_days') {
        body.repeat_interval = parseInt(document.getElementById('se-interval').value, 10);
    }

    const url    = id ? '/api/schedule/server-events/' + id : '/api/schedule/server-events';
    const method = id ? 'PUT' : 'POST';

    try {
        const res = await fetch(url, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            errEl.textContent = await res.text() || 'Save failed';
            return;
        }
    } catch {
        errEl.textContent = 'Network error';
        return;
    }

    document.getElementById('server-event-modal').style.display = '';
    await loadServerEvents();
    // Re-render week so banners update
    await loadWeek();
}

function confirmDeleteServerEvent(id, container, delBtn) {
    delBtn.style.display = 'none';

    const span = document.createElement('span');
    span.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';

    const label = document.createElement('span');
    label.textContent = 'Sure?';
    label.style.fontSize = '0.82rem';

    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-danger btn-sm';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', async () => {
        await fetch('/api/schedule/server-events/' + id, { method: 'DELETE' });
        span.remove();
        await loadServerEvents();
        await loadWeek();
    });

    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => {
        span.remove();
        delBtn.style.display = '';
    });

    span.append(label, yesBtn, noBtn);
    container.appendChild(span);
}

// ── Settings tab ──────────────────────────────────────────────────────────────

function populateSettingsForm() {
    document.getElementById('set-mg-baseline').value   = settings.mg_baseline ?? '';
    document.getElementById('set-zs-baseline').value   = settings.zs_baseline ?? '';
    document.getElementById('set-mg-time').value       = settings.mg_default_time ?? '';
    document.getElementById('set-zs-time').value       = settings.zs_default_time ?? '';
    document.getElementById('set-season').value        = settings.current_season ?? '';
    document.getElementById('set-season-start').value  = settings.season_start_date ?? '';

    // Generation rule settings
    document.getElementById('gen-mg-anchor').value     = settings.mg_anchor_date ?? '';
    const mode = settings.zs_schedule_mode || 'weekdays';
    document.getElementById('zs-mode-weekdays').checked = mode === 'weekdays';
    document.getElementById('zs-mode-asap').checked    = mode === 'asap';
    updateZSModeUI(mode);

    // ZS weekday checkboxes
    const wds = (settings.zs_weekdays || '1,4').split(',').map(Number);
    document.querySelectorAll('input[name="zs-wd"]').forEach(cb => {
        cb.checked = wds.includes(parseInt(cb.value, 10));
    });

    document.getElementById('gen-zs-anchor').value      = settings.zs_anchor_date ?? '';
    document.getElementById('gen-zs-anchor-time').value = settings.zs_anchor_time ?? '';

    // Default generate range: today → today+28
    const today = todayGameDate();
    document.getElementById('gen-from').value = today;
    document.getElementById('gen-to').value   = addDays(today, 28);
}

function updateZSModeUI(mode) {
    document.getElementById('zs-weekdays-config').style.display = (mode === 'weekdays') ? '' : 'none';
    document.getElementById('zs-asap-config').style.display     = (mode === 'asap') ? 'flex' : 'none';
}

// Merge patch fields into the current server settings and PUT the full object.
// This prevents partial saves from zeroing out unrelated settings columns.
async function patchSettings(patch) {
    const currentRes = await fetch('/api/settings');
    if (!currentRes.ok) throw new Error('Could not load current settings');
    const current = await currentRes.json();
    const merged = Object.assign({}, current, patch);
    const res = await fetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(merged),
    });
    if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || 'Save failed');
    }
    // Refresh local settings state from server
    const r2 = await fetch('/api/settings');
    if (r2.ok) settings = await r2.json();
}

async function saveSettings() {
    const statusEl = document.getElementById('settings-status');
    const mgBaselineVal = document.getElementById('set-mg-baseline').value;
    const zsBaselineVal = document.getElementById('set-zs-baseline').value;

    const patch = {
        mg_baseline:      mgBaselineVal !== '' ? parseInt(mgBaselineVal, 10) : (settings.mg_baseline ?? 11),
        zs_baseline:      zsBaselineVal !== '' ? parseInt(zsBaselineVal, 10) : (settings.zs_baseline ?? 7),
        mg_default_time:  document.getElementById('set-mg-time').value || settings.mg_default_time || '00:30',
        zs_default_time:  document.getElementById('set-zs-time').value || settings.zs_default_time || '23:00',
        current_season:   document.getElementById('set-season').value ? parseInt(document.getElementById('set-season').value, 10) : null,
        season_start_date: document.getElementById('set-season-start').value || null,
        mg_anchor_date:   document.getElementById('gen-mg-anchor').value || null,
        zs_schedule_mode: document.querySelector('input[name="zs-mode"]:checked')?.value || 'weekdays',
        zs_weekdays:      Array.from(document.querySelectorAll('input[name="zs-wd"]:checked')).map(cb => cb.value).join(',') || '1,4',
        zs_anchor_date:   document.getElementById('gen-zs-anchor').value || null,
        zs_anchor_time:   document.getElementById('gen-zs-anchor-time').value || '23:00',
    };

    try {
        await patchSettings(patch);
        showStatus(statusEl, 'Saved', false);
        renderWeek(weekDates(currentWeekStart));
        updateSeasonSubtitle();
    } catch (err) {
        showStatus(statusEl, err.message || 'Network error', true);
    }
}

async function generateEvents() {
    const statusEl = document.getElementById('generate-status');
    const body = {
        from:  document.getElementById('gen-from').value,
        to:    document.getElementById('gen-to').value,
        types: [],
    };
    if (document.getElementById('gen-mg').checked) body.types.push('mg');
    if (document.getElementById('gen-zs').checked) body.types.push('zs');

    if (!body.types.length) {
        showStatus(statusEl, 'Select at least one type.', true);
        return;
    }

    // Save anchor / rule settings first (merge into full settings to avoid zeroing other fields)
    const savePatch = {
        mg_anchor_date:   document.getElementById('gen-mg-anchor').value || null,
        zs_schedule_mode: document.querySelector('input[name="zs-mode"]:checked')?.value || 'weekdays',
        zs_weekdays:      Array.from(document.querySelectorAll('input[name="zs-wd"]:checked')).map(cb => cb.value).join(',') || '1,4',
        zs_anchor_date:   document.getElementById('gen-zs-anchor').value || null,
        zs_anchor_time:   document.getElementById('gen-zs-anchor-time').value || '23:00',
    };
    try { await patchSettings(savePatch); } catch { /* non-fatal; generate will use whatever's in DB */ }

    try {
        showStatus(statusEl, 'Generating…', false, 0);
        const res = await fetch('/api/schedule/events/generate', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            showStatus(statusEl, await res.text() || 'Generation failed', true);
            return;
        }
        const data = await res.json();
        showStatus(statusEl,
            'Created ' + data.mg_created + ' MG, ' + data.zs_created + ' ZS events.', false);
        await loadWeek();
    } catch {
        showStatus(statusEl, 'Network error', true);
    }
}

// ── Season subtitle ───────────────────────────────────────────────────────────

function updateSeasonSubtitle() {
    const el = document.getElementById('season-subtitle');
    if (!el) return;
    if (settings.current_season && settings.season_start_date) {
        const d = todayGameDate();
        const seDay = dayOfSeason(d);
        el.textContent = seDay
            ? 'Season ' + settings.current_season + ' · Day ' + seDay
            : 'Season ' + settings.current_season;
    } else {
        el.textContent = '';
    }
}

// ── Text output ───────────────────────────────────────────────────────────────

function buildTextOutput() {
    const dates = weekDates(currentWeekStart);
    const lines = [];

    dates.forEach(d => {
        const vs = getVSTheme(d);
        const seDay = dayOfSeason(d);
        const dow = (new Date(d + 'T12:00:00Z').getUTCDay() + 6) % 7;
        const isFriday = dow === 4;

        let header = formatDateShort(d) + ' · ' + vs.label + ' ' + vs.icon;
        if (seDay !== null && settings.current_season) {
            header += ' (S' + settings.current_season + ' D' + seDay + ')';
        }
        lines.push(header);

        const dayEvts = weekEvents
            .filter(e => e.event_date === d)
            .sort((a, b) => a.event_time.localeCompare(b.event_time));

        dayEvts.forEach(evt => {
            let line = '  ' + evt.type_icon + ' ' + evt.type_name + (evt.all_day ? ' — All Day' : ' @ ' + formatTime(evt.event_time) + ' ST');
            if (evt.level != null) line += '  Lv.' + evt.level;
            if (evt.notes) line += '  — ' + evt.notes;
            lines.push(line);
        });

        // Storm pills
        if (isFriday) {
            try {
                ['A', 'B'].forEach(tf => {
                    const slotNum = stormTFConfig['tf_' + tf.toLowerCase() + '_slot'];
                    if (!slotNum) return;
                    const slot = stormSlotTimes.find(s => s.slot === slotNum);
                    if (!slot) return;
                    lines.push('  ⚡ TF ' + tf + ' @ ' + formatTime(slot.time_st) + ' ST');
                });
            } catch { /* no storm info */ }
        }

        // Server event banners
        const seBanners = serverEvents.filter(evt => {
            return getServerEventOccurrencesInWeek(evt, dates).has(d);
        });
        seBanners.forEach(evt => {
            lines.push('  ' + evt.icon + ' ' + evt.name);
        });

        lines.push('');
    });

    return lines.join('\n');
}

// ── Canvas: Week Image ────────────────────────────────────────────────────────

function drawWeekImage() {
    const dates = weekDates(currentWeekStart);

    // ── Palette (matches site theme) ──────────────────────────────────────
    const C = {
        pageBg1:     '#0d1b35',          // deep navy top
        pageBg2:     '#162344',          // deep navy bottom
        cardBg:      '#1a2540',          // card fill — solid, readable
        cardBorder:  '#2e3f6e',          // subtle blue border
        hdrBg1:      '#667eea',          // accent gradient start (site primary)
        hdrBg2:      '#764ba2',          // accent gradient end  (site primary)
        hdrText:     '#ffffff',
        dateText:    '#ffffff',
        seasonText:  'rgba(255,255,255,0.75)',
        vsText:      'rgba(255,255,255,0.65)',
        evtName:     '#e9ecef',          // --text-primary dark
        evtTime:     '#a0aec0',          // muted
        divider:     '#2e3f6e',
        bannerBg:    '#1e3a5f',          // server event teal-navy strip
        bannerText:  '#90cdf4',
        stormBg:     '#7c4f08',          // amber toned down so text is readable
        stormText:   '#fcd34d',          // bright amber text
        stormBorder: '#f59e0b',
    };

    // ── Layout ─────────────────────────────────────────────────────────────
    // 4 columns top (Mon–Thu), 3 columns bottom (Fri–Sun)
    const ROWS  = [[0,1,2,3], [4,5,6]];
    const colW  = 230;
    const padX  = 14;
    const hdrH  = 62;
    const rowH  = 24;
    const banH  = 22;   // server-event banner strip
    const gap   = 8;
    const rowGap = 14;
    const font  = 'Segoe UI, Tahoma, Verdana, sans-serif';

    // Size rows by the busiest column
    let maxEvts = 0;
    dates.forEach(d => {
        let n = weekEvents.filter(e => e.event_date === d).length;
        const dow = (new Date(d + 'T12:00:00Z').getUTCDay() + 6) % 7;
        if (dow === 4) n += 2;
        if (n > maxEvts) maxEvts = n;
    });
    const colH   = banH + gap + hdrH + gap + Math.max(maxEvts, 1) * rowH + gap * 2;
    const totalW = 4 * colW + 5 * gap;
    const totalH = gap + colH + rowGap + colH + gap;

    const canvas = document.getElementById('schedule-canvas');
    canvas.width  = totalW;
    canvas.height = totalH;

    const ctx = canvas.getContext('2d');

    // ── Page background ────────────────────────────────────────────────────
    const pageBg = ctx.createLinearGradient(0, 0, 0, totalH);
    pageBg.addColorStop(0, C.pageBg1);
    pageBg.addColorStop(1, C.pageBg2);
    ctx.fillStyle = pageBg;
    ctx.fillRect(0, 0, totalW, totalH);

    // ── Helper: fit text (full → short) ────────────────────────────────────
    function fitText(full, short, maxW) {
        return ctx.measureText(full).width <= maxW ? full : short;
    }

    // ── Draw one day column ────────────────────────────────────────────────
    function drawDayColumn(d, colIdx, rowIdx) {
        let xOff = 0;
        if (rowIdx === 1) xOff = Math.round((colW + gap) / 2); // centre 3-col row
        const x = gap + colIdx * (colW + gap) + xOff;
        const y = gap + rowIdx * (colH + rowGap);

        // Card body
        ctx.fillStyle = C.cardBg;
        roundRect(ctx, x, y, colW, colH, 10);
        ctx.fill();
        ctx.strokeStyle = C.cardBorder;
        ctx.lineWidth = 1.5;
        roundRect(ctx, x, y, colW, colH, 10);
        ctx.stroke();

        // Server event strip (above header)
        const seBanners = serverEvents.filter(e =>
            getServerEventOccurrencesInWeek(e, dates).has(d));
        ctx.fillStyle = C.bannerBg;
        roundRect(ctx, x, y, colW, banH, [10, 10, 0, 0]);
        ctx.fill();
        if (seBanners.length) {
            ctx.fillStyle = C.bannerText;
            ctx.font = '600 11px ' + font;
            ctx.textAlign = 'center';
            const bFull  = seBanners.map(e => e.icon + ' ' + e.name).join('  ');
            const bShort = seBanners.map(e => e.icon + ' ' + e.short_name).join('  ');
            ctx.fillText(fitText(bFull, bShort, colW - 10), x + colW / 2, y + banH - 5, colW - 10);
        }

        // Header — accent gradient
        const hdrY = y + banH + gap;
        const hdrGrad = ctx.createLinearGradient(x, hdrY, x + colW, hdrY);
        hdrGrad.addColorStop(0, C.hdrBg1);
        hdrGrad.addColorStop(1, C.hdrBg2);
        ctx.fillStyle = hdrGrad;
        roundRect(ctx, x + 4, hdrY, colW - 8, hdrH, 6);
        ctx.fill();

        // Date
        ctx.fillStyle = C.dateText;
        ctx.font = 'bold 15px ' + font;
        ctx.textAlign = 'center';
        ctx.fillText(formatDateShort(d), x + colW / 2, hdrY + 20, colW - 12);

        // Season day (small, under date)
        const seDay = dayOfSeason(d);
        const hasSeasonInfo = seDay !== null && settings.current_season;
        if (hasSeasonInfo) {
            ctx.fillStyle = C.seasonText;
            ctx.font = '11px ' + font;
            ctx.fillText('S' + settings.current_season + ' · D' + seDay, x + colW / 2, hdrY + 36, colW - 12);
        }

        // VS theme (bottom of header)
        const vs = getVSTheme(d);
        ctx.fillStyle = 'rgba(255,255,255,0.80)';
        ctx.font = '11px ' + font;
        const vsBaseY = hasSeasonInfo ? hdrY + hdrH - 10 : hdrY + hdrH - 8;
        ctx.fillText(
            fitText(vs.icon + ' ' + vs.label, vs.icon + ' ' + vs.short, colW - 12),
            x + colW / 2, vsBaseY, colW - 12
        );

        // Divider below header
        const divY = hdrY + hdrH + 4;
        ctx.strokeStyle = C.divider;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(x + 8, divY);
        ctx.lineTo(x + colW - 8, divY);
        ctx.stroke();

        // Events
        const dayEvts = weekEvents
            .filter(e => e.event_date === d)
            .sort((a, b) => a.event_time.localeCompare(b.event_time));

        ctx.font = '12px ' + font;
        const timeW     = Math.ceil(ctx.measureText('00:00').width) + 4;
        const nameAvailW = colW - padX * 2 - timeW - 6;

        let evtY = divY + rowH - 6;
        dayEvts.forEach(evt => {
            ctx.font = '12px ' + font;

            // Name + level — left
            ctx.fillStyle = C.evtName;
            ctx.textAlign = 'left';
            const lvl      = evt.level != null ? ' Lv.' + evt.level : '';
            const nameFull  = evt.type_icon + ' ' + evt.type_name  + lvl;
            const nameShort = evt.type_icon + ' ' + evt.type_short + lvl;
            ctx.fillText(fitText(nameFull, nameShort, nameAvailW), x + padX, evtY, nameAvailW);

            // Time — right
            ctx.fillStyle = C.evtTime;
            ctx.textAlign = 'right';
            ctx.fillText(evt.all_day ? 'All Day' : formatTime(evt.event_time), x + colW - padX, evtY);

            evtY += rowH;
        });

        // Storm pills (Friday only)
        const dow = (new Date(d + 'T12:00:00Z').getUTCDay() + 6) % 7;
        if (dow === 4) {
            try {
                ['A', 'B'].forEach(tf => {
                    const slotNum = stormTFConfig['tf_' + tf.toLowerCase() + '_slot'];
                    if (!slotNum) return;
                    const slot = stormSlotTimes.find(s => s.slot === slotNum);
                    if (!slot) return;

                    const pillW = colW - padX * 2;
                    ctx.fillStyle = C.stormBg;
                    roundRect(ctx, x + padX, evtY - 16, pillW, 18, 4);
                    ctx.fill();
                    ctx.strokeStyle = C.stormBorder;
                    ctx.lineWidth = 1;
                    roundRect(ctx, x + padX, evtY - 16, pillW, 18, 4);
                    ctx.stroke();

                    ctx.fillStyle = C.stormText;
                    ctx.font = 'bold 10px ' + font;
                    ctx.textAlign = 'center';
                    ctx.fillText('⚡ TF ' + tf + '  ' + formatTime(slot.time_st) + ' ST', x + colW / 2, evtY - 3, pillW - 4);
                    evtY += rowH;
                });
            } catch { /* no storm info */ }
        }
    }

    ROWS[0].forEach((dayIdx, colIdx) => drawDayColumn(dates[dayIdx], colIdx, 0));
    ROWS[1].forEach((dayIdx, colIdx) => drawDayColumn(dates[dayIdx], colIdx, 1));
}

// ── Canvas: Day Card ──────────────────────────────────────────────────────────

function drawDayCard(dateStr) {
    // ── Palette (same as week image) ──────────────────────────────────────
    const C = {
        pageBg1:    '#0d1b35',
        pageBg2:    '#162344',
        cardBg:     '#1a2540',
        cardBorder: '#2e3f6e',
        hdrBg1:     '#667eea',
        hdrBg2:     '#764ba2',
        hdrText:    '#ffffff',
        seasonText: 'rgba(255,255,255,0.75)',
        vsText:     'rgba(255,255,255,0.80)',
        evtName:    '#e9ecef',
        evtTime:    '#a0aec0',
        notesText:  'rgba(255,255,255,0.45)',
        divider:    '#2e3f6e',
        bannerBg:   '#1e3a5f',
        bannerText: '#90cdf4',
        stormBg:    '#7c4f08',
        stormText:  '#fcd34d',
        stormBorder:'#f59e0b',
    };

    const font  = 'Segoe UI, Tahoma, Verdana, sans-serif';
    const pad   = 32;  // horizontal padding inside card
    const vs    = getVSTheme(dateStr);
    const seDay = dayOfSeason(dateStr);
    const dow   = (new Date(dateStr + 'T12:00:00Z').getUTCDay() + 6) % 7;
    const isFri = dow === 4;

    const dates     = weekDates(currentWeekStart);
    const seBanners = serverEvents.filter(e => getServerEventOccurrencesInWeek(e, dates).has(dateStr));
    const dayEvts   = weekEvents
        .filter(e => e.event_date === dateStr)
        .sort((a, b) => a.event_time.localeCompare(b.event_time));

    // ── Dynamic height ─────────────────────────────────────────────────────
    const hdrH    = 80;
    const banH    = seBanners.length ? 28 : 0;
    const evtRowH = 28;
    const noteLineH = 15;
    const stormH  = isFri ? 28 : 0;
    const W = 600;
    const noteMaxW = W - pad * 2 - 20;

    // Temporary context to measure note line wrapping before drawing
    const _tmpCanvas = document.createElement('canvas');
    const _tmpCtx = _tmpCanvas.getContext('2d');
    _tmpCtx.font = '11px ' + font;
    function countNoteLines(evt) {
        if (!evt.notes) return 0;
        return wrapNoteLines(_tmpCtx, evt.notes, noteMaxW).length;
    }

    let eventsH = dayEvts.reduce((s, e) => s + evtRowH + countNoteLines(e) * noteLineH, 0);
    if (!dayEvts.length) eventsH = evtRowH; // "No events" placeholder
    const H = 16 + hdrH + 12 + eventsH + stormH + banH + 24;
    const canvas = document.getElementById('schedule-canvas');
    canvas.width  = W;
    canvas.height = Math.max(H, 220);

    const ctx = canvas.getContext('2d');

    // ── Background ─────────────────────────────────────────────────────────
    const bgGrad = ctx.createLinearGradient(0, 0, 0, canvas.height);
    bgGrad.addColorStop(0, C.pageBg1);
    bgGrad.addColorStop(1, C.pageBg2);
    ctx.fillStyle = bgGrad;
    ctx.fillRect(0, 0, W, canvas.height);

    // ── Outer card ─────────────────────────────────────────────────────────
    ctx.fillStyle = C.cardBg;
    roundRect(ctx, 12, 12, W - 24, canvas.height - 24, 12);
    ctx.fill();
    ctx.strokeStyle = C.cardBorder;
    ctx.lineWidth = 1.5;
    roundRect(ctx, 12, 12, W - 24, canvas.height - 24, 12);
    ctx.stroke();

    // ── Header band ────────────────────────────────────────────────────────
    const hdrGrad = ctx.createLinearGradient(12, 0, W - 12, 0);
    hdrGrad.addColorStop(0, C.hdrBg1);
    hdrGrad.addColorStop(1, C.hdrBg2);
    ctx.fillStyle = hdrGrad;
    roundRect(ctx, 12, 12, W - 24, hdrH, [12, 12, 0, 0]);
    ctx.fill();

    // Date
    ctx.fillStyle = C.hdrText;
    ctx.font = 'bold 24px ' + font;
    ctx.textAlign = 'center';
    ctx.fillText(formatDateShort(dateStr), W / 2, 12 + 32);

    // Season + VS theme row
    const subParts = [];
    if (seDay !== null && settings.current_season) {
        subParts.push('S' + settings.current_season + ' · D' + seDay);
    }
    const vsAvailW = W - pad * 2;
    const vsFull  = vs.icon + ' ' + vs.label;
    const vsShort = vs.icon + ' ' + vs.short;
    const vsText  = ctx.measureText(vsFull).width + (subParts.length ? ctx.measureText('   ' + subParts[0]).width : 0) <= vsAvailW
        ? vsFull : vsShort;
    if (subParts.length) subParts.unshift(vsText); else subParts.push(vsText);
    ctx.fillStyle = 'rgba(255,255,255,0.82)';
    ctx.font = '13px ' + font;
    ctx.fillText(subParts.join('   '), W / 2, 12 + 56);

    // ── Divider ────────────────────────────────────────────────────────────
    const divY = 12 + hdrH + 6;
    ctx.strokeStyle = C.divider;
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(pad, divY);
    ctx.lineTo(W - pad, divY);
    ctx.stroke();

    // ── Events ─────────────────────────────────────────────────────────────
    ctx.font = '14px ' + font;
    const timeColW   = Math.ceil(ctx.measureText('00:00 ST').width) + 8;
    const nameAvailW = W - pad * 2 - timeColW - 8;

    let y = divY + 20;

    if (!dayEvts.length) {
        ctx.fillStyle = C.evtTime;
        ctx.font = '13px ' + font;
        ctx.textAlign = 'center';
        ctx.fillText('No events scheduled', W / 2, y);
        y += evtRowH;
    }

    dayEvts.forEach(evt => {
        ctx.font = '14px ' + font;

        // Name + level — left
        ctx.fillStyle = C.evtName;
        ctx.textAlign = 'left';
        const lvl       = evt.level != null ? ' Lv.' + evt.level : '';
        const nameFull  = evt.type_icon + ' ' + evt.type_name  + lvl;
        const nameShort = evt.type_icon + ' ' + evt.type_short + lvl;
        const nameLabel = ctx.measureText(nameFull).width <= nameAvailW ? nameFull : nameShort;
        ctx.fillText(nameLabel, pad, y, nameAvailW);

        // Time — right
        ctx.fillStyle = C.evtTime;
        ctx.font = '13px ' + font;
        ctx.textAlign = 'right';
        ctx.fillText(evt.all_day ? 'All Day' : formatTime(evt.event_time) + ' ST', W - pad, y);

        y += evtRowH;

        if (evt.notes) {
            ctx.fillStyle = C.notesText;
            ctx.font = '11px ' + font;
            ctx.textAlign = 'left';
            const noteLines = wrapNoteLines(ctx, evt.notes, noteMaxW);
            noteLines.forEach((line, li) => {
                ctx.fillText(line, pad + 20, y - 4 + li * noteLineH);
            });
            y += noteLines.length * noteLineH;
        }
    });

    // ── Storm pills ────────────────────────────────────────────────────────
    if (isFri) {
        try {
            ['A', 'B'].forEach(tf => {
                const slotNum = stormTFConfig['tf_' + tf.toLowerCase() + '_slot'];
                if (!slotNum) return;
                const slot = stormSlotTimes.find(s => s.slot === slotNum);
                if (!slot) return;

                const pillW = (W - pad * 2 - 8) / 2;
                const pillX = tf === 'A' ? pad : pad + pillW + 8;

                ctx.fillStyle = C.stormBg;
                roundRect(ctx, pillX, y - 2, pillW, 22, 5);
                ctx.fill();
                ctx.strokeStyle = C.stormBorder;
                ctx.lineWidth = 1;
                roundRect(ctx, pillX, y - 2, pillW, 22, 5);
                ctx.stroke();

                ctx.fillStyle = C.stormText;
                ctx.font = 'bold 11px ' + font;
                ctx.textAlign = 'center';
                ctx.fillText('⚡ TF ' + tf + '  ' + formatTime(slot.time_st) + ' ST', pillX + pillW / 2, y + 13, pillW - 8);
            });
            y += 28;
        } catch { /* no storm */ }
    }

    // ── Server event banner (bottom) ───────────────────────────────────────
    if (seBanners.length) {
        ctx.fillStyle = C.bannerBg;
        roundRect(ctx, pad, y, W - pad * 2, 22, 5);
        ctx.fill();
        ctx.fillStyle = C.bannerText;
        ctx.font = '600 12px ' + font;
        ctx.textAlign = 'center';
        const bAvailW = W - pad * 2 - 10;
        const bFull   = seBanners.map(e => e.icon + ' ' + e.name).join('  ');
        const bShort  = seBanners.map(e => e.icon + ' ' + e.short_name).join('  ');
        ctx.fillText(ctx.measureText(bFull).width <= bAvailW ? bFull : bShort, W / 2, y + 15);
    }
}

// ── Canvas helper: rounded rect ───────────────────────────────────────────────

// Split notes text into canvas-renderable lines, respecting \n and maxW.
// ctx must have the desired font set before calling.
function wrapNoteLines(ctx, text, maxW) {
    const lines = [];
    for (const paragraph of text.split('\n')) {
        if (paragraph === '') { lines.push(''); continue; }
        const words = paragraph.split(' ');
        let current = '';
        for (const word of words) {
            const test = current ? current + ' ' + word : word;
            if (ctx.measureText(test).width > maxW && current) {
                lines.push(current);
                current = word;
            } else {
                current = test;
            }
        }
        if (current) lines.push(current);
    }
    return lines;
}

// r can be a number (uniform) or [tl, tr, br, bl]
function roundRect(ctx, x, y, w, h, r) {
    let tl, tr, br, bl;
    if (Array.isArray(r)) {
        [tl, tr, br, bl] = r;
    } else {
        tl = tr = br = bl = r;
    }
    ctx.beginPath();
    ctx.moveTo(x + tl, y);
    ctx.lineTo(x + w - tr, y);
    ctx.quadraticCurveTo(x + w, y,     x + w,      y + tr);
    ctx.lineTo(x + w,      y + h - br);
    ctx.quadraticCurveTo(x + w, y + h, x + w - br, y + h);
    ctx.lineTo(x + bl,     y + h);
    ctx.quadraticCurveTo(x,     y + h, x,          y + h - bl);
    ctx.lineTo(x,          y + tr);
    ctx.quadraticCurveTo(x,     y,     x + tl,     y);
    ctx.closePath();
}

// ── Init ──────────────────────────────────────────────────────────────────────

async function init() {
    initTabs();

    // Parallel fetches
    const [settingsRes, slotTimesRes, stormConfigRes, typesRes, serverEventsRes] = await Promise.all([
        fetch('/api/settings').catch(() => null),
        fetch('/api/storm/slot-times').catch(() => null),
        fetch('/api/storm/config').catch(() => null),
        fetch('/api/schedule/event-types').catch(() => null),
        fetch('/api/schedule/server-events').catch(() => null),
    ]);

    try { settings = settingsRes && settingsRes.ok ? await settingsRes.json() : {}; } catch { settings = {}; }
    try { stormSlotTimes = slotTimesRes && slotTimesRes.ok ? await slotTimesRes.json() : []; } catch { stormSlotTimes = []; }
    try { stormTFConfig  = stormConfigRes && stormConfigRes.ok ? await stormConfigRes.json() : {}; } catch { stormTFConfig = {}; }
    try { eventTypes     = typesRes && typesRes.ok ? await typesRes.json() : []; } catch { eventTypes = []; }
    try { serverEvents   = serverEventsRes && serverEventsRes.ok ? await serverEventsRes.json() : []; } catch { serverEvents = []; }

    renderEventTypes();
    renderServerEvents();
    updateSeasonSubtitle();

    if (CAN_MANAGE) {
        populateSettingsForm();
    }

    currentWeekStart = currentGameMonday();
    mobileActiveDayIndex = 0;
    await loadWeek();

    bindEvents();
}

// ── Event binding ─────────────────────────────────────────────────────────────

function bindEvents() {
    // Week / day navigation — prev/next buttons behave as day-nav on mobile (≤600px)
    function isMobileView() { return window.matchMedia('(max-width: 600px)').matches; }

    document.getElementById('btn-prev-week').addEventListener('click', async () => {
        if (isMobileView()) {
            if (mobileActiveDayIndex > 0) {
                mobileActiveDayIndex--;
                syncMobileDay(weekDates(currentWeekStart));
            } else {
                currentWeekStart = addDays(currentWeekStart, -7);
                mobileActiveDayIndex = 6;
                await loadWeek();
            }
        } else {
            currentWeekStart = addDays(currentWeekStart, -7);
            mobileActiveDayIndex = 0;
            await loadWeek();
        }
    });
    document.getElementById('btn-next-week').addEventListener('click', async () => {
        if (isMobileView()) {
            if (mobileActiveDayIndex < 6) {
                mobileActiveDayIndex++;
                syncMobileDay(weekDates(currentWeekStart));
            } else {
                currentWeekStart = addDays(currentWeekStart, 7);
                mobileActiveDayIndex = 0;
                await loadWeek();
            }
        } else {
            currentWeekStart = addDays(currentWeekStart, 7);
            mobileActiveDayIndex = 0;
            await loadWeek();
        }
    });
    document.getElementById('btn-today').addEventListener('click', async () => {
        currentWeekStart = currentGameMonday();
        mobileActiveDayIndex = 0;
        await loadWeek();
    });

    // Output buttons
    document.getElementById('btn-text-output').addEventListener('click', () => {
        const sec = document.getElementById('text-output-section');
        const canvasSec = document.getElementById('canvas-section');
        canvasSec.classList.add('hidden');
        sec.classList.toggle('hidden');
        if (!sec.classList.contains('hidden')) {
            document.getElementById('text-output').value = buildTextOutput();
        }
    });

    document.getElementById('btn-copy-text').addEventListener('click', () => {
        const ta = document.getElementById('text-output');
        ta.select();
        document.execCommand('copy');
    });

    document.getElementById('btn-close-text').addEventListener('click', () => {
        document.getElementById('text-output-section').classList.add('hidden');
    });

    document.getElementById('btn-week-image').addEventListener('click', () => {
        const sec = document.getElementById('canvas-section');
        document.getElementById('canvas-section-title').textContent = '🖼️ Week Image';
        document.getElementById('text-output-section').classList.add('hidden');
        sec.classList.remove('hidden');
        drawWeekImage();
    });

    document.getElementById('btn-day-card').addEventListener('click', () => {
        const dateStr = document.getElementById('day-card-picker').value;
        const sec = document.getElementById('canvas-section');
        document.getElementById('canvas-section-title').textContent = '📅 Day Card – ' + formatDateShort(dateStr);
        document.getElementById('text-output-section').classList.add('hidden');
        sec.classList.remove('hidden');
        drawDayCard(dateStr);
    });

    document.getElementById('btn-close-canvas').addEventListener('click', () => {
        document.getElementById('canvas-section').classList.add('hidden');
    });

    document.getElementById('btn-download-png').addEventListener('click', () => {
        const canvas = document.getElementById('schedule-canvas');
        const a = document.createElement('a');
        a.download = 'schedule.png';
        a.href = canvas.toDataURL('image/png');
        a.click();
    });

    // Event modal
    document.getElementById('event-form').addEventListener('submit', saveEvent);
    document.getElementById('btn-close-event-modal').addEventListener('click', () => {
        document.getElementById('event-modal').style.display = '';
    });
    document.getElementById('event-type-select').addEventListener('change', updateEventModalForType);

    // Event type modal
    if (CAN_MANAGE) {
        document.getElementById('btn-add-event-type')?.addEventListener('click', openAddEventTypeModal);
    }
    document.getElementById('event-type-form').addEventListener('submit', saveEventType);
    document.getElementById('btn-close-event-type-modal').addEventListener('click', () => {
        document.getElementById('event-type-modal').style.display = '';
    });

    // Server event modal
    if (CAN_MANAGE) {
        document.getElementById('btn-add-server-event')?.addEventListener('click', openAddServerEventModal);
    }
    document.getElementById('server-event-form').addEventListener('submit', saveServerEvent);
    document.getElementById('se-repeat').addEventListener('change', updateServerEventRepeatUI);
    document.getElementById('btn-close-server-event-modal').addEventListener('click', () => {
        document.getElementById('server-event-modal').style.display = '';
    });

    // Settings tab (manage only)
    if (CAN_MANAGE) {
        document.getElementById('btn-save-settings')?.addEventListener('click', saveSettings);
        document.getElementById('btn-generate')?.addEventListener('click', generateEvents);

        document.querySelectorAll('input[name="zs-mode"]').forEach(radio => {
            radio.addEventListener('change', () => updateZSModeUI(radio.value));
        });
    }

    // Flatpickr: time picker for event time field
    const timeFp = flatpickr('#event-time-input', {
        enableTime: true,
        noCalendar: true,
        dateFormat: 'H:i',
        time_24hr: true,
        minuteIncrement: 30,
        allowInput: true,
    });

    // Wire All Day toggle to also disable/enable flatpickr
    document.getElementById('event-allday-input').addEventListener('change', e => {
        const timeInput = document.getElementById('event-time-input');
        timeInput.disabled = e.target.checked;
        if (e.target.checked) { timeFp.clear(); }
    });

    // Flatpickr: time pickers for settings default times + ZS anchor time
    ['#set-mg-time', '#set-zs-time', '#gen-zs-anchor-time'].forEach(sel => {
        flatpickr(sel, {
            enableTime: true,
            noCalendar: true,
            dateFormat: 'H:i',
            time_24hr: true,
            minuteIncrement: 30,
            allowInput: true,
        });
    });

    // Flatpickr: season start date — Mondays only
    flatpickr('#set-season-start', {
        dateFormat: 'Y-m-d',
        disable: [d => d.getDay() !== 1],  // 1 = Monday
        allowInput: true,
    });

    // Close modals on backdrop click
    ['event-modal', 'event-type-modal', 'server-event-modal'].forEach(id => {
        const modal = document.getElementById(id);
        if (modal) {
            modal.addEventListener('click', e => {
                if (e.target === modal) modal.style.display = '';
            });
        }
    });
}

// ── Boot ──────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', init);
