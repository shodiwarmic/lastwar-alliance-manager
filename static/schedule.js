// schedule.js — Dynamic Schedule Builder

let schedules = [];
let currentSchedule = null; // { id, name, duration_days, is_active, schedule_data: [] }
const canManage = window.CAN_MANAGE === true;

// ── Helpers ──────────────────────────────────────────────────────────────────

function csrfToken() {
    const el = document.querySelector('input[name="gorilla.csrf.Token"]');
    return el ? el.value : '';
}

function timeToMinutes(timeStr) {
    const [h, m] = timeStr.split(':').map(Number);
    return h * 60 + m;
}

function dayTimeToAbsoluteMinutes(dayIndex, timeStr) {
    return dayIndex * 24 * 60 + timeToMinutes(timeStr);
}

// ── Validation Engine ─────────────────────────────────────────────────────────

function validateSchedule(days) {
    const warnings = [];

    // Collect all ZS events with their absolute time (minutes from day 0 start)
    const zsEvents = [];
    days.forEach((day, di) => {
        (day.events || []).filter(e => e.type === 'zs').forEach(e => {
            zsEvents.push({ day: di + 1, absMinutes: dayTimeToAbsoluteMinutes(di, e.start_time) });
        });
    });

    // ZS cooldown: 71.5 hours = 4290 minutes
    for (let i = 0; i < zsEvents.length - 1; i++) {
        const gap = zsEvents[i + 1].absMinutes - zsEvents[i].absMinutes;
        if (gap < 4290) {
            const gapH = (gap / 60).toFixed(1);
            warnings.push(`ZS on Day ${zsEvents[i].day} and Day ${zsEvents[i + 1].day} are only ${gapH}h apart (minimum 71.5h)`);
        }
    }

    // MG cutoff: must start before 22:00
    days.forEach((day, di) => {
        (day.events || []).filter(e => e.type === 'mg').forEach(e => {
            if (timeToMinutes(e.start_time) >= 22 * 60) {
                warnings.push(`MG on Day ${di + 1} starts at ${e.start_time} — must be before 22:00`);
            }
        });
    });

    return warnings;
}

function renderValidation(days) {
    const banner = document.getElementById('validation-banner');
    const list = document.getElementById('validation-list');
    const warnings = validateSchedule(days);
    list.innerHTML = '';
    if (warnings.length === 0) {
        banner.classList.add('hidden');
    } else {
        warnings.forEach(w => {
            const li = document.createElement('li');
            li.textContent = '⚠️ ' + w;
            list.appendChild(li);
        });
        banner.classList.remove('hidden');
    }
}

// ── Vibe Generator ────────────────────────────────────────────────────────────

function generateVibeText(schedule) {
    const lines = [`📅 **${schedule.name}** (${schedule.duration_days}-Day Cycle)\n`];
    (schedule.schedule_data || []).forEach(day => {
        if (!day.events || day.events.length === 0) return;
        lines.push(`**Day ${day.day_number}**`);
        day.events.forEach(e => {
            const icon = e.type === 'zs' ? '🧟' : '🛡️';
            const label = e.vibe ? ` — ${e.vibe}` : '';
            const lvl = e.level_override ? ` (Lv.${e.level_override})` : '';
            lines.push(`  ${icon} ${e.type.toUpperCase()} @ ${e.start_time}${lvl}${label}`);
        });
    });
    return lines.join('\n');
}

// ── Day Grid Renderer ─────────────────────────────────────────────────────────

function buildEmptyDays(count) {
    return Array.from({ length: count }, (_, i) => ({ day_number: i + 1, events: [] }));
}

