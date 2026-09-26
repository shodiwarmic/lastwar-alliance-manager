// participation-parse.js — reads a score as the game prints it, for the participation
// recording screen's score inputs: "81.20G", "392.35M", "1,234,567", "25".
// Mirrors parseBoardAmount (handlers_participation.go), which reads the same values
// out of an imported CSV; keep the two accepting the same forms.
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

    // Compact display: 81200000000 → "81.2G". The raw integer is what is stored.
    function formatAmount(n) {
        if (n == null) return '';
        const abs = Math.abs(n);
        if (abs >= 1e9) return +(n / 1e9).toFixed(2) + 'G';
        if (abs >= 1e6) return +(n / 1e6).toFixed(2) + 'M';
        if (abs >= 1e4) return +(n / 1e3).toFixed(1) + 'K';
        return String(n);
    }

    const api = { parseAmount, formatAmount };
    if (typeof module !== 'undefined' && module.exports) module.exports = api;
    else root.ParticipationParse = api;
})(typeof window !== 'undefined' ? window : this);
