// schedule.js — Schedule Policy Builder

// ── Constants ─────────────────────────────────────────────────────────────────

const VS_THEMES = [
    { label: 'Radar Training',     icon: '📡' },
    { label: 'Base Expansion',     icon: '🏗️' },
    { label: 'Age of Science',     icon: '🔬' },
    { label: 'Train Heroes',       icon: '🦸' },
    { label: 'Total Mobilization', icon: '📦' },
    { label: 'Enemy Buster',       icon: '💥' },
    { label: 'Alliance Star',      icon: '⭐' },
];

// Fixed server events by day_number — never stored, always derived
const FIXED_SERVER_EVENTS = {
    1:  [{ key: 'ironclad',        label: 'Ironclad Vehicle', icon: '🚙' }],
    2:  [{ key: 'ironclad',        label: 'Ironclad Vehicle', icon: '🚙' }],
    3:  [{ key: 'zombie_invasion', label: 'Zombie Invasion',  icon: '☣️' }],
    4:  [{ key: 'zombie_invasion', label: 'Zombie Invasion',  icon: '☣️' }],
    5:  [{ key: 'zombie_invasion', label: 'Zombie Invasion',  icon: '☣️' }],
    7:  [{ key: 'rampage_bosses',  label: 'Rampage Bosses',   icon: '👹' }],
    11: [{ key: 'generals_trial',  label: "General's Trial",  icon: '⚔️' }],
    12: [{ key: 'generals_trial',  label: "General's Trial",  icon: '⚔️' }],
    13: [{ key: 'generals_trial',  label: "General's Trial",  icon: '⚔️' }],
    14: [{ key: 'doomsday',        label: 'Doomsday',         icon: '🌋' }],
};

function getServerEvents(dayNumber) {
    return FIXED_SERVER_EVENTS[dayNumber] || [];
}

const CUSTOM_EVENT_ICONS = [
    '☄️', '🌪️', '🏔️', '⚡', '🌊', '🔥', '💀', '🎯',
    '🛸', '🌙', '🌍', '🏳️', '⚔️', '🛡️', '🎪', '📦',
];

const VIBES = {
    base:        { label: 'Base',        delta: 0  },
    push:        { label: 'Push',        delta: 1  },
    chill:       { label: 'Chill',       delta: -1 },
    extra_chill: { label: 'Extra Chill', delta: -2 },
};

const DEFAULT_MG_TIME = '00:30';
const DEFAULT_ZS_TIME = '23:00';

// ── State ─────────────────────────────────────────────────────────────────────

let schedules = [];
let currentSchedule = null;
let currentPolicy = null;
const canManage = window.CAN_MANAGE === true;

// ── Helpers ───────────────────────────────────────────────────────────────────

function csrfToken() {
    const el = document.querySelector('input[name="gorilla.csrf.Token"]');
    return el ? el.value : '';
}

function timeToMinutes(timeStr) {
    if (!timeStr) return 0;
    const [h, m] = timeStr.split(':').map(Number);
    return h * 60 + (m || 0);
}

function getVSTheme(dayNumber) {
    return VS_THEMES[(dayNumber - 1) % 7];
}

function getLevel(baseline, vibe) {
    if (!vibe || !VIBES[vibe]) return baseline;
    return baseline + VIBES[vibe].delta;
}

function buildEmptyPolicy(durationDays = 14) {
    return {
        mg_baseline: 11,
        zs_baseline: 7,
        mg_time: DEFAULT_MG_TIME,
        zs_time: DEFAULT_ZS_TIME,
        week_offset: 0,
        days: Array.from({ length: durationDays }, (_, i) => ({
            day_number: i + 1,
            mg: { active: false, vibe: 'base', time_override: null, conditional: null },
            zs: { active: false, vibe: 'base', conditional: null },
            custom_events: [],
            notes: null,
        })),
    };
}

function getPolicyData(schedule) {
    let raw = schedule.schedule_data;
    if (typeof raw === 'string') {
        try { raw = JSON.parse(raw); } catch { raw = null; }
    }
    // Detect old/missing shapes → return empty policy
    if (!raw || Array.isArray(raw) || !raw.days) {
        return buildEmptyPolicy(schedule.duration_days);
    }
    // Backfill fields added in later revisions
    if (raw.week_offset === undefined) raw.week_offset = 0;
    raw.days.forEach(d => {
        if (!d.custom_events) d.custom_events = [];
        if (d.mg && d.mg.time_override === undefined) d.mg.time_override = null;
        // Drop stored server_events if present (now derived from constants)
        delete d.server_events;
    });
    return raw;
}