function renderDayGrid(schedule) {
    const grid = document.getElementById('day-grid');
    grid.innerHTML = '';
    const days = schedule.schedule_data || [];

    days.forEach((day, di) => {
        const section = document.createElement('div');
        section.className = 'form-section';
        section.style.marginBottom = '12px';

        const header = document.createElement('div');
        header.style.cssText = 'display:flex; align-items:center; gap:12px; margin-bottom:8px;';

        const title = document.createElement('strong');
        title.textContent = `Day ${day.day_number}`;
        header.appendChild(title);

        if (canManage) {
            const addBtn = document.createElement('button');
            addBtn.className = 'btn btn-secondary';
            addBtn.style.padding = '4px 10px';
            addBtn.textContent = '+ Add Event';
            addBtn.onclick = () => openEventModal(di, null);
            header.appendChild(addBtn);
        }

        section.appendChild(header);

        const chips = document.createElement('div');
        chips.style.cssText = 'display:flex; flex-wrap:wrap; gap:8px;';

        if (!day.events || day.events.length === 0) {
            const empty = document.createElement('span');
            empty.style.cssText = 'color: var(--text-secondary); font-size: 0.9em;';
            empty.textContent = 'No events scheduled';
            chips.appendChild(empty);
        } else {
            day.events.forEach((evt, ei) => {
                const chip = document.createElement('div');
                chip.className = 'event-chip';
                chip.style.cssText = `
                    display:inline-flex; align-items:center; gap:6px;
                    padding: 5px 10px; border-radius: 20px; font-size: 0.875em;
                    background: ${evt.type === 'zs' ? 'var(--accent-danger, #c0392b)' : 'var(--accent-primary, #2980b9)'};
                    color: #fff; cursor: ${canManage ? 'pointer' : 'default'};
                `;
                const icon = evt.type === 'zs' ? '🧟' : '🛡️';
                const lvl = evt.level_override ? ` Lv.${evt.level_override}` : '';
                const vibe = evt.vibe ? ` · ${evt.vibe}` : '';
                chip.textContent = `${icon} ${evt.type.toUpperCase()} @ ${evt.start_time}${lvl}${vibe}`;
                if (canManage) chip.onclick = () => openEventModal(di, ei);
                chips.appendChild(chip);
            });
        }

        section.appendChild(chips);
        grid.appendChild(section);
    });

    renderValidation(days);
}

// ── Schedule Selector ─────────────────────────────────────────────────────────

function populateSelector(selectedId) {
    const sel = document.getElementById('schedule-select');
    sel.innerHTML = '<option value="">-- Select a schedule --</option>';
    schedules.forEach(s => {
        const opt = document.createElement('option');
        opt.value = s.id;
        opt.textContent = (s.is_active ? '★ ' : '') + s.name + ` (${s.duration_days}-day)`;
        sel.appendChild(opt);
    });
    if (selectedId) sel.value = selectedId;
}

function loadSchedule(id) {
    const s = schedules.find(x => x.id === Number(id));
    if (!s) {
        currentSchedule = null;
        document.getElementById('day-grid-section').style.display = 'none';
        document.getElementById('vibe-section').classList.add('hidden');
        if (canManage) {
            document.getElementById('btn-save').disabled = true;
            document.getElementById('btn-delete').disabled = true;
            document.getElementById('btn-activate').disabled = true;
        }
        document.getElementById('btn-vibe').disabled = true;
        return;
    }

    currentSchedule = JSON.parse(JSON.stringify(s)); // deep clone
    // Ensure schedule_data is an array
    if (!Array.isArray(currentSchedule.schedule_data)) {
        try { currentSchedule.schedule_data = JSON.parse(currentSchedule.schedule_data || '[]'); } catch { currentSchedule.schedule_data = []; }
    }
    // Backfill missing days if duration changed
    while (currentSchedule.schedule_data.length < currentSchedule.duration_days) {
        const n = currentSchedule.schedule_data.length + 1;
        currentSchedule.schedule_data.push({ day_number: n, events: [] });
    }
    currentSchedule.schedule_data = currentSchedule.schedule_data.slice(0, currentSchedule.duration_days);

    document.getElementById('schedule-title-display').textContent = currentSchedule.name;
    document.getElementById('day-grid-section').style.display = '';

    const durationRow = document.getElementById('duration-row');
    if (durationRow) {
        durationRow.style.display = '';
        document.querySelectorAll('input[name="duration"]').forEach(r => {
            r.checked = Number(r.value) === currentSchedule.duration_days;
        });
    }

    if (canManage) {
        document.getElementById('btn-save').disabled = false;
        document.getElementById('btn-delete').disabled = !!currentSchedule.is_active;
        document.getElementById('btn-activate').disabled = !!currentSchedule.is_active;
    }
    document.getElementById('btn-vibe').disabled = false;

    renderDayGrid(currentSchedule);
}

