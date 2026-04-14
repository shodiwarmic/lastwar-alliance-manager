// activity.js - Activity log page

const IS_ADMIN = window.IS_ADMIN === true;

// Choices.js instance — initialised in DOMContentLoaded
let userFilterChoices = null;

const ENTITY_LABELS = {
    member:           'member',
    alias:            'alias',
    user:             'user',
    prospect:         'prospect',
    ally:             'ally',
    agreement_type:   'agreement type',
    train_log:        'train log',
    eligibility_rule: 'eligibility rule',
    oc_category:      'OC category',
    oc_responsibility:'OC responsibility',
    oc_assignee:      'OC assignee',
    award_type:       'award type',
    awards:           'awards',
    file:             'file',
    schedule:             'schedule',
    schedule_event:       'schedule event',
    schedule_event_type:  'schedule event type',
    server_event:         'server event',
    storm_assignments:'storm assignments',
    storm_config:     'storm config',
    storm_group:      'storm group',
    invite:           'invite',
    vs_points:        'VS points',
    power_records:    'power records',
    permissions:      'permissions',
    settings:              'settings',
    credentials:           'credentials',
    accountability_strike: 'accountability strike',
    storm_attendance:      'storm attendance',
    season_attendance:     'season attendance',
    season_contributions:  'season contributions',
    season_rewards:        'season reward',
    season_mail:           'season mail',
    season_config:         'season config',
};

const ENTITY_LABELS_PLURAL = {
    member:           'members',
    alias:            'aliases',
    user:             'users',
    prospect:         'prospects',
    ally:             'allies',
    agreement_type:   'agreement types',
    train_log:        'train logs',
    eligibility_rule: 'eligibility rules',
    oc_category:      'OC categories',
    oc_responsibility:'OC responsibilities',
    oc_assignee:      'OC assignees',
    award_type:       'award types',
    file:             'files',
    storm_group:      'storm groups',
    vs_points:             'VS points',
    power_records:         'power records',
    accountability_strike: 'accountability strikes',
    season_contributions:  'season contributions',
    season_rewards:        'season rewards',
    season_mail:           'season mail items',
};

function entityLabel(type, count) {
    if (count > 1 && ENTITY_LABELS_PLURAL[type]) {
        return ENTITY_LABELS_PLURAL[type];
    }
    return ENTITY_LABELS[type] || type;
}

