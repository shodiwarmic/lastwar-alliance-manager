// participation-parse.js — reads a score as the game prints it, for the participation
// recording screen's score inputs: "81.20G", "392.35M", "1,234,567", "25".
// Mirrors parseBoardAmount (handlers_participation.go), which reads the same values
// out of an imported CSV; keep the two accepting the same forms.
//
// It PARSES only. Displaying a compact number goes through the shared formatCompact
// (global.js), whose metric option prints the game's G; there is no second formatter here.
(function (root) {
    'use strict';

    const NUMERIC = /^[\d.,]+[kmgb]?$/i;
    const SUFFIX = { k: 1e3, m: 1e6, g: 1e9, b: 1e9 };

    // "81.20G" → 81200000000; "1,234,567" → 1234567. null if it is not a number.
    function parseAmount(token) {
        token = String(token || '').trim();
        if (!NUMERIC.test(token)) return null;
        const last = token.slice(-1).toLowerCase();
        const mult = SUFFIX[last] || 1;
        const digits = (SUFFIX[last] ? token.slice(0, -1) : token).replace(/,/g, '');
        if (!/^\d*\.?\d+$/.test(digits)) return null;
        const n = Math.round(parseFloat(digits) * mult);
        return Number.isFinite(n) ? n : null;
    }

    const api = { parseAmount };
    if (typeof module !== 'undefined' && module.exports) module.exports = api;
    else root.ParticipationParse = api;
})(typeof window !== 'undefined' ? window : this);
