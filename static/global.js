// static/global.js - Global JavaScript for handling mobile menu, user dropdown, and logout functionality

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

    // Handle Logout — inline confirm swap, no browser confirm()
    if (logoutBtn) {
        logoutBtn.addEventListener('click', (event) => {
            event.preventDefault();
            event.stopPropagation(); // keep dropdown open

            // Replace the logout item with Sure? Yes / No
            logoutBtn.style.display = 'none';
            const confirmSpan = document.createElement('span');
            confirmSpan.style.cssText = 'display:flex;align-items:center;gap:6px;padding:8px 16px;font-size:0.9em;';
            const label = document.createElement('span');
            label.textContent = 'Log out?';
            const yesBtn = document.createElement('button');
            yesBtn.className = 'btn btn-danger btn-sm';
            yesBtn.textContent = 'Yes';
            yesBtn.addEventListener('click', async () => {
                try {
                    const response = await fetch('/api/logout', { method: 'POST' });
                    if (!response.ok) throw new Error('logout failed');
                    window.location.href = '/login';
                } catch (error) {
                    console.error('Logout failed:', error);
                    confirmSpan.remove();
                    logoutBtn.style.display = '';
                    // Show inline error in the dropdown
                    const errMsg = document.createElement('span');
                    errMsg.style.cssText = 'display:block;padding:6px 16px;font-size:0.8em;color:#dc3545;';
                    errMsg.textContent = 'Logout failed — check console.';
                    logoutBtn.parentNode.insertBefore(errMsg, logoutBtn.nextSibling);
                    setTimeout(() => errMsg.remove(), 4000);
                }
            });
            const noBtn = document.createElement('button');
            noBtn.className = 'btn btn-secondary btn-sm';
            noBtn.textContent = 'No';
            noBtn.addEventListener('click', () => {
                confirmSpan.remove();
                logoutBtn.style.display = '';
            });
            confirmSpan.append(label, yesBtn, noBtn);
            logoutBtn.parentNode.insertBefore(confirmSpan, logoutBtn);
        });
    }
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