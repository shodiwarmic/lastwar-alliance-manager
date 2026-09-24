// participation-parse.js — turns a ranked list pasted from an event mail into rows
// for the participation recording screen. Its own file so it can be exercised
// without a page (see the fixture in the PR for #13); the page loads it before
// participation.js.
//
// Token-based, not one regex, because the three mails differ in what trails the
// name (decision 23):
//   Alliance Exercise   "1  Pàcha  81.20G"            rank, name, damage
//   Zombie Siege        "4  Boom 붐  12.3M  25"        rank, name, POWER, waves
//   Desert Storm        "2  [PoWr] Skworl  1,234,567"  rank, tagged name, points
// A leading integer is the rank. Every trailing token that looks like a number
// (optional K/M/G/B suffix) is a numeric column; the LAST one on the line is the
// value and any before it (Zombie Siege's power) are dropped. What remains is the
// name, with a leading [TAG] removed. A line with no number is reported as
// unparsed, never silently skipped. The check table is where a leader corrects
// anything this gets wrong.
(function (root) {
    'use strict';

    const NUMERIC = /^[\d.,]+[kmgb]?$/i;
    const RANK = /^#?(\d+)[.)]?$/;
    const TAG = /^\[[^\]]{1,12}\]\s*/;
    const SUFFIX = { k: 1e3, m: 1e6, g: 1e9, b: 1e9 };

    // "81.20G" → 81200000000; "1,234,567" → 1234567. null if it is not a number.
    function parseAmount(token) {
        if (!NUMERIC.test(token)) return null;
        const last = token.slice(-1).toLowerCase();
        const mult = SUFFIX[last] || 1;
        const digits = (SUFFIX[last] ? token.slice(0, -1) : token).replace(/,/g, '');
        if (!/^\d*\.?\d+$/.test(digits)) return null;
        const n = Math.round(parseFloat(digits) * mult);
        return Number.isFinite(n) ? n : null;
    }

    // Returns { rank, name, value } or null when the line has no usable number/name.
    function parseLine(line) {
        const tokens = String(line || '').trim().split(/\s+/).filter(Boolean);
        if (!tokens.length) return null;
        let rank = null;
        const m = RANK.exec(tokens[0]);
        if (m && tokens.length > 1) {
            rank = parseInt(m[1], 10);
            tokens.shift();
        }
        const numbers = [];
        while (tokens.length > 1 && parseAmount(tokens[tokens.length - 1]) !== null) {
            numbers.unshift(parseAmount(tokens.pop()));
        }
        const name = tokens.join(' ').replace(TAG, '').trim();
        if (!numbers.length || !name) return null;
        return { rank, name, value: numbers[numbers.length - 1] };
    }

    // Returns { rows: [{rank, name, value}], unparsed: [line] }.
    function parse(text) {
        const rows = [];
        const unparsed = [];
        String(text || '').split(/\r?\n/).forEach(raw => {
            if (!raw.trim()) return;
            const row = parseLine(raw);
            if (row) rows.push(row);
            else unparsed.push(raw.trim());
        });
        return { rows, unparsed };
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

    const api = { parse, parseLine, parseAmount, formatAmount };
    if (typeof module !== 'undefined' && module.exports) module.exports = api;
    else root.ParticipationParse = api;
})(typeof window !== 'undefined' ? window : this);