// ── API Calls ─────────────────────────────────────────────────────────────────

async function fetchSchedules() {
    const res = await fetch('/api/schedules');
    if (!res.ok) return;
    schedules = await res.json();
    if (!Array.isArray(schedules)) schedules = [];

    const activeId = (schedules.find(s => s.is_active) || {}).id;
    populateSelector(activeId || (schedules[0] || {}).id);

    const sel = document.getElementById('schedule-select');
    if (sel.value) loadSchedule(sel.value);
}

async function saveCurrentSchedule() {
    if (!currentSchedule) return;

    const name = currentSchedule.name;
    const duration_days = currentSchedule.duration_days;
    const schedule_data = currentSchedule.schedule_data;

    const body = { name, duration_days, schedule_data };
    const url = `/api/schedules/${currentSchedule.id}`;
    const res = await fetch(url, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
        body: JSON.stringify(body),
    });
    if (!res.ok) { alert('Save failed: ' + await res.text()); return; }
    const updated = await res.json();
    const idx = schedules.findIndex(s => s.id === updated.id);
    if (idx !== -1) schedules[idx] = updated;
    loadSchedule(updated.id);
    populateSelector(updated.id);
}

async function createNewSchedule(name, duration_days) {
    const schedule_data = buildEmptyDays(duration_days);
    const res = await fetch('/api/schedules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
        body: JSON.stringify({ name, duration_days, schedule_data }),
    });
    if (!res.ok) { alert('Create failed: ' + await res.text()); return; }
    const created = await res.json();
    schedules.unshift(created);
    populateSelector(created.id);
    loadSchedule(created.id);
}

