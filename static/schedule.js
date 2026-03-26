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

// Sort event objects by .time — null/missing times float to the top.
function sortByTime(items) {
    return [...items].sort((a, b) => {
        if (!a.time && !b.time) return 0;
        if (!a.time) return -1;
        if (!b.time) return 1;
        return timeToMinutes(a.time) - timeToMinutes(b.time);
    });
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
            zs: { active: false, vibe: 'base', time_override: null, conditional: null },
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
        if (d.zs && d.zs.time_override === undefined) d.zs.time_override = null;
        // Backfill column/time on custom events added before these fields existed
        d.custom_events.forEach(ev => {
            if (ev.column === undefined) ev.column = 'alliance';
            if (ev.time === undefined) ev.time = null;
        });
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
    list.replaceChildren();
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
    grid.replaceChildren();
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
        dayLabel.appendChild(document.createTextNode(`D${day.day_number}\u00a0`));
        const themeSpan = document.createElement('span');
        themeSpan.style.cssText = 'font-weight:normal; color:var(--text-secondary);';
        themeSpan.textContent = `${theme.icon} ${theme.label}`;
        dayLabel.appendChild(themeSpan);
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
            const zsTime = day.zs.time_override || policy.zs_time;
            chips.appendChild(makeChip(`🧟 ZS @ ${zsTime} (Lv.${level}${vibeLabel})`, 'var(--accent-danger, #c0392b)'));
        }

        // Custom events
        (day.custom_events || []).forEach(ev => {
            const timeStr = ev.time ? ` @ ${ev.time}` : '';
            const bg = ev.column === 'server' ? 'var(--accent-secondary, #555)' : 'var(--accent-warning, #7d6608)';
            chips.appendChild(makeChip(`${ev.icon} ${ev.label}${timeStr}`, bg));
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

function generateText(schedule, policy) {
    const lines = [];

    lines.push(`📅 ${schedule.name}`);
    lines.push(`📊 Weekly VS Theme: ${VS_THEMES.map(t => t.label).join(' > ')}`);
    lines.push(`🎯 Current Baselines: MG ${policy.mg_baseline} | ZS ${policy.zs_baseline}`);
    lines.push(`   (Note: Baselines are reduced during gameplay seasons)`);
    lines.push('');

    const weeks = Math.ceil(policy.days.length / 7);
    for (let w = 1; w <= weeks; w++) {
        const displayWeek = w + (policy.week_offset || 0);
        lines.push(`📅 WEEK ${displayWeek}`);
        policy.days.filter(d => Math.ceil(d.day_number / 7) === w).forEach(day => {
            const evs = [];
            getServerEvents(day.day_number).forEach(ev => {
                evs.push({ time: null, text: `${ev.label} ${ev.icon}` });
            });
            if (day.mg && day.mg.active) {
                const mgTime = day.mg.time_override || policy.mg_time;
                const vibe = day.mg.vibe ? VIBES[day.mg.vibe].label : 'Base';
                let s = `MG @ ${mgTime} (${vibe})`;
                if (day.mg.conditional) s += ` [${day.mg.conditional}]`;
                evs.push({ time: mgTime, text: s });
            }
            if (day.zs && day.zs.active) {
                const zsTime = day.zs.time_override || policy.zs_time;
                const vibe = day.zs.vibe ? VIBES[day.zs.vibe].label : 'Base';
                let s = `ZS @ ${zsTime} (${vibe})`;
                if (day.zs.conditional) s += ` [${day.zs.conditional}]`;
                evs.push({ time: zsTime, text: s });
            }
            (day.custom_events || []).forEach(ev => {
                const timeStr = ev.time ? ` @ ${ev.time}` : '';
                evs.push({ time: ev.time || null, text: `${ev.icon} ${ev.label}${timeStr}` });
            });
            const sorted = sortByTime(evs);
            const displayDayNum = ((day.day_number - 1) % 7) + 1;
            lines.push(`• D${displayDayNum}: ${sorted.length ? sorted.map(e => e.text).join(' | ') : 'No Events'}`);
        });
        lines.push('');
    }

    lines.push(`📋 LEVEL GUIDE`);
    lines.push(`• Extra Chill (Post-Event): -2 Levels. Run after major server events.`);
    lines.push(`• Chill (Easy): -1 Level for a weekend breather.`);
    lines.push(`• Base (Standard): ±0 Levels`);
    lines.push(`• Push (Challenge): +1 Level. Leadership announces on event days.`);

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

function drawCellLines(ctx, lines, centerX, ry, rowH) {
    ctx.textAlign = 'center';
    if (lines.length === 0) {
        ctx.font = '11px Arial';
        ctx.fillStyle = COLORS.textSecondary;
        ctx.fillText('-', centerX, ry + rowH / 2 + 5);
        return;
    }
    const fontSize = lines.length <= 2 ? 10 : 9;
    const lineH = lines.length === 1 ? 0 : Math.min(16, (rowH - 10) / (lines.length - 1));
    const totalH = (lines.length - 1) * lineH;
    const startY = ry + (rowH - totalH) / 2 + fontSize / 2;
    lines.forEach((line, i) => {
        ctx.font = `${fontSize}px Arial`;
        ctx.fillStyle = line.color;
        ctx.fillText(line.text, centerX, startY + i * lineH);
    });
}

function renderCanvas(schedule, policy) {
    const canvas = document.getElementById('schedule-canvas');
    const weeks = Math.ceil(policy.days.length / 7);

    const ROW_H      = 52;
    const HEADER_H   = 60;
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
    ctx.fillText(schedule.name, canvas.width / 2, 40);

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
        const subCols = ['Day', 'VS Theme', 'Alliance', 'Server'];
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

            // Day number — cycles 1-7 per week
            const displayDay = ((day.day_number - 1) % 7) + 1;
            ctx.fillStyle = COLORS.textPrimary;
            ctx.font = 'bold 13px Arial';
            ctx.textAlign = 'center';
            ctx.fillText(`D${displayDay}`, cx + subColW * 0.5, ry + ROW_H / 2 + 5);

            // VS theme — icon + full label, two lines, centered
            const themeCenterX = cx + subColW * 1.5;
            ctx.textAlign = 'center';
            ctx.fillStyle = COLORS.textTheme;
            ctx.font = '14px Arial';
            ctx.fillText(theme.icon, themeCenterX, ry + ROW_H / 2 - 4);
            ctx.font = '10px Arial';
            ctx.fillText(theme.label, themeCenterX, ry + ROW_H / 2 + 10);

            // Alliance column — MG, ZS, and alliance-type custom events, sorted by time
            const allianceRaw = [];
            if (day.mg && day.mg.active) {
                const mgTime = day.mg.time_override || policy.mg_time;
                const vibe = day.mg.vibe ? VIBES[day.mg.vibe].label : 'Base';
                allianceRaw.push({ time: mgTime, text: `🛡️ MG @${mgTime} (${vibe})`, color: COLORS.accentMG });
            }
            if (day.zs && day.zs.active) {
                const zsTime = day.zs.time_override || policy.zs_time;
                const vibe = day.zs.vibe ? VIBES[day.zs.vibe].label : 'Base';
                allianceRaw.push({ time: zsTime, text: `🧟 ZS @${zsTime} (${vibe})`, color: COLORS.accentZS });
            }
            (day.custom_events || []).filter(ev => ev.column !== 'server').forEach(ev => {
                const timeStr = ev.time ? ` @${ev.time}` : '';
                allianceRaw.push({ time: ev.time || null, text: `${ev.icon} ${ev.label}${timeStr}`, color: COLORS.gold });
            });
            drawCellLines(ctx, sortByTime(allianceRaw), cx + subColW * 2.5, ry, ROW_H);

            // Server column — fixed server events + server-type custom events, sorted by time
            const serverEvs = getServerEvents(day.day_number);
            const serverCustom = (day.custom_events || []).filter(ev => ev.column === 'server');
            const serverRaw = [
                ...serverEvs.map(ev => ({ time: null, text: `${ev.icon} ${ev.label}`, color: COLORS.accentServer })),
                ...serverCustom.map(ev => {
                    const timeStr = ev.time ? ` @${ev.time}` : '';
                    return { time: ev.time || null, text: `${ev.icon} ${ev.label}${timeStr}`, color: COLORS.accentServer };
                }),
            ];
            drawCellLines(ctx, sortByTime(serverRaw), cx + subColW * 3.5, ry, ROW_H);
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
        `Extra Chill = -2 Levels  |  Chill = -1 Level  |  Base = Standard  |  Push = +1 Level`,
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
    document.getElementById('schedule-title-input').disabled = !canManage;
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
}

// ── API ───────────────────────────────────────────────────────────────────────

function setButtonStates(loaded) {
    if (canManage) {
        document.getElementById('btn-save').disabled = !loaded;
        document.getElementById('btn-import').disabled = !loaded;
        document.getElementById('btn-clear').disabled = !loaded;
    }
    document.getElementById('btn-text').disabled = !loaded;
    document.getElementById('btn-infographic').disabled = !loaded;
    document.getElementById('btn-export').disabled = !loaded;
}

function applySchedule(s) {
    currentSchedule = s;
    currentPolicy = getPolicyData(s);
    document.getElementById('schedule-title-display').textContent = s.name;
    document.getElementById('schedule-title-input').value = s.name;
    document.getElementById('baseline-section').style.display = '';
    document.getElementById('day-grid-section').style.display = '';
    document.getElementById('text-section').classList.add('hidden');
    document.getElementById('infographic-section').classList.add('hidden');
    syncBaselineInputs(currentPolicy);
    setButtonStates(true);
    renderDayGrid(currentPolicy);
}

async function fetchSchedule() {
    const res = await fetch('/api/schedule');
    if (!res.ok) return;
    applySchedule(await res.json());
}

async function saveCurrentSchedule() {
    if (!currentSchedule || !currentPolicy) return;
    const res = await fetch('/api/schedule', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
        body: JSON.stringify({
            name: document.getElementById('schedule-title-input').value.trim() || currentSchedule.name,
            duration_days: currentSchedule.duration_days,
            schedule_data: currentPolicy,
        }),
    });
    if (!res.ok) { showToast('Save failed: ' + await res.text(), 'error'); return; }
    applySchedule(await res.json());
    showToast('Schedule saved.');
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
    document.getElementById('zs-time-override').value = day.zs?.time_override || '';
    document.getElementById('zs-conditional').value = day.zs?.conditional || '';
    document.getElementById('zs-options').style.display = (day.zs && day.zs.active) ? '' : 'none';

    // Render custom events list
    renderCustomEventsList(day.custom_events || []);

    document.getElementById('day-notes').value = day.notes || '';
    document.getElementById('day-modal').style.display = 'flex';
}

function renderCustomEventsList(events) {
    const list = document.getElementById('custom-events-list');
    list.replaceChildren();
    events.forEach((ev, i) => {
        const row = document.createElement('div');
        row.style.cssText = 'display:flex; align-items:center; gap:8px; margin-bottom:6px; flex-wrap:wrap;';

        const iconSel = document.createElement('select');
        iconSel.style.cssText = 'width:60px; font-size:1.2em;';
        CUSTOM_EVENT_ICONS.forEach(ic => {
            const opt = document.createElement('option');
            opt.value = ic;
            opt.textContent = ic;
            if (ic === ev.icon) opt.selected = true;
            iconSel.appendChild(opt);
        });

        const labelInput = document.createElement('input');
        labelInput.type = 'text';
        labelInput.value = ev.label || '';
        labelInput.maxLength = 60;
        labelInput.placeholder = 'Event label';
        labelInput.style.flex = '1';
        labelInput.style.minWidth = '100px';

        const timeInput = document.createElement('input');
        timeInput.type = 'text';
        timeInput.value = ev.time || '';
        timeInput.pattern = '([0-1][0-9]|2[0-3]):[0-5][0-9]';
        timeInput.placeholder = 'HH:MM';
        timeInput.style.width = '72px';
        timeInput.title = 'Optional time (24h)';

        const colSel = document.createElement('select');
        colSel.style.width = '90px';
        [['alliance', 'Alliance'], ['server', 'Server']].forEach(([val, lbl]) => {
            const opt = document.createElement('option');
            opt.value = val;
            opt.textContent = lbl;
            if ((ev.column || 'alliance') === val) opt.selected = true;
            colSel.appendChild(opt);
        });

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
        row.appendChild(timeInput);
        row.appendChild(colSel);
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
    day.custom_events.push({ icon: CUSTOM_EVENT_ICONS[0], label: '', time: null, column: 'alliance' });
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
        time_override: document.getElementById('zs-time-override').value.trim() || null,
        conditional: document.getElementById('zs-conditional').value.trim() || null,
    };

    // Collect custom events from dynamic list
    day.custom_events = Array.from(
        document.querySelectorAll('#custom-events-list > div')
    ).map(row => {
        const selects = row.querySelectorAll('select');
        const inputs  = row.querySelectorAll('input[type="text"]');
        return {
            icon:   selects[0].value,
            label:  inputs[0].value.trim(),
            time:   inputs[1].value.trim() || null,
            column: selects[1].value,
        };
    }).filter(ev => ev.label);

    day.notes = document.getElementById('day-notes').value.trim() || null;

    renderDayGrid(currentPolicy);
    closeDayModal();
});

document.getElementById('day-modal').addEventListener('click', e => {
    if (e.target === e.currentTarget) closeDayModal();
});

// ── Toolbar Buttons ───────────────────────────────────────────────────────────

if (canManage) {
    document.getElementById('btn-save').addEventListener('click', saveCurrentSchedule);
}

document.getElementById('btn-text').addEventListener('click', () => {
    if (!currentSchedule || !currentPolicy) return;
    document.getElementById('text-output').value = generateText(currentSchedule, currentPolicy);
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
    if (!currentSchedule || !currentPolicy) return;
    document.getElementById('infographic-section').classList.remove('hidden');
    document.getElementById('text-section').classList.add('hidden');
    renderCanvas(currentSchedule, currentPolicy);
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

// ── Import / Export / Clear ───────────────────────────────────────────────────

document.getElementById('btn-export').addEventListener('click', () => {
    if (!currentPolicy) return;
    const json = JSON.stringify(currentPolicy, null, 2);
    const blob = new Blob([json], { type: 'application/json' });
    const link = document.createElement('a');
    link.download = `${(currentSchedule?.name || 'schedule').replace(/\s+/g, '_')}_policy.json`;
    link.href = URL.createObjectURL(blob);
    link.click();
    URL.revokeObjectURL(link.href);
});

if (canManage) {
    document.getElementById('btn-import').addEventListener('click', () => {
        document.getElementById('import-file-input').click();
    });

    document.getElementById('import-file-input').addEventListener('change', e => {
        const file = e.target.files[0];
        if (!file) return;
        const reader = new FileReader();
        reader.onload = evt => {
            try {
                const parsed = JSON.parse(evt.target.result);
                if (!parsed.days || !Array.isArray(parsed.days)) {
                    showToast('Invalid schedule JSON: missing "days" array.', 'error');
                    return;
                }
                currentPolicy = getPolicyData({ ...currentSchedule, schedule_data: parsed });
                syncBaselineInputs(currentPolicy);
                renderDayGrid(currentPolicy);
                document.getElementById('text-section').classList.add('hidden');
                document.getElementById('infographic-section').classList.add('hidden');
            } catch {
                showToast('Failed to parse JSON file.', 'error');
            }
        };
        reader.readAsText(file);
        e.target.value = '';
    });

    document.getElementById('btn-clear').addEventListener('click', async () => {
        if (!currentPolicy) return;
        if (!await showConfirm('Clear all events from every day? Policy settings (baselines, times) will be kept.', 'Clear All')) return;
        currentPolicy.days.forEach(d => {
            d.mg = { active: false, vibe: 'base', time_override: null, conditional: null };
            d.zs = { active: false, vibe: 'base', time_override: null, conditional: null };
            d.custom_events = [];
            d.notes = null;
        });
        renderDayGrid(currentPolicy);
        document.getElementById('text-section').classList.add('hidden');
        document.getElementById('infographic-section').classList.add('hidden');
    });
}

// ── Init ──────────────────────────────────────────────────────────────────────

wireBaselineInputs();
fetchSchedule();
