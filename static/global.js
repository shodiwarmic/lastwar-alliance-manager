// static/global.js - Global JavaScript for handling mobile menu, user dropdown, and logout functionality

// ---- Table export (CSV + XLSX) ----

function _extractTableData(tableEl) {
    const skipCols = new Set();
    const rows = [];

    const ths = tableEl.querySelectorAll('thead th');
    const headers = [];
    ths.forEach((th, i) => {
        if ('noExport' in th.dataset) { skipCols.add(i); return; }
        headers.push(th.textContent.trim());
    });
    rows.push(headers);

    tableEl.querySelectorAll('tbody tr').forEach(tr => {
        const tds = tr.querySelectorAll('td');
        if (tds.length === 1 && tds[0].colSpan > 1) return;
        const cells = [];
        tds.forEach((td, i) => {
            if (skipCols.has(i)) return;
            const input = td.querySelector('input, select, textarea');
            let val;
            if (input) {
                val = input.type === 'checkbox' ? (input.checked ? 'Yes' : 'No') : input.value;
            } else {
                val = td.textContent;
            }
            cells.push(val.trim());
        });
        if (cells.length) rows.push(cells);
    });

    return rows;
}

function exportTableToCSV(tableEl, filename) {
    if (typeof tableEl === 'string') tableEl = document.getElementById(tableEl);
    if (!tableEl) return;

    const rows = _extractTableData(tableEl);
    const csv = '﻿' + rows.map(row =>
        row.map(val => {
            const s = String(val ?? '');
            return (s.includes(',') || s.includes('"') || s.includes('\n') || s.includes('\r'))
                ? '"' + s.replace(/"/g, '""') + '"'
                : s;
        }).join(',')
    ).join('\r\n');

    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
}

function exportTableToXLSX(tableEl, filename) {
    if (typeof tableEl === 'string') tableEl = document.getElementById(tableEl);
    if (!tableEl) return;
    const rows = _extractTableData(tableEl);
    const ws = XLSX.utils.aoa_to_sheet(rows);
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, 'Sheet1');
    XLSX.writeFile(wb, filename);
}

// ---- Toast notifications ----
function showToast(message, type = 'success', duration = 3500) {
    const container = document.getElementById('toast-container');
    if (!container) return;
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    container.appendChild(toast);
    requestAnimationFrame(() => {
        requestAnimationFrame(() => toast.classList.add('toast-show'));
    });
    setTimeout(() => {
        toast.classList.remove('toast-show');
        toast.addEventListener('transitionend', () => toast.remove(), { once: true });
    }, duration);
}

// ---- Confirmation modal ----
function showConfirm(message, confirmLabel = 'Confirm', title = 'Are you sure?') {
    return new Promise(resolve => {
        const modal  = document.getElementById('confirm-modal');
        const msg    = document.getElementById('confirm-modal-message');
        const titleEl = document.getElementById('confirm-modal-title');
        if (titleEl) titleEl.textContent = title;
        msg.textContent = message;

        // Re-query after potential cloneNode replacements
        const freshConfirm = () => document.getElementById('confirm-modal-confirm');
        const freshCancel  = () => document.getElementById('confirm-modal-cancel');

        freshConfirm().textContent = confirmLabel;
        modal.style.display = 'flex';

        const cleanup = (result) => {
            modal.style.display = 'none';
            // Remove listeners by replacing nodes
            const c = freshConfirm();
            const x = freshCancel();
            c.replaceWith(c.cloneNode(true));
            x.replaceWith(x.cloneNode(true));
            resolve(result);
        };

        freshConfirm().addEventListener('click', () => cleanup(true),  { once: true });
        freshCancel().addEventListener('click',  () => cleanup(false), { once: true });
    });
}

// ---- Inline field validation ----
function setFieldError(fieldEl, message) {
    clearFieldError(fieldEl);
    fieldEl.classList.add('field-error');
    const err = document.createElement('span');
    err.className = 'field-error-message';
    err.textContent = message;
    fieldEl.insertAdjacentElement('afterend', err);
}

function clearFieldError(fieldEl) {
    fieldEl.classList.remove('field-error');
    const next = fieldEl.nextElementSibling;
    if (next?.classList.contains('field-error-message')) next.remove();
}

