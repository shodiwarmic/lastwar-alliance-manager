// tabs.js — shared tab-bar switching with optional URL-hash persistence.
//
// For pages using the dominant convention: a `.tab-bar` of `.tab-btn[data-tab]`
// buttons and matching `#tab-<name>` `.tab-content` panels, toggled via
// `style.display` (the CLAUDE.md canonical mechanism). Train consumes this today;
// the other data-tab pages (accountability, schedule, accountability_profile,
// season-hub, recruiting, allies, comms) still carry local copies and are tracked
// to migrate here.
//
// Provenance: the hash / permission-guard / replaceState logic is generalized from
// vs-league.js showTab; the visibility mechanic is train's setupTabs (inline
// style.display). vs-league/storm instead drive visibility via an `.active` class and
// carry lazy-load side effects (initLeague/renderAllTime) → those map to onActivate
// and migrate separately.
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
        const fallback = () => opts.defaultTab || firstTabName();

        function show(name) {
            // `name` may come from an untrusted URL hash — reject anything that isn't a
            // bare tab token BEFORE it reaches querySelector (a stray `"` / `]` / `\`
            // would throw a SyntaxError). This is the guard vs-league kept via whitelisting.
            if (!/^[\w-]+$/.test(name || '')) name = fallback();

            const btn = document.querySelector(`${barSel} .tab-btn[data-tab="${name}"]`);
            const valid = btn ? name : fallback();   // permission guard: missing tab → default

            document.querySelectorAll(`${barSel} .tab-btn`).forEach(b =>
                b.classList.toggle('active', b.dataset.tab === valid));
            document.querySelectorAll('.tab-content').forEach(c => { c.style.display = 'none'; });
            const panel = document.getElementById('tab-' + valid);
            if (panel) panel.style.display = 'block';

            if (opts.hash && ('#' + valid) !== location.hash) {
                history.replaceState(null, '', '#' + valid);
            }
            if (opts.onActivate) opts.onActivate(valid);
        }

        document.querySelectorAll(`${barSel} .tab-btn`).forEach(b =>
            b.addEventListener('click', () => show(b.dataset.tab)));

        const hashName = () => decodeURIComponent((location.hash || '').slice(1));
        show(opts.hash && location.hash ? hashName() : fallback());

        if (opts.hash) {
            window.addEventListener('hashchange', () => show(hashName()));
        }

        return { show };
    }

    window.Tabs = { init };
})();
