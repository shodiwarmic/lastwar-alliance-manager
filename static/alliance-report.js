// static/alliance-report.js — the External Alliances "Scout Report" tab.
//
// Turns a LastRank alliance link into a sortable / filterable / exportable member table
// for an outside alliance, usually a VS opponent.
//
// NOTHING HERE IS PERSISTED. The rows live in `rows` for the life of this tab; the server
// stores only the alliance-level numbers, and only for alliances already in the registry.
// That is also why the extended pass is a loop here rather than a background job: the jobs
// framework takes no per-run target, and its item table would persist one opponent
// player's name per row. Pacing is enforced server-side by the shared 1 req/sec limiter,
// so this loop cannot outrun the politeness budget however fast it iterates.
'use strict';

(function () {
    const tabBar = document.querySelector('.tab-bar');
    // No tab bar means a view-only rank: the report tab was never rendered and the
    // registry shows on its own. Nothing to wire.
    if (!tabBar) return;

    // init() activates the hash's tab (or the default) itself — no follow-up show() needed.
    window.Tabs.init({ hash: true, defaultTab: 'registry' });

    const runBtn = document.getElementById('rep-run-btn');
    const stopBtn = document.getElementById('rep-stop-btn');
    const input = document.getElementById('rep-alliance-input');
    const extendedBox = document.getElementById('rep-extended');
    const statusEl = document.getElementById('rep-status');
    const progressEl = document.getElementById('rep-progress');
    const summaryEl = document.getElementById('rep-summary');
    const resultsEl = document.getElementById('rep-results');
    const theadEl = document.getElementById('rep-thead');
    const tbodyEl = document.getElementById('rep-tbody');
    const countEl = document.getElementById('rep-count');
    const exportHint = document.getElementById('rep-export-hint');
    if (!runBtn || !input) return;

    // Deliberately no OUR_SERVER_ID here. Scout targets are usually VS Duel League
    // opponents, and that league is CROSS-SERVER — defaulting the search to our own server
    // would hide exactly the alliances an officer is trying to find.

    // ---- state ----
    let alliance = null;      // AllianceReport minus members
    let rows = [];            // AllianceReportMember + merged extended fields
    let filtered = [];
    let hasExtended = false;  // any row carries extended data → render those columns
    let running = false;
    let abortFlag = false;
    let inFlight = null;      // AbortController for the current player request
    const sortState = { field: 'power', dir: 'desc' };

    // ---- small helpers (same shape as external-alliances.js, per F-R10) ----
    function el(tag, props, ...children) {
        const node = document.createElement(tag);
        if (props) Object.entries(props).forEach(([k, v]) => {
            if (v == null) return;
            if (k === 'className') node.className = v;
            else if (k === 'text') node.textContent = v;
            else if (k === 'onclick') node.addEventListener('click', v);
            else node.setAttribute(k, v);
        });
        children.flat().forEach(c => { if (c != null) node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c); });
        return node;
    }
    const fmtBig = n => {
        if (n == null) return '—';
        n = Number(n);
        if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B';
        if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
        if (n >= 1e3) return (n / 1e3).toFixed(0) + 'K';
        return '' + n;
    };
    async function api(method, url, body, signal) {
        const opt = { method, headers: {}, signal };
        if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
        const res = await fetch(url, opt);
        const text = (await res.text()).trim();
        if (!res.ok) throw new Error(text || (res.status + ' error'));
        return text ? JSON.parse(text) : null;
    }
    const setStatus = m => { if (statusEl) statusEl.textContent = m || ''; };
    const rankLabel = r => (r >= 1 && r <= 5) ? 'R' + r : '—';
    const dateOnly = s => (s || '').slice(0, 10) || '—';

    // What LastRank did for this member. "fetched" is the slow case — a live game re-pull —
    // so naming it turns an unexplained 20-second row into an understood one.
    const ENRICH_LABEL = {
        fetched: 'refreshed live',
        cached: 'cached',
        gated: 'cached (refresh rate-limited)',
        unavailable: 'cached (refresh unavailable)',
    };

    // ---- the alliance picker ----
    // Local matches over the registry the other tab already loaded, plus a click-only
    // LastRank lookup. Never fires the remote search per keystroke — every remote source
    // is the volunteer-run API behind the shared limiter.
    let registryCache = [];
    async function loadRegistry() {
        try {
            registryCache = (await api('GET', '/api/external-alliances')) || [];
        } catch { registryCache = []; }
    }

    const finder = window.buildRemoteFinder({
        inputs: [input],
        localSearch: q => {
            const f = window.foldSearch(q);
            if (!f) return [];
            return registryCache
                .filter(a => window.foldSearch(a.tag || '').includes(f) || window.foldSearch(a.name || '').includes(f))
                .filter(a => a.lastrank_id)
                .slice(0, 8);
        },
        // scope=any → the site's own search, every server. No server parameter at all.
        remoteSearch: async q =>
            (await api('GET', '/api/external-alliances/search?scope=any&q=' + encodeURIComponent(q))) || [],
        guard: q => q.length < 2 ? 'Type at least 2 characters to search LastRank.' : null,
        actionLabel: q => 'Search LastRank for “' + q + '” (all servers)',
        item: (a, kind) => {
            const tag = a.tag ? '[' + a.tag + '] ' : '';
            // Server leads the meta line: a tag search routinely returns the same tag on a
            // dozen different servers, so it is the only thing that tells them apart.
            const bits = [];
            bits.push(a.server != null ? 'Server ' + a.server : 'server unknown');
            if (a.power != null) bits.push(fmtBig(a.power) + ' power');
            bits.push(kind === 'local' ? 'in your registry' : 'LastRank');
            return {
                label: tag + (a.name || ''),
                meta: bits.join(' · '),
                onPick: () => { input.value = a.lastrank_id || ''; runReport(); },
            };
        },
        prefix: 'finder',
    });
    input.parentElement.appendChild(finder.dropdown);

    // ---- basic report ----
    async function runReport() {
        if (running) return;
        const raw = input.value.trim();
        if (!raw) { showToast('Paste an alliance link or pick one from the list.', 'error'); return; }

        finder.close();
        running = true;
        runBtn.disabled = true;
        setStatus('Fetching the alliance…');
        try {
            const data = await api('POST', '/api/external-alliances/report', { lastrank_id: raw });
            alliance = data;
            rows = (data.members || []).map(m => ({ ...m, _ext: null, _s_name: window.foldSearch(m.name) }));
            hasExtended = false;
            syncExtendedFilterVisibility();
            renderSummary();
            // LastRank only knows players it has actually seen, so members[] is often
            // empty for an alliance nobody has looked up. Hiding the table (rather than
            // showing an empty one) keeps that from reading as a broken page — the
            // explanation goes in the summary, where the eye already is.
            resultsEl.style.display = rows.length ? 'block' : 'none';
            applyFilters();
            announceSave(data.registry);
            setStatus('');
            if (extendedBox.checked) await runExtended();
        } catch (e) {
            setStatus('');
            showToast(e.message || 'Could not build that report.', 'error');
        } finally {
            running = false;
            runBtn.disabled = false;
        }
    }

    function announceSave(reg) {
        if (!reg) return;
        if (!rows.length) {
            // The saved-stats toast would bury the thing the officer actually needs to
            // know, which is why the table is empty.
            showToast('LastRank has no member records for this alliance — see the note above.', 'info');
            return;
        }
        if (reg.is_own) {
            showToast('That is your own alliance — recorded in your own stats history.', 'info');
        } else if (!reg.in_registry) {
            showToast('Report ready. This alliance is not in your registry, so nothing was saved.', 'info');
        } else if (reg.stats_applied || reg.history_added) {
            showToast('Report ready — registry stats refreshed.');
        } else {
            showToast('Report ready. Registry already up to date.');
        }
    }

    // ---- extended pass ----
    // One request per member, sequential. A failure marks that row and the loop CONTINUES:
    // one unreachable player must never sink the whole run.
    async function runExtended() {
        const targets = rows.filter(r => r.public_id > 0);
        if (!targets.length) { showToast('No members carry a LastRank id to enrich.', 'info'); return; }

        running = true;
        abortFlag = false;
        runBtn.disabled = true;
        stopBtn.style.display = '';
        window.addEventListener('beforeunload', warnOnLeave);

        const prog = buildProgress(targets);
        let done = 0, failed = 0, refreshed = 0;
        for (let i = 0; i < targets.length; i++) {
            if (abortFlag) break;
            const r = targets[i];
            prog.mark(i, 'active', 'fetching…');
            // A stale record triggers a live re-pull upstream, which can take ~20s. Say that
            // up front on the row rather than leaving a stalled-looking "fetching…".
            setStatus(`Fetching ${i + 1} of ${targets.length}…`
                + (refreshed ? ` (${refreshed} needed a live refresh)` : ''));
            try {
                inFlight = new AbortController();
                const p = await api('GET', '/api/external-alliances/report/player?public_id=' + encodeURIComponent(r.public_id),
                    undefined, inFlight.signal);
                r._ext = p;
                // The player record is fresher than the roster snapshot for these three.
                if (p.power) r.power = p.power;
                if (p.hero_power != null) r.hero_power = p.hero_power;
                if (p.base_level != null) r.base_level = p.base_level;
                if (!hasExtended) { hasExtended = true; syncExtendedFilterVisibility(); }
                done++;
                if (p.enrich_status === 'fetched') refreshed++;
                prog.mark(i, 'done', ENRICH_LABEL[p.enrich_status] || 'done');
            } catch (e) {
                if (abortFlag) { prog.mark(i, 'skip', 'stopped'); break; }
                failed++;
                prog.mark(i, 'err', 'failed');
            } finally {
                inFlight = null;
            }
            applyFilters();
            renderSummary();
        }

        window.removeEventListener('beforeunload', warnOnLeave);
        stopBtn.style.display = 'none';
        running = false;
        runBtn.disabled = false;
        setStatus('');
        const refreshNote = refreshed ? ` ${refreshed} were refreshed live from the game.` : '';
        if (abortFlag) showToast(`Stopped after ${done} of ${targets.length}.${refreshNote}`, 'info');
        else if (failed) showToast(`Extended report complete — ${done} fetched, ${failed} unavailable.${refreshNote}`);
        else showToast(`Extended report complete across ${done} member(s).${refreshNote}`);
    }

    function warnOnLeave(e) { e.preventDefault(); e.returnValue = ''; }

    // Reuses the consolidated .job-progress / .job-prog-* classes rather than inventing a
    // fourth progress mechanism — see job-progress.css.
    function buildProgress(targets) {
        progressEl.replaceChildren();
        progressEl.style.display = 'block';
        const entries = targets.map(t => {
            const status = el('span', { className: 'job-prog-status', text: 'queued' });
            const row = el('div', { className: 'job-prog-row queued' },
                el('span', { className: 'job-prog-name', text: t.name }), status);
            progressEl.appendChild(row);
            return { row, status };
        });
        return {
            mark(i, state, text) {
                const e = entries[i];
                if (!e) return;
                e.row.className = 'job-prog-row ' + state;
                e.status.textContent = text;
                if (state === 'active') e.row.scrollIntoView({ block: 'nearest' });
            },
        };
    }

    stopBtn.addEventListener('click', () => {
        abortFlag = true;
        if (inFlight) inFlight.abort();
        setStatus('Stopping…');
    });

    // ---- summary tiles ----
    function renderSummary() {
        if (!alliance) return;
        const powers = rows.map(r => r.power || 0);
        const total = powers.reduce((a, b) => a + b, 0);
        const avg = powers.length ? Math.round(total / powers.length) : 0;
        const hqs = rows.map(r => r.base_level).filter(v => v != null);
        const kills = rows.map(r => r._ext && r._ext.kills).filter(v => v != null);

        const tiles = [
            ['Members', alliance.member_count + (alliance.max_member ? ' / ' + alliance.max_member : '')],
            ['Alliance power', fmtBig(alliance.power)],
            ['Alliance kills', fmtBig(alliance.kills)],
            ['Roster power', fmtBig(total)],
            ['Average power', fmtBig(avg)],
            ['Top power', fmtBig(powers.length ? Math.max(...powers) : null)],
            ['Highest HQ', hqs.length ? Math.max(...hqs) : '—'],
            ['Last seen', dateOnly(alliance.last_seen_at)],
        ];
        if (kills.length) tiles.push(['Roster kills', fmtBig(kills.reduce((a, b) => a + b, 0))]);

        const header = el('div', { className: 'rep-alliance-head' },
            el('h3', { text: (alliance.tag ? '[' + alliance.tag + '] ' : '') + (alliance.name || 'Unknown alliance') }),
            el('span', { className: 'rep-alliance-meta', text: 'Server ' + (alliance.server_id || '—') }),
            alliance.lastrank_id
                ? el('a', {
                    className: 'rep-alliance-link',
                    href: 'https://lastrank.fun/a/' + alliance.lastrank_id,
                    target: '_blank', rel: 'noopener noreferrer',
                    title: 'View on LastRank',
                }, '↗')
                : null);

        const grid = el('div', { className: 'rep-tiles' },
            tiles.map(([label, value]) => el('div', { className: 'card rep-tile' },
                el('span', { className: 'rep-tile-label', text: label }),
                el('span', { className: 'rep-tile-value', text: String(value) }))));

        summaryEl.replaceChildren(header, grid, coverageNote());
        summaryEl.style.display = 'block';
    }

    // LastRank's members[] only contains players it holds a record for, which is a
    // function of who has been looked up there — not of the alliance's real size. For a
    // never-scouted opponent it is routinely empty even though the alliance-level power
    // and kills are perfectly good. Say that plainly: an unexplained empty table reads as
    // a bug, and an officer would waste time retrying it.
    function coverageNote() {
        const known = rows.length;
        const actual = alliance.member_count || 0;
        const note = el('p', { className: 'rep-coverage' });

        if (!known) {
            note.classList.add('rep-coverage-empty');
            note.append(
                el('strong', { text: 'LastRank has no member records for this alliance. ' }),
                'Its alliance-level power, kills and member count above are current, but the '
                + 'per-member breakdown only covers players LastRank has already seen — so an '
                + 'alliance nobody has looked up there yet returns none. Nothing is wrong with '
                + 'the lookup, and retrying will not change it.');
            return note;
        }
        if (actual && known < actual) {
            note.textContent = `Showing ${known} of about ${actual} members — LastRank only holds `
                + `records for players it has already seen, so the rest are missing upstream.`;
            return note;
        }
        note.textContent = `Showing ${known} member${known === 1 ? '' : 's'} known to LastRank.`;
        return note;
    }

    // ---- filter + sort + render ----
    const activeChips = (sel, attr) =>
        Array.from(document.querySelectorAll(sel + '.active')).map(c => c.dataset[attr]);

    function hqBand(v) {
        if (v == null) return 'unknown';
        if (v >= 30) return '30plus';
        if (v >= 25) return '25-29';
        return 'under25';
    }

    function killsBand(k) {
        if (k == null) return null;
        if (k >= 1e6) return '1m';
        if (k >= 1e5) return '100k';
        return 'low';
    }

    // Native vs transferred-in. src_server_id 0 means "no recorded transfer", so it reads
    // as native rather than as a migration from server zero.
    function originBand(e) {
        if (!e || !e.home_server_id) return null;
        if (!e.src_server_id || e.src_server_id === e.home_server_id) return 'native';
        return 'transfer';
    }

    function applyFilters() {
        const q = window.foldSearch((document.getElementById('rep-search') || {}).value || '');
        const ranks = activeChips('.rep-rank-chip', 'rank');
        const hqs = activeChips('.rep-hq-chip', 'hq');
        const profs = activeChips('.rep-prof-chip', 'prof');
        const kills = activeChips('.rep-kills-chip', 'kills');
        const origins = activeChips('.rep-origin-chip', 'origin');

        filtered = rows.filter(r => {
            if (q && !r._s_name.includes(q)) return false;
            if (!ranks.includes('all')) {
                const key = (r.alliance_rank >= 1 && r.alliance_rank <= 5) ? 'R' + r.alliance_rank : 'none';
                if (!ranks.includes(key)) return false;
            }
            if (!hqs.includes('all') && !hqs.includes(hqBand(r.base_level))) return false;

            // Extended filters act on data only fetched rows have. A row still awaiting its
            // extended lookup genuinely doesn't match "Engineer", so it drops out — which is
            // why these chips stay hidden until a run has supplied the data.
            const e = r._ext;
            if (!profs.includes('all')) {
                if (!e) return false;
                if (!profs.includes(e.profession || 'unknown')) return false;
            }
            if (!kills.includes('all')) {
                const band = killsBand(e && e.kills);
                if (!band || !kills.includes(band)) return false;
            }
            if (!origins.includes('all')) {
                const band = originBand(e);
                if (!band || !origins.includes(band)) return false;
            }
            return true;
        });

        filtered.sort((a, b) => {
            const diff = compare(a, b, sortState.field);
            return sortState.dir === 'asc' ? diff : -diff;
        });

        render();
        FilterPanel.updateActiveBadge(CHIP_GROUPS,
            { badgeId: 'rep-filter-count', clearId: 'rep-clear-filters', extra: q ? 1 : 0 });
    }

    const CHIP_GROUPS = [
        ['.rep-rank-chip', 'rank'],
        ['.rep-hq-chip', 'hq'],
        ['.rep-prof-chip', 'prof'],
        ['.rep-kills-chip', 'kills'],
        ['.rep-origin-chip', 'origin'],
    ];

    // Extended filters appear the moment the first extended row lands, so the controls
    // arrive with the columns they act on.
    function syncExtendedFilterVisibility() {
        document.querySelectorAll('.rep-ext-row').forEach(row => {
            row.style.display = hasExtended ? 'flex' : 'none';
        });
        if (!hasExtended) {
            // Reset them on the way out, or a stale extended filter would silently hide
            // rows in the next (basic) report with no visible chip to explain why.
            FilterPanel.clearChipGroups(CHIP_GROUPS.slice(2));
        }
    }

    function compare(a, b, field) {
        const num = (x, y) => (x == null ? -Infinity : x) - (y == null ? -Infinity : y);
        switch (field) {
            case 'name':    return a.name.localeCompare(b.name);
            case 'rank':    return num(a.alliance_rank, b.alliance_rank) || a.name.localeCompare(b.name);
            case 'hero':    return num(a.hero_power, b.hero_power) || a.name.localeCompare(b.name);
            case 'hq':      return num(a.base_level, b.base_level) || a.name.localeCompare(b.name);
            case 'country': return String(a.country || '').localeCompare(String(b.country || ''));
            case 'kills':   return num(a._ext && a._ext.kills, b._ext && b._ext.kills) || a.name.localeCompare(b.name);
            case 'prof':    return String((a._ext || {}).profession || '').localeCompare(String((b._ext || {}).profession || ''));
            case 'career':  return num((a._ext || {}).career_level, (b._ext || {}).career_level) || a.name.localeCompare(b.name);
            case 'seen':    return String((a._ext || {}).last_seen_at || '').localeCompare(String((b._ext || {}).last_seen_at || ''));
            case 'source':  return num((a._ext || {}).src_server_id, (b._ext || {}).src_server_id) || a.name.localeCompare(b.name);
            default:        return num(a.power, b.power) || a.name.localeCompare(b.name);
        }
    }

    // Columns are added and removed as real nodes rather than hidden with CSS: the shared
    // export helper reads thead/tbody cells BY INDEX, so a hidden column would still land
    // in the CSV, shifted against its header.
    // Member leads: on mobile the first column is frozen while the rest scrolls (styles.css),
    // so it has to be the thing that identifies the row. A position number there would pin a
    // meaningless digit and push the name off-screen — and the table sorts by power anyway,
    // which is the only thing that number was telling you.
    const BASE_COLS = [
        { key: 'name',    label: 'Member' },
        { key: 'rank',    label: 'Rank' },
        { key: 'power',   label: 'Power' },
        { key: 'hero',    label: 'Hero Power' },
        { key: 'hq',      label: 'HQ' },
        { key: 'country', label: 'Country' },
    ];
    const EXT_COLS = [
        { key: 'kills',  label: 'Kills' },
        { key: 'prof',   label: 'Profession' },
        { key: 'career', label: 'Career Lv' },
        // NOT "Last Active" — last_seen_at is when LastRank scanned this player from the
        // game, i.e. how fresh the row is. Members of one alliance are scanned together,
        // so their timestamps cluster within seconds; reading it as activity would invite
        // writing off a live player on the strength of scan scheduling.
        { key: 'seen',   label: 'Scanned', title: 'When LastRank last scanned this player from the game — how fresh this row is, not the player’s last login' },
        { key: 'source', label: 'Source Server', title: 'Server this player originally came from' },
    ];

    function render() {
        const cols = hasExtended ? BASE_COLS.concat(EXT_COLS) : BASE_COLS;

        const headRow = el('tr', {});
        cols.forEach(c => {
            const th = el('th', { 'data-sort': c.key, text: c.label, title: c.title });
            if (sortState.field === c.key) th.className = sortState.dir === 'asc' ? 'sort-asc' : 'sort-desc';
            headRow.appendChild(th);
        });
        theadEl.replaceChildren(headRow);

        tbodyEl.replaceChildren();
        if (!filtered.length) {
            tbodyEl.appendChild(el('tr', {}, el('td', {
                colspan: String(cols.length),
                style: 'text-align:center;padding:20px;color:var(--text-muted);',
                text: rows.length ? 'No members match.' : 'No members returned.',
            })));
        } else {
            filtered.forEach(r => {
                const e = r._ext;
                const nameCell = el('td', {});
                if (r.public_id > 0) {
                    nameCell.appendChild(el('a', {
                        href: 'https://lastrank.fun/p/' + r.public_id,
                        target: '_blank', rel: 'noopener noreferrer',
                        className: 'rep-player-link',
                    }, r.name));
                } else {
                    nameCell.textContent = r.name;
                }

                const cells = [
                    nameCell,
                    el('td', { text: rankLabel(r.alliance_rank) }),
                    el('td', { className: 'rep-num', text: fmtBig(r.power) }),
                    el('td', { className: 'rep-num', text: fmtBig(r.hero_power) }),
                    el('td', { className: 'rep-num', text: r.base_level != null ? String(r.base_level) : '—' }),
                    el('td', { text: r.country || '—' }),
                ];
                if (hasExtended) {
                    cells.push(
                        el('td', { className: 'rep-num', text: e ? fmtBig(e.kills) : '—' }),
                        el('td', { text: (e && e.profession) || '—' }),
                        el('td', { className: 'rep-num', text: e && e.career_level ? String(e.career_level) : '—' }),
                        el('td', { className: 'rep-num', text: e ? dateOnly(e.last_seen_at) : '—' }),
                        el('td', { className: 'rep-num', text: e && e.src_server_id ? String(e.src_server_id) : '—' }));
                }
                tbodyEl.appendChild(el('tr', {}, cells));
            });
        }

        countEl.textContent = rows.length ? filtered.length + ' of ' + rows.length : '';
        // The export buttons are auto-wired at page load, long before a report exists.
        // Leaving them live would hand back a header-only file.
        const exportable = filtered.length > 0;
        document.querySelectorAll('.table-export-actions button').forEach(b => { b.disabled = !exportable; });
        if (exportHint) {
            exportHint.textContent = exportable
                ? 'Exports the ' + filtered.length + ' row(s) shown.'
                : '';
        }
    }

    theadEl.addEventListener('click', e => {
        const th = e.target.closest('th[data-sort]');
        if (!th) return;
        const field = th.dataset.sort;
        if (sortState.field === field) {
            sortState.dir = sortState.dir === 'asc' ? 'desc' : 'asc';
        } else {
            sortState.field = field;
            sortState.dir = (field === 'name' || field === 'prof' || field === 'country') ? 'asc' : 'desc';
        }
        applyFilters();
    });

    // ---- wiring ----
    runBtn.addEventListener('click', () => runReport());
    input.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); runReport(); } });

    FilterPanel.setupSearch('rep-search', 'rep-clear-search', applyFilters);
    CHIP_GROUPS.forEach(([sel, attr]) => FilterPanel.setupChipGroup(sel, attr, applyFilters));
    FilterPanel.setupToggle({
        toggleId: 'rep-toggle-filters',
        panelId: 'rep-filter-collapse',
        clearId: 'rep-clear-filters',
        onClear: () => {
            FilterPanel.clearChipGroups(CHIP_GROUPS);
            const s = document.getElementById('rep-search');
            if (s) { s.value = ''; s.dispatchEvent(new Event('input')); }
            applyFilters();
        },
    });

    loadRegistry();
})();
