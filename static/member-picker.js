// member-picker.js — shared searchable member typeahead.
// One reusable inline picker for Polls (by-option), Officer Command, and Desert
// Storm. The dropdown is position:fixed and anchored to the input, so it escapes
// every clipping ancestor (modal .overflow-y:auto, table .overflow:auto, etc.).
//
//   const picker = createMemberPicker({ getCandidates, isExcluded, onPick, … });
//   container.appendChild(picker.el);
//   // …later, when the container is torn down:
//   picker.destroy();   // idempotent — removes ALL listeners
//
// IMPORTANT: destroy() must be called from every full re-render / teardown path.
// Removing the focused input from the DOM does NOT fire `blur`, so cleanup can
// never be blur-dependent.
(function () {
    'use strict';

    function defaultMatches(m, q) {
        return (m.name || '').toLowerCase().includes(q) ||
               (m.rank || '').toLowerCase().includes(q);
    }
    function defaultSort(a, b) {
        return (a.name || '').localeCompare(b.name || '');
    }
    function defaultRenderRow(m) {
        const frag = document.createDocumentFragment();
        frag.appendChild(document.createTextNode((m.name || '') + ' '));
        const rank = document.createElement('span');
        rank.className = 'member-rank rank-' + (m.rank || '');
        rank.textContent = m.rank || '';
        frag.appendChild(rank);
        return frag;
    }

    // Nearest ancestor that scrolls — used to hide the fixed dropdown when the
    // anchor input is scrolled out of that container's visible box.
    function nearestScrollable(el) {
        let node = el.parentElement;
        while (node && node !== document.body && node !== document.documentElement) {
            const s = getComputedStyle(node);
            if (/(auto|scroll)/.test(s.overflowY) || /(auto|scroll)/.test(s.overflowX)) {
                return node;
            }
            node = node.parentElement;
        }
        return null;
    }

    window.createMemberPicker = function createMemberPicker(opts) {
        opts = opts || {};
        const getCandidates = opts.getCandidates || (() => []);
        const isExcluded = opts.isExcluded || (() => false);
        const onPick = opts.onPick || (() => {});
        const matches = opts.matches || defaultMatches;
        const sort = opts.sort || defaultSort;
        const renderRow = opts.renderRow || defaultRenderRow;
        const maxResults = opts.maxResults || 50;
        const keepOpenOnPick = !!opts.keepOpenOnPick;

        const wrapper = document.createElement('div');
        wrapper.className = 'inline-search';

        const input = document.createElement('input');
        input.type = 'text';
        input.className = 'form-input inline-search-input';
        input.placeholder = opts.placeholder || 'Add member…';
        input.autocomplete = 'off';
        input.setAttribute('role', 'combobox');
        input.setAttribute('aria-autocomplete', 'list');
        input.setAttribute('aria-expanded', 'false');

        const dropdown = document.createElement('div');
        dropdown.className = 'inline-dropdown';
        dropdown.setAttribute('role', 'listbox');
        dropdown.hidden = true;
        const ddId = 'mp-' + Math.random().toString(36).slice(2);
        dropdown.id = ddId;
        input.setAttribute('aria-controls', ddId);

        wrapper.append(input, dropdown);

        let open = false;
        let destroyed = false;
        let rafId = null;
        let rows = [];    // result <div>s (parallel to items)
        let items = [];   // candidate objects
        let activeIndex = -1;

        // ---- fixed positioning (rAF-coalesced) ----
        function scheduleReposition() {
            if (rafId != null) return;
            rafId = requestAnimationFrame(() => { rafId = null; reposition(); });
        }
        function reposition() {
            if (!open) return;
            const r = input.getBoundingClientRect();
            const sc = nearestScrollable(input);
            if (sc) {
                const c = sc.getBoundingClientRect();
                if (r.bottom < c.top || r.top > c.bottom || r.right < c.left || r.left > c.right) {
                    dropdown.style.visibility = 'hidden';
                    return;
                }
            }
            dropdown.style.visibility = '';
            dropdown.style.left = r.left + 'px';
            dropdown.style.top = r.bottom + 'px';
            dropdown.style.width = r.width + 'px';
        }

        // ---- listeners bound only while open ----
        function onDocPointer(e) {
            if (!wrapper.contains(e.target)) close();
        }
        function bindOpen() {
            document.addEventListener('mousedown', onDocPointer, true);
            window.addEventListener('scroll', scheduleReposition, true); // capture: catches inner scrollers
            window.addEventListener('resize', scheduleReposition);
            window.addEventListener('orientationchange', scheduleReposition);
            if (window.visualViewport) {
                window.visualViewport.addEventListener('resize', scheduleReposition);
                window.visualViewport.addEventListener('scroll', scheduleReposition);
            }
        }
        function unbindOpen() {
            document.removeEventListener('mousedown', onDocPointer, true);
            window.removeEventListener('scroll', scheduleReposition, true);
            window.removeEventListener('resize', scheduleReposition);
            window.removeEventListener('orientationchange', scheduleReposition);
            if (window.visualViewport) {
                window.visualViewport.removeEventListener('resize', scheduleReposition);
                window.visualViewport.removeEventListener('scroll', scheduleReposition);
            }
        }

        function renderList() {
            const q = input.value.trim().toLowerCase();
            let list = (getCandidates() || []).filter(m => !isExcluded(m));
            if (q) list = list.filter(m => matches(m, q));
            list.sort(sort);
            const shown = list.slice(0, maxResults);

            dropdown.replaceChildren();
            rows = [];
            items = [];
            setActive(-1);

            if (shown.length === 0) {
                const empty = document.createElement('div');
                empty.className = 'inline-empty';
                empty.textContent = q ? 'No members match' : 'No members available';
                dropdown.appendChild(empty);
                return;
            }
            shown.forEach((m, i) => {
                const row = document.createElement('div');
                row.className = 'inline-result';
                row.setAttribute('role', 'option');
                row.setAttribute('aria-selected', 'false');
                row.id = ddId + '-o' + i;
                row.appendChild(renderRow(m));
                // mousedown+preventDefault keeps the input focused through the pick
                // so a blur-driven teardown can't destroy the row before it fires.
                row.addEventListener('mousedown', (e) => {
                    e.preventDefault();
                    pick(m);
                });
                dropdown.appendChild(row);
                rows.push(row);
                items.push(m);
            });
        }

        function setActive(idx) {
            if (activeIndex >= 0 && rows[activeIndex]) {
                rows[activeIndex].classList.remove('active');
                rows[activeIndex].setAttribute('aria-selected', 'false');
            }
            activeIndex = idx;
            if (activeIndex >= 0 && rows[activeIndex]) {
                const row = rows[activeIndex];
                row.classList.add('active');
                row.setAttribute('aria-selected', 'true');
                input.setAttribute('aria-activedescendant', row.id);
                row.scrollIntoView({ block: 'nearest' });
            } else {
                input.removeAttribute('aria-activedescendant');
            }
        }

        function openDropdown() {
            if (open || destroyed) return;
            open = true;
            dropdown.hidden = false;
            input.setAttribute('aria-expanded', 'true');
            renderList();
            bindOpen();
            reposition();
        }
        function close() {
            if (!open) return;
            open = false;
            dropdown.hidden = true;
            dropdown.style.visibility = '';
            input.setAttribute('aria-expanded', 'false');
            input.removeAttribute('aria-activedescendant');
            unbindOpen();
            dropdown.replaceChildren();
            rows = [];
            items = [];
            activeIndex = -1;
        }

        function pick(m) {
            input.value = '';
            onPick(m); // caller owns state mutation + async persistence + rollback
            if (destroyed) return;
            if (keepOpenOnPick) {
                renderList();
                reposition();
                input.focus();
            } else {
                close();
                input.blur();
            }
        }

        input.addEventListener('focus', openDropdown);
        input.addEventListener('input', () => { open ? renderList() : openDropdown(); });
        input.addEventListener('keydown', (e) => {
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                if (!open) { openDropdown(); return; }
                if (rows.length) setActive((activeIndex + 1) % rows.length);
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                if (open && rows.length) setActive((activeIndex - 1 + rows.length) % rows.length);
            } else if (e.key === 'Enter') {
                if (open && activeIndex >= 0 && items[activeIndex]) {
                    e.preventDefault();
                    pick(items[activeIndex]);
                }
            } else if (e.key === 'Escape') {
                if (open) { e.preventDefault(); close(); input.blur(); }
            }
        });

        function destroy() {
            if (destroyed) return;
            destroyed = true;
            close();
            if (rafId != null) { cancelAnimationFrame(rafId); rafId = null; }
            if (wrapper.parentElement) wrapper.remove();
        }

        return { el: wrapper, input, reposition, destroy };
    };
})();
