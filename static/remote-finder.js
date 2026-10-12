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
//   inputs       [HTMLElement]  fields that drive the query (last focused wins)
//   localSearch  q => [item]    synchronous, no network
//   remoteSearch async q => [item]   omit for a local-only finder
//   item         (obj, kind) => { label, meta, onPick }   kind: 'local' | 'remote'
//   guard        q => string|null    non-null blocks the remote lookup with that message
//   actionLabel  q => string
//   position     () => void      optional; called on open for custom anchoring
//   prefix       string          CSS class prefix (default 'finder')
//
// Returns { dropdown, refresh, close, runRemote } — insert `dropdown` yourself so
// the caller controls where it sits in the DOM.
function buildRemoteFinder(opts) {
    const prefix = opts.prefix || 'finder';
    const localSearch = opts.localSearch || (() => []);
    const guard = opts.guard || (() => null);
    const actionLabel = opts.actionLabel || (q => 'Look up “' + q + '” on LastRank');

    const dropdown = document.createElement('div');
    dropdown.className = prefix + '-dropdown';
    dropdown.hidden = true;

    let activeField = opts.inputs[0] || null;
    const queryVal = () => (activeField ? activeField.value : '').trim();

    const onDocDown = e => {
        if (dropdown.contains(e.target)) return;
        if (opts.inputs.some(f => f === e.target || f.contains?.(e.target))) return;
        close();
    };
    function open() {
        if (opts.position) opts.position(activeField);
        dropdown.hidden = false;
        document.addEventListener('mousedown', onDocDown);
    }
    function close() {
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
            act.addEventListener('click', runRemote);
            dropdown.appendChild(act);
        }
        if (dropdown.childNodes.length) open(); else close();
    }

    async function runRemote() {
        const q = queryVal();
        if (!q || !opts.remoteSearch) return;
        const blocked = guard(q);
        if (blocked) { render(localSearch(q), null, blocked, true, false); return; }
        render(localSearch(q), null, 'Searching…', false, false);
        try {
            const list = await opts.remoteSearch(q);
            render(localSearch(q), list, (list && list.length) ? null : 'No matches found.', false, false);
        } catch (e) {
            render(localSearch(q), null, e.message || 'Search failed.', true, false);
        }
    }

    function refresh() {
        const q = queryVal();
        if (!q) { close(); return; }
        render(localSearch(q), null, null, false, true);
    }

    opts.inputs.forEach(f => {
        f.addEventListener('focus', () => { activeField = f; });
        f.addEventListener('input', () => { activeField = f; refresh(); });
        f.addEventListener('keydown', e => {
            if (e.key === 'Escape') close();
            else if (e.key === 'Enter' && !dropdown.hidden) { e.preventDefault(); runRemote(); }
        });
    });

    return { dropdown, refresh, close, runRemote };
}
