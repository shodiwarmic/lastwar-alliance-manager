// tabs.js — shared tab-bar switching with optional URL-hash persistence.
//
// For pages using the dominant convention: a `.tab-bar` of `.tab-btn[data-tab]`
// buttons and matching `#tab-<name>` `.tab-content` panels, toggled via
// `style.display` (the CLAUDE.md canonical mechanism). EVERY tabbed page uses it:
// accountability, accountability_profile, admin, allies, comms, external-alliances,
// rankings, recruiting, schedule, season-hub, storm, train and vs (vs-league.js).
//
// show() also keeps `.active` on the panels in step with the inline display, so a page
// that paints its first panel `active` server-side (vs.html, for a first paint before JS)
// stays consistent; for every other page it's redundant with .tab-content.active.
//
// onActivate fires for the INITIAL tab too, synchronously inside init() — so anything
// it touches must already be declared (a `let`/`const` lower down throws a TDZ error).
//
// Loaded ONLY on pages that opt in (not global.js, which loads everywhere).

(function () {
    'use strict';

    // Tabs.init(opts) → { show }
    //   opts.barSelector  default '.tab-bar'  (scopes the .tab-btn query)
    //   opts.defaultTab   fallback tab name    (else the first button's data-tab)
    //   opts.hash         bool: persist to / read from location.hash
    //   opts.onActivate   optional (name) => void, called after each activation
    function init(opts) {
        opts = opts || {};
        const barSel = opts.barSelector || '.tab-bar';

        const firstTabName = () => {
            const first = document.querySelector(`${barSel} .tab-btn`);
            return first ? first.dataset.tab : '';
        };
        // defaultTab only if its button exists: a permission-gated default would otherwise
        // leave no tab shown at all.
        const fallback = () => (opts.defaultTab
            && document.querySelector(`${barSel} .tab-btn[data-tab="${opts.defaultTab}"]`))
            ? opts.defaultTab : firstTabName();

        function show(name) {
            // `name` may come from an untrusted URL hash — reject anything that isn't a
            // bare tab token BEFORE it reaches querySelector (a stray `"` / `]` / `\`
            // would throw a SyntaxError). This is the guard vs-league kept via whitelisting.
            if (!/^[\w-]+$/.test(name || '')) name = fallback();

            const btn = document.querySelector(`${barSel} .tab-btn[data-tab="${name}"]`);
            const valid = btn ? name : fallback();   // permission guard: missing tab → default

            document.querySelectorAll(`${barSel} .tab-btn`).forEach(b =>
                b.classList.toggle('active', b.dataset.tab === valid));
            document.querySelectorAll('.tab-content').forEach(c => {
                c.style.display = 'none';
                c.classList.remove('active');
            });
            const panel = document.getElementById('tab-' + valid);
            if (panel) { panel.style.display = 'block'; panel.classList.add('active'); }

            if (opts.hash && ('#' + valid) !== location.hash) {
                history.replaceState(null, '', '#' + valid);
            }
            if (opts.onActivate) opts.onActivate(valid);
        }

        document.querySelectorAll(`${barSel} .tab-btn`).forEach(b =>
            b.addEventListener('click', () => show(b.dataset.tab)));

        // A malformed escape (`#%E0`) makes decodeURIComponent throw a URIError, which
        // during init would leave the page with no tab shown — fall back instead.
        const hashName = () => {
            try {
                return decodeURIComponent((location.hash || '').slice(1));
            } catch (err) {
                console.warn('tabs.js: unreadable hash, showing the default tab', err);
                return fallback();
            }
        };
        show(opts.hash && location.hash ? hashName() : fallback());

        if (opts.hash) {
            window.addEventListener('hashchange', () => show(hashName()));
        }

        return { show };
    }

    window.Tabs = { init };
})();