// ── Validation ────────────────────────────────────────────────────────────────

function validatePolicy(policy) {
    const warnings = [];
    const days = policy.days || [];

    const zsEvents = days
        .filter(d => d.zs && d.zs.active)
        .map(d => ({
            day: d.day_number,
            absMinutes: (d.day_number - 1) * 24 * 60 + timeToMinutes(policy.zs_time || DEFAULT_ZS_TIME),
        }));

    for (let i = 0; i < zsEvents.length - 1; i++) {
        const gap = zsEvents[i + 1].absMinutes - zsEvents[i].absMinutes;
        if (gap < 4290) {
            warnings.push(`ZS on Day ${zsEvents[i].day} → Day ${zsEvents[i + 1].day}: only ${(gap / 60).toFixed(1)}h apart (min 71.5h)`);
        }
    }

    if (timeToMinutes(policy.mg_time || DEFAULT_MG_TIME) >= 22 * 60) {
        if (days.some(d => d.mg && d.mg.active)) {
            warnings.push(`MG time ${policy.mg_time} is at or after 22:00 Server Time`);
        }
    }

    return warnings;
}

function renderValidation(policy) {
    const banner = document.getElementById('validation-banner');
    const list = document.getElementById('validation-list');
    const warnings = validatePolicy(policy);
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

// ── Day Grid ──────────────────────────────────────────────────────────────────

function makeChip(text, bg) {
    const chip = document.createElement('div');
    chip.style.cssText = `
        display:inline-flex; align-items:center; padding:4px 10px;
        border-radius:20px; font-size:0.875em; background:${bg}; color:#fff;`;
    chip.textContent = text;
    return chip;
}

function renderDayGrid(policy) {
    const grid = document.getElementById('day-grid');
    grid.innerHTML = '';
    const days = policy.days || [];
    let weekLabel = '';

    days.forEach((day, di) => {
        const week = Math.ceil(day.day_number / 7);
        const displayWeek = week + (policy.week_offset || 0);
        const newWeekLabel = `Week ${displayWeek}`;
        if (newWeekLabel !== weekLabel) {
            weekLabel = newWeekLabel;
            const sep = document.createElement('h4');
            sep.textContent = `📅 ${weekLabel}`;
            sep.style.cssText = 'margin:16px 0 8px; color:var(--accent-primary);';
            grid.appendChild(sep);
        }

        const theme = getVSTheme(day.day_number);
        const row = document.createElement('div');
        row.className = 'form-section';
        row.style.cssText = 'display:flex; align-items:center; gap:12px; flex-wrap:wrap; padding:10px 14px; margin-bottom:6px;';

        const dayLabel = document.createElement('div');
        dayLabel.style.cssText = 'min-width:200px; font-weight:600;';
        dayLabel.innerHTML = `D${day.day_number} &nbsp;<span style="font-weight:normal; color:var(--text-secondary);">${theme.icon} ${theme.label}</span>`;
        row.appendChild(dayLabel);

        const chips = document.createElement('div');
        chips.style.cssText = 'display:flex; flex-wrap:wrap; gap:6px; flex:1;';

        // Fixed server events (derived, not stored)
        getServerEvents(day.day_number).forEach(ev => {
            chips.appendChild(makeChip(`${ev.icon} ${ev.label}`, 'var(--accent-secondary, #555)'));
        });

        if (day.mg && day.mg.active) {
            const level = getLevel(policy.mg_baseline, day.mg.vibe);
            const vibeLabel = day.mg.vibe ? ` · ${VIBES[day.mg.vibe].label}` : '';
            const mgTime = day.mg.time_override || policy.mg_time;
            chips.appendChild(makeChip(`🛡️ MG @ ${mgTime} (Lv.${level}${vibeLabel})`, 'var(--accent-primary)'));
        }

        if (day.zs && day.zs.active) {
            const level = getLevel(policy.zs_baseline, day.zs.vibe);
            const vibeLabel = day.zs.vibe ? ` · ${VIBES[day.zs.vibe].label}` : '';
            chips.appendChild(makeChip(`🧟 ZS @ ${policy.zs_time} (Lv.${level}${vibeLabel})`, 'var(--accent-danger, #c0392b)'));
        }

        // Custom events
        (day.custom_events || []).forEach(ev => {
            chips.appendChild(makeChip(`${ev.icon} ${ev.label}`, 'var(--text-secondary, #555)'));
        });

        if (chips.children.length === 0) {
            const empty = document.createElement('span');
            empty.style.cssText = 'color:var(--text-secondary); font-size:0.9em;';
            empty.textContent = 'No events';
            chips.appendChild(empty);
        }
        row.appendChild(chips);

        if (canManage) {
            const editBtn = document.createElement('button');
            editBtn.className = 'btn btn-secondary';
            editBtn.style.padding = '4px 10px';
            editBtn.textContent = 'Edit';
            editBtn.onclick = () => openDayModal(di);
            row.appendChild(editBtn);
        }

        grid.appendChild(row);
    });

    renderValidation(policy);
}

// ── Text Generator ────────────────────────────────────────────────────────────

function generateText(schedule) {
    const policy = getPolicyData(schedule);
    const lines = [];

    lines.push(`📅 LAST WAR: ${schedule.duration_days}-DAY MASTER SCHEDULE`);
    lines.push(`📊 Weekly VS Theme: ${VS_THEMES.map(t => t.label).join(' > ')}`);
    lines.push(`🎯 Current Baselines: MG ${policy.mg_baseline} | ZS ${policy.zs_baseline}`);
    lines.push(`   (Note: Baselines are reduced during gameplay seasons)`);
    lines.push('');

    const weeks = Math.ceil(policy.days.length / 7);
    for (let w = 1; w <= weeks; w++) {
        const displayWeek = w + (policy.week_offset || 0);
        lines.push(`📅 WEEK ${displayWeek}`);
        policy.days.filter(d => Math.ceil(d.day_number / 7) === w).forEach(day => {
            const parts = [];
            getServerEvents(day.day_number).forEach(ev => {
                parts.push(`${ev.label} ${ev.icon}`);
            });
            if (day.mg && day.mg.active) {
                const vibe = day.mg.vibe ? VIBES[day.mg.vibe].label : 'Base';
                const mgTime = day.mg.time_override || policy.mg_time;
                let s = `MG @ ${mgTime} (${vibe})`;
                if (day.mg.conditional) s += ` [${day.mg.conditional}]`;
                parts.push(s);
            }
            if (day.zs && day.zs.active) {
                const vibe = day.zs.vibe ? VIBES[day.zs.vibe].label : 'Base';
                let s = `ZS @ ${policy.zs_time} (${vibe})`;
                if (day.zs.conditional) s += ` [${day.zs.conditional}]`;
                parts.push(s);
            }
            (day.custom_events || []).forEach(ev => parts.push(`${ev.icon} ${ev.label}`));
            lines.push(`• D${day.day_number}: ${parts.length ? parts.join(' | ') : 'No Events'}`);
        });
        lines.push('');
    }

    lines.push(`📋 LEVEL GUIDE & TIMINGS`);
    lines.push(`• Base (Standard): Default difficulty — Lv.${policy.mg_baseline} MG / Lv.${policy.zs_baseline} ZS`);
    lines.push(`• Push (Challenge): +1 Level. Leadership announces on event days.`);
    lines.push(`• Chill (Easy): -1 Level for a weekend breather.`);
    lines.push(`• Extra Chill (Post-Event): -2 Levels. Run after major server events.`);
    lines.push(`• ZS Timing: ${policy.zs_time} Server Time`);
    lines.push(`• MG Timing: ${policy.mg_time} Server Time`);

    return lines.join('\n');
}

// ── Canvas Infographic ────────────────────────────────────────────────────────

const CANVAS_WIDTH = 1200;

const COLORS = {
    bg:           '#0d1b2a',
    bgCard:       '#1a2a3a',
    bgWeekHeader: '#1e3a5f',
    bgRow:        '#162030',
    bgRowAlt:     '#1a2535',
    border:       '#2a4a6a',
    textPrimary:  '#e8f4f8',
    textSecondary:'#8ab4c8',
    textTheme:    '#a8d8ea',
    accentMG:     '#2980b9',
    accentZS:     '#c0392b',
    accentServer: '#2ecc71',
    accentTitle:  '#f39c12',
    gold:         '#f1c40f',
};

function renderCanvas(schedule) {
    const policy = getPolicyData(schedule);
    const canvas = document.getElementById('schedule-canvas');
    const weeks = Math.ceil(policy.days.length / 7);

    const ROW_H      = 52;
    const HEADER_H   = 140;
    const WEEK_HDR_H = 40;
    const COL_HDR_H  = 36;
    const LEGEND_H   = 80;

    canvas.width  = CANVAS_WIDTH;
    canvas.height = HEADER_H + WEEK_HDR_H + COL_HDR_H + 7 * ROW_H + LEGEND_H + 20;

    const ctx = canvas.getContext('2d');
    ctx.fillStyle = COLORS.bg;
    ctx.fillRect(0, 0, canvas.width, canvas.height);

    // Title
    ctx.fillStyle = COLORS.accentTitle;
    ctx.font = 'bold 28px Arial';
    ctx.textAlign = 'center';
    ctx.fillText(`LAST WAR: SURVIVAL MASTER SCHEDULE (${schedule.duration_days}-DAY CYCLE)`, canvas.width / 2, 40);

    // VS Theme row
    ctx.fillStyle = COLORS.bgWeekHeader;
    ctx.fillRect(10, 55, canvas.width - 20, 75);
    ctx.strokeStyle = COLORS.border;
    ctx.strokeRect(10, 55, canvas.width - 20, 75);
    ctx.fillStyle = COLORS.textSecondary;
    ctx.font = 'bold 13px Arial';
    ctx.textAlign = 'center';
    ctx.fillText('VS DUEL THEME CYCLE (REPEATS WEEKLY)', canvas.width / 2, 73);

    const themeColW = (canvas.width - 20) / VS_THEMES.length;
    VS_THEMES.forEach((theme, i) => {
        const tx = 10 + i * themeColW + themeColW / 2;
        ctx.fillStyle = COLORS.textPrimary;
        ctx.font = '20px Arial';
        ctx.fillText(theme.icon, tx, 98);
        ctx.font = '11px Arial';
        ctx.fillStyle = COLORS.textTheme;
        ctx.fillText(`${i + 1}. ${theme.label}`, tx, 116);
    });

    const y = HEADER_H;
    const colW = (canvas.width - 20) / weeks;

    for (let w = 1; w <= weeks; w++) {
        const cx = 10 + (w - 1) * colW;
        const weekDays = policy.days.filter(d => Math.ceil(d.day_number / 7) === w);

        // Week header
        ctx.fillStyle = COLORS.bgWeekHeader;
        ctx.fillRect(cx, y, colW, WEEK_HDR_H);
        ctx.strokeStyle = COLORS.border;
        ctx.strokeRect(cx, y, colW, WEEK_HDR_H);
        const displayWeek = w + (policy.week_offset || 0);
        ctx.fillStyle = COLORS.textPrimary;
        ctx.font = 'bold 16px Arial';
        ctx.textAlign = 'center';
        ctx.fillText(`WEEK ${displayWeek}`, cx + colW / 2, y + 26);

        // Column headers
        const subY = y + WEEK_HDR_H;
        const subCols = ['Day', 'VS Theme', 'MG / ZS', 'Server'];
        const subColW = colW / subCols.length;
        ctx.fillStyle = COLORS.bgCard;
        ctx.fillRect(cx, subY, colW, COL_HDR_H);
        subCols.forEach((hdr, si) => {
            ctx.fillStyle = COLORS.textSecondary;
            ctx.font = 'bold 11px Arial';
            ctx.textAlign = 'center';
            ctx.fillText(hdr, cx + si * subColW + subColW / 2, subY + 23);
        });

        // Data rows
        weekDays.forEach((day, ri) => {
            const ry = subY + COL_HDR_H + ri * ROW_H;
            const theme = getVSTheme(day.day_number);

            ctx.fillStyle = ri % 2 === 0 ? COLORS.bgRow : COLORS.bgRowAlt;
            ctx.fillRect(cx, ry, colW, ROW_H);
            ctx.strokeStyle = COLORS.border;
            ctx.strokeRect(cx, ry, colW, ROW_H);

            // Day number
            ctx.fillStyle = COLORS.textPrimary;
            ctx.font = 'bold 13px Arial';
            ctx.textAlign = 'center';
            ctx.fillText(`D${day.day_number}`, cx + subColW * 0.5, ry + ROW_H / 2 + 5);

            // VS theme
            ctx.font = '13px Arial';
            ctx.fillStyle = COLORS.textTheme;
            ctx.fillText(theme.icon, cx + subColW * 1.5 - 12, ry + ROW_H / 2 + 5);
            ctx.font = '10px Arial';
            ctx.fillText(theme.label, cx + subColW * 1.5 + 6, ry + ROW_H / 2 + 5);

            // MG/ZS events
            const eventLines = [];
            if (day.mg && day.mg.active) {
                const vibe = day.mg.vibe ? VIBES[day.mg.vibe].label : 'Base';
                const mgTime = day.mg.time_override || policy.mg_time;
                eventLines.push({ text: `🛡️ MG @${mgTime} (${vibe})`, color: COLORS.accentMG });
            }
            if (day.zs && day.zs.active) {
                const vibe = day.zs.vibe ? VIBES[day.zs.vibe].label : 'Base';
                eventLines.push({ text: `🧟 ZS @${policy.zs_time} (${vibe})`, color: COLORS.accentZS });
            }
            if (eventLines.length === 0) {
                ctx.font = 'italic 11px Arial';
                ctx.fillStyle = COLORS.textSecondary;
                ctx.fillText('-', cx + subColW * 2.5, ry + ROW_H / 2 + 5);
            } else {
                eventLines.forEach((el, eli) => {
                    const lineY = eventLines.length === 1 ? ry + ROW_H / 2 + 5 : ry + 16 + eli * 18;
                    ctx.font = '10px Arial';
                    ctx.fillStyle = el.color;
                    ctx.fillText(el.text, cx + subColW * 2.5, lineY);
                });
            }

            // Server events (derived from constants, never stored)
            const serverEvs = getServerEvents(day.day_number);
            const serverIcons = serverEvs.map(ev => ev.icon).join(' ');
            ctx.font = serverIcons ? '16px Arial' : '11px Arial';
            ctx.fillStyle = serverIcons ? COLORS.accentServer : COLORS.textSecondary;
            ctx.textAlign = 'center';
            ctx.fillText(serverIcons || '-', cx + subColW * 3.5, ry + ROW_H / 2 + 5);
        });
    }

    // Legend
    const legendY = y + WEEK_HDR_H + COL_HDR_H + 7 * ROW_H + 10;
    ctx.fillStyle = COLORS.bgCard;
    ctx.fillRect(10, legendY, canvas.width - 20, LEGEND_H);
    ctx.strokeStyle = COLORS.border;
    ctx.strokeRect(10, legendY, canvas.width - 20, LEGEND_H);

    ctx.fillStyle = COLORS.textSecondary;
    ctx.font = 'bold 12px Arial';
    ctx.textAlign = 'left';
    ctx.fillText('LEGEND & TIMINGS', 20, legendY + 20);
    ctx.font = '11px Arial';
    [
        `Base = Standard  |  Push = +1 Level  |  Chill = -1 Level  |  Extra Chill = -2 Levels`,
        `ZS: ${policy.zs_time} Server Time  |  MG: ${policy.mg_time} Server Time`,
    ].forEach((item, i) => {
        ctx.fillStyle = COLORS.textSecondary;
        ctx.fillText(item, 20, legendY + 38 + i * 16);
    });

    ctx.fillStyle = COLORS.gold;
    ctx.font = 'bold 14px Arial';
    ctx.textAlign = 'right';
    ctx.fillText('CURRENT BASELINES', canvas.width - 20, legendY + 22);
    ctx.font = 'bold 22px Arial';
    ctx.fillStyle = COLORS.textPrimary;
    ctx.fillText(`MG LEVEL: ${policy.mg_baseline}  |  ZS LEVEL: ${policy.zs_baseline}`, canvas.width - 20, legendY + 52);
    ctx.font = '11px Arial';
    ctx.fillStyle = COLORS.textSecondary;
    ctx.fillText('(Reduced during gameplay seasons)', canvas.width - 20, legendY + 68);
}

// ── Baseline Inputs ───────────────────────────────────────────────────────────

function syncBaselineInputs(policy) {
    document.getElementById('mg-baseline').value = policy.mg_baseline;
    document.getElementById('zs-baseline').value = policy.zs_baseline;
    document.getElementById('mg-time').value = policy.mg_time || DEFAULT_MG_TIME;
    document.getElementById('zs-time').value = policy.zs_time || DEFAULT_ZS_TIME;
    document.getElementById('week-offset').value = policy.week_offset || 0;
    document.querySelectorAll('input[name="duration"]').forEach(r => {
        r.checked = Number(r.value) === (currentSchedule ? currentSchedule.duration_days : 14);
    });
    if (!canManage) {
        ['mg-baseline', 'zs-baseline', 'mg-time', 'zs-time', 'week-offset'].forEach(id => {
            document.getElementById(id).disabled = true;
        });
    }
}

function wireBaselineInputs() {
    document.getElementById('mg-baseline').addEventListener('input', e => {
        if (!currentPolicy) return;
        currentPolicy.mg_baseline = Number(e.target.value) || 11;
        renderDayGrid(currentPolicy);
    });
    document.getElementById('zs-baseline').addEventListener('input', e => {
        if (!currentPolicy) return;
        currentPolicy.zs_baseline = Number(e.target.value) || 7;
        renderDayGrid(currentPolicy);
    });
    document.getElementById('mg-time').addEventListener('change', e => {
        if (!currentPolicy) return;
        currentPolicy.mg_time = e.target.value;
        renderDayGrid(currentPolicy);
    });
    document.getElementById('zs-time').addEventListener('change', e => {
        if (!currentPolicy) return;
        currentPolicy.zs_time = e.target.value;
        renderDayGrid(currentPolicy);
    });
    document.getElementById('week-offset').addEventListener('input', e => {
        if (!currentPolicy) return;
        currentPolicy.week_offset = Number(e.target.value) || 0;
        renderDayGrid(currentPolicy);
    });
    document.querySelectorAll('input[name="duration"]').forEach(radio => {
        radio.addEventListener('change', () => {
            if (!currentPolicy || !currentSchedule) return;
            const newDur = Number(radio.value);
            currentSchedule.duration_days = newDur;
            while (currentPolicy.days.length < newDur) {
                const n = currentPolicy.days.length + 1;
                currentPolicy.days.push({
                    day_number: n,
                    mg: { active: false, vibe: 'base', time_override: null, conditional: null },
                    zs: { active: false, vibe: 'base', conditional: null },
                    custom_events: [],
                    notes: null,
                });
            }
            currentPolicy.days = currentPolicy.days.slice(0, newDur);
            renderDayGrid(currentPolicy);
        });
    });
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

function setButtonStates(hasSchedule) {
    if (canManage) {
        document.getElementById('btn-save').disabled = !hasSchedule;
        document.getElementById('btn-delete').disabled = !hasSchedule || !!(currentSchedule && currentSchedule.is_active);
        document.getElementById('btn-activate').disabled = !hasSchedule || !!(currentSchedule && currentSchedule.is_active);
    }
    document.getElementById('btn-text').disabled = !hasSchedule;
    document.getElementById('btn-infographic').disabled = !hasSchedule;
}

function loadSchedule(id) {
    const s = schedules.find(x => x.id === Number(id));
    if (!s) {
        currentSchedule = null;
        currentPolicy = null;
        document.getElementById('baseline-section').style.display = 'none';
        document.getElementById('day-grid-section').style.display = 'none';
        document.getElementById('text-section').classList.add('hidden');
        document.getElementById('infographic-section').classList.add('hidden');
        setButtonStates(false);
        return;
    }

    currentSchedule = JSON.parse(JSON.stringify(s));
    currentPolicy = getPolicyData(currentSchedule);

    document.getElementById('schedule-title-display').textContent = currentSchedule.name;
    document.getElementById('baseline-section').style.display = '';
    document.getElementById('day-grid-section').style.display = '';
    document.getElementById('text-section').classList.add('hidden');
    document.getElementById('infographic-section').classList.add('hidden');

    syncBaselineInputs(currentPolicy);
    setButtonStates(true);
    renderDayGrid(currentPolicy);
}

// ── API ───────────────────────────────────────────────────────────────────────

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
    if (!currentSchedule || !currentPolicy) return;
    const res = await fetch(`/api/schedules/${currentSchedule.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
        body: JSON.stringify({
            name: currentSchedule.name,
            duration_days: currentSchedule.duration_days,
            schedule_data: currentPolicy,
        }),
    });
    if (!res.ok) { alert('Save failed: ' + await res.text()); return; }
    const updated = await res.json();
    const idx = schedules.findIndex(s => s.id === updated.id);
    if (idx !== -1) schedules[idx] = updated;
    populateSelector(updated.id);
    loadSchedule(updated.id);
}

async function createNewSchedule(name, duration_days) {
    const res = await fetch('/api/schedules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
        body: JSON.stringify({ name, duration_days, schedule_data: buildEmptyPolicy(duration_days) }),
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
    currentPolicy = null;
    populateSelector(null);
    document.getElementById('baseline-section').style.display = 'none';
    document.getElementById('day-grid-section').style.display = 'none';
    document.getElementById('text-section').classList.add('hidden');
    document.getElementById('infographic-section').classList.add('hidden');
    setButtonStates(false);
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

// ── Day Modal ─────────────────────────────────────────────────────────────────

let _editDayIndex = null;

function openDayModal(dayIndex) {
    _editDayIndex = dayIndex;
    const day = currentPolicy.days[dayIndex];
    document.getElementById('day-modal-title').textContent = `Edit Day ${day.day_number}`;

    document.getElementById('mg-active').checked = !!(day.mg && day.mg.active);
    document.getElementById('mg-vibe').value = day.mg?.vibe || 'base';
    document.getElementById('mg-time-override').value = day.mg?.time_override || '';
    document.getElementById('mg-conditional').value = day.mg?.conditional || '';
    document.getElementById('mg-options').style.display = (day.mg && day.mg.active) ? '' : 'none';

    document.getElementById('zs-active').checked = !!(day.zs && day.zs.active);
    document.getElementById('zs-vibe').value = day.zs?.vibe || 'base';
    document.getElementById('zs-conditional').value = day.zs?.conditional || '';
    document.getElementById('zs-options').style.display = (day.zs && day.zs.active) ? '' : 'none';

    // Render custom events list
    renderCustomEventsList(day.custom_events || []);

    document.getElementById('day-notes').value = day.notes || '';
    document.getElementById('day-modal').style.display = 'flex';
}

function renderCustomEventsList(events) {
    const list = document.getElementById('custom-events-list');
    list.innerHTML = '';
    events.forEach((ev, i) => {
        const row = document.createElement('div');
        row.style.cssText = 'display:flex; align-items:center; gap:8px; margin-bottom:6px;';

        const iconSel = document.createElement('select');
        iconSel.style.cssText = 'width:60px; font-size:1.2em;';
        iconSel.dataset.ceIndex = i;
        iconSel.dataset.ceField = 'icon';
        CUSTOM_EVENT_ICONS.forEach(ic => {
            const opt = document.createElement('option');
            opt.value = ic;
            opt.textContent = ic;
            if (ic === ev.icon) opt.selected = true;
            iconSel.appendChild(opt);
        });

        const labelInput = document.createElement('input');
        labelInput.type = 'text';
        labelInput.value = ev.label;
        labelInput.maxLength = 60;
        labelInput.placeholder = 'Event label';
        labelInput.style.flex = '1';
        labelInput.dataset.ceIndex = i;
        labelInput.dataset.ceField = 'label';

        const removeBtn = document.createElement('button');
        removeBtn.type = 'button';
        removeBtn.className = 'btn btn-danger';
        removeBtn.style.padding = '2px 8px';
        removeBtn.textContent = '✕';
        removeBtn.onclick = () => {
            const day = currentPolicy.days[_editDayIndex];
            day.custom_events.splice(i, 1);
            renderCustomEventsList(day.custom_events);
        };

        row.appendChild(iconSel);
        row.appendChild(labelInput);
        row.appendChild(removeBtn);
        list.appendChild(row);
    });
}

function closeDayModal() {
    document.getElementById('day-modal').style.display = 'none';
    _editDayIndex = null;
}

document.getElementById('btn-add-custom-event').addEventListener('click', () => {
    if (_editDayIndex === null) return;
    const day = currentPolicy.days[_editDayIndex];
    day.custom_events = day.custom_events || [];
    day.custom_events.push({ icon: CUSTOM_EVENT_ICONS[0], label: '' });
    renderCustomEventsList(day.custom_events);
});

document.getElementById('mg-active').addEventListener('change', e => {
    document.getElementById('mg-options').style.display = e.target.checked ? '' : 'none';
});
document.getElementById('zs-active').addEventListener('change', e => {
    document.getElementById('zs-options').style.display = e.target.checked ? '' : 'none';
});

document.getElementById('day-form').addEventListener('submit', e => {
    e.preventDefault();
    if (_editDayIndex === null) return;
    const day = currentPolicy.days[_editDayIndex];

    day.mg = {
        active: document.getElementById('mg-active').checked,
        vibe: document.getElementById('mg-vibe').value,
        time_override: document.getElementById('mg-time-override').value.trim() || null,
        conditional: document.getElementById('mg-conditional').value.trim() || null,
    };
    day.zs = {
        active: document.getElementById('zs-active').checked,
        vibe: document.getElementById('zs-vibe').value,
        conditional: document.getElementById('zs-conditional').value.trim() || null,
    };

    // Collect custom events from dynamic list
    day.custom_events = Array.from(
        document.querySelectorAll('#custom-events-list > div')
    ).map(row => {
        const iconSel = row.querySelector('select');
        const labelInp = row.querySelector('input[type="text"]');
        return { icon: iconSel.value, label: labelInp.value.trim() };
    }).filter(ev => ev.label);

    day.notes = document.getElementById('day-notes').value.trim() || null;

    renderDayGrid(currentPolicy);
    closeDayModal();
});

document.getElementById('day-modal').addEventListener('click', e => {
    if (e.target === e.currentTarget) closeDayModal();
});

// ── New Schedule Modal ────────────────────────────────────────────────────────

function closeNewScheduleModal() {
    document.getElementById('new-schedule-modal').style.display = 'none';
}

document.getElementById('new-schedule-modal').addEventListener('click', e => {
    if (e.target === e.currentTarget) closeNewScheduleModal();
});

document.getElementById('new-schedule-form').addEventListener('submit', async e => {
    e.preventDefault();
    const name = document.getElementById('new-schedule-name').value.trim();
    const duration_days = Number(document.querySelector('input[name="new-duration"]:checked').value);
    closeNewScheduleModal();
    await createNewSchedule(name, duration_days);
});

// ── Toolbar Buttons ───────────────────────────────────────────────────────────

document.getElementById('schedule-select').addEventListener('change', e => loadSchedule(e.target.value));

if (canManage) {
    document.getElementById('btn-new').addEventListener('click', () => {
        document.getElementById('new-schedule-name').value = '';
        document.querySelector('input[name="new-duration"][value="14"]').checked = true;
        document.getElementById('new-schedule-modal').style.display = 'flex';
    });
    document.getElementById('btn-save').addEventListener('click', saveCurrentSchedule);
    document.getElementById('btn-delete').addEventListener('click', deleteCurrentSchedule);
    document.getElementById('btn-activate').addEventListener('click', activateCurrentSchedule);
}

document.getElementById('btn-text').addEventListener('click', () => {
    if (!currentSchedule) return;
    document.getElementById('text-output').value = generateText(currentSchedule);
    document.getElementById('text-section').classList.remove('hidden');
    document.getElementById('infographic-section').classList.add('hidden');
    document.getElementById('text-section').scrollIntoView({ behavior: 'smooth' });
});

document.getElementById('btn-copy-text').addEventListener('click', () => {
    navigator.clipboard.writeText(document.getElementById('text-output').value).then(() => {
        const btn = document.getElementById('btn-copy-text');
        const orig = btn.textContent;
        btn.textContent = 'Copied!';
        setTimeout(() => { btn.textContent = orig; }, 2000);
    });
});

document.getElementById('btn-infographic').addEventListener('click', () => {
    if (!currentSchedule) return;
    document.getElementById('infographic-section').classList.remove('hidden');
    document.getElementById('text-section').classList.add('hidden');
    renderCanvas(currentSchedule);
    document.getElementById('infographic-section').scrollIntoView({ behavior: 'smooth' });
});

document.getElementById('btn-download-png').addEventListener('click', () => {
    const canvas = document.getElementById('schedule-canvas');
    const link = document.createElement('a');
    link.download = `${currentSchedule.name.replace(/\s+/g, '_')}_schedule.png`;
    link.href = canvas.toDataURL('image/png');
    link.click();
});

document.getElementById('btn-download-jpg').addEventListener('click', () => {
    const canvas = document.getElementById('schedule-canvas');
    const offscreen = document.createElement('canvas');
    offscreen.width = canvas.width;
    offscreen.height = canvas.height;
    const ctx = offscreen.getContext('2d');
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, offscreen.width, offscreen.height);
    ctx.drawImage(canvas, 0, 0);
    const link = document.createElement('a');
    link.download = `${currentSchedule.name.replace(/\s+/g, '_')}_schedule.jpg`;
    link.href = offscreen.toDataURL('image/jpeg', 0.92);
    link.click();
});

// ── Init ──────────────────────────────────────────────────────────────────────

wireBaselineInputs();
fetchSchedules();
