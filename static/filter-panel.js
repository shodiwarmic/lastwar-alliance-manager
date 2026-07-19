// filter-panel.js — shared search / sort / filter-chip plumbing for list pages.
//
// Files consumes this today; Members and External Alliances still carry their own
// local copies and are tracked to migrate here (see the filter-panel dedup follow-up).
// The page owns its data and its apply/sort function; this module owns the generic
// bits: chip-group toggle logic, the collapsible panel, the active-filter badge, the
// Clear button, the search box, and the sort-chip direction toggle. Every helper runs
// the page's own logic through an onChange callback.
//
// Loaded ONLY on pages with a filter panel (not global.js, which loads everywhere).

(function () {
    'use strict';

    // Mutually-exclusive "All" chip + multi-select others; deselecting everything
    // snaps back to "All". Fires onChange after every click.
    function setupChipGroup(selector, attr, onChange) {
        const chips = document.querySelectorAll(selector);
        chips.forEach(chip => {
            chip.addEventListener('click', () => {
                const val = chip.dataset[attr];
                if (val === 'all') {
                    chips.forEach(c => c.classList.remove('active'));
                    chip.classList.add('active');
                } else {
                    const allChip = document.querySelector(`${selector}[data-${attr}="all"]`);
                    if (allChip) allChip.classList.remove('active');
                    chip.classList.toggle('active');
                    if (document.querySelectorAll(`${selector}.active`).length === 0 && allChip) {
                        allChip.classList.add('active');
                    }
                }
                if (onChange) onChange();
            });
        });
    }

    // Wire the collapsible panel + the Clear button. Panel starts collapsed.
    function setupToggle(opts) {
        opts = opts || {};
        const toggleId = opts.toggleId || 'toggle-filters';
        const panelId = opts.panelId || 'filter-collapse';
        const clearId = opts.clearId || 'clear-filters';
        const toggle = document.getElementById(toggleId);
        const panel = document.getElementById(panelId);
        if (toggle && panel) {
            const setOpen = (open) => {
                panel.classList.toggle('open', open);
                toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
            };
            setOpen(false);
            toggle.addEventListener('click', () => setOpen(!panel.classList.contains('open')));
        }
        const clearBtn = document.getElementById(clearId);
        if (clearBtn && opts.onClear) clearBtn.addEventListener('click', opts.onClear);
    }

    // Count chip groups narrowed off "All"; drive the badge + Clear enabled state.
    // groups = [[selector, attr], ...]. opts.extra adds non-chip filters (e.g. search).
    function updateActiveBadge(groups, opts) {
        opts = opts || {};
        const badgeId = opts.badgeId || 'active-filter-count';
        const clearId = opts.clearId || 'clear-filters';
        let count = groups.reduce((n, group) => {
            const [sel, attr] = group;
            const active = Array.from(document.querySelectorAll(`${sel}.active`));
            return n + (active.length > 0 && !active.some(c => c.dataset[attr] === 'all') ? 1 : 0);
        }, 0);
        count += (opts.extra || 0);
        const badge = document.getElementById(badgeId);
        if (badge) { badge.textContent = String(count); badge.hidden = count === 0; }
        const clearBtn = document.getElementById(clearId);
        if (clearBtn) clearBtn.disabled = count === 0;
    }

    // Reset every chip group to its "All" chip. Leaves sort and search alone.
    function clearChipGroups(groups) {
        groups.forEach(group => {
            const [sel, attr] = group;
            document.querySelectorAll(sel).forEach(c => {
                c.classList.toggle('active', c.dataset[attr] === 'all');
            });
        });
    }

    // Wire a search input + its clear (×) button. onChange fires on input and clear.
    function setupSearch(inputId, clearBtnId, onChange) {
        const input = document.getElementById(inputId);
        const clearBtn = document.getElementById(clearBtnId);
        if (!input) return;
        input.addEventListener('input', () => {
            if (clearBtn) clearBtn.style.display = input.value ? 'flex' : 'none';
            if (onChange) onChange();
        });
        if (clearBtn) {
            clearBtn.addEventListener('click', () => {
                input.value = '';
                clearBtn.style.display = 'none';
                if (onChange) onChange();
                input.focus();
            });
        }
    }

    // Sort chips with an asc/desc direction toggle on re-click. state = {field, dir};
    // labels maps field->label; defaults maps field->'asc'|'desc'. Renders arrows and
    // returns its render fn so the caller can re-render after a programmatic reset.
    function setupSortChips(selector, state, labels, defaults, onChange) {
        function render() {
            document.querySelectorAll(selector).forEach(btn => {
                const field = btn.dataset.sort;
                const isActive = field === state.field;
                btn.classList.toggle('active', isActive);
                const base = labels[field] || field;
                btn.textContent = base + (isActive ? (state.dir === 'asc' ? ' ↑' : ' ↓') : '');
            });
        }
        document.querySelectorAll(selector).forEach(btn => {
            btn.addEventListener('click', () => {
                const field = btn.dataset.sort;
                if (state.field === field) {
                    state.dir = state.dir === 'asc' ? 'desc' : 'asc';
                } else {
                    state.field = field;
                    state.dir = defaults[field] || 'asc';
                }
                render();
                if (onChange) onChange();
            });
        });
        render();
        return render;
    }

    window.FilterPanel = {
        setupChipGroup, setupToggle, updateActiveBadge,
        clearChipGroups, setupSearch, setupSortChips,
    };
})();