function formatRelativeTime(dateStr) {
    const date = new Date(dateStr + (dateStr.endsWith('Z') ? '' : 'Z'));
    const diffMs = Date.now() - date.getTime();
    const diffSec = Math.floor(diffMs / 1000);

    if (diffSec < 60)    return 'just now';
    if (diffSec < 3600)  return `${Math.floor(diffSec / 60)}m ago`;
    if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
    if (diffSec < 604800) return `${Math.floor(diffSec / 86400)}d ago`;

    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

function formatAbsoluteTime(dateStr) {
    const date = new Date(dateStr + (dateStr.endsWith('Z') ? '' : 'Z'));
    return date.toLocaleString();
}

function buildDescription(entry) {
    const count = entry.entity_count;
    const label = entityLabel(entry.entity_type, count);

    if (count > 1) {
        // e.g. "created 3 OC categories"
        return { verb: entry.action, count, label, name: null };
    }
    return { verb: entry.action, count: 1, label, name: entry.entity_name };
}

function renderEntry(entry) {
    const row = document.createElement('div');
    row.className = 'activity-entry' + (entry.is_sensitive ? ' activity-entry--sensitive' : '');

    const actor = document.createElement('span');
    actor.className = 'activity-entry__actor';
    actor.textContent = entry.username;

    const desc = buildDescription(entry);
    const text = document.createElement('span');
    text.className = 'activity-entry__text';

    const verbSpan = document.createElement('span');
    verbSpan.textContent = ' ' + desc.verb + ' ';

    const countSpan = document.createElement('span');
    if (desc.count > 1) {
        countSpan.textContent = desc.count + ' ';
    }

    const labelSpan = document.createElement('span');
    labelSpan.textContent = desc.label;

    text.appendChild(verbSpan);
    text.appendChild(countSpan);
    text.appendChild(labelSpan);

    if (desc.name) {
        const nameSpan = document.createElement('span');
        nameSpan.className = 'activity-entry__name';
        nameSpan.textContent = ' "' + desc.name + '"';
        text.appendChild(nameSpan);
    }

    // Use updated_at for display (reflects batching window)
    const timeEl = document.createElement('span');
    timeEl.className = 'activity-entry__time';
    timeEl.textContent = formatRelativeTime(entry.updated_at);
    timeEl.title = formatAbsoluteTime(entry.updated_at);

    row.appendChild(actor);
    row.appendChild(text);
    row.appendChild(timeEl);

    if (entry.details) {
        const detailsEl = document.createElement('span');
        detailsEl.className = 'activity-entry__details activity-entry__details--collapsed';
        detailsEl.textContent = entry.details;

        const expandBtn = document.createElement('span');
        expandBtn.className = 'activity-entry__expand-btn';
        expandBtn.textContent = 'show more';
        expandBtn.style.display = 'none'; // hidden until we confirm truncation

        const toggle = () => {
            const collapsed = detailsEl.classList.contains('activity-entry__details--collapsed');
            detailsEl.classList.toggle('activity-entry__details--collapsed', !collapsed);
            detailsEl.classList.toggle('activity-entry__details--expanded', collapsed);
            expandBtn.textContent = collapsed ? 'show less' : 'show more';
        };
        detailsEl.addEventListener('click', toggle);
        expandBtn.addEventListener('click', toggle);

        row.appendChild(detailsEl);
        row.appendChild(expandBtn);

        // Reveal the toggle only when the text is genuinely clamped
        requestAnimationFrame(() => {
            if (detailsEl.scrollWidth > detailsEl.clientWidth || detailsEl.scrollHeight > detailsEl.clientHeight) {
                expandBtn.style.display = '';
            } else {
                // Not truncated — remove collapsed styling so full text shows
                detailsEl.classList.remove('activity-entry__details--collapsed');
            }
        });
    }

    if (IS_ADMIN && entry.is_sensitive) {
        const badge = document.createElement('span');
        badge.className = 'activity-entry__badge';
        badge.textContent = 'sensitive';
        row.appendChild(badge);
    }

    return row;
}

function populateUserFilter(entries) {
    if (!userFilterChoices) return;
    const seen = new Set();
    const userOpts = [];
    entries.forEach(e => {
        if (!seen.has(e.username)) {
            seen.add(e.username);
            userOpts.push({ value: e.user_id != null ? String(e.user_id) : '', label: e.username });
        }
    });
    userFilterChoices.setChoices(
        [{ value: '', label: 'All users', placeholder: true }, ...userOpts],
        'value', 'label', true
    );
}

let allEntries = [];
let userFilterPopulated = false;

async function loadActivity() {
    const list = document.getElementById('activity-list');
    const limit = document.getElementById('limit-select').value;
    const userID = document.getElementById('user-filter').value;
    const showSensitive = IS_ADMIN
        ? document.getElementById('sensitive-toggle').checked
        : false;

    let url = `/api/activity?limit=${limit}`;
    if (userID) url += `&user_id=${userID}`;

    try {
        const res = await fetch(url);
        if (!res.ok) throw new Error('Request failed');
        const entries = await res.json();

        allEntries = entries;

        if (!userFilterPopulated) {
            populateUserFilter(entries);
            userFilterPopulated = true;
        }

        list.replaceChildren();

        const filtered = IS_ADMIN && !showSensitive
            ? entries.filter(e => !e.is_sensitive)
            : entries;

        if (!filtered.length) {
            const msg = document.createElement('p');
            msg.className = 'empty-msg';
            msg.textContent = 'No activity found.';
            list.appendChild(msg);
            return;
        }

        filtered.forEach(entry => list.appendChild(renderEntry(entry)));
    } catch {
        list.replaceChildren();
        const msg = document.createElement('p');
        msg.className = 'error-msg';
        msg.textContent = 'Failed to load activity log.';
        list.appendChild(msg);
    }
}

document.addEventListener('DOMContentLoaded', () => {
    userFilterChoices = new Choices('#user-filter', {
        searchEnabled: true, searchPlaceholderValue: 'Search…',
        itemSelectText: '', shouldSort: false,
    });

    loadActivity();

    document.getElementById('limit-select').addEventListener('change', () => {
        userFilterPopulated = false;
        userFilterChoices.setChoices(
            [{ value: '', label: 'All users', placeholder: true }],
            'value', 'label', true
        );
        loadActivity();
    });

    document.getElementById('user-filter').addEventListener('change', loadActivity);

    if (IS_ADMIN) {
        document.getElementById('sensitive-toggle').addEventListener('change', () => {
            // Filter client-side — no re-fetch needed
            const list = document.getElementById('activity-list');
            const showSensitive = document.getElementById('sensitive-toggle').checked;
            const filtered = showSensitive ? allEntries : allEntries.filter(e => !e.is_sensitive);

            list.replaceChildren();
            if (!filtered.length) {
                const msg = document.createElement('p');
                msg.className = 'empty-msg';
                msg.textContent = 'No activity found.';
                list.appendChild(msg);
                return;
            }
            filtered.forEach(entry => list.appendChild(renderEntry(entry)));
        });
    }
});
