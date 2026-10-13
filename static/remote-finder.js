// remote-finder.js — the shared type-ahead combobox.
//
// One dropdown, two sources: instant local matches as you type, plus a remote
// lookup behind an explicit click. Used by the Scout Report's alliance finder
// (External Alliances) and the Recruiting player picker.
//
// An opt-in module, not global.js: two pages use it (CLAUDE.md -> "What belongs in
// global.js"). Link remote-finder.css first in head_tags and load this before the
// page script; build-check.yml's "Opt-in module dependency check" enforces both.
//
// The remote lookup is NEVER fired per keystroke. Every remote source here is the
// volunteer-run LastRank API behind a shared 1 req/sec limiter, so searching must
// stay a deliberate act — debouncing would still turn one officer typing a name
// into a burst of upstream calls.
//
// Options:
//   inputs        [HTMLElement]  fields that drive the query (last focused wins)
//   localSearch   q => [item]    synchronous, no network
//   remoteSearch  async q => [item]   omit for a local-only finder
//   item          (obj, kind) => { label, meta, onPick }   kind: 'local' | 'remote'
//   guard         q => string|null    non-null blocks the remote lookup with that message
//   actionLabel   q => string
//   position      (field) => void     optional; called on open for custom anchoring
//   prefix        string          CSS class prefix (default 'finder')
//   localHead     string          optional heading over the local matches
//   remoteHead    string          optional heading over the remote results
//   searchingText string          default 'Searching…'
//   emptyText     q => string     shown when the lookup finds nothing (default 'No matches found.')
//   context       () => string    the rest of the lookup's key (e.g. a server number)
//
// Returns { dropdown, refresh, close, runRemote } — insert `dropdown` yourself so
// the caller controls where it sits in the DOM.
//
// Request ownership. A response renders only if it still belongs to what's on screen: the
// same query and context it was asked for, and no close() (Escape, a click outside, a
// field switch) since. So an older answer can't overwrite a newer query, a server change
// discards a pending lookup, and a late answer can't reopen a dismissed dropdown — or one
// whose modal has gone (the dropdown is no longer in the document). While a lookup for the
// same query and context is in flight, Enter and the action do nothing, so repeat presses
// can't spend the 1 req/sec budget twice.
function buildRemoteFinder(opts) {
    const prefix = opts.prefix || 'finder';
    const localSearch = opts.localSearch || (() => []);
    const guard = opts.guard || (() => null);
    const actionLabel = opts.actionLabel || (q => 'Look up “' + q + '” on LastRank');
    const searchingText = opts.searchingText || 'Searching…';
    const emptyText = opts.emptyText || (() => 'No matches found.');

    const dropdown = document.createElement('div');
    dropdown.className = prefix + '-dropdown';
    dropdown.hidden = true;

    let activeField = opts.inputs[0] || null;
    const queryVal = () => (activeField ? activeField.value : '').trim();
    const lookupKey = () => queryVal() + '\u0000' + (opts.context ? String(opts.context() || '') : '');
    let generation = 0;     // bumped by close(): anything asked before it is stale
    let pending = null;     // lookupKey() of the lookup in flight
    let searching = null;   // lookupKey() whose "Searching…" line is on screen

    const onDocDown = e => {
        if (dropdown.contains(e.target)) return;
        if (opts.inputs.some(f => f === e.target || f.contains?.(e.target))) return;
        close();
    };
    function open() {
        if (!dropdown.isConnected) return;   // its modal is gone
        if (opts.position) opts.position(activeField);
        dropdown.hidden = false;
        document.addEventListener('mousedown', onDocDown);
    }
    function close() {
        generation++;
        dropdown.hidden = true;
        document.removeEventListener('mousedown', onDocDown);
    }

    function node(cls, text) {
        const n = document.createElement('div');
        n.className = prefix + '-' + cls;
        n.textContent = text;
        return n;
    }

    function entry(obj, kind) {
        const spec = opts.item(obj, kind);
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = prefix + '-item';
        const nameEl = document.createElement('span');
        nameEl.className = prefix + '-name';
        nameEl.textContent = spec.label;
        noTranslate(nameEl);   // a player/alliance name, not prose
        btn.appendChild(nameEl);
        if (spec.meta) {
            const metaEl = document.createElement('span');
            metaEl.className = prefix + '-meta';
            metaEl.textContent = spec.meta;
            btn.appendChild(metaEl);
        }
        btn.addEventListener('click', () => { close(); spec.onPick(obj); });
        return btn;
    }

    function render(locals, remotes, msg, isError, showAction) {
        searching = null;
        dropdown.replaceChildren();
        if (locals && locals.length) {
            if (opts.localHead) dropdown.appendChild(node('head', opts.localHead));
            locals.forEach(x => dropdown.appendChild(entry(x, 'local')));
        }
        if (remotes && remotes.length) {
            if (opts.remoteHead) dropdown.appendChild(node('head', opts.remoteHead));
            remotes.forEach(x => dropdown.appendChild(entry(x, 'remote')));
        }
        if (msg) {
            const m = node('msg', msg);
            if (isError) m.classList.add(prefix + '-err');
            dropdown.appendChild(m);
        }
        if (showAction && opts.remoteSearch) {
            const act = document.createElement('button');
            act.type = 'button';
            act.className = prefix + '-action';
            act.textContent = '🔎 ' + actionLabel(queryVal());
            act.disabled = pending === lookupKey();
            act.addEventListener('click', runRemote);
            dropdown.appendChild(act);
        }
        if (dropdown.childNodes.length) open(); else close();
    }

    async function runRemote() {
        const q = queryVal();
        if (!q || !opts.remoteSearch) return;
        const key = lookupKey();
        if (pending === key) return;   // already asking this exact question
        const blocked = guard(q);
        if (blocked) { render(localSearch(q), null, blocked, true, false); return; }
        const mine = generation;
        pending = key;
        render(localSearch(q), null, searchingText, false, false);
        searching = key;
        const current = () => mine === generation && key === lookupKey();
        let answered = false;
        try {
            const list = await opts.remoteSearch(q);
            if (current()) { answered = true; render(localSearch(q), list, (list && list.length) ? null : emptyText(q), false, false); }
        } catch (e) {
            if (current()) { answered = true; render(localSearch(q), null, e.message || 'Search failed.', true, false); }
        } finally {
            if (pending === key) pending = null;
            // Discarded (the context changed under it) while still open: drop the "Searching…"
            // line and offer the lookup again for what's on screen now.
            if (!answered && searching === key && !dropdown.hidden) refresh();
        }
    }

    function refresh() {
        const q = queryVal();
        if (!q) { close(); return; }
        render(localSearch(q), null, null, false, true);
    }

    opts.inputs.forEach(f => {
        f.addEventListener('focus', () => {
            if (activeField !== f) { activeField = f; generation++; }
        });
        f.addEventListener('input', () => { activeField = f; refresh(); });
        f.addEventListener('keydown', e => {
            if (e.key === 'Escape') close();
            else if (e.key === 'Enter' && !dropdown.hidden) { e.preventDefault(); runRemote(); }
        });
    });

    return { dropdown, refresh, close, runRemote };
}
