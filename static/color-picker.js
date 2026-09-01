// color-picker.js — the shared palette + colour <select> upgrade.
//
// Deliberately NOT in global.js. The upgrade needs Choices.js, which is a
// per-template CDN script, and global.js is loaded on every page — so a helper
// living there implies an availability it cannot promise. exportTableToXLSX is
// the cautionary tale: it sits in global.js, depends on the per-template SheetJS
// tag, and shipped as a dead button on the Members page because the tag was
// missing. Keeping this beside its dependency makes the requirement visible in
// the template, and build-check.yml fails a page that loads one without the other.
//
// Every entry point still degrades to a plain, fully working <select> if Choices
// is absent, so a missed tag costs the preview, never the feature.
//
// Load AFTER the Choices CDN script:
//   <link rel="stylesheet" href="{{asset "/color-picker.css"}}">
//   <script src="https://cdn.jsdelivr.net/npm/choices.js@10.2/.../choices.min.js"></script>
//   <script src="{{asset "/color-picker.js"}}"></script>

(function () {
    'use strict';

    // The six palette slots every user-configurable colour in the app picks from
    // (file tags, Season Hub reward tiers). Each maps to a --color-*-bg /
    // --color-* token pair, so they follow the theme automatically.
    //
    // Keep in sync with validTierColors in handlers_season_hub.go — those two are
    // the only copies of this list.
    const PALETTE_COLORS = [
        { value: 'neutral', label: 'Neutral' },
        { value: 'info',    label: 'Blue' },
        { value: 'success', label: 'Green' },
        { value: 'warning', label: 'Amber' },
        { value: 'danger',  label: 'Red' },
        { value: 'purple',  label: 'Purple' },
    ];

    // Build a <select> over the palette with `current` preselected.
    function buildColorSelect(current, className) {
        const sel = document.createElement('select');
        sel.className = className || 'form-input';
        PALETTE_COLORS.forEach(c => {
            const opt = document.createElement('option');
            opt.value = c.value;
            opt.textContent = c.label;
            if ((current || 'neutral') === c.value) opt.selected = true;
            sel.appendChild(opt);
        });
        return sel;
    }

    // Upgrade a colour <select> to a Choices dropdown that previews each option.
    // The preview is pure CSS (color-picker.css) keyed on the option value; this
    // only tags the Choices wrapper with .color-choices to scope it. Pass
    // extraClass to pick a preview style — 'tier-color-choices' renders full
    // badges instead of dots.
    //
    // Accepts a selector or an element, and returns null when Choices is absent
    // so callers keep working against the untouched native <select>.
    function makeColorChoices(target, extraClass) {
        if (!window.Choices) return null;
        const el = typeof target === 'string' ? document.querySelector(target) : target;
        if (!el) return null;
        const c = new Choices(el, { searchEnabled: false, shouldSort: false, itemSelectText: '', allowHTML: false });
        const wrap = el.closest('.choices');
        if (wrap) {
            wrap.classList.add('color-choices');
            if (extraClass) wrap.classList.add(extraClass);
        }
        return c;
    }

    // Set a colour dropdown's value through Choices when present, else on the
    // native select. Accepts the select's id or the element itself.
    function setColorValue(choices, select, value) {
        if (choices) {
            choices.setChoiceByValue(value);
            return;
        }
        const el = typeof select === 'string' ? document.getElementById(select) : select;
        if (el) el.value = value;
    }

    // Upgrade every colour <select> matching `selector` inside `root`, stashing
    // each instance on its row so it can be destroyed later — Choices leaves its
    // wrapper behind if the element is removed without destroy(). Safe to call
    // repeatedly; already-upgraded elements are skipped.
    function upgradeAll(root, selector, extraClass) {
        if (!root || !window.Choices) return;
        root.querySelectorAll(selector).forEach(sel => {
            if (sel._colorChoices) return;
            sel._colorChoices = makeColorChoices(sel, extraClass);
        });
    }

    // Tear down the instance attached to a colour <select> before its row is
    // removed. No-op when Choices never ran.
    function destroyIn(container, selector) {
        if (!container) return;
        container.querySelectorAll(selector).forEach(sel => {
            if (!sel._colorChoices) return;
            try { sel._colorChoices.destroy(); } catch (e) { /* already gone */ }
            sel._colorChoices = null;
        });
    }

    window.ColorPicker = {
        PALETTE: PALETTE_COLORS,
        buildSelect: buildColorSelect,
        make: makeColorChoices,
        setValue: setColorValue,
        upgradeAll,
        destroyIn,
    };
})();