async function deleteCurrentSchedule() {
    if (!currentSchedule) return;
    if (!confirm(`Delete schedule "${currentSchedule.name}"? This cannot be undone.`)) return;
    const res = await fetch(`/api/schedules/${currentSchedule.id}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': csrfToken() },
    });
    if (!res.ok) { alert('Delete failed: ' + await res.text()); return; }
    schedules = schedules.filter(s => s.id !== currentSchedule.id);
    currentSchedule = null;
    populateSelector(null);
    document.getElementById('day-grid-section').style.display = 'none';
    document.getElementById('vibe-section').classList.add('hidden');
    if (canManage) {
        document.getElementById('btn-save').disabled = true;
        document.getElementById('btn-delete').disabled = true;
        document.getElementById('btn-activate').disabled = true;
    }
    document.getElementById('btn-vibe').disabled = true;
}

async function activateCurrentSchedule() {
    if (!currentSchedule) return;
    const res = await fetch(`/api/schedules/${currentSchedule.id}/activate`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken() },
    });
    if (!res.ok) { alert('Activate failed: ' + await res.text()); return; }
    schedules.forEach(s => s.is_active = s.id === currentSchedule.id);
    populateSelector(currentSchedule.id);
    loadSchedule(currentSchedule.id);
}

// ── Event Modal ───────────────────────────────────────────────────────────────

let _editDayIndex = null;
let _editEventIndex = null;

function openEventModal(dayIndex, eventIndex) {
    _editDayIndex = dayIndex;
    _editEventIndex = eventIndex;

    const modal = document.getElementById('event-modal');
    const deleteBtn = document.getElementById('btn-delete-event');
    document.getElementById('event-modal-title').textContent = eventIndex !== null ? 'Edit Event' : 'Add Event';

    if (eventIndex !== null) {
        const evt = currentSchedule.schedule_data[dayIndex].events[eventIndex];
        document.querySelector(`input[name="event-type"][value="${evt.type}"]`).checked = true;
        document.getElementById('event-time').value = evt.start_time;
        document.getElementById('event-level').value = evt.level_override || '';
        document.getElementById('event-vibe').value = evt.vibe || '';
        deleteBtn.style.display = '';
    } else {
        document.querySelector('input[name="event-type"][value="mg"]').checked = true;
        document.getElementById('event-time').value = '';
        document.getElementById('event-level').value = '';
        document.getElementById('event-vibe').value = '';
        deleteBtn.style.display = 'none';
    }

    modal.style.display = 'flex';
}

function closeEventModal() {
    document.getElementById('event-modal').style.display = 'none';
    _editDayIndex = null;
    _editEventIndex = null;
}

function deleteCurrentEvent() {
    if (_editDayIndex === null || _editEventIndex === null) return;
    currentSchedule.schedule_data[_editDayIndex].events.splice(_editEventIndex, 1);
    renderDayGrid(currentSchedule);
    closeEventModal();
}

document.getElementById('event-form').addEventListener('submit', e => {
    e.preventDefault();
    if (_editDayIndex === null) return;

    const type = document.querySelector('input[name="event-type"]:checked').value;
    const start_time = document.getElementById('event-time').value;
    const level_override = document.getElementById('event-level').value ? Number(document.getElementById('event-level').value) : null;
    const vibe = document.getElementById('event-vibe').value.trim() || null;

    const evt = { type, start_time, level_override, vibe };

    if (_editEventIndex !== null) {
        currentSchedule.schedule_data[_editDayIndex].events[_editEventIndex] = evt;
    } else {
        currentSchedule.schedule_data[_editDayIndex].events.push(evt);
    }

    renderDayGrid(currentSchedule);
    closeEventModal();
});

// Close modals on overlay click
document.getElementById('event-modal').addEventListener('click', e => {
    if (e.target === e.currentTarget) closeEventModal();
});
document.getElementById('new-schedule-modal').addEventListener('click', e => {
    if (e.target === e.currentTarget) closeNewScheduleModal();
});

// ── New Schedule Modal ────────────────────────────────────────────────────────

function closeNewScheduleModal() {
    document.getElementById('new-schedule-modal').style.display = 'none';
}

document.getElementById('new-schedule-form').addEventListener('submit', async e => {
    e.preventDefault();
    const name = document.getElementById('new-schedule-name').value.trim();
    const duration_days = Number(document.querySelector('input[name="new-duration"]:checked').value);
    closeNewScheduleModal();
    await createNewSchedule(name, duration_days);
});

// ── Duration Change ───────────────────────────────────────────────────────────

document.querySelectorAll('input[name="duration"]').forEach(radio => {
    radio.addEventListener('change', () => {
        if (!currentSchedule) return;
        const newDuration = Number(radio.value);
        currentSchedule.duration_days = newDuration;
        while (currentSchedule.schedule_data.length < newDuration) {
            const n = currentSchedule.schedule_data.length + 1;
            currentSchedule.schedule_data.push({ day_number: n, events: [] });
        }
        currentSchedule.schedule_data = currentSchedule.schedule_data.slice(0, newDuration);
        renderDayGrid(currentSchedule);
    });
});

// ── Toolbar Buttons ───────────────────────────────────────────────────────────

document.getElementById('schedule-select').addEventListener('change', e => {
    loadSchedule(e.target.value);
});

if (canManage) {
    document.getElementById('btn-new').addEventListener('click', () => {
        document.getElementById('new-schedule-name').value = '';
        document.querySelector('input[name="new-duration"][value="7"]').checked = true;
        document.getElementById('new-schedule-modal').style.display = 'flex';
    });
    document.getElementById('btn-save').addEventListener('click', saveCurrentSchedule);
    document.getElementById('btn-delete').addEventListener('click', deleteCurrentSchedule);
    document.getElementById('btn-activate').addEventListener('click', activateCurrentSchedule);
}

document.getElementById('btn-vibe').addEventListener('click', () => {
    if (!currentSchedule) return;
    const text = generateVibeText(currentSchedule);
    document.getElementById('vibe-output').value = text;
    document.getElementById('vibe-section').classList.remove('hidden');
    document.getElementById('vibe-section').scrollIntoView({ behavior: 'smooth' });
});

document.getElementById('btn-copy-vibe').addEventListener('click', () => {
    const ta = document.getElementById('vibe-output');
    navigator.clipboard.writeText(ta.value).then(() => {
        const btn = document.getElementById('btn-copy-vibe');
        const orig = btn.textContent;
        btn.textContent = 'Copied!';
        setTimeout(() => { btn.textContent = orig; }, 2000);
    });
});

// ── Init ──────────────────────────────────────────────────────────────────────

fetchSchedules();