function clearAllFieldErrors(formEl) {
    formEl.querySelectorAll('.field-error').forEach(el => clearFieldError(el));
}

// ---- Button loading state ----
function setButtonLoading(btn, loadingText = 'Saving…') {
    btn.disabled = true;
    btn._originalText = btn.textContent;
    btn.textContent = loadingText;
}

function clearButtonLoading(btn) {
    btn.disabled = false;
    btn.textContent = btn._originalText ?? btn.textContent;
}

document.addEventListener('DOMContentLoaded', () => {
    // Mobile Menu Toggle
    const menuBtn = document.getElementById("mobile-menu-btn");
    const navMenu = document.getElementById("main-nav");
    if(menuBtn && navMenu) {
        menuBtn.addEventListener("click", () => {
            navMenu.classList.toggle("show");
        });
    }

    const usernameDisplay = document.getElementById('username-display');
    const logoutBtn = document.getElementById('dropdown-logout-btn');
    
    // Toggle user dropdown menu
    if (usernameDisplay) {
        usernameDisplay.addEventListener('click', (event) => {
            event.stopPropagation();
            const dropdown = document.getElementById('user-dropdown-menu');
            if (dropdown) dropdown.classList.toggle('show');
        });
    }
    
    // Close dropdown when clicking outside
    document.addEventListener('click', (event) => {
        const dropdown = document.getElementById('user-dropdown-menu');
        if (dropdown && usernameDisplay && !usernameDisplay.contains(event.target) && !dropdown.contains(event.target)) {
            dropdown.classList.remove('show');
        }
    });

    // Handle Logout
    if (logoutBtn) {
        logoutBtn.addEventListener('click', async (event) => {
            event.preventDefault();
            if (!await showConfirm('Are you sure you want to logout?', 'Logout')) return;

            try {
                const response = await fetch('/api/logout', { method: 'POST' });
                if (!response.ok) {
                    throw new Error(`Server rejected logout: ${response.status} ${response.statusText}`);
                }
                window.location.href = '/login';
            } catch (error) {
                console.error('Logout failed:', error);
                showToast('Logout failed. Check the browser console for details.', 'error');
            }
        });
    }

    // Auto-wire CSV + XLSX export buttons for tables with data-export-csv attribute
    document.querySelectorAll('table[data-export-csv]').forEach(table => {
        const csvFilename  = table.dataset.exportCsv;
        const xlsxFilename = csvFilename.replace(/\.csv$/i, '.xlsx');

        const csvBtn = document.createElement('button');
        csvBtn.className = 'btn btn-secondary btn-sm';
        csvBtn.textContent = '↓ CSV';
        csvBtn.title = 'Download as CSV';
        csvBtn.addEventListener('click', () => exportTableToCSV(table, csvFilename));

        const xlsxBtn = document.createElement('button');
        xlsxBtn.className = 'btn btn-secondary btn-sm';
        xlsxBtn.textContent = '↓ XLSX';
        xlsxBtn.title = 'Download as Excel spreadsheet';
        xlsxBtn.addEventListener('click', () => exportTableToXLSX(table, xlsxFilename));

        // Find nearest preceding .tab-toolbar, searching up through ancestors
        let toolbar = null;
        let cur = table;
        outer: while (cur && cur !== document.body) {
            let prev = cur.previousElementSibling;
            while (prev) {
                if (prev.classList.contains('tab-toolbar')) { toolbar = prev; break outer; }
                prev = prev.previousElementSibling;
            }
            cur = cur.parentElement;
        }

        if (toolbar) {
            toolbar.appendChild(csvBtn);
            toolbar.appendChild(xlsxBtn);
        } else {
            const wrap = document.createElement('div');
            wrap.className = 'tab-toolbar';
            wrap.appendChild(csvBtn);
            wrap.appendChild(xlsxBtn);
            table.parentNode.insertBefore(wrap, table);
        }
    });
});

document.addEventListener('DOMContentLoaded', () => {
    // Fix Theme Dropdown State
    const themeDropdown = document.getElementById('theme-selector'); // Update ID if different
    if (themeDropdown) {
        // Read the saved theme (fallback to 'dark' or 'light' as your default)
        const currentTheme = localStorage.getItem('theme') || 'dark'; 
        themeDropdown.value = currentTheme;
    }
});