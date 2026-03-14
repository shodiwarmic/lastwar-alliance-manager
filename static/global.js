// static/global.js
document.addEventListener('DOMContentLoaded', () => {
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
            if (!confirm('Are you sure you want to logout?')) return;
            
            try {
                const response = await fetch('/api/logout', { method: 'POST' });
                
                // NEW: Force an error if the server rejected the logout
                if (!response.ok) {
                    throw new Error(`Server rejected logout: ${response.status} ${response.statusText}`);
                }
                
                window.location.href = '/login'; 
            } catch (error) {
                console.error('Logout failed:', error);
                alert('Logout failed! Check the F12 Developer Console for the exact error.');
            }
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