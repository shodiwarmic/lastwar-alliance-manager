// order-buttons.js — up/down reorder controls for user-ordered lists (strike types,
// settings lists, reward tiers).
//
// Pure DOM, no external dependency — deliberately not beside the colour picker, which
// needs Choices: a list can be reorderable on a page that has no Choices at all.
// An opt-in module, not global.js: three pages use it (CLAUDE.md -> "What belongs in
// global.js"). Officer Command builds its own buttons on the same classes, so it links
// order-buttons.css only.

// Up/down reorder buttons for a table row in a user-ordered list. Moves the row
// among its siblings and calls onMove() so the caller can persist the new order.
// Returns the container; call refreshOrderButtons(tbody) after any structural
// change so the first/last rows disable the direction they can't go.
function buildOrderButtons(tr, onMove) {
    const wrap = document.createElement('span');
    wrap.className = 'order-btns';

    const mk = (icon, label, move) => {
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'order-btn';
        b.title = label;
        b.setAttribute('aria-label', label);
        b.appendChild(svgIcon(icon, 12));
        b.addEventListener('click', () => {
            const parent = tr.parentNode;
            if (!parent) return;
            move(parent);
            refreshOrderButtons(parent);
            if (onMove) onMove();
        });
        return b;
    };

    wrap.appendChild(mk('chevron-up', 'Move up', parent => {
        const prev = tr.previousElementSibling;
        if (prev) parent.insertBefore(tr, prev);
    }));
    wrap.appendChild(mk('chevron-down', 'Move down', parent => {
        const next = tr.nextElementSibling;
        if (next) parent.insertBefore(next, tr);
    }));

    return wrap;
}

// Disable the up button on the first row and the down button on the last, so
// the ends of the list are visible rather than silently doing nothing.
function refreshOrderButtons(tbody) {
    const rows = Array.from(tbody.children);
    rows.forEach((row, i) => {
        const btns = row.querySelectorAll('.order-btn');
        if (btns.length < 2) return;
        btns[0].disabled = i === 0;
        btns[1].disabled = i === rows.length - 1;
    });
}

// Index of a row within its tbody — the value a user-ordered list persists as
// sort_order. Not tr.rowIndex, which counts the header rows too.
function rowPosition(tr) {
    return tr.parentNode ? Array.prototype.indexOf.call(tr.parentNode.children, tr) : 0;
}
