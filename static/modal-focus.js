'use strict';

// Focus trap for modals — keeps keyboard navigation inside an open modal.
// Usage:
//   trapFocus(modalEl)    — call when opening a modal
//   releaseFocus(modalEl) — call when closing a modal

const FOCUSABLE_SELECTOR = [
    'a[href]',
    'button:not([disabled])',
    'input:not([disabled])',
    'select:not([disabled])',
    'textarea:not([disabled])',
    '[tabindex]:not([tabindex="-1"])',
].join(', ');

// Map from modal element → its keydown handler, so each modal manages its own trap.
const _traps = new WeakMap();

function trapFocus(modal) {
    // Remove any existing trap on this modal first
    releaseFocus(modal);

    const getFocusable = () => [...modal.querySelectorAll(FOCUSABLE_SELECTOR)]
        .filter(el => !el.closest('[hidden]') && getComputedStyle(el).display !== 'none');

    const handler = e => {
        if (e.key === 'Escape') {
            releaseFocus(modal);
            modal.style.display = '';
            return;
        }
        if (e.key !== 'Tab') return;

        const els = getFocusable();
        if (!els.length) return;

        const first = els[0];
        const last = els[els.length - 1];

        if (e.shiftKey) {
            if (document.activeElement === first) { e.preventDefault(); last.focus(); }
        } else {
            if (document.activeElement === last)  { e.preventDefault(); first.focus(); }
        }
    };

    _traps.set(modal, handler);
    modal.addEventListener('keydown', handler);

    // Move focus inside the modal, skipping Flatpickr date inputs (focusing them
    // triggers the calendar to open immediately, which is disorienting).
    const els = getFocusable();
    const firstNonDate = els.find(el => !el.classList.contains('flatpickr-input')) || els[0];
    if (firstNonDate) firstNonDate.focus();
}

function releaseFocus(modal) {
    const handler = _traps.get(modal);
    if (handler) {
        modal.removeEventListener('keydown', handler);
        _traps.delete(modal);
    }
}
